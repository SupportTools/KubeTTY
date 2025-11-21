package config

import (
	"testing"
	"time"
)

func TestDatabaseConfig_ConnString(t *testing.T) {
	tests := []struct {
		name string
		cfg  DatabaseConfig
		want string
	}{
		{
			name: "standard configuration",
			cfg: DatabaseConfig{
				Host:     "localhost",
				Port:     "5432",
				Database: "kubetty",
				User:     "kubetty_user",
				Password: "secret",
			},
			want: "host=localhost port=5432 dbname=kubetty user=kubetty_user password=secret sslmode=disable",
		},
		{
			name: "CNPG cluster configuration",
			cfg: DatabaseConfig{
				Host:     "postgres-primary.default.svc.cluster.local",
				Port:     "5432",
				Database: "kubetty",
				User:     "kubetty",
				Password: "password123",
			},
			want: "host=postgres-primary.default.svc.cluster.local port=5432 dbname=kubetty user=kubetty password=password123 sslmode=disable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.cfg.ConnString()
			if got != tt.want {
				t.Errorf("DatabaseConfig.ConnString() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAuthConfig_IsEnabled(t *testing.T) {
	tests := []struct {
		name string
		mode string
		want bool
	}{
		{
			name: "enabled with local mode",
			mode: "local",
			want: true,
		},
		{
			name: "disabled with explicit disabled",
			mode: "disabled",
			want: false,
		},
		{
			name: "disabled with empty string",
			mode: "",
			want: false,
		},
		{
			name: "enabled with custom mode",
			mode: "oauth",
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := AuthConfig{Mode: tt.mode}
			got := cfg.IsEnabled()
			if got != tt.want {
				t.Errorf("AuthConfig.IsEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSessionConfig_MaxInactiveDuration(t *testing.T) {
	tests := []struct {
		name    string
		minutes int
		want    time.Duration
	}{
		{
			name:    "default 24 hours",
			minutes: 1440,
			want:    24 * time.Hour,
		},
		{
			name:    "custom 1 hour",
			minutes: 60,
			want:    1 * time.Hour,
		},
		{
			name:    "zero minutes",
			minutes: 0,
			want:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := SessionConfig{MaxInactiveMinutes: tt.minutes}
			got := cfg.MaxInactiveDuration()
			if got != tt.want {
				t.Errorf("SessionConfig.MaxInactiveDuration() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSessionConfig_PruneInterval(t *testing.T) {
	tests := []struct {
		name  string
		hours int
		want  time.Duration
	}{
		{
			name:  "default 1 hour",
			hours: 1,
			want:  1 * time.Hour,
		},
		{
			name:  "custom 6 hours",
			hours: 6,
			want:  6 * time.Hour,
		},
		{
			name:  "zero hours",
			hours: 0,
			want:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := SessionConfig{PruneIntervalHours: tt.hours}
			got := cfg.PruneInterval()
			if got != tt.want {
				t.Errorf("SessionConfig.PruneInterval() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestServerConfig_Addr(t *testing.T) {
	tests := []struct {
		name string
		port int
		want string
	}{
		{
			name: "standard port 8080",
			port: 8080,
			want: ":8080",
		},
		{
			name: "custom port 3000",
			port: 3000,
			want: ":3000",
		},
		{
			name: "zero port defaults to 8080",
			port: 0,
			want: ":8080",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := ServerConfig{Port: tt.port}
			got := cfg.Addr()
			if got != tt.want {
				t.Errorf("ServerConfig.Addr() = %v, want %v", got, tt.want)
			}
		})
	}
}
