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
};

export type ProjectInfo = {
  id: string;
  displayName: string;
  namespace: string;
  service: string;
  port: number;
  description?: string;
  icon?: string;
  tags?: string[];
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
};

export type TabsResponse = {
  tabs: GatewayTab[];
};

export type TabEvent =
  | { type: "snapshot"; tabs: GatewayTab[] }
  | { type: "update"; tab: GatewayTab }
  | { type: "delete"; tabId: string };
