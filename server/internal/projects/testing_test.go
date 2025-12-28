package projects

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// TestNewMockStore verifies the mock store constructor.
func TestNewMockStore(t *testing.T) {
	store := NewMockStore()

	require.NotNil(t, store)
	require.NotNil(t, store.projects)
	require.NotNil(t, store.projectsByName)
	require.Equal(t, "test-namespace", store.targetNamespace)
}

// TestMockStore_SetError verifies error injection.
func TestMockStore_SetError(t *testing.T) {
	ctx := context.Background()
	store := NewMockStore()
	testErr := errors.New("test error")

	store.SetError(testErr)

	_, err := store.Get(ctx, uuid.New())
	require.Equal(t, testErr, err)

	// Error should be cleared after use
	_, err = store.Get(ctx, uuid.New())
	require.Equal(t, ErrProjectNotFound, err)
}

// TestMockStore_Create verifies project creation.
func TestMockStore_Create(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name    string
		setup   func(*MockStore)
		req     CreateProjectRequest
		wantErr error
	}{
		{
			name:  "valid project",
			setup: func(store *MockStore) {},
			req: CreateProjectRequest{
				Name:        "test-project",
				DisplayName: "Test Project",
				UserName:    "testuser",
			},
			wantErr: nil,
		},
		{
			name:  "invalid name - uppercase",
			setup: func(store *MockStore) {},
			req: CreateProjectRequest{
				Name:        "Test-Project",
				DisplayName: "Test Project",
			},
			wantErr: ErrInvalidName,
		},
		{
			name:  "invalid name - special chars",
			setup: func(store *MockStore) {},
			req: CreateProjectRequest{
				Name:        "test_project",
				DisplayName: "Test Project",
			},
			wantErr: ErrInvalidName,
		},
		{
			name: "duplicate name",
			setup: func(store *MockStore) {
				store.AddProject(&Project{
					ID:   uuid.New(),
					Name: "existing-project",
				})
			},
			req: CreateProjectRequest{
				Name:        "existing-project",
				DisplayName: "Existing Project",
			},
			wantErr: ErrDuplicateName,
		},
		{
			name: "store error",
			setup: func(store *MockStore) {
				store.SetError(errors.New("db error"))
			},
			req: CreateProjectRequest{
				Name:        "test-project",
				DisplayName: "Test Project",
			},
			wantErr: errors.New("db error"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewMockStore()
			tt.setup(store)

			project, err := store.Create(ctx, tt.req)

			if tt.wantErr != nil {
				require.Error(t, err)
				require.Nil(t, project)
			} else {
				require.NoError(t, err)
				require.NotNil(t, project)
				require.Equal(t, tt.req.Name, project.Name)
				require.Equal(t, tt.req.DisplayName, project.DisplayName)
				require.Equal(t, StatusPending, project.Status)
			}
		})
	}
}

// TestMockStore_Get verifies project retrieval by ID.
func TestMockStore_Get(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name    string
		setup   func(*MockStore) uuid.UUID
		wantErr error
	}{
		{
			name: "found",
			setup: func(store *MockStore) uuid.UUID {
				id := uuid.New()
				store.AddProject(&Project{ID: id, Name: "test"})
				return id
			},
			wantErr: nil,
		},
		{
			name: "not found",
			setup: func(store *MockStore) uuid.UUID {
				return uuid.New()
			},
			wantErr: ErrProjectNotFound,
		},
		{
			name: "deleted project",
			setup: func(store *MockStore) uuid.UUID {
				id := uuid.New()
				now := time.Now()
				store.AddProject(&Project{ID: id, Name: "deleted", DeletedAt: &now})
				return id
			},
			wantErr: ErrProjectNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewMockStore()
			id := tt.setup(store)

			project, err := store.Get(ctx, id)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				require.Nil(t, project)
			} else {
				require.NoError(t, err)
				require.NotNil(t, project)
			}
		})
	}
}

// TestMockStore_GetByName verifies project retrieval by name.
func TestMockStore_GetByName(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		projectName string
		setup       func(*MockStore)
		wantErr     error
	}{
		{
			name:        "found",
			projectName: "test-project",
			setup: func(store *MockStore) {
				store.AddProject(&Project{ID: uuid.New(), Name: "test-project"})
			},
			wantErr: nil,
		},
		{
			name:        "not found",
			projectName: "nonexistent",
			setup:       func(store *MockStore) {},
			wantErr:     ErrProjectNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewMockStore()
			tt.setup(store)

			project, err := store.GetByName(ctx, tt.projectName)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.projectName, project.Name)
			}
		})
	}
}

// TestMockStore_GetByServiceName verifies project retrieval by service name.
func TestMockStore_GetByServiceName(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		serviceName string
		setup       func(*MockStore)
		wantErr     error
	}{
		{
			name:        "found",
			serviceName: "kubetty-project-test",
			setup: func(store *MockStore) {
				store.AddProject(&Project{
					ID:          uuid.New(),
					Name:        "test",
					ServiceName: "kubetty-project-test",
				})
			},
			wantErr: nil,
		},
		{
			name:        "not found",
			serviceName: "nonexistent",
			setup:       func(store *MockStore) {},
			wantErr:     ErrProjectNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewMockStore()
			tt.setup(store)

			project, err := store.GetByServiceName(ctx, tt.serviceName)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.serviceName, project.ServiceName)
			}
		})
	}
}

// TestMockStore_List verifies project listing with filters.
func TestMockStore_List(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name      string
		setup     func(*MockStore)
		filter    ListFilter
		wantCount int
		wantErr   bool
	}{
		{
			name: "all active",
			setup: func(store *MockStore) {
				store.AddProject(&Project{ID: uuid.New(), Name: "p1", Status: StatusRunning})
				store.AddProject(&Project{ID: uuid.New(), Name: "p2", Status: StatusRunning})
			},
			filter:    ListFilter{},
			wantCount: 2,
		},
		{
			name: "filter by status",
			setup: func(store *MockStore) {
				store.AddProject(&Project{ID: uuid.New(), Name: "p1", Status: StatusRunning})
				store.AddProject(&Project{ID: uuid.New(), Name: "p2", Status: StatusPending})
			},
			filter:    ListFilter{Status: StatusRunning},
			wantCount: 1,
		},
		{
			name: "filter by user",
			setup: func(store *MockStore) {
				store.AddProject(&Project{ID: uuid.New(), Name: "p1", UserName: "user1"})
				store.AddProject(&Project{ID: uuid.New(), Name: "p2", UserName: "user2"})
			},
			filter:    ListFilter{UserName: "user1"},
			wantCount: 1,
		},
		{
			name: "exclude deleted by default",
			setup: func(store *MockStore) {
				now := time.Now()
				store.AddProject(&Project{ID: uuid.New(), Name: "p1"})
				store.AddProject(&Project{ID: uuid.New(), Name: "p2", DeletedAt: &now})
			},
			filter:    ListFilter{},
			wantCount: 1,
		},
		{
			name: "include deleted",
			setup: func(store *MockStore) {
				now := time.Now()
				store.AddProject(&Project{ID: uuid.New(), Name: "p1"})
				store.AddProject(&Project{ID: uuid.New(), Name: "p2", DeletedAt: &now})
			},
			filter:    ListFilter{IncludeAll: true},
			wantCount: 2,
		},
		{
			name: "with limit",
			setup: func(store *MockStore) {
				store.AddProject(&Project{ID: uuid.New(), Name: "p1"})
				store.AddProject(&Project{ID: uuid.New(), Name: "p2"})
				store.AddProject(&Project{ID: uuid.New(), Name: "p3"})
			},
			filter:    ListFilter{Limit: 2},
			wantCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewMockStore()
			tt.setup(store)

			projects, err := store.List(ctx, tt.filter)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Len(t, projects, tt.wantCount)
			}
		})
	}
}

// TestMockStore_ListByStatuses verifies listing by multiple statuses.
func TestMockStore_ListByStatuses(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name      string
		setup     func(*MockStore)
		statuses  []ProjectStatus
		wantCount int
	}{
		{
			name: "multiple statuses",
			setup: func(store *MockStore) {
				store.AddProject(&Project{ID: uuid.New(), Name: "p1", Status: StatusRunning})
				store.AddProject(&Project{ID: uuid.New(), Name: "p2", Status: StatusPending})
				store.AddProject(&Project{ID: uuid.New(), Name: "p3", Status: StatusFailed})
			},
			statuses:  []ProjectStatus{StatusRunning, StatusPending},
			wantCount: 2,
		},
		{
			name:      "empty statuses",
			setup:     func(store *MockStore) {},
			statuses:  []ProjectStatus{},
			wantCount: 0,
		},
		{
			name: "include deleting",
			setup: func(store *MockStore) {
				now := time.Now()
				store.AddProject(&Project{ID: uuid.New(), Name: "p1", Status: StatusDeleting, DeletedAt: &now})
			},
			statuses:  []ProjectStatus{StatusDeleting},
			wantCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewMockStore()
			tt.setup(store)

			projects, err := store.ListByStatuses(ctx, tt.statuses)

			require.NoError(t, err)
			require.Len(t, projects, tt.wantCount)
		})
	}
}

// TestMockStore_Update verifies project updates.
func TestMockStore_Update(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name    string
		setup   func(*MockStore) uuid.UUID
		req     UpdateProjectRequest
		wantErr error
	}{
		{
			name: "update display name",
			setup: func(store *MockStore) uuid.UUID {
				id := uuid.New()
				store.AddProject(&Project{ID: id, Name: "test", DisplayName: "Old Name"})
				return id
			},
			req: UpdateProjectRequest{
				DisplayName: strPtr("New Name"),
			},
			wantErr: nil,
		},
		{
			name: "update multiple fields",
			setup: func(store *MockStore) uuid.UUID {
				id := uuid.New()
				store.AddProject(&Project{ID: id, Name: "test"})
				return id
			},
			req: UpdateProjectRequest{
				DisplayName: strPtr("New Name"),
				Description: strPtr("New description"),
				CPURequest:  strPtr("1000m"),
			},
			wantErr: nil,
		},
		{
			name: "not found",
			setup: func(store *MockStore) uuid.UUID {
				return uuid.New()
			},
			req: UpdateProjectRequest{
				DisplayName: strPtr("New Name"),
			},
			wantErr: ErrProjectNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewMockStore()
			id := tt.setup(store)

			project, err := store.Update(ctx, id, tt.req)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
				if tt.req.DisplayName != nil {
					require.Equal(t, *tt.req.DisplayName, project.DisplayName)
				}
			}
		})
	}
}

// TestMockStore_Delete verifies soft deletion.
func TestMockStore_Delete(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name    string
		setup   func(*MockStore) uuid.UUID
		wantErr error
	}{
		{
			name: "successful delete",
			setup: func(store *MockStore) uuid.UUID {
				id := uuid.New()
				store.AddProject(&Project{ID: id, Name: "test"})
				return id
			},
			wantErr: nil,
		},
		{
			name: "not found",
			setup: func(store *MockStore) uuid.UUID {
				return uuid.New()
			},
			wantErr: ErrProjectNotFound,
		},
		{
			name: "already deleted",
			setup: func(store *MockStore) uuid.UUID {
				id := uuid.New()
				now := time.Now()
				store.AddProject(&Project{ID: id, Name: "test", DeletedAt: &now})
				return id
			},
			wantErr: ErrProjectNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewMockStore()
			id := tt.setup(store)

			err := store.Delete(ctx, id)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
				// Verify project is marked as deleted
				project := store.projects[id]
				require.NotNil(t, project.DeletedAt)
				require.Equal(t, StatusDeleting, project.Status)
			}
		})
	}
}

// TestMockStore_HardDelete verifies permanent deletion.
func TestMockStore_HardDelete(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name    string
		setup   func(*MockStore) uuid.UUID
		wantErr error
	}{
		{
			name: "successful hard delete",
			setup: func(store *MockStore) uuid.UUID {
				id := uuid.New()
				store.AddProject(&Project{ID: id, Name: "test"})
				return id
			},
			wantErr: nil,
		},
		{
			name: "not found",
			setup: func(store *MockStore) uuid.UUID {
				return uuid.New()
			},
			wantErr: ErrProjectNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewMockStore()
			id := tt.setup(store)

			err := store.HardDelete(ctx, id)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
				// Verify project is removed
				_, ok := store.projects[id]
				require.False(t, ok)
			}
		})
	}
}

// TestMockStore_SetStatus verifies status updates.
func TestMockStore_SetStatus(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name    string
		setup   func(*MockStore) uuid.UUID
		status  ProjectStatus
		message string
		wantErr error
	}{
		{
			name: "set running",
			setup: func(store *MockStore) uuid.UUID {
				id := uuid.New()
				store.AddProject(&Project{ID: id, Name: "test", Status: StatusPending})
				return id
			},
			status:  StatusRunning,
			message: "Ready",
			wantErr: nil,
		},
		{
			name: "not found",
			setup: func(store *MockStore) uuid.UUID {
				return uuid.New()
			},
			status:  StatusRunning,
			wantErr: ErrProjectNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewMockStore()
			id := tt.setup(store)

			err := store.SetStatus(ctx, id, tt.status, tt.message)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
				project, _ := store.Get(ctx, id)
				require.Equal(t, tt.status, project.Status)
				require.Equal(t, tt.message, project.StatusMessage)
			}
		})
	}
}

// TestMockStore_SetPaused verifies pause flag updates.
func TestMockStore_SetPaused(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name    string
		setup   func(*MockStore) uuid.UUID
		paused  bool
		wantErr error
	}{
		{
			name: "pause project",
			setup: func(store *MockStore) uuid.UUID {
				id := uuid.New()
				store.AddProject(&Project{ID: id, Name: "test", Paused: false})
				return id
			},
			paused:  true,
			wantErr: nil,
		},
		{
			name: "unpause project",
			setup: func(store *MockStore) uuid.UUID {
				id := uuid.New()
				store.AddProject(&Project{ID: id, Name: "test", Paused: true})
				return id
			},
			paused:  false,
			wantErr: nil,
		},
		{
			name: "not found",
			setup: func(store *MockStore) uuid.UUID {
				return uuid.New()
			},
			paused:  true,
			wantErr: ErrProjectNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewMockStore()
			id := tt.setup(store)

			err := store.SetPaused(ctx, id, tt.paused)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
				project, _ := store.Get(ctx, id)
				require.Equal(t, tt.paused, project.Paused)
			}
		})
	}
}

// TestMockStore_UpdateHealthCheck verifies health check updates.
func TestMockStore_UpdateHealthCheck(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name    string
		setup   func(*MockStore) uuid.UUID
		podIP   string
		wantErr error
	}{
		{
			name: "update health check",
			setup: func(store *MockStore) uuid.UUID {
				id := uuid.New()
				store.AddProject(&Project{ID: id, Name: "test"})
				return id
			},
			podIP:   "10.0.0.1",
			wantErr: nil,
		},
		{
			name: "not found",
			setup: func(store *MockStore) uuid.UUID {
				return uuid.New()
			},
			podIP:   "10.0.0.1",
			wantErr: ErrProjectNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewMockStore()
			id := tt.setup(store)

			err := store.UpdateHealthCheck(ctx, id, tt.podIP)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
				project, _ := store.Get(ctx, id)
				require.NotNil(t, project.LastHealthCheck)
				require.Equal(t, tt.podIP, project.PodIP)
			}
		})
	}
}

// TestMockStore_UpdateLastActivity verifies last activity updates.
func TestMockStore_UpdateLastActivity(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		projectName string
		setup       func(*MockStore)
		wantErr     error
	}{
		{
			name:        "update activity",
			projectName: "test-project",
			setup: func(store *MockStore) {
				store.AddProject(&Project{ID: uuid.New(), Name: "test-project"})
			},
			wantErr: nil,
		},
		{
			name:        "not found",
			projectName: "nonexistent",
			setup:       func(store *MockStore) {},
			wantErr:     ErrProjectNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewMockStore()
			tt.setup(store)

			err := store.UpdateLastActivity(ctx, tt.projectName)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
				project, _ := store.GetByName(ctx, tt.projectName)
				require.NotNil(t, project.LastActivity)
			}
		})
	}
}

// TestMockStore_GetStatusCounts verifies status count aggregation.
func TestMockStore_GetStatusCounts(t *testing.T) {
	ctx := context.Background()
	store := NewMockStore()

	store.AddProject(&Project{ID: uuid.New(), Name: "p1", Status: StatusRunning})
	store.AddProject(&Project{ID: uuid.New(), Name: "p2", Status: StatusRunning})
	store.AddProject(&Project{ID: uuid.New(), Name: "p3", Status: StatusPending})
	store.AddProject(&Project{ID: uuid.New(), Name: "p4", Status: StatusFailed})

	counts, err := store.GetStatusCounts(ctx)

	require.NoError(t, err)
	require.Equal(t, 2, counts[StatusRunning])
	require.Equal(t, 1, counts[StatusPending])
	require.Equal(t, 1, counts[StatusFailed])
}

// TestMockStore_GetRecentlyFailed verifies recently failed query.
func TestMockStore_GetRecentlyFailed(t *testing.T) {
	ctx := context.Background()
	store := NewMockStore()

	now := time.Now()
	oneHourAgo := now.Add(-1 * time.Hour)
	twoHoursAgo := now.Add(-2 * time.Hour)

	store.AddProject(&Project{ID: uuid.New(), Name: "p1", Status: StatusFailed, UpdatedAt: now})
	store.AddProject(&Project{ID: uuid.New(), Name: "p2", Status: StatusFailed, UpdatedAt: oneHourAgo})
	store.AddProject(&Project{ID: uuid.New(), Name: "p3", Status: StatusRunning, UpdatedAt: now})

	// Get failed in last 90 minutes
	since := now.Add(-90 * time.Minute)
	projects, err := store.GetRecentlyFailed(ctx, since, 10)

	require.NoError(t, err)
	require.Len(t, projects, 2)

	// Get failed in last 30 minutes
	since = now.Add(-30 * time.Minute)
	projects, err = store.GetRecentlyFailed(ctx, since, 10)

	require.NoError(t, err)
	require.Len(t, projects, 1)

	// Get with limit
	since = twoHoursAgo
	projects, err = store.GetRecentlyFailed(ctx, since, 1)

	require.NoError(t, err)
	require.Len(t, projects, 1)
}

// TestMockStore_ConcurrentAccess verifies thread safety.
func TestMockStore_ConcurrentAccess(t *testing.T) {
	ctx := context.Background()
	store := NewMockStore()
	projectID := uuid.New()
	store.AddProject(&Project{ID: projectID, Name: "test", Status: StatusRunning})

	done := make(chan bool)

	// Concurrent reads
	for i := 0; i < 10; i++ {
		go func() {
			_, _ = store.Get(ctx, projectID)
			_, _ = store.List(ctx, ListFilter{})
			_, _ = store.GetStatusCounts(ctx)
			done <- true
		}()
	}

	// Concurrent writes
	for i := 0; i < 10; i++ {
		go func() {
			_ = store.UpdateHealthCheck(ctx, projectID, "10.0.0.1")
			_ = store.UpdateLastActivity(ctx, "test")
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 20; i++ {
		<-done
	}
}

// Helper to create string pointers
func strPtr(s string) *string {
	return &s
}
