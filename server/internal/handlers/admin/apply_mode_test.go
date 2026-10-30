package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/supporttools/KubeTTY/server/internal/controller"
	"github.com/supporttools/KubeTTY/server/internal/projects"
	"k8s.io/client-go/kubernetes/fake"
)

// cluster-services-1024 part (c): PUT /api/admin/projects/{id} with
// applyMode "next_restart" saves to the DB without status=updating, so no pod
// is rolled.

func runningProject() *projects.Project {
	return &projects.Project{
		ID:              uuid.New(),
		Name:            "deferred",
		DisplayName:     "Deferred",
		SessionID:       uuid.New(),
		UserName:        "tester",
		CPURequest:      "100m",
		CPULimit:        "500m",
		MemoryRequest:   "128Mi",
		MemoryLimit:     "512Mi",
		StorageSize:     "10Gi",
		StorageClass:    "standard",
		ImageRepository: "harbor.support.tools/kubetty/kubetty",
		ImageTag:        "v1.0.0",
		Status:          projects.StatusRunning,
	}
}

func putProject(t *testing.T, h *ProjectHandlers, id uuid.UUID, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPut, "/api/admin/projects/"+id.String(), bytes.NewReader(b))
	req.SetPathValue("id", id.String())
	rec := httptest.NewRecorder()
	h.UpdateProject(rec, req)
	return rec
}

func TestUpdateProject_DefaultModeStillRollsPod(t *testing.T) {
	store := newMockProjectStore()
	p := runningProject()
	store.projects[p.ID] = p
	h := NewProjectHandlers(store, nil, "v1.0.0")

	rec := putProject(t, h, p.ID, map[string]any{"cpuLimit": "2"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	if store.calledStatus != projects.StatusUpdating {
		t.Errorf("status set = %q, want %q (default mode applies now)", store.calledStatus, projects.StatusUpdating)
	}
}

func TestUpdateProject_ExplicitNowRollsPod(t *testing.T) {
	store := newMockProjectStore()
	p := runningProject()
	store.projects[p.ID] = p
	h := NewProjectHandlers(store, nil, "v1.0.0")

	rec := putProject(t, h, p.ID, map[string]any{"cpuLimit": "2", "applyMode": "now"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	if store.calledStatus != projects.StatusUpdating {
		t.Errorf("status set = %q, want %q", store.calledStatus, projects.StatusUpdating)
	}
}

func TestUpdateProject_NextRestartSavesWithoutUpdating(t *testing.T) {
	store := newMockProjectStore()
	p := runningProject()
	store.projects[p.ID] = p
	h := NewProjectHandlers(store, nil, "v1.0.0")

	rec := putProject(t, h, p.ID, map[string]any{
		"cpuLimit":    "2",
		"memoryLimit": "4Gi",
		"imageTag":    "v2.0.0",
		"applyMode":   "next_restart",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	if store.updateCalls != 1 {
		t.Errorf("store.Update calls = %d, want 1 (the change is saved)", store.updateCalls)
	}
	if store.calledStatus != "" {
		t.Errorf("SetStatus called with %q; next_restart must not set a status", store.calledStatus)
	}
	if p.Status != projects.StatusRunning {
		t.Errorf("project status = %q, want running", p.Status)
	}
	if p.CPULimit != "2" || p.MemoryLimit != "4Gi" || p.ImageTag != "v2.0.0" {
		t.Errorf("DB row not updated: cpu=%s mem=%s image=%s", p.CPULimit, p.MemoryLimit, p.ImageTag)
	}
}

func TestUpdateProject_InvalidApplyMode(t *testing.T) {
	store := newMockProjectStore()
	p := runningProject()
	store.projects[p.ID] = p
	h := NewProjectHandlers(store, nil, "v1.0.0")

	rec := putProject(t, h, p.ID, map[string]any{"cpuLimit": "2", "applyMode": "later"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if store.updateCalls != 0 || store.calledStatus != "" {
		t.Errorf("invalid mode must change nothing: updates=%d status=%q", store.updateCalls, store.calledStatus)
	}
}

func TestUpdateProject_NextRestartRefusesStorageResize(t *testing.T) {
	store := newMockProjectStore()
	p := runningProject()
	store.projects[p.ID] = p
	h := NewProjectHandlers(store, nil, "v1.0.0")

	rec := putProject(t, h, p.ID, map[string]any{"storageSize": "20Gi", "applyMode": "next_restart"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (a resize cannot wait for a restart)", rec.Code)
	}
	if store.updateCalls != 0 {
		t.Errorf("store.Update called %d times; a refused request must save nothing", store.updateCalls)
	}
}

// The admin UI always sends the current storageSize with a resource edit; an
// unchanged value must not block a deferred save.
func TestUpdateProject_NextRestartAllowsUnchangedStorage(t *testing.T) {
	store := newMockProjectStore()
	p := runningProject()
	store.projects[p.ID] = p
	h := NewProjectHandlers(store, nil, "v1.0.0")

	rec := putProject(t, h, p.ID, map[string]any{
		"cpuLimit":    "2",
		"storageSize": p.StorageSize,
		"applyMode":   "next_restart",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	if store.calledStatus != "" {
		t.Errorf("SetStatus called with %q; next_restart must not set a status", store.calledStatus)
	}
}

// With a controller, a deferred save reports the new pending changes in the
// response and never writes to the Deployment.
func TestUpdateProject_NextRestartReportsPendingChangesWithoutRollout(t *testing.T) {
	store := newMockProjectStore()
	p := runningProject()
	store.projects[p.ID] = p

	cfg := controller.DefaultConfig()
	running := *p
	client := fake.NewSimpleClientset(controller.BuildDeployment(&running, cfg.ResourceConfig, cfg.ResourceConfig.EnvSecretName(p.Name)))
	ctrl := controller.NewWithClient(cfg, store, client)
	h := NewProjectHandlers(store, ctrl, "v1.0.0")

	rec := putProject(t, h, p.ID, map[string]any{"cpuLimit": "2", "applyMode": "next_restart"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}

	var got projects.Project
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got.PendingChanges) != 1 || got.PendingChanges[0].Field != "cpuLimit" ||
		got.PendingChanges[0].Running != "500m" || got.PendingChanges[0].Desired != "2" {
		t.Errorf("pendingChanges = %+v, want one cpuLimit change 500m -> 2", got.PendingChanges)
	}
	if store.calledStatus != "" {
		t.Errorf("SetStatus called with %q; next_restart must not set a status", store.calledStatus)
	}
	for _, a := range client.Actions() {
		if a.GetVerb() != "get" {
			t.Errorf("clientset saw %s %s; a deferred save must not touch the Deployment", a.GetVerb(), a.GetResource().Resource)
		}
	}
}
