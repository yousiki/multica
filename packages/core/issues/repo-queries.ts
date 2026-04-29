import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type { WorkspaceRepo } from "../types";

// Issue-scope repo bindings (Step 3 of MUL-14). Mirrors the project-scope
// hook shape (packages/core/projects/repo-queries.ts) — same per-row commit
// model and same optimistic mutation semantics — so the issue properties
// sidebar and the project sidebar can use identical RepoListEditor surfaces
// with only the binding ID differing between them.

export const issueRepoKeys = {
  // wsId is part of the key so workspace-switching invalidates automatically
  // (the issue ID alone isn't enough for cross-workspace cache symmetry on
  // workspace teardown — same rationale as projectRepoKeys).
  all: (wsId: string) => ["issue-repos", wsId] as const,
  list: (wsId: string, issueId: string) =>
    [...issueRepoKeys.all(wsId), issueId] as const,
};

export function issueReposOptions(wsId: string, issueId: string) {
  return queryOptions({
    queryKey: issueRepoKeys.list(wsId, issueId),
    queryFn: () => api.listIssueRepos(issueId).then((r) => r.repos),
  });
}

export function useAddIssueRepo(wsId: string, issueId: string) {
  const qc = useQueryClient();
  const key = issueRepoKeys.list(wsId, issueId);

  return useMutation({
    mutationFn: (repo: { url: string; description?: string }) =>
      api.addIssueRepo(issueId, repo),
    onMutate: async (repo) => {
      await qc.cancelQueries({ queryKey: key });
      const prev = qc.getQueryData<WorkspaceRepo[]>(key) ?? [];
      // Optimistic insert / update by URL — matches the server's idempotent
      // upsert so concurrent renders don't show a duplicate row briefly.
      const url = repo.url.trim();
      const description = (repo.description ?? "").trim();
      const next = prev.some((r) => r.url === url)
        ? prev.map((r) => (r.url === url ? { url, description } : r))
        : [...prev, { url, description }];
      qc.setQueryData(key, next);
      return { prev };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev) qc.setQueryData(key, ctx.prev);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: key });
    },
  });
}

export function useRemoveIssueRepo(wsId: string, issueId: string) {
  const qc = useQueryClient();
  const key = issueRepoKeys.list(wsId, issueId);

  return useMutation({
    mutationFn: (urlOrRepoId: string) => api.removeIssueRepo(issueId, urlOrRepoId),
    onMutate: async (urlOrRepoId) => {
      await qc.cancelQueries({ queryKey: key });
      const prev = qc.getQueryData<WorkspaceRepo[]>(key) ?? [];
      qc.setQueryData(
        key,
        prev.filter((r) => r.url !== urlOrRepoId),
      );
      return { prev };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev) qc.setQueryData(key, ctx.prev);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: key });
    },
  });
}
