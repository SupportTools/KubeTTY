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
