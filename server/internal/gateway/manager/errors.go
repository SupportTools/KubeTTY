package manager

import "fmt"

// TabLimitExceededError is returned when a client attempts to create more tabs
// than allowed by the project's maxTabsPerClient configuration.
type TabLimitExceededError struct {
	ProjectID string
	Limit     int
}

func (e *TabLimitExceededError) Error() string {
	return fmt.Sprintf("tab limit exceeded for project %s: maximum %d tabs per client", e.ProjectID, e.Limit)
}

// TabTotalLimitExceededError is returned when a project reaches maxTabsTotal.
type TabTotalLimitExceededError struct {
	ProjectID string
	Limit     int
}

func (e *TabTotalLimitExceededError) Error() string {
	return fmt.Sprintf("tab limit exceeded for project %s: maximum %d tabs total", e.ProjectID, e.Limit)
}
