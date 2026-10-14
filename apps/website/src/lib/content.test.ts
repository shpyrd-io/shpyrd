import { describe, expect, it, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { createElement } from "react";
import { slug } from "./content";
import { document, slugs, menus } from "./mdx/catalog";
vi.mock("server-only", () => ({}));

describe("editorial content discovery", () => {
  it("discovers articles without treating copy indexes as documents", async () => {
    expect(slugs("docs")).toHaveLength(23);
    expect(slugs("docs")).not.toContain("index");
    expect(slugs("solutions")).not.toContain("groups");
    for (const name of slugs("docs")) {
      const doc = (await document("docs", name))!;
      expect(doc.frontmatter.title).toBeTruthy();
      expect(doc.frontmatter.description).toBeTruthy();
      expect(new Set(doc.headings.map((h) => h.id)).size).toBe(doc.headings.length);
    }
    expect(await document("docs", "../site/home")).toBeUndefined();
  });
  it("preserves readable unique heading anchors", () => {
    const seen = new Map<string, number>();
    expect(slug("Olá, mundo!")).toBe("ola-mundo");
    expect(slug("Status", seen)).toBe("status");
    expect(slug("Status 2", seen)).toBe("status-2");
    expect(slug("Status", seen)).toBe("status-3");
  });
  it("discovers all landings and keeps every solution in a menu group", async () => {
    const menu = await menus();
    expect(menu.features).toHaveLength(6);
    expect(menu.solutionGroups.flatMap((g) => g.items)).toHaveLength(12);
    for (const collection of ["features", "solutions"] as const) {
      for (const name of slugs(collection)) expect((await document(collection, name))!.frontmatter.layout).toBe("landing");
    }
  });
  it("keeps article screenshots local", async () => {
    const doc = (await document("docs", "tour"))!;
    const html = renderToStaticMarkup(createElement(doc.default, { components: {
      Callout: ({ children }: { children?: React.ReactNode }) => createElement("aside", {}, children),
      Fence: () => null,
    } }));
    expect(html).toContain("/screenshots/");
    expect(html).not.toContain("https://shpyrd.io/screenshots/");
  });
});
