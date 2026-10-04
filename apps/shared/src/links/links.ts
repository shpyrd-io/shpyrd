// The links extensions add to a sidebar (GET /api/links): each goes in its
// section after the page it names, or at the end of its section; a section
// the sidebar does not have comes after the sidebar's own. A link opens
// another application, as a page of its own: it carries its way back.

export type Link = {
  area?: "workspace" | "console";
  section: string;
  label: string;
  url: string;
  icon?: string;
  after?: string;
};

export type Entry<P> = { page: P; link?: undefined } | { link: Link; page?: undefined };

export type Group<P> = { group: string; entries: Entry<P>[] };

// placeLinks lays the sidebar out: the pages in their groups, every page
// the sidebar knows (one hidden to this person still holds its place, so a
// link can follow it), and the links in place. Hide pages afterwards.
export function placeLinks<P extends { group: string; slug: string }>(pages: readonly P[], links: readonly Link[]): Group<P>[] {
  const groups: Group<P>[] = [];
  const byName = new Map<string, Group<P>>();
  const groupOf = (name: string) => {
    let g = byName.get(name);
    if (!g) {
      g = { group: name, entries: [] };
      byName.set(name, g);
      groups.push(g);
    }
    return g;
  };
  for (const page of pages) groupOf(page.group).entries.push({ page });
  for (const link of links) {
    const g = groupOf(link.section);
    let at = link.after ? g.entries.findIndex((e) => e.page?.slug === link.after) : -1;
    if (at < 0) {
      g.entries.push({ link });
      continue;
    }
    // After the page, and after the links already put after it, in the
    // order the server gave them.
    at++;
    while (at < g.entries.length && g.entries[at].link) at++;
    g.entries.splice(at, 0, { link });
  }
  return groups;
}

// shown keeps the pages the person may see, and the groups with something
// left in them.
export function shown<P>(groups: Group<P>[], visible: (page: P) => boolean): Group<P>[] {
  return groups
    .map((g) => ({ group: g.group, entries: g.entries.filter((e) => e.link || visible(e.page!)) }))
    .filter((g) => g.entries.length > 0);
}
