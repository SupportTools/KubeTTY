package github

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type fakeGitHub struct {
	t           *testing.T
	key         *rsa.PrivateKey
	mu          sync.Mutex
	tokenMints  int
	gotVerifier string
	orgs        map[string]bool   // "org/login" -> active
	teams       map[string]string // "org/team/login" -> state
	perms       map[string]string // "owner/repo/login" -> permission
	installed   map[string]bool   // org or owner
	calls       map[string]int
}

func newFakeGitHub(t *testing.T) (*fakeGitHub, *httptest.Server, Config) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeGitHub{t: t, key: key, orgs: map[string]bool{}, teams: map[string]string{}, perms: map[string]string{}, installed: map[string]bool{"SupportTools": true}, calls: map[string]int{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	return f, srv, Config{ClientID: "Iv1.test", ClientSecret: "s3cret", PrivateKeyPEM: pemBytes, APIURL: srv.URL, WebURL: srv.URL}
}

func (f *fakeGitHub) appJWTValid(r *http.Request) bool {
	raw := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	claims := jwt.RegisteredClaims{}
	_, err := jwt.ParseWithClaims(raw, &claims, func(*jwt.Token) (any, error) { return &f.key.PublicKey, nil },
		jwt.WithValidMethods([]string{"RS256"}), jwt.WithIssuer("Iv1.test"))
	return err == nil
}

func (f *fakeGitHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls[r.URL.Path]++
	p := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	reply := func(code int, v any) {
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(v)
	}
	switch {
	case r.URL.Path == "/login/oauth/access_token":
		_ = r.ParseForm()
		f.gotVerifier = r.Form.Get("code_verifier")
		if r.Form.Get("code") != "good-code" || r.Form.Get("client_secret") != "s3cret" {
			reply(http.StatusOK, map[string]string{"error": "bad_verification_code"})
			return
		}
		reply(http.StatusOK, map[string]string{"access_token": "ghu_user"})
	case r.URL.Path == "/user":
		if r.Header.Get("Authorization") != "token ghu_user" {
			reply(http.StatusUnauthorized, nil)
			return
		}
		reply(http.StatusOK, map[string]any{"id": 42, "login": "octo"})
	case len(p) == 3 && p[2] == "installation":
		if !f.appJWTValid(r) {
			reply(http.StatusUnauthorized, nil)
			return
		}
		if !f.installed[p[1]] {
			reply(http.StatusNotFound, nil)
			return
		}
		reply(http.StatusOK, map[string]any{"id": 7})
	case len(p) == 4 && p[0] == "repos" && p[3] == "installation":
		if !f.appJWTValid(r) || !f.installed[p[1]] {
			reply(http.StatusNotFound, nil)
			return
		}
		reply(http.StatusOK, map[string]any{"id": 7})
	case r.URL.Path == "/app/installations/7/access_tokens" && r.Method == http.MethodPost:
		if !f.appJWTValid(r) {
			reply(http.StatusUnauthorized, nil)
			return
		}
		f.tokenMints++
		reply(http.StatusCreated, map[string]any{"token": "ghs_inst", "expires_at": time.Now().Add(time.Hour)})
	case r.Header.Get("Authorization") != "token ghs_inst":
		reply(http.StatusUnauthorized, nil)
	case len(p) == 4 && p[0] == "orgs" && p[2] == "memberships":
		if !f.orgs[p[1]+"/"+p[3]] {
			reply(http.StatusNotFound, nil)
			return
		}
		reply(http.StatusOK, map[string]string{"state": "active"})
	case len(p) == 6 && p[0] == "orgs" && p[2] == "teams" && p[4] == "memberships":
		state, ok := f.teams[p[1]+"/"+p[3]+"/"+p[5]]
		if !ok {
			reply(http.StatusNotFound, nil)
			return
		}
		reply(http.StatusOK, map[string]string{"state": state})
	case len(p) == 6 && p[0] == "repos" && p[3] == "collaborators" && p[5] == "permission":
		perm, ok := f.perms[p[1]+"/"+p[2]+"/"+p[4]]
		if !ok {
			reply(http.StatusNotFound, nil)
			return
		}
		reply(http.StatusOK, map[string]string{"permission": perm})
	default:
		f.t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		reply(http.StatusNotFound, nil)
	}
}

func TestSignIn(t *testing.T) {
	f, _, cfg := newFakeGitHub(t)
	c, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	id, err := c.SignIn(context.Background(), "good-code", "verifier-123", "https://kubetty.example/cb")
	if err != nil || id.ID != 42 || id.Login != "octo" {
		t.Fatalf("got %+v %v", id, err)
	}
	if f.gotVerifier != "verifier-123" {
		t.Fatalf("PKCE verifier not sent: %q", f.gotVerifier)
	}
	if _, err := c.SignIn(context.Background(), "bad-code", "v", "https://kubetty.example/cb"); err == nil {
		t.Fatal("a refused code must fail")
	}
}

func TestAuthorizeURL(t *testing.T) {
	_, _, cfg := newFakeGitHub(t)
	c, _ := New(cfg)
	u := c.AuthorizeURL("https://kubetty.example/api/auth/github/callback", "st", "ch")
	for _, want := range []string{"client_id=Iv1.test", "state=st", "code_challenge=ch", "code_challenge_method=S256", "redirect_uri=https%3A%2F%2Fkubetty.example%2Fapi%2Fauth%2Fgithub%2Fcallback"} {
		if !strings.Contains(u, want) {
			t.Errorf("authorize URL %q lacks %q", u, want)
		}
	}
}

func TestMembershipAndPermissionLookups(t *testing.T) {
	f, _, cfg := newFakeGitHub(t)
	f.orgs["SupportTools/octo"] = true
	f.teams["SupportTools/admins/octo"] = "active"
	f.teams["SupportTools/admins/newbie"] = "pending"
	f.perms["SupportTools/KubeTTY/octo"] = "write"
	c, _ := New(cfg)
	ctx := context.Background()

	if ok, err := c.OrgMember(ctx, "SupportTools", "octo"); err != nil || !ok {
		t.Fatalf("org member: %v %v", ok, err)
	}
	if ok, err := c.OrgMember(ctx, "SupportTools", "stranger"); err != nil || ok {
		t.Fatalf("non-member: %v %v", ok, err)
	}
	if ok, err := c.TeamMember(ctx, "SupportTools", "admins", "octo"); err != nil || !ok {
		t.Fatalf("team member: %v %v", ok, err)
	}
	if ok, err := c.TeamMember(ctx, "SupportTools", "admins", "newbie"); err != nil || ok {
		t.Fatalf("pending membership must not count: %v %v", ok, err)
	}
	if perm, err := c.RepoPermission(ctx, "SupportTools", "KubeTTY", "octo"); err != nil || perm != "write" {
		t.Fatalf("permission: %q %v", perm, err)
	}
	if perm, err := c.RepoPermission(ctx, "SupportTools", "KubeTTY", "stranger"); err != nil || perm != "none" {
		t.Fatalf("no access: %q %v", perm, err)
	}
	if f.tokenMints != 1 {
		t.Fatalf("installation token minted %d times, want 1 (cached)", f.tokenMints)
	}
	if _, err := c.OrgMember(ctx, "OtherOrg", "octo"); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("org without the app: %v", err)
	}
}

func TestNewRejectsBadConfig(t *testing.T) {
	_, _, cfg := newFakeGitHub(t)
	bad := cfg
	bad.PrivateKeyPEM = []byte("not a key")
	if _, err := New(bad); err == nil {
		t.Fatal("bad key must fail")
	}
	bad = cfg
	bad.ClientSecret = ""
	if _, err := New(bad); err == nil {
		t.Fatal("missing secret must fail")
	}
}
