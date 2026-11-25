import { describe, expect, it } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { mayView, usePerms, useUserOnly, type Perms } from "./perms";

// The Mock's person is an owner of the workspace, with the roles enforced.
const wrap = ({ children }: { children: ReactNode }) => <QueryClientProvider client={new QueryClient()}>{children}</QueryClientProvider>;

describe("what the person may do", () => {
  it("is everything for an owner, on the workspace and on any project", async () => {
    const { result } = renderHook(() => usePerms("hello-world"), { wrapper: wrap });
    await waitFor(() => expect(result.current.loaded).toBe(true));
    expect(result.current.owner).toBe(true);
    expect(result.current.admin).toBe(true);
    expect(result.current.create).toBe(true);
    expect(result.current.deploy).toBe(true);
    expect(result.current.destroy).toBe(true);
    expect(result.current.enforced).toBe(true);
  });

  it("is not someone who only opens apps", async () => {
    const { result } = renderHook(() => useUserOnly(usePerms()), { wrapper: wrap });
    await waitFor(() => expect(result.current).toBe(false));
  });
});

describe("who may view a project of the launcher", () => {
  const base = { loaded: true, enforced: true, owner: false, admin: false } as Perms;
  const roles = (projects: Record<string, "user" | "reader" | "developer">) => ({ ...base, me: { subject: "x", provider: "local", admin: false, roles: { enforced: true, projects } } }) as Perms;

  it("is not who holds only the right to open it, by the role the card says", () => {
    expect(mayView(base, { slug: "a", role: "user" })).toBe(false);
    expect(mayView(base, { slug: "a", role: "reader" })).toBe(false);
    expect(mayView(base, { slug: "a" })).toBe(true);
  });

  it("is not who holds only that role, by the person's own roles", () => {
    expect(mayView(roles({ a: "user", b: "developer" }), { slug: "a" })).toBe(false);
    expect(mayView(roles({ a: "user", b: "developer" }), { slug: "b" })).toBe(true);
  });

  it("is everyone while roles are not enforced, and the workspace's administrators", () => {
    expect(mayView({ ...base, enforced: false }, { slug: "a", role: "user" })).toBe(true);
    expect(mayView({ ...base, admin: true }, { slug: "a", role: "user" })).toBe(true);
  });
});
