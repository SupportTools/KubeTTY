package metrics

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewCollector_OutsideCluster tests collector creation when not running in a K8s cluster.
// When running outside a cluster (e.g., in tests), the collector should still work
// but with K8s metrics disabled.
func TestNewCollector_OutsideCluster(t *testing.T) {
	var called bool
	callback := func(tabID string, metrics TabMetrics) {
		called = true
	}

	collector, err := NewCollector(time.Second, callback)
	require.NoError(t, err)
	require.NotNil(t, collector)

	// K8s client should be nil when running outside cluster
	assert.Nil(t, collector.metricsClient, "metricsClient should be nil outside cluster")
	assert.NotNil(t, collector.httpClient)
	assert.NotNil(t, collector.tabs)
	assert.NotNil(t, collector.prevNetwork)
	assert.NotNil(t, collector.prevNetworkTs)
	assert.Equal(t, time.Second, collector.interval)
	assert.False(t, called, "callback should not be called during creation")
}

func TestCollector_RegisterUnregisterTab(t *testing.T) {
	collector := &Collector{
		tabs:          make(map[string]TabInfo),
		prevNetwork:   make(map[string]NetworkMetric),
		prevNetworkTs: make(map[string]time.Time),
	}

	tabInfo := TabInfo{
		TabID:         "tab-123",
		ProjectID:     "project-1",
		ProjectName:   "project-1",
		Namespace:     "default",
		DownstreamURI: "http://localhost:8080",
		CPULimit:      1000,
		MemoryLimit:   1073741824,
	}

	// Register tab
	collector.RegisterTab(tabInfo)
	assert.Len(t, collector.tabs, 1)
	assert.Contains(t, collector.tabs, "tab-123")
	assert.Equal(t, tabInfo, collector.tabs["tab-123"])

	// Register another tab
	tabInfo2 := TabInfo{TabID: "tab-456", ProjectID: "project-2"}
	collector.RegisterTab(tabInfo2)
	assert.Len(t, collector.tabs, 2)

	// Simulate network history
	collector.prevNetwork["tab-123"] = NetworkMetric{RxBytes: 1000, TxBytes: 500}
	collector.prevNetworkTs["tab-123"] = time.Now()

	// Unregister first tab
	collector.UnregisterTab("tab-123")
	assert.Len(t, collector.tabs, 1)
	assert.NotContains(t, collector.tabs, "tab-123")
	assert.NotContains(t, collector.prevNetwork, "tab-123")
	assert.NotContains(t, collector.prevNetworkTs, "tab-123")

	// Unregister non-existent tab (should not panic)
	collector.UnregisterTab("non-existent")
	assert.Len(t, collector.tabs, 1)

	// Unregister last tab
	collector.UnregisterTab("tab-456")
	assert.Len(t, collector.tabs, 0)
}

func TestCollector_StartStop(t *testing.T) {
	var callCount atomic.Int32
	callback := func(tabID string, metrics TabMetrics) {
		callCount.Add(1)
	}

	collector := &Collector{
		httpClient:    &http.Client{Timeout: time.Second},
		interval:      50 * time.Millisecond,
		callback:      callback,
		tabs:          make(map[string]TabInfo),
		prevNetwork:   make(map[string]NetworkMetric),
		prevNetworkTs: make(map[string]time.Time),
	}

	// Register a tab with no downstream (won't actually collect anything)
	collector.RegisterTab(TabInfo{TabID: "test-tab"})

	// Start collector
	collector.Start()
	assert.NotNil(t, collector.cancel)

	// Wait for at least 2 collection cycles
	time.Sleep(150 * time.Millisecond)

	// Stop collector
	collector.Stop()

	// Should have collected metrics at least twice
	assert.GreaterOrEqual(t, callCount.Load(), int32(2))

	// Stop again should be safe (nil cancel)
	collector.cancel = nil
	collector.Stop() // Should not panic
}

func TestCollector_CollectPodMetrics(t *testing.T) {
	// Create mock server that returns pod metrics
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/metrics", r.URL.Path)
		assert.Equal(t, http.MethodGet, r.Method)

		response := PodMetrics{
			Disk: ResourceMetric{
				Usage:   5368709120,  // 5GB
				Limit:   10737418240, // 10GB
				Percent: 50,
			},
			Network: NetworkMetric{
				RxBytes: 1048576, // 1MB
				TxBytes: 524288,  // 512KB
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	collector := &Collector{
		httpClient:    &http.Client{Timeout: 5 * time.Second},
		tabs:          make(map[string]TabInfo),
		prevNetwork:   make(map[string]NetworkMetric),
		prevNetworkTs: make(map[string]time.Time),
	}

	tab := TabInfo{
		TabID:         "test-tab",
		DownstreamURI: server.URL,
	}

	metrics := collector.collectPodMetrics(t.Context(), tab)

	assert.Equal(t, int64(5368709120), metrics.Disk.Usage)
	assert.Equal(t, int64(10737418240), metrics.Disk.Limit)
	assert.Equal(t, 50, metrics.Disk.Percent)
	assert.Equal(t, int64(1048576), metrics.Network.RxBytes)
	assert.Equal(t, int64(524288), metrics.Network.TxBytes)
}

func TestCollector_CollectPodMetrics_HTTPErrors(t *testing.T) {
	tests := []struct {
		name        string
		handler     http.HandlerFunc
		wantMetrics PodMetrics
	}{
		{
			name: "server returns 500",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			wantMetrics: PodMetrics{},
		},
		{
			name: "server returns 404",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			wantMetrics: PodMetrics{},
		},
		{
			name: "invalid JSON",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte("not json"))
			},
			wantMetrics: PodMetrics{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			collector := &Collector{
				httpClient:    &http.Client{Timeout: time.Second},
				tabs:          make(map[string]TabInfo),
				prevNetwork:   make(map[string]NetworkMetric),
				prevNetworkTs: make(map[string]time.Time),
			}

			tab := TabInfo{TabID: "test", DownstreamURI: server.URL}
			metrics := collector.collectPodMetrics(t.Context(), tab)
			assert.Equal(t, tt.wantMetrics, metrics)
		})
	}
}

func TestCollector_CollectPodMetrics_Unreachable(t *testing.T) {
	collector := &Collector{
		httpClient:    &http.Client{Timeout: 100 * time.Millisecond},
		tabs:          make(map[string]TabInfo),
		prevNetwork:   make(map[string]NetworkMetric),
		prevNetworkTs: make(map[string]time.Time),
	}

	tab := TabInfo{
		TabID:         "test",
		DownstreamURI: "http://localhost:59999", // Non-existent port
	}

	metrics := collector.collectPodMetrics(t.Context(), tab)
	assert.Equal(t, PodMetrics{}, metrics)
}

func TestCollector_NetworkRateCalculation(t *testing.T) {
	// Create mock server that returns increasing network bytes
	var callCount int
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		callCount++
		currentCall := callCount
		mu.Unlock()

		response := PodMetrics{
			Network: NetworkMetric{
				// Each call increases by 1000 bytes
				RxBytes: int64(currentCall * 1000),
				TxBytes: int64(currentCall * 500),
			},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	collector := &Collector{
		httpClient:    &http.Client{Timeout: 5 * time.Second},
		tabs:          make(map[string]TabInfo),
		prevNetwork:   make(map[string]NetworkMetric),
		prevNetworkTs: make(map[string]time.Time),
	}

	tab := TabInfo{
		TabID:         "rate-test",
		DownstreamURI: server.URL,
	}

	// First call - no rate calculation (no previous data)
	metrics1 := collector.collectPodMetrics(t.Context(), tab)
	assert.Equal(t, int64(1000), metrics1.Network.RxBytes)
	assert.Equal(t, int64(0), metrics1.Network.RxRate) // No rate on first call

	// Wait a bit and collect again
	time.Sleep(100 * time.Millisecond)
	metrics2 := collector.collectPodMetrics(t.Context(), tab)
	assert.Equal(t, int64(2000), metrics2.Network.RxBytes)
	// Rate should be calculated (1000 bytes / ~0.1 seconds ≈ 10000 bytes/sec)
	assert.Greater(t, metrics2.Network.RxRate, int64(0), "RxRate should be positive")
	assert.Greater(t, metrics2.Network.TxRate, int64(0), "TxRate should be positive")
}

func TestCollector_CollectTabMetrics_NilClients(t *testing.T) {
	collector := &Collector{
		httpClient:    &http.Client{Timeout: time.Second},
		metricsClient: nil, // No K8s client
		tabs:          make(map[string]TabInfo),
		prevNetwork:   make(map[string]NetworkMetric),
		prevNetworkTs: make(map[string]time.Time),
	}

	tab := TabInfo{
		TabID:       "test",
		ProjectName: "project-1",
		Namespace:   "default",
		// No DownstreamURI
	}

	metrics := collector.collectTabMetrics(t.Context(), tab)

	// Should return empty metrics but not panic
	assert.Equal(t, int64(0), metrics.CPU.Usage)
	assert.Equal(t, int64(0), metrics.Memory.Usage)
	assert.Equal(t, int64(0), metrics.Disk.Usage)
	assert.NotZero(t, metrics.UpdatedAt)
}

func TestCollector_ConcurrentAccess(t *testing.T) {
	collector := &Collector{
		httpClient:    &http.Client{Timeout: time.Second},
		interval:      10 * time.Millisecond,
		tabs:          make(map[string]TabInfo),
		prevNetwork:   make(map[string]NetworkMetric),
		prevNetworkTs: make(map[string]time.Time),
		callback:      func(tabID string, metrics TabMetrics) {},
	}

	// Start collector
	collector.Start()
	defer collector.Stop()

	// Concurrent registration/unregistration
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(2)
		tabID := "tab-" + string(rune('a'+i))
		go func(id string) {
			defer wg.Done()
			collector.RegisterTab(TabInfo{TabID: id})
		}(tabID)
		go func(id string) {
			defer wg.Done()
			time.Sleep(5 * time.Millisecond)
			collector.UnregisterTab(id)
		}(tabID)
	}
	wg.Wait()
}

func TestCollector_CollectAll_CallsCallback(t *testing.T) {
	var received []string
	var mu sync.Mutex
	callback := func(tabID string, metrics TabMetrics) {
		mu.Lock()
		received = append(received, tabID)
		mu.Unlock()
	}

	collector := &Collector{
		httpClient:    &http.Client{Timeout: time.Second},
		callback:      callback,
		tabs:          make(map[string]TabInfo),
		prevNetwork:   make(map[string]NetworkMetric),
		prevNetworkTs: make(map[string]time.Time),
	}

	// Register multiple tabs
	collector.RegisterTab(TabInfo{TabID: "tab-1"})
	collector.RegisterTab(TabInfo{TabID: "tab-2"})
	collector.RegisterTab(TabInfo{TabID: "tab-3"})

	// Manually trigger collection
	collector.collectAll(t.Context())

	// All tabs should have their callback invoked
	assert.Len(t, received, 3)
	assert.Contains(t, received, "tab-1")
	assert.Contains(t, received, "tab-2")
	assert.Contains(t, received, "tab-3")
}

func TestCollector_NilCallback(t *testing.T) {
	collector := &Collector{
		httpClient:    &http.Client{Timeout: time.Second},
		callback:      nil, // No callback
		tabs:          make(map[string]TabInfo),
		prevNetwork:   make(map[string]NetworkMetric),
		prevNetworkTs: make(map[string]time.Time),
	}

	collector.RegisterTab(TabInfo{TabID: "test"})

	// Should not panic with nil callback
	collector.collectAll(t.Context())
}
