import { describe, expect, it } from "vitest";
import { compile, evaluate } from "@mdx-js/mdx";
import frontmatter from "remark-frontmatter";
import gfm from "remark-gfm";
import * as runtime from "react/jsx-runtime";
import { readFileSync, readdirSync } from "node:fs";
import path from "node:path";
import editorial from "./remark.mjs";
import { readCopyFile } from "./read.mjs";

const options = { remarkPlugins: [frontmatter, gfm, editorial] };
const header = '---\ntitle: Sample\ndescription: A document\n---\n\n';
const root = path.resolve(import.meta.dirname, "../../../../content");

describe("editorial MDX", () => {
  it("compiles every content document without application code", async () => {
    const files = readdirSync(root, { recursive: true }).filter((file) => String(file).endsWith(".mdx"));
    expect(files.length).toBeGreaterThan(70);
    for (const file of files) {
      const filename = path.join(root, String(file));
      await compile({ path: filename, value: readFileSync(filename, "utf8") }, options);
    }
  });
  it.each(['import x from "x"', 'export const x = 1', '{process.env.SECRET}', '<script />', '<Lead className="red">Hi</Lead>', '[click](javascript:alert%281%29)'])("rejects non-editorial content: %s", async (body) => {
    await expect(compile(header + body, options)).rejects.toThrow();
  });
  it("exports folded metadata and collision-free anchors", async () => {
    const result = await evaluate('---\ntitle: Sample\ndescription: >\n  First line\n  second line\n---\n\n## Status\n\n## Status 2\n\n## Status\n', { ...runtime, ...options });
    expect(result.frontmatter).toEqual({ title: "Sample", description: "First line second line\n" });
    expect(result.headings).toEqual([
      { title: "Status", id: "status", level: 2 },
      { title: "Status 2", id: "status-2", level: 2 },
      { title: "Status", id: "status-3", level: 2 },
    ]);
  });
  it("preserves URL literals in content records, including query underscores", () => {
    const home = readCopyFile(path.join(root, "site/home.mdx"));
    const text = JSON.stringify(home);
    expect(text).toContain("utm_source=");
    expect(text).not.toContain("utm\\\\_source");
    const offer = readCopyFile(path.join(root, "site/offer.mdx"));
    expect(offer.signIn.href).toBe("https://accounts.shpyrd.io/signin");
  });
  it("keeps setup defaults and message steps in the compiled document", async () => {
    const result = String(await compile(header + '<AddToAgent />\n\n<Chat>\n<Message from="agent">\n<MessageSteps>\n- Deploy\n- Share\n</MessageSteps>\nDone.\n</Message>\n</Chat>', options));
    expect(result).toContain("manual: false");
    expect(result).toContain('"Deploy"');
    expect(result).not.toContain("MessageSteps:");
  });
});
