import { describe, it, expect } from "vitest";
import { resolveInternalLink, isMentionHref } from "./link-handler";

describe("resolveInternalLink", () => {
  it("returns null for external URLs (caller opens those in a new browser tab)", () => {
    expect(resolveInternalLink("https://example.com/page")).toBeNull();
    expect(resolveInternalLink("mailto:foo@bar.com")).toBeNull();
    expect(resolveInternalLink("tel:+1234567890")).toBeNull();
  });

  it("passes already-scoped workspace paths through unchanged", () => {
    expect(resolveInternalLink("/acme/issues/abc", "acme")).toBe("/acme/issues/abc");
    // Cross-workspace links keep their target slug — the navigator decides
    // whether to switch workspaces, link-handler does not rewrite them.
    expect(resolveInternalLink("/butter/issues/abc", "acme")).toBe("/butter/issues/abc");
  });

  it("prepends the current slug to legacy slug-less paths whose first segment is a known route", () => {
    // The whole point of WORKSPACE_ROUTE_SEGMENTS — markdown authored before
    // the URL refactor (or hand-written by users) often lacks the slug. The
    // editor picks up the surrounding workspace's slug and prefixes it.
    expect(resolveInternalLink("/issues/abc", "acme")).toBe("/acme/issues/abc");
    expect(resolveInternalLink("/projects", "acme")).toBe("/acme/projects");
    expect(resolveInternalLink("/inbox", "acme")).toBe("/acme/inbox");
    expect(resolveInternalLink("/my-issues", "acme")).toBe("/acme/my-issues");
  });

  it("does NOT prefix paths whose first segment is a global / reserved route", () => {
    // /login, /workspaces/new, /invite/... are intentionally global. Adding
    // a slug here would break the auth/onboarding flows.
    expect(resolveInternalLink("/login", "acme")).toBe("/login");
    expect(resolveInternalLink("/workspaces/new", "acme")).toBe("/workspaces/new");
    expect(resolveInternalLink("/invite/abc", "acme")).toBe("/invite/abc");
  });

  it("does NOT prefix unknown first segments — the author meant what they wrote", () => {
    // /acme-issues/issues looks like /<unknown>/issues. The first segment is
    // a slug we don't recognize as a workspace route, so we treat it as a
    // user-authored slug and leave it alone.
    expect(resolveInternalLink("/foo/bar", "acme")).toBe("/foo/bar");
  });

  it("returns the path unchanged when no current slug is provided", () => {
    expect(resolveInternalLink("/issues/abc")).toBe("/issues/abc");
    expect(resolveInternalLink("/issues/abc", null)).toBe("/issues/abc");
  });
});

describe("isMentionHref", () => {
  it("detects mention:// links", () => {
    expect(isMentionHref("mention://issue/abc")).toBe(true);
    expect(isMentionHref("mention://member/foo")).toBe(true);
  });

  it("rejects non-mention strings", () => {
    expect(isMentionHref("/acme/issues")).toBe(false);
    expect(isMentionHref("https://example.com")).toBe(false);
    expect(isMentionHref(null)).toBe(false);
    expect(isMentionHref(undefined)).toBe(false);
    expect(isMentionHref("")).toBe(false);
  });
});
