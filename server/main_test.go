package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/supporttools/KubeTTY/server/internal/config"
	"github.com/supporttools/KubeTTY/server/internal/sessions"
)

func TestHandleSessionLogs(t *testing.T) {
	now := time.Now()
	store := &testStore{
		logs: []sessions.LogEntry{
			{SessionID: "abc", Direction: "out", Data: []byte("echo hi"), CreatedAt: now},
		},
	}
	srv := &server{
		cfg:   config.Config{},
		store: store,
	}

	req := httptest.NewRequest(http.MethodGet, "/session/logs?session=abc&limit=25", nil)
	rr := httptest.NewRecorder()

	srv.handleSessionLogs(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", rr.Code, rr.Body.String())
	}
	if store.listSession != "abc" {
		t.Fatalf("expected session abc, got %s", store.listSession)
	}
	if store.listLimit != 25 {
		t.Fatalf("expected limit 25, got %d", store.listLimit)
	}

	var resp struct {
		SessionID string              `json:"sessionId"`
		Logs      []sessions.LogEntry `json:"logs"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.SessionID != "abc" || len(resp.Logs) != 1 {
		t.Fatalf("unexpected response %#v", resp)
	}
}

func TestEnforceLogRetention(t *testing.T) {
	store := &testStore{}
	cfg := config.Config{
		LogRetentionHours: 48,
		LogMaxPerSession:  100,
	}
	srv := &server{
		cfg:   cfg,
		store: store,
	}

	start := time.Now()
	srv.enforceLogRetention(context.Background())

	if len(store.pruneCalls) != 1 {
		t.Fatalf("expected prune call, got %d", len(store.pruneCalls))
	}
	cutoff := store.pruneCalls[0]
	want := start.Add(-48 * time.Hour)
	if cutoff.After(want.Add(time.Minute)) || cutoff.Before(want.Add(-time.Minute)) {
		t.Fatalf("unexpected cutoff %v want around %v", cutoff, want)
	}
	if len(store.trimCalls) != 1 || store.trimCalls[0] != 100 {
		t.Fatalf("expected trim call with 100, got %#v", store.trimCalls)
	}
}

func TestPtySessionBasics(t *testing.T) {
	// Basic test to verify ptySession structure
	ps := &ptySession{
		createdAt: time.Now(),
	}

	if ps.createdAt.IsZero() {
		t.Fatal("expected createdAt to be set")
	}

	// Verify isAlive returns false for uninitialized session
	if ps.isAlive() {
		t.Fatal("expected isAlive to return false for nil cmd")
	}
}

type testStore struct {
	logs        []sessions.LogEntry
	listSession string
	listLimit   int
	pruneCalls  []time.Time
	trimCalls   []int
}

func (s *testStore) GetSession(ctx context.Context, sessionID string) (*sessions.Session, error) {
	return nil, sessions.ErrNotFound
}

func (s *testStore) ListSessions(ctx context.Context, deploymentID string) ([]sessions.Session, error) {
	return nil, nil
}

func (s *testStore) UpsertSession(ctx context.Context, sess sessions.Session) error {
	return nil
}

func (s *testStore) DeleteSession(ctx context.Context, sessionID string) error {
	return nil
}

func (s *testStore) ClearAttachments(ctx context.Context, deploymentID string) error {
	return nil
}

func (s *testStore) SetAttachment(ctx context.Context, sessionID, clientID string, attached bool) error {
	return nil
}

func (s *testStore) AppendLog(ctx context.Context, entry sessions.LogEntry) error {
	return nil
}

func (s *testStore) ListLogs(ctx context.Context, sessionID string, limit int) ([]sessions.LogEntry, error) {
	s.listSession = sessionID
	s.listLimit = limit
	return s.logs, nil
}

func (s *testStore) PruneLogs(ctx context.Context, cutoff time.Time) (int64, error) {
	s.pruneCalls = append(s.pruneCalls, cutoff)
	return 0, nil
}

func (s *testStore) TrimLogs(ctx context.Context, maxEntries int) (int64, error) {
	s.trimCalls = append(s.trimCalls, maxEntries)
	return 0, nil
}
