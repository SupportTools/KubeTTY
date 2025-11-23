package ptylogger

import (
	"bytes"
	"strings"
	"testing"

	log "github.com/sirupsen/logrus"
)

// captureOutput captures logrus output during test execution.
func captureOutput(fn func()) string {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(nil)
	log.SetFormatter(&log.TextFormatter{DisableTimestamp: true})
	fn()
	return buf.String()
}

func TestNew(t *testing.T) {
	t.Run("creates logger with defaults", func(t *testing.T) {
		l := New("test-session", Options{Enabled: true})
		if l.sessionID != "test-session" {
			t.Errorf("expected sessionID test-session, got %s", l.sessionID)
		}
		if !l.opts.Enabled {
			t.Error("expected Enabled to be true")
		}
		if l.opts.MaxLineLen != 4096 {
			t.Errorf("expected MaxLineLen 4096, got %d", l.opts.MaxLineLen)
		}
	})

	t.Run("respects custom max line length", func(t *testing.T) {
		l := New("test", Options{Enabled: true, MaxLineLen: 1024})
		if l.opts.MaxLineLen != 1024 {
			t.Errorf("expected MaxLineLen 1024, got %d", l.opts.MaxLineLen)
		}
	})

	t.Run("uses default for zero max line length", func(t *testing.T) {
		l := New("test", Options{Enabled: true, MaxLineLen: 0})
		if l.opts.MaxLineLen != 4096 {
			t.Errorf("expected MaxLineLen 4096, got %d", l.opts.MaxLineLen)
		}
	})
}

func TestLogger_Write_Disabled(t *testing.T) {
	l := New("test", Options{Enabled: false})
	output := captureOutput(func() {
		l.Write(DirectionOut, []byte("hello\n"))
	})
	if output != "" {
		t.Errorf("expected no output when disabled, got %q", output)
	}
}

func TestLogger_Write_EmptyData(t *testing.T) {
	l := New("test", Options{Enabled: true})
	output := captureOutput(func() {
		l.Write(DirectionOut, []byte{})
		l.Write(DirectionOut, nil)
	})
	if output != "" {
		t.Errorf("expected no output for empty data, got %q", output)
	}
}

func TestLogger_Write_SingleLine(t *testing.T) {
	l := New("test-session", Options{Enabled: true})
	output := captureOutput(func() {
		l.Write(DirectionOut, []byte("hello world\n"))
	})

	if !strings.Contains(output, "hello world") {
		t.Errorf("expected output to contain 'hello world', got %q", output)
	}
	if !strings.Contains(output, "session_id=test-session") {
		t.Errorf("expected output to contain session_id, got %q", output)
	}
	if !strings.Contains(output, "direction=out") {
		t.Errorf("expected output to contain direction=out, got %q", output)
	}
}

func TestLogger_Write_MultipleLines(t *testing.T) {
	l := New("test", Options{Enabled: true})
	output := captureOutput(func() {
		l.Write(DirectionOut, []byte("line1\nline2\nline3\n"))
	})

	if !strings.Contains(output, "line1") {
		t.Errorf("expected output to contain 'line1', got %q", output)
	}
	if !strings.Contains(output, "line2") {
		t.Errorf("expected output to contain 'line2', got %q", output)
	}
	if !strings.Contains(output, "line3") {
		t.Errorf("expected output to contain 'line3', got %q", output)
	}
}

func TestLogger_Write_Chunked(t *testing.T) {
	l := New("test", Options{Enabled: true})
	output := captureOutput(func() {
		// Simulate data coming in chunks
		l.Write(DirectionOut, []byte("hel"))
		l.Write(DirectionOut, []byte("lo "))
		l.Write(DirectionOut, []byte("world\n"))
	})

	if !strings.Contains(output, "hello world") {
		t.Errorf("expected output to contain 'hello world', got %q", output)
	}
}

func TestLogger_Write_MaxLineLen(t *testing.T) {
	l := New("test", Options{Enabled: true, MaxLineLen: 10})
	output := captureOutput(func() {
		l.Write(DirectionOut, []byte("12345678901234567890"))
	})

	// Should have flushed at least once due to max line length
	if output == "" {
		t.Error("expected output due to max line length, got none")
	}
}

func TestLogger_Write_Direction(t *testing.T) {
	t.Run("input direction", func(t *testing.T) {
		l := New("test", Options{Enabled: true})
		output := captureOutput(func() {
			l.Write(DirectionIn, []byte("user input\n"))
		})

		if !strings.Contains(output, "direction=in") {
			t.Errorf("expected direction=in, got %q", output)
		}
	})

	t.Run("output direction", func(t *testing.T) {
		l := New("test", Options{Enabled: true})
		output := captureOutput(func() {
			l.Write(DirectionOut, []byte("pty output\n"))
		})

		if !strings.Contains(output, "direction=out") {
			t.Errorf("expected direction=out, got %q", output)
		}
	})
}

func TestLogger_Flush(t *testing.T) {
	t.Run("flushes partial input buffer", func(t *testing.T) {
		l := New("test", Options{Enabled: true})
		output := captureOutput(func() {
			l.Write(DirectionIn, []byte("partial input"))
			l.Flush()
		})

		if !strings.Contains(output, "partial input") {
			t.Errorf("expected flushed content, got %q", output)
		}
	})

	t.Run("flushes partial output buffer", func(t *testing.T) {
		l := New("test", Options{Enabled: true})
		output := captureOutput(func() {
			l.Write(DirectionOut, []byte("partial output"))
			l.Flush()
		})

		if !strings.Contains(output, "partial output") {
			t.Errorf("expected flushed content, got %q", output)
		}
	})

	t.Run("flushes both buffers", func(t *testing.T) {
		l := New("test", Options{Enabled: true})
		output := captureOutput(func() {
			l.Write(DirectionIn, []byte("input data"))
			l.Write(DirectionOut, []byte("output data"))
			l.Flush()
		})

		if !strings.Contains(output, "input data") {
			t.Errorf("expected input data, got %q", output)
		}
		if !strings.Contains(output, "output data") {
			t.Errorf("expected output data, got %q", output)
		}
	})

	t.Run("no output when disabled", func(t *testing.T) {
		l := New("test", Options{Enabled: false})
		output := captureOutput(func() {
			l.Write(DirectionOut, []byte("data"))
			l.Flush()
		})

		if output != "" {
			t.Errorf("expected no output when disabled, got %q", output)
		}
	})
}

func TestLogger_Write_CRLFHandling(t *testing.T) {
	l := New("test", Options{Enabled: true})
	output := captureOutput(func() {
		// Terminal output often has \r\n
		l.Write(DirectionOut, []byte("line with crlf\r\n"))
	})

	// Should strip both \r and \n
	if strings.Contains(output, "\\r") || strings.Contains(output, "\\n") {
		t.Errorf("expected stripped line endings, got %q", output)
	}
	if !strings.Contains(output, "line with crlf") {
		t.Errorf("expected line content, got %q", output)
	}
}

func TestLogger_Enabled(t *testing.T) {
	t.Run("returns true when enabled", func(t *testing.T) {
		l := New("test", Options{Enabled: true})
		if !l.Enabled() {
			t.Error("expected Enabled() to return true")
		}
	})

	t.Run("returns false when disabled", func(t *testing.T) {
		l := New("test", Options{Enabled: false})
		if l.Enabled() {
			t.Error("expected Enabled() to return false")
		}
	})
}

func TestDefaultOptions(t *testing.T) {
	opts := DefaultOptions()
	if opts.Enabled {
		t.Error("expected default Enabled to be false")
	}
	if opts.MaxLineLen != 4096 {
		t.Errorf("expected default MaxLineLen 4096, got %d", opts.MaxLineLen)
	}
}
