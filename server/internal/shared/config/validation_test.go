package config

import (
	"strings"
	"testing"
	"time"
)

func TestValidateAuth(t *testing.T) {
	tests := []struct {
		name    string
		cfg     AuthConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid local auth with all fields",
			cfg: AuthConfig{
				Mode:       "local",
				JWTSecret:  "secret123",
				AccessTTL:  15 * time.Minute,
				RefreshTTL: 7 * 24 * time.Hour,
			},
			wantErr: false,
		},
		{
			name: "valid disabled auth",
			cfg: AuthConfig{
				Mode: "disabled",
			},
			wantErr: false,
		},
		{
			name: "empty mode rejected - requires explicit setting",
			cfg: AuthConfig{
				Mode: "",
			},
			wantErr: true,
			errMsg:  "AUTH_MODE must be explicitly set",
		},
		{
			name: "missing JWT secret for local mode",
			cfg: AuthConfig{
				Mode:       "local",
				JWTSecret:  "",
				AccessTTL:  15 * time.Minute,
				RefreshTTL: 7 * 24 * time.Hour,
			},
			wantErr: true,
			errMsg:  "JWT secret is required",
		},
		{
			name: "zero access TTL for local mode",
			cfg: AuthConfig{
				Mode:       "local",
				JWTSecret:  "secret123",
				AccessTTL:  0,
				RefreshTTL: 7 * 24 * time.Hour,
			},
			wantErr: true,
			errMsg:  "access token TTL must be greater than 0",
		},
		{
			name: "zero refresh TTL for local mode",
			cfg: AuthConfig{
				Mode:       "local",
				JWTSecret:  "secret123",
				AccessTTL:  15 * time.Minute,
				RefreshTTL: 0,
			},
			wantErr: true,
			errMsg:  "refresh token TTL must be greater than 0",
		},
		{
			name: "unsupported auth mode",
			cfg: AuthConfig{
				Mode: "oauth",
			},
			wantErr: true,
			errMsg:  "unsupported auth mode",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateAuth(tt.cfg)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ValidateAuth() expected error, got nil")
					return
				}
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("ValidateAuth() error = %v, want error containing %q", err, tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("ValidateAuth() unexpected error = %v", err)
				}
			}
		})
	}
}

func TestValidateDatabase(t *testing.T) {
	tests := []struct {
		name    string
		cfg     DatabaseConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid database config",
			cfg: DatabaseConfig{
				Host:     "localhost",
				Port:     "5432",
				Database: "kubetty",
				User:     "kubetty_user",
				Password: "secret",
			},
			wantErr: false,
		},
		{
			name: "missing host",
			cfg: DatabaseConfig{
				Port:     "5432",
				Database: "kubetty",
				User:     "kubetty_user",
				Password: "secret",
			},
			wantErr: true,
			errMsg:  "database host is required",
		},
		{
			name: "missing port",
			cfg: DatabaseConfig{
				Host:     "localhost",
				Database: "kubetty",
				User:     "kubetty_user",
				Password: "secret",
			},
			wantErr: true,
			errMsg:  "database port is required",
		},
		{
			name: "missing database name",
			cfg: DatabaseConfig{
				Host:     "localhost",
				Port:     "5432",
				User:     "kubetty_user",
				Password: "secret",
			},
			wantErr: true,
			errMsg:  "database name is required",
		},
		{
			name: "missing user",
			cfg: DatabaseConfig{
				Host:     "localhost",
				Port:     "5432",
				Database: "kubetty",
				Password: "secret",
			},
			wantErr: true,
			errMsg:  "database user is required",
		},
		{
			name: "missing password",
			cfg: DatabaseConfig{
				Host:     "localhost",
				Port:     "5432",
				Database: "kubetty",
				User:     "kubetty_user",
			},
			wantErr: true,
			errMsg:  "database password is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDatabase(tt.cfg)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ValidateDatabase() expected error, got nil")
					return
				}
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("ValidateDatabase() error = %v, want error containing %q", err, tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("ValidateDatabase() unexpected error = %v", err)
				}
			}
		})
	}
}

func TestValidateSession(t *testing.T) {
	tests := []struct {
		name    string
		cfg     SessionConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid session config",
			cfg: SessionConfig{
				MaxInactiveMinutes: 1440,
				PruneIntervalHours: 1,
				TrimMaxEntries:     10000,
			},
			wantErr: false,
		},
		{
			name: "valid with zero values",
			cfg: SessionConfig{
				MaxInactiveMinutes: 0,
				PruneIntervalHours: 0,
				TrimMaxEntries:     0,
			},
			wantErr: false,
		},
		{
			name: "negative max inactive minutes",
			cfg: SessionConfig{
				MaxInactiveMinutes: -1,
				PruneIntervalHours: 1,
				TrimMaxEntries:     10000,
			},
			wantErr: true,
			errMsg:  "max inactive minutes cannot be negative",
		},
		{
			name: "negative prune interval hours",
			cfg: SessionConfig{
				MaxInactiveMinutes: 1440,
				PruneIntervalHours: -1,
				TrimMaxEntries:     10000,
			},
			wantErr: true,
			errMsg:  "prune interval hours cannot be negative",
		},
		{
			name: "negative trim max entries",
			cfg: SessionConfig{
				MaxInactiveMinutes: 1440,
				PruneIntervalHours: 1,
				TrimMaxEntries:     -1,
			},
			wantErr: true,
			errMsg:  "trim max entries cannot be negative",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSession(tt.cfg)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ValidateSession() expected error, got nil")
					return
				}
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("ValidateSession() error = %v, want error containing %q", err, tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("ValidateSession() unexpected error = %v", err)
				}
			}
		})
	}
}

func TestValidateServer(t *testing.T) {
	tests := []struct {
		name    string
		cfg     ServerConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid server config",
			cfg: ServerConfig{
				Port:         8080,
				ReadTimeout:  30 * time.Second,
				WriteTimeout: 30 * time.Second,
				IdleTimeout:  60 * time.Second,
			},
			wantErr: false,
		},
		{
			name: "valid with zero timeouts",
			cfg: ServerConfig{
				Port:         8080,
				ReadTimeout:  0,
				WriteTimeout: 0,
				IdleTimeout:  0,
			},
			wantErr: false,
		},
		{
			name: "port too low",
			cfg: ServerConfig{
				Port: -1,
			},
			wantErr: true,
			errMsg:  "server port must be between 0 and 65535",
		},
		{
			name: "port too high",
			cfg: ServerConfig{
				Port: 65536,
			},
			wantErr: true,
			errMsg:  "server port must be between 0 and 65535",
		},
		{
			name: "negative read timeout",
			cfg: ServerConfig{
				Port:        8080,
				ReadTimeout: -1 * time.Second,
			},
			wantErr: true,
			errMsg:  "read timeout cannot be negative",
		},
		{
			name: "negative write timeout",
			cfg: ServerConfig{
				Port:         8080,
				WriteTimeout: -1 * time.Second,
			},
			wantErr: true,
			errMsg:  "write timeout cannot be negative",
		},
		{
			name: "negative idle timeout",
			cfg: ServerConfig{
				Port:        8080,
				IdleTimeout: -1 * time.Second,
			},
			wantErr: true,
			errMsg:  "idle timeout cannot be negative",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateServer(tt.cfg)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ValidateServer() expected error, got nil")
					return
				}
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("ValidateServer() error = %v, want error containing %q", err, tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("ValidateServer() unexpected error = %v", err)
				}
			}
		})
	}
}
