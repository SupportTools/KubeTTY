import { FormEvent, useState, useCallback } from "react";
import {
  useAuth,
  AUTH_MODE_CLOUDFLARE_ACCESS,
  AUTH_MODE_GITHUB,
  GITHUB_LOGIN_URL,
  LOCAL_LOGIN_PATH
} from "../contexts/AuthContext";
import logo from "../assets/logo.svg";

const GITHUB_ERRORS: Record<string, string> = {
  not_allowed: "Your GitHub account is not allowed to use KubeTTY. Ask an admin to add you to an allowed org or team.",
  github_denied: "GitHub sign-in was cancelled.",
  github_failed: "GitHub sign-in failed. Try again in a moment.",
  state: "The sign-in link expired. Try again."
};

function githubError(): string | null {
  const reason = new URLSearchParams(window.location.search).get("auth_error");
  if (!reason) return null;
  return GITHUB_ERRORS[reason] ?? "Sign-in failed.";
}

const Login = () => {
  const { login, authMode } = useAuth();
  const [form, setForm] = useState({ username: "", password: "" });
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  const handleSubmit = useCallback(
    async (event: FormEvent<HTMLFormElement>) => {
      event.preventDefault();

      // Basic validation
      if (!form.username.trim()) {
        setError("Username is required");
        return;
      }
      if (!form.password) {
        setError("Password is required");
        return;
      }

      setSubmitting(true);
      setError(null);

      try {
        await login(form.username.trim(), form.password);
        // Clear form on success (component will unmount anyway)
        setForm({ username: "", password: "" });
        if (window.location.pathname === LOCAL_LOGIN_PATH) {
          window.history.replaceState(null, "", "/");
        }
      } catch (err) {
        const message = err instanceof Error ? err.message : String(err);
        // Clean up common error messages
        if (message.includes("invalid credentials") || message.includes("401")) {
          setError("Invalid username or password");
        } else if (message.includes("cannot sign in with a password") || message.includes("403")) {
          setError("This account cannot sign in with a password");
        } else if (message.includes("network") || message.includes("fetch")) {
          setError("Network error. Please check your connection.");
        } else {
          setError(message || "Login failed");
        }
      } finally {
        setSubmitting(false);
      }
    },
    [form, login]
  );

  const handleInputChange = useCallback(
    (field: "username" | "password") => (event: React.ChangeEvent<HTMLInputElement>) => {
      setForm((prev) => ({ ...prev, [field]: event.target.value }));
      // Clear error when user starts typing
      if (error) setError(null);
    },
    [error]
  );

  const localForm = window.location.pathname === LOCAL_LOGIN_PATH;

  if (authMode === AUTH_MODE_GITHUB && !localForm) {
    const signInError = githubError();
    return (
      <div className="app-shell">
        <header className="header">
          <div className="brand">
            <img src={logo} alt="KubeTTY" className="logo" />
            <h1>KubeTTY</h1>
          </div>
        </header>
        <section className="main full-width auth">
          <div className="auth-card">
            <h2>Sign in</h2>
            {signInError && (
              <p className="error" role="alert">
                {signInError}
              </p>
            )}
            <button type="button" onClick={() => window.location.assign(GITHUB_LOGIN_URL)}>
              Sign in with GitHub
            </button>
          </div>
        </section>
      </div>
    );
  }

  if (authMode === AUTH_MODE_CLOUDFLARE_ACCESS) {
    // SSO mode: there is no password. Reloading sends the browser back through
    // Cloudflare Access, which signs in (Google) and returns here with a JWT.
    return (
      <div className="app-shell">
        <header className="header">
          <div className="brand">
            <img src={logo} alt="KubeTTY" className="logo" />
            <h1>KubeTTY</h1>
          </div>
        </header>
        <section className="main full-width auth">
          <div className="auth-card">
            <h2>Sign in</h2>
            <p>
              KubeTTY uses Cloudflare Access single sign-on. Your Access session has ended, or this
              request did not come through Cloudflare.
            </p>
            <button type="button" onClick={() => window.location.reload()}>
              Sign in with Cloudflare Access
            </button>
          </div>
        </section>
      </div>
    );
  }

  return (
    <div className="app-shell">
      <header className="header">
        <div className="brand">
          <img src={logo} alt="KubeTTY" className="logo" />
          <h1>KubeTTY</h1>
        </div>
      </header>
      <section className="main full-width auth">
        <form className="auth-card" onSubmit={handleSubmit}>
          <h2>Sign in</h2>
          {error && (
            <p className="error" role="alert">
              {error}
            </p>
          )}
          <label>
            Username
            <input
              type="text"
              value={form.username}
              autoComplete="username"
              autoFocus
              disabled={submitting}
              onChange={handleInputChange("username")}
            />
          </label>
          <label>
            Password
            <input
              type="password"
              value={form.password}
              autoComplete="current-password"
              disabled={submitting}
              onChange={handleInputChange("password")}
            />
          </label>
          <button type="submit" disabled={submitting}>
            {submitting ? "Signing in..." : "Sign in"}
          </button>
        </form>
      </section>
    </div>
  );
};

export default Login;
