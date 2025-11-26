package relay

import (
	"context"

	"github.com/gorilla/websocket"
)

// Proxier defines the interface for terminal relay implementations.
// Both WebSocket relay and kubectl exec relay implement this interface.
type Proxier interface {
	// Proxy handles bidirectional streaming between the upstream WebSocket
	// and the downstream terminal session.
	Proxy(ctx context.Context, upstream *websocket.Conn) error

	// Close terminates the relay and releases resources.
	Close() error

	// Subscribe returns a channel for status updates.
	Subscribe() <-chan StatusEvent

	// ActivityChan returns a channel that signals data activity.
	ActivityChan() <-chan struct{}
}
