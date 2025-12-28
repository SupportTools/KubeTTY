package filelogger

import (
	"bufio"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	log "github.com/sirupsen/logrus"
)

// Options configures the file logger behavior.
type Options struct {
	// FilePath is the path to the log file.
	// Default: /var/log/kubetty/pty-session.jsonl
	FilePath string

	// MaxSize is the maximum file size in bytes before rotation.
	// Default: 104857600 (100MB)
	MaxSize int64

	// MaxBackups is the number of rotated files to keep.
	// Default: 3
	MaxBackups int

	// BufferSize is the write buffer size in bytes.
	// Default: 65536 (64KB)
	BufferSize int

	// FlushInterval is how often to flush the buffer to disk.
	// Default: 5s
	FlushInterval time.Duration

	// IncludeRaw controls whether to include base64-encoded raw bytes.
	// Default: true
	IncludeRaw bool
}

// DefaultOptions returns the default logger options.
func DefaultOptions() Options {
	return Options{
		FilePath:      "/var/log/kubetty/pty-session.jsonl",
		MaxSize:       104857600, // 100MB
		MaxBackups:    3,
		BufferSize:    65536, // 64KB
		FlushInterval: 5 * time.Second,
		IncludeRaw:    true,
	}
}

// Logger writes PTY I/O to JSONL files with buffering and rotation.
// It is safe for concurrent use.
type Logger struct {
	sessionID string
	project   string
	user      string
	opts      Options

	mu       sync.Mutex
	file     *os.File
	writer   *bufio.Writer
	fileSize int64
	sequence atomic.Int64

	stopChan chan struct{}
	doneChan chan struct{}
}

// New creates a new file logger for the given session.
// It opens/creates the log file and starts the periodic flush goroutine.
func New(sessionID, project, user string, opts Options) (*Logger, error) {
	// Apply defaults for zero values
	if opts.FilePath == "" {
		opts.FilePath = DefaultOptions().FilePath
	}
	if opts.MaxSize <= 0 {
		opts.MaxSize = DefaultOptions().MaxSize
	}
	if opts.MaxBackups <= 0 {
		opts.MaxBackups = DefaultOptions().MaxBackups
	}
	if opts.BufferSize <= 0 {
		opts.BufferSize = DefaultOptions().BufferSize
	}
	if opts.FlushInterval <= 0 {
		opts.FlushInterval = DefaultOptions().FlushInterval
	}

	l := &Logger{
		sessionID: sessionID,
		project:   project,
		user:      user,
		opts:      opts,
		stopChan:  make(chan struct{}),
		doneChan:  make(chan struct{}),
	}

	// Ensure directory exists
	dir := filepath.Dir(opts.FilePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	// Open log file
	if err := l.openFile(); err != nil {
		return nil, err
	}

	// Start periodic flush goroutine
	go l.flushLoop()

	log.WithFields(log.Fields{
		"session_id": sessionID,
		"file_path":  opts.FilePath,
		"max_size":   opts.MaxSize,
	}).Info("File logger initialized")

	return l, nil
}

// Write logs PTY data with the specified direction.
// Data is buffered and flushed periodically or when buffer fills.
func (l *Logger) Write(dir Direction, data []byte) {
	if len(data) == 0 {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	// Create entry with incrementing sequence number
	seq := l.sequence.Add(1)
	entry := NewEntry(l.sessionID, l.project, l.user, dir, data, seq, l.opts.IncludeRaw)

	// Serialize to JSON line
	jsonBytes, err := entry.MarshalJSONLine()
	if err != nil {
		log.WithError(err).Error("Failed to marshal log entry")
		return
	}

	// Check if rotation needed before write
	entrySize := int64(len(jsonBytes) + 1) // +1 for newline
	if l.fileSize+entrySize > l.opts.MaxSize {
		if err := l.rotate(); err != nil {
			log.WithError(err).Error("Failed to rotate log file")
			// Continue writing to current file even if rotation fails
		}
	}

	// Write JSON line with newline
	n, err := l.writer.Write(jsonBytes)
	if err != nil {
		log.WithError(err).Error("Failed to write log entry")
		return
	}
	l.fileSize += int64(n)

	n, err = l.writer.Write([]byte{'\n'})
	if err != nil {
		log.WithError(err).Error("Failed to write newline")
		return
	}
	l.fileSize += int64(n)
}

// Flush writes any buffered data to disk.
func (l *Logger) Flush() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.writer == nil {
		return nil
	}
	return l.writer.Flush()
}

// Close stops the flush loop, flushes remaining data, and closes the file.
func (l *Logger) Close() error {
	// Signal flush loop to stop
	close(l.stopChan)

	// Wait for flush loop to finish
	<-l.doneChan

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.writer != nil {
		if err := l.writer.Flush(); err != nil {
			log.WithError(err).Error("Failed to flush on close")
		}
	}

	if l.file != nil {
		if err := l.file.Close(); err != nil {
			log.WithError(err).Error("Failed to close file")
			return err
		}
		l.file = nil
		l.writer = nil
	}

	log.WithField("session_id", l.sessionID).Info("File logger closed")
	return nil
}

// openFile opens or creates the log file and sets up the buffered writer.
// Caller must hold l.mu.
func (l *Logger) openFile() error {
	f, err := os.OpenFile(l.opts.FilePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}

	// Get current file size
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}

	l.file = f
	l.writer = bufio.NewWriterSize(f, l.opts.BufferSize)
	l.fileSize = info.Size()

	return nil
}

// flushLoop periodically flushes the write buffer.
func (l *Logger) flushLoop() {
	defer close(l.doneChan)

	ticker := time.NewTicker(l.opts.FlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := l.Flush(); err != nil {
				log.WithError(err).Warn("Periodic flush failed")
			}
		case <-l.stopChan:
			return
		}
	}
}

// Enabled returns true since file logger is always enabled when instantiated.
func (l *Logger) Enabled() bool {
	return true
}
