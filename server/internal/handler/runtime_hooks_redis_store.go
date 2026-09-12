package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	hookReadKeyPrefix          = "mul:" + runtimePendingRedisHashTag + ":hook_read:req:"
	hookReadPendingPrefix      = "mul:" + runtimePendingRedisHashTag + ":hook_read:pending:"
	hookReadRedisPopMaxRetries = 5
)

func hookReadKey(id string) string               { return hookReadKeyPrefix + id }
func hookReadPendingKey(runtimeID string) string { return hookReadPendingPrefix + runtimeID }

type RedisHookReadStore struct{ rdb *redis.Client }

func NewRedisHookReadStore(rdb *redis.Client) *RedisHookReadStore {
	return &RedisHookReadStore{rdb: rdb}
}

type redisHookReadEnvelope struct {
	Public       *HookReadRequest `json:"r"`
	RunStartedAt *time.Time       `json:"s,omitempty"`
}

func marshalHookRead(req *HookReadRequest) ([]byte, error) {
	raw, err := json.Marshal(redisHookReadEnvelope{Public: req, RunStartedAt: req.RunStartedAt})
	if err != nil {
		return nil, fmt.Errorf("marshal hook read request: %w", err)
	}
	return raw, nil
}

func unmarshalHookRead(raw []byte) (*HookReadRequest, error) {
	var envelope redisHookReadEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("decode hook read request: %w", err)
	}
	if envelope.Public == nil {
		return nil, fmt.Errorf("decode hook read request: missing payload")
	}
	envelope.Public.RunStartedAt = envelope.RunStartedAt
	return envelope.Public, nil
}

func (s *RedisHookReadStore) Create(ctx context.Context, runtimeID string) (*HookReadRequest, error) {
	now := time.Now()
	req := &HookReadRequest{ID: randomID(), RuntimeID: runtimeID, Status: HookReadPending, CreatedAt: now, UpdatedAt: now}
	raw, err := marshalHookRead(req)
	if err != nil {
		return nil, err
	}
	pipe := s.rdb.Pipeline()
	pipe.Set(ctx, hookReadKey(req.ID), raw, hookReadStoreRetention)
	pipe.ZAdd(ctx, hookReadPendingKey(runtimeID), redis.Z{Score: float64(now.UnixNano()), Member: req.ID})
	pipe.Expire(ctx, hookReadPendingKey(runtimeID), hookReadStoreRetention*2)
	if _, err := pipe.Exec(ctx); err != nil {
		_ = s.rdb.Del(ctx, hookReadKey(req.ID)).Err()
		_ = s.rdb.ZRem(ctx, hookReadPendingKey(runtimeID), req.ID).Err()
		return nil, fmt.Errorf("persist hook read request: %w", err)
	}
	return req, nil
}

func (s *RedisHookReadStore) Get(ctx context.Context, id string) (*HookReadRequest, error) {
	return s.load(ctx, id)
}

func (s *RedisHookReadStore) load(ctx context.Context, id string) (*HookReadRequest, error) {
	raw, err := s.rdb.Get(ctx, hookReadKey(id)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get hook read request: %w", err)
	}
	req, err := unmarshalHookRead(raw)
	if err != nil {
		return nil, err
	}
	if applyHookReadTimeout(req, time.Now()) {
		if err := s.persist(ctx, req); err != nil {
			return nil, err
		}
		_ = s.rdb.ZRem(ctx, hookReadPendingKey(req.RuntimeID), req.ID).Err()
	}
	return req, nil
}

func (s *RedisHookReadStore) persist(ctx context.Context, req *HookReadRequest) error {
	raw, err := marshalHookRead(req)
	if err != nil {
		return err
	}
	if err := s.rdb.Set(ctx, hookReadKey(req.ID), raw, hookReadStoreRetention).Err(); err != nil {
		return fmt.Errorf("persist hook read request: %w", err)
	}
	return nil
}

func (s *RedisHookReadStore) HasPending(ctx context.Context, runtimeID string) (bool, error) {
	count, err := s.rdb.ZCard(ctx, hookReadPendingKey(runtimeID)).Result()
	if err != nil {
		return false, fmt.Errorf("zcard pending hook reads: %w", err)
	}
	return count > 0, nil
}

func (s *RedisHookReadStore) PopPending(ctx context.Context, runtimeID string) (*HookReadRequest, error) {
	pendingKey := hookReadPendingKey(runtimeID)
	for attempt := 0; attempt < hookReadRedisPopMaxRetries; attempt++ {
		ids, err := s.rdb.ZRange(ctx, pendingKey, 0, 0).Result()
		if err != nil {
			return nil, fmt.Errorf("zrange pending hook reads: %w", err)
		}
		if len(ids) == 0 {
			return nil, nil
		}
		req, err := s.load(ctx, ids[0])
		if err != nil {
			return nil, err
		}
		if req == nil || req.Status != HookReadPending {
			_ = s.rdb.ZRem(ctx, pendingKey, ids[0]).Err()
			continue
		}
		now := time.Now()
		req.Status = HookReadRunning
		req.RunStartedAt = &now
		req.UpdatedAt = now
		raw, err := marshalHookRead(req)
		if err != nil {
			return nil, err
		}
		claimed, err := claimPendingScript.Run(ctx, s.rdb, []string{pendingKey, hookReadKey(req.ID)}, req.ID, raw, int(hookReadStoreRetention.Seconds())).Int64()
		if err != nil {
			return nil, fmt.Errorf("claim pending hook read: %w", err)
		}
		if claimed == 1 {
			return req, nil
		}
	}
	return nil, nil
}

func (s *RedisHookReadStore) Complete(ctx context.Context, id string, observedAt time.Time) error {
	req, err := s.load(ctx, id)
	if err != nil || req == nil {
		return err
	}
	req.Status = HookReadCompleted
	req.ObservedAt = &observedAt
	req.UpdatedAt = time.Now()
	return s.persist(ctx, req)
}

func (s *RedisHookReadStore) Fail(ctx context.Context, id, errMsg string) error {
	req, err := s.load(ctx, id)
	if err != nil || req == nil {
		return err
	}
	req.Status = HookReadFailed
	req.Error = errMsg
	req.UpdatedAt = time.Now()
	return s.persist(ctx, req)
}
