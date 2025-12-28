import { useState, useEffect, useCallback, useRef } from 'react';
import TerminalView from './TerminalView';
import GUIView from './GUIView';
import SplitPane from './SplitPane';
import ViewToolbar from './ViewToolbar';
import { ViewMode, ProjectInfo } from '../types';
import './TabPane.css';

// LocalStorage key prefix for view mode persistence
const VIEW_MODE_KEY_PREFIX = 'kubetty-view-mode-';
const SPLIT_RATIO_KEY_PREFIX = 'kubetty-split-ratio-';

// Dev-mode logging helper
const isDev = import.meta.env.DEV;
const devLog = (message: string, data?: Record<string, unknown>) => {
  if (isDev) {
    if (data) {
      console.debug(`[TabPane] ${message}`, data);
    } else {
      console.debug(`[TabPane] ${message}`);
    }
  }
};

type Props = {
  /** Unique tab identifier */
  tabId: string;
  /** WebSocket URL for terminal */
  wsUrl: string;
  /** Health check URL for terminal */
  healthUrl: string;
  /** Whether this tab is currently focused */
  isFocused: boolean;
  /** Project info (for GUI settings) */
  project?: ProjectInfo | null;
  /** External status from tab events */
  externalStatus?: 'connecting' | 'connected' | 'reconnecting' | 'closed';
  /** Called when terminal reconnects */
  onReconnect?: () => void;
  /** Called when terminal output matches GUI app pattern */
  onTerminalOutput?: (data: string) => void;
  /** Called when terminal receives bell character */
  onBell?: () => void;
};

/**
 * TabPane - Wrapper component for terminal/GUI views with view mode management
 *
 * Features:
 * - ViewMode switching (terminal, gui, split-horizontal, split-vertical)
 * - Keyboard shortcuts (Ctrl+Shift+T/G/H/V)
 * - LocalStorage persistence for view mode and split ratio
 * - GUI hint notifications when GUI apps are detected
 */
const TabPane = ({
  tabId,
  wsUrl,
  healthUrl,
  isFocused,
  project,
  externalStatus,
  onReconnect,
  onBell,
}: Props) => {
  const guiEnabled = project?.guiEnabled ?? false;

  // Load initial view mode from localStorage
  const [viewMode, setViewMode] = useState<ViewMode>(() => {
    if (!guiEnabled) return 'terminal';
    try {
      const stored = localStorage.getItem(`${VIEW_MODE_KEY_PREFIX}${tabId}`);
      if (stored && ['terminal', 'gui', 'split-horizontal', 'split-vertical'].includes(stored)) {
        return stored as ViewMode;
      }
    } catch {
      // Ignore localStorage errors
    }
    return 'terminal';
  });

  // Load initial split ratio from localStorage
  const [splitRatio, setSplitRatio] = useState<number>(() => {
    try {
      const stored = localStorage.getItem(`${SPLIT_RATIO_KEY_PREFIX}${tabId}`);
      if (stored) {
        const parsed = parseFloat(stored);
        if (!isNaN(parsed) && parsed >= 10 && parsed <= 90) {
          return parsed;
        }
      }
    } catch {
      // Ignore localStorage errors
    }
    return 50;
  });

  // GUI hint state
  const [showGUIHint, setShowGUIHint] = useState(false);
  const hintTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  // VNC URL construction
  const vncUrl = useCallback(() => {
    if (!project?.guiVNCPort) return '';
    // Use the same host as the terminal WebSocket, with tab query parameter
    const protocol = window.location.protocol === 'https:' ? 'wss' : 'ws';
    return `${protocol}://${window.location.host}/vnc?tab=${tabId}`;
  }, [tabId, project?.guiVNCPort]);

  // Persist view mode to localStorage
  useEffect(() => {
    if (!guiEnabled) return;
    try {
      localStorage.setItem(`${VIEW_MODE_KEY_PREFIX}${tabId}`, viewMode);
    } catch {
      // Ignore localStorage errors
    }
  }, [tabId, viewMode, guiEnabled]);

  // Persist split ratio to localStorage
  useEffect(() => {
    try {
      localStorage.setItem(`${SPLIT_RATIO_KEY_PREFIX}${tabId}`, String(splitRatio));
    } catch {
      // Ignore localStorage errors
    }
  }, [tabId, splitRatio]);

  // Keyboard shortcuts
  useEffect(() => {
    if (!isFocused || !guiEnabled) return;

    const handleKeyDown = (e: KeyboardEvent) => {
      // Require Ctrl+Shift modifier
      if (!e.ctrlKey || !e.shiftKey) return;

      let newMode: ViewMode | null = null;

      switch (e.key.toLowerCase()) {
        case 't':
          newMode = 'terminal';
          break;
        case 'g':
          newMode = 'gui';
          break;
        case 'h':
          newMode = 'split-horizontal';
          break;
        case 'v':
          newMode = 'split-vertical';
          break;
        case 's':
          // Toggle between terminal and last split mode
          if (viewMode === 'terminal') {
            newMode = 'split-horizontal';
          } else if (viewMode.startsWith('split-')) {
            newMode = 'terminal';
          } else {
            newMode = 'split-horizontal';
          }
          break;
        default:
          return;
      }

      if (newMode && newMode !== viewMode) {
        e.preventDefault();
        devLog('Keyboard shortcut triggered', { key: e.key, newMode });
        setViewMode(newMode);
      }
    };

    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [isFocused, guiEnabled, viewMode]);

  // Handle view mode change from toolbar
  const handleModeChange = useCallback((mode: ViewMode) => {
    devLog('View mode changed', { from: viewMode, to: mode });
    setViewMode(mode);
  }, [viewMode]);

  // Handle split pane resize
  const handleSplitResize = useCallback((percent: number) => {
    setSplitRatio(percent);
  }, []);

  // Show GUI hint when GUI app is detected
  const showHint = useCallback(() => {
    if (!guiEnabled || viewMode !== 'terminal') return;

    setShowGUIHint(true);

    // Clear existing timeout
    if (hintTimeoutRef.current) {
      clearTimeout(hintTimeoutRef.current);
    }

    // Auto-dismiss after 10 seconds
    hintTimeoutRef.current = setTimeout(() => {
      setShowGUIHint(false);
    }, 10000);
  }, [guiEnabled, viewMode]);

  // Handle hint click - switch to split view
  const handleHintClick = useCallback(() => {
    setShowGUIHint(false);
    if (hintTimeoutRef.current) {
      clearTimeout(hintTimeoutRef.current);
    }
    setViewMode('split-horizontal');
  }, []);

  // Dismiss hint
  const dismissHint = useCallback(() => {
    setShowGUIHint(false);
    if (hintTimeoutRef.current) {
      clearTimeout(hintTimeoutRef.current);
    }
  }, []);

  // Cleanup timeout on unmount
  useEffect(() => {
    return () => {
      if (hintTimeoutRef.current) {
        clearTimeout(hintTimeoutRef.current);
      }
    };
  }, []);

  // Clean up localStorage when tab is closed (called from parent)
  useEffect(() => {
    return () => {
      // Note: This cleanup happens when component unmounts
      // Parent should call this explicitly when tab is closed
    };
  }, [tabId]);

  // Render terminal component
  const renderTerminal = (focused: boolean) => (
    <TerminalView
      wsUrl={wsUrl}
      healthUrl={healthUrl}
      isFocused={focused}
      onReconnect={onReconnect}
      externalStatus={externalStatus}
      onBell={onBell}
    />
  );

  // Render GUI component
  const renderGUI = (focused: boolean) => {
    const url = vncUrl();
    if (!url) {
      return (
        <div className="tab-pane__gui-unavailable">
          <p>GUI not available for this project</p>
        </div>
      );
    }
    return (
      <GUIView
        vncUrl={url}
        isFocused={focused}
      />
    );
  };

  // Determine which view to render
  const renderContent = () => {
    if (!guiEnabled) {
      // GUI not enabled - just render terminal
      return (
        <div className="tab-pane__terminal-only">
          {renderTerminal(isFocused)}
        </div>
      );
    }

    switch (viewMode) {
      case 'terminal':
        return (
          <div className="tab-pane__terminal-only">
            {renderTerminal(isFocused)}
          </div>
        );

      case 'gui':
        return (
          <div className="tab-pane__gui-only">
            {renderGUI(isFocused)}
          </div>
        );

      case 'split-horizontal':
        return (
          <SplitPane
            direction="horizontal"
            firstPane={renderTerminal(isFocused)}
            secondPane={renderGUI(isFocused)}
            initialFirstPaneSize={splitRatio}
            onResize={handleSplitResize}
          />
        );

      case 'split-vertical':
        return (
          <SplitPane
            direction="vertical"
            firstPane={renderTerminal(isFocused)}
            secondPane={renderGUI(isFocused)}
            initialFirstPaneSize={splitRatio}
            onResize={handleSplitResize}
          />
        );

      default:
        return renderTerminal(isFocused);
    }
  };

  return (
    <div className="tab-pane">
      {guiEnabled && (
        <div className="tab-pane__toolbar">
          <ViewToolbar
            currentMode={viewMode}
            onModeChange={handleModeChange}
            guiEnabled={guiEnabled}
          />
          <div className="tab-pane__shortcuts-hint">
            <span title="Keyboard shortcuts">
              ⌨ Ctrl+Shift: T=Terminal, G=GUI, H=Split H, V=Split V
            </span>
          </div>
        </div>
      )}
      <div className="tab-pane__content">
        {renderContent()}
      </div>
      {showGUIHint && (
        <div className="tab-pane__gui-hint" role="alert">
          <span>GUI application detected!</span>
          <button onClick={handleHintClick} className="tab-pane__gui-hint-action">
            Open Split View →
          </button>
          <button
            onClick={dismissHint}
            className="tab-pane__gui-hint-dismiss"
            aria-label="Dismiss"
          >
            ×
          </button>
        </div>
      )}
    </div>
  );
};

// Helper function to clean up localStorage for a closed tab
export const cleanupTabStorage = (tabId: string) => {
  try {
    localStorage.removeItem(`${VIEW_MODE_KEY_PREFIX}${tabId}`);
    localStorage.removeItem(`${SPLIT_RATIO_KEY_PREFIX}${tabId}`);
  } catch {
    // Ignore localStorage errors
  }
};

export default TabPane;
