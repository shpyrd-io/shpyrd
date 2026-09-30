import { describe, expect, it } from "vitest";
import { documents, meta, read, slug } from "./content";

describe("the texts of the site", () => {
  it("reads the front matter as names and their values", () => {
    expect(meta('title: Installation\ndescription: "Create a cluster"')).toEqual({
      title: "Installation",
      description: "Create a cluster",
    });
  });

  it("names a heading by its words", () => {
    expect(slug("What you get")).toBe("what-you-get");
    expect(slug("Olá, mundo!")).toBe("ola-mundo");
  });

  it("finds every document of the site and reads each", () => {
    const all = documents();
    expect(all.length).toBeGreaterThan(20);
    for (const name of all) expect(read(name).title).not.toBe("");
  });

  it("lists the headings of a text, each with a name for its address", () => {
    const { headings } = read("getting-started");
    expect(headings.map((h) => h.id)).toContain("what-you-get");
  });

  it("gives every document a title and a description", () => {
    for (const name of documents()) {
      const text = read(name);
      expect(text.title, `${name} has no title`).toBeTruthy();
      expect(text.description, `${name} has no description`).toBeTruthy();
    }
  });

  it("gives two headings of the same words two different names", () => {
    const seen = new Map<string, number>();
    expect(slug("Status", seen)).toBe("status");
    expect(slug("Status", seen)).toBe("status-2");
    expect(slug("Status", seen)).toBe("status-3");
  });

  it("never names two headings of one document the same", () => {
    for (const name of documents()) {
      const ids = read(name).headings.map((h) => h.id);
      expect(new Set(ids).size, `${name} repeats a heading name`).toBe(ids.length);
    }
  });

  it("keeps a heading name unique even when another heading spells the suffix", () => {
    const seen = new Map<string, number>();
    expect(slug("Status", seen)).toBe("status");
    expect(slug("Status 2", seen)).toBe("status-2");
    expect(slug("Status", seen)).not.toBe("status-2");
  });

  it("reads a description written as a folded scalar", () => {
    expect(meta("title: Tour\ndescription: >\n  A long sentence\n  over two lines.")).toEqual({
      title: "Tour",
      description: "A long sentence over two lines.",
    });
  });

  it("leaves the pictures of a text where the site serves them", () => {
    const html = JSON.stringify(read("tour").content);
    expect(html).not.toMatch(/https:\/\/shpyrd\.io/);
    expect(html).toMatch(/\/screenshots\//);
  });
});
