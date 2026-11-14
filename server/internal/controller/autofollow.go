package controller

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/supporttools/KubeTTY/server/internal/projects"
	"github.com/supporttools/KubeTTY/server/internal/shared/metrics"
)

// Sandbox image auto-follow (cluster-services-1376).
//
// Operator decision 2026-11-13: projects follow the gateway's
// RECOMMENDED_IMAGE_TAG automatically, but a pod with a live user is never
// restarted. The loop runs on the leader only. For each running project it
// decides a state (see AutoFollowState). An eligible project gets one
// conditional UPDATE (projects.Store.AutoFollowImage) that sets the new tag and
// status=updating only if the project is still idle at that instant: no open
// gateway tab and no activity within IdleGrace. The existing updating pass
// then rolls the Deployment, as a manual upgrade from the admin UI does.
//
// Rollouts are serialized and self-halting. While any project on the
// recommended tag is still pending, creating, updating, syncing or failed, no
// new auto-upgrade starts (state "halted"). The first project to move is the
// canary: if the recommended image cannot be pulled or does not start, that
// project never reaches running and auto-follow stops there instead of
// rolling every idle sandbox onto a broken image. RECOMMENDED_IMAGE_TAG on a
// main-branch deploy is the newest git tag, whose image build can still be in
// flight, so this matters.
//
// It never downgrades, never touches a project whose tag or repository it
// cannot compare (non-semver tag such as "latest", or a custom repository),
// and skips projects with image_auto_follow=false (pinned) or paused ones.

// Auto-follow defaults used when the AutoFollowConfig fields are zero.
const (
	DefaultAutoFollowInterval   = 5 * time.Minute
	DefaultAutoFollowIdleGrace  = 30 * time.Minute
	DefaultAutoFollowMaxPerPass = 1
)

// AutoFollowConfig configures the sandbox image auto-follow loop.
type AutoFollowConfig struct {
	// Enabled turns the loop on. RecommendedTag must also be a strict semver.
	Enabled bool
	// RecommendedTag is the gateway's RECOMMENDED_IMAGE_TAG.
	RecommendedTag string
	// Repository is the image repository the recommended tag belongs to.
	// Empty means projects.DefaultImageRepository.
	Repository string
	// Interval between passes. Zero means DefaultAutoFollowInterval.
	Interval time.Duration
	// IdleGrace is how long a project must have had no terminal activity.
	// Zero means DefaultAutoFollowIdleGrace.
	IdleGrace time.Duration
	// MaxPerPass caps upgrades started per pass so a new release does not
	// roll every sandbox at once. Zero means DefaultAutoFollowMaxPerPass.
	MaxPerPass int
}

func (a AutoFollowConfig) interval() time.Duration {
	if a.Interval > 0 {
		return a.Interval
	}
	return DefaultAutoFollowInterval
}

func (a AutoFollowConfig) idleGrace() time.Duration {
	if a.IdleGrace > 0 {
		return a.IdleGrace
	}
	return DefaultAutoFollowIdleGrace
}

func (a AutoFollowConfig) maxPerPass() int {
	if a.MaxPerPass > 0 {
		return a.MaxPerPass
	}
	return DefaultAutoFollowMaxPerPass
}

func (a AutoFollowConfig) repository() string {
	if a.Repository != "" {
		return a.Repository
	}
	return projects.DefaultImageRepository
}

// Active reports whether the loop should run: enabled, with a semver
// recommended tag to follow.
func (a AutoFollowConfig) Active() bool {
	_, ok := parseSemver(a.RecommendedTag)
	return a.Enabled && ok
}

// AutoFollowState is what the auto-follow pass decided for one project.
type AutoFollowState string

const (
	AutoFollowUpToDate  AutoFollowState = "up_to_date"
	AutoFollowAhead     AutoFollowState = "ahead"
	AutoFollowPinned    AutoFollowState = "pinned"
	AutoFollowUnmanaged AutoFollowState = "unmanaged"
	AutoFollowBusy      AutoFollowState = "busy"
	AutoFollowEligible  AutoFollowState = "eligible"
	AutoFollowUpgraded  AutoFollowState = "upgraded"
	AutoFollowDeferred  AutoFollowState = "deferred"
	AutoFollowHalted    AutoFollowState = "halted"
	AutoFollowError     AutoFollowState = "error"
)

// autoFollowStates lists every state reported in the projects gauge, so a
// state that drops to zero is reset rather than left at its last value.
var autoFollowStates = []AutoFollowState{
	AutoFollowUpToDate, AutoFollowAhead, AutoFollowPinned, AutoFollowUnmanaged,
	AutoFollowBusy, AutoFollowUpgraded, AutoFollowDeferred, AutoFollowHalted, AutoFollowError,
}

// inFlightStatuses are the statuses of a project whose rollout has not
// finished (or has failed). A project in one of these on the recommended tag
// halts auto-follow.
var inFlightStatuses = []projects.ProjectStatus{
	projects.StatusPending, projects.StatusSyncing, projects.StatusCreating,
	projects.StatusUpdating, projects.StatusFailed,
}

// DecideAutoFollow is the pure, pre-write part of the decision. It reads only
// the project row: the store's conditional UPDATE re-checks idleness (tabs and
// last activity) atomically, so a user who connects after this decision still
// blocks the write.
func DecideAutoFollow(p *projects.Project, cfg AutoFollowConfig, now time.Time) AutoFollowState {
	if !p.ImageAutoFollow || p.Paused {
		return AutoFollowPinned
	}
	if p.ImageRepository != cfg.repository() {
		return AutoFollowUnmanaged
	}
	cur, ok := parseSemver(p.ImageTag)
	if !ok {
		return AutoFollowUnmanaged
	}
	want, ok := parseSemver(cfg.RecommendedTag)
	if !ok {
		return AutoFollowUnmanaged
	}
	switch c := compareSemver(cur, want); {
	case c == 0:
		return AutoFollowUpToDate
	case c > 0:
		return AutoFollowAhead
	}
	if p.LastActivity != nil && now.Sub(*p.LastActivity) < cfg.idleGrace() {
		return AutoFollowBusy
	}
	return AutoFollowEligible
}

func (c *Controller) runAutoFollowLoop(ctx context.Context) {
	defer c.wg.Done()

	af := c.cfg.AutoFollow
	ticker := time.NewTicker(af.interval())
	defer ticker.Stop()

	log.WithField("interval", af.interval()).
		WithField("recommended_tag", af.RecommendedTag).
		WithField("idle_grace", af.idleGrace()).
		Info("Starting sandbox image auto-follow loop (upgrades idle projects only)")

	c.AutoFollowPass(ctx, time.Now())
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopCh:
			return
		case t := <-ticker.C:
			c.AutoFollowPass(ctx, t)
		}
	}
}

// AutoFollowPass runs one auto-follow pass over the running projects and
// returns the state decided for each, keyed by project name.
func (c *Controller) AutoFollowPass(ctx context.Context, now time.Time) map[string]AutoFollowState {
	af := c.cfg.AutoFollow
	result := map[string]AutoFollowState{}
	if !af.Active() {
		return result
	}

	list, err := c.store.ListByStatuses(ctx, []projects.ProjectStatus{projects.StatusRunning})
	if err != nil {
		log.WithError(err).Error("Auto-follow: failed to list running projects")
		return result
	}

	blocker, err := c.recommendedRolloutInFlight(ctx, af)
	if err != nil {
		log.WithError(err).Error("Auto-follow: failed to check in-flight rollouts; skipping this pass")
		return result
	}

	idleBefore := now.Add(-af.idleGrace())
	started := 0
	for i := range list {
		p := &list[i]
		state := DecideAutoFollow(p, af, now)
		if state == AutoFollowEligible {
			if blocker != nil {
				state = AutoFollowHalted
			} else {
				state = c.autoFollowOne(ctx, p, af, idleBefore, &started)
			}
		}
		result[p.Name] = state
	}
	if blocker != nil && countState(result, AutoFollowHalted) > 0 {
		log.WithField("blocking_project", blocker.Name).
			WithField("blocking_status", blocker.Status).
			WithField("recommended_tag", af.RecommendedTag).
			WithField("halted", countState(result, AutoFollowHalted)).
			Warn("Auto-follow: halted until this project's rollout to the recommended tag reaches running")
	}

	counts := map[AutoFollowState]int{}
	for _, s := range result {
		counts[s]++
	}
	for _, s := range autoFollowStates {
		metrics.SandboxImageAutoFollowProjects.WithLabelValues(string(s)).Set(float64(counts[s]))
	}
	return result
}

// recommendedRolloutInFlight returns a project on the recommended tag whose
// rollout has not reached running (or failed), or nil if there is none.
func (c *Controller) recommendedRolloutInFlight(ctx context.Context, af AutoFollowConfig) (*projects.Project, error) {
	want, _ := parseSemver(af.RecommendedTag)
	list, err := c.store.ListByStatuses(ctx, inFlightStatuses)
	if err != nil {
		return nil, err
	}
	for i := range list {
		if v, ok := parseSemver(list[i].ImageTag); ok && v == want {
			return &list[i], nil
		}
	}
	return nil, nil
}

func countState(m map[string]AutoFollowState, s AutoFollowState) int {
	n := 0
	for _, v := range m {
		if v == s {
			n++
		}
	}
	return n
}

func (c *Controller) autoFollowOne(ctx context.Context, p *projects.Project, af AutoFollowConfig, idleBefore time.Time, started *int) AutoFollowState {
	if *started >= af.maxPerPass() {
		return AutoFollowDeferred
	}
	applied, err := c.store.AutoFollowImage(ctx, p.ID, p.ImageTag, af.RecommendedTag, idleBefore)
	if err != nil {
		metrics.SandboxImageAutoFollowTotal.WithLabelValues("error").Inc()
		log.WithError(err).WithField("project", p.Name).Warn("Auto-follow: upgrade write failed")
		return AutoFollowError
	}
	if !applied {
		// An open gateway tab, fresh activity, or a concurrent change
		// (status, tag, opt-out) made the conditional UPDATE a no-op.
		return AutoFollowBusy
	}
	*started++
	metrics.SandboxImageAutoFollowTotal.WithLabelValues("upgraded").Inc()
	log.WithField("project", p.Name).
		WithField("from_tag", p.ImageTag).
		WithField("to_tag", af.RecommendedTag).
		Info("Auto-follow: idle project upgraded to the recommended sandbox image")
	return AutoFollowUpgraded
}

// semver is a parsed strict X.Y.Z version (an optional leading "v").
type semver [3]int

// parseSemver accepts the same strict versions as the admin upgrade API:
// v1.2.3 or 1.2.3, no pre-release or build suffix.
func parseSemver(s string) (semver, bool) {
	var v semver
	parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(s), "v"), ".")
	if len(parts) != 3 {
		return v, false
	}
	for i, part := range parts {
		if part == "" || strings.TrimLeft(part, "0123456789") != "" {
			return v, false
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			return v, false
		}
		v[i] = n
	}
	return v, true
}

func compareSemver(a, b semver) int {
	for i := range a {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}
