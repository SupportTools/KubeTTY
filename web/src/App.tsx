import { useState, useCallback, useEffect, useMemo } from "react";
import TabPane, { cleanupTabStorage } from "./components/TabPane";
import TerminalView from "./components/TerminalView";
import TabBar from "./components/TabBar";
import StatusBar from "./components/StatusBar";
import ProjectPicker from "./components/ProjectPicker";
import Login from "./components/Login";
import ProfileModal from "./components/ProfileModal";
import PasswordChangeModal from "./components/PasswordChangeModal";
import LogoutConfirmDialog from "./components/LogoutConfirmDialog";
import AdminProjectList from "./components/AdminProjectList";
import AdminDashboard from "./components/AdminDashboard";
import AdminSettings from "./components/AdminSettings";
import { useAuth } from "./contexts/AuthContext";
import { GatewayTab, ProjectInfo, ProjectsResponse, TabEvent } from "./types";
import { parseErrorResponse } from "./utils/errorParser";
import logo from "./assets/logo.svg";

type GatewayState = "unknown" | "enabled" | "disabled";

type ClientTab = GatewayTab & {
  wsUrl: string;
  hasBellAlert?: boolean;
};

// Bell sound rate limiting
let lastBellTime = 0;
const BELL_COOLDOWN_MS = 1000; // 1 second between sounds

const playBellSound = () => {
  const now = Date.now();
  if (now - lastBellTime < BELL_COOLDOWN_MS) {
    return; // Rate limited
  }
  lastBellTime = now;

  try {
    const audioContext = new (window.AudioContext || (window as unknown as { webkitAudioContext: typeof AudioContext }).webkitAudioContext)();
    const oscillator = audioContext.createOscillator();
    const gainNode = audioContext.createGain();

    oscillator.connect(gainNode);
    gainNode.connect(audioContext.destination);

    oscillator.frequency.value = 800; // Hz - pleasant tone
    oscillator.type = 'sine';
    gainNode.gain.value = 0.1; // Subtle volume

    oscillator.start();
    oscillator.stop(audioContext.currentTime + 0.1); // 100ms beep
  } catch {
    // Ignore audio errors (e.g., autoplay restrictions)
  }
};

const App = () => {
  const { authState, user: authUser, authFetch } = useAuth();

  const [toast, setToast] = useState<string | null>(null);
  const [gatewayState, setGatewayState] = useState<GatewayState>("unknown");
  const [projects, setProjects] = useState<ProjectInfo[]>([]);
  const [tabs, setTabs] = useState<ClientTab[]>([]);
  const [activeTabId, setActiveTabId] = useState<string | null>(null);
  const [pickerOpen, setPickerOpen] = useState(false);
  const [profileOpen, setProfileOpen] = useState(false);
  const [passwordChangeOpen, setPasswordChangeOpen] = useState(false);
  const [logoutDialogOpen, setLogoutDialogOpen] = useState(false);
  const [adminOpen, setAdminOpen] = useState(false);
  const [dashboardOpen, setDashboardOpen] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [eventRetry, setEventRetry] = useState(0);
  const authenticated = authState === "authenticated";

  const showToast = useCallback((message: string) => {
    setToast(message);
    setTimeout(() => setToast(null), 4000);
  }, []);

  const handleReconnect = useCallback(() => {
    showToast("Reconnected to shell");
  }, [showToast]);

  const wsForTab = useCallback((tabId: string) => {
    const protocol = window.location.protocol === "https:" ? "wss" : "ws";
    return `${protocol}://${window.location.host}/ws?tab=${encodeURIComponent(tabId)}`;
  }, []);

  const healthForTab = useCallback((tabId: string) => {
    return `${window.location.origin}/api/tabs/${encodeURIComponent(tabId)}/health`;
  }, []);

  useEffect(() => {
    if (authState !== "unauthenticated") {
      return;
    }
    setProjects([]);
    setTabs([]);
    setActiveTabId(null);
    setPickerOpen(false);
    setGatewayState("unknown");
    setEventRetry(0);
  }, [authState]);

  useEffect(() => {
    if (!authenticated) {
      return;
    }
    let cancelled = false;
    const loadProjects = async () => {
      try {
        const res = await authFetch("/api/projects");
        if (!res.ok) {
          throw new Error("gateway disabled");
        }
        const data = (await res.json()) as ProjectsResponse;
        if (!cancelled) {
          setProjects(data.projects || []);
          setGatewayState("enabled");
        }
      } catch (err) {
        if (!cancelled) {
          setGatewayState("disabled");
        }
      }
    };
    loadProjects();
    return () => {
      cancelled = true;
    };
  }, [authenticated, authFetch]);

  // Poll for project updates every 10 seconds to detect new projects
  useEffect(() => {
    if (gatewayState !== "enabled" || !authenticated) {
      return;
    }
    const pollProjects = async () => {
      try {
        const res = await authFetch("/api/projects");
        if (res.ok) {
          const data = (await res.json()) as ProjectsResponse;
          setProjects(data.projects || []);
        }
      } catch {
        // Ignore polling errors - initial load already set gateway state
      }
    };
    const interval = setInterval(pollProjects, 10000);
    return () => clearInterval(interval);
  }, [gatewayState, authenticated, authFetch]);

  useEffect(() => {
    if (gatewayState !== "enabled" || !authenticated) {
      return;
    }
    const protocol = window.location.protocol === "https:" ? "wss" : "ws";
    const url = `${protocol}://${window.location.host}/api/tabs/events`;

    const ws = new WebSocket(url);

    ws.onmessage = (event) => {
      try {
        const payload = JSON.parse(event.data) as TabEvent;
        if (payload.type === "snapshot") {
          const next = (payload.tabs || []).map((tab) => ({
            ...tab,
            wsUrl: wsForTab(tab.tabId)
          }));
          setTabs(next);
          setActiveTabId((current) => {
            if (current && next.some((tab) => tab.tabId === current)) {
              return current;
            }
            return next.length ? next[0].tabId : null;
          });
        } else if (payload.type === "update") {
          setTabs((prev) => {
            const exists = prev.find((t) => t.tabId === payload.tab.tabId);
            if (exists) {
              return prev.map((t) => (t.tabId === payload.tab.tabId ? { ...t, ...payload.tab } : t));
            }
            return [...prev, { ...payload.tab, wsUrl: wsForTab(payload.tab.tabId) }];
          });
        } else if (payload.type === "delete") {
          setTabs((prev) => {
            const next = prev.filter((t) => t.tabId !== payload.tabId);
            setActiveTabId((current) => {
              if (current !== payload.tabId) {
                return current;
              }
              return next.length ? next[next.length - 1].tabId : null;
            });
            return next;
          });
        } else if (payload.type === "metrics") {
          setTabs((prev) =>
            prev.map((t) =>
              t.tabId === payload.tabId ? { ...t, metrics: payload.metrics } : t
            )
          );
        }
      } catch {
        // Ignore parse errors
      }
    };
    ws.onerror = () => {
      ws.close();
    };
    ws.onclose = () => {
      // Add delay before reconnecting to prevent rapid reconnection storms
      const delay = Math.min(1000 * Math.pow(2, eventRetry), 16000);
      setTimeout(() => {
        setEventRetry((prev) => prev + 1);
      }, delay);
    };
    return () => {
      ws.close();
    };
  }, [gatewayState, wsForTab, eventRetry, authenticated]);

  const projectLabels = useMemo(() => {
    const map = new Map<string, string>();
    projects.forEach((p) => map.set(p.id, p.displayName || p.id));
    return map;
  }, [projects]);

  const handleCreateTab = useCallback(
    async (projectId: string) => {
      try {
        const res = await authFetch("/api/tabs", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ projectId })
        });
        if (!res.ok) {
          const errorMessage = await parseErrorResponse(res);
          throw new Error(errorMessage);
        }
        const data = await res.json();
        const tab: ClientTab = {
          ...data.tab,
          wsUrl: data.wsUrl || wsForTab(data.tab.tabId)
        };
        // Use functional update with deduplication to prevent race condition with SSE
        setTabs((prev) => {
          if (prev.some((t) => t.tabId === tab.tabId)) {
            return prev;
          }
          return [...prev, tab];
        });
        setActiveTabId(tab.tabId);
        setPickerOpen(false);
      } catch {
        showToast("Failed to open tab");
      }
    },
    [showToast, wsForTab, authFetch]
  );

  const handleCloseTab = useCallback(
    async (tabId: string) => {
      if (!window.confirm('Close this tab?')) {
        return;
      }
      try {
        const res = await authFetch(`/api/tabs/${tabId}`, { method: "DELETE" });
        if (!res.ok && res.status !== 404) {
          const errorMessage = await parseErrorResponse(res);
          throw new Error(errorMessage);
        }
        // Clean up localStorage for view mode preferences
        cleanupTabStorage(tabId);
        setTabs((prev) => {
          const next = prev.filter((tab) => tab.tabId !== tabId);
          setActiveTabId((current) => {
            if (current !== tabId) {
              return current;
            }
            return next.length ? next[next.length - 1].tabId : null;
          });
          return next;
        });
      } catch {
        showToast("Failed to close tab");
      }
    },
    [showToast, authFetch]
  );

  // Handle bell alerts from terminal
  const handleBellAlert = useCallback((tabId: string) => {
    // Only alert if tab is not focused
    if (tabId !== activeTabId) {
      setTabs(prev => prev.map(t =>
        t.tabId === tabId
          ? { ...t, hasBellAlert: true }
          : t
      ));
      playBellSound();
    }
  }, [activeTabId]);

  // Handle tab selection - clears bell alert
  const handleTabSelect = useCallback((tabId: string) => {
    setActiveTabId(tabId);
    // Clear bell alert when tab is selected
    setTabs(prev => prev.map(t =>
      t.tabId === tabId ? { ...t, hasBellAlert: false } : t
    ));
  }, []);

  const showGatewayUI = authenticated && gatewayState === "enabled";
  const decoratedTabs = useMemo(
    () =>
      tabs.map((tab) => ({
        tabId: tab.tabId,
        label: projectLabels.get(tab.projectId) || tab.projectId,
        status: tab.status,
        metrics: tab.metrics,
        hasBellAlert: tab.hasBellAlert
      })),
    [tabs, projectLabels]
  );

  const activeTab = useMemo(
    () => (activeTabId ? tabs.find((t) => t.tabId === activeTabId) : null),
    [tabs, activeTabId]
  );

  const activeTabLabel = useMemo(
    () =>
      activeTab
        ? projectLabels.get(activeTab.projectId) || activeTab.projectId
        : "",
    [activeTab, projectLabels]
  );

  // Helper to get project info for a tab (needed for GUI features)
  const getProjectForTab = useCallback((projectId: string) => {
    return projects.find(p => p.id === projectId) ?? null;
  }, [projects]);

  const renderHeader = () => (
    <header className="header">
      <div className="brand">
        <img src={logo} alt="KubeTTY" className="logo" />
        <h1>KubeTTY</h1>
      </div>
      {authenticated && (
        <div className="session-info">
          {showGatewayUI && (
            <>
              <button className="admin-link" onClick={() => setDashboardOpen(true)}>
                Dashboard
              </button>
              <button className="admin-link" onClick={() => setAdminOpen(true)}>
                Projects
              </button>
              <button
                className="admin-link settings-btn"
                onClick={() => setSettingsOpen(true)}
                title="Settings"
              >
                ⚙
              </button>
            </>
          )}
          {authUser && (
            <button
              className="username-button"
              onClick={() => setProfileOpen(true)}
            >
              {authUser.username}
            </button>
          )}
          <button className="secondary" onClick={() => setLogoutDialogOpen(true)}>
            Logout
          </button>
        </div>
      )}
    </header>
  );

  if (authState === "checking") {
    return (
      <div className="app-shell">
        {renderHeader()}
        <section className="main full-width">
          <p>Checking authentication…</p>
        </section>
      </div>
    );
  }

  if (!authenticated) {
    return <Login />;
  }

  return (
    <div className="app-shell">
      {renderHeader()}
      {toast && <div className="toast">{toast}</div>}
      {showGatewayUI ? (
        <>
          <TabBar
            tabs={decoratedTabs}
            activeTabId={activeTabId}
            onSelect={handleTabSelect}
            onClose={handleCloseTab}
            onNew={() => setPickerOpen(true)}
            projects={projects}
          />
          {activeTab && (
            <StatusBar tabLabel={activeTabLabel} metrics={activeTab.metrics} />
          )}
          <section className="main full-width tabbed">
            {tabs.length === 0 ? (
              <div className="tab-empty">
                <p>No tabs yet.</p>
                <button onClick={() => setPickerOpen(true)} disabled={projects.length === 0}>
                  Open a tab
                </button>
              </div>
            ) : (
              tabs.map((tab) => (
                <div
                  key={tab.tabId}
                  className={`terminal-pane${tab.tabId !== activeTabId ? ' hidden' : ''}`}
                >
                  <TabPane
                    tabId={tab.tabId}
                    wsUrl={tab.wsUrl}
                    healthUrl={healthForTab(tab.tabId)}
                    isFocused={tab.tabId === activeTabId}
                    project={getProjectForTab(tab.projectId)}
                    externalStatus={tab.status as 'connecting' | 'connected' | 'reconnecting' | 'closed'}
                    onReconnect={handleReconnect}
                    onBell={() => handleBellAlert(tab.tabId)}
                  />
                </div>
              ))
            )}
          </section>
          {pickerOpen && (
            <ProjectPicker
              projects={projects}
              onClose={() => setPickerOpen(false)}
              onSelect={handleCreateTab}
            />
          )}
        </>
      ) : (
        <section className="main full-width">
          <TerminalView onReconnect={handleReconnect} />
        </section>
      )}
      {profileOpen && (
        <ProfileModal
          onClose={() => setProfileOpen(false)}
          onPasswordChange={() => {
            setProfileOpen(false);
            setPasswordChangeOpen(true);
          }}
        />
      )}
      {passwordChangeOpen && (
        <PasswordChangeModal
          onClose={() => setPasswordChangeOpen(false)}
          onSuccess={() => {
            setPasswordChangeOpen(false);
            showToast("Password changed. Please log in again.");
          }}
        />
      )}
      {logoutDialogOpen && (
        <LogoutConfirmDialog onClose={() => setLogoutDialogOpen(false)} />
      )}
      {adminOpen && (
        <AdminProjectList onClose={() => setAdminOpen(false)} />
      )}
      {dashboardOpen && (
        <AdminDashboard onClose={() => setDashboardOpen(false)} />
      )}
      {settingsOpen && (
        <AdminSettings onClose={() => setSettingsOpen(false)} />
      )}
    </div>
  );
};

export default App;
