import type { Link } from "@/api/types";

// The links extensions add to the sidebar (RFC-0083), one group per
// section, in the order the server gave them.
export function bySection(links: Link[]): [string, Link[]][] {
  const groups = new Map<string, Link[]>();
  for (const link of links) {
    groups.set(link.section, [...(groups.get(link.section) ?? []), link]);
  }
  return [...groups.entries()];
}
