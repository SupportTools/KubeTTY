export type SessionMeta = {
  sessionId: string;
  deploymentId: string;
  shellPid: number;
  createdAt: string;
  updatedAt: string;
  forkedFrom?: string | null;
  attachedTo?: string | null;
  attachedAt?: string | null;
  lastLogAt?: string | null;
  logCount?: number;
};

export type SessionResponse = {
  sessionUUID: string;
  sessions: SessionMeta[];
};

export type SessionLogEntry = {
  sessionId: string;
  direction: string;
  data: string;
  createdAt: string;
};

export type SessionLogsResponse = {
  sessionId: string;
  logs: SessionLogEntry[];
  matchCount: number;
};

export type ProjectHealthStatus = 'online' | 'degraded' | 'offline' | 'unknown';

// GUI view modes for split-view UI
export type ViewMode = 'terminal' | 'gui' | 'split-horizontal' | 'split-vertical';

// Tab Resource Metrics Types
export type ResourceMetric = {
  usage: number;    // Current usage in bytes (memory/disk) or millicores (CPU)
  limit: number;    // Limit/capacity in same units
  percent: number;  // Usage percentage (0-100)
};

export type NetworkMetric = {
  rxBytes: number;  // Total bytes received
  txBytes: number;  // Total bytes transmitted
  rxRate: number;   // Receive rate in bytes/sec
  txRate: number;   // Transmit rate in bytes/sec
};

export type PodMetadata = {
  podName: string;    // Name of the pod running the terminal
  nodeName: string;   // Name of the node where pod is scheduled
  namespace: string;  // Kubernetes namespace
  podIP: string;      // Pod IP address
};

export type TabMetrics = {
  cpu: ResourceMetric;
  memory: ResourceMetric;
  disk: ResourceMetric;
  network: NetworkMetric;
  metadata?: PodMetadata;  // Optional pod/node metadata
  updatedAt: string;
};

export type MetricStatus = 'healthy' | 'warning' | 'critical';

export function getMetricStatus(percent: number): MetricStatus {
  if (percent >= 80) return 'critical';
  if (percent >= 60) return 'warning';
  return 'healthy';
}

// Project lifecycle status (from database)
export type ProjectLifecycleStatus = 'pending' | 'syncing' | 'creating' | 'running' | 'updating' | 'failed' | 'deleting' | 'deleted';

export type ProjectInfo = {
  id: string;
  displayName: string;
  namespace: string;
  service: string;
  port: number;
  description?: string;
  icon?: string;
  tags?: string[];
  lifecycleStatus?: ProjectLifecycleStatus; // pending, syncing, running, failed, etc.
  paused?: boolean;
  status?: ProjectHealthStatus; // health status: online, offline, degraded, unknown
  lastCheckedAt?: string;
  // GUI desktop support
  guiEnabled?: boolean;
  guiResolution?: string;
  guiVNCPort?: number;
};

export type ProjectsResponse = {
  projects: ProjectInfo[];
};

export type GatewayTab = {
  tabId: string;
  projectId: string;
  clientId: string;
  status: 'connecting' | 'connected' | 'reconnecting' | 'closed';
  position: number;
  createdAt: string;
  updatedAt: string;
  lastError?: string;
  downstreamUri?: string;
  metrics?: TabMetrics;
};

export type TabsResponse = {
  tabs: GatewayTab[];
};

export type TabEvent =
  | { type: "snapshot"; tabs: GatewayTab[] }
  | { type: "update"; tab: GatewayTab }
  | { type: "delete"; tabId: string }
  | { type: "metrics"; tabId: string; metrics: TabMetrics };

export interface ErrorResponse {
  status: number;
  error: string;
  message: string;
  details?: string;
}

// Admin Project Types
export type AdminProjectStatus =
  | 'pending'
  | 'syncing'
  | 'creating'
  | 'running'
  | 'updating'
  | 'failed'
  | 'deleting'
  | 'deleted';

export interface AdminProject {
  id: string;
  name: string;
  displayName: string;
  description?: string;
  icon?: string;
  targetNamespace: string;
  sessionId: string;
  userName: string;
  cpuRequest: string;
  cpuLimit: string;
  memoryRequest: string;
  memoryLimit: string;
  storageSize: string;
  storageClass: string;
  adminNamespaces: string[];
  readNamespaces: string[];
  maxTabsPerClient: number;
  maxTabsTotal: number;
  dindEnabled: boolean;
  // GUI desktop support
  guiEnabled: boolean;
  guiResolution?: string;
  guiVNCPort?: number;
  // PTY session logging for Loki integration
  ptyLoggingEnabled: boolean;
  ptyLoggingMaxSize?: number;       // Max file size before rotation (bytes)
  ptyLoggingMaxBackups?: number;    // Rotated files to keep
  ptyLoggingBufferSize?: number;    // Write buffer size (bytes)
  ptyLoggingFlushInterval?: string; // Flush interval (e.g., "5s")
  ptyLoggingIncludeRaw?: boolean;   // Include base64 raw bytes
  envVars?: Record<string, string>;
  imageRepository: string;
  imageTag: string;
  status: AdminProjectStatus;
  statusMessage?: string;
  lastHealthCheck?: string;
  lastActivity?: string;
  podIP?: string;
  paused: boolean;
  createdAt: string;
  updatedAt: string;
  deletedAt?: string;
}

export interface CreateProjectRequest {
  name: string;
  displayName: string;
  description?: string;
  icon?: string;
  userName: string;
  cpuRequest?: string;
  cpuLimit?: string;
  memoryRequest?: string;
  memoryLimit?: string;
  storageSize?: string;
  storageClass?: string;
  adminNamespaces?: string[];
  readNamespaces?: string[];
  maxTabsPerClient?: number;
  maxTabsTotal?: number;
  dindEnabled?: boolean;
  // GUI desktop support
  guiEnabled?: boolean;
  guiResolution?: string;
  guiVNCPort?: number;
  // PTY session logging for Loki integration
  ptyLoggingEnabled?: boolean;
  ptyLoggingMaxSize?: number;       // Max file size before rotation (bytes)
  ptyLoggingMaxBackups?: number;    // Rotated files to keep
  ptyLoggingBufferSize?: number;    // Write buffer size (bytes)
  ptyLoggingFlushInterval?: string; // Flush interval (e.g., "5s")
  ptyLoggingIncludeRaw?: boolean;   // Include base64 raw bytes
  envVars?: Record<string, string>;
  imageRepository?: string;
  imageTag?: string;
}

export interface UpdateProjectRequest {
  displayName?: string;
  description?: string;
  icon?: string;
  cpuRequest?: string;
  cpuLimit?: string;
  memoryRequest?: string;
  memoryLimit?: string;
  maxTabsPerClient?: number;
  maxTabsTotal?: number;
  dindEnabled?: boolean;
  // GUI desktop support
  guiEnabled?: boolean;
  guiResolution?: string;
  guiVNCPort?: number;
  // PTY session logging for Loki integration
  ptyLoggingEnabled?: boolean;
  ptyLoggingMaxSize?: number;       // Max file size before rotation (bytes)
  ptyLoggingMaxBackups?: number;    // Rotated files to keep
  ptyLoggingBufferSize?: number;    // Write buffer size (bytes)
  ptyLoggingFlushInterval?: string; // Flush interval (e.g., "5s")
  ptyLoggingIncludeRaw?: boolean;   // Include base64 raw bytes
  envVars?: Record<string, string>;
  imageTag?: string;
}

export interface AdminProjectsResponse {
  projects: AdminProject[];
  total: number;
}

export interface DeploymentStatus {
  namespace: string;
  deploymentName: string;
  ready: boolean;
  replicas: number;
  readyReplicas: number;
  availableReplicas: number;
  pods: PodStatus[];
}

export interface PodStatus {
  name: string;
  phase: string;
  ready: boolean;
  ip: string;
  restarts: number;
  age: string;
}

export interface ProjectStatusResponse {
  project: AdminProject;
  deployment: DeploymentStatus;
}

export interface UpgradeInfoResponse {
  currentVersion: string;
  recommendedVersion?: string;
  lastActivity?: string;
  minutesSinceActivity?: number;
}

export interface UpgradeProjectRequest {
  imageTag: string;
}

// Dashboard Types
export interface DashboardSummary {
  activeConnections: number;
  projects: {
    running: number;
    failed: number;
    total: number;
  };
  tabs: {
    active: number;
    total: number;
  };
  last24h: {
    connections: number;
    disconnects: number;
    errors: number;
    errorRate: number;
  };
}

export interface ConnectionDataPoint {
  timestamp: string;
  active: number;
  connects: number;
  disconnects: number;
}

export interface DashboardMetrics {
  period: string;
  connectionTimeseries: ConnectionDataPoint[];
  disconnectsByReason: Record<string, number>;
  flowControlPauses: number;
  writeErrors: number;
}

export interface DashboardError {
  type: 'disconnect' | 'project_failed' | 'tab_error';
  reason: string;
  projectId: string;
  projectName: string;
  tabId?: string;
  timestamp: string;
  details?: string;
}

export interface DashboardErrorsResponse {
  errors: DashboardError[];
  total: number;
}

export interface HourlyCount {
  hour: string;
  count: number;
}

export interface ProjectUsage {
  projectId: string;
  name: string;
  displayName: string;
  connections: number;
}

export interface DashboardUsage {
  period: string;
  hourlyConnections: HourlyCount[];
  topProjects: ProjectUsage[];
  peakHour: string;
  avgSessionDuration: number;
}

// Settings Types
export type SettingCategory =
  | 'project_defaults'
  | 'auth'
  | 'features'
  | 'ui'
  | 'controller'
  | 'notifications'
  | 'secrets';

export type SettingValueType = 'string' | 'int' | 'bool' | 'json';

export interface Setting {
  id: string;
  category: SettingCategory;
  key: string;
  value: unknown;
  valueType: SettingValueType;
  displayName: string;
  description?: string;
  isSensitive: boolean;
  isReadonly: boolean;
  validation?: unknown;
  createdAt: string;
  updatedAt: string;
}

export interface SettingHistory {
  id: string;
  settingId?: string;
  category: SettingCategory;
  key: string;
  oldValue?: unknown;
  newValue?: unknown;
  changeType: 'insert' | 'update' | 'delete';
  changedBy: string;
  changedAt: string;
  changeSource: string;
  changeReason?: string;
  clientIp?: string;
  userAgent?: string;
}

export interface SettingsResponse {
  settings: Setting[];
  categories: Record<string, number>;
  total: number;
}

export interface SettingCategoryInfo {
  name: SettingCategory;
  displayName: string;
}

export interface UpdateSettingRequest {
  value: unknown;
  changeReason?: string;
}

export interface SettingHistoryResponse {
  history: SettingHistory[];
  category?: SettingCategory;
  key?: string;
  total: number;
}
