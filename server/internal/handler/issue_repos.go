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

// issueRepoFromURL resolves the URL parameter `:id` to an issue the caller is
// allowed to read. Returns the loaded row + its workspace ID (as a string) for
// downstream use, or writes the appropriate error and returns ok=false. The
// lookup goes through `loadIssueForUser`, which already accepts both `MUL-123`
// identifier form and a UUID.
//
// Importantly, the workspace ID returned here comes from the resolved issue
// row, NOT from the X-Workspace-Slug header — a member of workspace A must
// not be able to bind repos to an issue in workspace B by spoofing the
// header. `loadIssueForUser` filters its lookup by the resolved workspace,
// so by the time we observe `issue.WorkspaceID` it has already been
// authenticated against the caller's session.
func (h *Handler) issueRepoFromURL(w http.ResponseWriter, r *http.Request) (db.Issue, string, bool) {
	id := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, id)
	if !ok {
		return db.Issue{}, "", false
	}
	return issue, uuidToString(issue.WorkspaceID), true
}

// requireIssueRepoWriter loads the issue + asserts the caller is an
// owner / admin of the issue's workspace. Mirrors `requireProjectRepoWriter`
// from Step 2: the router-level role middleware enforces the same gate, but
// re-checking inside the handler keeps the contract intact when handlers are
// invoked directly (tests, future router refactors).
//
// Anchoring the role to the *issue's* workspace (rather than whatever slug
// the caller's session header advertises) closes the same cross-workspace
// hole Step 2 closed for project-scope repos: the issue lookup inside
// `issueRepoFromURL` already verifies the issue lives in the workspace the
// session is authenticated for, so by the time we re-check role we're
// reading the right member row.
func (h *Handler) requireIssueRepoWriter(w http.ResponseWriter, r *http.Request) (db.Issue, string, bool) {
	issue, workspaceID, ok := h.issueRepoFromURL(w, r)
	if !ok {
		return db.Issue{}, "", false
	}
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return db.Issue{}, "", false
	}
	if !roleAllowed(member.Role, "owner", "admin") {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return db.Issue{}, "", false
	}
	return issue, workspaceID, true
}

// ListIssueRepos returns the repos bound at issue scope. The response shape
// matches `ListProjectRepos` so the frontend can reuse the RepoListEditor
// without translation. Reads stay open to any workspace member; the role
// gate only fires on writes.
func (h *Handler) ListIssueRepos(w http.ResponseWriter, r *http.Request) {
	issue, _, ok := h.issueRepoFromURL(w, r)
	if !ok {
		return
	}

	repos, err := h.loadRepoDataByScope(r.Context(), repoScopeIssue, issue.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load issue repos")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"repos": repos})
}

// CreateIssueRepoRequest is the body for `POST /api/issues/:id/repos`. Same
// shape as the project variant: per-binding (no list form) so the issue
// properties UI can add rows one at a time and report per-row errors.
type CreateIssueRepoRequest struct {
	URL         string `json:"url"`
	Description string `json:"description"`
}

// CreateIssueRepo binds a single repo to the issue. Idempotent — re-posting
// the same URL with a new description updates in place via the
// CreateRepoBinding upsert rather than producing a duplicate row.
func (h *Handler) CreateIssueRepo(w http.ResponseWriter, r *http.Request) {
	issue, workspaceID, ok := h.requireIssueRepoWriter(w, r)
	if !ok {
		return
	}

	var req CreateIssueRepoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.URL) == "" {
		writeError(w, http.StatusBadRequest, "url is required")
		return
	}

	stored, err := h.addRepoBindingToScope(r.Context(), repoScopeIssue, issue.ID, RepoData{
		URL:         req.URL,
		Description: req.Description,
	})
	if err != nil {
		if errors.Is(err, errEmptyRepoURL) {
			writeError(w, http.StatusBadRequest, "url is required")
			return
		}
		slog.Warn("create issue repo failed", "error", err, "issue_id", uuidToString(issue.ID))
		writeError(w, http.StatusInternalServerError, "failed to bind repo to issue")
		return
	}

	repos, err := h.loadRepoDataByScope(r.Context(), repoScopeIssue, issue.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load issue repos")
		return
	}

	h.publishIssueReposUpdated(r, workspaceID, uuidToString(issue.ID), repos)
	writeJSON(w, http.StatusCreated, map[string]any{"repo": stored, "repos": repos})
}

// DeleteIssueRepo drops one binding from the issue scope. Same dual-input
// shape as the project endpoint: UUID on the path (`/repos/{repoId}`) or git
// URL on the `?url=` query string. Baking a git URL into a path segment is
// avoided because percent-decoded `/` collides with chi's separator.
func (h *Handler) DeleteIssueRepo(w http.ResponseWriter, r *http.Request) {
	issue, workspaceID, ok := h.requireIssueRepoWriter(w, r)
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

	if err := h.removeRepoBindingFromScope(r.Context(), repoScopeIssue, issue.ID, repoIDOrURL); err != nil {
		if errors.Is(err, errRepoBindingNotFound) {
			writeError(w, http.StatusNotFound, "repo binding not found")
			return
		}
		slog.Warn("delete issue repo failed", "error", err, "issue_id", uuidToString(issue.ID))
		writeError(w, http.StatusInternalServerError, "failed to unbind repo from issue")
		return
	}

	repos, err := h.loadRepoDataByScope(r.Context(), repoScopeIssue, issue.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load issue repos")
		return
	}

	h.publishIssueReposUpdated(r, workspaceID, uuidToString(issue.ID), repos)
	w.WriteHeader(http.StatusNoContent)
}

// publishIssueReposUpdated emits an issue_repos:updated WS event so any open
// issue properties sidebar invalidates its useIssueRepos cache. Centralized
// here because both create and delete need to fire it and forgetting one
// causes silent UI drift.
func (h *Handler) publishIssueReposUpdated(r *http.Request, workspaceID, issueID string, repos []RepoData) {
	h.publish(protocol.EventIssueReposUpdated, workspaceID, "member", requestUserID(r), map[string]any{
		"issue_id": issueID,
		"repos":    repos,
	})
}
