package manager

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	gatewayconfig "github.com/supporttools/KubeTTY/server/internal/gateway/config"
)

// recordingStore counts UpdateClientID calls, the write a cross-owner
// takeover used to make (cluster-services-1542).
type recordingStore struct {
	fakeStore
	updateClientID atomic.Int32
}

func (r *recordingStore) UpdateClientID(context.Context, string, string) error {
	r.updateClientID.Add(1)
	return nil
}

func newOwnershipManager(t *testing.T) (*Manager, *recordingStore, string, *blockingProxier) {
	t.Helper()
	cat := gatewayconfig.Catalog{Projects: []gatewayconfig.Project{{ID: "proj", Namespace: "ns", Service: "svc", Port: 8080, GUIEnabled: true}}}
	store := &recordingStore{}
	mgr := New(cat, store, 2*time.Hour)
	tab, err := mgr.CreateTab(context.Background(), "proj", "user:alice")
	if err != nil {
		t.Fatalf("CreateTab: %v", err)
	}
	fake := newBlockingProxier()
	mgr.mu.Lock()
	mgr.tabs[tab.TabID].proxier = fake
	mgr.mu.Unlock()
	return mgr, store, tab.TabID, fake
}

func tabOwner(mgr *Manager, tabID string) string {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	return mgr.tabs[tabID].clientID
}

// TestForceAttach_OtherOwnerRefused: force=true from another user must not
// attach, must not change the tab's owner, and must not reach Proxy().
func TestForceAttach_OtherOwnerRefused(t *testing.T) {
	mgr, store, tabID, fake := newOwnershipManager(t)

	if err := mgr.CheckAttach(tabID, "user:mallory"); !errors.Is(err, ErrTabNotOwned) {
		t.Fatalf("CheckAttach(other user) = %v, want ErrTabNotOwned", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := mgr.AttachWithOptions(ctx, tabID, "user:mallory", nil, true)
	if !errors.Is(err, ErrTabNotOwned) {
		t.Fatalf("AttachWithOptions(force, other user) = %v, want ErrTabNotOwned", err)
	}
	if got := tabOwner(mgr, tabID); got != "user:alice" {
		t.Fatalf("tab owner changed to %q, want user:alice", got)
	}
	if n := store.updateClientID.Load(); n != 0 {
		t.Fatalf("UpdateClientID called %d times, want 0", n)
	}
	select {
	case <-fake.enterCh:
		t.Fatal("refused attach reached Proxy()")
	default:
	}
}

// TestForceAttach_OtherOwnerRefusedWhileOwnerConnected: a refused takeover
// must not preempt the owner's live session.
func TestForceAttach_OtherOwnerRefusedWhileOwnerConnected(t *testing.T) {
	mgr, _, tabID, fake := newOwnershipManager(t)

	ownerDone := make(chan error, 1)
	go func() { ownerDone <- mgr.Attach(context.Background(), tabID, "user:alice", nil) }()
	select {
	case <-fake.enterCh:
	case <-time.After(2 * time.Second):
		t.Fatal("owner Proxy() did not start")
	}

	if err := mgr.AttachWithOptions(context.Background(), tabID, "user:mallory", nil, true); !errors.Is(err, ErrTabNotOwned) {
		t.Fatalf("force attach by other user = %v, want ErrTabNotOwned", err)
	}
	select {
	case err := <-ownerDone:
		t.Fatalf("owner session ended by a refused takeover: %v", err)
	case <-time.After(200 * time.Millisecond):
	}

	close(fake.startCh)
	select {
	case <-ownerDone:
	case <-time.After(2 * time.Second):
		t.Fatal("owner Attach did not exit")
	}
}

// TestForceAttach_SameOwnerTakesOver: force=true from the owner (another
// browser or device of the same user) preempts the existing connection.
func TestForceAttach_SameOwnerTakesOver(t *testing.T) {
	mgr, store, tabID, fake := newOwnershipManager(t)

	if err := mgr.CheckAttach(tabID, "user:alice"); err != nil {
		t.Fatalf("CheckAttach(owner) = %v, want nil", err)
	}

	firstDone := make(chan error, 1)
	go func() { firstDone <- mgr.Attach(context.Background(), tabID, "user:alice", nil) }()
	select {
	case <-fake.enterCh:
	case <-time.After(2 * time.Second):
		t.Fatal("first Proxy() did not start")
	}

	secondDone := make(chan error, 1)
	go func() { secondDone <- mgr.AttachWithOptions(context.Background(), tabID, "user:alice", nil, true) }()
	select {
	case <-fake.enterCh:
	case <-time.After(3 * time.Second):
		t.Fatal("force attach by owner did not reach Proxy()")
	}
	select {
	case <-firstDone:
	case <-time.After(2 * time.Second):
		t.Fatal("first session was not preempted")
	}

	close(fake.startCh)
	select {
	case err := <-secondDone:
		if err != nil {
			t.Fatalf("owner force attach: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("owner force attach did not exit")
	}
	if got := tabOwner(mgr, tabID); got != "user:alice" {
		t.Fatalf("owner = %q, want user:alice", got)
	}
	if n := store.updateClientID.Load(); n != 0 {
		t.Fatalf("UpdateClientID called %d times, want 0", n)
	}
}

// TestCheckAttach_UnknownTab keeps tabs.ErrNotFound distinct from a refusal.
func TestCheckAttach_UnknownTab(t *testing.T) {
	mgr, _, _, _ := newOwnershipManager(t)
	if err := mgr.CheckAttach("missing", "user:alice"); err == nil || errors.Is(err, ErrTabNotOwned) {
		t.Fatalf("CheckAttach(missing) = %v, want tabs.ErrNotFound", err)
	}
}

// TestForceAttachVNC_OtherOwnerRefused covers the VNC attach path (a GUI
// project's terminal tab carries the VNC relay).
func TestForceAttachVNC_OtherOwnerRefused(t *testing.T) {
	mgr, store, tabID, _ := newOwnershipManager(t)
	if !mgr.HasVNCSupport(tabID) {
		t.Fatal("test tab has no VNC relay")
	}
	if err := mgr.AttachVNC(context.Background(), tabID, "user:mallory", nil, true); !errors.Is(err, ErrTabNotOwned) {
		t.Fatalf("AttachVNC(force, other user) = %v, want ErrTabNotOwned", err)
	}
	if got := tabOwner(mgr, tabID); got != "user:alice" {
		t.Fatalf("VNC tab owner changed to %q", got)
	}
	if n := store.updateClientID.Load(); n != 0 {
		t.Fatalf("UpdateClientID called %d times, want 0", n)
	}
}
