import { useState, useCallback, useEffect, useMemo } from "react";
import TerminalView from "./components/TerminalView";
import TabBar from "./components/TabBar";
import StatusBar from "./components/StatusBar";
import ProjectPanel from "./components/ProjectPanel";
import Login from "./components/Login";
import ProfileModal from "./components/ProfileModal";
import PasswordChangeModal from "./components/PasswordChangeModal";
import LogoutConfirmDialog from "./components/LogoutConfirmDialog";
import AdminProjectList from "./components/AdminProjectList";
import AdminDashboard from "./components/AdminDashboard";
import Footer from "./components/Footer";
import { useAuth } from "./contexts/AuthContext";
import { GatewayTab, ProjectInfo, ProjectsResponse, TabEvent } from "./types";
import { parseErrorResponse } from "./utils/errorParser";
import logo from "./assets/logo.svg";

type GatewayState = "unknown" | "enabled" | "disabled";

type ClientTab = GatewayTab & { wsUrl: string };

const STORAGE_KEY_SHOW_PANEL = "kubetty:showProjectPanel";

const App = () => {
  const { authState, user: authUser, authFetch } = useAuth();

  const [toast, setToast] = useState<string | null>(null);
  const [gatewayState, setGatewayState] = useState<GatewayState>("unknown");
  const [projects, setProjects] = useState<ProjectInfo[]>([]);
  const [tabs, setTabs] = useState<ClientTab[]>([]);
  const [activeTabId, setActiveTabId] = useState<string | null>(null);
  const [profileOpen, setProfileOpen] = useState(false);
  const [passwordChangeOpen, setPasswordChangeOpen] = useState(false);
  const [logoutDialogOpen, setLogoutDialogOpen] = useState(false);
  const [adminOpen, setAdminOpen] = useState(false);
  const [dashboardOpen, setDashboardOpen] = useState(false);
  const [eventRetry, setEventRetry] = useState(0);
  const [showProjectPanel, setShowProjectPanel] = useState(() => {
    try {
      return localStorage.getItem(STORAGE_KEY_SHOW_PANEL) === "true";
    } catch {
      return false;
    }
  });
  const authenticated = authState === "authenticated";

  const toggleProjectPanel = useCallback(() => {
    setShowProjectPanel((prev) => {
      const next = !prev;
      try {
        localStorage.setItem(STORAGE_KEY_SHOW_PANEL, String(next));
      } catch {
        // Ignore localStorage errors
      }
      return next;
    });
  }, []);

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

  const projectMap = useMemo(() => {
    const map = new Map<string, ProjectInfo>();
    projects.forEach((p) => map.set(p.id, p));
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

  const handleReorderTabs = useCallback(
    async (tabIds: string[]) => {
      // Optimistically reorder tabs in UI
      setTabs((prev) => {
        const tabMap = new Map(prev.map((t) => [t.tabId, t]));
        return tabIds
          .map((id) => tabMap.get(id))
          .filter((t): t is ClientTab => t !== undefined);
      });

      // Persist to server
      try {
        const res = await authFetch("/api/tabs/reorder", {
          method: "PUT",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ tabIds })
        });
        if (!res.ok) {
          const errorMessage = await parseErrorResponse(res);
          throw new Error(errorMessage);
        }
      } catch {
        showToast("Failed to save tab order");
      }
    },
    [showToast, authFetch]
  );

  const handleNewSessionShortcut = useCallback(() => {
    if (projects.length === 0) {
      showToast("No projects are available yet.");
      return;
    }
    if (!showProjectPanel) {
      // Show sidebar automatically when user clicks "+ New session"
      setShowProjectPanel(true);
      try {
        localStorage.setItem(STORAGE_KEY_SHOW_PANEL, "true");
      } catch {
        // Ignore localStorage errors
      }
      showToast("Select a project from the sidebar to start a session.");
    } else {
      showToast("Select a project from the sidebar to start a session.");
    }
  }, [projects.length, showToast, showProjectPanel]);

  const showGatewayUI = authenticated && gatewayState === "enabled";
  const decoratedTabs = useMemo(
    () =>
      tabs.map((tab) => {
        const project = projectMap.get(tab.projectId);
        return {
          tabId: tab.tabId,
          projectLabel: project?.displayName || project?.id || tab.projectId,
          status: tab.status,
          metrics: tab.metrics,
          projectNamespace: project?.namespace,
          lifecycleStatus: project?.lifecycleStatus,
          projectHealth: project?.status,
          paused: project?.paused,
          createdAt: tab.createdAt,
          updatedAt: tab.updatedAt,
          lastError: tab.lastError,
        };
      }),
    [tabs, projectMap]
  );

  const activeTab = useMemo(
    () => (activeTabId ? tabs.find((t) => t.tabId === activeTabId) : null),
    [tabs, activeTabId]
  );

  const activeProject = useMemo(() => {
    if (!activeTab) {
      return null;
    }
    return projectMap.get(activeTab.projectId) || null;
  }, [activeTab, projectMap]);

  const activeTabLabel = useMemo(
    () =>
      activeTab
        ? projectLabels.get(activeTab.projectId) || activeTab.projectId
        : "",
    [activeTab, projectLabels]
  );

  if (authState === "checking") {
    return (
      <div className="app-shell loading">
        <div className="global-rail">
          <div className="rail-brand">
            <img src={logo} alt="KubeTTY" />
            <span>KubeTTY</span>
          </div>
        </div>
        <div className="workspace-shell">
          <div className="workspace">
            <section className="workspace-body">
              <p>Checking authentication…</p>
            </section>
            <Footer />
          </div>
        </div>
      </div>
    );
  }

  if (!authenticated) {
    return <Login />;
  }

  return (
    <div className={`app-shell layout-modern${showProjectPanel ? " show-panel" : ""}`}>
      <aside className="global-rail">
        <div className="rail-brand">
          <img src={logo} alt="KubeTTY" className="logo" />
          <div>
            <p>KubeTTY</p>
            <span>Internal terminal</span>
          </div>
        </div>
        <div className="rail-actions">
          <button
            onClick={toggleProjectPanel}
            className={showProjectPanel ? "active" : ""}
            title={showProjectPanel ? "Hide project sidebar" : "Show project sidebar"}
          >
            {showProjectPanel ? "Hide Sidebar" : "Show Sidebar"}
          </button>
          <button onClick={() => setDashboardOpen(true)} disabled={!authenticated}>
            Dashboard
          </button>
          <button onClick={() => setAdminOpen(true)} disabled={!authenticated}>
            Projects
          </button>
        </div>
        <div className="rail-user">
          {authUser && (
            <button className="rail-username" onClick={() => setProfileOpen(true)}>
              {authUser.username}
            </button>
          )}
          <button className="rail-logout" onClick={() => setLogoutDialogOpen(true)}>
            Logout
          </button>
        </div>
      </aside>
      <div className="workspace-shell">
        <ProjectPanel
          projects={projects}
          gatewayState={gatewayState}
          onSelect={handleCreateTab}
          selectedProjectId={activeTab?.projectId}
          onOpenAdmin={() => setAdminOpen(true)}
          onOpenDashboard={() => setDashboardOpen(true)}
        />
        <div className="workspace">
          {toast && <div className="toast floating">{toast}</div>}
          {showGatewayUI ? (
            <>
              <TabBar
                tabs={decoratedTabs}
                activeTabId={activeTabId}
                onSelect={setActiveTabId}
                onClose={handleCloseTab}
                onNew={handleNewSessionShortcut}
                onReorder={handleReorderTabs}
                projects={projects}
              />
              {activeTab ? (
                <StatusBar
                  tabLabel={activeTabLabel}
                  tabStatus={activeTab.status}
                  metrics={activeTab.metrics}
                  namespace={activeProject?.namespace}
                  createdAt={activeTab.createdAt}
                  updatedAt={activeTab.updatedAt}
                  lastError={activeTab.lastError}
                />
              ) : (
                <div className="session-hud-empty">
                  {showProjectPanel
                    ? "Select a project from the sidebar to start a session."
                    : "Click \"+ New session\" or enable the sidebar to start."}
                </div>
              )}
              <section className="workspace-body tabbed">
                {tabs.length === 0 ? (
                  <div className="tab-empty">
                    <h2>No sessions yet</h2>
                    <p>
                      {showProjectPanel
                        ? "Pick a project from the sidebar to launch an interactive shell."
                        : "Click \"+ New session\" above or use \"Show Sidebar\" to pick a project."}
                    </p>
                  </div>
                ) : (
                  tabs.map((tab) => (
                    <div
                      key={tab.tabId}
                      className={`terminal-pane${tab.tabId !== activeTabId ? ' hidden' : ''}`}
                    >
                      <TerminalView
                        wsUrl={tab.wsUrl}
                        healthUrl={healthForTab(tab.tabId)}
                        isFocused={tab.tabId === activeTabId}
                        onReconnect={handleReconnect}
                        externalStatus={tab.status as 'connecting' | 'connected' | 'reconnecting' | 'closed'}
                      />
                    </div>
                  ))
                )}
              </section>
            </>
          ) : (
            <section className="workspace-body solo">
              <div className="gateway-disabled-card">
                <h2>Gateway disabled</h2>
                <p>
                  The multi-tab gateway is currently unavailable. You still have access to the
                  default PTY below.
                </p>
              </div>
              <div className="terminal-pane single">
                <TerminalView onReconnect={handleReconnect} />
              </div>
            </section>
          )}
          <Footer />
        </div>
      </div>
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
    </div>
  );
};

export default App;
