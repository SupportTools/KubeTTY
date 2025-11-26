package controller

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/supporttools/KubeTTY/server/internal/projects"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

var log = logrus.WithField("component", "controller")

// Config holds controller configuration.
type Config struct {
	// ReconcileInterval is how often to run the reconciliation loop.
	ReconcileInterval time.Duration

	// HealthCheckInterval is how often to check project health.
	HealthCheckInterval time.Duration

	// EnvSecretName is the name of the secret containing environment variables.
	EnvSecretName string

	// ResourceConfig holds naming configuration for Kubernetes resources.
	ResourceConfig ResourceConfig

	// StorageMonitor holds configuration for automatic PVC expansion.
	StorageMonitor StorageMonitorConfig
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		ReconcileInterval:   30 * time.Second,
		HealthCheckInterval: 60 * time.Second,
		EnvSecretName:       "env-secrets",
		ResourceConfig: ResourceConfig{
			Namespace: "kubetty-projects",
			Prefix:    "kubetty-project-",
			Env:       "dev",
		},
	}
}

// ProjectStatusCallback is called when a project transitions to a new status.
type ProjectStatusCallback func(project *projects.Project, newStatus projects.ProjectStatus)

// LeaderInfo provides read-only access to leader election status.
type LeaderInfo interface {
	IsLeader() bool
	GetCurrentLeader() string
	GetIdentity() string
}

// Controller manages the lifecycle of KubeTTY project resources.
type Controller struct {
	cfg       Config
	store     projects.Store
	clientset kubernetes.Interface

	statusCallback ProjectStatusCallback
	stopCh         chan struct{}
	wg             sync.WaitGroup

	// running tracks whether the controller loops are active.
	running atomic.Bool

	// leaderInfo provides access to leader election status (optional).
	leaderInfo LeaderInfo

	// lastExpansionTime tracks the last PVC expansion time per PVC name.
	// Used to enforce cooldown between expansions and prevent runaway growth.
	lastExpansionTime map[string]time.Time
}

// New creates a new Controller instance.
func New(cfg Config, store projects.Store) (*Controller, error) {
	// Create in-cluster Kubernetes client
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get in-cluster config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	return &Controller{
		cfg:               cfg,
		store:             store,
		clientset:         clientset,
		stopCh:            make(chan struct{}),
		lastExpansionTime: make(map[string]time.Time),
	}, nil
}

// NewWithClient creates a Controller with an existing Kubernetes client (useful for testing).
func NewWithClient(cfg Config, store projects.Store, clientset kubernetes.Interface) *Controller {
	return &Controller{
		cfg:               cfg,
		store:             store,
		clientset:         clientset,
		stopCh:            make(chan struct{}),
		lastExpansionTime: make(map[string]time.Time),
	}
}

// SetStatusCallback sets the callback for project status changes.
func (c *Controller) SetStatusCallback(cb ProjectStatusCallback) {
	c.statusCallback = cb
}

// SetLeaderInfo sets the leader election info provider.
func (c *Controller) SetLeaderInfo(info LeaderInfo) {
	c.leaderInfo = info
}

// IsRunning returns true if the controller loops are currently active.
func (c *Controller) IsRunning() bool {
	return c.running.Load()
}

// IsLeader returns true if this instance is the leader.
// Returns true if leader election is not configured (single replica mode).
func (c *Controller) IsLeader() bool {
	if c.leaderInfo == nil {
		return true // No leader election = always leader
	}
	return c.leaderInfo.IsLeader()
}

// GetLeaderInfo returns leader election information.
// Returns nil values if leader election is not configured.
func (c *Controller) GetLeaderInfo() (isLeader bool, currentLeader, identity string) {
	if c.leaderInfo == nil {
		return true, "", ""
	}
	return c.leaderInfo.IsLeader(), c.leaderInfo.GetCurrentLeader(), c.leaderInfo.GetIdentity()
}

// Start begins the controller's reconciliation loops.
func (c *Controller) Start(ctx context.Context) {
	if c.running.Load() {
		log.Warn("Controller already running, ignoring Start() call")
		return
	}

	log.Info("Starting project controller")
	c.running.Store(true)

	// Reset stop channel for new run
	c.stopCh = make(chan struct{})

	// Reconciliation loop
	c.wg.Add(1)
	go c.runReconcileLoop(ctx)

	// Health check loop
	c.wg.Add(1)
	go c.runHealthCheckLoop(ctx)

	// Storage monitor loop (for automatic PVC expansion)
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.runStorageMonitorLoop(ctx)
	}()
}

// Stop gracefully shuts down the controller.
func (c *Controller) Stop() {
	if !c.running.Load() {
		log.Warn("Controller not running, ignoring Stop() call")
		return
	}

	log.Info("Stopping project controller")
	c.running.Store(false)
	close(c.stopCh)
	c.wg.Wait()
	log.Info("Project controller stopped")
}

func (c *Controller) runReconcileLoop(ctx context.Context) {
	defer c.wg.Done()

	ticker := time.NewTicker(c.cfg.ReconcileInterval)
	defer ticker.Stop()

	// Run immediately on start
	c.reconcileAll(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopCh:
			return
		case <-ticker.C:
			c.reconcileAll(ctx)
		}
	}
}

func (c *Controller) runHealthCheckLoop(ctx context.Context) {
	defer c.wg.Done()

	ticker := time.NewTicker(c.cfg.HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopCh:
			return
		case <-ticker.C:
			c.checkAllHealth(ctx)
		}
	}
}

func (c *Controller) reconcileAll(ctx context.Context) {
	// Get all projects that need reconciliation
	statuses := []projects.ProjectStatus{
		projects.StatusPending,
		projects.StatusSyncing,
		projects.StatusCreating,
		projects.StatusUpdating,
		projects.StatusDeleting,
	}

	projectList, err := c.store.ListByStatuses(ctx, statuses)
	if err != nil {
		log.WithError(err).Error("Failed to list projects for reconciliation")
		return
	}

	for _, p := range projectList {
		if err := c.reconcileProject(ctx, &p); err != nil {
			log.WithError(err).WithField("project", p.Name).Error("Failed to reconcile project")
		}
	}
}

func (c *Controller) reconcileProject(ctx context.Context, p *projects.Project) error {
	logger := log.WithField("project", p.Name).WithField("status", p.Status)
	logger.Debug("Reconciling project")

	switch p.Status {
	case projects.StatusPending:
		return c.handlePending(ctx, p)
	case projects.StatusSyncing:
		return c.handleSyncing(ctx, p)
	case projects.StatusCreating:
		return c.handleCreating(ctx, p)
	case projects.StatusUpdating:
		return c.handleUpdating(ctx, p)
	case projects.StatusDeleting:
		return c.handleDeleting(ctx, p)
	default:
		return nil
	}
}

func (c *Controller) handlePending(ctx context.Context, p *projects.Project) error {
	logger := log.WithField("project", p.Name)
	logger.Info("Creating project resources")

	cfg := c.cfg.ResourceConfig

	// Update status to creating
	if err := c.store.SetStatus(ctx, p.ID, projects.StatusCreating, "Creating Kubernetes resources"); err != nil {
		return fmt.Errorf("failed to update status: %w", err)
	}

	// Create PVC
	pvc := BuildPVC(p, cfg)
	if _, err := c.clientset.CoreV1().PersistentVolumeClaims(cfg.Namespace).Create(ctx, pvc, metav1.CreateOptions{}); err != nil {
		if !errors.IsAlreadyExists(err) {
			if statusErr := c.store.SetStatus(ctx, p.ID, projects.StatusFailed, fmt.Sprintf("Failed to create PVC: %v", err)); statusErr != nil {
				logger.WithError(statusErr).Error("Failed to update project status to failed")
			}
			return fmt.Errorf("failed to create PVC: %w", err)
		}
	}

	// Create ServiceAccount
	sa := BuildServiceAccount(p, cfg)
	if _, err := c.clientset.CoreV1().ServiceAccounts(cfg.Namespace).Create(ctx, sa, metav1.CreateOptions{}); err != nil {
		if !errors.IsAlreadyExists(err) {
			if statusErr := c.store.SetStatus(ctx, p.ID, projects.StatusFailed, fmt.Sprintf("Failed to create ServiceAccount: %v", err)); statusErr != nil {
				logger.WithError(statusErr).Error("Failed to update project status to failed")
			}
			return fmt.Errorf("failed to create ServiceAccount: %w", err)
		}
	}

	// Create ClusterRole and ClusterRoleBinding for admin access
	adminRole := BuildAdminClusterRole(p, cfg)
	if _, err := c.clientset.RbacV1().ClusterRoles().Create(ctx, adminRole, metav1.CreateOptions{}); err != nil {
		if !errors.IsAlreadyExists(err) {
			logger.WithError(err).Warn("Failed to create admin ClusterRole")
		}
	}
	adminBinding := BuildAdminClusterRoleBinding(p, cfg)
	if _, err := c.clientset.RbacV1().ClusterRoleBindings().Create(ctx, adminBinding, metav1.CreateOptions{}); err != nil {
		if !errors.IsAlreadyExists(err) {
			logger.WithError(err).Warn("Failed to create admin ClusterRoleBinding")
		}
	}

	// Create ClusterRole and ClusterRoleBinding for read access
	readRole := BuildReadClusterRole(p, cfg)
	if _, err := c.clientset.RbacV1().ClusterRoles().Create(ctx, readRole, metav1.CreateOptions{}); err != nil {
		if !errors.IsAlreadyExists(err) {
			logger.WithError(err).Warn("Failed to create read ClusterRole")
		}
	}
	readBinding := BuildReadClusterRoleBinding(p, cfg)
	if _, err := c.clientset.RbacV1().ClusterRoleBindings().Create(ctx, readBinding, metav1.CreateOptions{}); err != nil {
		if !errors.IsAlreadyExists(err) {
			logger.WithError(err).Warn("Failed to create read ClusterRoleBinding")
		}
	}

	// Create Service
	svc := BuildService(p, cfg)
	if _, err := c.clientset.CoreV1().Services(cfg.Namespace).Create(ctx, svc, metav1.CreateOptions{}); err != nil {
		if !errors.IsAlreadyExists(err) {
			if statusErr := c.store.SetStatus(ctx, p.ID, projects.StatusFailed, fmt.Sprintf("Failed to create Service: %v", err)); statusErr != nil {
				logger.WithError(statusErr).Error("Failed to update project status to failed")
			}
			return fmt.Errorf("failed to create Service: %w", err)
		}
	}

	// Create per-project env secret (empty initially, can be populated via API)
	envSecret := BuildEnvSecret(p, cfg, nil)
	if _, err := c.clientset.CoreV1().Secrets(cfg.Namespace).Create(ctx, envSecret, metav1.CreateOptions{}); err != nil {
		if !errors.IsAlreadyExists(err) {
			logger.WithError(err).Warn("Failed to create env secret")
		}
	}

	// If template sync is enabled, create sync Job and wait for it to complete
	// before creating the Deployment. This avoids keeping the template PVC
	// attached to running project pods.
	if cfg.TemplatePVCName != "" {
		syncJob := BuildTemplateSyncJob(p, cfg)
		if _, err := c.clientset.BatchV1().Jobs(cfg.Namespace).Create(ctx, syncJob, metav1.CreateOptions{}); err != nil {
			if !errors.IsAlreadyExists(err) {
				if statusErr := c.store.SetStatus(ctx, p.ID, projects.StatusFailed, fmt.Sprintf("Failed to create sync Job: %v", err)); statusErr != nil {
					logger.WithError(statusErr).Error("Failed to update project status to failed")
				}
				return fmt.Errorf("failed to create sync Job: %w", err)
			}
		}
		logger.Info("Template sync Job created, waiting for completion")
		return c.store.SetStatus(ctx, p.ID, projects.StatusSyncing, "Waiting for template sync to complete")
	}

	// No template sync - create Deployment directly
	envSecretName := cfg.EnvSecretName(p.Name)
	deploy := BuildDeployment(p, cfg, envSecretName)
	if _, err := c.clientset.AppsV1().Deployments(cfg.Namespace).Create(ctx, deploy, metav1.CreateOptions{}); err != nil {
		if !errors.IsAlreadyExists(err) {
			if statusErr := c.store.SetStatus(ctx, p.ID, projects.StatusFailed, fmt.Sprintf("Failed to create Deployment: %v", err)); statusErr != nil {
				logger.WithError(statusErr).Error("Failed to update project status to failed")
			}
			return fmt.Errorf("failed to create Deployment: %w", err)
		}
	}

	logger.Info("Project resources created, waiting for deployment")
	return nil
}

// handleSyncing waits for the template sync Job to complete, then creates the Deployment.
func (c *Controller) handleSyncing(ctx context.Context, p *projects.Project) error {
	logger := log.WithField("project", p.Name)
	cfg := c.cfg.ResourceConfig
	jobName := cfg.TemplateSyncJobName(p.Name)

	// Check Job status
	job, err := c.clientset.BatchV1().Jobs(cfg.Namespace).Get(ctx, jobName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			// Job doesn't exist - either it was never created or TTL cleaned it up
			// Check if marker file exists via creating deployment anyway
			logger.Warn("Sync Job not found, proceeding to create deployment")
			return c.createDeploymentAndTransition(ctx, p, cfg, logger)
		}
		return fmt.Errorf("failed to get sync Job: %w", err)
	}

	// Check Job completion status
	if job.Status.Succeeded >= 1 {
		logger.Info("Template sync Job completed successfully")
		// Delete the Job (it will also be cleaned up by TTL, but let's be proactive)
		propagationPolicy := metav1.DeletePropagationBackground
		if err := c.clientset.BatchV1().Jobs(cfg.Namespace).Delete(ctx, jobName, metav1.DeleteOptions{
			PropagationPolicy: &propagationPolicy,
		}); err != nil && !errors.IsNotFound(err) {
			logger.WithError(err).Warn("Failed to delete completed sync Job")
		}
		return c.createDeploymentAndTransition(ctx, p, cfg, logger)
	}

	if job.Status.Failed >= 3 { // BackoffLimit is 3
		logger.Error("Template sync Job failed after retries")
		// Delete the failed Job
		propagationPolicy := metav1.DeletePropagationBackground
		if err := c.clientset.BatchV1().Jobs(cfg.Namespace).Delete(ctx, jobName, metav1.DeleteOptions{
			PropagationPolicy: &propagationPolicy,
		}); err != nil && !errors.IsNotFound(err) {
			logger.WithError(err).Warn("Failed to delete failed sync Job")
		}
		return c.store.SetStatus(ctx, p.ID, projects.StatusFailed, "Template sync failed after retries")
	}

	// Job is still running
	logger.Debug("Waiting for template sync Job to complete")
	return nil
}

// createDeploymentAndTransition creates the Deployment and transitions to creating status.
func (c *Controller) createDeploymentAndTransition(ctx context.Context, p *projects.Project, cfg ResourceConfig, logger *logrus.Entry) error {
	envSecretName := cfg.EnvSecretName(p.Name)
	deploy := BuildDeployment(p, cfg, envSecretName)
	if _, err := c.clientset.AppsV1().Deployments(cfg.Namespace).Create(ctx, deploy, metav1.CreateOptions{}); err != nil {
		if !errors.IsAlreadyExists(err) {
			if statusErr := c.store.SetStatus(ctx, p.ID, projects.StatusFailed, fmt.Sprintf("Failed to create Deployment: %v", err)); statusErr != nil {
				logger.WithError(statusErr).Error("Failed to update project status to failed")
			}
			return fmt.Errorf("failed to create Deployment: %w", err)
		}
	}

	logger.Info("Deployment created, transitioning to creating status")
	return c.store.SetStatus(ctx, p.ID, projects.StatusCreating, "Waiting for deployment to be ready")
}

func (c *Controller) handleCreating(ctx context.Context, p *projects.Project) error {
	logger := log.WithField("project", p.Name)
	cfg := c.cfg.ResourceConfig
	resourceName := cfg.ResourceName(p.Name)

	// Check deployment status
	deploy, err := c.clientset.AppsV1().Deployments(cfg.Namespace).Get(ctx, resourceName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			// Deployment doesn't exist, go back to pending
			return c.store.SetStatus(ctx, p.ID, projects.StatusPending, "Deployment not found, recreating")
		}
		return fmt.Errorf("failed to get deployment: %w", err)
	}

	// Check if deployment is ready
	if deploy.Status.ReadyReplicas >= 1 && deploy.Status.AvailableReplicas >= 1 {
		logger.Info("Project deployment is ready")

		// Get pod IP
		podIP := ""
		pods, err := c.clientset.CoreV1().Pods(cfg.Namespace).List(ctx, metav1.ListOptions{
			LabelSelector: fmt.Sprintf("%s=%s,%s=%s", labelApp, "kubetty", labelInstance, p.Name),
		})
		if err == nil && len(pods.Items) > 0 {
			podIP = pods.Items[0].Status.PodIP
		}

		if err := c.store.UpdateHealthCheck(ctx, p.ID, podIP); err != nil {
			logger.WithError(err).Warn("Failed to update health check")
		}

		if err := c.store.SetStatus(ctx, p.ID, projects.StatusRunning, ""); err != nil {
			return err
		}

		// Notify callback that project is now running
		if c.statusCallback != nil {
			c.statusCallback(p, projects.StatusRunning)
		}
		return nil
	}

	// Still waiting
	logger.Debug("Waiting for deployment to be ready")
	return nil
}

func (c *Controller) handleUpdating(ctx context.Context, p *projects.Project) error {
	logger := log.WithField("project", p.Name)
	cfg := c.cfg.ResourceConfig
	resourceName := cfg.ResourceName(p.Name)

	// Handle PVC expansion if storage size changed
	if err := c.expandPVCIfNeeded(ctx, p, cfg, resourceName); err != nil {
		logger.WithError(err).Warn("Failed to expand PVC (may require manual intervention)")
		// Don't fail the entire update - PVC expansion may not be supported by storage class
	}

	// Update the deployment spec (use per-project secret name)
	envSecretName := cfg.EnvSecretName(p.Name)
	deploy := BuildDeployment(p, cfg, envSecretName)

	// Retry loop for handling optimistic concurrency conflicts
	const maxRetries = 5
	var lastErr error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		existing, err := c.clientset.AppsV1().Deployments(cfg.Namespace).Get(ctx, resourceName, metav1.GetOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
				// Create if doesn't exist
				_, err = c.clientset.AppsV1().Deployments(cfg.Namespace).Create(ctx, deploy, metav1.CreateOptions{})
				if err == nil {
					logger.Info("Deployment created (was not found), transitioning to creating status")
					return c.store.SetStatus(ctx, p.ID, projects.StatusCreating, "Waiting for deployment rollout")
				}
			}
			return err
		}

		// Update spec
		existing.Spec = deploy.Spec
		_, err = c.clientset.AppsV1().Deployments(cfg.Namespace).Update(ctx, existing, metav1.UpdateOptions{})
		if err == nil {
			logger.Info("Deployment updated, transitioning to creating status")
			return c.store.SetStatus(ctx, p.ID, projects.StatusCreating, "Waiting for deployment rollout")
		}

		// Check if it's a conflict error - retry with fresh data
		if errors.IsConflict(err) {
			logger.WithField("attempt", attempt).Warn("Deployment update conflict, retrying with fresh data")
			lastErr = err
			continue
		}

		// Non-conflict error - fail immediately
		if statusErr := c.store.SetStatus(ctx, p.ID, projects.StatusFailed, fmt.Sprintf("Failed to update deployment: %v", err)); statusErr != nil {
			logger.WithError(statusErr).Error("Failed to update project status to failed")
		}
		return fmt.Errorf("failed to update deployment: %w", err)
	}

	// Exhausted retries
	if statusErr := c.store.SetStatus(ctx, p.ID, projects.StatusFailed, fmt.Sprintf("Failed to update deployment after %d retries: %v", maxRetries, lastErr)); statusErr != nil {
		logger.WithError(statusErr).Error("Failed to update project status to failed")
	}
	return fmt.Errorf("failed to update deployment after %d retries: %w", maxRetries, lastErr)
}

func (c *Controller) handleDeleting(ctx context.Context, p *projects.Project) error {
	logger := log.WithField("project", p.Name)
	logger.Info("Deleting project resources")

	cfg := c.cfg.ResourceConfig
	resourceName := cfg.ResourceName(p.Name)

	// Delete sync Job if it exists
	jobName := cfg.TemplateSyncJobName(p.Name)
	propagationPolicy := metav1.DeletePropagationBackground
	if err := c.clientset.BatchV1().Jobs(cfg.Namespace).Delete(ctx, jobName, metav1.DeleteOptions{
		PropagationPolicy: &propagationPolicy,
	}); err != nil {
		if !errors.IsNotFound(err) {
			logger.WithError(err).Warn("Failed to delete sync Job")
		}
	}

	// Delete deployment
	if err := c.clientset.AppsV1().Deployments(cfg.Namespace).Delete(ctx, resourceName, metav1.DeleteOptions{}); err != nil {
		if !errors.IsNotFound(err) {
			logger.WithError(err).Warn("Failed to delete deployment")
		}
	}

	// Delete service
	if err := c.clientset.CoreV1().Services(cfg.Namespace).Delete(ctx, resourceName, metav1.DeleteOptions{}); err != nil {
		if !errors.IsNotFound(err) {
			logger.WithError(err).Warn("Failed to delete service")
		}
	}

	// Delete service account
	saName := fmt.Sprintf("%s-sa", resourceName)
	if err := c.clientset.CoreV1().ServiceAccounts(cfg.Namespace).Delete(ctx, saName, metav1.DeleteOptions{}); err != nil {
		if !errors.IsNotFound(err) {
			logger.WithError(err).Warn("Failed to delete service account")
		}
	}

	// Delete per-project env secret
	envSecretName := cfg.EnvSecretName(p.Name)
	if err := c.clientset.CoreV1().Secrets(cfg.Namespace).Delete(ctx, envSecretName, metav1.DeleteOptions{}); err != nil {
		if !errors.IsNotFound(err) {
			logger.WithError(err).Warn("Failed to delete env secret")
		}
	}

	// Delete cluster-scoped RBAC resources
	// Use ClusterRoleName for consistent naming with the builders
	adminRoleName := cfg.ClusterRoleName(p.Name, "admin")
	readRoleName := cfg.ClusterRoleName(p.Name, "read")

	// Delete admin ClusterRoleBinding and ClusterRole
	if err := c.clientset.RbacV1().ClusterRoleBindings().Delete(ctx, adminRoleName, metav1.DeleteOptions{}); err != nil {
		if !errors.IsNotFound(err) {
			logger.WithError(err).Warn("Failed to delete admin ClusterRoleBinding")
		}
	}
	if err := c.clientset.RbacV1().ClusterRoles().Delete(ctx, adminRoleName, metav1.DeleteOptions{}); err != nil {
		if !errors.IsNotFound(err) {
			logger.WithError(err).Warn("Failed to delete admin ClusterRole")
		}
	}

	// Delete read ClusterRoleBinding and ClusterRole
	if err := c.clientset.RbacV1().ClusterRoleBindings().Delete(ctx, readRoleName, metav1.DeleteOptions{}); err != nil {
		if !errors.IsNotFound(err) {
			logger.WithError(err).Warn("Failed to delete read ClusterRoleBinding")
		}
	}
	if err := c.clientset.RbacV1().ClusterRoles().Delete(ctx, readRoleName, metav1.DeleteOptions{}); err != nil {
		if !errors.IsNotFound(err) {
			logger.WithError(err).Warn("Failed to delete read ClusterRole")
		}
	}

	// Note: We intentionally don't delete the PVC to preserve data
	// The PVC can be cleaned up manually if needed

	// Hard delete from database
	if err := c.store.HardDelete(ctx, p.ID); err != nil {
		return fmt.Errorf("failed to hard delete project: %w", err)
	}

	logger.Info("Project resources deleted")
	return nil
}

func (c *Controller) checkAllHealth(ctx context.Context) {
	// Get all running projects
	projectList, err := c.store.ListByStatuses(ctx, []projects.ProjectStatus{projects.StatusRunning})
	if err != nil {
		log.WithError(err).Error("Failed to list running projects for health check")
		return
	}

	for _, p := range projectList {
		c.checkProjectHealth(ctx, &p)
	}
}

func (c *Controller) checkProjectHealth(ctx context.Context, p *projects.Project) {
	logger := log.WithField("project", p.Name)
	cfg := c.cfg.ResourceConfig
	resourceName := cfg.ResourceName(p.Name)

	// Check deployment
	deploy, err := c.clientset.AppsV1().Deployments(cfg.Namespace).Get(ctx, resourceName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Warn("Deployment not found, marking as failed")
			if statusErr := c.store.SetStatus(ctx, p.ID, projects.StatusFailed, "Deployment not found"); statusErr != nil {
				logger.WithError(statusErr).Error("Failed to update project status to failed")
			}
		}
		return
	}

	// Check if healthy
	if deploy.Status.ReadyReplicas < 1 {
		logger.Warn("Deployment not ready")
		if statusErr := c.store.SetStatus(ctx, p.ID, projects.StatusFailed, "Deployment not ready"); statusErr != nil {
			logger.WithError(statusErr).Error("Failed to update project status to failed")
		}
		return
	}

	// Get pod IP
	podIP := ""
	pods, err := c.clientset.CoreV1().Pods(cfg.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s,%s=%s", labelApp, "kubetty", labelInstance, p.Name),
	})
	if err == nil && len(pods.Items) > 0 {
		podIP = pods.Items[0].Status.PodIP
	}

	// Perform HTTP health check
	healthURL := fmt.Sprintf("http://%s.%s.svc:8080/api/healthz", resourceName, cfg.Namespace)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(healthURL)
	if err != nil {
		logger.WithError(err).Debug("Health check failed")
		// Don't mark as failed for transient network issues
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.WithField("status", resp.StatusCode).Warn("Health check returned non-OK status")
	}

	// Update last health check
	if err := c.store.UpdateHealthCheck(ctx, p.ID, podIP); err != nil {
		logger.WithError(err).Warn("Failed to update health check")
	}
}

// ResyncProject triggers a full resync of the project resources.
// This sets the project status to "pending" which causes the controller to
// recreate any missing resources (deployment, service, etc.) while preserving
// existing resources like PVCs. This is useful for recovering from a "failed"
// state when resources were accidentally deleted.
func (c *Controller) ResyncProject(ctx context.Context, p *projects.Project) error {
	logger := log.WithField("project", p.Name)
	logger.Info("Resyncing project resources")

	// Set status to pending to trigger full resource recreation
	// The handlePending function uses IsAlreadyExists checks, so existing
	// resources (like PVCs with important data) will be preserved
	if err := c.store.SetStatus(ctx, p.ID, projects.StatusPending, "Resyncing project resources"); err != nil {
		return fmt.Errorf("failed to set status to pending: %w", err)
	}

	logger.Info("Project marked for resync, controller will recreate missing resources")
	return nil
}

// RestartProject triggers a restart of the project deployment.
func (c *Controller) RestartProject(ctx context.Context, p *projects.Project) error {
	logger := log.WithField("project", p.Name)
	logger.Info("Restarting project")

	cfg := c.cfg.ResourceConfig
	resourceName := cfg.ResourceName(p.Name)

	// Retry loop for handling optimistic concurrency conflicts
	const maxRetries = 5
	var lastErr error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		// Get current deployment (fresh on each attempt)
		deploy, err := c.clientset.AppsV1().Deployments(cfg.Namespace).Get(ctx, resourceName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to get deployment: %w", err)
		}

		// Add/update restart annotation
		if deploy.Spec.Template.Annotations == nil {
			deploy.Spec.Template.Annotations = map[string]string{}
		}
		deploy.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] = time.Now().Format(time.RFC3339)

		// Update deployment
		_, err = c.clientset.AppsV1().Deployments(cfg.Namespace).Update(ctx, deploy, metav1.UpdateOptions{})
		if err == nil {
			// Set status to creating to wait for rollout
			return c.store.SetStatus(ctx, p.ID, projects.StatusCreating, "Restarting deployment")
		}

		// Check if it's a conflict error - retry with fresh data
		if errors.IsConflict(err) {
			logger.WithField("attempt", attempt).Warn("Deployment restart conflict, retrying with fresh data")
			lastErr = err
			continue
		}

		// Non-conflict error - fail immediately
		return fmt.Errorf("failed to update deployment: %w", err)
	}

	// Exhausted retries
	return fmt.Errorf("failed to restart deployment after %d retries: %w", maxRetries, lastErr)
}

// GetDeploymentStatus returns the current status of a project's deployment.
func (c *Controller) GetDeploymentStatus(ctx context.Context, p *projects.Project) (*DeploymentStatus, error) {
	cfg := c.cfg.ResourceConfig
	resourceName := cfg.ResourceName(p.Name)

	deploy, err := c.clientset.AppsV1().Deployments(cfg.Namespace).Get(ctx, resourceName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return &DeploymentStatus{Exists: false}, nil
		}
		return nil, err
	}

	pods, _ := c.clientset.CoreV1().Pods(cfg.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s,%s=%s", labelApp, "kubetty", labelInstance, p.Name),
	})

	status := &DeploymentStatus{
		Exists:            true,
		Replicas:          int(deploy.Status.Replicas),
		ReadyReplicas:     int(deploy.Status.ReadyReplicas),
		AvailableReplicas: int(deploy.Status.AvailableReplicas),
		Pods:              []PodStatus{},
	}

	if pods != nil {
		for _, pod := range pods.Items {
			ps := PodStatus{
				Name:   pod.Name,
				Phase:  string(pod.Status.Phase),
				PodIP:  pod.Status.PodIP,
				Ready:  isPodReady(&pod),
				Reason: pod.Status.Reason,
			}
			status.Pods = append(status.Pods, ps)
		}
	}

	return status, nil
}

// DeploymentStatus represents the status of a project's Kubernetes resources.
type DeploymentStatus struct {
	Exists            bool        `json:"exists"`
	Replicas          int         `json:"replicas"`
	ReadyReplicas     int         `json:"readyReplicas"`
	AvailableReplicas int         `json:"availableReplicas"`
	Pods              []PodStatus `json:"pods"`
}

// PodStatus represents the status of a single pod.
type PodStatus struct {
	Name   string `json:"name"`
	Phase  string `json:"phase"`
	PodIP  string `json:"podIP"`
	Ready  bool   `json:"ready"`
	Reason string `json:"reason,omitempty"`
}

func isPodReady(pod *corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

// GetProjectSecrets returns the environment secrets for a project (key names only, not values).
func (c *Controller) GetProjectSecrets(ctx context.Context, p *projects.Project) (map[string]string, error) {
	cfg := c.cfg.ResourceConfig
	envSecretName := cfg.EnvSecretName(p.Name)

	secret, err := c.clientset.CoreV1().Secrets(cfg.Namespace).Get(ctx, envSecretName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			// Secret doesn't exist yet, return empty map
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("failed to get secret: %w", err)
	}

	// Return key-value pairs (values as strings)
	result := make(map[string]string)
	for k, v := range secret.Data {
		result[k] = string(v)
	}
	return result, nil
}

// UpdateProjectSecrets updates the environment secrets for a project.
// This replaces the entire secret data with the new values and triggers a deployment restart.
func (c *Controller) UpdateProjectSecrets(ctx context.Context, p *projects.Project, secrets map[string]string) error {
	logger := log.WithField("project", p.Name)
	cfg := c.cfg.ResourceConfig
	envSecretName := cfg.EnvSecretName(p.Name)

	// Build the secret data
	secretData := make(map[string][]byte)
	for k, v := range secrets {
		secretData[k] = []byte(v)
	}

	// Check if secret exists
	existing, err := c.clientset.CoreV1().Secrets(cfg.Namespace).Get(ctx, envSecretName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			// Create new secret
			newSecret := BuildEnvSecret(p, cfg, secrets)
			if _, err := c.clientset.CoreV1().Secrets(cfg.Namespace).Create(ctx, newSecret, metav1.CreateOptions{}); err != nil {
				return fmt.Errorf("failed to create secret: %w", err)
			}
			logger.Info("Created project env secret")
		} else {
			return fmt.Errorf("failed to get secret: %w", err)
		}
	} else {
		// Update existing secret
		existing.Data = secretData
		if _, err := c.clientset.CoreV1().Secrets(cfg.Namespace).Update(ctx, existing, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("failed to update secret: %w", err)
		}
		logger.Info("Updated project env secret")
	}

	// Trigger a deployment restart to pick up the new secrets
	return c.RestartProject(ctx, p)
}

// expandPVCIfNeeded expands a project's PVC if the requested storage is larger than current.
// This only works if the storage class supports volume expansion (allowVolumeExpansion: true).
func (c *Controller) expandPVCIfNeeded(ctx context.Context, p *projects.Project, cfg ResourceConfig, resourceName string) error {
	logger := log.WithField("project", p.Name)
	pvcName := fmt.Sprintf("%s-data", resourceName)

	// Get current PVC
	pvc, err := c.clientset.CoreV1().PersistentVolumeClaims(cfg.Namespace).Get(ctx, pvcName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			// PVC doesn't exist, nothing to expand
			return nil
		}
		return fmt.Errorf("failed to get PVC: %w", err)
	}

	// Parse current storage from PVC
	currentStorage := pvc.Spec.Resources.Requests[corev1.ResourceStorage]

	// Parse requested storage from project
	requestedStorage, err := resource.ParseQuantity(p.StorageSize)
	if err != nil {
		return fmt.Errorf("failed to parse requested storage size %q: %w", p.StorageSize, err)
	}

	// Compare: only expand, never shrink
	if requestedStorage.Cmp(currentStorage) <= 0 {
		logger.WithFields(logrus.Fields{
			"current":   currentStorage.String(),
			"requested": requestedStorage.String(),
		}).Debug("PVC expansion not needed (requested <= current)")
		return nil
	}

	// Update PVC with new storage request
	logger.WithFields(logrus.Fields{
		"current":   currentStorage.String(),
		"requested": requestedStorage.String(),
	}).Info("Expanding PVC storage")

	pvc.Spec.Resources.Requests[corev1.ResourceStorage] = requestedStorage
	if _, err := c.clientset.CoreV1().PersistentVolumeClaims(cfg.Namespace).Update(ctx, pvc, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("failed to update PVC for expansion: %w", err)
	}

	logger.Info("PVC expansion requested successfully")
	return nil
}

// Ensure interfaces are implemented (compile-time check)
var (
	_ *appsv1.Deployment         = nil
	_ *rbacv1.ClusterRole        = nil
	_ *rbacv1.ClusterRoleBinding = nil
)
