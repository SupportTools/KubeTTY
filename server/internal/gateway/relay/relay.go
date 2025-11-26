package relay

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
)

// CircuitBreaker tracks consecutive failures and opens to prevent reconnection storms.
type CircuitBreaker struct {
	consecutiveFailures int32
	lastFailure         atomic.Value // time.Time
	threshold           int32        // failures before opening
	resetAfter          time.Duration
}

// NewCircuitBreaker creates a circuit breaker with the given threshold.
func NewCircuitBreaker(threshold int, resetAfter time.Duration) *CircuitBreaker {
	cb := &CircuitBreaker{
		threshold:  int32(threshold),
		resetAfter: resetAfter,
	}
	cb.lastFailure.Store(time.Time{})
	return cb
}

// RecordFailure increments the failure count.
func (cb *CircuitBreaker) RecordFailure() {
	atomic.AddInt32(&cb.consecutiveFailures, 1)
	cb.lastFailure.Store(time.Now())
}

// RecordSuccess resets the failure count.
func (cb *CircuitBreaker) RecordSuccess() {
	atomic.StoreInt32(&cb.consecutiveFailures, 0)
}

// IsOpen returns true if too many consecutive failures have occurred.
func (cb *CircuitBreaker) IsOpen() bool {
	failures := atomic.LoadInt32(&cb.consecutiveFailures)
	if failures < cb.threshold {
		return false
	}
	// Check if enough time has passed to reset
	lastFail := cb.lastFailure.Load().(time.Time)
	if time.Since(lastFail) > cb.resetAfter {
		// Half-open: allow one attempt
		return false
	}
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

// New creates a relay for a downstream endpoint.
func New(cfg Config) *Relay {
	if cfg.Dialer == nil {
		cfg.Dialer = websocket.DefaultDialer
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

// Connect ensures a downstream WebSocket connection exists, retrying with backoff until context cancellation.
func (r *Relay) Connect(ctx context.Context, backoff Backoff) (*websocket.Conn, error) {
	r.mu.RLock()
	if r.downstream != nil {
		conn := r.downstream
		r.mu.RUnlock()
		log.WithFields(log.Fields{
			"project_id": r.cfg.ProjectID,
			"endpoint":   r.cfg.Endpoint.String(),
			"status":     r.status,
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
			}).Debug("gateway/relay: context error during connect")
			return nil, ctx.Err()
		}
		r.setStatus(StatusConnecting, nil)
		log.WithFields(log.Fields{
			"project_id": r.cfg.ProjectID,
			"endpoint":   r.cfg.Endpoint.String(),
			"attempt":    attempt + 1,
		}).Debug("gateway/relay: attempting to connect downstream")
		conn, resp, err := r.cfg.Dialer.DialContext(ctx, r.cfg.Endpoint.String(), r.cfg.Headers)
		if err == nil {
			r.mu.Lock()
			r.downstream = conn
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
		}).Warn("gateway/relay: downstream connect failed")
		r.setStatus(StatusReconnecting, err)
		wait := backoff.Next(attempt)
		log.WithFields(log.Fields{
			"project_id": r.cfg.ProjectID,
			"wait":       wait.String(),
			"attempt":    attempt,
		}).Debug("gateway/relay: waiting before retry")
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			log.WithFields(log.Fields{
				"project_id": r.cfg.ProjectID,
				"endpoint":   r.cfg.Endpoint.String(),
				"error":      ctx.Err().Error(),
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
	log.WithFields(log.Fields{
		"project_id": r.cfg.ProjectID,
		"endpoint":   r.cfg.Endpoint.String(),
	}).Debug("gateway/relay: starting proxy")

	for {
		// Check circuit breaker before attempting connection
		if r.circuitBreaker.IsOpen() {
			err := fmt.Errorf("circuit breaker open after %d consecutive failures", r.circuitBreaker.Failures())
			log.WithFields(log.Fields{
				"project_id": r.cfg.ProjectID,
				"failures":   r.circuitBreaker.Failures(),
			}).Error("gateway/relay: circuit breaker open, giving up")
			r.setStatus(StatusClosed, err)
			return err
		}

		// Connect to downstream
		downstream, err := r.Connect(ctx, DefaultBackoff())
		if err != nil {
			log.WithFields(log.Fields{
				"project_id": r.cfg.ProjectID,
				"endpoint":   r.cfg.Endpoint.String(),
				"error":      err.Error(),
			}).Error("gateway/relay: failed to connect downstream")
			r.circuitBreaker.RecordFailure()
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
				log.WithFields(log.Fields{
					"project_id": r.cfg.ProjectID,
					"error":      pErr.err.Error(),
				}).Info("gateway/relay: upstream disconnected, exiting")
				r.setStatus(StatusClosed, pipeErr)
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

		// Record failure and check if we should continue
		r.circuitBreaker.RecordFailure()
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
	case firstErr = <-errCh:
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
	log.WithFields(log.Fields{
		"project_id": r.cfg.ProjectID,
		"direction":  label,
	}).Debug("gateway/relay: pipe goroutine started")

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
