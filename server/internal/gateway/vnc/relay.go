package vnc

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"

	"github.com/supporttools/KubeTTY/server/internal/gateway/relay"
)

// RelayStatus is an alias to relay.Status for VNC relay.
type RelayStatus = relay.Status

// Relay status constants using the relay package values.
const (
	RelayStatusIdle         = relay.StatusIdle
	RelayStatusConnecting   = relay.StatusConnecting
	RelayStatusConnected    = relay.StatusConnected
	RelayStatusReconnecting = relay.StatusReconnecting
	RelayStatusClosed       = relay.StatusClosed
)

// VNCRelay manages bidirectional streaming between a WebSocket client and a VNC server.
// It bridges WebSocket binary frames to raw TCP for VNC's RFB protocol.
type VNCRelay struct {
	config Config

	mu         sync.RWMutex
	vncConn    net.Conn
	status     RelayStatus
	lastError  error
	observers  []chan relay.StatusEvent
	activityCh chan struct{}
	retryCount int

	// Context for graceful shutdown
	cancelFunc context.CancelFunc
}

// NewRelay creates a new VNC relay for the specified target.
// Target should be in the format "host:port" (e.g., "vnc-service.namespace.svc:5901").
func NewRelay(target string, opts ...Option) *VNCRelay {
	cfg := NewConfig(target, opts...)
	return &VNCRelay{
		config:     cfg,
		status:     RelayStatusIdle,
		activityCh: make(chan struct{}, 1),
	}
}

// Status returns the current relay status.
func (r *VNCRelay) Status() RelayStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.status
}

// LastError returns the most recent error.
func (r *VNCRelay) LastError() error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.lastError
}

func (r *VNCRelay) setStatus(status RelayStatus, err error) {
	r.mu.Lock()
	oldStatus := r.status
	r.status = status
	r.lastError = err
	observers := append([]chan relay.StatusEvent(nil), r.observers...)
	r.mu.Unlock()

	logFields := log.Fields{
		"target":     r.config.Target,
		"old_status": oldStatus,
		"new_status": status,
	}
	if err != nil {
		logFields["error"] = err.Error()
		log.WithFields(logFields).Warn("gateway/vnc: status transition with error")
	} else {
		log.WithFields(logFields).Debug("gateway/vnc: status transition")
	}

	evt := relay.StatusEvent{Status: status, Err: err, When: time.Now()}
	for _, ch := range observers {
		select {
		case ch <- evt:
		default:
		}
	}
}

// Subscribe returns a channel for status updates.
func (r *VNCRelay) Subscribe() <-chan relay.StatusEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	ch := make(chan relay.StatusEvent, 4)
	r.observers = append(r.observers, ch)
	return ch
}

// ActivityChan returns a channel that signals data activity.
// Used for idle timeout tracking.
func (r *VNCRelay) ActivityChan() <-chan struct{} {
	return r.activityCh
}

// connect establishes a TCP connection to the VNC server.
func (r *VNCRelay) connect(ctx context.Context) (net.Conn, error) {
	r.setStatus(RelayStatusConnecting, nil)

	log.WithFields(log.Fields{
		"target":  r.config.Target,
		"timeout": r.config.DialTimeout,
	}).Debug("gateway/vnc: connecting to VNC server")

	dialer := &net.Dialer{
		Timeout: r.config.DialTimeout,
	}

	conn, err := dialer.DialContext(ctx, "tcp", r.config.Target)
	if err != nil {
		log.WithFields(log.Fields{
			"target": r.config.Target,
			"error":  err.Error(),
		}).Warn("gateway/vnc: failed to connect to VNC server")
		return nil, fmt.Errorf("dial VNC server: %w", err)
	}

	log.WithFields(log.Fields{
		"target":      r.config.Target,
		"local_addr":  conn.LocalAddr().String(),
		"remote_addr": conn.RemoteAddr().String(),
	}).Info("gateway/vnc: connected to VNC server")

	return conn, nil
}

// reconnect attempts to re-establish the VNC connection after a failure.
func (r *VNCRelay) reconnect(ctx context.Context) error {
	r.mu.Lock()
	if r.status == RelayStatusClosed {
		r.mu.Unlock()
		return fmt.Errorf("relay is closed")
	}

	if r.retryCount >= r.config.MaxRetries {
		r.mu.Unlock()
		err := fmt.Errorf("max reconnection attempts (%d) exceeded", r.config.MaxRetries)
		r.setStatus(RelayStatusClosed, err)
		return err
	}

	r.retryCount++
	attempt := r.retryCount

	// Close existing connection if any
	oldConn := r.vncConn
	r.vncConn = nil
	r.mu.Unlock()

	if oldConn != nil {
		oldConn.Close()
	}

	r.setStatus(RelayStatusReconnecting, nil)

	// Calculate exponential backoff delay
	delay := r.config.RetryDelay * time.Duration(1<<(attempt-1))
	if delay > 30*time.Second {
		delay = 30 * time.Second
	}

	log.WithFields(log.Fields{
		"target":  r.config.Target,
		"attempt": attempt,
		"max":     r.config.MaxRetries,
		"delay":   delay,
	}).Info("gateway/vnc: attempting reconnection")

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(delay):
	}

	// Check if relay was closed during wait
	r.mu.RLock()
	if r.status == RelayStatusClosed {
		r.mu.RUnlock()
		return fmt.Errorf("relay closed during reconnection wait")
	}
	r.mu.RUnlock()

	// Attempt to connect
	conn, err := r.connect(ctx)
	if err != nil {
		log.WithFields(log.Fields{
			"target":  r.config.Target,
			"attempt": attempt,
			"error":   err.Error(),
		}).Warn("gateway/vnc: reconnection failed")
		return err
	}

	// Success - update state
	r.mu.Lock()
	if r.status == RelayStatusClosed {
		r.mu.Unlock()
		conn.Close()
		return fmt.Errorf("relay closed during reconnection")
	}
	r.vncConn = conn
	r.retryCount = 0
	r.mu.Unlock()

	r.setStatus(RelayStatusConnected, nil)

	log.WithFields(log.Fields{
		"target":  r.config.Target,
		"attempt": attempt,
	}).Info("gateway/vnc: reconnection successful")

	return nil
}

// Proxy connects a WebSocket client to the VNC server.
// This is the main entry point for handling VNC sessions.
func (r *VNCRelay) Proxy(ctx context.Context, upstream *websocket.Conn) error {
	log.WithFields(log.Fields{
		"target": r.config.Target,
	}).Debug("gateway/vnc: starting proxy")

	// Establish initial VNC connection
	if err := r.ensureConnection(ctx); err != nil {
		return err
	}

	// Create cancellable context for cleanup
	proxyCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	r.mu.Lock()
	r.cancelFunc = cancel
	vncConn := r.vncConn
	r.mu.Unlock()

	// Run bidirectional pipes
	return r.runPipes(proxyCtx, upstream, vncConn)
}

// ensureConnection establishes the VNC connection if not already connected.
// If the relay was previously closed due to a failed connection, it resets
// the status to allow retry. This supports noVNC's reconnection behavior.
func (r *VNCRelay) ensureConnection(ctx context.Context) error {
	r.mu.Lock()
	if r.vncConn != nil && r.status == RelayStatusConnected {
		r.mu.Unlock()
		return nil
	}

	// Reset closed state for retry - allows reconnection after failure
	// The relay should only stay closed after explicit Close() call
	if r.status == RelayStatusClosed {
		log.WithFields(log.Fields{
			"target": r.config.Target,
		}).Debug("gateway/vnc: resetting closed relay for new connection attempt")
		r.status = RelayStatusIdle
		r.retryCount = 0
		r.lastError = nil
	}
	r.mu.Unlock()

	conn, err := r.connect(ctx)
	if err != nil {
		// Don't set to Closed on connection failure - allow future retries
		// Just return the error so the current Proxy call fails
		return err
	}

	r.mu.Lock()
	if r.status == RelayStatusClosed {
		r.mu.Unlock()
		conn.Close()
		return fmt.Errorf("relay closed during connection")
	}
	r.vncConn = conn
	r.mu.Unlock()

	r.setStatus(RelayStatusConnected, nil)
	return nil
}

// runPipes runs bidirectional data transfer between WebSocket and TCP.
func (r *VNCRelay) runPipes(ctx context.Context, upstream *websocket.Conn, vncConn net.Conn) error {
	log.WithFields(log.Fields{
		"target": r.config.Target,
	}).Info("gateway/vnc: starting bidirectional pipe")

	errCh := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)

	// WebSocket -> VNC (client input: mouse, keyboard events)
	go func() {
		defer wg.Done()
		r.wsToVNC(ctx, upstream, vncConn, errCh)
	}()

	// VNC -> WebSocket (server output: framebuffer updates)
	go func() {
		defer wg.Done()
		r.vncToWS(ctx, vncConn, upstream, errCh)
	}()

	// Wait for first error or context cancellation
	var firstErr error
	select {
	case <-ctx.Done():
		firstErr = ctx.Err()
	case firstErr = <-errCh:
	}

	// Clean up
	log.WithFields(log.Fields{
		"target": r.config.Target,
		"error":  fmt.Sprintf("%v", firstErr),
	}).Debug("gateway/vnc: pipe ended, cleaning up")

	// Close connections to signal goroutines to exit
	vncConn.Close()
	upstream.Close()

	// Clear the connection reference so ensureConnection will establish a new one
	// This is critical: the relay is reused across WebSocket reconnections,
	// so we must reset state to allow new VNC connections.
	r.mu.Lock()
	r.vncConn = nil
	r.status = RelayStatusIdle
	r.mu.Unlock()

	// Wait for both pipes to complete with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.WithFields(log.Fields{
			"target": r.config.Target,
		}).Debug("gateway/vnc: both pipes exited cleanly")
	case <-time.After(5 * time.Second):
		log.WithFields(log.Fields{
			"target": r.config.Target,
		}).Warn("gateway/vnc: timeout waiting for pipes to exit")
	}

	// Drain remaining error
	select {
	case <-errCh:
	default:
	}

	return firstErr
}

// wsToVNC reads from WebSocket and writes to VNC TCP connection.
// This handles client input (mouse movements, clicks, keyboard events).
func (r *VNCRelay) wsToVNC(ctx context.Context, ws *websocket.Conn, vnc net.Conn, errCh chan<- error) {
	defer func() {
		if p := recover(); p != nil {
			log.WithFields(log.Fields{
				"target": r.config.Target,
				"panic":  p,
			}).Error("gateway/vnc: wsToVNC recovered from panic")
			errCh <- fmt.Errorf("ws->vnc panic: %v", p)
		}
	}()

	log.WithFields(log.Fields{
		"target": r.config.Target,
	}).Debug("gateway/vnc: wsToVNC pipe started")

	for {
		select {
		case <-ctx.Done():
			errCh <- ctx.Err()
			return
		default:
		}

		// Set read deadline for WebSocket
		if err := ws.SetReadDeadline(time.Now().Add(r.config.ReadTimeout)); err != nil {
			errCh <- fmt.Errorf("ws set read deadline: %w", err)
			return
		}

		msgType, data, err := ws.ReadMessage()
		if err != nil {
			log.WithFields(log.Fields{
				"target": r.config.Target,
				"error":  err.Error(),
			}).Debug("gateway/vnc: wsToVNC read error")
			errCh <- fmt.Errorf("ws read: %w", err)
			return
		}

		// Only handle binary messages (VNC uses binary RFB protocol)
		if msgType != websocket.BinaryMessage {
			continue
		}

		// Write to VNC connection
		if _, err := vnc.Write(data); err != nil {
			log.WithFields(log.Fields{
				"target": r.config.Target,
				"error":  err.Error(),
			}).Debug("gateway/vnc: wsToVNC write error")
			errCh <- fmt.Errorf("vnc write: %w", err)
			return
		}

		// Signal activity
		select {
		case r.activityCh <- struct{}{}:
		default:
		}
	}
}

// vncToWS reads from VNC TCP connection and writes to WebSocket.
// This handles server output (framebuffer updates, server messages).
func (r *VNCRelay) vncToWS(ctx context.Context, vnc net.Conn, ws *websocket.Conn, errCh chan<- error) {
	defer func() {
		if p := recover(); p != nil {
			log.WithFields(log.Fields{
				"target": r.config.Target,
				"panic":  p,
			}).Error("gateway/vnc: vncToWS recovered from panic")
			errCh <- fmt.Errorf("vnc->ws panic: %v", p)
		}
	}()

	log.WithFields(log.Fields{
		"target": r.config.Target,
	}).Debug("gateway/vnc: vncToWS pipe started")

	buf := make([]byte, r.config.ReadBufferSize)

	for {
		select {
		case <-ctx.Done():
			errCh <- ctx.Err()
			return
		default:
		}

		// Read from VNC connection
		n, err := vnc.Read(buf)
		if err != nil {
			if err != io.EOF {
				log.WithFields(log.Fields{
					"target": r.config.Target,
					"error":  err.Error(),
				}).Debug("gateway/vnc: vncToWS read error")
			}
			errCh <- fmt.Errorf("vnc read: %w", err)
			return
		}

		if n > 0 {
			// Set write deadline for WebSocket
			if err := ws.SetWriteDeadline(time.Now().Add(r.config.WriteTimeout)); err != nil {
				errCh <- fmt.Errorf("ws set write deadline: %w", err)
				return
			}

			// Write as binary message to WebSocket
			if err := ws.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
				log.WithFields(log.Fields{
					"target": r.config.Target,
					"error":  err.Error(),
				}).Debug("gateway/vnc: vncToWS write error")
				errCh <- fmt.Errorf("ws write: %w", err)
				return
			}

			// Signal activity
			select {
			case r.activityCh <- struct{}{}:
			default:
			}
		}
	}
}

// Close terminates the relay and underlying VNC connection.
func (r *VNCRelay) Close() error {
	r.mu.Lock()
	// Check if already closed
	if r.status == RelayStatusClosed {
		r.mu.Unlock()
		return nil
	}

	vncConn := r.vncConn
	r.vncConn = nil
	cancelFunc := r.cancelFunc
	r.cancelFunc = nil
	observers := r.observers
	r.observers = nil
	r.status = RelayStatusClosed
	r.mu.Unlock()

	log.WithFields(log.Fields{
		"target": r.config.Target,
	}).Info("gateway/vnc: closing relay")

	// Cancel context to signal goroutines to exit
	if cancelFunc != nil {
		cancelFunc()
	}

	// Notify observers of closed status
	evt := relay.StatusEvent{Status: RelayStatusClosed, Err: nil, When: time.Now()}
	for _, ch := range observers {
		select {
		case ch <- evt:
		default:
		}
	}

	// Close underlying connection
	var err error
	if vncConn != nil {
		err = vncConn.Close()
	}

	// Close observer channels
	for _, ch := range observers {
		close(ch)
	}

	return err
}

// Target returns the VNC server target address.
func (r *VNCRelay) Target() string {
	return r.config.Target
}
