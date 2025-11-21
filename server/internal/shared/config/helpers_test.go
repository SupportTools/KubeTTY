package config

import (
	"os"
	"testing"
	"time"
)

func TestGetEnv(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		setValue string
		setEnv   bool
		def      string
		want     string
	}{
		{
			name:     "returns value when set",
			key:      "TEST_STRING",
			setValue: "test_value",
			setEnv:   true,
			def:      "default",
			want:     "test_value",
		},
		{
			name:   "returns default when not set",
			key:    "TEST_STRING_MISSING",
			setEnv: false,
			def:    "default",
			want:   "default",
		},
		{
			name:     "returns default when empty",
			key:      "TEST_STRING_EMPTY",
			setValue: "",
			setEnv:   true,
			def:      "default",
			want:     "default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setEnv {
				os.Setenv(tt.key, tt.setValue)
				defer os.Unsetenv(tt.key)
			}

			got := GetEnv(tt.key, tt.def)
			if got != tt.want {
				t.Errorf("GetEnv() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetEnvInt(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		setValue string
		setEnv   bool
		def      int
		want     int
	}{
		{
			name:     "returns parsed value when valid",
			key:      "TEST_INT",
			setValue: "42",
			setEnv:   true,
			def:      10,
			want:     42,
		},
		{
			name:   "returns default when not set",
			key:    "TEST_INT_MISSING",
			setEnv: false,
			def:    10,
			want:   10,
		},
		{
			name:     "returns default when empty",
			key:      "TEST_INT_EMPTY",
			setValue: "",
			setEnv:   true,
			def:      10,
			want:     10,
		},
		{
			name:     "returns default when invalid",
			key:      "TEST_INT_INVALID",
			setValue: "not_a_number",
			setEnv:   true,
			def:      10,
			want:     10,
		},
		{
			name:     "handles negative values",
			key:      "TEST_INT_NEGATIVE",
			setValue: "-5",
			setEnv:   true,
			def:      10,
			want:     -5,
		},
		{
			name:     "handles zero",
			key:      "TEST_INT_ZERO",
			setValue: "0",
			setEnv:   true,
			def:      10,
			want:     0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setEnv {
				os.Setenv(tt.key, tt.setValue)
				defer os.Unsetenv(tt.key)
			}

			got := GetEnvInt(tt.key, tt.def)
			if got != tt.want {
				t.Errorf("GetEnvInt() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetEnvDuration(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		setValue string
		setEnv   bool
		def      time.Duration
		want     time.Duration
	}{
		{
			name:     "returns parsed duration for seconds",
			key:      "TEST_DURATION",
			setValue: "30s",
			setEnv:   true,
			def:      10 * time.Second,
			want:     30 * time.Second,
		},
		{
			name:     "returns parsed duration for minutes",
			key:      "TEST_DURATION_MIN",
			setValue: "5m",
			setEnv:   true,
			def:      10 * time.Second,
			want:     5 * time.Minute,
		},
		{
			name:     "returns parsed duration for hours",
			key:      "TEST_DURATION_HOUR",
			setValue: "2h",
			setEnv:   true,
			def:      10 * time.Second,
			want:     2 * time.Hour,
		},
		{
			name:   "returns default when not set",
			key:    "TEST_DURATION_MISSING",
			setEnv: false,
			def:    10 * time.Second,
			want:   10 * time.Second,
		},
		{
			name:     "returns default when empty",
			key:      "TEST_DURATION_EMPTY",
			setValue: "",
			setEnv:   true,
			def:      10 * time.Second,
			want:     10 * time.Second,
		},
		{
			name:     "returns default when invalid",
			key:      "TEST_DURATION_INVALID",
			setValue: "not_a_duration",
			setEnv:   true,
			def:      10 * time.Second,
			want:     10 * time.Second,
		},
		{
			name:     "handles complex durations",
			key:      "TEST_DURATION_COMPLEX",
			setValue: "1h30m45s",
			setEnv:   true,
			def:      10 * time.Second,
			want:     1*time.Hour + 30*time.Minute + 45*time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setEnv {
				os.Setenv(tt.key, tt.setValue)
				defer os.Unsetenv(tt.key)
			}

			got := GetEnvDuration(tt.key, tt.def)
			if got != tt.want {
				t.Errorf("GetEnvDuration() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetEnvBool(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		setValue string
		setEnv   bool
		def      bool
		want     bool
	}{
		{
			name:     "returns true for 'true'",
			key:      "TEST_BOOL",
			setValue: "true",
			setEnv:   true,
			def:      false,
			want:     true,
		},
		{
			name:     "returns false for 'false'",
			key:      "TEST_BOOL_FALSE",
			setValue: "false",
			setEnv:   true,
			def:      true,
			want:     false,
		},
		{
			name:     "returns true for '1'",
			key:      "TEST_BOOL_ONE",
			setValue: "1",
			setEnv:   true,
			def:      false,
			want:     true,
		},
		{
			name:     "returns false for '0'",
			key:      "TEST_BOOL_ZERO",
			setValue: "0",
			setEnv:   true,
			def:      true,
			want:     false,
		},
		{
			name:   "returns default when not set",
			key:    "TEST_BOOL_MISSING",
			setEnv: false,
			def:    true,
			want:   true,
		},
		{
			name:     "returns default when empty",
			key:      "TEST_BOOL_EMPTY",
			setValue: "",
			setEnv:   true,
			def:      true,
			want:     true,
		},
		{
			name:     "returns default when invalid",
			key:      "TEST_BOOL_INVALID",
			setValue: "not_a_bool",
			setEnv:   true,
			def:      true,
			want:     true,
		},
		{
			name:     "handles case insensitive TRUE",
			key:      "TEST_BOOL_UPPER",
			setValue: "TRUE",
			setEnv:   true,
			def:      false,
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setEnv {
				os.Setenv(tt.key, tt.setValue)
				defer os.Unsetenv(tt.key)
			}

			got := GetEnvBool(tt.key, tt.def)
			if got != tt.want {
				t.Errorf("GetEnvBool() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildPostgresConnString(t *testing.T) {
	tests := []struct {
		name     string
		host     string
		port     string
		database string
		user     string
		password string
		want     string
	}{
		{
			name:     "builds standard connection string",
			host:     "localhost",
			port:     "5432",
			database: "kubetty",
			user:     "kubetty_user",
			password: "secret",
			want:     "host=localhost port=5432 dbname=kubetty user=kubetty_user password=secret sslmode=disable",
		},
		{
			name:     "handles CNPG cluster DNS",
			host:     "postgres-primary.default.svc.cluster.local",
			port:     "5432",
			database: "kubetty",
			user:     "kubetty",
			password: "password123",
			want:     "host=postgres-primary.default.svc.cluster.local port=5432 dbname=kubetty user=kubetty password=password123 sslmode=disable",
		},
		{
			name:     "handles custom port",
			host:     "db.example.com",
			port:     "5433",
			database: "mydb",
			user:     "admin",
			password: "pass",
			want:     "host=db.example.com port=5433 dbname=mydb user=admin password=pass sslmode=disable",
		},
		{
			name:     "handles empty password",
			host:     "localhost",
			port:     "5432",
			database: "test",
			user:     "testuser",
			password: "",
			want:     "host=localhost port=5432 dbname=test user=testuser password= sslmode=disable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildPostgresConnString(tt.host, tt.port, tt.database, tt.user, tt.password)
			if got != tt.want {
				t.Errorf("BuildPostgresConnString() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildPostgresConfig(t *testing.T) {
	tests := []struct {
		name     string
		host     string
		port     string
		database string
		user     string
		password string
		wantErr  bool
		errMsg   string
		validate func(*testing.T, string, string, string, string, string)
	}{
		{
			name:     "standard alphanumeric password",
			host:     "localhost",
			port:     "5432",
			database: "kubetty",
			user:     "kubetty_user",
			password: "secret123",
			wantErr:  false,
			validate: func(t *testing.T, host, port, db, user, pass string) {
				cfg, err := BuildPostgresConfig(host, port, db, user, pass)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if cfg.ConnConfig.Host != host {
					t.Errorf("Host = %v, want %v", cfg.ConnConfig.Host, host)
				}
				if cfg.ConnConfig.Port != 5432 {
					t.Errorf("Port = %v, want 5432", cfg.ConnConfig.Port)
				}
				if cfg.ConnConfig.Database != db {
					t.Errorf("Database = %v, want %v", cfg.ConnConfig.Database, db)
				}
				if cfg.ConnConfig.User != user {
					t.Errorf("User = %v, want %v", cfg.ConnConfig.User, user)
				}
				if cfg.ConnConfig.Password != pass {
					t.Errorf("Password = %v, want %v", cfg.ConnConfig.Password, pass)
				}
				if cfg.ConnConfig.TLSConfig != nil {
					t.Error("TLSConfig should be nil (sslmode=disable)")
				}
			},
		},
		{
			name:     "password with spaces",
			host:     "localhost",
			port:     "5432",
			database: "kubetty",
			user:     "kubetty_user",
			password: "my password with spaces",
			wantErr:  false,
			validate: func(t *testing.T, host, port, db, user, pass string) {
				cfg, err := BuildPostgresConfig(host, port, db, user, pass)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if cfg.ConnConfig.Password != pass {
					t.Errorf("Password = %v, want %v", cfg.ConnConfig.Password, pass)
				}
			},
		},
		{
			name:     "password with single quotes",
			host:     "localhost",
			port:     "5432",
			database: "kubetty",
			user:     "kubetty_user",
			password: "it's a secret",
			wantErr:  false,
			validate: func(t *testing.T, host, port, db, user, pass string) {
				cfg, err := BuildPostgresConfig(host, port, db, user, pass)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if cfg.ConnConfig.Password != pass {
					t.Errorf("Password = %v, want %v", cfg.ConnConfig.Password, pass)
				}
			},
		},
		{
			name:     "password with double quotes",
			host:     "localhost",
			port:     "5432",
			database: "kubetty",
			user:     "kubetty_user",
			password: `say "hello"`,
			wantErr:  false,
			validate: func(t *testing.T, host, port, db, user, pass string) {
				cfg, err := BuildPostgresConfig(host, port, db, user, pass)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if cfg.ConnConfig.Password != pass {
					t.Errorf("Password = %v, want %v", cfg.ConnConfig.Password, pass)
				}
			},
		},
		{
			name:     "password with backslashes",
			host:     "localhost",
			port:     "5432",
			database: "kubetty",
			user:     "kubetty_user",
			password: `c:\path\to\key`,
			wantErr:  false,
			validate: func(t *testing.T, host, port, db, user, pass string) {
				cfg, err := BuildPostgresConfig(host, port, db, user, pass)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if cfg.ConnConfig.Password != pass {
					t.Errorf("Password = %v, want %v", cfg.ConnConfig.Password, pass)
				}
			},
		},
		{
			name:     "password with unicode",
			host:     "localhost",
			port:     "5432",
			database: "kubetty",
			user:     "kubetty_user",
			password: "пароль123",
			wantErr:  false,
			validate: func(t *testing.T, host, port, db, user, pass string) {
				cfg, err := BuildPostgresConfig(host, port, db, user, pass)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if cfg.ConnConfig.Password != pass {
					t.Errorf("Password = %v, want %v", cfg.ConnConfig.Password, pass)
				}
			},
		},
		{
			name:     "password with symbols",
			host:     "localhost",
			port:     "5432",
			database: "kubetty",
			user:     "kubetty_user",
			password: "p@ssw0rd!#$%^&*()",
			wantErr:  false,
			validate: func(t *testing.T, host, port, db, user, pass string) {
				cfg, err := BuildPostgresConfig(host, port, db, user, pass)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if cfg.ConnConfig.Password != pass {
					t.Errorf("Password = %v, want %v", cfg.ConnConfig.Password, pass)
				}
			},
		},
		{
			name:     "CNPG cluster DNS name",
			host:     "postgres-primary.default.svc.cluster.local",
			port:     "5432",
			database: "kubetty",
			user:     "kubetty",
			password: "password123",
			wantErr:  false,
			validate: func(t *testing.T, host, port, db, user, pass string) {
				cfg, err := BuildPostgresConfig(host, port, db, user, pass)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if cfg.ConnConfig.Host != host {
					t.Errorf("Host = %v, want %v", cfg.ConnConfig.Host, host)
				}
			},
		},
		{
			name:     "empty password is allowed",
			host:     "localhost",
			port:     "5432",
			database: "kubetty",
			user:     "kubetty_user",
			password: "",
			wantErr:  false,
			validate: func(t *testing.T, host, port, db, user, pass string) {
				cfg, err := BuildPostgresConfig(host, port, db, user, pass)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if cfg.ConnConfig.Password != "" {
					t.Errorf("Password = %v, want empty", cfg.ConnConfig.Password)
				}
			},
		},
		{
			name:     "custom port",
			host:     "localhost",
			port:     "5433",
			database: "kubetty",
			user:     "kubetty_user",
			password: "secret",
			wantErr:  false,
			validate: func(t *testing.T, host, port, db, user, pass string) {
				cfg, err := BuildPostgresConfig(host, port, db, user, pass)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if cfg.ConnConfig.Port != 5433 {
					t.Errorf("Port = %v, want 5433", cfg.ConnConfig.Port)
				}
			},
		},
		{
			name:     "minimum valid port",
			host:     "localhost",
			port:     "1",
			database: "kubetty",
			user:     "kubetty_user",
			password: "secret",
			wantErr:  false,
			validate: func(t *testing.T, host, port, db, user, pass string) {
				cfg, err := BuildPostgresConfig(host, port, db, user, pass)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if cfg.ConnConfig.Port != 1 {
					t.Errorf("Port = %v, want 1", cfg.ConnConfig.Port)
				}
			},
		},
		{
			name:     "maximum valid port",
			host:     "localhost",
			port:     "65535",
			database: "kubetty",
			user:     "kubetty_user",
			password: "secret",
			wantErr:  false,
			validate: func(t *testing.T, host, port, db, user, pass string) {
				cfg, err := BuildPostgresConfig(host, port, db, user, pass)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if cfg.ConnConfig.Port != 65535 {
					t.Errorf("Port = %v, want 65535", cfg.ConnConfig.Port)
				}
			},
		},
		{
			name:     "empty host returns error",
			host:     "",
			port:     "5432",
			database: "kubetty",
			user:     "kubetty_user",
			password: "secret",
			wantErr:  true,
			errMsg:   "database host is required",
		},
		{
			name:     "empty database returns error",
			host:     "localhost",
			port:     "5432",
			database: "",
			user:     "kubetty_user",
			password: "secret",
			wantErr:  true,
			errMsg:   "database name is required",
		},
		{
			name:     "empty user returns error",
			host:     "localhost",
			port:     "5432",
			database: "kubetty",
			user:     "",
			password: "secret",
			wantErr:  true,
			errMsg:   "database user is required",
		},
		{
			name:     "non-numeric port returns error",
			host:     "localhost",
			port:     "abc",
			database: "kubetty",
			user:     "kubetty_user",
			password: "secret",
			wantErr:  true,
			errMsg:   "invalid port number",
		},
		{
			name:     "port zero returns error",
			host:     "localhost",
			port:     "0",
			database: "kubetty",
			user:     "kubetty_user",
			password: "secret",
			wantErr:  true,
			errMsg:   "must be between 1 and 65535",
		},
		{
			name:     "negative port returns error",
			host:     "localhost",
			port:     "-1",
			database: "kubetty",
			user:     "kubetty_user",
			password: "secret",
			wantErr:  true,
			errMsg:   "must be between 1 and 65535",
		},
		{
			name:     "port above 65535 returns error",
			host:     "localhost",
			port:     "65536",
			database: "kubetty",
			user:     "kubetty_user",
			password: "secret",
			wantErr:  true,
			errMsg:   "must be between 1 and 65535",
		},
		{
			name:     "injection attempt in password is safely handled",
			host:     "localhost",
			port:     "5432",
			database: "kubetty",
			user:     "kubetty_user",
			password: "secret' OR '1'='1",
			wantErr:  false,
			validate: func(t *testing.T, host, port, db, user, pass string) {
				cfg, err := BuildPostgresConfig(host, port, db, user, pass)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				// The password should be stored exactly as-is with no interpretation
				if cfg.ConnConfig.Password != pass {
					t.Errorf("Password = %v, want %v", cfg.ConnConfig.Password, pass)
				}
			},
		},
		{
			name:     "connection parameter injection attempt is safely handled",
			host:     "localhost",
			port:     "5432",
			database: "kubetty",
			user:     "kubetty_user",
			password: "secret sslmode=require host=evil.com",
			wantErr:  false,
			validate: func(t *testing.T, host, port, db, user, pass string) {
				cfg, err := BuildPostgresConfig(host, port, db, user, pass)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				// Verify the password is stored as-is and doesn't affect other config
				if cfg.ConnConfig.Password != pass {
					t.Errorf("Password = %v, want %v", cfg.ConnConfig.Password, pass)
				}
				// Verify host wasn't affected by injection attempt
				if cfg.ConnConfig.Host != "localhost" {
					t.Errorf("Host = %v, want localhost (injection attempt affected config)", cfg.ConnConfig.Host)
				}
				// Verify TLS is still disabled
				if cfg.ConnConfig.TLSConfig != nil {
					t.Error("TLSConfig should still be nil (injection attempt affected config)")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantErr {
				_, err := BuildPostgresConfig(tt.host, tt.port, tt.database, tt.user, tt.password)
				if err == nil {
					t.Errorf("BuildPostgresConfig() expected error, got nil")
					return
				}
				if tt.errMsg != "" && !contains(err.Error(), tt.errMsg) {
					t.Errorf("BuildPostgresConfig() error = %v, want error containing %q", err, tt.errMsg)
				}
			} else {
				if tt.validate != nil {
					tt.validate(t, tt.host, tt.port, tt.database, tt.user, tt.password)
				}
			}
		})
	}
}

// Helper function for error message checking
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
