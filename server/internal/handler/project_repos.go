package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// projectRepoFromURL resolves the URL parameter `:id` to a project the caller
// owns. Returns the loaded row + workspace ID string for downstream use, or
// writes the appropriate error and returns ok=false. The lookup goes through
// GetProjectInWorkspace so a project UUID from a different workspace can't
// be used to bind / unbind repos through someone else's session.
func (h *Handler) projectRepoFromURL(w http.ResponseWriter, r *http.Request) (db.Project, string, bool) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)

	idUUID, ok := parseUUIDOrBadRequest(w, id, "project id")
	if !ok {
		return db.Project{}, "", false
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return db.Project{}, "", false
	}
	project, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
		ID: idUUID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return db.Project{}, "", false
	}
	return project, workspaceID, true
}

// ListProjectRepos returns the repos bound at project scope. The response
// shape mirrors the workspace `repos` field (`[{url, description}]`) so the
// frontend can reuse the same RepoListEditor component without translation.
func (h *Handler) ListProjectRepos(w http.ResponseWriter, r *http.Request) {
	project, _, ok := h.projectRepoFromURL(w, r)
	if !ok {
		return
	}

	repos, err := h.loadRepoDataByScope(r.Context(), repoScopeProject, project.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load project repos")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"repos": repos})
}

// CreateProjectRepoRequest is the body for `POST /api/projects/:id/repos`. The
// shape matches one entry of the workspace `repos` field; it intentionally
// stays per-binding (no list form) because the project settings UI adds rows
// one at a time and per-row error reporting is cleaner than rejecting a whole
// batch when a single URL is malformed.
type CreateProjectRepoRequest struct {
	URL         string `json:"url"`
	Description string `json:"description"`
}

// CreateProjectRepo binds a single repo to the project. The handler is
// idempotent: posting the same URL twice updates the description (via the
// CreateRepoBinding upsert) rather than producing a duplicate row. That
// matches the workspace settings flow, where saving the same list twice is a
// no-op.
func (h *Handler) CreateProjectRepo(w http.ResponseWriter, r *http.Request) {
	project, workspaceID, ok := h.projectRepoFromURL(w, r)
	if !ok {
		return
	}

	var req CreateProjectRepoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.URL) == "" {
		writeError(w, http.StatusBadRequest, "url is required")
		return
	}

	stored, err := h.addRepoBindingToScope(r.Context(), repoScopeProject, project.ID, RepoData{
		URL:         req.URL,
		Description: req.Description,
	})
	if err != nil {
		if errors.Is(err, errEmptyRepoURL) {
			writeError(w, http.StatusBadRequest, "url is required")
			return
		}
		slog.Warn("create project repo failed", "error", err, "project_id", uuidToString(project.ID))
		writeError(w, http.StatusInternalServerError, "failed to bind repo to project")
		return
	}

	repos, err := h.loadRepoDataByScope(r.Context(), repoScopeProject, project.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load project repos")
		return
	}

	h.publishProjectReposUpdated(r, workspaceID, uuidToString(project.ID), repos)
	writeJSON(w, http.StatusCreated, map[string]any{"repo": stored, "repos": repos})
}

// DeleteProjectRepo removes one binding from the project scope. The repo is
// identified by either a UUID on the path (`/repos/{repoId}`) or a `?url=`
// query string. URL-on-path is avoided because percent-decoded slashes inside
// a git URL collide with chi's segment separator.
func (h *Handler) DeleteProjectRepo(w http.ResponseWriter, r *http.Request) {
	project, workspaceID, ok := h.projectRepoFromURL(w, r)
	if !ok {
		return
	}

	repoIDOrURL := strings.TrimSpace(chi.URLParam(r, "repoId"))
	if repoIDOrURL == "" {
		repoIDOrURL = strings.TrimSpace(r.URL.Query().Get("url"))
	}
	if repoIDOrURL == "" {
		writeError(w, http.StatusBadRequest, "repo id or url is required")
		return
	}

	if err := h.removeRepoBindingFromScope(r.Context(), repoScopeProject, project.ID, repoIDOrURL); err != nil {
		if errors.Is(err, errRepoBindingNotFound) {
			writeError(w, http.StatusNotFound, "repo binding not found")
			return
		}
		slog.Warn("delete project repo failed", "error", err, "project_id", uuidToString(project.ID))
		writeError(w, http.StatusInternalServerError, "failed to unbind repo from project")
		return
	}

	repos, err := h.loadRepoDataByScope(r.Context(), repoScopeProject, project.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load project repos")
		return
	}

	h.publishProjectReposUpdated(r, workspaceID, uuidToString(project.ID), repos)
	w.WriteHeader(http.StatusNoContent)
}

// publishProjectReposUpdated emits a project_repos_updated WS event so any
// open settings page invalidates its useProjectRepos cache. Kept in one place
// because both create and delete need to fire it and forgetting one causes
// silent UI drift.
func (h *Handler) publishProjectReposUpdated(r *http.Request, workspaceID, projectID string, repos []RepoData) {
	h.publish(protocol.EventProjectReposUpdated, workspaceID, "member", requestUserID(r), map[string]any{
		"project_id": projectID,
		"repos":      repos,
	})
}
