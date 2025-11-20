package relay

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

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
	cfg        Config
	downstream *websocket.Conn
	mu         sync.RWMutex
	status     Status
	lastError  error
	observers  []chan StatusEvent
	activityCh chan struct{} // Signals activity (data transfer) for idle timeout tracking
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
		cfg:        cfg,
		status:     StatusIdle,
		activityCh: make(chan struct{}, 1), // Buffer size 1 for non-blocking sends
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
	r.status = status
	r.lastError = err
	observers := append([]chan StatusEvent(nil), r.observers...)
	r.mu.Unlock()

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
		log.Printf("[Relay %s] Reusing existing downstream connection", r.cfg.ProjectID)
		return conn, nil
	}
	r.mu.RUnlock()

	attempt := 0
	for {
		if ctx.Err() != nil {
			log.Printf("[Relay %s] Context error during connect: %v", r.cfg.ProjectID, ctx.Err())
			return nil, ctx.Err()
		}
		r.setStatus(StatusConnecting, nil)
		log.Printf("[Relay %s] Dial attempt %d to %s", r.cfg.ProjectID, attempt+1, r.cfg.Endpoint.String())
		conn, resp, err := r.cfg.Dialer.DialContext(ctx, r.cfg.Endpoint.String(), r.cfg.Headers)
		if err == nil {
			r.mu.Lock()
			r.downstream = conn
			r.mu.Unlock()
			r.setStatus(StatusConnected, nil)
			log.Printf("[Relay %s] Successfully connected to downstream", r.cfg.ProjectID)
			return conn, nil
		}
		attempt++
		statusCode := 0
		if resp != nil {
			statusCode = resp.StatusCode
		}
		log.Printf("[Relay %s] Connect failed (attempt %d): %v (HTTP status: %d)", r.cfg.ProjectID, attempt, err, statusCode)
		r.setStatus(StatusReconnecting, err)
		wait := backoff.Next(attempt)
		log.Printf("[Relay %s] Waiting %s before retry", r.cfg.ProjectID, wait)
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			log.Printf("[Relay %s] Context cancelled during backoff", r.cfg.ProjectID)
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
	r.mu.Unlock()

	// Close connection outside of lock
	var err error
	if downstream != nil {
		err = downstream.Close()
	}

	// Notify observers outside of lock to avoid deadlock
	evt := StatusEvent{Status: StatusClosed, Err: err, When: time.Now()}
	for _, ch := range observers {
		select {
		case ch <- evt:
		default:
		}
	}

	return err
}

// Proxy pumps data between upstream and downstream.
func (r *Relay) Proxy(ctx context.Context, upstream *websocket.Conn) error {
	log.Printf("[Relay %s] Starting proxy", r.cfg.ProjectID)
	for {
		log.Printf("[Relay %s] Connecting to downstream: %s", r.cfg.ProjectID, r.cfg.Endpoint.String())
		downstream, err := r.Connect(ctx, DefaultBackoff())
		if err != nil {
			log.Printf("[Relay %s] Failed to connect downstream: %v", r.cfg.ProjectID, err)
			return err
		}
		log.Printf("[Relay %s] Downstream connected, starting bidirectional pipe", r.cfg.ProjectID)

		pipeCtx, cancel := context.WithCancel(ctx)
		errCh := make(chan error, 2)
		go r.pipe(pipeCtx, "up->down", upstream, downstream, errCh)
		go r.pipe(pipeCtx, "down->up", downstream, upstream, errCh)

		select {
		case <-ctx.Done():
			log.Printf("[Relay %s] Context cancelled", r.cfg.ProjectID)
			cancel()
			return ctx.Err()
		case err := <-errCh:
			log.Printf("[Relay %s] Pipe error: %v", r.cfg.ProjectID, err)
			cancel()
			_ = downstream.Close()
			r.mu.Lock()
			if r.downstream == downstream {
				r.downstream = nil
			}
			r.mu.Unlock()
			// drain second error if present
			select {
			case <-errCh:
			default:
			}
			if errors.Is(err, context.Canceled) {
				log.Printf("[Relay %s] Pipe cancelled, exiting", r.cfg.ProjectID)
				return nil
			}

			// Check if this is an upstream error (client disconnected) - don't reconnect
			// Upstream errors: read up->down failed (client stopped sending) or write down->up failed (can't send to client)
			// Downstream errors: write up->down failed (can't send to downstream) or read down->up failed (downstream stopped)
			if pe, ok := err.(*pipeError); ok {
				isUpstreamError := (pe.direction == "up->down" && pe.isRead) || (pe.direction == "down->up" && !pe.isRead)
				if isUpstreamError {
					log.Printf("[Relay %s] Upstream connection closed, exiting proxy", r.cfg.ProjectID)
					return err
				}
			}

			// Try again for downstream errors
			log.Printf("[Relay %s] Reconnecting after downstream error...", r.cfg.ProjectID)
			r.setStatus(StatusReconnecting, err)
			continue
		}
	}
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
	for {
		select {
		case <-ctx.Done():
			errCh <- ctx.Err()
			return
		default:
		}
		msgType, data, err := src.ReadMessage()
		if err != nil {
			errCh <- &pipeError{
				err:       fmt.Errorf("relay %s: read %s: %w", r.cfg.ProjectID, label, err),
				direction: label,
				isRead:    true,
			}
			return
		}
		if err := dst.WriteMessage(msgType, data); err != nil {
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
