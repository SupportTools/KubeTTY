package filelogger

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewEntry(t *testing.T) {
	data := []byte("hello world\n")
	entry := NewEntry("session-123", "test-project", "testuser", DirectionOut, data, 1, true)

	if entry.SessionID != "session-123" {
		t.Errorf("SessionID = %q, want %q", entry.SessionID, "session-123")
	}
	if entry.Project != "test-project" {
		t.Errorf("Project = %q, want %q", entry.Project, "test-project")
	}
	if entry.User != "testuser" {
		t.Errorf("User = %q, want %q", entry.User, "testuser")
	}
	if entry.Direction != DirectionOut {
		t.Errorf("Direction = %q, want %q", entry.Direction, DirectionOut)
	}
	if entry.Content != "hello world\n" {
		t.Errorf("Content = %q, want %q", entry.Content, "hello world\n")
	}
	if entry.Seq != 1 {
		t.Errorf("Seq = %d, want %d", entry.Seq, 1)
	}

	// Check raw is base64 encoded
	decoded, err := base64.StdEncoding.DecodeString(entry.Raw)
	if err != nil {
		t.Fatalf("Failed to decode raw: %v", err)
	}
	if string(decoded) != "hello world\n" {
		t.Errorf("Decoded raw = %q, want %q", string(decoded), "hello world\n")
	}
}

func TestNewEntry_NoRaw(t *testing.T) {
	data := []byte("hello world")
	entry := NewEntry("session-123", "test-project", "testuser", DirectionIn, data, 1, false)

	if entry.Raw != "" {
		t.Errorf("Raw = %q, want empty string", entry.Raw)
	}
}

func TestSanitizeContent(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
		want  string
	}{
		{
			name:  "plain text",
			input: []byte("hello world"),
			want:  "hello world",
		},
		{
			name:  "with newline",
			input: []byte("hello\nworld"),
			want:  "hello\nworld",
		},
		{
			name:  "with tab",
			input: []byte("hello\tworld"),
			want:  "hello\tworld",
		},
		{
			name:  "with carriage return",
			input: []byte("hello\r\nworld"),
			want:  "hello\r\nworld",
		},
		{
			name:  "with ANSI escape",
			input: []byte("\x1b[32mgreen\x1b[0m"),
			want:  "[32mgreen[0m",
		},
		{
			name:  "with bell",
			input: []byte("hello\x07world"),
			want:  "helloworld",
		},
		{
			name:  "empty",
			input: []byte{},
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeContent(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeContent() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEntryMarshalJSONLine(t *testing.T) {
	entry := &Entry{
		Timestamp: "2025-01-01T00:00:00Z",
		SessionID: "session-123",
		Project:   "test-project",
		User:      "testuser",
		Direction: DirectionOut,
		Content:   "hello",
		Raw:       "aGVsbG8=",
		Seq:       42,
	}

	jsonBytes, err := entry.MarshalJSONLine()
	if err != nil {
		t.Fatalf("MarshalJSONLine() error = %v", err)
	}

	// Verify it's valid JSON
	var parsed Entry
	if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	if parsed.Seq != 42 {
		t.Errorf("Parsed Seq = %d, want %d", parsed.Seq, 42)
	}
}

func TestLogger_Write(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test.jsonl")

	// Create logger with short flush interval for testing
	logger, err := New("session-123", "test-project", "testuser", Options{
		FilePath:      logPath,
		MaxSize:       10 * 1024 * 1024, // 10MB
		MaxBackups:    3,
		BufferSize:    1024,
		FlushInterval: 100 * time.Millisecond,
		IncludeRaw:    true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer logger.Close()

	// Write some data
	logger.Write(DirectionOut, []byte("hello world"))
	logger.Write(DirectionIn, []byte("user input"))

	// Flush to ensure data is written
	if err := logger.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}

	// Read and verify log file
	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	if len(lines) != 2 {
		t.Fatalf("Expected 2 lines, got %d", len(lines))
	}

	// Parse first line
	var entry1 Entry
	if err := json.Unmarshal([]byte(lines[0]), &entry1); err != nil {
		t.Fatalf("Failed to parse line 1: %v", err)
	}
	if entry1.Direction != DirectionOut {
		t.Errorf("Line 1 direction = %q, want %q", entry1.Direction, DirectionOut)
	}
	if entry1.Content != "hello world" {
		t.Errorf("Line 1 content = %q, want %q", entry1.Content, "hello world")
	}
	if entry1.Seq != 1 {
		t.Errorf("Line 1 seq = %d, want %d", entry1.Seq, 1)
	}

	// Parse second line
	var entry2 Entry
	if err := json.Unmarshal([]byte(lines[1]), &entry2); err != nil {
		t.Fatalf("Failed to parse line 2: %v", err)
	}
	if entry2.Direction != DirectionIn {
		t.Errorf("Line 2 direction = %q, want %q", entry2.Direction, DirectionIn)
	}
	if entry2.Seq != 2 {
		t.Errorf("Line 2 seq = %d, want %d", entry2.Seq, 2)
	}
}

func TestLogger_Rotation(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test.jsonl")

	// Create logger with very small max size to trigger rotation
	logger, err := New("session-123", "test-project", "testuser", Options{
		FilePath:      logPath,
		MaxSize:       200, // Very small to trigger rotation quickly
		MaxBackups:    2,
		BufferSize:    64,
		FlushInterval: time.Hour, // Long interval, we'll flush manually
		IncludeRaw:    false,     // Smaller entries
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer logger.Close()

	// Write enough data to trigger rotation
	for i := 0; i < 10; i++ {
		logger.Write(DirectionOut, []byte("this is a test message that is long enough"))
		logger.Flush()
	}

	// Check that backup files exist
	backup1 := logPath + ".1"
	backup2 := logPath + ".2"
	backup3 := logPath + ".3"

	if _, err := os.Stat(backup1); os.IsNotExist(err) {
		t.Error("Expected backup .1 to exist")
	}
	if _, err := os.Stat(backup2); os.IsNotExist(err) {
		t.Error("Expected backup .2 to exist")
	}
	// Backup .3 should NOT exist (maxBackups=2)
	if _, err := os.Stat(backup3); !os.IsNotExist(err) {
		t.Error("Expected backup .3 to NOT exist")
	}
}

func TestLogger_ConcurrentWrites(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test.jsonl")

	logger, err := New("session-123", "test-project", "testuser", Options{
		FilePath:      logPath,
		MaxSize:       10 * 1024 * 1024,
		MaxBackups:    3,
		BufferSize:    1024,
		FlushInterval: 100 * time.Millisecond,
		IncludeRaw:    false,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer logger.Close()

	// Write concurrently from multiple goroutines
	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func(id int) {
			defer func() { done <- struct{}{} }()
			for j := 0; j < 100; j++ {
				logger.Write(DirectionOut, []byte("message from goroutine"))
			}
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Flush and close
	logger.Flush()

	// Read and verify log file
	f, err := os.Open(logPath)
	if err != nil {
		t.Fatalf("Failed to open log file: %v", err)
	}
	defer f.Close()

	// Count lines
	scanner := bufio.NewScanner(f)
	lineCount := 0
	for scanner.Scan() {
		lineCount++
		// Verify each line is valid JSON
		var entry Entry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			t.Errorf("Line %d is not valid JSON: %v", lineCount, err)
		}
	}

	// Should have 10 goroutines * 100 writes = 1000 lines
	if lineCount != 1000 {
		t.Errorf("Line count = %d, want 1000", lineCount)
	}
}

func TestLogger_EmptyWrite(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test.jsonl")

	logger, err := New("session-123", "test-project", "testuser", Options{
		FilePath:      logPath,
		FlushInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer logger.Close()

	// Write empty data (should be no-op)
	logger.Write(DirectionOut, []byte{})
	logger.Write(DirectionOut, nil)
	logger.Flush()

	// File should be empty
	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}
	if len(content) != 0 {
		t.Errorf("Expected empty file, got %d bytes", len(content))
	}
}

func TestBackupPath(t *testing.T) {
	logger := &Logger{
		opts: Options{
			FilePath: "/var/log/kubetty/pty-session.jsonl",
		},
	}

	tests := []struct {
		n    int
		want string
	}{
		{1, "/var/log/kubetty/pty-session.jsonl.1"},
		{2, "/var/log/kubetty/pty-session.jsonl.2"},
		{10, "/var/log/kubetty/pty-session.jsonl.10"},
	}

	for _, tt := range tests {
		got := logger.backupPath(tt.n)
		if got != tt.want {
			t.Errorf("backupPath(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestIsNumeric(t *testing.T) {
	tests := []struct {
		s    string
		want bool
	}{
		{"123", true},
		{"0", true},
		{"", false},
		{"1a", false},
		{"abc", false},
		{"1.2", false},
	}

	for _, tt := range tests {
		got := isNumeric(tt.s)
		if got != tt.want {
			t.Errorf("isNumeric(%q) = %v, want %v", tt.s, got, tt.want)
		}
	}
}

func TestDefaultOptions(t *testing.T) {
	opts := DefaultOptions()

	if opts.FilePath != "/var/log/kubetty/pty-session.jsonl" {
		t.Errorf("Default FilePath = %q, want %q", opts.FilePath, "/var/log/kubetty/pty-session.jsonl")
	}
	if opts.MaxSize != 104857600 {
		t.Errorf("Default MaxSize = %d, want %d", opts.MaxSize, 104857600)
	}
	if opts.MaxBackups != 3 {
		t.Errorf("Default MaxBackups = %d, want %d", opts.MaxBackups, 3)
	}
	if opts.BufferSize != 65536 {
		t.Errorf("Default BufferSize = %d, want %d", opts.BufferSize, 65536)
	}
	if opts.FlushInterval != 5*time.Second {
		t.Errorf("Default FlushInterval = %v, want %v", opts.FlushInterval, 5*time.Second)
	}
	if !opts.IncludeRaw {
		t.Error("Default IncludeRaw = false, want true")
	}
}
