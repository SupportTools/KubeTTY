package metrics

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---- ResourceMetric tests ----

func TestResourceMetric_Fields(t *testing.T) {
	m := ResourceMetric{
		Usage:   500,
		Limit:   1000,
		Percent: 50,
	}

	assert.Equal(t, int64(500), m.Usage)
	assert.Equal(t, int64(1000), m.Limit)
	assert.Equal(t, 50, m.Percent)
}

func TestResourceMetric_JSONSerialization(t *testing.T) {
	m := ResourceMetric{
		Usage:   1024,
		Limit:   2048,
		Percent: 50,
	}

	data, err := json.Marshal(m)
	require.NoError(t, err)

	var decoded ResourceMetric
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, m, decoded)
}

func TestResourceMetric_Status(t *testing.T) {
	tests := []struct {
		name     string
		percent  int
		expected string
	}{
		{"zero usage", 0, "healthy"},
		{"low usage", 30, "healthy"},
		{"moderate usage", 59, "healthy"},
		{"warning threshold", 60, "warning"},
		{"warning range", 70, "warning"},
		{"upper warning", 79, "warning"},
		{"critical threshold", 80, "critical"},
		{"high critical", 90, "critical"},
		{"max usage", 100, "critical"},
		{"over 100", 150, "critical"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := ResourceMetric{Percent: tt.percent}
			assert.Equal(t, tt.expected, m.Status())
		})
	}
}

func TestTabMetrics_AllStatuses(t *testing.T) {
	metrics := TabMetrics{
		CPU:    ResourceMetric{Usage: 500, Limit: 1000, Percent: 50},
		Memory: ResourceMetric{Usage: 700, Limit: 1000, Percent: 70},
		Disk:   ResourceMetric{Usage: 900, Limit: 1000, Percent: 90},
	}

	assert.Equal(t, "healthy", metrics.CPU.Status())
	assert.Equal(t, "warning", metrics.Memory.Status())
	assert.Equal(t, "critical", metrics.Disk.Status())
}

// ---- NetworkMetric tests ----

func TestNetworkMetric_Fields(t *testing.T) {
	n := NetworkMetric{
		RxBytes: 1048576,
		TxBytes: 524288,
		RxRate:  10240,
		TxRate:  5120,
	}

	assert.Equal(t, int64(1048576), n.RxBytes)
	assert.Equal(t, int64(524288), n.TxBytes)
	assert.Equal(t, int64(10240), n.RxRate)
	assert.Equal(t, int64(5120), n.TxRate)
}

func TestNetworkMetric_JSONSerialization(t *testing.T) {
	n := NetworkMetric{
		RxBytes: 1000000,
		TxBytes: 500000,
		RxRate:  10000,
		TxRate:  5000,
	}

	data, err := json.Marshal(n)
	require.NoError(t, err)

	var decoded NetworkMetric
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, n, decoded)
}

func TestNetworkMetric_ZeroValues(t *testing.T) {
	var n NetworkMetric
	assert.Equal(t, int64(0), n.RxBytes)
	assert.Equal(t, int64(0), n.TxBytes)
	assert.Equal(t, int64(0), n.RxRate)
	assert.Equal(t, int64(0), n.TxRate)
}

// ---- PodMetadata tests ----

func TestPodMetadata_Fields(t *testing.T) {
	m := PodMetadata{
		PodName:   "kubetty-project-test-abc123",
		NodeName:  "node-1",
		Namespace: "kubetty",
		PodIP:     "10.0.0.1",
	}

	assert.Equal(t, "kubetty-project-test-abc123", m.PodName)
	assert.Equal(t, "node-1", m.NodeName)
	assert.Equal(t, "kubetty", m.Namespace)
	assert.Equal(t, "10.0.0.1", m.PodIP)
}

func TestPodMetadata_JSONSerialization(t *testing.T) {
	m := PodMetadata{
		PodName:   "test-pod",
		NodeName:  "test-node",
		Namespace: "default",
		PodIP:     "192.168.1.1",
	}

	data, err := json.Marshal(m)
	require.NoError(t, err)

	var decoded PodMetadata
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, m, decoded)
}

// ---- TabMetrics tests ----

func TestTabMetrics_Fields(t *testing.T) {
	now := time.Now()
	metadata := &PodMetadata{PodName: "test-pod"}

	metrics := TabMetrics{
		CPU:       ResourceMetric{Usage: 100, Limit: 1000, Percent: 10},
		Memory:    ResourceMetric{Usage: 200, Limit: 2000, Percent: 10},
		Disk:      ResourceMetric{Usage: 500, Limit: 5000, Percent: 10},
		Network:   NetworkMetric{RxBytes: 1000, TxBytes: 500},
		Metadata:  metadata,
		UpdatedAt: now,
	}

	assert.Equal(t, int64(100), metrics.CPU.Usage)
	assert.Equal(t, int64(200), metrics.Memory.Usage)
	assert.Equal(t, int64(500), metrics.Disk.Usage)
	assert.Equal(t, int64(1000), metrics.Network.RxBytes)
	assert.Equal(t, metadata, metrics.Metadata)
	assert.Equal(t, now, metrics.UpdatedAt)
}

func TestTabMetrics_JSONSerialization(t *testing.T) {
	metrics := TabMetrics{
		CPU:       ResourceMetric{Usage: 100, Limit: 1000, Percent: 10},
		Memory:    ResourceMetric{Usage: 200, Limit: 2000, Percent: 10},
		Disk:      ResourceMetric{Usage: 500, Limit: 5000, Percent: 10},
		Network:   NetworkMetric{RxBytes: 1000, TxBytes: 500},
		UpdatedAt: time.Now().Round(time.Second),
	}

	data, err := json.Marshal(metrics)
	require.NoError(t, err)

	var decoded TabMetrics
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	// Compare fields (time comparison needs rounding)
	assert.Equal(t, metrics.CPU, decoded.CPU)
	assert.Equal(t, metrics.Memory, decoded.Memory)
	assert.Equal(t, metrics.Disk, decoded.Disk)
	assert.Equal(t, metrics.Network, decoded.Network)
}

func TestTabMetrics_OmitEmptyMetadata(t *testing.T) {
	metrics := TabMetrics{
		CPU:       ResourceMetric{Usage: 100, Limit: 1000, Percent: 10},
		Metadata:  nil, // Should be omitted
		UpdatedAt: time.Now(),
	}

	data, err := json.Marshal(metrics)
	require.NoError(t, err)

	// Metadata field should not be in JSON when nil
	var rawMap map[string]interface{}
	err = json.Unmarshal(data, &rawMap)
	require.NoError(t, err)

	_, hasMetadata := rawMap["metadata"]
	assert.False(t, hasMetadata, "metadata should be omitted when nil")
}

func TestTabMetrics_WithMetadata(t *testing.T) {
	metrics := TabMetrics{
		CPU: ResourceMetric{Usage: 100, Limit: 1000, Percent: 10},
		Metadata: &PodMetadata{
			PodName:   "test-pod",
			NodeName:  "test-node",
			Namespace: "default",
			PodIP:     "10.0.0.1",
		},
		UpdatedAt: time.Now(),
	}

	data, err := json.Marshal(metrics)
	require.NoError(t, err)

	var rawMap map[string]interface{}
	err = json.Unmarshal(data, &rawMap)
	require.NoError(t, err)

	_, hasMetadata := rawMap["metadata"]
	assert.True(t, hasMetadata, "metadata should be present when set")
}

// ---- PodMetrics tests ----

func TestPodMetrics_Fields(t *testing.T) {
	pm := PodMetrics{
		Disk:    ResourceMetric{Usage: 5000, Limit: 10000, Percent: 50},
		Network: NetworkMetric{RxBytes: 1000, TxBytes: 500},
	}

	assert.Equal(t, int64(5000), pm.Disk.Usage)
	assert.Equal(t, int64(10000), pm.Disk.Limit)
	assert.Equal(t, int64(1000), pm.Network.RxBytes)
}

func TestPodMetrics_JSONSerialization(t *testing.T) {
	pm := PodMetrics{
		Disk:    ResourceMetric{Usage: 5000, Limit: 10000, Percent: 50},
		Network: NetworkMetric{RxBytes: 1000, TxBytes: 500, RxRate: 100, TxRate: 50},
	}

	data, err := json.Marshal(pm)
	require.NoError(t, err)

	var decoded PodMetrics
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, pm, decoded)
}

// ---- K8sMetrics tests ----

func TestK8sMetrics_Fields(t *testing.T) {
	km := K8sMetrics{
		CPU:    ResourceMetric{Usage: 500, Limit: 1000, Percent: 50},
		Memory: ResourceMetric{Usage: 1073741824, Limit: 2147483648, Percent: 50},
		Metadata: &PodMetadata{
			PodName: "test-pod",
		},
	}

	assert.Equal(t, int64(500), km.CPU.Usage)
	assert.Equal(t, int64(1073741824), km.Memory.Usage)
	assert.NotNil(t, km.Metadata)
	assert.Equal(t, "test-pod", km.Metadata.PodName)
}

func TestK8sMetrics_JSONSerialization(t *testing.T) {
	km := K8sMetrics{
		CPU:    ResourceMetric{Usage: 500, Limit: 1000, Percent: 50},
		Memory: ResourceMetric{Usage: 1073741824, Limit: 2147483648, Percent: 50},
	}

	data, err := json.Marshal(km)
	require.NoError(t, err)

	var decoded K8sMetrics
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, km.CPU, decoded.CPU)
	assert.Equal(t, km.Memory, decoded.Memory)
}

func TestK8sMetrics_OmitEmptyMetadata(t *testing.T) {
	km := K8sMetrics{
		CPU:      ResourceMetric{Usage: 500, Limit: 1000, Percent: 50},
		Memory:   ResourceMetric{Usage: 1024, Limit: 2048, Percent: 50},
		Metadata: nil,
	}

	data, err := json.Marshal(km)
	require.NoError(t, err)

	var rawMap map[string]interface{}
	err = json.Unmarshal(data, &rawMap)
	require.NoError(t, err)

	_, hasMetadata := rawMap["metadata"]
	assert.False(t, hasMetadata, "metadata should be omitted when nil")
}

// ---- MetricsUpdate tests ----

func TestMetricsUpdate_Fields(t *testing.T) {
	mu := MetricsUpdate{
		TabID: "tab-123",
		Metrics: TabMetrics{
			CPU:       ResourceMetric{Usage: 100, Limit: 1000, Percent: 10},
			UpdatedAt: time.Now(),
		},
	}

	assert.Equal(t, "tab-123", mu.TabID)
	assert.Equal(t, int64(100), mu.Metrics.CPU.Usage)
}

func TestMetricsUpdate_JSONSerialization(t *testing.T) {
	mu := MetricsUpdate{
		TabID: "tab-456",
		Metrics: TabMetrics{
			CPU:       ResourceMetric{Usage: 200, Limit: 1000, Percent: 20},
			Memory:    ResourceMetric{Usage: 300, Limit: 2000, Percent: 15},
			UpdatedAt: time.Now().Round(time.Second),
		},
	}

	data, err := json.Marshal(mu)
	require.NoError(t, err)

	var decoded MetricsUpdate
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, mu.TabID, decoded.TabID)
	assert.Equal(t, mu.Metrics.CPU, decoded.Metrics.CPU)
	assert.Equal(t, mu.Metrics.Memory, decoded.Metrics.Memory)
}

// ---- TabInfo tests ----

func TestTabInfo_Fields(t *testing.T) {
	ti := TabInfo{
		TabID:         "tab-123",
		ProjectID:     "project-1",
		ProjectName:   "my-project",
		Namespace:     "kubetty",
		DownstreamURI: "http://10.0.0.1:8080",
		CPULimit:      1000,
		MemoryLimit:   2147483648,
	}

	assert.Equal(t, "tab-123", ti.TabID)
	assert.Equal(t, "project-1", ti.ProjectID)
	assert.Equal(t, "my-project", ti.ProjectName)
	assert.Equal(t, "kubetty", ti.Namespace)
	assert.Equal(t, "http://10.0.0.1:8080", ti.DownstreamURI)
	assert.Equal(t, int64(1000), ti.CPULimit)
	assert.Equal(t, int64(2147483648), ti.MemoryLimit)
}

func TestTabInfo_ZeroValues(t *testing.T) {
	var ti TabInfo
	assert.Equal(t, "", ti.TabID)
	assert.Equal(t, "", ti.ProjectID)
	assert.Equal(t, int64(0), ti.CPULimit)
	assert.Equal(t, int64(0), ti.MemoryLimit)
}
