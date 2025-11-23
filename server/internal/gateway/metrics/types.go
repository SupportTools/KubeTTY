// Package metrics provides resource metrics collection for gateway tabs.
package metrics

import "time"

// ResourceMetric represents a single resource measurement with usage and limits.
type ResourceMetric struct {
	Usage   int64 `json:"usage"`   // Current usage in bytes (memory/disk) or millicores (CPU)
	Limit   int64 `json:"limit"`   // Limit/capacity in same units
	Percent int   `json:"percent"` // Usage percentage (0-100)
}

// NetworkMetric represents network I/O metrics.
type NetworkMetric struct {
	RxBytes int64 `json:"rxBytes"` // Total bytes received
	TxBytes int64 `json:"txBytes"` // Total bytes transmitted
	RxRate  int64 `json:"rxRate"`  // Receive rate in bytes/sec
	TxRate  int64 `json:"txRate"`  // Transmit rate in bytes/sec
}

// PodMetadata contains Kubernetes pod and node information.
type PodMetadata struct {
	PodName   string `json:"podName"`   // Name of the pod running the terminal
	NodeName  string `json:"nodeName"`  // Name of the node where pod is scheduled
	Namespace string `json:"namespace"` // Kubernetes namespace
	PodIP     string `json:"podIP"`     // Pod IP address
}

// TabMetrics contains all resource metrics for a tab.
type TabMetrics struct {
	CPU       ResourceMetric `json:"cpu"`
	Memory    ResourceMetric `json:"memory"`
	Disk      ResourceMetric `json:"disk"`
	Network   NetworkMetric  `json:"network"`
	Metadata  *PodMetadata   `json:"metadata,omitempty"` // Optional pod/node metadata
	UpdatedAt time.Time      `json:"updatedAt"`
}

// Status returns a health status based on resource usage.
// Returns: "healthy" (0-60%), "warning" (60-80%), "critical" (80-100%)
func (m *ResourceMetric) Status() string {
	if m.Percent >= 80 {
		return "critical"
	}
	if m.Percent >= 60 {
		return "warning"
	}
	return "healthy"
}

// PodMetrics is the response format from project pod /api/metrics endpoint.
type PodMetrics struct {
	Disk    ResourceMetric `json:"disk"`
	Network NetworkMetric  `json:"network"`
}

// K8sMetrics represents metrics from Kubernetes metrics-server.
type K8sMetrics struct {
	CPU      ResourceMetric `json:"cpu"`
	Memory   ResourceMetric `json:"memory"`
	Metadata *PodMetadata   `json:"metadata,omitempty"` // Pod/node metadata
}

// MetricsUpdate is sent via SSE/WebSocket when metrics change.
type MetricsUpdate struct {
	TabID   string     `json:"tabId"`
	Metrics TabMetrics `json:"metrics"`
}
