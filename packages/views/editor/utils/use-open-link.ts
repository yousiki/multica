"use client";

/**
 * `useOpenLink` — bridge between editor link clicks and platform navigation.
 *
 * Callers (content-editor, readonly-content, link-hover-card) get a single
 * function `(href: string) => void` that:
 *   - Routes internal app paths through `useNavigation().push`, so on desktop
 *     a cross-workspace link automatically switches workspace via the
 *     unified `navigateDesktopPath` flow.
 *   - Opens external URLs in a new browser tab.
 *
 * Why a hook (not a util): we need `push` and the current workspace slug,
 * both of which come from React context. Keeping the URL-resolution logic
 * pure (`resolveInternalLink`) and isolating the React-bound pieces here
 * preserves testability of the path-rewrite rules without spinning up a
 * provider tree for every assertion.
 */

import { useCallback } from "react";
import { useWorkspaceSlug } from "@multica/core/paths";
import { useNavigation } from "../../navigation";
import { resolveInternalLink } from "./link-handler";

export function useOpenLink(): (href: string) => void {
  const { push } = useNavigation();
  const slug = useWorkspaceSlug();
  return useCallback(
    (href: string) => {
      const internal = resolveInternalLink(href, slug);
      if (internal !== null) {
        push(internal);
      } else {
        window.open(href, "_blank", "noopener,noreferrer");
      }
    },
    [push, slug],
  );
}
