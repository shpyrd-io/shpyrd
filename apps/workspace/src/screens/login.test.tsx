import { describe, expect, it } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { useDoor } from "./login";

const wrap = ({ children }: { children: ReactNode }) => <QueryClientProvider client={new QueryClient()}>{children}</QueryClientProvider>;

describe("the door", () => {
  it("waits for the server, then opens: the Mock's person is signed in", async () => {
    const { result } = renderHook(() => useDoor(), { wrapper: wrap });
    expect(result.current.state).toBe("waiting");
    // The Mock is loaded on the first call and answers each one after a
    // short wait; two calls open the door. A loaded machine takes longer
    // than waitFor's second.
    await waitFor(() => expect(result.current.state).toBe("open"), { timeout: 5000 });
    expect(result.current.state === "open" && result.current.config.workspace?.slug).toBe("acme");
    expect(result.current.state === "open" && result.current.me?.email).toBe("ana@acme.com");
  });
});
