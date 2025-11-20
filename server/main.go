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
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
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

	"github.com/supporttools/KubeTTY/server/internal/auth"
	"github.com/supporttools/KubeTTY/server/internal/config"
	"github.com/supporttools/KubeTTY/server/internal/gateway/manager"
	"github.com/supporttools/KubeTTY/server/internal/gateway/tabs"
	"github.com/supporttools/KubeTTY/server/internal/sessions"
	apierrors "github.com/supporttools/KubeTTY/server/internal/shared/errors"
)

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
	maxPTYCols        = 500
	maxPTYRows        = 200
)

// usernameRegex allows only alphanumeric characters, underscores, and dashes
var usernameRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

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
		cfg:       cfg,
		store:     store,
		authStore: authStore,
		authMgr:   authManager,
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
	mux.Handle("/metrics", promhttp.Handler())
	mux.Handle("/debug/vars", expvar.Handler())
	mux.Handle("/api/healthz", http.HandlerFunc(srv.handleHealthz))
	if srv.authEnabled() {
		mux.Handle("/api/auth/login", http.HandlerFunc(srv.handleAuthLogin))
		mux.Handle("/api/auth/refresh", http.HandlerFunc(srv.handleAuthRefresh))
		mux.Handle("/api/auth/logout", srv.requireAuth(http.HandlerFunc(srv.handleAuthLogout)))
		mux.Handle("/api/auth/me", srv.requireAuth(http.HandlerFunc(srv.handleAuthMe)))
		mux.Handle("/api/auth/password", srv.requireAuth(http.HandlerFunc(srv.handleAuthPasswordChange)))
		mux.Handle("/session/logs", srv.requireAuth(http.HandlerFunc(srv.handleSessionLogs)))
		mux.Handle("/ws", srv.requireAuth(http.HandlerFunc(srv.handleWSRoute)))
		mux.Handle("/api/projects", srv.requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if srv.tabManager == nil {
				apierrors.WriteError(w, apierrors.NotFound("gateway disabled", ""))
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"projects": srv.tabManager.ListProjects()})
		})))
		mux.Handle("/api/tabs", srv.requireAuth(http.HandlerFunc(srv.handleTabs)))
		mux.Handle("/api/tabs/", srv.requireAuth(http.HandlerFunc(srv.handleTabByID)))
		mux.Handle("/api/tabs/events", srv.requireAuth(http.HandlerFunc(srv.handleTabEvents)))
	} else {
		mux.Handle("/session/logs", srv.appMetrics.instrumentHandler("session_logs", http.HandlerFunc(srv.handleSessionLogs)))
		mux.Handle("/ws", srv.appMetrics.instrumentHandler("ws", http.HandlerFunc(srv.handleWSRoute)))
		mux.Handle("/api/projects", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if srv.tabManager == nil {
				apierrors.WriteError(w, apierrors.NotFound("gateway disabled", ""))
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"projects": srv.tabManager.ListProjects()})
		}))
		mux.Handle("/api/tabs", http.HandlerFunc(srv.handleTabs))
		mux.Handle("/api/tabs/", http.HandlerFunc(srv.handleTabByID))
		mux.Handle("/api/tabs/events", http.HandlerFunc(srv.handleTabEvents))
	}
	// Static files are always public (React handles auth state)
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
	authStore  auth.Store
	authMgr    *auth.Manager
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

func (s *server) authEnabled() bool {
	return s != nil && s.cfg.AuthMode == "local" && s.authMgr != nil
}

func (s *server) requireAuth(next http.Handler) http.Handler {
	if next == nil || !s.authEnabled() {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, err := s.authenticateRequest(r)
		if err != nil {
			s.handleAuthFailure(w, err)
			return
		}
		ctx := context.WithValue(r.Context(), authUserContextKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *server) authenticateRequest(r *http.Request) (*authUser, error) {
	if !s.authEnabled() {
		return nil, errAuthDisabled
	}
	token := s.accessTokenFromRequest(r)
	if token == "" {
		return nil, errAuthMissingToken
	}
	claims, err := s.authMgr.ValidateAccessToken(token)
	if err != nil {
		return nil, err
	}
	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return nil, auth.ErrTokenMalformed
	}
	return &authUser{ID: userID, Username: claims.Username}, nil
}

func authUserFromContext(ctx context.Context) *authUser {
	if ctx == nil {
		return nil
	}
	if v, ok := ctx.Value(authUserContextKey).(*authUser); ok {
		return v
	}
	return nil
}

func (s *server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	if !s.authEnabled() {
		apierrors.WriteError(w, apierrors.ServiceUnavailable("authentication disabled", ""))
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierrors.WriteError(w, apierrors.BadRequest("invalid JSON", ""))
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || req.Password == "" {
		apierrors.WriteError(w, apierrors.BadRequest("username and password required", ""))
		return
	}
	if len(req.Username) > maxUsernameLength {
		apierrors.WriteError(w, apierrors.BadRequest(
			fmt.Sprintf("username must be %d characters or less", maxUsernameLength),
			"",
		))
		return
	}
	if !usernameRegex.MatchString(req.Username) {
		apierrors.WriteError(w, apierrors.BadRequest(
			"username must contain only letters, numbers, underscores, and dashes",
			"",
		))
		return
	}
	user, err := s.authMgr.Authenticate(r.Context(), req.Username, req.Password)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			apierrors.WriteError(w, apierrors.Unauthorized("invalid credentials", ""))
			return
		}
		log.Printf("authentication error: %v", err)
		apierrors.WriteError(w, apierrors.InternalServerError("internal error", ""))
		return
	}
	meta := s.tokenMetadataFromRequest(r)
	tokens, err := s.authMgr.IssueTokenPair(r.Context(), user, meta)
	if err != nil {
		log.Printf("issue tokens error: %v", err)
		apierrors.WriteError(w, apierrors.InternalServerError("internal error", ""))
		return
	}
	if s.authStore != nil {
		if err := s.authStore.UpdateLastLogin(r.Context(), user.ID, time.Now()); err != nil {
			log.Printf("warn: update last login: %v", err)
		}
	}
	s.setAuthCookies(w, tokens)
	writeJSON(w, http.StatusOK, map[string]any{
		"user":             map[string]any{"id": user.ID.String(), "username": user.Username},
		"accessToken":      tokens.AccessToken,
		"accessExpiresAt":  tokens.AccessExpiresAt,
		"refreshExpiresAt": tokens.RefreshExpiresAt,
	})
}

func (s *server) handleAuthRefresh(w http.ResponseWriter, r *http.Request) {
	if !s.authEnabled() {
		apierrors.WriteError(w, apierrors.ServiceUnavailable("authentication disabled", ""))
		return
	}
	var req struct {
		RefreshToken string `json:"refreshToken"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		apierrors.WriteError(w, apierrors.BadRequest("invalid JSON", ""))
		return
	}
	token := s.refreshTokenFromRequest(r, strings.TrimSpace(req.RefreshToken))
	if token == "" {
		apierrors.WriteError(w, apierrors.Unauthorized("refresh token required", ""))
		return
	}
	meta := s.tokenMetadataFromRequest(r)
	pair, err := s.authMgr.Refresh(r.Context(), token, meta)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrTokenExpired):
			apierrors.WriteError(w, apierrors.Unauthorized("refresh token expired", ""))
		case errors.Is(err, auth.ErrTokenRevoked):
			apierrors.WriteError(w, apierrors.Unauthorized("refresh token revoked", ""))
		case errors.Is(err, auth.ErrInvalidCredentials):
			apierrors.WriteError(w, apierrors.Unauthorized("account disabled", ""))
		default:
			log.Printf("refresh token error: %v", err)
			apierrors.WriteError(w, apierrors.InternalServerError("internal error", ""))
		}
		return
	}
	s.setAuthCookies(w, pair)
	claims, err := s.authMgr.ValidateAccessToken(pair.AccessToken)
	if err != nil {
		log.Printf("validate access token error: %v", err)
		apierrors.WriteError(w, apierrors.InternalServerError("internal error", ""))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user":             map[string]any{"id": claims.Subject, "username": claims.Username},
		"accessToken":      pair.AccessToken,
		"accessExpiresAt":  pair.AccessExpiresAt,
		"refreshExpiresAt": pair.RefreshExpiresAt,
	})
}

func (s *server) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	if !s.authEnabled() {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	var req struct {
		RefreshToken string `json:"refreshToken"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		apierrors.WriteError(w, apierrors.BadRequest("invalid JSON", ""))
		return
	}
	token := s.refreshTokenFromRequest(r, strings.TrimSpace(req.RefreshToken))
	if token != "" && s.authStore != nil {
		if tokenID, _, err := auth.ParseRefreshToken(token); err == nil {
			if err := s.authStore.RevokeRefreshToken(r.Context(), tokenID, time.Now()); err != nil && !errors.Is(err, auth.ErrRefreshTokenNotFound) {
				log.Printf("warn: revoke refresh token: %v", err)
			}
		}
	}
	s.clearAuthCookies(w)
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handleAuthMe(w http.ResponseWriter, r *http.Request) {
	user := authUserFromContext(r.Context())
	if user == nil {
		apierrors.WriteError(w, apierrors.Unauthorized("unauthorized", ""))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user": map[string]any{"id": user.ID.String(), "username": user.Username},
	})
}

func (s *server) handleAuthPasswordChange(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	user := authUserFromContext(r.Context())
	if user == nil {
		apierrors.WriteError(w, apierrors.Unauthorized("unauthorized", ""))
		return
	}

	var req struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierrors.WriteError(w, apierrors.BadRequest("invalid request body", ""))
		return
	}

	if req.CurrentPassword == "" || req.NewPassword == "" {
		apierrors.WriteError(w, apierrors.BadRequest("current and new password are required", ""))
		return
	}

	err := s.authMgr.ChangePassword(r.Context(), user.ID, req.CurrentPassword, req.NewPassword)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrInvalidCredentials):
			apierrors.WriteError(w, apierrors.Unauthorized("current password is incorrect", ""))
		case errors.Is(err, auth.ErrWeakPassword):
			apierrors.WriteError(w, apierrors.BadRequest(err.Error(), ""))
		default:
			log.Printf("password change error: %v", err)
			apierrors.WriteError(w, apierrors.InternalServerError("internal error", ""))
		}
		return
	}

	// Clear auth cookies since refresh tokens were revoked
	s.clearAccessCookie(w)
	s.clearRefreshCookie(w)

	writeJSON(w, http.StatusOK, map[string]any{
		"message": "password changed successfully",
	})
}

func (s *server) handleAuthFailure(w http.ResponseWriter, err error) {
	var errResp apierrors.ErrorResponse
	switch {
	case errors.Is(err, errAuthMissingToken):
		errResp = apierrors.Unauthorized("authentication required", "")
	case errors.Is(err, auth.ErrTokenExpired):
		errResp = apierrors.Unauthorized("token expired", "")
	case errors.Is(err, auth.ErrTokenMalformed):
		errResp = apierrors.Unauthorized("token malformed", "")
	case errors.Is(err, errAuthDisabled):
		errResp = apierrors.ServiceUnavailable("authentication disabled", "")
	default:
		if err != nil {
			log.Printf("auth failure: %v", err)
		}
		errResp = apierrors.Unauthorized("unauthorized", "")
	}
	s.clearAccessCookie(w)
	w.Header().Set("WWW-Authenticate", `Bearer realm="kubetty"`)
	apierrors.WriteError(w, errResp)
}

func (s *server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	status := "healthy"
	httpStatus := http.StatusOK
	components := make(map[string]string)

	// Check database connectivity
	if s.store != nil {
		if pgxStore, ok := s.store.(*sessions.PGXStore); ok {
			if err := pgxStore.Ping(ctx); err != nil {
				status = "unhealthy"
				httpStatus = http.StatusServiceUnavailable
				components["database"] = fmt.Sprintf("error: %v", err)
			} else {
				components["database"] = "ok"
			}
		} else {
			components["database"] = "unknown"
		}
	} else {
		components["database"] = "not_configured"
	}

	// Check PTY status
	s.mu.RLock()
	pty := s.pty
	s.mu.RUnlock()
	if pty != nil && pty.isAlive() {
		components["pty"] = "ok"
	} else if pty != nil {
		components["pty"] = "not_running"
	} else {
		components["pty"] = "not_initialized"
	}

	writeJSON(w, httpStatus, map[string]any{
		"status":     status,
		"components": components,
	})
}

func (s *server) tokenMetadataFromRequest(r *http.Request) auth.TokenMetadata {
	return auth.TokenMetadata{
		CreatedBy: r.Header.Get("X-Requested-By"),
		UserAgent: r.UserAgent(),
		ClientIP:  clientIPFromRequest(r),
	}
}

func (s *server) accessTokenFromRequest(r *http.Request) string {
	authz := r.Header.Get("Authorization")
	if strings.HasPrefix(strings.ToLower(authz), "bearer ") {
		return strings.TrimSpace(authz[7:])
	}
	if c, err := r.Cookie(accessTokenCookieName); err == nil && c.Value != "" {
		return c.Value
	}
	return ""
}

func (s *server) refreshTokenFromRequest(r *http.Request, provided string) string {
	if provided != "" {
		return provided
	}
	if c, err := r.Cookie(refreshTokenCookieName); err == nil && c.Value != "" {
		return c.Value
	}
	return ""
}

func (s *server) setAuthCookies(w http.ResponseWriter, pair *auth.TokenPair) {
	if pair == nil {
		return
	}
	http.SetCookie(w, s.cookieTemplate(accessTokenCookieName, pair.AccessToken, pair.AccessExpiresAt))
	http.SetCookie(w, s.cookieTemplate(refreshTokenCookieName, pair.RefreshToken, pair.RefreshExpiresAt))
}

func (s *server) clearAuthCookies(w http.ResponseWriter) {
	s.clearAccessCookie(w)
	s.clearRefreshCookie(w)
}

func (s *server) clearAccessCookie(w http.ResponseWriter) {
	c := s.cookieTemplate(accessTokenCookieName, "", time.Time{})
	c.MaxAge = -1
	c.Expires = time.Unix(0, 0)
	http.SetCookie(w, c)
}

func (s *server) clearRefreshCookie(w http.ResponseWriter) {
	c := s.cookieTemplate(refreshTokenCookieName, "", time.Time{})
	c.MaxAge = -1
	c.Expires = time.Unix(0, 0)
	http.SetCookie(w, c)
}

func (s *server) cookieTemplate(name, value string, expires time.Time) *http.Cookie {
	c := &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cfg.AuthCookieSecure,
		SameSite: http.SameSiteLaxMode,
	}
	if s.cfg.AuthCookieDomain != "" {
		c.Domain = s.cfg.AuthCookieDomain
	}
	if !expires.IsZero() {
		c.Expires = expires
		maxAge := int(time.Until(expires).Seconds())
		if maxAge < 0 {
			maxAge = 0
		}
		c.MaxAge = maxAge
	}
	return c
}

func clientIPFromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}
	if xf := r.Header.Get("X-Forwarded-For"); xf != "" {
		parts := strings.Split(xf, ",")
		if len(parts) > 0 {
			return strings.TrimSpace(parts[0])
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func (s *server) handleSessionLogs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sessionID := r.URL.Query().Get("session")
	if sessionID == "" {
		apierrors.WriteError(w, apierrors.BadRequest("missing session parameter", ""))
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
		log.Printf("list logs error: %v", err)
		apierrors.WriteError(w, apierrors.InternalServerError("internal error", ""))
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
		log.Printf("encode session logs response error: %v", err)
		apierrors.WriteError(w, apierrors.InternalServerError("internal error", ""))
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
		writeJSON(w, http.StatusOK, map[string]any{"tabs": items})
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
			log.Printf("create tab error: %v", err)
			apierrors.WriteError(w, apierrors.InternalServerError("internal error", ""))
			return
		}
		resp := map[string]any{
			"tab":   tab,
			"wsUrl": fmt.Sprintf("%s://%s/ws?tab=%s", wsScheme(r), r.Host, tab.TabID),
		}
		writeJSON(w, http.StatusCreated, resp)
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
		apierrors.WriteError(w, apierrors.Conflict(
			"session already attached",
			"only one client allowed per session",
		))
		return
	}

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[WS %s] Upgrade failed: %v", connID, err)
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
