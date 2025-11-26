package config

import (
	"os"
	"testing"
)

func TestLoadCommonConfig_Defaults(t *testing.T) {
	// Clear environment
	os.Clearenv()

	cfg, err := loadCommonConfig()
	if err != nil {
		t.Fatalf("loadCommonConfig() unexpected error = %v", err)
	}

	// Check defaults
	if cfg.Port != "8080" {
		t.Errorf("Port = %s, want 8080 (default)", cfg.Port)
	}
	if cfg.CNPGPort != "5432" {
		t.Errorf("CNPGPort = %s, want 5432 (default)", cfg.CNPGPort)
	}
	if cfg.LogRetentionHours != 24*30 {
		t.Errorf("LogRetentionHours = %d, want %d (default)", cfg.LogRetentionHours, 24*30)
	}
	if cfg.LogMaxPerSession != 5000 {
		t.Errorf("LogMaxPerSession = %d, want 5000 (default)", cfg.LogMaxPerSession)
	}
}

func TestLoadCommonConfig_WithEnvVars(t *testing.T) {
	os.Clearenv()
	os.Setenv("PORT", "9090")
	os.Setenv("SESSION_ID", "test-session-123")
	os.Setenv("DEPLOYMENT_ID", "test-deployment-456")
	os.Setenv("CNPG_HOST", "postgres.example.com")
	os.Setenv("CNPG_PORT", "5433")
	os.Setenv("CNPG_DATABASE", "kubetty_test")
	os.Setenv("CNPG_USER", "testuser")
	os.Setenv("CNPG_PASSWORD", "testpass")
	os.Setenv("SESSION_LOG_RETENTION_HOURS", "168")
	os.Setenv("SESSION_LOG_MAX_ENTRIES", "10000")

	cfg, err := loadCommonConfig()
	if err != nil {
		t.Fatalf("loadCommonConfig() unexpected error = %v", err)
	}

	if cfg.Port != "9090" {
		t.Errorf("Port = %s, want 9090", cfg.Port)
	}
	if cfg.SessionID != "test-session-123" {
		t.Errorf("SessionID = %s, want test-session-123", cfg.SessionID)
	}
	if cfg.DeploymentID != "test-deployment-456" {
		t.Errorf("DeploymentID = %s, want test-deployment-456", cfg.DeploymentID)
	}
	if cfg.CNPGHost != "postgres.example.com" {
		t.Errorf("CNPGHost = %s, want postgres.example.com", cfg.CNPGHost)
	}
	if cfg.CNPGPort != "5433" {
		t.Errorf("CNPGPort = %s, want 5433", cfg.CNPGPort)
	}
	if cfg.CNPGDatabase != "kubetty_test" {
		t.Errorf("CNPGDatabase = %s, want kubetty_test", cfg.CNPGDatabase)
	}
	if cfg.CNPGUser != "testuser" {
		t.Errorf("CNPGUser = %s, want testuser", cfg.CNPGUser)
	}
	if cfg.CNPGPassword != "testpass" {
		t.Errorf("CNPGPassword = %s, want testpass", cfg.CNPGPassword)
	}
	if cfg.LogRetentionHours != 168 {
		t.Errorf("LogRetentionHours = %d, want 168", cfg.LogRetentionHours)
	}
	if cfg.LogMaxPerSession != 10000 {
		t.Errorf("LogMaxPerSession = %d, want 10000", cfg.LogMaxPerSession)
	}
}

func TestLoadCommonConfig_DeploymentIDDefaultsToSessionID(t *testing.T) {
	os.Clearenv()
	os.Setenv("SESSION_ID", "my-session-id")

	cfg, err := loadCommonConfig()
	if err != nil {
		t.Fatalf("loadCommonConfig() unexpected error = %v", err)
	}

	if cfg.DeploymentID != "my-session-id" {
		t.Errorf("DeploymentID = %s, want my-session-id (defaulted from SessionID)", cfg.DeploymentID)
	}
}

func TestCommonConfig_ConnString(t *testing.T) {
	cfg := CommonConfig{
		CNPGHost:     "localhost",
		CNPGPort:     "5432",
		CNPGDatabase: "kubetty",
		CNPGUser:     "admin",
		CNPGPassword: "secret",
	}

	connStr := cfg.ConnString()
	// ConnString uses libpq key=value format
	expected := "host=localhost port=5432 dbname=kubetty user=admin password=secret sslmode=disable"
	if connStr != expected {
		t.Errorf("ConnString() = %s, want %s", connStr, expected)
	}
}

func TestCommonConfig_ConnString_EmptyFields(t *testing.T) {
	cfg := CommonConfig{}
	connStr := cfg.ConnString()
	// Empty fields still produce key=value pairs
	expected := "host= port= dbname= user= password= sslmode=disable"
	if connStr != expected {
		t.Errorf("ConnString() = %s, want %s", connStr, expected)
	}
}

func TestCommonConfig_ConnConfig_Valid(t *testing.T) {
	cfg := CommonConfig{
		CNPGHost:     "localhost",
		CNPGPort:     "5432",
		CNPGDatabase: "kubetty",
		CNPGUser:     "admin",
		CNPGPassword: "secret",
	}

	poolCfg, err := cfg.ConnConfig()
	if err != nil {
		t.Fatalf("ConnConfig() unexpected error = %v", err)
	}
	if poolCfg == nil {
		t.Fatal("ConnConfig() returned nil")
	}

	// Check that the connection config was built correctly
	connCfg := poolCfg.ConnConfig
	if connCfg.Host != "localhost" {
		t.Errorf("Host = %s, want localhost", connCfg.Host)
	}
	if connCfg.Port != 5432 {
		t.Errorf("Port = %d, want 5432", connCfg.Port)
	}
	if connCfg.Database != "kubetty" {
		t.Errorf("Database = %s, want kubetty", connCfg.Database)
	}
	if connCfg.User != "admin" {
		t.Errorf("User = %s, want admin", connCfg.User)
	}
}

func TestCommonConfig_ConnConfig_InvalidPort(t *testing.T) {
	cfg := CommonConfig{
		CNPGHost:     "localhost",
		CNPGPort:     "invalid",
		CNPGDatabase: "kubetty",
		CNPGUser:     "admin",
		CNPGPassword: "secret",
	}

	_, err := cfg.ConnConfig()
	if err == nil {
		t.Fatal("ConnConfig() expected error for invalid port")
	}
}

func TestCommonConfig_ConnConfig_PortOutOfRange(t *testing.T) {
	cfg := CommonConfig{
		CNPGHost:     "localhost",
		CNPGPort:     "99999",
		CNPGDatabase: "kubetty",
		CNPGUser:     "admin",
		CNPGPassword: "secret",
	}

	_, err := cfg.ConnConfig()
	if err == nil {
		t.Fatal("ConnConfig() expected error for port out of range")
	}
}

func TestCommonConfigStruct_Fields(t *testing.T) {
	cfg := CommonConfig{
		Port:              "8080",
		SessionID:         "sess-123",
		DeploymentID:      "dep-456",
		CNPGHost:          "db.example.com",
		CNPGPort:          "5432",
		CNPGDatabase:      "kubetty",
		CNPGUser:          "user",
		CNPGPassword:      "pass",
		LogRetentionHours: 720,
		LogMaxPerSession:  5000,
	}

	// Verify all fields are accessible
	if cfg.Port != "8080" {
		t.Errorf("Port = %s, want 8080", cfg.Port)
	}
	if cfg.SessionID != "sess-123" {
		t.Errorf("SessionID = %s, want sess-123", cfg.SessionID)
	}
	if cfg.DeploymentID != "dep-456" {
		t.Errorf("DeploymentID = %s, want dep-456", cfg.DeploymentID)
	}
	if cfg.CNPGHost != "db.example.com" {
		t.Errorf("CNPGHost = %s, want db.example.com", cfg.CNPGHost)
	}
	if cfg.CNPGPort != "5432" {
		t.Errorf("CNPGPort = %s, want 5432", cfg.CNPGPort)
	}
	if cfg.CNPGDatabase != "kubetty" {
		t.Errorf("CNPGDatabase = %s, want kubetty", cfg.CNPGDatabase)
	}
	if cfg.CNPGUser != "user" {
		t.Errorf("CNPGUser = %s, want user", cfg.CNPGUser)
	}
	if cfg.CNPGPassword != "pass" {
		t.Errorf("CNPGPassword = %s, want pass", cfg.CNPGPassword)
	}
	if cfg.LogRetentionHours != 720 {
		t.Errorf("LogRetentionHours = %d, want 720", cfg.LogRetentionHours)
	}
	if cfg.LogMaxPerSession != 5000 {
		t.Errorf("LogMaxPerSession = %d, want 5000", cfg.LogMaxPerSession)
	}
}
