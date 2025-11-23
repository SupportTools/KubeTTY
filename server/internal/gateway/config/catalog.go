package config

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	defaultServicePort = 8080
)

// Catalog describes all projects available through the gateway UI.
type Catalog struct {
	Projects []Project `yaml:"projects" json:"projects"`
}

// Project captures the routing details for a single downstream pod.
type Project struct {
	ID          string        `yaml:"id" json:"id"`
	DisplayName string        `yaml:"displayName" json:"displayName"`
	Namespace   string        `yaml:"namespace" json:"namespace"`
	Service     string        `yaml:"service" json:"service"`
	Port        int           `yaml:"port" json:"port"`
	Description string        `yaml:"description" json:"description"`
	Icon        string        `yaml:"icon" json:"icon"`
	Tags        []string      `yaml:"tags" json:"tags"`
	HealthCheck *HealthCheck  `yaml:"healthCheck" json:"healthCheck"`
	Limits      ProjectLimits `yaml:"limits" json:"limits"`
}

// HealthCheck defines how to poll downstream readiness (optional).
type HealthCheck struct {
	Path           string `yaml:"path" json:"path"`
	PeriodSeconds  int    `yaml:"periodSeconds" json:"periodSeconds"`
	TimeoutSeconds int    `yaml:"timeoutSeconds" json:"timeoutSeconds"`
}

// ProjectLimits allows overriding per-project concurrency controls and resource limits.
type ProjectLimits struct {
	MaxTabsPerClient int   `yaml:"maxTabsPerClient" json:"maxTabsPerClient"`
	MaxTabsTotal     int   `yaml:"maxTabsTotal" json:"maxTabsTotal"`
	CPUMillicores    int64 `yaml:"cpuMillicores" json:"cpuMillicores"` // CPU limit in millicores (e.g., 1000 = 1 CPU)
	MemoryBytes      int64 `yaml:"memoryBytes" json:"memoryBytes"`     // Memory limit in bytes
}

// LoadCatalog reads a YAML/JSON file from disk and validates it.
func LoadCatalog(path string) (Catalog, error) {
	if path == "" {
		return Catalog{}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Catalog{}, fmt.Errorf("read catalog %q: %w", path, err)
	}
	return ParseCatalog(data)
}

// ParseCatalog unmarshals raw YAML/JSON bytes.
func ParseCatalog(data []byte) (Catalog, error) {
	var cat Catalog
	if len(strings.TrimSpace(string(data))) == 0 {
		return cat, nil
	}
	if err := yaml.Unmarshal(data, &cat); err != nil {
		return Catalog{}, fmt.Errorf("unmarshal catalog: %w", err)
	}
	cat.normalize()
	if err := cat.Validate(); err != nil {
		return Catalog{}, err
	}
	return cat, nil
}

// Validate returns an aggregated error when the catalog contains invalid projects.
func (c Catalog) Validate() error {
	var errs []error
	seen := make(map[string]struct{}, len(c.Projects))
	for idx := range c.Projects {
		p := c.Projects[idx]
		if _, ok := seen[p.ID]; ok && p.ID != "" {
			errs = append(errs, fmt.Errorf("project %q: duplicate id", p.ID))
		}
		seen[p.ID] = struct{}{}
		if err := p.validate(); err != nil {
			errs = append(errs, fmt.Errorf("project[%d]: %w", idx, err))
		}
	}
	return errors.Join(errs...)
}

func (c *Catalog) normalize() {
	for i := range c.Projects {
		if c.Projects[i].Port == 0 {
			c.Projects[i].Port = defaultServicePort
		}
	}
}

func (p Project) validate() error {
	var errs []error
	if p.ID == "" {
		errs = append(errs, errors.New("id is required"))
	} else if !isValidProjectID(p.ID) {
		errs = append(errs, fmt.Errorf("id %q must be lowercase alphanumeric or hyphen", p.ID))
	}
	if p.Namespace == "" {
		errs = append(errs, errors.New("namespace is required"))
	}
	if p.Service == "" {
		errs = append(errs, errors.New("service is required"))
	}
	if p.Port <= 0 || p.Port > 65535 {
		errs = append(errs, fmt.Errorf("port %d must be between 1-65535", p.Port))
	}
	return errors.Join(errs...)
}

func isValidProjectID(id string) bool {
	for _, r := range id {
		if r >= 'a' && r <= 'z' {
			continue
		}
		if r >= '0' && r <= '9' {
			continue
		}
		if r == '-' {
			continue
		}
		return false
	}
	return id != ""
}
