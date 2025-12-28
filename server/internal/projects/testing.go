package projects

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
)

// MockStore implements Store interface for unit tests.
type MockStore struct {
	mu              sync.RWMutex
	projects        map[uuid.UUID]*Project
	projectsByName  map[string]*Project
	err             error // For simulating errors
	targetNamespace string
}

// NewMockStore creates a new mock store for testing.
func NewMockStore() *MockStore {
	return &MockStore{
		projects:        make(map[uuid.UUID]*Project),
		projectsByName:  make(map[string]*Project),
		targetNamespace: "test-namespace",
	}
}

// SetError configures the mock to return an error on next operation.
func (m *MockStore) SetError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.err = err
}

// AddProject adds a project to the mock store (test helper).
func (m *MockStore) AddProject(project *Project) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.projects[project.ID] = project
	m.projectsByName[project.Name] = project
}

// Create creates a new project.
func (m *MockStore) Create(ctx context.Context, req CreateProjectRequest) (*Project, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.err != nil {
		err := m.err
		m.err = nil
		return nil, err
	}

	// Validate name
	if !namePattern.MatchString(req.Name) {
		return nil, ErrInvalidName
	}

	// Check for duplicates
	if _, exists := m.projectsByName[req.Name]; exists {
		return nil, ErrDuplicateName
	}

	req.ApplyDefaults()

	now := time.Now()
	project := &Project{
		ID:               uuid.New(),
		Name:             req.Name,
		DisplayName:      req.DisplayName,
		Description:      req.Description,
		Icon:             req.Icon,
		TargetNamespace:  m.targetNamespace,
		ServiceName:      ComputeServiceName(req.Name),
		SessionID:        uuid.New(),
		UserName:         req.UserName,
		CPURequest:       req.CPURequest,
		CPULimit:         req.CPULimit,
		MemoryRequest:    req.MemoryRequest,
		MemoryLimit:      req.MemoryLimit,
		StorageSize:      req.StorageSize,
		StorageClass:     req.StorageClass,
		AdminNamespaces:  req.AdminNamespaces,
		ReadNamespaces:   req.ReadNamespaces,
		MaxTabsPerClient: req.MaxTabsPerClient,
		MaxTabsTotal:     req.MaxTabsTotal,
		DinDEnabled:      *req.DinDEnabled,
		EnvVars:          req.EnvVars,
		ImageRepository:  req.ImageRepository,
		ImageTag:         req.ImageTag,
		Status:           StatusPending,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	m.projects[project.ID] = project
	m.projectsByName[project.Name] = project
	return project, nil
}

// Get retrieves a project by ID.
func (m *MockStore) Get(ctx context.Context, id uuid.UUID) (*Project, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.err != nil {
		err := m.err
		m.err = nil
		return nil, err
	}

	project, ok := m.projects[id]
	if !ok || project.DeletedAt != nil {
		return nil, ErrProjectNotFound
	}
	return project, nil
}

// GetByName retrieves a project by name.
func (m *MockStore) GetByName(ctx context.Context, name string) (*Project, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.err != nil {
		err := m.err
		m.err = nil
		return nil, err
	}

	project, ok := m.projectsByName[name]
	if !ok || project.DeletedAt != nil {
		return nil, ErrProjectNotFound
	}
	return project, nil
}

// GetByServiceName retrieves a project by service name.
func (m *MockStore) GetByServiceName(ctx context.Context, serviceName string) (*Project, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.err != nil {
		err := m.err
		m.err = nil
		return nil, err
	}

	for _, project := range m.projects {
		if project.ServiceName == serviceName && project.DeletedAt == nil {
			return project, nil
		}
	}
	return nil, ErrProjectNotFound
}

// List returns projects matching the filter.
func (m *MockStore) List(ctx context.Context, filter ListFilter) ([]Project, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.err != nil {
		err := m.err
		m.err = nil
		return nil, err
	}

	var result []Project
	for _, project := range m.projects {
		if !filter.IncludeAll && project.DeletedAt != nil {
			continue
		}
		if filter.Status != "" && project.Status != filter.Status {
			continue
		}
		if filter.UserName != "" && project.UserName != filter.UserName {
			continue
		}
		result = append(result, *project)
	}

	// Apply limit and offset
	if filter.Offset > 0 && filter.Offset < len(result) {
		result = result[filter.Offset:]
	} else if filter.Offset >= len(result) {
		result = []Project{}
	}
	if filter.Limit > 0 && filter.Limit < len(result) {
		result = result[:filter.Limit]
	}

	return result, nil
}

// ListByStatuses returns projects with any of the given statuses.
func (m *MockStore) ListByStatuses(ctx context.Context, statuses []ProjectStatus) ([]Project, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.err != nil {
		err := m.err
		m.err = nil
		return nil, err
	}

	if len(statuses) == 0 {
		return []Project{}, nil
	}

	statusSet := make(map[ProjectStatus]bool)
	includeDeleting := false
	for _, s := range statuses {
		statusSet[s] = true
		if s == StatusDeleting {
			includeDeleting = true
		}
	}

	var result []Project
	for _, project := range m.projects {
		if !statusSet[project.Status] {
			continue
		}
		// Skip deleted unless looking for deleting status
		if project.DeletedAt != nil && !includeDeleting {
			continue
		}
		result = append(result, *project)
	}

	return result, nil
}

// Update updates a project.
func (m *MockStore) Update(ctx context.Context, id uuid.UUID, req UpdateProjectRequest) (*Project, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.err != nil {
		err := m.err
		m.err = nil
		return nil, err
	}

	project, ok := m.projects[id]
	if !ok || project.DeletedAt != nil {
		return nil, ErrProjectNotFound
	}

	if req.DisplayName != nil {
		project.DisplayName = *req.DisplayName
	}
	if req.Description != nil {
		project.Description = *req.Description
	}
	if req.Icon != nil {
		project.Icon = *req.Icon
	}
	if req.CPURequest != nil {
		project.CPURequest = *req.CPURequest
	}
	if req.CPULimit != nil {
		project.CPULimit = *req.CPULimit
	}
	if req.MemoryRequest != nil {
		project.MemoryRequest = *req.MemoryRequest
	}
	if req.MemoryLimit != nil {
		project.MemoryLimit = *req.MemoryLimit
	}
	if req.StorageSize != nil {
		project.StorageSize = *req.StorageSize
	}
	if req.MaxTabsPerClient != nil {
		project.MaxTabsPerClient = *req.MaxTabsPerClient
	}
	if req.MaxTabsTotal != nil {
		project.MaxTabsTotal = *req.MaxTabsTotal
	}
	if req.DinDEnabled != nil {
		project.DinDEnabled = *req.DinDEnabled
	}
	if req.EnvVars != nil {
		project.EnvVars = req.EnvVars
	}
	if req.ImageTag != nil {
		project.ImageTag = *req.ImageTag
	}

	project.UpdatedAt = time.Now()
	return project, nil
}

// Delete soft-deletes a project.
func (m *MockStore) Delete(ctx context.Context, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.err != nil {
		err := m.err
		m.err = nil
		return err
	}

	project, ok := m.projects[id]
	if !ok || project.DeletedAt != nil {
		return ErrProjectNotFound
	}

	now := time.Now()
	project.DeletedAt = &now
	project.Status = StatusDeleting
	return nil
}

// HardDelete permanently removes a project.
func (m *MockStore) HardDelete(ctx context.Context, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.err != nil {
		err := m.err
		m.err = nil
		return err
	}

	if _, ok := m.projects[id]; !ok {
		return ErrProjectNotFound
	}

	project := m.projects[id]
	delete(m.projects, id)
	delete(m.projectsByName, project.Name)
	return nil
}

// SetStatus updates the project status.
func (m *MockStore) SetStatus(ctx context.Context, id uuid.UUID, status ProjectStatus, message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.err != nil {
		err := m.err
		m.err = nil
		return err
	}

	project, ok := m.projects[id]
	if !ok {
		return ErrProjectNotFound
	}

	project.Status = status
	project.StatusMessage = message
	project.UpdatedAt = time.Now()
	return nil
}

// SetPaused updates the paused flag.
func (m *MockStore) SetPaused(ctx context.Context, id uuid.UUID, paused bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.err != nil {
		err := m.err
		m.err = nil
		return err
	}

	project, ok := m.projects[id]
	if !ok || project.DeletedAt != nil {
		return ErrProjectNotFound
	}

	project.Paused = paused
	project.UpdatedAt = time.Now()
	return nil
}

// UpdateHealthCheck updates the health check timestamp.
func (m *MockStore) UpdateHealthCheck(ctx context.Context, id uuid.UUID, podIP string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.err != nil {
		err := m.err
		m.err = nil
		return err
	}

	project, ok := m.projects[id]
	if !ok || project.DeletedAt != nil {
		return ErrProjectNotFound
	}

	now := time.Now()
	project.LastHealthCheck = &now
	project.PodIP = podIP
	return nil
}

// UpdateLastActivity updates the last activity timestamp.
func (m *MockStore) UpdateLastActivity(ctx context.Context, projectName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.err != nil {
		err := m.err
		m.err = nil
		return err
	}

	project, ok := m.projectsByName[projectName]
	if !ok || project.DeletedAt != nil {
		return ErrProjectNotFound
	}

	now := time.Now()
	project.LastActivity = &now
	return nil
}

// GetStatusCounts returns project counts by status.
func (m *MockStore) GetStatusCounts(ctx context.Context) (map[ProjectStatus]int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.err != nil {
		err := m.err
		m.err = nil
		return nil, err
	}

	counts := make(map[ProjectStatus]int)
	for _, project := range m.projects {
		if project.DeletedAt == nil {
			counts[project.Status]++
		}
	}
	return counts, nil
}

// GetRecentlyFailed returns recently failed projects.
func (m *MockStore) GetRecentlyFailed(ctx context.Context, since time.Time, limit int) ([]Project, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.err != nil {
		err := m.err
		m.err = nil
		return nil, err
	}

	var result []Project
	for _, project := range m.projects {
		if project.Status == StatusFailed && project.DeletedAt == nil && project.UpdatedAt.After(since) {
			result = append(result, *project)
		}
	}

	if limit > 0 && limit < len(result) {
		result = result[:limit]
	}

	return result, nil
}
