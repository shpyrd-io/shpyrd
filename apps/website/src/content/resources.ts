import { copy } from "@shpyrd/content/site/resources.mdx";

export const resources = copy as {
  label: string;
  groups: { title: string; links: { title: string; href: string; description: string }[] }[];
};
