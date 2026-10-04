import { describe, expect, it } from "vitest";
import { accountWords } from "./console-users";

describe("how a console user signs in, in words", () => {
  it("is an email and password for an active account", () => {
    expect(accountWords("active")).toBe("email and password");
  });

  it("says when the account has no password yet or is locked", () => {
    expect(accountWords("pending")).toBe("no password yet");
    expect(accountWords("locked")).toBe("locked for now");
  });

  it("is the sign-in methods only without an account", () => {
    expect(accountWords("")).toBe("sign-in methods only");
  });
});
