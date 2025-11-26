// Package exec provides kubectl exec-based terminal sessions using Kubernetes remotecommand API.
package exec

import (
	"context"
	"fmt"
	"io"
	"sync"

	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

// SessionConfig holds configuration for an exec session.
type SessionConfig struct {
	Namespace string
	PodName   string
	Container string   // Optional: defaults to first container
	Command   []string // Command to execute, defaults to ["/bin/bash", "-l"]
}

// Session manages a kubectl exec connection to a pod.
type Session struct {
	config     SessionConfig
	restConfig *rest.Config
	clientset  kubernetes.Interface

	// I/O streams
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser

	// Terminal size management
	sizeQueue *terminalSizeQueue

	// Lifecycle
	mu       sync.RWMutex
	started  bool
	closed   bool
	doneCh   chan struct{}
	executor remotecommand.Executor
}

// terminalSizeQueue implements remotecommand.TerminalSizeQueue
type terminalSizeQueue struct {
	mu      sync.Mutex
	current *remotecommand.TerminalSize
	ch      chan remotecommand.TerminalSize
}

func newTerminalSizeQueue() *terminalSizeQueue {
	return &terminalSizeQueue{
		ch: make(chan remotecommand.TerminalSize, 1),
	}
}

// Next returns the next terminal size or blocks until one is available.
func (q *terminalSizeQueue) Next() *remotecommand.TerminalSize {
	select {
	case size := <-q.ch:
		q.mu.Lock()
		q.current = &size
		q.mu.Unlock()
		return &size
	}
}

// Resize queues a terminal resize event.
func (q *terminalSizeQueue) Resize(cols, rows uint16) {
	size := remotecommand.TerminalSize{Width: cols, Height: rows}
	select {
	case q.ch <- size:
	default:
		// Channel full, replace with latest
		select {
		case <-q.ch:
		default:
		}
		q.ch <- size
	}
}

// Current returns the current terminal size if known.
func (q *terminalSizeQueue) Current() *remotecommand.TerminalSize {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.current
}

// NewSession creates a new exec session for a pod.
func NewSession(restConfig *rest.Config, cfg SessionConfig) (*Session, error) {
	if cfg.Namespace == "" {
		return nil, fmt.Errorf("namespace is required")
	}
	if cfg.PodName == "" {
		return nil, fmt.Errorf("pod name is required")
	}
	if len(cfg.Command) == 0 {
		cfg.Command = []string{"/bin/bash", "-l"}
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	return &Session{
		config:     cfg,
		restConfig: restConfig,
		clientset:  clientset,
		sizeQueue:  newTerminalSizeQueue(),
		doneCh:     make(chan struct{}),
	}, nil
}

// Start initiates the exec session and returns streams for I/O.
// The session runs until the command exits or Close is called.
func (s *Session) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return fmt.Errorf("session already started")
	}
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("session is closed")
	}
	s.started = true
	s.mu.Unlock()

	log.WithFields(log.Fields{
		"namespace": s.config.Namespace,
		"pod":       s.config.PodName,
		"container": s.config.Container,
		"command":   s.config.Command,
	}).Info("gateway/exec: starting exec session")

	// Create pipes for I/O
	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()
	stderrReader, stderrWriter := io.Pipe()

	s.stdin = stdinWriter
	s.stdout = stdoutReader
	s.stderr = stderrReader

	// Build exec request
	req := s.clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(s.config.PodName).
		Namespace(s.config.Namespace).
		SubResource("exec")

	req.VersionedParams(&corev1.PodExecOptions{
		Container: s.config.Container,
		Command:   s.config.Command,
		Stdin:     true,
		Stdout:    true,
		Stderr:    true,
		TTY:       true,
	}, scheme.ParameterCodec)

	log.WithFields(log.Fields{
		"url": req.URL().String(),
	}).Debug("gateway/exec: creating SPDY executor")

	// Create SPDY executor
	executor, err := remotecommand.NewSPDYExecutor(s.restConfig, "POST", req.URL())
	if err != nil {
		stdinReader.Close()
		stdoutWriter.Close()
		stderrWriter.Close()
		return fmt.Errorf("failed to create executor: %w", err)
	}
	s.executor = executor

	// Set initial terminal size (80x24 default)
	s.sizeQueue.Resize(80, 24)

	// Start streaming in background
	go func() {
		defer close(s.doneCh)
		defer stdinReader.Close()
		defer stdoutWriter.Close()
		defer stderrWriter.Close()

		err := executor.StreamWithContext(ctx, remotecommand.StreamOptions{
			Stdin:             stdinReader,
			Stdout:            stdoutWriter,
			Stderr:            stderrWriter,
			Tty:               true,
			TerminalSizeQueue: s.sizeQueue,
		})

		if err != nil {
			log.WithFields(log.Fields{
				"namespace": s.config.Namespace,
				"pod":       s.config.PodName,
				"error":     err.Error(),
			}).Warn("gateway/exec: exec session ended with error")
		} else {
			log.WithFields(log.Fields{
				"namespace": s.config.Namespace,
				"pod":       s.config.PodName,
			}).Info("gateway/exec: exec session ended normally")
		}

		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()
	}()

	return nil
}

// Write sends data to the exec session stdin.
func (s *Session) Write(data []byte) (int, error) {
	s.mu.RLock()
	if !s.started {
		s.mu.RUnlock()
		return 0, fmt.Errorf("session not started")
	}
	if s.closed {
		s.mu.RUnlock()
		return 0, fmt.Errorf("session is closed")
	}
	stdin := s.stdin
	s.mu.RUnlock()

	return stdin.Write(data)
}

// Read reads data from the exec session stdout.
func (s *Session) Read(data []byte) (int, error) {
	s.mu.RLock()
	if !s.started {
		s.mu.RUnlock()
		return 0, fmt.Errorf("session not started")
	}
	stdout := s.stdout
	s.mu.RUnlock()

	return stdout.Read(data)
}

// ReadStderr reads data from the exec session stderr.
func (s *Session) ReadStderr(data []byte) (int, error) {
	s.mu.RLock()
	if !s.started {
		s.mu.RUnlock()
		return 0, fmt.Errorf("session not started")
	}
	stderr := s.stderr
	s.mu.RUnlock()

	return stderr.Read(data)
}

// Resize changes the terminal size.
func (s *Session) Resize(cols, rows uint16) error {
	s.mu.RLock()
	if !s.started {
		s.mu.RUnlock()
		return fmt.Errorf("session not started")
	}
	if s.closed {
		s.mu.RUnlock()
		return fmt.Errorf("session is closed")
	}
	s.mu.RUnlock()

	log.WithFields(log.Fields{
		"namespace": s.config.Namespace,
		"pod":       s.config.PodName,
		"cols":      cols,
		"rows":      rows,
	}).Debug("gateway/exec: resizing terminal")

	s.sizeQueue.Resize(cols, rows)
	return nil
}

// Close terminates the exec session.
func (s *Session) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	stdin := s.stdin
	s.mu.Unlock()

	log.WithFields(log.Fields{
		"namespace": s.config.Namespace,
		"pod":       s.config.PodName,
	}).Info("gateway/exec: closing exec session")

	// Close stdin to signal EOF to the remote process
	if stdin != nil {
		stdin.Close()
	}

	// Wait for session to end (with timeout handled by caller's context)
	<-s.doneCh

	return nil
}

// Done returns a channel that's closed when the session ends.
func (s *Session) Done() <-chan struct{} {
	return s.doneCh
}

// IsRunning returns true if the session is active.
func (s *Session) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.started && !s.closed
}
