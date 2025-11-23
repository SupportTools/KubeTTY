package health

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	gatewayconfig "github.com/supporttools/KubeTTY/server/internal/gateway/config"

	log "github.com/sirupsen/logrus"
)

// Status represents the health state of a project.
type Status string

const (
	// StatusOnline indicates the project is healthy.
	StatusOnline Status = "online"
	// StatusDegraded indicates the project is partially available.
	StatusDegraded Status = "degraded"
	// StatusOffline indicates the project is unreachable.
	StatusOffline Status = "offline"
	// StatusUnknown indicates health has not been checked yet.
	StatusUnknown Status = "unknown"
)

const (
	defaultHealthPath    = "/api/healthz"
	defaultPeriodSeconds = 30
	defaultTimeout       = 5 * time.Second
	consecutiveFailures  = 3 // Mark degraded after this many failures
)

// ProjectStatus holds the health status for a single project.
type ProjectStatus struct {
	Status        Status    `json:"status"`
	LastCheckedAt time.Time `json:"lastCheckedAt"`
	LastError     string    `json:"lastError,omitempty"`
	FailureCount  int       `json:"-"`
}

// Checker performs background health checking for projects.
type Checker struct {
	projects   []gatewayconfig.Project
	statuses   map[string]*ProjectStatus
	mu         sync.RWMutex
	httpClient *http.Client
	stopCh     chan struct{}
	wg         sync.WaitGroup
}

// NewChecker creates a new health checker for the given projects.
func NewChecker(projects []gatewayconfig.Project) *Checker {
	statuses := make(map[string]*ProjectStatus, len(projects))
	for _, p := range projects {
		statuses[p.ID] = &ProjectStatus{
			Status: StatusUnknown,
		}
	}

	return &Checker{
		projects: projects,
		statuses: statuses,
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
		stopCh: make(chan struct{}),
	}
}

// Start begins the background health checking goroutine.
func (c *Checker) Start() {
	c.wg.Add(1)
	go c.run()
	log.Info("Health checker started")
}

// Stop signals the health checker to stop and waits for completion.
func (c *Checker) Stop() {
	close(c.stopCh)
	c.wg.Wait()
	log.Info("Health checker stopped")
}

// GetStatus returns the current health status for a project.
func (c *Checker) GetStatus(projectID string) *ProjectStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if status, ok := c.statuses[projectID]; ok {
		// Return a copy to avoid data races
		return &ProjectStatus{
			Status:        status.Status,
			LastCheckedAt: status.LastCheckedAt,
			LastError:     status.LastError,
			FailureCount:  status.FailureCount,
		}
	}
	return nil
}

// GetAllStatuses returns a copy of all project statuses.
func (c *Checker) GetAllStatuses() map[string]*ProjectStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make(map[string]*ProjectStatus, len(c.statuses))
	for id, status := range c.statuses {
		result[id] = &ProjectStatus{
			Status:        status.Status,
			LastCheckedAt: status.LastCheckedAt,
			LastError:     status.LastError,
			FailureCount:  status.FailureCount,
		}
	}
	return result
}

// AddProject adds a project to the health checker dynamically.
// If the project already exists, it will be skipped.
func (c *Checker) AddProject(p gatewayconfig.Project) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if project already exists
	if _, exists := c.statuses[p.ID]; exists {
		log.WithField("project_id", p.ID).Debug("Project already in health checker, skipping")
		return
	}

	c.statuses[p.ID] = &ProjectStatus{
		Status: StatusUnknown,
	}
	c.projects = append(c.projects, p)
	log.WithFields(log.Fields{
		"project_id": p.ID,
		"service":    p.Service,
		"namespace":  p.Namespace,
	}).Info("Added project to health checker")
}

// RemoveProject removes a project from the health checker.
func (c *Checker) RemoveProject(projectID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Remove from statuses map
	delete(c.statuses, projectID)

	// Remove from projects slice
	for i, p := range c.projects {
		if p.ID == projectID {
			c.projects = append(c.projects[:i], c.projects[i+1:]...)
			break
		}
	}

	log.WithField("project_id", projectID).Info("Removed project from health checker")
}

func (c *Checker) run() {
	defer c.wg.Done()

	// Perform initial check immediately
	c.checkAllProjects()

	// Use a single ticker for the default check period
	// Projects with custom periods will be handled by tracking their last check time
	ticker := time.NewTicker(time.Duration(defaultPeriodSeconds) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
			// Re-fetch current projects on each tick to handle dynamic additions
			c.checkAllProjects()
		}
	}
}

func (c *Checker) groupByPeriod() map[int][]gatewayconfig.Project {
	c.mu.RLock()
	defer c.mu.RUnlock()

	periods := make(map[int][]gatewayconfig.Project)
	for _, p := range c.projects {
		period := c.getPeriod(p)
		periods[period] = append(periods[period], p)
	}
	return periods
}

func (c *Checker) getPeriod(p gatewayconfig.Project) int {
	if p.HealthCheck != nil && p.HealthCheck.PeriodSeconds > 0 {
		return p.HealthCheck.PeriodSeconds
	}
	return defaultPeriodSeconds
}

func (c *Checker) checkAllProjects() {
	// Make a copy of the projects slice to avoid holding the lock during HTTP calls
	c.mu.RLock()
	projects := make([]gatewayconfig.Project, len(c.projects))
	copy(projects, c.projects)
	c.mu.RUnlock()

	for _, p := range projects {
		c.checkProject(p)
	}
}

func (c *Checker) checkProjects(projects []gatewayconfig.Project) {
	for _, p := range projects {
		c.checkProject(p)
	}
}

func (c *Checker) checkProject(p gatewayconfig.Project) {
	url := c.buildHealthURL(p)
	timeout := c.getTimeout(p)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		c.updateStatus(p.ID, StatusOffline, fmt.Sprintf("invalid request: %v", err))
		return
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.handleFailure(p.ID, fmt.Sprintf("request failed: %v", err))
		return
	}
	defer resp.Body.Close()

	// Check for successful status codes (2xx)
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		c.updateStatus(p.ID, StatusOnline, "")
	} else {
		c.handleFailure(p.ID, fmt.Sprintf("unhealthy status: %d", resp.StatusCode))
	}
}

func (c *Checker) buildHealthURL(p gatewayconfig.Project) string {
	path := defaultHealthPath
	if p.HealthCheck != nil && p.HealthCheck.Path != "" {
		path = p.HealthCheck.Path
	}

	// Build Kubernetes service DNS URL
	return fmt.Sprintf("http://%s.%s.svc:%d%s",
		p.Service, p.Namespace, p.Port, path)
}

func (c *Checker) getTimeout(p gatewayconfig.Project) time.Duration {
	if p.HealthCheck != nil && p.HealthCheck.TimeoutSeconds > 0 {
		return time.Duration(p.HealthCheck.TimeoutSeconds) * time.Second
	}
	return defaultTimeout
}

func (c *Checker) handleFailure(projectID string, errMsg string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	status, ok := c.statuses[projectID]
	if !ok {
		return
	}

	status.FailureCount++
	status.LastCheckedAt = time.Now()
	status.LastError = errMsg

	if status.FailureCount >= consecutiveFailures {
		status.Status = StatusOffline
	} else if status.Status == StatusOnline {
		status.Status = StatusDegraded
	}

	log.WithFields(log.Fields{
		"project_id":    projectID,
		"failure_count": status.FailureCount,
		"status":        status.Status,
		"error":         errMsg,
	}).Warn("Health check failed")
}

func (c *Checker) updateStatus(projectID string, newStatus Status, errMsg string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	status, ok := c.statuses[projectID]
	if !ok {
		return
	}

	oldStatus := status.Status
	status.Status = newStatus
	status.LastCheckedAt = time.Now()
	status.LastError = errMsg

	if newStatus == StatusOnline {
		status.FailureCount = 0
	}

	if oldStatus != newStatus {
		log.WithFields(log.Fields{
			"project_id": projectID,
			"old_status": oldStatus,
			"new_status": newStatus,
		}).Info("Project health status changed")
	}
}
