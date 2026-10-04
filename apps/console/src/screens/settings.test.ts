import { describe, expect, it } from "vitest";
import { formOf, licenseSummary, renewalText, settingsOf } from "./settings";

const license = { id: "lic_1", customer: "Acme Corp", issuedAt: "2026-10-04T00:00:00Z", expiresAt: "2027-10-04T00:00:00Z" };

describe("the license, in words", () => {
  it("is in force until the day it expires", () => {
    expect(licenseSummary({ active: true, license })).toEqual({ badge: "success", label: "in force", text: "In force until 2027-10-04." });
  });

  it("says when it expired and that the features are off", () => {
    expect(licenseSummary({ active: false, license })).toMatchObject({ badge: "error", label: "expired", text: "Expired on 2027-10-04: the enterprise features are off." });
  });

  it("says why a license is refused", () => {
    expect(licenseSummary({ active: false, error: "the license's signature does not match" })).toMatchObject({ label: "refused", text: "The license installed is refused: the license's signature does not match." });
  });

  it("says there is none", () => {
    expect(licenseSummary({ active: false })).toMatchObject({ badge: "neutral", label: "none" });
  });

  it("is on without a license where the build runs the platform itself", () => {
    expect(licenseSummary({ active: true, unlocked: true })).toMatchObject({ badge: "success", label: "on" });
  });
});

describe("how the license renews, in words", () => {
  it("names the billing app it renews at, and how the last try went", () => {
    const online = { ...license, issuer: "https://billing.example.com" };
    expect(renewalText({ active: true, license: online })).toBe("Renews online at https://billing.example.com, a week before it expires.");
    expect(renewalText({ active: true, license: online, renewal: { at: "2026-10-04T09:12:00Z", error: "billing answered 409: the account is suspended" } })).toMatch(/failed: billing answered 409: the account is suspended$/);
  });

  it("says a license issued by hand renews offline", () => {
    expect(renewalText({ active: true, license })).toBe("Issued by hand: it renews offline, with a new license.");
  });
});

describe("the workspace's settings, as a form", () => {
  it("turns what is set into fields and back", () => {
    const settings = { limits: { projects: 2, cpu: "2", memory: "512Mi" }, sleep: { appsAfter: "10m", appsResuming: "page" as const, databasesAfter: "10m" } };
    const form = formOf(settings);
    expect(form).toMatchObject({ projects: "2", instances: "", cpu: "2", memory: "512Mi", appsAfter: "10m", appsResuming: "page", databasesAfter: "10m" });
    expect(settingsOf(form)).toEqual(settings);
  });

  it("is no limit and no sleep when every field is empty", () => {
    expect(settingsOf(formOf({ limits: null, sleep: null }))).toEqual({ limits: null, sleep: null });
  });

  it("drops how apps wake when they never sleep", () => {
    expect(settingsOf({ ...formOf({ limits: null, sleep: null }), appsResuming: "page", databasesAfter: "30m" })).toEqual({ limits: null, sleep: { databasesAfter: "30m" } });
  });
});
