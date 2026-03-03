package relay

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
)

// CircuitBreaker tracks consecutive failures and opens to prevent reconnection storms.
// It implements exponential backoff: each failed half-open attempt doubles the wait time.
type CircuitBreaker struct {
	consecutiveFailures int32
	lastFailure         atomic.Value  // time.Time
	threshold           int32         // failures before opening
	baseResetAfter      time.Duration // base reset time (doubles on each half-open failure)
	maxResetAfter       time.Duration // maximum reset time cap
	halfOpenAttempts    int32         // number of failed half-open attempts (for backoff calculation)
	lastHalfOpenTime    atomic.Value  // time.Time - when we last entered half-open state
}

// NewCircuitBreaker creates a circuit breaker with the given threshold.
// The reset time starts at baseResetAfter and doubles after each failed half-open attempt,
// up to a maximum of 10 minutes.
func NewCircuitBreaker(threshold int, baseResetAfter time.Duration) *CircuitBreaker {
	cb := &CircuitBreaker{
		threshold:      int32(threshold),
		baseResetAfter: baseResetAfter,
		maxResetAfter:  10 * time.Minute, // Cap at 10 minutes
	}
	cb.lastFailure.Store(time.Time{})
	cb.lastHalfOpenTime.Store(time.Time{})
	return cb
}

// NewCircuitBreakerWithMax creates a circuit breaker with custom max reset time.
func NewCircuitBreakerWithMax(threshold int, baseResetAfter, maxResetAfter time.Duration) *CircuitBreaker {
	cb := &CircuitBreaker{
		threshold:      int32(threshold),
		baseResetAfter: baseResetAfter,
		maxResetAfter:  maxResetAfter,
	}
	cb.lastFailure.Store(time.Time{})
	cb.lastHalfOpenTime.Store(time.Time{})
	return cb
}

// RecordFailure increments the failure count.
// If the circuit was in half-open state (failures >= threshold), this increments
// the half-open attempt counter which increases the backoff time.
func (cb *CircuitBreaker) RecordFailure() {
	oldCount := atomic.LoadInt32(&cb.consecutiveFailures)
	newCount := atomic.AddInt32(&cb.consecutiveFailures, 1)
	now := time.Now()

	// Check if this failure is from a half-open attempt
	// (i.e., we were already at or above threshold before this failure)
	wasHalfOpenAttempt := oldCount >= cb.threshold
	halfOpenAttempts := atomic.LoadInt32(&cb.halfOpenAttempts)

	if wasHalfOpenAttempt {
		// This was a half-open attempt that failed - increase backoff
		halfOpenAttempts = atomic.AddInt32(&cb.halfOpenAttempts, 1)
		log.WithFields(log.Fields{
			"old_failures":       oldCount,
			"new_failures":       newCount,
			"threshold":          cb.threshold,
			"half_open_attempts": halfOpenAttempts,
			"current_reset_time": cb.currentResetAfter().String(),
			"max_reset_time":     cb.maxResetAfter.String(),
		}).Warn("gateway/relay/circuit_breaker: half-open attempt failed, increasing backoff")
	}

	cb.lastFailure.Store(now)

	log.WithFields(log.Fields{
		"old_failures":          oldCount,
		"new_failures":          newCount,
		"threshold":             cb.threshold,
		"base_reset_after":      cb.baseResetAfter.String(),
		"current_reset_after":   cb.currentResetAfter().String(),
		"half_open_attempts":    halfOpenAttempts,
		"is_now_open":           newCount >= cb.threshold,
		"was_half_open_attempt": wasHalfOpenAttempt,
		"last_failure_set":      now.Format(time.RFC3339Nano),
	}).Debug("gateway/relay/circuit_breaker: recorded failure")
}

// RecordSuccess resets the failure count and the backoff state.
// This should be called when a connection is successfully established.
func (cb *CircuitBreaker) RecordSuccess() {
	oldCount := atomic.LoadInt32(&cb.consecutiveFailures)
	oldHalfOpenAttempts := atomic.LoadInt32(&cb.halfOpenAttempts)

	atomic.StoreInt32(&cb.consecutiveFailures, 0)
	atomic.StoreInt32(&cb.halfOpenAttempts, 0)

	log.WithFields(log.Fields{
		"old_failures":           oldCount,
		"new_failures":           0,
		"old_half_open_attempts": oldHalfOpenAttempts,
		"new_half_open_attempts": 0,
		"threshold":              cb.threshold,
		"base_reset_after":       cb.baseResetAfter.String(),
		"backoff_was_reset":      oldHalfOpenAttempts > 0,
	}).Debug("gateway/relay/circuit_breaker: recorded success, reset failure count and backoff")
}

// Reset forcibly resets the circuit breaker to closed state.
// This allows immediate reconnection attempts, useful when we've detected
// a stale connection (via keepalive) and want to reconnect immediately.
func (cb *CircuitBreaker) Reset() {
	oldCount := atomic.LoadInt32(&cb.consecutiveFailures)
	oldHalfOpenAttempts := atomic.LoadInt32(&cb.halfOpenAttempts)

	atomic.StoreInt32(&cb.consecutiveFailures, 0)
	atomic.StoreInt32(&cb.halfOpenAttempts, 0)
	cb.lastFailure.Store(time.Time{})

	log.WithFields(log.Fields{
		"old_failures":           oldCount,
		"old_half_open_attempts": oldHalfOpenAttempts,
		"reason":                 "forced_reset",
	}).Info("gateway/relay/circuit_breaker: circuit breaker reset (forced)")
}

// ErrorType classifies connection errors for intelligent circuit breaker behavior.
type ErrorType int

const (
	ErrUnknown ErrorType = iota
	ErrConnectionRefused
	ErrConnectionReset
	ErrUnexpectedEOF
	ErrTimeout
	ErrNoRouteToHost
)

func (e ErrorType) String() string {
	switch e {
	case ErrConnectionRefused:
		return "connection_refused"
	case ErrConnectionReset:
		return "connection_reset"
	case ErrUnexpectedEOF:
		return "unexpected_eof"
	case ErrTimeout:
		return "timeout"
	case ErrNoRouteToHost:
		return "no_route_to_host"
	default:
		return "unknown"
	}
}

// ClassifyError determines the type of connection error from its message.
// This helps distinguish between transient errors and errors that indicate
// the downstream pod has restarted (new IP).
func ClassifyError(err error) ErrorType {
	if err == nil {
		return ErrUnknown
	}
	errStr := strings.ToLower(err.Error())
	switch {
	case strings.Contains(errStr, "connection refused"):
		return ErrConnectionRefused
	case strings.Contains(errStr, "connection reset"):
		return ErrConnectionReset
	case strings.Contains(errStr, "unexpected eof"):
		return ErrUnexpectedEOF
	case strings.Contains(errStr, "i/o timeout") || strings.Contains(errStr, "timeout"):
		return ErrTimeout
	case strings.Contains(errStr, "no route to host"):
		return ErrNoRouteToHost
	default:
		return ErrUnknown
	}
}

// RecordFailureWithType handles failures intelligently based on error type.
// For errors that indicate a pod restart (connection refused, reset, EOF),
// the circuit breaker is reset to allow immediate reconnection to the new IP.
// For other errors, standard failure tracking applies.
func (cb *CircuitBreaker) RecordFailureWithType(errType ErrorType, projectID string) {
	switch errType {
	case ErrConnectionRefused, ErrConnectionReset, ErrUnexpectedEOF, ErrNoRouteToHost:
		// These errors likely indicate pod restart with new IP - reset for immediate reconnect
		log.WithFields(log.Fields{
			"error_type": errType.String(),
			"project_id": projectID,
			"action":     "fast_reset",
		}).Info("gateway/relay/circuit_breaker: connection error indicates pod restart, resetting for immediate reconnect")
		cb.Reset()
	default:
		// Standard failure tracking for other errors
		cb.RecordFailure()
	}
}

// currentResetAfter calculates the current reset time based on half-open attempts.
// Uses exponential backoff: baseResetAfter * 2^halfOpenAttempts, capped at maxResetAfter.
func (cb *CircuitBreaker) currentResetAfter() time.Duration {
	attempts := atomic.LoadInt32(&cb.halfOpenAttempts)
	if attempts == 0 {
		return cb.baseResetAfter
	}

	// Calculate 2^attempts, but cap the exponent to avoid overflow
	if attempts > 10 {
		attempts = 10 // 2^10 = 1024x multiplier max
	}

	multiplier := int64(1) << uint(attempts) // 2^attempts
	resetTime := time.Duration(int64(cb.baseResetAfter) * multiplier)

	// Cap at maxResetAfter
	if resetTime > cb.maxResetAfter {
		return cb.maxResetAfter
	}
	return resetTime
}

// ResetAfter returns the current reset time (for logging/debugging).
func (cb *CircuitBreaker) ResetAfter() time.Duration {
	return cb.currentResetAfter()
}

// HalfOpenAttempts returns the number of failed half-open attempts.
func (cb *CircuitBreaker) HalfOpenAttempts() int {
	return int(atomic.LoadInt32(&cb.halfOpenAttempts))
}

// IsOpen returns true if too many consecutive failures have occurred.
// The reset time uses exponential backoff based on the number of failed half-open attempts.
func (cb *CircuitBreaker) IsOpen() bool {
	failures := atomic.LoadInt32(&cb.consecutiveFailures)
	lastFail := cb.lastFailure.Load().(time.Time)
	timeSinceLastFail := time.Since(lastFail)
	halfOpenAttempts := atomic.LoadInt32(&cb.halfOpenAttempts)
	currentResetTime := cb.currentResetAfter()

	// Below threshold - circuit is closed
	if failures < cb.threshold {
		log.WithFields(log.Fields{
			"failures":             failures,
			"threshold":            cb.threshold,
			"state":                "closed",
			"reason":               "below_threshold",
			"last_failure":         lastFail.Format(time.RFC3339Nano),
			"time_since_last_fail": timeSinceLastFail.String(),
		}).Trace("gateway/relay/circuit_breaker: IsOpen check - circuit closed (below threshold)")
		return false
	}

	// Check if enough time has passed to enter half-open state
	// Uses exponential backoff: each failed half-open attempt doubles the wait time
	if timeSinceLastFail > currentResetTime {
		// Half-open: allow one attempt
		log.WithFields(log.Fields{
			"failures":             failures,
			"threshold":            cb.threshold,
			"state":                "half-open",
			"reason":               "reset_timeout_elapsed",
			"last_failure":         lastFail.Format(time.RFC3339Nano),
			"time_since_last_fail": timeSinceLastFail.String(),
			"base_reset_after":     cb.baseResetAfter.String(),
			"current_reset_after":  currentResetTime.String(),
			"half_open_attempts":   halfOpenAttempts,
			"max_reset_after":      cb.maxResetAfter.String(),
		}).Debug("gateway/relay/circuit_breaker: IsOpen check - entering half-open state, allowing one attempt")
		return false
	}

	// Circuit is open - still waiting for backoff period
	timeUntilHalfOpen := currentResetTime - timeSinceLastFail
	log.WithFields(log.Fields{
		"failures":             failures,
		"threshold":            cb.threshold,
		"state":                "open",
		"reason":               "threshold_exceeded",
		"last_failure":         lastFail.Format(time.RFC3339Nano),
		"time_since_last_fail": timeSinceLastFail.String(),
		"base_reset_after":     cb.baseResetAfter.String(),
		"current_reset_after":  currentResetTime.String(),
		"half_open_attempts":   halfOpenAttempts,
		"time_until_half_open": timeUntilHalfOpen.String(),
		"max_reset_after":      cb.maxResetAfter.String(),
	}).Debug("gateway/relay/circuit_breaker: IsOpen check - circuit open")
	return true
}

// Failures returns the current consecutive failure count.
func (cb *CircuitBreaker) Failures() int {
	return int(atomic.LoadInt32(&cb.consecutiveFailures))
}

// Config describes the downstream target to connect to.
type Config struct {
	ProjectID     string
	Endpoint      *url.URL
	Headers       http.Header
	Dialer        *websocket.Dialer
	DownstreamURI string

	// Keepalive settings for proactive stale connection detection
	PingInterval time.Duration // How often to send ping (default: 30s)
	PongTimeout  time.Duration // Max time to wait for pong response (default: 10s)
}

// Relay manages bidirectional streaming between an upstream client and downstream project /ws.
type Relay struct {
	cfg            Config
	downstream     *websocket.Conn
	mu             sync.RWMutex
	status         Status
	lastError      error
	observers      []chan StatusEvent
	activityCh     chan struct{}   // Signals activity (data transfer) for idle timeout tracking
	circuitBreaker *CircuitBreaker // Prevents reconnection storms

	// Keepalive goroutine management
	keepaliveCancel context.CancelFunc // Cancels the keepalive goroutine

	forceNextConnect atomic.Bool // Append force=true to the next downstream dial.
	nextConnectMu    sync.Mutex
	nextShellID      string
}

// Status represents connection state.
type Status string

const (
	StatusIdle         Status = "idle"
	StatusConnecting   Status = "connecting"
	StatusConnected    Status = "connected"
	StatusReconnecting Status = "reconnecting"
	StatusClosed       Status = "closed"
)

// StatusEvent surfaces state transitions to observers.
type StatusEvent struct {
	Status Status
	Err    error
	When   time.Time
}

// Default keepalive settings
const (
	DefaultPingInterval = 30 * time.Second
	DefaultPongTimeout  = 10 * time.Second
)

// New creates a relay for a downstream endpoint.
func New(cfg Config) *Relay {
	if cfg.Dialer == nil {
		cfg.Dialer = websocket.DefaultDialer
	}
	// Set keepalive defaults if not specified
	if cfg.PingInterval == 0 {
		cfg.PingInterval = DefaultPingInterval
	}
	if cfg.PongTimeout == 0 {
		cfg.PongTimeout = DefaultPongTimeout
	}
	return &Relay{
		cfg:            cfg,
		status:         StatusIdle,
		activityCh:     make(chan struct{}, 1),               // Buffer size 1 for non-blocking sends
		circuitBreaker: NewCircuitBreaker(5, 30*time.Second), // Open after 5 failures, reset after 30s
	}
}

// Status returns the current relay state.
func (r *Relay) Status() Status {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.status
}

// RequestForceNextConnect marks the next downstream connect to include force=true.
func (r *Relay) RequestForceNextConnect() {
	r.forceNextConnect.Store(true)
}

// RequestShellNextConnect marks the next downstream connect to include shell=<id>.
func (r *Relay) RequestShellNextConnect(shellID string) {
	r.nextConnectMu.Lock()
	r.nextShellID = strings.TrimSpace(shellID)
	r.nextConnectMu.Unlock()
}

func (r *Relay) consumeNextConnectParams() (bool, string) {
	force := r.forceNextConnect.Swap(false)
	r.nextConnectMu.Lock()
	shell := r.nextShellID
	r.nextShellID = ""
	r.nextConnectMu.Unlock()
	return force, shell
}

// LastError returns the most recent failure.
func (r *Relay) LastError() error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.lastError
}

func (r *Relay) setStatus(status Status, err error) {
	r.mu.Lock()
	oldStatus := r.status
	r.status = status
	r.lastError = err
	observers := append([]chan StatusEvent(nil), r.observers...)
	r.mu.Unlock()

	logFields := log.Fields{
		"project_id":    r.cfg.ProjectID,
		"old_status":    oldStatus,
		"new_status":    status,
		"num_observers": len(observers),
	}
	if err != nil {
		logFields["error"] = err.Error()
		log.WithFields(logFields).Warn("gateway/relay: status transition with error")
	} else {
		log.WithFields(logFields).Debug("gateway/relay: status transition")
	}

	evt := StatusEvent{Status: status, Err: err, When: time.Now()}
	for _, ch := range observers {
		select {
		case ch <- evt:
		default:
		}
	}
}

// Subscribe allows callers to watch status changes.
func (r *Relay) Subscribe() <-chan StatusEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	ch := make(chan StatusEvent, 4)
	r.observers = append(r.observers, ch)
	return ch
}

// ActivityChan returns a channel that signals when data flows through the relay.
// Used for idle timeout tracking. Signals are sent non-blocking, so consumers
// should drain the channel to detect activity.
func (r *Relay) ActivityChan() <-chan struct{} {
	return r.activityCh
}

// startKeepalive runs a goroutine that periodically pings the downstream connection.
// If a ping fails or pong times out, the connection is invalidated for immediate reconnection.
// This proactively detects stale connections (e.g., after pod restart with new IP).
func (r *Relay) startKeepalive(ctx context.Context) {
	ticker := time.NewTicker(r.cfg.PingInterval)
	defer ticker.Stop()

	log.WithFields(log.Fields{
		"project_id":    r.cfg.ProjectID,
		"ping_interval": r.cfg.PingInterval.String(),
		"pong_timeout":  r.cfg.PongTimeout.String(),
	}).Debug("gateway/relay: starting keepalive goroutine")

	for {
		select {
		case <-ctx.Done():
			log.WithFields(log.Fields{
				"project_id": r.cfg.ProjectID,
			}).Debug("gateway/relay: keepalive goroutine stopped")
			return
		case <-ticker.C:
			r.mu.RLock()
			conn := r.downstream
			r.mu.RUnlock()

			if conn == nil {
				// No connection to ping, skip this cycle
				continue
			}

			// Set a deadline for receiving the pong response
			if err := conn.SetReadDeadline(time.Now().Add(r.cfg.PongTimeout)); err != nil {
				log.WithFields(log.Fields{
					"project_id": r.cfg.ProjectID,
					"error":      err.Error(),
				}).Warn("gateway/relay: failed to set read deadline for keepalive")
				// Don't invalidate on deadline set failure, just continue
				continue
			}

			// Send ping (gorilla/websocket handles pong automatically if SetPongHandler is set)
			if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)); err != nil {
				log.WithFields(log.Fields{
					"project_id": r.cfg.ProjectID,
					"error":      err.Error(),
				}).Warn("gateway/relay: keepalive ping failed, invalidating connection")
				r.InvalidateConnection()
				return // Exit keepalive loop - will be restarted on reconnect
			}

			// Reset read deadline after successful ping (allow normal operation)
			// The pong will be handled by the pipe goroutine's ReadMessage
			if err := conn.SetReadDeadline(time.Time{}); err != nil {
				log.WithFields(log.Fields{
					"project_id": r.cfg.ProjectID,
					"error":      err.Error(),
				}).Warn("gateway/relay: failed to clear read deadline after ping")
			}
		}
	}
}

// InvalidateConnection closes the current downstream connection and resets the circuit breaker.
// This allows immediate reconnection attempts, useful when we've detected a stale connection
// (via keepalive failure or known pod IP change).
func (r *Relay) InvalidateConnection() {
	r.mu.Lock()
	conn := r.downstream
	r.downstream = nil

	// Cancel keepalive goroutine if running
	if r.keepaliveCancel != nil {
		r.keepaliveCancel()
		r.keepaliveCancel = nil
	}
	r.mu.Unlock()

	if conn != nil {
		log.WithFields(log.Fields{
			"project_id": r.cfg.ProjectID,
		}).Info("gateway/relay: invalidating stale connection")
		_ = conn.Close()
	}

	// Reset circuit breaker to allow immediate reconnection
	r.circuitBreaker.Reset()

	// Set status to idle so new connection attempts can proceed
	r.setStatus(StatusIdle, nil)
}

// Connect ensures a downstream WebSocket connection exists, retrying with backoff until context cancellation.
func (r *Relay) Connect(ctx context.Context, backoff Backoff) (*websocket.Conn, error) {
	r.mu.RLock()
	initialStatus := r.status
	hasExistingConn := r.downstream != nil
	cbFailures := r.circuitBreaker.Failures()
	r.mu.RUnlock()

	log.WithFields(log.Fields{
		"project_id":        r.cfg.ProjectID,
		"endpoint":          r.cfg.Endpoint.String(),
		"initial_status":    initialStatus,
		"has_existing_conn": hasExistingConn,
		"cb_failures":       cbFailures,
	}).Debug("gateway/relay: Connect() called")

	r.mu.RLock()
	if r.downstream != nil {
		conn := r.downstream
		status := r.status
		r.mu.RUnlock()
		log.WithFields(log.Fields{
			"project_id": r.cfg.ProjectID,
			"endpoint":   r.cfg.Endpoint.String(),
			"status":     status,
		}).Debug("gateway/relay: reusing existing downstream connection")
		return conn, nil
	}
	r.mu.RUnlock()

	attempt := 0
	for {
		if ctx.Err() != nil {
			log.WithFields(log.Fields{
				"project_id": r.cfg.ProjectID,
				"endpoint":   r.cfg.Endpoint.String(),
				"error":      ctx.Err().Error(),
				"attempt":    attempt,
			}).Debug("gateway/relay: context error during connect")
			return nil, ctx.Err()
		}

		// Check if relay was closed externally (via Close() method)
		// NOTE: This check is for permanent closure only. The circuit breaker
		// no longer sets StatusClosed, so this should only trigger when Close()
		// is explicitly called.
		r.mu.RLock()
		currentStatus := r.status
		r.mu.RUnlock()

		log.WithFields(log.Fields{
			"project_id":     r.cfg.ProjectID,
			"current_status": currentStatus,
			"attempt":        attempt,
			"cb_failures":    r.circuitBreaker.Failures(),
		}).Trace("gateway/relay: Connect() checking status before dial")

		if currentStatus == StatusClosed {
			log.WithFields(log.Fields{
				"project_id":  r.cfg.ProjectID,
				"status":      currentStatus,
				"attempt":     attempt,
				"cb_failures": r.circuitBreaker.Failures(),
				"note":        "relay was explicitly closed via Close(), not circuit breaker",
			}).Debug("gateway/relay: relay closed during connect, aborting")
			return nil, errors.New("relay closed")
		}

		r.setStatus(StatusConnecting, nil)
		log.WithFields(log.Fields{
			"project_id":  r.cfg.ProjectID,
			"endpoint":    r.cfg.Endpoint.String(),
			"attempt":     attempt + 1,
			"cb_failures": r.circuitBreaker.Failures(),
		}).Debug("gateway/relay: attempting to connect downstream")

		dialURL := r.cfg.Endpoint.String()
		forceNext, shellNext := r.consumeNextConnectParams()
		if forceNext || shellNext != "" {
			u := *r.cfg.Endpoint
			q := u.Query()
			if forceNext {
				q.Set("force", "true")
			}
			if shellNext != "" {
				q.Set("shell", shellNext)
			}
			u.RawQuery = q.Encode()
			dialURL = u.String()
		}
		conn, resp, err := r.cfg.Dialer.DialContext(ctx, dialURL, r.cfg.Headers)
		if err == nil {
			r.mu.Lock()
			r.downstream = conn

			// Cancel any existing keepalive goroutine before starting new one
			if r.keepaliveCancel != nil {
				r.keepaliveCancel()
			}
			// Start keepalive goroutine for proactive stale connection detection
			keepaliveCtx, keepaliveCancel := context.WithCancel(context.Background())
			r.keepaliveCancel = keepaliveCancel
			go r.startKeepalive(keepaliveCtx)

			r.mu.Unlock()
			r.setStatus(StatusConnected, nil)
			log.WithFields(log.Fields{
				"project_id": r.cfg.ProjectID,
				"endpoint":   r.cfg.Endpoint.String(),
				"attempts":   attempt + 1,
			}).Info("gateway/relay: successfully connected to downstream")
			return conn, nil
		}

		attempt++
		statusCode := 0
		if resp != nil {
			statusCode = resp.StatusCode
		}
		log.WithFields(log.Fields{
			"project_id":  r.cfg.ProjectID,
			"endpoint":    r.cfg.Endpoint.String(),
			"attempt":     attempt,
			"http_status": statusCode,
			"error":       err.Error(),
			"cb_failures": r.circuitBreaker.Failures(),
		}).Warn("gateway/relay: downstream connect failed")

		r.setStatus(StatusReconnecting, err)
		wait := backoff.Next(attempt)
		log.WithFields(log.Fields{
			"project_id":  r.cfg.ProjectID,
			"wait":        wait.String(),
			"attempt":     attempt,
			"cb_failures": r.circuitBreaker.Failures(),
		}).Debug("gateway/relay: waiting before retry")

		select {
		case <-time.After(wait):
			log.WithFields(log.Fields{
				"project_id": r.cfg.ProjectID,
				"attempt":    attempt,
			}).Trace("gateway/relay: backoff wait completed, retrying")
		case <-ctx.Done():
			log.WithFields(log.Fields{
				"project_id": r.cfg.ProjectID,
				"endpoint":   r.cfg.Endpoint.String(),
				"error":      ctx.Err().Error(),
				"attempt":    attempt,
			}).Debug("gateway/relay: context cancelled during backoff")
			return nil, ctx.Err()
		}
	}
}

// Close tears down the downstream connection.
func (r *Relay) Close() error {
	r.mu.Lock()
	downstream := r.downstream
	r.downstream = nil
	r.status = StatusClosed
	observers := append([]chan StatusEvent(nil), r.observers...)
	numObservers := len(observers)

	// Cancel keepalive goroutine
	if r.keepaliveCancel != nil {
		r.keepaliveCancel()
		r.keepaliveCancel = nil
	}
	r.mu.Unlock()

	log.WithFields(log.Fields{
		"project_id":    r.cfg.ProjectID,
		"had_conn":      downstream != nil,
		"num_observers": numObservers,
	}).Debug("gateway/relay: closing relay")

	// Close connection outside of lock
	var err error
	if downstream != nil {
		err = downstream.Close()
		if err != nil {
			log.WithFields(log.Fields{
				"project_id": r.cfg.ProjectID,
				"error":      err.Error(),
			}).Warn("gateway/relay: error closing downstream connection")
		} else {
			log.WithFields(log.Fields{
				"project_id": r.cfg.ProjectID,
			}).Debug("gateway/relay: downstream connection closed successfully")
		}
	}

	// Notify observers outside of lock to avoid deadlock
	evt := StatusEvent{Status: StatusClosed, Err: err, When: time.Now()}
	for _, ch := range observers {
		select {
		case ch <- evt:
		default:
		}
	}

	log.WithFields(log.Fields{
		"project_id": r.cfg.ProjectID,
		"had_error":  err != nil,
	}).Info("gateway/relay: relay closed")

	return err
}

// Proxy pumps data between upstream and downstream with automatic reconnection.
// On downstream failure, attempts reconnection up to circuit breaker threshold.
// On upstream failure (browser disconnected), exits immediately.
func (r *Relay) Proxy(ctx context.Context, upstream *websocket.Conn) error {
	r.mu.RLock()
	currentStatus := r.status
	cbFailures := r.circuitBreaker.Failures()
	r.mu.RUnlock()

	log.WithFields(log.Fields{
		"project_id":     r.cfg.ProjectID,
		"endpoint":       r.cfg.Endpoint.String(),
		"current_status": currentStatus,
		"cb_failures":    cbFailures,
	}).Debug("gateway/relay: starting proxy")

	for {
		// Check circuit breaker before attempting connection
		cbOpen := r.circuitBreaker.IsOpen()
		cbFailures = r.circuitBreaker.Failures()

		log.WithFields(log.Fields{
			"project_id":      r.cfg.ProjectID,
			"circuit_breaker": map[string]interface{}{"open": cbOpen, "failures": cbFailures},
			"current_status":  r.Status(),
		}).Trace("gateway/relay: proxy loop iteration - checking circuit breaker")

		if cbOpen {
			err := fmt.Errorf("circuit breaker open after %d consecutive failures", cbFailures)
			log.WithFields(log.Fields{
				"project_id": r.cfg.ProjectID,
				"failures":   cbFailures,
			}).Error("gateway/relay: circuit breaker open, giving up")
			// BUG FIX: Do NOT set status to StatusClosed here!
			// Setting StatusClosed prevents future reconnection attempts when the circuit
			// breaker goes half-open. The status should remain as-is (probably StatusReconnecting).
			// Only Close() should set StatusClosed.
			log.WithFields(log.Fields{
				"project_id":     r.cfg.ProjectID,
				"current_status": r.Status(),
				"note":           "NOT setting StatusClosed - would prevent half-open reconnection",
			}).Debug("gateway/relay: circuit breaker open - returning error without status change")
			return err
		}

		// Connect to downstream
		log.WithFields(log.Fields{
			"project_id":     r.cfg.ProjectID,
			"endpoint":       r.cfg.Endpoint.String(),
			"current_status": r.Status(),
		}).Debug("gateway/relay: attempting downstream connection")

		downstream, err := r.Connect(ctx, DefaultBackoff())
		if err != nil {
			log.WithFields(log.Fields{
				"project_id":     r.cfg.ProjectID,
				"endpoint":       r.cfg.Endpoint.String(),
				"error":          err.Error(),
				"current_status": r.Status(),
			}).Error("gateway/relay: failed to connect downstream")
			r.circuitBreaker.RecordFailureWithType(ClassifyError(err), r.cfg.ProjectID)
			return err
		}

		// Run the bidirectional pipe
		pipeErr := r.runPipes(ctx, upstream, downstream)

		// Clean up downstream connection
		_ = downstream.Close()
		r.mu.Lock()
		if r.downstream == downstream {
			r.downstream = nil
		}
		r.mu.Unlock()

		// Check what kind of error we got
		if pipeErr == nil || errors.Is(pipeErr, context.Canceled) {
			log.WithFields(log.Fields{
				"project_id": r.cfg.ProjectID,
			}).Debug("gateway/relay: pipe exited cleanly")
			return nil
		}

		// Check if this is a pipeError to determine if it's upstream or downstream
		var pErr *pipeError
		if errors.As(pipeErr, &pErr) {
			if pErr.direction == "up->down" && pErr.isRead {
				// Upstream (browser) disconnected - don't reconnect, just exit
				// IMPORTANT: Do NOT set StatusClosed here! The relay needs to remain
				// reusable so that when the browser reconnects (same tab), we can
				// reconnect to the downstream. Only Close() should set StatusClosed.
				log.WithFields(log.Fields{
					"project_id":     r.cfg.ProjectID,
					"error":          pErr.err.Error(),
					"current_status": r.Status(),
					"note":           "NOT setting StatusClosed - relay remains reusable",
				}).Info("gateway/relay: upstream disconnected, exiting proxy loop")
				// Set status back to idle so relay can accept new connections
				r.setStatus(StatusIdle, nil)
				return pipeErr
			}

			// Downstream error - attempt reconnection
			log.WithFields(log.Fields{
				"project_id": r.cfg.ProjectID,
				"direction":  pErr.direction,
				"is_read":    pErr.isRead,
				"error":      pErr.err.Error(),
			}).Warn("gateway/relay: downstream error, attempting reconnection")
		} else {
			// Unknown error type - attempt reconnection
			log.WithFields(log.Fields{
				"project_id": r.cfg.ProjectID,
				"error":      pipeErr.Error(),
			}).Warn("gateway/relay: pipe error, attempting reconnection")
		}

		// Check if relay was closed externally before attempting reconnection.
		// This prevents the "invalid Body.Read call. After hijacked" panic
		// that occurs when trying to read from an upstream connection after
		// the relay has been closed.
		r.mu.RLock()
		if r.status == StatusClosed {
			r.mu.RUnlock()
			log.WithFields(log.Fields{
				"project_id": r.cfg.ProjectID,
			}).Info("gateway/relay: relay closed externally, exiting proxy loop")
			return nil
		}
		r.mu.RUnlock()

		// Record failure and check if we should continue
		// Use error classification to detect pod restart scenarios (connection refused, EOF, etc.)
		r.circuitBreaker.RecordFailureWithType(ClassifyError(pipeErr), r.cfg.ProjectID)
		r.setStatus(StatusReconnecting, pipeErr)

		// Brief delay before reconnection attempt
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
			// Continue to next iteration
		}
	}
}

// runPipes runs bidirectional pipes and waits for both to complete.
// Returns the first error encountered.
func (r *Relay) runPipes(ctx context.Context, upstream, downstream *websocket.Conn) error {
	log.WithFields(log.Fields{
		"project_id": r.cfg.ProjectID,
		"endpoint":   r.cfg.Endpoint.String(),
	}).Info("gateway/relay: starting bidirectional pipe")

	// Reset circuit breaker on successful connection
	r.circuitBreaker.RecordSuccess()

	pipeCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	errCh := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		r.pipe(pipeCtx, "up->down", upstream, downstream, errCh)
	}()
	go func() {
		defer wg.Done()
		r.pipe(pipeCtx, "down->up", downstream, upstream, errCh)
	}()

	// Wait for first error or context cancellation
	var firstErr error
	select {
	case <-ctx.Done():
		firstErr = ctx.Err()
		log.WithFields(log.Fields{
			"project_id": r.cfg.ProjectID,
			"endpoint":   r.cfg.Endpoint.String(),
			"reason":     firstErr.Error(),
		}).Debug("gateway/relay: pipe loop exiting — parent context cancelled")
	case firstErr = <-errCh:
		var pe *pipeError
		if errors.As(firstErr, &pe) {
			log.WithFields(log.Fields{
				"project_id": r.cfg.ProjectID,
				"endpoint":   r.cfg.Endpoint.String(),
				"direction":  pe.direction,
				"is_read":    pe.isRead,
				"error":      pe.err.Error(),
			}).Debug("gateway/relay: pipe loop exiting — pipe error")
		} else {
			log.WithFields(log.Fields{
				"project_id": r.cfg.ProjectID,
				"endpoint":   r.cfg.Endpoint.String(),
				"error":      firstErr.Error(),
			}).Debug("gateway/relay: pipe loop exiting — context error from pipe")
		}
	}

	// Cancel context to signal other pipe to exit
	cancel()

	// Wait for both pipes to complete (with timeout to prevent hanging)
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.WithFields(log.Fields{
			"project_id": r.cfg.ProjectID,
		}).Debug("gateway/relay: both pipes exited cleanly")
	case <-time.After(5 * time.Second):
		log.WithFields(log.Fields{
			"project_id": r.cfg.ProjectID,
		}).Warn("gateway/relay: timeout waiting for pipes to exit")
	}

	// Drain any remaining error
	select {
	case <-errCh:
	default:
	}

	return firstErr
}

// pipeError wraps errors with direction information for proper error handling
type pipeError struct {
	err       error
	direction string // "up->down" or "down->up"
	isRead    bool   // true if error occurred during read, false for write
}

func (e *pipeError) Error() string {
	return e.err.Error()
}

func (e *pipeError) Unwrap() error {
	return e.err
}

func (r *Relay) pipe(ctx context.Context, label string, src *websocket.Conn, dst *websocket.Conn, errCh chan<- error) {
	// Recover from panics that can occur when reading from an invalidated
	// WebSocket connection (e.g., "invalid Body.Read call. After hijacked").
	// This can happen during relay shutdown races.
	defer func() {
		if p := recover(); p != nil {
			log.WithFields(log.Fields{
				"project_id": r.cfg.ProjectID,
				"direction":  label,
				"panic":      p,
			}).Error("gateway/relay: pipe recovered from panic")
			errCh <- &pipeError{
				err:       fmt.Errorf("relay %s: panic in %s: %v", r.cfg.ProjectID, label, p),
				direction: label,
				isRead:    true,
			}
		}
	}()

	log.WithFields(log.Fields{
		"project_id": r.cfg.ProjectID,
		"direction":  label,
	}).Debug("gateway/relay: pipe goroutine started")

	// Watcher goroutine: when the context is cancelled, set a short read
	// deadline on src so any blocked ReadMessage() call returns promptly.
	// Without this, gorilla/websocket's ReadMessage() blocks indefinitely
	// and auto-responds to ping frames, keeping the downstream TCP connection
	// alive (and holding the project's single-client slot) for hours after
	// the relay has logically shut down.
	stopWatcher := make(chan struct{})
	defer close(stopWatcher)
	go func() {
		select {
		case <-ctx.Done():
			// Force any blocked ReadMessage to return within 100 ms.
			log.WithFields(log.Fields{
				"project_id": r.cfg.ProjectID,
				"direction":  label,
			}).Debug("gateway/relay: pipe watcher firing — setting read deadline to unblock ReadMessage")
			_ = src.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		case <-stopWatcher:
		}
	}()

	for {
		select {
		case <-ctx.Done():
			log.WithFields(log.Fields{
				"project_id": r.cfg.ProjectID,
				"direction":  label,
			}).Debug("gateway/relay: pipe goroutine context cancelled")
			errCh <- ctx.Err()
			return
		default:
		}
		msgType, data, err := src.ReadMessage()
		if err != nil {
			// If the context was cancelled, the watcher goroutine may have
			// triggered a deadline timeout to unblock ReadMessage(). Treat
			// this as a clean shutdown rather than a connection error.
			select {
			case <-ctx.Done():
				log.WithFields(log.Fields{
					"project_id": r.cfg.ProjectID,
					"direction":  label,
				}).Debug("gateway/relay: pipe goroutine unblocked by context cancellation")
				// Reset the deadline so the connection can be reused on reconnect.
				_ = src.SetReadDeadline(time.Time{})
				errCh <- ctx.Err()
				return
			default:
			}
			log.WithFields(log.Fields{
				"project_id": r.cfg.ProjectID,
				"direction":  label,
				"error":      err.Error(),
			}).Warn("gateway/relay: pipe read error")
			errCh <- &pipeError{
				err:       fmt.Errorf("relay %s: read %s: %w", r.cfg.ProjectID, label, err),
				direction: label,
				isRead:    true,
			}
			return
		}
		if err := dst.WriteMessage(msgType, data); err != nil {
			log.WithFields(log.Fields{
				"project_id":   r.cfg.ProjectID,
				"direction":    label,
				"error":        err.Error(),
				"message_type": msgType,
				"data_size":    len(data),
			}).Warn("gateway/relay: pipe write error")
			errCh <- &pipeError{
				err:       fmt.Errorf("relay %s: write %s: %w", r.cfg.ProjectID, label, err),
				direction: label,
				isRead:    false,
			}
			return
		}

		// Signal activity for idle timeout tracking (non-blocking)
		select {
		case r.activityCh <- struct{}{}:
		default:
			// Channel full, skip signal (activity already noted)
		}
	}
}

// Backoff controls retry timing.
type Backoff interface {
	Next(attempt int) time.Duration
}

// defaultBackoff implements simple exponential delays.
type defaultBackoff struct{}

// DefaultBackoff returns a shared instance.
func DefaultBackoff() Backoff { return defaultBackoff{} }

func (defaultBackoff) Next(attempt int) time.Duration {
	if attempt <= 1 {
		return time.Second
	}
	if attempt > 6 {
		attempt = 6
	}
	return time.Duration(1<<uint(attempt-1)) * time.Second
}
