package config_test

import (
	"fmt"
	"os"
	"time"

	"github.com/supporttools/KubeTTY/server/internal/shared/config"
)

// ExampleGetEnv demonstrates retrieving string environment variables with defaults.
func ExampleGetEnv() {
	// Set an environment variable
	os.Setenv("APP_NAME", "kubetty-gateway")
	defer os.Unsetenv("APP_NAME")

	// Get the value
	appName := config.GetEnv("APP_NAME", "kubetty")
	fmt.Printf("App name: %s\n", appName)

	// Get a missing variable (uses default)
	namespace := config.GetEnv("NAMESPACE", "default")
	fmt.Printf("Namespace: %s\n", namespace)

	// Output:
	// App name: kubetty-gateway
	// Namespace: default
}

// ExampleGetEnvInt demonstrates retrieving integer environment variables with defaults.
func ExampleGetEnvInt() {
	// Set valid integer
	os.Setenv("MAX_CONNECTIONS", "100")
	defer os.Unsetenv("MAX_CONNECTIONS")

	maxConns := config.GetEnvInt("MAX_CONNECTIONS", 50)
	fmt.Printf("Max connections: %d\n", maxConns)

	// Get missing variable (uses default)
	port := config.GetEnvInt("SERVER_PORT", 8080)
	fmt.Printf("Server port: %d\n", port)

	// Output:
	// Max connections: 100
	// Server port: 8080
}

// ExampleGetEnvDuration demonstrates retrieving duration environment variables.
func ExampleGetEnvDuration() {
	// Set valid duration
	os.Setenv("REQUEST_TIMEOUT", "30s")
	defer os.Unsetenv("REQUEST_TIMEOUT")

	timeout := config.GetEnvDuration("REQUEST_TIMEOUT", 10*time.Second)
	fmt.Printf("Request timeout: %v\n", timeout)

	// Get missing variable (uses default)
	idleTimeout := config.GetEnvDuration("IDLE_TIMEOUT", 5*time.Minute)
	fmt.Printf("Idle timeout: %v\n", idleTimeout)

	// Output:
	// Request timeout: 30s
	// Idle timeout: 5m0s
}

// ExampleGetEnvBool demonstrates retrieving boolean environment variables.
func ExampleGetEnvBool() {
	// Set valid boolean
	os.Setenv("DEBUG_MODE", "true")
	defer os.Unsetenv("DEBUG_MODE")

	debug := config.GetEnvBool("DEBUG_MODE", false)
	fmt.Printf("Debug mode: %v\n", debug)

	// Get missing variable (uses default)
	verbose := config.GetEnvBool("VERBOSE", false)
	fmt.Printf("Verbose: %v\n", verbose)

	// Output:
	// Debug mode: true
	// Verbose: false
}

// ExampleBuildPostgresConnString demonstrates building PostgreSQL connection strings.
func ExampleBuildPostgresConnString() {
	// Build connection string for local development
	localConn := config.BuildPostgresConnString(
		"localhost",
		"5432",
		"kubetty_dev",
		"developer",
		"devpass",
	)
	fmt.Println("Local connection:")
	fmt.Println(localConn)

	// Build connection string for CNPG cluster
	cnpgConn := config.BuildPostgresConnString(
		"postgres-primary.kubetty.svc.cluster.local",
		"5432",
		"kubetty",
		"kubetty_user",
		"secret",
	)
	fmt.Println("\nCNPG connection:")
	fmt.Println(cnpgConn)

	// Output:
	// Local connection:
	// host=localhost port=5432 dbname=kubetty_dev user=developer password=devpass sslmode=disable
	//
	// CNPG connection:
	// host=postgres-primary.kubetty.svc.cluster.local port=5432 dbname=kubetty user=kubetty_user password=secret sslmode=disable
}
