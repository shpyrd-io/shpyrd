import { compile } from "@mdx-js/mdx";
import frontmatter from "remark-frontmatter";
import gfm from "remark-gfm";
import editorial from "./remark.mjs";

// Tests compile the same editorial documents as Next, without a second format.
export function editorialMdx() {
  return {
    name: "editorial-mdx",
    enforce: "pre",
    async transform(source, id) {
      if (!id.endsWith(".mdx")) return;
      const result = await compile({ value: source, path: id }, {
        remarkPlugins: [frontmatter, gfm, editorial],
      });
      return { code: String(result), map: null };
    },
  };
}
