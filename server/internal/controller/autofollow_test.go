package controller

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/supporttools/KubeTTY/server/internal/projects"
	"github.com/supporttools/KubeTTY/server/internal/shared/metrics"
	"k8s.io/client-go/kubernetes/fake"
)

// cluster-services-1376: idle sandboxes follow the recommended image; a
// project with a live user is never touched.

var afNow = time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)

func afConfig() AutoFollowConfig {
	return AutoFollowConfig{Enabled: true, RecommendedTag: "1.16.1", IdleGrace: 30 * time.Minute}
}

func afProject(name, tag string) *projects.Project {
	p := newTestProjectWithStatus(name, projects.StatusRunning)
	p.ImageRepository = projects.DefaultImageRepository
	p.ImageTag = tag
	p.ImageAutoFollow = true
	return p
}

func ago(d time.Duration) *time.Time {
	t := afNow.Add(-d)
	return &t
}

func TestParseSemver(t *testing.T) {
	ok := map[string]semver{"1.2.3": {1, 2, 3}, "v10.0.21": {10, 0, 21}, " 0.0.0 ": {0, 0, 0}}
	for in, want := range ok {
		got, valid := parseSemver(in)
		if !valid || got != want {
			t.Errorf("parseSemver(%q) = %v,%v want %v,true", in, got, valid, want)
		}
	}
	for _, in := range []string{"", "latest", "1.2", "1.2.3.4", "1.2.3-rc1", "v1.2.x", "sha-a671b8e", "1..3", "+1.2.3", "vv1.2.3"} {
		if _, valid := parseSemver(in); valid {
			t.Errorf("parseSemver(%q) accepted, want rejected", in)
		}
	}
}

func TestDecideAutoFollow(t *testing.T) {
	cfg := afConfig()
	cases := []struct {
		name string
		mut  func(p *projects.Project)
		want AutoFollowState
	}{
		{"behind, never used", func(p *projects.Project) {}, AutoFollowEligible},
		{"behind, idle past grace", func(p *projects.Project) { p.LastActivity = ago(31 * time.Minute) }, AutoFollowEligible},
		{"behind, active within grace", func(p *projects.Project) { p.LastActivity = ago(5 * time.Minute) }, AutoFollowBusy},
		{"v-prefixed tag compares as semver", func(p *projects.Project) { p.ImageTag = "v1.15.0" }, AutoFollowEligible},
		{"older major", func(p *projects.Project) { p.ImageTag = "0.99.99" }, AutoFollowEligible},
		{"same version", func(p *projects.Project) { p.ImageTag = "1.16.1" }, AutoFollowUpToDate},
		{"same version, v prefix", func(p *projects.Project) { p.ImageTag = "v1.16.1" }, AutoFollowUpToDate},
		{"ahead is never downgraded", func(p *projects.Project) { p.ImageTag = "1.17.0" }, AutoFollowAhead},
		{"opted out (pinned)", func(p *projects.Project) { p.ImageAutoFollow = false }, AutoFollowPinned},
		{"paused", func(p *projects.Project) { p.Paused = true }, AutoFollowPinned},
		{"non-semver tag", func(p *projects.Project) { p.ImageTag = "latest" }, AutoFollowUnmanaged},
		{"custom repository", func(p *projects.Project) { p.ImageRepository = "harbor.support.tools/other/img" }, AutoFollowUnmanaged},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := afProject("p", "1.15.0")
			tc.mut(p)
			if got := DecideAutoFollow(p, cfg, afNow); got != tc.want {
				t.Errorf("DecideAutoFollow = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAutoFollowConfig_Active(t *testing.T) {
	for _, tc := range []struct {
		cfg  AutoFollowConfig
		want bool
	}{
		{AutoFollowConfig{Enabled: true, RecommendedTag: "1.16.1"}, true},
		{AutoFollowConfig{Enabled: false, RecommendedTag: "1.16.1"}, false},
		{AutoFollowConfig{Enabled: true, RecommendedTag: "latest"}, false}, // gateway default
		{AutoFollowConfig{Enabled: true, RecommendedTag: ""}, false},
	} {
		if got := tc.cfg.Active(); got != tc.want {
			t.Errorf("%+v Active() = %v, want %v", tc.cfg, got, tc.want)
		}
	}
}

func newAutoFollowController(store projects.Store, af AutoFollowConfig) (*Controller, *fake.Clientset) {
	cfg := newControllerConfig()
	cfg.AutoFollow = af
	client := fake.NewSimpleClientset()
	return NewWithClient(cfg, store, client), client
}

func TestAutoFollowPass_UpgradesOnlyIdleProjects(t *testing.T) {
	store := projects.NewMockStore()
	add := func(p *projects.Project) *projects.Project { store.AddProject(p); return p }

	idle := add(afProject("idle", "1.15.0"))
	idleOld := add(afProject("idle-old-activity", "1.14.2"))
	idleOld.LastActivity = ago(3 * time.Hour)
	tab := add(afProject("open-tab", "1.15.0")) // user attached but quiet
	tab.LastActivity = ago(3 * time.Hour)
	store.SetActiveTabs("open-tab", 1)
	recent := add(afProject("recent", "1.15.0"))
	recent.LastActivity = ago(2 * time.Minute)
	pinned := add(afProject("pinned", "1.15.0"))
	pinned.ImageAutoFollow = false
	current := add(afProject("current", "1.16.1"))
	ahead := add(afProject("ahead", "1.17.0"))
	latest := add(afProject("latest", "latest"))
	updating := add(afProject("mid-rollout", "1.15.0"))
	updating.Status = projects.StatusUpdating // not listed as running; on the old tag, so no halt

	af := afConfig()
	af.MaxPerPass = 10
	c, client := newAutoFollowController(store, af)
	got := c.AutoFollowPass(context.Background(), afNow)

	want := map[string]AutoFollowState{
		"idle":              AutoFollowUpgraded,
		"idle-old-activity": AutoFollowUpgraded,
		"open-tab":          AutoFollowBusy,
		"recent":            AutoFollowBusy,
		"pinned":            AutoFollowPinned,
		"current":           AutoFollowUpToDate,
		"ahead":             AutoFollowAhead,
		"latest":            AutoFollowUnmanaged,
	}
	if len(got) != len(want) {
		t.Errorf("states = %v, want %v", got, want)
	}
	for name, w := range want {
		if got[name] != w {
			t.Errorf("%s: state %q, want %q", name, got[name], w)
		}
	}

	for _, p := range []*projects.Project{idle, idleOld} {
		if p.ImageTag != "1.16.1" || p.Status != projects.StatusUpdating {
			t.Errorf("%s: tag=%s status=%s, want 1.16.1/updating", p.Name, p.ImageTag, p.Status)
		}
	}
	for _, p := range []*projects.Project{tab, recent, pinned, current, ahead, latest, updating} {
		if p.Status == projects.StatusUpdating && p != updating {
			t.Errorf("%s was set to updating; it must not be touched", p.Name)
		}
	}
	if tab.ImageTag != "1.15.0" || recent.ImageTag != "1.15.0" || pinned.ImageTag != "1.15.0" ||
		ahead.ImageTag != "1.17.0" || latest.ImageTag != "latest" || updating.ImageTag != "1.15.0" {
		t.Errorf("a busy/pinned/ahead/unmanaged project's tag changed: tab=%s recent=%s pinned=%s ahead=%s latest=%s updating=%s",
			tab.ImageTag, recent.ImageTag, pinned.ImageTag, ahead.ImageTag, latest.ImageTag, updating.ImageTag)
	}

	// The pass only writes the DB; the reconcile loop rolls the Deployment.
	if n := len(client.Actions()); n != 0 {
		t.Errorf("auto-follow pass issued %d Kubernetes calls, want 0: %v", n, client.Actions())
	}

	if v := testutil.ToFloat64(metrics.SandboxImageAutoFollowProjects.WithLabelValues("busy")); v != 2 {
		t.Errorf("busy gauge = %v, want 2", v)
	}
	if v := testutil.ToFloat64(metrics.SandboxImageAutoFollowProjects.WithLabelValues("upgraded")); v != 2 {
		t.Errorf("upgraded gauge = %v, want 2", v)
	}
}

// A user who connects between the decision and the write still wins: the
// conditional UPDATE re-checks the tabs, so DecideAutoFollow's "eligible" is
// not enough on its own.
func TestAutoFollowPass_StoreRecheckBlocksLateSession(t *testing.T) {
	store := projects.NewMockStore()
	p := afProject("late-login", "1.15.0")
	store.AddProject(p)
	if DecideAutoFollow(p, afConfig(), afNow) != AutoFollowEligible {
		t.Fatal("precondition: project should look eligible from its row")
	}
	store.SetActiveTabs("late-login", 1)

	c, _ := newAutoFollowController(store, afConfig())
	got := c.AutoFollowPass(context.Background(), afNow)
	if got["late-login"] != AutoFollowBusy {
		t.Errorf("state = %q, want busy", got["late-login"])
	}
	if p.ImageTag != "1.15.0" || p.Status != projects.StatusRunning {
		t.Errorf("project changed to %s/%s; a live session must block the upgrade", p.ImageTag, p.Status)
	}
}

func TestAutoFollowPass_CapsAndSerializesRollouts(t *testing.T) {
	store := projects.NewMockStore()
	all := map[string]*projects.Project{}
	for _, n := range []string{"a", "b", "c", "d", "e"} {
		all[n] = afProject(n, "1.15.0")
		store.AddProject(all[n])
	}
	af := afConfig()
	af.MaxPerPass = 2
	c, _ := newAutoFollowController(store, af)

	tally := func() map[AutoFollowState]int {
		counts := map[AutoFollowState]int{}
		for _, s := range c.AutoFollowPass(context.Background(), afNow) {
			counts[s]++
		}
		return counts
	}

	if got := tally(); got[AutoFollowUpgraded] != 2 || got[AutoFollowDeferred] != 3 {
		t.Errorf("pass 1 = %v, want 2 upgraded, 3 deferred", got)
	}
	// The two upgraded projects are still rolling out: nothing new starts.
	if got := tally(); got[AutoFollowHalted] != 3 || got[AutoFollowUpgraded] != 0 {
		t.Errorf("pass 2 = %v, want 3 halted while the first rollouts are in flight", got)
	}
	// Both reach running on the new image: the next batch goes.
	for _, p := range all {
		if p.Status == projects.StatusUpdating {
			p.Status = projects.StatusRunning
		}
	}
	if got := tally(); got[AutoFollowUpgraded] != 2 || got[AutoFollowDeferred] != 1 || got[AutoFollowUpToDate] != 2 {
		t.Errorf("pass 3 = %v, want 2 upgraded, 1 deferred, 2 up_to_date", got)
	}
}

// A project on the recommended tag that is stuck creating (bad image, pull
// failure) or failed halts auto-follow: the canary protects everyone else.
func TestAutoFollowPass_HaltsOnStuckOrFailedCanary(t *testing.T) {
	for _, st := range []projects.ProjectStatus{projects.StatusCreating, projects.StatusFailed, projects.StatusUpdating, projects.StatusPending} {
		t.Run(string(st), func(t *testing.T) {
			store := projects.NewMockStore()
			canary := afProject("canary", "v1.16.1") // v-prefix still matches
			canary.Status = st
			store.AddProject(canary)
			idle := afProject("idle", "1.15.0")
			store.AddProject(idle)

			c, _ := newAutoFollowController(store, afConfig())
			got := c.AutoFollowPass(context.Background(), afNow)
			if got["idle"] != AutoFollowHalted {
				t.Errorf("idle state = %q, want halted", got["idle"])
			}
			if idle.ImageTag != "1.15.0" || idle.Status != projects.StatusRunning {
				t.Errorf("idle project changed to %s/%s while the canary is %s", idle.ImageTag, idle.Status, st)
			}
		})
	}
}

func TestAutoFollowPass_InactiveDoesNothing(t *testing.T) {
	for _, af := range []AutoFollowConfig{
		{Enabled: false, RecommendedTag: "1.16.1"},
		{Enabled: true, RecommendedTag: "latest"},
	} {
		store := projects.NewMockStore()
		p := afProject("idle", "1.15.0")
		store.AddProject(p)
		c, _ := newAutoFollowController(store, af)
		if got := c.AutoFollowPass(context.Background(), afNow); len(got) != 0 {
			t.Errorf("%+v: pass returned %v, want nothing", af, got)
		}
		if p.ImageTag != "1.15.0" || p.Status != projects.StatusRunning {
			t.Errorf("%+v: project changed to %s/%s", af, p.ImageTag, p.Status)
		}
	}
}

func TestAutoFollowPass_StoreErrorCountsAsError(t *testing.T) {
	store := &errOnAutoFollowStore{MockStore: projects.NewMockStore()}
	p := afProject("idle", "1.15.0")
	store.AddProject(p)
	c, _ := newAutoFollowController(store, afConfig())

	before := testutil.ToFloat64(metrics.SandboxImageAutoFollowTotal.WithLabelValues("error"))
	got := c.AutoFollowPass(context.Background(), afNow)
	if got["idle"] != AutoFollowError {
		t.Errorf("state = %q, want error", got["idle"])
	}
	if after := testutil.ToFloat64(metrics.SandboxImageAutoFollowTotal.WithLabelValues("error")); after != before+1 {
		t.Errorf("error counter %v -> %v, want +1", before, after)
	}
}

type errOnAutoFollowStore struct{ *projects.MockStore }

func (s *errOnAutoFollowStore) AutoFollowImage(context.Context, uuid.UUID, string, string, time.Time) (bool, error) {
	return false, errors.New("db down")
}
