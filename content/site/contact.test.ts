import { describe, expect, it } from "vitest";
import { enterprise, privacy, sales, thanks } from "./contact";
import { pricing } from "./pricing";

describe("the contact pages", () => {
  it("ask what the spec says, in its order", () => {
    expect(sales.fields.map((f) => f.name)).toEqual(["firstName", "lastName", "email", "company", "interest", "message"]);
    expect(enterprise.fields.map((f) => f.name)).toEqual([
      "firstName", "lastName", "email", "company", "jobTitle", "size", "runsOn", "needs", "timeline", "message",
    ]);
  });

  it("give every select and every set of checkboxes its choices", () => {
    for (const f of [...sales.fields, ...enterprise.fields]) {
      if (f.type === "select" || f.type === "checkboxes") expect(f.choices?.length, f.name).toBeGreaterThan(1);
    }
  });

  it("say beside the enterprise form what the Enterprise plan adds", () => {
    expect(enterprise.beside.items).toEqual(pricing.enterprise.features);
  });

  it("link the privacy policy the footer links", () => {
    expect(privacy.href).toBe("https://legal.shpyrd.io/global/privacy-policy");
  });

  it("thank the person by the address they gave", () => {
    expect(thanks("ana@acme.com")).toContain("ana@acme.com");
  });

  it("write the name in lower case", () => {
    expect(JSON.stringify([sales, enterprise])).not.toMatch(/Shpyrd/);
  });
});
