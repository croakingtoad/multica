package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisPathCheckStore stores pending / running / completed path-check
// requests in Redis so every API node agrees on the same state.
//
// Storage layout mirrors RedisLocalSkillListStore (same hash tag, same
// record + pending-ZSET pair, same atomic claim script):
//
//	mul:{runtime_pending}:path_check:<request_id>        → JSON request, TTL = retention
//	mul:{runtime_pending}:path_check:pending:<runtime_id> → ZSET { request_id @ created_at }
//
// Reusing claimPendingScript keeps the claim atomic: ZREM + SET run as one
// server-side unit, so a crash or Redis hiccup cannot strand a request
// between "claimed" and "running" (see the list-store comment for the
// full rationale).
const (
	pathCheckKeyPrefix       = "mul:" + runtimePendingRedisHashTag + ":path_check:"
	pathCheckPendingPrefix   = "mul:" + runtimePendingRedisHashTag + ":path_check:pending:"
	pathCheckRedisMaxRetries = 5
)

func pathCheckKey(id string) string { return pathCheckKeyPrefix + id }
func pathCheckPendingKey(runtimeID string) string {
	return pathCheckPendingPrefix + runtimeID
}

type RedisPathCheckStore struct {
	rdb *redis.Client
}

func NewRedisPathCheckStore(rdb *redis.Client) *RedisPathCheckStore {
	return &RedisPathCheckStore{rdb: rdb}
}

func (s *RedisPathCheckStore) Create(ctx context.Context, runtimeID, path string) (*DaemonPathCheckRequest, error) {
	now := time.Now()
	req := &DaemonPathCheckRequest{
		ID:        randomID(),
		RuntimeID: runtimeID,
		Path:      path,
		Status:    DaemonPathCheckPending,
		CreatedAt: now,
		UpdatedAt: now,
	}
	data, err := s.marshalRequest(req)
	if err != nil {
		return nil, err
	}

	requestKey := pathCheckKey(req.ID)
	pendingKey := pathCheckPendingKey(runtimeID)
	pipe := s.rdb.Pipeline()
	pipe.Set(ctx, requestKey, data, daemonPathCheckStoreRetention)
	pipe.ZAdd(ctx, pendingKey, redis.Z{
		Score:  float64(now.UnixNano()),
		Member: req.ID,
	})
	// Keep the pending ZSET alive a bit longer than the individual request
	// so stale members still in the zset can be swept lazily on PopPending.
	pipe.Expire(ctx, pendingKey, daemonPathCheckStoreRetention*2)
	if _, err := pipe.Exec(ctx); err != nil {
		_ = s.rdb.Del(ctx, requestKey).Err()
		_ = s.rdb.ZRem(ctx, pendingKey, req.ID).Err()
		return nil, fmt.Errorf("persist path check request: %w", err)
	}
	return req, nil
}

func (s *RedisPathCheckStore) Get(ctx context.Context, id string) (*DaemonPathCheckRequest, error) {
	raw, err := s.rdb.Get(ctx, pathCheckKey(id)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get path check request: %w", err)
	}
	req, err := s.unmarshalRequest(raw)
	if err != nil {
		return nil, err
	}
	if applyDaemonPathCheckTimeout(req, time.Now()) {
		// Persist the timeout so subsequent Get / PopPending on any node see
		// the terminal state, and drop the id from the pending zset.
		if err := s.persistPathCheckRequest(ctx, req); err != nil {
			return nil, err
		}
		s.rdb.ZRem(ctx, pathCheckPendingKey(req.RuntimeID), req.ID)
	}
	return req, nil
}

func (s *RedisPathCheckStore) persistPathCheckRequest(ctx context.Context, req *DaemonPathCheckRequest) error {
	data, err := s.marshalRequest(req)
	if err != nil {
		return err
	}
	if err := s.rdb.Set(ctx, pathCheckKey(req.ID), data, daemonPathCheckStoreRetention).Err(); err != nil {
		return fmt.Errorf("persist path check request: %w", err)
	}
	return nil
}

// RunStartedAt is internal timeout bookkeeping and is deliberately hidden
// from API responses by json:"-". Redis still needs it so another server
// node can time out a claimed request. Keep it in a private envelope.
type redisPathCheckEnvelope struct {
	Public       *DaemonPathCheckRequest `json:"r"`
	RunStartedAt *time.Time              `json:"s,omitempty"`
}

func (s *RedisPathCheckStore) marshalRequest(req *DaemonPathCheckRequest) ([]byte, error) {
	env := redisPathCheckEnvelope{Public: req, RunStartedAt: req.RunStartedAt}
	data, err := json.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("marshal path check request: %w", err)
	}
	return data, nil
}

func (s *RedisPathCheckStore) unmarshalRequest(raw []byte) (*DaemonPathCheckRequest, error) {
	var env redisPathCheckEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("decode path check request: %w", err)
	}
	if env.Public == nil {
		return nil, fmt.Errorf("decode path check request: missing payload")
	}
	env.Public.RunStartedAt = env.RunStartedAt
	return env.Public, nil
}

// HasPending is a cheap read-only probe (ZCARD) used by the heartbeat
// handler. It does NOT sweep expired / already-claimed entries — a spurious
// "true" is fine because the follow-up PopPending handles the race.
func (s *RedisPathCheckStore) HasPending(ctx context.Context, runtimeID string) (bool, error) {
	cnt, err := s.rdb.ZCard(ctx, pathCheckPendingKey(runtimeID)).Result()
	if err != nil {
		return false, fmt.Errorf("zcard pending: %w", err)
	}
	return cnt > 0, nil
}

func (s *RedisPathCheckStore) PopPending(ctx context.Context, runtimeID string) (*DaemonPathCheckRequest, error) {
	pendingKey := pathCheckPendingKey(runtimeID)

	for attempt := 0; attempt < pathCheckRedisMaxRetries; attempt++ {
		ids, err := s.rdb.ZRange(ctx, pendingKey, 0, 0).Result()
		if err != nil {
			return nil, fmt.Errorf("zrange pending: %w", err)
		}
		if len(ids) == 0 {
			return nil, nil
		}
		id := ids[0]

		req, err := s.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		if req == nil {
			// Record expired but the zset still references it — drop and retry.
			s.rdb.ZRem(ctx, pendingKey, id)
			continue
		}
		if req.Status != DaemonPathCheckPending {
			// Either the timeout fired inside Get or another node already
			// picked it up. Unlink from the pending set and move on.
			s.rdb.ZRem(ctx, pendingKey, id)
			continue
		}

		now := time.Now()
		req.Status = DaemonPathCheckRunning
		req.RunStartedAt = &now
		req.UpdatedAt = now
		data, err := s.marshalRequest(req)
		if err != nil {
			return nil, err
		}

		result, err := claimPendingScript.Run(
			ctx, s.rdb,
			[]string{pendingKey, pathCheckKey(id)},
			id, data, int(daemonPathCheckStoreRetention.Seconds()),
		).Int64()
		if err != nil {
			return nil, fmt.Errorf("claim pending: %w", err)
		}
		if result == 0 {
			// Another node won the race; retry to pick up whatever else is
			// queued (or nothing).
			continue
		}
		return req, nil
	}
	return nil, nil
}

func (s *RedisPathCheckStore) Complete(ctx context.Context, id string, result PathCheckResult) error {
	req, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if req == nil {
		return nil
	}
	req.Status = DaemonPathCheckCompleted
	req.Exists = result.Exists
	req.IsDirectory = result.IsDirectory
	req.Readable = result.Readable
	req.Writable = result.Writable
	req.IsGitRepo = result.IsGitRepo
	req.Reason = result.Reason
	req.UpdatedAt = time.Now()
	return s.persistPathCheckRequest(ctx, req)
}

func (s *RedisPathCheckStore) Fail(ctx context.Context, id string, errMsg string) error {
	req, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if req == nil {
		return nil
	}
	req.Status = DaemonPathCheckFailed
	req.Error = errMsg
	req.UpdatedAt = time.Now()
	return s.persistPathCheckRequest(ctx, req)
}
