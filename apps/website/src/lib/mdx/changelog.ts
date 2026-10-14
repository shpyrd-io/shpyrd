import "server-only";
import { readdirSync } from "node:fs";
import path from "node:path";
import type { ComponentType } from "react";
import type { MDXProps } from "mdx/types";
import { copy } from "@shpyrd/content/changelog/index.mdx";

export const changelogCopy = copy as {
  title: string; description: string;
  archive: string; loadMore: string; back: string; empty: string;
};
export type ChangelogPost = {
  slug: string; Content: ComponentType<MDXProps>;
  title: string; description: string; date: string;
  type: "feature" | "improvement" | "fix";
};
export async function changelogPosts(): Promise<ChangelogPost[]> {
  const files = readdirSync(path.resolve(process.cwd(), "../../content/changelog/entries"))
    .filter((file) => file.endsWith(".mdx"));
  const posts = await Promise.all(files.map(async (file) => {
    const name = file.slice(0, -4);
    const { default: Content, frontmatter: meta } = await import(`../../../../../content/changelog/entries/${name}.mdx`);
    if (meta.draft !== undefined && typeof meta.draft !== "boolean") throw new Error(`${file}: draft must be true or false`);
    if (meta.draft) return undefined;
    if (typeof meta.date !== "string" || !/^\d{4}-\d{2}-\d{2}$/.test(meta.date)
      || Number.isNaN(Date.parse(meta.date)) || new Date(meta.date).toISOString().slice(0, 10) !== meta.date) {
      throw new Error(`${file}: use a valid quoted date, such as "2026-10-10"`);
    }
    if (typeof meta.type !== "string" || !["feature", "improvement", "fix"].includes(meta.type)) throw new Error(`${file}: type must be feature, improvement or fix`);
    const slug = name.replace(/^\d{4}-\d{2}-\d{2}-/, "");
    return { slug, Content, title: meta.title, description: meta.description, date: meta.date, type: meta.type } as ChangelogPost;
  }));
  const published = posts.filter((post): post is ChangelogPost => post !== undefined);
  if (new Set(published.map((post) => post.slug)).size !== published.length) throw new Error("Changelog entries must have unique slugs");
  return published.sort((a, b) => b.date.localeCompare(a.date) || a.slug.localeCompare(b.slug));
}
export const changelogDate = (date: string, includeYear = true) => new Intl.DateTimeFormat("en-US", {
  month: "short", day: "numeric", year: includeYear ? "numeric" : undefined, timeZone: "UTC",
}).format(new Date(`${date}T00:00:00Z`));
