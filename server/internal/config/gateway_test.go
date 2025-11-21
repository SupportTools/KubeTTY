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
			name: "missing SESSION_ID",
			envVars: map[string]string{
				"CNPG_HOST":     "localhost",
				"CNPG_DATABASE": "kubetty",
				"CNPG_USER":     "user",
				"CNPG_PASSWORD": "pass",
			},
			wantErr: true,
			errMsg:  "SESSION_ID is required",
		},
		{
			name: "missing CNPG_HOST",
			envVars: map[string]string{
				"SESSION_ID":    "test-session",
				"CNPG_DATABASE": "kubetty",
				"CNPG_USER":     "user",
				"CNPG_PASSWORD": "pass",
			},
			wantErr: true,
			errMsg:  "CNPG_* env vars are required",
		},
		{
			name: "missing CNPG_DATABASE",
			envVars: map[string]string{
				"SESSION_ID":    "test-session",
				"CNPG_HOST":     "localhost",
				"CNPG_USER":     "user",
				"CNPG_PASSWORD": "pass",
			},
			wantErr: true,
			errMsg:  "CNPG_* env vars are required",
		},
		{
			name: "missing CNPG_USER",
			envVars: map[string]string{
				"SESSION_ID":    "test-session",
				"CNPG_HOST":     "localhost",
				"CNPG_DATABASE": "kubetty",
				"CNPG_PASSWORD": "pass",
			},
			wantErr: true,
			errMsg:  "CNPG_* env vars are required",
		},
		{
			name: "missing CNPG_PASSWORD",
			envVars: map[string]string{
				"SESSION_ID":    "test-session",
				"CNPG_HOST":     "localhost",
				"CNPG_DATABASE": "kubetty",
				"CNPG_USER":     "user",
			},
			wantErr: true,
			errMsg:  "CNPG_* env vars are required",
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
