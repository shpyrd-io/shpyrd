import { describe, expect, it } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { usePerms, useUserOnly } from "./perms";

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
