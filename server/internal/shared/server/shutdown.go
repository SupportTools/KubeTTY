// Package server provides HTTP server lifecycle management utilities for KubeTTY.
//
// This package centralizes common server patterns including graceful shutdown handling,
// request logging middleware, and signal-based termination coordination. It ensures
// KubeTTY components shut down cleanly when receiving SIGINT or SIGTERM from Kubernetes.
//
// Key features:
//   - Graceful shutdown with 30-second timeout for HTTP servers
//   - SIGINT/SIGTERM signal handling for Kubernetes pod lifecycle
//   - Pluggable ShutdownHandler interface for custom cleanup logic
//   - Request logging middleware with structured logrus fields
//   - Automatic status code capture for HTTP responses
//
// The shutdown system coordinates HTTP server termination with database connection cleanup,
// session persistence, and other stateful component shutdown to prevent data loss during
// pod restarts or cluster operations.
package server

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	log "github.com/sirupsen/logrus"
)

// ShutdownHandler is an interface for components that need custom cleanup logic.
type ShutdownHandler interface {
	Shutdown(ctx context.Context) error
}

// GracefulShutdown sets up signal handling and coordinates graceful shutdown
// of the HTTP server and any additional shutdown handlers.
func GracefulShutdown(srv *http.Server, handlers ...ShutdownHandler) {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	<-quit
	log.Info("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Shutdown HTTP server
	if err := srv.Shutdown(ctx); err != nil {
		log.WithError(err).Error("Server forced to shutdown")
	}

	// Run additional shutdown handlers
	for _, handler := range handlers {
		if err := handler.Shutdown(ctx); err != nil {
			log.WithError(err).Error("Shutdown handler error")
		}
	}

	log.Info("Server exiting")
}
