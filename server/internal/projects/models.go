package projects

import (
	"time"

	"github.com/google/uuid"
)

// ProjectStatus represents the lifecycle state of a project.
type ProjectStatus string

const (
	StatusPending  ProjectStatus = "pending"
	StatusSyncing  ProjectStatus = "syncing" // Template sync Job is running
	StatusCreating ProjectStatus = "creating"
	StatusRunning  ProjectStatus = "running"
	StatusUpdating ProjectStatus = "updating"
	StatusFailed   ProjectStatus = "failed"
	StatusDeleting ProjectStatus = "deleting"
	StatusDeleted  ProjectStatus = "deleted"
)

// ServiceNamePrefix is the prefix used to generate Kubernetes service names.
const ServiceNamePrefix = "kubetty-project-"

// MaxServiceNameLength is the maximum length for Kubernetes service names (DNS-1123).
const MaxServiceNameLength = 63

// ComputeServiceName generates a Kubernetes service name from a project name.
// The result follows pattern: kubetty-project-{name}, truncated to 63 chars max.
func ComputeServiceName(name string) string {
	full := ServiceNamePrefix + name
	if len(full) > MaxServiceNameLength {
		return full[:MaxServiceNameLength]
	}
	return full
}

// Project represents a KubeTTY project configuration.
type Project struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	DisplayName string    `json:"displayName"`
	Description string    `json:"description,omitempty"`
	Icon        string    `json:"icon,omitempty"`

	// Target configuration
	TargetNamespace string    `json:"targetNamespace"`
	ServiceName     string    `json:"serviceName"` // Generated: kubetty-project-{name}
	SessionID       uuid.UUID `json:"sessionId"`
	UserName        string    `json:"userName"`

	// Resource configuration
	CPURequest    string `json:"cpuRequest"`
	CPULimit      string `json:"cpuLimit"`
	MemoryRequest string `json:"memoryRequest"`
	MemoryLimit   string `json:"memoryLimit"`
	StorageSize   string `json:"storageSize"`
	StorageClass  string `json:"storageClass"`

	// RBAC configuration
	AdminNamespaces []string `json:"adminNamespaces"`
	ReadNamespaces  []string `json:"readNamespaces"`

	// Tab limits
	MaxTabsPerClient int `json:"maxTabsPerClient"`
	MaxTabsTotal     int `json:"maxTabsTotal"`

	// Feature flags
	DinDEnabled bool `json:"dindEnabled"`

	// GUI desktop support
	GUIEnabled    bool   `json:"guiEnabled"`
	GUIResolution string `json:"guiResolution,omitempty"`
	GUIVNCPort    int    `json:"guiVNCPort,omitempty"`

	// PTY session logging for Loki integration
	PTYLoggingEnabled       bool   `json:"ptyLoggingEnabled"`
	PTYLoggingMaxSize       int64  `json:"ptyLoggingMaxSize,omitempty"`       // Max file size before rotation (bytes)
	PTYLoggingMaxBackups    int    `json:"ptyLoggingMaxBackups,omitempty"`    // Rotated files to keep
	PTYLoggingBufferSize    int    `json:"ptyLoggingBufferSize,omitempty"`    // Write buffer size (bytes)
	PTYLoggingFlushInterval string `json:"ptyLoggingFlushInterval,omitempty"` // Flush interval (e.g., "5s")
	PTYLoggingIncludeRaw    bool   `json:"ptyLoggingIncludeRaw"`              // Include base64 raw bytes

	// Environment variables
	EnvVars map[string]string `json:"envVars,omitempty"`

	// Image configuration
	ImageRepository string `json:"imageRepository"`
	ImageTag        string `json:"imageTag"`

	// Status tracking
	Status          ProjectStatus `json:"status"`
	StatusMessage   string        `json:"statusMessage,omitempty"`
	LastHealthCheck *time.Time    `json:"lastHealthCheck,omitempty"`
	LastActivity    *time.Time    `json:"lastActivity,omitempty"`
	PodIP           string        `json:"podIP,omitempty"`
	Paused          bool          `json:"paused"`

	// Timestamps
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
	DeletedAt *time.Time `json:"deletedAt,omitempty"`
}

// CreateProjectRequest represents a request to create a new project.
type CreateProjectRequest struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Description string `json:"description,omitempty"`
	Icon        string `json:"icon,omitempty"`
	UserName    string `json:"userName"`

	// Resource configuration (optional, uses defaults if not provided)
	CPURequest    string `json:"cpuRequest,omitempty"`
	CPULimit      string `json:"cpuLimit,omitempty"`
	MemoryRequest string `json:"memoryRequest,omitempty"`
	MemoryLimit   string `json:"memoryLimit,omitempty"`
	StorageSize   string `json:"storageSize,omitempty"`
	StorageClass  string `json:"storageClass,omitempty"`

	// RBAC configuration
	AdminNamespaces []string `json:"adminNamespaces,omitempty"`
	ReadNamespaces  []string `json:"readNamespaces,omitempty"`

	// Tab limits
	MaxTabsPerClient int `json:"maxTabsPerClient,omitempty"`
	MaxTabsTotal     int `json:"maxTabsTotal,omitempty"`

	// Feature flags
	DinDEnabled *bool `json:"dindEnabled,omitempty"`

	// GUI desktop support
	GUIEnabled    *bool  `json:"guiEnabled,omitempty"`
	GUIResolution string `json:"guiResolution,omitempty"`
	GUIVNCPort    int    `json:"guiVNCPort,omitempty"`

	// PTY session logging for Loki integration
	PTYLoggingEnabled       *bool  `json:"ptyLoggingEnabled,omitempty"`
	PTYLoggingMaxSize       int64  `json:"ptyLoggingMaxSize,omitempty"`       // Max file size before rotation (bytes)
	PTYLoggingMaxBackups    int    `json:"ptyLoggingMaxBackups,omitempty"`    // Rotated files to keep
	PTYLoggingBufferSize    int    `json:"ptyLoggingBufferSize,omitempty"`    // Write buffer size (bytes)
	PTYLoggingFlushInterval string `json:"ptyLoggingFlushInterval,omitempty"` // Flush interval (e.g., "5s")
	PTYLoggingIncludeRaw    *bool  `json:"ptyLoggingIncludeRaw,omitempty"`    // Include base64 raw bytes

	// Environment variables
	EnvVars map[string]string `json:"envVars,omitempty"`

	// Image configuration
	ImageRepository string `json:"imageRepository,omitempty"`
	ImageTag        string `json:"imageTag,omitempty"`
}

// UpdateProjectRequest represents a request to update an existing project.
type UpdateProjectRequest struct {
	DisplayName *string `json:"displayName,omitempty"`
	Description *string `json:"description,omitempty"`
	Icon        *string `json:"icon,omitempty"`

	// Resource configuration
	CPURequest    *string `json:"cpuRequest,omitempty"`
	CPULimit      *string `json:"cpuLimit,omitempty"`
	MemoryRequest *string `json:"memoryRequest,omitempty"`
	MemoryLimit   *string `json:"memoryLimit,omitempty"`
	StorageSize   *string `json:"storageSize,omitempty"` // PVC expansion (requires storage class support)

	// Tab limits
	MaxTabsPerClient *int `json:"maxTabsPerClient,omitempty"`
	MaxTabsTotal     *int `json:"maxTabsTotal,omitempty"`

	// Feature flags
	DinDEnabled *bool `json:"dindEnabled,omitempty"`

	// GUI desktop support
	GUIEnabled    *bool   `json:"guiEnabled,omitempty"`
	GUIResolution *string `json:"guiResolution,omitempty"`
	GUIVNCPort    *int    `json:"guiVNCPort,omitempty"`

	// PTY session logging for Loki integration
	PTYLoggingEnabled       *bool   `json:"ptyLoggingEnabled,omitempty"`
	PTYLoggingMaxSize       *int64  `json:"ptyLoggingMaxSize,omitempty"`       // Max file size before rotation (bytes)
	PTYLoggingMaxBackups    *int    `json:"ptyLoggingMaxBackups,omitempty"`    // Rotated files to keep
	PTYLoggingBufferSize    *int    `json:"ptyLoggingBufferSize,omitempty"`    // Write buffer size (bytes)
	PTYLoggingFlushInterval *string `json:"ptyLoggingFlushInterval,omitempty"` // Flush interval (e.g., "5s")
	PTYLoggingIncludeRaw    *bool   `json:"ptyLoggingIncludeRaw,omitempty"`    // Include base64 raw bytes

	// Environment variables
	EnvVars map[string]string `json:"envVars,omitempty"`

	// Image configuration
	ImageTag *string `json:"imageTag,omitempty"`
}

// ListFilter defines filtering options for listing projects.
type ListFilter struct {
	Status     ProjectStatus
	UserName   string
	IncludeAll bool // Include deleted projects
	Limit      int
	Offset     int
}

// Defaults for project configuration
const (
	DefaultCPURequest       = "500m"
	DefaultCPULimit         = "4000m"
	DefaultMemoryRequest    = "2Gi"
	DefaultMemoryLimit      = "8Gi"
	DefaultStorageSize      = "50Gi"
	DefaultStorageClass     = "freenas-iscsi-csi"
	DefaultMaxTabsPerClient = 3
	DefaultMaxTabsTotal     = 10
	DefaultImageRepository  = "harbor.support.tools/kubetty/kubetty"
	DefaultImageTag         = "latest"

	// GUI defaults
	DefaultGUIEnabled    = false
	DefaultGUIResolution = "1920x1080x24"
	DefaultGUIVNCPort    = 5901

	// PTY logging defaults
	DefaultPTYLoggingEnabled       = false
	DefaultPTYLoggingMaxSize       = 104857600 // 100MB
	DefaultPTYLoggingMaxBackups    = 3
	DefaultPTYLoggingBufferSize    = 65536 // 64KB
	DefaultPTYLoggingFlushInterval = "5s"
	DefaultPTYLoggingIncludeRaw    = true
)

// ApplyDefaults fills in default values for any unset fields.
func (r *CreateProjectRequest) ApplyDefaults() {
	if r.CPURequest == "" {
		r.CPURequest = DefaultCPURequest
	}
	if r.CPULimit == "" {
		r.CPULimit = DefaultCPULimit
	}
	if r.MemoryRequest == "" {
		r.MemoryRequest = DefaultMemoryRequest
	}
	if r.MemoryLimit == "" {
		r.MemoryLimit = DefaultMemoryLimit
	}
	if r.StorageSize == "" {
		r.StorageSize = DefaultStorageSize
	}
	if r.StorageClass == "" {
		r.StorageClass = DefaultStorageClass
	}
	if r.MaxTabsPerClient == 0 {
		r.MaxTabsPerClient = DefaultMaxTabsPerClient
	}
	if r.MaxTabsTotal == 0 {
		r.MaxTabsTotal = DefaultMaxTabsTotal
	}
	if r.DinDEnabled == nil {
		enabled := true
		r.DinDEnabled = &enabled
	}
	if r.GUIEnabled == nil {
		guiEnabled := DefaultGUIEnabled
		r.GUIEnabled = &guiEnabled
	}
	if r.GUIResolution == "" {
		r.GUIResolution = DefaultGUIResolution
	}
	if r.GUIVNCPort == 0 {
		r.GUIVNCPort = DefaultGUIVNCPort
	}
	if r.PTYLoggingEnabled == nil {
		ptyEnabled := DefaultPTYLoggingEnabled
		r.PTYLoggingEnabled = &ptyEnabled
	}
	if r.PTYLoggingMaxSize == 0 {
		r.PTYLoggingMaxSize = DefaultPTYLoggingMaxSize
	}
	if r.PTYLoggingMaxBackups == 0 {
		r.PTYLoggingMaxBackups = DefaultPTYLoggingMaxBackups
	}
	if r.PTYLoggingBufferSize == 0 {
		r.PTYLoggingBufferSize = DefaultPTYLoggingBufferSize
	}
	if r.PTYLoggingFlushInterval == "" {
		r.PTYLoggingFlushInterval = DefaultPTYLoggingFlushInterval
	}
	if r.PTYLoggingIncludeRaw == nil {
		ptyIncludeRaw := DefaultPTYLoggingIncludeRaw
		r.PTYLoggingIncludeRaw = &ptyIncludeRaw
	}
	if r.ImageRepository == "" {
		r.ImageRepository = DefaultImageRepository
	}
	if r.ImageTag == "" {
		r.ImageTag = DefaultImageTag
	}
	if r.AdminNamespaces == nil {
		r.AdminNamespaces = []string{}
	}
	if r.ReadNamespaces == nil {
		r.ReadNamespaces = []string{}
	}
	if r.EnvVars == nil {
		r.EnvVars = map[string]string{}
	}
}
