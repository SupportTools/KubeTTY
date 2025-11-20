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
export type AuthUser = { id: string; username: string } | null;

interface AuthContextType {
  authState: AuthState;
  user: AuthUser;
  login: (username: string, password: string) => Promise<void>;
  logout: () => Promise<void>;
  authFetch: (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>;
  refreshToken: () => Promise<boolean>;
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

export function AuthProvider({ children }: { children: ReactNode }) {
  const [authState, setAuthState] = useState<AuthState>("checking");
  const [user, setUser] = useState<AuthUser>(null);
  const refreshTimerRef = useRef<number | null>(null);
  const isRefreshingRef = useRef(false);

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

    if (delay > 0) {
      refreshTimerRef.current = window.setTimeout(async () => {
        await refreshToken();
      }, delay);
    } else if (expiryMs > now) {
      // Token still valid but within buffer - refresh immediately
      refreshToken();
    }
  }, [clearRefreshTimer]);

  // Refresh the access token
  const refreshToken = useCallback(async (): Promise<boolean> => {
    if (isRefreshingRef.current) {
      return false;
    }

    isRefreshingRef.current = true;
    try {
      const res = await fetch("/api/auth/refresh", {
        method: "POST",
        credentials: "include"
      });

      if (!res.ok) {
        setAuthState("unauthenticated");
        setUser(null);
        clearRefreshTimer();
        return false;
      }

      const data = await res.json();

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
    } catch {
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
        // Try to refresh token once
        const refreshed = await refreshToken();
        if (refreshed) {
          // Retry the original request
          return fetch(input, { credentials: "include", ...(init || {}) });
        }
        setAuthState("unauthenticated");
        setUser(null);
      }

      return response;
    },
    [refreshToken]
  );

  // Login function
  const login = useCallback(async (username: string, password: string): Promise<void> => {
    const res = await fetch("/api/auth/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      credentials: "include",
      body: JSON.stringify({ username, password })
    });

    if (!res.ok) {
      const errorMessage = await parseErrorResponse(res);
      throw new Error(errorMessage);
    }

    const data = await res.json();
    setAuthState("authenticated");
    setUser(data.user);

    // Schedule token refresh (15m access TTL)
    const tokenExpiry = Date.now() + (15 * 60 * 1000);
    scheduleRefresh(tokenExpiry);
  }, [scheduleRefresh]);

  // Logout function
  const logout = useCallback(async (): Promise<void> => {
    clearRefreshTimer();
    try {
      await fetch("/api/auth/logout", { method: "POST", credentials: "include" });
    } catch {
      // Ignore errors during logout
    }
    setAuthState("unauthenticated");
    setUser(null);
  }, [clearRefreshTimer]);

  // Initial auth check on mount
  useEffect(() => {
    let cancelled = false;

    const probeAuth = async () => {
      try {
        const res = await fetch("/api/auth/me", { credentials: "include" });

        // If auth endpoints don't exist, assume authenticated (disabled auth mode)
        if (res.status === 404 || res.status === 405) {
          if (!cancelled) {
            setAuthState("authenticated");
            setUser(null);
          }
          return;
        }

        if (!res.ok) {
          throw new Error("unauthorized");
        }

        const data = await res.json();
        if (!cancelled) {
          setAuthState("authenticated");
          setUser(data.user);

          // Schedule token refresh (assume 15m from now if we can't parse token)
          const tokenExpiry = Date.now() + (15 * 60 * 1000);
          scheduleRefresh(tokenExpiry);
        }
      } catch {
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
  }, [scheduleRefresh, clearRefreshTimer]);

  return (
    <AuthContext.Provider
      value={{
        authState,
        user,
        login,
        logout,
        authFetch,
        refreshToken
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
