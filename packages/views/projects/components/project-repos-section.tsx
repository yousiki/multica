"use client";

import { useState } from "react";
import { ChevronRight, Plus, Trash2 } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { toast } from "sonner";
import {
  projectReposOptions,
  useAddProjectRepo,
  useRemoveProjectRepo,
} from "@multica/core/projects/repo-queries";
import { useWorkspaceId } from "@multica/core/hooks";

// Project-scope repo bindings sidebar section. Lives next to the project's
// Description / Properties to mirror the proposal's framing — "where the
// agent works" is project-level metadata, same conceptual surface as
// status / lead / priority.
//
// Per-row commit (no Save button) is intentional: the API is per-binding
// add/remove, and the optimistic mutations in repo-queries.ts handle the
// flicker. A dirty-list-with-Save model would force the client to diff
// before submitting and bring the workspace settings' UX into a place where
// it doesn't belong (workspace settings is a wholesale settings page; this
// is a property-row sidebar inside the project view).
//
// The "Add" affordance is a tiny inline form rather than a modal because
// adding a repo is the most common action and round-tripping through a modal
// would feel heavy in a sidebar.
export function ProjectReposSection({
  projectId,
  canEdit,
}: {
  projectId: string;
  canEdit: boolean;
}) {
  const wsId = useWorkspaceId();
  const [open, setOpen] = useState(true);
  const [adding, setAdding] = useState(false);
  const [newURL, setNewURL] = useState("");
  const [newDesc, setNewDesc] = useState("");

  const { data: repos = [], isLoading } = useQuery(
    projectReposOptions(wsId, projectId),
  );
  const addRepo = useAddProjectRepo(wsId, projectId);
  const removeRepo = useRemoveProjectRepo(wsId, projectId);

  const handleAdd = async () => {
    const url = newURL.trim();
    if (!url) return;
    try {
      await addRepo.mutateAsync({ url, description: newDesc.trim() });
      setNewURL("");
      setNewDesc("");
      setAdding(false);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to bind repo");
    }
  };

  const handleRemove = async (url: string) => {
    try {
      await removeRepo.mutateAsync(url);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to unbind repo");
    }
  };

  return (
    <div>
      <button
        className={`flex w-full items-center gap-1 rounded-md px-2 py-1 text-xs font-medium transition-colors mb-2 hover:bg-accent/70 ${open ? "" : "text-muted-foreground hover:text-foreground"}`}
        onClick={() => setOpen(!open)}
      >
        Repositories
        <ChevronRight
          className={`!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform ${open ? "rotate-90" : ""}`}
        />
      </button>
      {open && (
        <div className="space-y-2 pl-2">
          {isLoading ? (
            <div className="text-xs text-muted-foreground">Loading…</div>
          ) : repos.length === 0 && !adding ? (
            <div className="text-xs text-muted-foreground">
              No repositories bound to this project. Workspace-level repos are still
              available to agents working on this project's issues.
            </div>
          ) : null}

          {repos.map((repo) => (
            <div key={repo.url} className="flex items-start gap-2">
              <div className="flex-1 min-w-0">
                <div className="text-xs font-mono truncate" title={repo.url}>
                  {repo.url}
                </div>
                {repo.description && (
                  <div className="text-xs text-muted-foreground truncate" title={repo.description}>
                    {repo.description}
                  </div>
                )}
              </div>
              {canEdit && (
                <Button
                  variant="ghost"
                  size="icon-sm"
                  className="shrink-0 text-muted-foreground hover:text-destructive"
                  title="Unbind repo"
                  onClick={() => handleRemove(repo.url)}
                  disabled={removeRepo.isPending}
                >
                  <Trash2 className="!size-3" />
                </Button>
              )}
            </div>
          ))}

          {canEdit && adding && (
            <div className="space-y-1.5 rounded-md border border-dashed p-2">
              <Input
                value={newURL}
                onChange={(e) => setNewURL(e.target.value)}
                placeholder="https://git.example.com/org/repo.git"
                className="text-xs h-7"
                autoFocus
              />
              <Input
                value={newDesc}
                onChange={(e) => setNewDesc(e.target.value)}
                placeholder="Description (optional)"
                className="text-xs h-7"
              />
              <div className="flex gap-1.5">
                <Button
                  size="sm"
                  onClick={handleAdd}
                  disabled={!newURL.trim() || addRepo.isPending}
                >
                  Save
                </Button>
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => {
                    setAdding(false);
                    setNewURL("");
                    setNewDesc("");
                  }}
                >
                  Cancel
                </Button>
              </div>
            </div>
          )}

          {canEdit && !adding && (
            <Button
              variant="ghost"
              size="sm"
              className="text-muted-foreground"
              onClick={() => setAdding(true)}
            >
              <Plus className="!size-3" />
              Add repository
            </Button>
          )}
        </div>
      )}
    </div>
  );
}
