package handler

import (
	"context"
	"testing"
	"time"
)

func TestRedisPathCheckStore_EnvelopePersistsRunStartedAt(t *testing.T) {
	store := &RedisPathCheckStore{}
	now := time.Now().UTC().Truncate(time.Microsecond)
	req := &DaemonPathCheckRequest{
		ID:           "id-1",
		RuntimeID:    "rt-1",
		Path:         "/tmp/project",
		Status:       DaemonPathCheckRunning,
		CreatedAt:    now.Add(-time.Second),
		UpdatedAt:    now,
		RunStartedAt: &now,
	}

	data, err := store.marshalRequest(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got, err := store.unmarshalRequest(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.RunStartedAt == nil {
		t.Fatal("RunStartedAt lost on round trip — running timeout would never fire across nodes")
	}
	if !got.RunStartedAt.Equal(now) {
		t.Errorf("RunStartedAt drifted: got %s, want %s", got.RunStartedAt, now)
	}
	if got.Status != DaemonPathCheckRunning || got.ID != "id-1" || got.RuntimeID != "rt-1" {
		t.Errorf("request fields lost: %+v", got)
	}
}

func TestRedisPathCheckStore_RunningTimeout(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()
	store := NewRedisPathCheckStore(rdb)

	req, err := store.Create(ctx, "runtime-running-timeout", "/tmp/project")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	popped, err := store.PopPending(ctx, "runtime-running-timeout")
	if err != nil {
		t.Fatalf("pop: %v", err)
	}
	if popped == nil || popped.Status != DaemonPathCheckRunning {
		t.Fatalf("expected running, got %+v", popped)
	}

	aged := time.Now().Add(-daemonPathCheckRunningTimeout - time.Second)
	popped.RunStartedAt = &aged
	if err := store.persistPathCheckRequest(ctx, popped); err != nil {
		t.Fatalf("persist rewound: %v", err)
	}

	got, err := store.Get(ctx, req.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil || got.Status != DaemonPathCheckTimeout {
		t.Fatalf("status = %v, want timeout", got)
	}
}
