package controller

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/supporttools/KubeTTY/server/internal/projects"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

const restartedAtAnnotation = "kubectl.kubernetes.io/restartedAt"

type secretsFixture struct {
	ctrl    *Controller
	client  *fake.Clientset
	cfg     Config
	project *projects.Project
}

func newSecretsFixture(t *testing.T, name string) secretsFixture {
	t.Helper()
	client := fake.NewSimpleClientset()
	store := newMockStore()
	cfg := newControllerConfig()
	ctrl := NewWithClient(cfg, store, client)

	project := newTestProjectWithStatus(name, projects.StatusPending)
	store.addProject(project)
	if err := ctrl.handlePending(context.Background(), project); err != nil {
		t.Fatalf("handlePending failed: %v", err)
	}
	project.Status = projects.StatusRunning
	return secretsFixture{ctrl: ctrl, client: client, cfg: cfg, project: project}
}

func (f secretsFixture) envSecret(t *testing.T) *corev1.Secret {
	t.Helper()
	rc := f.cfg.ResourceConfig
	s, err := f.client.CoreV1().Secrets(rc.Namespace).Get(context.Background(), rc.EnvSecretName(f.project.Name), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get env secret: %v", err)
	}
	return s
}

func (f secretsFixture) setEnvSecret(t *testing.T, data map[string]string, esoOwned bool) {
	t.Helper()
	s := f.envSecret(t)
	s.Data = map[string][]byte{}
	for k, v := range data {
		s.Data[k] = []byte(v)
	}
	if esoOwned {
		controller := true
		s.OwnerReferences = []metav1.OwnerReference{{
			APIVersion: "external-secrets.io/v1",
			Kind:       "ExternalSecret",
			Name:       s.Name,
			UID:        "eso-uid",
			Controller: &controller,
		}}
	}
	if _, err := f.client.CoreV1().Secrets(s.Namespace).Update(context.Background(), s, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("update env secret: %v", err)
	}
}

func (f secretsFixture) restarted(t *testing.T) bool {
	t.Helper()
	rc := f.cfg.ResourceConfig
	d, err := f.client.AppsV1().Deployments(rc.Namespace).Get(context.Background(), rc.ResourceName(f.project.Name), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get deployment: %v", err)
	}
	return d.Spec.Template.Annotations[restartedAtAnnotation] != ""
}

func dataOf(s *corev1.Secret) map[string]string {
	out := map[string]string{}
	for k, v := range s.Data {
		out[k] = string(v)
	}
	return out
}

func strPtr(s string) *string { return &s }

func TestGetProjectSecrets_ReturnsSortedKeyNamesWithoutValues(t *testing.T) {
	f := newSecretsFixture(t, "secrets-get")
	f.setEnvSecret(t, map[string]string{"OPENAI_API_KEY": "sk-value-one", "ANTHROPIC_BASE_URL": "http://value-two"}, false)

	got, err := f.ctrl.GetProjectSecrets(context.Background(), f.project)
	if err != nil {
		t.Fatalf("GetProjectSecrets: %v", err)
	}
	want := ProjectSecrets{Keys: []string{"ANTHROPIC_BASE_URL", "OPENAI_API_KEY"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GetProjectSecrets = %+v, want %+v", got, want)
	}

	body, _ := json.Marshal(got)
	for _, value := range []string{"sk-value-one", "http://value-two"} {
		if strings.Contains(string(body), value) {
			t.Errorf("response %s leaks a secret value", body)
		}
	}
}

func TestGetProjectSecrets_MissingSecretIsEmptyList(t *testing.T) {
	f := newSecretsFixture(t, "secrets-missing")
	rc := f.cfg.ResourceConfig
	if err := f.client.CoreV1().Secrets(rc.Namespace).Delete(context.Background(), rc.EnvSecretName(f.project.Name), metav1.DeleteOptions{}); err != nil {
		t.Fatalf("delete env secret: %v", err)
	}

	got, err := f.ctrl.GetProjectSecrets(context.Background(), f.project)
	if err != nil {
		t.Fatalf("GetProjectSecrets: %v", err)
	}
	body, _ := json.Marshal(got)
	if string(body) != `{"keys":[],"externallyManaged":false}` {
		t.Errorf("GetProjectSecrets JSON = %s", body)
	}
}

func TestGetProjectSecrets_ReportsExternalSecretOwnership(t *testing.T) {
	f := newSecretsFixture(t, "secrets-eso-get")
	f.setEnvSecret(t, map[string]string{"OPENAI_API_KEY": "from-vault"}, true)

	got, err := f.ctrl.GetProjectSecrets(context.Background(), f.project)
	if err != nil {
		t.Fatalf("GetProjectSecrets: %v", err)
	}
	if !got.ExternallyManaged || !reflect.DeepEqual(got.Keys, []string{"OPENAI_API_KEY"}) {
		t.Errorf("GetProjectSecrets = %+v, want externally managed with key OPENAI_API_KEY", got)
	}
}

func TestUpdateProjectSecrets_KeepsReplacesAddsAndDropsKeys(t *testing.T) {
	f := newSecretsFixture(t, "secrets-update")
	f.setEnvSecret(t, map[string]string{"KEEP": "kept", "CHANGE": "old", "DROP": "gone"}, false)

	err := f.ctrl.UpdateProjectSecrets(context.Background(), f.project, map[string]*string{
		"KEEP":   nil,
		"CHANGE": strPtr("new"),
		"ADD":    strPtr("added"),
	})
	if err != nil {
		t.Fatalf("UpdateProjectSecrets: %v", err)
	}

	want := map[string]string{"KEEP": "kept", "CHANGE": "new", "ADD": "added"}
	if got := dataOf(f.envSecret(t)); !reflect.DeepEqual(got, want) {
		t.Errorf("secret data = %v, want %v", got, want)
	}
	if !f.restarted(t) {
		t.Error("project was not restarted after a secrets update")
	}
}

func TestUpdateProjectSecrets_CreatesMissingSecret(t *testing.T) {
	f := newSecretsFixture(t, "secrets-create")
	rc := f.cfg.ResourceConfig
	if err := f.client.CoreV1().Secrets(rc.Namespace).Delete(context.Background(), rc.EnvSecretName(f.project.Name), metav1.DeleteOptions{}); err != nil {
		t.Fatalf("delete env secret: %v", err)
	}

	if err := f.ctrl.UpdateProjectSecrets(context.Background(), f.project, map[string]*string{"API_KEY": strPtr("v")}); err != nil {
		t.Fatalf("UpdateProjectSecrets: %v", err)
	}
	s := f.envSecret(t)
	if got := dataOf(s); !reflect.DeepEqual(got, map[string]string{"API_KEY": "v"}) {
		t.Errorf("secret data = %v", got)
	}
	if s.Labels[labelProject] != f.project.Name {
		t.Errorf("created secret labels = %v, want project label", s.Labels)
	}
}

func TestUpdateProjectSecrets_KeepingAnUnknownKeyFails(t *testing.T) {
	f := newSecretsFixture(t, "secrets-unknown")
	f.setEnvSecret(t, map[string]string{"KEEP": "kept"}, false)

	err := f.ctrl.UpdateProjectSecrets(context.Background(), f.project, map[string]*string{"KEEP": nil, "TYPO": nil})
	if !errors.Is(err, ErrUnknownSecretKey) || !strings.Contains(err.Error(), "TYPO") {
		t.Fatalf("err = %v, want ErrUnknownSecretKey naming TYPO", err)
	}
	if got := dataOf(f.envSecret(t)); !reflect.DeepEqual(got, map[string]string{"KEEP": "kept"}) {
		t.Errorf("secret changed after a rejected update: %v", got)
	}
}

func TestUpdateProjectSecrets_RefusesExternalSecretOwnedSecret(t *testing.T) {
	f := newSecretsFixture(t, "secrets-eso-put")
	f.setEnvSecret(t, map[string]string{"OPENAI_API_KEY": "from-vault"}, true)

	err := f.ctrl.UpdateProjectSecrets(context.Background(), f.project, map[string]*string{"OPENAI_API_KEY": strPtr("hand-edit")})
	if !errors.Is(err, ErrSecretExternallyManaged) {
		t.Fatalf("err = %v, want ErrSecretExternallyManaged", err)
	}
	if got := dataOf(f.envSecret(t)); got["OPENAI_API_KEY"] != "from-vault" {
		t.Errorf("ESO-owned secret was modified: %v", got)
	}
	if f.restarted(t) {
		t.Error("project restarted although the update was refused")
	}
}

func TestHandleDeleting_EnvSecretOwnership(t *testing.T) {
	for _, tc := range []struct {
		name     string
		esoOwned bool
		wantKept bool
	}{
		{name: "plain secret is deleted", esoOwned: false, wantKept: false},
		{name: "ExternalSecret-owned secret is kept", esoOwned: true, wantKept: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newSecretsFixture(t, "secrets-delete")
			f.setEnvSecret(t, map[string]string{"K": "v"}, tc.esoOwned)

			f.project.Status = projects.StatusDeleting
			if err := f.ctrl.handleDeleting(context.Background(), f.project); err != nil {
				t.Fatalf("handleDeleting: %v", err)
			}

			rc := f.cfg.ResourceConfig
			_, err := f.client.CoreV1().Secrets(rc.Namespace).Get(context.Background(), rc.EnvSecretName(f.project.Name), metav1.GetOptions{})
			if kept := err == nil; kept != tc.wantKept {
				t.Errorf("env secret kept = %v, want %v (get err: %v)", kept, tc.wantKept, err)
			}
			if err != nil && !apierrors.IsNotFound(err) {
				t.Errorf("unexpected get error: %v", err)
			}
		})
	}
}
