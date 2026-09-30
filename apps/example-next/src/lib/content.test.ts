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
});
