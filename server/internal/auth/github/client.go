// Package github signs users in with a GitHub App and answers membership and
// repository permission questions with the App's installation tokens.
package github

import (
	"bytes"
	"context"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ErrNotInstalled means the App has no installation on the org or repo owner.
var ErrNotInstalled = errors.New("github app is not installed there")

// Config describes the GitHub App.
type Config struct {
	ClientID      string
	ClientSecret  string
	PrivateKeyPEM []byte
	APIURL        string
	WebURL        string
	HTTPClient    *http.Client
	Now           func() time.Time
}

// Identity is the GitHub account that signed in.
type Identity struct {
	ID    int64
	Login string
}

// Client talks to GitHub for one App.
type Client struct {
	clientID     string
	clientSecret string
	key          *rsa.PrivateKey
	apiURL       string
	web          string
	http         *http.Client
	now          func() time.Time

	mu            sync.Mutex
	installations map[string]int64
	tokens        map[int64]installationToken
}

type installationToken struct {
	token   string
	expires time.Time
}

// New validates the config and parses the App private key.
func New(cfg Config) (*Client, error) {
	if cfg.ClientID == "" || cfg.ClientSecret == "" {
		return nil, errors.New("github: client ID and client secret are required")
	}
	key, err := jwt.ParseRSAPrivateKeyFromPEM(cfg.PrivateKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("github: parse app private key: %w", err)
	}
	c := &Client{
		clientID:      cfg.ClientID,
		clientSecret:  cfg.ClientSecret,
		key:           key,
		apiURL:        strings.TrimRight(orDefault(cfg.APIURL, "https://api.github.com"), "/"),
		web:           strings.TrimRight(orDefault(cfg.WebURL, "https://github.com"), "/"),
		http:          cfg.HTTPClient,
		now:           cfg.Now,
		installations: map[string]int64{},
		tokens:        map[int64]installationToken{},
	}
	if c.http == nil {
		c.http = &http.Client{Timeout: 15 * time.Second}
	}
	if c.now == nil {
		c.now = time.Now
	}
	return c, nil
}

func orDefault(v, d string) string {
	if v == "" {
		return d
	}
	return v
}

// AuthorizeURL is where the browser goes to sign in.
func (c *Client) AuthorizeURL(redirectURI, state, codeChallenge string) string {
	q := url.Values{
		"client_id":             {c.clientID},
		"redirect_uri":          {redirectURI},
		"state":                 {state},
		"code_challenge":        {codeChallenge},
		"code_challenge_method": {"S256"},
		"allow_signup":          {"false"},
	}
	return c.web + "/login/oauth/authorize?" + q.Encode()
}

// SignIn exchanges the callback code for a user token, reads the account, and
// drops the token: later checks use installation tokens.
func (c *Client) SignIn(ctx context.Context, code, codeVerifier, redirectURI string) (Identity, error) {
	form := url.Values{
		"client_id":     {c.clientID},
		"client_secret": {c.clientSecret},
		"code":          {code},
		"code_verifier": {codeVerifier},
		"redirect_uri":  {redirectURI},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.web+"/login/oauth/access_token", strings.NewReader(form.Encode()))
	if err != nil {
		return Identity{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	var tok struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if status, err := c.doJSON(req, &tok); err != nil {
		return Identity{}, err
	} else if status != http.StatusOK || tok.AccessToken == "" {
		return Identity{}, fmt.Errorf("github: code exchange failed (HTTP %d, %s)", status, tok.Error)
	}

	var user struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
	}
	status, err := c.call(ctx, http.MethodGet, "/user", "token "+tok.AccessToken, &user)
	if err != nil {
		return Identity{}, err
	}
	if status != http.StatusOK || user.ID == 0 || user.Login == "" {
		return Identity{}, fmt.Errorf("github: GET /user returned HTTP %d", status)
	}
	return Identity{ID: user.ID, Login: user.Login}, nil
}

// OrgMember reports whether login is an active member of org.
func (c *Client) OrgMember(ctx context.Context, org, login string) (bool, error) {
	var m struct {
		State string `json:"state"`
	}
	status, err := c.asInstallation(ctx, "/orgs/"+url.PathEscape(org)+"/installation", http.MethodGet,
		"/orgs/"+url.PathEscape(org)+"/memberships/"+url.PathEscape(login), &m)
	return membership(status, m.State, err)
}

// TeamMember reports whether login is an active member of org/team.
func (c *Client) TeamMember(ctx context.Context, org, team, login string) (bool, error) {
	var m struct {
		State string `json:"state"`
	}
	status, err := c.asInstallation(ctx, "/orgs/"+url.PathEscape(org)+"/installation", http.MethodGet,
		"/orgs/"+url.PathEscape(org)+"/teams/"+url.PathEscape(team)+"/memberships/"+url.PathEscape(login), &m)
	return membership(status, m.State, err)
}

func membership(status int, state string, err error) (bool, error) {
	switch {
	case err != nil:
		return false, err
	case status == http.StatusOK:
		return state == "active", nil
	case status == http.StatusNotFound:
		return false, nil
	default:
		return false, fmt.Errorf("github: membership lookup returned HTTP %d", status)
	}
}

// RepoPermission returns login's base permission on owner/repo: admin, write
// (maintain counts as write), read (triage counts as read) or none.
func (c *Client) RepoPermission(ctx context.Context, owner, repo, login string) (string, error) {
	var p struct {
		Permission string `json:"permission"`
	}
	path := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo)
	status, err := c.asInstallation(ctx, path+"/installation", http.MethodGet,
		path+"/collaborators/"+url.PathEscape(login)+"/permission", &p)
	switch {
	case err != nil:
		return "", err
	case status == http.StatusNotFound:
		return "none", nil
	case status != http.StatusOK:
		return "", fmt.Errorf("github: permission lookup on %s/%s returned HTTP %d", owner, repo, status)
	}
	return orDefault(p.Permission, "none"), nil
}

func (c *Client) asInstallation(ctx context.Context, installationPath, method, path string, out any) (int, error) {
	token, err := c.installationToken(ctx, installationPath)
	if err != nil {
		return 0, err
	}
	return c.call(ctx, method, path, "token "+token, out)
}

func (c *Client) installationToken(ctx context.Context, installationPath string) (string, error) {
	c.mu.Lock()
	id, known := c.installations[installationPath]
	tok, cached := c.tokens[id]
	c.mu.Unlock()
	if known && cached && c.now().Before(tok.expires.Add(-5*time.Minute)) {
		return tok.token, nil
	}

	appJWT, err := c.appJWT()
	if err != nil {
		return "", err
	}
	if !known {
		var inst struct {
			ID int64 `json:"id"`
		}
		status, err := c.call(ctx, http.MethodGet, installationPath, "Bearer "+appJWT, &inst)
		if err != nil {
			return "", err
		}
		if status == http.StatusNotFound {
			return "", fmt.Errorf("%w (%s)", ErrNotInstalled, installationPath)
		}
		if status != http.StatusOK || inst.ID == 0 {
			return "", fmt.Errorf("github: %s returned HTTP %d", installationPath, status)
		}
		id = inst.ID
		c.mu.Lock()
		c.installations[installationPath] = id
		tok, cached = c.tokens[id]
		c.mu.Unlock()
		if cached && c.now().Before(tok.expires.Add(-5*time.Minute)) {
			return tok.token, nil
		}
	}

	var t struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	status, err := c.call(ctx, http.MethodPost, fmt.Sprintf("/app/installations/%d/access_tokens", id), "Bearer "+appJWT, &t)
	if err != nil {
		return "", err
	}
	if status != http.StatusCreated || t.Token == "" {
		return "", fmt.Errorf("github: installation token for %d returned HTTP %d", id, status)
	}
	c.mu.Lock()
	c.tokens[id] = installationToken{token: t.Token, expires: t.ExpiresAt}
	c.mu.Unlock()
	return t.Token, nil
}

func (c *Client) appJWT() (string, error) {
	now := c.now()
	claims := jwt.RegisteredClaims{
		Issuer:    c.clientID,
		IssuedAt:  jwt.NewNumericDate(now.Add(-60 * time.Second)),
		ExpiresAt: jwt.NewNumericDate(now.Add(9 * time.Minute)),
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(c.key)
	if err != nil {
		return "", fmt.Errorf("github: sign app JWT: %w", err)
	}
	return signed, nil
}

func (c *Client) call(ctx context.Context, method, path, authorization string, out any) (int, error) {
	var body io.Reader
	if method == http.MethodPost {
		body = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.apiURL+path, body)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", authorization)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	return c.doJSON(req, out)
}

func (c *Client) doJSON(req *http.Request, out any) (int, error) {
	req.Header.Set("User-Agent", "kubetty")
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("github: %s %s: %w", req.Method, req.URL.Path, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return resp.StatusCode, fmt.Errorf("github: read %s: %w", req.URL.Path, err)
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 && len(b) > 0 && out != nil {
		if err := json.Unmarshal(b, out); err != nil {
			return resp.StatusCode, fmt.Errorf("github: decode %s: %w", req.URL.Path, err)
		}
	}
	return resp.StatusCode, nil
}
