import { FormEvent, useState, useCallback } from "react";
import { useAuth } from "../contexts/AuthContext";
import logo from "../assets/logo.svg";

const Login = () => {
  const { login } = useAuth();
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
      } catch (err) {
        const message = err instanceof Error ? err.message : String(err);
        // Clean up common error messages
        if (message.includes("invalid credentials") || message.includes("401")) {
          setError("Invalid username or password");
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
