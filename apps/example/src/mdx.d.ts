declare module "*.mdx" {
  import type { ComponentType } from "react";
  import type { MDXProps } from "mdx/types";
  export const copy: unknown;
  export const frontmatter: { title: string; description: string };
  export const headings: { id: string; title: string; level: number }[];
  const Content: ComponentType<MDXProps>;
  export default Content;
}
