package relay

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestDefaultBackoff(t *testing.T) {
	b := DefaultBackoff()
	got := []time.Duration{}
	for i := 1; i <= 5; i++ {
		got = append(got, b.Next(i))
	}
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("attempt %d: got %s want %s", i+1, got[i], want[i])
		}
	}
}

func TestFixedBackoff(t *testing.T) {
	fb := FixedBackoff{Delay: 3 * time.Second}
	if d := fb.Next(10); d != 3*time.Second {
		t.Fatalf("expected 3s backoff, got %s", d)
	}
}

func TestRelayConnectFailure(t *testing.T) {
	endpoint, _ := url.Parse("ws://localhost:0/ws")
	r := New(Config{ProjectID: "test", Endpoint: endpoint})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := r.Connect(ctx, FixedBackoff{Delay: 10 * time.Millisecond}); err == nil {
		t.Fatalf("expected connect error, got nil")
	}
}

func TestRelayProxyEcho(t *testing.T) {
	// Start downstream echo server
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		conn, err := upgrader.Upgrade(w, req, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		defer conn.Close()
		for {
			mt, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			_ = conn.WriteMessage(mt, data)
		}
	}))
	defer srv.Close()

	u := ""
	if raw, err := url.Parse("ws" + strings.TrimPrefix(srv.URL, "http")); err == nil {
		u = raw.String()
	} else {
		t.Fatalf("parse downstream url: %v", err)
	}
	endpoint, _ := url.Parse(u)
	r := New(Config{ProjectID: "test", Endpoint: endpoint})

	// Start upstream WS
	upstreamSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		upstream, err := upgrader.Upgrade(w, req, nil)
		if err != nil {
			t.Fatalf("upgrade upstream: %v", err)
		}
		defer upstream.Close()

		ctx, cancel := context.WithTimeout(req.Context(), 2*time.Second)
		defer cancel()
		if err := r.Proxy(ctx, upstream); err != nil {
			t.Fatalf("proxy error: %v", err)
		}
	}))
	defer upstreamSrv.Close()

	// Client writes through upstream, expect echo
	uUpstream := ""
	if raw, err := url.Parse("ws" + strings.TrimPrefix(upstreamSrv.URL, "http")); err == nil {
		uUpstream = raw.String()
	} else {
		t.Fatalf("parse upstream url: %v", err)
	}
	c, _, err := websocket.DefaultDialer.Dial(uUpstream, nil)
	if err != nil {
		t.Fatalf("dial upstream: %v", err)
	}
	defer c.Close()

	msg := []byte("hello")
	if err := c.WriteMessage(websocket.BinaryMessage, msg); err != nil {
		t.Fatalf("write upstream: %v", err)
	}
	_, data, err := c.ReadMessage()
	if err != nil {
		t.Fatalf("read upstream: %v", err)
	}
	if string(data) != string(msg) {
		t.Fatalf("expected %q, got %q", msg, data)
	}
}
