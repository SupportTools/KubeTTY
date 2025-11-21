package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"

	"github.com/supporttools/KubeTTY/server/internal/config"
	apierrors "github.com/supporttools/KubeTTY/server/internal/shared/errors"
	"github.com/supporttools/KubeTTY/server/internal/shared/health"
	sharedserver "github.com/supporttools/KubeTTY/server/internal/shared/server"
)

//go:embed ui/dist ui/dist/*
var embeddedUI embed.FS

// PTY input validation limits
const (
	maxPTYCols = 500
	maxPTYRows = 200
)

type ptySession struct {
	cmd       *exec.Cmd
	ptmx      *os.File
	createdAt time.Time

	mu            sync.RWMutex
	clients       map[*websocket.Conn]bool
	outputBuffer  []byte // Buffer for initial output (MOTD, etc.)
	bufferMaxSize int    // Maximum buffer size (64KB)
}

type server struct {
	cfg *config.ProjectConfig

	// PTY session management
	mu  sync.RWMutex
	pty *ptySession

	// WebSocket upgrader
	upgrader websocket.Upgrader

	// Metrics
	appMetrics *appMetrics
}

func main() {
	log.SetFormatter(&log.TextFormatter{
		FullTimestamp: true,
	})

	cfg, err := config.LoadProjectConfig()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize metrics
	appMetrics := newAppMetrics()

	srv := &server{
		cfg: &cfg,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
		appMetrics: appMetrics,
	}

	// Setup HTTP routes
	mux := http.NewServeMux()

	// Public endpoints
	mux.Handle("/metrics", promhttp.Handler())

	// Health check with PTY component status (no database)
	ptyChecker := health.NewPTYChecker(&srv.mu, func() bool {
		return srv.pty != nil && srv.pty.isAlive()
	})
	mux.Handle("/api/healthz", health.NewCompatHandler(nil, ptyChecker))

	// Project endpoints - PTY WebSocket only (no session logs without DB)
	mux.HandleFunc("/ws", srv.handleWebsocket)

	// Static files
	mux.HandleFunc("/", srv.staticHandler)

	httpServer := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: mux,
	}

	// Graceful shutdown with custom cleanup handlers
	go func() {
		sharedserver.GracefulShutdown(httpServer, srv)
		cancel()
	}()

	log.Infof("Project listening on :%s (stateless PTY mode - no database)", cfg.Port)
	if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("HTTP server error: %v", err)
	}
}

// PTY WebSocket handler - project-specific (not gateway)
func (s *server) handleWebsocket(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	remoteAddr := r.RemoteAddr
	connID := fmt.Sprintf("%s-%d", remoteAddr, time.Now().UnixNano())

	log.Printf("[WS %s] Connection attempt from %s", connID, remoteAddr)

	// Ensure PTY exists
	if err := s.initPTY(ctx); err != nil {
		log.WithError(err).WithField("conn_id", connID).Error("PTY initialization failed")
		apierrors.WriteError(w, apierrors.InternalServerError("PTY unavailable", ""))
		return
	}

	// Get PTY reference and check for existing client (single-client enforcement)
	s.mu.RLock()
	ps := s.pty
	s.mu.RUnlock()

	if ps == nil {
		apierrors.WriteError(w, apierrors.InternalServerError("PTY not initialized", ""))
		return
	}

	// Enforce single client per session
	if ps.hasClients() {
		log.Printf("[WS %s] Rejected: another client is already connected to this session", connID)
		apierrors.WriteError(w, apierrors.Conflict("session already attached", "only one client allowed"))
		return
	}

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.WithError(err).WithField("conn_id", connID).Error("WebSocket upgrade failed")
		apierrors.WriteError(w, apierrors.InternalServerError("WebSocket upgrade failed", ""))
		return
	}
	defer func() {
		conn.Close()
		log.Printf("[WS %s] Connection closed", connID)
	}()

	log.Printf("[WS %s] Connection established", connID)

	// Register this client
	ps.addClient(conn)
	defer ps.removeClient(conn)

	if s.appMetrics != nil {
		s.appMetrics.observeSessionAttached()
		defer s.appMetrics.observeSessionDetached()
	}

	// WS -> PTY (input)
	for {
		messageType, data, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				log.Printf("[WS %s] Client closed connection normally", connID)
			} else if errors.Is(err, io.EOF) {
				log.Printf("[WS %s] Connection EOF", connID)
			} else {
				log.Printf("[WS %s] Read error: %v", connID, err)
			}
			return
		}

		if messageType == websocket.TextMessage {
			var msg struct {
				Type string `json:"type"`
				Cols uint16 `json:"cols"`
				Rows uint16 `json:"rows"`
			}
			if err := json.Unmarshal(data, &msg); err != nil {
				log.Printf("[WS %s] Invalid JSON message: %v", connID, err)
				continue
			}
			switch msg.Type {
			case "resize":
				if msg.Cols == 0 || msg.Rows == 0 {
					log.Printf("[WS %s] Invalid resize: cols and rows must be > 0", connID)
					continue
				}
				if msg.Cols > maxPTYCols {
					log.Printf("[WS %s] Invalid resize: cols %d exceeds max %d", connID, msg.Cols, maxPTYCols)
					continue
				}
				if msg.Rows > maxPTYRows {
					log.Printf("[WS %s] Invalid resize: rows %d exceeds max %d", connID, msg.Rows, maxPTYRows)
					continue
				}
				if err := pty.Setsize(ps.ptmx, &pty.Winsize{Cols: msg.Cols, Rows: msg.Rows}); err != nil {
					log.Printf("[WS %s] PTY resize error: %v", connID, err)
				}
			case "ping":
				// Send pong response to keep connection alive
				if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"pong"}`)); err != nil {
					log.Printf("[WS %s] Pong write error: %v", connID, err)
				}
			}
			continue
		}

		// Binary input
		if s.appMetrics != nil {
			s.appMetrics.observeWSBytes("in", len(data))
		}
		if _, err := ps.ptmx.Write(data); err != nil {
			log.Printf("pty write error: %v", err)
			return
		}
	}
}

// Shutdown implements the ShutdownHandler interface for graceful shutdown.
func (s *server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	pty := s.pty
	s.mu.Unlock()

	if pty != nil {
		pty.broadcastClose()
		if pty.cmd != nil && pty.cmd.Process != nil {
			_ = pty.cmd.Process.Signal(syscall.SIGTERM)
			time.Sleep(100 * time.Millisecond)
			_ = pty.cmd.Process.Kill()
		}
		if pty.ptmx != nil {
			_ = pty.ptmx.Close()
		}
	}
	return nil
}

func (ps *ptySession) isAlive() bool {
	if ps == nil || ps.cmd == nil || ps.cmd.Process == nil {
		return false
	}
	return ps.cmd.ProcessState == nil || !ps.cmd.ProcessState.Exited()
}

func (s *server) initPTY(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.pty != nil && s.pty.isAlive() {
		return nil // Already initialized
	}

	// Start bash as a login shell to source .bash_profile and display MOTD
	cmd := exec.Command(s.cfg.Shell, "-l")
	cmd.Env = append(os.Environ(),
		"KUBETTY_USER="+s.cfg.KubettyUser,
		"KUBETTY_PROJECT="+s.cfg.KubettyProject,
	)
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return fmt.Errorf("start pty: %w", err)
	}

	s.pty = &ptySession{
		cmd:           cmd,
		ptmx:          ptmx,
		createdAt:     time.Now(),
		clients:       make(map[*websocket.Conn]bool),
		outputBuffer:  make([]byte, 0, 65536), // Pre-allocate 64KB capacity
		bufferMaxSize: 65536,                  // 64KB max buffer size
	}

	// Start PTY reader (broadcast to all clients)
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if err != nil {
				log.Printf("PTY read error: %v", err)
				break
			}
			if n > 0 {
				data := make([]byte, n)
				copy(data, buf[:n])

				// Store in output buffer for replay to new clients (limited to bufferMaxSize)
				s.pty.mu.Lock()
				if len(s.pty.outputBuffer) < s.pty.bufferMaxSize {
					remainingSpace := s.pty.bufferMaxSize - len(s.pty.outputBuffer)
					if n <= remainingSpace {
						s.pty.outputBuffer = append(s.pty.outputBuffer, data...)
					} else {
						// If data would exceed buffer, only append what fits
						s.pty.outputBuffer = append(s.pty.outputBuffer, data[:remainingSpace]...)
					}
				}
				s.pty.mu.Unlock()

				s.pty.broadcast(data)

				if s.appMetrics != nil {
					s.appMetrics.observeWSBytes("out", n)
				}
			}
		}
	}()

	// Monitor PTY exit
	go func() {
		err := cmd.Wait()
		log.Printf("PTY exited: %v", err)
		exitCode := 0
		if err != nil {
			exitCode = 1
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			}
		}
		if s.appMetrics != nil {
			s.appMetrics.observePtyExit(exitCode)
		}
		s.mu.Lock()
		if s.pty != nil {
			s.pty.broadcastClose()
		}
		s.pty = nil
		s.mu.Unlock()
	}()

	return nil
}

func (ps *ptySession) addClient(conn *websocket.Conn) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	// Replay buffered output to the new client (MOTD, etc.)
	if len(ps.outputBuffer) > 0 {
		if err := conn.WriteMessage(websocket.BinaryMessage, ps.outputBuffer); err != nil {
			log.Printf("warn: failed to replay buffer to new client: %v", err)
		}
	}

	ps.clients[conn] = true
}

func (ps *ptySession) removeClient(conn *websocket.Conn) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	delete(ps.clients, conn)
}

func (ps *ptySession) hasClients() bool {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	return len(ps.clients) > 0
}

func (ps *ptySession) broadcast(data []byte) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	for conn := range ps.clients {
		if err := conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
			log.Printf("broadcast error: %v", err)
		}
	}
}

func (ps *ptySession) broadcastClose() {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	for conn := range ps.clients {
		_ = conn.WriteControl(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, "PTY exited"),
			time.Now().Add(time.Second))
		conn.Close()
	}
}

func (s *server) staticHandler(w http.ResponseWriter, r *http.Request) {
	// Placeholder - will be implemented
	http.NotFound(w, r)
}
