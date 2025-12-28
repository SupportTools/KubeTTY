// Package buffer provides thread-safe buffer implementations for terminal output.
package buffer

import (
	"errors"
	"sync"
)

const (
	// DefaultBufferSize is the default size for output buffering (8MB)
	DefaultBufferSize = 8 * 1024 * 1024
)

// ErrOffsetTooOld is returned when requested offset has been overwritten.
var ErrOffsetTooOld = errors.New("requested offset has been overwritten")

// BufferInfo contains metadata about the buffer state.
type BufferInfo struct {
	TotalWritten   int64 // Total bytes ever written (monotonic counter)
	OldestOffset   int64 // Oldest available byte offset
	NewestOffset   int64 // Newest byte offset (TotalWritten - 1)
	AvailableBytes int   // Currently buffered bytes
	Capacity       int   // Buffer capacity
}

// RingBuffer provides a thread-safe circular buffer for terminal output.
// It allows replaying recent output when clients reconnect and supports
// offset-based range reads for lazy-loading historical content.
type RingBuffer struct {
	mu           sync.RWMutex
	data         []byte
	size         int
	writePos     int
	wrapped      bool
	totalWritten int64 // Monotonic counter of all bytes ever written
}

// NewRingBuffer creates a new ring buffer with the specified size.
// If size <= 0, DefaultBufferSize (8MB) is used.
func NewRingBuffer(size int) *RingBuffer {
	if size <= 0 {
		size = DefaultBufferSize
	}
	return &RingBuffer{
		data: make([]byte, size),
		size: size,
	}
}

// Write appends data to the buffer, overwriting oldest data if full.
// Implements io.Writer interface.
func (b *RingBuffer) Write(p []byte) (n int, err error) {
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
		b.totalWritten += int64(n)
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

	b.totalWritten += int64(n)
	return n, nil
}

// Bytes returns a copy of all buffered data in chronological order.
func (b *RingBuffer) Bytes() []byte {
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

// ReadRange returns data starting from the given logical offset with a limit.
// Returns the actual data, the actual start offset (may be higher if requested
// offset was overwritten), and any error.
//
// This method supports lazy-loading of historical content by allowing clients
// to request specific byte ranges.
func (b *RingBuffer) ReadRange(offset int64, limit int) ([]byte, int64, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if limit <= 0 {
		limit = b.size
	}

	// Calculate available range
	availableBytes := b.writePos
	if b.wrapped {
		availableBytes = b.size
	}

	oldestOffset := b.totalWritten - int64(availableBytes)
	if oldestOffset < 0 {
		oldestOffset = 0
	}

	// Clamp offset to available range
	actualOffset := offset
	if actualOffset < oldestOffset {
		actualOffset = oldestOffset
	}
	if actualOffset >= b.totalWritten {
		// Requesting data that doesn't exist yet
		return nil, b.totalWritten, nil
	}

	// Calculate how many bytes we can return
	bytesToRead := int(b.totalWritten - actualOffset)
	if bytesToRead > limit {
		bytesToRead = limit
	}
	if bytesToRead > availableBytes {
		bytesToRead = availableBytes
	}

	// Calculate physical position in ring buffer
	// offset from end = totalWritten - actualOffset
	// physical start = writePos - (totalWritten - actualOffset)
	offsetFromEnd := int(b.totalWritten - actualOffset)
	physicalStart := b.writePos - offsetFromEnd
	if physicalStart < 0 {
		physicalStart += b.size
	}

	// Extract data (may need to wrap)
	result := make([]byte, bytesToRead)
	if physicalStart+bytesToRead <= b.size {
		// No wrap needed
		copy(result, b.data[physicalStart:physicalStart+bytesToRead])
	} else {
		// Need to wrap
		firstPart := b.size - physicalStart
		copy(result, b.data[physicalStart:])
		copy(result[firstPart:], b.data[:bytesToRead-firstPart])
	}

	return result, actualOffset, nil
}

// Info returns metadata about the current buffer state.
func (b *RingBuffer) Info() BufferInfo {
	b.mu.RLock()
	defer b.mu.RUnlock()

	availableBytes := b.writePos
	if b.wrapped {
		availableBytes = b.size
	}

	oldestOffset := b.totalWritten - int64(availableBytes)
	if oldestOffset < 0 {
		oldestOffset = 0
	}

	newestOffset := b.totalWritten - 1
	if newestOffset < 0 {
		newestOffset = 0
	}

	return BufferInfo{
		TotalWritten:   b.totalWritten,
		OldestOffset:   oldestOffset,
		NewestOffset:   newestOffset,
		AvailableBytes: availableBytes,
		Capacity:       b.size,
	}
}

// Len returns the current amount of data in the buffer.
func (b *RingBuffer) Len() int {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.wrapped {
		return b.size
	}
	return b.writePos
}

// Reset clears the buffer.
// Note: totalWritten is NOT reset to preserve offset consistency.
func (b *RingBuffer) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.writePos = 0
	b.wrapped = false
	// Note: we don't reset totalWritten to maintain offset consistency
	// for any clients that may have cached offset information
}
