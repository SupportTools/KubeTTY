package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/gorilla/websocket"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"

	"github.com/supporttools/KubeTTY/server/internal/auth"
	"github.com/supporttools/KubeTTY/server/internal/config"
	"github.com/supporttools/KubeTTY/server/internal/sessions"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

//go:embed ui/dist ui/dist/*
var embeddedUI embed.FS

type ptySession struct {
	cmd    *exec.Cmd
	ptmx   *os.File
	mu     sync.Mutex
	closed bool
}

type server struct {
	cfg     *config.Config
	store   sessions.Store
	authMgr *auth.Manager

	mu  sync.RWMutex
	pty *ptySession

	// Output buffer for reconnection
	outputBuf   []byte
	outputBufMu sync.RWMutex

	// WebSocket upgrader
	upgrader websocket.Upgrader

	// Single client tracking
	clientMu    sync.Mutex
	hasClient   bool
	clientCount int
}

func main() {
	log.SetFormatter(&log.TextFormatter{
		FullTimestamp: true,
	})

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Run migrations
	if err := applyMigrations(cfg.ConnString()); err != nil {
		log.Fatalf("apply migrations: %v", err)
	}

	// Initialize stores
	store, err := sessions.NewPGXStore(ctx, cfg.ConnString())
	if err != nil {
		log.Fatalf("create session store: %v", err)
	}

	srv := &server{
		cfg:   &cfg,
		store: store,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}

	// Initialize auth manager if enabled
	if cfg.AuthMode == "local" {
		authStore, err := auth.NewStore(ctx, cfg.ConnString())
		if err != nil {
			log.Fatalf("create auth store: %v", err)
		}
		srv.authMgr, err = auth.NewManager(authStore, cfg.AuthJWTSecret, cfg.AuthIssuer, cfg.AuthAccessTTL, cfg.AuthRefreshTTL)
		if err != nil {
			log.Fatalf("create auth manager: %v", err)
		}
	}

	// Initialize PTY
	if err := srv.initPTY(ctx); err != nil {
		log.Fatalf("init PTY: %v", err)
	}

	// Start log retention goroutine
	go srv.runLogRetention(ctx)

	// Setup HTTP routes
	mux := http.NewServeMux()

	// Public endpoints
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/api/healthz", srv.handleHealthz)

	// Auth endpoints (optional for project)
	if srv.authMgr != nil {
		mux.HandleFunc("/api/auth/login", srv.handleAuthLogin)
		mux.HandleFunc("/api/auth/refresh", srv.handleAuthRefresh)
		mux.HandleFunc("/api/auth/logout", srv.handleAuthLogout)
		mux.Handle("/api/auth/me", srv.requireAuth(http.HandlerFunc(srv.handleAuthMe)))
		mux.Handle("/api/auth/password", srv.requireAuth(http.HandlerFunc(srv.handlePasswordChange)))
	}

	// Project endpoints
	mux.HandleFunc("/ws", srv.handleWebsocket)
	mux.HandleFunc("/session/logs", srv.handleSessionLogs)

	// Static files
	mux.HandleFunc("/", srv.staticHandler)

	httpServer := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: mux,
	}

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Info("Shutting down project...")
		cancel()
		srv.shutdown()
		httpServer.Shutdown(context.Background())
	}()

	log.Infof("Project listening on :%s", cfg.Port)
	if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("HTTP server error: %v", err)
	}
}

func applyMigrations(connString string) error {
	sourceDriver, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("create source driver: %w", err)
	}

	m, err := migrate.NewWithSourceInstance("iofs", sourceDriver, connString)
	if err != nil {
		return fmt.Errorf("create migrate: %w", err)
	}
	defer m.Close()

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("migrate up: %w", err)
	}

	return nil
}

// writeJSON writes a JSON response
func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// Placeholder handlers - will be implemented in handlers.go and pty.go

func (s *server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "healthy"})
}

func (s *server) handleWebsocket(w http.ResponseWriter, r *http.Request) {
	// TODO: Implement
}

func (s *server) handleSessionLogs(w http.ResponseWriter, r *http.Request) {
	// TODO: Implement
}

func (s *server) staticHandler(w http.ResponseWriter, r *http.Request) {
	// TODO: Implement
}

func (s *server) initPTY(ctx context.Context) error {
	// TODO: Implement
	return nil
}

func (s *server) shutdown() {
	// TODO: Implement
}

func (s *server) runLogRetention(ctx context.Context) {
	// TODO: Implement
}

func (s *server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	// TODO: Implement
}

func (s *server) handleAuthRefresh(w http.ResponseWriter, r *http.Request) {
	// TODO: Implement
}

func (s *server) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	// TODO: Implement
}

func (s *server) handleAuthMe(w http.ResponseWriter, r *http.Request) {
	// TODO: Implement
}

func (s *server) handlePasswordChange(w http.ResponseWriter, r *http.Request) {
	// TODO: Implement
}

func (s *server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// TODO: Implement
		next.ServeHTTP(w, r)
	})
}

// Suppress unused import warnings during stub phase
var _ = pty.Start
var _ = time.Now
