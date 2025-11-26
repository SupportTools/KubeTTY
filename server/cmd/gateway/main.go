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
	log "github.com/sirupsen/logrus"

	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/supporttools/KubeTTY/server/internal/auth"
	"github.com/supporttools/KubeTTY/server/internal/config"
	"github.com/supporttools/KubeTTY/server/internal/controller"
	gatewayconfig "github.com/supporttools/KubeTTY/server/internal/gateway/config"
	"github.com/supporttools/KubeTTY/server/internal/gateway/manager"
	gatewaymetrics "github.com/supporttools/KubeTTY/server/internal/gateway/metrics"
	"github.com/supporttools/KubeTTY/server/internal/gateway/tabs"
	handlers_admin "github.com/supporttools/KubeTTY/server/internal/handlers/admin"
	handlers_auth "github.com/supporttools/KubeTTY/server/internal/handlers/auth"
	handlers_dashboard "github.com/supporttools/KubeTTY/server/internal/handlers/dashboard"
	handlers_session "github.com/supporttools/KubeTTY/server/internal/handlers/session"
	"github.com/supporttools/KubeTTY/server/internal/leaderelection"
	"github.com/supporttools/KubeTTY/server/internal/projects"
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
		log.WithFields(log.Fields{
			"error": err.Error(),
		}).Fatal("gateway/main: failed to load gateway configuration")
	}

	// Security warning when authentication is disabled
	if cfg.AuthMode != "local" {
		log.WithFields(log.Fields{
			"auth_mode": cfg.AuthMode,
		}).Warn("gateway/main: SECURITY WARNING - authentication is DISABLED, all routes are unprotected")
		log.Warn("gateway/main: set AUTH_MODE=local and configure AUTH_JWT_SECRET to enable authentication")
	}

	if err := runMigrations(ctx, cfg.ConnString()); err != nil {
		log.WithFields(log.Fields{
			"error": err.Error(),
		}).Fatal("gateway/main: failed to apply database migrations")
	}

	poolConfig, err := cfg.ConnConfig()
	if err != nil {
		log.WithFields(log.Fields{
			"error": err.Error(),
		}).Fatal("gateway/main: failed to build database pool config")
	}

	store, err := sessions.NewPGXStore(ctx, poolConfig)
	if err != nil {
		log.WithFields(log.Fields{
			"error": err.Error(),
		}).Fatal("gateway/main: failed to connect to CNPG database")
	}
	defer store.Close()

	var (
		authStore   *auth.PGStore
		authManager *auth.Manager
	)
	if cfg.AuthMode == "local" {
		authPoolConfig, err := cfg.ConnConfig()
		if err != nil {
			log.WithFields(log.Fields{
				"error": err.Error(),
			}).Fatal("gateway/main: failed to build auth pool config")
		}
		authStore, err = auth.NewStore(ctx, authPoolConfig)
		if err != nil {
			log.WithFields(log.Fields{
				"error": err.Error(),
			}).Fatal("gateway/main: failed to connect to auth store")
		}
		authManager, err = auth.NewManager(authStore, cfg.AuthJWTSecret, cfg.AuthIssuer, cfg.AuthAccessTTL, cfg.AuthRefreshTTL)
		if err != nil {
			log.WithFields(log.Fields{
				"error":       err.Error(),
				"access_ttl":  cfg.AuthAccessTTL.String(),
				"refresh_ttl": cfg.AuthRefreshTTL.String(),
			}).Fatal("gateway/main: failed to initialize auth manager")
		}
	}
	if authStore != nil {
		defer authStore.Close()
	}

	// Initialize project store and controller for project lifecycle management
	var (
		projectStore *projects.PGStore
		projCtrl     *controller.Controller
	)
	projectPoolConfig, err := cfg.ConnConfig()
	if err != nil {
		log.WithFields(log.Fields{
			"error": err.Error(),
		}).Fatal("gateway/main: failed to build project pool config")
	}
	projectStore = projects.NewStoreFromPool(func() *pgxpool.Pool {
		pool, err := pgxpool.NewWithConfig(ctx, projectPoolConfig)
		if err != nil {
			log.WithFields(log.Fields{
				"error": err.Error(),
			}).Fatal("gateway/main: failed to create project store pool")
		}
		return pool
	}(), cfg.Controller.ProjectsNamespace)

	// Always initialize tabManager FIRST to support both static catalog and dynamic projects
	// NOTE: tabManager must be created before controller starts so the callback can be set
	tabPoolConfig, err := cfg.ConnConfig()
	if err != nil {
		log.WithFields(log.Fields{
			"error": err.Error(),
		}).Fatal("gateway/main: failed to build tab pool config")
	}
	tabPool, err := pgxpool.NewWithConfig(ctx, tabPoolConfig)
	if err != nil {
		log.WithFields(log.Fields{
			"error": err.Error(),
		}).Fatal("gateway/main: failed to create gateway database pool")
	}
	defer tabPool.Close()

	tabStore := tabs.NewPGXStore(tabPool)
	tabManager := manager.NewWithConfig(cfg.ProjectCatalog, tabStore, manager.ManagerConfig{
		IdleTimeout:     cfg.TabIdleTimeout,
		MetricsEnabled:  cfg.MetricsEnabled,
		MetricsInterval: cfg.MetricsInterval,
	})
	defer tabManager.Stop()

	// Set project store for activity tracking
	if projectStore != nil {
		tabManager.SetProjectStore(projectStore)
	}

	// leaderElector is declared here so it can be captured for the server struct
	var leaderElector *leaderelection.LeaderElector

	// Create project controller (manages K8s resources) if enabled
	// NOTE: We create the controller but don't start it yet - we need to set the callback first
	if cfg.Controller.Enabled && cfg.Controller.ProjectsNamespace != "" {
		ctrlCfg := controller.Config{
			ReconcileInterval:   cfg.Controller.ReconcileInterval,
			HealthCheckInterval: cfg.Controller.HealthCheckInterval,
			EnvSecretName:       cfg.Controller.EnvSecretName,
			ResourceConfig: controller.ResourceConfig{
				Namespace:        cfg.Controller.ProjectsNamespace,
				Prefix:           cfg.Controller.ResourcePrefix,
				Env:              cfg.Controller.ParseEnvironment(),
				ImagePullSecrets: cfg.Controller.ImagePullSecrets,
				TemplatePVCName:  cfg.Controller.TemplatePVCName,
			},
			StorageMonitor: controller.StorageMonitorConfig{
				Enabled:         cfg.Controller.StorageMonitorEnabled,
				Interval:        cfg.Controller.StorageMonitorInterval,
				ExpandThreshold: cfg.Controller.StorageExpandThreshold,
				ExpandAmount:    cfg.Controller.StorageExpandAmount,
				ExpandCooldown:  cfg.Controller.StorageExpandCooldown,
			},
		}
		projCtrl, err = controller.New(ctrlCfg, projectStore)
		if err != nil {
			log.WithFields(log.Fields{
				"error":     err.Error(),
				"namespace": cfg.Controller.ProjectsNamespace,
			}).Warn("gateway/main: project controller disabled due to initialization error")
		} else {
			// Set up controller callback BEFORE starting the controller
			// This ensures projects that transition to running during initial reconciliation
			// are properly registered with tabManager (fixes race condition)
			projCtrl.SetStatusCallback(func(p *projects.Project, status projects.ProjectStatus) {
				if status == projects.StatusRunning {
					// Use ServiceName from database, fallback to computed name for backwards compatibility
					serviceName := p.ServiceName
					if serviceName == "" {
						serviceName = projects.ComputeServiceName(p.Name)
						log.WithFields(log.Fields{
							"project":      p.Name,
							"service_name": serviceName,
						}).Warn("gateway/main: controller callback - project missing ServiceName, using computed name")
					}

					// Parse CPU and memory limits for metrics percentage calculation
					var cpuMillicores, memoryBytes int64
					if p.CPULimit != "" {
						if qty, err := resource.ParseQuantity(p.CPULimit); err == nil {
							cpuMillicores = qty.MilliValue()
						} else {
							log.WithFields(log.Fields{
								"project":   p.Name,
								"cpu_limit": p.CPULimit,
								"error":     err.Error(),
							}).Warn("gateway/main: controller callback - project has invalid CPULimit, cannot parse quantity")
						}
					}
					if p.MemoryLimit != "" {
						if qty, err := resource.ParseQuantity(p.MemoryLimit); err == nil {
							memoryBytes = qty.Value()
						} else {
							log.WithFields(log.Fields{
								"project":      p.Name,
								"memory_limit": p.MemoryLimit,
								"error":        err.Error(),
							}).Warn("gateway/main: controller callback - project has invalid MemoryLimit, cannot parse quantity")
						}
					}

					log.WithFields(log.Fields{
						"project": p.Name,
					}).Info("gateway/main: controller callback - registering project with tabManager")
					tabManager.RegisterProject(gatewayconfig.Project{
						ID:          p.Name,
						DisplayName: p.DisplayName,
						Namespace:   p.TargetNamespace,
						Service:     serviceName,
						Port:        8080,
						Description: p.Description,
						Icon:        p.Icon,
						Limits: gatewayconfig.ProjectLimits{
							MaxTabsPerClient: p.MaxTabsPerClient,
							MaxTabsTotal:     p.MaxTabsTotal,
							CPUMillicores:    cpuMillicores,
							MemoryBytes:      memoryBytes,
						},
					})
				} else if status == projects.StatusDeleting || status == projects.StatusDeleted {
					log.WithFields(log.Fields{
						"project": p.Name,
					}).Info("gateway/main: controller callback - unregistering project from tabManager")
					tabManager.UnregisterProject(p.Name)
				}
			})

			// Start controller with or without leader election
			if cfg.LeaderElection.Enabled {
				// Configure leader election
				leCfg := leaderelection.DefaultConfig()
				leCfg.LeaseName = cfg.LeaderElection.LeaseName
				leCfg.LeaseDuration = cfg.LeaderElection.LeaseDuration
				leCfg.RenewDeadline = cfg.LeaderElection.RenewDeadline
				leCfg.RetryPeriod = cfg.LeaderElection.RetryPeriod

				// Create leader elector with controller lifecycle callbacks
				leaderElector, err = leaderelection.New(leCfg, leaderelection.Callbacks{
					OnStartedLeading: func(leaderCtx context.Context) {
						log.Info("gateway/main: acquired leadership, starting controller")
						projCtrl.Start(leaderCtx)
					},
					OnStoppedLeading: func() {
						log.Warn("gateway/main: lost leadership, stopping controller")
						projCtrl.Stop()
					},
					OnNewLeader: func(identity string) {
						log.WithField("leader", identity).Info("gateway/main: new leader elected")
					},
				})
				if err != nil {
					log.WithError(err).Fatal("gateway/main: failed to create leader elector")
				}

				// Set leader info on controller for status endpoints
				projCtrl.SetLeaderInfo(leaderElector)

				// Start leader election in background
				go func() {
					if err := leaderElector.Start(ctx); err != nil {
						log.WithError(err).Error("gateway/main: leader election failed")
					}
				}()
				defer leaderElector.Stop()

				log.WithFields(log.Fields{
					"namespace":      ctrlCfg.ResourceConfig.Namespace,
					"prefix":         ctrlCfg.ResourceConfig.Prefix,
					"env":            ctrlCfg.ResourceConfig.Env,
					"lease_name":     leCfg.LeaseName,
					"lease_duration": leCfg.LeaseDuration,
				}).Info("gateway/main: project controller started with leader election")
			} else {
				// No leader election - start controller directly
				projCtrl.Start(ctx)
				defer projCtrl.Stop()
				log.WithFields(log.Fields{
					"namespace": ctrlCfg.ResourceConfig.Namespace,
					"prefix":    ctrlCfg.ResourceConfig.Prefix,
					"env":       ctrlCfg.ResourceConfig.Env,
				}).Info("gateway/main: project controller started (no leader election)")
			}
		}
	} else {
		log.WithFields(log.Fields{
			"enabled":   cfg.Controller.Enabled,
			"namespace": cfg.Controller.ProjectsNamespace,
		}).Info("gateway/main: project controller disabled")
	}

	// Register running projects from the database (for projects that were already running before gateway started)
	if projectStore != nil {
		runningProjects, err := projectStore.List(ctx, projects.ListFilter{Status: "running"})
		if err != nil {
			log.WithFields(log.Fields{
				"error": err.Error(),
			}).Warn("gateway/main: failed to load running projects from database")
		} else {
			for _, p := range runningProjects {
				// Use ServiceName from database, fallback to computed name for backwards compatibility
				serviceName := p.ServiceName
				if serviceName == "" {
					serviceName = projects.ComputeServiceName(p.Name)
					log.WithFields(log.Fields{
						"project":      p.Name,
						"service_name": serviceName,
					}).Warn("gateway/main: project missing ServiceName in database, using computed name")
				}

				// Parse CPU and memory limits for metrics percentage calculation
				var cpuMillicores, memoryBytes int64
				if p.CPULimit != "" {
					if qty, err := resource.ParseQuantity(p.CPULimit); err == nil {
						cpuMillicores = qty.MilliValue()
					} else {
						log.WithFields(log.Fields{
							"project":   p.Name,
							"cpu_limit": p.CPULimit,
							"error":     err.Error(),
						}).Warn("gateway/main: project has invalid CPULimit, cannot parse quantity")
					}
				}
				if p.MemoryLimit != "" {
					if qty, err := resource.ParseQuantity(p.MemoryLimit); err == nil {
						memoryBytes = qty.Value()
					} else {
						log.WithFields(log.Fields{
							"project":      p.Name,
							"memory_limit": p.MemoryLimit,
							"error":        err.Error(),
						}).Warn("gateway/main: project has invalid MemoryLimit, cannot parse quantity")
					}
				}

				tabManager.RegisterProject(gatewayconfig.Project{
					ID:          p.Name,
					DisplayName: p.DisplayName,
					Namespace:   p.TargetNamespace,
					Service:     serviceName,
					Port:        8080,
					Description: p.Description,
					Icon:        p.Icon,
					Limits: gatewayconfig.ProjectLimits{
						MaxTabsPerClient: p.MaxTabsPerClient,
						MaxTabsTotal:     p.MaxTabsTotal,
						CPUMillicores:    cpuMillicores,
						MemoryBytes:      memoryBytes,
					},
				})
			}
			log.WithFields(log.Fields{
				"count": len(runningProjects),
			}).Info("gateway/main: registered running projects from database")
		}
	}

	if err := tabManager.RestoreTabs(ctx); err != nil {
		log.WithFields(log.Fields{
			"error": err.Error(),
		}).Warn("gateway/main: failed to restore tabs")
	}
	// Start idle checker goroutine for tab timeout monitoring
	go tabManager.StartIdleChecker(ctx)
	// Start background health checking for projects
	tabManager.StartHealthChecker()

	uiFS, err := fs.Sub(embeddedUI, "ui/dist")
	if err != nil {
		log.WithFields(log.Fields{
			"error": err.Error(),
			"path":  "ui/dist",
		}).Fatal("gateway/main: failed to prepare static assets")
	}

	appMetrics := metrics.NewAppMetrics()

	srv := &server{
		cfg:           cfg,
		store:         store,
		authStore:     authStore,
		authMgr:       authManager,
		projectStore:  projectStore,
		projCtrl:      projCtrl,
		leaderElector: leaderElector,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
		uiFS:        uiFS,
		appMetrics:  appMetrics,
		tabManager:  tabManager,
		tabStore:    tabStore,
		tabSubs:     make(map[string]map[chan []byte]struct{}),
		shutdownCtx: ctx, // For graceful shutdown signaling to WebSocket handlers
	}
	if srv.tabManager != nil {
		srv.tabManager.SetStatusCallback(srv.handleTabStatusUpdate)
		srv.tabManager.SetMetricsCallback(srv.handleTabMetricsUpdate)
		// Start metrics collector for tab resource monitoring
		srv.tabManager.StartMetricsCollector()
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

	// Leader status endpoint for monitoring leader election
	mux.Handle("/api/healthz/leader", health.NewLeaderStatusHandler(srv.leaderElector))

	// Auth middleware
	requireAuth := handlers_auth.RequireAuth(srv.cfg, srv.authMgr)

	// Admin API handlers for project management (requires auth when enabled)
	if srv.projectStore != nil && srv.projCtrl != nil {
		adminHandlers := handlers_admin.NewProjectHandlers(srv.projectStore, srv.projCtrl, srv.cfg.RecommendedImageTag)
		// Set callback to unregister project from tabManager when deleted
		adminHandlers.SetDeleteCallback(func(projectName string) {
			tabManager.UnregisterProject(projectName)
		})
		if srv.authEnabled() {
			mux.Handle("GET /api/admin/projects", requireAuth(http.HandlerFunc(adminHandlers.ListProjects)))
			mux.Handle("POST /api/admin/projects", requireAuth(http.HandlerFunc(adminHandlers.CreateProject)))
			mux.Handle("GET /api/admin/projects/{id}", requireAuth(http.HandlerFunc(adminHandlers.GetProject)))
			mux.Handle("PUT /api/admin/projects/{id}", requireAuth(http.HandlerFunc(adminHandlers.UpdateProject)))
			mux.Handle("DELETE /api/admin/projects/{id}", requireAuth(http.HandlerFunc(adminHandlers.DeleteProject)))
			mux.Handle("POST /api/admin/projects/{id}/restart", requireAuth(http.HandlerFunc(adminHandlers.RestartProject)))
			mux.Handle("POST /api/admin/projects/{id}/resync", requireAuth(http.HandlerFunc(adminHandlers.ResyncProject)))
			mux.Handle("GET /api/admin/projects/{id}/status", requireAuth(http.HandlerFunc(adminHandlers.GetProjectStatus)))
			mux.Handle("GET /api/admin/projects/{id}/upgrade-info", requireAuth(http.HandlerFunc(adminHandlers.GetUpgradeInfo)))
			mux.Handle("POST /api/admin/projects/{id}/upgrade", requireAuth(http.HandlerFunc(adminHandlers.UpgradeProject)))
			mux.Handle("GET /api/admin/projects/{id}/secrets", requireAuth(http.HandlerFunc(adminHandlers.GetProjectSecrets)))
			mux.Handle("PUT /api/admin/projects/{id}/secrets", requireAuth(http.HandlerFunc(adminHandlers.UpdateProjectSecrets)))
		} else {
			mux.HandleFunc("GET /api/admin/projects", adminHandlers.ListProjects)
			mux.HandleFunc("POST /api/admin/projects", adminHandlers.CreateProject)
			mux.HandleFunc("GET /api/admin/projects/{id}", adminHandlers.GetProject)
			mux.HandleFunc("PUT /api/admin/projects/{id}", adminHandlers.UpdateProject)
			mux.HandleFunc("DELETE /api/admin/projects/{id}", adminHandlers.DeleteProject)
			mux.HandleFunc("POST /api/admin/projects/{id}/restart", adminHandlers.RestartProject)
			mux.HandleFunc("POST /api/admin/projects/{id}/resync", adminHandlers.ResyncProject)
			mux.HandleFunc("GET /api/admin/projects/{id}/status", adminHandlers.GetProjectStatus)
			mux.HandleFunc("GET /api/admin/projects/{id}/upgrade-info", adminHandlers.GetUpgradeInfo)
			mux.HandleFunc("POST /api/admin/projects/{id}/upgrade", adminHandlers.UpgradeProject)
			mux.HandleFunc("GET /api/admin/projects/{id}/secrets", adminHandlers.GetProjectSecrets)
			mux.HandleFunc("PUT /api/admin/projects/{id}/secrets", adminHandlers.UpdateProjectSecrets)
		}
	}

	// Dashboard API handlers
	dashboardHandlers := handlers_dashboard.New(srv.projectStore, srv.tabStore, handlers_dashboard.NewNullMetricsCollector())
	if srv.authEnabled() {
		mux.Handle("GET /api/admin/dashboard/summary", requireAuth(http.HandlerFunc(dashboardHandlers.GetSummary)))
		mux.Handle("GET /api/admin/dashboard/metrics", requireAuth(http.HandlerFunc(dashboardHandlers.GetMetrics)))
		mux.Handle("GET /api/admin/dashboard/errors", requireAuth(http.HandlerFunc(dashboardHandlers.GetErrors)))
		mux.Handle("GET /api/admin/dashboard/usage", requireAuth(http.HandlerFunc(dashboardHandlers.GetUsage)))
	} else {
		mux.HandleFunc("GET /api/admin/dashboard/summary", dashboardHandlers.GetSummary)
		mux.HandleFunc("GET /api/admin/dashboard/metrics", dashboardHandlers.GetMetrics)
		mux.HandleFunc("GET /api/admin/dashboard/errors", dashboardHandlers.GetErrors)
		mux.HandleFunc("GET /api/admin/dashboard/usage", dashboardHandlers.GetUsage)
	}

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
			_ = util.WriteJSON(w, http.StatusOK, map[string]any{"projects": srv.tabManager.ListProjectsWithStatus()})
		})))
		mux.Handle("/api/tabs", requireAuth(http.HandlerFunc(srv.handleTabs)))
		mux.Handle("/api/tabs/", requireAuth(http.HandlerFunc(srv.routeTabByID)))
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
			_ = util.WriteJSON(w, http.StatusOK, map[string]any{"projects": srv.tabManager.ListProjectsWithStatus()})
		}))
		mux.Handle("/api/tabs", http.HandlerFunc(srv.handleTabs))
		mux.Handle("/api/tabs/", http.HandlerFunc(srv.routeTabByID))
		mux.Handle("/api/tabs/events", http.HandlerFunc(srv.handleTabEvents))
	}
	// Static files are always public (React handles auth state)
	mux.Handle("/", srv.appMetrics.InstrumentHandler("static", srv.staticHandler()))

	// Apply middlewares: auth warning (adds X-Auth-Warning header when auth disabled), then logging
	handler := sharedserver.AuthWarningMiddleware(cfg.AuthMode)(mux)
	handler = sharedserver.LoggingMiddleware(handler)

	httpSrv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: handler,
	}

	// Start graceful shutdown handler in background
	go sharedserver.GracefulShutdown(httpSrv)

	log.WithFields(log.Fields{
		"port": cfg.Port,
	}).Info("gateway/main: KubeTTY Gateway listening")
	if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.WithFields(log.Fields{
			"error": err.Error(),
			"port":  cfg.Port,
		}).Fatal("gateway/main: server exited unexpectedly")
	}
}

type server struct {
	cfg           config.GatewayConfig
	store         sessions.Store
	authStore     auth.Store
	authMgr       *auth.Manager
	projectStore  *projects.PGStore
	projCtrl      *controller.Controller
	leaderElector *leaderelection.LeaderElector
	upgrader      websocket.Upgrader
	uiFS          fs.FS
	appMetrics    *metrics.AppMetrics
	tabManager    *manager.Manager
	tabStore      tabs.Store
	tabSubsMu     sync.Mutex
	tabSubs       map[string]map[chan []byte]struct{}
	shutdownCtx   context.Context // Context cancelled on graceful shutdown
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
	forceTakeover := r.URL.Query().Get("force") == "true"
	clientID := s.ensureClientID(w, r)
	remoteAddr := r.RemoteAddr

	log.WithFields(log.Fields{
		"tab_id":      tabID,
		"remote_addr": remoteAddr,
		"client_id":   clientID,
		"force":       forceTakeover,
	}).Debug("gateway/main: WebSocket tab connection attempt")

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.WithFields(log.Fields{
			"tab_id": tabID,
			"error":  err.Error(),
		}).Error("gateway/main: WebSocket upgrade failed")
		apierrors.WriteError(w, apierrors.InternalServerError("WebSocket upgrade failed", ""))
		return
	}
	defer func() {
		conn.Close()
		log.WithFields(log.Fields{
			"tab_id":    tabID,
			"client_id": clientID,
		}).Debug("gateway/main: WebSocket connection closed")
	}()

	log.WithFields(log.Fields{
		"tab_id":    tabID,
		"client_id": clientID,
	}).Info("gateway/main: WebSocket connection established")

	// Use shutdown context after WebSocket upgrade - the original request context
	// is no longer valid after hijacking and can cause "invalid Body.Read" panics.
	// Using shutdownCtx allows graceful shutdown to signal active connections.
	if err := s.tabManager.AttachWithOptions(s.shutdownCtx, tabID, clientID, conn, forceTakeover); err != nil {
		log.WithFields(log.Fields{
			"tab_id": tabID,
			"error":  err.Error(),
		}).Error("gateway/main: tab attach failed")
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
			log.WithFields(log.Fields{
				"client_id": clientID,
				"error":     err.Error(),
			}).Error("gateway/main: failed to list tabs")
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
			log.WithFields(log.Fields{
				"client_id":  clientID,
				"project_id": req.ProjectID,
				"error":      err.Error(),
			}).Error("gateway/main: failed to create tab")
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

// routeTabByID dispatches tab-specific requests to the appropriate handler
func (s *server) routeTabByID(w http.ResponseWriter, r *http.Request) {
	// Check if this is a health check request
	if strings.HasSuffix(r.URL.Path, "/health") {
		s.handleTabHealth(w, r)
		return
	}
	// Otherwise, handle as tab deletion
	s.handleTabByID(w, r)
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
		log.WithFields(log.Fields{
			"error": err.Error(),
		}).Error("gateway/main: failed to load tab")
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
		log.WithFields(log.Fields{
			"tab_id": id,
			"error":  err.Error(),
		}).Error("gateway/main: failed to close tab")
		apierrors.WriteError(w, apierrors.InternalServerError("internal error", ""))
		return
	}
	w.WriteHeader(http.StatusNoContent)
	s.broadcastTabDelete(clientID, id)
}

func (s *server) handleTabHealth(w http.ResponseWriter, r *http.Request) {
	if s.tabManager == nil || s.tabStore == nil {
		apierrors.WriteError(w, apierrors.NotFound("gateway disabled", ""))
		return
	}
	if r.Method != http.MethodGet {
		apierrors.WriteError(w, apierrors.ErrorResponse{
			Status:  http.StatusMethodNotAllowed,
			Error:   "method_not_allowed",
			Message: "method not allowed",
		})
		return
	}

	// Extract tab ID from path: /api/tabs/{tabId}/health
	path := strings.TrimPrefix(r.URL.Path, "/api/tabs/")
	path = strings.TrimSuffix(path, "/health")
	tabID := path
	if tabID == "" {
		apierrors.WriteError(w, apierrors.BadRequest("missing tab id", ""))
		return
	}

	clientID := s.ensureClientID(w, r)
	tab, err := s.tabStore.Get(r.Context(), tabID)
	if err != nil {
		if errors.Is(err, tabs.ErrNotFound) {
			apierrors.WriteError(w, apierrors.NotFound("tab not found", ""))
			return
		}
		log.WithFields(log.Fields{
			"error": err.Error(),
		}).Error("gateway/main: failed to load tab")
		apierrors.WriteError(w, apierrors.InternalServerError("internal error", ""))
		return
	}

	if tab.ClientID != clientID {
		apierrors.WriteError(w, apierrors.Forbidden("forbidden", ""))
		return
	}

	// Check if tab has downstream URI
	if tab.DownstreamURI == nil || *tab.DownstreamURI == "" {
		apierrors.WriteError(w, apierrors.ServiceUnavailable("tab not ready", ""))
		return
	}

	// Parse downstream URI and build health check URL
	downstreamURL, err := url.Parse(*tab.DownstreamURI)
	if err != nil {
		log.WithFields(log.Fields{
			"tab_id": tabID,
			"error":  err.Error(),
		}).Error("gateway/main: invalid downstream URI for tab")
		apierrors.WriteError(w, apierrors.InternalServerError("internal error", ""))
		return
	}

	// Build health check URL: convert ws(s):// to http(s):// and use /api/healthz path
	healthURL := &url.URL{
		Scheme: "http",
		Host:   downstreamURL.Host,
		Path:   "/api/healthz",
	}
	if downstreamURL.Scheme == "wss" {
		healthURL.Scheme = "https"
	}

	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: 4 * time.Second,
	}

	// Make request to project pod
	resp, err := client.Get(healthURL.String())
	if err != nil {
		log.WithFields(log.Fields{
			"tab_id": tabID,
			"error":  err.Error(),
		}).Warn("gateway/main: health check failed for tab")
		apierrors.WriteError(w, apierrors.ServiceUnavailable("health check failed", ""))
		return
	}
	defer resp.Body.Close()

	// Copy status code
	w.WriteHeader(resp.StatusCode)

	// Copy response headers
	for k, v := range resp.Header {
		for _, vv := range v {
			w.Header().Add(k, vv)
		}
	}

	// Copy response body
	if _, err := io.Copy(w, resp.Body); err != nil {
		log.WithFields(log.Fields{
			"tab_id": tabID,
			"error":  err.Error(),
		}).Warn("gateway/main: failed to copy health response for tab")
	}
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

	log.WithFields(log.Fields{
		"client_id":   clientID,
		"remote_addr": remoteAddr,
	}).Debug("gateway/main: TabEvents WebSocket connection attempt")

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.WithFields(log.Fields{
			"client_id": clientID,
			"error":     err.Error(),
		}).Error("gateway/main: TabEvents WebSocket upgrade failed")
		apierrors.WriteError(w, apierrors.InternalServerError("WebSocket upgrade failed", ""))
		return
	}
	defer func() {
		conn.Close()
		log.WithFields(log.Fields{
			"client_id": clientID,
		}).Debug("gateway/main: TabEvents WebSocket connection closed")
	}()

	log.WithFields(log.Fields{
		"client_id": clientID,
	}).Info("gateway/main: TabEvents WebSocket connection established")

	ch := s.subscribeTabEvents(clientID)
	defer s.unsubscribeTabEvents(clientID, ch)
	s.broadcastTabSnapshot(clientID)
	// Use a done channel tied to connection close instead of r.Context()
	// because the original request context is invalid after WebSocket hijack
	done := make(chan struct{})
	go func() {
		// This goroutine waits for the connection to close (ReadMessage will fail)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				close(done)
				return
			}
		}
	}()
	for {
		select {
		case <-done:
			log.WithFields(log.Fields{
				"client_id": clientID,
			}).Debug("gateway/main: TabEvents connection closed by client")
			return
		case msg, ok := <-ch:
			if !ok {
				log.WithFields(log.Fields{
					"client_id": clientID,
				}).Debug("gateway/main: TabEvents channel closed")
				return
			}
			if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				log.WithFields(log.Fields{
					"client_id": clientID,
					"error":     err.Error(),
				}).Warn("gateway/main: TabEvents write error")
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

func (s *server) handleTabMetricsUpdate(tabID string, metrics gatewaymetrics.TabMetrics) {
	s.broadcastMetricsUpdate(tabID, metrics)
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
		log.WithFields(log.Fields{
			"client_id": clientID,
			"error":     err.Error(),
		}).Warn("gateway/main: failed to list tabs snapshot")
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

func (s *server) broadcastMetricsUpdate(tabID string, metrics gatewaymetrics.TabMetrics) {
	// Get tab to find the client ID
	if s.tabStore == nil {
		return
	}
	tab, err := s.tabStore.Get(context.Background(), tabID)
	if err != nil {
		return
	}
	s.sendTabEvent(tab.ClientID, map[string]any{
		"type":    "metrics",
		"tabId":   tabID,
		"metrics": metrics,
	})
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
