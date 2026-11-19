package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// fakeGateway mimics the parts of the KubeTTY gateway the smoke uses.
type fakeGateway struct {
	mu          sync.Mutex
	tabs        map[string]bool
	username    string
	restricted  bool
	shellEvals  bool // false: the "shell" only echoes input, never runs it
	deleteCode  int
	sawSecret   bool
	leftoverTab string
}

var printfRe = regexp.MustCompile(`printf 'KUBETTY_SMOKE_%s_%s\\n' ([0-9a-f]+) \$\(\(6\*7\)\)`)

func (g *fakeGateway) handler(t *testing.T) http.Handler {
	up := websocket.Upgrader{}
	n := 0
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("CF-Access-Client-Id") != "cid.access" || r.Header.Get("CF-Access-Client-Secret") != "s3cret" {
			http.Redirect(w, r, "https://team.cloudflareaccess.com/cdn-cgi/access/login", http.StatusFound)
			return
		}
		g.mu.Lock()
		g.sawSecret = true
		g.mu.Unlock()
		switch {
		case r.URL.Path == "/api/auth/me":
			_ = json.NewEncoder(w).Encode(map[string]any{"user": map[string]any{"id": "u1", "username": g.username}, "authMode": "cloudflare-access"})
		case r.URL.Path == "/api/projects" || strings.HasPrefix(r.URL.Path, "/api/admin/"):
			if g.restricted {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			_, _ = w.Write([]byte(`{"projects":[]}`))
		case r.URL.Path == "/api/tabs" && r.Method == http.MethodGet:
			g.mu.Lock()
			var l []map[string]string
			for id := range g.tabs {
				l = append(l, map[string]string{"tabId": id})
			}
			g.mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"tabs": l})
		case r.URL.Path == "/api/tabs" && r.Method == http.MethodPost:
			var req map[string]string
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req["projectId"] != "test-project" {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			g.mu.Lock()
			n++
			id := "tab-" + string(rune('a'+n))
			g.tabs[id] = true
			g.mu.Unlock()
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"tab": map[string]string{"tabId": id}})
		case strings.HasPrefix(r.URL.Path, "/api/tabs/") && r.Method == http.MethodDelete:
			id := strings.TrimPrefix(r.URL.Path, "/api/tabs/")
			g.mu.Lock()
			code := g.deleteCode
			if code == 0 {
				code = http.StatusNoContent
			}
			if code == http.StatusNoContent {
				delete(g.tabs, id)
			}
			g.mu.Unlock()
			w.WriteHeader(code)
		case r.URL.Path == "/ws":
			conn, err := up.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer conn.Close()
			_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"buffer_info","totalWritten":0}`))
			_ = conn.WriteMessage(websocket.BinaryMessage, []byte("motd\r\n$ "))
			for {
				_, b, err := conn.ReadMessage()
				if err != nil {
					return
				}
				_ = conn.WriteMessage(websocket.BinaryMessage, b) // terminal echo of the input
				if m := printfRe.FindSubmatch(b); m != nil && g.shellEvals {
					_ = conn.WriteMessage(websocket.BinaryMessage, []byte("\r\nKUBETTY_SMOKE_"+string(m[1])+"_42\r\n$ "))
				}
			}
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

func newFake() *fakeGateway {
	return &fakeGateway{tabs: map[string]bool{}, username: "kubetty-smoke", restricted: true, shellEvals: true}
}

func runAgainst(t *testing.T, g *fakeGateway, mod func(*config)) (string, error) {
	t.Helper()
	srv := httptest.NewServer(g.handler(t))
	t.Cleanup(srv.Close)
	cfg := config{BaseURL: srv.URL, ClientID: "cid.access", ClientSecret: "s3cret", User: "kubetty-smoke", Project: "test-project", Timeout: 5 * time.Second, AttachRetry: 200 * time.Millisecond}
	if mod != nil {
		mod(&cfg)
	}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()
	var out bytes.Buffer
	err := run(ctx, cfg, &out)
	return out.String(), err
}

func TestSmokePasses(t *testing.T) {
	g := newFake()
	g.tabs["leftover"] = true
	out, err := runAgainst(t, g, nil)
	if err != nil {
		t.Fatalf("smoke failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "ALL CHECKS PASSED") || !strings.Contains(out, "cleaned up leftover tab leftover") {
		t.Fatalf("output: %s", out)
	}
	if len(g.tabs) != 0 {
		t.Fatalf("tabs left behind: %v", g.tabs)
	}
	if strings.Contains(out, "s3cret") {
		t.Fatal("secret printed")
	}
}

func TestSmokeFailures(t *testing.T) {
	cases := map[string]struct {
		g    func(*fakeGateway)
		cfg  func(*config)
		want string
	}{
		"token refused (Access redirect)": {cfg: func(c *config) { c.ClientSecret = "wrong" }, want: "/api/auth/me = 302"},
		"wrong user":                      {g: func(g *fakeGateway) { g.username = "mmattox" }, want: `user "mmattox"`},
		"not restricted":                  {g: func(g *fakeGateway) { g.restricted = false }, want: "want 403"},
		"shell does not run commands":     {g: func(g *fakeGateway) { g.shellEvals = false }, cfg: func(c *config) { c.Timeout = time.Second }, want: "terminal echo"},
		"tab not closed":                  {g: func(g *fakeGateway) { g.deleteCode = http.StatusInternalServerError }, want: "want 204"},
		"wrong project refused":           {cfg: func(c *config) { c.Project = "kubetty" }, want: "POST /api/tabs = 403"},
		"missing config":                  {cfg: func(c *config) { c.ClientID = "" }, want: "required"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			g := newFake()
			if tc.g != nil {
				tc.g(g)
			}
			out, err := runAgainst(t, g, tc.cfg)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error containing %q, got %v\n%s", tc.want, err, out)
			}
			if strings.Contains(err.Error()+out, "s3cret") {
				t.Fatal("secret printed")
			}
		})
	}
}
