package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// The CLI must get past config loading without the server's auth settings (it
// then stops at the usage text: no subcommand is given and the pool connects lazily).
func TestRunsWithoutAuthSettings(t *testing.T) {
	if os.Getenv("KUBETTY_AUTHUSER_CHILD") == "1" {
		main()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestRunsWithoutAuthSettings")
	cmd.Env = append(os.Environ(),
		"KUBETTY_AUTHUSER_CHILD=1",
		"AUTH_MODE=local",
		"AUTH_JWT_SECRET=",
		"CNPG_HOST=127.0.0.1", "CNPG_PORT=1", "CNPG_DATABASE=kubetty", "CNPG_USER=u", "CNPG_PASSWORD=p",
	)
	out, _ := cmd.CombinedOutput()
	if strings.Contains(string(out), "load config") {
		t.Fatalf("config loading refused to start without auth settings: %s", out)
	}
	if !strings.Contains(string(out), "Usage:") {
		t.Fatalf("expected to get past config loading to the usage text, got: %s", out)
	}
}
