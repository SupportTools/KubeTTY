import { useEffect, useRef, useState, useCallback } from 'react';
import RFB from '@novnc/novnc/lib/rfb';
import './GUIView.css';

// Dev-mode logging helper
const isDev = import.meta.env.DEV;
const devLog = (message: string, data?: Record<string, unknown>) => {
  if (isDev) {
    if (data) {
      console.debug(`[GUIView] ${message}`, data);
    } else {
      console.debug(`[GUIView] ${message}`);
    }
  }
};

type ConnectionStatus = 'disconnected' | 'connecting' | 'connected' | 'error';

type Props = {
  /** WebSocket URL for VNC connection (ws:// or wss://) */
  vncUrl: string;
  /** Called when connection status changes */
  onStatusChange?: (status: ConnectionStatus) => void;
  /** Called on connection error */
  onError?: (message: string) => void;
  /** Whether this view is focused/visible */
  isFocused?: boolean;
  /** View-only mode (no input sent to server) */
  viewOnly?: boolean;
  /** Scale viewport to fit container */
  scaleViewport?: boolean;
  /** Clip viewport if larger than container */
  clipViewport?: boolean;
  /** Request server to resize to match viewport */
  resizeSession?: boolean;
  /** Compression level 0-9 */
  compressionLevel?: number;
  /** Quality level 0-9 */
  qualityLevel?: number;
};

const MAX_RECONNECT_ATTEMPTS = 5;
const BASE_RECONNECT_DELAY = 1000;
const MAX_RECONNECT_DELAY = 16000;

const calculateReconnectDelay = (attempt: number): number => {
  const baseDelay = Math.min(BASE_RECONNECT_DELAY * Math.pow(2, attempt), MAX_RECONNECT_DELAY);
  const jitter = baseDelay * 0.25 * (Math.random() * 2 - 1);
  return Math.round(baseDelay + jitter);
};

/**
 * GUIView - VNC client component using noVNC
 *
 * Provides a VNC desktop view with automatic reconnection,
 * configurable quality settings, and status indication.
 */
const GUIView = ({
  vncUrl,
  onStatusChange,
  onError,
  isFocused = true,
  viewOnly = false,
  scaleViewport = true,
  clipViewport = false,
  resizeSession = false, // Disabled: Xvfb has fixed resolution, scaling handles display fit
  compressionLevel = 2,
  qualityLevel = 6,
}: Props) => {
  const containerRef = useRef<HTMLDivElement>(null);
  const rfbRef = useRef<RFB | null>(null);
  const [status, setStatus] = useState<ConnectionStatus>('disconnected');
  const [statusMessage, setStatusMessage] = useState('');
  const reconnectTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const reconnectAttempts = useRef(0);
  const closingRef = useRef(false);
  const [reconnectCountdown, setReconnectCountdown] = useState<number | null>(null);
  const countdownTimer = useRef<ReturnType<typeof setInterval> | null>(null);
  const connectedOnceRef = useRef(false);

  const updateStatus = useCallback(
    (newStatus: ConnectionStatus, message?: string) => {
      setStatus(newStatus);
      if (message) {
        setStatusMessage(message);
      }
      onStatusChange?.(newStatus);
    },
    [onStatusChange]
  );

  const cleanup = useCallback(() => {
    closingRef.current = true;
    if (rfbRef.current) {
      devLog('Disconnecting RFB');
      try {
        rfbRef.current.disconnect();
      } catch (e) {
        devLog('Error disconnecting RFB', { error: e });
      }
      rfbRef.current = null;
    }
    reconnectTimer.current && clearTimeout(reconnectTimer.current);
    countdownTimer.current && clearInterval(countdownTimer.current);
  }, []);

  const scheduleReconnect = useCallback(() => {
    if (closingRef.current) return;
    if (reconnectAttempts.current >= MAX_RECONNECT_ATTEMPTS) {
      devLog('Max reconnect attempts reached');
      updateStatus('error', `Connection failed after ${MAX_RECONNECT_ATTEMPTS} attempts`);
      return;
    }

    const delay = calculateReconnectDelay(reconnectAttempts.current);
    reconnectAttempts.current++;

    devLog('Scheduling reconnect', {
      attempt: reconnectAttempts.current,
      delayMs: delay,
    });

    // Start countdown
    const countdownEnd = Date.now() + delay;
    setReconnectCountdown(Math.ceil(delay / 1000));

    countdownTimer.current && clearInterval(countdownTimer.current);
    countdownTimer.current = setInterval(() => {
      const remaining = Math.max(0, Math.ceil((countdownEnd - Date.now()) / 1000));
      setReconnectCountdown(remaining);
      if (remaining <= 0) {
        countdownTimer.current && clearInterval(countdownTimer.current);
      }
    }, 1000);

    reconnectTimer.current = setTimeout(() => {
      countdownTimer.current && clearInterval(countdownTimer.current);
      setReconnectCountdown(null);
      connect();
    }, delay);

    updateStatus('connecting', `Reconnecting (attempt ${reconnectAttempts.current}/${MAX_RECONNECT_ATTEMPTS})`);
  }, [updateStatus]);

  const connect = useCallback(() => {
    if (!containerRef.current || closingRef.current) return;

    // Clear any existing connection
    if (rfbRef.current) {
      try {
        rfbRef.current.disconnect();
      } catch {
        // Ignore disconnect errors
      }
      rfbRef.current = null;
    }

    devLog('Connecting to VNC', { url: vncUrl });
    updateStatus('connecting', 'Connecting to desktop...');

    try {
      const rfb = new RFB(containerRef.current, vncUrl, {
        shared: true,
      });

      // Configure RFB
      rfb.viewOnly = viewOnly;
      rfb.scaleViewport = scaleViewport;
      rfb.clipViewport = clipViewport;
      rfb.resizeSession = resizeSession;
      rfb.compressionLevel = compressionLevel;
      rfb.qualityLevel = qualityLevel;
      rfb.background = '#050608';

      // Event handlers
      rfb.addEventListener('connect', () => {
        const wasReconnecting = connectedOnceRef.current;
        connectedOnceRef.current = true;
        reconnectAttempts.current = 0;
        setReconnectCountdown(null);
        countdownTimer.current && clearInterval(countdownTimer.current);

        devLog('VNC connected', { wasReconnecting });
        updateStatus('connected', wasReconnecting ? 'Reconnected' : 'Connected');
      });

      rfb.addEventListener('disconnect', (e) => {
        const { clean } = e.detail;
        devLog('VNC disconnected', { clean });

        if (closingRef.current) {
          updateStatus('disconnected', 'Disconnected');
          return;
        }

        if (!clean) {
          scheduleReconnect();
        } else {
          updateStatus('disconnected', 'Disconnected');
        }
      });

      rfb.addEventListener('credentialsrequired', (e) => {
        devLog('VNC credentials required', { types: e.detail.types });
        // For now, we don't support authenticated VNC
        updateStatus('error', 'VNC authentication not supported');
        onError?.('VNC server requires authentication');
      });

      rfb.addEventListener('securityfailure', (e) => {
        devLog('VNC security failure', { status: e.detail.status, reason: e.detail.reason });
        updateStatus('error', e.detail.reason || 'Security failure');
        onError?.(e.detail.reason || 'VNC security failure');
      });

      rfb.addEventListener('desktopname', (e) => {
        devLog('Desktop name', { name: e.detail.name });
      });

      rfbRef.current = rfb;
    } catch (error) {
      devLog('Error creating RFB', { error });
      updateStatus('error', 'Failed to initialize VNC client');
      onError?.('Failed to initialize VNC client');
    }
  }, [
    vncUrl,
    viewOnly,
    scaleViewport,
    clipViewport,
    resizeSession,
    compressionLevel,
    qualityLevel,
    updateStatus,
    onError,
    scheduleReconnect,
  ]);

  // Connect on mount - delay slightly to ensure container has dimensions
  useEffect(() => {
    closingRef.current = false;
    // Small delay to ensure the container has been laid out and has dimensions
    // noVNC's scaleViewport relies on getBoundingClientRect() returning non-zero values
    const connectTimeout = setTimeout(() => {
      connect();
    }, 50);
    return () => {
      clearTimeout(connectTimeout);
      cleanup();
    };
  }, [vncUrl]); // Reconnect if URL changes

  // Force scale update when container resizes (backup for noVNC's internal observer)
  useEffect(() => {
    if (!containerRef.current || !rfbRef.current) return;

    const resizeObserver = new ResizeObserver(() => {
      if (rfbRef.current && scaleViewport) {
        // Trigger noVNC to recalculate scaling
        rfbRef.current.scaleViewport = scaleViewport;
      }
    });

    resizeObserver.observe(containerRef.current);
    return () => resizeObserver.disconnect();
  }, [scaleViewport]);

  // Focus management
  useEffect(() => {
    if (isFocused && rfbRef.current) {
      rfbRef.current.focus();
    } else if (!isFocused && rfbRef.current) {
      rfbRef.current.blur();
    }
  }, [isFocused]);

  // Update RFB settings when props change
  useEffect(() => {
    if (rfbRef.current) {
      rfbRef.current.viewOnly = viewOnly;
      rfbRef.current.scaleViewport = scaleViewport;
      rfbRef.current.clipViewport = clipViewport;
      rfbRef.current.resizeSession = resizeSession;
      rfbRef.current.compressionLevel = compressionLevel;
      rfbRef.current.qualityLevel = qualityLevel;
    }
  }, [viewOnly, scaleViewport, clipViewport, resizeSession, compressionLevel, qualityLevel]);

  const isConnecting = status === 'connecting';
  const isError = status === 'error';
  const showOverlay = isConnecting || isError;

  const handleRetry = () => {
    reconnectAttempts.current = 0;
    setReconnectCountdown(null);
    countdownTimer.current && clearInterval(countdownTimer.current);
    closingRef.current = false;
    connect();
  };

  return (
    <div className="gui-view">
      <div className="gui-view__canvas-container" ref={containerRef} />
      {showOverlay && (
        <div className="gui-view__overlay">
          {isConnecting && <div className="gui-view__spinner" />}
          <div className="gui-view__message">
            {statusMessage}
            {reconnectCountdown !== null && ` in ${reconnectCountdown}s`}
          </div>
          {isError && (
            <button className="gui-view__retry-button" onClick={handleRetry}>
              Retry Connection
            </button>
          )}
        </div>
      )}
      <div className="gui-view__status-bar">
        <span className={`gui-view__status-indicator gui-view__status-indicator--${status}`} />
        <span className="gui-view__status-text">
          {status === 'connected' ? 'Desktop' : status}
        </span>
      </div>
    </div>
  );
};

export default GUIView;
