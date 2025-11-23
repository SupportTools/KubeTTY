import { useState, useEffect, useCallback } from "react";
import { useAuth } from "../contexts/AuthContext";
import {
  AdminProject,
  AdminProjectStatus,
  ProjectStatusResponse,
  DeploymentStatus,
  UpgradeInfoResponse,
} from "../types";
import { parseErrorResponse } from "../utils/errorParser";

interface Props {
  project: AdminProject;
  onClose: () => void;
  onRefresh: () => void;
  onDelete: (projectId: string) => void;
  onRestart: (projectId: string) => void;
}

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

const AdminProjectDetail = ({
  project: initialProject,
  onClose,
  onRefresh,
  onDelete,
  onRestart,
}: Props) => {
  const { authFetch } = useAuth();
  const [project, setProject] = useState<AdminProject>(initialProject);
  const [deployment, setDeployment] = useState<DeploymentStatus | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showUpgradeModal, setShowUpgradeModal] = useState(false);
  const [upgradeInfo, setUpgradeInfo] = useState<UpgradeInfoResponse | null>(null);
  const [newVersion, setNewVersion] = useState("");
  const [upgrading, setUpgrading] = useState(false);

  const loadStatus = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const res = await authFetch(`/api/admin/projects/${project.id}/status`);
      if (!res.ok) {
        throw new Error(await parseErrorResponse(res));
      }
      const data = (await res.json()) as ProjectStatusResponse;
      setProject(data.project);
      setDeployment(data.deployment);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load project status");
    } finally {
      setLoading(false);
    }
  }, [authFetch, project.id]);

  useEffect(() => {
    loadStatus();
    const interval = setInterval(loadStatus, 5000);
    return () => clearInterval(interval);
  }, [loadStatus]);

  const formatDate = (dateStr?: string) => {
    if (!dateStr) return "N/A";
    const date = new Date(dateStr);
    return date.toLocaleString();
  };

  const handleDelete = () => {
    onDelete(project.id);
    onClose();
  };

  const handleRestart = () => {
    onRestart(project.id);
  };

  const handleUpgradeClick = async () => {
    try {
      const res = await authFetch(`/api/admin/projects/${project.id}/upgrade-info`);
      if (!res.ok) {
        throw new Error(await parseErrorResponse(res));
      }
      const data = (await res.json()) as UpgradeInfoResponse;
      setUpgradeInfo(data);
      setShowUpgradeModal(true);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load upgrade info");
    }
  };

  const handleUpgradeConfirm = async () => {
    if (!newVersion.trim()) {
      setError("Please enter a version number");
      return;
    }

    setUpgrading(true);
    setError(null);
    try {
      const res = await authFetch(`/api/admin/projects/${project.id}/upgrade`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ imageTag: newVersion.trim() }),
      });
      if (!res.ok) {
        throw new Error(await parseErrorResponse(res));
      }
      setShowUpgradeModal(false);
      setNewVersion("");
      await loadStatus();
      onRefresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to upgrade project");
    } finally {
      setUpgrading(false);
    }
  };

  const handleUpgradeCancel = () => {
    setShowUpgradeModal(false);
    setNewVersion("");
    setUpgradeInfo(null);
    setError(null);
  };

  const formatActivityTime = (dateStr?: string) => {
    if (!dateStr) return "Never";
    const date = new Date(dateStr);
    return date.toLocaleString();
  };

  return (
    <div className="modal-backdrop">
      <div className="modal admin-modal admin-detail-modal">
        <div className="modal-header">
          <div className="header-content">
            <h2>{project.displayName}</h2>
            <span
              className="status-badge"
              style={{ backgroundColor: statusColors[project.status] }}
            >
              {statusLabels[project.status]}
            </span>
          </div>
          <button className="icon-button" onClick={onClose}>
            &times;
          </button>
        </div>
        <div className="modal-body admin-detail-body">
          {error && <p className="error">{error}</p>}

          <div className="detail-section">
            <h3>Overview</h3>
            <div className="detail-grid">
              <div className="detail-item">
                <label>Project ID</label>
                <span className="mono">{project.id}</span>
              </div>
              <div className="detail-item">
                <label>Name</label>
                <span>{project.name}</span>
              </div>
              <div className="detail-item">
                <label>Owner</label>
                <span>{project.userName}</span>
              </div>
              <div className="detail-item">
                <label>Namespace</label>
                <span className="mono">{project.targetNamespace}</span>
              </div>
              <div className="detail-item">
                <label>Session ID</label>
                <span className="mono">{project.sessionId}</span>
              </div>
              <div className="detail-item">
                <label>Pod IP</label>
                <span className="mono">{project.podIP || "N/A"}</span>
              </div>
              {project.description && (
                <div className="detail-item full-width">
                  <label>Description</label>
                  <span>{project.description}</span>
                </div>
              )}
            </div>
          </div>

          {project.statusMessage && (
            <div className="detail-section">
              <h3>Status Message</h3>
              <div className="status-message-box">
                {project.statusMessage}
              </div>
            </div>
          )}

          <div className="detail-section">
            <h3>Resource Configuration</h3>
            <div className="detail-grid">
              <div className="detail-item">
                <label>CPU Request</label>
                <span>{project.cpuRequest}</span>
              </div>
              <div className="detail-item">
                <label>CPU Limit</label>
                <span>{project.cpuLimit}</span>
              </div>
              <div className="detail-item">
                <label>Memory Request</label>
                <span>{project.memoryRequest}</span>
              </div>
              <div className="detail-item">
                <label>Memory Limit</label>
                <span>{project.memoryLimit}</span>
              </div>
              <div className="detail-item">
                <label>Storage Size</label>
                <span>{project.storageSize}</span>
              </div>
              <div className="detail-item">
                <label>Storage Class</label>
                <span>{project.storageClass}</span>
              </div>
            </div>
          </div>

          <div className="detail-section">
            <h3>Configuration</h3>
            <div className="detail-grid">
              <div className="detail-item">
                <label>Image</label>
                <span className="mono">{project.imageRepository}:{project.imageTag}</span>
              </div>
              <div className="detail-item">
                <label>Max Tabs (Per Client)</label>
                <span>{project.maxTabsPerClient}</span>
              </div>
              <div className="detail-item">
                <label>Max Tabs (Total)</label>
                <span>{project.maxTabsTotal}</span>
              </div>
              <div className="detail-item">
                <label>Docker-in-Docker</label>
                <span>{project.dindEnabled ? "Enabled" : "Disabled"}</span>
              </div>
            </div>
          </div>

          {deployment && (
            <div className="detail-section">
              <h3>Deployment Status</h3>
              <div className="detail-grid">
                <div className="detail-item">
                  <label>Deployment</label>
                  <span className="mono">{deployment.deploymentName}</span>
                </div>
                <div className="detail-item">
                  <label>Ready</label>
                  <span className={deployment.ready ? "status-ok" : "status-error"}>
                    {deployment.ready ? "Yes" : "No"}
                  </span>
                </div>
                <div className="detail-item">
                  <label>Replicas</label>
                  <span>
                    {deployment.readyReplicas}/{deployment.replicas} ready
                  </span>
                </div>
                <div className="detail-item">
                  <label>Available</label>
                  <span>{deployment.availableReplicas}</span>
                </div>
              </div>

              {deployment.pods && deployment.pods.length > 0 && (
                <div className="pods-section">
                  <h4>Pods</h4>
                  <table className="pods-table">
                    <thead>
                      <tr>
                        <th>Name</th>
                        <th>Phase</th>
                        <th>Ready</th>
                        <th>IP</th>
                        <th>Restarts</th>
                        <th>Age</th>
                      </tr>
                    </thead>
                    <tbody>
                      {deployment.pods.map((pod) => (
                        <tr key={pod.name}>
                          <td className="mono">{pod.name}</td>
                          <td>
                            <span className={`phase-${pod.phase.toLowerCase()}`}>
                              {pod.phase}
                            </span>
                          </td>
                          <td>{pod.ready ? "Yes" : "No"}</td>
                          <td className="mono">{pod.ip || "N/A"}</td>
                          <td>{pod.restarts}</td>
                          <td>{pod.age}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </div>
          )}

          <div className="detail-section">
            <h3>Timestamps</h3>
            <div className="detail-grid">
              <div className="detail-item">
                <label>Created</label>
                <span>{formatDate(project.createdAt)}</span>
              </div>
              <div className="detail-item">
                <label>Updated</label>
                <span>{formatDate(project.updatedAt)}</span>
              </div>
              <div className="detail-item">
                <label>Last Health Check</label>
                <span>{formatDate(project.lastHealthCheck)}</span>
              </div>
              <div className="detail-item">
                <label>Last Activity</label>
                <span>{formatDate(project.lastActivity)}</span>
              </div>
            </div>
          </div>
        </div>
        <div className="modal-actions">
          <button className="secondary" onClick={onClose}>
            Close
          </button>
          <button
            className="secondary refresh-button"
            onClick={loadStatus}
            disabled={loading}
          >
            Refresh
          </button>
          {project.status === "running" && (
            <button className="primary-button" onClick={handleUpgradeClick}>
              Upgrade
            </button>
          )}
          {(project.status === "running" || project.status === "failed") && (
            <button className="warning-button" onClick={handleRestart}>
              Restart
            </button>
          )}
          {project.status !== "deleting" && project.status !== "deleted" && (
            <button className="danger" onClick={handleDelete}>
              Delete
            </button>
          )}
        </div>

        {showUpgradeModal && upgradeInfo && (
          <div className="modal-backdrop" onClick={handleUpgradeCancel}>
            <div className="modal upgrade-modal" onClick={(e) => e.stopPropagation()}>
              <div className="modal-header">
                <h2>Upgrade Project</h2>
                <button className="icon-button" onClick={handleUpgradeCancel}>
                  &times;
                </button>
              </div>
              <div className="modal-body">
                {error && <p className="error">{error}</p>}

                <div className="upgrade-info">
                  <div className="info-item">
                    <label>Current Version:</label>
                    <span className="mono">{upgradeInfo.currentVersion}</span>
                  </div>
                  <div className="info-item">
                    <label>Last Activity:</label>
                    <span>{formatActivityTime(upgradeInfo.lastActivity)}</span>
                  </div>
                  {upgradeInfo.minutesSinceActivity !== undefined && (
                    <div className="info-item">
                      <label>Time Since Activity:</label>
                      <span>
                        {upgradeInfo.minutesSinceActivity < 60
                          ? `${upgradeInfo.minutesSinceActivity} minutes`
                          : `${Math.floor(upgradeInfo.minutesSinceActivity / 60)} hours`}
                      </span>
                    </div>
                  )}
                </div>

                {upgradeInfo.minutesSinceActivity !== undefined && upgradeInfo.minutesSinceActivity < 60 && (
                  <div className="warning-box">
                    <strong>⚠️ Warning:</strong> This project was active within the last hour.
                    Upgrading will restart the pod and terminate any running processes.
                  </div>
                )}

                <div className="form-group">
                  <label htmlFor="newVersion">New Version:</label>
                  <input
                    id="newVersion"
                    type="text"
                    className="form-input"
                    placeholder="e.g., v1.2.3 or 1.2.3"
                    value={newVersion}
                    onChange={(e) => setNewVersion(e.target.value)}
                    disabled={upgrading}
                  />
                  <small className="form-help">
                    Enter a semantic version (e.g., v1.2.3, 1.2.3, or v0.5.1-rc1)
                  </small>
                </div>
              </div>
              <div className="modal-actions">
                <button className="secondary" onClick={handleUpgradeCancel} disabled={upgrading}>
                  Cancel
                </button>
                <button
                  className="primary-button"
                  onClick={handleUpgradeConfirm}
                  disabled={upgrading || !newVersion.trim()}
                >
                  {upgrading ? "Upgrading..." : "Upgrade"}
                </button>
              </div>
            </div>
          </div>
        )}
      </div>
    </div>
  );
};

export default AdminProjectDetail;
