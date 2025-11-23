import { useEffect, useRef, useState, useCallback } from 'react';

export type HealthStatus = 'healthy' | 'degraded' | 'unhealthy' | 'checking';

type HealthIndicatorProps = {
  wsUrl?: string;
  onUnhealthy?: () => void;
  checkInterval?: number;
  failureThreshold?: number;
};

/**
 * HealthIndicator component that polls the project pod's health check endpoint
 * and displays connection status. Triggers reconnect callback on failures.
 */
const HealthIndicator = ({
  wsUrl,
  onUnhealthy,
  checkInterval = 5000,
  failureThreshold = 3,
}: HealthIndicatorProps) => {
  const [status, setStatus] = useState<HealthStatus>('checking');
  const [lastCheck, setLastCheck] = useState<Date | null>(null);
  const [ptyStatus, setPtyStatus] = useState<string>('unknown');
  const consecutiveFailures = useRef(0);
  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null);

  // Derive health check URL from WebSocket URL
  const getHealthUrl = useCallback((ws?: string): string => {
    if (!ws) {
      // Default to same origin
      return `${window.location.origin}/api/healthz`;
    }
    try {
      const url = new URL(ws);
      url.protocol = url.protocol === 'wss:' ? 'https:' : 'http:';
      url.pathname = '/api/healthz';
      return url.toString();
    } catch {
      return `${window.location.origin}/api/healthz`;
    }
  }, []);

  const checkHealth = useCallback(async () => {
    const healthUrl = getHealthUrl(wsUrl);

    try {
      const response = await fetch(healthUrl, {
        method: 'GET',
        credentials: 'include',
        // Short timeout to avoid blocking
        signal: AbortSignal.timeout(4000),
      });

      if (response.ok) {
        const data = await response.json();
        consecutiveFailures.current = 0;
        setLastCheck(new Date());

        // Check PTY status from response
        const pty = data?.components?.pty || 'unknown';
        setPtyStatus(pty);

        if (pty === 'alive') {
          setStatus('healthy');
        } else {
          // PTY not started or unknown - degraded state
          setStatus('degraded');
        }
      } else {
        handleFailure();
      }
    } catch {
      handleFailure();
    }
  }, [wsUrl, getHealthUrl]);

  const handleFailure = useCallback(() => {
    consecutiveFailures.current++;
    setLastCheck(new Date());

    if (consecutiveFailures.current >= failureThreshold) {
      setStatus('unhealthy');
      setPtyStatus('unreachable');
      if (onUnhealthy) {
        onUnhealthy();
      }
    } else {
      setStatus('degraded');
    }
  }, [failureThreshold, onUnhealthy]);

  useEffect(() => {
    // Initial check
    checkHealth();

    // Set up interval
    timerRef.current = setInterval(checkHealth, checkInterval);

    return () => {
      if (timerRef.current) {
        clearInterval(timerRef.current);
      }
    };
  }, [checkHealth, checkInterval]);

  // Reset on wsUrl change
  useEffect(() => {
    consecutiveFailures.current = 0;
    setStatus('checking');
    setPtyStatus('unknown');
  }, [wsUrl]);

  const statusClass = `health-indicator health-${status}`;
  const tooltip = getTooltip(status, lastCheck, ptyStatus, consecutiveFailures.current);

  return (
    <div className={statusClass} title={tooltip}>
      <span className="health-dot" />
      <span className="health-label">{getLabel(status)}</span>
    </div>
  );
};

function getLabel(status: HealthStatus): string {
  switch (status) {
    case 'healthy':
      return 'Healthy';
    case 'degraded':
      return 'Degraded';
    case 'unhealthy':
      return 'Unhealthy';
    case 'checking':
      return 'Checking...';
  }
}

function getTooltip(
  status: HealthStatus,
  lastCheck: Date | null,
  ptyStatus: string,
  failures: number
): string {
  const lastCheckStr = lastCheck
    ? `Last check: ${lastCheck.toLocaleTimeString()}`
    : 'No check yet';

  const statusStr = `PTY: ${ptyStatus}`;
  const failureStr = failures > 0 ? `Failures: ${failures}` : '';

  return [statusStr, lastCheckStr, failureStr].filter(Boolean).join('\n');
}

export default HealthIndicator;
