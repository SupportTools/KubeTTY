package sessions

import (
	"context"
	"strings"
	"sync"
	"time"
)

// MockStore implements Store interface for unit tests.
type MockStore struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	logs     map[string][]LogEntry
	err      error // For simulating errors
}

// NewMockStore creates a new mock store for testing.
func NewMockStore() *MockStore {
	return &MockStore{
		sessions: make(map[string]*Session),
		logs:     make(map[string][]LogEntry),
	}
}

// SetError configures the mock to return an error on next operation.
func (m *MockStore) SetError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.err = err
}

// AddSession adds a session to the mock store (test helper).
func (m *MockStore) AddSession(sess *Session) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[sess.SessionID] = sess
}

// GetSession retrieves a session by ID.
func (m *MockStore) GetSession(ctx context.Context, sessionID string) (*Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.err != nil {
		err := m.err
		m.err = nil
		return nil, err
	}

	sess, ok := m.sessions[sessionID]
	if !ok {
		return nil, ErrNotFound
	}
	return sess, nil
}

// ListSessions lists sessions by deployment ID.
func (m *MockStore) ListSessions(ctx context.Context, deploymentID string) ([]Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.err != nil {
		err := m.err
		m.err = nil
		return nil, err
	}

	var result []Session
	for _, sess := range m.sessions {
		if sess.DeploymentID == deploymentID {
			result = append(result, *sess)
		}
	}
	return result, nil
}

// UpsertSession creates or updates a session.
func (m *MockStore) UpsertSession(ctx context.Context, s Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.err != nil {
		err := m.err
		m.err = nil
		return err
	}

	s.UpdatedAt = time.Now()
	if s.CreatedAt.IsZero() {
		s.CreatedAt = s.UpdatedAt
	}
	m.sessions[s.SessionID] = &s
	return nil
}

// DeleteSession removes a session.
func (m *MockStore) DeleteSession(ctx context.Context, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.err != nil {
		err := m.err
		m.err = nil
		return err
	}

	if _, ok := m.sessions[sessionID]; !ok {
		return ErrNotFound
	}

	delete(m.sessions, sessionID)
	delete(m.logs, sessionID)
	return nil
}

// ClearAttachments clears all attachments for a deployment.
func (m *MockStore) ClearAttachments(ctx context.Context, deploymentID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.err != nil {
		err := m.err
		m.err = nil
		return err
	}

	for _, sess := range m.sessions {
		if sess.DeploymentID == deploymentID {
			sess.AttachedTo = nil
			sess.AttachedAt = nil
		}
	}
	return nil
}

// SetAttachment sets or clears the attachment for a session.
func (m *MockStore) SetAttachment(ctx context.Context, sessionID, clientID string, attached bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.err != nil {
		err := m.err
		m.err = nil
		return err
	}

	sess, ok := m.sessions[sessionID]
	if !ok {
		return ErrNotFound
	}

	if attached {
		sess.AttachedTo = &clientID
		now := time.Now()
		sess.AttachedAt = &now
	} else {
		sess.AttachedTo = nil
		sess.AttachedAt = nil
	}
	sess.UpdatedAt = time.Now()
	return nil
}

// AppendLog adds a log entry.
func (m *MockStore) AppendLog(ctx context.Context, entry LogEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.err != nil {
		err := m.err
		m.err = nil
		return err
	}

	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now()
	}
	m.logs[entry.SessionID] = append(m.logs[entry.SessionID], entry)

	// Update session log count
	if sess, ok := m.sessions[entry.SessionID]; ok {
		sess.LogCount++
		now := entry.CreatedAt
		sess.LastLogAt = &now
	}
	return nil
}

// ListLogs lists log entries for a session with optional filtering.
func (m *MockStore) ListLogs(ctx context.Context, sessionID string, limit int, filter *LogFilter) (LogsResult, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.err != nil {
		err := m.err
		m.err = nil
		return LogsResult{}, err
	}

	logs, ok := m.logs[sessionID]
	if !ok {
		return LogsResult{Logs: []LogEntry{}, MatchCount: 0}, nil
	}

	// Apply filtering
	var filtered []LogEntry
	for _, log := range logs {
		if filter != nil {
			if filter.Direction != "" && log.Direction != filter.Direction {
				continue
			}
			if filter.Search != "" && !strings.Contains(strings.ToLower(string(log.Data)), strings.ToLower(filter.Search)) {
				continue
			}
		}
		filtered = append(filtered, log)
	}

	matchCount := len(filtered)

	// Apply limit
	if limit > 0 && limit < len(filtered) {
		filtered = filtered[:limit]
	}

	return LogsResult{Logs: filtered, MatchCount: matchCount}, nil
}

// PruneLogs removes logs older than the cutoff time.
func (m *MockStore) PruneLogs(ctx context.Context, cutoff time.Time) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.err != nil {
		err := m.err
		m.err = nil
		return 0, err
	}

	var count int64
	for sessionID, logs := range m.logs {
		var kept []LogEntry
		for _, log := range logs {
			if log.CreatedAt.After(cutoff) {
				kept = append(kept, log)
			} else {
				count++
			}
		}
		m.logs[sessionID] = kept
	}
	return count, nil
}

// TrimLogs keeps only the most recent maxEntries per session.
func (m *MockStore) TrimLogs(ctx context.Context, maxEntries int) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.err != nil {
		err := m.err
		m.err = nil
		return 0, err
	}

	var count int64
	for sessionID, logs := range m.logs {
		if len(logs) > maxEntries {
			removed := len(logs) - maxEntries
			count += int64(removed)
			m.logs[sessionID] = logs[removed:]
		}
	}
	return count, nil
}
