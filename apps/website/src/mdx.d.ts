declare module "*.mdx" {
  import type { ComponentType } from "react";
  import type { MDXProps } from "mdx/types";
  import type { Frontmatter } from "@/lib/mdx/types";
  import type { Heading } from "@/lib/content";
  export const copy: unknown;
  export const frontmatter: Frontmatter;
  export const headings: Heading[];
  const Content: ComponentType<MDXProps>;
  export default Content;
}
