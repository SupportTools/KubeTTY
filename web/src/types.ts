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

export type ProjectInfo = {
  id: string;
  displayName: string;
  namespace: string;
  service: string;
  port: number;
  description?: string;
  icon?: string;
  tags?: string[];
  status?: ProjectHealthStatus;
  lastCheckedAt?: string;
};

export type ProjectsResponse = {
  projects: ProjectInfo[];
};

export type GatewayTab = {
  tabId: string;
  projectId: string;
  clientId: string;
  status: 'connecting' | 'connected' | 'reconnecting' | 'closed';
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
  envVars?: Record<string, string>;
  imageRepository: string;
  imageTag: string;
  status: AdminProjectStatus;
  statusMessage?: string;
  lastHealthCheck?: string;
  lastActivity?: string;
  podIP?: string;
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
