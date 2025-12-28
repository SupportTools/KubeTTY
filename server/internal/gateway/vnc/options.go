package vnc

import "time"

// Default configuration values.
const (
	DefaultDialTimeout     = 10 * time.Second
	DefaultReadBufferSize  = 64 * 1024 // 64KB - suitable for VNC framebuffer updates
	DefaultWriteBufferSize = 4 * 1024  // 4KB - sufficient for client events
	DefaultReadTimeout     = 60 * time.Second
	DefaultWriteTimeout    = 10 * time.Second
	DefaultMaxRetries      = 3
	DefaultRetryDelay      = 5 * time.Second
)

// Config holds configuration for VNC relay.
type Config struct {
	Target          string        // VNC server address (e.g., "service.namespace.svc:5901")
	DialTimeout     time.Duration // TCP dial timeout
	ReadBufferSize  int           // Read buffer size in bytes
	WriteBufferSize int           // Write buffer size in bytes
	ReadTimeout     time.Duration // WebSocket read timeout
	WriteTimeout    time.Duration // WebSocket write timeout
	MaxRetries      int           // Maximum reconnection attempts
	RetryDelay      time.Duration // Base delay between retries (exponential backoff)
}

// Option is a functional option for configuring VNC relay.
type Option func(*Config)

// NewConfig creates a Config with defaults and applies options.
func NewConfig(target string, opts ...Option) Config {
	cfg := Config{
		Target:          target,
		DialTimeout:     DefaultDialTimeout,
		ReadBufferSize:  DefaultReadBufferSize,
		WriteBufferSize: DefaultWriteBufferSize,
		ReadTimeout:     DefaultReadTimeout,
		WriteTimeout:    DefaultWriteTimeout,
		MaxRetries:      DefaultMaxRetries,
		RetryDelay:      DefaultRetryDelay,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}

// WithDialTimeout sets the TCP dial timeout.
func WithDialTimeout(d time.Duration) Option {
	return func(c *Config) {
		if d > 0 {
			c.DialTimeout = d
		}
	}
}

// WithReadBufferSize sets the read buffer size.
func WithReadBufferSize(size int) Option {
	return func(c *Config) {
		if size > 0 {
			c.ReadBufferSize = size
		}
	}
}

// WithWriteBufferSize sets the write buffer size.
func WithWriteBufferSize(size int) Option {
	return func(c *Config) {
		if size > 0 {
			c.WriteBufferSize = size
		}
	}
}

// WithReadTimeout sets the WebSocket read timeout.
func WithReadTimeout(d time.Duration) Option {
	return func(c *Config) {
		if d > 0 {
			c.ReadTimeout = d
		}
	}
}

// WithWriteTimeout sets the WebSocket write timeout.
func WithWriteTimeout(d time.Duration) Option {
	return func(c *Config) {
		if d > 0 {
			c.WriteTimeout = d
		}
	}
}

// WithMaxRetries sets the maximum reconnection attempts.
func WithMaxRetries(n int) Option {
	return func(c *Config) {
		if n >= 0 {
			c.MaxRetries = n
		}
	}
}

// WithRetryDelay sets the base delay between reconnection attempts.
func WithRetryDelay(d time.Duration) Option {
	return func(c *Config) {
		if d > 0 {
			c.RetryDelay = d
		}
	}
}
