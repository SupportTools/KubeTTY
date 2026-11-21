import { useCallback } from "react";
import { useAuth } from "../contexts/AuthContext";

interface ProfileModalProps {
  onClose: () => void;
  onPasswordChange: () => void;
}

const ProfileModal = ({ onClose, onPasswordChange }: ProfileModalProps) => {
  const { user, logout } = useAuth();

  const handleLogout = useCallback(async () => {
    await logout();
    onClose();
  }, [logout, onClose]);

  const handlePasswordChange = useCallback(() => {
    onPasswordChange();
  }, [onPasswordChange]);

  const handleBackdropClick = useCallback(
    (e: React.MouseEvent<HTMLDivElement>) => {
      if (e.target === e.currentTarget) {
        onClose();
      }
    },
    [onClose]
  );

  if (!user) {
    return null;
  }

  return (
    <div className="modal-backdrop" onClick={handleBackdropClick}>
      <div className="modal profile-modal" role="dialog" aria-labelledby="profile-title">
        <div className="modal-header">
          <h2 id="profile-title">User Profile</h2>
          <button
            className="modal-close"
            onClick={onClose}
            aria-label="Close profile"
          >
            &times;
          </button>
        </div>
        <div className="modal-body">
          <div className="profile-info">
            <div className="profile-avatar">
              {user.username.charAt(0).toUpperCase()}
            </div>
            <div className="profile-details">
              <div className="profile-field">
                <label>Username</label>
                <span>{user.username}</span>
              </div>
              <div className="profile-field">
                <label>User ID</label>
                <span className="user-id">{user.id}</span>
              </div>
            </div>
          </div>
        </div>
        <div className="modal-footer">
          <button className="secondary" onClick={handlePasswordChange}>
            Change Password
          </button>
          <button className="danger" onClick={handleLogout}>
            Logout
          </button>
        </div>
      </div>
    </div>
  );
};

export default ProfileModal;
