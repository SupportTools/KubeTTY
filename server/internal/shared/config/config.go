package config

import (
	"fmt"
	"time"
)

// DatabaseConfig contains PostgreSQL database connection settings.
// These settings are used to connect to CloudNativePG (CNPG) clusters.
type DatabaseConfig struct {
	// Host is the PostgreSQL server hostname or IP address
	Host string

	// Port is the PostgreSQL server port (typically 5432)
	Port string

	// Database is the name of the database to connect to
	Database string

	// User is the database username for authentication
	User string

	// Password is the database password for authentication
	Password string
}

// ConnString builds a PostgreSQL connection string from the database configuration.
// The connection string uses sslmode=disable for CNPG internal connections.
func (c DatabaseConfig) ConnString() string {
	return BuildPostgresConnString(c.Host, c.Port, c.Database, c.User, c.Password)
}

// AuthConfig contains authentication and JWT token settings.
// These settings control user authentication, token generation, and cookie behavior.
type AuthConfig struct {
	// Mode specifies the authentication mode: "disabled", "local"
	// - "disabled" or "": No authentication required
	// - "local": Username/password authentication with JWT tokens
	Mode string

	// JWTSecret is the secret key used to sign JWT tokens
	// Required when Mode is "local"
	JWTSecret string

	// AccessTTL is the lifetime of access tokens (default: 15 minutes)
	AccessTTL time.Duration

	// RefreshTTL is the lifetime of refresh tokens (default: 7 days)
	RefreshTTL time.Duration

	// CookieDomain specifies the domain for auth cookies (e.g., ".example.com")
	// Empty string means cookies are scoped to the current domain only
	CookieDomain string

	// CookieSecure controls whether cookies require HTTPS
	// Should be true in production, false for local development
	CookieSecure bool
}

// IsEnabled returns true if authentication is enabled (mode is not "" or "disabled").
func (c AuthConfig) IsEnabled() bool {
	return c.Mode != "" && c.Mode != "disabled"
}

// SessionConfig contains session management settings.
// These settings control session lifecycle, pruning, and storage behavior.
type SessionConfig struct {
	// SessionID is the unique identifier for this session (UUID)
	// Used by project binaries to identify the PTY session
	SessionID string

	// DeploymentID identifies the Kubernetes deployment this session belongs to
	// Used for grouping sessions and cleanup operations
	DeploymentID string

	// MaxInactiveMinutes is the number of minutes before inactive sessions are pruned
	// Default: 1440 (24 hours)
	MaxInactiveMinutes int

	// PruneIntervalHours is the interval in hours between session cleanup runs
	// Default: 1 hour
	PruneIntervalHours int

	// TrimMaxEntries is the maximum number of log entries to keep per session
	// Default: 10000
	TrimMaxEntries int
}

// MaxInactiveDuration returns the max inactive time as a time.Duration.
func (c SessionConfig) MaxInactiveDuration() time.Duration {
	return time.Duration(c.MaxInactiveMinutes) * time.Minute
}

// PruneInterval returns the prune interval as a time.Duration.
func (c SessionConfig) PruneInterval() time.Duration {
	return time.Duration(c.PruneIntervalHours) * time.Hour
}

// ServerConfig contains HTTP server settings.
// These settings control the server's network binding and behavior.
type ServerConfig struct {
	// Port is the HTTP server port (default: 8080)
	Port int

	// ReadTimeout is the maximum duration for reading the entire request
	ReadTimeout time.Duration

	// WriteTimeout is the maximum duration before timing out writes of the response
	WriteTimeout time.Duration

	// IdleTimeout is the maximum time to wait for the next request when keep-alives are enabled
	IdleTimeout time.Duration
}

// Addr returns the server address in the format ":port" suitable for http.Server.
func (c ServerConfig) Addr() string {
	if c.Port == 0 {
		return ":8080"
	}
	return fmt.Sprintf(":%d", c.Port)
}
