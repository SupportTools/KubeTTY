// Package filelogger provides file-based PTY session logging for Loki integration.
//
// This package writes PTY I/O to JSONL files on disk, which can be streamed
// to log aggregation systems like Loki via a sidecar tail process.
//
// Features:
//   - Structured JSON log entries with session metadata
//   - Direction tracking (in/out) for input vs output
//   - Base64-encoded raw content for escape sequence preservation
//   - File rotation with configurable size limits
//   - Buffered writes with periodic flushing
//   - Thread-safe for concurrent PTY read/write operations
package filelogger

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
	"unicode"
)

// Direction indicates whether data is input (user->PTY) or output (PTY->user).
type Direction string

const (
	// DirectionIn represents user input to the PTY.
	DirectionIn Direction = "in"
	// DirectionOut represents PTY output to the user.
	DirectionOut Direction = "out"
)

// Entry represents a single log entry for PTY I/O.
// Each entry is serialized as a JSON line in the log file.
type Entry struct {
	// Timestamp in RFC3339Nano format
	Timestamp string `json:"ts"`
	// SessionID is the PTY session UUID
	SessionID string `json:"session_id"`
	// Project name (from KUBETTY_PROJECT env)
	Project string `json:"project"`
	// User name (from KUBETTY_USER env)
	User string `json:"user"`
	// Direction indicates input ("in") or output ("out")
	Direction Direction `json:"direction"`
	// Content is the sanitized text (control chars stripped)
	Content string `json:"content"`
	// Raw is the base64-encoded raw bytes (preserves escape sequences)
	Raw string `json:"raw,omitempty"`
	// Seq is a monotonic sequence number for ordering
	Seq int64 `json:"seq"`
}

// NewEntry creates a new log entry with the current timestamp.
func NewEntry(sessionID, project, user string, dir Direction, data []byte, seq int64, includeRaw bool) *Entry {
	e := &Entry{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		SessionID: sessionID,
		Project:   project,
		User:      user,
		Direction: dir,
		Content:   sanitizeContent(data),
		Seq:       seq,
	}

	if includeRaw {
		e.Raw = base64.StdEncoding.EncodeToString(data)
	}

	return e
}

// MarshalJSONLine serializes the entry as a JSON line (no trailing newline).
func (e *Entry) MarshalJSONLine() ([]byte, error) {
	return json.Marshal(e)
}

// sanitizeContent removes control characters from the data while preserving
// printable characters, newlines, tabs, and carriage returns.
func sanitizeContent(data []byte) string {
	var sb strings.Builder
	sb.Grow(len(data))

	for _, b := range data {
		r := rune(b)
		// Keep printable characters, newlines, tabs, carriage returns
		if unicode.IsPrint(r) || r == '\n' || r == '\t' || r == '\r' {
			sb.WriteByte(b)
		}
		// Skip control characters (ANSI escape sequences, etc.)
	}

	return sb.String()
}
