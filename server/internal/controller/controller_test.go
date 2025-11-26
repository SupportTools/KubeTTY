package controller

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/supporttools/KubeTTY/server/internal/projects"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// mockStore implements projects.Store for testing.
type mockStore struct {
	mu       sync.RWMutex
	projects map[uuid.UUID]*projects.Project
	statuses []statusUpdate
}

type statusUpdate struct {
	id      uuid.UUID
	status  projects.ProjectStatus
	message string
}

func newMockStore() *mockStore {
	return &mockStore{
		projects: make(map[uuid.UUID]*projects.Project),
		statuses: []statusUpdate{},
	}
}

func (m *mockStore) Create(ctx context.Context, req projects.CreateProjectRequest) (*projects.Project, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	p := &projects.Project{
		ID:              uuid.New(),
		Name:            req.Name,
		DisplayName:     req.DisplayName,
		TargetNamespace: fmt.Sprintf("kubetty-%s", req.Name),
		ServiceName:     projects.ComputeServiceName(req.Name),
		SessionID:       uuid.New(),
		UserName:        req.UserName,
		CPURequest:      req.CPURequest,
		CPULimit:        req.CPULimit,
		MemoryRequest:   req.MemoryRequest,
		MemoryLimit:     req.MemoryLimit,
		StorageSize:     req.StorageSize,
		StorageClass:    req.StorageClass,
		AdminNamespaces: req.AdminNamespaces,
		ReadNamespaces:  req.ReadNamespaces,
		Status:          projects.StatusPending,
		CreatedAt:       time.Now(),
	}
	m.projects[p.ID] = p
	return p, nil
}

func (m *mockStore) Get(ctx context.Context, id uuid.UUID) (*projects.Project, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if p, ok := m.projects[id]; ok {
		return p, nil
	}
	return nil, projects.ErrProjectNotFound
}

func (m *mockStore) GetByName(ctx context.Context, name string) (*projects.Project, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, p := range m.projects {
		if p.Name == name {
			return p, nil
		}
	}
	return nil, projects.ErrProjectNotFound
}

func (m *mockStore) GetByServiceName(ctx context.Context, serviceName string) (*projects.Project, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, p := range m.projects {
		if p.ServiceName == serviceName {
			return p, nil
		}
	}
	return nil, projects.ErrProjectNotFound
}

func (m *mockStore) List(ctx context.Context, filter projects.ListFilter) ([]projects.Project, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []projects.Project
	for _, p := range m.projects {
		result = append(result, *p)
	}
	return result, nil
}

func (m *mockStore) Update(ctx context.Context, id uuid.UUID, req projects.UpdateProjectRequest) (*projects.Project, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.projects[id]
	if !ok {
		return nil, projects.ErrProjectNotFound
	}
	return p, nil
}

func (m *mockStore) Delete(ctx context.Context, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.projects[id]; !ok {
		return projects.ErrProjectNotFound
	}
	m.projects[id].Status = projects.StatusDeleting
	return nil
}

func (m *mockStore) HardDelete(ctx context.Context, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.projects, id)
	return nil
}

func (m *mockStore) SetStatus(ctx context.Context, id uuid.UUID, status projects.ProjectStatus, message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p, ok := m.projects[id]; ok {
		p.Status = status
		p.StatusMessage = message
		m.statuses = append(m.statuses, statusUpdate{id: id, status: status, message: message})
		return nil
	}
	return projects.ErrProjectNotFound
}

func (m *mockStore) UpdateHealthCheck(ctx context.Context, id uuid.UUID, podIP string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p, ok := m.projects[id]; ok {
		p.PodIP = podIP
		now := time.Now()
		p.LastHealthCheck = &now
		return nil
	}
	return projects.ErrProjectNotFound
}

func (m *mockStore) UpdateLastActivity(ctx context.Context, projectName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.projects {
		if p.Name == projectName {
			now := time.Now()
			p.LastActivity = &now
			return nil
		}
	}
	return projects.ErrProjectNotFound
}

func (m *mockStore) ListByStatuses(ctx context.Context, statuses []projects.ProjectStatus) ([]projects.Project, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []projects.Project
	statusSet := make(map[projects.ProjectStatus]bool)
	for _, s := range statuses {
		statusSet[s] = true
	}
	for _, p := range m.projects {
		if statusSet[p.Status] {
			result = append(result, *p)
		}
	}
	return result, nil
}

func (m *mockStore) GetStatusCounts(ctx context.Context) (map[projects.ProjectStatus]int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	counts := make(map[projects.ProjectStatus]int)
	for _, p := range m.projects {
		counts[p.Status]++
	}
	return counts, nil
}

func (m *mockStore) GetRecentlyFailed(ctx context.Context, since time.Time, limit int) ([]projects.Project, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []projects.Project
	for _, p := range m.projects {
		if p.Status == projects.StatusFailed && p.UpdatedAt.After(since) {
			result = append(result, *p)
		}
	}
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (m *mockStore) addProject(p *projects.Project) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.projects[p.ID] = p
}

func (m *mockStore) getStatuses() []statusUpdate {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.statuses
}

// newControllerConfig returns a config suitable for controller testing.
func newControllerConfig() Config {
	return Config{
		ReconcileInterval:   time.Hour, // Long interval to prevent automatic reconciles
		HealthCheckInterval: time.Hour,
		EnvSecretName:       "test-env-secrets",
		ResourceConfig: ResourceConfig{
			Namespace: "kubetty-projects",
			Prefix:    "kubetty-project-",
			Env:       "test",
		},
	}
}

// newTestProjectWithStatus creates a test project with a specified status.
func newTestProjectWithStatus(name string, status projects.ProjectStatus) *projects.Project {
	titleCaser := cases.Title(language.English)
	return &projects.Project{
		ID:              uuid.New(),
		Name:            name,
		DisplayName:     titleCaser.String(name),
		TargetNamespace: fmt.Sprintf("kubetty-%s", name),
		ServiceName:     projects.ComputeServiceName(name),
		SessionID:       uuid.New(),
		UserName:        "testuser",
		CPURequest:      "100m",
		CPULimit:        "500m",
		MemoryRequest:   "128Mi",
		MemoryLimit:     "512Mi",
		StorageSize:     "1Gi",
		StorageClass:    "standard",
		AdminNamespaces: []string{"default"},
		ReadNamespaces:  []string{"kube-system"},
		Status:          status,
		CreatedAt:       time.Now(),
	}
}

// TestController_NoNamespaceCreation verifies that the controller does NOT create namespaces.
// All project resources should be created in the shared namespace.
func TestController_NoNamespaceCreation(t *testing.T) {
	client := fake.NewSimpleClientset()
	store := newMockStore()
	cfg := newControllerConfig()

	// Track all API calls (thread-safe)
	var createCalls []k8stesting.Action
	var createMu sync.Mutex

	client.PrependReactor("create", "*", func(action k8stesting.Action) (bool, runtime.Object, error) {
		createMu.Lock()
		createCalls = append(createCalls, action)
		createMu.Unlock()
		return false, nil, nil // Continue with default handling
	})

	ctrl := NewWithClient(cfg, store, client)

	// Create a test project
	project := newTestProjectWithStatus("test-project", projects.StatusPending)
	store.addProject(project)

	// Run handlePending which creates all resources
	ctx := context.Background()
	err := ctrl.handlePending(ctx, project)
	if err != nil {
		t.Fatalf("handlePending failed: %v", err)
	}

	// Verify NO namespace creation calls were made
	createMu.Lock()
	defer createMu.Unlock()
	for _, action := range createCalls {
		if action.GetResource().Resource == "namespaces" {
			t.Errorf("Unexpected namespace creation: controller should use shared namespace, not create new ones")
		}
	}
}

// TestController_NoNamespaceDeletion verifies that the controller does NOT delete namespaces.
func TestController_NoNamespaceDeletion(t *testing.T) {
	client := fake.NewSimpleClientset()
	store := newMockStore()
	cfg := newControllerConfig()

	// Track all API calls (thread-safe)
	var deleteCalls []k8stesting.Action
	var deleteMu sync.Mutex

	client.PrependReactor("delete", "*", func(action k8stesting.Action) (bool, runtime.Object, error) {
		deleteMu.Lock()
		deleteCalls = append(deleteCalls, action)
		deleteMu.Unlock()
		return false, nil, nil // Continue with default handling
	})

	ctrl := NewWithClient(cfg, store, client)

	// Create a test project in deleting state
	project := newTestProjectWithStatus("delete-me", projects.StatusDeleting)
	store.addProject(project)

	// Run handleDeleting which removes resources
	ctx := context.Background()
	err := ctrl.handleDeleting(ctx, project)
	if err != nil {
		t.Fatalf("handleDeleting failed: %v", err)
	}

	// Verify NO namespace deletion calls were made
	deleteMu.Lock()
	defer deleteMu.Unlock()
	for _, action := range deleteCalls {
		if action.GetResource().Resource == "namespaces" {
			t.Errorf("Unexpected namespace deletion: controller should not delete namespaces")
		}
	}
}

// TestController_ResourcesInSharedNamespace verifies that all resources are created in the shared namespace.
func TestController_ResourcesInSharedNamespace(t *testing.T) {
	client := fake.NewSimpleClientset()
	store := newMockStore()
	cfg := newControllerConfig()
	sharedNS := cfg.ResourceConfig.Namespace

	ctrl := NewWithClient(cfg, store, client)

	// Create a test project
	project := newTestProjectWithStatus("shared-ns-test", projects.StatusPending)
	store.addProject(project)

	// Run handlePending
	ctx := context.Background()
	if err := ctrl.handlePending(ctx, project); err != nil {
		t.Fatalf("handlePending failed: %v", err)
	}

	// Verify PVC is in shared namespace
	pvcs, err := client.CoreV1().PersistentVolumeClaims(sharedNS).List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("Failed to list PVCs: %v", err)
	}
	if len(pvcs.Items) != 1 {
		t.Errorf("Expected 1 PVC in shared namespace %s, got %d", sharedNS, len(pvcs.Items))
	}

	// Verify ServiceAccount is in shared namespace
	sas, err := client.CoreV1().ServiceAccounts(sharedNS).List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("Failed to list ServiceAccounts: %v", err)
	}
	if len(sas.Items) != 1 {
		t.Errorf("Expected 1 ServiceAccount in shared namespace %s, got %d", sharedNS, len(sas.Items))
	}

	// Verify Service is in shared namespace
	svcs, err := client.CoreV1().Services(sharedNS).List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("Failed to list Services: %v", err)
	}
	if len(svcs.Items) != 1 {
		t.Errorf("Expected 1 Service in shared namespace %s, got %d", sharedNS, len(svcs.Items))
	}

	// Verify Deployment is in shared namespace
	deploys, err := client.AppsV1().Deployments(sharedNS).List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("Failed to list Deployments: %v", err)
	}
	if len(deploys.Items) != 1 {
		t.Errorf("Expected 1 Deployment in shared namespace %s, got %d", sharedNS, len(deploys.Items))
	}
}

// TestController_CorrectResourceNaming verifies resources are named correctly with prefix.
func TestController_CorrectResourceNaming(t *testing.T) {
	client := fake.NewSimpleClientset()
	store := newMockStore()
	cfg := newControllerConfig()
	sharedNS := cfg.ResourceConfig.Namespace

	ctrl := NewWithClient(cfg, store, client)

	project := newTestProjectWithStatus("naming-test", projects.StatusPending)
	store.addProject(project)

	ctx := context.Background()
	if err := ctrl.handlePending(ctx, project); err != nil {
		t.Fatalf("handlePending failed: %v", err)
	}

	expectedResourceName := cfg.ResourceConfig.ResourceName(project.Name)

	// Verify Deployment name
	deploy, err := client.AppsV1().Deployments(sharedNS).Get(ctx, expectedResourceName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Deployment not found with expected name %s: %v", expectedResourceName, err)
	}
	if deploy.Name != expectedResourceName {
		t.Errorf("Deployment name mismatch: expected %s, got %s", expectedResourceName, deploy.Name)
	}

	// Verify Service name
	svc, err := client.CoreV1().Services(sharedNS).Get(ctx, expectedResourceName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Service not found with expected name %s: %v", expectedResourceName, err)
	}
	if svc.Name != expectedResourceName {
		t.Errorf("Service name mismatch: expected %s, got %s", expectedResourceName, svc.Name)
	}

	// Verify PVC name (has -data suffix)
	expectedPVCName := expectedResourceName + "-data"
	pvc, err := client.CoreV1().PersistentVolumeClaims(sharedNS).Get(ctx, expectedPVCName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("PVC not found with expected name %s: %v", expectedPVCName, err)
	}
	if pvc.Name != expectedPVCName {
		t.Errorf("PVC name mismatch: expected %s, got %s", expectedPVCName, pvc.Name)
	}

	// Verify ServiceAccount name (has -sa suffix)
	expectedSAName := expectedResourceName + "-sa"
	sa, err := client.CoreV1().ServiceAccounts(sharedNS).Get(ctx, expectedSAName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("ServiceAccount not found with expected name %s: %v", expectedSAName, err)
	}
	if sa.Name != expectedSAName {
		t.Errorf("ServiceAccount name mismatch: expected %s, got %s", expectedSAName, sa.Name)
	}
}

// TestController_ClusterRoleNaming verifies cluster-scoped resources use ClusterRoleName.
func TestController_ClusterRoleNaming(t *testing.T) {
	client := fake.NewSimpleClientset()
	store := newMockStore()
	cfg := newControllerConfig()

	ctrl := NewWithClient(cfg, store, client)

	project := newTestProjectWithStatus("rbac-test", projects.StatusPending)
	store.addProject(project)

	ctx := context.Background()
	if err := ctrl.handlePending(ctx, project); err != nil {
		t.Fatalf("handlePending failed: %v", err)
	}

	// Verify admin ClusterRole name uses env suffix
	expectedAdminName := cfg.ResourceConfig.ClusterRoleName(project.Name, "admin")
	adminRole, err := client.RbacV1().ClusterRoles().Get(ctx, expectedAdminName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Admin ClusterRole not found with expected name %s: %v", expectedAdminName, err)
	}
	if adminRole.Name != expectedAdminName {
		t.Errorf("Admin ClusterRole name mismatch: expected %s, got %s", expectedAdminName, adminRole.Name)
	}

	// Verify admin ClusterRoleBinding name
	adminBinding, err := client.RbacV1().ClusterRoleBindings().Get(ctx, expectedAdminName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Admin ClusterRoleBinding not found with expected name %s: %v", expectedAdminName, err)
	}
	if adminBinding.Name != expectedAdminName {
		t.Errorf("Admin ClusterRoleBinding name mismatch: expected %s, got %s", expectedAdminName, adminBinding.Name)
	}

	// Verify read ClusterRole name
	expectedReadName := cfg.ResourceConfig.ClusterRoleName(project.Name, "read")
	readRole, err := client.RbacV1().ClusterRoles().Get(ctx, expectedReadName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Read ClusterRole not found with expected name %s: %v", expectedReadName, err)
	}
	if readRole.Name != expectedReadName {
		t.Errorf("Read ClusterRole name mismatch: expected %s, got %s", expectedReadName, readRole.Name)
	}

	// Verify read ClusterRoleBinding name
	readBinding, err := client.RbacV1().ClusterRoleBindings().Get(ctx, expectedReadName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Read ClusterRoleBinding not found with expected name %s: %v", expectedReadName, err)
	}
	if readBinding.Name != expectedReadName {
		t.Errorf("Read ClusterRoleBinding name mismatch: expected %s, got %s", expectedReadName, readBinding.Name)
	}
}

// TestController_LabelSelectors verifies resources have correct labels for selection.
func TestController_LabelSelectors(t *testing.T) {
	client := fake.NewSimpleClientset()
	store := newMockStore()
	cfg := newControllerConfig()
	sharedNS := cfg.ResourceConfig.Namespace

	ctrl := NewWithClient(cfg, store, client)

	project := newTestProjectWithStatus("label-test", projects.StatusPending)
	store.addProject(project)

	ctx := context.Background()
	if err := ctrl.handlePending(ctx, project); err != nil {
		t.Fatalf("handlePending failed: %v", err)
	}

	// Get the deployment
	resourceName := cfg.ResourceConfig.ResourceName(project.Name)
	deploy, err := client.AppsV1().Deployments(sharedNS).Get(ctx, resourceName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get deployment: %v", err)
	}

	// Verify deployment has correct labels
	expectedLabels := map[string]string{
		labelApp:      "kubetty",
		labelInstance: project.Name,
	}

	for key, expected := range expectedLabels {
		if got := deploy.Labels[key]; got != expected {
			t.Errorf("Deployment label %s: expected %s, got %s", key, expected, got)
		}
	}

	// Verify pod template has matching labels
	podLabels := deploy.Spec.Template.Labels
	for key, expected := range expectedLabels {
		if got := podLabels[key]; got != expected {
			t.Errorf("Pod template label %s: expected %s, got %s", key, expected, got)
		}
	}

	// Verify selector matches pod labels
	selector := deploy.Spec.Selector.MatchLabels
	for key, expected := range expectedLabels {
		if got := selector[key]; got != expected {
			t.Errorf("Deployment selector %s: expected %s, got %s", key, expected, got)
		}
	}

	// Verify service selector
	svc, err := client.CoreV1().Services(sharedNS).Get(ctx, resourceName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get service: %v", err)
	}
	for key, expected := range expectedLabels {
		if got := svc.Spec.Selector[key]; got != expected {
			t.Errorf("Service selector %s: expected %s, got %s", key, expected, got)
		}
	}
}

// TestController_HandleDeletingCleansUpClusterRoles verifies cluster-scoped resources are deleted.
func TestController_HandleDeletingCleansUpClusterRoles(t *testing.T) {
	client := fake.NewSimpleClientset()
	store := newMockStore()
	cfg := newControllerConfig()

	ctrl := NewWithClient(cfg, store, client)

	project := newTestProjectWithStatus("cleanup-test", projects.StatusPending)
	store.addProject(project)

	ctx := context.Background()

	// First create resources
	if err := ctrl.handlePending(ctx, project); err != nil {
		t.Fatalf("handlePending failed: %v", err)
	}

	// Verify resources exist before deletion
	expectedAdminName := cfg.ResourceConfig.ClusterRoleName(project.Name, "admin")
	expectedReadName := cfg.ResourceConfig.ClusterRoleName(project.Name, "read")

	_, err := client.RbacV1().ClusterRoles().Get(ctx, expectedAdminName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Admin ClusterRole should exist before deletion: %v", err)
	}
	_, err = client.RbacV1().ClusterRoles().Get(ctx, expectedReadName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Read ClusterRole should exist before deletion: %v", err)
	}

	// Now delete
	project.Status = projects.StatusDeleting
	if err := ctrl.handleDeleting(ctx, project); err != nil {
		t.Fatalf("handleDeleting failed: %v", err)
	}

	// Verify ClusterRoles are deleted
	_, err = client.RbacV1().ClusterRoles().Get(ctx, expectedAdminName, metav1.GetOptions{})
	if err == nil {
		t.Errorf("Admin ClusterRole should be deleted but still exists")
	}

	_, err = client.RbacV1().ClusterRoles().Get(ctx, expectedReadName, metav1.GetOptions{})
	if err == nil {
		t.Errorf("Read ClusterRole should be deleted but still exists")
	}

	// Verify ClusterRoleBindings are deleted
	_, err = client.RbacV1().ClusterRoleBindings().Get(ctx, expectedAdminName, metav1.GetOptions{})
	if err == nil {
		t.Errorf("Admin ClusterRoleBinding should be deleted but still exists")
	}

	_, err = client.RbacV1().ClusterRoleBindings().Get(ctx, expectedReadName, metav1.GetOptions{})
	if err == nil {
		t.Errorf("Read ClusterRoleBinding should be deleted but still exists")
	}
}

// TestController_MultipleProjectsInSharedNamespace verifies multiple projects can coexist.
func TestController_MultipleProjectsInSharedNamespace(t *testing.T) {
	client := fake.NewSimpleClientset()
	store := newMockStore()
	cfg := newControllerConfig()
	sharedNS := cfg.ResourceConfig.Namespace

	ctrl := NewWithClient(cfg, store, client)

	// Create three projects
	projectNames := []string{"project-alpha", "project-beta", "project-gamma"}
	for _, name := range projectNames {
		project := newTestProjectWithStatus(name, projects.StatusPending)
		store.addProject(project)

		ctx := context.Background()
		if err := ctrl.handlePending(ctx, project); err != nil {
			t.Fatalf("handlePending failed for %s: %v", name, err)
		}
	}

	ctx := context.Background()

	// Verify all deployments exist in shared namespace
	deploys, err := client.AppsV1().Deployments(sharedNS).List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("Failed to list deployments: %v", err)
	}
	if len(deploys.Items) != 3 {
		t.Errorf("Expected 3 deployments in shared namespace, got %d", len(deploys.Items))
	}

	// Verify all services exist
	svcs, err := client.CoreV1().Services(sharedNS).List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("Failed to list services: %v", err)
	}
	if len(svcs.Items) != 3 {
		t.Errorf("Expected 3 services in shared namespace, got %d", len(svcs.Items))
	}

	// Verify each deployment can be found by instance label
	for _, name := range projectNames {
		selector := fmt.Sprintf("%s=%s,%s=%s", labelApp, "kubetty", labelInstance, name)
		deploys, err := client.AppsV1().Deployments(sharedNS).List(ctx, metav1.ListOptions{
			LabelSelector: selector,
		})
		if err != nil {
			t.Fatalf("Failed to list deployments for %s: %v", name, err)
		}
		if len(deploys.Items) != 1 {
			t.Errorf("Expected 1 deployment for project %s, got %d", name, len(deploys.Items))
		}
	}
}

// TestController_StatusTransitions verifies correct status updates during lifecycle.
func TestController_StatusTransitions(t *testing.T) {
	client := fake.NewSimpleClientset()
	store := newMockStore()
	cfg := newControllerConfig()

	ctrl := NewWithClient(cfg, store, client)

	project := newTestProjectWithStatus("status-test", projects.StatusPending)
	store.addProject(project)

	ctx := context.Background()

	// handlePending should transition to StatusCreating
	if err := ctrl.handlePending(ctx, project); err != nil {
		t.Fatalf("handlePending failed: %v", err)
	}

	statuses := store.getStatuses()
	if len(statuses) == 0 {
		t.Fatal("Expected at least one status update")
	}

	// First status update should be to StatusCreating
	if statuses[0].status != projects.StatusCreating {
		t.Errorf("Expected first status to be %s, got %s", projects.StatusCreating, statuses[0].status)
	}
}

// TestNewWithClient verifies the constructor works correctly.
func TestNewWithClient(t *testing.T) {
	client := fake.NewSimpleClientset()
	store := newMockStore()
	cfg := newControllerConfig()

	ctrl := NewWithClient(cfg, store, client)
	if ctrl == nil {
		t.Fatal("NewWithClient returned nil")
	}
	if ctrl.cfg.ResourceConfig.Namespace != cfg.ResourceConfig.Namespace {
		t.Errorf("Config not set correctly: expected namespace %s, got %s",
			cfg.ResourceConfig.Namespace, ctrl.cfg.ResourceConfig.Namespace)
	}
}

// Ensure mockStore implements projects.Store interface at compile time
var _ projects.Store = (*mockStore)(nil)

// TestDefaultConfig verifies default configuration values.
func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.ReconcileInterval != 30*time.Second {
		t.Errorf("ReconcileInterval = %v, want 30s", cfg.ReconcileInterval)
	}
	if cfg.HealthCheckInterval != 60*time.Second {
		t.Errorf("HealthCheckInterval = %v, want 60s", cfg.HealthCheckInterval)
	}
	if cfg.EnvSecretName != "env-secrets" {
		t.Errorf("EnvSecretName = %q, want %q", cfg.EnvSecretName, "env-secrets")
	}
	if cfg.ResourceConfig.Namespace != "kubetty-projects" {
		t.Errorf("ResourceConfig.Namespace = %q, want %q", cfg.ResourceConfig.Namespace, "kubetty-projects")
	}
	if cfg.ResourceConfig.Prefix != "kubetty-project-" {
		t.Errorf("ResourceConfig.Prefix = %q, want %q", cfg.ResourceConfig.Prefix, "kubetty-project-")
	}
	if cfg.ResourceConfig.Env != "dev" {
		t.Errorf("ResourceConfig.Env = %q, want %q", cfg.ResourceConfig.Env, "dev")
	}
}

// TestSetStatusCallback verifies the status callback setter.
func TestSetStatusCallback(t *testing.T) {
	client := fake.NewSimpleClientset()
	store := newMockStore()
	cfg := newControllerConfig()

	ctrl := NewWithClient(cfg, store, client)

	ctrl.SetStatusCallback(func(project *projects.Project, newStatus projects.ProjectStatus) {
		// Callback set - verifying it's not nil below
	})

	if ctrl.statusCallback == nil {
		t.Error("statusCallback should not be nil after SetStatusCallback")
	}
}

// TestController_HandleCreating_DeploymentReady verifies transition to running when deployment is ready.
func TestController_HandleCreating_DeploymentReady(t *testing.T) {
	store := newMockStore()
	cfg := newControllerConfig()

	project := newTestProjectWithStatus("creating-ready-test", projects.StatusCreating)
	store.addProject(project)

	// Create fake clientset with a ready deployment
	client := fake.NewSimpleClientset()
	ctrl := NewWithClient(cfg, store, client)

	// First create the project resources
	ctx := context.Background()
	if err := ctrl.handlePending(ctx, project); err != nil {
		t.Fatalf("handlePending failed: %v", err)
	}

	// Get the created deployment and update its status to ready
	resourceName := cfg.ResourceConfig.ResourceName(project.Name)
	deploy, err := client.AppsV1().Deployments(cfg.ResourceConfig.Namespace).Get(ctx, resourceName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get deployment: %v", err)
	}

	// Update deployment status to ready
	deploy.Status.ReadyReplicas = 1
	deploy.Status.AvailableReplicas = 1
	deploy.Status.Replicas = 1
	_, err = client.AppsV1().Deployments(cfg.ResourceConfig.Namespace).UpdateStatus(ctx, deploy, metav1.UpdateOptions{})
	if err != nil {
		t.Fatalf("Failed to update deployment status: %v", err)
	}

	// Reset status tracking
	store.statuses = []statusUpdate{}

	// Call handleCreating
	if err := ctrl.handleCreating(ctx, project); err != nil {
		t.Fatalf("handleCreating failed: %v", err)
	}

	// Verify transition to running status
	statuses := store.getStatuses()
	found := false
	for _, s := range statuses {
		if s.status == projects.StatusRunning {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected project to transition to StatusRunning")
	}
}

// TestController_HandleCreating_DeploymentNotReady verifies waiting state when deployment not ready.
func TestController_HandleCreating_DeploymentNotReady(t *testing.T) {
	store := newMockStore()
	cfg := newControllerConfig()

	project := newTestProjectWithStatus("creating-waiting-test", projects.StatusCreating)
	store.addProject(project)

	client := fake.NewSimpleClientset()
	ctrl := NewWithClient(cfg, store, client)

	// Create the project resources
	ctx := context.Background()
	if err := ctrl.handlePending(ctx, project); err != nil {
		t.Fatalf("handlePending failed: %v", err)
	}

	// Deployment status remains 0 replicas (default) - not ready
	// Reset status tracking
	store.statuses = []statusUpdate{}

	// Call handleCreating - should not error, should not transition
	if err := ctrl.handleCreating(ctx, project); err != nil {
		t.Fatalf("handleCreating failed: %v", err)
	}

	// Verify no transition (should stay in creating)
	statuses := store.getStatuses()
	for _, s := range statuses {
		if s.status == projects.StatusRunning {
			t.Error("Should not transition to running when deployment not ready")
		}
	}
}

// TestController_HandleCreating_DeploymentNotFound verifies fallback to pending when deployment missing.
func TestController_HandleCreating_DeploymentNotFound(t *testing.T) {
	store := newMockStore()
	cfg := newControllerConfig()

	project := newTestProjectWithStatus("creating-missing-test", projects.StatusCreating)
	store.addProject(project)

	// Create client WITHOUT creating the deployment
	client := fake.NewSimpleClientset()
	ctrl := NewWithClient(cfg, store, client)

	ctx := context.Background()

	// Call handleCreating when deployment doesn't exist
	if err := ctrl.handleCreating(ctx, project); err != nil {
		t.Fatalf("handleCreating failed: %v", err)
	}

	// Verify transition back to pending
	statuses := store.getStatuses()
	found := false
	for _, s := range statuses {
		if s.status == projects.StatusPending {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected project to transition back to StatusPending when deployment not found")
	}
}

// TestController_HandleUpdating_Success verifies deployment update.
func TestController_HandleUpdating_Success(t *testing.T) {
	store := newMockStore()
	cfg := newControllerConfig()

	project := newTestProjectWithStatus("update-test", projects.StatusUpdating)
	store.addProject(project)

	client := fake.NewSimpleClientset()
	ctrl := NewWithClient(cfg, store, client)

	// First create the project resources
	ctx := context.Background()
	project.Status = projects.StatusPending
	if err := ctrl.handlePending(ctx, project); err != nil {
		t.Fatalf("handlePending failed: %v", err)
	}

	// Now update it
	project.Status = projects.StatusUpdating
	store.statuses = []statusUpdate{}

	if err := ctrl.handleUpdating(ctx, project); err != nil {
		t.Fatalf("handleUpdating failed: %v", err)
	}

	// Verify transition to creating (waiting for rollout)
	statuses := store.getStatuses()
	found := false
	for _, s := range statuses {
		if s.status == projects.StatusCreating {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected project to transition to StatusCreating after update")
	}
}

// TestController_GetDeploymentStatus verifies deployment status retrieval.
func TestController_GetDeploymentStatus(t *testing.T) {
	store := newMockStore()
	cfg := newControllerConfig()

	project := newTestProjectWithStatus("status-get-test", projects.StatusRunning)
	store.addProject(project)

	client := fake.NewSimpleClientset()
	ctrl := NewWithClient(cfg, store, client)

	// First create the resources
	ctx := context.Background()
	project.Status = projects.StatusPending
	if err := ctrl.handlePending(ctx, project); err != nil {
		t.Fatalf("handlePending failed: %v", err)
	}

	// Get deployment status
	status, err := ctrl.GetDeploymentStatus(ctx, project)
	if err != nil {
		t.Fatalf("GetDeploymentStatus failed: %v", err)
	}

	if !status.Exists {
		t.Error("Expected deployment to exist")
	}
}

// TestController_GetDeploymentStatus_NotFound verifies behavior when deployment doesn't exist.
func TestController_GetDeploymentStatus_NotFound(t *testing.T) {
	store := newMockStore()
	cfg := newControllerConfig()

	project := newTestProjectWithStatus("status-notfound-test", projects.StatusRunning)
	store.addProject(project)

	client := fake.NewSimpleClientset()
	ctrl := NewWithClient(cfg, store, client)

	ctx := context.Background()

	// Get status when deployment doesn't exist
	status, err := ctrl.GetDeploymentStatus(ctx, project)
	if err != nil {
		t.Fatalf("GetDeploymentStatus failed: %v", err)
	}

	if status.Exists {
		t.Error("Expected deployment to not exist")
	}
}

// TestController_GetProjectSecrets verifies secrets retrieval.
func TestController_GetProjectSecrets(t *testing.T) {
	store := newMockStore()
	cfg := newControllerConfig()

	project := newTestProjectWithStatus("secrets-get-test", projects.StatusRunning)
	store.addProject(project)

	client := fake.NewSimpleClientset()
	ctrl := NewWithClient(cfg, store, client)

	// First create the resources (which includes the empty env secret)
	ctx := context.Background()
	project.Status = projects.StatusPending
	if err := ctrl.handlePending(ctx, project); err != nil {
		t.Fatalf("handlePending failed: %v", err)
	}

	// Get secrets (should be empty initially)
	secrets, err := ctrl.GetProjectSecrets(ctx, project)
	if err != nil {
		t.Fatalf("GetProjectSecrets failed: %v", err)
	}

	// Should be empty map since no secrets were added
	if len(secrets) != 0 {
		t.Errorf("Expected empty secrets map, got %d entries", len(secrets))
	}
}

// TestController_GetProjectSecrets_NotFound verifies behavior when secret doesn't exist.
func TestController_GetProjectSecrets_NotFound(t *testing.T) {
	store := newMockStore()
	cfg := newControllerConfig()

	project := newTestProjectWithStatus("secrets-notfound-test", projects.StatusRunning)
	store.addProject(project)

	client := fake.NewSimpleClientset()
	ctrl := NewWithClient(cfg, store, client)

	ctx := context.Background()

	// Get secrets when secret doesn't exist
	secrets, err := ctrl.GetProjectSecrets(ctx, project)
	if err != nil {
		t.Fatalf("GetProjectSecrets should not error for missing secret, got: %v", err)
	}

	// Should return empty map
	if len(secrets) != 0 {
		t.Errorf("Expected empty secrets map for missing secret, got %d entries", len(secrets))
	}
}

// TestController_RestartProject verifies deployment restart.
func TestController_RestartProject(t *testing.T) {
	store := newMockStore()
	cfg := newControllerConfig()

	project := newTestProjectWithStatus("restart-test", projects.StatusRunning)
	store.addProject(project)

	client := fake.NewSimpleClientset()
	ctrl := NewWithClient(cfg, store, client)

	// First create the resources
	ctx := context.Background()
	project.Status = projects.StatusPending
	if err := ctrl.handlePending(ctx, project); err != nil {
		t.Fatalf("handlePending failed: %v", err)
	}

	// Restart the project
	store.statuses = []statusUpdate{}
	if err := ctrl.RestartProject(ctx, project); err != nil {
		t.Fatalf("RestartProject failed: %v", err)
	}

	// Verify transition to creating (waiting for restart)
	statuses := store.getStatuses()
	found := false
	for _, s := range statuses {
		if s.status == projects.StatusCreating && s.message == "Restarting deployment" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected project to transition to StatusCreating with restart message")
	}
}

// TestController_RestartProject_NotFound verifies error when deployment doesn't exist.
func TestController_RestartProject_NotFound(t *testing.T) {
	store := newMockStore()
	cfg := newControllerConfig()

	project := newTestProjectWithStatus("restart-notfound-test", projects.StatusRunning)
	store.addProject(project)

	client := fake.NewSimpleClientset()
	ctrl := NewWithClient(cfg, store, client)

	ctx := context.Background()

	// Restart when deployment doesn't exist
	err := ctrl.RestartProject(ctx, project)
	if err == nil {
		t.Error("Expected error when restarting non-existent deployment")
	}
}

// TestDeploymentStatus_Fields verifies DeploymentStatus struct.
func TestDeploymentStatus_Fields(t *testing.T) {
	status := DeploymentStatus{
		Exists:            true,
		Replicas:          3,
		ReadyReplicas:     2,
		AvailableReplicas: 2,
		Pods: []PodStatus{
			{Name: "pod-1", Phase: "Running", PodIP: "10.0.0.1", Ready: true},
			{Name: "pod-2", Phase: "Pending", PodIP: "", Ready: false, Reason: "ImagePullBackOff"},
		},
	}

	if !status.Exists {
		t.Error("Exists should be true")
	}
	if status.Replicas != 3 {
		t.Errorf("Replicas = %d, want 3", status.Replicas)
	}
	if status.ReadyReplicas != 2 {
		t.Errorf("ReadyReplicas = %d, want 2", status.ReadyReplicas)
	}
	if len(status.Pods) != 2 {
		t.Errorf("Pods count = %d, want 2", len(status.Pods))
	}
	if status.Pods[0].Name != "pod-1" {
		t.Errorf("Pod 0 name = %q, want %q", status.Pods[0].Name, "pod-1")
	}
	if status.Pods[1].Reason != "ImagePullBackOff" {
		t.Errorf("Pod 1 reason = %q, want %q", status.Pods[1].Reason, "ImagePullBackOff")
	}
}

// TestPodStatus_Fields verifies PodStatus struct.
func TestPodStatus_Fields(t *testing.T) {
	ps := PodStatus{
		Name:   "test-pod",
		Phase:  "Running",
		PodIP:  "10.0.0.5",
		Ready:  true,
		Reason: "",
	}

	if ps.Name != "test-pod" {
		t.Errorf("Name = %q, want %q", ps.Name, "test-pod")
	}
	if ps.Phase != "Running" {
		t.Errorf("Phase = %q, want %q", ps.Phase, "Running")
	}
	if ps.PodIP != "10.0.0.5" {
		t.Errorf("PodIP = %q, want %q", ps.PodIP, "10.0.0.5")
	}
	if !ps.Ready {
		t.Error("Ready should be true")
	}
}

// TestController_HandleCreating_StatusCallback verifies callback is called on status change.
func TestController_HandleCreating_StatusCallback(t *testing.T) {
	store := newMockStore()
	cfg := newControllerConfig()

	project := newTestProjectWithStatus("callback-test", projects.StatusCreating)
	store.addProject(project)

	client := fake.NewSimpleClientset()
	ctrl := NewWithClient(cfg, store, client)

	// Set up callback
	var callbackProject *projects.Project
	var callbackStatus projects.ProjectStatus
	ctrl.SetStatusCallback(func(p *projects.Project, status projects.ProjectStatus) {
		callbackProject = p
		callbackStatus = status
	})

	// First create the project resources
	ctx := context.Background()
	project.Status = projects.StatusPending
	if err := ctrl.handlePending(ctx, project); err != nil {
		t.Fatalf("handlePending failed: %v", err)
	}

	// Update deployment to ready state
	resourceName := cfg.ResourceConfig.ResourceName(project.Name)
	deploy, err := client.AppsV1().Deployments(cfg.ResourceConfig.Namespace).Get(ctx, resourceName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get deployment: %v", err)
	}
	deploy.Status.ReadyReplicas = 1
	deploy.Status.AvailableReplicas = 1
	deploy.Status.Replicas = 1
	client.AppsV1().Deployments(cfg.ResourceConfig.Namespace).UpdateStatus(ctx, deploy, metav1.UpdateOptions{})

	// Call handleCreating
	project.Status = projects.StatusCreating
	if err := ctrl.handleCreating(ctx, project); err != nil {
		t.Fatalf("handleCreating failed: %v", err)
	}

	// Verify callback was called
	if callbackProject == nil {
		t.Error("Callback should have been called")
	}
	if callbackStatus != projects.StatusRunning {
		t.Errorf("Callback status = %s, want %s", callbackStatus, projects.StatusRunning)
	}
}
