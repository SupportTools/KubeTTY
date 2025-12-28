import { useState, useEffect, useCallback, useMemo } from "react";
import { useAuth } from "../contexts/AuthContext";
import { AdminProject, AdminProjectsResponse, AdminProjectStatus } from "../types";
import { parseErrorResponse } from "../utils/errorParser";
import AdminProjectForm from "./AdminProjectForm";
import AdminProjectDetail from "./AdminProjectDetail";

type ViewMode = "list" | "create" | "detail";
type SortField = "name" | "user" | "status" | "namespace" | "created";
type SortDirection = "asc" | "desc";

const ITEMS_PER_PAGE = 10;

const statusColors: Record<AdminProjectStatus, string> = {
  pending: "#f59e0b",
  syncing: "#60a5fa",
  creating: "#3b82f6",
  running: "#10b981",
  updating: "#8b5cf6",
  failed: "#ef4444",
  deleting: "#f97316",
  deleted: "#6b7280",
};

const statusLabels: Record<AdminProjectStatus, string> = {
  pending: "Pending",
  syncing: "Syncing",
  creating: "Creating",
  running: "Running",
  updating: "Updating",
  failed: "Failed",
  deleting: "Deleting",
  deleted: "Deleted",
};

const getStatusPriority = (status: AdminProjectStatus): number => {
  switch (status) {
    case "running":
      return 1;
    case "creating":
    case "updating":
    case "syncing":
      return 2;
    case "pending":
      return 3;
    case "failed":
      return 4;
    case "deleting":
      return 5;
    case "deleted":
      return 6;
    default:
      return 10;
  }
};

interface Props {
  onClose: () => void;
}

const AdminProjectList = ({ onClose }: Props) => {
  const { authFetch } = useAuth();
  const [projects, setProjects] = useState<AdminProject[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [viewMode, setViewMode] = useState<ViewMode>("list");
  const [selectedProject, setSelectedProject] = useState<AdminProject | null>(null);
  const [statusFilter, setStatusFilter] = useState<AdminProjectStatus | "all">("all");
  const [searchQuery, setSearchQuery] = useState("");
  const [sortField, setSortField] = useState<SortField>("name");
  const [sortDirection, setSortDirection] = useState<SortDirection>("asc");
  const [currentPage, setCurrentPage] = useState(1);
  // Gateway version for quick upgrades
  const [gatewayVersion, setGatewayVersion] = useState<string | null>(null);
  const [upgradingProjectId, setUpgradingProjectId] = useState<string | null>(null);

  const loadProjects = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const res = await authFetch("/api/admin/projects");
      if (!res.ok) {
        throw new Error(await parseErrorResponse(res));
      }
      const data = (await res.json()) as AdminProjectsResponse;
      setProjects(data.projects || []);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load projects");
    } finally {
      setLoading(false);
    }
  }, [authFetch]);

  useEffect(() => {
    loadProjects();
  }, [loadProjects]);

  // Poll for updates every 10 seconds
  useEffect(() => {
    const interval = setInterval(loadProjects, 10000);
    return () => clearInterval(interval);
  }, [loadProjects]);

  // Fetch gateway version for quick upgrade feature
  useEffect(() => {
    const fetchGatewayVersion = async () => {
      try {
        const res = await authFetch("/api/admin/gateway-version");
        if (res.ok) {
          const data = await res.json();
          setGatewayVersion(data.recommendedVersion || null);
        }
      } catch {
        // Silently ignore - upgrade feature just won't be available
      }
    };
    fetchGatewayVersion();
  }, [authFetch]);

  // Filter projects based on search query and status filter
  const filteredProjects = useMemo(() => {
    let filtered = projects;

    // Apply status filter
    if (statusFilter !== "all") {
      filtered = filtered.filter((p) => p.status === statusFilter);
    }

    // Apply search filter
    if (searchQuery.trim()) {
      const query = searchQuery.toLowerCase();
      filtered = filtered.filter(
        (project) =>
          project.displayName?.toLowerCase().includes(query) ||
          project.name.toLowerCase().includes(query) ||
          project.userName?.toLowerCase().includes(query) ||
          project.targetNamespace?.toLowerCase().includes(query)
      );
    }

    return filtered;
  }, [projects, statusFilter, searchQuery]);

  // Sort filtered projects
  const sortedProjects = useMemo(() => {
    const sorted = [...filteredProjects].sort((a, b) => {
      let comparison = 0;
      switch (sortField) {
        case "name":
          comparison = (a.displayName || a.name).localeCompare(b.displayName || b.name);
          break;
        case "user":
          comparison = (a.userName || "").localeCompare(b.userName || "");
          break;
        case "status":
          comparison = getStatusPriority(a.status) - getStatusPriority(b.status);
          break;
        case "namespace":
          comparison = (a.targetNamespace || "").localeCompare(b.targetNamespace || "");
          break;
        case "created":
          comparison = new Date(a.createdAt).getTime() - new Date(b.createdAt).getTime();
          break;
      }
      return sortDirection === "asc" ? comparison : -comparison;
    });
    return sorted;
  }, [filteredProjects, sortField, sortDirection]);

  // Paginate sorted projects
  const totalPages = Math.ceil(sortedProjects.length / ITEMS_PER_PAGE);
  const paginatedProjects = useMemo(() => {
    const start = (currentPage - 1) * ITEMS_PER_PAGE;
    return sortedProjects.slice(start, start + ITEMS_PER_PAGE);
  }, [sortedProjects, currentPage]);

  // Reset to page 1 when search, filter, or sort changes
  const handleSearchChange = (value: string) => {
    setSearchQuery(value);
    setCurrentPage(1);
  };

  const handleStatusFilterChange = (value: AdminProjectStatus | "all") => {
    setStatusFilter(value);
    setCurrentPage(1);
  };

  const handleSortChange = (field: SortField) => {
    if (field === sortField) {
      setSortDirection((prev) => (prev === "asc" ? "desc" : "asc"));
    } else {
      setSortField(field);
      setSortDirection("asc");
    }
    setCurrentPage(1);
  };

  const getSortIcon = (field: SortField) => {
    if (sortField !== field) return "";
    return sortDirection === "asc" ? " ▲" : " ▼";
  };

  const handleCreateSuccess = useCallback(() => {
    setViewMode("list");
    loadProjects();
  }, [loadProjects]);

  const handleViewProject = useCallback((project: AdminProject) => {
    setSelectedProject(project);
    setViewMode("detail");
  }, []);

  const handleDeleteProject = useCallback(
    async (projectId: string) => {
      if (!window.confirm("Are you sure you want to delete this project? This will remove all associated resources.")) {
        return;
      }
      try {
        const res = await authFetch(`/api/admin/projects/${projectId}`, {
          method: "DELETE",
        });
        if (!res.ok && res.status !== 204) {
          throw new Error(await parseErrorResponse(res));
        }
        loadProjects();
      } catch (err) {
        setError(err instanceof Error ? err.message : "Failed to delete project");
      }
    },
    [authFetch, loadProjects]
  );

  const handleRestartProject = useCallback(
    async (projectId: string) => {
      if (!window.confirm("Are you sure you want to restart this project?")) {
        return;
      }
      try {
        const res = await authFetch(`/api/admin/projects/${projectId}/restart`, {
          method: "POST",
        });
        if (!res.ok) {
          throw new Error(await parseErrorResponse(res));
        }
        loadProjects();
      } catch (err) {
        setError(err instanceof Error ? err.message : "Failed to restart project");
      }
    },
    [authFetch, loadProjects]
  );

  // Quick upgrade to gateway version (no confirmation needed)
  const handleQuickUpgrade = useCallback(
    async (project: AdminProject) => {
      if (!gatewayVersion) return;
      if (project.imageTag === gatewayVersion) return;

      setUpgradingProjectId(project.id);
      setError(null);
      try {
        const res = await authFetch(`/api/admin/projects/${project.id}/upgrade`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ imageTag: gatewayVersion }),
        });
        if (!res.ok) {
          throw new Error(await parseErrorResponse(res));
        }
        loadProjects();
      } catch (err) {
        setError(err instanceof Error ? err.message : "Failed to upgrade project");
      } finally {
        setUpgradingProjectId(null);
      }
    },
    [authFetch, gatewayVersion, loadProjects]
  );

  const formatDate = (dateStr: string) => {
    const date = new Date(dateStr);
    return date.toLocaleString();
  };

  if (viewMode === "create") {
    return (
      <AdminProjectForm
        onClose={() => setViewMode("list")}
        onSuccess={handleCreateSuccess}
      />
    );
  }

  if (viewMode === "detail" && selectedProject) {
    return (
      <AdminProjectDetail
        project={selectedProject}
        onClose={() => {
          setSelectedProject(null);
          setViewMode("list");
        }}
        onRefresh={loadProjects}
        onDelete={handleDeleteProject}
        onRestart={handleRestartProject}
      />
    );
  }

  return (
    <div className="modal-backdrop">
      <div className="modal admin-modal admin-project-modal">
        <div className="modal-header">
          <h2>Project Management</h2>
          <button className="icon-button" onClick={onClose}>
            &times;
          </button>
        </div>
        <div className="admin-toolbar">
          <button className="primary-button" onClick={() => setViewMode("create")}>
            + New Project
          </button>
          <div className="spacer" />
          <div className="admin-search">
            <input
              type="text"
              placeholder="Search projects..."
              value={searchQuery}
              onChange={(e) => handleSearchChange(e.target.value)}
              className="search-input"
            />
            {searchQuery && (
              <button
                className="search-clear"
                onClick={() => handleSearchChange("")}
                aria-label="Clear search"
              >
                &times;
              </button>
            )}
          </div>
          <label className="filter-label">
            Status:
            <select
              value={statusFilter}
              onChange={(e) => handleStatusFilterChange(e.target.value as AdminProjectStatus | "all")}
            >
              <option value="all">All</option>
              <option value="running">Running</option>
              <option value="pending">Pending</option>
              <option value="creating">Creating</option>
              <option value="updating">Updating</option>
              <option value="failed">Failed</option>
            </select>
          </label>
          <button className="secondary refresh-button" onClick={loadProjects} disabled={loading}>
            Refresh
          </button>
        </div>

        {/* Sort controls */}
        <div className="admin-sort-bar">
          <span className="sort-label">Sort by:</span>
          <button
            className={`sort-btn ${sortField === "name" ? "active" : ""}`}
            onClick={() => handleSortChange("name")}
          >
            Name{getSortIcon("name")}
          </button>
          <button
            className={`sort-btn ${sortField === "user" ? "active" : ""}`}
            onClick={() => handleSortChange("user")}
          >
            User{getSortIcon("user")}
          </button>
          <button
            className={`sort-btn ${sortField === "status" ? "active" : ""}`}
            onClick={() => handleSortChange("status")}
          >
            Status{getSortIcon("status")}
          </button>
          <button
            className={`sort-btn ${sortField === "namespace" ? "active" : ""}`}
            onClick={() => handleSortChange("namespace")}
          >
            Namespace{getSortIcon("namespace")}
          </button>
          <button
            className={`sort-btn ${sortField === "created" ? "active" : ""}`}
            onClick={() => handleSortChange("created")}
          >
            Created{getSortIcon("created")}
          </button>
        </div>

        <div className="modal-body admin-body">
          {error && <p className="error">{error}</p>}
          {loading && projects.length === 0 ? (
            <p className="loading-text">Loading projects...</p>
          ) : paginatedProjects.length === 0 ? (
            <div className="empty-state">
              <p>{searchQuery || statusFilter !== "all" ? "No projects match your search." : "No projects found."}</p>
              {!searchQuery && statusFilter === "all" && (
                <button className="primary-button" onClick={() => setViewMode("create")}>
                  Create your first project
                </button>
              )}
            </div>
          ) : (
            <table className="admin-table">
              <thead>
                <tr>
                  <th>Name</th>
                  <th>User</th>
                  <th>Status</th>
                  <th>Version</th>
                  <th>Namespace</th>
                  <th>Created</th>
                  <th>Actions</th>
                </tr>
              </thead>
              <tbody>
                {paginatedProjects.map((project) => (
                  <tr key={project.id}>
                    <td className="project-cell">
                      <button
                        className="link-button project-name"
                        onClick={() => handleViewProject(project)}
                      >
                        {project.displayName}
                      </button>
                      <span className="project-id">{project.name}</span>
                    </td>
                    <td>{project.userName}</td>
                    <td>
                      <span
                        className="status-badge"
                        style={{ backgroundColor: project.paused ? "#6b7280" : statusColors[project.status] }}
                      >
                        {project.paused ? "Paused" : statusLabels[project.status]}
                      </span>
                    </td>
                    <td className={gatewayVersion && project.imageTag !== gatewayVersion ? "version-outdated" : "version-current"}>
                      {project.imageTag}
                    </td>
                    <td className="namespace">{project.targetNamespace}</td>
                    <td className="date">{formatDate(project.createdAt)}</td>
                    <td className="actions">
                      <button
                        className="link-button"
                        onClick={() => handleViewProject(project)}
                      >
                        View
                      </button>
                      {/* Quick upgrade button */}
                      {gatewayVersion && (project.status === "running" || project.status === "failed") && (
                        <button
                          className={`icon-button ${project.imageTag !== gatewayVersion ? "upgrade-available" : "upgrade-current"}`}
                          onClick={() => handleQuickUpgrade(project)}
                          title={project.imageTag !== gatewayVersion
                            ? `Upgrade to ${gatewayVersion}`
                            : `Current (${gatewayVersion})`}
                          disabled={project.imageTag === gatewayVersion || upgradingProjectId === project.id}
                        >
                          {upgradingProjectId === project.id ? "..." : (project.imageTag !== gatewayVersion ? "⬆" : "✓")}
                        </button>
                      )}
                      {(project.status === "running" || project.status === "failed") && (
                        <button
                          className="link-button"
                          onClick={() => handleRestartProject(project.id)}
                        >
                          Restart
                        </button>
                      )}
                      {project.status !== "deleting" && project.status !== "deleted" && (
                        <button
                          className="link-button danger"
                          onClick={() => handleDeleteProject(project.id)}
                        >
                          Delete
                        </button>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>

        {/* Pagination */}
        {totalPages > 1 && (
          <div className="admin-pagination">
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
        <div className="admin-results-count">
          {filteredProjects.length} project{filteredProjects.length !== 1 ? "s" : ""} found
          {(searchQuery || statusFilter !== "all") && ` (${projects.length} total)`}
        </div>
      </div>
    </div>
  );
};

export default AdminProjectList;
