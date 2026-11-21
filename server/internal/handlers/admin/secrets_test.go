package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/supporttools/KubeTTY/server/internal/controller"
	"github.com/supporttools/KubeTTY/server/internal/projects"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func secretsHandlers(t *testing.T, data map[string]string, esoOwned bool) (*ProjectHandlers, *projects.Project) {
	t.Helper()
	store := newMockProjectStore()
	p := runningProject()
	store.projects[p.ID] = p

	cfg := controller.DefaultConfig()
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
		Name:      cfg.ResourceConfig.EnvSecretName(p.Name),
		Namespace: cfg.ResourceConfig.Namespace,
	}, Data: map[string][]byte{}}
	for k, v := range data {
		secret.Data[k] = []byte(v)
	}
	if esoOwned {
		secret.OwnerReferences = []metav1.OwnerReference{{APIVersion: "external-secrets.io/v1", Kind: "ExternalSecret", Name: secret.Name, UID: "eso-uid"}}
	}

	client := fake.NewSimpleClientset(secret)
	return NewProjectHandlers(store, controller.NewWithClient(cfg, store, client), "v1.0.0"), p
}

func callSecrets(h *ProjectHandlers, p *projects.Project, method, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/api/admin/projects/"+p.ID.String()+"/secrets", strings.NewReader(body))
	req.SetPathValue("id", p.ID.String())
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	if method == http.MethodGet {
		h.GetProjectSecrets(rec, req)
	} else {
		h.UpdateProjectSecrets(rec, req)
	}
	return rec
}

func TestGetProjectSecrets_ReturnsKeyNamesOnly(t *testing.T) {
	h, p := secretsHandlers(t, map[string]string{"OPENAI_API_KEY": "sk-secret-value"}, false)

	rec := callSecrets(h, p, http.MethodGet, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `{"keys":["OPENAI_API_KEY"],"externallyManaged":false}` {
		t.Errorf("body = %s", got)
	}
}

func TestUpdateProjectSecrets_ExternalSecretOwnedIs409(t *testing.T) {
	h, p := secretsHandlers(t, map[string]string{"OPENAI_API_KEY": "from-vault"}, true)

	rec := callSecrets(h, p, http.MethodPut, `{"secrets": {"OPENAI_API_KEY": "hand-edit"}}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "ExternalSecret") {
		t.Errorf("409 body does not explain why: %s", rec.Body.String())
	}
}

func TestUpdateProjectSecrets_KeepingUnknownKeyIs400(t *testing.T) {
	h, p := secretsHandlers(t, map[string]string{"KEEP": "kept"}, false)

	rec := callSecrets(h, p, http.MethodPut, `{"secrets": {"KEEP": null, "TYPO": null}}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if msg, _ := body["message"].(string); !strings.Contains(msg, "TYPO") {
		t.Errorf("400 body does not name the key: %s", rec.Body.String())
	}
}

func TestCreateProject_RejectsNamespaceAccess(t *testing.T) {
	for _, field := range []string{"adminNamespaces", "readNamespaces"} {
		t.Run(field, func(t *testing.T) {
			store := newMockProjectStore()
			h := NewProjectHandlers(store, nil, "v1.0.0")

			body := `{"name": "ns-project", "displayName": "NS", "userName": "tester", "` + field + `": ["app-staging"]}`
			req := httptest.NewRequest(http.MethodPost, "/api/admin/projects", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			h.CreateProject(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body %s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), "not supported") {
				t.Errorf("400 body does not explain why: %s", rec.Body.String())
			}
			if len(store.projects) != 0 {
				t.Errorf("project was created despite the rejection")
			}
		})
	}
}
