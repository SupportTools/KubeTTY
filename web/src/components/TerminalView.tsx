import { useEffect, useRef, useState, useCallback } from 'react';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import '@xterm/xterm/css/xterm.css';
import HealthIndicator from './HealthIndicator';

type Props = {
  onReconnect?: () => void;
  wsUrl?: string;
  isFocused?: boolean;
  externalStatus?: 'connecting' | 'connected' | 'reconnecting' | 'closed';
};

// PTY resize limits - must match server constants
const MAX_PTY_COLS = 500;
const MAX_PTY_ROWS = 200;

const encoder = new TextEncoder();
const decoder = new TextDecoder();

const FORCE_RECONNECT_THRESHOLD = 3; // Show force button after this many failed attempts

const TerminalView = ({ onReconnect, wsUrl, isFocused = true, externalStatus }: Props) => {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const termRef = useRef<Terminal | null>(null);
  const fitAddonRef = useRef<FitAddon | null>(null);
  const socketRef = useRef<WebSocket | null>(null);
  const dataDisposable = useRef<{ dispose(): void } | null>(null);
  const [status, setStatus] = useState('Disconnected');
  const reconnectTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const closingRef = useRef(false);
  const [retrySignal, setRetrySignal] = useState(0);
  const pingTimer = useRef<ReturnType<typeof setInterval> | null>(null);
  const connectedOnceRef = useRef(false);
  const reconnectAttempts = useRef(0);
  const [showForceButton, setShowForceButton] = useState(false);
  const forceReconnectRef = useRef(false);

  // Handler for when HTTP health check fails - trigger WebSocket reconnect
  const handleHealthUnhealthy = useCallback(() => {
    // Only trigger reconnect if WebSocket is currently open
    if (socketRef.current && socketRef.current.readyState === WebSocket.OPEN) {
      // Close the WebSocket to trigger reconnection logic
      socketRef.current.close();
    }
  }, []);

  // Handler for force reconnect button - takes over session from another client
  const handleForceReconnect = useCallback(() => {
    forceReconnectRef.current = true;
    setShowForceButton(false);
    reconnectAttempts.current = 0;
    // Trigger reconnect
    setRetrySignal((prev) => prev + 1);
  }, []);

  useEffect(() => {
    const term = new Terminal({
      cursorBlink: true,
      fontSize: 14,
      theme: {
        background: '#050608',
        foreground: '#f2f4f8'
      }
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    termRef.current = term;
    fitAddonRef.current = fit;
    if (containerRef.current) {
      term.open(containerRef.current);
      fit.fit();
      term.focus();
    }
    const resize = () => {
      fit.fit();
      notifyResize();
    };
    window.addEventListener('resize', resize);
    return () => {
      window.removeEventListener('resize', resize);
      dataDisposable.current?.dispose();
      term.dispose();
      socketRef.current?.close();
      reconnectTimer.current && clearTimeout(reconnectTimer.current);
      pingTimer.current && clearInterval(pingTimer.current);
    };
  }, []);

  useEffect(() => {
    if (!termRef.current) {
      return;
    }

    let targetUrl = wsUrl || (() => {
      const protocol = window.location.protocol === 'https:' ? 'wss' : 'ws';
      return `${protocol}://${window.location.host}/ws`;
    })();

    // Append force=true parameter if force reconnect was triggered
    if (forceReconnectRef.current) {
      const separator = targetUrl.includes('?') ? '&' : '?';
      targetUrl = `${targetUrl}${separator}force=true`;
      forceReconnectRef.current = false; // Reset after use
    }

    socketRef.current?.close();
    const socket = new WebSocket(targetUrl);
    socket.binaryType = 'arraybuffer';
    socketRef.current = socket;
    setStatus('Connecting…');
    closingRef.current = false;

    socket.onopen = () => {
      const wasReconnecting = connectedOnceRef.current;
      connectedOnceRef.current = true;
      reconnectAttempts.current = 0; // Reset on successful connection
      setShowForceButton(false); // Hide force button on successful connection
      setStatus(wasReconnecting ? 'Reconnected' : 'Connected');
      if (wasReconnecting && onReconnect) {
        onReconnect();
      }
      termRef.current?.focus();
      notifyResize();
    };

    socket.onclose = () => {
      setStatus('Disconnected');
      pingTimer.current && clearInterval(pingTimer.current);
      if (!closingRef.current) {
        // Exponential backoff: 1s, 2s, 4s, 8s, max 16s
        const delay = Math.min(1000 * Math.pow(2, reconnectAttempts.current), 16000);
        reconnectAttempts.current++;
        // Show force reconnect button after threshold attempts
        if (reconnectAttempts.current >= FORCE_RECONNECT_THRESHOLD) {
          setShowForceButton(true);
        }
        reconnectTimer.current = setTimeout(() => {
          setRetrySignal((prev) => prev + 1);
        }, delay);
        setStatus('Reconnecting…');
      }
    };

    socket.onerror = () => {
      setStatus('Error');
    };

    socket.onmessage = (event) => {
      const payload = event.data instanceof ArrayBuffer ? decoder.decode(event.data) : String(event.data);
      // Filter out control messages (pong responses)
      if (payload.startsWith('{"type":')) {
        try {
          const msg = JSON.parse(payload);
          if (msg.type === 'pong') {
            return; // Don't write pong to terminal
          }
        } catch {
          // Not valid JSON, write to terminal
        }
      }
      termRef.current?.write(payload);
    };

    dataDisposable.current?.dispose();
    dataDisposable.current = termRef.current.onData((chunk) => {
      if (socket.readyState === WebSocket.OPEN) {
        socket.send(encoder.encode(chunk));
      }
    });

    pingTimer.current && clearInterval(pingTimer.current);
    pingTimer.current = setInterval(() => {
      if (socketRef.current && socketRef.current.readyState === WebSocket.OPEN) {
        socketRef.current.send(JSON.stringify({ type: 'ping' }));
      }
    }, 10000); // Reduced from 15s to 10s for faster dead connection detection

    return () => {
      closingRef.current = true;
      pingTimer.current && clearInterval(pingTimer.current);
      socket.close();
    };
  }, [retrySignal, onReconnect, wsUrl]);

  useEffect(() => {
    if (isFocused) {
      termRef.current?.focus();
    }
  }, [isFocused]);

  const notifyResize = () => {
    const socket = socketRef.current;
    const term = termRef.current;
    if (!socket || !term || socket.readyState !== WebSocket.OPEN) {
      return;
    }
    // Clamp to server-enforced limits
    const cols = Math.min(Math.max(1, term.cols), MAX_PTY_COLS);
    const rows = Math.min(Math.max(1, term.rows), MAX_PTY_ROWS);
    const payload = JSON.stringify({ type: 'resize', cols, rows });
    socket.send(payload);
  };

  const isConnecting = status === 'Connecting…' || status === 'Reconnecting…';

  const extOverlay = externalStatus && externalStatus !== 'connected';
  const externalLabel = externalStatus ? externalStatusDisplay(externalStatus) : null;

  return (
    <div className="terminal-container" ref={containerRef}>
      {(isConnecting || extOverlay) && (
        <div className="connection-overlay">
          <div className="spinner"></div>
          <div className="connection-message">{extOverlay && externalLabel ? externalLabel : status}</div>
          {showForceButton && (
            <button
              className="force-reconnect-button"
              onClick={handleForceReconnect}
              title="Force takeover of session from another client"
            >
              Force Reconnect
            </button>
          )}
        </div>
      )}
      <div
        style={{
          position: 'absolute',
          top: 8,
          right: 12,
          display: 'flex',
          alignItems: 'center',
          gap: '0.75rem',
          zIndex: 1
        }}
      >
        <HealthIndicator
          wsUrl={wsUrl}
          onUnhealthy={handleHealthUnhealthy}
          checkInterval={5000}
          failureThreshold={3}
        />
        <div
          style={{
            background: '#0f1115',
            padding: '2px 8px',
            borderRadius: 4,
            fontSize: 12,
            color: '#b3b6c2'
          }}
        >
          {externalLabel || status}
        </div>
      </div>
    </div>
  );
};

const externalStatusDisplay = (value: Props['externalStatus']) => {
  switch (value) {
    case 'connecting':
      return 'Connecting…';
    case 'reconnecting':
      return 'Reconnecting…';
    case 'closed':
      return 'Disconnected';
    default:
      return 'Connected';
  }
};

export default TerminalView;
