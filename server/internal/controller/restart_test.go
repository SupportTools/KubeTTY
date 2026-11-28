package controller

import (
	"context"
	"strings"
	"testing"

	"github.com/supporttools/KubeTTY/server/internal/projects"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// staleDeployment returns the project's Deployment as it looked before a DB
// change: built from the project, then hand-modified to carry an old CPU limit,
// an old image and a manual nodeName pin.
func staleDeployment(p *projects.Project, cfg ResourceConfig, replicas int32) *appsv1.Deployment {
	old := *p
	old.CPULimit = "16000m"
	old.ImageTag = "v0.0.1"
	d := BuildDeployment(&old, cfg, cfg.EnvSecretName(p.Name))
	d.Spec.Replicas = &replicas
	d.Spec.Template.Spec.NodeName = "pinned-node"
	return d
}

func kubettyContainer(t *testing.T, d *appsv1.Deployment) corev1.Container {
	t.Helper()
	for _, c := range d.Spec.Template.Spec.Containers {
		if c.Name == "kubetty" {
			return c
		}
	}
	t.Fatal("kubetty container not found")
	return corev1.Container{}
}

// TestRestartProject_RebuildsDeploymentFromDB is the cluster-services-1008
// regression: a restart must apply the DB's resources, not reuse the stale
// pod template.
func TestRestartProject_RebuildsDeploymentFromDB(t *testing.T) {
	cfg := newControllerConfig()
	store := newMockStore()
	p := newTestProjectWithStatus("restart-me", projects.StatusRunning)
	p.CPULimit = "4000m"
	p.ImageRepository = "harbor.support.tools/kubetty/kubetty"
	p.ImageTag = "v9.9.9"
	store.addProject(p)

	client := fake.NewSimpleClientset(staleDeployment(p, cfg.ResourceConfig, 1))
	ctrl := NewWithClient(cfg, store, client)

	if err := ctrl.RestartProject(context.Background(), p); err != nil {
		t.Fatalf("RestartProject: %v", err)
	}

	got, err := client.AppsV1().Deployments(cfg.ResourceConfig.Namespace).Get(
		context.Background(), cfg.ResourceConfig.ResourceName(p.Name), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get deployment: %v", err)
	}

	kc := kubettyContainer(t, got)
	if lim := kc.Resources.Limits[corev1.ResourceCPU]; lim.Cmp(resource.MustParse("4000m")) != 0 {
		t.Errorf("cpu limit = %s, want 4000m from the DB", lim.String())
	}
	if want := "harbor.support.tools/kubetty/kubetty:v9.9.9"; kc.Image != want {
		t.Errorf("image = %q, want %q", kc.Image, want)
	}
	if got.Spec.Template.Spec.NodeName != "" {
		t.Errorf("nodeName = %q, want the rebuilt spec (no hand pin)", got.Spec.Template.Spec.NodeName)
	}
	if got.Spec.Replicas == nil || *got.Spec.Replicas != 1 {
		t.Errorf("replicas = %v, want 1 for an unpaused project", got.Spec.Replicas)
	}
	if got.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] == "" {
		t.Error("restartedAt annotation missing; an unchanged spec would not roll")
	}
	if got.Spec.Template.Annotations["prometheus.io/scrape"] != "true" {
		t.Error("rebuilt template lost the prometheus annotations")
	}

	statuses := store.getStatuses()
	if len(statuses) == 0 || statuses[len(statuses)-1].status != projects.StatusCreating {
		t.Errorf("last status = %+v, want creating", statuses)
	}
}

func TestRestartProject_PausedProjectStaysScaledDown(t *testing.T) {
	cfg := newControllerConfig()
	store := newMockStore()
	p := newTestProjectWithStatus("paused", projects.StatusRunning)
	p.Paused = true
	store.addProject(p)

	client := fake.NewSimpleClientset(staleDeployment(p, cfg.ResourceConfig, 0))
	ctrl := NewWithClient(cfg, store, client)

	if err := ctrl.RestartProject(context.Background(), p); err != nil {
		t.Fatalf("RestartProject: %v", err)
	}
	got, err := client.AppsV1().Deployments(cfg.ResourceConfig.Namespace).Get(
		context.Background(), cfg.ResourceConfig.ResourceName(p.Name), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get deployment: %v", err)
	}
	if got.Spec.Replicas == nil || *got.Spec.Replicas != 0 {
		t.Errorf("replicas = %v, want 0 for a paused project", got.Spec.Replicas)
	}
	if lim := kubettyContainer(t, got).Resources.Limits[corev1.ResourceCPU]; lim.Cmp(resource.MustParse(p.CPULimit)) != 0 {
		t.Errorf("cpu limit = %s, want %s", lim.String(), p.CPULimit)
	}
}

func TestRestartProject_InvalidQuantityErrorsWithoutTouchingDeployment(t *testing.T) {
	cfg := newControllerConfig()
	store := newMockStore()
	p := newTestProjectWithStatus("bad-qty", projects.StatusRunning)
	store.addProject(p)

	stale := staleDeployment(p, cfg.ResourceConfig, 1)
	client := fake.NewSimpleClientset(stale)
	ctrl := NewWithClient(cfg, store, client)

	bad := *p
	bad.CPULimit = "four cores"
	err := ctrl.RestartProject(context.Background(), &bad)
	if err == nil || !strings.Contains(err.Error(), "cpu_limit") {
		t.Fatalf("err = %v, want an invalid cpu_limit error", err)
	}

	got, _ := client.AppsV1().Deployments(cfg.ResourceConfig.Namespace).Get(
		context.Background(), cfg.ResourceConfig.ResourceName(p.Name), metav1.GetOptions{})
	if lim := kubettyContainer(t, got).Resources.Limits[corev1.ResourceCPU]; lim.Cmp(resource.MustParse("16000m")) != 0 {
		t.Errorf("deployment was modified despite the error: cpu limit %s", lim.String())
	}
	if n := len(store.getStatuses()); n != 0 {
		t.Errorf("status changed %d time(s) despite the error", n)
	}
}

func TestRestartProject_MissingDeploymentErrors(t *testing.T) {
	cfg := newControllerConfig()
	store := newMockStore()
	p := newTestProjectWithStatus("gone", projects.StatusFailed)
	store.addProject(p)

	ctrl := NewWithClient(cfg, store, fake.NewSimpleClientset())
	if err := ctrl.RestartProject(context.Background(), p); err == nil {
		t.Fatal("RestartProject succeeded with no deployment, want an error")
	}
}

// TestBuildDeployment_DinDSidecarHardening covers the cluster-services-1008
// DinD fixes: no unauthenticated TCP listener, and the bumped image.
func TestBuildDeployment_DinDSidecarHardening(t *testing.T) {
	p := testProject("dind")
	p.DinDEnabled = true
	d := BuildDeployment(p, testConfig(), "")

	var docker *corev1.Container
	for i := range d.Spec.Template.Spec.Containers {
		if d.Spec.Template.Spec.Containers[i].Name == "docker" {
			docker = &d.Spec.Template.Spec.Containers[i]
		}
	}
	if docker == nil {
		t.Fatal("docker sidecar missing")
	}
	if docker.Image != DinDImage {
		t.Errorf("image = %q, want %q", docker.Image, DinDImage)
	}
	if strings.HasPrefix(DinDImage, "docker:24.") {
		t.Errorf("DinDImage %q is still on the EOL 24.x line", DinDImage)
	}
	for _, a := range docker.Args {
		if strings.Contains(a, "tcp://") {
			t.Errorf("docker sidecar still has a TCP listener: %q", a)
		}
	}
	// "dockerd" first, or the image entrypoint prepends its own default
	// --host=tcp://0.0.0.0:2375 when DOCKER_TLS_CERTDIR is empty.
	if len(docker.Args) == 0 || docker.Args[0] != "dockerd" {
		t.Errorf("args = %v, want \"dockerd\" first so the entrypoint adds no default TCP host", docker.Args)
	}
	hosts := 0
	group := ""
	for _, a := range docker.Args {
		if strings.HasPrefix(a, "--host=") {
			hosts++
			if a != "--host=unix:///var/run/docker/docker.sock" {
				t.Errorf("unexpected host arg %q", a)
			}
		}
		if strings.HasPrefix(a, "--group=") {
			group = strings.TrimPrefix(a, "--group=")
		}
	}
	if hosts != 1 {
		t.Errorf("args = %v, want exactly the unix socket host", docker.Args)
	}
	// The kubetty user reaches the socket via supplemental group 0.
	if group != "root" {
		t.Errorf("socket group = %q, want root (docker group gid 2375 locks the kubetty user out)", group)
	}
	for _, port := range docker.Ports {
		if port.ContainerPort == 2375 {
			t.Error("docker sidecar exposes port 2375")
		}
	}
}
