import {
  createContext,
  useContext,
  useState,
  useCallback,
  useEffect,
  useRef,
  ReactNode
} from "react";
import { parseErrorResponse } from "../utils/errorParser";

export type AuthState = "checking" | "authenticated" | "unauthenticated";
export type AuthUser = { id: string; username: string; role?: string; githubLogin?: string } | null;

// Server auth mode. "cloudflare-access" (cluster-services-1299): identity comes
// from the Cloudflare Access JWT; there is no password form and logout goes to
// Cloudflare's /cdn-cgi/access/logout.
export const AUTH_MODE_CLOUDFLARE_ACCESS = "cloudflare-access";
export const ACCESS_LOGOUT_URL = "/cdn-cgi/access/logout";
export const AUTH_MODE_GITHUB = "github";
export const GITHUB_LOGIN_URL = "/api/auth/github/login";
// The break-glass password form is served only at this path in GitHub mode.
export const LOCAL_LOGIN_PATH = "/login/local";

interface AuthContextType {
  authState: AuthState;
  user: AuthUser;
  login: (username: string, password: string) => Promise<void>;
  logout: () => Promise<void>;
  authFetch: (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>;
  refreshToken: () => Promise<boolean>;
  authMode?: string | null;
  isAdmin: boolean;
}

const AuthContext = createContext<AuthContextType | null>(null);

// Parse JWT to get expiry time (without verifying signature)
function getTokenExpiry(token: string): number | null {
  try {
    const parts = token.split(".");
    if (parts.length !== 3) return null;
    const payload = JSON.parse(atob(parts[1]));
    return payload.exp ? payload.exp * 1000 : null; // Convert to milliseconds
  } catch {
    return null;
  }
}

// Time before expiry to trigger refresh (2 minutes)
const REFRESH_BUFFER_MS = 2 * 60 * 1000;

// Minimum time between visibility-triggered refreshes (30 seconds)
const VISIBILITY_REFRESH_COOLDOWN_MS = 30 * 1000;

// Debug logging helper
const authLog = (message: string, ...args: unknown[]) => {
  const timestamp = new Date().toISOString();
  console.log(`[Auth ${timestamp}] ${message}`, ...args);
};

export function AuthProvider({ children }: { children: ReactNode }) {
  const [authState, setAuthState] = useState<AuthState>("checking");
  const [user, setUser] = useState<AuthUser>(null);
  const [authMode, setAuthMode] = useState<string | null>(null);
  const refreshTimerRef = useRef<number | null>(null);
  const isRefreshingRef = useRef(false);
  const lastVisibilityRefreshRef = useRef<number>(0);

  // Clear any scheduled refresh
  const clearRefreshTimer = useCallback(() => {
    if (refreshTimerRef.current) {
      clearTimeout(refreshTimerRef.current);
      refreshTimerRef.current = null;
    }
  }, []);

  // Schedule token refresh based on expiry time
  const scheduleRefresh = useCallback((expiryMs: number) => {
    clearRefreshTimer();

    const now = Date.now();
    const refreshAt = expiryMs - REFRESH_BUFFER_MS;
    const delay = Math.max(0, refreshAt - now);

    authLog(`scheduleRefresh called: expiryMs=${expiryMs}, now=${now}, delay=${delay}ms (${Math.round(delay/1000/60)}min)`);

    if (delay > 0) {
      authLog(`Setting setTimeout for ${delay}ms`);
      refreshTimerRef.current = window.setTimeout(async () => {
        authLog(`setTimeout FIRED after ${delay}ms delay`);
        await refreshToken();
      }, delay);
    } else if (expiryMs > now) {
      // Token still valid but within buffer - refresh immediately
      authLog(`Token within buffer, refreshing immediately`);
      refreshToken();
    } else {
      authLog(`Token already expired, not scheduling refresh`);
    }
  }, [clearRefreshTimer]);

  // Refresh the access token
  const refreshToken = useCallback(async (): Promise<boolean> => {
    if (isRefreshingRef.current) {
      authLog(`refreshToken called but already refreshing, skipping`);
      return false;
    }

    authLog(`refreshToken starting...`);
    isRefreshingRef.current = true;
    try {
      const res = await fetch("/api/auth/refresh", {
        method: "POST",
        credentials: "include"
      });

      if (res.status === 503) {
        // Sign-in checks are briefly unavailable (database or GitHub); keep the session and retry.
        authLog(`refreshToken: 503, retrying in 30s`);
        scheduleRefresh(Date.now() + REFRESH_BUFFER_MS + 30 * 1000);
        return false;
      }

      if (!res.ok) {
        authLog(`refreshToken FAILED: status=${res.status}`);
        setAuthState("unauthenticated");
        setUser(null);
        clearRefreshTimer();
        return false;
      }

      const data = await res.json();
      authLog(`refreshToken SUCCESS, user=${data.user?.username}`);

      // Update user if returned
      if (data.user) {
        setUser(data.user);
      }

      // Schedule next refresh if we can determine expiry
      // The cookie is HttpOnly so we can't read it directly
      // Use the default access TTL (15m) minus buffer
      const nextRefresh = Date.now() + (15 * 60 * 1000); // 15 minutes
      scheduleRefresh(nextRefresh);

      return true;
    } catch (err) {
      authLog(`refreshToken EXCEPTION:`, err);
      setAuthState("unauthenticated");
      setUser(null);
      clearRefreshTimer();
      return false;
    } finally {
      isRefreshingRef.current = false;
    }
  }, [clearRefreshTimer, scheduleRefresh]);

  // Authenticated fetch wrapper
  const authFetch = useCallback(
    async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
      const response = await fetch(input, { credentials: "include", ...(init || {}) });

      if (response.status === 401) {
        authLog(`authFetch: got 401 for ${input}, attempting refresh...`);
        // Try to refresh token once
        const refreshed = await refreshToken();
        if (refreshed) {
          authLog(`authFetch: refresh succeeded, retrying request`);
          // Retry the original request
          return fetch(input, { credentials: "include", ...(init || {}) });
        }
        authLog(`authFetch: refresh failed, setting unauthenticated`);
        setAuthState("unauthenticated");
        setUser(null);
      }

      return response;
    },
    [refreshToken]
  );

  // Login function
  const login = useCallback(async (username: string, password: string): Promise<void> => {
    authLog(`login: attempting login for ${username}`);
    const res = await fetch("/api/auth/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      credentials: "include",
      body: JSON.stringify({ username, password })
    });

    if (!res.ok) {
      authLog(`login: failed with status=${res.status}`);
      const errorMessage = await parseErrorResponse(res);
      throw new Error(errorMessage);
    }

    const data = await res.json();
    authLog(`login: success for ${data.user?.username}`);
    setAuthState("authenticated");
    setUser(data.user);

    // Schedule token refresh (15m access TTL)
    const tokenExpiry = Date.now() + (15 * 60 * 1000);
    scheduleRefresh(tokenExpiry);
  }, [scheduleRefresh]);

  // Logout function
  const logout = useCallback(async (): Promise<void> => {
    clearRefreshTimer();
    if (authMode === AUTH_MODE_CLOUDFLARE_ACCESS) {
      // The Access session is the login: end it at Cloudflare.
      window.location.assign(ACCESS_LOGOUT_URL);
      return;
    }
    try {
      await fetch("/api/auth/logout", { method: "POST", credentials: "include" });
    } catch {
      // Ignore errors during logout
    }
    setAuthState("unauthenticated");
    setUser(null);
  }, [clearRefreshTimer, authMode]);

  // Initial auth check on mount
  useEffect(() => {
    let cancelled = false;

    const probeAuth = async () => {
      authLog(`probeAuth: checking initial auth state...`);
      fetch("/api/auth/config", { credentials: "include" })
        .then((res) => (res.ok ? res.json() : null))
        .then((cfg) => {
          if (!cancelled && cfg && typeof cfg.authMode === "string") {
            setAuthMode(cfg.authMode);
          }
        })
        .catch(() => undefined);
      try {
        const res = await fetch("/api/auth/me", { credentials: "include" });
        authLog(`probeAuth: /api/auth/me returned status=${res.status}`);
        const headerMode = res.headers?.get?.("X-Kubetty-Auth-Mode");
        if (headerMode && !cancelled) {
          setAuthMode(headerMode);
        }

        // If auth endpoints don't exist, assume authenticated (disabled auth mode)
        if (res.status === 404 || res.status === 405) {
          authLog(`probeAuth: auth disabled (404/405), setting authenticated`);
          if (!cancelled) {
            setAuthState("authenticated");
            setUser(null);
          }
          return;
        }

        if (res.status === 401) {
          // Access token expired - try to refresh using the refresh token
          authLog(`probeAuth: got 401, attempting refresh...`);
          const refreshed = await refreshToken();
          if (!cancelled) {
            if (refreshed) {
              authLog(`probeAuth: refresh succeeded, setting authenticated`);
              setAuthState("authenticated");
              // User is already set by refreshToken()
            } else {
              authLog(`probeAuth: refresh failed, setting unauthenticated`);
              setAuthState("unauthenticated");
              setUser(null);
            }
          }
          return;
        }

        if (!res.ok) {
          throw new Error("unauthorized");
        }

        const data = await res.json();
        authLog(`probeAuth: authenticated as ${data.user?.username}`);
        if (!cancelled) {
          if (typeof data.authMode === "string") {
            setAuthMode(data.authMode);
          }
          setAuthState("authenticated");
          setUser(data.user);

          // Schedule token refresh (assume 15m from now if we can't parse token)
          const tokenExpiry = Date.now() + (15 * 60 * 1000);
          scheduleRefresh(tokenExpiry);
        }
      } catch (err) {
        authLog(`probeAuth: exception`, err);
        if (!cancelled) {
          setAuthState("unauthenticated");
          setUser(null);
        }
      }
    };

    probeAuth();

    return () => {
      cancelled = true;
      clearRefreshTimer();
    };
  }, [scheduleRefresh, clearRefreshTimer, refreshToken]);

  // Visibility change handler - refresh token when tab becomes visible
  useEffect(() => {
    const handleVisibilityChange = async () => {
      const now = Date.now();
      const timeSinceLastRefresh = now - lastVisibilityRefreshRef.current;
      authLog(`Visibility changed to: ${document.visibilityState}, authState=${authState}, timeSinceLastRefresh=${timeSinceLastRefresh}ms`);

      if (document.visibilityState === "visible" && authState === "authenticated") {
        // Avoid refreshing too frequently
        if (timeSinceLastRefresh > VISIBILITY_REFRESH_COOLDOWN_MS) {
          authLog(`Visibility refresh: triggering refresh (cooldown passed)`);
          lastVisibilityRefreshRef.current = now;
          // Proactively refresh when tab becomes visible to ensure token is valid
          await refreshToken();
        } else {
          authLog(`Visibility refresh: skipped (cooldown not passed, ${VISIBILITY_REFRESH_COOLDOWN_MS - timeSinceLastRefresh}ms remaining)`);
        }
      }
    };

    document.addEventListener("visibilitychange", handleVisibilityChange);
    return () => {
      document.removeEventListener("visibilitychange", handleVisibilityChange);
    };
  }, [authState, refreshToken]);

  return (
    <AuthContext.Provider
      value={{
        authState,
        user,
        login,
        logout,
        authFetch,
        refreshToken,
        authMode,
        isAdmin: !user || !user.role || user.role === "admin"
      }}
    >
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth(): AuthContextType {
  const context = useContext(AuthContext);
  if (!context) {
    throw new Error("useAuth must be used within an AuthProvider");
  }
  return context;
}
