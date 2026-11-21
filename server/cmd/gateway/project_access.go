package main

import (
	"errors"
	"net/http"

	log "github.com/sirupsen/logrus"

	handlers_auth "github.com/supporttools/KubeTTY/server/internal/handlers/auth"
	"github.com/supporttools/KubeTTY/server/internal/projects"
	apierrors "github.com/supporttools/KubeTTY/server/internal/shared/errors"
)

// canUseProject reports whether the request's user may open or attach
// terminals on projectID. repo is the project's GitHub repo, if known.
func (s *server) canUseProject(r *http.Request, projectID, repo string) (bool, error) {
	if !s.authEnabled() {
		return true, nil
	}
	var check handlers_auth.RepoCheck
	if s.githubAuthz != nil {
		check = s.githubAuthz.CanWrite
	}
	return handlers_auth.CanUseProject(r.Context(), handlers_auth.UserFromContext(r.Context()), projectID, repo, check)
}

func (s *server) projectRepo(r *http.Request, projectID string) (string, error) {
	if s.projectStore == nil {
		return "", nil
	}
	p, err := s.projectStore.GetByName(r.Context(), projectID)
	if errors.Is(err, projects.ErrProjectNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return p.GitHubRepo, nil
}

// requireProjectAccess writes 403 or 503 and returns false when the user may
// not use projectID.
func (s *server) requireProjectAccess(w http.ResponseWriter, r *http.Request, projectID string) bool {
	if !s.authEnabled() {
		return true
	}
	repo, err := s.projectRepo(r, projectID)
	if err == nil {
		var ok bool
		if ok, err = s.canUseProject(r, projectID, repo); err == nil && ok {
			return true
		}
		if err == nil {
			log.WithFields(log.Fields{"project": projectID, "user": userName(r)}).Warn("gateway/main: project access refused")
			apierrors.WriteError(w, apierrors.Forbidden("you do not have access to this project", "access needs write on the project's GitHub repository"))
			return false
		}
	}
	log.WithError(err).WithField("project", projectID).Warn("gateway/main: project access check failed")
	apierrors.WriteError(w, apierrors.ServiceUnavailable("cannot check project access right now", ""))
	return false
}

func userName(r *http.Request) string {
	if u := handlers_auth.UserFromContext(r.Context()); u != nil {
		return u.Username
	}
	return ""
}
