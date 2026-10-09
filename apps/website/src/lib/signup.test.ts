import { describe, expect, it } from "vitest";
import { signUp, signUpFor } from "./signup";

describe("the sign-up link", () => {
  it("chooses the free plan unless a plan is named", () => {
    expect(signUp).toBe("https://signup.shpyrd.io/?plan=free");
    expect(signUpFor()).toBe(signUp);
  });

  it("chooses the plan it is given", () => {
    expect(signUpFor("starter")).toBe("https://signup.shpyrd.io/?plan=starter");
  });

  it("keeps a plan's name in its own parameter", () => {
    expect(signUpFor("a&b")).toBe("https://signup.shpyrd.io/?plan=a%26b");
  });
});
