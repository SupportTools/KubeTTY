package metrics

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

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
