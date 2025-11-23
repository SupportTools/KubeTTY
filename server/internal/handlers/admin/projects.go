package admin

import (
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
	apierrors "github.com/supporttools/KubeTTY/server/internal/shared/errors"
	"github.com/supporttools/KubeTTY/server/internal/shared/util"
)

// ProjectHandlers provides HTTP handlers for project management.
type ProjectHandlers struct {
	store            projects.Store
	controller       *controller.Controller
	onProjectDeleted func(projectName string) // Callback when project is deleted
}

// NewProjectHandlers creates a new ProjectHandlers instance.
func NewProjectHandlers(store projects.Store, ctrl *controller.Controller) *ProjectHandlers {
	return &ProjectHandlers{
		store:      store,
		controller: ctrl,
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
	if req.ImageTag != nil || req.CPULimit != nil || req.MemoryLimit != nil {
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

	// Calculate minutes since last activity
	var minutesSinceActivity *int
	if project.LastActivity != nil {
		minutes := int(time.Since(*project.LastActivity).Minutes())
		minutesSinceActivity = &minutes
	}

	_ = util.WriteJSON(w, http.StatusOK, map[string]any{
		"currentVersion":       project.ImageTag,
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

// RegisterRoutes registers all admin project routes on the provided mux.
func (h *ProjectHandlers) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/admin/projects", h.ListProjects)
	mux.HandleFunc("POST /api/admin/projects", h.CreateProject)
	mux.HandleFunc("GET /api/admin/projects/{id}", h.GetProject)
	mux.HandleFunc("PUT /api/admin/projects/{id}", h.UpdateProject)
	mux.HandleFunc("DELETE /api/admin/projects/{id}", h.DeleteProject)
	mux.HandleFunc("POST /api/admin/projects/{id}/restart", h.RestartProject)
	mux.HandleFunc("GET /api/admin/projects/{id}/status", h.GetProjectStatus)
	mux.HandleFunc("GET /api/admin/projects/{id}/upgrade-info", h.GetUpgradeInfo)
	mux.HandleFunc("POST /api/admin/projects/{id}/upgrade", h.UpgradeProject)
}

// extractProjectID extracts the project ID from the URL path.
func extractProjectID(r *http.Request) (uuid.UUID, error) {
	idStr := r.PathValue("id")
	if idStr == "" {
		return uuid.Nil, errors.New("missing project ID")
	}
	return uuid.Parse(idStr)
}

// versionPattern matches semantic versions like v1.2.3, 1.2.3, v0.7.2-rc1, etc.
var versionPattern = regexp.MustCompile(`^v?\d+\.\d+\.\d+(-[a-zA-Z0-9.-]+)?$`)

// isValidVersion checks if a string is a valid semantic version.
func isValidVersion(version string) bool {
	return versionPattern.MatchString(version)
}
