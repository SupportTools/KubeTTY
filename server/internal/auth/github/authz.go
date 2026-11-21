package github

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Lookups is what the Authorizer asks GitHub. *Client implements it.
type Lookups interface {
	OrgMember(ctx context.Context, org, login string) (bool, error)
	TeamMember(ctx context.Context, org, team, login string) (bool, error)
	RepoPermission(ctx context.Context, owner, repo, login string) (string, error)
}

// Rule admits a GitHub user. Exactly one of the forms is set.
type Rule struct {
	Org  string
	Team string
	User string
}

var (
	ownerPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})$`)
	teamPattern  = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)
)

// ParseRules parses AUTH_GITHUB_ALLOW: comma-separated org:<org>,
// team:<org>/<team-slug> and user:<login> entries.
func ParseRules(spec string) ([]Rule, error) {
	var rules []Rule
	for _, item := range strings.Split(spec, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		kind, value, ok := strings.Cut(item, ":")
		value = strings.TrimSpace(value)
		if !ok {
			return nil, fmt.Errorf("AUTH_GITHUB_ALLOW entry %q must be org:<org>, team:<org>/<team> or user:<login>", item)
		}
		switch strings.TrimSpace(kind) {
		case "org":
			if !ownerPattern.MatchString(value) {
				return nil, fmt.Errorf("AUTH_GITHUB_ALLOW entry %q: invalid org", item)
			}
			rules = append(rules, Rule{Org: value})
		case "team":
			if _, _, err := ParseTeam(value); err != nil {
				return nil, fmt.Errorf("AUTH_GITHUB_ALLOW entry %q: %w", item, err)
			}
			rules = append(rules, Rule{Team: value})
		case "user":
			if !ownerPattern.MatchString(value) {
				return nil, fmt.Errorf("AUTH_GITHUB_ALLOW entry %q: invalid login", item)
			}
			rules = append(rules, Rule{User: value})
		default:
			return nil, fmt.Errorf("AUTH_GITHUB_ALLOW entry %q must be org:<org>, team:<org>/<team> or user:<login>", item)
		}
	}
	if len(rules) == 0 {
		return nil, fmt.Errorf("AUTH_GITHUB_ALLOW must list at least one org, team or user")
	}
	return rules, nil
}

// ParseTeam splits "org/team-slug".
func ParseTeam(s string) (org, team string, err error) {
	org, team, ok := strings.Cut(strings.TrimSpace(s), "/")
	if !ok || !ownerPattern.MatchString(org) || !teamPattern.MatchString(team) {
		return "", "", fmt.Errorf("team %q must be <org>/<team-slug>", s)
	}
	return org, team, nil
}

// ParseTeams parses a comma-separated list of org/team-slug.
func ParseTeams(spec string) ([]string, error) {
	var teams []string
	for _, item := range strings.Split(spec, ",") {
		if item = strings.TrimSpace(item); item == "" {
			continue
		}
		if _, _, err := ParseTeam(item); err != nil {
			return nil, err
		}
		teams = append(teams, item)
	}
	return teams, nil
}

// Authorizer answers who may sign in, who is an admin, and who may use a
// repository's project. Answers are cached for TTL; errors are not cached.
type Authorizer struct {
	lookups    Lookups
	allow      []Rule
	adminTeams []string
	ttl        time.Duration
	now        func() time.Time

	mu    sync.Mutex
	cache map[string]cached
}

type cached struct {
	ok      bool
	expires time.Time
}

// NewAuthorizer builds an Authorizer. ttl <= 0 disables caching.
func NewAuthorizer(lookups Lookups, allow []Rule, adminTeams []string, ttl time.Duration) *Authorizer {
	return &Authorizer{lookups: lookups, allow: allow, adminTeams: adminTeams, ttl: ttl, now: time.Now, cache: map[string]cached{}}
}

// Admitted reports whether any allow rule matches login.
func (a *Authorizer) Admitted(ctx context.Context, login string) (bool, error) {
	var firstErr error
	for _, r := range a.allow {
		var ok bool
		var err error
		switch {
		case r.User != "":
			ok = strings.EqualFold(r.User, login)
		case r.Org != "":
			ok, err = a.check("org:"+r.Org+":"+login, func() (bool, error) { return a.lookups.OrgMember(ctx, r.Org, login) })
		case r.Team != "":
			ok, err = a.inTeam(ctx, r.Team, login)
		}
		if ok {
			return true, nil
		}
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return false, firstErr
}

// IsAdmin reports whether login is in any admin team.
func (a *Authorizer) IsAdmin(ctx context.Context, login string) (bool, error) {
	var firstErr error
	for _, t := range a.adminTeams {
		ok, err := a.inTeam(ctx, t, login)
		if ok {
			return true, nil
		}
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return false, firstErr
}

// CanWrite reports whether login has write, maintain or admin on "owner/name".
func (a *Authorizer) CanWrite(ctx context.Context, repo, login string) (bool, error) {
	owner, name, ok := strings.Cut(repo, "/")
	if !ok || owner == "" || name == "" || login == "" {
		return false, nil
	}
	return a.check("repo:"+strings.ToLower(repo)+":"+login, func() (bool, error) {
		perm, err := a.lookups.RepoPermission(ctx, owner, name, login)
		return perm == "admin" || perm == "write", err
	})
}

func (a *Authorizer) inTeam(ctx context.Context, team, login string) (bool, error) {
	org, slug, err := ParseTeam(team)
	if err != nil {
		return false, err
	}
	return a.check("team:"+strings.ToLower(team)+":"+login, func() (bool, error) { return a.lookups.TeamMember(ctx, org, slug, login) })
}

func (a *Authorizer) check(key string, lookup func() (bool, error)) (bool, error) {
	key = strings.ToLower(key)
	now := a.now()
	a.mu.Lock()
	if c, ok := a.cache[key]; ok && now.Before(c.expires) {
		a.mu.Unlock()
		return c.ok, nil
	}
	a.mu.Unlock()
	ok, err := lookup()
	if err != nil {
		return false, err
	}
	if a.ttl > 0 {
		a.mu.Lock()
		a.cache[key] = cached{ok: ok, expires: now.Add(a.ttl)}
		a.mu.Unlock()
	}
	return ok, nil
}
