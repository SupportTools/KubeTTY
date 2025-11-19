import { useState, useCallback, useEffect, useMemo } from "react";
import TerminalView from "./components/TerminalView";
import TabBar from "./components/TabBar";
import ProjectPicker from "./components/ProjectPicker";
import { GatewayTab, ProjectInfo, ProjectsResponse, TabEvent } from "./types";
import logo from "./assets/logo.svg";

type GatewayState = "unknown" | "enabled" | "disabled";

type ClientTab = GatewayTab & { wsUrl: string };

const App = () => {
  const [toast, setToast] = useState<string | null>(null);
  const [gatewayState, setGatewayState] = useState<GatewayState>("unknown");
  const [projects, setProjects] = useState<ProjectInfo[]>([]);
  const [tabs, setTabs] = useState<ClientTab[]>([]);
  const [activeTabId, setActiveTabId] = useState<string | null>(null);
  const [pickerOpen, setPickerOpen] = useState(false);
  const [eventRetry, setEventRetry] = useState(0);

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

  useEffect(() => {
    let cancelled = false;
    const loadProjects = async () => {
      try {
        const res = await fetch("/api/projects");
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
  }, []);

  useEffect(() => {
    if (gatewayState !== "enabled") {
      return;
    }
    const protocol = window.location.protocol === "https:" ? "wss" : "ws";
    const url = `${protocol}://${window.location.host}/api/tabs/events`;
    console.log(`[TabEvents] Connecting to ${url} (retry ${eventRetry})`);

    const ws = new WebSocket(url);

    ws.onopen = () => {
      console.log("[TabEvents] Connected successfully");
    };

    ws.onmessage = (event) => {
      try {
        const payload = JSON.parse(event.data) as TabEvent;
        if (payload.type === "snapshot") {
          console.log(`[TabEvents] Received snapshot with ${payload.tabs?.length || 0} tabs`);
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
          console.log(`[TabEvents] Tab update: ${payload.tab.tabId} -> ${payload.tab.status}`);
          setTabs((prev) => {
            const exists = prev.find((t) => t.tabId === payload.tab.tabId);
            if (exists) {
              return prev.map((t) => (t.tabId === payload.tab.tabId ? { ...t, ...payload.tab } : t));
            }
            return [...prev, { ...payload.tab, wsUrl: wsForTab(payload.tab.tabId) }];
          });
        } else if (payload.type === "delete") {
          console.log(`[TabEvents] Tab deleted: ${payload.tabId}`);
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
        }
      } catch (err) {
        console.error("[TabEvents] Parse error:", err);
      }
    };
    ws.onerror = (event) => {
      console.error("[TabEvents] Connection error:", event);
      ws.close();
    };
    ws.onclose = (event) => {
      console.log(`[TabEvents] Connection closed: code=${event.code}, reason=${event.reason || 'none'}, wasClean=${event.wasClean}`);
      // Add delay before reconnecting to prevent rapid reconnection storms
      const delay = Math.min(1000 * Math.pow(2, eventRetry), 16000);
      console.log(`[TabEvents] Scheduling reconnect in ${delay}ms`);
      setTimeout(() => {
        setEventRetry((prev) => prev + 1);
      }, delay);
    };
    return () => {
      console.log("[TabEvents] Closing connection (cleanup)");
      ws.close();
    };
  }, [gatewayState, wsForTab, eventRetry]);

  const projectLabels = useMemo(() => {
    const map = new Map<string, string>();
    projects.forEach((p) => map.set(p.id, p.displayName || p.id));
    return map;
  }, [projects]);

  const handleCreateTab = useCallback(
    async (projectId: string) => {
      try {
        const res = await fetch("/api/tabs", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ projectId })
        });
        if (!res.ok) {
          throw new Error(await res.text());
        }
        const data = await res.json();
        const tab: ClientTab = {
          ...data.tab,
          wsUrl: data.wsUrl || wsForTab(data.tab.tabId)
        };
        setTabs((prev) => [...prev, tab]);
        setActiveTabId(tab.tabId);
        setPickerOpen(false);
      } catch (err) {
        console.error("create tab", err);
        showToast("Failed to open tab");
      }
    },
    [showToast, wsForTab]
  );

  const handleCloseTab = useCallback(
    async (tabId: string) => {
      if (!window.confirm('Close this tab?')) {
        return;
      }
      try {
        const res = await fetch(`/api/tabs/${tabId}`, { method: "DELETE" });
        if (!res.ok && res.status !== 404) {
          throw new Error(await res.text());
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
      } catch (err) {
        console.error("close tab", err);
        showToast("Failed to close tab");
      }
    },
    [showToast]
  );

  const showGatewayUI = gatewayState === "enabled";
  const decoratedTabs = useMemo(
    () =>
      tabs.map((tab) => ({
        tabId: tab.tabId,
        label: projectLabels.get(tab.projectId) || tab.projectId,
        status: tab.status
      })),
    [tabs, projectLabels]
  );


  return (
    <div className="app-shell">
      <header className="header">
        <div className="brand">
          <img src={logo} alt="KubeTTY" className="logo" />
          <h1>KubeTTY</h1>
        </div>
      </header>
      {toast && <div className="toast">{toast}</div>}
      {showGatewayUI ? (
        <>
          <TabBar
            tabs={decoratedTabs}
            activeTabId={activeTabId}
            onSelect={setActiveTabId}
            onClose={handleCloseTab}
            onNew={() => setPickerOpen(true)}
            projects={projects}
          />
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
                  className="terminal-pane"
                  style={{ display: tab.tabId === activeTabId ? "block" : "none" }}
                >
                  <TerminalView
                    wsUrl={tab.wsUrl}
                    isFocused={tab.tabId === activeTabId}
                    onReconnect={handleReconnect}
                    externalStatus={tab.status as 'connecting' | 'connected' | 'reconnecting' | 'closed'}
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
    </div>
  );
};

export default App;
