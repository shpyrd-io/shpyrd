// The pages of the site and their order, beside the texts they name.

export type Link = { title: string; href: string };
export type Group = { title: string; links: Link[] };

export const navigation: Group[] = [
  {
    title: "Introduction",
    links: [
      { title: "Getting started", href: "/docs/getting-started" },
      { title: "Tour", href: "/docs/tour" },
      { title: "Installation", href: "/docs/installation" },
      { title: "Oracle Cloud (OKE)", href: "/docs/oracle-cloud" },
      { title: "AWS (EKS)", href: "/docs/aws" },
      { title: "Concepts", href: "/docs/concepts" },
    ],
  },
  {
    title: "Using shpyrd",
    links: [
      { title: "Deploying", href: "/docs/deploying" },
      { title: "shpyrd.yaml", href: "/docs/shpyrd-yaml" },
      { title: "Resources", href: "/docs/resources" },
      { title: "Databases and caches", href: "/docs/databases" },
      { title: "Domains and exposure", href: "/docs/domains" },
      { title: "Sign-in for your app", href: "/docs/app-access" },
      { title: "AI assistants (MCP)", href: "/docs/mcp" },
      { title: "Logs", href: "/docs/logs" },
      { title: "Extensions and sign-in", href: "/docs/extensions" },
      { title: "Teams, roles and security", href: "/docs/access" },
      { title: "Platform backups", href: "/docs/backups" },
      { title: "Dashboard", href: "/docs/dashboard" },
      { title: "CLI reference", href: "/docs/cli" },
    ],
  },
  {
    title: "Project",
    links: [
      { title: "Architecture guide", href: "/docs/architecture-guide" },
      { title: "Design principles", href: "/docs/design-principles" },
      { title: "Roadmap", href: "/docs/roadmap" },
      { title: "How to contribute", href: "/docs/how-to-contribute" },
      {
        title: "Support the project",
        href: "https://donate.stripe.com/9B63cxfbwg8H31OgPX2ZO01",
      },
    ],
  },
];

/** The group and the link of an address, when the navigation has it. */
export function find(path: string): { group: Group; link: Link } | undefined {
  const here = path.replace(/\/$/, "") || "/";
  for (const group of navigation) {
    const link = group.links.find((l) => l.href === here);
    if (link) return { group, link };
  }
  return undefined;
}
