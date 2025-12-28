// Package settings provides global configuration management for KubeTTY.
package settings

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// SettingCategory represents the category of a setting.
type SettingCategory string

const (
	CategoryProjectDefaults SettingCategory = "project_defaults"
	CategoryAuth            SettingCategory = "auth"
	CategoryFeatures        SettingCategory = "features"
	CategoryUI              SettingCategory = "ui"
	CategoryController      SettingCategory = "controller"
	CategoryNotifications   SettingCategory = "notifications"
	CategorySecrets         SettingCategory = "secrets"
)

// ValidCategories returns all valid setting categories.
func ValidCategories() []SettingCategory {
	return []SettingCategory{
		CategoryProjectDefaults,
		CategoryAuth,
		CategoryFeatures,
		CategoryUI,
		CategoryController,
		CategoryNotifications,
		CategorySecrets,
	}
}

// IsValid checks if the category is valid.
func (c SettingCategory) IsValid() bool {
	for _, valid := range ValidCategories() {
		if c == valid {
			return true
		}
	}
	return false
}

// SettingValueType represents the data type of a setting value.
type SettingValueType string

const (
	ValueTypeString SettingValueType = "string"
	ValueTypeInt    SettingValueType = "int"
	ValueTypeBool   SettingValueType = "bool"
	ValueTypeJSON   SettingValueType = "json"
)

// Setting represents a global configuration setting.
type Setting struct {
	ID          uuid.UUID        `json:"id"`
	Category    SettingCategory  `json:"category"`
	Key         string           `json:"key"`
	Value       json.RawMessage  `json:"value"`
	ValueType   SettingValueType `json:"valueType"`
	DisplayName string           `json:"displayName"`
	Description string           `json:"description,omitempty"`
	IsSensitive bool             `json:"isSensitive"`
	IsReadonly  bool             `json:"isReadonly"`
	Validation  json.RawMessage  `json:"validation,omitempty"`
	CreatedAt   time.Time        `json:"createdAt"`
	UpdatedAt   time.Time        `json:"updatedAt"`
}

// GetString returns the setting value as a string.
// Returns defaultVal if the value cannot be parsed.
func (s *Setting) GetString(defaultVal string) string {
	if s == nil || s.Value == nil {
		return defaultVal
	}
	var val string
	if err := json.Unmarshal(s.Value, &val); err != nil {
		return defaultVal
	}
	return val
}

// GetInt returns the setting value as an int.
// Returns defaultVal if the value cannot be parsed.
func (s *Setting) GetInt(defaultVal int) int {
	if s == nil || s.Value == nil {
		return defaultVal
	}
	var val int
	if err := json.Unmarshal(s.Value, &val); err != nil {
		// Try parsing as float64 (JSON numbers are float64)
		var f float64
		if err := json.Unmarshal(s.Value, &f); err != nil {
			return defaultVal
		}
		return int(f)
	}
	return val
}

// GetBool returns the setting value as a bool.
// Returns defaultVal if the value cannot be parsed.
func (s *Setting) GetBool(defaultVal bool) bool {
	if s == nil || s.Value == nil {
		return defaultVal
	}
	var val bool
	if err := json.Unmarshal(s.Value, &val); err != nil {
		return defaultVal
	}
	return val
}

// GetJSON unmarshals the setting value into the provided target.
// Returns error if unmarshaling fails.
func (s *Setting) GetJSON(target interface{}) error {
	if s == nil || s.Value == nil {
		return nil
	}
	return json.Unmarshal(s.Value, target)
}

// MaskedValue returns the value with sensitive data masked.
func (s *Setting) MaskedValue() json.RawMessage {
	if s.IsSensitive {
		// Check if value is empty string
		var str string
		if err := json.Unmarshal(s.Value, &str); err == nil && str == "" {
			return s.Value // Return empty as-is
		}
		return json.RawMessage(`"********"`)
	}
	return s.Value
}

// SettingHistory represents a change record for a setting.
type SettingHistory struct {
	ID           uuid.UUID       `json:"id"`
	SettingID    *uuid.UUID      `json:"settingId,omitempty"`
	Category     SettingCategory `json:"category"`
	Key          string          `json:"key"`
	OldValue     json.RawMessage `json:"oldValue,omitempty"`
	NewValue     json.RawMessage `json:"newValue,omitempty"`
	ChangeType   string          `json:"changeType"` // insert, update, delete
	ChangedBy    string          `json:"changedBy"`
	ChangedAt    time.Time       `json:"changedAt"`
	ChangeSource string          `json:"changeSource"` // api, migration, system
	ChangeReason string          `json:"changeReason,omitempty"`
	ClientIP     string          `json:"clientIp,omitempty"`
	UserAgent    string          `json:"userAgent,omitempty"`
}

// UpdateSettingRequest represents a request to update a setting.
type UpdateSettingRequest struct {
	Value        interface{} `json:"value"`
	ChangeReason string      `json:"changeReason,omitempty"`
}

// CreateSettingRequest represents a request to create a new setting.
type CreateSettingRequest struct {
	Category    SettingCategory  `json:"category"`
	Key         string           `json:"key"`
	Value       interface{}      `json:"value"`
	ValueType   SettingValueType `json:"valueType"`
	DisplayName string           `json:"displayName"`
	Description string           `json:"description,omitempty"`
	IsSensitive bool             `json:"isSensitive"`
	IsReadonly  bool             `json:"isReadonly"`
	Validation  interface{}      `json:"validation,omitempty"`
}

// HistoryFilter provides filtering options for history queries.
type HistoryFilter struct {
	Limit      int
	Offset     int
	ChangeType string // Optional: filter by change type
	ChangedBy  string // Optional: filter by user
	Since      *time.Time
	Until      *time.Time
}

// ListFilter provides filtering options for listing settings.
type ListFilter struct {
	Category               SettingCategory
	IncludeSensitiveValues bool // If false, sensitive values are masked
}

// SettingsResponse represents a collection of settings grouped by category.
type SettingsResponse struct {
	Settings   []Setting      `json:"settings"`
	Categories map[string]int `json:"categories"` // Count per category
	Total      int            `json:"total"`
}
