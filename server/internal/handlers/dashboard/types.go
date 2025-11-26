package dashboard

import "time"

// SummaryResponse contains the dashboard summary data.
type SummaryResponse struct {
	ActiveConnections int            `json:"activeConnections"`
	Projects          ProjectCounts  `json:"projects"`
	Tabs              TabCounts      `json:"tabs"`
	Last24h           Last24hMetrics `json:"last24h"`
}

// ProjectCounts contains project count breakdown by status.
type ProjectCounts struct {
	Running int `json:"running"`
	Failed  int `json:"failed"`
	Total   int `json:"total"`
}

// TabCounts contains tab count breakdown.
type TabCounts struct {
	Active int `json:"active"`
	Total  int `json:"total"`
}

// Last24hMetrics contains metrics for the last 24 hours.
type Last24hMetrics struct {
	Connections int64   `json:"connections"`
	Disconnects int64   `json:"disconnects"`
	Errors      int64   `json:"errors"`
	ErrorRate   float64 `json:"errorRate"`
}

// MetricsResponse contains time-series metrics data.
type MetricsResponse struct {
	Period               string                `json:"period"`
	ConnectionTimeseries []ConnectionDataPoint `json:"connectionTimeseries"`
	DisconnectsByReason  map[string]int64      `json:"disconnectsByReason"`
	FlowControlPauses    int64                 `json:"flowControlPauses"`
	WriteErrors          int64                 `json:"writeErrors"`
}

// ConnectionDataPoint represents a single data point in the connection time series.
type ConnectionDataPoint struct {
	Timestamp   time.Time `json:"timestamp"`
	Active      int       `json:"active"`
	Connects    int       `json:"connects"`
	Disconnects int       `json:"disconnects"`
}

// ErrorsResponse contains recent error information.
type ErrorsResponse struct {
	Errors []DashboardError `json:"errors"`
	Total  int              `json:"total"`
}

// DashboardError represents a single error event.
type DashboardError struct {
	Type        string    `json:"type"`        // disconnect, project_failed, tab_error
	Reason      string    `json:"reason"`      // error reason or status message
	ProjectID   string    `json:"projectId"`   // associated project ID
	ProjectName string    `json:"projectName"` // human-readable project name
	TabID       string    `json:"tabId,omitempty"`
	Timestamp   time.Time `json:"timestamp"`
	Details     string    `json:"details,omitempty"`
}

// UsageResponse contains usage analytics data.
type UsageResponse struct {
	Period             string         `json:"period"`
	HourlyConnections  []HourlyCount  `json:"hourlyConnections"`
	TopProjects        []ProjectUsage `json:"topProjects"`
	PeakHour           string         `json:"peakHour"`
	AvgSessionDuration int64          `json:"avgSessionDuration"` // seconds
}

// HourlyCount represents connection count for a specific hour.
type HourlyCount struct {
	Hour  time.Time `json:"hour"`
	Count int64     `json:"count"`
}

// ProjectUsage represents usage statistics for a project.
type ProjectUsage struct {
	ProjectID   string `json:"projectId"`
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Connections int64  `json:"connections"`
}
