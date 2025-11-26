package exec

import (
	"bytes"
	"testing"
)

func TestNewOutputBuffer(t *testing.T) {
	tests := []struct {
		name     string
		size     int
		expected int
	}{
		{"default size when zero", 0, DefaultBufferSize},
		{"default size when negative", -1, DefaultBufferSize},
		{"custom size", 1024, 1024},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := NewOutputBuffer(tt.size)
			if buf.size != tt.expected {
				t.Errorf("expected size %d, got %d", tt.expected, buf.size)
			}
		})
	}
}

func TestOutputBuffer_Write(t *testing.T) {
	buf := NewOutputBuffer(10)

	// Write small data
	n, err := buf.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 5 {
		t.Errorf("expected n=5, got %d", n)
	}

	data := buf.Bytes()
	if !bytes.Equal(data, []byte("hello")) {
		t.Errorf("expected 'hello', got %q", data)
	}
}

func TestOutputBuffer_WriteWrap(t *testing.T) {
	buf := NewOutputBuffer(10)

	// Fill buffer exactly
	buf.Write([]byte("0123456789"))
	if buf.Len() != 10 {
		t.Errorf("expected len=10, got %d", buf.Len())
	}

	// Write more data, causing wrap
	buf.Write([]byte("ABC"))
	data := buf.Bytes()
	// Should have "3456789ABC" (oldest data "012" overwritten)
	if !bytes.Equal(data, []byte("3456789ABC")) {
		t.Errorf("expected '3456789ABC', got %q", data)
	}
}

func TestOutputBuffer_WriteLargerThanBuffer(t *testing.T) {
	buf := NewOutputBuffer(5)

	// Write data larger than buffer
	buf.Write([]byte("0123456789"))

	// Should only keep last 5 bytes
	data := buf.Bytes()
	if !bytes.Equal(data, []byte("56789")) {
		t.Errorf("expected '56789', got %q", data)
	}
}

func TestOutputBuffer_WriteEmpty(t *testing.T) {
	buf := NewOutputBuffer(10)

	n, err := buf.Write([]byte{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 0 {
		t.Errorf("expected n=0, got %d", n)
	}

	if buf.Len() != 0 {
		t.Errorf("expected len=0, got %d", buf.Len())
	}
}

func TestOutputBuffer_Len(t *testing.T) {
	buf := NewOutputBuffer(10)

	if buf.Len() != 0 {
		t.Errorf("expected initial len=0, got %d", buf.Len())
	}

	buf.Write([]byte("hello"))
	if buf.Len() != 5 {
		t.Errorf("expected len=5, got %d", buf.Len())
	}

	// After wrap, len should be buffer size
	buf.Write([]byte("world12345"))
	if buf.Len() != 10 {
		t.Errorf("expected len=10 after wrap, got %d", buf.Len())
	}
}

func TestOutputBuffer_Reset(t *testing.T) {
	buf := NewOutputBuffer(10)
	buf.Write([]byte("hello"))

	buf.Reset()

	if buf.Len() != 0 {
		t.Errorf("expected len=0 after reset, got %d", buf.Len())
	}

	data := buf.Bytes()
	if len(data) != 0 {
		t.Errorf("expected empty bytes after reset, got %q", data)
	}
}

func TestOutputBuffer_BytesCopy(t *testing.T) {
	buf := NewOutputBuffer(10)
	buf.Write([]byte("hello"))

	// Get bytes and modify them
	data := buf.Bytes()
	data[0] = 'X'

	// Original buffer should be unchanged
	original := buf.Bytes()
	if original[0] != 'h' {
		t.Error("Bytes() should return a copy, not a reference")
	}
}

func TestOutputBuffer_ConcurrentAccess(t *testing.T) {
	buf := NewOutputBuffer(1024)
	done := make(chan struct{})

	// Writer goroutine
	go func() {
		for i := 0; i < 100; i++ {
			buf.Write([]byte("test data"))
		}
		close(done)
	}()

	// Reader goroutines
	for i := 0; i < 3; i++ {
		go func() {
			for {
				select {
				case <-done:
					return
				default:
					_ = buf.Bytes()
					_ = buf.Len()
				}
			}
		}()
	}

	<-done
}
