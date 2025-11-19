package main

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"expvar"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/supporttools/KubeTTY/server/internal/config"
	"github.com/supporttools/KubeTTY/server/internal/gateway/manager"
	"github.com/supporttools/KubeTTY/server/internal/gateway/tabs"
	"github.com/supporttools/KubeTTY/server/internal/sessions"
)

//go:embed ui/dist ui/dist/*
var embeddedUI embed.FS

const clientCookieName = "kubetty_client"

func main() {
	ctx := context.Background()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	if err := runMigrations(ctx, cfg.ConnString()); err != nil {
		log.Fatalf("apply migrations: %v", err)
	}

	store, err := sessions.NewPGXStore(ctx, cfg.ConnString())
	if err != nil {
		log.Fatalf("connect cnpg: %v", err)
	}
	defer store.Close()

	var (
		tabStore   tabs.Store
		tabManager *manager.Manager
		tabPool    *pgxpool.Pool
	)
	if len(cfg.ProjectCatalog.Projects) > 0 {
		tabPool, err = pgxpool.New(ctx, cfg.ConnString())
		if err != nil {
			log.Fatalf("gateway pool: %v", err)
		}
		tabStore = tabs.NewPGXStore(tabPool)
		tabManager = manager.New(cfg.ProjectCatalog, tabStore)
		if err := tabManager.RestoreTabs(ctx); err != nil {
			log.Printf("warn: restore tabs: %v", err)
		}
	}
	if tabPool != nil {
		defer tabPool.Close()
	}

	uiFS, err := fs.Sub(embeddedUI, "ui/dist")
	if err != nil {
		log.Fatalf("prepare static assets: %v", err)
	}

	metrics := newCleanupMetrics()
	appMetrics := newAppMetrics()

	srv := &server{
		cfg:   cfg,
		store: store,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
		uiFS:       uiFS,
		metrics:    metrics,
		appMetrics: appMetrics,
		tabManager: tabManager,
		tabStore:   tabStore,
		tabSubs:    make(map[string]map[chan []byte]struct{}),
	}
	if srv.tabManager != nil {
		srv.tabManager.SetStatusCallback(srv.handleTabStatusUpdate)
	}

	maintCtx, cancelMaintenance := context.WithCancel(ctx)
	defer cancelMaintenance()
	go srv.runLogRetention(maintCtx)

	mux := http.NewServeMux()
	mux.Handle("/session/logs", srv.appMetrics.instrumentHandler("session_logs", http.HandlerFunc(srv.handleSessionLogs)))
	mux.Handle("/ws", srv.appMetrics.instrumentHandler("ws", http.HandlerFunc(srv.handleWSRoute)))
	mux.Handle("/api/projects", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if srv.tabManager == nil {
			http.Error(w, "gateway disabled", http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"projects": srv.tabManager.ListProjects()})
	}))
	mux.Handle("/api/tabs", http.HandlerFunc(srv.handleTabs))
	mux.Handle("/api/tabs/", http.HandlerFunc(srv.handleTabByID))
	mux.Handle("/api/tabs/events", http.HandlerFunc(srv.handleTabEvents))
	mux.Handle("/metrics", promhttp.Handler())
	mux.Handle("/debug/vars", expvar.Handler())
	mux.Handle("/", srv.appMetrics.instrumentHandler("static", srv.staticHandler()))

	httpSrv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: logRequest(mux),
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		log.Printf("received %s, shutting down", sig)
		if err := httpSrv.Shutdown(context.Background()); err != nil {
			log.Printf("warn: http shutdown: %v", err)
		}
		srv.shutdown()
	}()

	log.Printf("KubeTTY listening on :%s (session %s)", cfg.Port, cfg.SessionID)
	if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("server exited: %v", err)
	}
}

type server struct {
	cfg        config.Config
	store      sessions.Store
	upgrader   websocket.Upgrader
	uiFS       fs.FS
	metrics    *cleanupMetrics
	appMetrics *appMetrics
	tabManager *manager.Manager
	tabStore   tabs.Store
	tabSubsMu  sync.Mutex
	tabSubs    map[string]map[chan []byte]struct{}

	mu  sync.RWMutex
	pty *ptySession
}

type ptySession struct {
	cmd       *exec.Cmd
	ptmx      *os.File
	createdAt time.Time
	logCh     chan sessions.LogEntry

	mu            sync.RWMutex
	clients       map[*websocket.Conn]bool
	outputBuffer  []byte // Buffer for initial output (MOTD, etc.)
	bufferMaxSize int    // Maximum buffer size (64KB)
}

func (s *server) staticHandler() http.Handler {
	fileServer := http.FileServer(http.FS(s.uiFS))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			fileServer.ServeHTTP(w, r)
			return
		}
		if _, err := s.uiFS.Open(path); err != nil {
			// Fallback to index for SPA routes.
			r2 := new(http.Request)
			*r2 = *r
			r2.URL = cloneURL(r.URL)
			r2.URL.Path = "/"
			fileServer.ServeHTTP(w, r2)
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}

func (s *server) handleSessionLogs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sessionID := r.URL.Query().Get("session")
	if sessionID == "" {
		http.Error(w, "missing session parameter", http.StatusBadRequest)
		return
	}
	limit := 200
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			switch {
			case parsed <= 0:
			case parsed > 2000:
				limit = 2000
			default:
				limit = parsed
			}
		}
	}
	start := time.Now()
	logs, err := s.store.ListLogs(ctx, sessionID, limit)
	s.observeStore("ListLogs", start, err)
	if err != nil {
		http.Error(w, fmt.Sprintf("list logs: %v", err), http.StatusInternalServerError)
		return
	}
	if logs == nil {
		logs = []sessions.LogEntry{}
	}
	resp := map[string]any{
		"sessionId": sessionID,
		"logs":      logs,
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *server) handleWSRoute(w http.ResponseWriter, r *http.Request) {
	if s.tabManager != nil && r.URL.Query().Get("tab") != "" {
		s.handleGatewayWebsocket(w, r)
		return
	}
	s.handleWebsocket(w, r)
}

func (s *server) handleGatewayWebsocket(w http.ResponseWriter, r *http.Request) {
	if s.tabManager == nil {
		http.Error(w, "gateway disabled", http.StatusNotFound)
		return
	}
	tabID := r.URL.Query().Get("tab")
	if tabID == "" {
		http.Error(w, "missing tab parameter", http.StatusBadRequest)
		return
	}
	clientID := s.ensureClientID(w, r)
	remoteAddr := r.RemoteAddr

	log.Printf("[GW %s] Tab connection attempt from %s (client=%s)", tabID, remoteAddr, clientID)

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[GW %s] Upgrade failed: %v", tabID, err)
		http.Error(w, fmt.Sprintf("upgrade: %v", err), http.StatusInternalServerError)
		return
	}
	defer func() {
		conn.Close()
		log.Printf("[GW %s] Connection closed (client=%s)", tabID, clientID)
	}()

	log.Printf("[GW %s] Connection established (client=%s)", tabID, clientID)

	if err := s.tabManager.Attach(r.Context(), tabID, clientID, conn); err != nil {
		log.Printf("[GW %s] Attach failed: %v", tabID, err)
		_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseInternalServerErr, err.Error()), time.Now().Add(time.Second))
		return
	}
}

func (s *server) handleTabs(w http.ResponseWriter, r *http.Request) {
	if s.tabManager == nil || s.tabStore == nil {
		http.Error(w, "gateway disabled", http.StatusNotFound)
		return
	}
	clientID := s.ensureClientID(w, r)
	switch r.Method {
	case http.MethodGet:
		items, err := s.tabStore.ListByClient(r.Context(), clientID, 50)
		if err != nil {
			http.Error(w, fmt.Sprintf("list tabs: %v", err), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"tabs": items})
	case http.MethodPost:
		var req struct {
			ProjectID string `json:"projectId"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf("decode body: %v", err), http.StatusBadRequest)
			return
		}
		if req.ProjectID == "" {
			http.Error(w, "projectId is required", http.StatusBadRequest)
			return
		}
		tab, err := s.tabManager.CreateTab(r.Context(), req.ProjectID, clientID)
		if err != nil {
			http.Error(w, fmt.Sprintf("create tab: %v", err), http.StatusInternalServerError)
			return
		}
		resp := map[string]any{
			"tab":   tab,
			"wsUrl": fmt.Sprintf("%s://%s/ws?tab=%s", wsScheme(r), r.Host, tab.TabID),
		}
		writeJSON(w, http.StatusCreated, resp)
		s.broadcastTabSnapshot(clientID)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *server) handleTabByID(w http.ResponseWriter, r *http.Request) {
	if s.tabManager == nil || s.tabStore == nil {
		http.Error(w, "gateway disabled", http.StatusNotFound)
		return
	}
	if r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/tabs/")
	if id == "" {
		http.Error(w, "missing tab id", http.StatusBadRequest)
		return
	}
	clientID := s.ensureClientID(w, r)
	tab, err := s.tabStore.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, tabs.ErrNotFound) {
			http.Error(w, "tab not found", http.StatusNotFound)
			return
		}
		http.Error(w, fmt.Sprintf("load tab: %v", err), http.StatusInternalServerError)
		return
	}
	if tab.ClientID != clientID {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if err := s.tabManager.CloseTab(r.Context(), id); err != nil {
		if errors.Is(err, tabs.ErrNotFound) {
			http.Error(w, "tab not found", http.StatusNotFound)
			return
		}
		http.Error(w, fmt.Sprintf("close tab: %v", err), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
	s.broadcastTabDelete(clientID, id)
}

func (s *server) handleTabEvents(w http.ResponseWriter, r *http.Request) {
	if s.tabManager == nil || s.tabStore == nil {
		http.Error(w, "gateway disabled", http.StatusNotFound)
		return
	}
	if websocket.IsWebSocketUpgrade(r) {
		s.handleTabEventsWS(w, r)
		return
	}
	s.handleTabEventsSSE(w, r)
}

func (s *server) handleTabEventsWS(w http.ResponseWriter, r *http.Request) {
	clientID := s.ensureClientID(w, r)
	remoteAddr := r.RemoteAddr

	log.Printf("[TabEvents %s] Connection attempt from %s", clientID, remoteAddr)

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[TabEvents %s] Upgrade failed: %v", clientID, err)
		http.Error(w, fmt.Sprintf("upgrade: %v", err), http.StatusInternalServerError)
		return
	}
	defer func() {
		conn.Close()
		log.Printf("[TabEvents %s] Connection closed", clientID)
	}()

	log.Printf("[TabEvents %s] Connection established", clientID)

	ch := s.subscribeTabEvents(clientID)
	defer s.unsubscribeTabEvents(clientID, ch)
	s.broadcastTabSnapshot(clientID)
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			log.Printf("[TabEvents %s] Context cancelled", clientID)
			return
		case msg, ok := <-ch:
			if !ok {
				log.Printf("[TabEvents %s] Channel closed", clientID)
				return
			}
			if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				log.Printf("[TabEvents %s] Write error: %v", clientID, err)
				return
			}
		}
	}
}

func (s *server) handleTabEventsSSE(w http.ResponseWriter, r *http.Request) {
	clientID := s.ensureClientID(w, r)
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	ch := s.subscribeTabEvents(clientID)
	defer s.unsubscribeTabEvents(clientID, ch)
	s.broadcastTabSnapshot(clientID)
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			if _, err := w.Write([]byte("data: ")); err != nil {
				return
			}
			if _, err := w.Write(msg); err != nil {
				return
			}
			if _, err := w.Write([]byte("\n\n")); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (s *server) handleTabStatusUpdate(tab tabs.Tab) {
	s.broadcastTabUpdate(tab)
}

func (s *server) ensureClientID(w http.ResponseWriter, r *http.Request) string {
	if c, err := r.Cookie(clientCookieName); err == nil && c.Value != "" {
		return c.Value
	}
	id := uuid.NewString()
	http.SetCookie(w, &http.Cookie{
		Name:     clientCookieName,
		Value:    id,
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int((365 * 24 * time.Hour) / time.Second),
	})
	return id
}

func wsScheme(r *http.Request) string {
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		return "wss"
	}
	return "ws"
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func (s *server) subscribeTabEvents(clientID string) chan []byte {
	s.tabSubsMu.Lock()
	defer s.tabSubsMu.Unlock()
	ch := make(chan []byte, 8)
	if s.tabSubs == nil {
		s.tabSubs = make(map[string]map[chan []byte]struct{})
	}
	if s.tabSubs[clientID] == nil {
		s.tabSubs[clientID] = make(map[chan []byte]struct{})
	}
	s.tabSubs[clientID][ch] = struct{}{}
	return ch
}

func (s *server) unsubscribeTabEvents(clientID string, ch chan []byte) {
	s.tabSubsMu.Lock()
	defer s.tabSubsMu.Unlock()
	if subs, ok := s.tabSubs[clientID]; ok {
		delete(subs, ch)
		if len(subs) == 0 {
			delete(s.tabSubs, clientID)
		}
	}
	close(ch)
}

func (s *server) broadcastTabSnapshot(clientID string) {
	if s.tabStore == nil {
		return
	}
	tabsList, err := s.tabStore.ListByClient(context.Background(), clientID, 50)
	if err != nil {
		log.Printf("gateway: list tabs snapshot: %v", err)
		return
	}
	s.sendTabEvent(clientID, map[string]any{"type": "snapshot", "tabs": tabsList})
}

func (s *server) broadcastTabUpdate(tab tabs.Tab) {
	s.sendTabEvent(tab.ClientID, map[string]any{"type": "update", "tab": tab})
}

func (s *server) broadcastTabDelete(clientID, tabID string) {
	s.sendTabEvent(clientID, map[string]any{"type": "delete", "tabId": tabID})
}

func (s *server) sendTabEvent(clientID string, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	s.tabSubsMu.Lock()
	subs := s.tabSubs[clientID]
	s.tabSubsMu.Unlock()
	for ch := range subs {
		select {
		case ch <- data:
		default:
		}
	}
}

func (s *server) handleWebsocket(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	remoteAddr := r.RemoteAddr
	connID := fmt.Sprintf("%s-%d", remoteAddr, time.Now().UnixNano())

	log.Printf("[WS %s] Connection attempt from %s", connID, remoteAddr)

	// Ensure PTY exists
	if err := s.initPTY(ctx); err != nil {
		log.Printf("[WS %s] PTY init failed: %v", connID, err)
		http.Error(w, fmt.Sprintf("PTY unavailable: %v", err), http.StatusInternalServerError)
		return
	}

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[WS %s] Upgrade failed: %v", connID, err)
		http.Error(w, fmt.Sprintf("upgrade: %v", err), http.StatusInternalServerError)
		return
	}
	defer func() {
		conn.Close()
		log.Printf("[WS %s] Connection closed", connID)
	}()

	log.Printf("[WS %s] Connection established", connID)

	// Get PTY reference
	s.mu.RLock()
	ps := s.pty
	s.mu.RUnlock()

	if ps == nil {
		http.Error(w, "PTY not initialized", http.StatusInternalServerError)
		return
	}

	// Register this client
	ps.addClient(conn)
	defer ps.removeClient(conn)

	if s.appMetrics != nil {
		s.appMetrics.sessionAttached(false)
		defer s.appMetrics.sessionDetached()
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
				if msg.Cols > 0 && msg.Rows > 0 {
					if err := pty.Setsize(ps.ptmx, &pty.Winsize{Cols: msg.Cols, Rows: msg.Rows}); err != nil {
						log.Printf("[WS %s] PTY resize error: %v", connID, err)
					}
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
		if ps.logCh != nil {
			queueLog(ps.logCh, s.cfg.SessionID, "in", data)
		}
		if _, err := ps.ptmx.Write(data); err != nil {
			log.Printf("pty write error: %v", err)
			return
		}
	}
}

func (s *server) shutdown() {
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
	cmd.Env = os.Environ()
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

	// Optional: setup logging to database
	if s.store != nil {
		s.pty.logCh = make(chan sessions.LogEntry, 256)
		go func() {
			for entry := range s.pty.logCh {
				start := time.Now()
				err := s.store.AppendLog(context.Background(), entry)
				s.observeStore("AppendLog", start, err)
				if err != nil {
					log.Printf("warn: append log: %v", err)
				}
			}
		}()

		// Persist metadata to DB
		meta := sessions.Session{
			SessionID:    s.cfg.SessionID,
			DeploymentID: s.cfg.DeploymentID,
			ShellPID:     cmd.Process.Pid,
			CreatedAt:    s.pty.createdAt,
			UpdatedAt:    s.pty.createdAt,
		}
		if err := s.store.UpsertSession(ctx, meta); err != nil {
			log.Printf("warn: persist PTY metadata: %v", err)
		}
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

				// Optional: log to database
				if s.store != nil {
					queueLog(s.pty.logCh, s.cfg.SessionID, "out", data)
				}
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
		s.mu.Lock()
		if s.pty != nil {
			s.pty.broadcastClose()
			if s.pty.logCh != nil {
				close(s.pty.logCh)
			}
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

type wsWriter struct {
	conn *websocket.Conn
}

func (w wsWriter) Write(p []byte) (int, error) {
	if err := w.conn.WriteMessage(websocket.BinaryMessage, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

type metricsWriter struct {
	metrics   *appMetrics
	direction string
}

func (mw metricsWriter) Write(p []byte) (int, error) {
	if mw.metrics != nil {
		mw.metrics.observeWSBytes(mw.direction, len(p))
	}
	return len(p), nil
}

type logWriter struct {
	sessionID string
	direction string
	ch        chan<- sessions.LogEntry
}

func (w logWriter) Write(p []byte) (int, error) {
	queueLog(w.ch, w.sessionID, w.direction, p)
	return len(p), nil
}

func queueLog(ch chan<- sessions.LogEntry, sessionID, direction string, data []byte) {
	if ch == nil || len(data) == 0 {
		return
	}
	buf := make([]byte, len(data))
	copy(buf, data)
	entry := sessions.LogEntry{
		SessionID: sessionID,
		Direction: direction,
		Data:      buf,
		CreatedAt: time.Now(),
	}
	select {
	case ch <- entry:
	default:
	}
}

func (s *server) runLogRetention(ctx context.Context) {
	if s.cfg.LogRetentionHours <= 0 && s.cfg.LogMaxPerSession <= 0 {
		return
	}
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()
	s.enforceLogRetention(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.enforceLogRetention(ctx)
		}
	}
}

func (s *server) enforceLogRetention(ctx context.Context) {
	cleanupCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if s.metrics != nil {
		s.metrics.recordRunStart()
	}

	if s.cfg.LogRetentionHours > 0 {
		cutoff := time.Now().Add(-time.Duration(s.cfg.LogRetentionHours) * time.Hour)
		if deleted, err := s.store.PruneLogs(cleanupCtx, cutoff); err != nil {
			log.Printf("warn: prune logs: %v", err)
			if s.metrics != nil {
				s.metrics.recordError(err)
			}
		} else if deleted > 0 {
			log.Printf("pruned %d session log rows older than %s", deleted, cutoff.Format(time.RFC3339))
			if s.metrics != nil {
				s.metrics.addPruned(deleted)
			}
		}
	}
	if s.cfg.LogMaxPerSession > 0 {
		if deleted, err := s.store.TrimLogs(cleanupCtx, s.cfg.LogMaxPerSession); err != nil {
			log.Printf("warn: trim logs: %v", err)
			if s.metrics != nil {
				s.metrics.recordError(err)
			}
		} else if deleted > 0 {
			log.Printf("trimmed %d session log rows over per-session cap %d", deleted, s.cfg.LogMaxPerSession)
			if s.metrics != nil {
				s.metrics.addTrimmed(deleted)
			}
		}
	}
}

func logRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start))
	})
}

func cloneURL(u *url.URL) *url.URL {
	if u == nil {
		return &url.URL{Path: "/"}
	}
	clone := *u
	return &clone
}

func (s *server) observeStore(op string, start time.Time, err error) {
	if s == nil || s.appMetrics == nil {
		return
	}
	s.appMetrics.observeStore(op, time.Since(start), err)
}

func runMigrations(ctx context.Context, connString string) error {
	source, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("migrations source: %w", err)
	}
	db, err := sql.Open("pgx", connString)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping db: %w", err)
	}
	driver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		return fmt.Errorf("postgres driver: %w", err)
	}
	m, err := migrate.NewWithInstance("iofs", source, "postgres", driver)
	if err != nil {
		return fmt.Errorf("migrate init: %w", err)
	}
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("migrate up: %w", err)
	}
	return nil
}
