package config

import (
	"os"
	"strings"
	"testing"
)

func TestLoadProjectConfig(t *testing.T) {
	tests := []struct {
		name     string
		envVars  map[string]string
		wantErr  bool
		errMsg   string
		validate func(*testing.T, ProjectConfig)
	}{
		{
			name: "valid project config with all fields",
			envVars: map[string]string{
				"SESSION_ID":      "test-session-123",
				"DEPLOYMENT_ID":   "test-deployment-456",
				"PORT":            "9090",
				"SHELL":           "/bin/zsh",
				"KUBETTY_USER":    "testuser",
				"KUBETTY_PROJECT": "test-project",
			},
			wantErr: false,
			validate: func(t *testing.T, cfg ProjectConfig) {
				if cfg.Port != "9090" {
					t.Errorf("Port = %s, want 9090", cfg.Port)
				}
				if cfg.SessionID != "test-session-123" {
					t.Errorf("SessionID = %s, want test-session-123", cfg.SessionID)
				}
				if cfg.DeploymentID != "test-deployment-456" {
					t.Errorf("DeploymentID = %s, want test-deployment-456", cfg.DeploymentID)
				}
				if cfg.Shell != "/bin/zsh" {
					t.Errorf("Shell = %s, want /bin/zsh", cfg.Shell)
				}
				if cfg.KubettyUser != "testuser" {
					t.Errorf("KubettyUser = %s, want testuser", cfg.KubettyUser)
				}
				if cfg.KubettyProject != "test-project" {
					t.Errorf("KubettyProject = %s, want test-project", cfg.KubettyProject)
				}
			},
		},
		{
			name: "valid config with defaults",
			envVars: map[string]string{
				"SESSION_ID": "test-session-456",
				"USER":       "systemuser",
			},
			wantErr: false,
			validate: func(t *testing.T, cfg ProjectConfig) {
				if cfg.Port != "8080" {
					t.Errorf("Port = %s, want 8080 (default)", cfg.Port)
				}
				if cfg.Shell != "/bin/bash" {
					t.Errorf("Shell = %s, want /bin/bash (default)", cfg.Shell)
				}
				if cfg.DeploymentID != "test-session-456" {
					t.Errorf("DeploymentID = %s, want test-session-456 (defaulted from SessionID)", cfg.DeploymentID)
				}
				if cfg.KubettyUser != "systemuser" {
					t.Errorf("KubettyUser = %s, want systemuser (defaulted from USER)", cfg.KubettyUser)
				}
				if cfg.KubettyProject != "test-session-456" {
					t.Errorf("KubettyProject = %s, want test-session-456 (defaulted from DeploymentID)", cfg.KubettyProject)
				}
			},
		},
		{
			name:    "missing SESSION_ID",
			envVars: map[string]string{},
			wantErr: true,
			errMsg:  "SESSION_ID is required",
		},
		{
			name: "deployment ID defaults to session ID",
			envVars: map[string]string{
				"SESSION_ID": "test-session-deployment",
			},
			wantErr: false,
			validate: func(t *testing.T, cfg ProjectConfig) {
				if cfg.DeploymentID != "test-session-deployment" {
					t.Errorf("DeploymentID = %s, want test-session-deployment (defaulted from SessionID)", cfg.DeploymentID)
				}
			},
		},
		{
			name: "kubetty user defaults to system USER",
			envVars: map[string]string{
				"SESSION_ID": "test-session",
				"USER":       "systemuser",
			},
			wantErr: false,
			validate: func(t *testing.T, cfg ProjectConfig) {
				if cfg.KubettyUser != "systemuser" {
					t.Errorf("KubettyUser = %s, want systemuser (defaulted from USER)", cfg.KubettyUser)
				}
			},
		},
		{
			name: "kubetty project defaults to deployment ID",
			envVars: map[string]string{
				"SESSION_ID":    "test-session",
				"DEPLOYMENT_ID": "test-deployment",
			},
			wantErr: false,
			validate: func(t *testing.T, cfg ProjectConfig) {
				if cfg.KubettyProject != "test-deployment" {
					t.Errorf("KubettyProject = %s, want test-deployment (defaulted from DeploymentID)", cfg.KubettyProject)
				}
			},
		},
		{
			name: "custom KUBETTY_USER overrides USER",
			envVars: map[string]string{
				"SESSION_ID":   "test-session",
				"USER":         "systemuser",
				"KUBETTY_USER": "customuser",
			},
			wantErr: false,
			validate: func(t *testing.T, cfg ProjectConfig) {
				if cfg.KubettyUser != "customuser" {
					t.Errorf("KubettyUser = %s, want customuser", cfg.KubettyUser)
				}
			},
		},
		{
			name: "custom KUBETTY_PROJECT overrides deployment ID",
			envVars: map[string]string{
				"SESSION_ID":      "test-session",
				"DEPLOYMENT_ID":   "test-deployment",
				"KUBETTY_PROJECT": "custom-project",
			},
			wantErr: false,
			validate: func(t *testing.T, cfg ProjectConfig) {
				if cfg.KubettyProject != "custom-project" {
					t.Errorf("KubettyProject = %s, want custom-project", cfg.KubettyProject)
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
			cfg, err := LoadProjectConfig()

			// Check error expectation
			if tt.wantErr {
				if err == nil {
					t.Errorf("LoadProjectConfig() expected error, got nil")
					return
				}
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("LoadProjectConfig() error = %v, want error containing %q", err, tt.errMsg)
				}
				return
			}

			// No error expected
			if err != nil {
				t.Errorf("LoadProjectConfig() unexpected error = %v", err)
				return
			}

			// Run validation function
			if tt.validate != nil {
				tt.validate(t, cfg)
			}
		})
	}
}
