package config

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestLoadGatewayConfig(t *testing.T) {
	tests := []struct {
		name     string
		envVars  map[string]string
		wantErr  bool
		errMsg   string
		validate func(*testing.T, GatewayConfig)
	}{
		{
			name: "valid gateway config with all fields",
			envVars: map[string]string{
				"SESSION_ID":           "test-session-123",
				"CNPG_HOST":            "postgres.example.com",
				"CNPG_PORT":            "5432",
				"CNPG_DATABASE":        "kubetty_db",
				"CNPG_USER":            "kubetty_user",
				"CNPG_PASSWORD":        "secret123",
				"PORT":                 "9090",
				"PROJECT_CATALOG_PATH": "",
				"AUTH_MODE":            "local",
				"AUTH_JWT_SECRET":      "jwt-secret-key",
				"AUTH_ACCESS_TTL":      "30m",
				"AUTH_REFRESH_TTL":     "336h", // 14 days
				"AUTH_ISSUER":          "kubetty-gateway",
				"AUTH_COOKIE_DOMAIN":   ".example.com",
				"AUTH_COOKIE_SECURE":   "true",
				"TAB_IDLE_TIMEOUT":     "3h",
			},
			wantErr: false,
			validate: func(t *testing.T, cfg GatewayConfig) {
				if cfg.Port != "9090" {
					t.Errorf("Port = %s, want 9090", cfg.Port)
				}
				if cfg.SessionID != "test-session-123" {
					t.Errorf("SessionID = %s, want test-session-123", cfg.SessionID)
				}
				if cfg.CNPGHost != "postgres.example.com" {
					t.Errorf("CNPGHost = %s, want postgres.example.com", cfg.CNPGHost)
				}
				if cfg.AuthMode != "local" {
					t.Errorf("AuthMode = %s, want local", cfg.AuthMode)
				}
				if cfg.AuthJWTSecret != "jwt-secret-key" {
					t.Errorf("AuthJWTSecret = %s, want jwt-secret-key", cfg.AuthJWTSecret)
				}
				if cfg.AuthAccessTTL != 30*time.Minute {
					t.Errorf("AuthAccessTTL = %v, want 30m", cfg.AuthAccessTTL)
				}
				if cfg.AuthRefreshTTL != 336*time.Hour {
					t.Errorf("AuthRefreshTTL = %v, want 336h", cfg.AuthRefreshTTL)
				}
				if cfg.AuthIssuer != "kubetty-gateway" {
					t.Errorf("AuthIssuer = %s, want kubetty-gateway", cfg.AuthIssuer)
				}
				if cfg.AuthCookieDomain != ".example.com" {
					t.Errorf("AuthCookieDomain = %s, want .example.com", cfg.AuthCookieDomain)
				}
				if !cfg.AuthCookieSecure {
					t.Error("AuthCookieSecure = false, want true")
				}
				if cfg.TabIdleTimeout != 3*time.Hour {
					t.Errorf("TabIdleTimeout = %v, want 3h", cfg.TabIdleTimeout)
				}
			},
		},
		{
			name: "valid config with defaults",
			envVars: map[string]string{
				"SESSION_ID":    "test-session-456",
				"CNPG_HOST":     "localhost",
				"CNPG_DATABASE": "kubetty",
				"CNPG_USER":     "user",
				"CNPG_PASSWORD": "pass",
			},
			wantErr: false,
			validate: func(t *testing.T, cfg GatewayConfig) {
				if cfg.Port != "8080" {
					t.Errorf("Port = %s, want 8080 (default)", cfg.Port)
				}
				if cfg.CNPGPort != "5432" {
					t.Errorf("CNPGPort = %s, want 5432 (default)", cfg.CNPGPort)
				}
				if cfg.AuthMode != "disabled" {
					t.Errorf("AuthMode = %s, want disabled (default)", cfg.AuthMode)
				}
				if cfg.AuthIssuer != "kubetty" {
					t.Errorf("AuthIssuer = %s, want kubetty (default)", cfg.AuthIssuer)
				}
				if !cfg.AuthCookieSecure {
					t.Error("AuthCookieSecure = false, want true (default)")
				}
				if cfg.TabIdleTimeout != 2*time.Hour {
					t.Errorf("TabIdleTimeout = %v, want 2h (default)", cfg.TabIdleTimeout)
				}
				if cfg.LogRetentionHours != 24*30 {
					t.Errorf("LogRetentionHours = %d, want 720 (default)", cfg.LogRetentionHours)
				}
				if cfg.LogMaxPerSession != 5000 {
					t.Errorf("LogMaxPerSession = %d, want 5000 (default)", cfg.LogMaxPerSession)
				}
			},
		},
		{
			name: "valid config with auth disabled",
			envVars: map[string]string{
				"SESSION_ID":    "test-session-789",
				"CNPG_HOST":     "localhost",
				"CNPG_DATABASE": "kubetty",
				"CNPG_USER":     "user",
				"CNPG_PASSWORD": "pass",
				"AUTH_MODE":     "disabled",
			},
			wantErr: false,
			validate: func(t *testing.T, cfg GatewayConfig) {
				if cfg.AuthMode != "disabled" {
					t.Errorf("AuthMode = %s, want disabled", cfg.AuthMode)
				}
			},
		},
		{
			name: "missing CNPG_HOST",
			envVars: map[string]string{
				"CNPG_DATABASE": "kubetty",
				"CNPG_USER":     "user",
				"CNPG_PASSWORD": "pass",
			},
			wantErr: true,
			errMsg:  "CNPG_* env vars are required for gateway mode",
		},
		{
			name: "missing CNPG_DATABASE",
			envVars: map[string]string{
				"CNPG_HOST":     "localhost",
				"CNPG_USER":     "user",
				"CNPG_PASSWORD": "pass",
			},
			wantErr: true,
			errMsg:  "CNPG_* env vars are required for gateway mode",
		},
		{
			name: "missing CNPG_USER",
			envVars: map[string]string{
				"CNPG_HOST":     "localhost",
				"CNPG_DATABASE": "kubetty",
				"CNPG_PASSWORD": "pass",
			},
			wantErr: true,
			errMsg:  "CNPG_* env vars are required for gateway mode",
		},
		{
			name: "missing CNPG_PASSWORD",
			envVars: map[string]string{
				"CNPG_HOST":     "localhost",
				"CNPG_DATABASE": "kubetty",
				"CNPG_USER":     "user",
			},
			wantErr: true,
			errMsg:  "CNPG_* env vars are required for gateway mode",
		},
		{
			name: "auth mode local without JWT secret",
			envVars: map[string]string{
				"SESSION_ID":    "test-session",
				"CNPG_HOST":     "localhost",
				"CNPG_DATABASE": "kubetty",
				"CNPG_USER":     "user",
				"CNPG_PASSWORD": "pass",
				"AUTH_MODE":     "local",
			},
			wantErr: true,
			errMsg:  "JWT secret is required",
		},
		{
			name: "auth mode local with zero access TTL",
			envVars: map[string]string{
				"SESSION_ID":      "test-session",
				"CNPG_HOST":       "localhost",
				"CNPG_DATABASE":   "kubetty",
				"CNPG_USER":       "user",
				"CNPG_PASSWORD":   "pass",
				"AUTH_MODE":       "local",
				"AUTH_JWT_SECRET": "secret",
				"AUTH_ACCESS_TTL": "0",
			},
			wantErr: true,
			errMsg:  "access token TTL must be greater than 0",
		},
		{
			name: "auth mode local with zero refresh TTL",
			envVars: map[string]string{
				"SESSION_ID":       "test-session",
				"CNPG_HOST":        "localhost",
				"CNPG_DATABASE":    "kubetty",
				"CNPG_USER":        "user",
				"CNPG_PASSWORD":    "pass",
				"AUTH_MODE":        "local",
				"AUTH_JWT_SECRET":  "secret",
				"AUTH_REFRESH_TTL": "0",
			},
			wantErr: true,
			errMsg:  "refresh token TTL must be greater than 0",
		},
		{
			name: "unsupported auth mode",
			envVars: map[string]string{
				"SESSION_ID":    "test-session",
				"CNPG_HOST":     "localhost",
				"CNPG_DATABASE": "kubetty",
				"CNPG_USER":     "user",
				"CNPG_PASSWORD": "pass",
				"AUTH_MODE":     "oauth",
			},
			wantErr: true,
			errMsg:  "unsupported auth mode",
		},
		{
			name: "deployment ID defaults to session ID",
			envVars: map[string]string{
				"SESSION_ID":    "test-session-deployment",
				"CNPG_HOST":     "localhost",
				"CNPG_DATABASE": "kubetty",
				"CNPG_USER":     "user",
				"CNPG_PASSWORD": "pass",
			},
			wantErr: false,
			validate: func(t *testing.T, cfg GatewayConfig) {
				if cfg.DeploymentID != "test-session-deployment" {
					t.Errorf("DeploymentID = %s, want test-session-deployment (defaulted from SessionID)", cfg.DeploymentID)
				}
			},
		},
		{
			name: "custom deployment ID",
			envVars: map[string]string{
				"SESSION_ID":    "test-session",
				"DEPLOYMENT_ID": "custom-deployment-123",
				"CNPG_HOST":     "localhost",
				"CNPG_DATABASE": "kubetty",
				"CNPG_USER":     "user",
				"CNPG_PASSWORD": "pass",
			},
			wantErr: false,
			validate: func(t *testing.T, cfg GatewayConfig) {
				if cfg.DeploymentID != "custom-deployment-123" {
					t.Errorf("DeploymentID = %s, want custom-deployment-123", cfg.DeploymentID)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear environment
			os.Clearenv()

			// Set test environment variables
			for key, val := range tt.envVars {
				os.Setenv(key, val)
			}

			// Load config
			cfg, err := LoadGatewayConfig()

			// Check error expectation
			if tt.wantErr {
				if err == nil {
					t.Errorf("LoadGatewayConfig() expected error, got nil")
					return
				}
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("LoadGatewayConfig() error = %v, want error containing %q", err, tt.errMsg)
				}
				return
			}

			// No error expected
			if err != nil {
				t.Errorf("LoadGatewayConfig() unexpected error = %v", err)
				return
			}

			// Run validation function
			if tt.validate != nil {
				tt.validate(t, cfg)
			}
		})
	}
}

func TestGatewayConfig_ConnString(t *testing.T) {
	cfg := GatewayConfig{
		CommonConfig: CommonConfig{
			CNPGHost:     "postgres.example.com",
			CNPGPort:     "5432",
			CNPGDatabase: "kubetty_db",
			CNPGUser:     "kubetty_user",
			CNPGPassword: "secret123",
		},
	}

	connStr := cfg.ConnString()
	expected := "host=postgres.example.com port=5432 dbname=kubetty_db user=kubetty_user password=secret123 sslmode=disable"

	if connStr != expected {
		t.Errorf("ConnString() = %q, want %q", connStr, expected)
	}
}

func TestGatewayConfig_AuthMethods(t *testing.T) {
	tests := []struct {
		name       string
		config     GatewayConfig
		wantMode   string
		wantDomain string
		wantSecure bool
	}{
		{
			name: "local auth with secure cookie",
			config: GatewayConfig{
				AuthMode:         "local",
				AuthCookieDomain: ".example.com",
				AuthCookieSecure: true,
			},
			wantMode:   "local",
			wantDomain: ".example.com",
			wantSecure: true,
		},
		{
			name: "disabled auth",
			config: GatewayConfig{
				AuthMode:         "disabled",
				AuthCookieDomain: "",
				AuthCookieSecure: false,
			},
			wantMode:   "disabled",
			wantDomain: "",
			wantSecure: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.config.GetAuthMode(); got != tt.wantMode {
				t.Errorf("GetAuthMode() = %v, want %v", got, tt.wantMode)
			}
			if got := tt.config.GetAuthCookieDomain(); got != tt.wantDomain {
				t.Errorf("GetAuthCookieDomain() = %v, want %v", got, tt.wantDomain)
			}
			if got := tt.config.GetAuthCookieSecure(); got != tt.wantSecure {
				t.Errorf("GetAuthCookieSecure() = %v, want %v", got, tt.wantSecure)
			}
		})
	}
}

func TestLoadGatewayConfig_ControllerConfig(t *testing.T) {
	tests := []struct {
		name     string
		envVars  map[string]string
		wantErr  bool
		errMsg   string
		validate func(*testing.T, GatewayConfig)
	}{
		{
			name: "controller config defaults when disabled",
			envVars: map[string]string{
				"SESSION_ID":    "test-session",
				"CNPG_HOST":     "localhost",
				"CNPG_DATABASE": "kubetty",
				"CNPG_USER":     "user",
				"CNPG_PASSWORD": "pass",
			},
			wantErr: false,
			validate: func(t *testing.T, cfg GatewayConfig) {
				if cfg.Controller.Enabled {
					t.Error("Controller.Enabled = true, want false (default)")
				}
				if cfg.Controller.ResourcePrefix != "kubetty-project-" {
					t.Errorf("Controller.ResourcePrefix = %s, want kubetty-project-", cfg.Controller.ResourcePrefix)
				}
				if cfg.Controller.ReconcileInterval != 30*time.Second {
					t.Errorf("Controller.ReconcileInterval = %v, want 30s", cfg.Controller.ReconcileInterval)
				}
				if cfg.Controller.HealthCheckInterval != 60*time.Second {
					t.Errorf("Controller.HealthCheckInterval = %v, want 60s", cfg.Controller.HealthCheckInterval)
				}
			},
		},
		{
			name: "controller config with all fields set",
			envVars: map[string]string{
				"SESSION_ID":            "test-session",
				"CNPG_HOST":             "localhost",
				"CNPG_DATABASE":         "kubetty",
				"CNPG_USER":             "user",
				"CNPG_PASSWORD":         "pass",
				"CONTROLLER_ENABLED":    "true",
				"PROJECTS_NAMESPACE":    "kubetty-projects-dev",
				"RESOURCE_PREFIX":       "kt-proj-",
				"RECONCILE_INTERVAL":    "45s",
				"HEALTH_CHECK_INTERVAL": "120s",
			},
			wantErr: false,
			validate: func(t *testing.T, cfg GatewayConfig) {
				if !cfg.Controller.Enabled {
					t.Error("Controller.Enabled = false, want true")
				}
				if cfg.Controller.ProjectsNamespace != "kubetty-projects-dev" {
					t.Errorf("Controller.ProjectsNamespace = %s, want kubetty-projects-dev", cfg.Controller.ProjectsNamespace)
				}
				if cfg.Controller.ResourcePrefix != "kt-proj-" {
					t.Errorf("Controller.ResourcePrefix = %s, want kt-proj-", cfg.Controller.ResourcePrefix)
				}
				if cfg.Controller.ReconcileInterval != 45*time.Second {
					t.Errorf("Controller.ReconcileInterval = %v, want 45s", cfg.Controller.ReconcileInterval)
				}
				if cfg.Controller.HealthCheckInterval != 120*time.Second {
					t.Errorf("Controller.HealthCheckInterval = %v, want 120s", cfg.Controller.HealthCheckInterval)
				}
			},
		},
		{
			name: "controller enabled without namespace fails",
			envVars: map[string]string{
				"SESSION_ID":         "test-session",
				"CNPG_HOST":          "localhost",
				"CNPG_DATABASE":      "kubetty",
				"CNPG_USER":          "user",
				"CNPG_PASSWORD":      "pass",
				"CONTROLLER_ENABLED": "true",
			},
			wantErr: true,
			errMsg:  "PROJECTS_NAMESPACE is required when CONTROLLER_ENABLED=true",
		},
		// Note: Empty resource prefix cannot be tested via env vars because
		// GetEnv treats empty string as unset and returns the default.
		// The validation for empty prefix is tested in TestValidateController.
		{
			name: "controller disabled allows missing namespace",
			envVars: map[string]string{
				"SESSION_ID":         "test-session",
				"CNPG_HOST":          "localhost",
				"CNPG_DATABASE":      "kubetty",
				"CNPG_USER":          "user",
				"CNPG_PASSWORD":      "pass",
				"CONTROLLER_ENABLED": "false",
			},
			wantErr: false,
			validate: func(t *testing.T, cfg GatewayConfig) {
				if cfg.Controller.Enabled {
					t.Error("Controller.Enabled = true, want false")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Clearenv()

			for key, val := range tt.envVars {
				os.Setenv(key, val)
			}

			cfg, err := LoadGatewayConfig()

			if tt.wantErr {
				if err == nil {
					t.Errorf("LoadGatewayConfig() expected error, got nil")
					return
				}
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("LoadGatewayConfig() error = %v, want error containing %q", err, tt.errMsg)
				}
				return
			}

			if err != nil {
				t.Errorf("LoadGatewayConfig() unexpected error = %v", err)
				return
			}

			if tt.validate != nil {
				tt.validate(t, cfg)
			}
		})
	}
}

func TestControllerConfig_ParseEnvironment(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		want      string
	}{
		{
			name:      "dev environment",
			namespace: "kubetty-projects-dev",
			want:      "dev",
		},
		{
			name:      "prd environment",
			namespace: "kubetty-projects-prd",
			want:      "prd",
		},
		{
			name:      "staging environment",
			namespace: "kubetty-projects-staging",
			want:      "staging",
		},
		{
			name:      "simple namespace",
			namespace: "production",
			want:      "production",
		},
		{
			name:      "empty namespace",
			namespace: "",
			want:      "",
		},
		{
			name:      "namespace with multiple dashes",
			namespace: "my-company-kubetty-projects-test",
			want:      "test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := ControllerConfig{
				ProjectsNamespace: tt.namespace,
			}
			if got := cfg.ParseEnvironment(); got != tt.want {
				t.Errorf("ParseEnvironment() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ---- ExecMode tests ----

func TestExecModeConstants(t *testing.T) {
	if ExecModeWebSocket != "websocket" {
		t.Errorf("ExecModeWebSocket = %q, want %q", ExecModeWebSocket, "websocket")
	}
	if ExecModeExec != "exec" {
		t.Errorf("ExecModeExec = %q, want %q", ExecModeExec, "exec")
	}
}

func TestParseExecMode(t *testing.T) {
	tests := []struct {
		input    string
		expected ExecModeType
	}{
		{"exec", ExecModeExec},
		{"EXEC", ExecModeExec},
		{"Exec", ExecModeExec},
		{"kubectl", ExecModeExec},
		{"KUBECTL", ExecModeExec},
		{"websocket", ExecModeWebSocket},
		{"WEBSOCKET", ExecModeWebSocket},
		{"ws", ExecModeWebSocket},
		{"WS", ExecModeWebSocket},
		{"", ExecModeWebSocket},
		{"unknown", ExecModeWebSocket},
		{"  exec  ", ExecModeExec},
		{"  websocket  ", ExecModeWebSocket},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := parseExecMode(tt.input)
			if result != tt.expected {
				t.Errorf("parseExecMode(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// ---- parseImagePullSecrets tests ----

func TestParseImagePullSecrets(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: nil,
		},
		{
			name:     "single secret",
			input:    "harbor-supporttools",
			expected: []string{"harbor-supporttools"},
		},
		{
			name:     "multiple secrets",
			input:    "harbor-supporttools,docker-registry,gcr-secret",
			expected: []string{"harbor-supporttools", "docker-registry", "gcr-secret"},
		},
		{
			name:     "secrets with spaces",
			input:    "  secret1  ,  secret2  ,  secret3  ",
			expected: []string{"secret1", "secret2", "secret3"},
		},
		{
			name:     "empty entries filtered",
			input:    "secret1,,secret2,",
			expected: []string{"secret1", "secret2"},
		},
		{
			name:     "only commas",
			input:    ",,,",
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseImagePullSecrets(tt.input)
			if tt.expected == nil {
				if result != nil {
					t.Errorf("parseImagePullSecrets(%q) = %v, want nil", tt.input, result)
				}
				return
			}
			if len(result) != len(tt.expected) {
				t.Errorf("parseImagePullSecrets(%q) length = %d, want %d", tt.input, len(result), len(tt.expected))
				return
			}
			for i, v := range result {
				if v != tt.expected[i] {
					t.Errorf("parseImagePullSecrets(%q)[%d] = %q, want %q", tt.input, i, v, tt.expected[i])
				}
			}
		})
	}
}

// ---- Metrics config tests ----

func TestLoadGatewayConfig_MetricsConfig(t *testing.T) {
	tests := []struct {
		name     string
		envVars  map[string]string
		wantErr  bool
		validate func(*testing.T, GatewayConfig)
	}{
		{
			name: "metrics enabled by default",
			envVars: map[string]string{
				"SESSION_ID":    "test-session",
				"CNPG_HOST":     "localhost",
				"CNPG_DATABASE": "kubetty",
				"CNPG_USER":     "user",
				"CNPG_PASSWORD": "pass",
			},
			wantErr: false,
			validate: func(t *testing.T, cfg GatewayConfig) {
				if !cfg.MetricsEnabled {
					t.Error("MetricsEnabled = false, want true (default)")
				}
				if cfg.MetricsInterval != 15*time.Second {
					t.Errorf("MetricsInterval = %v, want 15s (default)", cfg.MetricsInterval)
				}
			},
		},
		{
			name: "metrics disabled explicitly",
			envVars: map[string]string{
				"SESSION_ID":      "test-session",
				"CNPG_HOST":       "localhost",
				"CNPG_DATABASE":   "kubetty",
				"CNPG_USER":       "user",
				"CNPG_PASSWORD":   "pass",
				"METRICS_ENABLED": "false",
			},
			wantErr: false,
			validate: func(t *testing.T, cfg GatewayConfig) {
				if cfg.MetricsEnabled {
					t.Error("MetricsEnabled = true, want false")
				}
			},
		},
		{
			name: "custom metrics interval",
			envVars: map[string]string{
				"SESSION_ID":       "test-session",
				"CNPG_HOST":        "localhost",
				"CNPG_DATABASE":    "kubetty",
				"CNPG_USER":        "user",
				"CNPG_PASSWORD":    "pass",
				"METRICS_INTERVAL": "30s",
			},
			wantErr: false,
			validate: func(t *testing.T, cfg GatewayConfig) {
				if cfg.MetricsInterval != 30*time.Second {
					t.Errorf("MetricsInterval = %v, want 30s", cfg.MetricsInterval)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Clearenv()

			for key, val := range tt.envVars {
				os.Setenv(key, val)
			}

			cfg, err := LoadGatewayConfig()

			if tt.wantErr {
				if err == nil {
					t.Errorf("LoadGatewayConfig() expected error, got nil")
					return
				}
				return
			}

			if err != nil {
				t.Errorf("LoadGatewayConfig() unexpected error = %v", err)
				return
			}

			if tt.validate != nil {
				tt.validate(t, cfg)
			}
		})
	}
}

// ---- ExecMode config tests ----

func TestLoadGatewayConfig_ExecMode(t *testing.T) {
	tests := []struct {
		name     string
		envVars  map[string]string
		wantMode ExecModeType
	}{
		{
			name: "default is websocket",
			envVars: map[string]string{
				"SESSION_ID":    "test-session",
				"CNPG_HOST":     "localhost",
				"CNPG_DATABASE": "kubetty",
				"CNPG_USER":     "user",
				"CNPG_PASSWORD": "pass",
			},
			wantMode: ExecModeWebSocket,
		},
		{
			name: "explicit websocket",
			envVars: map[string]string{
				"SESSION_ID":        "test-session",
				"CNPG_HOST":         "localhost",
				"CNPG_DATABASE":     "kubetty",
				"CNPG_USER":         "user",
				"CNPG_PASSWORD":     "pass",
				"KUBETTY_EXEC_MODE": "websocket",
			},
			wantMode: ExecModeWebSocket,
		},
		{
			name: "exec mode",
			envVars: map[string]string{
				"SESSION_ID":        "test-session",
				"CNPG_HOST":         "localhost",
				"CNPG_DATABASE":     "kubetty",
				"CNPG_USER":         "user",
				"CNPG_PASSWORD":     "pass",
				"KUBETTY_EXEC_MODE": "exec",
			},
			wantMode: ExecModeExec,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Clearenv()

			for key, val := range tt.envVars {
				os.Setenv(key, val)
			}

			cfg, err := LoadGatewayConfig()
			if err != nil {
				t.Fatalf("LoadGatewayConfig() error = %v", err)
			}

			if cfg.ExecMode != tt.wantMode {
				t.Errorf("ExecMode = %q, want %q", cfg.ExecMode, tt.wantMode)
			}
		})
	}
}

// ---- RecommendedImageTag tests ----

func TestLoadGatewayConfig_RecommendedImageTag(t *testing.T) {
	tests := []struct {
		name    string
		envVars map[string]string
		want    string
	}{
		{
			name: "default is latest",
			envVars: map[string]string{
				"SESSION_ID":    "test-session",
				"CNPG_HOST":     "localhost",
				"CNPG_DATABASE": "kubetty",
				"CNPG_USER":     "user",
				"CNPG_PASSWORD": "pass",
			},
			want: "latest",
		},
		{
			name: "custom tag",
			envVars: map[string]string{
				"SESSION_ID":            "test-session",
				"CNPG_HOST":             "localhost",
				"CNPG_DATABASE":         "kubetty",
				"CNPG_USER":             "user",
				"CNPG_PASSWORD":         "pass",
				"RECOMMENDED_IMAGE_TAG": "v1.2.3",
			},
			want: "v1.2.3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Clearenv()

			for key, val := range tt.envVars {
				os.Setenv(key, val)
			}

			cfg, err := LoadGatewayConfig()
			if err != nil {
				t.Fatalf("LoadGatewayConfig() error = %v", err)
			}

			if cfg.RecommendedImageTag != tt.want {
				t.Errorf("RecommendedImageTag = %q, want %q", cfg.RecommendedImageTag, tt.want)
			}
		})
	}
}

// ---- ControllerConfig ImagePullSecrets and EnvSecretName tests ----

func TestLoadGatewayConfig_ControllerImagePullSecrets(t *testing.T) {
	tests := []struct {
		name     string
		envVars  map[string]string
		expected []string
	}{
		{
			name: "default image pull secrets",
			envVars: map[string]string{
				"SESSION_ID":         "test-session",
				"CNPG_HOST":          "localhost",
				"CNPG_DATABASE":      "kubetty",
				"CNPG_USER":          "user",
				"CNPG_PASSWORD":      "pass",
				"CONTROLLER_ENABLED": "true",
				"PROJECTS_NAMESPACE": "kubetty-projects-dev",
			},
			expected: []string{"harbor-supporttools"},
		},
		{
			name: "custom image pull secrets",
			envVars: map[string]string{
				"SESSION_ID":         "test-session",
				"CNPG_HOST":          "localhost",
				"CNPG_DATABASE":      "kubetty",
				"CNPG_USER":          "user",
				"CNPG_PASSWORD":      "pass",
				"CONTROLLER_ENABLED": "true",
				"PROJECTS_NAMESPACE": "kubetty-projects-dev",
				"IMAGE_PULL_SECRETS": "secret1,secret2",
			},
			expected: []string{"secret1", "secret2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Clearenv()

			for key, val := range tt.envVars {
				os.Setenv(key, val)
			}

			cfg, err := LoadGatewayConfig()
			if err != nil {
				t.Fatalf("LoadGatewayConfig() error = %v", err)
			}

			if len(cfg.Controller.ImagePullSecrets) != len(tt.expected) {
				t.Errorf("ImagePullSecrets length = %d, want %d", len(cfg.Controller.ImagePullSecrets), len(tt.expected))
				return
			}
			for i, v := range cfg.Controller.ImagePullSecrets {
				if v != tt.expected[i] {
					t.Errorf("ImagePullSecrets[%d] = %q, want %q", i, v, tt.expected[i])
				}
			}
		})
	}
}

func TestLoadGatewayConfig_ControllerEnvSecretName(t *testing.T) {
	tests := []struct {
		name     string
		envVars  map[string]string
		expected string
	}{
		{
			name: "default env secret name",
			envVars: map[string]string{
				"SESSION_ID":         "test-session",
				"CNPG_HOST":          "localhost",
				"CNPG_DATABASE":      "kubetty",
				"CNPG_USER":          "user",
				"CNPG_PASSWORD":      "pass",
				"CONTROLLER_ENABLED": "true",
				"PROJECTS_NAMESPACE": "kubetty-projects-dev",
			},
			expected: "env-secrets",
		},
		{
			name: "custom env secret name",
			envVars: map[string]string{
				"SESSION_ID":         "test-session",
				"CNPG_HOST":          "localhost",
				"CNPG_DATABASE":      "kubetty",
				"CNPG_USER":          "user",
				"CNPG_PASSWORD":      "pass",
				"CONTROLLER_ENABLED": "true",
				"PROJECTS_NAMESPACE": "kubetty-projects-dev",
				"ENV_SECRET_NAME":    "my-env-secrets",
			},
			expected: "my-env-secrets",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Clearenv()

			for key, val := range tt.envVars {
				os.Setenv(key, val)
			}

			cfg, err := LoadGatewayConfig()
			if err != nil {
				t.Fatalf("LoadGatewayConfig() error = %v", err)
			}

			if cfg.Controller.EnvSecretName != tt.expected {
				t.Errorf("EnvSecretName = %q, want %q", cfg.Controller.EnvSecretName, tt.expected)
			}
		})
	}
}
