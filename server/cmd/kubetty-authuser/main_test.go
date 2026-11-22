package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// In the gateway pod AUTH_MODE=github and the AUTH_GITHUB_* values are
// gateway-only settings. The CLI must still get past config loading (it then
// stops at the usage text: no subcommand is given and the pool connects lazily).
func TestRunsInGitHubGatewayEnvironment(t *testing.T) {
	if os.Getenv("KUBETTY_AUTHUSER_CHILD") == "1" {
		main()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestRunsInGitHubGatewayEnvironment")
	cmd.Env = append(os.Environ(),
		"KUBETTY_AUTHUSER_CHILD=1",
		"AUTH_MODE=github",
		"AUTH_GITHUB_CLIENT_ID=Iv1.test",
		"CNPG_HOST=127.0.0.1", "CNPG_PORT=1", "CNPG_DATABASE=kubetty", "CNPG_USER=u", "CNPG_PASSWORD=p",
	)
	out, _ := cmd.CombinedOutput()
	if strings.Contains(string(out), "load config") {
		t.Fatalf("config loading refused the gateway environment: %s", out)
	}
	if !strings.Contains(string(out), "Usage:") {
		t.Fatalf("expected to get past config loading to the usage text, got: %s", out)
	}
}
