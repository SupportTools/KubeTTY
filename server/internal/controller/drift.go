package controller

import (
	"context"
	"fmt"
	"time"

	"github.com/supporttools/KubeTTY/server/internal/projects"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DefaultDriftCheckInterval is how often the controller compares running
// project Deployments with their DB rows when Config.DriftCheckInterval is 0.
const DefaultDriftCheckInterval = 5 * time.Minute

// Drift check (cluster-services-1024).
//
// A project's DB row can be ahead of its running Deployment: an admin saved a
// change with "apply on next restart", or edited the DB directly (as
// cluster-services-990 had to). RestartProject rebuilds the Deployment from the
// DB, so the change lands on the next restart. Until then the drift check makes
// the gap visible: it compares the running Deployment's kubetty container
// (cpu/memory requests and limits, image) with what BuildDeployment would
// produce from the DB row, and stores the differences as the project's
// pending changes.
//
// The drift check is READ-ONLY against Kubernetes. It only GETs Deployments and
// writes the result to the DB. It never updates, patches, scales or deletes a
// Deployment or pod; tests assert that no write verb reaches the clientset.

func (c *Controller) driftCheckInterval() time.Duration {
	if c.cfg.DriftCheckInterval > 0 {
		return c.cfg.DriftCheckInterval
	}
	return DefaultDriftCheckInterval
}

// runDriftCheckLoop runs checkAllDrift on start and then every interval.
func (c *Controller) runDriftCheckLoop(ctx context.Context) {
	defer c.wg.Done()

	interval := c.driftCheckInterval()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	log.WithField("interval", interval).Info("Starting deployment drift check loop (report only, never restarts pods)")

	c.checkAllDrift(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopCh:
			return
		case <-ticker.C:
			c.checkAllDrift(ctx)
		}
	}
}

// checkAllDrift runs CheckProjectDrift for every running project.
func (c *Controller) checkAllDrift(ctx context.Context) {
	projectList, err := c.store.ListByStatuses(ctx, []projects.ProjectStatus{projects.StatusRunning})
	if err != nil {
		log.WithError(err).Error("Drift check: failed to list running projects")
		return
	}
	for i := range projectList {
		if _, err := c.CheckProjectDrift(ctx, &projectList[i]); err != nil {
			log.WithError(err).WithField("project", projectList[i].Name).Warn("Drift check failed")
		}
	}
}

// CheckProjectDrift compares the project's running Deployment with the DB row,
// stores the result as the project's pending changes, and returns it. A missing
// Deployment is not drift (the reconcile loop owns creation) and records no
// changes. It never modifies the Deployment.
func (c *Controller) CheckProjectDrift(ctx context.Context, p *projects.Project) ([]projects.PendingChange, error) {
	cfg := c.cfg.ResourceConfig
	deploy, err := c.clientset.AppsV1().Deployments(cfg.Namespace).Get(ctx, cfg.ResourceName(p.Name), metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("get deployment: %w", err)
	}

	changes, err := ComputeDrift(p, deploy, cfg)
	if err != nil {
		return nil, err
	}
	if err := c.store.SetPendingChanges(ctx, p.ID, changes); err != nil {
		return changes, fmt.Errorf("store pending changes: %w", err)
	}
	if len(changes) > 0 {
		log.WithField("project", p.Name).WithField("changes", changes).
			Info("Deployment differs from DB; pending changes apply on next restart")
	}
	return changes, nil
}

// ComputeDrift returns the fields where the running Deployment's kubetty
// container differs from the container BuildDeployment would produce from the
// project's DB row: cpu/memory requests and limits, and image. Quantities are
// compared by value, so "1" and "1000m" are equal. The order of the result is
// fixed (cpuRequest, cpuLimit, memoryRequest, memoryLimit, image).
func ComputeDrift(p *projects.Project, running *appsv1.Deployment, cfg ResourceConfig) ([]projects.PendingChange, error) {
	if err := validateProjectResources(p); err != nil {
		return nil, fmt.Errorf("cannot build desired deployment: %w", err)
	}
	desired := BuildDeployment(p, cfg, cfg.EnvSecretName(p.Name))

	want, ok := findContainer(desired, "kubetty")
	if !ok {
		return nil, fmt.Errorf("desired deployment has no kubetty container")
	}
	have, ok := findContainer(running, "kubetty")
	if !ok {
		// The running Deployment has no kubetty container at all; every
		// tracked field differs.
		have = corev1.Container{}
	}

	var changes []projects.PendingChange
	for _, f := range []struct {
		field string
		list  func(corev1.ResourceRequirements) corev1.ResourceList
		name  corev1.ResourceName
	}{
		{"cpuRequest", requestsOf, corev1.ResourceCPU},
		{"cpuLimit", limitsOf, corev1.ResourceCPU},
		{"memoryRequest", requestsOf, corev1.ResourceMemory},
		{"memoryLimit", limitsOf, corev1.ResourceMemory},
	} {
		h, hok := f.list(have.Resources)[f.name]
		w := f.list(want.Resources)[f.name]
		if !hok || h.Cmp(w) != 0 {
			changes = append(changes, projects.PendingChange{
				Field:   f.field,
				Running: quantityString(h, hok),
				Desired: w.String(),
			})
		}
	}
	if have.Image != want.Image {
		changes = append(changes, projects.PendingChange{Field: "image", Running: have.Image, Desired: want.Image})
	}
	return changes, nil
}

func requestsOf(r corev1.ResourceRequirements) corev1.ResourceList { return r.Requests }
func limitsOf(r corev1.ResourceRequirements) corev1.ResourceList   { return r.Limits }

func quantityString(q resource.Quantity, ok bool) string {
	if !ok {
		return ""
	}
	return q.String()
}

func findContainer(d *appsv1.Deployment, name string) (corev1.Container, bool) {
	for _, ctr := range d.Spec.Template.Spec.Containers {
		if ctr.Name == name {
			return ctr, true
		}
	}
	return corev1.Container{}, false
}

// clearPendingChanges resets a project's pending changes after its Deployment
// has been rebuilt from the DB. Best effort: the next drift check corrects it.
func (c *Controller) clearPendingChanges(ctx context.Context, p *projects.Project) {
	if len(p.PendingChanges) == 0 {
		return
	}
	if err := c.store.SetPendingChanges(ctx, p.ID, nil); err != nil {
		log.WithError(err).WithField("project", p.Name).Warn("Failed to clear pending changes after rebuild")
	}
}
