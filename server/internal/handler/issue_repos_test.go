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

// createIssueForRepoTest creates an issue owned by the test workspace member
// and returns its ID. The cleanup hook fires DeleteIssue, which exercises the
// new issue-scope binding cascade hook end-to-end (so leftover state from a
// failing test still hits the same cleanup path the production code does).
func createIssueForRepoTest(t *testing.T, title string) string {
	t.Helper()

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":    title,
		"status":   "todo",
		"priority": "medium",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var issue IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&issue); err != nil {
		t.Fatalf("decode issue: %v", err)
	}

	t.Cleanup(func() {
		req := newRequest("DELETE", "/api/issues/"+issue.ID, nil)
		req = withURLParam(req, "id", issue.ID)
		testHandler.DeleteIssue(httptest.NewRecorder(), req)
	})

	return issue.ID
}

// TestIssueRepos_CRUDRoundTrip walks the new POST/DELETE/GET endpoints end
// to end. Mirrors TestProjectRepos_CRUDRoundTrip — re-posting the same URL
// must update the description in place rather than producing a duplicate
// row, DELETE-by-URL is the CLI flow, and DELETE-by-UUID is what the
// frontend hits after listing the bindings.
func TestIssueRepos_CRUDRoundTrip(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	issueID := createIssueForRepoTest(t, "Repo CRUD issue")

	post := func(body map[string]any, expect int) []byte {
		t.Helper()
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/issues/"+issueID+"/repos", body)
		req = withURLParam(req, "id", issueID)
		testHandler.CreateIssueRepo(w, req)
		if w.Code != expect {
			t.Fatalf("CreateIssueRepo: want %d, got %d: %s", expect, w.Code, w.Body.String())
		}
		return w.Body.Bytes()
	}
	post(map[string]any{"url": "git@example.com:team/api.git", "description": "Backend API"}, http.StatusCreated)
	post(map[string]any{"url": "git@example.com:team/web.git", "description": "Web frontend"}, http.StatusCreated)
	post(map[string]any{"url": "git@example.com:team/api.git", "description": "API server"}, http.StatusCreated)

	{
		w := httptest.NewRecorder()
		req := newRequest("GET", "/api/issues/"+issueID+"/repos", nil)
		req = withURLParam(req, "id", issueID)
		testHandler.ListIssueRepos(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("ListIssueRepos: want 200, got %d: %s", w.Code, w.Body.String())
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
		path := "/api/issues/" + issueID + "/repos?url=" + url.QueryEscape("git@example.com:team/api.git")
		req := newRequest("DELETE", path, nil)
		req = withURLParam(req, "id", issueID)
		testHandler.DeleteIssueRepo(w, req)
		if w.Code != http.StatusNoContent {
			t.Fatalf("DeleteIssueRepo by URL: want 204, got %d: %s", w.Code, w.Body.String())
		}
	}

	// One left.
	repos, err := testHandler.loadRepoDataByScope(ctx, repoScopeIssue, parseUUID(issueID))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(repos) != 1 || repos[0].URL != "git@example.com:team/web.git" {
		t.Fatalf("after URL-delete: %+v", repos)
	}

	// DELETE by UUID (frontend path).
	repo, err := testHandler.Queries.GetRepoByURL(ctx, "git@example.com:team/web.git")
	if err != nil {
		t.Fatalf("GetRepoByURL: %v", err)
	}
	{
		w := httptest.NewRecorder()
		req := newRequest("DELETE", "/api/issues/"+issueID+"/repos/"+uuidToString(repo.ID), nil)
		req = withURLParams(req, "id", issueID, "repoId", uuidToString(repo.ID))
		testHandler.DeleteIssueRepo(w, req)
		if w.Code != http.StatusNoContent {
			t.Fatalf("DeleteIssueRepo by UUID: want 204, got %d: %s", w.Code, w.Body.String())
		}
	}

	if _, err := testHandler.Queries.GetRepoByURL(ctx, "git@example.com:team/web.git"); err == nil {
		t.Fatalf("expected repo catalog row to be GC'd after last binding dropped")
	}
}

// TestIssueRepos_DeleteUnknownReturns404 pins the same contract Step 2 has
// at project scope: a URL that's not bound here is a 404, not a 204. The
// frontend's optimistic-revert path relies on this.
func TestIssueRepos_DeleteUnknownReturns404(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	issueID := createIssueForRepoTest(t, "404 issue")

	w := httptest.NewRecorder()
	path := "/api/issues/" + issueID + "/repos?url=" + url.QueryEscape("git@example.com:team/never-bound.git")
	req := newRequest("DELETE", path, nil)
	req = withURLParam(req, "id", issueID)
	testHandler.DeleteIssueRepo(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown URL, got %d: %s", w.Code, w.Body.String())
	}
}

// TestIssueRepos_DeleteUrlBoundElsewhereReturns404 catches the regression
// pattern Step 2 fixed: URL exists in the catalog (because some workspace or
// project binding references it), but it's NOT bound to *this* issue. The
// `:exec DELETE` would silently 204, masking the no-op. The handler uses
// RETURNING to detect zero matches and surfaces 404 so the frontend / CLI
// know the call did nothing.
func TestIssueRepos_DeleteUrlBoundElsewhereReturns404(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	wsUUID := parseUUID(testWorkspaceID)
	issueID := createIssueForRepoTest(t, "Bound-elsewhere issue")

	const sharedURL = "git@example.com:team/issue-bound-at-workspace.git"
	t.Cleanup(func() {
		testHandler.Queries.DeleteRepoBindingsForScope(ctx, db.DeleteRepoBindingsForScopeParams{
			ScopeType: repoScopeWorkspace,
			ScopeID:   wsUUID,
		})
		testHandler.Queries.DeleteOrphanRepos(ctx)
	})
	if err := testHandler.setRepoBindingsForScope(ctx, repoScopeWorkspace, wsUUID, []RepoData{
		{URL: sharedURL, Description: "lives at workspace scope"},
	}); err != nil {
		t.Fatalf("seed workspace binding: %v", err)
	}

	w := httptest.NewRecorder()
	path := "/api/issues/" + issueID + "/repos?url=" + url.QueryEscape(sharedURL)
	req := newRequest("DELETE", path, nil)
	req = withURLParam(req, "id", issueID)
	testHandler.DeleteIssueRepo(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when URL is bound elsewhere but not on this issue, got %d: %s", w.Code, w.Body.String())
	}

	// Workspace binding must survive — issue DELETE must not touch any other
	// scope's bindings.
	if _, err := testHandler.Queries.GetRepoByURL(ctx, sharedURL); err != nil {
		t.Fatalf("workspace binding should still exist after a no-op issue delete: %v", err)
	}
}

// TestIssueRepos_NonAdminGetsForbidden pins the role gate. Plain 'member'
// role gets 403 on POST/DELETE; reads stay open. Same shape as the project
// endpoint; the router-level middleware enforces the gate but the handler
// re-checks via `requireIssueRepoWriter` so the contract holds when handlers
// are invoked directly (tests, future router refactors).
func TestIssueRepos_NonAdminGetsForbidden(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	issueID := createIssueForRepoTest(t, "Role-gate issue")

	withTestMemberRole(t, "member")

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/"+issueID+"/repos",
		map[string]any{"url": "git@example.com:team/forbidden.git"})
	req = withURLParam(req, "id", issueID)
	testHandler.CreateIssueRepo(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("CreateIssueRepo as 'member': want 403, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	path := "/api/issues/" + issueID + "/repos?url=" + url.QueryEscape("git@example.com:team/anything.git")
	req = newRequest("DELETE", path, nil)
	req = withURLParam(req, "id", issueID)
	testHandler.DeleteIssueRepo(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("DeleteIssueRepo as 'member': want 403, got %d: %s", w.Code, w.Body.String())
	}

	// Reads stay open for plain members.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/issues/"+issueID+"/repos", nil)
	req = withURLParam(req, "id", issueID)
	testHandler.ListIssueRepos(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListIssueRepos as 'member': want 200, got %d: %s", w.Code, w.Body.String())
	}
}

// TestDeleteIssue_OrphansBindings asserts that deleting an issue drops its
// repo bindings and GCs the catalog entry when no other scope references it.
// Mirrors the workspace (Step 1) and project (Step 2) cascade tests — the
// trio guarantees the cleanup hooks stay aligned across scopes.
func TestDeleteIssue_OrphansBindings(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":    "Cleanup issue",
		"status":   "todo",
		"priority": "medium",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: %d %s", w.Code, w.Body.String())
	}
	var issue IssueResponse
	json.NewDecoder(w.Body).Decode(&issue)

	const url1 = "git@example.com:team/issue-cleanup.git"
	if err := testHandler.setRepoBindingsForScope(ctx, repoScopeIssue, parseUUID(issue.ID), []RepoData{
		{URL: url1, Description: "alone in this scope"},
	}); err != nil {
		t.Fatalf("seed binding: %v", err)
	}

	if _, err := testHandler.Queries.GetRepoByURL(ctx, url1); err != nil {
		t.Fatalf("GetRepoByURL pre-delete: %v", err)
	}

	w = httptest.NewRecorder()
	req = newRequest("DELETE", "/api/issues/"+issue.ID, nil)
	req = withURLParam(req, "id", issue.ID)
	testHandler.DeleteIssue(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteIssue: %d %s", w.Code, w.Body.String())
	}

	bindings, err := testHandler.Queries.ListReposByScope(ctx, db.ListReposByScopeParams{
		ScopeType: repoScopeIssue,
		ScopeID:   parseUUID(issue.ID),
	})
	if err != nil {
		t.Fatalf("ListReposByScope: %v", err)
	}
	if len(bindings) != 0 {
		t.Fatalf("expected issue bindings to be cleared, got %d", len(bindings))
	}
	if _, err := testHandler.Queries.GetRepoByURL(ctx, url1); err == nil {
		t.Fatalf("expected orphan repo row to be GC'd after issue delete")
	}
}

// TestIssueRepos_CardinalityMatrix exercises the 0/1/N cardinality story
// from the parent design at issue scope, as observed via the operational-
// repos resolver task-claim uses. The 0-repo branch is the documentation /
// design issue case the proposal calls out — daemon must see `[]` when no
// scope binds anything.
func TestIssueRepos_CardinalityMatrix(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	wsUUID := parseUUID(testWorkspaceID)

	t.Cleanup(func() {
		testHandler.Queries.DeleteRepoBindingsForScope(ctx, db.DeleteRepoBindingsForScopeParams{
			ScopeType: repoScopeWorkspace,
			ScopeID:   wsUUID,
		})
		testHandler.Queries.DeleteOrphanRepos(ctx)
	})

	// 0-repo case — issue with no project, no workspace bindings, no issue
	// bindings. The "pure documentation issue" branch the proposal calls out.
	docIssueID := createIssueForRepoTest(t, "0-repo doc issue")
	docIssue := db.Issue{
		ID:          parseUUID(docIssueID),
		WorkspaceID: wsUUID,
	}
	got, err := testHandler.loadOperationalReposForIssue(ctx, docIssue)
	if err != nil {
		t.Fatalf("0-repo: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("0-repo: expected empty, got %+v", got)
	}

	// 1-repo case — single binding at issue scope.
	oneIssueID := createIssueForRepoTest(t, "1-repo issue")
	const repoOne = "git@example.com:team/single-issue.git"
	if err := testHandler.setRepoBindingsForScope(ctx, repoScopeIssue, parseUUID(oneIssueID), []RepoData{
		{URL: repoOne, Description: "the only one"},
	}); err != nil {
		t.Fatalf("seed 1-repo issue binding: %v", err)
	}
	oneIssue := db.Issue{ID: parseUUID(oneIssueID), WorkspaceID: wsUUID}
	if got, err := testHandler.loadOperationalReposForIssue(ctx, oneIssue); err != nil {
		t.Fatalf("1-repo: %v", err)
	} else if len(got) != 1 || got[0].URL != repoOne {
		t.Fatalf("1-repo: %+v", got)
	}

	// N-repo case — workspace adds a couple of repos that union with the
	// issue's. Order is by URL.
	nIssueID := createIssueForRepoTest(t, "N-repo issue")
	if err := testHandler.setRepoBindingsForScope(ctx, repoScopeIssue, parseUUID(nIssueID), []RepoData{
		{URL: "git@example.com:team/issue-extra.git", Description: "issue scope only"},
	}); err != nil {
		t.Fatalf("seed issue binding: %v", err)
	}
	if err := testHandler.setRepoBindingsForScope(ctx, repoScopeWorkspace, wsUUID, []RepoData{
		{URL: "git@example.com:team/api.git", Description: "ws api"},
		{URL: "git@example.com:team/web.git", Description: "ws web"},
	}); err != nil {
		t.Fatalf("seed workspace bindings: %v", err)
	}
	nIssue := db.Issue{ID: parseUUID(nIssueID), WorkspaceID: wsUUID}
	if got, err := testHandler.loadOperationalReposForIssue(ctx, nIssue); err != nil {
		t.Fatalf("N-repo: %v", err)
	} else if len(got) != 3 {
		t.Fatalf("N-repo: expected 3 (workspace 2 + issue 1), got %d: %+v", len(got), got)
	}
}

// TestClaimTask_IssueScopeBinding_IncludesIssueRepos extends the Step-2
// claim integration test with an issue-scope binding to assert all three
// levels surface in the daemon claim response. Repos at workspace, project
// and issue scope each carry a different description so the precedence
// ladder ("issue > project > workspace" wins on collision) and the union
// behavior (no scope shadows another scope's distinct URLs) are both pinned
// on the wire — not just at the helper level.
func TestClaimTask_IssueScopeBinding_IncludesIssueRepos(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	wsUUID := parseUUID(testWorkspaceID)
	projectID := createProjectForRepoTest(t, "Step 3 claim integration project")

	t.Cleanup(func() {
		testHandler.Queries.DeleteRepoBindingsForScope(ctx, db.DeleteRepoBindingsForScopeParams{
			ScopeType: repoScopeWorkspace,
			ScopeID:   wsUUID,
		})
		testHandler.Queries.DeleteOrphanRepos(ctx)
	})

	wsRepo := "git@example.com:team/claim3-ws.git"
	projRepo := "git@example.com:team/claim3-proj.git"
	issueRepo := "git@example.com:team/claim3-issue.git"
	overlap := "git@example.com:team/claim3-overlap.git"
	if err := testHandler.setRepoBindingsForScope(ctx, repoScopeWorkspace, wsUUID, []RepoData{
		{URL: wsRepo, Description: "workspace-bound"},
		{URL: overlap, Description: "ws description"},
	}); err != nil {
		t.Fatalf("seed workspace binding: %v", err)
	}
	if err := testHandler.setRepoBindingsForScope(ctx, repoScopeProject, parseUUID(projectID), []RepoData{
		{URL: projRepo, Description: "project-bound"},
		{URL: overlap, Description: "project description"},
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
		VALUES ($1, 'Step 3 claim integration', 'todo', 'member', $2, $3)
		RETURNING id
	`, testWorkspaceID, testUserID, projectID).Scan(&issueID); err != nil {
		t.Fatalf("setup: create issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})
	if err := testHandler.setRepoBindingsForScope(ctx, repoScopeIssue, parseUUID(issueID), []RepoData{
		{URL: issueRepo, Description: "issue-bound"},
		{URL: overlap, Description: "issue description wins"},
	}); err != nil {
		t.Fatalf("seed issue binding: %v", err)
	}

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
		testWorkspaceID, "claim-int-step3-test")
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
	if urls[issueRepo] != "issue-bound" {
		t.Fatalf("issue repo missing or description wrong: %+v", urls)
	}
	if urls[overlap] != "issue description wins" {
		t.Fatalf("expected issue description to win on URL collision, got: %q", urls[overlap])
	}
	if len(urls) != 4 {
		t.Fatalf("expected exactly 4 repos in claim response (3 distinct + 1 overlap deduped), got %d: %+v", len(urls), resp.Task.Repos)
	}
}

// TestBatchDeleteIssues_OrphansBindings is the regression Codex flagged on
// PR #5: the multi-select batch delete path was calling DeleteIssue directly
// without the issue-scope repo cleanup the single-delete handler runs. Any
// issue removed via the batch UI used to leave repo_binding rows for
// scope_type=issue behind, and orphan repo rows would never be GC'd —
// violating the cleanup invariant Step 3 introduced. Pinning this here
// guarantees the two delete paths stay in sync going forward.
func TestBatchDeleteIssues_OrphansBindings(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	mkIssue := func(title string) string {
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
			"title":    title,
			"status":   "todo",
			"priority": "medium",
		})
		testHandler.CreateIssue(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("CreateIssue: %d %s", w.Code, w.Body.String())
		}
		var issue IssueResponse
		json.NewDecoder(w.Body).Decode(&issue)
		return issue.ID
	}

	idA := mkIssue("Batch delete A")
	idB := mkIssue("Batch delete B")

	const urlA = "git@example.com:team/batch-cleanup-a.git"
	const urlB = "git@example.com:team/batch-cleanup-b.git"
	if err := testHandler.setRepoBindingsForScope(ctx, repoScopeIssue, parseUUID(idA), []RepoData{
		{URL: urlA, Description: "issue A only"},
	}); err != nil {
		t.Fatalf("seed binding A: %v", err)
	}
	if err := testHandler.setRepoBindingsForScope(ctx, repoScopeIssue, parseUUID(idB), []RepoData{
		{URL: urlB, Description: "issue B only"},
	}); err != nil {
		t.Fatalf("seed binding B: %v", err)
	}

	// Sanity — both repo rows exist before the batch delete.
	if _, err := testHandler.Queries.GetRepoByURL(ctx, urlA); err != nil {
		t.Fatalf("GetRepoByURL pre-delete A: %v", err)
	}
	if _, err := testHandler.Queries.GetRepoByURL(ctx, urlB); err != nil {
		t.Fatalf("GetRepoByURL pre-delete B: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/batch-delete?workspace_id="+testWorkspaceID, map[string]any{
		"issue_ids": []string{idA, idB},
	})
	testHandler.BatchDeleteIssues(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("BatchDeleteIssues: %d %s", w.Code, w.Body.String())
	}
	var resp struct {
		Deleted int `json:"deleted"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Deleted != 2 {
		t.Fatalf("expected 2 deleted, got %d", resp.Deleted)
	}

	// Bindings on each issue scope must be gone.
	for _, id := range []string{idA, idB} {
		bindings, err := testHandler.Queries.ListReposByScope(ctx, db.ListReposByScopeParams{
			ScopeType: repoScopeIssue,
			ScopeID:   parseUUID(id),
		})
		if err != nil {
			t.Fatalf("ListReposByScope %s: %v", id, err)
		}
		if len(bindings) != 0 {
			t.Fatalf("expected issue %s bindings cleared by batch delete, got %d", id, len(bindings))
		}
	}

	// Orphan GC must have swept both catalog rows.
	if _, err := testHandler.Queries.GetRepoByURL(ctx, urlA); err == nil {
		t.Fatalf("expected repo %s to be GC'd after batch delete", urlA)
	}
	if _, err := testHandler.Queries.GetRepoByURL(ctx, urlB); err == nil {
		t.Fatalf("expected repo %s to be GC'd after batch delete", urlB)
	}
}

// TestBatchDeleteIssues_PreservesSharedRepoCatalog complements the test
// above: when a URL is bound at workspace scope (or any other surviving
// scope) AND on an issue we're deleting, the batch path must drop the
// issue-scope binding without GC'ing the shared catalog row. That's the
// orphan-GC predicate's contract — keep alive as long as any binding
// references the repo.
func TestBatchDeleteIssues_PreservesSharedRepoCatalog(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	wsUUID := parseUUID(testWorkspaceID)

	t.Cleanup(func() {
		testHandler.Queries.DeleteRepoBindingsForScope(ctx, db.DeleteRepoBindingsForScopeParams{
			ScopeType: repoScopeWorkspace,
			ScopeID:   wsUUID,
		})
		testHandler.Queries.DeleteOrphanRepos(ctx)
	})

	const shared = "git@example.com:team/shared-after-batch-delete.git"
	if err := testHandler.setRepoBindingsForScope(ctx, repoScopeWorkspace, wsUUID, []RepoData{
		{URL: shared, Description: "lives at workspace scope"},
	}); err != nil {
		t.Fatalf("seed workspace binding: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":    "Issue with overlapping repo",
		"status":   "todo",
		"priority": "medium",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: %d %s", w.Code, w.Body.String())
	}
	var issue IssueResponse
	json.NewDecoder(w.Body).Decode(&issue)
	if err := testHandler.setRepoBindingsForScope(ctx, repoScopeIssue, parseUUID(issue.ID), []RepoData{
		{URL: shared, Description: "issue's view"},
	}); err != nil {
		t.Fatalf("seed issue binding: %v", err)
	}

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/issues/batch-delete?workspace_id="+testWorkspaceID, map[string]any{
		"issue_ids": []string{issue.ID},
	})
	testHandler.BatchDeleteIssues(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("BatchDeleteIssues: %d %s", w.Code, w.Body.String())
	}

	// Workspace binding survives — orphan GC must keep the catalog row alive
	// because some scope still references it.
	if _, err := testHandler.Queries.GetRepoByURL(ctx, shared); err != nil {
		t.Fatalf("workspace binding should survive batch delete: %v", err)
	}
}

// TestIssueRepos_VisibilityNoLeak pins the proposal's "visibility property":
// an issue's `repos` view must NOT include unbound workspace repos that
// happen to be in the catalog only because some *other* issue or project
// binds them. The bindings layer already enforces this — it's the union
// resolver's contract — so the test exercises the user-facing GET to lock
// in that the property holds at the HTTP boundary too.
func TestIssueRepos_VisibilityNoLeak(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	otherIssueID := createIssueForRepoTest(t, "Sibling issue with private repo")
	const sibling = "git@example.com:team/sibling-private.git"
	if err := testHandler.setRepoBindingsForScope(ctx, repoScopeIssue, parseUUID(otherIssueID), []RepoData{
		{URL: sibling, Description: "should not leak"},
	}); err != nil {
		t.Fatalf("seed sibling binding: %v", err)
	}

	thisIssueID := createIssueForRepoTest(t, "Visibility issue")

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/issues/"+thisIssueID+"/repos", nil)
	req = withURLParam(req, "id", thisIssueID)
	testHandler.ListIssueRepos(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListIssueRepos: %d %s", w.Code, w.Body.String())
	}
	var got struct {
		Repos []RepoData `json:"repos"`
	}
	json.NewDecoder(w.Body).Decode(&got)
	for _, r := range got.Repos {
		if r.URL == sibling {
			t.Fatalf("sibling-issue repo leaked into this issue's view: %+v", got.Repos)
		}
	}
	if len(got.Repos) != 0 {
		t.Fatalf("expected empty list for an unbound issue, got %+v", got.Repos)
	}
}
