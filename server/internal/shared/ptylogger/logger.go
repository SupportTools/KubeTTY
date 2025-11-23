// Package ptylogger provides line-buffered logging of PTY I/O for Loki capture.
//
// This package enables streaming PTY session data to pod logs in a format
// optimized for log aggregation systems like Loki. It buffers bytes until
// complete lines are formed, then emits structured log entries.
//
// Features:
//   - Line-buffered output (waits for newline before logging)
//   - Structured JSON log entries with session metadata
//   - Direction tracking (in/out) for input vs output
//   - Thread-safe for concurrent PTY read/write operations
//   - Configurable max line length with automatic flush
//
// Example usage:
//
//	logger := ptylogger.New("session-123", ptylogger.Options{
//	    Enabled:     true,
//	    MaxLineLen:  4096,
//	})
//
//	// Log PTY output
//	logger.Write(DirectionOut, outputData)
//
//	// Log user input
//	logger.Write(DirectionIn, inputData)
//
//	// Flush any remaining buffered data on session end
//	logger.Flush()
package ptylogger

import (
	"bytes"
	"sync"

	log "github.com/sirupsen/logrus"
)

// Direction indicates whether data is input (user->PTY) or output (PTY->user).
type Direction string

const (
	// DirectionIn represents user input to the PTY.
	DirectionIn Direction = "in"
	// DirectionOut represents PTY output to the user.
	DirectionOut Direction = "out"
)

// Options configures the PTY logger behavior.
type Options struct {
	// Enabled controls whether logging is active.
	// When false, Write() becomes a no-op.
	Enabled bool

	// MaxLineLen is the maximum line length before automatic flush.
	// Lines exceeding this length will be split and logged.
	// Default: 4096
	MaxLineLen int
}

// DefaultOptions returns the default logger options.
func DefaultOptions() Options {
	return Options{
		Enabled:    false,
		MaxLineLen: 4096,
	}
}

// Logger provides line-buffered PTY I/O logging.
// It is safe for concurrent use.
type Logger struct {
	sessionID string
	opts      Options

	mu     sync.Mutex
	inBuf  bytes.Buffer
	outBuf bytes.Buffer
}

// New creates a new PTY logger for the given session.
func New(sessionID string, opts Options) *Logger {
	if opts.MaxLineLen <= 0 {
		opts.MaxLineLen = 4096
	}
	return &Logger{
		sessionID: sessionID,
		opts:      opts,
	}
}

// Write logs PTY data with the specified direction.
// Data is buffered until a newline is encountered, then emitted as a log entry.
// If the buffer exceeds MaxLineLen, it is flushed regardless of newlines.
func (l *Logger) Write(dir Direction, data []byte) {
	if !l.opts.Enabled || len(data) == 0 {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	buf := l.bufferFor(dir)
	for _, b := range data {
		buf.WriteByte(b)

		// Emit line on newline or max length
		if b == '\n' || buf.Len() >= l.opts.MaxLineLen {
			l.emitLine(dir, buf)
		}
	}
}

// Flush emits any remaining buffered data for both directions.
// Call this when the session ends to ensure all data is logged.
func (l *Logger) Flush() {
	if !l.opts.Enabled {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.inBuf.Len() > 0 {
		l.emitLine(DirectionIn, &l.inBuf)
	}
	if l.outBuf.Len() > 0 {
		l.emitLine(DirectionOut, &l.outBuf)
	}
}

// bufferFor returns the appropriate buffer for the direction.
// Caller must hold l.mu.
func (l *Logger) bufferFor(dir Direction) *bytes.Buffer {
	if dir == DirectionIn {
		return &l.inBuf
	}
	return &l.outBuf
}

// emitLine logs the current buffer contents and resets the buffer.
// Caller must hold l.mu.
func (l *Logger) emitLine(dir Direction, buf *bytes.Buffer) {
	if buf.Len() == 0 {
		return
	}

	line := buf.String()
	buf.Reset()

	// Strip trailing newline for cleaner logs
	if len(line) > 0 && line[len(line)-1] == '\n' {
		line = line[:len(line)-1]
	}
	// Also strip carriage return (common in terminal output)
	if len(line) > 0 && line[len(line)-1] == '\r' {
		line = line[:len(line)-1]
	}

	// Skip empty lines after stripping
	if line == "" {
		return
	}

	log.WithFields(log.Fields{
		"session_id": l.sessionID,
		"direction":  string(dir),
		"pty":        true,
	}).Info(line)
}

// Enabled returns whether logging is active.
func (l *Logger) Enabled() bool {
	return l.opts.Enabled
}
