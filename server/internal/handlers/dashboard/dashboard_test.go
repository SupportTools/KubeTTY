package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/supporttools/KubeTTY/server/internal/gateway/tabs"
	"github.com/supporttools/KubeTTY/server/internal/projects"
)

// mockProjectStore implements ProjectStore for testing
type mockProjectStore struct {
	statusCounts    map[projects.ProjectStatus]int
	statusCountsErr error
	recentlyFailed  []projects.Project
	recentlyFailErr error
	projectList     []projects.Project
	projectListErr  error
}

func (m *mockProjectStore) List(ctx context.Context, filter projects.ListFilter) ([]projects.Project, error) {
	if m.projectListErr != nil {
		return nil, m.projectListErr
	}
	return m.projectList, nil
}

func (m *mockProjectStore) GetStatusCounts(ctx context.Context) (map[projects.ProjectStatus]int, error) {
	if m.statusCountsErr != nil {
		return nil, m.statusCountsErr
	}
	return m.statusCounts, nil
}

func (m *mockProjectStore) GetRecentlyFailed(ctx context.Context, since time.Time, limit int) ([]projects.Project, error) {
	if m.recentlyFailErr != nil {
		return nil, m.recentlyFailErr
	}
	return m.recentlyFailed, nil
}

// mockTabStore implements TabStore for testing
type mockTabStore struct {
	statusCounts         map[string]int
	statusCountsErr      error
	recentErrors         []tabs.Tab
	recentErrorsErr      error
	activeCountByProj    map[string]int
	activeCountByProjErr error
}

func (m *mockTabStore) GetStatusCounts(ctx context.Context) (map[string]int, error) {
	if m.statusCountsErr != nil {
		return nil, m.statusCountsErr
	}
	return m.statusCounts, nil
}

func (m *mockTabStore) GetRecentErrors(ctx context.Context, limit int) ([]tabs.Tab, error) {
	if m.recentErrorsErr != nil {
		return nil, m.recentErrorsErr
	}
	return m.recentErrors, nil
}

func (m *mockTabStore) GetActiveCountByProject(ctx context.Context) (map[string]int, error) {
	if m.activeCountByProjErr != nil {
		return nil, m.activeCountByProjErr
	}
	return m.activeCountByProj, nil
}

// mockMetricsCollector implements MetricsCollector for testing
type mockMetricsCollector struct {
	activeConnections   int
	totalConnections    int64
	totalDisconnects    int64
	disconnectsByReason map[string]int64
	totalErrors         int64
	flowControlPauses   int64
	writeErrors         int64
}

func (m *mockMetricsCollector) GetActiveConnections() int {
	return m.activeConnections
}

func (m *mockMetricsCollector) GetTotalConnections() int64 {
	return m.totalConnections
}

func (m *mockMetricsCollector) GetTotalDisconnects() int64 {
	return m.totalDisconnects
}

func (m *mockMetricsCollector) GetDisconnectsByReason() map[string]int64 {
	if m.disconnectsByReason == nil {
		return make(map[string]int64)
	}
	return m.disconnectsByReason
}

func (m *mockMetricsCollector) GetTotalErrors() int64 {
	return m.totalErrors
}

func (m *mockMetricsCollector) GetFlowControlPauses() int64 {
	return m.flowControlPauses
}

func (m *mockMetricsCollector) GetWriteErrors() int64 {
	return m.writeErrors
}

// TestNew verifies the Handlers constructor
func TestNew(t *testing.T) {
	ps := &mockProjectStore{}
	ts := &mockTabStore{}
	mc := &mockMetricsCollector{}

	h := New(ps, ts, mc)

	if h == nil {
		t.Fatal("New() returned nil")
	}
	if h.projectStore != ps {
		t.Error("projectStore not set correctly")
	}
	if h.tabStore != ts {
		t.Error("tabStore not set correctly")
	}
	if h.metrics != mc {
		t.Error("metrics not set correctly")
	}
}

func TestNew_NilStores(t *testing.T) {
	h := New(nil, nil, nil)

	if h == nil {
		t.Fatal("New() returned nil even with nil stores")
	}
}

// TestGetSummary tests the GetSummary handler
func TestGetSummary(t *testing.T) {
	tests := []struct {
		name             string
		projectStore     *mockProjectStore
		tabStore         *mockTabStore
		metrics          *mockMetricsCollector
		wantStatus       int
		wantActiveConns  int
		wantRunningProjs int
		wantFailedProjs  int
		wantTotalProjs   int
		wantActiveTabs   int
		wantTotalTabs    int
		wantConnections  int64
		wantErrorRate    float64
	}{
		{
			name: "full data with all stores",
			projectStore: &mockProjectStore{
				statusCounts: map[projects.ProjectStatus]int{
					projects.StatusRunning: 5,
					projects.StatusFailed:  2,
					projects.StatusPending: 1,
				},
			},
			tabStore: &mockTabStore{
				statusCounts: map[string]int{
					"connected":    10,
					"connecting":   2,
					"reconnecting": 1,
					"closed":       5,
				},
			},
			metrics: &mockMetricsCollector{
				activeConnections: 15,
				totalConnections:  100,
				totalDisconnects:  50,
				totalErrors:       10,
			},
			wantStatus:       http.StatusOK,
			wantActiveConns:  15,
			wantRunningProjs: 5,
			wantFailedProjs:  2,
			wantTotalProjs:   8,
			wantActiveTabs:   13, // connected + connecting + reconnecting
			wantTotalTabs:    18,
			wantConnections:  100,
			wantErrorRate:    10.0, // 10/100 * 100
		},
		{
			name:         "nil stores returns empty data",
			projectStore: nil,
			tabStore:     nil,
			metrics:      nil,
			wantStatus:   http.StatusOK,
		},
		{
			name: "project store error continues gracefully",
			projectStore: &mockProjectStore{
				statusCountsErr: errors.New("db error"),
			},
			tabStore: &mockTabStore{
				statusCounts: map[string]int{"connected": 5},
			},
			metrics:         &mockMetricsCollector{activeConnections: 5},
			wantStatus:      http.StatusOK,
			wantActiveConns: 5,
			wantActiveTabs:  5,
			wantTotalTabs:   5,
		},
		{
			name: "tab store error continues gracefully",
			projectStore: &mockProjectStore{
				statusCounts: map[projects.ProjectStatus]int{
					projects.StatusRunning: 3,
				},
			},
			tabStore: &mockTabStore{
				statusCountsErr: errors.New("db error"),
			},
			metrics:          &mockMetricsCollector{},
			wantStatus:       http.StatusOK,
			wantRunningProjs: 3,
			wantTotalProjs:   3,
		},
		{
			name:         "zero connections gives zero error rate",
			projectStore: &mockProjectStore{},
			tabStore:     &mockTabStore{},
			metrics: &mockMetricsCollector{
				totalConnections: 0,
				totalErrors:      5,
			},
			wantStatus:    http.StatusOK,
			wantErrorRate: 0, // Should not divide by zero
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ps ProjectStore
			var ts TabStore
			var mc MetricsCollector

			if tt.projectStore != nil {
				ps = tt.projectStore
			}
			if tt.tabStore != nil {
				ts = tt.tabStore
			}
			if tt.metrics != nil {
				mc = tt.metrics
			}

			h := New(ps, ts, mc)

			req := httptest.NewRequest(http.MethodGet, "/api/admin/dashboard/summary", nil)
			rec := httptest.NewRecorder()

			h.GetSummary(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("GetSummary() status = %d, want %d", rec.Code, tt.wantStatus)
			}

			if tt.wantStatus == http.StatusOK {
				var resp SummaryResponse
				if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}

				if resp.ActiveConnections != tt.wantActiveConns {
					t.Errorf("ActiveConnections = %d, want %d", resp.ActiveConnections, tt.wantActiveConns)
				}
				if resp.Projects.Running != tt.wantRunningProjs {
					t.Errorf("Projects.Running = %d, want %d", resp.Projects.Running, tt.wantRunningProjs)
				}
				if resp.Projects.Failed != tt.wantFailedProjs {
					t.Errorf("Projects.Failed = %d, want %d", resp.Projects.Failed, tt.wantFailedProjs)
				}
				if resp.Projects.Total != tt.wantTotalProjs {
					t.Errorf("Projects.Total = %d, want %d", resp.Projects.Total, tt.wantTotalProjs)
				}
				if resp.Tabs.Active != tt.wantActiveTabs {
					t.Errorf("Tabs.Active = %d, want %d", resp.Tabs.Active, tt.wantActiveTabs)
				}
				if resp.Tabs.Total != tt.wantTotalTabs {
					t.Errorf("Tabs.Total = %d, want %d", resp.Tabs.Total, tt.wantTotalTabs)
				}
				if resp.Last24h.Connections != tt.wantConnections {
					t.Errorf("Last24h.Connections = %d, want %d", resp.Last24h.Connections, tt.wantConnections)
				}
				if resp.Last24h.ErrorRate != tt.wantErrorRate {
					t.Errorf("Last24h.ErrorRate = %f, want %f", resp.Last24h.ErrorRate, tt.wantErrorRate)
				}
			}
		})
	}
}

// TestGetErrors tests the GetErrors handler
func TestGetErrors(t *testing.T) {
	now := time.Now()
	projectID := uuid.New()
	errorMsg := "connection failed"

	tests := []struct {
		name           string
		queryParams    string
		projectStore   *mockProjectStore
		tabStore       *mockTabStore
		wantStatus     int
		wantErrorCount int
	}{
		{
			name:        "returns combined errors from projects and tabs",
			queryParams: "",
			projectStore: &mockProjectStore{
				recentlyFailed: []projects.Project{
					{
						ID:            projectID,
						DisplayName:   "Test Project",
						Status:        projects.StatusFailed,
						StatusMessage: "deployment failed",
						UpdatedAt:     now,
					},
				},
			},
			tabStore: &mockTabStore{
				recentErrors: []tabs.Tab{
					{
						TabID:     "tab-1",
						ProjectID: "project-1",
						Status:    tabs.StatusClosed,
						LastError: &errorMsg,
						UpdatedAt: now.Add(-time.Minute),
					},
				},
			},
			wantStatus:     http.StatusOK,
			wantErrorCount: 2,
		},
		{
			name:        "respects limit parameter",
			queryParams: "?limit=1",
			projectStore: &mockProjectStore{
				recentlyFailed: []projects.Project{
					{ID: uuid.New(), DisplayName: "Project 1", UpdatedAt: now},
					{ID: uuid.New(), DisplayName: "Project 2", UpdatedAt: now.Add(-time.Hour)},
				},
			},
			tabStore: &mockTabStore{
				recentErrors: []tabs.Tab{
					{TabID: "tab-1", UpdatedAt: now.Add(-time.Minute)},
				},
			},
			wantStatus:     http.StatusOK,
			wantErrorCount: 1,
		},
		{
			name:        "invalid limit uses default",
			queryParams: "?limit=invalid",
			projectStore: &mockProjectStore{
				recentlyFailed: []projects.Project{},
			},
			tabStore: &mockTabStore{
				recentErrors: []tabs.Tab{},
			},
			wantStatus:     http.StatusOK,
			wantErrorCount: 0,
		},
		{
			name:        "limit over max uses provided value within bounds",
			queryParams: "?limit=201",
			projectStore: &mockProjectStore{
				recentlyFailed: []projects.Project{},
			},
			tabStore: &mockTabStore{
				recentErrors: []tabs.Tab{},
			},
			wantStatus:     http.StatusOK,
			wantErrorCount: 0,
		},
		{
			name:           "nil stores return empty errors",
			queryParams:    "",
			projectStore:   nil,
			tabStore:       nil,
			wantStatus:     http.StatusOK,
			wantErrorCount: 0,
		},
		{
			name:        "project store error continues gracefully",
			queryParams: "",
			projectStore: &mockProjectStore{
				recentlyFailErr: errors.New("db error"),
			},
			tabStore: &mockTabStore{
				recentErrors: []tabs.Tab{
					{TabID: "tab-1", UpdatedAt: now},
				},
			},
			wantStatus:     http.StatusOK,
			wantErrorCount: 1,
		},
		{
			name:        "tab store error continues gracefully",
			queryParams: "",
			projectStore: &mockProjectStore{
				recentlyFailed: []projects.Project{
					{ID: uuid.New(), DisplayName: "Project 1", UpdatedAt: now},
				},
			},
			tabStore: &mockTabStore{
				recentErrorsErr: errors.New("db error"),
			},
			wantStatus:     http.StatusOK,
			wantErrorCount: 1,
		},
		{
			name:        "errors sorted by timestamp descending",
			queryParams: "",
			projectStore: &mockProjectStore{
				recentlyFailed: []projects.Project{
					{ID: uuid.New(), DisplayName: "Old Project", UpdatedAt: now.Add(-time.Hour)},
				},
			},
			tabStore: &mockTabStore{
				recentErrors: []tabs.Tab{
					{TabID: "tab-new", UpdatedAt: now},
				},
			},
			wantStatus:     http.StatusOK,
			wantErrorCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ps ProjectStore
			var ts TabStore

			if tt.projectStore != nil {
				ps = tt.projectStore
			}
			if tt.tabStore != nil {
				ts = tt.tabStore
			}

			h := New(ps, ts, nil)

			req := httptest.NewRequest(http.MethodGet, "/api/admin/dashboard/errors"+tt.queryParams, nil)
			rec := httptest.NewRecorder()

			h.GetErrors(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("GetErrors() status = %d, want %d", rec.Code, tt.wantStatus)
			}

			if tt.wantStatus == http.StatusOK {
				var resp ErrorsResponse
				if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}

				if resp.Total != tt.wantErrorCount {
					t.Errorf("Total errors = %d, want %d", resp.Total, tt.wantErrorCount)
				}
				if len(resp.Errors) != tt.wantErrorCount {
					t.Errorf("len(Errors) = %d, want %d", len(resp.Errors), tt.wantErrorCount)
				}
			}
		})
	}
}

// TestGetErrors_SortOrder verifies errors are sorted by timestamp descending
func TestGetErrors_SortOrder(t *testing.T) {
	now := time.Now()

	projectStore := &mockProjectStore{
		recentlyFailed: []projects.Project{
			{ID: uuid.New(), DisplayName: "Oldest", UpdatedAt: now.Add(-2 * time.Hour)},
		},
	}
	tabStore := &mockTabStore{
		recentErrors: []tabs.Tab{
			{TabID: "tab-1", ProjectID: "proj-1", UpdatedAt: now},
			{TabID: "tab-2", ProjectID: "proj-2", UpdatedAt: now.Add(-time.Hour)},
		},
	}

	h := New(projectStore, tabStore, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/dashboard/errors", nil)
	rec := httptest.NewRecorder()

	h.GetErrors(rec, req)

	var resp ErrorsResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(resp.Errors) != 3 {
		t.Fatalf("Expected 3 errors, got %d", len(resp.Errors))
	}

	// Check errors are sorted by timestamp (most recent first)
	for i := 1; i < len(resp.Errors); i++ {
		if resp.Errors[i].Timestamp.After(resp.Errors[i-1].Timestamp) {
			t.Errorf("Errors not sorted: index %d timestamp (%v) is after index %d timestamp (%v)",
				i, resp.Errors[i].Timestamp, i-1, resp.Errors[i-1].Timestamp)
		}
	}
}

// TestGetUsage tests the GetUsage handler
func TestGetUsage(t *testing.T) {
	tests := []struct {
		name            string
		queryParams     string
		projectStore    *mockProjectStore
		tabStore        *mockTabStore
		wantStatus      int
		wantPeriod      string
		wantTopProjects int
	}{
		{
			name:        "default period is 24h",
			queryParams: "",
			projectStore: &mockProjectStore{
				projectList: []projects.Project{},
			},
			tabStore: &mockTabStore{
				activeCountByProj: map[string]int{},
			},
			wantStatus: http.StatusOK,
			wantPeriod: "24h",
		},
		{
			name:        "valid period 7d",
			queryParams: "?period=7d",
			projectStore: &mockProjectStore{
				projectList: []projects.Project{},
			},
			tabStore: &mockTabStore{
				activeCountByProj: map[string]int{},
			},
			wantStatus: http.StatusOK,
			wantPeriod: "7d",
		},
		{
			name:        "valid period 30d",
			queryParams: "?period=30d",
			projectStore: &mockProjectStore{
				projectList: []projects.Project{},
			},
			tabStore: &mockTabStore{
				activeCountByProj: map[string]int{},
			},
			wantStatus: http.StatusOK,
			wantPeriod: "30d",
		},
		{
			name:        "invalid period returns 400",
			queryParams: "?period=invalid",
			wantStatus:  http.StatusBadRequest,
		},
		{
			name:        "returns top projects sorted by connection count",
			queryParams: "",
			projectStore: &mockProjectStore{
				projectList: []projects.Project{
					{Name: "project-a", DisplayName: "Project A"},
					{Name: "project-b", DisplayName: "Project B"},
					{Name: "project-c", DisplayName: "Project C"},
				},
			},
			tabStore: &mockTabStore{
				activeCountByProj: map[string]int{
					"project-a": 5,
					"project-b": 10,
					"project-c": 3,
				},
			},
			wantStatus:      http.StatusOK,
			wantPeriod:      "24h",
			wantTopProjects: 3,
		},
		{
			name:         "nil stores return empty data",
			queryParams:  "",
			projectStore: nil,
			tabStore:     nil,
			wantStatus:   http.StatusOK,
			wantPeriod:   "24h",
		},
		{
			name:        "tab store error continues gracefully",
			queryParams: "",
			projectStore: &mockProjectStore{
				projectList: []projects.Project{},
			},
			tabStore: &mockTabStore{
				activeCountByProjErr: errors.New("db error"),
			},
			wantStatus: http.StatusOK,
			wantPeriod: "24h",
		},
		{
			name:        "project store list error continues gracefully",
			queryParams: "",
			projectStore: &mockProjectStore{
				projectListErr: errors.New("db error"),
			},
			tabStore: &mockTabStore{
				activeCountByProj: map[string]int{"project-a": 5},
			},
			wantStatus:      http.StatusOK,
			wantPeriod:      "24h",
			wantTopProjects: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ps ProjectStore
			var ts TabStore

			if tt.projectStore != nil {
				ps = tt.projectStore
			}
			if tt.tabStore != nil {
				ts = tt.tabStore
			}

			h := New(ps, ts, nil)

			req := httptest.NewRequest(http.MethodGet, "/api/admin/dashboard/usage"+tt.queryParams, nil)
			rec := httptest.NewRecorder()

			h.GetUsage(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("GetUsage() status = %d, want %d", rec.Code, tt.wantStatus)
			}

			if tt.wantStatus == http.StatusOK {
				var resp UsageResponse
				if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}

				if resp.Period != tt.wantPeriod {
					t.Errorf("Period = %q, want %q", resp.Period, tt.wantPeriod)
				}
				if len(resp.TopProjects) != tt.wantTopProjects {
					t.Errorf("len(TopProjects) = %d, want %d", len(resp.TopProjects), tt.wantTopProjects)
				}
			}
		})
	}
}

// TestGetUsage_TopProjectsSort verifies projects are sorted by connection count descending
func TestGetUsage_TopProjectsSort(t *testing.T) {
	projectStore := &mockProjectStore{
		projectList: []projects.Project{
			{Name: "low", DisplayName: "Low"},
			{Name: "high", DisplayName: "High"},
			{Name: "medium", DisplayName: "Medium"},
		},
	}
	tabStore := &mockTabStore{
		activeCountByProj: map[string]int{
			"low":    1,
			"high":   100,
			"medium": 50,
		},
	}

	h := New(projectStore, tabStore, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/dashboard/usage", nil)
	rec := httptest.NewRecorder()

	h.GetUsage(rec, req)

	var resp UsageResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(resp.TopProjects) != 3 {
		t.Fatalf("Expected 3 projects, got %d", len(resp.TopProjects))
	}

	// Check sorted by connections descending
	for i := 1; i < len(resp.TopProjects); i++ {
		if resp.TopProjects[i].Connections > resp.TopProjects[i-1].Connections {
			t.Errorf("Projects not sorted: index %d has more connections (%d) than index %d (%d)",
				i, resp.TopProjects[i].Connections, i-1, resp.TopProjects[i-1].Connections)
		}
	}

	// First should be "high"
	if resp.TopProjects[0].Name != "high" {
		t.Errorf("First project should be 'high', got %q", resp.TopProjects[0].Name)
	}
}

// TestGetUsage_LimitsTop10 verifies only top 10 projects are returned
func TestGetUsage_LimitsTop10(t *testing.T) {
	projectList := make([]projects.Project, 15)
	activeCount := make(map[string]int, 15)

	for i := 0; i < 15; i++ {
		name := "project-" + string(rune('a'+i))
		projectList[i] = projects.Project{Name: name, DisplayName: name}
		activeCount[name] = i + 1
	}

	projectStore := &mockProjectStore{projectList: projectList}
	tabStore := &mockTabStore{activeCountByProj: activeCount}

	h := New(projectStore, tabStore, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/dashboard/usage", nil)
	rec := httptest.NewRecorder()

	h.GetUsage(rec, req)

	var resp UsageResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(resp.TopProjects) != 10 {
		t.Errorf("Expected 10 projects (max), got %d", len(resp.TopProjects))
	}
}

// TestGetMetrics tests the GetMetrics handler
func TestGetMetrics(t *testing.T) {
	tests := []struct {
		name        string
		queryParams string
		metrics     *mockMetricsCollector
		wantStatus  int
		wantPeriod  string
		wantPauses  int64
		wantWrites  int64
	}{
		{
			name:        "default period is 1h",
			queryParams: "",
			metrics:     &mockMetricsCollector{},
			wantStatus:  http.StatusOK,
			wantPeriod:  "1h",
		},
		{
			name:        "valid period 24h",
			queryParams: "?period=24h",
			metrics:     &mockMetricsCollector{},
			wantStatus:  http.StatusOK,
			wantPeriod:  "24h",
		},
		{
			name:        "valid period 7d",
			queryParams: "?period=7d",
			metrics:     &mockMetricsCollector{},
			wantStatus:  http.StatusOK,
			wantPeriod:  "7d",
		},
		{
			name:        "invalid period returns 400",
			queryParams: "?period=invalid",
			metrics:     &mockMetricsCollector{},
			wantStatus:  http.StatusBadRequest,
		},
		{
			name:        "returns metrics data",
			queryParams: "",
			metrics: &mockMetricsCollector{
				disconnectsByReason: map[string]int64{
					"timeout":     5,
					"user_closed": 10,
				},
				flowControlPauses: 25,
				writeErrors:       3,
			},
			wantStatus: http.StatusOK,
			wantPeriod: "1h",
			wantPauses: 25,
			wantWrites: 3,
		},
		{
			name:        "nil metrics returns empty data",
			queryParams: "",
			metrics:     nil,
			wantStatus:  http.StatusOK,
			wantPeriod:  "1h",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var mc MetricsCollector
			if tt.metrics != nil {
				mc = tt.metrics
			}

			h := New(nil, nil, mc)

			req := httptest.NewRequest(http.MethodGet, "/api/admin/dashboard/metrics"+tt.queryParams, nil)
			rec := httptest.NewRecorder()

			h.GetMetrics(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("GetMetrics() status = %d, want %d", rec.Code, tt.wantStatus)
			}

			if tt.wantStatus == http.StatusOK {
				var resp MetricsResponse
				if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}

				if resp.Period != tt.wantPeriod {
					t.Errorf("Period = %q, want %q", resp.Period, tt.wantPeriod)
				}
				if resp.FlowControlPauses != tt.wantPauses {
					t.Errorf("FlowControlPauses = %d, want %d", resp.FlowControlPauses, tt.wantPauses)
				}
				if resp.WriteErrors != tt.wantWrites {
					t.Errorf("WriteErrors = %d, want %d", resp.WriteErrors, tt.wantWrites)
				}
			}
		})
	}
}

// TestGetMetrics_DisconnectsByReason verifies disconnects by reason map is returned
func TestGetMetrics_DisconnectsByReason(t *testing.T) {
	metrics := &mockMetricsCollector{
		disconnectsByReason: map[string]int64{
			"timeout":      5,
			"user_closed":  10,
			"server_error": 2,
		},
	}

	h := New(nil, nil, metrics)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/dashboard/metrics", nil)
	rec := httptest.NewRecorder()

	h.GetMetrics(rec, req)

	var resp MetricsResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(resp.DisconnectsByReason) != 3 {
		t.Errorf("Expected 3 disconnect reasons, got %d", len(resp.DisconnectsByReason))
	}

	if resp.DisconnectsByReason["timeout"] != 5 {
		t.Errorf("timeout disconnects = %d, want 5", resp.DisconnectsByReason["timeout"])
	}
	if resp.DisconnectsByReason["user_closed"] != 10 {
		t.Errorf("user_closed disconnects = %d, want 10", resp.DisconnectsByReason["user_closed"])
	}
}

// TestNullMetricsCollector tests the NullMetricsCollector implementation
func TestNullMetricsCollector(t *testing.T) {
	c := NewNullMetricsCollector()

	if c == nil {
		t.Fatal("NewNullMetricsCollector() returned nil")
	}

	if c.GetActiveConnections() != 0 {
		t.Errorf("GetActiveConnections() = %d, want 0", c.GetActiveConnections())
	}

	if c.GetTotalConnections() != 0 {
		t.Errorf("GetTotalConnections() = %d, want 0", c.GetTotalConnections())
	}

	if c.GetTotalDisconnects() != 0 {
		t.Errorf("GetTotalDisconnects() = %d, want 0", c.GetTotalDisconnects())
	}

	reasons := c.GetDisconnectsByReason()
	if reasons == nil {
		t.Error("GetDisconnectsByReason() returned nil")
	}
	if len(reasons) != 0 {
		t.Errorf("GetDisconnectsByReason() length = %d, want 0", len(reasons))
	}

	if c.GetTotalErrors() != 0 {
		t.Errorf("GetTotalErrors() = %d, want 0", c.GetTotalErrors())
	}

	if c.GetFlowControlPauses() != 0 {
		t.Errorf("GetFlowControlPauses() = %d, want 0", c.GetFlowControlPauses())
	}

	if c.GetWriteErrors() != 0 {
		t.Errorf("GetWriteErrors() = %d, want 0", c.GetWriteErrors())
	}
}

// TestErrorTypes verifies error type strings in response
func TestErrorTypes(t *testing.T) {
	now := time.Now()
	errorMsg := "test error"

	projectStore := &mockProjectStore{
		recentlyFailed: []projects.Project{
			{
				ID:            uuid.New(),
				DisplayName:   "Failed Project",
				Status:        projects.StatusFailed,
				StatusMessage: "crash",
				UpdatedAt:     now,
			},
		},
	}
	tabStore := &mockTabStore{
		recentErrors: []tabs.Tab{
			{
				TabID:     "tab-1",
				ProjectID: "project-1",
				Status:    tabs.StatusClosed,
				LastError: &errorMsg,
				UpdatedAt: now.Add(-time.Minute),
			},
		},
	}

	h := New(projectStore, tabStore, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/dashboard/errors", nil)
	rec := httptest.NewRecorder()

	h.GetErrors(rec, req)

	var resp ErrorsResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Find project_failed and tab_error types
	var foundProjectFailed, foundTabError bool
	for _, e := range resp.Errors {
		switch e.Type {
		case "project_failed":
			foundProjectFailed = true
			if e.Details != "crash" {
				t.Errorf("project_failed Details = %q, want 'crash'", e.Details)
			}
		case "tab_error":
			foundTabError = true
			if e.Details != errorMsg {
				t.Errorf("tab_error Details = %q, want %q", e.Details, errorMsg)
			}
			if e.TabID != "tab-1" {
				t.Errorf("tab_error TabID = %q, want 'tab-1'", e.TabID)
			}
		}
	}

	if !foundProjectFailed {
		t.Error("Expected to find error of type 'project_failed'")
	}
	if !foundTabError {
		t.Error("Expected to find error of type 'tab_error'")
	}
}

// TestGetErrors_NilLastError verifies tabs with nil LastError are handled
func TestGetErrors_NilLastError(t *testing.T) {
	tabStore := &mockTabStore{
		recentErrors: []tabs.Tab{
			{
				TabID:     "tab-1",
				ProjectID: "project-1",
				Status:    tabs.StatusClosed,
				LastError: nil, // nil error
				UpdatedAt: time.Now(),
			},
		},
	}

	h := New(nil, tabStore, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/dashboard/errors", nil)
	rec := httptest.NewRecorder()

	h.GetErrors(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d", rec.Code)
	}

	var resp ErrorsResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(resp.Errors) != 1 {
		t.Fatalf("Expected 1 error, got %d", len(resp.Errors))
	}

	if resp.Errors[0].Details != "" {
		t.Errorf("Expected empty Details for nil LastError, got %q", resp.Errors[0].Details)
	}
}
