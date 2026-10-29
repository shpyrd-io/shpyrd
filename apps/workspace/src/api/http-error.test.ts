import { afterEach, expect, it, vi } from "vitest";
import { ApiError } from "@shpyrd/shared/api/error";
import { request } from "@shpyrd/shared/api/http";

afterEach(() => vi.unstubAllGlobals());

it("carries the server's code with its words on a refusal", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => new Response(JSON.stringify({ error: "Link your GitHub account first", code: "github_link_required" }), { status: 409 })),
  );
  const err = (await request("/api/workspace/github/accounts").catch((e) => e)) as ApiError;
  expect(err).toBeInstanceOf(ApiError);
  expect(err.status).toBe(409);
  expect(err.code).toBe("github_link_required");
  expect(err.message).toBe("Link your GitHub account first");
});
