package buffer

import (
	"bytes"
	"sync"
	"testing"
)

func TestNewRingBuffer(t *testing.T) {
	tests := []struct {
		name     string
		size     int
		expected int
	}{
		{"default size", 0, DefaultBufferSize},
		{"negative size", -1, DefaultBufferSize},
		{"custom size", 1024, 1024},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := NewRingBuffer(tt.size)
			if buf.size != tt.expected {
				t.Errorf("NewRingBuffer(%d).size = %d, want %d", tt.size, buf.size, tt.expected)
			}
		})
	}
}

func TestRingBuffer_Write_NoWrap(t *testing.T) {
	buf := NewRingBuffer(100)

	data := []byte("hello world")
	n, err := buf.Write(data)

	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if n != len(data) {
		t.Errorf("Write() = %d, want %d", n, len(data))
	}
	if buf.Len() != len(data) {
		t.Errorf("Len() = %d, want %d", buf.Len(), len(data))
	}

	result := buf.Bytes()
	if !bytes.Equal(result, data) {
		t.Errorf("Bytes() = %q, want %q", result, data)
	}

	info := buf.Info()
	if info.TotalWritten != int64(len(data)) {
		t.Errorf("TotalWritten = %d, want %d", info.TotalWritten, len(data))
	}
}

func TestRingBuffer_Write_Wrap(t *testing.T) {
	buf := NewRingBuffer(10)

	// Write more data than buffer size
	data1 := []byte("12345")
	data2 := []byte("67890")
	data3 := []byte("ABCDE")

	buf.Write(data1)
	buf.Write(data2)
	buf.Write(data3) // This should wrap and overwrite oldest

	// The newest data should be "67890ABCDE" but only last 10 chars fit
	result := buf.Bytes()

	// Verify the total written
	info := buf.Info()
	if info.TotalWritten != 15 {
		t.Errorf("TotalWritten = %d, want 15", info.TotalWritten)
	}
	if info.AvailableBytes != 10 {
		t.Errorf("AvailableBytes = %d, want 10", info.AvailableBytes)
	}

	// The buffer should contain the newest 10 bytes
	if len(result) != 10 {
		t.Errorf("Bytes() len = %d, want 10", len(result))
	}

	// Should be "67890ABCDE" -> last 10 bytes of all writes
	// After write 1: "12345" (pos=5, wrapped=false)
	// After write 2: "1234567890" (pos=10 -> 0, wrapped=true)
	// After write 3: data[0:5] = "ABCDE", so buffer = "ABCDE67890"
	// Bytes() should return from writePos to end, then start to writePos
	// writePos=5, so: "67890" + "ABCDE" = "67890ABCDE"

	expected := []byte("67890ABCDE")
	if !bytes.Equal(result, expected) {
		t.Errorf("Bytes() = %q, want %q", result, expected)
	}
}

func TestRingBuffer_Write_LargerThanBuffer(t *testing.T) {
	buf := NewRingBuffer(10)

	// Write data larger than buffer
	data := []byte("12345678901234567890")
	n, err := buf.Write(data)

	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if n != len(data) {
		t.Errorf("Write() = %d, want %d", n, len(data))
	}

	result := buf.Bytes()
	// Should keep only last 10 bytes
	expected := []byte("1234567890")
	if !bytes.Equal(result, expected) {
		t.Errorf("Bytes() = %q, want %q", result, expected)
	}
}

func TestRingBuffer_Write_Empty(t *testing.T) {
	buf := NewRingBuffer(10)

	n, err := buf.Write(nil)
	if err != nil {
		t.Fatalf("Write(nil) error = %v", err)
	}
	if n != 0 {
		t.Errorf("Write(nil) = %d, want 0", n)
	}

	n, err = buf.Write([]byte{})
	if err != nil {
		t.Fatalf("Write([]) error = %v", err)
	}
	if n != 0 {
		t.Errorf("Write([]) = %d, want 0", n)
	}
}

func TestRingBuffer_ReadRange_NoWrap(t *testing.T) {
	buf := NewRingBuffer(100)
	buf.Write([]byte("hello world"))

	// Read all
	data, offset, err := buf.ReadRange(0, 100)
	if err != nil {
		t.Fatalf("ReadRange() error = %v", err)
	}
	if offset != 0 {
		t.Errorf("offset = %d, want 0", offset)
	}
	if !bytes.Equal(data, []byte("hello world")) {
		t.Errorf("data = %q, want %q", data, "hello world")
	}

	// Read partial
	data, offset, err = buf.ReadRange(0, 5)
	if err != nil {
		t.Fatalf("ReadRange() error = %v", err)
	}
	if !bytes.Equal(data, []byte("hello")) {
		t.Errorf("data = %q, want %q", data, "hello")
	}

	// Read from offset
	data, offset, err = buf.ReadRange(6, 5)
	if err != nil {
		t.Fatalf("ReadRange() error = %v", err)
	}
	if offset != 6 {
		t.Errorf("offset = %d, want 6", offset)
	}
	if !bytes.Equal(data, []byte("world")) {
		t.Errorf("data = %q, want %q", data, "world")
	}
}

func TestRingBuffer_ReadRange_Wrapped(t *testing.T) {
	buf := NewRingBuffer(10)

	// Fill buffer and wrap
	buf.Write([]byte("12345"))
	buf.Write([]byte("67890"))
	buf.Write([]byte("ABCDE"))

	// totalWritten = 15, availableBytes = 10
	// oldest offset = 15 - 10 = 5
	// Buffer contains: "67890ABCDE"

	info := buf.Info()
	if info.OldestOffset != 5 {
		t.Errorf("OldestOffset = %d, want 5", info.OldestOffset)
	}

	// Try to read from offset 0 (too old, should clamp to oldest)
	data, actualOffset, err := buf.ReadRange(0, 5)
	if err != nil {
		t.Fatalf("ReadRange() error = %v", err)
	}
	if actualOffset != 5 {
		t.Errorf("actualOffset = %d, want 5", actualOffset)
	}
	if !bytes.Equal(data, []byte("67890")) {
		t.Errorf("data = %q, want %q", data, "67890")
	}

	// Read from valid offset
	data, actualOffset, err = buf.ReadRange(10, 5)
	if err != nil {
		t.Fatalf("ReadRange() error = %v", err)
	}
	if actualOffset != 10 {
		t.Errorf("actualOffset = %d, want 10", actualOffset)
	}
	if !bytes.Equal(data, []byte("ABCDE")) {
		t.Errorf("data = %q, want %q", data, "ABCDE")
	}
}

func TestRingBuffer_ReadRange_FutureOffset(t *testing.T) {
	buf := NewRingBuffer(100)
	buf.Write([]byte("hello"))

	// Request offset beyond what's written
	data, actualOffset, err := buf.ReadRange(100, 10)
	if err != nil {
		t.Fatalf("ReadRange() error = %v", err)
	}
	if len(data) != 0 {
		t.Errorf("data len = %d, want 0", len(data))
	}
	if actualOffset != 5 { // Should be totalWritten
		t.Errorf("actualOffset = %d, want 5", actualOffset)
	}
}

func TestRingBuffer_Info(t *testing.T) {
	buf := NewRingBuffer(100)

	// Empty buffer
	info := buf.Info()
	if info.TotalWritten != 0 {
		t.Errorf("TotalWritten = %d, want 0", info.TotalWritten)
	}
	if info.AvailableBytes != 0 {
		t.Errorf("AvailableBytes = %d, want 0", info.AvailableBytes)
	}
	if info.Capacity != 100 {
		t.Errorf("Capacity = %d, want 100", info.Capacity)
	}

	// After write
	buf.Write([]byte("hello"))
	info = buf.Info()
	if info.TotalWritten != 5 {
		t.Errorf("TotalWritten = %d, want 5", info.TotalWritten)
	}
	if info.AvailableBytes != 5 {
		t.Errorf("AvailableBytes = %d, want 5", info.AvailableBytes)
	}
	if info.OldestOffset != 0 {
		t.Errorf("OldestOffset = %d, want 0", info.OldestOffset)
	}
	if info.NewestOffset != 4 {
		t.Errorf("NewestOffset = %d, want 4", info.NewestOffset)
	}
}

func TestRingBuffer_Reset(t *testing.T) {
	buf := NewRingBuffer(100)
	buf.Write([]byte("hello world"))

	beforeReset := buf.Info().TotalWritten

	buf.Reset()

	if buf.Len() != 0 {
		t.Errorf("Len() after Reset() = %d, want 0", buf.Len())
	}

	// totalWritten should be preserved for offset consistency
	afterReset := buf.Info().TotalWritten
	if afterReset != beforeReset {
		t.Errorf("TotalWritten changed after Reset: %d -> %d", beforeReset, afterReset)
	}
}

func TestRingBuffer_Concurrent(t *testing.T) {
	buf := NewRingBuffer(1024)
	var wg sync.WaitGroup

	// Multiple writers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				buf.Write([]byte("test data from writer"))
			}
		}(i)
	}

	// Multiple readers
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = buf.Bytes()
				_ = buf.Info()
				_, _, _ = buf.ReadRange(0, 100)
			}
		}()
	}

	wg.Wait()

	// Just verify no panics and reasonable state
	if buf.Len() > buf.size {
		t.Errorf("Len() = %d exceeds size %d", buf.Len(), buf.size)
	}
}

func TestRingBuffer_ExactFit(t *testing.T) {
	buf := NewRingBuffer(10)

	// Write exactly buffer size
	data := []byte("1234567890")
	buf.Write(data)

	result := buf.Bytes()
	if !bytes.Equal(result, data) {
		t.Errorf("Bytes() = %q, want %q", result, data)
	}

	if !buf.wrapped {
		t.Error("Buffer should be marked as wrapped after exact fit")
	}
}
