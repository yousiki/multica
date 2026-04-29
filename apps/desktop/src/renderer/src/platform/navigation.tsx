import { useEffect, useMemo, useState } from "react";
import type { DataRouter } from "react-router-dom";
import {
  NavigationProvider,
  type NavigationAdapter,
} from "@multica/views/navigation";
import { useAuthStore } from "@multica/core/auth";
import { extractWorkspaceSlug } from "@multica/core/paths";
import {
  useTabStore,
  resolveRouteIcon,
  useActiveTabIdentity,
  useActiveTabRouter,
  getActiveTab,
} from "@/stores/tab-store";
import { useWindowOverlayStore } from "@/stores/window-overlay-store";

// Public web app URL — injected at build time via .env.production. In dev
// (no VITE_APP_URL set) falls back to the local web dev server so "Copy
// link" in a dev build yields a URL that points at the running dev
// frontend, not the prod host. Matches the fallback used in pages/login.tsx.
const APP_URL = import.meta.env.VITE_APP_URL || "http://localhost:3000";

/**
 * Intercept navigation to "transition" paths — pre-workspace flows that on
 * desktop are rendered as a window-level overlay instead of a tab route.
 * Returns `true` if the navigation was handled (caller should NOT proceed).
 *
 * Side effect: when opening the new-workspace overlay, the tab router is
 * ALSO reset to "/". Rationale — the only way a push lands on
 * /workspaces/new is that the workspace context is gone (fresh install,
 * delete-last, leave-last). Leaving the tab parked on a workspace-scoped
 * path would keep those components mounted under the overlay; the next
 * render after the list cache updates would then throw (useWorkspaceId
 * etc) because the slug no longer resolves.
 */
function tryRouteToOverlay(path: string, router?: DataRouter): boolean {
  const overlay = useWindowOverlayStore.getState();
  if (path === "/workspaces/new") {
    overlay.open({ type: "new-workspace" });
    if (router && router.state.location.pathname !== "/") {
      router.navigate("/", { replace: true });
    }
    return true;
  }
  if (path === "/onboarding") {
    overlay.open({ type: "onboarding" });
    if (router && router.state.location.pathname !== "/") {
      router.navigate("/", { replace: true });
    }
    return true;
  }
  if (path.startsWith("/invite/")) {
    let id = "";
    try {
      id = decodeURIComponent(path.slice("/invite/".length));
    } catch {
      return true;
    }
    if (id) {
      overlay.open({ type: "invite", invitationId: id });
      return true;
    }
  }
  // Any other navigation cancels a live overlay.
  if (overlay.overlay) overlay.close();
  return false;
}

export type NavigateMode = "push" | "replace" | "new-tab";

/**
 * The single internal-navigation entry point on desktop. Every path-based
 * navigation — sidebar, cmd+k, AppLink, in-app `<a>` clicks, OS-notification
 * IPC, editor markdown links — funnels through here, so the rules for
 * cross-workspace dispatch live in exactly one place.
 *
 * Decision tree (in order):
 *   1. `/login` push → kick off logout (preserves existing UX where pushing
 *      `/login` is the "log me out" intent).
 *   2. Overlay paths (`/workspaces/new`, `/onboarding`, `/invite/...`) →
 *      open the window-level overlay; the tab tree stays put.
 *   3. Cross-workspace path (path's leading slug ≠ active workspace) →
 *      `tab-store.switchWorkspace(slug, path)`. We can't `router.navigate`
 *      here: the active tab belongs to a different workspace's group, so
 *      pushing /A/... onto a /B-tab's history would corrupt the per-tab
 *      memory router. switchWorkspace either dedupes into A's existing
 *      matching tab or seeds A's group with the new path.
 *   4. Same-workspace `new-tab` mode → openTab + setActiveTab, like the
 *      legacy openInNewTab adapter.
 *   5. Same-workspace `push` / `replace` → forward to the tab's router.
 *      Caller-supplied `router` (from `TabNavigationProvider`) wins;
 *      otherwise we navigate the active tab's router.
 *
 * The cross-workspace branch absorbs what was a separate
 * `tryRouteToOtherWorkspace` helper plus a pile of identical adapter logic
 * across `DesktopNavigationProvider` and `TabNavigationProvider`. It also
 * removes the only reason `multica:navigate` CustomEvent ever existed —
 * any caller can now just call `navigateDesktopPath` (or
 * `useNavigation().push/replace/openInNewTab`).
 */
export function navigateDesktopPath(opts: {
  path: string;
  mode: NavigateMode;
  title?: string;
  /**
   * Tab router to navigate for `push`/`replace`. When omitted, the active
   * tab's router is used — the right default for shell-level callers
   * (sidebar, IPC handlers) that aren't scoped to a specific tab.
   */
  router?: DataRouter;
}): void {
  const { path, mode, title, router } = opts;

  if (path === "/login" && mode === "push") {
    useAuthStore.getState().logout();
    return;
  }

  const targetRouter = router ?? getActiveTab(useTabStore.getState())?.router;
  if (tryRouteToOverlay(path, targetRouter)) return;

  const targetSlug = extractWorkspaceSlug(path);
  const store = useTabStore.getState();

  if (targetSlug && targetSlug !== store.activeWorkspaceSlug) {
    store.switchWorkspace(targetSlug, path);
    return;
  }

  if (mode === "new-tab") {
    const icon = resolveRouteIcon(path);
    const tabId = store.openTab(path, title ?? path, icon);
    if (tabId) store.setActiveTab(tabId);
    return;
  }

  targetRouter?.navigate(path, { replace: mode === "replace" });
}

/**
 * Root-level navigation provider for components outside the per-tab
 * RouterProviders (sidebar, search dialog, modals, WindowOverlay contents).
 *
 * Reads from the active tab's memory router via router.subscribe().
 * Does NOT use any react-router hooks — it's above all RouterProviders.
 */
export function DesktopNavigationProvider({
  children,
}: {
  children: React.ReactNode;
}) {
  // Primitive-only subscriptions so this component doesn't re-render on
  // unrelated store updates (e.g. an inactive tab's router tick). We
  // resolve the active router here only to subscribe once per tab switch.
  const { tabId: activeTabId } = useActiveTabIdentity();
  const router = useActiveTabRouter();
  // Mirror the active tab router's full location (pathname + search) so
  // shell-level consumers of useNavigation() — ChatWindow in particular —
  // can read URL search params. Must stay in sync with TabNavigationProvider
  // below; a partial shape here (just pathname) silently broke focus-mode
  // anchor resolution on `/inbox?issue=…`.
  const [location, setLocation] = useState<{ pathname: string; search: string }>(
    () => ({
      pathname: router?.state.location.pathname ?? "/",
      search: router?.state.location.search ?? "",
    }),
  );

  useEffect(() => {
    if (!router) {
      setLocation({ pathname: "/", search: "" });
      return;
    }
    setLocation({
      pathname: router.state.location.pathname,
      search: router.state.location.search,
    });
    return router.subscribe((state) => {
      setLocation({
        pathname: state.location.pathname,
        search: state.location.search,
      });
    });
  }, [activeTabId, router]);

  const adapter: NavigationAdapter = useMemo(
    () => ({
      push: (path) => navigateDesktopPath({ path, mode: "push" }),
      replace: (path) => navigateDesktopPath({ path, mode: "replace" }),
      back: () => getActiveTab(useTabStore.getState())?.router.navigate(-1),
      pathname: location.pathname,
      searchParams: new URLSearchParams(location.search),
      openInNewTab: (path, title) =>
        navigateDesktopPath({ path, mode: "new-tab", title }),
      getShareableUrl: (path) => `${APP_URL}${path}`,
    }),
    [location],
  );

  return <NavigationProvider value={adapter}>{children}</NavigationProvider>;
}

/**
 * Per-tab navigation provider rendered inside each tab's Activity wrapper.
 * Subscribes to the tab's own router for up-to-date pathname.
 *
 * This is what @multica/views page components read via useNavigation().
 */
export function TabNavigationProvider({
  router,
  children,
}: {
  router: DataRouter;
  children: React.ReactNode;
}) {
  const [location, setLocation] = useState(router.state.location);

  useEffect(() => {
    setLocation(router.state.location);
    return router.subscribe((state) => {
      setLocation(state.location);
    });
  }, [router]);

  const adapter: NavigationAdapter = useMemo(
    () => ({
      push: (path) => navigateDesktopPath({ path, mode: "push", router }),
      replace: (path) => navigateDesktopPath({ path, mode: "replace", router }),
      back: () => router.navigate(-1),
      pathname: location.pathname,
      searchParams: new URLSearchParams(location.search),
      openInNewTab: (path, title) =>
        navigateDesktopPath({ path, mode: "new-tab", title, router }),
      getShareableUrl: (path) => `${APP_URL}${path}`,
    }),
    [router, location],
  );

  return <NavigationProvider value={adapter}>{children}</NavigationProvider>;
}
