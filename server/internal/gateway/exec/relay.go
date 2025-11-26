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

// RelayConfig configures an ExecRelay.
type RelayConfig struct {
	Namespace    string
	PodName      string
	Container    string
	Command      []string
	BufferSize   int           // Output buffer size (default 64KB)
	ReadTimeout  time.Duration // WebSocket read timeout
	WriteTimeout time.Duration // WebSocket write timeout
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

	// Output broadcasting - single reader from session, multiple writers to clients
	outputCh  chan []byte  // Channel for session output
	clientsMu sync.RWMutex // Protects clients map
	clients   map[*websocket.Conn]struct{}
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

	return &ExecRelay{
		config:     cfg,
		restConfig: restConfig,
		status:     RelayStatusIdle,
		buffer:     NewOutputBuffer(cfg.BufferSize),
		activityCh: make(chan struct{}, 1),
		outputCh:   make(chan []byte, 64), // Buffered channel for output broadcasting
		clients:    make(map[*websocket.Conn]struct{}),
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

	r.setStatus(RelayStatusConnecting, nil)

	session, err := NewSession(r.restConfig, SessionConfig{
		Namespace: r.config.Namespace,
		PodName:   r.config.PodName,
		Container: r.config.Container,
		Command:   r.config.Command,
	})
	if err != nil {
		r.mu.Unlock()
		r.setStatus(RelayStatusClosed, err)
		return fmt.Errorf("create session: %w", err)
	}

	if err := session.Start(ctx); err != nil {
		r.mu.Unlock()
		r.setStatus(RelayStatusClosed, err)
		return fmt.Errorf("start session: %w", err)
	}

	r.session = session
	r.mu.Unlock()
	r.setStatus(RelayStatusConnected, nil)

	// Start output reader goroutine (single reader from session)
	go r.readOutput(ctx)

	// Start broadcaster goroutine (sends output to all clients)
	go r.broadcastOutput(ctx)

	return nil
}

func (r *ExecRelay) readOutput(ctx context.Context) {
	buf := make([]byte, 32*1024)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		r.mu.RLock()
		session := r.session
		r.mu.RUnlock()

		if session == nil {
			return
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
			return
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
			default:
				// Channel full, drop oldest and retry
				select {
				case <-r.outputCh:
				default:
				}
				select {
				case r.outputCh <- data:
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
	for {
		select {
		case <-ctx.Done():
			return
		case data, ok := <-r.outputCh:
			if !ok {
				return
			}
			r.clientsMu.RLock()
			clients := make([]*websocket.Conn, 0, len(r.clients))
			for c := range r.clients {
				clients = append(clients, c)
			}
			r.clientsMu.RUnlock()

			for _, conn := range clients {
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
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Check if session is still running
		r.mu.RLock()
		session := r.session
		r.mu.RUnlock()
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
	session := r.session
	r.session = nil
	observers := r.observers
	r.observers = nil
	r.mu.Unlock()

	r.setStatus(RelayStatusClosed, nil)

	if session != nil {
		session.Close()
	}

	// Close observer channels
	for _, ch := range observers {
		close(ch)
	}

	return nil
}
