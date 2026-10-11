import { describe, expect, it } from "vitest";
import { signUp } from "./signup";

describe("the sign-up link", () => {
  it("creates an account and redirects to cloud signup", () => {
    expect(signUp).toBe("https://accounts.shpyrd.io/signup?redirect_url=/signup/cloud");
    const url = new URL(signUp);
    expect(url.searchParams.get("redirect_url")).toBe("/signup/cloud");
  });
});
