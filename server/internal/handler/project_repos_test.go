package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/go-chi/chi/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// createProjectForRepoTest creates a project owned by the test workspace
// member and returns its ID. The cleanup hook drops the project + any repo
// bindings the test happened to leave behind so the table doesn't accumulate
// across runs (DeleteProject also runs the new orphan-repo GC, so this cleanup
// path doubles as a smoke test for the cascade hook).
func createProjectForRepoTest(t *testing.T, title string) string {
	t.Helper()

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title": title,
	})
	testHandler.CreateProject(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateProject: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var project ProjectResponse
	if err := json.NewDecoder(w.Body).Decode(&project); err != nil {
		t.Fatalf("decode project: %v", err)
	}

	t.Cleanup(func() {
		req := newRequest("DELETE", "/api/projects/"+project.ID, nil)
		req = withURLParam(req, "id", project.ID)
		testHandler.DeleteProject(httptest.NewRecorder(), req)
	})

	return project.ID
}

// TestProjectRepos_CRUDRoundTrip exercises the new POST/DELETE/GET endpoints
// end to end against the test DB. Adds two URLs, asserts the GET reflects
// them, deletes one by URL (the typical CLI flow) and one by UUID (the
// frontend flow), and finally asserts the catalog cleanup ran.
func TestProjectRepos_CRUDRoundTrip(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	projectID := createProjectForRepoTest(t, "Repo CRUD project")

	// POST first repo.
	post := func(body map[string]any, expect int) []byte {
		t.Helper()
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/projects/"+projectID+"/repos", body)
		req = withURLParam(req, "id", projectID)
		testHandler.CreateProjectRepo(w, req)
		if w.Code != expect {
			t.Fatalf("CreateProjectRepo: want %d, got %d: %s", expect, w.Code, w.Body.String())
		}
		return w.Body.Bytes()
	}
	post(map[string]any{"url": "git@example.com:team/api.git", "description": "Backend API"}, http.StatusCreated)
	post(map[string]any{"url": "git@example.com:team/web.git", "description": "Web frontend"}, http.StatusCreated)

	// Re-posting the same URL with a new description must update in place
	// (idempotent upsert), not produce a duplicate row.
	post(map[string]any{"url": "git@example.com:team/api.git", "description": "API server"}, http.StatusCreated)

	// GET — exact set after dedup, with the latest description for the
	// re-posted URL.
	{
		w := httptest.NewRecorder()
		req := newRequest("GET", "/api/projects/"+projectID+"/repos", nil)
		req = withURLParam(req, "id", projectID)
		testHandler.ListProjectRepos(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("ListProjectRepos: want 200, got %d: %s", w.Code, w.Body.String())
		}
		var got struct {
			Repos []RepoData `json:"repos"`
		}
		json.NewDecoder(w.Body).Decode(&got)
		if len(got.Repos) != 2 {
			t.Fatalf("expected 2 repos after dedup, got %d: %+v", len(got.Repos), got.Repos)
		}
		byURL := map[string]string{}
		for _, r := range got.Repos {
			byURL[r.URL] = r.Description
		}
		if byURL["git@example.com:team/api.git"] != "API server" {
			t.Fatalf("description not updated by re-post: %+v", byURL)
		}
	}

	// DELETE by URL (CLI path).
	{
		w := httptest.NewRecorder()
		path := "/api/projects/" + projectID + "/repos?url=" + url.QueryEscape("git@example.com:team/api.git")
		req := newRequest("DELETE", path, nil)
		req = withURLParam(req, "id", projectID)
		testHandler.DeleteProjectRepo(w, req)
		if w.Code != http.StatusNoContent {
			t.Fatalf("DeleteProjectRepo by URL: want 204, got %d: %s", w.Code, w.Body.String())
		}
	}

	// One left.
	repos, err := testHandler.loadRepoDataByScope(ctx, repoScopeProject, parseUUID(projectID))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(repos) != 1 || repos[0].URL != "git@example.com:team/web.git" {
		t.Fatalf("after URL-delete: %+v", repos)
	}

	// DELETE by UUID (frontend path) — fetch the repo's catalog ID first.
	repo, err := testHandler.Queries.GetRepoByURL(ctx, "git@example.com:team/web.git")
	if err != nil {
		t.Fatalf("GetRepoByURL: %v", err)
	}
	{
		w := httptest.NewRecorder()
		req := newRequest("DELETE", "/api/projects/"+projectID+"/repos/"+uuidToString(repo.ID), nil)
		req = withURLParams(req, "id", projectID, "repoId", uuidToString(repo.ID))
		testHandler.DeleteProjectRepo(w, req)
		if w.Code != http.StatusNoContent {
			t.Fatalf("DeleteProjectRepo by UUID: want 204, got %d: %s", w.Code, w.Body.String())
		}
	}

	// Catalog cleanup: with both bindings dropped and no other scope
	// referencing those URLs, the repo rows should be GC'd.
	if _, err := testHandler.Queries.GetRepoByURL(ctx, "git@example.com:team/web.git"); err == nil {
		t.Fatalf("expected repo catalog row to be GC'd after last binding dropped")
	}
}

// TestProjectRepos_DeleteUnknownReturns404 catches the difference between
// "I'm asking to remove a binding that's not there" and "the repo doesn't
// exist at all". Both are 404 today; the test pins that contract so the
// frontend can rely on it for the optimistic-revert path.
func TestProjectRepos_DeleteUnknownReturns404(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	projectID := createProjectForRepoTest(t, "404 project")

	w := httptest.NewRecorder()
	path := "/api/projects/" + projectID + "/repos?url=" + url.QueryEscape("git@example.com:team/never-bound.git")
	req := newRequest("DELETE", path, nil)
	req = withURLParam(req, "id", projectID)
	testHandler.DeleteProjectRepo(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown URL, got %d: %s", w.Code, w.Body.String())
	}
}

// TestDeleteProject_OrphansBindings asserts that deleting a project drops its
// repo bindings and GCs the catalog entry when no other scope references it.
// The Step-1 workspace path covers the same contract; this makes sure Step 2
// extended the cleanup hook the same way.
func TestDeleteProject_OrphansBindings(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Cleanup project",
	})
	testHandler.CreateProject(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateProject: %d %s", w.Code, w.Body.String())
	}
	var project ProjectResponse
	json.NewDecoder(w.Body).Decode(&project)

	const url1 = "git@example.com:team/cleanup.git"
	if err := testHandler.setRepoBindingsForScope(ctx, repoScopeProject, parseUUID(project.ID), []RepoData{
		{URL: url1, Description: "alone in this scope"},
	}); err != nil {
		t.Fatalf("seed binding: %v", err)
	}

	// Sanity — repo exists.
	if _, err := testHandler.Queries.GetRepoByURL(ctx, url1); err != nil {
		t.Fatalf("GetRepoByURL pre-delete: %v", err)
	}

	// Delete project. The handler must run DeleteRepoBindingsForScope +
	// DeleteOrphanRepos.
	w = httptest.NewRecorder()
	req = newRequest("DELETE", "/api/projects/"+project.ID, nil)
	req = withURLParam(req, "id", project.ID)
	testHandler.DeleteProject(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteProject: %d %s", w.Code, w.Body.String())
	}

	// Bindings + catalog row should both be gone.
	bindings, err := testHandler.Queries.ListReposByScope(ctx, db.ListReposByScopeParams{
		ScopeType: repoScopeProject,
		ScopeID:   parseUUID(project.ID),
	})
	if err != nil {
		t.Fatalf("ListReposByScope: %v", err)
	}
	if len(bindings) != 0 {
		t.Fatalf("expected project bindings to be cleared, got %d", len(bindings))
	}
	if _, err := testHandler.Queries.GetRepoByURL(ctx, url1); err == nil {
		t.Fatalf("expected orphan repo row to be GC'd after project delete")
	}
}

// TestProjectRepos_CardinalityMatrix exercises 0/1/N project bindings
// observed via the operational-repos resolver. The test uses
// loadOperationalReposForIssue (the function task-claim uses) so the
// behaviour shift Step 2 introduces — issue-on-project sees workspace ∪
// project — is covered by an issue-flavored test, not just the lower-level
// helper.
func TestProjectRepos_CardinalityMatrix(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	wsUUID := parseUUID(testWorkspaceID)
	projectID := createProjectForRepoTest(t, "Cardinality project")

	t.Cleanup(func() {
		testHandler.Queries.DeleteRepoBindingsForScope(ctx, db.DeleteRepoBindingsForScopeParams{
			ScopeType: repoScopeWorkspace,
			ScopeID:   wsUUID,
		})
		testHandler.Queries.DeleteOrphanRepos(ctx)
	})

	// Stub issue rows aren't necessary — loadOperationalReposForIssue only
	// reads ProjectID + WorkspaceID from the input. Build a synthetic
	// db.Issue with those two fields populated and call directly.
	mkIssue := func(includeProject bool) db.Issue {
		out := db.Issue{WorkspaceID: wsUUID}
		if includeProject {
			out.ProjectID = parseUUID(projectID)
			out.ProjectID.Valid = true
		}
		return out
	}

	// 0-repo case (no bindings at any scope) — issue lives in this project
	// but neither workspace nor project has bindings.
	if got, err := testHandler.loadOperationalReposForIssue(ctx, mkIssue(true)); err != nil {
		t.Fatalf("0-repo: %v", err)
	} else if len(got) != 0 {
		t.Fatalf("0-repo: expected empty, got %+v", got)
	}

	// 1-repo case — one binding at project scope, none at workspace.
	const repoOne = "git@example.com:team/single.git"
	if err := testHandler.setRepoBindingsForScope(ctx, repoScopeProject, parseUUID(projectID), []RepoData{
		{URL: repoOne, Description: "the only one"},
	}); err != nil {
		t.Fatalf("seed 1-repo project binding: %v", err)
	}
	if got, err := testHandler.loadOperationalReposForIssue(ctx, mkIssue(true)); err != nil {
		t.Fatalf("1-repo: %v", err)
	} else if len(got) != 1 || got[0].URL != repoOne {
		t.Fatalf("1-repo: %+v", got)
	}

	// N-repo case — workspace adds a couple of repos that union with the
	// project's. Order is by URL.
	if err := testHandler.setRepoBindingsForScope(ctx, repoScopeWorkspace, wsUUID, []RepoData{
		{URL: "git@example.com:team/api.git", Description: "ws api"},
		{URL: "git@example.com:team/web.git", Description: "ws web"},
	}); err != nil {
		t.Fatalf("seed workspace bindings: %v", err)
	}
	if got, err := testHandler.loadOperationalReposForIssue(ctx, mkIssue(true)); err != nil {
		t.Fatalf("N-repo: %v", err)
	} else if len(got) != 3 {
		t.Fatalf("N-repo: expected 3 (workspace 2 + project 1), got %d: %+v", len(got), got)
	}

	// Issue without a project — only the workspace bindings should appear.
	if got, err := testHandler.loadOperationalReposForIssue(ctx, mkIssue(false)); err != nil {
		t.Fatalf("no-project: %v", err)
	} else if len(got) != 2 {
		t.Fatalf("no-project: expected 2 (workspace only), got %d: %+v", len(got), got)
	}
}

// TestClaimTask_IssueOnProject_IncludesProjectRepos is the integration-shaped
// equivalent of the e2e flow the parent issue (MUL-14) prescribes for Step 2:
//
//	create project → bind repo via API → claim a task assigned to an issue
//	in that project → assert daemon response contains the project repo.
//
// It exercises the actual ClaimTaskByRuntime path so a regression in the
// claim-side wiring (forgetting to call loadOperationalReposForIssue, e.g.)
// would surface here even when the helper-level tests stay green. The
// scenario binds one repo at workspace scope and a different one at project
// scope; the claim must surface both, deduped, with the project description
// preserved.
func TestClaimTask_IssueOnProject_IncludesProjectRepos(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	wsUUID := parseUUID(testWorkspaceID)
	projectID := createProjectForRepoTest(t, "Claim integration project")

	t.Cleanup(func() {
		testHandler.Queries.DeleteRepoBindingsForScope(ctx, db.DeleteRepoBindingsForScopeParams{
			ScopeType: repoScopeWorkspace,
			ScopeID:   wsUUID,
		})
		testHandler.Queries.DeleteOrphanRepos(ctx)
	})

	wsRepo := "git@example.com:team/claim-ws.git"
	projRepo := "git@example.com:team/claim-proj.git"
	if err := testHandler.setRepoBindingsForScope(ctx, repoScopeWorkspace, wsUUID, []RepoData{
		{URL: wsRepo, Description: "workspace-bound"},
	}); err != nil {
		t.Fatalf("seed workspace binding: %v", err)
	}
	if err := testHandler.setRepoBindingsForScope(ctx, repoScopeProject, parseUUID(projectID), []RepoData{
		{URL: projRepo, Description: "project-bound"},
	}); err != nil {
		t.Fatalf("seed project binding: %v", err)
	}

	var agentID, runtimeID string
	if err := testPool.QueryRow(ctx, `
		SELECT a.id, a.runtime_id FROM agent a WHERE a.workspace_id = $1 LIMIT 1
	`, testWorkspaceID).Scan(&agentID, &runtimeID); err != nil {
		t.Fatalf("setup: get agent: %v", err)
	}

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, creator_type, creator_id, project_id)
		VALUES ($1, 'Step 2 claim integration', 'todo', 'member', $2, $3)
		RETURNING id
	`, testWorkspaceID, testUserID, projectID).Scan(&issueID); err != nil {
		t.Fatalf("setup: create issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority)
		VALUES ($1, $2, $3, 'queued', 0)
		RETURNING id
	`, agentID, runtimeID, issueID).Scan(&taskID); err != nil {
		t.Fatalf("setup: create task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})

	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/claim", nil,
		testWorkspaceID, "claim-int-test")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("runtimeId", runtimeID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	testHandler.ClaimTaskByRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ClaimTaskByRuntime: want 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Task *struct {
			WorkspaceID string     `json:"workspace_id"`
			Repos       []RepoData `json:"repos"`
		} `json:"task"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Task == nil {
		t.Fatal("expected a task in response")
	}
	if resp.Task.WorkspaceID != testWorkspaceID {
		t.Fatalf("workspace_id mismatch: got %q, want %q", resp.Task.WorkspaceID, testWorkspaceID)
	}

	urls := map[string]string{}
	for _, r := range resp.Task.Repos {
		urls[r.URL] = r.Description
	}
	if urls[wsRepo] != "workspace-bound" {
		t.Fatalf("workspace repo missing or description wrong: %+v", urls)
	}
	if urls[projRepo] != "project-bound" {
		t.Fatalf("project repo missing or description wrong: %+v", urls)
	}
	if len(urls) != 2 {
		t.Fatalf("expected exactly 2 repos in claim response, got %d: %+v", len(urls), resp.Task.Repos)
	}
}
