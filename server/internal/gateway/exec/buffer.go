package exec

import (
	"sync"
)

const (
	// DefaultBufferSize is the default size for output buffering (64KB)
	DefaultBufferSize = 64 * 1024
)

// OutputBuffer provides a thread-safe circular buffer for terminal output.
// It allows replaying recent output when clients reconnect.
type OutputBuffer struct {
	mu       sync.RWMutex
	data     []byte
	size     int
	writePos int
	wrapped  bool
}

// NewOutputBuffer creates a new output buffer with the specified size.
func NewOutputBuffer(size int) *OutputBuffer {
	if size <= 0 {
		size = DefaultBufferSize
	}
	return &OutputBuffer{
		data: make([]byte, size),
		size: size,
	}
}

// Write appends data to the buffer, overwriting oldest data if full.
func (b *OutputBuffer) Write(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	n = len(p)

	// If data is larger than buffer, only keep the last bufferSize bytes
	if len(p) >= b.size {
		copy(b.data, p[len(p)-b.size:])
		b.writePos = 0
		b.wrapped = true
		return n, nil
	}

	// Calculate how much space we have before wrapping
	remaining := b.size - b.writePos

	if len(p) <= remaining {
		// Fits without wrapping
		copy(b.data[b.writePos:], p)
		b.writePos += len(p)
		if b.writePos == b.size {
			b.writePos = 0
			b.wrapped = true
		}
	} else {
		// Need to wrap
		copy(b.data[b.writePos:], p[:remaining])
		copy(b.data, p[remaining:])
		b.writePos = len(p) - remaining
		b.wrapped = true
	}

	return n, nil
}

// Bytes returns a copy of all buffered data in chronological order.
func (b *OutputBuffer) Bytes() []byte {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if !b.wrapped {
		// Haven't wrapped yet, return from start to writePos
		result := make([]byte, b.writePos)
		copy(result, b.data[:b.writePos])
		return result
	}

	// Buffer has wrapped, need to return writePos to end, then start to writePos
	result := make([]byte, b.size)
	copy(result, b.data[b.writePos:])
	copy(result[b.size-b.writePos:], b.data[:b.writePos])
	return result
}

// Len returns the current amount of data in the buffer.
func (b *OutputBuffer) Len() int {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.wrapped {
		return b.size
	}
	return b.writePos
}

// Reset clears the buffer.
func (b *OutputBuffer) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.writePos = 0
	b.wrapped = false
}
