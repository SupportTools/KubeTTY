package projects

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// ---- Tests for CreateProjectRequest.ApplyDefaults ----

func TestApplyDefaults(t *testing.T) {
	req := CreateProjectRequest{
		Name:        "test",
		DisplayName: "Test",
		UserName:    "testuser",
	}
	req.ApplyDefaults()

	if req.CPURequest != DefaultCPURequest {
		t.Errorf("CPURequest = %q, want %q", req.CPURequest, DefaultCPURequest)
	}
	if req.CPULimit != DefaultCPULimit {
		t.Errorf("CPULimit = %q, want %q", req.CPULimit, DefaultCPULimit)
	}
	if req.MemoryRequest != DefaultMemoryRequest {
		t.Errorf("MemoryRequest = %q, want %q", req.MemoryRequest, DefaultMemoryRequest)
	}
	if req.MemoryLimit != DefaultMemoryLimit {
		t.Errorf("MemoryLimit = %q, want %q", req.MemoryLimit, DefaultMemoryLimit)
	}
	if req.StorageSize != DefaultStorageSize {
		t.Errorf("StorageSize = %q, want %q", req.StorageSize, DefaultStorageSize)
	}
	if req.StorageClass != DefaultStorageClass {
		t.Errorf("StorageClass = %q, want %q", req.StorageClass, DefaultStorageClass)
	}
	if req.MaxTabsPerClient != DefaultMaxTabsPerClient {
		t.Errorf("MaxTabsPerClient = %d, want %d", req.MaxTabsPerClient, DefaultMaxTabsPerClient)
	}
	if req.MaxTabsTotal != DefaultMaxTabsTotal {
		t.Errorf("MaxTabsTotal = %d, want %d", req.MaxTabsTotal, DefaultMaxTabsTotal)
	}
	if req.DinDEnabled == nil || !*req.DinDEnabled {
		t.Error("DinDEnabled should default to true")
	}
	if req.ImageRepository != DefaultImageRepository {
		t.Errorf("ImageRepository = %q, want %q", req.ImageRepository, DefaultImageRepository)
	}
	if req.ImageTag != DefaultImageTag {
		t.Errorf("ImageTag = %q, want %q", req.ImageTag, DefaultImageTag)
	}
	if req.AdminNamespaces == nil {
		t.Error("AdminNamespaces should not be nil")
	}
	if req.ReadNamespaces == nil {
		t.Error("ReadNamespaces should not be nil")
	}
	if req.EnvVars == nil {
		t.Error("EnvVars should not be nil")
	}
}

func TestApplyDefaults_DoesNotOverwrite(t *testing.T) {
	dind := false
	req := CreateProjectRequest{
		Name:             "test",
		DisplayName:      "Test",
		UserName:         "testuser",
		CPURequest:       "100m",
		CPULimit:         "200m",
		MemoryRequest:    "256Mi",
		MemoryLimit:      "512Mi",
		StorageSize:      "10Gi",
		StorageClass:     "custom-class",
		MaxTabsPerClient: 5,
		MaxTabsTotal:     20,
		DinDEnabled:      &dind,
		ImageRepository:  "custom/repo",
		ImageTag:         "custom-tag",
		AdminNamespaces:  []string{"ns1"},
		ReadNamespaces:   []string{"ns2"},
		EnvVars:          map[string]string{"KEY": "value"},
	}
	req.ApplyDefaults()

	if req.CPURequest != "100m" {
		t.Errorf("CPURequest was overwritten: %q", req.CPURequest)
	}
	if req.CPULimit != "200m" {
		t.Errorf("CPULimit was overwritten: %q", req.CPULimit)
	}
	if req.MemoryRequest != "256Mi" {
		t.Errorf("MemoryRequest was overwritten: %q", req.MemoryRequest)
	}
	if req.MemoryLimit != "512Mi" {
		t.Errorf("MemoryLimit was overwritten: %q", req.MemoryLimit)
	}
	if req.StorageSize != "10Gi" {
		t.Errorf("StorageSize was overwritten: %q", req.StorageSize)
	}
	if req.StorageClass != "custom-class" {
		t.Errorf("StorageClass was overwritten: %q", req.StorageClass)
	}
	if req.MaxTabsPerClient != 5 {
		t.Errorf("MaxTabsPerClient was overwritten: %d", req.MaxTabsPerClient)
	}
	if req.MaxTabsTotal != 20 {
		t.Errorf("MaxTabsTotal was overwritten: %d", req.MaxTabsTotal)
	}
	if *req.DinDEnabled != false {
		t.Error("DinDEnabled was overwritten")
	}
	if req.ImageRepository != "custom/repo" {
		t.Errorf("ImageRepository was overwritten: %q", req.ImageRepository)
	}
	if req.ImageTag != "custom-tag" {
		t.Errorf("ImageTag was overwritten: %q", req.ImageTag)
	}
	if len(req.AdminNamespaces) != 1 || req.AdminNamespaces[0] != "ns1" {
		t.Errorf("AdminNamespaces was overwritten: %v", req.AdminNamespaces)
	}
	if len(req.ReadNamespaces) != 1 || req.ReadNamespaces[0] != "ns2" {
		t.Errorf("ReadNamespaces was overwritten: %v", req.ReadNamespaces)
	}
	if len(req.EnvVars) != 1 || req.EnvVars["KEY"] != "value" {
		t.Errorf("EnvVars was overwritten: %v", req.EnvVars)
	}
}

// ---- Tests for name pattern validation ----

func TestNamePattern(t *testing.T) {
	validNames := []string{
		"a",
		"abc",
		"a-b",
		"abc-def",
		"a1",
		"abc123",
		"a-1-b-2",
		"project1",
		"my-cool-project",
		"1abc", // Starts with number but still valid
	}

	for _, name := range validNames {
		if !namePattern.MatchString(name) {
			t.Errorf("expected %q to be valid", name)
		}
	}

	// Names that must fail validation
	mustFail := []string{
		"",
		"-abc",
		"abc-",
		"ABC",
		"a_b",
		"a.b",
		"a b",
		"test!",
	}

	for _, name := range mustFail {
		if namePattern.MatchString(name) {
			t.Errorf("expected %q to be invalid", name)
		}
	}
}

// ---- Tests for ProjectStatus constants ----

func TestProjectStatusConstants(t *testing.T) {
	statuses := []struct {
		status   ProjectStatus
		expected string
	}{
		{StatusPending, "pending"},
		{StatusCreating, "creating"},
		{StatusRunning, "running"},
		{StatusUpdating, "updating"},
		{StatusFailed, "failed"},
		{StatusDeleting, "deleting"},
		{StatusDeleted, "deleted"},
	}

	for _, s := range statuses {
		if string(s.status) != s.expected {
			t.Errorf("status %v = %q, want %q", s.status, string(s.status), s.expected)
		}
	}
}

// ---- Tests for default constants ----

func TestDefaultConstants(t *testing.T) {
	tests := []struct {
		name     string
		got      string
		expected string
	}{
		{"DefaultCPURequest", DefaultCPURequest, "500m"},
		{"DefaultCPULimit", DefaultCPULimit, "4000m"},
		{"DefaultMemoryRequest", DefaultMemoryRequest, "2Gi"},
		{"DefaultMemoryLimit", DefaultMemoryLimit, "8Gi"},
		{"DefaultStorageSize", DefaultStorageSize, "50Gi"},
		{"DefaultStorageClass", DefaultStorageClass, "longhorn"},
		{"DefaultImageRepository", DefaultImageRepository, "harbor.support.tools/kubetty/kubetty"},
		{"DefaultImageTag", DefaultImageTag, "latest"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("%s = %q, want %q", tt.name, tt.got, tt.expected)
			}
		})
	}

	// Test int defaults
	if DefaultMaxTabsPerClient != 3 {
		t.Errorf("DefaultMaxTabsPerClient = %d, want 3", DefaultMaxTabsPerClient)
	}
	if DefaultMaxTabsTotal != 10 {
		t.Errorf("DefaultMaxTabsTotal = %d, want 10", DefaultMaxTabsTotal)
	}
}

// ---- Tests for helper functions ----

func TestNullIfEmpty(t *testing.T) {
	tests := []struct {
		input    string
		expected interface{}
	}{
		{"", nil},
		{"value", "value"},
		{" ", " "}, // Space is not empty
	}

	for _, tt := range tests {
		result := nullIfEmpty(tt.input)
		if result != tt.expected {
			t.Errorf("nullIfEmpty(%q) = %v, want %v", tt.input, result, tt.expected)
		}
	}
}

func TestJoinStrings(t *testing.T) {
	tests := []struct {
		name     string
		strs     []string
		sep      string
		expected string
	}{
		{"empty slice", []string{}, ", ", ""},
		{"single element", []string{"a"}, ", ", "a"},
		{"two elements", []string{"a", "b"}, ", ", "a, b"},
		{"three elements", []string{"a", "b", "c"}, ", ", "a, b, c"},
		{"custom separator", []string{"a", "b", "c"}, " AND ", "a AND b AND c"},
		{"no separator", []string{"a", "b"}, "", "ab"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := joinStrings(tt.strs, tt.sep)
			if result != tt.expected {
				t.Errorf("joinStrings(%v, %q) = %q, want %q", tt.strs, tt.sep, result, tt.expected)
			}
		})
	}
}

// ---- Tests for error sentinels ----

func TestErrorSentinels(t *testing.T) {
	errors := []struct {
		err      error
		contains string
	}{
		{ErrProjectNotFound, "not found"},
		{ErrDuplicateName, "already exists"},
		{ErrDuplicateNamespace, "already exists"},
		{ErrDuplicateServiceName, "already exists"},
		{ErrInvalidName, "invalid"},
	}

	for _, e := range errors {
		if e.err == nil {
			t.Errorf("error sentinel should not be nil")
		}
		if e.err.Error() == "" {
			t.Errorf("error sentinel should have a message")
		}
	}
}

// ---- Tests for ListFilter ----

func TestListFilter_Defaults(t *testing.T) {
	// Default ListFilter should have zero values
	var f ListFilter

	if f.Status != "" {
		t.Errorf("default Status should be empty, got %q", f.Status)
	}
	if f.UserName != "" {
		t.Errorf("default UserName should be empty, got %q", f.UserName)
	}
	if f.IncludeAll != false {
		t.Error("default IncludeAll should be false")
	}
	if f.Limit != 0 {
		t.Errorf("default Limit should be 0, got %d", f.Limit)
	}
	if f.Offset != 0 {
		t.Errorf("default Offset should be 0, got %d", f.Offset)
	}
}

// ---- Tests for UpdateProjectRequest ----

func TestUpdateProjectRequest_OptionalFields(t *testing.T) {
	// All fields should be optional (pointers or maps)
	var req UpdateProjectRequest

	// Verify all fields are nil by default
	if req.DisplayName != nil {
		t.Error("DisplayName should default to nil")
	}
	if req.Description != nil {
		t.Error("Description should default to nil")
	}
	if req.Icon != nil {
		t.Error("Icon should default to nil")
	}
	if req.CPURequest != nil {
		t.Error("CPURequest should default to nil")
	}
	if req.CPULimit != nil {
		t.Error("CPULimit should default to nil")
	}
	if req.MemoryRequest != nil {
		t.Error("MemoryRequest should default to nil")
	}
	if req.MemoryLimit != nil {
		t.Error("MemoryLimit should default to nil")
	}
	if req.StorageSize != nil {
		t.Error("StorageSize should default to nil")
	}
	if req.MaxTabsPerClient != nil {
		t.Error("MaxTabsPerClient should default to nil")
	}
	if req.MaxTabsTotal != nil {
		t.Error("MaxTabsTotal should default to nil")
	}
	if req.DinDEnabled != nil {
		t.Error("DinDEnabled should default to nil")
	}
	if req.EnvVars != nil {
		t.Error("EnvVars should default to nil")
	}
	if req.ImageTag != nil {
		t.Error("ImageTag should default to nil")
	}
}

// TestComputeServiceName verifies service name generation.
func TestComputeServiceName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Short name",
			input:    "myproject",
			expected: "kubetty-project-myproject",
		},
		{
			name:     "Single character",
			input:    "a",
			expected: "kubetty-project-a",
		},
		{
			name:     "Name with dashes",
			input:    "my-cool-project",
			expected: "kubetty-project-my-cool-project",
		},
		{
			name:     "Maximum length before truncation (47 chars)",
			input:    "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", // 47 chars
			expected: "kubetty-project-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
		{
			name:     "Exceeds 63 char limit - should truncate",
			input:    "this-is-a-very-long-project-name-that-exceeds-the-kubernetes-limit",
			expected: "kubetty-project-this-is-a-very-long-project-name-that-exceeds-t",
		},
		{
			name:     "Exactly at max length (name = 47 chars)",
			input:    "12345678901234567890123456789012345678901234567",
			expected: "kubetty-project-12345678901234567890123456789012345678901234567",
		},
		{
			name:     "One over max length (name = 48 chars)",
			input:    "123456789012345678901234567890123456789012345678",
			expected: "kubetty-project-12345678901234567890123456789012345678901234567",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ComputeServiceName(tt.input)
			if result != tt.expected {
				t.Errorf("ComputeServiceName(%q) = %q, want %q", tt.input, result, tt.expected)
			}
			if len(result) > MaxServiceNameLength {
				t.Errorf("ComputeServiceName(%q) length = %d, exceeds max %d", tt.input, len(result), MaxServiceNameLength)
			}
		})
	}
}

// TestComputeServiceNameLength verifies that result never exceeds 63 chars.
func TestComputeServiceNameLength(t *testing.T) {
	// Test with various lengths
	for i := 1; i <= 100; i++ {
		name := make([]byte, i)
		for j := range name {
			name[j] = 'a'
		}
		result := ComputeServiceName(string(name))
		if len(result) > MaxServiceNameLength {
			t.Errorf("ComputeServiceName with %d char input produced %d char output, exceeds %d",
				i, len(result), MaxServiceNameLength)
		}
	}
}

// TestServiceNamePrefix verifies the prefix constant.
func TestServiceNamePrefix(t *testing.T) {
	if ServiceNamePrefix != "kubetty-project-" {
		t.Errorf("ServiceNamePrefix = %q, want %q", ServiceNamePrefix, "kubetty-project-")
	}
	if len(ServiceNamePrefix) != 16 {
		t.Errorf("ServiceNamePrefix length = %d, want 16", len(ServiceNamePrefix))
	}
}

// TestMaxServiceNameLength verifies the max length constant.
func TestMaxServiceNameLength(t *testing.T) {
	if MaxServiceNameLength != 63 {
		t.Errorf("MaxServiceNameLength = %d, want 63", MaxServiceNameLength)
	}
}

// ---- Integration tests (require database) ----

func setupTestStore(t *testing.T) (*PGStore, func()) {
	t.Helper()

	// Build connection string from environment variables
	host := os.Getenv("CNPG_HOST")
	if host == "" {
		host = "localhost"
	}
	port := os.Getenv("CNPG_PORT")
	if port == "" {
		port = "5432"
	}
	user := os.Getenv("CNPG_USER")
	if user == "" {
		user = "kubetty_test"
	}
	password := os.Getenv("CNPG_PASSWORD")
	if password == "" {
		password = "kubetty_test"
	}
	database := os.Getenv("CNPG_DATABASE")
	if database == "" {
		database = "kubetty_test"
	}

	connStr := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", user, password, host, port, database)
	pool, err := pgxpool.New(context.Background(), connStr)
	if err != nil {
		t.Skipf("Skipping database test: database not available: %v", err)
	}

	// Clean up any existing test data
	_, err = pool.Exec(context.Background(), "DELETE FROM kubetty_projects")
	if err != nil {
		pool.Close()
		t.Skipf("Skipping database test: failed to clean test data: %v", err)
	}

	store := NewStoreFromPool(pool, "kubetty-test-ns")

	cleanup := func() {
		pool.Exec(context.Background(), "DELETE FROM kubetty_projects")
		pool.Close()
	}

	return store, cleanup
}

func TestIntegration_Create(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	req := CreateProjectRequest{
		Name:        "test-project",
		DisplayName: "Test Project",
		UserName:    "testuser",
	}

	project, err := store.Create(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, project)
	require.Equal(t, "test-project", project.Name)
	require.Equal(t, "Test Project", project.DisplayName)
	require.Equal(t, "testuser", project.UserName)
	require.Equal(t, StatusPending, project.Status)
	require.Equal(t, "kubetty-test-ns", project.TargetNamespace)
	require.Equal(t, "kubetty-project-test-project", project.ServiceName)
	require.NotEqual(t, uuid.Nil, project.ID)
	require.NotEqual(t, uuid.Nil, project.SessionID)

	// Verify defaults were applied
	require.Equal(t, DefaultCPURequest, project.CPURequest)
	require.Equal(t, DefaultCPULimit, project.CPULimit)
	require.Equal(t, DefaultMemoryRequest, project.MemoryRequest)
	require.Equal(t, DefaultMemoryLimit, project.MemoryLimit)
	require.Equal(t, DefaultStorageSize, project.StorageSize)
	require.True(t, project.DinDEnabled)
}

func TestIntegration_Create_InvalidName(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	req := CreateProjectRequest{
		Name:        "Invalid_Name", // underscores not allowed
		DisplayName: "Test",
		UserName:    "testuser",
	}

	project, err := store.Create(ctx, req)
	require.ErrorIs(t, err, ErrInvalidName)
	require.Nil(t, project)
}

func TestIntegration_Create_DuplicateName(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	req := CreateProjectRequest{
		Name:        "duplicate-project",
		DisplayName: "Test",
		UserName:    "testuser",
	}

	_, err := store.Create(ctx, req)
	require.NoError(t, err)

	// Try to create another with same name
	_, err = store.Create(ctx, req)
	require.Error(t, err) // Should fail due to unique constraint
}

func TestIntegration_Get(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create a project
	req := CreateProjectRequest{
		Name:        "get-test",
		DisplayName: "Get Test",
		UserName:    "testuser",
		Description: "A test project",
	}
	created, err := store.Create(ctx, req)
	require.NoError(t, err)

	// Get it by ID
	fetched, err := store.Get(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, created.ID, fetched.ID)
	require.Equal(t, created.Name, fetched.Name)
	require.Equal(t, "A test project", fetched.Description)
}

func TestIntegration_Get_NotFound(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Try to get non-existent project
	_, err := store.Get(ctx, uuid.New())
	require.ErrorIs(t, err, ErrProjectNotFound)
}

func TestIntegration_GetByName(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create a project
	req := CreateProjectRequest{
		Name:        "byname-test",
		DisplayName: "By Name Test",
		UserName:    "testuser",
	}
	created, err := store.Create(ctx, req)
	require.NoError(t, err)

	// Get it by name
	fetched, err := store.GetByName(ctx, "byname-test")
	require.NoError(t, err)
	require.Equal(t, created.ID, fetched.ID)
	require.Equal(t, "byname-test", fetched.Name)
}

func TestIntegration_GetByName_NotFound(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	_, err := store.GetByName(ctx, "nonexistent")
	require.ErrorIs(t, err, ErrProjectNotFound)
}

func TestIntegration_GetByServiceName(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	req := CreateProjectRequest{
		Name:        "svcname-test",
		DisplayName: "Service Name Test",
		UserName:    "testuser",
	}
	created, err := store.Create(ctx, req)
	require.NoError(t, err)

	// Get by computed service name
	fetched, err := store.GetByServiceName(ctx, "kubetty-project-svcname-test")
	require.NoError(t, err)
	require.Equal(t, created.ID, fetched.ID)
}

func TestIntegration_List_Empty(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	projects, err := store.List(ctx, ListFilter{})
	require.NoError(t, err)
	require.Empty(t, projects)
}

func TestIntegration_List_MultipleProjects(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create multiple projects
	for i := 1; i <= 5; i++ {
		req := CreateProjectRequest{
			Name:        fmt.Sprintf("list-project-%d", i),
			DisplayName: fmt.Sprintf("List Project %d", i),
			UserName:    "testuser",
		}
		_, err := store.Create(ctx, req)
		require.NoError(t, err)
	}

	projects, err := store.List(ctx, ListFilter{})
	require.NoError(t, err)
	require.Len(t, projects, 5)
}

func TestIntegration_List_WithStatusFilter(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create projects with different statuses
	req := CreateProjectRequest{
		Name:        "status-filter-test",
		DisplayName: "Status Filter Test",
		UserName:    "testuser",
	}
	project, err := store.Create(ctx, req)
	require.NoError(t, err)

	// Change one to running
	err = store.SetStatus(ctx, project.ID, StatusRunning, "")
	require.NoError(t, err)

	// Create another (will be pending by default)
	req2 := CreateProjectRequest{
		Name:        "status-filter-test-2",
		DisplayName: "Status Filter Test 2",
		UserName:    "testuser",
	}
	_, err = store.Create(ctx, req2)
	require.NoError(t, err)

	// List only running
	projects, err := store.List(ctx, ListFilter{Status: "running"})
	require.NoError(t, err)
	require.Len(t, projects, 1)
	require.Equal(t, "status-filter-test", projects[0].Name)

	// List only pending
	projects, err = store.List(ctx, ListFilter{Status: "pending"})
	require.NoError(t, err)
	require.Len(t, projects, 1)
	require.Equal(t, "status-filter-test-2", projects[0].Name)
}

func TestIntegration_List_WithUserNameFilter(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create projects for different users
	req1 := CreateProjectRequest{
		Name:        "user1-project",
		DisplayName: "User1 Project",
		UserName:    "user1",
	}
	_, err := store.Create(ctx, req1)
	require.NoError(t, err)

	req2 := CreateProjectRequest{
		Name:        "user2-project",
		DisplayName: "User2 Project",
		UserName:    "user2",
	}
	_, err = store.Create(ctx, req2)
	require.NoError(t, err)

	// List only user1's projects
	projects, err := store.List(ctx, ListFilter{UserName: "user1"})
	require.NoError(t, err)
	require.Len(t, projects, 1)
	require.Equal(t, "user1", projects[0].UserName)
}

func TestIntegration_List_WithPagination(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create 10 projects
	for i := 1; i <= 10; i++ {
		req := CreateProjectRequest{
			Name:        fmt.Sprintf("page-project-%02d", i),
			DisplayName: fmt.Sprintf("Page Project %d", i),
			UserName:    "testuser",
		}
		_, err := store.Create(ctx, req)
		require.NoError(t, err)
	}

	// Get first 5
	projects, err := store.List(ctx, ListFilter{Limit: 5})
	require.NoError(t, err)
	require.Len(t, projects, 5)

	// Get next 5
	projects, err = store.List(ctx, ListFilter{Limit: 5, Offset: 5})
	require.NoError(t, err)
	require.Len(t, projects, 5)
}

func TestIntegration_ListByStatuses(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create projects with different statuses
	req1 := CreateProjectRequest{
		Name:        "status-pending",
		DisplayName: "Pending",
		UserName:    "testuser",
	}
	_, err := store.Create(ctx, req1)
	require.NoError(t, err)

	req2 := CreateProjectRequest{
		Name:        "status-running",
		DisplayName: "Running",
		UserName:    "testuser",
	}
	p2, err := store.Create(ctx, req2)
	require.NoError(t, err)
	err = store.SetStatus(ctx, p2.ID, StatusRunning, "")
	require.NoError(t, err)

	req3 := CreateProjectRequest{
		Name:        "status-failed",
		DisplayName: "Failed",
		UserName:    "testuser",
	}
	p3, err := store.Create(ctx, req3)
	require.NoError(t, err)
	err = store.SetStatus(ctx, p3.ID, StatusFailed, "error message")
	require.NoError(t, err)

	// List pending and running
	projects, err := store.ListByStatuses(ctx, []ProjectStatus{StatusPending, StatusRunning})
	require.NoError(t, err)
	require.Len(t, projects, 2)

	// List only failed
	projects, err = store.ListByStatuses(ctx, []ProjectStatus{StatusFailed})
	require.NoError(t, err)
	require.Len(t, projects, 1)
	require.Equal(t, "status-failed", projects[0].Name)

	// Empty statuses returns empty list
	projects, err = store.ListByStatuses(ctx, []ProjectStatus{})
	require.NoError(t, err)
	require.Empty(t, projects)
}

func TestIntegration_Update(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	req := CreateProjectRequest{
		Name:        "update-test",
		DisplayName: "Original Name",
		UserName:    "testuser",
	}
	project, err := store.Create(ctx, req)
	require.NoError(t, err)

	// Update display name and description
	newName := "Updated Name"
	newDesc := "New description"
	newCPU := "1000m"
	updated, err := store.Update(ctx, project.ID, UpdateProjectRequest{
		DisplayName: &newName,
		Description: &newDesc,
		CPURequest:  &newCPU,
	})
	require.NoError(t, err)
	require.Equal(t, "Updated Name", updated.DisplayName)
	require.Equal(t, "New description", updated.Description)
	require.Equal(t, "1000m", updated.CPURequest)
	// Original unchanged fields
	require.Equal(t, "update-test", updated.Name)
}

func TestIntegration_Update_EmptyRequest(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	req := CreateProjectRequest{
		Name:        "noop-update",
		DisplayName: "No Change",
		UserName:    "testuser",
	}
	project, err := store.Create(ctx, req)
	require.NoError(t, err)

	// Update with empty request - should just return current state
	updated, err := store.Update(ctx, project.ID, UpdateProjectRequest{})
	require.NoError(t, err)
	require.Equal(t, project.ID, updated.ID)
	require.Equal(t, "No Change", updated.DisplayName)
}

func TestIntegration_Update_NotFound(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	newName := "test"
	_, err := store.Update(ctx, uuid.New(), UpdateProjectRequest{
		DisplayName: &newName,
	})
	require.ErrorIs(t, err, ErrProjectNotFound)
}

func TestIntegration_Delete(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	req := CreateProjectRequest{
		Name:        "delete-test",
		DisplayName: "Delete Test",
		UserName:    "testuser",
	}
	project, err := store.Create(ctx, req)
	require.NoError(t, err)

	// Soft delete
	err = store.Delete(ctx, project.ID)
	require.NoError(t, err)

	// Should not be found by Get (filters deleted_at IS NULL)
	_, err = store.Get(ctx, project.ID)
	require.ErrorIs(t, err, ErrProjectNotFound)

	// Should show up in ListByStatuses with deleting status
	projects, err := store.ListByStatuses(ctx, []ProjectStatus{StatusDeleting})
	require.NoError(t, err)
	require.Len(t, projects, 1)
	require.Equal(t, "delete-test", projects[0].Name)
}

func TestIntegration_Delete_NotFound(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	err := store.Delete(ctx, uuid.New())
	require.ErrorIs(t, err, ErrProjectNotFound)
}

func TestIntegration_HardDelete(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	req := CreateProjectRequest{
		Name:        "hard-delete-test",
		DisplayName: "Hard Delete Test",
		UserName:    "testuser",
	}
	project, err := store.Create(ctx, req)
	require.NoError(t, err)

	// Hard delete
	err = store.HardDelete(ctx, project.ID)
	require.NoError(t, err)

	// Should not be found anywhere
	_, err = store.Get(ctx, project.ID)
	require.ErrorIs(t, err, ErrProjectNotFound)

	// Even IncludeAll won't show it
	projects, err := store.List(ctx, ListFilter{IncludeAll: true})
	require.NoError(t, err)
	require.Empty(t, projects)
}

func TestIntegration_HardDelete_NotFound(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	err := store.HardDelete(ctx, uuid.New())
	require.ErrorIs(t, err, ErrProjectNotFound)
}

func TestIntegration_SetStatus(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	req := CreateProjectRequest{
		Name:        "setstatus-test",
		DisplayName: "Set Status Test",
		UserName:    "testuser",
	}
	project, err := store.Create(ctx, req)
	require.NoError(t, err)
	require.Equal(t, StatusPending, project.Status)

	// Update to running
	err = store.SetStatus(ctx, project.ID, StatusRunning, "")
	require.NoError(t, err)

	fetched, err := store.Get(ctx, project.ID)
	require.NoError(t, err)
	require.Equal(t, StatusRunning, fetched.Status)

	// Update to failed with message
	err = store.SetStatus(ctx, project.ID, StatusFailed, "something went wrong")
	require.NoError(t, err)

	fetched, err = store.Get(ctx, project.ID)
	require.NoError(t, err)
	require.Equal(t, StatusFailed, fetched.Status)
	require.Equal(t, "something went wrong", fetched.StatusMessage)
}

func TestIntegration_SetStatus_NotFound(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	err := store.SetStatus(ctx, uuid.New(), StatusRunning, "")
	require.ErrorIs(t, err, ErrProjectNotFound)
}

func TestIntegration_UpdateHealthCheck(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	req := CreateProjectRequest{
		Name:        "healthcheck-test",
		DisplayName: "Health Check Test",
		UserName:    "testuser",
	}
	project, err := store.Create(ctx, req)
	require.NoError(t, err)
	require.Nil(t, project.LastHealthCheck)

	// Update health check
	err = store.UpdateHealthCheck(ctx, project.ID, "10.0.0.1")
	require.NoError(t, err)

	fetched, err := store.Get(ctx, project.ID)
	require.NoError(t, err)
	require.NotNil(t, fetched.LastHealthCheck)
	require.Equal(t, "10.0.0.1", fetched.PodIP)
}

func TestIntegration_UpdateHealthCheck_NotFound(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	err := store.UpdateHealthCheck(ctx, uuid.New(), "10.0.0.1")
	require.ErrorIs(t, err, ErrProjectNotFound)
}

func TestIntegration_UpdateLastActivity(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	req := CreateProjectRequest{
		Name:        "activity-test",
		DisplayName: "Activity Test",
		UserName:    "testuser",
	}
	project, err := store.Create(ctx, req)
	require.NoError(t, err)

	initialActivity := project.LastActivity

	// Update activity by name
	err = store.UpdateLastActivity(ctx, "activity-test")
	require.NoError(t, err)

	fetched, err := store.Get(ctx, project.ID)
	require.NoError(t, err)
	require.NotNil(t, fetched.LastActivity)
	// If initialActivity was nil, this just confirms it's now set
	// If initialActivity was set, verify it was updated
	if initialActivity != nil {
		require.True(t, fetched.LastActivity.After(*initialActivity) || fetched.LastActivity.Equal(*initialActivity))
	}
}

func TestIntegration_UpdateLastActivity_NotFound(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	err := store.UpdateLastActivity(ctx, "nonexistent-project")
	require.ErrorIs(t, err, ErrProjectNotFound)
}

func TestIntegration_FullLifecycle(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// 1. Create project
	req := CreateProjectRequest{
		Name:        "lifecycle-test",
		DisplayName: "Lifecycle Test",
		UserName:    "testuser",
		Description: "Testing full lifecycle",
	}
	project, err := store.Create(ctx, req)
	require.NoError(t, err)
	require.Equal(t, StatusPending, project.Status)

	// 2. Set status to creating
	err = store.SetStatus(ctx, project.ID, StatusCreating, "creating resources")
	require.NoError(t, err)

	// 3. Set status to running
	err = store.SetStatus(ctx, project.ID, StatusRunning, "")
	require.NoError(t, err)

	// 4. Update health check
	err = store.UpdateHealthCheck(ctx, project.ID, "10.0.0.5")
	require.NoError(t, err)

	// 5. Update activity
	err = store.UpdateLastActivity(ctx, "lifecycle-test")
	require.NoError(t, err)

	// 6. Update settings
	newDesc := "Updated description"
	_, err = store.Update(ctx, project.ID, UpdateProjectRequest{
		Description: &newDesc,
	})
	require.NoError(t, err)

	// 7. Verify final state
	final, err := store.Get(ctx, project.ID)
	require.NoError(t, err)
	require.Equal(t, StatusRunning, final.Status)
	require.Equal(t, "10.0.0.5", final.PodIP)
	require.NotNil(t, final.LastHealthCheck)
	require.NotNil(t, final.LastActivity)
	require.Equal(t, "Updated description", final.Description)

	// 8. Soft delete
	err = store.Delete(ctx, project.ID)
	require.NoError(t, err)

	// 9. Verify in deleting state
	projects, err := store.ListByStatuses(ctx, []ProjectStatus{StatusDeleting})
	require.NoError(t, err)
	require.Len(t, projects, 1)

	// 10. Hard delete
	err = store.HardDelete(ctx, project.ID)
	require.NoError(t, err)

	// 11. Verify completely removed
	projects, err = store.List(ctx, ListFilter{IncludeAll: true})
	require.NoError(t, err)
	require.Empty(t, projects)
}
