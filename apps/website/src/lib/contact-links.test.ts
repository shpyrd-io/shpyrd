import { existsSync, readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import { contact, enterpriseContact } from "@shpyrd/content/site/offer";
import { pricing } from "@shpyrd/content/site/pricing";

const app = fileURLToPath(new URL("../../app", import.meta.url));
const pricingSource = fileURLToPath(new URL("../components/pricing.tsx", import.meta.url));

describe("the contact links", () => {
  it("lead to the contact pages, which exist", () => {
    expect(contact.href).toBe("/contact/sales");
    expect(enterpriseContact.href).toBe("/contact/enterprise");
    for (const href of [contact.href, enterpriseContact.href]) {
      expect(existsSync(`${app}${href}/page.tsx`), href).toBe(true);
    }
  });

  it("read Contact us, and nowhere Talk to us", () => {
    expect(pricing.enterprise.action.label).toBe("Contact us");
    expect(readFileSync(pricingSource, "utf8")).not.toMatch(/Talk to us/);
  });
});
