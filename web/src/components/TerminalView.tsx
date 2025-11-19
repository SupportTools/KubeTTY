import { useEffect, useRef, useState } from 'react';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import '@xterm/xterm/css/xterm.css';

type Props = {
  onReconnect?: () => void;
  wsUrl?: string;
  isFocused?: boolean;
  externalStatus?: 'connecting' | 'connected' | 'reconnecting' | 'closed';
};

const encoder = new TextEncoder();
const decoder = new TextDecoder();

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

    const targetUrl = wsUrl || (() => {
      const protocol = window.location.protocol === 'https:' ? 'wss' : 'ws';
      return `${protocol}://${window.location.host}/ws`;
    })();

    console.log(`[WS] Connecting to ${targetUrl} (attempt ${reconnectAttempts.current + 1})`);

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
      console.log(`[WS] Connected successfully (reconnect: ${wasReconnecting})`);
      setStatus(wasReconnecting ? 'Reconnected' : 'Connected');
      if (wasReconnecting && onReconnect) {
        onReconnect();
      }
      termRef.current?.focus();
      notifyResize();
    };

    socket.onclose = (event) => {
      console.log(`[WS] Connection closed: code=${event.code}, reason=${event.reason || 'none'}, wasClean=${event.wasClean}`);
      setStatus('Disconnected');
      pingTimer.current && clearInterval(pingTimer.current);
      if (!closingRef.current) {
        // Exponential backoff: 1s, 2s, 4s, 8s, max 16s
        const delay = Math.min(1000 * Math.pow(2, reconnectAttempts.current), 16000);
        reconnectAttempts.current++;
        console.log(`[WS] Scheduling reconnect in ${delay}ms (attempt ${reconnectAttempts.current})`);
        reconnectTimer.current = setTimeout(() => {
          setRetrySignal((prev) => prev + 1);
        }, delay);
        setStatus('Reconnecting…');
      }
    };

    socket.onerror = (event) => {
      console.error('[WS] Connection error:', event);
      setStatus('Error');
    };

    socket.onmessage = (event) => {
      const payload = event.data instanceof ArrayBuffer ? decoder.decode(event.data) : String(event.data);
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
      } else {
        console.log(`[WS] Ping skipped - socket state: ${socketRef.current?.readyState}`);
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
    const cols = term.cols;
    const rows = term.rows;
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
        </div>
      )}
      <div
        style={{
          position: 'absolute',
          top: 8,
          right: 12,
          background: '#0f1115',
          padding: '2px 8px',
          borderRadius: 4,
          fontSize: 12,
          color: '#b3b6c2',
          zIndex: 1
        }}
      >
        {externalLabel || status}
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
