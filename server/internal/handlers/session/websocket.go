package session

// NOTE: This file defines the PTY WebSocket handler interface and helper types.
// The actual handler implementation remains in cmd/project/main.go due to tight
// coupling with ptySession management. This file provides the documentation
// and type definitions for clarity.

// PTY input validation limits
const (
	MaxPTYCols = 500
	MaxPTYRows = 200
)

// ResizeMessage represents a terminal resize message from the client.
type ResizeMessage struct {
	Type string `json:"type"` // Should be "resize"
	Cols uint16 `json:"cols"` // Terminal columns (max 500)
	Rows uint16 `json:"rows"` // Terminal rows (max 200)
}

// PingMessage represents a ping message from the client.
type PingMessage struct {
	Type string `json:"type"` // Should be "ping"
}

// PongResponse represents a pong response to the client.
type PongResponse struct {
	Type string `json:"type"` // Always "pong"
}

// NewWebsocketHandler creates an HTTP handler for PTY WebSocket connections.
//
// Endpoint: WS /ws
// Protocol: WebSocket
//
// Message Types (Client -> Server):
//   1. Binary messages: Raw terminal input sent to PTY
//   2. Resize message (JSON):
//      {
//        "type": "resize",
//        "cols": number,  // Terminal columns (1-500)
//        "rows": number   // Terminal rows (1-200)
//      }
//   3. Ping message (JSON):
//      {
//        "type": "ping"
//      }
//
// Message Types (Server -> Client):
//   1. Binary messages: Raw terminal output from PTY
//   2. Pong response (JSON):
//      {
//        "type": "pong"
//      }
//
// Connection Flow:
//   1. Client initiates WebSocket connection
//   2. Server upgrades HTTP connection to WebSocket
//   3. Server enforces single-client policy (returns 409 if client exists)
//   4. Client is registered and receives buffered output
//   5. Bidirectional communication:
//      - Client input -> PTY
//      - PTY output -> Client
//   6. On disconnect, client is deregistered
//
// Response (409 Conflict):
//   - "session already attached" - Another client is already connected
//
// Response (500 Internal Server Error):
//   - "PTY unavailable" - PTY initialization failed
//   - "PTY not initialized" - PTY session is nil
//   - "WebSocket upgrade failed" - Failed to upgrade connection
//
// Security Considerations:
//   - Resize dimensions are validated (max 500x200)
//   - Invalid resize requests are logged but do not disconnect client
//   - Single client enforcement prevents session hijacking
//   - All terminal I/O is logged if session logging is enabled
//
// NOTE: The actual implementation is in cmd/project/main.go as handleWebsocket()
// due to tight coupling with PTY session management (ptySession struct).
