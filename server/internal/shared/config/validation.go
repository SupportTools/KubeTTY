package config

import "fmt"

// ValidateAuth validates authentication configuration settings.
// It ensures that:
//   - When auth mode is "local", JWT secret and TTLs are properly configured
//   - Auth mode is explicitly set to either "disabled" or "local"
//   - Empty auth mode is rejected to prevent accidental insecure deployments
//
// Returns an error if validation fails, nil otherwise.
func ValidateAuth(cfg AuthConfig) error {
	switch cfg.Mode {
	case "":
		// Empty auth mode is rejected - require explicit configuration
		return fmt.Errorf("AUTH_MODE must be explicitly set to 'disabled' or 'local'; empty value is not allowed to prevent accidental insecure deployments")

	case "disabled":
		// Explicit disabled mode - no authentication, no validation needed
		// Warning is logged at startup in gateway main.go
		return nil

	case "local":
		// Local authentication requires JWT configuration
		if cfg.JWTSecret == "" {
			return fmt.Errorf("JWT secret is required when auth mode is 'local'")
		}
		if cfg.AccessTTL <= 0 {
			return fmt.Errorf("access token TTL must be greater than 0 when auth mode is 'local'")
		}
		if cfg.RefreshTTL <= 0 {
			return fmt.Errorf("refresh token TTL must be greater than 0 when auth mode is 'local'")
		}
		return nil

	default:
		return fmt.Errorf("unsupported auth mode: %q (valid options: \"disabled\", \"local\")", cfg.Mode)
	}
}

// ValidateDatabase validates database configuration settings.
// It ensures that all required database connection parameters are provided.
//
// Returns an error if validation fails, nil otherwise.
func ValidateDatabase(cfg DatabaseConfig) error {
	if cfg.Host == "" {
		return fmt.Errorf("database host is required")
	}
	if cfg.Port == "" {
		return fmt.Errorf("database port is required")
	}
	if cfg.Database == "" {
		return fmt.Errorf("database name is required")
	}
	if cfg.User == "" {
		return fmt.Errorf("database user is required")
	}
	if cfg.Password == "" {
		return fmt.Errorf("database password is required")
	}
	return nil
}

// ValidateSession validates session configuration settings.
// It ensures that session parameters are within reasonable bounds.
//
// Returns an error if validation fails, nil otherwise.
func ValidateSession(cfg SessionConfig) error {
	if cfg.MaxInactiveMinutes < 0 {
		return fmt.Errorf("max inactive minutes cannot be negative")
	}
	if cfg.PruneIntervalHours < 0 {
		return fmt.Errorf("prune interval hours cannot be negative")
	}
	if cfg.TrimMaxEntries < 0 {
		return fmt.Errorf("trim max entries cannot be negative")
	}
	return nil
}

// ValidateServer validates server configuration settings.
// It ensures that server parameters are within reasonable bounds.
//
// Returns an error if validation fails, nil otherwise.
func ValidateServer(cfg ServerConfig) error {
	if cfg.Port < 0 || cfg.Port > 65535 {
		return fmt.Errorf("server port must be between 0 and 65535")
	}
	if cfg.ReadTimeout < 0 {
		return fmt.Errorf("read timeout cannot be negative")
	}
	if cfg.WriteTimeout < 0 {
		return fmt.Errorf("write timeout cannot be negative")
	}
	if cfg.IdleTimeout < 0 {
		return fmt.Errorf("idle timeout cannot be negative")
	}
	return nil
}

// ValidateController validates controller configuration settings.
// When the controller is disabled, validation passes without checking other fields.
// When enabled, it ensures that:
//   - ProjectsNamespace is provided
//   - ResourcePrefix is not empty
//   - ReconcileInterval is positive
//   - HealthCheckInterval is positive
//
// Returns an error if validation fails, nil otherwise.
func ValidateController(cfg ControllerConfig) error {
	if !cfg.Enabled {
		// No validation needed when controller is disabled
		return nil
	}

	if cfg.ProjectsNamespace == "" {
		return fmt.Errorf("PROJECTS_NAMESPACE is required when CONTROLLER_ENABLED=true")
	}
	if cfg.ResourcePrefix == "" {
		return fmt.Errorf("RESOURCE_PREFIX cannot be empty when controller is enabled")
	}
	if cfg.ReconcileInterval <= 0 {
		return fmt.Errorf("RECONCILE_INTERVAL must be positive when controller is enabled")
	}
	if cfg.HealthCheckInterval <= 0 {
		return fmt.Errorf("HEALTH_CHECK_INTERVAL must be positive when controller is enabled")
	}
	return nil
}
