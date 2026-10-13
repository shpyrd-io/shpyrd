import { copy } from "@shpyrd/content/docs/index.mdx";
export type Link = { title: string; href: string };
export type Group = { title: string; links: Link[] };
export const { navigation } = copy as { navigation: Group[] };
/** The group and the link of an address, when the navigation has it. */
export function find(path: string): { group: Group; link: Link } | undefined {
  const here = path.replace(/\/$/, "") || "/";
  for (const group of navigation) {
    const link = group.links.find((l) => l.href === here);
    if (link) return { group, link };
  }
  return undefined;
}
