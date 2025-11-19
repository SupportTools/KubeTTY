import { useEffect, useRef, useState } from 'react';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import '@xterm/xterm/css/xterm.css';

type Props = {
  onReconnect?: () => void;
};

const encoder = new TextEncoder();
const decoder = new TextDecoder();

const TerminalView = ({ onReconnect }: Props) => {
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

    const protocol = window.location.protocol === 'https:' ? 'wss' : 'ws';
    const url = `${protocol}://${window.location.host}/ws`;

    socketRef.current?.close();
    const socket = new WebSocket(url);
    socket.binaryType = 'arraybuffer';
    socketRef.current = socket;
    setStatus('Connecting…');
    closingRef.current = false;

    socket.onopen = () => {
      const wasReconnecting = connectedOnceRef.current;
      connectedOnceRef.current = true;
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
        reconnectTimer.current = setTimeout(() => {
          setRetrySignal((prev) => prev + 1);
        }, 3000);
        setStatus('Reconnecting…');
      }
    };

    socket.onerror = () => setStatus('Error');

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
      }
    }, 15000);

    return () => {
      closingRef.current = true;
      pingTimer.current && clearInterval(pingTimer.current);
      socket.close();
    };
  }, [retrySignal, onReconnect]);

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

  return (
    <div className="terminal-container" ref={containerRef}>
      {isConnecting && (
        <div className="connection-overlay">
          <div className="spinner"></div>
          <div className="connection-message">{status}</div>
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
        {status}
      </div>
    </div>
  );
};

export default TerminalView;
