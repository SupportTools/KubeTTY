package exec

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"k8s.io/apimachinery/pkg/util/remotecommand"
	"k8s.io/client-go/rest"
)

type execRequestLog struct {
	mu       sync.Mutex
	requests []string
}

func (l *execRequestLog) record(r *http.Request) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.requests = append(l.requests, r.Method+" "+r.Header.Get("Upgrade"))
}

func (l *execRequestLog) seen() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.requests)
}

func runExecSession(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	api := httptest.NewServer(handler)
	t.Cleanup(api.Close)

	s, err := NewSession(&rest.Config{Host: api.URL}, SessionConfig{Namespace: "ns", PodName: "pod"})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	select {
	case <-s.Done():
	case <-time.After(15 * time.Second):
		t.Fatal("exec session did not end")
	}
}

func TestSession_Start_FallsBackToSPDYWhenWebSocketUpgradeRefused(t *testing.T) {
	var log execRequestLog
	runExecSession(t, func(w http.ResponseWriter, r *http.Request) {
		log.record(r)
		http.Error(w, "forbidden", http.StatusForbidden)
	})

	want := []string{"GET websocket", "POST SPDY/3.1"}
	if got := log.seen(); !slices.Equal(got, want) {
		t.Fatalf("exec requests = %q, want %q", got, want)
	}
}

func TestSession_Start_UsesWebSocketWhenUpgradeAccepted(t *testing.T) {
	var log execRequestLog
	upgrader := websocket.Upgrader{Subprotocols: []string{remotecommand.StreamProtocolV5Name}}
	runExecSession(t, func(w http.ResponseWriter, r *http.Request) {
		log.record(r)
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.WriteMessage(websocket.BinaryMessage, append([]byte{remotecommand.StreamErr}, `{"metadata":{},"status":"Success"}`...))
		_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	})

	want := []string{"GET websocket"}
	if got := log.seen(); !slices.Equal(got, want) {
		t.Fatalf("exec requests = %q, want %q", got, want)
	}
}
