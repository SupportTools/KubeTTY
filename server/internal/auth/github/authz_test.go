package github

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeLookups struct {
	orgs  map[string]bool
	teams map[string]bool
	perms map[string]string
	err   error
	calls int
}

func (f *fakeLookups) OrgMember(_ context.Context, org, login string) (bool, error) {
	f.calls++
	return f.orgs[org+"/"+login], f.err
}

func (f *fakeLookups) TeamMember(_ context.Context, org, team, login string) (bool, error) {
	f.calls++
	return f.teams[org+"/"+team+"/"+login], f.err
}

func (f *fakeLookups) RepoPermission(_ context.Context, owner, repo, login string) (string, error) {
	f.calls++
	return f.perms[owner+"/"+repo+"/"+login], f.err
}

func TestParseRules(t *testing.T) {
	rules, err := ParseRules(" org:SupportTools , team:SupportTools/admins,user:octo-cat ,")
	if err != nil || len(rules) != 3 || rules[0].Org != "SupportTools" || rules[1].Team != "SupportTools/admins" || rules[2].User != "octo-cat" {
		t.Fatalf("got %+v %v", rules, err)
	}
	for _, bad := range []string{"", "SupportTools", "org:", "team:SupportTools", "team:/admins", "user:bad name", "group:x"} {
		if _, err := ParseRules(bad); err == nil {
			t.Errorf("%q must be rejected", bad)
		}
	}
	if teams, err := ParseTeams("SupportTools/admins, Other/ops"); err != nil || len(teams) != 2 {
		t.Fatalf("teams: %v %v", teams, err)
	}
	if _, err := ParseTeams("admins"); err == nil {
		t.Fatal("a team without an org must be rejected")
	}
}

func TestAuthorizer(t *testing.T) {
	f := &fakeLookups{
		orgs:  map[string]bool{"SupportTools/member": true},
		teams: map[string]bool{"SupportTools/admins/boss": true, "SupportTools/devs/dev": true},
		perms: map[string]string{"SupportTools/KubeTTY/writer": "write", "SupportTools/KubeTTY/boss": "admin", "SupportTools/KubeTTY/reader": "read"},
	}
	rules, _ := ParseRules("org:SupportTools,team:SupportTools/devs,user:Guest")
	a := NewAuthorizer(f, rules, []string{"SupportTools/admins"}, time.Minute)
	ctx := context.Background()

	for login, want := range map[string]bool{"member": true, "dev": true, "guest": true, "stranger": false} {
		if ok, err := a.Admitted(ctx, login); err != nil || ok != want {
			t.Errorf("Admitted(%s) = %v %v, want %v", login, ok, err, want)
		}
	}
	if ok, _ := a.IsAdmin(ctx, "boss"); !ok {
		t.Error("boss is in the admin team")
	}
	if ok, _ := a.IsAdmin(ctx, "member"); ok {
		t.Error("member is not an admin")
	}
	for login, want := range map[string]bool{"writer": true, "boss": true, "reader": false, "stranger": false} {
		if ok, err := a.CanWrite(ctx, "SupportTools/KubeTTY", login); err != nil || ok != want {
			t.Errorf("CanWrite(%s) = %v %v, want %v", login, ok, err, want)
		}
	}
	if ok, _ := a.CanWrite(ctx, "not-a-repo", "writer"); ok {
		t.Error("a malformed repo grants nothing")
	}
}

func TestAuthorizerCachesAnswersNotErrors(t *testing.T) {
	f := &fakeLookups{teams: map[string]bool{"SupportTools/admins/boss": true}}
	a := NewAuthorizer(f, nil, []string{"SupportTools/admins"}, time.Minute)
	now := time.Now()
	a.now = func() time.Time { return now }
	ctx := context.Background()

	_, _ = a.IsAdmin(ctx, "boss")
	_, _ = a.IsAdmin(ctx, "boss")
	if f.calls != 1 {
		t.Fatalf("calls = %d, want 1 (cached)", f.calls)
	}
	now = now.Add(2 * time.Minute)
	_, _ = a.IsAdmin(ctx, "boss")
	if f.calls != 2 {
		t.Fatalf("calls = %d, want 2 after the TTL", f.calls)
	}

	f.err = errors.New("github down")
	now = now.Add(2 * time.Minute)
	if ok, err := a.IsAdmin(ctx, "boss"); ok || err == nil {
		t.Fatalf("an error must deny and be returned: %v %v", ok, err)
	}
	f.err = nil
	if ok, _ := a.IsAdmin(ctx, "boss"); !ok {
		t.Fatal("the error must not be cached")
	}
}

func TestCanWriteWhenTheAppCannotSeeTheRepo(t *testing.T) {
	f := &fakeLookups{err: ErrNotInstalled}
	a := NewAuthorizer(f, nil, nil, time.Minute)
	if ok, err := a.CanWrite(context.Background(), "Other/repo", "octo"); ok || err != nil {
		t.Fatalf("a repo the App cannot see means no access, not an error: %v %v", ok, err)
	}
}
