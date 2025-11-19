package manager

import (
	"context"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	gatewayconfig "github.com/supporttools/KubeTTY/server/internal/gateway/config"
	"github.com/supporttools/KubeTTY/server/internal/gateway/tabs"
)

type fakeStore struct {
	createCalled bool
	allTabs      []tabs.Tab
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

func TestNewManager(t *testing.T) {
	cat := gatewayconfig.Catalog{Projects: []gatewayconfig.Project{{ID: "proj", Namespace: "ns", Service: "svc", Port: 8080}}}
	store := &fakeStore{}
	mgr := New(cat, store)
	if len(mgr.ListProjects()) != 1 {
		t.Fatalf("expected 1 project")
	}
}

func TestCreateTabUnknownProject(t *testing.T) {
	store := &fakeStore{}
	mgr := New(gatewayconfig.Catalog{}, store)
	if _, err := mgr.CreateTab(context.Background(), "missing", "client"); err == nil {
		t.Fatalf("expected error for missing project")
	}
}

func TestCreateTab(t *testing.T) {
	cat := gatewayconfig.Catalog{Projects: []gatewayconfig.Project{{ID: "proj", Namespace: "ns", Service: "svc", Port: 8080}}}
	store := &fakeStore{}
	mgr := New(cat, store)
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
	mgr := New(gatewayconfig.Catalog{}, &fakeStore{})
	conn := &websocket.Conn{}
	if err := mgr.Attach(context.Background(), "missing", "client", conn); err == nil {
		t.Fatalf("expected ErrNotFound")
	}
}

func TestRestoreTabs(t *testing.T) {
	cat := gatewayconfig.Catalog{Projects: []gatewayconfig.Project{{ID: "proj", Namespace: "ns", Service: "svc", Port: 8080}}}
	store := &fakeStore{allTabs: []tabs.Tab{{TabID: "restore", ProjectID: "proj", ClientID: "cli", CreatedAt: time.Now()}}}
	mgr := New(cat, store)
	if err := mgr.RestoreTabs(context.Background()); err != nil {
		t.Fatalf("RestoreTabs error: %v", err)
	}
}
