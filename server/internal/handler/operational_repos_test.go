package handler

import (
	"context"
	"sort"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestLoadOperationalRepos_UnionSemantics pins the union behaviour Step 2
// adds — `operational = workspace ∪ project` — across the four cardinality
// shapes Step 3 will reuse: workspace-only, project-only, both-overlapping,
// and neither. The collision case is the key one: when the same git URL is
// bound at both workspace and project scope, the project description wins
// because project is the closer scope.
func TestLoadOperationalRepos_UnionSemantics(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	wsUUID := parseUUID(testWorkspaceID)
	// Throwaway project UUIDs — the bindings are polymorphic over scope_id, so
	// we don't need actual project rows to exercise the resolver.
	projectA := parseUUID("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	projectB := parseUUID("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")

	t.Cleanup(func() {
		for _, scope := range []db.DeleteRepoBindingsForScopeParams{
			{ScopeType: repoScopeWorkspace, ScopeID: wsUUID},
			{ScopeType: repoScopeProject, ScopeID: projectA},
			{ScopeType: repoScopeProject, ScopeID: projectB},
		} {
			testHandler.Queries.DeleteRepoBindingsForScope(ctx, scope)
		}
		testHandler.Queries.DeleteOrphanRepos(ctx)
	})

	wsOnly := "git@example.com:team/ws-only.git"
	projOnly := "git@example.com:team/proj-only.git"
	overlap := "git@example.com:team/overlap.git"

	if err := testHandler.setRepoBindingsForScope(ctx, repoScopeWorkspace, wsUUID, []RepoData{
		{URL: wsOnly, Description: "workspace says ws"},
		{URL: overlap, Description: "workspace's view of overlap"},
	}); err != nil {
		t.Fatalf("seed workspace bindings: %v", err)
	}
	if err := testHandler.setRepoBindingsForScope(ctx, repoScopeProject, projectA, []RepoData{
		{URL: projOnly, Description: "project says proj"},
		{URL: overlap, Description: "project's view of overlap"},
	}); err != nil {
		t.Fatalf("seed project bindings: %v", err)
	}

	cases := []struct {
		name   string
		scopes []scopeKey
		want   []RepoData
	}{
		{
			name:   "workspace only (no project bound)",
			scopes: []scopeKey{{Type: repoScopeWorkspace, ID: wsUUID}},
			want: []RepoData{
				{URL: overlap, Description: "workspace's view of overlap"},
				{URL: wsOnly, Description: "workspace says ws"},
			},
		},
		{
			name:   "project only (call site dropped the workspace scope)",
			scopes: []scopeKey{{Type: repoScopeProject, ID: projectA}},
			want: []RepoData{
				{URL: overlap, Description: "project's view of overlap"},
				{URL: projOnly, Description: "project says proj"},
			},
		},
		{
			name: "both with overlap — project description wins",
			scopes: []scopeKey{
				{Type: repoScopeWorkspace, ID: wsUUID},
				{Type: repoScopeProject, ID: projectA},
			},
			want: []RepoData{
				{URL: overlap, Description: "project's view of overlap"},
				{URL: projOnly, Description: "project says proj"},
				{URL: wsOnly, Description: "workspace says ws"},
			},
		},
		{
			name: "neither (project B has no bindings, workspace empty case)",
			scopes: []scopeKey{
				{Type: repoScopeProject, ID: projectB},
			},
			want: []RepoData{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := testHandler.loadOperationalRepos(ctx, tc.scopes)
			if err != nil {
				t.Fatalf("loadOperationalRepos: %v", err)
			}
			sort.Slice(got, func(i, j int) bool { return got[i].URL < got[j].URL })
			sort.Slice(tc.want, func(i, j int) bool { return tc.want[i].URL < tc.want[j].URL })

			if len(got) != len(tc.want) {
				t.Fatalf("len mismatch: got %d, want %d (%+v vs %+v)", len(got), len(tc.want), got, tc.want)
			}
			for i := range got {
				if got[i].URL != tc.want[i].URL || got[i].Description != tc.want[i].Description {
					t.Fatalf("entry %d mismatch: got %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestLoadOperationalRepos_InvalidScopesSkipped guards against a nil-scope
// foot-gun — `loadOperationalReposForIssue` appends `{project, issue.ProjectID}`
// only when the project ID is valid, but defensive callers might still pass
// scopes with a zero UUID and the helper must treat those as no-op rather
// than firing a query that matches "no scope at all" semantics.
func TestLoadOperationalRepos_InvalidScopesSkipped(t *testing.T) {
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

	const url = "git@example.com:team/skip-scope.git"
	if err := testHandler.setRepoBindingsForScope(ctx, repoScopeWorkspace, wsUUID, []RepoData{
		{URL: url, Description: "the only one"},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	got, err := testHandler.loadOperationalRepos(ctx, []scopeKey{
		{Type: repoScopeWorkspace, ID: wsUUID},
		{Type: repoScopeProject}, // zero ID, must be ignored
		{Type: ""},                // empty type, must be ignored
	})
	if err != nil {
		t.Fatalf("loadOperationalRepos: %v", err)
	}
	if len(got) != 1 || got[0].URL != url {
		t.Fatalf("invalid scopes leaked through: got %+v", got)
	}
}
