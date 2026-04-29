import { describe, it, expect, vi, beforeEach } from "vitest";

// createTabRouter pulls in real route modules — stub it here exactly like
// tab-store.test.ts does, so the store can mint `Tab` objects without
// touching the browser router.
const createTabRouterMock = vi.hoisted(() =>
  vi.fn(() => ({
    dispose: vi.fn(),
    state: { location: { pathname: "/" } },
    navigate: vi.fn(),
    subscribe: vi.fn(() => () => {}),
  })),
);
vi.mock("../routes", () => ({
  createTabRouter: createTabRouterMock,
}));

const logoutMock = vi.hoisted(() => vi.fn());
vi.mock("@multica/core/auth", () => ({
  useAuthStore: { getState: () => ({ logout: logoutMock }) },
}));

import { navigateDesktopPath } from "./navigation";
import { useTabStore } from "../stores/tab-store";
import { useWindowOverlayStore } from "../stores/window-overlay-store";

beforeEach(() => {
  createTabRouterMock.mockClear();
  logoutMock.mockClear();
  useTabStore.getState().reset();
  useWindowOverlayStore.getState().close?.();
});

describe("navigateDesktopPath", () => {
  // ---------------------------------------------------------------------
  // Same workspace — push/replace must hit the supplied router, never the
  // tab store's openTab. This is the "navigate within the active tab"
  // mode and was the legacy default for in-tab `<AppLink>` clicks.
  // ---------------------------------------------------------------------
  it("push within the active workspace navigates the supplied router", () => {
    const store = useTabStore.getState();
    store.switchWorkspace("acme");

    const navigate = vi.fn();
    const router = {
      navigate,
      state: { location: { pathname: "/acme/issues" } },
      subscribe: vi.fn(() => () => {}),
      dispose: vi.fn(),
    } as unknown as Parameters<typeof navigateDesktopPath>[0]["router"];

    navigateDesktopPath({ path: "/acme/issues/abc", mode: "push", router });

    expect(navigate).toHaveBeenCalledWith("/acme/issues/abc", { replace: false });
    // No new tab opened — push stays in-place.
    expect(useTabStore.getState().byWorkspace.acme.tabs).toHaveLength(1);
  });

  it("replace within the active workspace navigates with replace=true", () => {
    const store = useTabStore.getState();
    store.switchWorkspace("acme");

    const navigate = vi.fn();
    const router = {
      navigate,
      state: { location: { pathname: "/acme/issues" } },
      subscribe: vi.fn(() => () => {}),
      dispose: vi.fn(),
    } as unknown as Parameters<typeof navigateDesktopPath>[0]["router"];

    navigateDesktopPath({ path: "/acme/projects", mode: "replace", router });

    expect(navigate).toHaveBeenCalledWith("/acme/projects", { replace: true });
  });

  it("new-tab within the active workspace adds a tab and activates it", () => {
    const store = useTabStore.getState();
    store.switchWorkspace("acme");

    navigateDesktopPath({ path: "/acme/projects", mode: "new-tab", title: "Projects" });

    const s = useTabStore.getState();
    expect(s.activeWorkspaceSlug).toBe("acme");
    expect(s.byWorkspace.acme.tabs.map((t) => t.path)).toContain("/acme/projects");
    const active = s.byWorkspace.acme.tabs.find(
      (t) => t.id === s.byWorkspace.acme.activeTabId,
    );
    expect(active?.path).toBe("/acme/projects");
  });

  // ---------------------------------------------------------------------
  // Cross-workspace dispatch — the SHA-33 bug class. The path's leading
  // slug differs from `activeWorkspaceSlug`; we must NOT push/replace into
  // the active tab (its router belongs to another workspace's group),
  // and we must NOT open a new tab in the active group. The only correct
  // move is `switchWorkspace(targetSlug, path)`.
  // ---------------------------------------------------------------------
  it("push to another workspace switches workspace via the tab store", () => {
    const store = useTabStore.getState();
    store.switchWorkspace("acme");
    store.switchWorkspace("butter");
    expect(useTabStore.getState().activeWorkspaceSlug).toBe("butter");

    const navigate = vi.fn();
    const router = {
      navigate,
      state: { location: { pathname: "/butter/issues" } },
      subscribe: vi.fn(() => () => {}),
      dispose: vi.fn(),
    } as unknown as Parameters<typeof navigateDesktopPath>[0]["router"];

    navigateDesktopPath({ path: "/acme/inbox?issue=abc", mode: "push", router });

    // Router was NOT touched — push across workspaces would corrupt history.
    expect(navigate).not.toHaveBeenCalled();

    const s = useTabStore.getState();
    expect(s.activeWorkspaceSlug).toBe("acme");
    // The tab landed in acme's group, never butter's.
    expect(s.byWorkspace.acme.tabs.some((t) => t.path === "/acme/inbox?issue=abc")).toBe(true);
    expect(s.byWorkspace.butter.tabs.some((t) => t.path.startsWith("/acme/"))).toBe(false);
  });

  it("replace to another workspace also switches workspace (router untouched)", () => {
    const store = useTabStore.getState();
    store.switchWorkspace("acme");
    store.switchWorkspace("butter");

    const navigate = vi.fn();
    const router = {
      navigate,
      state: { location: { pathname: "/butter/issues" } },
      subscribe: vi.fn(() => () => {}),
      dispose: vi.fn(),
    } as unknown as Parameters<typeof navigateDesktopPath>[0]["router"];

    navigateDesktopPath({ path: "/acme/projects", mode: "replace", router });

    expect(navigate).not.toHaveBeenCalled();
    expect(useTabStore.getState().activeWorkspaceSlug).toBe("acme");
  });

  it("new-tab to another workspace switches workspace and lands in the right group", () => {
    const store = useTabStore.getState();
    store.switchWorkspace("acme");
    store.switchWorkspace("butter");

    navigateDesktopPath({ path: "/acme/projects", mode: "new-tab" });

    const s = useTabStore.getState();
    expect(s.activeWorkspaceSlug).toBe("acme");
    expect(s.byWorkspace.acme.tabs.some((t) => t.path === "/acme/projects")).toBe(true);
    expect(s.byWorkspace.butter.tabs.some((t) => t.path === "/acme/projects")).toBe(false);
  });

  // ---------------------------------------------------------------------
  // Window-overlay paths — these never become tabs, no matter the mode.
  // The interception order is: overlay first, then cross-workspace, then
  // tab-store. Pre-workspace flows depend on this so the overlay opens
  // even when activeWorkspaceSlug is null.
  // ---------------------------------------------------------------------
  it("/workspaces/new opens the new-workspace overlay and skips the tab store", () => {
    navigateDesktopPath({ path: "/workspaces/new", mode: "push" });
    const overlay = useWindowOverlayStore.getState();
    expect(overlay.overlay?.type).toBe("new-workspace");
    expect(useTabStore.getState().byWorkspace).toEqual({});
  });

  it("/invite/<id> opens the invite overlay and skips the tab store", () => {
    navigateDesktopPath({ path: "/invite/abc-123", mode: "push" });
    const overlay = useWindowOverlayStore.getState();
    expect(overlay.overlay?.type).toBe("invite");
    expect(useTabStore.getState().byWorkspace).toEqual({});
  });

  // ---------------------------------------------------------------------
  // /login push triggers logout (preserves legacy DesktopNavigationProvider
  // behavior). replace/new-tab are intentionally NOT logout — they were
  // never used historically and turning them into logout would surprise
  // any future caller.
  // ---------------------------------------------------------------------
  it("push to /login triggers auth.logout(), not navigation", () => {
    navigateDesktopPath({ path: "/login", mode: "push" });
    expect(logoutMock).toHaveBeenCalledTimes(1);
    expect(useTabStore.getState().byWorkspace).toEqual({});
  });

  it("replace to /login does NOT trigger logout", () => {
    navigateDesktopPath({ path: "/login", mode: "replace" });
    expect(logoutMock).not.toHaveBeenCalled();
  });
});
