package controller

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// initPermissionsCommand returns the init-permissions container command as
// rendered into the Deployment, so the tests exercise the shipped script and
// not a copy of it.
func initPermissionsCommand(t *testing.T) []string {
	t.Helper()
	dep := BuildDeployment(testProject("perm"), testConfig(), "")
	for _, c := range dep.Spec.Template.Spec.InitContainers {
		if c.Name == "init-permissions" {
			return c.Command
		}
	}
	t.Fatal("init-permissions init container not found")
	return nil
}

func TestInitPermissions_ScriptShape(t *testing.T) {
	cmd := initPermissionsCommand(t)
	if len(cmd) != 3 || cmd[0] != "/bin/bash" || cmd[1] != "-c" {
		t.Fatalf("unexpected command shape: %q", cmd[:2])
	}
	script := cmd[2]
	if strings.Contains(script, "timeout 300") {
		t.Error("script still hard-kills the walk with timeout 300 (cluster-services-1430)")
	}
	for _, want := range []string{"-exec chown -h mmattox:mmattox {} +", "-xdev", "CHOWN_BUDGET_SECONDS", "CHOWN_STALL_SECONDS"} {
		if !strings.Contains(script, want) {
			t.Errorf("script missing %q", want)
		}
	}
	if out, err := exec.Command("bash", "-n", "-c", script).CombinedOutput(); err != nil {
		t.Fatalf("bash -n: %v\n%s", err, out)
	}
}

// runInitPermissions runs the shipped script with stub getent/id/find on PATH
// and a temp dir as the volume, and returns its exit code and stderr.
func runInitPermissions(t *testing.T, findStub string, env ...string) (int, string, time.Duration) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("the walk supervisor reads /proc/<pid>/stat")
	}
	script := initPermissionsCommand(t)[2]
	bin := t.TempDir()
	stubs := map[string]string{
		"getent": "#!/bin/bash\nexit 0\n",
		"id":     "#!/bin/bash\necho 1000\n",
		"find":   "#!/bin/bash\n" + findStub + "\n",
	}
	for name, body := range stubs {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	data := t.TempDir()
	cmd := exec.Command("bash", "-c", script)
	cmd.Env = append([]string{
		"PATH=" + bin + ":" + os.Getenv("PATH"),
		"KUBETTY_DATA_DIR=" + data,
		"CHOWN_POLL_SECONDS=1",
	}, env...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	start := time.Now()
	err := cmd.Run()
	elapsed := time.Since(start)
	code := 0
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(filepath.Join(data, "home", "mmattox")); statErr != nil {
		t.Errorf("home dir not created: %v", statErr)
	}
	return code, stderr.String(), elapsed
}

func TestInitPermissions_FastWalkSucceeds(t *testing.T) {
	code, stderr, _ := runInitPermissions(t, "exit 0", "CHOWN_BUDGET_SECONDS=30", "CHOWN_STALL_SECONDS=10")
	if code != 0 {
		t.Fatalf("exit %d, want 0; stderr=%s", code, stderr)
	}
}

// A large but healthy volume: the walk keeps burning CPU past the budget. The
// container must exit 0 (startup continues) instead of failing into an init
// restart that repeats the whole walk.
func TestInitPermissions_BudgetExceededContinuesStartup(t *testing.T) {
	code, stderr, elapsed := runInitPermissions(t, "while :; do :; done",
		"CHOWN_BUDGET_SECONDS=3", "CHOWN_STALL_SECONDS=30")
	if code != 0 {
		t.Fatalf("exit %d, want 0 (continue startup); stderr=%s", code, stderr)
	}
	if !strings.Contains(stderr, "continuing startup") {
		t.Errorf("missing budget message; stderr=%s", stderr)
	}
	if elapsed > 20*time.Second {
		t.Errorf("took %s; budget did not stop the walk", elapsed)
	}
}

// A hung volume: the walk accrues no CPU. The container must fail so kubelet
// retries, rather than waiting out the whole budget.
func TestInitPermissions_StalledWalkFails(t *testing.T) {
	code, stderr, elapsed := runInitPermissions(t, "exec sleep 60",
		"CHOWN_BUDGET_SECONDS=60", "CHOWN_STALL_SECONDS=3")
	if code != 1 {
		t.Fatalf("exit %d, want 1 (stall); stderr=%s", code, stderr)
	}
	if !strings.Contains(stderr, "no CPU progress") {
		t.Errorf("missing stall message; stderr=%s", stderr)
	}
	if elapsed > 30*time.Second {
		t.Errorf("took %s; stall detection did not fire before the budget", elapsed)
	}
}

func TestInitPermissions_WalkErrorFails(t *testing.T) {
	code, stderr, _ := runInitPermissions(t, "exit 3", "CHOWN_BUDGET_SECONDS=30", "CHOWN_STALL_SECONDS=10")
	if code != 3 {
		t.Fatalf("exit %d, want 3; stderr=%s", code, stderr)
	}
}
