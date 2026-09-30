"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { SegmentedNav } from "@shpyrd/ui/components/segmented-nav";

// The subpages of a solution or a use case, as a bar over them. It lives in the
// section's layout, so it stays while its pages change and the light slides
// from one to the next. A subpage with no slug is the section's own page.
export function SectionTabs({
  base,
  label,
  pages,
}: {
  base: string;
  label: string;
  pages: { title: string; slug: string }[];
}) {
  const path = usePathname().replace(/\/$/, "");
  return (
    <SegmentedNav
      aria-label={label}
      links={pages.map(({ title, slug }) => {
        const href = slug ? `${base}/${slug}` : base;
        return (
          <Link key={href} href={href} aria-current={path === href ? "page" : undefined}>
            {title}
          </Link>
        );
      })}
    />
  );
}
