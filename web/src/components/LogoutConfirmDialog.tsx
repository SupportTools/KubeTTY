import { useCallback, useState } from "react";
import { useAuth } from "../contexts/AuthContext";

interface LogoutConfirmDialogProps {
  onClose: () => void;
}

const LogoutConfirmDialog = ({ onClose }: LogoutConfirmDialogProps) => {
  const { logout } = useAuth();
  const [loggingOut, setLoggingOut] = useState(false);

  const handleLogout = useCallback(async () => {
    setLoggingOut(true);
    try {
      await logout();
      onClose();
    } catch {
      // Ignore errors, still close
      onClose();
    }
  }, [logout, onClose]);

  const handleBackdropClick = useCallback(
    (e: React.MouseEvent<HTMLDivElement>) => {
      if (e.target === e.currentTarget && !loggingOut) {
        onClose();
      }
    },
    [onClose, loggingOut]
  );

  return (
    <div className="modal-backdrop" onClick={handleBackdropClick}>
      <div className="modal confirm-dialog" role="alertdialog" aria-labelledby="logout-title">
        <div className="modal-header">
          <h2 id="logout-title">Confirm Logout</h2>
        </div>
        <div className="modal-body">
          <p>Are you sure you want to log out?</p>
          <p className="hint">Any unsaved work in open terminals will be preserved on the server.</p>
        </div>
        <div className="modal-footer">
          <button
            className="secondary"
            onClick={onClose}
            disabled={loggingOut}
          >
            Cancel
          </button>
          <button
            className="danger"
            onClick={handleLogout}
            disabled={loggingOut}
          >
            {loggingOut ? "Logging out..." : "Logout"}
          </button>
        </div>
      </div>
    </div>
  );
};

export default LogoutConfirmDialog;
