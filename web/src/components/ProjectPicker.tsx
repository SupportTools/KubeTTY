import { useState, useMemo } from 'react';
import { ProjectInfo, ProjectHealthStatus, SessionMode } from '../types';

type Props = {
  projects: ProjectInfo[];
  existingTabs: Array<{ tabId: string; projectId: string }>;
  onSelect: (selection: {
    projectId: string;
    openAction?: 'create_new' | 'attach_recent' | 'attach_specific';
    existingTabId?: string;
  }) => void;
  onClose: () => void;
};

type SortField = 'name' | 'namespace' | 'status';
type SortDirection = 'asc' | 'desc';

const ITEMS_PER_PAGE = 5;

const defaultOpenActionForMode = (mode?: SessionMode): 'create_new' | 'attach_recent' => {
  switch (mode) {
    case 'exclusive_takeover':
      return 'attach_recent';
    case 'independent_shells':
      return 'create_new';
    case 'shared_concurrent':
      return 'create_new';
    default:
      return 'attach_recent';
  }
};

const createActionLabelForMode = (mode?: SessionMode): string => {
  switch (mode) {
    case 'independent_shells':
      return 'New Shell';
    case 'exclusive_takeover':
      return 'New Session';
    case 'shared_concurrent':
      return 'New Session';
    default:
      return 'New Session';
  }
};

const getStatusClass = (status?: ProjectHealthStatus): string => {
  switch (status) {
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

const getStatusLabel = (status?: ProjectHealthStatus): string => {
  switch (status) {
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

const getStatusPriority = (status?: ProjectHealthStatus): number => {
  switch (status) {
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

const ProjectPicker = ({ projects, existingTabs, onSelect, onClose }: Props) => {
  const [searchQuery, setSearchQuery] = useState('');
  const [sortField, setSortField] = useState<SortField>('name');
  const [sortDirection, setSortDirection] = useState<SortDirection>('asc');
  const [currentPage, setCurrentPage] = useState(1);
  const [expandedProjectId, setExpandedProjectId] = useState<string | null>(null);

  // Filter projects based on search query
  const filteredProjects = useMemo(() => {
    if (!searchQuery.trim()) return projects;
    const query = searchQuery.toLowerCase();
    return projects.filter(
      (project) =>
        (project.displayName || project.id).toLowerCase().includes(query) ||
        project.namespace?.toLowerCase().includes(query) ||
        project.description?.toLowerCase().includes(query)
    );
  }, [projects, searchQuery]);

  // Sort filtered projects
  const sortedProjects = useMemo(() => {
    const sorted = [...filteredProjects].sort((a, b) => {
      let comparison = 0;
      switch (sortField) {
        case 'name':
          comparison = (a.displayName || a.id).localeCompare(b.displayName || b.id);
          break;
        case 'namespace':
          comparison = (a.namespace || '').localeCompare(b.namespace || '');
          break;
        case 'status':
          comparison = getStatusPriority(a.status) - getStatusPriority(b.status);
          break;
      }
      return sortDirection === 'asc' ? comparison : -comparison;
    });
    return sorted;
  }, [filteredProjects, sortField, sortDirection]);

  // Paginate sorted projects
  const totalPages = Math.ceil(sortedProjects.length / ITEMS_PER_PAGE);
  const paginatedProjects = useMemo(() => {
    const start = (currentPage - 1) * ITEMS_PER_PAGE;
    return sortedProjects.slice(start, start + ITEMS_PER_PAGE);
  }, [sortedProjects, currentPage]);

  // Reset to page 1 when search or sort changes
  const handleSearchChange = (value: string) => {
    setSearchQuery(value);
    setCurrentPage(1);
  };

  const handleSortChange = (field: SortField) => {
    if (field === sortField) {
      setSortDirection((prev) => (prev === 'asc' ? 'desc' : 'asc'));
    } else {
      setSortField(field);
      setSortDirection('asc');
    }
    setCurrentPage(1);
  };

  const getSortIcon = (field: SortField) => {
    if (sortField !== field) return '';
    return sortDirection === 'asc' ? ' ▲' : ' ▼';
  };

  const tabsByProject = useMemo(() => {
    const map = new Map<string, string[]>();
    existingTabs.forEach((tab) => {
      const list = map.get(tab.projectId) ?? [];
      list.push(tab.tabId);
      map.set(tab.projectId, list);
    });
    return map;
  }, [existingTabs]);

  return (
    <div className="project-picker-backdrop">
      <div className="project-picker">
        <div className="picker-header">
          <h3>Select Project</h3>
          <button onClick={onClose} aria-label="Close picker">
            &times;
          </button>
        </div>

        {/* Search input */}
        <div className="picker-search">
          <input
            type="text"
            placeholder="Search projects..."
            value={searchQuery}
            onChange={(e) => handleSearchChange(e.target.value)}
            autoFocus
          />
        </div>

        {/* Sort controls */}
        <div className="picker-sort">
          <span className="sort-label">Sort by:</span>
          <button
            className={`sort-btn ${sortField === 'name' ? 'active' : ''}`}
            onClick={() => handleSortChange('name')}
          >
            Name{getSortIcon('name')}
          </button>
          <button
            className={`sort-btn ${sortField === 'namespace' ? 'active' : ''}`}
            onClick={() => handleSortChange('namespace')}
          >
            Namespace{getSortIcon('namespace')}
          </button>
          <button
            className={`sort-btn ${sortField === 'status' ? 'active' : ''}`}
            onClick={() => handleSortChange('status')}
          >
            Status{getSortIcon('status')}
          </button>
        </div>

        {/* Project list */}
        {paginatedProjects.length === 0 ? (
          <p className="picker-empty">
            {searchQuery ? 'No projects match your search.' : 'No projects available.'}
          </p>
        ) : (
          <ul>
            {paginatedProjects.map((project) => (
              <li key={project.id}>
                <button
                  className="project-option"
                  onClick={() =>
                    onSelect({
                      projectId: project.id,
                      openAction: defaultOpenActionForMode(project.sessionMode),
                    })
                  }
                >
                  <div className="project-info">
                    <div className="project-header">
                      <strong>{project.displayName || project.id}</strong>
                      <span
                        className={`status-indicator ${getStatusClass(project.status)}`}
                        title={getStatusLabel(project.status)}
                      />
                    </div>
                    <p>{project.description}</p>
                  </div>
                  <span className="project-namespace">{project.namespace}</span>
                </button>
                {((tabsByProject.get(project.id)?.length || 0) > 0) && (
                  <div className="picker-existing-tabs">
                    <button
                      className="page-btn"
                      onClick={() =>
                        onSelect({
                          projectId: project.id,
                          openAction: 'create_new',
                        })
                      }
                    >
                      {createActionLabelForMode(project.sessionMode)}
                    </button>
                    <button
                      className="page-btn"
                      onClick={() =>
                        onSelect({
                          projectId: project.id,
                          openAction: 'attach_recent',
                        })
                      }
                    >
                      Attach Most Recent
                    </button>
                    <button
                      className="page-btn"
                      onClick={() =>
                        setExpandedProjectId((current) =>
                          current === project.id ? null : project.id
                        )
                      }
                    >
                      {expandedProjectId === project.id ? 'Hide Tabs' : 'Choose Existing Tab'}
                    </button>
                  </div>
                )}
                {expandedProjectId === project.id && (
                  <div className="picker-tab-list">
                    {(tabsByProject.get(project.id) || []).map((tabId) => (
                      <button
                        key={tabId}
                        className="page-btn"
                        onClick={() =>
                          onSelect({
                            projectId: project.id,
                            openAction: 'attach_specific',
                            existingTabId: tabId,
                          })
                        }
                      >
                        Attach {tabId.slice(0, 8)}
                      </button>
                    ))}
                  </div>
                )}
              </li>
            ))}
          </ul>
        )}

        {/* Pagination */}
        {totalPages > 1 && (
          <div className="picker-pagination">
            <button
              className="page-btn"
              disabled={currentPage === 1}
              onClick={() => setCurrentPage((p) => p - 1)}
            >
              &laquo; Prev
            </button>
            <span className="page-info">
              Page {currentPage} of {totalPages}
            </span>
            <button
              className="page-btn"
              disabled={currentPage === totalPages}
              onClick={() => setCurrentPage((p) => p + 1)}
            >
              Next &raquo;
            </button>
          </div>
        )}

        {/* Results count */}
        <div className="picker-results-count">
          {filteredProjects.length} project{filteredProjects.length !== 1 ? 's' : ''} found
        </div>
      </div>
    </div>
  );
};

export default ProjectPicker;
