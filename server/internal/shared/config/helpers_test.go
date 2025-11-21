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
