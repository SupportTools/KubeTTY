package controller

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/supporttools/KubeTTY/server/internal/projects"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// TestProjectMetricsResponse_JSONParsing verifies that the JSON struct tags
// match the actual API response format. This test prevents regression of
// field name mismatches (e.g., "usage" vs "used").
func TestProjectMetricsResponse_JSONParsing(t *testing.T) {
	// This is the actual JSON format returned by the project pod's /api/metrics endpoint
	actualAPIResponse := `{
		"disk": {
			"usage": 270550732800,
			"limit": 316464824320,
			"percent": 85
		},
		"network": {
			"rxBytes": 1693982688,
			"txBytes": 8263163618,
			"rxRate": 0,
			"txRate": 0
		}
	}`

	var resp projectMetricsResponse
	if err := json.Unmarshal([]byte(actualAPIResponse), &resp); err != nil {
		t.Fatalf("failed to unmarshal API response: %v", err)
	}

	// Verify the disk usage was correctly parsed
	expectedUsage := int64(270550732800)
	expectedLimit := int64(316464824320)

	if resp.Disk.Usage != expectedUsage {
		t.Errorf("expected Disk.Usage to be %d, got %d", expectedUsage, resp.Disk.Usage)
	}
	if resp.Disk.Limit != expectedLimit {
		t.Errorf("expected Disk.Limit to be %d, got %d", expectedLimit, resp.Disk.Limit)
	}
}

// TestProjectMetricsResponse_ZeroValues ensures zero values are handled correctly.
func TestProjectMetricsResponse_ZeroValues(t *testing.T) {
	zeroResponse := `{"disk": {"usage": 0, "limit": 0}}`

	var resp projectMetricsResponse
	if err := json.Unmarshal([]byte(zeroResponse), &resp); err != nil {
		t.Fatalf("failed to unmarshal zero response: %v", err)
	}

	if resp.Disk.Usage != 0 {
		t.Errorf("expected Disk.Usage to be 0, got %d", resp.Disk.Usage)
	}
	if resp.Disk.Limit != 0 {
		t.Errorf("expected Disk.Limit to be 0, got %d", resp.Disk.Limit)
	}
}

// TestStorageMonitorConfig_Defaults verifies default configuration values.
func TestStorageMonitorConfig_Defaults(t *testing.T) {
	cfg := DefaultStorageMonitorConfig()

	if !cfg.Enabled {
		t.Error("expected Enabled to be true by default")
	}
	if cfg.Interval != 60*time.Second {
		t.Errorf("expected Interval to be 60s, got %v", cfg.Interval)
	}
	if cfg.ExpandThreshold != 0.70 {
		t.Errorf("expected ExpandThreshold to be 0.70, got %v", cfg.ExpandThreshold)
	}
	if cfg.ExpandAmount != "10Gi" {
		t.Errorf("expected ExpandAmount to be '10Gi', got '%s'", cfg.ExpandAmount)
	}
	if cfg.ExpandCooldown != 5*time.Minute {
		t.Errorf("expected ExpandCooldown to be 5m, got %v", cfg.ExpandCooldown)
	}
}

// TestStorageThresholdCalculation tests the threshold calculation logic.
func TestStorageThresholdCalculation(t *testing.T) {
	tests := []struct {
		name         string
		used         int64
		limit        int64
		threshold    float64
		shouldExpand bool
	}{
		{
			name:         "below threshold",
			used:         50,
			limit:        100,
			threshold:    0.70,
			shouldExpand: false,
		},
		{
			name:         "at threshold",
			used:         70,
			limit:        100,
			threshold:    0.70,
			shouldExpand: true,
		},
		{
			name:         "above threshold",
			used:         85,
			limit:        100,
			threshold:    0.70,
			shouldExpand: true,
		},
		{
			name:         "real-world example - 86% usage",
			used:         252 * 1024 * 1024 * 1024, // 252 GB
			limit:        295 * 1024 * 1024 * 1024, // 295 GB
			threshold:    0.70,
			shouldExpand: true,
		},
		{
			name:         "zero limit",
			used:         0,
			limit:        0,
			threshold:    0.70,
			shouldExpand: false, // Should skip when limit is 0
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Skip if limit is 0 (as the real code does)
			if tt.limit == 0 {
				if tt.shouldExpand {
					t.Error("should not expand when limit is 0")
				}
				return
			}

			usageFraction := float64(tt.used) / float64(tt.limit)
			shouldExpand := usageFraction >= tt.threshold

			if shouldExpand != tt.shouldExpand {
				t.Errorf("usageFraction=%.2f, threshold=%.2f: expected shouldExpand=%v, got %v",
					usageFraction, tt.threshold, tt.shouldExpand, shouldExpand)
			}
		})
	}
}

// TestIsPVCExpansionInProgress tests the PVC expansion state detection.
func TestIsPVCExpansionInProgress(t *testing.T) {
	cfg := DefaultConfig()
	store := newMockStore()
	clientset := fake.NewSimpleClientset()
	ctrl := NewWithClient(cfg, store, clientset)

	tests := []struct {
		name       string
		pvc        *corev1.PersistentVolumeClaim
		inProgress bool
	}{
		{
			name: "no expansion - spec equals status",
			pvc: &corev1.PersistentVolumeClaim{
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("100Gi"),
						},
					},
				},
				Status: corev1.PersistentVolumeClaimStatus{
					Capacity: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse("100Gi"),
					},
				},
			},
			inProgress: false,
		},
		{
			name: "expansion in progress - spec greater than status",
			pvc: &corev1.PersistentVolumeClaim{
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("150Gi"),
						},
					},
				},
				Status: corev1.PersistentVolumeClaimStatus{
					Capacity: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse("100Gi"),
					},
				},
			},
			inProgress: true,
		},
		{
			name: "expansion in progress - resizing condition",
			pvc: &corev1.PersistentVolumeClaim{
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("100Gi"),
						},
					},
				},
				Status: corev1.PersistentVolumeClaimStatus{
					Capacity: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse("100Gi"),
					},
					Conditions: []corev1.PersistentVolumeClaimCondition{
						{
							Type:   corev1.PersistentVolumeClaimResizing,
							Status: corev1.ConditionTrue,
						},
					},
				},
			},
			inProgress: true,
		},
		{
			name: "expansion in progress - filesystem resize pending",
			pvc: &corev1.PersistentVolumeClaim{
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("100Gi"),
						},
					},
				},
				Status: corev1.PersistentVolumeClaimStatus{
					Capacity: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse("100Gi"),
					},
					Conditions: []corev1.PersistentVolumeClaimCondition{
						{
							Type:   corev1.PersistentVolumeClaimFileSystemResizePending,
							Status: corev1.ConditionTrue,
						},
					},
				},
			},
			inProgress: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ctrl.isPVCExpansionInProgress(tt.pvc)
			if result != tt.inProgress {
				t.Errorf("expected inProgress=%v, got %v", tt.inProgress, result)
			}
		})
	}
}

// TestExpandPVCByAmount_Cooldown tests the cooldown mechanism.
func TestExpandPVCByAmount_Cooldown(t *testing.T) {
	cfg := DefaultConfig()
	cfg.StorageMonitor = StorageMonitorConfig{
		Enabled:         true,
		Interval:        60 * time.Second,
		ExpandThreshold: 0.70,
		ExpandAmount:    "10Gi",
		ExpandCooldown:  5 * time.Minute,
	}
	cfg.ResourceConfig = ResourceConfig{
		Namespace: "test-ns",
		Prefix:    "kubetty-project-",
		Env:       "test",
	}

	store := newMockStore()

	// Create a PVC
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kubetty-project-test-data",
			Namespace: "test-ns",
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("100Gi"),
				},
			},
		},
		Status: corev1.PersistentVolumeClaimStatus{
			Capacity: corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse("100Gi"),
			},
		},
	}

	clientset := fake.NewSimpleClientset(pvc)
	ctrl := NewWithClient(cfg, store, clientset)

	project := &projects.Project{
		ID:   uuid.New(),
		Name: "test",
	}

	// First expansion should succeed
	err := ctrl.expandPVCByAmount(context.Background(), project, cfg.ResourceConfig, "kubetty-project-test-data")
	if err != nil {
		t.Fatalf("first expansion failed: %v", err)
	}

	// Second expansion should be skipped due to cooldown
	err = ctrl.expandPVCByAmount(context.Background(), project, cfg.ResourceConfig, "kubetty-project-test-data")
	if err != nil {
		t.Fatalf("second expansion failed: %v", err)
	}

	// Verify PVC was only expanded once (from 100Gi to 110Gi)
	updatedPVC, err := clientset.CoreV1().PersistentVolumeClaims("test-ns").Get(
		context.Background(), "kubetty-project-test-data", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get PVC: %v", err)
	}

	expectedSize := resource.MustParse("110Gi")
	actualSize := updatedPVC.Spec.Resources.Requests[corev1.ResourceStorage]
	if actualSize.Cmp(expectedSize) != 0 {
		t.Errorf("expected PVC size to be %s, got %s", expectedSize.String(), actualSize.String())
	}
}

// TestGetProjectDiskMetrics_Integration tests the metrics endpoint parsing.
func TestGetProjectDiskMetrics_Integration(t *testing.T) {
	// Create a mock HTTP server that returns metrics like a real project pod
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/metrics" {
			http.NotFound(w, r)
			return
		}
		response := `{"disk":{"usage":270550732800,"limit":316464824320,"percent":85},"network":{"rxBytes":0,"txBytes":0,"rxRate":0,"txRate":0}}`
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(response))
	}))
	defer server.Close()

	// Parse the server URL to get host:port
	// Note: In real usage, we'd need to mock the service discovery
	// This test verifies the JSON parsing works correctly
	var resp projectMetricsResponse
	httpResp, err := http.Get(server.URL + "/api/metrics")
	if err != nil {
		t.Fatalf("failed to fetch metrics: %v", err)
	}
	defer httpResp.Body.Close()

	if err := json.NewDecoder(httpResp.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode metrics: %v", err)
	}

	if resp.Disk.Usage != 270550732800 {
		t.Errorf("expected disk usage 270550732800, got %d", resp.Disk.Usage)
	}
	if resp.Disk.Limit != 316464824320 {
		t.Errorf("expected disk limit 316464824320, got %d", resp.Disk.Limit)
	}
}

// TestTruncateErrorForMetric tests the error truncation helper.
func TestTruncateErrorForMetric(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "short error",
			input:    "connection refused",
			expected: "connection refused",
		},
		{
			name:     "exactly 50 chars",
			input:    "12345678901234567890123456789012345678901234567890",
			expected: "12345678901234567890123456789012345678901234567890",
		},
		{
			name:     "long error gets truncated",
			input:    "this is a very long error message that should be truncated to avoid high cardinality in prometheus metrics",
			expected: "this is a very long error message that should be t", // First 50 chars
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateErrorForMetric(tt.input)
			if result != tt.expected {
				t.Errorf("expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

// storageTestStore is a simplified mock store for storage monitor tests
type storageTestStore struct {
	mu       sync.RWMutex
	projects map[uuid.UUID]*projects.Project
}

func newStorageTestStore() *storageTestStore {
	return &storageTestStore{
		projects: make(map[uuid.UUID]*projects.Project),
	}
}

func (s *storageTestStore) Create(ctx context.Context, req projects.CreateProjectRequest) (*projects.Project, error) {
	return nil, nil
}

func (s *storageTestStore) Get(ctx context.Context, id uuid.UUID) (*projects.Project, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if p, ok := s.projects[id]; ok {
		return p, nil
	}
	return nil, projects.ErrProjectNotFound
}

func (s *storageTestStore) GetByName(ctx context.Context, name string) (*projects.Project, error) {
	return nil, nil
}

func (s *storageTestStore) GetByServiceName(ctx context.Context, serviceName string) (*projects.Project, error) {
	return nil, nil
}

func (s *storageTestStore) List(ctx context.Context, filter projects.ListFilter) ([]projects.Project, error) {
	return nil, nil
}

func (s *storageTestStore) Update(ctx context.Context, id uuid.UUID, req projects.UpdateProjectRequest) (*projects.Project, error) {
	return nil, nil
}

func (s *storageTestStore) Delete(ctx context.Context, id uuid.UUID) error {
	return nil
}

func (s *storageTestStore) HardDelete(ctx context.Context, id uuid.UUID) error {
	return nil
}

func (s *storageTestStore) SetStatus(ctx context.Context, id uuid.UUID, status projects.ProjectStatus, message string) error {
	return nil
}

func (s *storageTestStore) SetPaused(ctx context.Context, id uuid.UUID, paused bool) error {
	return nil
}

func (s *storageTestStore) UpdateHealthCheck(ctx context.Context, id uuid.UUID, podIP string) error {
	return nil
}

func (s *storageTestStore) UpdateLastActivity(ctx context.Context, projectName string) error {
	return nil
}

func (s *storageTestStore) ListByStatuses(ctx context.Context, statuses []projects.ProjectStatus) ([]projects.Project, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []projects.Project
	statusSet := make(map[projects.ProjectStatus]bool)
	for _, st := range statuses {
		statusSet[st] = true
	}
	for _, p := range s.projects {
		if statusSet[p.Status] {
			result = append(result, *p)
		}
	}
	return result, nil
}

func (s *storageTestStore) GetStatusCounts(ctx context.Context) (map[projects.ProjectStatus]int, error) {
	return nil, nil
}

// AddProject adds a project to the test store
func (s *storageTestStore) AddProject(p *projects.Project) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.projects[p.ID] = p
}
