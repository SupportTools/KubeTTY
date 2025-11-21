package manager

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	gatewayconfig "github.com/supporttools/KubeTTY/server/internal/gateway/config"
	"github.com/supporttools/KubeTTY/server/internal/gateway/tabs"
)

type fakeStore struct {
	createCalled bool
	allTabs      []tabs.Tab
	tabCounts    map[string]int // Maps "clientID:projectID" to count
}

func (f *fakeStore) Create(ctx context.Context, tab tabs.Tab) error {
	f.createCalled = true
	return nil
}

func (f *fakeStore) UpdateStatus(context.Context, string, tabs.Status, *string, *string) error {
	return nil
}
func (f *fakeStore) Delete(context.Context, string) error                          { return nil }
func (f *fakeStore) Get(context.Context, string) (*tabs.Tab, error)                { return nil, tabs.ErrNotFound }
func (f *fakeStore) ListByClient(context.Context, string, int) ([]tabs.Tab, error) { return nil, nil }
func (f *fakeStore) ListAll(context.Context) ([]tabs.Tab, error)                   { return f.allTabs, nil }
func (f *fakeStore) CountByClientAndProject(ctx context.Context, clientID, projectID string) (int, error) {
	if f.tabCounts == nil {
		return 0, nil
	}
	key := clientID + ":" + projectID
	return f.tabCounts[key], nil
}

func TestNewManager(t *testing.T) {
	cat := gatewayconfig.Catalog{Projects: []gatewayconfig.Project{{ID: "proj", Namespace: "ns", Service: "svc", Port: 8080}}}
	store := &fakeStore{}
	mgr := New(cat, store, 2*time.Hour)
	if len(mgr.ListProjects()) != 1 {
		t.Fatalf("expected 1 project")
	}
}

func TestCreateTabUnknownProject(t *testing.T) {
	store := &fakeStore{}
	mgr := New(gatewayconfig.Catalog{}, store, 2*time.Hour)
	if _, err := mgr.CreateTab(context.Background(), "missing", "client"); err == nil {
		t.Fatalf("expected error for missing project")
	}
}

func TestCreateTab(t *testing.T) {
	cat := gatewayconfig.Catalog{Projects: []gatewayconfig.Project{{ID: "proj", Namespace: "ns", Service: "svc", Port: 8080}}}
	store := &fakeStore{}
	mgr := New(cat, store, 2*time.Hour)
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if _, err := mgr.CreateTab(ctx, "proj", "client1"); err != nil {
		t.Fatalf("CreateTab error: %v", err)
	}
	if !store.createCalled {
		t.Fatalf("expected store.Create to be called")
	}
}

func TestAttachTabNotFound(t *testing.T) {
	mgr := New(gatewayconfig.Catalog{}, &fakeStore{}, 2*time.Hour)
	conn := &websocket.Conn{}
	if err := mgr.Attach(context.Background(), "missing", "client", conn); err == nil {
		t.Fatalf("expected ErrNotFound")
	}
}

func TestRestoreTabs(t *testing.T) {
	cat := gatewayconfig.Catalog{Projects: []gatewayconfig.Project{{ID: "proj", Namespace: "ns", Service: "svc", Port: 8080}}}
	store := &fakeStore{allTabs: []tabs.Tab{{TabID: "restore", ProjectID: "proj", ClientID: "cli", CreatedAt: time.Now()}}}
	mgr := New(cat, store, 2*time.Hour)
	if err := mgr.RestoreTabs(context.Background()); err != nil {
		t.Fatalf("RestoreTabs error: %v", err)
	}
}

// TestCreateTab_LimitEnforcement verifies that tab creation respects maxTabsPerClient limits.
func TestCreateTab_LimitEnforcement(t *testing.T) {
	tests := []struct {
		name         string
		limit        int
		existingTabs int
		expectError  bool
	}{
		{
			name:         "No limit (0) allows unlimited tabs",
			limit:        0,
			existingTabs: 10,
			expectError:  false,
		},
		{
			name:         "Under limit allows creation",
			limit:        3,
			existingTabs: 2,
			expectError:  false,
		},
		{
			name:         "At limit blocks creation",
			limit:        3,
			existingTabs: 3,
			expectError:  true,
		},
		{
			name:         "Over limit blocks creation",
			limit:        2,
			existingTabs: 5,
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cat := gatewayconfig.Catalog{
				Projects: []gatewayconfig.Project{{
					ID:        "proj",
					Namespace: "ns",
					Service:   "svc",
					Port:      8080,
					Limits: gatewayconfig.ProjectLimits{
						MaxTabsPerClient: tt.limit,
					},
				}},
			}
			store := &fakeStore{
				tabCounts: map[string]int{
					"client1:proj": tt.existingTabs,
				},
			}
			mgr := New(cat, store, 2*time.Hour)

			_, err := mgr.CreateTab(context.Background(), "proj", "client1")

			if tt.expectError {
				if err == nil {
					t.Fatalf("expected error when at/over limit, got nil")
				}
				var limitErr *TabLimitExceededError
				if !errors.As(err, &limitErr) {
					t.Fatalf("expected TabLimitExceededError, got: %T", err)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}
}

// TestCreateTab_DifferentProjects verifies that limits are per-project.
func TestCreateTab_DifferentProjects(t *testing.T) {
	cat := gatewayconfig.Catalog{
		Projects: []gatewayconfig.Project{
			{
				ID: "proj-a", Namespace: "ns", Service: "svc", Port: 8080,
				Limits: gatewayconfig.ProjectLimits{MaxTabsPerClient: 1},
			},
			{
				ID: "proj-b", Namespace: "ns", Service: "svc", Port: 8080,
				Limits: gatewayconfig.ProjectLimits{MaxTabsPerClient: 1},
			},
		},
	}
	store := &fakeStore{
		tabCounts: map[string]int{
			"client1:proj-a": 1, // Client already has 1 tab on proj-a
			"client1:proj-b": 0, // No tabs on proj-b
		},
	}
	mgr := New(cat, store, 2*time.Hour)

	// Should fail for proj-a (at limit)
	_, err := mgr.CreateTab(context.Background(), "proj-a", "client1")
	if err == nil {
		t.Fatal("expected error for proj-a (at limit)")
	}

	// Should succeed for proj-b (different project)
	_, err = mgr.CreateTab(context.Background(), "proj-b", "client1")
	if err != nil {
		t.Fatalf("unexpected error for proj-b: %v", err)
	}
}
