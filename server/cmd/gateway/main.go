package main

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"expvar"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/supporttools/KubeTTY/server/internal/auth"
	"github.com/supporttools/KubeTTY/server/internal/config"
	"github.com/supporttools/KubeTTY/server/internal/gateway/manager"
	"github.com/supporttools/KubeTTY/server/internal/gateway/tabs"
	handlers_auth "github.com/supporttools/KubeTTY/server/internal/handlers/auth"
	handlers_session "github.com/supporttools/KubeTTY/server/internal/handlers/session"
	"github.com/supporttools/KubeTTY/server/internal/sessions"
	apierrors "github.com/supporttools/KubeTTY/server/internal/shared/errors"
	"github.com/supporttools/KubeTTY/server/internal/shared/health"
	"github.com/supporttools/KubeTTY/server/internal/shared/metrics"
	sharedserver "github.com/supporttools/KubeTTY/server/internal/shared/server"
	"github.com/supporttools/KubeTTY/server/internal/shared/util"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

//go:embed ui/dist ui/dist/*
var embeddedUI embed.FS

const (
	clientCookieName       = "kubetty_client"
	accessTokenCookieName  = "kubetty_access"
	refreshTokenCookieName = "kubetty_refresh"
)

// Input validation limits
const (
	maxUsernameLength = 64
)

// usernameRegex allows only alphanumeric characters, underscores, and dashes
var usernameRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

func main() {
	// Create cancellable context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg, err := config.LoadGatewayConfig()
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
		authStore   *auth.PGStore
		authManager *auth.Manager
	)
	if cfg.AuthMode == "local" {
		authStore, err = auth.NewStore(ctx, cfg.ConnString())
		if err != nil {
			log.Fatalf("connect auth store: %v", err)
		}
		authManager, err = auth.NewManager(authStore, cfg.AuthJWTSecret, cfg.AuthIssuer, cfg.AuthAccessTTL, cfg.AuthRefreshTTL)
		if err != nil {
			log.Fatalf("init auth manager: %v", err)
		}
	}
	if authStore != nil {
		defer authStore.Close()
	}

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
		tabManager = manager.New(cfg.ProjectCatalog, tabStore, cfg.TabIdleTimeout)
		if err := tabManager.RestoreTabs(ctx); err != nil {
			log.Printf("warn: restore tabs: %v", err)
		}
		// Start idle checker goroutine for tab timeout monitoring
		go tabManager.StartIdleChecker(ctx)
	}
	if tabPool != nil {
		defer tabPool.Close()
	}
	if tabManager != nil {
		defer tabManager.Stop()
	}

	uiFS, err := fs.Sub(embeddedUI, "ui/dist")
	if err != nil {
		log.Fatalf("prepare static assets: %v", err)
	}

	appMetrics := metrics.NewAppMetrics()

	srv := &server{
		cfg:       cfg,
		store:     store,
		authStore: authStore,
		authMgr:   authManager,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
		uiFS:       uiFS,
		appMetrics: appMetrics,
		tabManager: tabManager,
		tabStore:   tabStore,
		tabSubs:    make(map[string]map[chan []byte]struct{}),
	}
	if srv.tabManager != nil {
		srv.tabManager.SetStatusCallback(srv.handleTabStatusUpdate)
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.Handle("/debug/vars", expvar.Handler())

	// Health check with gateway component status
	gatewayChecker := health.NewComponentChecker("gateway", func() string {
		if srv.tabManager != nil {
			return "enabled"
		}
		return "disabled"
	})
	var dbPinger health.Pinger
	if srv.store != nil {
		if pgxStore, ok := srv.store.(*sessions.PGXStore); ok {
			dbPinger = pgxStore
		}
	}
	mux.Handle("/api/healthz", health.NewCompatHandler(dbPinger, gatewayChecker))

	// Auth middleware
	requireAuth := handlers_auth.RequireAuth(srv.cfg, srv.authMgr)

	if srv.authEnabled() {
		// Auth handlers (extracted)
		mux.Handle("/api/auth/login", handlers_auth.NewAuthLoginHandler(srv.cfg, srv.authMgr, srv.authStore))
		mux.Handle("/api/auth/refresh", handlers_auth.NewAuthRefreshHandler(srv.cfg, srv.authMgr))
		mux.Handle("/api/auth/logout", requireAuth(handlers_auth.NewAuthLogoutHandler(srv.cfg, srv.authMgr, srv.authStore)))
		mux.Handle("/api/auth/me", requireAuth(handlers_auth.NewAuthMeHandler()))
		mux.Handle("/api/auth/password", requireAuth(handlers_auth.NewAuthPasswordChangeHandler(srv.cfg, srv.authMgr)))

		// Session handlers (extracted)
		mux.Handle("/session/logs", requireAuth(handlers_session.NewSessionLogsHandler(srv.store, srv)))

		// Gateway WebSocket (not yet extracted)
		mux.Handle("/ws", requireAuth(http.HandlerFunc(srv.handleGatewayWebsocket)))
		mux.Handle("/api/projects", requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if srv.tabManager == nil {
				apierrors.WriteError(w, apierrors.NotFound("gateway disabled", ""))
				return
			}
			_ = util.WriteJSON(w, http.StatusOK, map[string]any{"projects": srv.tabManager.ListProjects()})
		})))
		mux.Handle("/api/tabs", requireAuth(http.HandlerFunc(srv.handleTabs)))
		mux.Handle("/api/tabs/", requireAuth(http.HandlerFunc(srv.handleTabByID)))
		mux.Handle("/api/tabs/events", requireAuth(http.HandlerFunc(srv.handleTabEvents)))
	} else {
		// Session handlers (extracted) - no auth
		mux.Handle("/session/logs", srv.appMetrics.InstrumentHandler("session_logs", handlers_session.NewSessionLogsHandler(srv.store, srv)))

		// Gateway WebSocket (not yet extracted)
		mux.Handle("/ws", srv.appMetrics.InstrumentHandler("ws", http.HandlerFunc(srv.handleGatewayWebsocket)))
		mux.Handle("/api/projects", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if srv.tabManager == nil {
				apierrors.WriteError(w, apierrors.NotFound("gateway disabled", ""))
				return
			}
			_ = util.WriteJSON(w, http.StatusOK, map[string]any{"projects": srv.tabManager.ListProjects()})
		}))
		mux.Handle("/api/tabs", http.HandlerFunc(srv.handleTabs))
		mux.Handle("/api/tabs/", http.HandlerFunc(srv.handleTabByID))
		mux.Handle("/api/tabs/events", http.HandlerFunc(srv.handleTabEvents))
	}
	// Static files are always public (React handles auth state)
	mux.Handle("/", srv.appMetrics.InstrumentHandler("static", srv.staticHandler()))

	httpSrv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: sharedserver.LoggingMiddleware(mux),
	}

	// Start graceful shutdown handler in background
	go sharedserver.GracefulShutdown(httpSrv)

	log.Printf("KubeTTY Gateway listening on :%s", cfg.Port)
	if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("server exited: %v", err)
	}
}

type server struct {
	cfg        config.GatewayConfig
	store      sessions.Store
	authStore  auth.Store
	authMgr    *auth.Manager
	upgrader   websocket.Upgrader
	uiFS       fs.FS
	appMetrics *metrics.AppMetrics
	tabManager *manager.Manager
	tabStore   tabs.Store
	tabSubsMu  sync.Mutex
	tabSubs    map[string]map[chan []byte]struct{}
}

type contextKey string

const authUserContextKey contextKey = "kubettyAuthUser"

var (
	errAuthMissingToken = errors.New("authentication token missing")
	errAuthDisabled     = errors.New("authentication disabled")
)

type authUser struct {
	ID       uuid.UUID
	Username string
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

func (s *server) authEnabled() bool {
	return s != nil && s.cfg.AuthMode == "local" && s.authMgr != nil
}


// ObserveStore implements handlers_session.StoreMetricsObserver interface
func (s *server) ObserveStore(operation string, start time.Time, err error) {
	s.observeStore(operation, start, err)
}

func (s *server) handleGatewayWebsocket(w http.ResponseWriter, r *http.Request) {
	if s.tabManager == nil {
		apierrors.WriteError(w, apierrors.NotFound("gateway disabled", ""))
		return
	}
	tabID := r.URL.Query().Get("tab")
	if tabID == "" {
		apierrors.WriteError(w, apierrors.BadRequest("missing tab parameter", ""))
		return
	}
	clientID := s.ensureClientID(w, r)
	remoteAddr := r.RemoteAddr

	log.Printf("[GW %s] Tab connection attempt from %s (client=%s)", tabID, remoteAddr, clientID)

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[GW %s] Upgrade failed: %v", tabID, err)
		apierrors.WriteError(w, apierrors.InternalServerError("WebSocket upgrade failed", ""))
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
		apierrors.WriteError(w, apierrors.NotFound("gateway disabled", ""))
		return
	}
	clientID := s.ensureClientID(w, r)
	switch r.Method {
	case http.MethodGet:
		items, err := s.tabStore.ListByClient(r.Context(), clientID, 50)
		if err != nil {
			log.Printf("list tabs error: %v", err)
			apierrors.WriteError(w, apierrors.InternalServerError("internal error", ""))
			return
		}
		_ = util.WriteJSON(w, http.StatusOK, map[string]any{"tabs": items})
	case http.MethodPost:
		var req struct {
			ProjectID string `json:"projectId"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			apierrors.WriteError(w, apierrors.BadRequest("invalid request body", ""))
			return
		}
		if req.ProjectID == "" {
			apierrors.WriteError(w, apierrors.BadRequest("projectId is required", ""))
			return
		}
		tab, err := s.tabManager.CreateTab(r.Context(), req.ProjectID, clientID)
		if err != nil {
			// Check if error is due to tab limit exceeded
			var limitErr *manager.TabLimitExceededError
			if errors.As(err, &limitErr) {
				apierrors.WriteError(w, apierrors.RateLimitExceeded(
					"maximum tabs per client exceeded",
					fmt.Sprintf("limit: %d tabs per client for project %s", limitErr.Limit, limitErr.ProjectID),
				))
				return
			}
			log.Printf("create tab error: %v", err)
			apierrors.WriteError(w, apierrors.InternalServerError("internal error", ""))
			return
		}
		resp := map[string]any{
			"tab":   tab,
			"wsUrl": fmt.Sprintf("%s://%s/ws?tab=%s", util.WebSocketScheme(r), r.Host, tab.TabID),
		}
		_ = util.WriteJSON(w, http.StatusCreated, resp)
		s.broadcastTabSnapshot(clientID)
	default:
		apierrors.WriteError(w, apierrors.ErrorResponse{
			Status:  http.StatusMethodNotAllowed,
			Error:   "method_not_allowed",
			Message: "method not allowed",
		})
	}
}

func (s *server) handleTabByID(w http.ResponseWriter, r *http.Request) {
	if s.tabManager == nil || s.tabStore == nil {
		apierrors.WriteError(w, apierrors.NotFound("gateway disabled", ""))
		return
	}
	if r.Method != http.MethodDelete {
		apierrors.WriteError(w, apierrors.ErrorResponse{
			Status:  http.StatusMethodNotAllowed,
			Error:   "method_not_allowed",
			Message: "method not allowed",
		})
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/tabs/")
	if id == "" {
		apierrors.WriteError(w, apierrors.BadRequest("missing tab id", ""))
		return
	}
	clientID := s.ensureClientID(w, r)
	tab, err := s.tabStore.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, tabs.ErrNotFound) {
			apierrors.WriteError(w, apierrors.NotFound("tab not found", ""))
			return
		}
		log.Printf("load tab error: %v", err)
		apierrors.WriteError(w, apierrors.InternalServerError("internal error", ""))
		return
	}
	if tab.ClientID != clientID {
		apierrors.WriteError(w, apierrors.Forbidden("forbidden", ""))
		return
	}
	if err := s.tabManager.CloseTab(r.Context(), id); err != nil {
		if errors.Is(err, tabs.ErrNotFound) {
			apierrors.WriteError(w, apierrors.NotFound("tab not found", ""))
			return
		}
		log.Printf("close tab error: %v", err)
		apierrors.WriteError(w, apierrors.InternalServerError("internal error", ""))
		return
	}
	w.WriteHeader(http.StatusNoContent)
	s.broadcastTabDelete(clientID, id)
}

func (s *server) handleTabEvents(w http.ResponseWriter, r *http.Request) {
	if s.tabManager == nil || s.tabStore == nil {
		apierrors.WriteError(w, apierrors.NotFound("gateway disabled", ""))
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
		apierrors.WriteError(w, apierrors.InternalServerError("WebSocket upgrade failed", ""))
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
		apierrors.WriteError(w, apierrors.InternalServerError("streaming unsupported", ""))
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

func (s *server) observeStore(op string, start time.Time, err error) {
	if s == nil || s.appMetrics == nil {
		return
	}
	s.appMetrics.ObserveStore(op, time.Since(start), err)
}

func cloneURL(u *url.URL) *url.URL {
	if u == nil {
		return &url.URL{Path: "/"}
	}
	clone := *u
	return &clone
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
