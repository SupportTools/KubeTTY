package health

import (
	"context"
	"sync"
)

// ComponentChecker wraps a simple function to implement the Checker interface.
// Useful for quick component status checks that always succeed.
type ComponentChecker struct {
	name      string
	checkFunc func() string
}

// NewComponentChecker creates a checker that always reports healthy with a dynamic status message.
func NewComponentChecker(name string, checkFunc func() string) *ComponentChecker {
	return &ComponentChecker{
		name:      name,
		checkFunc: checkFunc,
	}
}

// Check executes the status function and returns the component name and status.
func (c *ComponentChecker) Check(ctx context.Context) (bool, string) {
	status := c.checkFunc()
	// Return format: (healthy=true, "name:status")
	// This will be parsed by the compatibility wrapper
	return true, c.name + ":" + status
}

// PTYChecker checks PTY process state with thread-safe mutex protection.
type PTYChecker struct {
	mu       *sync.RWMutex
	checkPTY func() bool // Function to check if PTY is alive
}

// NewPTYChecker creates a checker for PTY process state.
func NewPTYChecker(mu *sync.RWMutex, checkPTY func() bool) *PTYChecker {
	return &PTYChecker{
		mu:       mu,
		checkPTY: checkPTY,
	}
}

// Check verifies PTY process state with proper locking.
func (c *PTYChecker) Check(ctx context.Context) (bool, string) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.checkPTY() {
		return true, "pty:alive"
	}
	return true, "pty:not_started"
}
