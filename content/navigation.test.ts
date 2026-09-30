import fs from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";
import { navigation } from "./navigation";

const docs = fs
  .readdirSync(path.join(import.meta.dirname, "docs"))
  .filter((f) => f.endsWith(".md"))
  .map((f) => `/docs/${f.replace(/\.md$/, "")}`);

const listed = navigation
  .flatMap((group) => group.links.map((link) => link.href))
  .filter((href) => href.startsWith("/docs/"));

describe("the navigation of the site", () => {
  it("names only documents that exist", () => {
    for (const href of listed) expect(docs, `${href} is listed and missing`).toContain(href);
  });

  it("leaves no document unreachable", () => {
    for (const href of docs) expect(listed, `${href} exists and is not listed`).toContain(href);
  });

  it("names each document once", () => {
    expect(new Set(listed).size).toBe(listed.length);
  });
});
