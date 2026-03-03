package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
	"github.com/supporttools/KubeTTY/server/internal/controller"
	"github.com/supporttools/KubeTTY/server/internal/projects"
	"github.com/supporttools/KubeTTY/server/internal/settings"
	apierrors "github.com/supporttools/KubeTTY/server/internal/shared/errors"
	"github.com/supporttools/KubeTTY/server/internal/shared/util"
)

// ProjectHandlers provides HTTP handlers for project management.
type ProjectHandlers struct {
	store               projects.Store
	settingsStore       settings.Store
	controller          *controller.Controller
	recommendedImageTag string
	onProjectDeleted    func(projectName string) // Callback when project is deleted
}

// NewProjectHandlers creates a new ProjectHandlers instance.
func NewProjectHandlers(store projects.Store, ctrl *controller.Controller, recommendedImageTag string) *ProjectHandlers {
	return &ProjectHandlers{
		store:               store,
		controller:          ctrl,
		recommendedImageTag: recommendedImageTag,
	}
}

// SetSettingsStore sets the settings store for applying defaults from DB.
func (h *ProjectHandlers) SetSettingsStore(store settings.Store) {
	h.settingsStore = store
}

// applySettingsDefaults applies default values from settings store to the request.
// Only fills in empty/zero values - explicitly provided values are preserved.
func (h *ProjectHandlers) applySettingsDefaults(ctx context.Context, req *projects.CreateProjectRequest) {
	if h.settingsStore == nil {
		return // No settings store configured, use hardcoded defaults
	}

	cat := settings.CategoryProjectDefaults

	if req.CPURequest == "" {
		req.CPURequest = h.settingsStore.GetString(ctx, cat, "cpu_request", projects.DefaultCPURequest)
	}
	if req.CPULimit == "" {
		req.CPULimit = h.settingsStore.GetString(ctx, cat, "cpu_limit", projects.DefaultCPULimit)
	}
	if req.MemoryRequest == "" {
		req.MemoryRequest = h.settingsStore.GetString(ctx, cat, "memory_request", projects.DefaultMemoryRequest)
	}
	if req.MemoryLimit == "" {
		req.MemoryLimit = h.settingsStore.GetString(ctx, cat, "memory_limit", projects.DefaultMemoryLimit)
	}
	if req.StorageSize == "" {
		req.StorageSize = h.settingsStore.GetString(ctx, cat, "storage_size", projects.DefaultStorageSize)
	}
	if req.StorageClass == "" {
		req.StorageClass = h.settingsStore.GetString(ctx, cat, "storage_class", projects.DefaultStorageClass)
	}
	if req.MaxTabsPerClient == 0 {
		req.MaxTabsPerClient = h.settingsStore.GetInt(ctx, cat, "max_tabs_per_client", projects.DefaultMaxTabsPerClient)
	}
	if req.MaxTabsTotal == 0 {
		req.MaxTabsTotal = h.settingsStore.GetInt(ctx, cat, "max_tabs_total", projects.DefaultMaxTabsTotal)
	}
	if req.SessionMode == "" {
		req.SessionMode = projects.SessionMode(h.settingsStore.GetString(ctx, cat, "session_mode", string(projects.DefaultSessionMode)))
	}
	if req.ImageRepository == "" {
		req.ImageRepository = h.settingsStore.GetString(ctx, cat, "image_repository", projects.DefaultImageRepository)
	}
	if req.ImageTag == "" {
		req.ImageTag = h.settingsStore.GetString(ctx, cat, "image_tag", projects.DefaultImageTag)
	}
	// DinDEnabled: only set from settings if not explicitly set in request
	// Using pointer allows distinguishing between "user set to false" vs "not set at all"
	if req.DinDEnabled == nil {
		dindDefault := h.settingsStore.GetBool(ctx, cat, "dind_enabled", true)
		req.DinDEnabled = &dindDefault
	}
}

// SetDeleteCallback sets a callback function to be called when a project is deleted.
// This is used to unregister the project from the tabManager.
func (h *ProjectHandlers) SetDeleteCallback(cb func(projectName string)) {
	h.onProjectDeleted = cb
}

// ListProjects handles GET /api/admin/projects
func (h *ProjectHandlers) ListProjects(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	filter := projects.ListFilter{
		Limit: 100, // Default limit
	}

	// Parse query parameters
	if status := r.URL.Query().Get("status"); status != "" {
		filter.Status = projects.ProjectStatus(status)
	}
	if user := r.URL.Query().Get("user"); user != "" {
		filter.UserName = user
	}

	projectList, err := h.store.List(ctx, filter)
	if err != nil {
		log.WithError(err).Error("admin/projects: failed to list projects")
		_ = apierrors.WriteError(w, apierrors.InternalServerError("failed to list projects", ""))
		return
	}

	// Return empty array instead of null
	if projectList == nil {
		projectList = []projects.Project{}
	}

	_ = util.WriteJSON(w, http.StatusOK, map[string]any{
		"projects": projectList,
		"total":    len(projectList),
	})
}

// CreateProject handles POST /api/admin/projects
func (h *ProjectHandlers) CreateProject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req projects.CreateProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		_ = apierrors.WriteError(w, apierrors.BadRequest("invalid JSON", ""))
		return
	}

	// Validate required fields
	req.Name = strings.TrimSpace(req.Name)
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	req.UserName = strings.TrimSpace(req.UserName)

	if req.Name == "" {
		_ = apierrors.WriteError(w, apierrors.BadRequest("name is required", ""))
		return
	}
	if len(req.Name) > 63 {
		_ = apierrors.WriteError(w, apierrors.BadRequest("name must be 63 characters or less", ""))
		return
	}
	if req.DisplayName == "" {
		_ = apierrors.WriteError(w, apierrors.BadRequest("displayName is required", ""))
		return
	}
	if req.UserName == "" {
		_ = apierrors.WriteError(w, apierrors.BadRequest("userName is required", ""))
		return
	}
	if req.SessionMode != "" && !req.SessionMode.IsValid() {
		_ = apierrors.WriteError(w, apierrors.BadRequest(
			"sessionMode must be one of: exclusive_takeover, shared_concurrent, independent_shells", ""))
		return
	}

	// Validate GUI configuration if provided
	if !isValidGUIResolution(req.GUIResolution) {
		_ = apierrors.WriteError(w, apierrors.BadRequest(
			"guiResolution must be in format WIDTHxHEIGHTxDEPTH (e.g., 1920x1080x24)", ""))
		return
	}
	if !isValidVNCPort(req.GUIVNCPort) {
		_ = apierrors.WriteError(w, apierrors.BadRequest(
			"guiVNCPort must be in range 5900-5999", ""))
		return
	}

	// Apply defaults from settings store (falls back to hardcoded defaults)
	h.applySettingsDefaults(ctx, &req)

	project, err := h.store.Create(ctx, req)
	if err != nil {
		if errors.Is(err, projects.ErrInvalidName) {
			_ = apierrors.WriteError(w, apierrors.BadRequest(
				"invalid name: must be lowercase alphanumeric with dashes, starting with a letter", ""))
			return
		}
		if errors.Is(err, projects.ErrDuplicateName) {
			_ = apierrors.WriteError(w, apierrors.Conflict("project name already exists", ""))
			return
		}
		log.WithError(err).Error("admin/projects: failed to create project")
		_ = apierrors.WriteError(w, apierrors.InternalServerError("failed to create project", ""))
		return
	}

	log.WithFields(log.Fields{
		"project_id":   project.ID.String(),
		"project_name": project.Name,
		"user":         project.UserName,
	}).Info("admin/projects: project created")

	_ = util.WriteJSON(w, http.StatusCreated, project)
}

// GetProject handles GET /api/admin/projects/{id}
func (h *ProjectHandlers) GetProject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract project ID from path
	projectID, err := extractProjectID(r)
	if err != nil {
		_ = apierrors.WriteError(w, apierrors.BadRequest("invalid project ID", ""))
		return
	}

	project, err := h.store.Get(ctx, projectID)
	if err != nil {
		if errors.Is(err, projects.ErrProjectNotFound) {
			_ = apierrors.WriteError(w, apierrors.NotFound("project not found", ""))
			return
		}
		log.WithError(err).Error("admin/projects: failed to get project")
		_ = apierrors.WriteError(w, apierrors.InternalServerError("failed to get project", ""))
		return
	}

	_ = util.WriteJSON(w, http.StatusOK, project)
}

// UpdateProject handles PUT /api/admin/projects/{id}
func (h *ProjectHandlers) UpdateProject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	projectID, err := extractProjectID(r)
	if err != nil {
		_ = apierrors.WriteError(w, apierrors.BadRequest("invalid project ID", ""))
		return
	}

	var req projects.UpdateProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		_ = apierrors.WriteError(w, apierrors.BadRequest("invalid JSON", ""))
		return
	}
	if req.SessionMode != nil && !req.SessionMode.IsValid() {
		_ = apierrors.WriteError(w, apierrors.BadRequest(
			"sessionMode must be one of: exclusive_takeover, shared_concurrent, independent_shells", ""))
		return
	}

	// Validate GUI configuration if provided
	if req.GUIResolution != nil && !isValidGUIResolution(*req.GUIResolution) {
		_ = apierrors.WriteError(w, apierrors.BadRequest(
			"guiResolution must be in format WIDTHxHEIGHTxDEPTH (e.g., 1920x1080x24)", ""))
		return
	}
	if req.GUIVNCPort != nil && !isValidVNCPort(*req.GUIVNCPort) {
		_ = apierrors.WriteError(w, apierrors.BadRequest(
			"guiVNCPort must be in range 5900-5999", ""))
		return
	}

	project, err := h.store.Update(ctx, projectID, req)
	if err != nil {
		if errors.Is(err, projects.ErrProjectNotFound) {
			_ = apierrors.WriteError(w, apierrors.NotFound("project not found", ""))
			return
		}
		log.WithError(err).Error("admin/projects: failed to update project")
		_ = apierrors.WriteError(w, apierrors.InternalServerError("failed to update project", ""))
		return
	}

	// If image tag or resources changed, trigger an update
	if req.ImageTag != nil || req.CPULimit != nil || req.MemoryLimit != nil || req.StorageSize != nil {
		if err := h.store.SetStatus(ctx, projectID, projects.StatusUpdating, "Applying configuration changes"); err != nil {
			log.WithError(err).Warn("admin/projects: failed to set updating status")
		}
	}

	log.WithFields(log.Fields{
		"project_id":   project.ID.String(),
		"project_name": project.Name,
	}).Info("admin/projects: project updated")

	_ = util.WriteJSON(w, http.StatusOK, project)
}

// DeleteProject handles DELETE /api/admin/projects/{id}
func (h *ProjectHandlers) DeleteProject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	projectID, err := extractProjectID(r)
	if err != nil {
		_ = apierrors.WriteError(w, apierrors.BadRequest("invalid project ID", ""))
		return
	}

	// Get project first for logging
	project, err := h.store.Get(ctx, projectID)
	if err != nil {
		if errors.Is(err, projects.ErrProjectNotFound) {
			_ = apierrors.WriteError(w, apierrors.NotFound("project not found", ""))
			return
		}
		log.WithError(err).Error("admin/projects: failed to get project for deletion")
		_ = apierrors.WriteError(w, apierrors.InternalServerError("failed to delete project", ""))
		return
	}

	// Soft delete (marks for deletion, controller will clean up)
	if err := h.store.Delete(ctx, projectID); err != nil {
		if errors.Is(err, projects.ErrProjectNotFound) {
			_ = apierrors.WriteError(w, apierrors.NotFound("project not found", ""))
			return
		}
		log.WithError(err).Error("admin/projects: failed to delete project")
		_ = apierrors.WriteError(w, apierrors.InternalServerError("failed to delete project", ""))
		return
	}

	log.WithFields(log.Fields{
		"project_id":   project.ID.String(),
		"project_name": project.Name,
	}).Info("admin/projects: project marked for deletion")

	// Notify callback to unregister project from tabManager
	if h.onProjectDeleted != nil {
		h.onProjectDeleted(project.Name)
	}

	w.WriteHeader(http.StatusNoContent)
}

// ResyncProject handles POST /api/admin/projects/{id}/resync
// This triggers a full resync of project resources, recreating any missing
// resources while preserving existing ones (especially PVCs with data).
func (h *ProjectHandlers) ResyncProject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	projectID, err := extractProjectID(r)
	if err != nil {
		_ = apierrors.WriteError(w, apierrors.BadRequest("invalid project ID", ""))
		return
	}

	project, err := h.store.Get(ctx, projectID)
	if err != nil {
		if errors.Is(err, projects.ErrProjectNotFound) {
			_ = apierrors.WriteError(w, apierrors.NotFound("project not found", ""))
			return
		}
		log.WithError(err).Error("admin/projects: failed to get project for resync")
		_ = apierrors.WriteError(w, apierrors.InternalServerError("failed to resync project", ""))
		return
	}

	// Only allow resync for failed or running projects
	if project.Status != projects.StatusRunning && project.Status != projects.StatusFailed {
		_ = apierrors.WriteError(w, apierrors.BadRequest(
			"project must be running or failed to resync (current status: "+string(project.Status)+")", ""))
		return
	}

	if err := h.controller.ResyncProject(ctx, project); err != nil {
		log.WithError(err).Error("admin/projects: failed to resync project")
		_ = apierrors.WriteError(w, apierrors.InternalServerError("failed to resync project", ""))
		return
	}

	log.WithFields(log.Fields{
		"project_id":      project.ID.String(),
		"project_name":    project.Name,
		"previous_status": project.Status,
	}).Info("admin/projects: project resync triggered")

	_ = util.WriteJSON(w, http.StatusOK, map[string]string{
		"message": "project resync triggered, missing resources will be recreated",
	})
}

// RestartProject handles POST /api/admin/projects/{id}/restart
func (h *ProjectHandlers) RestartProject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	projectID, err := extractProjectID(r)
	if err != nil {
		_ = apierrors.WriteError(w, apierrors.BadRequest("invalid project ID", ""))
		return
	}

	project, err := h.store.Get(ctx, projectID)
	if err != nil {
		if errors.Is(err, projects.ErrProjectNotFound) {
			_ = apierrors.WriteError(w, apierrors.NotFound("project not found", ""))
			return
		}
		log.WithError(err).Error("admin/projects: failed to get project for restart")
		_ = apierrors.WriteError(w, apierrors.InternalServerError("failed to restart project", ""))
		return
	}

	if project.Status != projects.StatusRunning && project.Status != projects.StatusFailed {
		_ = apierrors.WriteError(w, apierrors.BadRequest("project must be running or failed to restart", ""))
		return
	}

	if err := h.controller.RestartProject(ctx, project); err != nil {
		log.WithError(err).Error("admin/projects: failed to restart project")
		_ = apierrors.WriteError(w, apierrors.InternalServerError("failed to restart project", ""))
		return
	}

	log.WithFields(log.Fields{
		"project_id":   project.ID.String(),
		"project_name": project.Name,
	}).Info("admin/projects: project restart triggered")

	_ = util.WriteJSON(w, http.StatusOK, map[string]string{
		"message": "project restart triggered",
	})
}

// GetProjectStatus handles GET /api/admin/projects/{id}/status
func (h *ProjectHandlers) GetProjectStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	projectID, err := extractProjectID(r)
	if err != nil {
		_ = apierrors.WriteError(w, apierrors.BadRequest("invalid project ID", ""))
		return
	}

	project, err := h.store.Get(ctx, projectID)
	if err != nil {
		if errors.Is(err, projects.ErrProjectNotFound) {
			_ = apierrors.WriteError(w, apierrors.NotFound("project not found", ""))
			return
		}
		log.WithError(err).Error("admin/projects: failed to get project")
		_ = apierrors.WriteError(w, apierrors.InternalServerError("failed to get project status", ""))
		return
	}

	deployStatus, err := h.controller.GetDeploymentStatus(ctx, project)
	if err != nil {
		log.WithError(err).Error("admin/projects: failed to get deployment status")
		_ = apierrors.WriteError(w, apierrors.InternalServerError("failed to get deployment status", ""))
		return
	}

	_ = util.WriteJSON(w, http.StatusOK, map[string]any{
		"project":    project,
		"deployment": deployStatus,
	})
}

// GetUpgradeInfo handles GET /api/admin/projects/{id}/upgrade-info
func (h *ProjectHandlers) GetUpgradeInfo(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	projectID, err := extractProjectID(r)
	if err != nil {
		_ = apierrors.WriteError(w, apierrors.BadRequest("invalid project ID", ""))
		return
	}

	project, err := h.store.Get(ctx, projectID)
	if err != nil {
		if errors.Is(err, projects.ErrProjectNotFound) {
			_ = apierrors.WriteError(w, apierrors.NotFound("project not found", ""))
			return
		}
		log.WithError(err).Error("admin/projects: failed to get project")
		_ = apierrors.WriteError(w, apierrors.InternalServerError("failed to get project", ""))
		return
	}

	// Calculate minutes since last activity (using UTC to avoid time zone issues)
	var minutesSinceActivity *int
	if project.LastActivity != nil {
		now := time.Now().UTC()
		lastActivity := project.LastActivity.UTC()
		minutes := int(now.Sub(lastActivity).Minutes())
		minutesSinceActivity = &minutes
	}

	_ = util.WriteJSON(w, http.StatusOK, map[string]any{
		"currentVersion":       project.ImageTag,
		"recommendedVersion":   h.recommendedImageTag,
		"lastActivity":         project.LastActivity,
		"minutesSinceActivity": minutesSinceActivity,
	})
}

// UpgradeProject handles POST /api/admin/projects/{id}/upgrade
func (h *ProjectHandlers) UpgradeProject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	projectID, err := extractProjectID(r)
	if err != nil {
		_ = apierrors.WriteError(w, apierrors.BadRequest("invalid project ID", ""))
		return
	}

	var req struct {
		ImageTag string `json:"imageTag"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		_ = apierrors.WriteError(w, apierrors.BadRequest("invalid JSON", ""))
		return
	}

	// Validate semantic version format
	req.ImageTag = strings.TrimSpace(req.ImageTag)
	if req.ImageTag == "" {
		_ = apierrors.WriteError(w, apierrors.BadRequest("imageTag is required", ""))
		return
	}

	// Basic semantic version validation (v1.2.3, 1.2.3, or similar)
	if !isValidVersion(req.ImageTag) {
		_ = apierrors.WriteError(w, apierrors.BadRequest(
			"imageTag must be a valid semantic version (e.g., v1.2.3, 1.2.3)", ""))
		return
	}

	// Update image tag and trigger update
	updateReq := projects.UpdateProjectRequest{
		ImageTag: &req.ImageTag,
	}
	project, err := h.store.Update(ctx, projectID, updateReq)
	if err != nil {
		if errors.Is(err, projects.ErrProjectNotFound) {
			_ = apierrors.WriteError(w, apierrors.NotFound("project not found", ""))
			return
		}
		log.WithError(err).Error("admin/projects: failed to update project")
		_ = apierrors.WriteError(w, apierrors.InternalServerError("failed to update project", ""))
		return
	}

	// Trigger update
	if err := h.store.SetStatus(ctx, projectID, projects.StatusUpdating, "Upgrading to "+req.ImageTag); err != nil {
		log.WithError(err).Warn("admin/projects: failed to set updating status")
	}

	log.WithFields(log.Fields{
		"project_id":   project.ID.String(),
		"project_name": project.Name,
		"new_version":  req.ImageTag,
	}).Info("admin/projects: project upgrade initiated")

	_ = util.WriteJSON(w, http.StatusOK, map[string]string{
		"message": "Upgrade initiated",
		"version": req.ImageTag,
	})
}

// GetProjectSecrets handles GET /api/admin/projects/{id}/secrets
func (h *ProjectHandlers) GetProjectSecrets(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	projectID, err := extractProjectID(r)
	if err != nil {
		_ = apierrors.WriteError(w, apierrors.BadRequest("invalid project ID", ""))
		return
	}

	project, err := h.store.Get(ctx, projectID)
	if err != nil {
		if errors.Is(err, projects.ErrProjectNotFound) {
			_ = apierrors.WriteError(w, apierrors.NotFound("project not found", ""))
			return
		}
		log.WithError(err).Error("admin/projects: failed to get project")
		_ = apierrors.WriteError(w, apierrors.InternalServerError("failed to get project", ""))
		return
	}

	secrets, err := h.controller.GetProjectSecrets(ctx, project)
	if err != nil {
		log.WithError(err).Error("admin/projects: failed to get project secrets")
		_ = apierrors.WriteError(w, apierrors.InternalServerError("failed to get project secrets", ""))
		return
	}

	_ = util.WriteJSON(w, http.StatusOK, map[string]any{
		"secrets": secrets,
	})
}

// UpdateProjectSecrets handles PUT /api/admin/projects/{id}/secrets
func (h *ProjectHandlers) UpdateProjectSecrets(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	projectID, err := extractProjectID(r)
	if err != nil {
		_ = apierrors.WriteError(w, apierrors.BadRequest("invalid project ID", ""))
		return
	}

	var req struct {
		Secrets map[string]string `json:"secrets"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		_ = apierrors.WriteError(w, apierrors.BadRequest("invalid JSON", ""))
		return
	}

	project, err := h.store.Get(ctx, projectID)
	if err != nil {
		if errors.Is(err, projects.ErrProjectNotFound) {
			_ = apierrors.WriteError(w, apierrors.NotFound("project not found", ""))
			return
		}
		log.WithError(err).Error("admin/projects: failed to get project")
		_ = apierrors.WriteError(w, apierrors.InternalServerError("failed to get project", ""))
		return
	}

	if err := h.controller.UpdateProjectSecrets(ctx, project, req.Secrets); err != nil {
		log.WithError(err).Error("admin/projects: failed to update project secrets")
		_ = apierrors.WriteError(w, apierrors.InternalServerError("failed to update project secrets", ""))
		return
	}

	log.WithFields(log.Fields{
		"project_id":   project.ID.String(),
		"project_name": project.Name,
		"secret_count": len(req.Secrets),
	}).Info("admin/projects: project secrets updated")

	_ = util.WriteJSON(w, http.StatusOK, map[string]string{
		"message": "secrets updated, deployment restart triggered",
	})
}

// PauseProject handles POST /api/admin/projects/{id}/pause
// This pauses a project by scaling the deployment to 0 replicas.
func (h *ProjectHandlers) PauseProject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	projectID, err := extractProjectID(r)
	if err != nil {
		_ = apierrors.WriteError(w, apierrors.BadRequest("invalid project ID", ""))
		return
	}

	project, err := h.store.Get(ctx, projectID)
	if err != nil {
		if errors.Is(err, projects.ErrProjectNotFound) {
			_ = apierrors.WriteError(w, apierrors.NotFound("project not found", ""))
			return
		}
		log.WithError(err).Error("admin/projects: failed to get project for pause")
		_ = apierrors.WriteError(w, apierrors.InternalServerError("failed to pause project", ""))
		return
	}

	// Can only pause running projects
	if project.Status != projects.StatusRunning {
		_ = apierrors.WriteError(w, apierrors.BadRequest(
			"project must be running to pause (current status: "+string(project.Status)+")", ""))
		return
	}

	if project.Paused {
		_ = apierrors.WriteError(w, apierrors.BadRequest("project is already paused", ""))
		return
	}

	// Set paused flag in database
	if err := h.store.SetPaused(ctx, projectID, true); err != nil {
		log.WithError(err).Error("admin/projects: failed to set paused flag")
		_ = apierrors.WriteError(w, apierrors.InternalServerError("failed to pause project", ""))
		return
	}

	// Scale deployment to 0
	if err := h.controller.ScaleProject(ctx, project, 0); err != nil {
		// Rollback paused flag on failure
		_ = h.store.SetPaused(ctx, projectID, false)
		log.WithError(err).Error("admin/projects: failed to scale deployment to 0")
		_ = apierrors.WriteError(w, apierrors.InternalServerError("failed to pause project", ""))
		return
	}

	log.WithFields(log.Fields{
		"project_id":   project.ID.String(),
		"project_name": project.Name,
	}).Info("admin/projects: project paused")

	_ = util.WriteJSON(w, http.StatusOK, map[string]string{
		"message": "project paused, deployment scaled to 0",
	})
}

// UnpauseProject handles POST /api/admin/projects/{id}/unpause
// This unpauses a project by scaling the deployment back to 1 replica.
func (h *ProjectHandlers) UnpauseProject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	projectID, err := extractProjectID(r)
	if err != nil {
		_ = apierrors.WriteError(w, apierrors.BadRequest("invalid project ID", ""))
		return
	}

	project, err := h.store.Get(ctx, projectID)
	if err != nil {
		if errors.Is(err, projects.ErrProjectNotFound) {
			_ = apierrors.WriteError(w, apierrors.NotFound("project not found", ""))
			return
		}
		log.WithError(err).Error("admin/projects: failed to get project for unpause")
		_ = apierrors.WriteError(w, apierrors.InternalServerError("failed to unpause project", ""))
		return
	}

	if !project.Paused {
		_ = apierrors.WriteError(w, apierrors.BadRequest("project is not paused", ""))
		return
	}

	// Set paused flag to false in database
	if err := h.store.SetPaused(ctx, projectID, false); err != nil {
		log.WithError(err).Error("admin/projects: failed to clear paused flag")
		_ = apierrors.WriteError(w, apierrors.InternalServerError("failed to unpause project", ""))
		return
	}

	// Scale deployment back to 1
	if err := h.controller.ScaleProject(ctx, project, 1); err != nil {
		// Rollback paused flag on failure
		_ = h.store.SetPaused(ctx, projectID, true)
		log.WithError(err).Error("admin/projects: failed to scale deployment to 1")
		_ = apierrors.WriteError(w, apierrors.InternalServerError("failed to unpause project", ""))
		return
	}

	log.WithFields(log.Fields{
		"project_id":   project.ID.String(),
		"project_name": project.Name,
	}).Info("admin/projects: project unpaused")

	_ = util.WriteJSON(w, http.StatusOK, map[string]string{
		"message": "project unpaused, deployment scaled to 1",
	})
}

// GetGatewayVersion handles GET /api/admin/gateway-version
// Returns the gateway's recommended image version for quick project upgrades.
func (h *ProjectHandlers) GetGatewayVersion(w http.ResponseWriter, r *http.Request) {
	_ = util.WriteJSON(w, http.StatusOK, map[string]string{
		"recommendedVersion": h.recommendedImageTag,
	})
}

// RegisterRoutes registers all admin project routes on the provided mux.
func (h *ProjectHandlers) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/admin/gateway-version", h.GetGatewayVersion)
	mux.HandleFunc("GET /api/admin/projects", h.ListProjects)
	mux.HandleFunc("POST /api/admin/projects", h.CreateProject)
	mux.HandleFunc("GET /api/admin/projects/{id}", h.GetProject)
	mux.HandleFunc("PUT /api/admin/projects/{id}", h.UpdateProject)
	mux.HandleFunc("DELETE /api/admin/projects/{id}", h.DeleteProject)
	mux.HandleFunc("POST /api/admin/projects/{id}/restart", h.RestartProject)
	mux.HandleFunc("POST /api/admin/projects/{id}/resync", h.ResyncProject)
	mux.HandleFunc("POST /api/admin/projects/{id}/pause", h.PauseProject)
	mux.HandleFunc("POST /api/admin/projects/{id}/unpause", h.UnpauseProject)
	mux.HandleFunc("GET /api/admin/projects/{id}/status", h.GetProjectStatus)
	mux.HandleFunc("GET /api/admin/projects/{id}/upgrade-info", h.GetUpgradeInfo)
	mux.HandleFunc("POST /api/admin/projects/{id}/upgrade", h.UpgradeProject)
	mux.HandleFunc("GET /api/admin/projects/{id}/secrets", h.GetProjectSecrets)
	mux.HandleFunc("PUT /api/admin/projects/{id}/secrets", h.UpdateProjectSecrets)
}

// extractProjectID extracts the project ID from the URL path.
func extractProjectID(r *http.Request) (uuid.UUID, error) {
	idStr := r.PathValue("id")
	if idStr == "" {
		return uuid.Nil, errors.New("missing project ID")
	}
	return uuid.Parse(idStr)
}

// versionPattern matches strict semantic versions: v1.2.3 or 1.2.3
// No pre-release suffixes allowed (e.g., -rc, -beta, -alpha) per project standards
var versionPattern = regexp.MustCompile(`^v?\d+\.\d+\.\d+$`)

// isValidVersion checks if a string is a valid semantic version.
func isValidVersion(version string) bool {
	return versionPattern.MatchString(version)
}

// guiResolutionPattern matches resolution format: WIDTHxHEIGHTxDEPTH (e.g., 1920x1080x24)
var guiResolutionPattern = regexp.MustCompile(`^\d{3,5}x\d{3,5}x\d{1,2}$`)

// isValidGUIResolution checks if a string is a valid GUI resolution.
func isValidGUIResolution(resolution string) bool {
	if resolution == "" {
		return true // Empty is OK (will use default)
	}
	return guiResolutionPattern.MatchString(resolution)
}

// isValidVNCPort checks if a port is a valid VNC port (5900-5999).
func isValidVNCPort(port int) bool {
	if port == 0 {
		return true // Zero means use default
	}
	return port >= 5900 && port <= 5999
}
