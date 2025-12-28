package health

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// mockLeaderInfo implements LeaderInfo for testing
type mockLeaderInfo struct {
	isLeader      bool
	currentLeader string
	identity      string
}

func (m *mockLeaderInfo) IsLeader() bool           { return m.isLeader }
func (m *mockLeaderInfo) GetCurrentLeader() string { return m.currentLeader }
func (m *mockLeaderInfo) GetIdentity() string      { return m.identity }

func TestNewLeaderStatusHandler_NilLeaderInfo(t *testing.T) {
	handler := NewLeaderStatusHandler(nil)

	req := httptest.NewRequest(http.MethodGet, "/api/healthz/leader", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	var response LeaderStatusResponse
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !response.IsLeader {
		t.Error("expected IsLeader to be true when leader election is disabled")
	}
	if response.Status != "disabled" {
		t.Errorf("expected Status to be 'disabled', got '%s'", response.Status)
	}
	if response.CurrentLeader != "n/a" {
		t.Errorf("expected CurrentLeader to be 'n/a', got '%s'", response.CurrentLeader)
	}
	if response.Identity != "n/a" {
		t.Errorf("expected Identity to be 'n/a', got '%s'", response.Identity)
	}
}

func TestNewLeaderStatusHandler_IsLeader(t *testing.T) {
	mock := &mockLeaderInfo{
		isLeader:      true,
		currentLeader: "pod-1",
		identity:      "pod-1",
	}
	handler := NewLeaderStatusHandler(mock)

	req := httptest.NewRequest(http.MethodGet, "/api/healthz/leader", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	var response LeaderStatusResponse
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !response.IsLeader {
		t.Error("expected IsLeader to be true")
	}
	if response.Status != "enabled" {
		t.Errorf("expected Status to be 'enabled', got '%s'", response.Status)
	}
	if response.CurrentLeader != "pod-1" {
		t.Errorf("expected CurrentLeader to be 'pod-1', got '%s'", response.CurrentLeader)
	}
	if response.Identity != "pod-1" {
		t.Errorf("expected Identity to be 'pod-1', got '%s'", response.Identity)
	}
}

func TestNewLeaderStatusHandler_NotLeader(t *testing.T) {
	mock := &mockLeaderInfo{
		isLeader:      false,
		currentLeader: "pod-2",
		identity:      "pod-1",
	}
	handler := NewLeaderStatusHandler(mock)

	req := httptest.NewRequest(http.MethodGet, "/api/healthz/leader", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	var response LeaderStatusResponse
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response.IsLeader {
		t.Error("expected IsLeader to be false")
	}
	if response.Status != "enabled" {
		t.Errorf("expected Status to be 'enabled', got '%s'", response.Status)
	}
	if response.CurrentLeader != "pod-2" {
		t.Errorf("expected CurrentLeader to be 'pod-2', got '%s'", response.CurrentLeader)
	}
	if response.Identity != "pod-1" {
		t.Errorf("expected Identity to be 'pod-1', got '%s'", response.Identity)
	}
}

func TestNewLeaderStatusHandler_NoLeaderYet(t *testing.T) {
	mock := &mockLeaderInfo{
		isLeader:      false,
		currentLeader: "",
		identity:      "pod-1",
	}
	handler := NewLeaderStatusHandler(mock)

	req := httptest.NewRequest(http.MethodGet, "/api/healthz/leader", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	var response LeaderStatusResponse
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response.IsLeader {
		t.Error("expected IsLeader to be false when no leader elected yet")
	}
	if response.CurrentLeader != "" {
		t.Errorf("expected CurrentLeader to be empty, got '%s'", response.CurrentLeader)
	}
}

func TestLeaderStatusResponse_JSONFields(t *testing.T) {
	// Test that JSON field names are correct
	response := LeaderStatusResponse{
		IsLeader:      true,
		CurrentLeader: "pod-123",
		Identity:      "pod-456",
		Status:        "enabled",
	}

	data, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("failed to marshal response: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	expectedFields := []string{"isLeader", "currentLeader", "identity", "status"}
	for _, field := range expectedFields {
		if _, ok := raw[field]; !ok {
			t.Errorf("expected JSON to contain field '%s'", field)
		}
	}
}
