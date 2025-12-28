import { useState, useEffect, useCallback } from "react";
import { useAuth } from "../contexts/AuthContext";
import {
  Setting,
  SettingCategory,
  SettingsResponse,
  SettingHistory,
  SettingHistoryResponse,
  SettingCategoryInfo,
} from "../types";
import { parseErrorResponse } from "../utils/errorParser";

interface Props {
  onClose: () => void;
}

const categoryDisplayNames: Record<SettingCategory, string> = {
  project_defaults: "Project Defaults",
  auth: "Authentication",
  features: "Features",
  ui: "User Interface",
  controller: "Controller",
  notifications: "Notifications",
  secrets: "Secrets",
};

const AdminSettings = ({ onClose }: Props) => {
  const { authFetch } = useAuth();
  const [settings, setSettings] = useState<Setting[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [activeCategory, setActiveCategory] = useState<SettingCategory>("project_defaults");
  const [editingSetting, setEditingSetting] = useState<Setting | null>(null);
  const [editValue, setEditValue] = useState<string>("");
  const [saving, setSaving] = useState(false);
  const [history, setHistory] = useState<SettingHistory[]>([]);
  const [showHistory, setShowHistory] = useState<Setting | null>(null);
  const [historyLoading, setHistoryLoading] = useState(false);

  const loadSettings = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const res = await authFetch("/api/admin/settings");
      if (!res.ok) {
        throw new Error(await parseErrorResponse(res));
      }
      const data = (await res.json()) as SettingsResponse;
      setSettings(data.settings || []);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load settings");
    } finally {
      setLoading(false);
    }
  }, [authFetch]);

  useEffect(() => {
    loadSettings();
  }, [loadSettings]);

  const loadHistory = useCallback(
    async (setting: Setting) => {
      setHistoryLoading(true);
      try {
        const res = await authFetch(
          `/api/admin/settings/${setting.category}/${setting.key}/history`
        );
        if (!res.ok) {
          throw new Error(await parseErrorResponse(res));
        }
        const data = (await res.json()) as SettingHistoryResponse;
        setHistory(data.history || []);
        setShowHistory(setting);
      } catch (err) {
        setError(err instanceof Error ? err.message : "Failed to load history");
      } finally {
        setHistoryLoading(false);
      }
    },
    [authFetch]
  );

  const handleEdit = (setting: Setting) => {
    setEditingSetting(setting);
    // Format value for editing
    if (setting.valueType === "json") {
      setEditValue(JSON.stringify(setting.value, null, 2));
    } else if (setting.valueType === "bool") {
      setEditValue(String(setting.value));
    } else {
      setEditValue(String(setting.value));
    }
  };

  const handleSave = async () => {
    if (!editingSetting) return;

    setSaving(true);
    setError(null);

    try {
      // Parse value based on type
      let parsedValue: unknown;
      if (editingSetting.valueType === "int") {
        parsedValue = parseInt(editValue, 10);
        if (isNaN(parsedValue as number)) {
          throw new Error("Invalid integer value");
        }
      } else if (editingSetting.valueType === "bool") {
        parsedValue = editValue.toLowerCase() === "true";
      } else if (editingSetting.valueType === "json") {
        try {
          parsedValue = JSON.parse(editValue);
        } catch {
          throw new Error("Invalid JSON value");
        }
      } else {
        parsedValue = editValue;
      }

      const res = await authFetch(
        `/api/admin/settings/${editingSetting.category}/${editingSetting.key}`,
        {
          method: "PUT",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ value: parsedValue }),
        }
      );

      if (!res.ok) {
        throw new Error(await parseErrorResponse(res));
      }

      setEditingSetting(null);
      loadSettings();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to save setting");
    } finally {
      setSaving(false);
    }
  };

  const handleCancel = () => {
    setEditingSetting(null);
    setEditValue("");
  };

  const filteredSettings = settings.filter((s) => s.category === activeCategory);

  const formatValue = (setting: Setting): string => {
    if (setting.isSensitive && setting.value === "********") {
      return "********";
    }
    if (setting.valueType === "json") {
      return JSON.stringify(setting.value);
    }
    return String(setting.value);
  };

  const formatHistoryValue = (value: unknown): string => {
    if (value === null || value === undefined) return "-";
    if (typeof value === "object") {
      return JSON.stringify(value);
    }
    return String(value);
  };

  const categories: SettingCategory[] = [
    "project_defaults",
    "auth",
    "features",
    "ui",
    "controller",
    "notifications",
    "secrets",
  ];

  return (
    <div className="admin-panel">
      <div className="admin-panel-header">
        <h2>Settings</h2>
        <button className="close-btn" onClick={onClose}>
          X
        </button>
      </div>

      {error && <div className="error-banner">{error}</div>}

      <div className="settings-layout">
        {/* Category Tabs */}
        <div className="settings-categories">
          {categories.map((cat) => (
            <button
              key={cat}
              className={`category-tab ${activeCategory === cat ? "active" : ""}`}
              onClick={() => setActiveCategory(cat)}
            >
              {categoryDisplayNames[cat]}
              <span className="category-count">
                ({settings.filter((s) => s.category === cat).length})
              </span>
            </button>
          ))}
        </div>

        {/* Settings List */}
        <div className="settings-content">
          {loading ? (
            <div className="loading">Loading settings...</div>
          ) : filteredSettings.length === 0 ? (
            <div className="empty-state">No settings in this category</div>
          ) : (
            <div className="settings-list">
              {filteredSettings.map((setting) => (
                <div key={setting.id} className="setting-item">
                  <div className="setting-header">
                    <div className="setting-info">
                      <span className="setting-name">{setting.displayName}</span>
                      <span className="setting-key">{setting.key}</span>
                      {setting.isReadonly && (
                        <span className="setting-badge readonly">Read-only</span>
                      )}
                      {setting.isSensitive && (
                        <span className="setting-badge sensitive">Sensitive</span>
                      )}
                    </div>
                    <div className="setting-actions">
                      <button
                        className="btn-small"
                        onClick={() => loadHistory(setting)}
                        disabled={historyLoading}
                      >
                        History
                      </button>
                      {!setting.isReadonly && (
                        <button
                          className="btn-small btn-primary"
                          onClick={() => handleEdit(setting)}
                        >
                          Edit
                        </button>
                      )}
                    </div>
                  </div>
                  {setting.description && (
                    <div className="setting-description">{setting.description}</div>
                  )}
                  <div className="setting-value">
                    <span className="value-type">{setting.valueType}</span>
                    <code className="value-content">{formatValue(setting)}</code>
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      </div>

      {/* Edit Modal */}
      {editingSetting && (
        <div className="modal-overlay" onClick={handleCancel}>
          <div className="modal" onClick={(e) => e.stopPropagation()}>
            <div className="modal-header">
              <h3>Edit Setting</h3>
              <button className="close-btn" onClick={handleCancel}>
                X
              </button>
            </div>
            <div className="modal-content">
              <div className="form-group">
                <label>{editingSetting.displayName}</label>
                <span className="help-text">{editingSetting.description}</span>
              </div>
              <div className="form-group">
                <label>Value ({editingSetting.valueType})</label>
                {editingSetting.valueType === "bool" ? (
                  <select
                    value={editValue}
                    onChange={(e) => setEditValue(e.target.value)}
                    className="form-select"
                  >
                    <option value="true">true</option>
                    <option value="false">false</option>
                  </select>
                ) : editingSetting.valueType === "json" ? (
                  <textarea
                    value={editValue}
                    onChange={(e) => setEditValue(e.target.value)}
                    className="form-textarea"
                    rows={6}
                  />
                ) : (
                  <input
                    type={editingSetting.valueType === "int" ? "number" : "text"}
                    value={editValue}
                    onChange={(e) => setEditValue(e.target.value)}
                    className="form-input"
                  />
                )}
              </div>
            </div>
            <div className="modal-footer">
              <button className="btn-secondary" onClick={handleCancel}>
                Cancel
              </button>
              <button
                className="btn-primary"
                onClick={handleSave}
                disabled={saving}
              >
                {saving ? "Saving..." : "Save"}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* History Modal */}
      {showHistory && (
        <div className="modal-overlay" onClick={() => setShowHistory(null)}>
          <div className="modal modal-wide" onClick={(e) => e.stopPropagation()}>
            <div className="modal-header">
              <h3>
                History: {showHistory.displayName}
              </h3>
              <button className="close-btn" onClick={() => setShowHistory(null)}>
                X
              </button>
            </div>
            <div className="modal-content">
              {history.length === 0 ? (
                <div className="empty-state">No history available</div>
              ) : (
                <table className="history-table">
                  <thead>
                    <tr>
                      <th>Date</th>
                      <th>Action</th>
                      <th>Old Value</th>
                      <th>New Value</th>
                      <th>Changed By</th>
                    </tr>
                  </thead>
                  <tbody>
                    {history.map((h) => (
                      <tr key={h.id}>
                        <td>{new Date(h.changedAt).toLocaleString()}</td>
                        <td>
                          <span className={`change-type ${h.changeType}`}>
                            {h.changeType}
                          </span>
                        </td>
                        <td>
                          <code>{formatHistoryValue(h.oldValue)}</code>
                        </td>
                        <td>
                          <code>{formatHistoryValue(h.newValue)}</code>
                        </td>
                        <td>{h.changedBy || "system"}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
            </div>
            <div className="modal-footer">
              <button
                className="btn-secondary"
                onClick={() => setShowHistory(null)}
              >
                Close
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
};

export default AdminSettings;
