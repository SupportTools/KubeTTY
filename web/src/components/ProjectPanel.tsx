import { useMemo, useState } from 'react';
import { ProjectInfo } from '../types';

export type ProjectPanelProps = {
  projects: ProjectInfo[];
  gatewayState: 'unknown' | 'enabled' | 'disabled';
  onSelect: (projectId: string) => void;
  selectedProjectId?: string | null;
  onOpenAdmin?: () => void;
  onOpenDashboard?: () => void;
};

type Filter = 'all' | 'failing' | 'paused';

const ITEMS_TO_SHOW = 25;

const isProjectSelectable = (project: ProjectInfo): boolean => {
  if (!project.lifecycleStatus) return true;
  return project.lifecycleStatus === 'running' && !project.paused;
};

const getUnavailableReason = (project: ProjectInfo): string | null => {
  if (!project.lifecycleStatus) return null;
  if (project.paused) return 'Paused by admin';
  switch (project.lifecycleStatus) {
    case 'pending':
      return 'Pending provisioning';
    case 'syncing':
      return 'Syncing template';
    case 'creating':
      return 'Creating resources';
    case 'updating':
      return 'Applying updates';
    case 'failed':
      return 'Failed provisioning';
    case 'deleting':
    case 'deleted':
      return 'Removed';
    default:
      return 'Unavailable';
  }
};

const getStatusLabel = (project: ProjectInfo): string => {
  if (!isProjectSelectable(project)) {
    if (project.paused) return 'Paused';
    return project.lifecycleStatus ? capitalize(project.lifecycleStatus) : 'Unavailable';
  }
  switch (project.status) {
    case 'online':
      return 'Online';
    case 'degraded':
      return 'Degraded';
    case 'offline':
      return 'Offline';
    default:
      return 'Unknown';
  }
};

const getStatusClass = (project: ProjectInfo): string => {
  if (!isProjectSelectable(project)) {
    if (project.paused) return 'status-paused';
    switch (project.lifecycleStatus) {
      case 'pending':
      case 'syncing':
      case 'creating':
      case 'updating':
        return 'status-pending';
      case 'failed':
        return 'status-failed';
      case 'deleting':
      case 'deleted':
        return 'status-offline';
      default:
        return 'status-unknown';
    }
  }
  switch (project.status) {
    case 'online':
      return 'status-online';
    case 'degraded':
      return 'status-degraded';
    case 'offline':
      return 'status-offline';
    default:
      return 'status-unknown';
  }
};

const getStatusPriority = (project: ProjectInfo): number => {
  if (!isProjectSelectable(project)) {
    if (project.lifecycleStatus === 'failed') return 0;
    return 10;
  }
  switch (project.status) {
    case 'online':
      return 1;
    case 'degraded':
      return 2;
    case 'offline':
      return 3;
    default:
      return 4;
  }
};

const formatRelativeTime = (timestamp?: string): string => {
  if (!timestamp) return 'No checks yet';
  const target = new Date(timestamp).getTime();
  if (Number.isNaN(target)) return 'Unknown';
  const delta = Date.now() - target;
  if (delta < 0) return 'Moments ago';
  const minutes = Math.round(delta / 60000);
  if (minutes < 1) return 'Now';
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.round(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.round(hours / 24);
  return `${days}d ago`;
};

const capitalize = (value: string): string => value.charAt(0).toUpperCase() + value.slice(1);

const ProjectPanel = ({
  projects,
  gatewayState,
  onSelect,
  selectedProjectId,
  onOpenAdmin,
  onOpenDashboard,
}: ProjectPanelProps) => {
  const [filter, setFilter] = useState<Filter>('all');
  const [searchQuery, setSearchQuery] = useState('');
  const gatewayDisabled = gatewayState !== 'enabled';

  const filteredProjects = useMemo(() => {
    const query = searchQuery.trim().toLowerCase();
    return projects
      .filter((project) => {
        if (filter === 'failing' && project.status !== 'degraded' && project.status !== 'offline') {
          return false;
        }
        if (filter === 'paused' && !project.paused) {
          return false;
        }
        if (!query) return true;
        return (
          (project.displayName || project.id).toLowerCase().includes(query) ||
          project.namespace?.toLowerCase().includes(query) ||
          project.description?.toLowerCase().includes(query)
        );
      })
      .sort((a, b) => {
        const priorityDiff = getStatusPriority(a) - getStatusPriority(b);
        if (priorityDiff !== 0) {
          return priorityDiff;
        }
        return (a.displayName || a.id).localeCompare(b.displayName || b.id);
      })
      .slice(0, ITEMS_TO_SHOW);
  }, [projects, filter, searchQuery]);

  const totalFailing = projects.filter((p) => p.status === 'offline' || p.status === 'degraded' || p.lifecycleStatus === 'failed').length;
  const totalPaused = projects.filter((p) => p.paused).length;

  return (
    <aside className="project-panel">
      <div className="project-panel-header">
        <div>
          <p className="panel-label">Gateway</p>
          <p className={`gateway-state state-${gatewayState}`}>
            <span className="state-dot" />
            {gatewayState === 'enabled' ? 'Ready for sessions' : gatewayState === 'disabled' ? 'Disabled' : 'Detecting…'}
          </p>
        </div>
        <div className="panel-actions">
          <button className="panel-link" onClick={() => onOpenDashboard?.()}>
            Dashboard
          </button>
          <button className="panel-link" onClick={() => onOpenAdmin?.()}>
            Projects
          </button>
        </div>
      </div>

      <div className="project-panel-search">
        <input
          type="search"
          placeholder="Search projects"
          value={searchQuery}
          onChange={(event) => setSearchQuery(event.target.value)}
        />
      </div>

      <div className="project-panel-filters">
        <button
          className={`filter-pill ${filter === 'all' ? 'active' : ''}`}
          onClick={() => setFilter('all')}
        >
          All ({projects.length})
        </button>
        <button
          className={`filter-pill ${filter === 'failing' ? 'active' : ''}`}
          onClick={() => setFilter('failing')}
        >
          Failing ({totalFailing})
        </button>
        <button
          className={`filter-pill ${filter === 'paused' ? 'active' : ''}`}
          onClick={() => setFilter('paused')}
        >
          Paused ({totalPaused})
        </button>
      </div>

      <div className="project-list">
        {filteredProjects.map((project) => {
          const statusLabel = getStatusLabel(project);
          const statusClass = getStatusClass(project);
          const disabled = gatewayDisabled || !isProjectSelectable(project);
          const reason = getUnavailableReason(project);
          const isActive = selectedProjectId === project.id;
          return (
            <button
              key={project.id}
              className={`project-card ${statusClass} ${isActive ? 'active' : ''}`}
              onClick={() => !disabled && onSelect(project.id)}
              disabled={disabled}
            >
              <div className="project-card-header">
                <div>
                  <p className="project-name">{project.displayName || project.id}</p>
                  <p className="project-namespace">{project.namespace || 'Unknown namespace'}</p>
                </div>
                <span className={`status-pill ${statusClass}`}>{statusLabel}</span>
              </div>
              <p className="project-description">{project.description || 'No description provided.'}</p>
              <div className="project-meta">
                <span>{formatRelativeTime(project.lastCheckedAt)}</span>
                {gatewayDisabled && <span className="project-reason">Gateway disabled</span>}
                {reason && <span className="project-reason">{reason}</span>}
              </div>
            </button>
          );
        })}
        {filteredProjects.length === 0 && (
          <div className="project-empty">
            <p>No projects match your filters.</p>
            <p className="project-empty-hint">Try clearing the search or switching filters.</p>
          </div>
        )}
      </div>
    </aside>
  );
};

export default ProjectPanel;
