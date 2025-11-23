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
  imageRepository: "harbor.support.tools/kubetty/kubetty",
  imageTag: "latest",
};

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
