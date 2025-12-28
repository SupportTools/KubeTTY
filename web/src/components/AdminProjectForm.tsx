import { useState, useCallback, FormEvent } from "react";
import { useAuth } from "../contexts/AuthContext";
import { CreateProjectRequest } from "../types";
import { parseErrorResponse } from "../utils/errorParser";

interface Props {
  onClose: () => void;
  onSuccess: () => void;
}

const defaultValues: CreateProjectRequest = {
  name: "",
  displayName: "",
  userName: "",
  description: "",
  cpuRequest: "500m",
  cpuLimit: "4000m",
  memoryRequest: "2Gi",
  memoryLimit: "8Gi",
  storageSize: "50Gi",
  storageClass: "longhorn",
  maxTabsPerClient: 3,
  maxTabsTotal: 10,
  dindEnabled: true,
  guiEnabled: false,
  guiResolution: "1280x720x24",
  // PTY session logging defaults
  ptyLoggingEnabled: false,
  ptyLoggingMaxSize: 104857600,      // 100MB
  ptyLoggingMaxBackups: 3,
  ptyLoggingBufferSize: 65536,       // 64KB
  ptyLoggingFlushInterval: "5s",
  ptyLoggingIncludeRaw: true,
  imageRepository: "harbor.support.tools/kubetty/kubetty",
  imageTag: "latest",
};

const GUI_RESOLUTIONS = [
  { value: "1024x768x24", label: "1024×768 (XGA)" },
  { value: "1280x720x24", label: "1280×720 (HD)" },
  { value: "1280x1024x24", label: "1280×1024 (SXGA)" },
  { value: "1920x1080x24", label: "1920×1080 (Full HD)" },
];

const AdminProjectForm = ({ onClose, onSuccess }: Props) => {
  const { authFetch, user } = useAuth();
  const [form, setForm] = useState<CreateProjectRequest>({
    ...defaultValues,
    userName: user?.username || "",
  });
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [showAdvanced, setShowAdvanced] = useState(false);

  const handleSubmit = useCallback(
    async (e: FormEvent) => {
      e.preventDefault();
      setError(null);

      // Validation
      if (!form.name.trim()) {
        setError("Project name is required");
        return;
      }
      if (!/^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/.test(form.name)) {
        setError("Name must be lowercase alphanumeric with dashes, starting and ending with alphanumeric");
        return;
      }
      if (!form.displayName.trim()) {
        setError("Display name is required");
        return;
      }
      if (!form.userName.trim()) {
        setError("Username is required");
        return;
      }

      setSubmitting(true);
      try {
        const res = await authFetch("/api/admin/projects", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(form),
        });
        if (!res.ok) {
          throw new Error(await parseErrorResponse(res));
        }
        onSuccess();
      } catch (err) {
        setError(err instanceof Error ? err.message : "Failed to create project");
      } finally {
        setSubmitting(false);
      }
    },
    [form, authFetch, onSuccess]
  );

  const updateField = <K extends keyof CreateProjectRequest>(
    field: K,
    value: CreateProjectRequest[K]
  ) => {
    setForm((prev) => ({ ...prev, [field]: value }));
    if (error) setError(null);
  };

  return (
    <div className="modal-backdrop">
      <div className="modal admin-modal admin-form-modal">
        <div className="modal-header">
          <h2>Create New Project</h2>
          <button className="icon-button" onClick={onClose}>
            &times;
          </button>
        </div>
        <form onSubmit={handleSubmit}>
          <div className="modal-body">
            {error && <p className="error">{error}</p>}

            <div className="form-section">
              <h3>Basic Information</h3>
              <div className="form-field">
                <label>
                  Project Name <span className="required">*</span>
                </label>
                <input
                  type="text"
                  value={form.name}
                  placeholder="my-project"
                  disabled={submitting}
                  onChange={(e) => updateField("name", e.target.value.toLowerCase())}
                />
                <span className="field-hint">
                  Lowercase alphanumeric with dashes (e.g., my-project-1)
                </span>
              </div>
              <div className="form-field">
                <label>
                  Display Name <span className="required">*</span>
                </label>
                <input
                  type="text"
                  value={form.displayName}
                  placeholder="My Project"
                  disabled={submitting}
                  onChange={(e) => updateField("displayName", e.target.value)}
                />
              </div>
              <div className="form-field">
                <label>
                  Username <span className="required">*</span>
                </label>
                <input
                  type="text"
                  value={form.userName}
                  placeholder="username"
                  disabled={submitting}
                  onChange={(e) => updateField("userName", e.target.value)}
                />
                <span className="field-hint">The user who will own this project</span>
              </div>
              <div className="form-field">
                <label>Description</label>
                <textarea
                  value={form.description || ""}
                  placeholder="Optional project description"
                  disabled={submitting}
                  onChange={(e) => updateField("description", e.target.value)}
                />
              </div>
            </div>

            <div className="form-section">
              <button
                type="button"
                className="section-toggle"
                onClick={() => setShowAdvanced(!showAdvanced)}
              >
                {showAdvanced ? "- Hide" : "+ Show"} Advanced Settings
              </button>

              {showAdvanced && (
                <>
                  <h3>Resource Limits</h3>
                  <div className="form-row">
                    <div className="form-field">
                      <label>CPU Request</label>
                      <input
                        type="text"
                        value={form.cpuRequest}
                        disabled={submitting}
                        onChange={(e) => updateField("cpuRequest", e.target.value)}
                      />
                    </div>
                    <div className="form-field">
                      <label>CPU Limit</label>
                      <input
                        type="text"
                        value={form.cpuLimit}
                        disabled={submitting}
                        onChange={(e) => updateField("cpuLimit", e.target.value)}
                      />
                    </div>
                  </div>
                  <div className="form-row">
                    <div className="form-field">
                      <label>Memory Request</label>
                      <input
                        type="text"
                        value={form.memoryRequest}
                        disabled={submitting}
                        onChange={(e) => updateField("memoryRequest", e.target.value)}
                      />
                    </div>
                    <div className="form-field">
                      <label>Memory Limit</label>
                      <input
                        type="text"
                        value={form.memoryLimit}
                        disabled={submitting}
                        onChange={(e) => updateField("memoryLimit", e.target.value)}
                      />
                    </div>
                  </div>
                  <div className="form-row">
                    <div className="form-field">
                      <label>Storage Size</label>
                      <input
                        type="text"
                        value={form.storageSize}
                        disabled={submitting}
                        onChange={(e) => updateField("storageSize", e.target.value)}
                      />
                    </div>
                    <div className="form-field">
                      <label>Storage Class</label>
                      <input
                        type="text"
                        value={form.storageClass}
                        disabled={submitting}
                        onChange={(e) => updateField("storageClass", e.target.value)}
                      />
                    </div>
                  </div>

                  <h3>Tab Limits</h3>
                  <div className="form-row">
                    <div className="form-field">
                      <label>Max Tabs Per Client</label>
                      <input
                        type="number"
                        min={1}
                        max={10}
                        value={form.maxTabsPerClient}
                        disabled={submitting}
                        onChange={(e) => updateField("maxTabsPerClient", parseInt(e.target.value) || 3)}
                      />
                    </div>
                    <div className="form-field">
                      <label>Max Tabs Total</label>
                      <input
                        type="number"
                        min={1}
                        max={50}
                        value={form.maxTabsTotal}
                        disabled={submitting}
                        onChange={(e) => updateField("maxTabsTotal", parseInt(e.target.value) || 10)}
                      />
                    </div>
                  </div>

                  <h3>Image Configuration</h3>
                  <div className="form-field">
                    <label>Image Repository</label>
                    <input
                      type="text"
                      value={form.imageRepository}
                      disabled={submitting}
                      onChange={(e) => updateField("imageRepository", e.target.value)}
                    />
                  </div>
                  <div className="form-field">
                    <label>Image Tag</label>
                    <input
                      type="text"
                      value={form.imageTag}
                      disabled={submitting}
                      onChange={(e) => updateField("imageTag", e.target.value)}
                    />
                  </div>

                  <h3>Features</h3>
                  <div className="form-field checkbox-field">
                    <label>
                      <input
                        type="checkbox"
                        checked={form.dindEnabled}
                        disabled={submitting}
                        onChange={(e) => updateField("dindEnabled", e.target.checked)}
                      />
                      Enable Docker-in-Docker
                    </label>
                  </div>
                  <div className="form-field checkbox-field">
                    <label>
                      <input
                        type="checkbox"
                        checked={form.guiEnabled || false}
                        disabled={submitting}
                        onChange={(e) => updateField("guiEnabled", e.target.checked)}
                      />
                      Enable GUI Desktop
                    </label>
                    <span className="field-hint">
                      Provides a graphical desktop environment via VNC
                    </span>
                  </div>
                  {form.guiEnabled && (
                    <div className="form-field">
                      <label>GUI Resolution</label>
                      <select
                        value={form.guiResolution || "1280x720x24"}
                        disabled={submitting}
                        onChange={(e) => updateField("guiResolution", e.target.value)}
                      >
                        {GUI_RESOLUTIONS.map((res) => (
                          <option key={res.value} value={res.value}>
                            {res.label}
                          </option>
                        ))}
                      </select>
                    </div>
                  )}

                  <h3>PTY Session Logging</h3>
                  <div className="form-field checkbox-field">
                    <label>
                      <input
                        type="checkbox"
                        checked={form.ptyLoggingEnabled || false}
                        disabled={submitting}
                        onChange={(e) => updateField("ptyLoggingEnabled", e.target.checked)}
                      />
                      Enable PTY Session Logging
                    </label>
                    <span className="field-hint">
                      Captures terminal I/O to JSONL files for Loki/Grafana integration
                    </span>
                  </div>
                  {form.ptyLoggingEnabled && (
                    <>
                      <div className="form-row">
                        <div className="form-field">
                          <label>Max Log Size (MB)</label>
                          <input
                            type="number"
                            min={10}
                            max={1000}
                            value={Math.round((form.ptyLoggingMaxSize || 104857600) / 1048576)}
                            disabled={submitting}
                            onChange={(e) => updateField("ptyLoggingMaxSize", parseInt(e.target.value) * 1048576 || 104857600)}
                          />
                          <span className="field-hint">File rotates at this size</span>
                        </div>
                        <div className="form-field">
                          <label>Max Backups</label>
                          <input
                            type="number"
                            min={1}
                            max={10}
                            value={form.ptyLoggingMaxBackups || 3}
                            disabled={submitting}
                            onChange={(e) => updateField("ptyLoggingMaxBackups", parseInt(e.target.value) || 3)}
                          />
                          <span className="field-hint">Rotated files to keep</span>
                        </div>
                      </div>
                      <div className="form-row">
                        <div className="form-field">
                          <label>Buffer Size (KB)</label>
                          <input
                            type="number"
                            min={8}
                            max={256}
                            value={Math.round((form.ptyLoggingBufferSize || 65536) / 1024)}
                            disabled={submitting}
                            onChange={(e) => updateField("ptyLoggingBufferSize", parseInt(e.target.value) * 1024 || 65536)}
                          />
                          <span className="field-hint">Write buffer size</span>
                        </div>
                        <div className="form-field">
                          <label>Flush Interval</label>
                          <input
                            type="text"
                            value={form.ptyLoggingFlushInterval || "5s"}
                            disabled={submitting}
                            onChange={(e) => updateField("ptyLoggingFlushInterval", e.target.value)}
                          />
                          <span className="field-hint">e.g., 5s, 10s, 1m</span>
                        </div>
                      </div>
                      <div className="form-field checkbox-field">
                        <label>
                          <input
                            type="checkbox"
                            checked={form.ptyLoggingIncludeRaw !== false}
                            disabled={submitting}
                            onChange={(e) => updateField("ptyLoggingIncludeRaw", e.target.checked)}
                          />
                          Include Raw Bytes
                        </label>
                        <span className="field-hint">
                          Include base64-encoded raw terminal data (preserves escape sequences)
                        </span>
                      </div>
                    </>
                  )}
                </>
              )}
            </div>
          </div>
          <div className="modal-actions">
            <button type="button" className="secondary" onClick={onClose} disabled={submitting}>
              Cancel
            </button>
            <button type="submit" className="primary-button" disabled={submitting}>
              {submitting ? "Creating..." : "Create Project"}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
};

export default AdminProjectForm;
