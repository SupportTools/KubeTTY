import { TabMetrics, getMetricStatus } from '../types';

type StatusBarProps = {
  tabLabel: string;
  metrics?: TabMetrics;
};

const StatusBar = ({ tabLabel, metrics }: StatusBarProps) => {
  if (!metrics) {
    return (
      <div className="status-bar">
        <span className="status-bar-label">{tabLabel}</span>
        <span className="status-bar-item">Metrics loading...</span>
      </div>
    );
  }

  return (
    <div className="status-bar">
      <span className="status-bar-label">{tabLabel}</span>
      <MetricBar
        label="CPU"
        percent={metrics.cpu.percent}
        value={formatCPU(metrics.cpu.usage)}
        limit={formatCPU(metrics.cpu.limit)}
      />
      <MetricBar
        label="MEM"
        percent={metrics.memory.percent}
        value={formatBytes(metrics.memory.usage)}
        limit={formatBytes(metrics.memory.limit)}
      />
      <MetricBar
        label="DISK"
        percent={metrics.disk.percent}
        value={formatBytes(metrics.disk.usage)}
        limit={formatBytes(metrics.disk.limit)}
      />
      <NetworkDisplay
        rxRate={metrics.network.rxRate}
        txRate={metrics.network.txRate}
      />
    </div>
  );
};

type MetricBarProps = {
  label: string;
  percent: number;
  value: string;
  limit: string;
};

const MetricBar = ({ label, percent, value, limit }: MetricBarProps) => {
  const status = getMetricStatus(percent);
  const clampedPercent = Math.min(100, Math.max(0, percent));

  return (
    <div className="metric-bar-container">
      <span className="metric-bar-label">{label}</span>
      <div className="metric-bar">
        <div
          className={`metric-bar-fill ${status}`}
          style={{ width: `${clampedPercent}%` }}
        />
      </div>
      <span className="metric-bar-value" title={`${value} / ${limit}`}>
        {percent}%
      </span>
    </div>
  );
};

type NetworkDisplayProps = {
  rxRate: number;
  txRate: number;
};

const NetworkDisplay = ({ rxRate, txRate }: NetworkDisplayProps) => {
  return (
    <div className="network-display">
      <span className="network-label">NET</span>
      <span className="network-value">
        <span className="network-down">↓{formatBytesRate(rxRate)}</span>
        <span className="network-up">↑{formatBytesRate(txRate)}</span>
      </span>
    </div>
  );
};

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
}

function formatBytesRate(bytesPerSec: number): string {
  if (bytesPerSec === 0) return '0 B/s';
  const k = 1024;
  const sizes = ['B/s', 'KB/s', 'MB/s', 'GB/s'];
  const i = Math.floor(Math.log(bytesPerSec) / Math.log(k));
  return parseFloat((bytesPerSec / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
}

function formatCPU(millicores: number): string {
  if (millicores >= 1000) {
    return (millicores / 1000).toFixed(1) + ' CPU';
  }
  return millicores + 'm';
}

export default StatusBar;
