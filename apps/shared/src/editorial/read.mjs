import { readFileSync } from "node:fs";
import { createProcessor } from "@mdx-js/mdx";
import frontmatter from "remark-frontmatter";
import gfm from "remark-gfm";
import { VFile } from "vfile";
import { readCopy } from "./copy.mjs";

// For Node configuration (before Next's loaders exist) and tool consumers.
// This reads the same Markdown records as the compile-time plugin, without eval.
export function readCopyFile(filename) {
  const file = new VFile({ path: filename, value: readFileSync(filename, "utf8") });
  const tree = createProcessor({ remarkPlugins: [frontmatter, gfm] }).parse(file);
  return readCopy(tree.children.filter((node) => node.type !== "yaml"), file);
}
