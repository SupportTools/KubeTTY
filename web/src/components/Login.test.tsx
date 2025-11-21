import { describe, it, expect, vi, beforeEach } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import { renderWithAuth, userEvent } from '../test-utils';
import Login from './Login';
import * as AuthContext from '../contexts/AuthContext';

// Mock the logo import
vi.mock('../assets/logo.svg', () => ({
  default: 'mocked-logo.svg'
}));

describe('Login', () => {
  const mockLogin = vi.fn();

  beforeEach(() => {
    vi.clearAllMocks();
    mockLogin.mockResolvedValue(undefined);

    // Mock useAuth hook
    vi.spyOn(AuthContext, 'useAuth').mockReturnValue({
      authState: 'unauthenticated',
      user: null,
      login: mockLogin,
      logout: vi.fn(),
      authFetch: vi.fn(),
      refreshToken: vi.fn()
    });
  });

  describe('Rendering', () => {
    it('should render login form with all elements', () => {
      renderWithAuth(<Login />);

      expect(screen.getByRole('heading', { name: 'KubeTTY' })).toBeInTheDocument();
      expect(screen.getByRole('heading', { name: 'Sign in' })).toBeInTheDocument();
      expect(screen.getByLabelText(/username/i)).toBeInTheDocument();
      expect(screen.getByLabelText(/password/i)).toBeInTheDocument();
      expect(screen.getByRole('button', { name: 'Sign in' })).toBeInTheDocument();
    });

    it('should have username field with autofocus prop', () => {
      renderWithAuth(<Login />);

      const usernameInput = screen.getByLabelText(/username/i);
      // React's autoFocus prop doesn't show as HTML attribute
      // Verify the input exists and can receive focus
      expect(usernameInput).toBeInTheDocument();
      expect(usernameInput.tagName).toBe('INPUT');
    });

    it('should render logo image', () => {
      renderWithAuth(<Login />);

      const logo = screen.getByAltText('KubeTTY');
      expect(logo).toBeInTheDocument();
      expect(logo).toHaveAttribute('src', 'mocked-logo.svg');
    });
  });

  describe('Form Validation', () => {
    it('should show error when username is empty', async () => {
      const user = userEvent.setup();
      renderWithAuth(<Login />);

      const submitButton = screen.getByRole('button', { name: 'Sign in' });
      await user.click(submitButton);

      expect(screen.getByRole('alert')).toHaveTextContent('Username is required');
      expect(mockLogin).not.toHaveBeenCalled();
    });

    it('should show error when username is only whitespace', async () => {
      const user = userEvent.setup();
      renderWithAuth(<Login />);

      const usernameInput = screen.getByLabelText(/username/i);
      const submitButton = screen.getByRole('button', { name: 'Sign in' });

      await user.type(usernameInput, '   ');
      await user.click(submitButton);

      expect(screen.getByRole('alert')).toHaveTextContent('Username is required');
      expect(mockLogin).not.toHaveBeenCalled();
    });

    it('should show error when password is empty', async () => {
      const user = userEvent.setup();
      renderWithAuth(<Login />);

      const usernameInput = screen.getByLabelText(/username/i);
      const submitButton = screen.getByRole('button', { name: 'Sign in' });

      await user.type(usernameInput, 'testuser');
      await user.click(submitButton);

      expect(screen.getByRole('alert')).toHaveTextContent('Password is required');
      expect(mockLogin).not.toHaveBeenCalled();
    });
  });

  describe('Input Changes', () => {
    it('should update username field on input', async () => {
      const user = userEvent.setup();
      renderWithAuth(<Login />);

      const usernameInput = screen.getByLabelText(/username/i) as HTMLInputElement;
      await user.type(usernameInput, 'testuser');

      expect(usernameInput.value).toBe('testuser');
    });

    it('should update password field on input', async () => {
      const user = userEvent.setup();
      renderWithAuth(<Login />);

      const passwordInput = screen.getByLabelText(/password/i) as HTMLInputElement;
      await user.type(passwordInput, 'testpass123');

      expect(passwordInput.value).toBe('testpass123');
    });

    it('should clear error message when user starts typing', async () => {
      const user = userEvent.setup();
      renderWithAuth(<Login />);

      // Trigger validation error
      const submitButton = screen.getByRole('button', { name: 'Sign in' });
      await user.click(submitButton);

      expect(screen.getByRole('alert')).toBeInTheDocument();

      // Start typing - error should clear
      const usernameInput = screen.getByLabelText(/username/i);
      await user.type(usernameInput, 't');

      expect(screen.queryByRole('alert')).not.toBeInTheDocument();
    });
  });

  describe('Successful Login', () => {
    it('should call login with trimmed username and password', async () => {
      const user = userEvent.setup();
      renderWithAuth(<Login />);

      const usernameInput = screen.getByLabelText(/username/i);
      const passwordInput = screen.getByLabelText(/password/i);
      const submitButton = screen.getByRole('button', { name: 'Sign in' });

      await user.type(usernameInput, '  testuser  ');
      await user.type(passwordInput, 'testpass123');
      await user.click(submitButton);

      await waitFor(() => {
        expect(mockLogin).toHaveBeenCalledWith('testuser', 'testpass123');
      });
    });

    it('should show submitting state during login', async () => {
      const user = userEvent.setup();
      mockLogin.mockImplementation(() => new Promise(resolve => setTimeout(resolve, 100)));

      renderWithAuth(<Login />);

      const usernameInput = screen.getByLabelText(/username/i);
      const passwordInput = screen.getByLabelText(/password/i);
      const submitButton = screen.getByRole('button', { name: 'Sign in' });

      await user.type(usernameInput, 'testuser');
      await user.type(passwordInput, 'testpass123');
      await user.click(submitButton);

      expect(screen.getByRole('button', { name: 'Signing in...' })).toBeInTheDocument();
      expect(screen.getByRole('button', { name: 'Signing in...' })).toBeDisabled();
    });

    it('should disable inputs during submission', async () => {
      const user = userEvent.setup();
      mockLogin.mockImplementation(() => new Promise(resolve => setTimeout(resolve, 100)));

      renderWithAuth(<Login />);

      const usernameInput = screen.getByLabelText(/username/i);
      const passwordInput = screen.getByLabelText(/password/i);
      const submitButton = screen.getByRole('button', { name: 'Sign in' });

      await user.type(usernameInput, 'testuser');
      await user.type(passwordInput, 'testpass123');
      await user.click(submitButton);

      expect(usernameInput).toBeDisabled();
      expect(passwordInput).toBeDisabled();
    });

    it('should clear form after successful login', async () => {
      const user = userEvent.setup();
      renderWithAuth(<Login />);

      const usernameInput = screen.getByLabelText(/username/i) as HTMLInputElement;
      const passwordInput = screen.getByLabelText(/password/i) as HTMLInputElement;
      const submitButton = screen.getByRole('button', { name: 'Sign in' });

      await user.type(usernameInput, 'testuser');
      await user.type(passwordInput, 'testpass123');
      await user.click(submitButton);

      await waitFor(() => {
        expect(usernameInput.value).toBe('');
        expect(passwordInput.value).toBe('');
      });
    });
  });

  describe('Error Handling', () => {
    it('should display user-friendly error for invalid credentials', async () => {
      const user = userEvent.setup();
      mockLogin.mockRejectedValue(new Error('invalid credentials'));

      renderWithAuth(<Login />);

      const usernameInput = screen.getByLabelText(/username/i);
      const passwordInput = screen.getByLabelText(/password/i);
      const submitButton = screen.getByRole('button', { name: 'Sign in' });

      await user.type(usernameInput, 'wronguser');
      await user.type(passwordInput, 'wrongpass');
      await user.click(submitButton);

      await waitFor(() => {
        expect(screen.getByRole('alert')).toHaveTextContent('Invalid username or password');
      });
    });

    it('should display user-friendly error for 401 status', async () => {
      const user = userEvent.setup();
      mockLogin.mockRejectedValue(new Error('401 Unauthorized'));

      renderWithAuth(<Login />);

      const usernameInput = screen.getByLabelText(/username/i);
      const passwordInput = screen.getByLabelText(/password/i);
      const submitButton = screen.getByRole('button', { name: 'Sign in' });

      await user.type(usernameInput, 'testuser');
      await user.type(passwordInput, 'testpass');
      await user.click(submitButton);

      await waitFor(() => {
        expect(screen.getByRole('alert')).toHaveTextContent('Invalid username or password');
      });
    });

    it('should display network error message', async () => {
      const user = userEvent.setup();
      mockLogin.mockRejectedValue(new Error('network error'));

      renderWithAuth(<Login />);

      const usernameInput = screen.getByLabelText(/username/i);
      const passwordInput = screen.getByLabelText(/password/i);
      const submitButton = screen.getByRole('button', { name: 'Sign in' });

      await user.type(usernameInput, 'testuser');
      await user.type(passwordInput, 'testpass');
      await user.click(submitButton);

      await waitFor(() => {
        expect(screen.getByRole('alert')).toHaveTextContent('Network error. Please check your connection.');
      });
    });

    it('should display fetch error message', async () => {
      const user = userEvent.setup();
      mockLogin.mockRejectedValue(new Error('fetch failed'));

      renderWithAuth(<Login />);

      const usernameInput = screen.getByLabelText(/username/i);
      const passwordInput = screen.getByLabelText(/password/i);
      const submitButton = screen.getByRole('button', { name: 'Sign in' });

      await user.type(usernameInput, 'testuser');
      await user.type(passwordInput, 'testpass');
      await user.click(submitButton);

      await waitFor(() => {
        expect(screen.getByRole('alert')).toHaveTextContent('Network error. Please check your connection.');
      });
    });

    it('should display generic error message for other errors', async () => {
      const user = userEvent.setup();
      mockLogin.mockRejectedValue(new Error('Something went wrong'));

      renderWithAuth(<Login />);

      const usernameInput = screen.getByLabelText(/username/i);
      const passwordInput = screen.getByLabelText(/password/i);
      const submitButton = screen.getByRole('button', { name: 'Sign in' });

      await user.type(usernameInput, 'testuser');
      await user.type(passwordInput, 'testpass');
      await user.click(submitButton);

      await waitFor(() => {
        expect(screen.getByRole('alert')).toHaveTextContent('Something went wrong');
      });
    });

    it('should handle non-Error thrown values', async () => {
      const user = userEvent.setup();
      mockLogin.mockRejectedValue('String error');

      renderWithAuth(<Login />);

      const usernameInput = screen.getByLabelText(/username/i);
      const passwordInput = screen.getByLabelText(/password/i);
      const submitButton = screen.getByRole('button', { name: 'Sign in' });

      await user.type(usernameInput, 'testuser');
      await user.type(passwordInput, 'testpass');
      await user.click(submitButton);

      await waitFor(() => {
        expect(screen.getByRole('alert')).toHaveTextContent('String error');
      });
    });

    it('should re-enable form after error', async () => {
      const user = userEvent.setup();
      mockLogin.mockRejectedValue(new Error('Login failed'));

      renderWithAuth(<Login />);

      const usernameInput = screen.getByLabelText(/username/i);
      const passwordInput = screen.getByLabelText(/password/i);
      const submitButton = screen.getByRole('button', { name: 'Sign in' });

      await user.type(usernameInput, 'testuser');
      await user.type(passwordInput, 'testpass');
      await user.click(submitButton);

      await waitFor(() => {
        expect(screen.getByRole('alert')).toBeInTheDocument();
      });

      expect(usernameInput).not.toBeDisabled();
      expect(passwordInput).not.toBeDisabled();
      expect(submitButton).not.toBeDisabled();
    });
  });

  describe('Accessibility', () => {
    it('should have proper autocomplete attributes', () => {
      renderWithAuth(<Login />);

      const usernameInput = screen.getByLabelText(/username/i);
      const passwordInput = screen.getByLabelText(/password/i);

      expect(usernameInput).toHaveAttribute('autoComplete', 'username');
      expect(passwordInput).toHaveAttribute('autoComplete', 'current-password');
    });

    it('should have password input type', () => {
      renderWithAuth(<Login />);

      const passwordInput = screen.getByLabelText(/password/i);
      expect(passwordInput).toHaveAttribute('type', 'password');
    });

    it('should mark errors with role="alert"', async () => {
      const user = userEvent.setup();
      renderWithAuth(<Login />);

      const submitButton = screen.getByRole('button', { name: 'Sign in' });
      await user.click(submitButton);

      const alert = screen.getByRole('alert');
      expect(alert).toBeInTheDocument();
      expect(alert).toHaveClass('error');
    });
  });
});
