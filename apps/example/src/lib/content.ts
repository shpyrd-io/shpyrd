import type { ComponentType } from "react";
import type { MDXProps } from "mdx/types";
import { readdirSync } from "node:fs";
import path from "node:path";
import { notFound } from "next/navigation";
export type Heading = { id: string; title: string; level: number };
export { slug } from "@shpyrd/shared/editorial/heading-slug";
export function documents(): string[] {
  return readdirSync(path.resolve(process.cwd(), "../../content/docs"))
    .filter((file) => file.endsWith(".mdx") && file !== "index.mdx").map((file) => file.slice(0, -4)).sort();
}
export type Text = { title: string; description: string; Content: ComponentType<MDXProps>; headings: Heading[] };
export async function read(name: string): Promise<Text> {
  const slug = name.replace(/^docs\//, "");
  if (!documents().includes(slug)) notFound();
  const page = await import(`../../../../content/docs/${slug}.mdx`);
  return { ...page.frontmatter, Content: page.default, headings: page.headings as Heading[] };
}
