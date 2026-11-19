// Command kubetty-smoke is the functional gate between the dev and production
// deploys (cluster-services-1534, operator decision 2026-11-19 "Automated
// smoke test before prod"). It goes through Cloudflare Access with the dev-only
// CI service token, exactly like a browser goes through Access with Google:
//
//  1. GET /api/auth/me is 200 as the expected smoke user (cloudflare-access mode);
//  2. the smoke user is really restricted (/api/projects and the admin API are 403);
//  3. it opens a throwaway terminal tab on its project, attaches to /ws, runs
//     printf with shell arithmetic and waits for the evaluated output (proves a
//     live shell answers, not just an echo of the input);
//  4. it closes the tab (DELETE 204) and checks it is gone.
//
// Configuration (environment):
//
//	KUBETTY_SMOKE_URL        base URL, e.g. https://kubetty-dev.support.tools
//	CF_ACCESS_CLIENT_ID      Access service token client ID
//	CF_ACCESS_CLIENT_SECRET  Access service token client secret
//	KUBETTY_SMOKE_USER       expected username (default kubetty-smoke)
//	KUBETTY_SMOKE_PROJECT    project to open the tab on (default test-project)
//	KUBETTY_SMOKE_TIMEOUT    overall timeout (default 3m)
//
// Exit status 0 only if every check passed. Secrets are never printed.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

type config struct {
	BaseURL      string
	ClientID     string
	ClientSecret string
	User         string
	Project      string
	Timeout      time.Duration
	// AttachRetry is how long to wait for shell output before resending the command.
	AttachRetry time.Duration
}

func main() {
	cfg := config{
		BaseURL:      strings.TrimRight(os.Getenv("KUBETTY_SMOKE_URL"), "/"),
		ClientID:     os.Getenv("CF_ACCESS_CLIENT_ID"),
		ClientSecret: os.Getenv("CF_ACCESS_CLIENT_SECRET"),
		User:         envOr("KUBETTY_SMOKE_USER", "kubetty-smoke"),
		Project:      envOr("KUBETTY_SMOKE_PROJECT", "test-project"),
		Timeout:      3 * time.Minute,
		AttachRetry:  15 * time.Second,
	}
	if v := os.Getenv("KUBETTY_SMOKE_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			fail(fmt.Errorf("KUBETTY_SMOKE_TIMEOUT: %w", err))
		}
		cfg.Timeout = d
	}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()
	if err := run(ctx, cfg, os.Stdout); err != nil {
		fail(err)
	}
}

func envOr(k, d string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return d
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "::error::kubetty smoke FAILED: %v\n", err)
	os.Exit(1)
}

type client struct {
	cfg  config
	http *http.Client
}

func (c *client) headers(h http.Header) {
	h.Set("CF-Access-Client-Id", c.cfg.ClientID)
	h.Set("CF-Access-Client-Secret", c.cfg.ClientSecret)
	h.Set("User-Agent", "kubetty-smoke/1 (cluster-services-1534)")
}

func (c *client) do(ctx context.Context, method, path string, body any) (int, []byte, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.cfg.BaseURL+path, rdr)
	if err != nil {
		return 0, nil, err
	}
	c.headers(req.Header)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, b, nil
}

func run(ctx context.Context, cfg config, out io.Writer) error {
	if cfg.BaseURL == "" || cfg.ClientID == "" || cfg.ClientSecret == "" {
		return errors.New("KUBETTY_SMOKE_URL, CF_ACCESS_CLIENT_ID and CF_ACCESS_CLIENT_SECRET are required")
	}
	u, err := url.Parse(cfg.BaseURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return fmt.Errorf("bad KUBETTY_SMOKE_URL %q", cfg.BaseURL)
	}
	// Never follow redirects: a 302 to the Access login page means the token was refused.
	c := &client{cfg: cfg, http: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}}
	say := func(format string, a ...any) { fmt.Fprintf(out, "kubetty-smoke: "+format+"\n", a...) }

	// 1. Identity.
	code, body, err := c.do(ctx, http.MethodGet, "/api/auth/me", nil)
	if err != nil {
		return err
	}
	if code != http.StatusOK {
		return fmt.Errorf("GET /api/auth/me = %d (want 200): %s", code, snippet(body))
	}
	var me struct {
		User struct {
			ID       string `json:"id"`
			Username string `json:"username"`
		} `json:"user"`
		AuthMode string `json:"authMode"`
	}
	if err := json.Unmarshal(body, &me); err != nil {
		return fmt.Errorf("GET /api/auth/me: bad JSON: %w", err)
	}
	if me.User.Username != cfg.User || me.AuthMode != "cloudflare-access" {
		return fmt.Errorf("GET /api/auth/me: user %q mode %q (want %q, cloudflare-access)", me.User.Username, me.AuthMode, cfg.User)
	}
	say("PASS /api/auth/me = 200 as %s (authMode cloudflare-access)", me.User.Username)

	// 2. The smoke principal is restricted.
	for _, p := range []string{"/api/projects", "/api/admin/projects"} {
		code, body, err := c.do(ctx, http.MethodGet, p, nil)
		if err != nil {
			return err
		}
		if code != http.StatusForbidden {
			return fmt.Errorf("GET %s = %d (want 403: the smoke principal must be restricted): %s", p, code, snippet(body))
		}
	}
	say("PASS restricted: /api/projects and /api/admin/projects = 403")

	// Clean up tabs a previous, interrupted run left behind (only its own are visible).
	if err := c.closeAllTabs(ctx, say); err != nil {
		return err
	}

	// 3. Throwaway tab + websocket echo.
	code, body, err = c.do(ctx, http.MethodPost, "/api/tabs", map[string]any{"projectId": cfg.Project, "openAction": "create_new"})
	if err != nil {
		return err
	}
	if code != http.StatusCreated && code != http.StatusOK {
		return fmt.Errorf("POST /api/tabs = %d: %s", code, snippet(body))
	}
	var created struct {
		Tab struct {
			TabID string `json:"tabId"`
		} `json:"tab"`
	}
	if err := json.Unmarshal(body, &created); err != nil || created.Tab.TabID == "" {
		return fmt.Errorf("POST /api/tabs: no tab id in %s", snippet(body))
	}
	tabID := created.Tab.TabID
	say("PASS opened tab %s on project %s", tabID, cfg.Project)
	echoErr := c.echo(ctx, u, tabID, say)

	// 4. Close the tab even if the echo failed.
	delCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	code, body, err = c.do(delCtx, http.MethodDelete, "/api/tabs/"+url.PathEscape(tabID), nil)
	if echoErr != nil {
		return echoErr
	}
	if err != nil {
		return err
	}
	if code != http.StatusNoContent {
		return fmt.Errorf("DELETE /api/tabs/%s = %d (want 204): %s", tabID, code, snippet(body))
	}
	if ids, err := c.listTabs(ctx); err != nil {
		return err
	} else if contains(ids, tabID) {
		return fmt.Errorf("tab %s still listed after DELETE", tabID)
	}
	say("PASS closed tab %s (DELETE 204, no longer listed)", tabID)
	say("ALL CHECKS PASSED against %s", cfg.BaseURL)
	return nil
}

func (c *client) listTabs(ctx context.Context) ([]string, error) {
	code, body, err := c.do(ctx, http.MethodGet, "/api/tabs", nil)
	if err != nil {
		return nil, err
	}
	if code != http.StatusOK {
		return nil, fmt.Errorf("GET /api/tabs = %d: %s", code, snippet(body))
	}
	var l struct {
		Tabs []struct {
			TabID string `json:"tabId"`
		} `json:"tabs"`
	}
	if err := json.Unmarshal(body, &l); err != nil {
		return nil, fmt.Errorf("GET /api/tabs: bad JSON: %w", err)
	}
	ids := make([]string, 0, len(l.Tabs))
	for _, t := range l.Tabs {
		ids = append(ids, t.TabID)
	}
	return ids, nil
}

func (c *client) closeAllTabs(ctx context.Context, say func(string, ...any)) error {
	ids, err := c.listTabs(ctx)
	if err != nil {
		return err
	}
	for _, id := range ids {
		code, body, err := c.do(ctx, http.MethodDelete, "/api/tabs/"+url.PathEscape(id), nil)
		if err != nil {
			return err
		}
		if code != http.StatusNoContent && code != http.StatusNotFound {
			return fmt.Errorf("cleanup DELETE /api/tabs/%s = %d: %s", id, code, snippet(body))
		}
		say("cleaned up leftover tab %s", id)
	}
	return nil
}

func (c *client) echo(ctx context.Context, base *url.URL, tabID string, say func(string, ...any)) error {
	ws := *base
	ws.Scheme = map[string]string{"https": "wss", "http": "ws"}[base.Scheme]
	ws.Path = "/ws"
	ws.RawQuery = url.Values{"tab": {tabID}}.Encode()
	h := http.Header{}
	c.headers(h)
	d := websocket.Dialer{HandshakeTimeout: 30 * time.Second}
	conn, resp, err := d.DialContext(ctx, ws.String(), h)
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		return fmt.Errorf("websocket %s: %w (HTTP %d)", ws.Path, err, status)
	}
	defer conn.Close()

	nonceB := make([]byte, 6)
	_, _ = rand.Read(nonceB)
	nonce := hex.EncodeToString(nonceB)
	want := "KUBETTY_SMOKE_" + nonce + "_42"
	// The input echo shows "$((6*7))"; only a shell that ran it prints _42.
	cmd := []byte("printf 'KUBETTY_SMOKE_%s_%s\\n' " + nonce + " $((6*7))\r")

	type msg struct {
		data []byte
		err  error
	}
	msgs := make(chan msg, 64)
	go func() {
		for {
			_, b, err := conn.ReadMessage()
			msgs <- msg{b, err}
			if err != nil {
				return
			}
		}
	}()
	var seen strings.Builder
	send := func() error { return conn.WriteMessage(websocket.BinaryMessage, cmd) }
	if err := send(); err != nil {
		return fmt.Errorf("websocket write: %w", err)
	}
	resend := time.NewTimer(c.cfg.AttachRetry)
	defer resend.Stop()
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("terminal echo: no %q within the timeout (got %d bytes of output)", want, seen.Len())
		case <-resend.C:
			_ = send() // the shell may not have been ready for the first line
		case m := <-msgs:
			if m.err != nil {
				return fmt.Errorf("websocket read: %w (after %d bytes of output)", m.err, seen.Len())
			}
			if bytes.HasPrefix(m.data, []byte(`{"type":`)) {
				continue // control frames (pong, buffer_info)
			}
			seen.Write(m.data)
			if strings.Contains(seen.String(), want) {
				say("PASS terminal websocket echo: shell printed %s", want)
				_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
				return nil
			}
		}
	}
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 300 {
		s = s[:300] + "..."
	}
	return s
}
