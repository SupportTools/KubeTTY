package exec

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
	"k8s.io/client-go/rest"

	"github.com/supporttools/KubeTTY/server/internal/gateway/relay"
)

// Default reconnection settings
const (
	DefaultMaxRetries = 3
	DefaultRetryDelay = 5 * time.Second
)

// RelayConfig configures an ExecRelay.
type RelayConfig struct {
	Namespace    string
	PodName      string
	Container    string
	Command      []string
	BufferSize   int           // Output buffer size (default 64KB)
	ReadTimeout  time.Duration // WebSocket read timeout
	WriteTimeout time.Duration // WebSocket write timeout
	MaxRetries   int           // Maximum reconnection attempts (default 3)
	RetryDelay   time.Duration // Base delay between retries (default 5s, uses exponential backoff)
}

// RelayStatus is an alias to relay.Status for exec relay.
type RelayStatus = relay.Status

// Relay status constants using the relay package values.
const (
	RelayStatusIdle         = relay.StatusIdle
	RelayStatusConnecting   = relay.StatusConnecting
	RelayStatusConnected    = relay.StatusConnected
	RelayStatusReconnecting = relay.StatusReconnecting
	RelayStatusClosed       = relay.StatusClosed
)

// ExecRelay manages bidirectional streaming between a WebSocket client and kubectl exec.
type ExecRelay struct {
	config     RelayConfig
	restConfig *rest.Config

	mu         sync.RWMutex
	session    *Session
	status     RelayStatus
	lastError  error
	buffer     *OutputBuffer
	observers  []chan relay.StatusEvent
	activityCh chan struct{}
	retryCount int // Current reconnection attempt count

	// Output broadcasting - single reader from session, multiple writers to clients
	outputCh  chan []byte  // Channel for session output
	clientsMu sync.RWMutex // Protects clients map
	clients   map[*websocket.Conn]struct{}

	// Flow control - tracks which clients have requested pause
	pausedClients map[*websocket.Conn]bool

	// Context for graceful shutdown
	cancelFunc context.CancelFunc
}

// NewExecRelay creates a new relay for kubectl exec sessions.
func NewExecRelay(restConfig *rest.Config, cfg RelayConfig) *ExecRelay {
	if cfg.BufferSize <= 0 {
		cfg.BufferSize = DefaultBufferSize
	}
	if cfg.ReadTimeout == 0 {
		cfg.ReadTimeout = 60 * time.Second
	}
	if cfg.WriteTimeout == 0 {
		cfg.WriteTimeout = 10 * time.Second
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = DefaultMaxRetries
	}
	if cfg.RetryDelay <= 0 {
		cfg.RetryDelay = DefaultRetryDelay
	}

	return &ExecRelay{
		config:        cfg,
		restConfig:    restConfig,
		status:        RelayStatusIdle,
		buffer:        NewOutputBuffer(cfg.BufferSize),
		activityCh:    make(chan struct{}, 1),
		outputCh:      make(chan []byte, 64), // Buffered channel for output broadcasting
		clients:       make(map[*websocket.Conn]struct{}),
		pausedClients: make(map[*websocket.Conn]bool),
	}
}

// Status returns the current relay status.
func (r *ExecRelay) Status() RelayStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.status
}

// LastError returns the most recent error.
func (r *ExecRelay) LastError() error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.lastError
}

func (r *ExecRelay) setStatus(status RelayStatus, err error) {
	r.mu.Lock()
	oldStatus := r.status
	r.status = status
	r.lastError = err
	observers := append([]chan relay.StatusEvent(nil), r.observers...)
	r.mu.Unlock()

	logFields := log.Fields{
		"namespace":  r.config.Namespace,
		"pod":        r.config.PodName,
		"old_status": oldStatus,
		"new_status": status,
	}
	if err != nil {
		logFields["error"] = err.Error()
		log.WithFields(logFields).Warn("gateway/exec: status transition with error")
	} else {
		log.WithFields(logFields).Debug("gateway/exec: status transition")
	}

	evt := relay.StatusEvent{Status: status, Err: err, When: time.Now()}
	for _, ch := range observers {
		select {
		case ch <- evt:
		default:
		}
	}
}

// reconnect attempts to re-establish the exec session after a failure.
// Uses exponential backoff with the configured MaxRetries and RetryDelay.
// Returns nil on success, error if max retries exceeded or relay was closed.
func (r *ExecRelay) reconnect(ctx context.Context) error {
	r.mu.Lock()
	// Check if relay was closed
	if r.status == RelayStatusClosed {
		r.mu.Unlock()
		return fmt.Errorf("relay is closed")
	}

	// Check if we've exceeded max retries
	if r.retryCount >= r.config.MaxRetries {
		r.mu.Unlock()
		err := fmt.Errorf("max reconnection attempts (%d) exceeded", r.config.MaxRetries)
		r.setStatus(RelayStatusClosed, err)
		return err
	}

	r.retryCount++
	attempt := r.retryCount

	// Close existing session if any
	oldSession := r.session
	r.session = nil
	r.mu.Unlock()

	if oldSession != nil {
		oldSession.Close()
	}

	r.setStatus(RelayStatusReconnecting, nil)

	// Calculate exponential backoff delay
	delay := r.config.RetryDelay * time.Duration(1<<(attempt-1))
	if delay > 30*time.Second {
		delay = 30 * time.Second // Cap at 30s
	}

	log.WithFields(log.Fields{
		"namespace": r.config.Namespace,
		"pod":       r.config.PodName,
		"attempt":   attempt,
		"max":       r.config.MaxRetries,
		"delay":     delay,
	}).Info("gateway/exec: attempting reconnection")

	// Wait before reconnecting
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(delay):
	}

	// Check again if relay was closed during wait
	r.mu.RLock()
	if r.status == RelayStatusClosed {
		r.mu.RUnlock()
		return fmt.Errorf("relay closed during reconnection wait")
	}
	r.mu.RUnlock()

	// Guard against nil rest config
	if r.restConfig == nil {
		return fmt.Errorf("cannot reconnect: no Kubernetes client configuration")
	}

	// Attempt to create new session
	session, err := NewSession(r.restConfig, SessionConfig{
		Namespace: r.config.Namespace,
		PodName:   r.config.PodName,
		Container: r.config.Container,
		Command:   r.config.Command,
	})
	if err != nil {
		log.WithFields(log.Fields{
			"namespace": r.config.Namespace,
			"pod":       r.config.PodName,
			"attempt":   attempt,
			"error":     err.Error(),
		}).Warn("gateway/exec: reconnection failed to create session")
		return err
	}

	if err := session.Start(ctx); err != nil {
		log.WithFields(log.Fields{
			"namespace": r.config.Namespace,
			"pod":       r.config.PodName,
			"attempt":   attempt,
			"error":     err.Error(),
		}).Warn("gateway/exec: reconnection failed to start session")
		return err
	}

	// Success - update state
	r.mu.Lock()
	// Final check if relay was closed
	if r.status == RelayStatusClosed {
		r.mu.Unlock()
		session.Close()
		return fmt.Errorf("relay closed during reconnection")
	}
	r.session = session
	r.retryCount = 0 // Reset on success
	r.mu.Unlock()

	r.setStatus(RelayStatusConnected, nil)

	log.WithFields(log.Fields{
		"namespace": r.config.Namespace,
		"pod":       r.config.PodName,
		"attempt":   attempt,
	}).Info("gateway/exec: reconnection successful")

	return nil
}

// Subscribe returns a channel for status updates.
func (r *ExecRelay) Subscribe() <-chan relay.StatusEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	ch := make(chan relay.StatusEvent, 4)
	r.observers = append(r.observers, ch)
	return ch
}

// ActivityChan returns a channel that signals data activity.
func (r *ExecRelay) ActivityChan() <-chan struct{} {
	return r.activityCh
}

// ReplayBuffer returns buffered output for new clients.
func (r *ExecRelay) ReplayBuffer() []byte {
	return r.buffer.Bytes()
}

// Proxy connects a WebSocket client to the kubectl exec session.
// Multiple clients can connect simultaneously - output is broadcast to all.
// The exec session persists even if all clients disconnect (until Close() is called).
func (r *ExecRelay) Proxy(ctx context.Context, upstream *websocket.Conn) error {
	log.WithFields(log.Fields{
		"namespace": r.config.Namespace,
		"pod":       r.config.PodName,
	}).Debug("gateway/exec: starting proxy")

	// Ensure exec session is started
	if err := r.ensureSession(ctx); err != nil {
		return err
	}

	// Register this client for output broadcasting
	r.clientsMu.Lock()
	r.clients[upstream] = struct{}{}
	clientCount := len(r.clients)
	r.clientsMu.Unlock()

	log.WithFields(log.Fields{
		"namespace":    r.config.Namespace,
		"pod":          r.config.PodName,
		"client_count": clientCount,
	}).Debug("gateway/exec: client registered")

	// Ensure we unregister on exit
	defer func() {
		r.clientsMu.Lock()
		delete(r.clients, upstream)
		delete(r.pausedClients, upstream) // Clean up pause state
		remainingClients := len(r.clients)
		r.clientsMu.Unlock()
		log.WithFields(log.Fields{
			"namespace":         r.config.Namespace,
			"pod":               r.config.PodName,
			"remaining_clients": remainingClients,
		}).Debug("gateway/exec: client unregistered")
	}()

	// Send buffered output to new client (replay)
	if buf := r.buffer.Bytes(); len(buf) > 0 {
		log.WithFields(log.Fields{
			"namespace":   r.config.Namespace,
			"pod":         r.config.PodName,
			"buffer_size": len(buf),
		}).Debug("gateway/exec: replaying buffered output")

		if err := upstream.SetWriteDeadline(time.Now().Add(r.config.WriteTimeout)); err != nil {
			return fmt.Errorf("set write deadline: %w", err)
		}
		if err := upstream.WriteMessage(websocket.BinaryMessage, buf); err != nil {
			return fmt.Errorf("replay buffer: %w", err)
		}
	}

	// Handle input from this client (output is handled by broadcaster)
	return r.handleClientInput(ctx, upstream)
}

func (r *ExecRelay) ensureSession(ctx context.Context) error {
	r.mu.Lock()
	if r.session != nil && r.session.IsRunning() {
		r.mu.Unlock()
		return nil
	}

	// Check if relay was closed
	if r.status == RelayStatusClosed {
		r.mu.Unlock()
		return fmt.Errorf("relay is closed")
	}
	// Release lock BEFORE calling setStatus to avoid deadlock
	// (setStatus also acquires r.mu.Lock, and Go mutexes are not re-entrant)
	r.mu.Unlock()

	// Guard against nil rest config
	if r.restConfig == nil {
		err := fmt.Errorf("cannot create session: no Kubernetes client configuration")
		r.setStatus(RelayStatusClosed, err)
		return err
	}

	r.setStatus(RelayStatusConnecting, nil)

	session, err := NewSession(r.restConfig, SessionConfig{
		Namespace: r.config.Namespace,
		PodName:   r.config.PodName,
		Container: r.config.Container,
		Command:   r.config.Command,
	})
	if err != nil {
		r.setStatus(RelayStatusClosed, err)
		return fmt.Errorf("create session: %w", err)
	}

	if err := session.Start(ctx); err != nil {
		r.setStatus(RelayStatusClosed, err)
		return fmt.Errorf("start session: %w", err)
	}

	// Create cancellable context for background goroutines
	sessionCtx, cancel := context.WithCancel(context.Background())

	// Re-acquire lock to store session safely
	r.mu.Lock()
	// Check if relay was closed while we were connecting
	if r.status == RelayStatusClosed {
		r.mu.Unlock()
		cancel()
		session.Close()
		return fmt.Errorf("relay closed during session creation")
	}
	r.cancelFunc = cancel
	r.session = session
	r.mu.Unlock()

	r.setStatus(RelayStatusConnected, nil)

	// Start output reader goroutine (single reader from session)
	go r.readOutput(sessionCtx)

	// Start broadcaster goroutine (sends output to all clients)
	go r.broadcastOutput(sessionCtx)

	return nil
}

func (r *ExecRelay) readOutput(ctx context.Context) {
	// Recover from panics to prevent crashing the gateway
	defer func() {
		if p := recover(); p != nil {
			log.WithFields(log.Fields{
				"namespace": r.config.Namespace,
				"pod":       r.config.PodName,
				"panic":     p,
			}).Error("gateway/exec: readOutput recovered from panic")
		}
	}()

	buf := make([]byte, 32*1024)
	for {
		select {
		case <-ctx.Done():
			log.WithFields(log.Fields{
				"namespace": r.config.Namespace,
				"pod":       r.config.PodName,
			}).Debug("gateway/exec: readOutput exiting due to context cancellation")
			return
		default:
		}

		// Check if relay was closed
		r.mu.RLock()
		status := r.status
		session := r.session
		r.mu.RUnlock()

		if status == RelayStatusClosed {
			log.WithFields(log.Fields{
				"namespace": r.config.Namespace,
				"pod":       r.config.PodName,
			}).Debug("gateway/exec: readOutput exiting - relay closed")
			return
		}

		if session == nil {
			// Session is nil - attempt reconnection if relay not closed
			if r.Status() == RelayStatusClosed {
				return
			}
			if err := r.reconnect(ctx); err != nil {
				log.WithFields(log.Fields{
					"namespace": r.config.Namespace,
					"pod":       r.config.PodName,
					"error":     err.Error(),
				}).Warn("gateway/exec: readOutput reconnection failed, exiting")
				return
			}
			continue // Retry with new session
		}

		n, err := session.Read(buf)
		if err != nil {
			if err != io.EOF {
				log.WithFields(log.Fields{
					"namespace": r.config.Namespace,
					"pod":       r.config.PodName,
					"error":     err.Error(),
				}).Debug("gateway/exec: output read error")
			}

			// Check if we should attempt reconnection
			if r.Status() == RelayStatusClosed {
				return
			}

			// Attempt reconnection on session failure
			if reconnectErr := r.reconnect(ctx); reconnectErr != nil {
				log.WithFields(log.Fields{
					"namespace":       r.config.Namespace,
					"pod":             r.config.PodName,
					"original_error":  err.Error(),
					"reconnect_error": reconnectErr.Error(),
				}).Warn("gateway/exec: readOutput reconnection failed, exiting")
				return
			}
			continue // Retry with new session
		}

		if n > 0 {
			// Make a copy for broadcasting
			data := make([]byte, n)
			copy(data, buf[:n])

			// Buffer output for replay
			r.buffer.Write(data)

			// Broadcast to all connected clients via channel
			select {
			case r.outputCh <- data:
			case <-ctx.Done():
				return
			default:
				// Channel full, drop oldest and retry
				select {
				case <-r.outputCh:
				default:
				}
				select {
				case r.outputCh <- data:
				case <-ctx.Done():
					return
				default:
				}
			}

			// Signal activity
			select {
			case r.activityCh <- struct{}{}:
			default:
			}
		}
	}
}

// broadcastOutput reads from outputCh and sends to all connected clients.
func (r *ExecRelay) broadcastOutput(ctx context.Context) {
	// Recover from panics to prevent crashing the gateway
	defer func() {
		if p := recover(); p != nil {
			log.WithFields(log.Fields{
				"namespace": r.config.Namespace,
				"pod":       r.config.PodName,
				"panic":     p,
			}).Error("gateway/exec: broadcastOutput recovered from panic")
		}
	}()

	for {
		// Check if relay was closed before waiting on channels
		r.mu.RLock()
		status := r.status
		r.mu.RUnlock()
		if status == RelayStatusClosed {
			log.WithFields(log.Fields{
				"namespace": r.config.Namespace,
				"pod":       r.config.PodName,
			}).Debug("gateway/exec: broadcastOutput exiting - relay closed")
			return
		}

		select {
		case <-ctx.Done():
			log.WithFields(log.Fields{
				"namespace": r.config.Namespace,
				"pod":       r.config.PodName,
			}).Debug("gateway/exec: broadcastOutput exiting due to context cancellation")
			return
		case data, ok := <-r.outputCh:
			if !ok {
				return
			}
			r.clientsMu.RLock()
			clients := make([]*websocket.Conn, 0, len(r.clients))
			pausedClients := make(map[*websocket.Conn]bool, len(r.pausedClients))
			for c := range r.clients {
				clients = append(clients, c)
			}
			for c := range r.pausedClients {
				pausedClients[c] = true
			}
			r.clientsMu.RUnlock()

			for _, conn := range clients {
				// Skip paused clients (flow control)
				if pausedClients[conn] {
					continue
				}
				if err := conn.SetWriteDeadline(time.Now().Add(r.config.WriteTimeout)); err != nil {
					continue
				}
				if err := conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
					log.WithFields(log.Fields{
						"namespace": r.config.Namespace,
						"pod":       r.config.PodName,
						"error":     err.Error(),
					}).Debug("gateway/exec: failed to send to client, will be cleaned up")
				}
			}
		}
	}
}

// handleClientInput handles input from a single WebSocket client.
// Output is handled separately by the broadcaster goroutine.
func (r *ExecRelay) handleClientInput(ctx context.Context, upstream *websocket.Conn) error {
	// Recover from panics to prevent crashing the gateway
	defer func() {
		if p := recover(); p != nil {
			log.WithFields(log.Fields{
				"namespace": r.config.Namespace,
				"pod":       r.config.PodName,
				"panic":     p,
			}).Error("gateway/exec: handleClientInput recovered from panic")
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Check if relay was closed or session ended
		r.mu.RLock()
		status := r.status
		session := r.session
		r.mu.RUnlock()

		if status == RelayStatusClosed {
			log.WithFields(log.Fields{
				"namespace": r.config.Namespace,
				"pod":       r.config.PodName,
			}).Debug("gateway/exec: handleClientInput exiting - relay closed")
			return fmt.Errorf("relay closed")
		}

		if session == nil || !session.IsRunning() {
			return fmt.Errorf("exec session ended")
		}

		if err := upstream.SetReadDeadline(time.Now().Add(r.config.ReadTimeout)); err != nil {
			return err
		}

		msgType, data, err := upstream.ReadMessage()
		if err != nil {
			return err
		}

		// Handle text messages as control commands
		if msgType == websocket.TextMessage {
			var msg controlMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				log.WithFields(log.Fields{
					"error": err.Error(),
				}).Debug("gateway/exec: invalid control message")
				continue
			}

			switch msg.Type {
			case "resize":
				if msg.Cols > 0 && msg.Rows > 0 {
					if session != nil {
						session.Resize(msg.Cols, msg.Rows)
					}
				}
			case "ping":
				// Respond with pong
				pong := []byte(`{"type":"pong"}`)
				upstream.SetWriteDeadline(time.Now().Add(r.config.WriteTimeout))
				upstream.WriteMessage(websocket.TextMessage, pong)
			case "pause":
				// Client requests pause - stop sending output to this client
				r.clientsMu.Lock()
				r.pausedClients[upstream] = true
				r.clientsMu.Unlock()
				log.WithFields(log.Fields{
					"namespace": r.config.Namespace,
					"pod":       r.config.PodName,
				}).Debug("gateway/exec: client requested pause (flow control)")
			case "resume":
				// Client requests resume - continue sending output to this client
				r.clientsMu.Lock()
				delete(r.pausedClients, upstream)
				r.clientsMu.Unlock()
				log.WithFields(log.Fields{
					"namespace": r.config.Namespace,
					"pod":       r.config.PodName,
				}).Debug("gateway/exec: client requested resume (flow control)")
			}
			continue
		}

		// Binary messages are stdin data
		if msgType == websocket.BinaryMessage && len(data) > 0 {
			if session != nil {
				if _, err := session.Write(data); err != nil {
					return fmt.Errorf("write to session: %w", err)
				}
			}

			// Signal activity
			select {
			case r.activityCh <- struct{}{}:
			default:
			}
		}
	}
}

// Control message types from client
type controlMessage struct {
	Type string `json:"type"`
	Cols uint16 `json:"cols,omitempty"`
	Rows uint16 `json:"rows,omitempty"`
}

// Close terminates the relay and underlying exec session.
func (r *ExecRelay) Close() error {
	r.mu.Lock()
	// Check if already closed
	if r.status == RelayStatusClosed {
		r.mu.Unlock()
		return nil
	}

	session := r.session
	r.session = nil
	cancelFunc := r.cancelFunc
	r.cancelFunc = nil
	observers := r.observers
	r.observers = nil
	r.status = RelayStatusClosed
	r.mu.Unlock()

	log.WithFields(log.Fields{
		"namespace": r.config.Namespace,
		"pod":       r.config.PodName,
	}).Info("gateway/exec: closing relay")

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

	// Close underlying session
	if session != nil {
		session.Close()
	}

	// Close observer channels
	for _, ch := range observers {
		close(ch)
	}

	return nil
}
