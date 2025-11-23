// Package metrics provides resource metrics collection for gateway tabs.
package metrics

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	metricsv1beta1 "k8s.io/metrics/pkg/client/clientset/versioned"
)

// TabInfo represents the information needed to collect metrics for a tab.
type TabInfo struct {
	TabID         string
	ProjectID     string
	PodName       string
	Namespace     string
	DownstreamURI string // Base URL for project pod (e.g., http://pod-ip:8080)
	CPULimit      int64  // CPU limit in millicores
	MemoryLimit   int64  // Memory limit in bytes
}

// Callback is called when metrics are updated for a tab.
type Callback func(tabID string, metrics TabMetrics)

// Collector collects resource metrics for tabs from Kubernetes and project pods.
type Collector struct {
	metricsClient *metricsv1beta1.Clientset
	httpClient    *http.Client
	interval      time.Duration
	callback      Callback

	mu   sync.RWMutex
	tabs map[string]TabInfo // tabID -> TabInfo

	// Track previous network bytes for rate calculation
	prevNetwork   map[string]NetworkMetric
	prevNetworkTs map[string]time.Time

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewCollector creates a new metrics collector.
func NewCollector(interval time.Duration, callback Callback) (*Collector, error) {
	// Create in-cluster Kubernetes config
	config, err := rest.InClusterConfig()
	if err != nil {
		log.WithError(err).Warn("Failed to create in-cluster config, metrics collection disabled")
		return &Collector{
			httpClient:    &http.Client{Timeout: 5 * time.Second},
			interval:      interval,
			callback:      callback,
			tabs:          make(map[string]TabInfo),
			prevNetwork:   make(map[string]NetworkMetric),
			prevNetworkTs: make(map[string]time.Time),
		}, nil
	}

	// Create metrics client
	metricsClient, err := metricsv1beta1.NewForConfig(config)
	if err != nil {
		log.WithError(err).Warn("Failed to create metrics client, K8s metrics disabled")
		return &Collector{
			httpClient:    &http.Client{Timeout: 5 * time.Second},
			interval:      interval,
			callback:      callback,
			tabs:          make(map[string]TabInfo),
			prevNetwork:   make(map[string]NetworkMetric),
			prevNetworkTs: make(map[string]time.Time),
		}, nil
	}

	return &Collector{
		metricsClient: metricsClient,
		httpClient:    &http.Client{Timeout: 5 * time.Second},
		interval:      interval,
		callback:      callback,
		tabs:          make(map[string]TabInfo),
		prevNetwork:   make(map[string]NetworkMetric),
		prevNetworkTs: make(map[string]time.Time),
	}, nil
}

// Start begins the metrics collection loop.
func (c *Collector) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel

	c.wg.Add(1)
	go c.collectLoop(ctx)
	log.Info("Metrics collector started")
}

// Stop stops the metrics collection loop.
func (c *Collector) Stop() {
	if c.cancel != nil {
		c.cancel()
		c.wg.Wait()
		log.Info("Metrics collector stopped")
	}
}

// RegisterTab adds a tab to be monitored for metrics.
func (c *Collector) RegisterTab(info TabInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tabs[info.TabID] = info
	log.WithField("tabId", info.TabID).Debug("Registered tab for metrics collection")
}

// UnregisterTab removes a tab from metrics collection.
func (c *Collector) UnregisterTab(tabID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.tabs, tabID)
	delete(c.prevNetwork, tabID)
	delete(c.prevNetworkTs, tabID)
	log.WithField("tabId", tabID).Debug("Unregistered tab from metrics collection")
}

// collectLoop runs the periodic metrics collection.
func (c *Collector) collectLoop(ctx context.Context) {
	defer c.wg.Done()

	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	// Collect immediately on start
	c.collectAll(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.collectAll(ctx)
		}
	}
}

// collectAll collects metrics for all registered tabs.
func (c *Collector) collectAll(ctx context.Context) {
	c.mu.RLock()
	tabs := make([]TabInfo, 0, len(c.tabs))
	for _, t := range c.tabs {
		tabs = append(tabs, t)
	}
	c.mu.RUnlock()

	for _, tab := range tabs {
		metrics := c.collectTabMetrics(ctx, tab)
		if c.callback != nil {
			c.callback(tab.TabID, metrics)
		}
	}
}

// collectTabMetrics collects all metrics for a single tab.
func (c *Collector) collectTabMetrics(ctx context.Context, tab TabInfo) TabMetrics {
	metrics := TabMetrics{
		UpdatedAt: time.Now(),
	}

	// Collect CPU/Memory from Kubernetes metrics-server
	if c.metricsClient != nil && tab.PodName != "" && tab.Namespace != "" {
		k8sMetrics := c.collectK8sMetrics(ctx, tab)
		metrics.CPU = k8sMetrics.CPU
		metrics.Memory = k8sMetrics.Memory
	}

	// Collect Disk/Network from project pod endpoint
	if tab.DownstreamURI != "" {
		podMetrics := c.collectPodMetrics(ctx, tab)
		metrics.Disk = podMetrics.Disk
		metrics.Network = podMetrics.Network
	}

	return metrics
}

// collectK8sMetrics fetches CPU and memory metrics from Kubernetes metrics-server.
func (c *Collector) collectK8sMetrics(ctx context.Context, tab TabInfo) K8sMetrics {
	result := K8sMetrics{}

	podMetrics, err := c.metricsClient.MetricsV1beta1().PodMetricses(tab.Namespace).Get(ctx, tab.PodName, metav1.GetOptions{})
	if err != nil {
		log.WithError(err).WithFields(log.Fields{
			"tabId":     tab.TabID,
			"pod":       tab.PodName,
			"namespace": tab.Namespace,
		}).Debug("Failed to get pod metrics from metrics-server")
		return result
	}

	// Sum metrics from all containers
	var cpuUsage, memUsage int64
	for _, container := range podMetrics.Containers {
		cpuUsage += container.Usage.Cpu().MilliValue()
		memUsage += container.Usage.Memory().Value()
	}

	// Calculate percentages
	result.CPU = ResourceMetric{
		Usage: cpuUsage,
		Limit: tab.CPULimit,
	}
	if tab.CPULimit > 0 {
		result.CPU.Percent = int((cpuUsage * 100) / tab.CPULimit)
	}

	result.Memory = ResourceMetric{
		Usage: memUsage,
		Limit: tab.MemoryLimit,
	}
	if tab.MemoryLimit > 0 {
		result.Memory.Percent = int((memUsage * 100) / tab.MemoryLimit)
	}

	return result
}

// collectPodMetrics fetches disk and network metrics from the project pod.
func (c *Collector) collectPodMetrics(ctx context.Context, tab TabInfo) PodMetrics {
	result := PodMetrics{}

	url := fmt.Sprintf("%s/api/metrics", tab.DownstreamURI)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		log.WithError(err).WithField("tabId", tab.TabID).Debug("Failed to create metrics request")
		return result
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		log.WithError(err).WithField("tabId", tab.TabID).Debug("Failed to fetch pod metrics")
		return result
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.WithFields(log.Fields{
			"tabId":  tab.TabID,
			"status": resp.StatusCode,
		}).Debug("Pod metrics endpoint returned non-200 status")
		return result
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.WithError(err).WithField("tabId", tab.TabID).Debug("Failed to decode pod metrics")
		return result
	}

	// Calculate network rates
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	if prev, ok := c.prevNetwork[tab.TabID]; ok {
		if prevTs, ok := c.prevNetworkTs[tab.TabID]; ok {
			elapsed := now.Sub(prevTs).Seconds()
			if elapsed > 0 {
				result.Network.RxRate = int64(float64(result.Network.RxBytes-prev.RxBytes) / elapsed)
				result.Network.TxRate = int64(float64(result.Network.TxBytes-prev.TxBytes) / elapsed)
				// Ensure rates are non-negative
				if result.Network.RxRate < 0 {
					result.Network.RxRate = 0
				}
				if result.Network.TxRate < 0 {
					result.Network.TxRate = 0
				}
			}
		}
	}
	c.prevNetwork[tab.TabID] = result.Network
	c.prevNetworkTs[tab.TabID] = now

	return result
}
