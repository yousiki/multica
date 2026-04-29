import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type { WorkspaceRepo } from "../types";

// Project repo bindings (Step 2 of MUL-14). Project-scope bindings are managed
// per-row over the API, so the cache shape is the deduped binding list and the
// add/remove mutations are optimistic — Save buttons in the workspace settings
// don't fit the per-binding interaction model.

export const projectRepoKeys = {
  // wsId is part of the key so workspace-switching invalidates automatically
  // (the project ID alone isn't enough — projects from different workspaces
  // never collide, but the pattern is "every workspace-scoped query keys on
  // wsId" and we follow it for cache symmetry on workspace teardown).
  all: (wsId: string) => ["project-repos", wsId] as const,
  list: (wsId: string, projectId: string) =>
    [...projectRepoKeys.all(wsId), projectId] as const,
};

export function projectReposOptions(wsId: string, projectId: string) {
  return queryOptions({
    queryKey: projectRepoKeys.list(wsId, projectId),
    queryFn: () => api.listProjectRepos(projectId).then((r) => r.repos),
  });
}

export function useAddProjectRepo(wsId: string, projectId: string) {
  const qc = useQueryClient();
  const key = projectRepoKeys.list(wsId, projectId);

  return useMutation({
    mutationFn: (repo: { url: string; description?: string }) =>
      api.addProjectRepo(projectId, repo),
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

export function useRemoveProjectRepo(wsId: string, projectId: string) {
  const qc = useQueryClient();
  const key = projectRepoKeys.list(wsId, projectId);

  return useMutation({
    mutationFn: (urlOrRepoId: string) => api.removeProjectRepo(projectId, urlOrRepoId),
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
