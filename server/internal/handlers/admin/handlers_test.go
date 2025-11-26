package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/supporttools/KubeTTY/server/internal/controller"
	"github.com/supporttools/KubeTTY/server/internal/projects"
)

// mockProjectStore implements projects.Store for testing.
type mockProjectStore struct {
	projects      map[uuid.UUID]*projects.Project
	listResult    []projects.Project
	createResult  *projects.Project
	createErr     error
	getErr        error
	updateErr     error
	deleteErr     error
	listErr       error
	setStatusErr  error
	calledStatus  projects.ProjectStatus
	calledMessage string
}

func newMockProjectStore() *mockProjectStore {
	return &mockProjectStore{
		projects: make(map[uuid.UUID]*projects.Project),
	}
}

func (m *mockProjectStore) Create(ctx context.Context, req projects.CreateProjectRequest) (*projects.Project, error) {
	if m.createErr != nil {
		return nil, m.createErr
	}
	if m.createResult != nil {
		return m.createResult, nil
	}
	p := &projects.Project{
		ID:          uuid.New(),
		Name:        req.Name,
		DisplayName: req.DisplayName,
		UserName:    req.UserName,
		Status:      projects.StatusPending,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	m.projects[p.ID] = p
	return p, nil
}

func (m *mockProjectStore) Get(ctx context.Context, id uuid.UUID) (*projects.Project, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	if p, ok := m.projects[id]; ok {
		return p, nil
	}
	return nil, projects.ErrProjectNotFound
}

func (m *mockProjectStore) GetByName(ctx context.Context, name string) (*projects.Project, error) {
	for _, p := range m.projects {
		if p.Name == name {
			return p, nil
		}
	}
	return nil, projects.ErrProjectNotFound
}

func (m *mockProjectStore) GetByServiceName(ctx context.Context, serviceName string) (*projects.Project, error) {
	for _, p := range m.projects {
		if p.ServiceName == serviceName {
			return p, nil
		}
	}
	return nil, projects.ErrProjectNotFound
}

func (m *mockProjectStore) List(ctx context.Context, filter projects.ListFilter) ([]projects.Project, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	if m.listResult != nil {
		return m.listResult, nil
	}
	result := []projects.Project{}
	for _, p := range m.projects {
		result = append(result, *p)
	}
	return result, nil
}

func (m *mockProjectStore) Update(ctx context.Context, id uuid.UUID, req projects.UpdateProjectRequest) (*projects.Project, error) {
	if m.updateErr != nil {
		return nil, m.updateErr
	}
	p, ok := m.projects[id]
	if !ok {
		return nil, projects.ErrProjectNotFound
	}
	if req.DisplayName != nil {
		p.DisplayName = *req.DisplayName
	}
	return p, nil
}

func (m *mockProjectStore) Delete(ctx context.Context, id uuid.UUID) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	if _, ok := m.projects[id]; !ok {
		return projects.ErrProjectNotFound
	}
	delete(m.projects, id)
	return nil
}

func (m *mockProjectStore) HardDelete(ctx context.Context, id uuid.UUID) error {
	return m.Delete(ctx, id)
}

func (m *mockProjectStore) SetStatus(ctx context.Context, id uuid.UUID, status projects.ProjectStatus, message string) error {
	m.calledStatus = status
	m.calledMessage = message
	if m.setStatusErr != nil {
		return m.setStatusErr
	}
	if p, ok := m.projects[id]; ok {
		p.Status = status
		p.StatusMessage = message
		return nil
	}
	return projects.ErrProjectNotFound
}

func (m *mockProjectStore) UpdateHealthCheck(ctx context.Context, id uuid.UUID, podIP string) error {
	return nil
}

func (m *mockProjectStore) UpdateLastActivity(ctx context.Context, projectName string) error {
	return nil
}

func (m *mockProjectStore) ListByStatuses(ctx context.Context, statuses []projects.ProjectStatus) ([]projects.Project, error) {
	return m.List(ctx, projects.ListFilter{})
}

func (m *mockProjectStore) GetStatusCounts(ctx context.Context) (map[projects.ProjectStatus]int, error) {
	counts := make(map[projects.ProjectStatus]int)
	for _, p := range m.projects {
		counts[p.Status]++
	}
	return counts, nil
}

func (m *mockProjectStore) GetRecentlyFailed(ctx context.Context, since time.Time, limit int) ([]projects.Project, error) {
	var result []projects.Project
	for _, p := range m.projects {
		if p.Status == projects.StatusFailed && !p.UpdatedAt.Before(since) {
			result = append(result, *p)
			if len(result) >= limit {
				break
			}
		}
	}
	return result, nil
}

// mockController provides a minimal mock of controller.Controller methods used by admin handlers.
type mockController struct {
	restartErr       error
	deploymentStatus *controller.DeploymentStatus
	deploymentErr    error
	secrets          map[string]string
	getSecretsErr    error
	updateSecretsErr error
}

func newMockController() *mockController {
	return &mockController{
		secrets: make(map[string]string),
	}
}

func (m *mockController) RestartProject(ctx context.Context, p *projects.Project) error {
	return m.restartErr
}

func (m *mockController) GetDeploymentStatus(ctx context.Context, p *projects.Project) (*controller.DeploymentStatus, error) {
	if m.deploymentErr != nil {
		return nil, m.deploymentErr
	}
	if m.deploymentStatus != nil {
		return m.deploymentStatus, nil
	}
	return &controller.DeploymentStatus{
		Exists:        true,
		Replicas:      1,
		ReadyReplicas: 1,
	}, nil
}

func (m *mockController) GetProjectSecrets(ctx context.Context, p *projects.Project) (map[string]string, error) {
	if m.getSecretsErr != nil {
		return nil, m.getSecretsErr
	}
	return m.secrets, nil
}

func (m *mockController) UpdateProjectSecrets(ctx context.Context, p *projects.Project, secrets map[string]string) error {
	if m.updateSecretsErr != nil {
		return m.updateSecretsErr
	}
	m.secrets = secrets
	return nil
}

// controllerInterface defines the interface for testing
type controllerInterface interface {
	RestartProject(ctx context.Context, p *projects.Project) error
	GetDeploymentStatus(ctx context.Context, p *projects.Project) (*controller.DeploymentStatus, error)
	GetProjectSecrets(ctx context.Context, p *projects.Project) (map[string]string, error)
	UpdateProjectSecrets(ctx context.Context, p *projects.Project, secrets map[string]string) error
}

// testableProjectHandlers wraps ProjectHandlers to use mockController
type testableProjectHandlers struct {
	*ProjectHandlers
	mockCtrl *mockController
}

func newTestableHandlers(store projects.Store, mockCtrl *mockController, recommendedTag string) *testableProjectHandlers {
	h := &testableProjectHandlers{
		ProjectHandlers: &ProjectHandlers{
			store:               store,
			recommendedImageTag: recommendedTag,
		},
		mockCtrl: mockCtrl,
	}
	return h
}

// ---- Tests for ListProjects ----

func TestListProjects_Success(t *testing.T) {
	store := newMockProjectStore()
	id := uuid.New()
	store.projects[id] = &projects.Project{
		ID:          id,
		Name:        "test-project",
		DisplayName: "Test Project",
		Status:      projects.StatusRunning,
	}

	handlers := NewProjectHandlers(store, nil, "v1.0.0")

	req := httptest.NewRequest(http.MethodGet, "/api/admin/projects", nil)
	rec := httptest.NewRecorder()

	handlers.ListProjects(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["total"].(float64) != 1 {
		t.Errorf("expected total 1, got %v", resp["total"])
	}
}

func TestListProjects_EmptyList(t *testing.T) {
	store := newMockProjectStore()
	handlers := NewProjectHandlers(store, nil, "v1.0.0")

	req := httptest.NewRequest(http.MethodGet, "/api/admin/projects", nil)
	rec := httptest.NewRecorder()

	handlers.ListProjects(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	projects, ok := resp["projects"].([]interface{})
	if !ok || len(projects) != 0 {
		t.Errorf("expected empty projects array, got %v", resp["projects"])
	}
}

func TestListProjects_StoreError(t *testing.T) {
	store := newMockProjectStore()
	store.listErr = errors.New("database error")
	handlers := NewProjectHandlers(store, nil, "v1.0.0")

	req := httptest.NewRequest(http.MethodGet, "/api/admin/projects", nil)
	rec := httptest.NewRecorder()

	handlers.ListProjects(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", rec.Code)
	}
}

func TestListProjects_StatusFilter(t *testing.T) {
	store := newMockProjectStore()
	handlers := NewProjectHandlers(store, nil, "v1.0.0")

	req := httptest.NewRequest(http.MethodGet, "/api/admin/projects?status=running", nil)
	rec := httptest.NewRecorder()

	handlers.ListProjects(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestListProjects_UserFilter(t *testing.T) {
	store := newMockProjectStore()
	handlers := NewProjectHandlers(store, nil, "v1.0.0")

	req := httptest.NewRequest(http.MethodGet, "/api/admin/projects?user=testuser", nil)
	rec := httptest.NewRecorder()

	handlers.ListProjects(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

// ---- Tests for CreateProject ----

func TestCreateProject_Success(t *testing.T) {
	store := newMockProjectStore()
	handlers := NewProjectHandlers(store, nil, "v1.0.0")

	body := `{"name": "test-project", "displayName": "Test Project", "userName": "testuser"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/projects", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handlers.CreateProject(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("expected status 201, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateProject_InvalidJSON(t *testing.T) {
	store := newMockProjectStore()
	handlers := NewProjectHandlers(store, nil, "v1.0.0")

	req := httptest.NewRequest(http.MethodPost, "/api/admin/projects", strings.NewReader("invalid"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handlers.CreateProject(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}
}

func TestCreateProject_MissingName(t *testing.T) {
	store := newMockProjectStore()
	handlers := NewProjectHandlers(store, nil, "v1.0.0")

	body := `{"displayName": "Test Project", "userName": "testuser"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/projects", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handlers.CreateProject(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}
}

func TestCreateProject_NameTooLong(t *testing.T) {
	store := newMockProjectStore()
	handlers := NewProjectHandlers(store, nil, "v1.0.0")

	longName := strings.Repeat("a", 64)
	body := `{"name": "` + longName + `", "displayName": "Test Project", "userName": "testuser"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/projects", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handlers.CreateProject(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}
}

func TestCreateProject_MissingDisplayName(t *testing.T) {
	store := newMockProjectStore()
	handlers := NewProjectHandlers(store, nil, "v1.0.0")

	body := `{"name": "test-project", "userName": "testuser"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/projects", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handlers.CreateProject(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}
}

func TestCreateProject_MissingUserName(t *testing.T) {
	store := newMockProjectStore()
	handlers := NewProjectHandlers(store, nil, "v1.0.0")

	body := `{"name": "test-project", "displayName": "Test Project"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/projects", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handlers.CreateProject(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}
}

func TestCreateProject_InvalidName(t *testing.T) {
	store := newMockProjectStore()
	store.createErr = projects.ErrInvalidName
	handlers := NewProjectHandlers(store, nil, "v1.0.0")

	body := `{"name": "Invalid_Name!", "displayName": "Test Project", "userName": "testuser"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/projects", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handlers.CreateProject(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}
}

func TestCreateProject_DuplicateName(t *testing.T) {
	store := newMockProjectStore()
	store.createErr = projects.ErrDuplicateName
	handlers := NewProjectHandlers(store, nil, "v1.0.0")

	body := `{"name": "existing-project", "displayName": "Test Project", "userName": "testuser"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/projects", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handlers.CreateProject(rec, req)

	if rec.Code != http.StatusConflict {
		t.Errorf("expected status 409, got %d", rec.Code)
	}
}

func TestCreateProject_StoreError(t *testing.T) {
	store := newMockProjectStore()
	store.createErr = errors.New("database error")
	handlers := NewProjectHandlers(store, nil, "v1.0.0")

	body := `{"name": "test-project", "displayName": "Test Project", "userName": "testuser"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/projects", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handlers.CreateProject(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", rec.Code)
	}
}

// ---- Tests for GetProject ----

func TestGetProject_Success(t *testing.T) {
	store := newMockProjectStore()
	id := uuid.New()
	store.projects[id] = &projects.Project{
		ID:          id,
		Name:        "test-project",
		DisplayName: "Test Project",
	}
	handlers := NewProjectHandlers(store, nil, "v1.0.0")

	req := httptest.NewRequest(http.MethodGet, "/api/admin/projects/"+id.String(), nil)
	req.SetPathValue("id", id.String())
	rec := httptest.NewRecorder()

	handlers.GetProject(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetProject_NotFound(t *testing.T) {
	store := newMockProjectStore()
	handlers := NewProjectHandlers(store, nil, "v1.0.0")

	id := uuid.New()
	req := httptest.NewRequest(http.MethodGet, "/api/admin/projects/"+id.String(), nil)
	req.SetPathValue("id", id.String())
	rec := httptest.NewRecorder()

	handlers.GetProject(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", rec.Code)
	}
}

func TestGetProject_InvalidUUID(t *testing.T) {
	store := newMockProjectStore()
	handlers := NewProjectHandlers(store, nil, "v1.0.0")

	req := httptest.NewRequest(http.MethodGet, "/api/admin/projects/invalid-uuid", nil)
	req.SetPathValue("id", "invalid-uuid")
	rec := httptest.NewRecorder()

	handlers.GetProject(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}
}

func TestGetProject_MissingID(t *testing.T) {
	store := newMockProjectStore()
	handlers := NewProjectHandlers(store, nil, "v1.0.0")

	req := httptest.NewRequest(http.MethodGet, "/api/admin/projects/", nil)
	// Don't set path value
	rec := httptest.NewRecorder()

	handlers.GetProject(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}
}

func TestGetProject_StoreError(t *testing.T) {
	store := newMockProjectStore()
	store.getErr = errors.New("database error")
	handlers := NewProjectHandlers(store, nil, "v1.0.0")

	id := uuid.New()
	req := httptest.NewRequest(http.MethodGet, "/api/admin/projects/"+id.String(), nil)
	req.SetPathValue("id", id.String())
	rec := httptest.NewRecorder()

	handlers.GetProject(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", rec.Code)
	}
}

// ---- Tests for UpdateProject ----

func TestUpdateProject_Success(t *testing.T) {
	store := newMockProjectStore()
	id := uuid.New()
	store.projects[id] = &projects.Project{
		ID:          id,
		Name:        "test-project",
		DisplayName: "Test Project",
	}
	handlers := NewProjectHandlers(store, nil, "v1.0.0")

	body := `{"displayName": "Updated Name"}`
	req := httptest.NewRequest(http.MethodPut, "/api/admin/projects/"+id.String(), strings.NewReader(body))
	req.SetPathValue("id", id.String())
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handlers.UpdateProject(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdateProject_InvalidJSON(t *testing.T) {
	store := newMockProjectStore()
	id := uuid.New()
	store.projects[id] = &projects.Project{ID: id}
	handlers := NewProjectHandlers(store, nil, "v1.0.0")

	req := httptest.NewRequest(http.MethodPut, "/api/admin/projects/"+id.String(), strings.NewReader("invalid"))
	req.SetPathValue("id", id.String())
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handlers.UpdateProject(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}
}

func TestUpdateProject_NotFound(t *testing.T) {
	store := newMockProjectStore()
	handlers := NewProjectHandlers(store, nil, "v1.0.0")

	id := uuid.New()
	body := `{"displayName": "Updated Name"}`
	req := httptest.NewRequest(http.MethodPut, "/api/admin/projects/"+id.String(), strings.NewReader(body))
	req.SetPathValue("id", id.String())
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handlers.UpdateProject(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", rec.Code)
	}
}

func TestUpdateProject_TriggersUpdate(t *testing.T) {
	store := newMockProjectStore()
	id := uuid.New()
	store.projects[id] = &projects.Project{
		ID:          id,
		Name:        "test-project",
		DisplayName: "Test Project",
	}
	handlers := NewProjectHandlers(store, nil, "v1.0.0")

	// Update with imageTag should trigger status change
	body := `{"imageTag": "v2.0.0"}`
	req := httptest.NewRequest(http.MethodPut, "/api/admin/projects/"+id.String(), strings.NewReader(body))
	req.SetPathValue("id", id.String())
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handlers.UpdateProject(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	// Verify SetStatus was called
	if store.calledStatus != projects.StatusUpdating {
		t.Errorf("expected status to be set to 'updating', got %q", store.calledStatus)
	}
}

// ---- Tests for DeleteProject ----

func TestDeleteProject_Success(t *testing.T) {
	store := newMockProjectStore()
	id := uuid.New()
	store.projects[id] = &projects.Project{
		ID:   id,
		Name: "test-project",
	}
	handlers := NewProjectHandlers(store, nil, "v1.0.0")

	req := httptest.NewRequest(http.MethodDelete, "/api/admin/projects/"+id.String(), nil)
	req.SetPathValue("id", id.String())
	rec := httptest.NewRecorder()

	handlers.DeleteProject(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("expected status 204, got %d", rec.Code)
	}
}

func TestDeleteProject_NotFound(t *testing.T) {
	store := newMockProjectStore()
	handlers := NewProjectHandlers(store, nil, "v1.0.0")

	id := uuid.New()
	req := httptest.NewRequest(http.MethodDelete, "/api/admin/projects/"+id.String(), nil)
	req.SetPathValue("id", id.String())
	rec := httptest.NewRecorder()

	handlers.DeleteProject(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", rec.Code)
	}
}

func TestDeleteProject_CallsCallback(t *testing.T) {
	store := newMockProjectStore()
	id := uuid.New()
	store.projects[id] = &projects.Project{
		ID:   id,
		Name: "test-project",
	}
	handlers := NewProjectHandlers(store, nil, "v1.0.0")

	var callbackCalled bool
	var callbackProjectName string
	handlers.SetDeleteCallback(func(projectName string) {
		callbackCalled = true
		callbackProjectName = projectName
	})

	req := httptest.NewRequest(http.MethodDelete, "/api/admin/projects/"+id.String(), nil)
	req.SetPathValue("id", id.String())
	rec := httptest.NewRecorder()

	handlers.DeleteProject(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("expected status 204, got %d", rec.Code)
	}
	if !callbackCalled {
		t.Error("expected callback to be called")
	}
	if callbackProjectName != "test-project" {
		t.Errorf("expected callback project name 'test-project', got %q", callbackProjectName)
	}
}

func TestDeleteProject_DeleteError(t *testing.T) {
	store := newMockProjectStore()
	id := uuid.New()
	store.projects[id] = &projects.Project{
		ID:   id,
		Name: "test-project",
	}
	store.deleteErr = errors.New("database error")
	handlers := NewProjectHandlers(store, nil, "v1.0.0")

	req := httptest.NewRequest(http.MethodDelete, "/api/admin/projects/"+id.String(), nil)
	req.SetPathValue("id", id.String())
	rec := httptest.NewRecorder()

	handlers.DeleteProject(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", rec.Code)
	}
}

// ---- Tests for GetUpgradeInfo ----

func TestGetUpgradeInfo_Success(t *testing.T) {
	store := newMockProjectStore()
	id := uuid.New()
	lastActivity := time.Now().Add(-30 * time.Minute)
	store.projects[id] = &projects.Project{
		ID:           id,
		Name:         "test-project",
		ImageTag:     "v1.0.0",
		LastActivity: &lastActivity,
	}
	handlers := NewProjectHandlers(store, nil, "v2.0.0")

	req := httptest.NewRequest(http.MethodGet, "/api/admin/projects/"+id.String()+"/upgrade-info", nil)
	req.SetPathValue("id", id.String())
	rec := httptest.NewRecorder()

	handlers.GetUpgradeInfo(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["currentVersion"] != "v1.0.0" {
		t.Errorf("expected currentVersion 'v1.0.0', got %v", resp["currentVersion"])
	}
	if resp["recommendedVersion"] != "v2.0.0" {
		t.Errorf("expected recommendedVersion 'v2.0.0', got %v", resp["recommendedVersion"])
	}
	if resp["minutesSinceActivity"] == nil {
		t.Error("expected minutesSinceActivity to be set")
	}
}

func TestGetUpgradeInfo_NoLastActivity(t *testing.T) {
	store := newMockProjectStore()
	id := uuid.New()
	store.projects[id] = &projects.Project{
		ID:           id,
		Name:         "test-project",
		ImageTag:     "v1.0.0",
		LastActivity: nil, // No activity
	}
	handlers := NewProjectHandlers(store, nil, "v2.0.0")

	req := httptest.NewRequest(http.MethodGet, "/api/admin/projects/"+id.String()+"/upgrade-info", nil)
	req.SetPathValue("id", id.String())
	rec := httptest.NewRecorder()

	handlers.GetUpgradeInfo(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["minutesSinceActivity"] != nil {
		t.Errorf("expected minutesSinceActivity to be nil, got %v", resp["minutesSinceActivity"])
	}
}

func TestGetUpgradeInfo_NotFound(t *testing.T) {
	store := newMockProjectStore()
	handlers := NewProjectHandlers(store, nil, "v2.0.0")

	id := uuid.New()
	req := httptest.NewRequest(http.MethodGet, "/api/admin/projects/"+id.String()+"/upgrade-info", nil)
	req.SetPathValue("id", id.String())
	rec := httptest.NewRecorder()

	handlers.GetUpgradeInfo(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", rec.Code)
	}
}

// ---- Tests for UpgradeProject ----

func TestUpgradeProject_Success(t *testing.T) {
	store := newMockProjectStore()
	id := uuid.New()
	store.projects[id] = &projects.Project{
		ID:       id,
		Name:     "test-project",
		ImageTag: "v1.0.0",
	}
	handlers := NewProjectHandlers(store, nil, "v2.0.0")

	body := `{"imageTag": "v2.0.0"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/projects/"+id.String()+"/upgrade", strings.NewReader(body))
	req.SetPathValue("id", id.String())
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handlers.UpgradeProject(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpgradeProject_InvalidJSON(t *testing.T) {
	store := newMockProjectStore()
	id := uuid.New()
	store.projects[id] = &projects.Project{ID: id}
	handlers := NewProjectHandlers(store, nil, "v2.0.0")

	req := httptest.NewRequest(http.MethodPost, "/api/admin/projects/"+id.String()+"/upgrade", strings.NewReader("invalid"))
	req.SetPathValue("id", id.String())
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handlers.UpgradeProject(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}
}

func TestUpgradeProject_MissingImageTag(t *testing.T) {
	store := newMockProjectStore()
	id := uuid.New()
	store.projects[id] = &projects.Project{ID: id}
	handlers := NewProjectHandlers(store, nil, "v2.0.0")

	body := `{}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/projects/"+id.String()+"/upgrade", strings.NewReader(body))
	req.SetPathValue("id", id.String())
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handlers.UpgradeProject(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}
}

func TestUpgradeProject_InvalidVersion(t *testing.T) {
	tests := []struct {
		name     string
		imageTag string
	}{
		{"prerelease suffix", "v1.0.0-beta"},
		{"rc suffix", "v1.0.0-rc1"},
		{"invalid format", "not-a-version"},
		{"missing minor", "v1.0"},
		{"empty string after trim", "   "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newMockProjectStore()
			id := uuid.New()
			store.projects[id] = &projects.Project{ID: id}
			handlers := NewProjectHandlers(store, nil, "v2.0.0")

			body := `{"imageTag": "` + tt.imageTag + `"}`
			req := httptest.NewRequest(http.MethodPost, "/api/admin/projects/"+id.String()+"/upgrade", strings.NewReader(body))
			req.SetPathValue("id", id.String())
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			handlers.UpgradeProject(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Errorf("expected status 400, got %d for %q", rec.Code, tt.imageTag)
			}
		})
	}
}

func TestUpgradeProject_ValidVersionFormats(t *testing.T) {
	tests := []struct {
		name     string
		imageTag string
	}{
		{"with v prefix", "v1.2.3"},
		{"without v prefix", "1.2.3"},
		{"zero version", "v0.0.0"},
		{"large numbers", "v100.200.300"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newMockProjectStore()
			id := uuid.New()
			store.projects[id] = &projects.Project{ID: id}
			handlers := NewProjectHandlers(store, nil, "v2.0.0")

			body := `{"imageTag": "` + tt.imageTag + `"}`
			req := httptest.NewRequest(http.MethodPost, "/api/admin/projects/"+id.String()+"/upgrade", strings.NewReader(body))
			req.SetPathValue("id", id.String())
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			handlers.UpgradeProject(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("expected status 200, got %d for %q", rec.Code, tt.imageTag)
			}
		})
	}
}

func TestUpgradeProject_NotFound(t *testing.T) {
	store := newMockProjectStore()
	store.updateErr = projects.ErrProjectNotFound
	handlers := NewProjectHandlers(store, nil, "v2.0.0")

	id := uuid.New()
	body := `{"imageTag": "v2.0.0"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/projects/"+id.String()+"/upgrade", strings.NewReader(body))
	req.SetPathValue("id", id.String())
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handlers.UpgradeProject(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", rec.Code)
	}
}

// ---- Tests for isValidVersion ----

func TestIsValidVersion(t *testing.T) {
	tests := []struct {
		version string
		valid   bool
	}{
		// Valid versions
		{"v1.2.3", true},
		{"1.2.3", true},
		{"v0.0.0", true},
		{"0.0.1", true},
		{"v10.20.30", true},

		// Invalid versions (per project standards - no prerelease suffixes)
		{"v1.2.3-rc1", false},
		{"v1.2.3-beta", false},
		{"v1.2.3-alpha", false},
		{"v1.2.3+build", false},
		{"v1.2", false},
		{"v1", false},
		{"latest", false},
		{"", false},
		{"master", false},
		{"main", false},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			got := isValidVersion(tt.version)
			if got != tt.valid {
				t.Errorf("isValidVersion(%q) = %v, want %v", tt.version, got, tt.valid)
			}
		})
	}
}

// ---- Tests for extractProjectID ----

func TestExtractProjectID(t *testing.T) {
	tests := []struct {
		name      string
		pathValue string
		wantErr   bool
	}{
		{"valid UUID", uuid.New().String(), false},
		{"empty string", "", true},
		{"invalid UUID", "not-a-uuid", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			if tt.pathValue != "" {
				req.SetPathValue("id", tt.pathValue)
			}

			_, err := extractProjectID(req)
			if (err != nil) != tt.wantErr {
				t.Errorf("extractProjectID() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// ---- Tests for NewProjectHandlers ----

func TestNewProjectHandlers(t *testing.T) {
	store := newMockProjectStore()
	// Note: passing nil for controller is valid for many operations
	handlers := NewProjectHandlers(store, nil, "v1.0.0")

	if handlers == nil {
		t.Fatal("expected handlers to be non-nil")
	}
	if handlers.store != store {
		t.Error("expected store to be set")
	}
	if handlers.recommendedImageTag != "v1.0.0" {
		t.Errorf("expected recommendedImageTag 'v1.0.0', got %q", handlers.recommendedImageTag)
	}
}

func TestSetDeleteCallback(t *testing.T) {
	store := newMockProjectStore()
	handlers := NewProjectHandlers(store, nil, "v1.0.0")

	var called bool
	cb := func(projectName string) {
		called = true
	}

	handlers.SetDeleteCallback(cb)

	if handlers.onProjectDeleted == nil {
		t.Error("expected callback to be set")
	}

	// Verify callback is callable
	handlers.onProjectDeleted("test")
	if !called {
		t.Error("expected callback to be called")
	}
}

// ---- Tests for RegisterRoutes ----

func TestRegisterRoutes(t *testing.T) {
	store := newMockProjectStore()
	handlers := NewProjectHandlers(store, nil, "v1.0.0")

	mux := http.NewServeMux()
	handlers.RegisterRoutes(mux)

	// Test that routes are registered by trying to match patterns
	// Just verify no panic occurs
	req := httptest.NewRequest(http.MethodGet, "/api/admin/projects", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	// Should return 200 (empty list), not 404
	if rec.Code == http.StatusNotFound {
		t.Error("expected route to be registered")
	}
}

// ---- Table-driven tests for handler edge cases ----

func TestProjectHandlers_TableDriven(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		pathValue  string
		setupStore func(*mockProjectStore)
		wantStatus int
	}{
		{
			name:       "list empty projects",
			method:     http.MethodGet,
			path:       "/api/admin/projects",
			wantStatus: http.StatusOK,
		},
		{
			name:       "get project invalid id",
			method:     http.MethodGet,
			path:       "/api/admin/projects/invalid",
			pathValue:  "invalid",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "create project whitespace name",
			method:     http.MethodPost,
			path:       "/api/admin/projects",
			body:       `{"name": "   ", "displayName": "Test", "userName": "test"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "create project whitespace displayName",
			method:     http.MethodPost,
			path:       "/api/admin/projects",
			body:       `{"name": "test", "displayName": "   ", "userName": "test"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "create project whitespace userName",
			method:     http.MethodPost,
			path:       "/api/admin/projects",
			body:       `{"name": "test", "displayName": "Test", "userName": "   "}`,
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newMockProjectStore()
			if tt.setupStore != nil {
				tt.setupStore(store)
			}
			handlers := NewProjectHandlers(store, nil, "v1.0.0")

			var bodyReader *strings.Reader
			if tt.body != "" {
				bodyReader = strings.NewReader(tt.body)
			} else {
				bodyReader = strings.NewReader("")
			}

			req := httptest.NewRequest(tt.method, tt.path, bodyReader)
			if tt.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			if tt.pathValue != "" {
				req.SetPathValue("id", tt.pathValue)
			}
			rec := httptest.NewRecorder()

			// Route to appropriate handler
			switch {
			case tt.method == http.MethodGet && tt.path == "/api/admin/projects":
				handlers.ListProjects(rec, req)
			case tt.method == http.MethodPost && tt.path == "/api/admin/projects":
				handlers.CreateProject(rec, req)
			case tt.method == http.MethodGet && strings.Contains(tt.path, "/api/admin/projects/"):
				handlers.GetProject(rec, req)
			default:
				t.Fatalf("unknown route: %s %s", tt.method, tt.path)
			}

			if rec.Code != tt.wantStatus {
				t.Errorf("expected status %d, got %d: %s", tt.wantStatus, rec.Code, rec.Body.String())
			}
		})
	}
}

// ---- Test response format consistency ----

func TestResponseFormat_ListProjects(t *testing.T) {
	store := newMockProjectStore()
	handlers := NewProjectHandlers(store, nil, "v1.0.0")

	req := httptest.NewRequest(http.MethodGet, "/api/admin/projects", nil)
	rec := httptest.NewRecorder()

	handlers.ListProjects(rec, req)

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Verify required fields
	if _, ok := resp["projects"]; !ok {
		t.Error("response missing 'projects' field")
	}
	if _, ok := resp["total"]; !ok {
		t.Error("response missing 'total' field")
	}
}

func TestResponseFormat_ErrorResponse(t *testing.T) {
	store := newMockProjectStore()
	handlers := NewProjectHandlers(store, nil, "v1.0.0")

	// Trigger a bad request error
	req := httptest.NewRequest(http.MethodPost, "/api/admin/projects", strings.NewReader("invalid"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handlers.CreateProject(rec, req)

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Error responses should have standard format
	if _, ok := resp["error"]; !ok {
		t.Error("error response missing 'error' field")
	}
}

// ---- Benchmark tests ----

func BenchmarkListProjects(b *testing.B) {
	store := newMockProjectStore()
	for i := 0; i < 100; i++ {
		id := uuid.New()
		store.projects[id] = &projects.Project{
			ID:          id,
			Name:        "test-project",
			DisplayName: "Test Project",
		}
	}
	handlers := NewProjectHandlers(store, nil, "v1.0.0")

	req := httptest.NewRequest(http.MethodGet, "/api/admin/projects", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		handlers.ListProjects(rec, req)
	}
}

func BenchmarkCreateProject(b *testing.B) {
	store := newMockProjectStore()
	handlers := NewProjectHandlers(store, nil, "v1.0.0")

	body := `{"name": "test-project", "displayName": "Test Project", "userName": "testuser"}`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/admin/projects", bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handlers.CreateProject(rec, req)
	}
}

// ---- Tests for RestartProject (pre-controller paths) ----

func TestRestartProject_InvalidID(t *testing.T) {
	store := newMockProjectStore()
	handlers := NewProjectHandlers(store, nil, "v1.0.0")

	req := httptest.NewRequest(http.MethodPost, "/api/admin/projects/invalid/restart", nil)
	req.SetPathValue("id", "invalid")
	rec := httptest.NewRecorder()

	handlers.RestartProject(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}
}

func TestRestartProject_NotFound(t *testing.T) {
	store := newMockProjectStore()
	handlers := NewProjectHandlers(store, nil, "v1.0.0")

	id := uuid.New()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/projects/"+id.String()+"/restart", nil)
	req.SetPathValue("id", id.String())
	rec := httptest.NewRecorder()

	handlers.RestartProject(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", rec.Code)
	}
}

func TestRestartProject_WrongStatus(t *testing.T) {
	store := newMockProjectStore()
	id := uuid.New()
	store.projects[id] = &projects.Project{
		ID:     id,
		Name:   "test-project",
		Status: projects.StatusPending, // Not running or failed
	}
	handlers := NewProjectHandlers(store, nil, "v1.0.0")

	req := httptest.NewRequest(http.MethodPost, "/api/admin/projects/"+id.String()+"/restart", nil)
	req.SetPathValue("id", id.String())
	rec := httptest.NewRecorder()

	handlers.RestartProject(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRestartProject_GetError(t *testing.T) {
	store := newMockProjectStore()
	store.getErr = errors.New("database error")
	handlers := NewProjectHandlers(store, nil, "v1.0.0")

	id := uuid.New()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/projects/"+id.String()+"/restart", nil)
	req.SetPathValue("id", id.String())
	rec := httptest.NewRecorder()

	handlers.RestartProject(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", rec.Code)
	}
}

// ---- Tests for GetProjectStatus (pre-controller paths) ----

func TestGetProjectStatus_InvalidID(t *testing.T) {
	store := newMockProjectStore()
	handlers := NewProjectHandlers(store, nil, "v1.0.0")

	req := httptest.NewRequest(http.MethodGet, "/api/admin/projects/invalid/status", nil)
	req.SetPathValue("id", "invalid")
	rec := httptest.NewRecorder()

	handlers.GetProjectStatus(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}
}

func TestGetProjectStatus_NotFound(t *testing.T) {
	store := newMockProjectStore()
	handlers := NewProjectHandlers(store, nil, "v1.0.0")

	id := uuid.New()
	req := httptest.NewRequest(http.MethodGet, "/api/admin/projects/"+id.String()+"/status", nil)
	req.SetPathValue("id", id.String())
	rec := httptest.NewRecorder()

	handlers.GetProjectStatus(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", rec.Code)
	}
}

func TestGetProjectStatus_GetError(t *testing.T) {
	store := newMockProjectStore()
	store.getErr = errors.New("database error")
	handlers := NewProjectHandlers(store, nil, "v1.0.0")

	id := uuid.New()
	req := httptest.NewRequest(http.MethodGet, "/api/admin/projects/"+id.String()+"/status", nil)
	req.SetPathValue("id", id.String())
	rec := httptest.NewRecorder()

	handlers.GetProjectStatus(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", rec.Code)
	}
}

// ---- Tests for GetProjectSecrets (pre-controller paths) ----

func TestGetProjectSecrets_InvalidID(t *testing.T) {
	store := newMockProjectStore()
	handlers := NewProjectHandlers(store, nil, "v1.0.0")

	req := httptest.NewRequest(http.MethodGet, "/api/admin/projects/invalid/secrets", nil)
	req.SetPathValue("id", "invalid")
	rec := httptest.NewRecorder()

	handlers.GetProjectSecrets(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}
}

func TestGetProjectSecrets_NotFound(t *testing.T) {
	store := newMockProjectStore()
	handlers := NewProjectHandlers(store, nil, "v1.0.0")

	id := uuid.New()
	req := httptest.NewRequest(http.MethodGet, "/api/admin/projects/"+id.String()+"/secrets", nil)
	req.SetPathValue("id", id.String())
	rec := httptest.NewRecorder()

	handlers.GetProjectSecrets(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", rec.Code)
	}
}

func TestGetProjectSecrets_GetError(t *testing.T) {
	store := newMockProjectStore()
	store.getErr = errors.New("database error")
	handlers := NewProjectHandlers(store, nil, "v1.0.0")

	id := uuid.New()
	req := httptest.NewRequest(http.MethodGet, "/api/admin/projects/"+id.String()+"/secrets", nil)
	req.SetPathValue("id", id.String())
	rec := httptest.NewRecorder()

	handlers.GetProjectSecrets(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", rec.Code)
	}
}

// ---- Tests for UpdateProjectSecrets (pre-controller paths) ----

func TestUpdateProjectSecrets_InvalidID(t *testing.T) {
	store := newMockProjectStore()
	handlers := NewProjectHandlers(store, nil, "v1.0.0")

	body := `{"secrets": {"KEY": "value"}}`
	req := httptest.NewRequest(http.MethodPut, "/api/admin/projects/invalid/secrets", strings.NewReader(body))
	req.SetPathValue("id", "invalid")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handlers.UpdateProjectSecrets(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}
}

func TestUpdateProjectSecrets_InvalidJSON(t *testing.T) {
	store := newMockProjectStore()
	id := uuid.New()
	store.projects[id] = &projects.Project{ID: id}
	handlers := NewProjectHandlers(store, nil, "v1.0.0")

	req := httptest.NewRequest(http.MethodPut, "/api/admin/projects/"+id.String()+"/secrets", strings.NewReader("invalid"))
	req.SetPathValue("id", id.String())
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handlers.UpdateProjectSecrets(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}
}

func TestUpdateProjectSecrets_NotFound(t *testing.T) {
	store := newMockProjectStore()
	handlers := NewProjectHandlers(store, nil, "v1.0.0")

	id := uuid.New()
	body := `{"secrets": {"KEY": "value"}}`
	req := httptest.NewRequest(http.MethodPut, "/api/admin/projects/"+id.String()+"/secrets", strings.NewReader(body))
	req.SetPathValue("id", id.String())
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handlers.UpdateProjectSecrets(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", rec.Code)
	}
}

func TestUpdateProjectSecrets_GetError(t *testing.T) {
	store := newMockProjectStore()
	store.getErr = errors.New("database error")
	handlers := NewProjectHandlers(store, nil, "v1.0.0")

	id := uuid.New()
	body := `{"secrets": {"KEY": "value"}}`
	req := httptest.NewRequest(http.MethodPut, "/api/admin/projects/"+id.String()+"/secrets", strings.NewReader(body))
	req.SetPathValue("id", id.String())
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handlers.UpdateProjectSecrets(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", rec.Code)
	}
}

// ---- Tests for UpgradeProject errors ----

func TestUpgradeProject_StoreError(t *testing.T) {
	store := newMockProjectStore()
	store.updateErr = errors.New("database error")
	id := uuid.New()
	store.projects[id] = &projects.Project{ID: id}
	handlers := NewProjectHandlers(store, nil, "v1.0.0")

	body := `{"imageTag": "v2.0.0"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/projects/"+id.String()+"/upgrade", strings.NewReader(body))
	req.SetPathValue("id", id.String())
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handlers.UpgradeProject(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", rec.Code)
	}
}

func TestUpgradeProject_InvalidID(t *testing.T) {
	store := newMockProjectStore()
	handlers := NewProjectHandlers(store, nil, "v1.0.0")

	body := `{"imageTag": "v2.0.0"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/projects/invalid/upgrade", strings.NewReader(body))
	req.SetPathValue("id", "invalid")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handlers.UpgradeProject(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}
}

// ---- Tests for GetUpgradeInfo errors ----

func TestGetUpgradeInfo_InvalidID(t *testing.T) {
	store := newMockProjectStore()
	handlers := NewProjectHandlers(store, nil, "v1.0.0")

	req := httptest.NewRequest(http.MethodGet, "/api/admin/projects/invalid/upgrade-info", nil)
	req.SetPathValue("id", "invalid")
	rec := httptest.NewRecorder()

	handlers.GetUpgradeInfo(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}
}

func TestGetUpgradeInfo_StoreError(t *testing.T) {
	store := newMockProjectStore()
	store.getErr = errors.New("database error")
	handlers := NewProjectHandlers(store, nil, "v1.0.0")

	id := uuid.New()
	req := httptest.NewRequest(http.MethodGet, "/api/admin/projects/"+id.String()+"/upgrade-info", nil)
	req.SetPathValue("id", id.String())
	rec := httptest.NewRecorder()

	handlers.GetUpgradeInfo(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", rec.Code)
	}
}

// ---- Tests for UpdateProject error paths ----

func TestUpdateProject_InvalidID(t *testing.T) {
	store := newMockProjectStore()
	handlers := NewProjectHandlers(store, nil, "v1.0.0")

	body := `{"displayName": "Updated"}`
	req := httptest.NewRequest(http.MethodPut, "/api/admin/projects/invalid", strings.NewReader(body))
	req.SetPathValue("id", "invalid")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handlers.UpdateProject(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}
}

func TestUpdateProject_StoreError(t *testing.T) {
	store := newMockProjectStore()
	store.updateErr = errors.New("database error")
	id := uuid.New()
	store.projects[id] = &projects.Project{ID: id}
	handlers := NewProjectHandlers(store, nil, "v1.0.0")

	body := `{"displayName": "Updated"}`
	req := httptest.NewRequest(http.MethodPut, "/api/admin/projects/"+id.String(), strings.NewReader(body))
	req.SetPathValue("id", id.String())
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handlers.UpdateProject(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", rec.Code)
	}
}

// ---- Tests for DeleteProject error paths ----

func TestDeleteProject_InvalidID(t *testing.T) {
	store := newMockProjectStore()
	handlers := NewProjectHandlers(store, nil, "v1.0.0")

	req := httptest.NewRequest(http.MethodDelete, "/api/admin/projects/invalid", nil)
	req.SetPathValue("id", "invalid")
	rec := httptest.NewRecorder()

	handlers.DeleteProject(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}
}

func TestDeleteProject_GetError(t *testing.T) {
	store := newMockProjectStore()
	store.getErr = errors.New("database error")
	handlers := NewProjectHandlers(store, nil, "v1.0.0")

	id := uuid.New()
	req := httptest.NewRequest(http.MethodDelete, "/api/admin/projects/"+id.String(), nil)
	req.SetPathValue("id", id.String())
	rec := httptest.NewRecorder()

	handlers.DeleteProject(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", rec.Code)
	}
}

func TestDeleteProject_DeleteReturnsNotFound(t *testing.T) {
	store := newMockProjectStore()
	id := uuid.New()
	store.projects[id] = &projects.Project{ID: id, Name: "test"}
	store.deleteErr = projects.ErrProjectNotFound // Delete returns not found (race condition case)
	handlers := NewProjectHandlers(store, nil, "v1.0.0")

	req := httptest.NewRequest(http.MethodDelete, "/api/admin/projects/"+id.String(), nil)
	req.SetPathValue("id", id.String())
	rec := httptest.NewRecorder()

	handlers.DeleteProject(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", rec.Code)
	}
}
