import { TabMetrics, getMetricStatus } from '../types';

type MetricsIndicatorProps = {
  metrics?: TabMetrics;
};

const MetricsIndicator = ({ metrics }: MetricsIndicatorProps) => {
  if (!metrics) {
    return (
      <div className="metrics-indicator">
        <span className="metric-dot unknown" title="CPU: N/A"></span>
        <span className="metric-dot unknown" title="Memory: N/A"></span>
        <span className="metric-dot unknown" title="Disk: N/A"></span>
        <span className="metric-dot unknown" title="Network: N/A"></span>
      </div>
    );
  }

  const cpuStatus = getMetricStatus(metrics.cpu.percent);
  const memStatus = getMetricStatus(metrics.memory.percent);
  const diskStatus = getMetricStatus(metrics.disk.percent);

  // Network status based on combined rate (over 100MB/s = critical, 50MB/s = warning)
  const netRate = metrics.network.rxRate + metrics.network.txRate;
  const netStatus = netRate > 100_000_000 ? 'critical' : netRate > 50_000_000 ? 'warning' : 'healthy';

  return (
    <div className="metrics-indicator">
      <span
        className={`metric-dot ${cpuStatus}`}
        title={`CPU: ${metrics.cpu.percent}%`}
      ></span>
      <span
        className={`metric-dot ${memStatus}`}
        title={`Memory: ${metrics.memory.percent}%`}
      ></span>
      <span
        className={`metric-dot ${diskStatus}`}
        title={`Disk: ${metrics.disk.percent}%`}
      ></span>
      <span
        className={`metric-dot ${netStatus}`}
        title={`Network: ↓${formatBytes(metrics.network.rxRate)}/s ↑${formatBytes(metrics.network.txRate)}/s`}
      ></span>
    </div>
  );
};

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
}

export default MetricsIndicator;
