import { describe, expect, it } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { permsOf, usePerms } from "./perms";

const wrap = ({ children }: { children: ReactNode }) => <QueryClientProvider client={new QueryClient()}>{children}</QueryClientProvider>;

describe("what the person may do at the console", () => {
  it("is everything for a platform admin", () => {
    expect(permsOf({ platform: "platform-admin", enforced: true })).toEqual({ enforced: true, view: true, admin: true });
  });

  it("is to look, and no more, for a platform viewer", () => {
    expect(permsOf({ platform: "platform-viewer", enforced: true })).toEqual({ enforced: true, view: true, admin: false });
  });

  it("is nothing for someone with no platform role", () => {
    expect(permsOf({ workspace: "owner", enforced: true })).toEqual({ enforced: true, view: false, admin: false });
    expect(permsOf(undefined)).toEqual({ enforced: true, view: false, admin: false });
  });

  it("is everything for anyone while the roles are not enforced", () => {
    expect(permsOf({ enforced: false })).toEqual({ enforced: false, view: true, admin: true });
  });

  it("comes from the person the api names: the Mock's is a platform admin", async () => {
    const { result } = renderHook(() => usePerms(), { wrapper: wrap });
    expect(result.current.loaded).toBe(false);
    await waitFor(() => expect(result.current.loaded).toBe(true));
    expect(result.current.me?.email).toBe("patrick@shpyrd.io");
    expect(result.current.admin).toBe(true);
    expect(result.current.view).toBe(true);
  });
});
