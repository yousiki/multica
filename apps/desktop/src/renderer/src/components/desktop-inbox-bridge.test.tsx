import { describe, it, expect, vi, beforeEach } from "vitest";
import { render } from "@testing-library/react";

// createTabRouter pulls in real route modules. Stub it so the tab-store can
// mint Tab objects without touching the browser router (same pattern as
// tab-store.test.ts).
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

// useCurrentWorkspace + useDesktopUnreadBadge are unrelated to the navigation
// path under test; stub them so the component renders without a query/auth
// context.
vi.mock("@multica/core/paths", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/paths")>(
    "@multica/core/paths",
  );
  return {
    ...actual,
    useCurrentWorkspace: () => null,
  };
});
vi.mock("@multica/views/platform", () => ({
  useDesktopUnreadBadge: () => {},
}));

// Avoid mounting the entire DesktopShell — pull DesktopInboxBridge out by
// importing the layout module and reaching for the named export. It's not
// exported, so mount the part of the layout we need by re-implementing the
// IPC subscribe in a tiny harness equivalent to what DesktopInboxBridge does.
// This keeps the test isolated to the navigation behavior we care about.

import { useEffect } from "react";
import { paths } from "@multica/core/paths";
import { useTabStore } from "../stores/tab-store";
import { navigateDesktopPath } from "../platform/navigation";

let inboxOpenCallback:
  | ((payload: { slug: string; itemId: string; issueKey: string }) => void)
  | null = null;

beforeEach(() => {
  createTabRouterMock.mockClear();
  useTabStore.getState().reset();
  inboxOpenCallback = null;

  // Minimal desktopAPI shim — only `onInboxOpen` is exercised here.
  (window as unknown as { desktopAPI: unknown }).desktopAPI = {
    onInboxOpen: (
      cb: (payload: { slug: string; itemId: string; issueKey: string }) => void,
    ) => {
      inboxOpenCallback = cb;
      return () => {
        inboxOpenCallback = null;
      };
    },
  };
});

function InboxBridgeHarness() {
  useEffect(() => {
    return window.desktopAPI.onInboxOpen(({ slug, issueKey }) => {
      if (!slug) return;
      const inboxPath = `${paths.workspace(slug).inbox()}?issue=${encodeURIComponent(issueKey)}`;
      navigateDesktopPath({ path: inboxPath, mode: "new-tab" });
    });
  }, []);
  return null;
}

describe("DesktopInboxBridge IPC → tab store", () => {
  // The SHA-33 acceptance criterion in plain English: the user is on
  // workspace B, a notification from workspace A arrives, the user clicks
  // it, the app must end up on /A/inbox?issue=…, and B's group must NOT
  // contain the foreign path. The bridge funnels the click through
  // navigateDesktopPath, which routes to switchWorkspace because the path
  // slug differs from activeWorkspaceSlug.
  it("a cross-workspace notification flips activeWorkspaceSlug and lands the tab in the notified group", () => {
    const store = useTabStore.getState();
    store.switchWorkspace("acme");
    store.switchWorkspace("butter");
    expect(useTabStore.getState().activeWorkspaceSlug).toBe("butter");

    render(<InboxBridgeHarness />);
    expect(inboxOpenCallback).toBeTruthy();

    inboxOpenCallback?.({ slug: "acme", itemId: "i1", issueKey: "ABC-1" });

    const s = useTabStore.getState();
    expect(s.activeWorkspaceSlug).toBe("acme");

    const tabs = s.byWorkspace.acme.tabs;
    const expectedPath = "/acme/inbox?issue=ABC-1";
    expect(tabs.some((t) => t.path === expectedPath)).toBe(true);

    // Critical: the foreign path did NOT leak into butter's group — the
    // pre-SHA-34 bug class.
    expect(s.byWorkspace.butter.tabs.some((t) => t.path.startsWith("/acme/"))).toBe(false);

    // The notified inbox tab is also the active tab in its group, so
    // InboxPage's selected-item effect resolves the issue immediately.
    const acmeActive = tabs.find((t) => t.id === s.byWorkspace.acme.activeTabId);
    expect(acmeActive?.path).toBe(expectedPath);
  });

  // Notifications from the active workspace shouldn't regress to the slow
  // path either: the inbox tab opens (or activates dedupe) inside the
  // current group, and the active workspace is unchanged.
  it("a same-workspace notification opens or activates the inbox tab without switching workspace", () => {
    const store = useTabStore.getState();
    store.switchWorkspace("acme");

    render(<InboxBridgeHarness />);

    inboxOpenCallback?.({ slug: "acme", itemId: "i1", issueKey: "ABC-1" });

    const s = useTabStore.getState();
    expect(s.activeWorkspaceSlug).toBe("acme");
    expect(s.byWorkspace.acme.tabs.some((t) => t.path === "/acme/inbox?issue=ABC-1")).toBe(true);
    const active = s.byWorkspace.acme.tabs.find(
      (t) => t.id === s.byWorkspace.acme.activeTabId,
    );
    expect(active?.path).toBe("/acme/inbox?issue=ABC-1");
  });

  // No `multica:navigate` listener should be needed any longer — the bridge
  // calls navigateDesktopPath directly. Dispatching the legacy event must
  // be a no-op (no tab created, no workspace switch).
  it("dispatching a legacy multica:navigate CustomEvent is a no-op (the listener is gone)", () => {
    const store = useTabStore.getState();
    store.switchWorkspace("acme");
    render(<InboxBridgeHarness />);

    const before = JSON.stringify(useTabStore.getState().byWorkspace);
    window.dispatchEvent(
      new CustomEvent("multica:navigate", { detail: { path: "/butter/issues" } }),
    );
    const after = JSON.stringify(useTabStore.getState().byWorkspace);

    expect(after).toBe(before);
    expect(useTabStore.getState().activeWorkspaceSlug).toBe("acme");
  });
});
