package admin

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	log "github.com/sirupsen/logrus"
	"github.com/supporttools/KubeTTY/server/internal/settings"
	apierrors "github.com/supporttools/KubeTTY/server/internal/shared/errors"
	"github.com/supporttools/KubeTTY/server/internal/shared/util"
)

// SettingsHandlers provides HTTP handlers for settings management.
type SettingsHandlers struct {
	store settings.Store
}

// NewSettingsHandlers creates a new SettingsHandlers instance.
func NewSettingsHandlers(store settings.Store) *SettingsHandlers {
	return &SettingsHandlers{store: store}
}

// ListSettings handles GET /api/admin/settings
func (h *SettingsHandlers) ListSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	allSettings, err := h.store.ListAll(ctx)
	if err != nil {
		log.WithError(err).Error("admin/settings: failed to list settings")
		_ = apierrors.WriteError(w, apierrors.InternalServerError("failed to list settings", ""))
		return
	}

	if allSettings == nil {
		allSettings = []settings.Setting{}
	}

	// Mask sensitive values
	maskSensitiveValues := r.URL.Query().Get("includeSensitive") != "true"
	if maskSensitiveValues {
		for i := range allSettings {
			if allSettings[i].IsSensitive {
				allSettings[i].Value = allSettings[i].MaskedValue()
			}
		}
	}

	// Count by category
	categories := make(map[string]int)
	for _, s := range allSettings {
		categories[string(s.Category)]++
	}

	_ = util.WriteJSON(w, http.StatusOK, settings.SettingsResponse{
		Settings:   allSettings,
		Categories: categories,
		Total:      len(allSettings),
	})
}

// ListByCategory handles GET /api/admin/settings/{category}
func (h *SettingsHandlers) ListByCategory(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	category := settings.SettingCategory(r.PathValue("category"))
	if !category.IsValid() {
		_ = apierrors.WriteError(w, apierrors.BadRequest("invalid category", ""))
		return
	}

	categorySettings, err := h.store.List(ctx, category)
	if err != nil {
		log.WithError(err).Error("admin/settings: failed to list settings by category")
		_ = apierrors.WriteError(w, apierrors.InternalServerError("failed to list settings", ""))
		return
	}

	if categorySettings == nil {
		categorySettings = []settings.Setting{}
	}

	// Mask sensitive values
	maskSensitiveValues := r.URL.Query().Get("includeSensitive") != "true"
	if maskSensitiveValues {
		for i := range categorySettings {
			if categorySettings[i].IsSensitive {
				categorySettings[i].Value = categorySettings[i].MaskedValue()
			}
		}
	}

	_ = util.WriteJSON(w, http.StatusOK, map[string]any{
		"settings": categorySettings,
		"category": category,
		"total":    len(categorySettings),
	})
}

// GetSetting handles GET /api/admin/settings/{category}/{key}
func (h *SettingsHandlers) GetSetting(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	category := settings.SettingCategory(r.PathValue("category"))
	key := r.PathValue("key")

	if !category.IsValid() {
		_ = apierrors.WriteError(w, apierrors.BadRequest("invalid category", ""))
		return
	}

	if key == "" {
		_ = apierrors.WriteError(w, apierrors.BadRequest("key is required", ""))
		return
	}

	setting, err := h.store.Get(ctx, category, key)
	if err != nil {
		if errors.Is(err, settings.ErrSettingNotFound) {
			_ = apierrors.WriteError(w, apierrors.NotFound("setting not found", ""))
			return
		}
		log.WithError(err).Error("admin/settings: failed to get setting")
		_ = apierrors.WriteError(w, apierrors.InternalServerError("failed to get setting", ""))
		return
	}

	// Mask sensitive value unless explicitly requested
	if setting.IsSensitive && r.URL.Query().Get("includeSensitive") != "true" {
		setting.Value = setting.MaskedValue()
	}

	_ = util.WriteJSON(w, http.StatusOK, setting)
}

// UpdateSetting handles PUT /api/admin/settings/{category}/{key}
func (h *SettingsHandlers) UpdateSetting(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	category := settings.SettingCategory(r.PathValue("category"))
	key := r.PathValue("key")

	if !category.IsValid() {
		_ = apierrors.WriteError(w, apierrors.BadRequest("invalid category", ""))
		return
	}

	if key == "" {
		_ = apierrors.WriteError(w, apierrors.BadRequest("key is required", ""))
		return
	}

	var req settings.UpdateSettingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		_ = apierrors.WriteError(w, apierrors.BadRequest("invalid JSON", ""))
		return
	}

	// Get username from context (set by auth middleware)
	changedBy := "anonymous"
	if username := r.Context().Value("username"); username != nil {
		changedBy = username.(string)
	}

	setting, err := h.store.Update(ctx, category, key, req.Value, changedBy)
	if err != nil {
		if errors.Is(err, settings.ErrSettingNotFound) {
			_ = apierrors.WriteError(w, apierrors.NotFound("setting not found", ""))
			return
		}
		if errors.Is(err, settings.ErrSettingReadonly) {
			_ = apierrors.WriteError(w, apierrors.Forbidden("setting is read-only", ""))
			return
		}
		log.WithError(err).Error("admin/settings: failed to update setting")
		_ = apierrors.WriteError(w, apierrors.InternalServerError("failed to update setting", ""))
		return
	}

	log.WithFields(log.Fields{
		"category":  category,
		"key":       key,
		"changedBy": changedBy,
	}).Info("admin/settings: setting updated")

	// Mask sensitive value in response
	if setting.IsSensitive {
		setting.Value = setting.MaskedValue()
	}

	_ = util.WriteJSON(w, http.StatusOK, setting)
}

// CreateSetting handles POST /api/admin/settings
func (h *SettingsHandlers) CreateSetting(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req settings.CreateSettingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		_ = apierrors.WriteError(w, apierrors.BadRequest("invalid JSON", ""))
		return
	}

	// Validate required fields
	req.Key = strings.TrimSpace(req.Key)
	req.DisplayName = strings.TrimSpace(req.DisplayName)

	if !req.Category.IsValid() {
		_ = apierrors.WriteError(w, apierrors.BadRequest("invalid category", ""))
		return
	}

	if req.Key == "" {
		_ = apierrors.WriteError(w, apierrors.BadRequest("key is required", ""))
		return
	}

	if req.DisplayName == "" {
		_ = apierrors.WriteError(w, apierrors.BadRequest("displayName is required", ""))
		return
	}

	// Get username from context
	changedBy := "anonymous"
	if username := r.Context().Value("username"); username != nil {
		changedBy = username.(string)
	}

	setting, err := h.store.Create(ctx, req, changedBy)
	if err != nil {
		if errors.Is(err, settings.ErrDuplicateSetting) {
			_ = apierrors.WriteError(w, apierrors.Conflict("setting already exists", ""))
			return
		}
		log.WithError(err).Error("admin/settings: failed to create setting")
		_ = apierrors.WriteError(w, apierrors.InternalServerError("failed to create setting", ""))
		return
	}

	log.WithFields(log.Fields{
		"category":  req.Category,
		"key":       req.Key,
		"changedBy": changedBy,
	}).Info("admin/settings: setting created")

	_ = util.WriteJSON(w, http.StatusCreated, setting)
}

// DeleteSetting handles DELETE /api/admin/settings/{category}/{key}
func (h *SettingsHandlers) DeleteSetting(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	category := settings.SettingCategory(r.PathValue("category"))
	key := r.PathValue("key")

	if !category.IsValid() {
		_ = apierrors.WriteError(w, apierrors.BadRequest("invalid category", ""))
		return
	}

	if key == "" {
		_ = apierrors.WriteError(w, apierrors.BadRequest("key is required", ""))
		return
	}

	// Get username from context
	changedBy := "anonymous"
	if username := r.Context().Value("username"); username != nil {
		changedBy = username.(string)
	}

	err := h.store.Delete(ctx, category, key, changedBy)
	if err != nil {
		if errors.Is(err, settings.ErrSettingNotFound) {
			_ = apierrors.WriteError(w, apierrors.NotFound("setting not found", ""))
			return
		}
		if errors.Is(err, settings.ErrSettingReadonly) {
			_ = apierrors.WriteError(w, apierrors.Forbidden("setting is read-only", ""))
			return
		}
		log.WithError(err).Error("admin/settings: failed to delete setting")
		_ = apierrors.WriteError(w, apierrors.InternalServerError("failed to delete setting", ""))
		return
	}

	log.WithFields(log.Fields{
		"category":  category,
		"key":       key,
		"changedBy": changedBy,
	}).Info("admin/settings: setting deleted")

	w.WriteHeader(http.StatusNoContent)
}

// GetSettingHistory handles GET /api/admin/settings/{category}/{key}/history
func (h *SettingsHandlers) GetSettingHistory(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	category := settings.SettingCategory(r.PathValue("category"))
	key := r.PathValue("key")

	if !category.IsValid() {
		_ = apierrors.WriteError(w, apierrors.BadRequest("invalid category", ""))
		return
	}

	if key == "" {
		_ = apierrors.WriteError(w, apierrors.BadRequest("key is required", ""))
		return
	}

	filter := settings.HistoryFilter{
		Limit: 50,
	}

	history, err := h.store.GetHistory(ctx, category, key, filter)
	if err != nil {
		log.WithError(err).Error("admin/settings: failed to get setting history")
		_ = apierrors.WriteError(w, apierrors.InternalServerError("failed to get history", ""))
		return
	}

	if history == nil {
		history = []settings.SettingHistory{}
	}

	_ = util.WriteJSON(w, http.StatusOK, map[string]any{
		"history":  history,
		"category": category,
		"key":      key,
		"total":    len(history),
	})
}

// GetAllHistory handles GET /api/admin/settings/history
func (h *SettingsHandlers) GetAllHistory(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	filter := settings.HistoryFilter{
		Limit: 100,
	}

	history, err := h.store.GetAllHistory(ctx, filter)
	if err != nil {
		log.WithError(err).Error("admin/settings: failed to get all history")
		_ = apierrors.WriteError(w, apierrors.InternalServerError("failed to get history", ""))
		return
	}

	if history == nil {
		history = []settings.SettingHistory{}
	}

	_ = util.WriteJSON(w, http.StatusOK, map[string]any{
		"history": history,
		"total":   len(history),
	})
}

// GetCategories handles GET /api/admin/settings/categories
func (h *SettingsHandlers) GetCategories(w http.ResponseWriter, r *http.Request) {
	categories := settings.ValidCategories()

	categoryInfo := make([]map[string]string, len(categories))
	for i, cat := range categories {
		categoryInfo[i] = map[string]string{
			"name":        string(cat),
			"displayName": getCategoryDisplayName(cat),
		}
	}

	_ = util.WriteJSON(w, http.StatusOK, map[string]any{
		"categories": categoryInfo,
	})
}

func getCategoryDisplayName(cat settings.SettingCategory) string {
	switch cat {
	case settings.CategoryProjectDefaults:
		return "Project Defaults"
	case settings.CategoryAuth:
		return "Authentication"
	case settings.CategoryFeatures:
		return "Features"
	case settings.CategoryUI:
		return "User Interface"
	case settings.CategoryController:
		return "Controller"
	case settings.CategoryNotifications:
		return "Notifications"
	case settings.CategorySecrets:
		return "Secrets"
	default:
		return string(cat)
	}
}
