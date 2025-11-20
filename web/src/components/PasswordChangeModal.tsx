import { FormEvent, useState, useCallback } from "react";
import { useAuth } from "../contexts/AuthContext";
import { parseErrorResponse } from "../utils/errorParser";

interface PasswordChangeModalProps {
  onClose: () => void;
  onSuccess: () => void;
}

const PasswordChangeModal = ({ onClose, onSuccess }: PasswordChangeModalProps) => {
  const { authFetch, logout } = useAuth();
  const [form, setForm] = useState({
    currentPassword: "",
    newPassword: "",
    confirmPassword: ""
  });
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  const handleSubmit = useCallback(
    async (event: FormEvent<HTMLFormElement>) => {
      event.preventDefault();

      // Validation
      if (!form.currentPassword) {
        setError("Current password is required");
        return;
      }
      if (!form.newPassword) {
        setError("New password is required");
        return;
      }
      if (form.newPassword.length < 8) {
        setError("New password must be at least 8 characters");
        return;
      }
      if (form.newPassword !== form.confirmPassword) {
        setError("New passwords do not match");
        return;
      }
      if (form.currentPassword === form.newPassword) {
        setError("New password must be different from current password");
        return;
      }

      setSubmitting(true);
      setError(null);

      try {
        const res = await authFetch("/api/auth/password", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            currentPassword: form.currentPassword,
            newPassword: form.newPassword
          })
        });

        if (!res.ok) {
          const errorMessage = await parseErrorResponse(res);
          throw new Error(errorMessage);
        }

        // Password changed successfully - user needs to log in again
        onSuccess();
        // Log out since tokens were revoked
        await logout();
      } catch (err) {
        const message = err instanceof Error ? err.message : String(err);
        if (message.includes("current password")) {
          setError("Current password is incorrect");
        } else {
          setError(message || "Failed to change password");
        }
      } finally {
        setSubmitting(false);
      }
    },
    [form, authFetch, logout, onSuccess]
  );

  const handleInputChange = useCallback(
    (field: keyof typeof form) => (event: React.ChangeEvent<HTMLInputElement>) => {
      setForm((prev) => ({ ...prev, [field]: event.target.value }));
      if (error) setError(null);
    },
    [error]
  );

  const handleBackdropClick = useCallback(
    (e: React.MouseEvent<HTMLDivElement>) => {
      if (e.target === e.currentTarget) {
        onClose();
      }
    },
    [onClose]
  );

  return (
    <div className="modal-backdrop" onClick={handleBackdropClick}>
      <div className="modal password-modal" role="dialog" aria-labelledby="password-title">
        <div className="modal-header">
          <h2 id="password-title">Change Password</h2>
          <button
            className="modal-close"
            onClick={onClose}
            aria-label="Close"
            disabled={submitting}
          >
            &times;
          </button>
        </div>
        <form onSubmit={handleSubmit}>
          <div className="modal-body">
            {error && (
              <p className="error" role="alert">
                {error}
              </p>
            )}
            <div className="form-field">
              <label htmlFor="current-password">Current Password</label>
              <input
                id="current-password"
                type="password"
                value={form.currentPassword}
                autoComplete="current-password"
                disabled={submitting}
                onChange={handleInputChange("currentPassword")}
              />
            </div>
            <div className="form-field">
              <label htmlFor="new-password">New Password</label>
              <input
                id="new-password"
                type="password"
                value={form.newPassword}
                autoComplete="new-password"
                disabled={submitting}
                onChange={handleInputChange("newPassword")}
              />
              <span className="field-hint">At least 8 characters</span>
            </div>
            <div className="form-field">
              <label htmlFor="confirm-password">Confirm New Password</label>
              <input
                id="confirm-password"
                type="password"
                value={form.confirmPassword}
                autoComplete="new-password"
                disabled={submitting}
                onChange={handleInputChange("confirmPassword")}
              />
            </div>
          </div>
          <div className="modal-footer">
            <button
              type="button"
              className="secondary"
              onClick={onClose}
              disabled={submitting}
            >
              Cancel
            </button>
            <button type="submit" disabled={submitting}>
              {submitting ? "Changing..." : "Change Password"}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
};

export default PasswordChangeModal;
