import { describe, expect, it } from "vitest";
import { enterprise, privacy, sales, thanks } from "./contact";
import { pricing } from "./pricing";

describe("the contact pages", () => {
  it("ask what the spec says, in its order", () => {
    expect(sales.fields.map((f) => f.name)).toEqual(["firstName", "lastName", "email", "message"]);
    expect(enterprise.fields.map((f) => f.name)).toEqual([
      "firstName", "lastName", "email", "company", "jobTitle", "size", "message",
    ]);
  });

  it("give every select its choices", () => {
    for (const f of [...sales.fields, ...enterprise.fields]) {
      if (f.type === "select") expect(f.choices?.length, f.name).toBeGreaterThan(1);
    }
  });

  it("say beside the enterprise form what the Enterprise plan adds", () => {
    expect(enterprise.beside.items).toEqual(pricing.enterprise.features);
  });

  it("link the privacy policy the footer links", () => {
    expect(privacy.href).toBe("https://legal.shpyrd.io/global/privacy-policy");
  });

  it("ask both forms' question in the person's own words, and require it", () => {
    for (const form of [sales, enterprise]) {
      const message = form.fields.find((f) => f.name === "message");
      expect(message).toMatchObject({ label: "How can we help you?", type: "textarea", required: true });
    }
  });

  it("ask for the company's email on both forms", () => {
    for (const form of [sales, enterprise]) {
      expect(form.fields.find((f) => f.name === "email")?.label).toBe("Company email");
    }
  });

  it("ask the company's size in employees, from the smallest to the largest", () => {
    const size = enterprise.fields.find((f) => f.name === "size");
    expect(size?.choices?.map((c) => c.label)).toEqual([
      "1–10 employees", "11–50 employees", "51–200 employees", "201–500 employees",
      "501–1,000 employees", "1,001–5,000 employees", "5,000+ employees",
    ]);
  });

  it("thank the person by the address they gave", () => {
    expect(thanks("ana@acme.com")).toContain("ana@acme.com");
  });

  it("write the name in lower case", () => {
    expect(JSON.stringify([sales, enterprise])).not.toMatch(/Shpyrd/);
  });
});
