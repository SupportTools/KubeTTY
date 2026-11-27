package controller

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
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
	for _, want := range []string{"-exec chown -h mmattox:mmattox {} +", "-xdev", "CHOWN_BUDGET_SECONDS", "CHOWN_STALL_SECONDS",
		"CHOWN_PARALLEL", "CHOWN_SPLIT_DEPTH", "xargs -0 -r -P", "set -m", `kill -- -"$pid"`} {
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
	return runInitPermissionsIn(t, t.TempDir(), map[string]string{"find": "#!/bin/bash\n" + findStub + "\n"}, env...)
}

// runInitPermissionsIn runs the shipped script against the volume dir data,
// with the given extra stubs (name -> script body) on PATH ahead of the real
// tools. getent and id are always stubbed so the script never adds users.
func runInitPermissionsIn(t *testing.T, data string, extra map[string]string, env ...string) (int, string, time.Duration) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("the walk supervisor reads /proc/<pid>/stat")
	}
	script := initPermissionsCommand(t)[2]
	bin := t.TempDir()
	stubs := map[string]string{
		"getent": "#!/bin/bash\nexit 0\n",
		"id":     "#!/bin/bash\necho 1000\n",
	}
	for name, body := range extra {
		stubs[name] = body
	}
	for name, body := range stubs {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("bash", "-c", script)
	cmd.Env = append([]string{
		"PATH=" + bin + ":" + os.Getenv("PATH"),
		"KUBETTY_DATA_DIR=" + data,
		"CHOWN_POLL_SECONDS=1",
	}, env...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	// A walker that outlives the script keeps its stderr open, and Run would
	// wait for it forever. Stop waiting after a few seconds and fail instead.
	cmd.WaitDelay = 5 * time.Second
	start := time.Now()
	err := cmd.Run()
	elapsed := time.Since(start)
	code := 0
	var ee *exec.ExitError
	switch {
	case errors.As(err, &ee):
		code = ee.ExitCode()
	case errors.Is(err, exec.ErrWaitDelay):
		t.Errorf("a process the script started outlived it and kept stderr open")
	case err != nil:
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
// restart that repeats the whole walk, and the stop must take every walker
// process with it, not just the group leader.
func TestInitPermissions_BudgetExceededContinuesStartup(t *testing.T) {
	pids := filepath.Join(t.TempDir(), "pids")
	code, stderr, elapsed := runInitPermissions(t, `echo $$ >>"$STUB_PIDS"; while :; do :; done`,
		"CHOWN_BUDGET_SECONDS=3", "CHOWN_STALL_SECONDS=30", "STUB_PIDS="+pids)
	if code != 0 {
		t.Fatalf("exit %d, want 0 (continue startup); stderr=%s", code, stderr)
	}
	if !strings.Contains(stderr, "continuing startup") {
		t.Errorf("missing budget message; stderr=%s", stderr)
	}
	if elapsed > 20*time.Second {
		t.Errorf("took %s; budget did not stop the walk", elapsed)
	}
	assertStubsGone(t, pids)
}

// A hung volume: the walk accrues no CPU. The container must fail so kubelet
// retries, rather than waiting out the whole budget.
func TestInitPermissions_StalledWalkFails(t *testing.T) {
	pids := filepath.Join(t.TempDir(), "pids")
	code, stderr, elapsed := runInitPermissions(t, `echo $$ >>"$STUB_PIDS"; exec sleep 60`,
		"CHOWN_BUDGET_SECONDS=60", "CHOWN_STALL_SECONDS=3", "STUB_PIDS="+pids)
	if code != 1 {
		t.Fatalf("exit %d, want 1 (stall); stderr=%s", code, stderr)
	}
	if !strings.Contains(stderr, "no CPU progress") {
		t.Errorf("missing stall message; stderr=%s", stderr)
	}
	if elapsed > 30*time.Second {
		t.Errorf("took %s; stall detection did not fire before the budget", elapsed)
	}
	assertStubsGone(t, pids)
}

// The progress signal is the whole walk's process group. Here the two top-level
// finds finish at once and all the work happens in the per-subtree finds that
// xargs starts, so the walk's own shell accrues no CPU. That must still count
// as progress (budget stop, exit 0), not as a stall.
func TestInitPermissions_SupervisorCountsTheWholeWalk(t *testing.T) {
	pids := filepath.Join(t.TempDir(), "pids")
	stub := `case " $* " in
  *" -mindepth "*) printf 'subtree-a\0subtree-b\0' ;;
  *" -maxdepth "*) : ;;
  *) echo $$ >>"$STUB_PIDS"; while :; do :; done ;;
esac`
	code, stderr, elapsed := runInitPermissions(t, stub,
		"CHOWN_BUDGET_SECONDS=8", "CHOWN_STALL_SECONDS=3", "STUB_PIDS="+pids)
	if code != 0 {
		t.Fatalf("exit %d, want 0 (budget, not stall); stderr=%s", code, stderr)
	}
	if strings.Contains(stderr, "no CPU progress") {
		t.Errorf("CPU of the per-subtree finds was not counted; stderr=%s", stderr)
	}
	if !strings.Contains(stderr, "continuing startup") {
		t.Errorf("missing budget message; stderr=%s", stderr)
	}
	if elapsed > 30*time.Second {
		t.Errorf("took %s", elapsed)
	}
	assertStubsGone(t, pids)
}

// assertStubsGone fails if a stub process recorded in pidFile (one pid per
// line) is still running a few seconds after the script returned: a leaked
// walker would keep reading the volume after startup continued.
func assertStubsGone(t *testing.T, pidFile string) {
	t.Helper()
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("stub never recorded a pid: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for _, pid := range strings.Fields(string(raw)) {
		for processRunning(pid) {
			if time.Now().After(deadline) {
				t.Errorf("walker process %s still running after the script exited", pid)
				// Do not leave a busy loop running after the test.
				if n, err := strconv.Atoi(pid); err == nil {
					if p, err := os.FindProcess(n); err == nil {
						_ = p.Kill()
					}
				}
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
}

// processRunning reports whether pid exists and is not a zombie.
func processRunning(pid string) bool {
	stat, err := os.ReadFile(filepath.Join("/proc", pid, "stat"))
	if err != nil {
		return false
	}
	s := string(stat)
	i := strings.LastIndex(s, ") ")
	return i < 0 || !strings.HasPrefix(s[i+2:], "Z")
}

func TestInitPermissions_WalkErrorFails(t *testing.T) {
	code, stderr, _ := runInitPermissions(t, "exit 3", "CHOWN_BUDGET_SECONDS=30", "CHOWN_STALL_SECONDS=10")
	if code != 3 {
		t.Fatalf("exit %d, want 3; stderr=%s", code, stderr)
	}
}

// The split walk must chown every entry under opt, home and usr-local-go
// exactly once, at any split depth and parallelism: nothing missed at the seams
// between the shallow find, the split-depth find and the per-subtree finds,
// nothing chowned twice. The tree puts files, empty directories and symlinks to
// directories at and around each split depth, and uses names with spaces and a
// newline. This runs the real find and xargs; chown is a stub that logs its
// operands. var-lib-docker must not be touched at all.
func TestInitPermissions_ParallelWalkCoversEveryEntryOnce(t *testing.T) {
	realFind, err := exec.LookPath("find")
	if err != nil {
		t.Skip("find not installed")
	}
	if _, err := exec.LookPath("xargs"); err != nil {
		t.Skip("xargs not installed")
	}
	// The test tree belongs to whoever runs the test (often a user that really
	// is called mmattox). Swap the user find tests against for one that owns
	// nothing here, so every entry counts as needing a chown.
	other := "root"
	if os.Geteuid() == 0 {
		other = "nobody"
	}
	findStub := fmt.Sprintf(`#!/bin/bash
args=()
for a in "$@"; do
  if [ "$a" = mmattox ]; then args+=(%s); else args+=("$a"); fi
done
exec %q "${args[@]}"
`, other, realFind)
	chownStub := `#!/bin/bash
if [ "$1" != -h ] || [ "$2" != mmattox:mmattox ]; then exit 99; fi
shift 2
printf '%s\0' "$@" >"$(mktemp "$CHOWN_LOG_DIR/chown.XXXXXX")"
`

	data := t.TempDir()
	for _, d := range []string{
		"opt/a/b/c/d/e/f/g/h",
		"opt/a/empty2",
		"opt/a/b/c/d/e/empty6",
		"opt/with space/x y",
		"home/mmattox/go/src/github.com/org/repo/pkg",
		"home/mmattox/new\nline/deeper",
		"usr-local-go/bin",
		"usr-local-go/src/runtime/internal",
		"var-lib-docker/overlay2/l",
	} {
		if err := os.MkdirAll(filepath.Join(data, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{
		"opt/top",                   // depth 1
		"opt/a/file2",               // depth 2
		"opt/a/b/file3",             // depth 3
		"opt/a/b/c/d/e/file6",       // depth 6
		"opt/a/b/c/d/e/f/g/h/file9", // depth 9
		"opt/with space/x y/f z",    // spaces
		"home/mmattox/.bashrc",      // depth 2
		"home/mmattox/go/src/github.com/org/repo/pkg/a.go",
		"home/mmattox/new\nline/deeper/n", // newline in a directory name
		"usr-local-go/bin/go",
		"var-lib-docker/overlay2/l/x",
	} {
		if err := os.WriteFile(filepath.Join(data, f), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for link, target := range map[string]string{
		"opt/l1":                "a", // symlink to a directory at depth 1
		"opt/a/b/l3":            "c", // ... at depth 3
		"home/mmattox/go/l3":    "src",
		"opt/a/b/c/d/e/l6":      "f", // ... at depth 6
		"usr-local-go/dangling": "nowhere",
	} {
		if err := os.Symlink(target, filepath.Join(data, link)); err != nil {
			t.Fatal(err)
		}
	}

	want := map[string]bool{}
	for _, root := range []string{"opt", "home", "usr-local-go"} {
		err := filepath.WalkDir(filepath.Join(data, root), func(p string, _ fs.DirEntry, err error) error {
			want[p] = true
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	for _, split := range []int{1, 2, 3, 6, 8} {
		for _, parallel := range []int{1, 4} {
			t.Run(fmt.Sprintf("split%d_parallel%d", split, parallel), func(t *testing.T) {
				logs := t.TempDir()
				code, stderr, _ := runInitPermissionsIn(t, data,
					map[string]string{"find": findStub, "chown": chownStub},
					"CHOWN_BUDGET_SECONDS=60", "CHOWN_STALL_SECONDS=30",
					fmt.Sprintf("CHOWN_SPLIT_DEPTH=%d", split),
					fmt.Sprintf("CHOWN_PARALLEL=%d", parallel),
					"CHOWN_LOG_DIR="+logs)
				if code != 0 {
					t.Fatalf("exit %d, want 0; stderr=%s", code, stderr)
				}
				got := map[string]int{}
				batches, err := os.ReadDir(logs)
				if err != nil {
					t.Fatal(err)
				}
				for _, b := range batches {
					raw, err := os.ReadFile(filepath.Join(logs, b.Name()))
					if err != nil {
						t.Fatal(err)
					}
					for _, p := range strings.Split(strings.TrimSuffix(string(raw), "\x00"), "\x00") {
						got[p]++
					}
				}
				for p := range want {
					if got[p] != 1 {
						t.Errorf("%q chowned %d times, want 1", p, got[p])
					}
				}
				for p, n := range got {
					if !want[p] {
						t.Errorf("%q chowned %d times but is outside the walked roots", p, n)
					}
				}
			})
		}
	}
}
