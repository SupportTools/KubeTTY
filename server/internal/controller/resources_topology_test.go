package controller

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

// TestBuildDeployment_TopologySpread checks that project pods carry a soft
// spread constraint across nodes, and that its selector actually matches the
// pods it is meant to spread.
func TestBuildDeployment_TopologySpread(t *testing.T) {
	p := testProject("spread")
	cfg := testConfig()

	deploy := BuildDeployment(p, cfg, cfg.EnvSecretName(p.Name))
	podSpec := deploy.Spec.Template.Spec

	if len(podSpec.TopologySpreadConstraints) != 1 {
		t.Fatalf("TopologySpreadConstraints = %d entries, expected 1", len(podSpec.TopologySpreadConstraints))
	}
	tsc := podSpec.TopologySpreadConstraints[0]

	// A hard constraint (DoNotSchedule) could leave a sandbox Pending when the
	// cluster cannot satisfy the skew; it must stay a scoring preference.
	if tsc.WhenUnsatisfiable != corev1.ScheduleAnyway {
		t.Errorf("WhenUnsatisfiable = %q, expected %q", tsc.WhenUnsatisfiable, corev1.ScheduleAnyway)
	}
	if tsc.MaxSkew != 1 {
		t.Errorf("MaxSkew = %d, expected 1", tsc.MaxSkew)
	}
	if tsc.TopologyKey != "kubernetes.io/hostname" {
		t.Errorf("TopologyKey = %q, expected kubernetes.io/hostname", tsc.TopologyKey)
	}
	if tsc.LabelSelector == nil {
		t.Fatal("LabelSelector is nil")
	}

	sel, err := metav1.LabelSelectorAsSelector(tsc.LabelSelector)
	if err != nil {
		t.Fatalf("LabelSelector is invalid: %v", err)
	}

	// The scheduler only counts pods the selector matches. A selector the pod
	// template does not satisfy silently spreads nothing.
	podLabels := labels.Set(deploy.Spec.Template.Labels)
	if !sel.Matches(podLabels) {
		t.Errorf("spread selector %q does not match the pod template labels %v", sel, deploy.Spec.Template.Labels)
	}

	// Another environment's projects must not count toward this one's skew.
	otherEnv := testConfig()
	otherEnv.Env = "prd"
	other := BuildDeployment(testProject("other"), otherEnv, otherEnv.EnvSecretName("other"))
	if sel.Matches(labels.Set(other.Spec.Template.Labels)) {
		t.Errorf("spread selector %q matches a pod from environment %q", sel, otherEnv.Env)
	}

	// A different project in the same environment must count.
	peer := BuildDeployment(testProject("peer"), cfg, cfg.EnvSecretName("peer"))
	if !sel.Matches(labels.Set(peer.Spec.Template.Labels)) {
		t.Errorf("spread selector %q does not match a peer project pod in the same environment", sel)
	}
}

// TestBuildDeployment_SelectorUnchanged guards the Deployment selector: it is
// immutable on existing Deployments, so the labels added for the spread
// constraint must go on the pod template only.
func TestBuildDeployment_SelectorUnchanged(t *testing.T) {
	p := testProject("immutable")
	cfg := testConfig()

	deploy := BuildDeployment(p, cfg, cfg.EnvSecretName(p.Name))

	want := projectSelectorLabels(p, cfg)
	got := deploy.Spec.Selector.MatchLabels
	if len(got) != len(want) {
		t.Errorf("Deployment selector = %v, expected exactly %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("Deployment selector %s = %q, expected %q", k, got[k], v)
		}
	}

	sel, err := metav1.LabelSelectorAsSelector(deploy.Spec.Selector)
	if err != nil {
		t.Fatalf("Deployment selector is invalid: %v", err)
	}
	if !sel.Matches(labels.Set(deploy.Spec.Template.Labels)) {
		t.Errorf("Deployment selector %q does not match its pod template labels %v", sel, deploy.Spec.Template.Labels)
	}
}
