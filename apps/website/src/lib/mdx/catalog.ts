import "server-only";
import { readdirSync } from "node:fs";
import path from "node:path";
import type { ComponentType } from "react";
import type { MDXProps } from "mdx/types";
import { copy } from "@shpyrd/content/site/solutions/groups.mdx";
import { editorialDesigns } from "./designs";
import type { FeatureEntry, Frontmatter } from "./types";
import type { Heading } from "../content";

export type Collection = "features" | "solutions" | "docs";
type Document = { default: ComponentType<MDXProps>; frontmatter: Frontmatter; headings: Heading[] };
export type SolutionGroup = { name: string; items: FeatureEntry[] };
const directories = {
  features: path.resolve(process.cwd(), "../../content/site/features"),
  solutions: path.resolve(process.cwd(), "../../content/site/solutions"),
  docs: path.resolve(process.cwd(), "../../content/docs"),
};
export function slugs(collection: Collection): string[] {
  return readdirSync(directories[collection])
    .filter((file) => file.endsWith(".mdx") && file !== "groups.mdx" && file !== "index.mdx")
    .map((file) => file.slice(0, -4)).sort();
}
export async function document(collection: Collection, slug: string): Promise<Document | undefined> {
  if (!slugs(collection).includes(slug)) return undefined;
  // Literal collection paths let Next watch every MDX file and refresh on save.
  if (collection === "features") return import(`../../../../../content/site/features/${slug}.mdx`);
  if (collection === "solutions") return import(`../../../../../content/site/solutions/${slug}.mdx`);
  return import(`../../../../../content/docs/${slug}.mdx`);
}
export async function menus() {
  const entries = async (collection: "features" | "solutions") => Promise.all(slugs(collection).map(async (slug) => {
    const { frontmatter } = (await document(collection, slug))!;
    return { slug, name: frontmatter.menu!.label, card: frontmatter.menu!.description,
      icon: editorialDesigns[slug]?.icon ?? "rocket" as const,
      order: frontmatter.menu!.order, group: frontmatter.menu!.group };
  }));
  const [features, solutions] = await Promise.all([entries("features"), entries("solutions")]);
  const groups = (copy as { groups: { id: string; name: string; order: number }[] }).groups;
  return { features: features.sort((a, b) => a.order - b.order),
    solutionGroups: [...groups].sort((a, b) => a.order - b.order).map(({ id, name }) => ({
      name, items: solutions.filter((entry) => entry.group === id).sort((a, b) => a.order - b.order),
    })) };
}
