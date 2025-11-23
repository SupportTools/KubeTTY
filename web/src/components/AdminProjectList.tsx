import { useState, useEffect, useCallback } from "react";
import { useAuth } from "../contexts/AuthContext";
import { AdminProject, AdminProjectsResponse, AdminProjectStatus } from "../types";
import { parseErrorResponse } from "../utils/errorParser";
import AdminProjectForm from "./AdminProjectForm";
import AdminProjectDetail from "./AdminProjectDetail";

type ViewMode = "list" | "create" | "detail";

const statusColors: Record<AdminProjectStatus, string> = {
  pending: "#f59e0b",
  creating: "#3b82f6",
  running: "#10b981",
  updating: "#8b5cf6",
  failed: "#ef4444",
  deleting: "#f97316",
  deleted: "#6b7280",
};

const statusLabels: Record<AdminProjectStatus, string> = {
  pending: "Pending",
  creating: "Creating",
  running: "Running",
  updating: "Updating",
  failed: "Failed",
  deleting: "Deleting",
  deleted: "Deleted",
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

  const loadProjects = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      let url = "/api/admin/projects";
      if (statusFilter !== "all") {
        url += `?status=${statusFilter}`;
      }
      const res = await authFetch(url);
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
  }, [authFetch, statusFilter]);

  useEffect(() => {
    loadProjects();
  }, [loadProjects]);

  // Poll for updates every 10 seconds
  useEffect(() => {
    const interval = setInterval(loadProjects, 10000);
    return () => clearInterval(interval);
  }, [loadProjects]);

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
      <div className="modal admin-modal">
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
          <label className="filter-label">
            Status:
            <select
              value={statusFilter}
              onChange={(e) => setStatusFilter(e.target.value as AdminProjectStatus | "all")}
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
        <div className="modal-body admin-body">
          {error && <p className="error">{error}</p>}
          {loading && projects.length === 0 ? (
            <p className="loading-text">Loading projects...</p>
          ) : projects.length === 0 ? (
            <div className="empty-state">
              <p>No projects found.</p>
              <button className="primary-button" onClick={() => setViewMode("create")}>
                Create your first project
              </button>
            </div>
          ) : (
            <table className="admin-table">
              <thead>
                <tr>
                  <th>Name</th>
                  <th>User</th>
                  <th>Status</th>
                  <th>Namespace</th>
                  <th>Created</th>
                  <th>Actions</th>
                </tr>
              </thead>
              <tbody>
                {projects.map((project) => (
                  <tr key={project.id}>
                    <td>
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
                        style={{ backgroundColor: statusColors[project.status] }}
                      >
                        {statusLabels[project.status]}
                      </span>
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
      </div>
    </div>
  );
};

export default AdminProjectList;
