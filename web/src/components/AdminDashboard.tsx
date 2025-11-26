import { useState, useEffect, useCallback } from "react";
import { useAuth } from "../contexts/AuthContext";
import {
  DashboardSummary,
  DashboardMetrics,
  DashboardError,
  DashboardErrorsResponse,
  DashboardUsage,
} from "../types";
import { parseErrorResponse } from "../utils/errorParser";

interface Props {
  onClose: () => void;
}

const errorTypeLabels: Record<string, string> = {
  disconnect: "Disconnect",
  project_failed: "Project Failed",
  tab_error: "Tab Error",
};

const errorTypeColors: Record<string, string> = {
  disconnect: "#f59e0b",
  project_failed: "#ef4444",
  tab_error: "#f97316",
};

const AdminDashboard = ({ onClose }: Props) => {
  const { authFetch } = useAuth();
  const [summary, setSummary] = useState<DashboardSummary | null>(null);
  const [metrics, setMetrics] = useState<DashboardMetrics | null>(null);
  const [errors, setErrors] = useState<DashboardError[]>([]);
  const [usage, setUsage] = useState<DashboardUsage | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [activeTab, setActiveTab] = useState<"overview" | "errors" | "usage">("overview");

  const loadDashboardData = useCallback(async () => {
    setLoading(true);
    setError(null);

    try {
      // Load all dashboard data in parallel
      const [summaryRes, metricsRes, errorsRes, usageRes] = await Promise.all([
        authFetch("/api/admin/dashboard/summary"),
        authFetch("/api/admin/dashboard/metrics?period=1h"),
        authFetch("/api/admin/dashboard/errors?limit=50"),
        authFetch("/api/admin/dashboard/usage?period=24h"),
      ]);

      if (!summaryRes.ok) {
        throw new Error(await parseErrorResponse(summaryRes));
      }

      const summaryData = await summaryRes.json();
      setSummary(summaryData);

      if (metricsRes.ok) {
        setMetrics(await metricsRes.json());
      }

      if (errorsRes.ok) {
        const errorsData: DashboardErrorsResponse = await errorsRes.json();
        setErrors(errorsData.errors || []);
      }

      if (usageRes.ok) {
        setUsage(await usageRes.json());
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load dashboard");
    } finally {
      setLoading(false);
    }
  }, [authFetch]);

  useEffect(() => {
    loadDashboardData();
  }, [loadDashboardData]);

  // Auto-refresh every 30 seconds
  useEffect(() => {
    const interval = setInterval(loadDashboardData, 30000);
    return () => clearInterval(interval);
  }, [loadDashboardData]);

  const formatDate = (dateStr: string) => {
    const date = new Date(dateStr);
    return date.toLocaleString();
  };

  const formatRelativeTime = (dateStr: string) => {
    const date = new Date(dateStr);
    const now = new Date();
    const diffMs = now.getTime() - date.getTime();
    const diffMins = Math.floor(diffMs / 60000);
    const diffHours = Math.floor(diffMins / 60);
    const diffDays = Math.floor(diffHours / 24);

    if (diffDays > 0) return `${diffDays}d ago`;
    if (diffHours > 0) return `${diffHours}h ago`;
    if (diffMins > 0) return `${diffMins}m ago`;
    return "just now";
  };

  return (
    <div className="modal-backdrop">
      <div className="modal admin-modal dashboard-modal">
        <div className="modal-header">
          <h2>Admin Dashboard</h2>
          <button className="icon-button" onClick={onClose}>
            &times;
          </button>
        </div>

        <div className="dashboard-tabs">
          <button
            className={`dashboard-tab ${activeTab === "overview" ? "active" : ""}`}
            onClick={() => setActiveTab("overview")}
          >
            Overview
          </button>
          <button
            className={`dashboard-tab ${activeTab === "errors" ? "active" : ""}`}
            onClick={() => setActiveTab("errors")}
          >
            Errors ({errors.length})
          </button>
          <button
            className={`dashboard-tab ${activeTab === "usage" ? "active" : ""}`}
            onClick={() => setActiveTab("usage")}
          >
            Usage
          </button>
          <div className="spacer" />
          <button
            className="secondary refresh-button"
            onClick={loadDashboardData}
            disabled={loading}
          >
            Refresh
          </button>
        </div>

        <div className="modal-body admin-body dashboard-body">
          {error && <p className="error">{error}</p>}
          {loading && !summary ? (
            <p className="loading-text">Loading dashboard...</p>
          ) : (
            <>
              {activeTab === "overview" && summary && (
                <div className="dashboard-overview">
                  {/* Summary Cards */}
                  <div className="dashboard-cards">
                    <div className="dashboard-card">
                      <div className="card-label">Active Connections</div>
                      <div className="card-value">{summary.activeConnections}</div>
                    </div>
                    <div className="dashboard-card">
                      <div className="card-label">Projects</div>
                      <div className="card-value">
                        <span className="status-running">{summary.projects.running}</span>
                        {summary.projects.failed > 0 && (
                          <span className="status-failed"> / {summary.projects.failed}</span>
                        )}
                        <span className="status-total"> / {summary.projects.total}</span>
                      </div>
                      <div className="card-subtitle">running / failed / total</div>
                    </div>
                    <div className="dashboard-card">
                      <div className="card-label">Active Tabs</div>
                      <div className="card-value">
                        {summary.tabs.active}
                        <span className="status-total"> / {summary.tabs.total}</span>
                      </div>
                      <div className="card-subtitle">active / total</div>
                    </div>
                    <div className="dashboard-card">
                      <div className="card-label">Error Rate (24h)</div>
                      <div className={`card-value ${summary.last24h.errorRate > 5 ? "status-failed" : ""}`}>
                        {summary.last24h.errorRate.toFixed(1)}%
                      </div>
                      <div className="card-subtitle">
                        {summary.last24h.errors} errors / {summary.last24h.connections} connections
                      </div>
                    </div>
                  </div>

                  {/* Disconnect Breakdown */}
                  {metrics && Object.keys(metrics.disconnectsByReason).length > 0 && (
                    <div className="dashboard-section">
                      <h3>Disconnects by Reason</h3>
                      <div className="disconnect-breakdown">
                        {Object.entries(metrics.disconnectsByReason).map(([reason, count]) => (
                          <div key={reason} className="disconnect-item">
                            <span className="disconnect-reason">{reason}</span>
                            <span className="disconnect-count">{count}</span>
                          </div>
                        ))}
                      </div>
                    </div>
                  )}

                  {/* Flow Control Stats */}
                  {metrics && (metrics.flowControlPauses > 0 || metrics.writeErrors > 0) && (
                    <div className="dashboard-section">
                      <h3>Flow Control</h3>
                      <div className="flow-control-stats">
                        <div className="stat-item">
                          <span className="stat-label">Pauses</span>
                          <span className="stat-value">{metrics.flowControlPauses}</span>
                        </div>
                        <div className="stat-item">
                          <span className="stat-label">Write Errors</span>
                          <span className={`stat-value ${metrics.writeErrors > 0 ? "status-failed" : ""}`}>
                            {metrics.writeErrors}
                          </span>
                        </div>
                      </div>
                    </div>
                  )}
                </div>
              )}

              {activeTab === "errors" && (
                <div className="dashboard-errors">
                  {errors.length === 0 ? (
                    <div className="empty-state">
                      <p>No recent errors.</p>
                    </div>
                  ) : (
                    <table className="admin-table">
                      <thead>
                        <tr>
                          <th>Type</th>
                          <th>Project</th>
                          <th>Reason</th>
                          <th>Time</th>
                          <th>Details</th>
                        </tr>
                      </thead>
                      <tbody>
                        {errors.map((err, idx) => (
                          <tr key={`${err.projectId}-${err.timestamp}-${idx}`}>
                            <td>
                              <span
                                className="error-type-badge"
                                style={{ backgroundColor: errorTypeColors[err.type] }}
                              >
                                {errorTypeLabels[err.type] || err.type}
                              </span>
                            </td>
                            <td className="project-name">{err.projectName}</td>
                            <td>{err.reason}</td>
                            <td className="date" title={formatDate(err.timestamp)}>
                              {formatRelativeTime(err.timestamp)}
                            </td>
                            <td className="error-details">{err.details || "-"}</td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  )}
                </div>
              )}

              {activeTab === "usage" && usage && (
                <div className="dashboard-usage">
                  {/* Top Projects */}
                  {usage.topProjects.length > 0 && (
                    <div className="dashboard-section">
                      <h3>Top Projects by Usage</h3>
                      <table className="admin-table">
                        <thead>
                          <tr>
                            <th>Project</th>
                            <th>Active Tabs</th>
                          </tr>
                        </thead>
                        <tbody>
                          {usage.topProjects.map((project) => (
                            <tr key={project.projectId}>
                              <td>{project.displayName || project.name}</td>
                              <td>{project.connections}</td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    </div>
                  )}

                  {usage.topProjects.length === 0 && (
                    <div className="empty-state">
                      <p>No usage data available yet.</p>
                    </div>
                  )}
                </div>
              )}
            </>
          )}
        </div>
      </div>
    </div>
  );
};

export default AdminDashboard;
