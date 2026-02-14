package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/supporttools/KubeTTY/server/internal/projects"
	"github.com/supporttools/KubeTTY/server/internal/shared/metrics"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// StorageMonitorConfig holds configuration for automatic PVC expansion.
type StorageMonitorConfig struct {
	// Enabled controls whether storage monitoring and auto-expansion is active.
	Enabled bool

	// Interval is how often to check storage usage for all running projects.
	Interval time.Duration

	// ExpandThreshold is the usage fraction (0.0-1.0) at which to trigger expansion.
	// For example, 0.70 means expand when disk usage >= 70%.
	ExpandThreshold float64

	// ExpandAmount is the fixed quantity to add to the PVC on each expansion.
	// Should be a valid Kubernetes quantity string like "10Gi" or "100Gi".
	ExpandAmount string

	// ExpandCooldown is the minimum time between expansions for a single PVC.
	// This prevents runaway expansions when metrics update slowly.
	ExpandCooldown time.Duration
}

// DefaultStorageMonitorConfig returns sensible defaults for storage monitoring.
func DefaultStorageMonitorConfig() StorageMonitorConfig {
	return StorageMonitorConfig{
		Enabled:         true,
		Interval:        60 * time.Second,
		ExpandThreshold: 0.70,
		ExpandAmount:    "10Gi",
		ExpandCooldown:  5 * time.Minute,
	}
}

// runStorageMonitorLoop periodically checks project PVC usage and expands if needed.
// It runs as a separate goroutine and exits when context is cancelled.
func (c *Controller) runStorageMonitorLoop(ctx context.Context) {
	if !c.cfg.StorageMonitor.Enabled {
		log.Info("Storage monitoring disabled")
		return
	}

	ticker := time.NewTicker(c.cfg.StorageMonitor.Interval)
	defer ticker.Stop()

	log.WithFields(map[string]interface{}{
		"interval":  c.cfg.StorageMonitor.Interval,
		"threshold": c.cfg.StorageMonitor.ExpandThreshold * 100,
		"amount":    c.cfg.StorageMonitor.ExpandAmount,
		"cooldown":  c.cfg.StorageMonitor.ExpandCooldown,
	}).Info("Starting storage monitor loop")

	for {
		select {
		case <-ctx.Done():
			log.Info("Storage monitor loop stopping")
			return
		case <-c.stopCh:
			log.Info("Storage monitor loop stopping")
			return
		case <-ticker.C:
			c.checkAllProjectStorage(ctx)
		}
	}
}

// checkAllProjectStorage checks storage usage for all running projects.
func (c *Controller) checkAllProjectStorage(ctx context.Context) {
	projectList, err := c.store.ListByStatuses(ctx, []projects.ProjectStatus{
		projects.StatusRunning,
	})
	if err != nil {
		log.WithError(err).Error("Failed to list running projects for storage check")
		return
	}

	for _, p := range projectList {
		if err := c.checkProjectStorage(ctx, &p); err != nil {
			log.WithError(err).WithField("project", p.Name).
				Warn("Failed to check project storage")
		}
	}
}

// checkProjectStorage checks a single project's storage and expands if needed.
func (c *Controller) checkProjectStorage(ctx context.Context, p *projects.Project) error {
	cfg := c.cfg.ResourceConfig
	pvcName := cfg.PVCName(p.Name)

	// Get disk metrics from project pod
	diskUsed, diskLimit, err := c.getProjectDiskMetrics(ctx, p, cfg)
	if err != nil {
		return fmt.Errorf("get disk metrics: %w", err)
	}

	// Skip if we couldn't get valid metrics
	if diskLimit == 0 {
		log.WithField("project", p.Name).Debug("Skipping storage check: no disk limit reported")
		return nil
	}

	// Update Prometheus metrics
	metrics.PVCUsageBytes.WithLabelValues(p.Name, pvcName).Set(float64(diskUsed))
	metrics.PVCLimitBytes.WithLabelValues(p.Name, pvcName).Set(float64(diskLimit))

	usagePercent := float64(diskUsed) / float64(diskLimit) * 100
	metrics.PVCUsagePercent.WithLabelValues(p.Name, pvcName).Set(usagePercent)

	// Check if expansion needed
	usageFraction := float64(diskUsed) / float64(diskLimit)
	if usageFraction >= c.cfg.StorageMonitor.ExpandThreshold {
		log.WithFields(map[string]interface{}{
			"project":      p.Name,
			"pvc":          pvcName,
			"usagePercent": fmt.Sprintf("%.1f", usagePercent),
			"threshold":    fmt.Sprintf("%.0f", c.cfg.StorageMonitor.ExpandThreshold*100),
		}).Info("Storage usage above threshold, attempting PVC expansion")

		if err := c.expandPVCByAmount(ctx, p, cfg, pvcName); err != nil {
			// Truncate error message for metric label (avoid high cardinality)
			errReason := truncateErrorForMetric(err.Error())
			metrics.PVCExpansionsTotal.WithLabelValues(p.Name, pvcName, "failed").Inc()
			metrics.PVCExpansionFailedTotal.WithLabelValues(p.Name, pvcName, errReason).Inc()
			return fmt.Errorf("expand PVC: %w", err)
		}

		metrics.PVCExpansionsTotal.WithLabelValues(p.Name, pvcName, "success").Inc()
	}

	return nil
}

// projectMetricsResponse represents the /api/metrics response from project pods.
type projectMetricsResponse struct {
	Disk struct {
		Usage int64 `json:"usage"`
		Limit int64 `json:"limit"`
	} `json:"disk"`
}

// getProjectDiskMetrics fetches disk metrics from project pod's /api/metrics endpoint.
func (c *Controller) getProjectDiskMetrics(ctx context.Context, p *projects.Project, cfg ResourceConfig) (used, limit int64, err error) {
	resourceName := cfg.ResourceName(p.Name)
	metricsURL := fmt.Sprintf("http://%s.%s.svc:8080/api/metrics", resourceName, cfg.Namespace)

	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metricsURL, nil)
	if err != nil {
		return 0, 0, fmt.Errorf("create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0, 0, fmt.Errorf("fetch metrics: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, 0, fmt.Errorf("metrics endpoint returned status %d", resp.StatusCode)
	}

	var metricsResp projectMetricsResponse
	if err := json.NewDecoder(resp.Body).Decode(&metricsResp); err != nil {
		return 0, 0, fmt.Errorf("decode metrics: %w", err)
	}

	return metricsResp.Disk.Usage, metricsResp.Disk.Limit, nil
}

// isPVCExpansionInProgress checks if a PVC expansion is already pending.
// Returns true if spec.requests > status.capacity (expansion in progress).
func (c *Controller) isPVCExpansionInProgress(pvc *corev1.PersistentVolumeClaim) bool {
	specSize := pvc.Spec.Resources.Requests[corev1.ResourceStorage]

	// Check status.capacity - if it exists and is less than spec, expansion is pending
	if pvc.Status.Capacity != nil {
		statusSize := pvc.Status.Capacity[corev1.ResourceStorage]
		if specSize.Cmp(statusSize) > 0 {
			return true
		}
	}

	// Also check PVC conditions for "Resizing" or "FileSystemResizePending"
	for _, cond := range pvc.Status.Conditions {
		if cond.Type == corev1.PersistentVolumeClaimResizing ||
			cond.Type == corev1.PersistentVolumeClaimFileSystemResizePending {
			if cond.Status == corev1.ConditionTrue {
				return true
			}
		}
	}

	return false
}

// expandPVCByAmount expands the PVC by the configured fixed amount.
// It includes safeguards against runaway expansions.
func (c *Controller) expandPVCByAmount(ctx context.Context, p *projects.Project, cfg ResourceConfig, pvcName string) error {
	logger := log.WithFields(map[string]interface{}{
		"project": p.Name,
		"pvc":     pvcName,
	})

	// Get current PVC
	pvc, err := c.clientset.CoreV1().PersistentVolumeClaims(cfg.Namespace).Get(ctx, pvcName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get PVC: %w", err)
	}

	// Safeguard 1: Check if expansion is already in progress
	if c.isPVCExpansionInProgress(pvc) {
		specSize := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
		var statusSize resource.Quantity
		if pvc.Status.Capacity != nil {
			statusSize = pvc.Status.Capacity[corev1.ResourceStorage]
		}
		logger.WithFields(map[string]interface{}{
			"spec":   specSize.String(),
			"status": statusSize.String(),
		}).Info("PVC expansion already in progress, skipping")
		return nil
	}

	// Safeguard 2: Check cooldown - don't expand if we expanded recently
	if lastExpand, ok := c.lastExpansionTime[pvcName]; ok {
		if time.Since(lastExpand) < c.cfg.StorageMonitor.ExpandCooldown {
			logger.WithFields(map[string]interface{}{
				"lastExpand": lastExpand.Format(time.RFC3339),
				"cooldown":   c.cfg.StorageMonitor.ExpandCooldown,
				"remaining":  c.cfg.StorageMonitor.ExpandCooldown - time.Since(lastExpand),
			}).Info("PVC expansion on cooldown, skipping")
			return nil
		}
	}

	// Calculate new size
	currentSize := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
	expandAmount, err := resource.ParseQuantity(c.cfg.StorageMonitor.ExpandAmount)
	if err != nil {
		return fmt.Errorf("parse expand amount %q: %w", c.cfg.StorageMonitor.ExpandAmount, err)
	}

	newSize := currentSize.DeepCopy()
	newSize.Add(expandAmount)

	logger.WithFields(map[string]interface{}{
		"currentSize": currentSize.String(),
		"expandBy":    expandAmount.String(),
		"newSize":     newSize.String(),
	}).Info("Expanding PVC")

	// Update PVC
	pvc.Spec.Resources.Requests[corev1.ResourceStorage] = newSize
	if _, err := c.clientset.CoreV1().PersistentVolumeClaims(cfg.Namespace).Update(ctx, pvc, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("update PVC: %w", err)
	}

	// Record expansion time for cooldown tracking
	c.lastExpansionTime[pvcName] = time.Now()

	// Update metric for current size
	metrics.PVCCurrentSizeBytes.WithLabelValues(p.Name, pvcName).Set(float64(newSize.Value()))

	logger.Info("PVC expansion requested successfully")
	return nil
}

// truncateErrorForMetric truncates an error message to avoid high cardinality metrics.
func truncateErrorForMetric(errMsg string) string {
	const maxLen = 50
	if len(errMsg) <= maxLen {
		return errMsg
	}
	return errMsg[:maxLen]
}
