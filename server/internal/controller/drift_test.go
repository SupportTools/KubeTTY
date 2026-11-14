package controller

import (
	"context"
	"testing"

	"github.com/supporttools/KubeTTY/server/internal/projects"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// cluster-services-1024 part (b): the drift check reports DB-vs-Deployment
// differences and never rolls a pod.

func driftProject(name string) *projects.Project {
	p := newTestProjectWithStatus(name, projects.StatusRunning)
	p.ImageRepository = "harbor.support.tools/kubetty/kubetty"
	p.ImageTag = "v1.0.0"
	return p
}

func deploymentFor(p *projects.Project, cfg ResourceConfig) *appsv1.Deployment {
	cp := *p
	return BuildDeployment(&cp, cfg, cfg.EnvSecretName(p.Name))
}

func assertReadOnly(t *testing.T, client *fake.Clientset) {
	t.Helper()
	for _, a := range client.Actions() {
		switch a.GetVerb() {
		case "get", "list", "watch":
		default:
			t.Errorf("drift check issued %s on %s; it must never modify a Deployment or pod",
				a.GetVerb(), a.GetResource().Resource)
		}
	}
}

func TestComputeDrift_NoDriftWhenBuiltFromSameRow(t *testing.T) {
	cfg := newControllerConfig().ResourceConfig
	p := driftProject("in-sync")
	changes, err := ComputeDrift(p, deploymentFor(p, cfg), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 {
		t.Errorf("changes = %+v, want none", changes)
	}
}

func TestComputeDrift_QuantitiesComparedByValue(t *testing.T) {
	cfg := newControllerConfig().ResourceConfig
	p := driftProject("same-value")
	// DB says "1000m"; the Deployment carries the same amount spelled "1".
	p.CPULimit = "1000m"
	running := *p
	running.CPULimit = "1"
	d := deploymentFor(&running, cfg)
	changes, err := ComputeDrift(p, d, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 {
		t.Errorf("changes = %+v, want none for 1 vs 1000m", changes)
	}
}

func TestComputeDrift_ReportsEachTrackedField(t *testing.T) {
	cfg := newControllerConfig().ResourceConfig
	p := driftProject("drifted")
	old := *p
	old.CPURequest = "50m"
	old.CPULimit = "250m"
	old.MemoryRequest = "64Mi"
	old.MemoryLimit = "256Mi"
	old.ImageTag = "v0.9.0"

	changes, err := ComputeDrift(p, deploymentFor(&old, cfg), cfg)
	if err != nil {
		t.Fatal(err)
	}
	want := []projects.PendingChange{
		{Field: "cpuRequest", Running: "50m", Desired: "100m"},
		{Field: "cpuLimit", Running: "250m", Desired: "500m"},
		{Field: "memoryRequest", Running: "64Mi", Desired: "128Mi"},
		{Field: "memoryLimit", Running: "256Mi", Desired: "512Mi"},
		{Field: "image", Running: "harbor.support.tools/kubetty/kubetty:v0.9.0", Desired: "harbor.support.tools/kubetty/kubetty:v1.0.0"},
	}
	if len(changes) != len(want) {
		t.Fatalf("changes = %+v, want %+v", changes, want)
	}
	for i := range want {
		if changes[i] != want[i] {
			t.Errorf("change[%d] = %+v, want %+v", i, changes[i], want[i])
		}
	}
}

// Fields the drift check does not track (here a hand-set nodeName) are not
// reported; they are still replaced by the next restart's rebuild.
func TestComputeDrift_IgnoresUntrackedFields(t *testing.T) {
	cfg := newControllerConfig().ResourceConfig
	p := driftProject("pinned")
	d := deploymentFor(p, cfg)
	d.Spec.Template.Spec.NodeName = "some-node"
	changes, err := ComputeDrift(p, d, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 {
		t.Errorf("changes = %+v, want none", changes)
	}
}

func TestComputeDrift_InvalidDBQuantityIsAnError(t *testing.T) {
	cfg := newControllerConfig().ResourceConfig
	p := driftProject("bad-row")
	d := deploymentFor(p, cfg)
	p.CPULimit = "lots"
	if _, err := ComputeDrift(p, d, cfg); err == nil {
		t.Error("want an error for an unparseable DB quantity, not a panic or silent pass")
	}
}

func TestCheckProjectDrift_RecordsPendingChangesReadOnly(t *testing.T) {
	cfg := newControllerConfig()
	store := newMockStore()
	p := driftProject("record-me")
	old := *p
	old.CPULimit = "250m"
	store.addProject(p)

	client := fake.NewSimpleClientset(deploymentFor(&old, cfg.ResourceConfig))
	ctrl := NewWithClient(cfg, store, client)

	changes, err := ctrl.CheckProjectDrift(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Field != "cpuLimit" {
		t.Fatalf("changes = %+v, want one cpuLimit change", changes)
	}
	stored, _ := store.Get(context.Background(), p.ID)
	if len(stored.PendingChanges) != 1 || stored.DriftCheckedAt == nil {
		t.Errorf("stored pending=%+v checkedAt=%v, want the change and a check time", stored.PendingChanges, stored.DriftCheckedAt)
	}
	if len(store.statuses) != 0 {
		t.Errorf("drift check set status %+v; it must not change project status", store.statuses)
	}
	assertReadOnly(t, client)
}

func TestCheckProjectDrift_ClearsWhenBackInSync(t *testing.T) {
	cfg := newControllerConfig()
	store := newMockStore()
	p := driftProject("cleared")
	p.PendingChanges = []projects.PendingChange{{Field: "cpuLimit", Running: "1", Desired: "2"}}
	store.addProject(p)

	client := fake.NewSimpleClientset(deploymentFor(p, cfg.ResourceConfig))
	ctrl := NewWithClient(cfg, store, client)

	if _, err := ctrl.CheckProjectDrift(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	stored, _ := store.Get(context.Background(), p.ID)
	if len(stored.PendingChanges) != 0 {
		t.Errorf("pending = %+v, want cleared", stored.PendingChanges)
	}
	assertReadOnly(t, client)
}

func TestCheckProjectDrift_MissingDeploymentIsNotDrift(t *testing.T) {
	cfg := newControllerConfig()
	store := newMockStore()
	p := driftProject("no-deploy")
	store.addProject(p)

	client := fake.NewSimpleClientset()
	ctrl := NewWithClient(cfg, store, client)

	changes, err := ctrl.CheckProjectDrift(context.Background(), p)
	if err != nil || changes != nil {
		t.Errorf("changes=%+v err=%v, want nil, nil", changes, err)
	}
	assertReadOnly(t, client)
}

func TestCheckAllDrift_OnlyRunningProjectsAndNoWrites(t *testing.T) {
	cfg := newControllerConfig()
	store := newMockStore()

	running := driftProject("running-one")
	oldRunning := *running
	oldRunning.ImageTag = "v0.1.0"
	store.addProject(running)

	failed := driftProject("failed-one")
	failed.Status = projects.StatusFailed
	oldFailed := *failed
	oldFailed.ImageTag = "v0.1.0"
	store.addProject(failed)

	client := fake.NewSimpleClientset(
		deploymentFor(&oldRunning, cfg.ResourceConfig),
		deploymentFor(&oldFailed, cfg.ResourceConfig),
	)
	ctrl := NewWithClient(cfg, store, client)

	ctrl.checkAllDrift(context.Background())

	gotRunning, _ := store.Get(context.Background(), running.ID)
	if len(gotRunning.PendingChanges) != 1 || gotRunning.PendingChanges[0].Field != "image" {
		t.Errorf("running project pending = %+v, want one image change", gotRunning.PendingChanges)
	}
	gotFailed, _ := store.Get(context.Background(), failed.ID)
	if gotFailed.DriftCheckedAt != nil {
		t.Error("drift check ran on a failed project; it only checks running ones")
	}
	assertReadOnly(t, client)
}

func TestRestartProject_ClearsPendingChanges(t *testing.T) {
	cfg := newControllerConfig()
	store := newMockStore()
	p := driftProject("restart-clears")
	p.PendingChanges = []projects.PendingChange{{Field: "cpuLimit", Running: "250m", Desired: "500m"}}
	old := *p
	old.CPULimit = "250m"
	store.addProject(p)

	client := fake.NewSimpleClientset(deploymentFor(&old, cfg.ResourceConfig))
	ctrl := NewWithClient(cfg, store, client)

	if err := ctrl.RestartProject(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	stored, _ := store.Get(context.Background(), p.ID)
	if len(stored.PendingChanges) != 0 {
		t.Errorf("pending = %+v, want cleared after the rebuild", stored.PendingChanges)
	}
}

func TestDriftCheckInterval_Default(t *testing.T) {
	ctrl := NewWithClient(Config{}, newMockStore(), fake.NewSimpleClientset())
	if got := ctrl.driftCheckInterval(); got != DefaultDriftCheckInterval {
		t.Errorf("interval = %v, want %v", got, DefaultDriftCheckInterval)
	}
}
