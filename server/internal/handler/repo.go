package handler

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// errEmptyRepoURL and errRepoBindingNotFound are the two domain errors the
// per-binding helpers surface. Handlers turn the first into 400 and the
// second into 404. Defining them up here means the HTTP layer can `errors.Is`
// rather than string-compare.
var (
	errEmptyRepoURL        = errors.New("repo url is required")
	errRepoBindingNotFound = errors.New("repo binding not found")
)

// Scope types for repo_binding.scope_type. The schema CHECK already restricts
// values, but exposing constants here keeps callers from hand-typing a string
// every time. `repoScopeIssue` is reserved for Step 3.
const (
	repoScopeWorkspace = "workspace"
	repoScopeProject   = "project"
)

// scopeKey is one entry in a (scope_type, scope_id) list. The union resolver
// takes a slice of these so callers don't have to keep two parallel arrays in
// sync just to feed the sqlc query — the helper splits them at the boundary.
type scopeKey struct {
	Type string
	ID   pgtype.UUID
}

// scopePrecedence orders scope_type values for collision resolution when two
// scopes bind the same URL. Higher value wins. Once issue-scope bindings land
// in Step 3 the third tier slots in here without changing the rest of the
// pipeline.
func scopePrecedence(t string) int {
	switch t {
	case repoScopeIssue:
		return 2
	case repoScopeProject:
		return 1
	case repoScopeWorkspace:
		return 0
	default:
		return -1
	}
}

// repoScopeIssue is reserved for Step 3 (issue-scope binding). Defined now so
// `scopePrecedence` can reference it without forward declarations and so the
// merge order is locked in at the helper level rather than at every caller.
const repoScopeIssue = "issue"

// loadRepoDataByScope returns the repos bound to (scope_type, scope_id), in
// the normalized shape that the daemon/agent code expects (URL-trimmed,
// deduped, empty URLs dropped). Description comes from the binding row, so
// two scopes that share the same git URL can each carry their own
// description without one overwriting the other.
// Returns an empty slice — never nil — when no bindings exist.
func (h *Handler) loadRepoDataByScope(ctx context.Context, scopeType string, scopeID pgtype.UUID) ([]RepoData, error) {
	rows, err := h.Queries.ListReposByScope(ctx, db.ListReposByScopeParams{
		ScopeType: scopeType,
		ScopeID:   scopeID,
	})
	if err != nil {
		return nil, err
	}
	repos := make([]RepoData, 0, len(rows))
	for _, r := range rows {
		repos = append(repos, RepoData{URL: r.Url, Description: r.Description})
	}
	return normalizeWorkspaceRepos(repos), nil
}

// loadWorkspaceRepoData is a thin convenience wrapper around the
// "workspace"-scoped lookup. Most call sites only need the workspace level
// today, so spelling it out here keeps them readable.
func (h *Handler) loadWorkspaceRepoData(ctx context.Context, workspaceID pgtype.UUID) ([]RepoData, error) {
	return h.loadRepoDataByScope(ctx, repoScopeWorkspace, workspaceID)
}

// loadOperationalRepos resolves the union of bindings across the scopes the
// caller provides — typically (workspace, optional project, optional issue)
// for a task — and returns the deduplicated `[]RepoData` an agent should
// actually operate on. Higher-precedence scopes (issue > project > workspace)
// supply the description when the same URL is bound at multiple levels, so
// the closer the user attached the description the more authoritative it is.
//
// Step 2 wires this in for issue-task / chat / autopilot claims with
// (workspace, project) inputs. Step 3 will simply add the issue scope to the
// scopes slice — no signature change.
//
// `scopes` may contain entries with an invalid (zero) UUID; those are skipped
// silently so callers can append optional scopes without explicit nil checks.
func (h *Handler) loadOperationalRepos(ctx context.Context, scopes []scopeKey) ([]RepoData, error) {
	types := make([]string, 0, len(scopes))
	ids := make([]pgtype.UUID, 0, len(scopes))
	for _, s := range scopes {
		if !s.ID.Valid || s.Type == "" {
			continue
		}
		types = append(types, s.Type)
		ids = append(ids, s.ID)
	}
	if len(types) == 0 {
		return []RepoData{}, nil
	}

	rows, err := h.Queries.ListReposByScopes(ctx, db.ListReposByScopesParams{
		ScopeTypes: types,
		ScopeIds:   ids,
	})
	if err != nil {
		return nil, err
	}

	// One pass to keep the highest-precedence binding per URL. Within a single
	// query trip there's no ordering guarantee on scope_type, so we always
	// compare and only overwrite when the new row outranks the stored one.
	type scoredRepo struct {
		data RepoData
		rank int
	}
	best := make(map[string]scoredRepo, len(rows))
	for _, row := range rows {
		url := strings.TrimSpace(row.Url)
		if url == "" {
			continue
		}
		rank := scopePrecedence(row.ScopeType)
		incoming := scoredRepo{
			data: RepoData{URL: url, Description: strings.TrimSpace(row.Description)},
			rank: rank,
		}
		if existing, ok := best[url]; ok {
			if rank <= existing.rank {
				continue
			}
		}
		best[url] = incoming
	}

	repos := make([]RepoData, 0, len(best))
	for _, sr := range best {
		repos = append(repos, sr.data)
	}
	// Stable URL order matches the per-scope helper — callers (including the
	// daemon repos_version hash) expect a deterministic sort.
	sort.Slice(repos, func(i, j int) bool { return repos[i].URL < repos[j].URL })
	return repos, nil
}

// loadOperationalReposForIssue is the convenience wrapper task-claim sites
// use. Builds the (workspace, project?) scope list from the issue and forwards
// to loadOperationalRepos. The issue scope is reserved for Step 3.
func (h *Handler) loadOperationalReposForIssue(ctx context.Context, issue db.Issue) ([]RepoData, error) {
	scopes := []scopeKey{{Type: repoScopeWorkspace, ID: issue.WorkspaceID}}
	if issue.ProjectID.Valid {
		scopes = append(scopes, scopeKey{Type: repoScopeProject, ID: issue.ProjectID})
	}
	return h.loadOperationalRepos(ctx, scopes)
}

// setRepoBindingsForScope replaces the entire set of bindings attached to
// (scope_type, scope_id) with `repos`. The repo catalog itself is upsert-by-URL
// so two scopes sharing the same git URL share a single repo row, but the
// description is stored per-binding so workspace A's "Backend API" doesn't
// clobber workspace B's "Shared SDK" for the same URL. Orphan repo rows that
// no scope references anymore are pruned at the end of the transaction.
func (h *Handler) setRepoBindingsForScope(ctx context.Context, scopeType string, scopeID pgtype.UUID, repos []RepoData) error {
	repos = normalizeWorkspaceRepos(repos)

	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	qtx := h.Queries.WithTx(tx)
	if err := qtx.DeleteRepoBindingsForScope(ctx, db.DeleteRepoBindingsForScopeParams{
		ScopeType: scopeType,
		ScopeID:   scopeID,
	}); err != nil {
		return err
	}
	for _, r := range repos {
		repo, err := qtx.UpsertRepoByURL(ctx, r.URL)
		if err != nil {
			return err
		}
		if _, err := qtx.CreateRepoBinding(ctx, db.CreateRepoBindingParams{
			RepoID:      repo.ID,
			ScopeType:   scopeType,
			ScopeID:     scopeID,
			Description: r.Description,
		}); err != nil {
			return err
		}
	}
	if err := qtx.DeleteOrphanRepos(ctx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// addRepoBindingToScope upserts a single (URL, scope) binding without touching
// any other binding on the scope. This is the per-row equivalent of
// setRepoBindingsForScope and exists so the project / issue CLIs can
// `add <url>` without paying for a wipe-and-replace that would needlessly
// re-mint created_at on every other binding. The CreateRepoBinding upsert
// stamps the latest `description` on the conflict path so calling this twice
// with different descriptions is the idiomatic way to mutate one in place.
func (h *Handler) addRepoBindingToScope(ctx context.Context, scopeType string, scopeID pgtype.UUID, repo RepoData) (RepoData, error) {
	url := strings.TrimSpace(repo.URL)
	desc := strings.TrimSpace(repo.Description)
	if url == "" {
		return RepoData{}, errEmptyRepoURL
	}

	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return RepoData{}, err
	}
	defer tx.Rollback(ctx)

	qtx := h.Queries.WithTx(tx)
	row, err := qtx.UpsertRepoByURL(ctx, url)
	if err != nil {
		return RepoData{}, err
	}
	if _, err := qtx.CreateRepoBinding(ctx, db.CreateRepoBindingParams{
		RepoID:      row.ID,
		ScopeType:   scopeType,
		ScopeID:     scopeID,
		Description: desc,
	}); err != nil {
		return RepoData{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return RepoData{}, err
	}
	return RepoData{URL: url, Description: desc}, nil
}

// removeRepoBindingFromScope drops one (URL, scope) binding and GCs the repo
// catalog row when no scope references it anymore. The lookup accepts either a
// repo UUID or the URL string — the URL form is what the CLI / UI hits because
// the binding ID isn't surfaced in the user-facing payload, and matching by
// URL keeps the caller from having to round-trip through GET first.
//
// Returns errRepoBindingNotFound when the URL/UUID does not resolve to a repo
// row, OR when that repo has no binding on this scope. Both are 404s at the
// HTTP layer.
func (h *Handler) removeRepoBindingFromScope(ctx context.Context, scopeType string, scopeID pgtype.UUID, repoIDOrURL string) error {
	repoIDOrURL = strings.TrimSpace(repoIDOrURL)
	if repoIDOrURL == "" {
		return errRepoBindingNotFound
	}

	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	qtx := h.Queries.WithTx(tx)

	var repo db.Repo
	if repoUUID, err := util.ParseUUID(repoIDOrURL); err == nil {
		repo, err = qtx.GetRepo(ctx, repoUUID)
		if err != nil {
			return errRepoBindingNotFound
		}
	} else {
		repo, err = qtx.GetRepoByURL(ctx, repoIDOrURL)
		if err != nil {
			return errRepoBindingNotFound
		}
	}

	// `:one DELETE … RETURNING` so we can tell apart "binding existed and was
	// dropped" from "URL resolves to a repo, but it isn't bound on this
	// scope". The latter is a 404 at the HTTP layer — the plain :exec variant
	// would silently 204, which Codex flagged as masking no-op deletes for
	// URLs that happen to be bound at workspace or another project scope.
	if _, err := qtx.DeleteRepoBindingIfExists(ctx, db.DeleteRepoBindingIfExistsParams{
		RepoID:    repo.ID,
		ScopeType: scopeType,
		ScopeID:   scopeID,
	}); err != nil {
		if isNotFound(err) {
			return errRepoBindingNotFound
		}
		return err
	}
	if err := qtx.DeleteOrphanRepos(ctx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
