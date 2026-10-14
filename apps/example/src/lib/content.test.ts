import { describe, expect, it } from "vitest";
import { documents, read, slug } from "./content";

describe("the texts of the site", () => {
  it("names a heading by its words", () => {
    expect(slug("What you get")).toBe("what-you-get");
    expect(slug("Olá, mundo!")).toBe("ola-mundo");
  });

  it("finds every document of the site and reads each", async () => {
    const all = documents();
    expect(all.length).toBeGreaterThan(20);
    expect(all).not.toContain("index");
    for (const name of all) expect((await read(`docs/${name}`)).title).not.toBe("");
  });

  it("lists the headings of a text, each with a name for its address", async () => {
    const { headings } = await read("docs/getting-started");
    expect(headings.map((h) => h.id)).toContain("what-you-get");
  });
});
