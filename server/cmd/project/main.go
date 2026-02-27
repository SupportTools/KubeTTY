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
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"

	"github.com/supporttools/KubeTTY/server/internal/config"
	"github.com/supporttools/KubeTTY/server/internal/shared/buffer"
	apierrors "github.com/supporttools/KubeTTY/server/internal/shared/errors"
	"github.com/supporttools/KubeTTY/server/internal/shared/filelogger"
	"github.com/supporttools/KubeTTY/server/internal/shared/health"
	"github.com/supporttools/KubeTTY/server/internal/shared/ptylogger"
	sharedserver "github.com/supporttools/KubeTTY/server/internal/shared/server"
	"github.com/supporttools/KubeTTY/server/pkg/logging"
)

//go:embed ui/dist ui/dist/*
var embeddedUI embed.FS

// Build-time variables set via -ldflags
var (
	version   = "dev"
	gitCommit = "unknown"
	buildTime = "unknown"
)

// PTY input validation limits
const (
	maxPTYCols = 500
	maxPTYRows = 200
)

// WebSocket heartbeat configuration
const (
	// How often the server sends ping frames to clients
	wsPingInterval = 15 * time.Second
	// How long to wait for pong response before considering connection dead
	wsPongTimeout = 5 * time.Second
	// Read deadline extension on each pong (pingInterval + pongTimeout + buffer)
	wsReadDeadline = wsPingInterval + wsPongTimeout + 2*time.Second
)

// Flow control constants
const (
	// Maximum buffer size for paused clients (64KB)
	pauseBufferMaxSize = 64 * 1024
	// Chunk size for flushing pause buffer (8KB) - prevents overwhelming client
	pauseBufferChunkSize = 8 * 1024
	// Delay between chunks when flushing (allows xterm.js to process)
	pauseBufferChunkDelay = 5 * time.Millisecond
)

// Replay buffer constants (for new client connection)
const (
	// Chunk size for replaying buffered output (64KB)
	replayChunkSize = 64 * 1024
	// Delay between replay chunks (prevents browser overwhelm)
	replayChunkDelay = 5 * time.Millisecond
)

// wsClient wraps a websocket connection with a write mutex to prevent concurrent writes.
// The gorilla/websocket library does not support concurrent writers.
type wsClient struct {
	conn    *websocket.Conn
	writeMu sync.Mutex
	paused  atomic.Bool // Flow control: true when client cannot accept more data

	// Pause buffer - stores data while client is paused for delivery on resume
	pauseBufferMu sync.Mutex
	pauseBuffer   []byte

	// Heartbeat tracking
	lastPongAt atomic.Int64 // Unix timestamp of last pong received
	stopPing   chan struct{}
}

// writeMessage safely writes a message to the websocket with mutex protection.
func (c *wsClient) writeMessage(messageType int, data []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.conn.WriteMessage(messageType, data)
}

// bufferPausedData adds data to the pause buffer (for delivery on resume).
// Returns true if data was buffered, false if buffer is full.
func (c *wsClient) bufferPausedData(data []byte) bool {
	c.pauseBufferMu.Lock()
	defer c.pauseBufferMu.Unlock()

	currentLen := len(c.pauseBuffer)
	if currentLen >= pauseBufferMaxSize {
		return false // Buffer full
	}

	// Only append what fits
	remainingSpace := pauseBufferMaxSize - currentLen
	if len(data) <= remainingSpace {
		c.pauseBuffer = append(c.pauseBuffer, data...)
	} else {
		c.pauseBuffer = append(c.pauseBuffer, data[:remainingSpace]...)
	}
	return true
}

// flushPauseBuffer sends all buffered data in chunks and clears the buffer.
// Uses chunked sending to avoid overwhelming the client's terminal renderer.
// Returns the number of bytes flushed and any error encountered.
func (c *wsClient) flushPauseBuffer() (int, error) {
	c.pauseBufferMu.Lock()
	data := c.pauseBuffer
	c.pauseBuffer = nil
	c.pauseBufferMu.Unlock()

	totalBytes := len(data)
	if totalBytes == 0 {
		return 0, nil
	}

	// Send data in chunks to avoid overwhelming the client
	bytesSent := 0
	for bytesSent < totalBytes {
		end := bytesSent + pauseBufferChunkSize
		if end > totalBytes {
			end = totalBytes
		}

		chunk := data[bytesSent:end]
		if err := c.writeMessage(websocket.BinaryMessage, chunk); err != nil {
			return bytesSent, err
		}

		bytesSent = end

		// Small delay between chunks to let xterm.js process
		if bytesSent < totalBytes {
			time.Sleep(pauseBufferChunkDelay)
		}
	}

	return bytesSent, nil
}

// pauseBufferLen returns the current pause buffer size.
func (c *wsClient) pauseBufferLen() int {
	c.pauseBufferMu.Lock()
	defer c.pauseBufferMu.Unlock()
	return len(c.pauseBuffer)
}

// writeControl safely writes a control message to the websocket with mutex protection.
func (c *wsClient) writeControl(messageType int, data []byte, deadline time.Time) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.conn.WriteControl(messageType, data, deadline)
}

// startHeartbeat starts a goroutine that sends ping frames and monitors for pong responses.
// Returns a channel that will be closed when heartbeat detects a dead connection.
func (c *wsClient) startHeartbeat(sessionUUID, connID string) <-chan struct{} {
	deadConn := make(chan struct{})
	c.stopPing = make(chan struct{})

	// Initialize lastPongAt to now (assume connection is alive)
	c.lastPongAt.Store(time.Now().Unix())

	// Set pong handler to update lastPongAt timestamp
	c.conn.SetPongHandler(func(appData string) error {
		c.lastPongAt.Store(time.Now().Unix())
		log.WithFields(log.Fields{
			"session_uuid": sessionUUID,
			"conn_id":      connID,
		}).Debug("project/ws: Pong received")
		// Extend read deadline on pong
		return c.conn.SetReadDeadline(time.Now().Add(wsReadDeadline))
	})

	// Set initial read deadline
	_ = c.conn.SetReadDeadline(time.Now().Add(wsReadDeadline))

	go func() {
		ticker := time.NewTicker(wsPingInterval)
		defer ticker.Stop()

		for {
			select {
			case <-c.stopPing:
				return
			case <-ticker.C:
				// Check if last pong is too old
				lastPong := time.Unix(c.lastPongAt.Load(), 0)
				sincePong := time.Since(lastPong)

				if sincePong > wsPingInterval+wsPongTimeout {
					log.WithFields(log.Fields{
						"session_uuid": sessionUUID,
						"conn_id":      connID,
						"since_pong":   sincePong.String(),
						"pong_timeout": (wsPingInterval + wsPongTimeout).String(),
					}).Warn("project/ws: Pong timeout - connection appears dead")
					close(deadConn)
					return
				}

				// Send ping frame
				if err := c.writeControl(websocket.PingMessage, []byte{}, time.Now().Add(wsPongTimeout)); err != nil {
					log.WithFields(log.Fields{
						"session_uuid": sessionUUID,
						"conn_id":      connID,
						"error":        err.Error(),
					}).Warn("project/ws: Ping write failed")
					close(deadConn)
					return
				}

				log.WithFields(log.Fields{
					"session_uuid": sessionUUID,
					"conn_id":      connID,
				}).Debug("project/ws: Ping sent")
			}
		}
	}()

	return deadConn
}

// stopHeartbeat stops the heartbeat goroutine.
func (c *wsClient) stopHeartbeat() {
	if c.stopPing != nil {
		close(c.stopPing)
	}
}

type ptySession struct {
	cmd       *exec.Cmd
	ptmx      *os.File
	createdAt time.Time

	mu           sync.RWMutex
	clients      map[*websocket.Conn]*wsClient
	reserved     int                // reserved slots for in-progress WebSocket upgrades
	outputBuffer *buffer.RingBuffer // Ring buffer for PTY output replay (default 8MB)

	// Metrics reference for broadcast error tracking
	appMetrics *appMetrics
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

	// PTY I/O logger for Loki capture
	ptyLogger *ptylogger.Logger

	// File-based PTY logger for Loki sidecar integration
	fileLogger *filelogger.Logger
}

func main() {
	logging.Init()

	cfg, err := config.LoadProjectConfig()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize metrics
	appMetrics := newAppMetrics()

	// Initialize PTY logger for Loki capture (stdout-based)
	ptyLog := ptylogger.New(cfg.SessionID, ptylogger.Options{
		Enabled:    cfg.PTYLogEnabled,
		MaxLineLen: cfg.PTYLogMaxLineLen,
	})
	if cfg.PTYLogEnabled {
		log.WithField("session_id", cfg.SessionID).Info("PTY logging enabled for Loki capture")
	}

	// Initialize file-based PTY logger for Loki sidecar integration
	var fileLog *filelogger.Logger
	if cfg.PTYFileLogEnabled {
		var err error
		fileLog, err = filelogger.New(cfg.SessionID, cfg.KubettyProject, cfg.KubettyUser,
			filelogger.Options{
				FilePath:      cfg.PTYFileLogPath,
				MaxSize:       cfg.PTYFileLogMaxSize,
				MaxBackups:    cfg.PTYFileLogMaxBackups,
				BufferSize:    cfg.PTYFileLogBufferSize,
				FlushInterval: cfg.PTYFileLogFlushInterval,
				IncludeRaw:    cfg.PTYFileLogIncludeRaw,
			})
		if err != nil {
			log.WithError(err).Error("Failed to initialize file logger - continuing without file logging")
		} else {
			log.WithFields(log.Fields{
				"session_id": cfg.SessionID,
				"file_path":  cfg.PTYFileLogPath,
				"max_size":   cfg.PTYFileLogMaxSize,
			}).Info("File-based PTY logging enabled for Loki sidecar")
		}
	}

	srv := &server{
		cfg: &cfg,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
		appMetrics: appMetrics,
		ptyLogger:  ptyLog,
		fileLogger: fileLog,
	}

	// Reap zombie children (critical when running as PID 1 in containers)
	// When kubetty-project runs as PID 1, it inherits responsibility for
	// reaping ALL orphaned child processes spawned by the bash PTY.
	sigchldChan := make(chan os.Signal, 1)
	signal.Notify(sigchldChan, syscall.SIGCHLD)
	go func() {
		for range sigchldChan {
			// Reap all exited children in a loop (multiple may have exited)
			for {
				var wstatus syscall.WaitStatus
				wpid, err := syscall.Wait4(-1, &wstatus, syscall.WNOHANG, nil)
				if wpid <= 0 || err != nil {
					break
				}
				log.WithField("pid", wpid).Debug("Reaped zombie child process")
			}
		}
	}()

	// Setup HTTP routes
	mux := http.NewServeMux()

	// Public endpoints
	mux.Handle("/metrics", promhttp.Handler())

	// Health check with PTY component status (no database)
	ptyChecker := health.NewPTYChecker(&srv.mu, func() bool {
		return srv.pty != nil && srv.pty.isAlive()
	})
	mux.Handle("/api/healthz", health.NewCompatHandler(nil, ptyChecker))

	// Version endpoint - returns the application version
	mux.HandleFunc("/api/version", handleVersion)

	// Project endpoints - PTY WebSocket only (no session logs without DB)
	mux.HandleFunc("/ws", srv.handleWebsocket)

	// Resource metrics endpoint for gateway polling
	mux.HandleFunc("/api/metrics", srv.handleMetrics)

	// GUI status endpoint (returns GUI stack health when GUI_ENABLED=true)
	mux.HandleFunc("/api/gui/status", srv.handleGUIStatus)

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
	sessionUUID := s.cfg.SessionID

	// Record connection attempt metric
	if s.appMetrics != nil {
		s.appMetrics.observeWSConnectionAttempt()
	}

	log.WithFields(log.Fields{
		"session_uuid": sessionUUID,
		"conn_id":      connID,
		"remote_addr":  remoteAddr,
		"user_agent":   r.UserAgent(),
		"path":         r.URL.Path,
	}).Debug("project/ws: WebSocket connection attempt")

	// Ensure PTY exists
	if err := s.initPTY(ctx); err != nil {
		log.WithFields(log.Fields{
			"session_uuid": sessionUUID,
			"conn_id":      connID,
			"error":        err.Error(),
		}).Error("project/ws: PTY initialization failed")
		apierrors.WriteError(w, apierrors.InternalServerError("PTY unavailable", ""))
		return
	}

	// Get PTY reference and check for existing client (single-client enforcement)
	s.mu.RLock()
	ps := s.pty
	s.mu.RUnlock()

	if ps == nil {
		log.WithFields(log.Fields{
			"session_uuid": sessionUUID,
			"conn_id":      connID,
		}).Error("project/ws: PTY not initialized after init")
		apierrors.WriteError(w, apierrors.InternalServerError("PTY not initialized", ""))
		return
	}

	// Enforce single client per session
	// Support ?force=true to disconnect existing client and take over
	forceParam := r.URL.Query().Get("force")
	forceConnect := forceParam == "true" || forceParam == "1"

	if forceConnect {
		if ps.hasClients() {
			// Force takeover: disconnect existing clients with explanation
			log.WithFields(log.Fields{
				"session_uuid": sessionUUID,
				"conn_id":      connID,
				"remote_addr":  remoteAddr,
				"client_count": ps.getClientCount(),
			}).Info("project/ws: Force takeover requested - disconnecting existing client(s)")
			ps.disconnectAllClients("session taken over by another client")
		}
	} else {
		// Atomically check-and-reserve to prevent TOCTOU race between
		// hasClients check and addClient after WebSocket upgrade
		if !ps.reserveSlot() {
			log.WithFields(log.Fields{
				"session_uuid": sessionUUID,
				"conn_id":      connID,
				"remote_addr":  remoteAddr,
				"client_count": ps.getClientCount(),
			}).Warn("project/ws: Rejected - another client is already connected (single-client enforcement)")
			apierrors.WriteError(w, apierrors.Conflict("session already attached", "only one client allowed; use ?force=true to take over"))
			return
		}
		// If WebSocket upgrade fails below, release the reserved slot
		defer func() {
			ps.releaseSlot()
		}()
	}

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.WithFields(log.Fields{
			"session_uuid": sessionUUID,
			"conn_id":      connID,
			"error":        err.Error(),
		}).Error("project/ws: WebSocket upgrade failed")
		apierrors.WriteError(w, apierrors.InternalServerError("WebSocket upgrade failed", ""))
		return
	}

	// Track active connection
	if s.appMetrics != nil {
		s.appMetrics.observeWSConnectionOpened()
	}

	// Track disconnect reason for metrics
	disconnectReason := "unknown"
	connectedAt := time.Now()
	defer func() {
		conn.Close()
		if s.appMetrics != nil {
			s.appMetrics.observeWSConnectionClosed()
			s.appMetrics.observeWSDisconnect(disconnectReason)
		}
		log.WithFields(log.Fields{
			"session_uuid":      sessionUUID,
			"conn_id":           connID,
			"remote_addr":       remoteAddr,
			"disconnect_reason": disconnectReason,
			"connection_dur":    time.Since(connectedAt).String(),
		}).Info("project/ws: Connection closed")
	}()

	log.WithFields(log.Fields{
		"session_uuid": sessionUUID,
		"conn_id":      connID,
		"remote_addr":  remoteAddr,
	}).Info("project/ws: WebSocket connection established")

	// Register this client and get the wsClient wrapper for safe writes
	wsClient := ps.addClient(conn)
	defer ps.removeClient(conn)

	if s.appMetrics != nil {
		s.appMetrics.observeSessionAttached()
		defer s.appMetrics.observeSessionDetached()
	}

	// Start server-initiated heartbeat (ping/pong)
	deadConn := wsClient.startHeartbeat(sessionUUID, connID)
	defer wsClient.stopHeartbeat()

	// Monitor for dead connection detected by heartbeat
	go func() {
		<-deadConn
		disconnectReason = "heartbeat_timeout"
		log.WithFields(log.Fields{
			"session_uuid": sessionUUID,
			"conn_id":      connID,
		}).Info("project/ws: Closing connection due to heartbeat timeout")
		conn.Close()
	}()

	// WS -> PTY (input)
	for {
		messageType, data, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				disconnectReason = "normal_closure"
				log.WithFields(log.Fields{
					"session_uuid": sessionUUID,
					"conn_id":      connID,
					"close_code":   "normal/going_away",
				}).Info("project/ws: Client closed connection normally")
			} else if errors.Is(err, io.EOF) {
				disconnectReason = "eof"
				log.WithFields(log.Fields{
					"session_uuid": sessionUUID,
					"conn_id":      connID,
				}).Info("project/ws: Connection EOF")
			} else {
				disconnectReason = "read_error"
				log.WithFields(log.Fields{
					"session_uuid": sessionUUID,
					"conn_id":      connID,
					"error":        err.Error(),
				}).Warn("project/ws: Read error - disconnecting client")
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
				log.WithFields(log.Fields{
					"session_uuid": sessionUUID,
					"conn_id":      connID,
					"error":        err.Error(),
					"data":         string(data),
				}).Warn("project/ws: Invalid JSON message")
				continue
			}
			switch msg.Type {
			case "resize":
				if msg.Cols == 0 || msg.Rows == 0 {
					log.WithFields(log.Fields{
						"session_uuid": sessionUUID,
						"conn_id":      connID,
						"cols":         msg.Cols,
						"rows":         msg.Rows,
					}).Warn("project/ws: Invalid resize - cols and rows must be > 0")
					continue
				}
				if msg.Cols > maxPTYCols {
					log.WithFields(log.Fields{
						"session_uuid": sessionUUID,
						"conn_id":      connID,
						"cols":         msg.Cols,
						"max":          maxPTYCols,
					}).Warn("project/ws: Invalid resize - cols exceeds max")
					continue
				}
				if msg.Rows > maxPTYRows {
					log.WithFields(log.Fields{
						"session_uuid": sessionUUID,
						"conn_id":      connID,
						"rows":         msg.Rows,
						"max":          maxPTYRows,
					}).Warn("project/ws: Invalid resize - rows exceeds max")
					continue
				}
				if err := pty.Setsize(ps.ptmx, &pty.Winsize{Cols: msg.Cols, Rows: msg.Rows}); err != nil {
					log.WithFields(log.Fields{
						"session_uuid": sessionUUID,
						"conn_id":      connID,
						"cols":         msg.Cols,
						"rows":         msg.Rows,
						"error":        err.Error(),
					}).Error("project/ws: PTY resize failed")
				} else {
					log.WithFields(log.Fields{
						"session_uuid": sessionUUID,
						"conn_id":      connID,
						"cols":         msg.Cols,
						"rows":         msg.Rows,
					}).Debug("project/ws: PTY resized")
				}
			case "ping":
				// Send pong response to keep connection alive (use safe write)
				if err := wsClient.writeMessage(websocket.TextMessage, []byte(`{"type":"pong"}`)); err != nil {
					log.WithFields(log.Fields{
						"session_uuid": sessionUUID,
						"conn_id":      connID,
						"error":        err.Error(),
					}).Warn("project/ws: Pong write error")
				}
				// Note: pong sent is Debug level noise, omitted for cleaner logs
			case "pause":
				// Client is requesting to pause PTY output (flow control)
				wsClient.paused.Store(true)
				if s.appMetrics != nil {
					s.appMetrics.observeWSFlowControlPause()
				}
				log.WithFields(log.Fields{
					"session_uuid": sessionUUID,
					"conn_id":      connID,
				}).Info("project/ws: Client paused (flow control enabled)")
			case "resume":
				// Client is ready to receive PTY output again (flow control)
				wsClient.paused.Store(false)

				// Flush any data buffered while paused (in chunks)
				bufferedBytes := wsClient.pauseBufferLen()
				if bufferedBytes > 0 {
					bytesSent, err := wsClient.flushPauseBuffer()
					if err != nil {
						log.WithFields(log.Fields{
							"session_uuid":   sessionUUID,
							"conn_id":        connID,
							"buffered_bytes": bufferedBytes,
							"bytes_sent":     bytesSent,
							"error":          err.Error(),
						}).Warn("project/ws: Failed to flush pause buffer on resume")
					} else {
						log.WithFields(log.Fields{
							"session_uuid":   sessionUUID,
							"conn_id":        connID,
							"buffered_bytes": bufferedBytes,
							"bytes_sent":     bytesSent,
							"chunks":         (bytesSent + pauseBufferChunkSize - 1) / pauseBufferChunkSize,
						}).Info("project/ws: Flushed pause buffer on resume")
					}
					// Track resume events in metrics
					if s.appMetrics != nil {
						s.appMetrics.observeFlowControlResume(bytesSent)
					}
				}

				log.WithFields(log.Fields{
					"session_uuid": sessionUUID,
					"conn_id":      connID,
				}).Info("project/ws: Client resumed (flow control disabled)")
			default:
				log.WithFields(log.Fields{
					"session_uuid": sessionUUID,
					"conn_id":      connID,
					"message_type": msg.Type,
				}).Debug("project/ws: Received unknown message type")
			}
			continue
		}

		// Binary input
		if s.appMetrics != nil {
			s.appMetrics.observeWSBytes("in", len(data))
		}

		// Log user input for Loki capture
		if s.ptyLogger != nil {
			s.ptyLogger.Write(ptylogger.DirectionIn, data)
		}
		if s.fileLogger != nil {
			s.fileLogger.Write(filelogger.DirectionIn, data)
		}

		if _, err := ps.ptmx.Write(data); err != nil {
			disconnectReason = "pty_write_error"
			log.WithFields(log.Fields{
				"session_uuid": sessionUUID,
				"conn_id":      connID,
				"error":        err.Error(),
			}).Error("project/ws: PTY write error - disconnecting")
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

	// Close file logger (flushes remaining data and stops flush goroutine)
	if s.fileLogger != nil {
		if err := s.fileLogger.Close(); err != nil {
			log.WithError(err).Warn("Failed to close file logger")
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
		log.Debug("project/pty: PTY already initialized and alive")
		return nil // Already initialized
	}

	log.WithFields(log.Fields{
		"shell":   s.cfg.Shell,
		"user":    s.cfg.KubettyUser,
		"project": s.cfg.KubettyProject,
	}).Info("project/pty: Initializing PTY session")

	// Start bash as a login shell to source .bash_profile and display MOTD
	cmd := exec.Command(s.cfg.Shell, "-l")
	cmd.Env = append(os.Environ(),
		"KUBETTY_USER="+s.cfg.KubettyUser,
		"KUBETTY_PROJECT="+s.cfg.KubettyProject,
	)
	ptmx, err := pty.Start(cmd)
	if err != nil {
		log.WithFields(log.Fields{
			"shell": s.cfg.Shell,
			"error": err.Error(),
		}).Error("project/pty: Failed to start PTY")
		return fmt.Errorf("start pty: %w", err)
	}

	s.pty = &ptySession{
		cmd:          cmd,
		ptmx:         ptmx,
		createdAt:    time.Now(),
		clients:      make(map[*websocket.Conn]*wsClient),
		outputBuffer: buffer.NewRingBuffer(s.cfg.OutputBufferSize), // Ring buffer (default 8MB)
		appMetrics:   s.appMetrics,                                 // For broadcast error tracking
	}

	// Set buffer capacity metric
	if s.appMetrics != nil {
		s.appMetrics.setOutputBufferCapacity(s.cfg.OutputBufferSize)
	}

	log.WithFields(log.Fields{
		"pid":         cmd.Process.Pid,
		"buffer_size": s.cfg.OutputBufferSize,
	}).Info("project/pty: PTY session created successfully")

	// Start PTY reader (broadcast to all clients)
	go func() {
		log.Debug("project/pty: PTY reader goroutine started")
		buf := make([]byte, 4096)
		totalBytesRead := 0

		for {
			n, err := ptmx.Read(buf)
			if err != nil {
				log.WithFields(log.Fields{
					"error":      err.Error(),
					"bytes_read": totalBytesRead,
				}).Info("project/pty: PTY reader exiting - read error")
				break
			}
			if n > 0 {
				totalBytesRead += n
				data := make([]byte, n)
				copy(data, buf[:n])

				// Store in ring buffer for replay to new clients
				// Ring buffer is thread-safe and handles wrapping internally
				s.pty.outputBuffer.Write(data)

				// Update buffer metrics
				if s.appMetrics != nil {
					s.appMetrics.observeOutputBufferWrite(n)
					s.appMetrics.setOutputBufferUsage(s.pty.outputBuffer.Len())
				}

				s.pty.broadcast(data)

				// Log PTY output for Loki capture
				if s.ptyLogger != nil {
					s.ptyLogger.Write(ptylogger.DirectionOut, data)
				}
				if s.fileLogger != nil {
					s.fileLogger.Write(filelogger.DirectionOut, data)
				}

				if s.appMetrics != nil {
					s.appMetrics.observeWSBytes("out", n)
				}
			}
		}

		// Flush remaining buffered log data when PTY closes
		if s.ptyLogger != nil {
			s.ptyLogger.Flush()
		}
		if s.fileLogger != nil {
			if err := s.fileLogger.Flush(); err != nil {
				log.WithError(err).Warn("Failed to flush file logger on PTY close")
			}
		}

		log.WithField("total_bytes", totalBytesRead).Debug("project/pty: PTY reader goroutine exited")
	}()

	// Monitor PTY exit
	go func() {
		log.Debug("project/pty: PTY monitor goroutine started")
		err := cmd.Wait()

		exitCode := 0
		if err != nil {
			exitCode = 1
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			}
		}

		log.WithFields(log.Fields{
			"exit_code": exitCode,
			"error":     fmt.Sprintf("%v", err),
			"pid":       cmd.Process.Pid,
			"runtime":   time.Since(s.pty.createdAt).String(),
		}).Info("project/pty: PTY process exited")

		if s.appMetrics != nil {
			s.appMetrics.observePtyExit(exitCode)
		}

		s.mu.Lock()
		if s.pty != nil {
			s.pty.broadcastClose()
			log.Debug("project/pty: Cleaning up PTY session")
		}
		s.pty = nil
		s.mu.Unlock()

		log.Debug("project/pty: PTY monitor goroutine exited")
	}()

	return nil
}

func (ps *ptySession) addClient(conn *websocket.Conn) *wsClient {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	// Consume reserved slot (set by reserveSlot before WebSocket upgrade)
	if ps.reserved > 0 {
		ps.reserved--
	}

	client := &wsClient{conn: conn}

	// Get ring buffer info for metadata
	bufInfo := ps.outputBuffer.Info()

	// Add client to receive real-time data immediately
	ps.clients[conn] = client

	log.WithFields(log.Fields{
		"client_count":  len(ps.clients),
		"pty_age":       time.Since(ps.createdAt).String(),
		"total_written": bufInfo.TotalWritten,
	}).Info("project/pty: Client added to PTY session")

	// Replay buffered output in chunks (prevents browser overwhelm)
	// Done in goroutine after adding client so real-time data flows immediately
	bufferedData := ps.outputBuffer.Bytes()
	bufferSize := len(bufferedData)
	if bufferSize > 0 {
		go ps.replayBufferChunked(client, bufferedData, bufInfo.TotalWritten)
	}

	return client
}

// replayBufferChunked sends buffered output to a client in chunks with delays.
// This prevents overwhelming the browser's rendering loop with large buffers.
func (ps *ptySession) replayBufferChunked(client *wsClient, data []byte, totalWritten int64) {
	bufferSize := len(data)
	chunkCount := (bufferSize + replayChunkSize - 1) / replayChunkSize

	log.WithFields(log.Fields{
		"buffer_size":   bufferSize,
		"chunk_size":    replayChunkSize,
		"chunk_count":   chunkCount,
		"total_written": totalWritten,
	}).Debug("project/pty: Starting chunked buffer replay")

	bytesSent := 0
	for i := 0; i < bufferSize; i += replayChunkSize {
		end := i + replayChunkSize
		if end > bufferSize {
			end = bufferSize
		}

		chunk := data[i:end]
		if err := client.writeMessage(websocket.BinaryMessage, chunk); err != nil {
			log.WithFields(log.Fields{
				"chunk_index":   i / replayChunkSize,
				"chunk_size":    len(chunk),
				"bytes_sent":    bytesSent,
				"buffer_size":   bufferSize,
				"total_written": totalWritten,
				"error":         err.Error(),
			}).Warn("project/pty: Failed to replay buffer chunk")
			return // Client disconnected, stop replay
		}

		bytesSent += len(chunk)

		// Delay between chunks (except after last chunk)
		if end < bufferSize {
			time.Sleep(replayChunkDelay)
		}
	}

	// Record replay metrics
	if ps.appMetrics != nil {
		ps.appMetrics.observeReplayBytes(bytesSent)
	}

	log.WithFields(log.Fields{
		"buffer_size":   bufferSize,
		"bytes_sent":    bytesSent,
		"chunks_sent":   chunkCount,
		"total_written": totalWritten,
	}).Debug("project/pty: Completed chunked buffer replay")
}

func (ps *ptySession) removeClient(conn *websocket.Conn) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	_, existed := ps.clients[conn]
	delete(ps.clients, conn)

	if existed {
		log.WithFields(log.Fields{
			"client_count": len(ps.clients),
			"pty_age":      time.Since(ps.createdAt).String(),
		}).Info("project/pty: Client removed from PTY session")
	}
}

func (ps *ptySession) hasClients() bool {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	return len(ps.clients) > 0 || ps.reserved > 0
}

// reserveSlot atomically checks for existing clients and reserves a slot.
// Returns true if the slot was reserved (no existing clients), false if busy.
func (ps *ptySession) reserveSlot() bool {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if len(ps.clients) > 0 || ps.reserved > 0 {
		return false
	}
	ps.reserved++
	return true
}

// releaseSlot releases a previously reserved slot (e.g. after upgrade failure).
func (ps *ptySession) releaseSlot() {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if ps.reserved > 0 {
		ps.reserved--
	}
}

func (ps *ptySession) getClientCount() int {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	return len(ps.clients)
}

func (ps *ptySession) broadcast(data []byte) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	totalClients := len(ps.clients)
	pausedCount := 0
	bufferedCount := 0
	droppedCount := 0
	errorCount := 0

	for _, client := range ps.clients {
		// Buffer data for paused clients (flow control)
		if client.paused.Load() {
			pausedCount++
			if client.bufferPausedData(data) {
				bufferedCount++
			} else {
				// Buffer full - data will be lost for this client
				droppedCount++
				if ps.appMetrics != nil {
					ps.appMetrics.observeFlowControlBufferDrop()
				}
			}
			continue
		}

		if err := client.writeMessage(websocket.BinaryMessage, data); err != nil {
			errorCount++
			if ps.appMetrics != nil {
				ps.appMetrics.observeWSWriteError()
			}
			log.WithFields(log.Fields{
				"error":     err.Error(),
				"data_size": len(data),
			}).Warn("project/pty: Broadcast write error")
		}
	}

	// Log summary if there were issues
	if pausedCount > 0 || errorCount > 0 || droppedCount > 0 {
		log.WithFields(log.Fields{
			"total_clients":  totalClients,
			"paused_clients": pausedCount,
			"buffered_count": bufferedCount,
			"dropped_count":  droppedCount,
			"write_errors":   errorCount,
			"data_size":      len(data),
		}).Debug("project/pty: Broadcast completed with paused/error clients")
	}
}

func (ps *ptySession) broadcastClose() {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	clientCount := len(ps.clients)

	log.WithFields(log.Fields{
		"client_count": clientCount,
		"pty_age":      time.Since(ps.createdAt).String(),
	}).Info("project/pty: Broadcasting close to all clients")

	for conn, client := range ps.clients {
		_ = client.writeControl(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, "PTY exited"),
			time.Now().Add(time.Second))
		conn.Close()
	}
}

// disconnectAllClients forcefully disconnects all connected clients with a reason message.
// This is used for force takeover when a new client connects with ?force=true.
func (ps *ptySession) disconnectAllClients(reason string) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	clientCount := len(ps.clients)
	if clientCount == 0 {
		return
	}

	log.WithFields(log.Fields{
		"client_count": clientCount,
		"reason":       reason,
	}).Info("project/pty: Forcefully disconnecting all clients")

	for conn, client := range ps.clients {
		// Send close message with reason (use code 4000 for custom application close)
		_ = client.writeControl(websocket.CloseMessage,
			websocket.FormatCloseMessage(4000, reason),
			time.Now().Add(time.Second))
		conn.Close()
		delete(ps.clients, conn)
	}
}

func (s *server) staticHandler(w http.ResponseWriter, r *http.Request) {
	// Placeholder - will be implemented
	http.NotFound(w, r)
}

// handleMetrics returns resource metrics (disk, network) for the project pod.
// CPU and memory metrics are collected by the gateway via Kubernetes metrics-server.
func (s *server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	response := struct {
		Disk    resourceMetric `json:"disk"`
		Network networkMetric  `json:"network"`
	}{
		Disk:    getDiskMetrics(),
		Network: getNetworkMetrics(),
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.WithError(err).Error("Failed to encode metrics response")
	}
}

type resourceMetric struct {
	Usage   int64 `json:"usage"`
	Limit   int64 `json:"limit"`
	Percent int   `json:"percent"`
}

type networkMetric struct {
	RxBytes int64 `json:"rxBytes"`
	TxBytes int64 `json:"txBytes"`
	RxRate  int64 `json:"rxRate"` // Calculated by gateway
	TxRate  int64 `json:"txRate"` // Calculated by gateway
}

// getDiskMetrics returns disk usage for the PVC mounted at /home.
func getDiskMetrics() resourceMetric {
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/home", &stat); err != nil {
		log.WithError(err).Debug("Failed to get disk stats for /home")
		return resourceMetric{}
	}

	total := int64(stat.Blocks) * int64(stat.Bsize)
	free := int64(stat.Bfree) * int64(stat.Bsize)
	used := total - free

	var percent int
	if total > 0 {
		percent = int((used * 100) / total)
	}

	return resourceMetric{
		Usage:   used,
		Limit:   total,
		Percent: percent,
	}
}

// getNetworkMetrics reads network statistics from /proc/net/dev.
func getNetworkMetrics() networkMetric {
	file, err := os.Open("/proc/net/dev")
	if err != nil {
		log.WithError(err).Debug("Failed to open /proc/net/dev")
		return networkMetric{}
	}
	defer file.Close()

	var totalRx, totalTx int64
	buf := make([]byte, 4096)
	n, err := file.Read(buf)
	if err != nil {
		log.WithError(err).Debug("Failed to read /proc/net/dev")
		return networkMetric{}
	}

	// Parse /proc/net/dev format:
	// Inter-|   Receive                                                |  Transmit
	//  face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
	//    lo: 1234...
	//  eth0: 5678...
	lines := string(buf[:n])
	for _, line := range splitLines(lines) {
		// Skip header lines (contain "|")
		if len(line) == 0 || containsChar(line, '|') {
			continue
		}

		// Parse interface line
		fields := splitFields(line)
		if len(fields) < 10 {
			continue
		}

		// Skip loopback interface
		iface := trimColon(fields[0])
		if iface == "lo" {
			continue
		}

		// fields[1] = rx bytes, fields[9] = tx bytes
		rx := parseIntSafe(fields[1])
		tx := parseIntSafe(fields[9])
		totalRx += rx
		totalTx += tx
	}

	return networkMetric{
		RxBytes: totalRx,
		TxBytes: totalTx,
	}
}

// Helper functions for parsing /proc/net/dev without importing strings package
func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func splitFields(s string) []string {
	var fields []string
	start := -1
	for i := 0; i < len(s); i++ {
		if s[i] == ' ' || s[i] == '\t' {
			if start >= 0 {
				fields = append(fields, s[start:i])
				start = -1
			}
		} else {
			if start < 0 {
				start = i
			}
		}
	}
	if start >= 0 {
		fields = append(fields, s[start:])
	}
	return fields
}

// handleVersion returns the application version and build info.
func handleVersion(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"version":   version,
		"gitCommit": gitCommit,
		"buildTime": buildTime,
	})
}

func containsChar(s string, c byte) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return true
		}
	}
	return false
}

func trimColon(s string) string {
	if len(s) > 0 && s[len(s)-1] == ':' {
		return s[:len(s)-1]
	}
	return s
}

func parseIntSafe(s string) int64 {
	var n int64
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			n = n*10 + int64(s[i]-'0')
		}
	}
	return n
}

// GUIStatus represents the current state of the GUI stack
type GUIStatus struct {
	Enabled    bool           `json:"enabled"`
	Resolution string         `json:"resolution,omitempty"`
	VNCPort    string         `json:"vncPort,omitempty"`
	Components []GUIComponent `json:"components,omitempty"`
	Apps       []string       `json:"apps,omitempty"`
}

// GUIComponent represents a GUI stack component (Xvfb, x11vnc, etc.)
type GUIComponent struct {
	Name    string `json:"name"`
	Running bool   `json:"running"`
	PID     int    `json:"pid,omitempty"`
}

// handleGUIStatus returns the current GUI stack status
func (s *server) handleGUIStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	guiEnabled := os.Getenv("GUI_ENABLED") == "true"
	status := GUIStatus{
		Enabled: guiEnabled,
	}

	if guiEnabled {
		status.Resolution = os.Getenv("GUI_RESOLUTION")
		if status.Resolution == "" {
			status.Resolution = "1920x1080x24"
		}
		status.VNCPort = os.Getenv("GUI_VNC_PORT")
		if status.VNCPort == "" {
			status.VNCPort = "5900"
		}

		// Check each component's status
		status.Components = []GUIComponent{
			checkProcess("xvfb", "Xvfb"),
			checkProcess("x11vnc", "x11vnc"),
			checkProcess("dbus-daemon", "dbus"),
			checkProcess("xfce4-session", "xfce"),
		}

		// List running X11 applications (optional - uses xdotool if available)
		status.Apps = listX11Apps()
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(status); err != nil {
		log.WithError(err).Error("Failed to encode GUI status response")
	}
}

// checkProcess checks if a process is running by name
func checkProcess(processName, displayName string) GUIComponent {
	cmd := exec.Command("pgrep", "-x", processName)
	output, err := cmd.Output()
	if err != nil {
		// Also try with -f for processes with arguments
		cmd = exec.Command("pgrep", "-f", processName)
		output, err = cmd.Output()
		if err != nil {
			return GUIComponent{Name: displayName, Running: false}
		}
	}

	// Parse first PID from output (may have multiple if -f matches several)
	pidStr := string(output)
	// Trim trailing newline and get first line only
	for i := 0; i < len(pidStr); i++ {
		if pidStr[i] == '\n' {
			pidStr = pidStr[:i]
			break
		}
	}

	pid := int(parseIntSafe(pidStr))
	return GUIComponent{Name: displayName, Running: true, PID: pid}
}

// listX11Apps lists running X11 applications on display :99
func listX11Apps() []string {
	// Check if xdotool is available
	_, err := exec.LookPath("xdotool")
	if err != nil {
		return nil
	}

	cmd := exec.Command("xdotool", "search", "--name", "")
	cmd.Env = append(os.Environ(), "DISPLAY=:99")
	output, err := cmd.Output()
	if err != nil {
		return nil
	}

	// Get window names for each window ID
	var apps []string
	windowIDs := strings.Split(strings.TrimSpace(string(output)), "\n")
	for i, wid := range windowIDs {
		if wid == "" || i >= 10 { // Limit to 10 apps
			continue
		}
		nameCmd := exec.Command("xdotool", "getwindowname", wid)
		nameCmd.Env = append(os.Environ(), "DISPLAY=:99")
		nameOutput, err := nameCmd.Output()
		if err == nil {
			name := string(nameOutput)
			if len(name) > 0 && name[len(name)-1] == '\n' {
				name = name[:len(name)-1]
			}
			if name != "" {
				apps = append(apps, name)
			}
		}
	}
	return apps
}
