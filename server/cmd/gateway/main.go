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
	"regexp"
	"strconv"
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
	"github.com/supporttools/KubeTTY/server/internal/sessions"
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
	if srv.authEnabled() {
		mux.Handle("/api/auth/login", http.HandlerFunc(srv.handleAuthLogin))
		mux.Handle("/api/auth/refresh", http.HandlerFunc(srv.handleAuthRefresh))
		mux.Handle("/api/auth/logout", srv.requireAuth(http.HandlerFunc(srv.handleAuthLogout)))
		mux.Handle("/api/auth/me", srv.requireAuth(http.HandlerFunc(srv.handleAuthMe)))
		mux.Handle("/api/auth/password", srv.requireAuth(http.HandlerFunc(srv.handleAuthPasswordChange)))
		mux.Handle("/session/logs", srv.requireAuth(http.HandlerFunc(srv.handleSessionLogs)))
		mux.Handle("/ws", srv.requireAuth(http.HandlerFunc(srv.handleGatewayWebsocket)))
		mux.Handle("/api/projects", srv.requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if srv.tabManager == nil {
				http.Error(w, "gateway disabled", http.StatusNotFound)
				return
			}
			_ = util.WriteJSON(w, http.StatusOK, map[string]any{"projects": srv.tabManager.ListProjects()})
		})))
		mux.Handle("/api/tabs", srv.requireAuth(http.HandlerFunc(srv.handleTabs)))
		mux.Handle("/api/tabs/", srv.requireAuth(http.HandlerFunc(srv.handleTabByID)))
		mux.Handle("/api/tabs/events", srv.requireAuth(http.HandlerFunc(srv.handleTabEvents)))
	} else {
		mux.Handle("/session/logs", srv.appMetrics.InstrumentHandler("session_logs", http.HandlerFunc(srv.handleSessionLogs)))
		mux.Handle("/ws", srv.appMetrics.InstrumentHandler("ws", http.HandlerFunc(srv.handleGatewayWebsocket)))
		mux.Handle("/api/projects", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if srv.tabManager == nil {
				http.Error(w, "gateway disabled", http.StatusNotFound)
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
	cfg        config.Config
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
		http.Error(w, "auth disabled", http.StatusNotImplemented)
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || req.Password == "" {
		http.Error(w, "username and password required", http.StatusBadRequest)
		return
	}
	if len(req.Username) > maxUsernameLength {
		http.Error(w, fmt.Sprintf("username must be %d characters or less", maxUsernameLength), http.StatusBadRequest)
		return
	}
	if !usernameRegex.MatchString(req.Username) {
		http.Error(w, "username must contain only letters, numbers, underscores, and dashes", http.StatusBadRequest)
		return
	}
	user, err := s.authMgr.Authenticate(r.Context(), req.Username, req.Password)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			http.Error(w, "invalid credentials", http.StatusUnauthorized)
			return
		}
		http.Error(w, fmt.Sprintf("authenticate: %v", err), http.StatusInternalServerError)
		return
	}
	meta := s.tokenMetadataFromRequest(r)
	tokens, err := s.authMgr.IssueTokenPair(r.Context(), user, meta)
	if err != nil {
		http.Error(w, fmt.Sprintf("issue tokens: %v", err), http.StatusInternalServerError)
		return
	}
	if s.authStore != nil {
		if err := s.authStore.UpdateLastLogin(r.Context(), user.ID, time.Now()); err != nil {
			log.Printf("warn: update last login: %v", err)
		}
	}
	s.setAuthCookies(w, tokens)
	_ = util.WriteJSON(w, http.StatusOK, map[string]any{
		"user":             map[string]any{"id": user.ID.String(), "username": user.Username},
		"accessToken":      tokens.AccessToken,
		"accessExpiresAt":  tokens.AccessExpiresAt,
		"refreshExpiresAt": tokens.RefreshExpiresAt,
	})
}

func (s *server) handleAuthRefresh(w http.ResponseWriter, r *http.Request) {
	if !s.authEnabled() {
		http.Error(w, "auth disabled", http.StatusNotImplemented)
		return
	}
	var req struct {
		RefreshToken string `json:"refreshToken"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	token := s.refreshTokenFromRequest(r, strings.TrimSpace(req.RefreshToken))
	if token == "" {
		http.Error(w, "refresh token required", http.StatusUnauthorized)
		return
	}
	meta := s.tokenMetadataFromRequest(r)
	pair, err := s.authMgr.Refresh(r.Context(), token, meta)
	if err != nil {
		status := http.StatusUnauthorized
		switch {
		case errors.Is(err, auth.ErrTokenExpired):
			http.Error(w, "refresh token expired", status)
		case errors.Is(err, auth.ErrTokenRevoked):
			http.Error(w, "refresh token revoked", status)
		case errors.Is(err, auth.ErrInvalidCredentials):
			http.Error(w, "account disabled", status)
		default:
			http.Error(w, fmt.Sprintf("refresh: %v", err), http.StatusInternalServerError)
		}
		return
	}
	s.setAuthCookies(w, pair)
	claims, err := s.authMgr.ValidateAccessToken(pair.AccessToken)
	if err != nil {
		http.Error(w, "invalid access token", http.StatusInternalServerError)
		return
	}
	_ = util.WriteJSON(w, http.StatusOK, map[string]any{
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
		http.Error(w, "invalid JSON", http.StatusBadRequest)
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
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	_ = util.WriteJSON(w, http.StatusOK, map[string]any{
		"user": map[string]any{"id": user.ID.String(), "username": user.Username},
	})
}

func (s *server) handleAuthPasswordChange(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	user := authUserFromContext(r.Context())
	if user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.CurrentPassword == "" || req.NewPassword == "" {
		http.Error(w, "current and new password are required", http.StatusBadRequest)
		return
	}

	err := s.authMgr.ChangePassword(r.Context(), user.ID, req.CurrentPassword, req.NewPassword)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrInvalidCredentials):
			http.Error(w, "current password is incorrect", http.StatusUnauthorized)
		case errors.Is(err, auth.ErrWeakPassword):
			http.Error(w, err.Error(), http.StatusBadRequest)
		default:
			log.Printf("password change error: %v", err)
			http.Error(w, "failed to change password", http.StatusInternalServerError)
		}
		return
	}

	// Clear auth cookies since refresh tokens were revoked
	s.clearAccessCookie(w)
	s.clearRefreshCookie(w)

	_ = util.WriteJSON(w, http.StatusOK, map[string]any{
		"message": "password changed successfully",
	})
}

func (s *server) handleAuthFailure(w http.ResponseWriter, err error) {
	msg := "unauthorized"
	status := http.StatusUnauthorized
	switch {
	case errors.Is(err, errAuthMissingToken):
		msg = "authentication required"
	case errors.Is(err, auth.ErrTokenExpired):
		msg = "token expired"
	case errors.Is(err, auth.ErrTokenMalformed):
		msg = "token malformed"
	case errors.Is(err, errAuthDisabled):
		msg = "auth disabled"
	default:
		if err != nil {
			log.Printf("auth failure: %v", err)
		}
	}
	s.clearAccessCookie(w)
	w.Header().Set("WWW-Authenticate", `Bearer realm="kubetty"`)
	_ = util.WriteJSON(w, status, map[string]any{"error": msg})
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
		_ = util.WriteJSON(w, http.StatusOK, map[string]any{"tabs": items})
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
		_ = util.WriteJSON(w, http.StatusCreated, resp)
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
