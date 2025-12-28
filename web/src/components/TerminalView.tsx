import { useEffect, useRef, useState, useCallback } from 'react';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import '@xterm/xterm/css/xterm.css';
import HealthIndicator from './HealthIndicator';

// Dev-mode logging helper
const isDev = import.meta.env.DEV;
const devLog = (message: string, data?: Record<string, unknown>) => {
  if (isDev) {
    if (data) {
      console.debug(`[TerminalView] ${message}`, data);
    } else {
      console.debug(`[TerminalView] ${message}`);
    }
  }
};

type Props = {
  onReconnect?: () => void;
  wsUrl?: string;
  healthUrl?: string;
  isFocused?: boolean;
  externalStatus?: 'connecting' | 'connected' | 'reconnecting' | 'closed';
  /** Called when terminal receives bell character (ASCII 0x07) */
  onBell?: () => void;
};

// PTY resize limits - must match server constants
const MAX_PTY_COLS = 500;
const MAX_PTY_ROWS = 200;

const encoder = new TextEncoder();
const decoder = new TextDecoder();

const FORCE_RECONNECT_THRESHOLD = 2; // Show force button after this many failed attempts
const HIGH_WATERMARK = 5; // Max pending writes before pausing server output
const QUICK_DISCONNECT_MS = 500; // If disconnected this fast, likely a 409 rejection
const MAX_RECONNECT_ATTEMPTS = 10; // Circuit breaker: stop after this many attempts
const JITTER_FACTOR = 0.25; // ±25% random jitter on reconnection delay
const BASE_RECONNECT_DELAY = 1000; // Base delay in ms (1 second)
const MAX_RECONNECT_DELAY = 16000; // Maximum delay in ms (16 seconds)

// Heartbeat configuration
const PING_INTERVAL_MS = 10000; // Send ping every 10 seconds
const PONG_TIMEOUT_MS = 25000; // Consider connection dead if no pong for 25 seconds

// Calculate reconnection delay with exponential backoff and jitter
const calculateReconnectDelay = (attempt: number): number => {
  // Exponential backoff: 1s, 2s, 4s, 8s, max 16s
  const baseDelay = Math.min(BASE_RECONNECT_DELAY * Math.pow(2, attempt), MAX_RECONNECT_DELAY);
  // Add random jitter: ±25% to prevent thundering herd
  const jitter = baseDelay * JITTER_FACTOR * (Math.random() * 2 - 1);
  return Math.round(baseDelay + jitter);
};

const TerminalView = ({ onReconnect, wsUrl, healthUrl, isFocused = true, externalStatus, onBell }: Props) => {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const termRef = useRef<Terminal | null>(null);
  const fitAddonRef = useRef<FitAddon | null>(null);
  const socketRef = useRef<WebSocket | null>(null);
  const dataDisposable = useRef<{ dispose(): void } | null>(null);
  const bellDisposable = useRef<{ dispose(): void } | null>(null);
  const onBellRef = useRef(onBell);
  onBellRef.current = onBell; // Keep ref current without triggering re-renders
  const [status, setStatus] = useState('Disconnected');
  const reconnectTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const closingRef = useRef(false);
  const [retrySignal, setRetrySignal] = useState(0);
  const pingTimer = useRef<ReturnType<typeof setInterval> | null>(null);
  const connectedOnceRef = useRef(false);
  const reconnectAttempts = useRef(0);
  const [showForceButton, setShowForceButton] = useState(false);
  const forceReconnectRef = useRef(false);
  const [pendingWrites, setPendingWrites] = useState(0);
  const pausedRef = useRef(false);
  const connectTimeRef = useRef<number>(0);
  const [takenOverMessage, setTakenOverMessage] = useState<string | null>(null);
  const [circuitBreakerTripped, setCircuitBreakerTripped] = useState(false);
  const [reconnectCountdown, setReconnectCountdown] = useState<number | null>(null);
  const countdownTimer = useRef<ReturnType<typeof setInterval> | null>(null);
  const lastPongAt = useRef<number>(0); // Timestamp of last pong received

  // Scroll tracking refs for viewport sync fix
  const isUserScrolled = useRef(false); // Track if user manually scrolled up
  const scrollToBottomPending = useRef(false); // Debounce scroll calls with rAF

  // Handler for when HTTP health check fails - trigger WebSocket reconnect
  const handleHealthUnhealthy = useCallback(() => {
    // Only trigger reconnect if WebSocket is currently open
    if (socketRef.current && socketRef.current.readyState === WebSocket.OPEN) {
      devLog('Health check failed, closing WebSocket to trigger reconnect');
      // Close the WebSocket to trigger reconnection logic
      socketRef.current.close();
    }
  }, []);

  // Handler for force reconnect button - takes over session from another client
  const handleForceReconnect = useCallback(() => {
    devLog('Force reconnect triggered');
    forceReconnectRef.current = true;
    setShowForceButton(false);
    reconnectAttempts.current = 0;
    setCircuitBreakerTripped(false); // Reset circuit breaker
    setReconnectCountdown(null); // Clear countdown
    countdownTimer.current && clearInterval(countdownTimer.current);
    // Trigger reconnect
    setRetrySignal((prev) => prev + 1);
  }, []);

  // Scroll to bottom helper with rAF debouncing to prevent excessive scroll calls
  // Only scrolls if user hasn't manually scrolled up (respects user position)
  const scrollToBottomIfNeeded = useCallback(() => {
    if (isUserScrolled.current || scrollToBottomPending.current) return;

    scrollToBottomPending.current = true;
    requestAnimationFrame(() => {
      termRef.current?.scrollToBottom();
      scrollToBottomPending.current = false;
    });
  }, []);

  useEffect(() => {
    const term = new Terminal({
      cursorBlink: true,
      fontSize: 14,
      scrollback: 100000,  // 100K lines for large Claude outputs (default was 1000)
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

      // Listen for scroll events to detect when user manually scrolls up
      // This prevents auto-scroll from overriding user's scroll position
      const viewport = term.element?.querySelector('.xterm-viewport');
      if (viewport) {
        const handleScroll = () => {
          // Check if user is at the bottom (with 10px tolerance for rounding)
          const atBottom = viewport.scrollTop >= viewport.scrollHeight - viewport.clientHeight - 10;
          isUserScrolled.current = !atBottom;
        };
        viewport.addEventListener('scroll', handleScroll);
        // Store cleanup function for later
        (term as unknown as { _scrollCleanup?: () => void })._scrollCleanup = () => {
          viewport.removeEventListener('scroll', handleScroll);
        };
      }
    }

    // Hook terminal bell event
    bellDisposable.current = term.onBell(() => {
      onBellRef.current?.();
    });

    const resize = () => {
      fit.fit();
      notifyResize();
    };
    window.addEventListener('resize', resize);
    return () => {
      window.removeEventListener('resize', resize);
      dataDisposable.current?.dispose();
      bellDisposable.current?.dispose();
      // Clean up scroll listener before disposing terminal
      (term as unknown as { _scrollCleanup?: () => void })._scrollCleanup?.();
      term.dispose();
      socketRef.current?.close();
      reconnectTimer.current && clearTimeout(reconnectTimer.current);
      pingTimer.current && clearInterval(pingTimer.current);
      countdownTimer.current && clearInterval(countdownTimer.current);
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
    setTakenOverMessage(null);
    closingRef.current = false;
    connectTimeRef.current = Date.now();

    socket.onopen = () => {
      const wasReconnecting = connectedOnceRef.current;
      connectedOnceRef.current = true;
      reconnectAttempts.current = 0; // Reset on successful connection
      setShowForceButton(false); // Hide force button on successful connection
      setCircuitBreakerTripped(false); // Reset circuit breaker
      setReconnectCountdown(null); // Clear countdown
      countdownTimer.current && clearInterval(countdownTimer.current);
      lastPongAt.current = Date.now(); // Initialize heartbeat timestamp
      setStatus(wasReconnecting ? 'Reconnected' : 'Connected');
      devLog('WebSocket connected', {
        wasReconnecting,
        url: targetUrl,
      });
      if (wasReconnecting && onReconnect) {
        onReconnect();
      }
      termRef.current?.focus();
      notifyResize();
    };

    socket.onclose = (event) => {
      pingTimer.current && clearInterval(pingTimer.current);
      const connectionDuration = Date.now() - connectTimeRef.current;
      const wasQuickDisconnect = connectionDuration < QUICK_DISCONNECT_MS;

      devLog('WebSocket closed', {
        code: event.code,
        reason: event.reason,
        wasClean: event.wasClean,
        intentional: closingRef.current,
        connectionDurationMs: connectionDuration,
        wasQuickDisconnect,
      });

      // Handle session takeover (custom close code 4000)
      if (event.code === 4000) {
        setTakenOverMessage(event.reason || 'Session taken over by another client');
        setStatus('Disconnected');
        // Show force button immediately for takeover case
        setShowForceButton(true);
        return; // Don't auto-reconnect when taken over
      }

      setStatus('Disconnected');

      if (!closingRef.current) {
        // Increment attempt counter first
        reconnectAttempts.current++;

        // Check circuit breaker
        if (reconnectAttempts.current >= MAX_RECONNECT_ATTEMPTS) {
          devLog('Circuit breaker tripped', {
            attempts: reconnectAttempts.current,
            maxAttempts: MAX_RECONNECT_ATTEMPTS,
          });
          setCircuitBreakerTripped(true);
          setShowForceButton(true);
          setStatus('Connection Failed');
          return;
        }

        // Calculate delay with exponential backoff and jitter (use 0-based index for delay)
        const delay = calculateReconnectDelay(reconnectAttempts.current - 1);

        devLog('Scheduling reconnect', {
          attempt: reconnectAttempts.current,
          maxAttempts: MAX_RECONNECT_ATTEMPTS,
          delayMs: delay,
        });

        // Show force reconnect button after threshold attempts or if quick disconnect (likely 409)
        if (reconnectAttempts.current >= FORCE_RECONNECT_THRESHOLD || wasQuickDisconnect) {
          setShowForceButton(true);
        }

        // Start countdown timer
        const countdownEnd = Date.now() + delay;
        setReconnectCountdown(Math.ceil(delay / 1000));

        // Clear any existing countdown timer
        countdownTimer.current && clearInterval(countdownTimer.current);

        // Update countdown every second
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
          setRetrySignal((prev) => prev + 1);
        }, delay);

        setStatus('Reconnecting…');
      }
    };

    socket.onerror = (event) => {
      setStatus('Error');
      devLog('WebSocket error', { event: event.type });
    };

    socket.onmessage = (event) => {
      const payload = event.data instanceof ArrayBuffer ? decoder.decode(event.data) : String(event.data);
      // Filter out control messages (pong responses, buffer info)
      if (payload.startsWith('{"type":')) {
        try {
          const msg = JSON.parse(payload);
          if (msg.type === 'pong') {
            // Update last pong timestamp for heartbeat monitoring
            lastPongAt.current = Date.now();
            devLog('Pong received');
            return; // Don't write pong to terminal
          }
          if (msg.type === 'buffer_info') {
            // Buffer metadata from server (for future lazy-load support)
            devLog('Buffer info received', {
              totalWritten: msg.totalWritten,
              availableBytes: msg.availableBytes,
              oldestOffset: msg.oldestOffset,
            });
            return; // Don't write buffer_info to terminal
          }
        } catch {
          // Not valid JSON, write to terminal
        }
      }

      // Flow control: increment pending writes counter
      setPendingWrites((prev) => {
        const newCount = prev + 1;

        // Pause server output if we exceed HIGH_WATERMARK
        if (!pausedRef.current && newCount >= HIGH_WATERMARK) {
          pausedRef.current = true;
          devLog('Flow control: pausing server output', {
            pendingWrites: newCount,
            highWatermark: HIGH_WATERMARK,
          });
          if (socket.readyState === WebSocket.OPEN) {
            socket.send(JSON.stringify({ type: 'pause' }));
          }
        }

        return newCount;
      });

      // Write to terminal with callback to track completion
      termRef.current?.write(payload, () => {
        // Write completed - decrement pending counter
        setPendingWrites((prev) => {
          const newCount = prev - 1;

          // Resume if we were paused and now below threshold
          if (pausedRef.current && newCount < HIGH_WATERMARK) {
            pausedRef.current = false;
            devLog('Flow control: resuming server output', {
              pendingWrites: newCount,
              highWatermark: HIGH_WATERMARK,
            });
            if (socketRef.current?.readyState === WebSocket.OPEN) {
              socketRef.current.send(JSON.stringify({ type: 'resume' }));
            }
          }

          return newCount;
        });

        // Sync scroll position after write completes to fix viewport desync
        // Uses rAF debouncing to prevent excessive scroll calls during rapid output
        scrollToBottomIfNeeded();
      });
    };

    dataDisposable.current?.dispose();
    dataDisposable.current = termRef.current.onData((chunk) => {
      // Reset scroll tracking when user types - they're likely at bottom interacting
      isUserScrolled.current = false;

      if (socket.readyState === WebSocket.OPEN) {
        socket.send(encoder.encode(chunk));
      }
    });

    pingTimer.current && clearInterval(pingTimer.current);
    pingTimer.current = setInterval(() => {
      if (socketRef.current && socketRef.current.readyState === WebSocket.OPEN) {
        // Check if we've received a pong recently
        const sincePong = Date.now() - lastPongAt.current;
        if (sincePong > PONG_TIMEOUT_MS) {
          devLog('Pong timeout - connection appears dead', {
            sincePongMs: sincePong,
            timeoutMs: PONG_TIMEOUT_MS,
          });
          // Close connection to trigger reconnect
          socketRef.current.close();
          return;
        }

        // Send ping
        socketRef.current.send(JSON.stringify({ type: 'ping' }));
        devLog('Ping sent');
      }
    }, PING_INTERVAL_MS);

    return () => {
      closingRef.current = true;
      pingTimer.current && clearInterval(pingTimer.current);
      socket.close();
    };
  }, [retrySignal, onReconnect, wsUrl]);

  useEffect(() => {
    if (isFocused && termRef.current && fitAddonRef.current) {
      // Small delay to ensure DOM has updated (display: none → block)
      // before fitting the terminal to its new container size
      const timeoutId = setTimeout(() => {
        fitAddonRef.current?.fit();
        notifyResize();
        termRef.current?.focus();
      }, 50);
      return () => clearTimeout(timeoutId);
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
  const isConnectionFailed = status === 'Connection Failed';

  const extOverlay = externalStatus && externalStatus !== 'connected';
  const externalLabel = externalStatus ? externalStatusDisplay(externalStatus) : null;

  const showOverlay = isConnecting || extOverlay || takenOverMessage || isConnectionFailed;

  // Build status message with attempt info and countdown
  const getStatusMessage = () => {
    if (takenOverMessage) return takenOverMessage;
    if (extOverlay && externalLabel) return externalLabel;
    if (circuitBreakerTripped) {
      return `Connection failed after ${MAX_RECONNECT_ATTEMPTS} attempts`;
    }
    if (status === 'Reconnecting…' && reconnectAttempts.current > 0) {
      const countdownText = reconnectCountdown !== null ? ` in ${reconnectCountdown}s` : '';
      return `Reconnecting${countdownText} (attempt ${reconnectAttempts.current}/${MAX_RECONNECT_ATTEMPTS})`;
    }
    return status;
  };

  return (
    <div className="terminal-container" ref={containerRef}>
      {showOverlay && (
        <div className="connection-overlay">
          {!takenOverMessage && !circuitBreakerTripped && <div className="spinner"></div>}
          <div className="connection-message">
            {getStatusMessage()}
          </div>
          {takenOverMessage && (
            <div className="takeover-hint">Click "Force Reconnect" to reclaim your session</div>
          )}
          {circuitBreakerTripped && !takenOverMessage && (
            <div className="circuit-breaker-hint">Click "Retry Connection" to try again</div>
          )}
          {showForceButton && (
            <button
              className="force-reconnect-button"
              onClick={handleForceReconnect}
              title={circuitBreakerTripped ? "Retry connecting to the server" : "Force takeover of session from another client"}
            >
              {circuitBreakerTripped ? 'Retry Connection' : 'Force Reconnect'}
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
          healthUrl={healthUrl}
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
