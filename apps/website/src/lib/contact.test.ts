import { describe, expect, it } from "vitest";
import { check, isBot, isKind, minimumMs, trap } from "./contact";

const person = { firstName: "Ana", lastName: "Souza", email: "ana@acme.com", company: "Acme" };
const salesAnswers = { ...person, interest: "cloud" };
const enterpriseAnswers = { ...person, jobTitle: "CTO", size: "250-999", runsOn: "own-cloud", timeline: "quarter", needs: ["sso", "sla"] };

describe("check", () => {
  it("passes a complete form", () => {
    expect(check("sales", salesAnswers)).toEqual({});
    expect(check("enterprise", enterpriseAnswers)).toEqual({});
  });

  it("names every required field left empty", () => {
    expect(Object.keys(check("sales", {})).sort()).toEqual(["company", "email", "firstName", "interest", "lastName"]);
  });

  it("refuses an address that is not one", () => {
    expect(check("sales", { ...salesAnswers, email: "ana at acme" }).email).toBeTruthy();
  });

  it("refuses a value no select offers", () => {
    expect(check("sales", { ...salesAnswers, interest: "everything" }).interest).toBeTruthy();
  });

  it("refuses a checkbox the form does not have", () => {
    expect(check("enterprise", { ...enterpriseAnswers, needs: ["sso", "free-lunch"] }).needs).toBeTruthy();
  });

  it("refuses a field longer than it may be", () => {
    expect(check("sales", { ...salesAnswers, company: "x".repeat(101) }).company).toBeTruthy();
    expect(check("sales", { ...salesAnswers, message: "x".repeat(501) }).message).toBeTruthy();
    expect(check("sales", { ...salesAnswers, message: "x".repeat(500) })).toEqual({});
  });

  it("refuses a number or an object where a word is expected, rather than fail", () => {
    expect(check("sales", { ...salesAnswers, firstName: 5 as never }).firstName).toBeTruthy();
    expect(check("sales", { ...salesAnswers, company: { a: 1 } as never }).company).toBeTruthy();
    expect(check("enterprise", { ...enterpriseAnswers, needs: ["sso", 5] as never }).needs).toBeTruthy();
  });

  it("refuses a list where a word is expected", () => {
    expect(check("sales", { ...salesAnswers, company: ["Acme"] }).company).toBeTruthy();
  });
});

describe("isBot", () => {
  it("lets a person through", () => {
    expect(isBot({ [trap]: "", elapsedMs: minimumMs + 1 })).toBe(false);
  });
  it("catches the hidden field filled", () => {
    expect(isBot({ [trap]: "https://spam.example", elapsedMs: 60_000 })).toBe(true);
  });
  it("catches a form sent faster than a person can", () => {
    expect(isBot({ [trap]: "", elapsedMs: 500 })).toBe(true);
  });
  it("catches a form that says nothing of how long it took", () => {
    expect(isBot({ [trap]: "" })).toBe(true);
  });
  it("goes by how long the person took, whatever their clock says", () => {
    expect(isBot({ [trap]: "", elapsedMs: 45_000, startedAt: Date.now() + 600_000 } as never)).toBe(false);
  });
});

describe("the hidden field", () => {
  it("has a name no browser or password manager fills in by itself", () => {
    expect(trap).not.toMatch(/web|url|site|mail|name|phone|tel|company|org|address|city|zip|code/i);
  });
});

describe("isKind", () => {
  it("knows the two forms and nothing else", () => {
    expect(isKind("sales")).toBe(true);
    expect(isKind("enterprise")).toBe(true);
    expect(isKind("support")).toBe(false);
    expect(isKind(undefined)).toBe(false);
  });
});
