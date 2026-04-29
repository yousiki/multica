import React from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, act, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

// Mock api.* before importing the hooks under test. The mutation hooks call
// `api.addIssueRepo` / `api.removeIssueRepo` / `api.listIssueRepos` directly,
// so steering those is enough to exercise the optimistic-cache and
// invalidation behavior end to end without touching the network.
const mockListIssueRepos = vi.fn();
const mockAddIssueRepo = vi.fn();
const mockRemoveIssueRepo = vi.fn();

vi.mock("@multica/core/api", () => ({
  api: {
    listIssueRepos: (...args: unknown[]) => mockListIssueRepos(...args),
    addIssueRepo: (...args: unknown[]) => mockAddIssueRepo(...args),
    removeIssueRepo: (...args: unknown[]) => mockRemoveIssueRepo(...args),
  },
}));

import {
  issueRepoKeys,
  issueReposOptions,
  useAddIssueRepo,
  useRemoveIssueRepo,
} from "@multica/core/issues/repo-queries";

function makeWrapper(qc: QueryClient) {
  // Tiny QueryClientProvider wrapper. Defining inline keeps each test
  // hermetic — no shared client across cases means optimistic state from one
  // test can't leak into the next.
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  };
}

function makeQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false, staleTime: 0 },
      mutations: { retry: false },
    },
  });
}

describe("useIssueRepos optimistic mutations", () => {
  beforeEach(() => {
    mockListIssueRepos.mockReset();
    mockAddIssueRepo.mockReset();
    mockRemoveIssueRepo.mockReset();
  });

  it("optimistically inserts a new binding and confirms via refetch", async () => {
    const wsId = "ws-1";
    const issueId = "issue-1";
    const initial = [{ url: "git@example.com:team/api.git", description: "API" }];
    const after = [
      ...initial,
      { url: "git@example.com:team/web.git", description: "Web" },
    ];

    mockListIssueRepos
      .mockResolvedValueOnce({ repos: initial })
      .mockResolvedValueOnce({ repos: after });
    mockAddIssueRepo.mockResolvedValue({
      repo: { url: "git@example.com:team/web.git", description: "Web" },
      repos: after,
    });

    const qc = makeQueryClient();
    const wrapper = makeWrapper(qc);

    // Seed the cache with the initial GET so the optimistic update has a
    // baseline to mutate; matches the runtime ordering (a sidebar always
    // mounts the query first, then dispatches the mutation).
    await qc.prefetchQuery(issueReposOptions(wsId, issueId));
    expect(qc.getQueryData(issueRepoKeys.list(wsId, issueId))).toEqual(initial);

    const { result } = renderHook(() => useAddIssueRepo(wsId, issueId), {
      wrapper,
    });

    await act(async () => {
      await result.current.mutateAsync({
        url: "git@example.com:team/web.git",
        description: "Web",
      });
    });

    // After the mutation settles, the invalidation triggers a refetch which
    // returns the canonical post-add state. The cache must reflect that.
    await waitFor(() => {
      expect(qc.getQueryData(issueRepoKeys.list(wsId, issueId))).toEqual(after);
    });
    expect(mockAddIssueRepo).toHaveBeenCalledWith(issueId, {
      url: "git@example.com:team/web.git",
      description: "Web",
    });
  });

  it("rolls back the optimistic add when the server rejects", async () => {
    const wsId = "ws-1";
    const issueId = "issue-2";
    const initial = [{ url: "git@example.com:team/api.git", description: "API" }];

    mockListIssueRepos.mockResolvedValue({ repos: initial });
    mockAddIssueRepo.mockRejectedValue(new Error("forbidden"));

    const qc = makeQueryClient();
    const wrapper = makeWrapper(qc);
    await qc.prefetchQuery(issueReposOptions(wsId, issueId));

    const { result } = renderHook(() => useAddIssueRepo(wsId, issueId), {
      wrapper,
    });

    await act(async () => {
      await expect(
        result.current.mutateAsync({ url: "git@example.com:team/forbidden.git" }),
      ).rejects.toThrow(/forbidden/);
    });

    // The optimistic write must be reverted to the prev cache value, even
    // though the onSettled invalidation also fires — both branches converge
    // on the same baseline because the server didn't accept the change.
    await waitFor(() => {
      expect(qc.getQueryData(issueRepoKeys.list(wsId, issueId))).toEqual(initial);
    });
  });

  it("optimistically removes a binding by URL", async () => {
    const wsId = "ws-1";
    const issueId = "issue-3";
    const initial = [
      { url: "git@example.com:team/api.git", description: "API" },
      { url: "git@example.com:team/web.git", description: "Web" },
    ];
    const after = [{ url: "git@example.com:team/api.git", description: "API" }];

    mockListIssueRepos
      .mockResolvedValueOnce({ repos: initial })
      .mockResolvedValueOnce({ repos: after });
    mockRemoveIssueRepo.mockResolvedValue(undefined);

    const qc = makeQueryClient();
    const wrapper = makeWrapper(qc);
    await qc.prefetchQuery(issueReposOptions(wsId, issueId));

    const { result } = renderHook(() => useRemoveIssueRepo(wsId, issueId), {
      wrapper,
    });

    await act(async () => {
      await result.current.mutateAsync("git@example.com:team/web.git");
    });

    await waitFor(() => {
      expect(qc.getQueryData(issueRepoKeys.list(wsId, issueId))).toEqual(after);
    });
    expect(mockRemoveIssueRepo).toHaveBeenCalledWith(
      issueId,
      "git@example.com:team/web.git",
    );
  });

  it("idempotent re-add updates description in place rather than duplicating", async () => {
    const wsId = "ws-1";
    const issueId = "issue-4";
    const initial = [
      { url: "git@example.com:team/api.git", description: "Old description" },
    ];
    const after = [
      { url: "git@example.com:team/api.git", description: "New description" },
    ];

    mockListIssueRepos
      .mockResolvedValueOnce({ repos: initial })
      .mockResolvedValueOnce({ repos: after });
    mockAddIssueRepo.mockResolvedValue({
      repo: { url: "git@example.com:team/api.git", description: "New description" },
      repos: after,
    });

    const qc = makeQueryClient();
    const wrapper = makeWrapper(qc);
    await qc.prefetchQuery(issueReposOptions(wsId, issueId));

    const { result } = renderHook(() => useAddIssueRepo(wsId, issueId), {
      wrapper,
    });

    await act(async () => {
      await result.current.mutateAsync({
        url: "git@example.com:team/api.git",
        description: "New description",
      });
    });

    // Critical: list length stays at 1 (no duplicate row), description
    // updates. This pins the same idempotency contract the server has.
    await waitFor(() => {
      const cached = qc.getQueryData(issueRepoKeys.list(wsId, issueId)) as Array<{
        url: string;
        description: string;
      }>;
      expect(cached).toHaveLength(1);
      expect(cached[0]?.description).toBe("New description");
    });
  });
});
