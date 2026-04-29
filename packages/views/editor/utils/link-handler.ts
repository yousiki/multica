/**
 * Pure link-handling utilities for the editor system.
 *
 * Used by content-editor (ProseMirror click handler), readonly-content
 * (react-markdown link component), and link-hover-card (Open button).
 *
 * The actual navigation step is intentionally NOT performed here — these
 * helpers are framework-agnostic. Callers wire the result into platform
 * navigation via `useOpenLink` (see `./use-open-link`), which routes
 * internal links through `useNavigation().push` so cross-workspace links
 * switch workspace correctly on desktop.
 */

import { isGlobalPath } from "@multica/core/paths";

/**
 * Top-level workspace-scoped routes. Used to detect "/{route}/..." paths that
 * were authored without a workspace slug — we prepend the current slug so they
 * resolve correctly under the new /{slug}/{route}/... URL shape.
 *
 * Why a hardcoded allowlist: the heuristic must be conservative. A path like
 * "/acme/issues/abc" already has a slug (first segment "acme" isn't a known
 * route), so leaving it alone is correct. A path like "/foo/bar" where "foo"
 * isn't a known route is ambiguous — we don't rewrite it, treating the author
 * as intentional. Only "/issues/..." style paths get auto-prefixed.
 */
const WORKSPACE_ROUTE_SEGMENTS = new Set([
  "issues",
  "projects",
  "autopilots",
  "agents",
  "inbox",
  "my-issues",
  "runtimes",
  "skills",
  "settings",
]);

/**
 * Resolve a clicked href into the internal path that should be navigated to,
 * or `null` for hrefs that aren't internal app paths (callers should treat
 * those as external and open them in a new browser tab).
 *
 * If `currentSlug` is provided and `href` is a workspace-scoped path lacking a
 * slug (e.g. "/issues/abc" instead of "/{slug}/issues/abc"), the slug is
 * prepended. This is for legacy markdown content authored before the URL
 * refactor, or future content where users forget the slug when pasting.
 */
export function resolveInternalLink(
  href: string,
  currentSlug?: string | null,
): string | null {
  if (!href.startsWith("/")) return null;
  let path = href;
  if (currentSlug && !isGlobalPath(path)) {
    const firstSegment = path.split("/")[1];
    if (firstSegment && WORKSPACE_ROUTE_SEGMENTS.has(firstSegment)) {
      // Path looks like /issues/abc (no slug) — prepend current slug.
      path = `/${currentSlug}${path}`;
    }
    // Otherwise the first segment is either already a slug (e.g. "acme" in
    // "/acme/issues") or something unknown (e.g. "/foo"). Leave it alone —
    // the user wrote what they meant.
  }
  return path;
}

/** Check if a href is a mention protocol link (should not be opened as a regular link). */
export function isMentionHref(href: string | null | undefined): href is string {
  return !!href && href.startsWith("mention://");
}
