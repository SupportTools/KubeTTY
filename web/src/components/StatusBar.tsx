import { TabMetrics, PodMetadata, getMetricStatus } from '../types';

type StatusBarProps = {
  tabLabel: string;
  tabStatus: 'connecting' | 'connected' | 'reconnecting' | 'closed';
  metrics?: TabMetrics;
  namespace?: string | null;
  createdAt?: string;
  updatedAt?: string;
  lastError?: string | null;
};

const StatusBar = ({ tabLabel, tabStatus, metrics, namespace, createdAt, updatedAt, lastError }: StatusBarProps) => {
  return (
    <div className="status-bar session-hud">
      <div className="session-overview">
        <div className="session-heading">
          <span className="status-bar-label">{tabLabel}</span>
          <span className={`status-pill status-pill-${tabStatus}`}>{formatStatusLabel(tabStatus)}</span>
          {namespace && <span className="session-namespace">{namespace}</span>}
          {createdAt && <span className="session-age">Age {formatDuration(createdAt)}</span>}
          {updatedAt && <span className="session-updated">Updated {formatRelativeTime(updatedAt)}</span>}
        </div>
        {lastError && <span className="session-error">{lastError}</span>}
      </div>
      {metrics ? (
        <div className="session-metrics">
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
          {metrics.metadata && (
            <MetadataDisplay metadata={metrics.metadata} />
          )}
        </div>
      ) : (
        <span className="status-bar-item">Metrics loading…</span>
      )}
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

type MetadataDisplayProps = {
  metadata: PodMetadata;
};

const MetadataDisplay = ({ metadata }: MetadataDisplayProps) => {
  const tooltip = [
    `Pod: ${metadata.podName}`,
    `Node: ${metadata.nodeName}`,
    `Namespace: ${metadata.namespace}`,
    `IP: ${metadata.podIP}`
  ].join('\n');

  return (
    <div className="metadata-display" title={tooltip}>
      <span className="metadata-label">NODE</span>
      <span className="metadata-value">{metadata.nodeName}</span>
    </div>
  );
};

const formatStatusLabel = (status: StatusBarProps['tabStatus']) => {
  switch (status) {
    case 'connecting':
      return 'Connecting';
    case 'reconnecting':
      return 'Reconnecting';
    case 'closed':
      return 'Closed';
    default:
      return 'Connected';
  }
};

const formatDuration = (timestamp: string) => {
  const ms = Date.now() - new Date(timestamp).getTime();
  if (Number.isNaN(ms) || ms < 0) {
    return '—';
  }
  const minutes = Math.floor(ms / 60000);
  if (minutes < 1) return '<1m';
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h`;
  const days = Math.floor(hours / 24);
  return `${days}d`;
};

const formatRelativeTime = (timestamp: string) => {
  const target = new Date(timestamp).getTime();
  if (Number.isNaN(target)) return 'Unknown';
  const delta = Date.now() - target;
  if (delta < 0) return 'Seconds ago';
  const minutes = Math.floor(delta / 60000);
  if (minutes < 1) return 'Seconds ago';
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  return `${days}d ago`;
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
