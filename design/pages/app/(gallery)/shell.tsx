"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { LayoutGrid, Menu, Moon, Sun } from "lucide-react";
import { AnchoredOverlay } from "@shpyrd/ui/components/anchored-overlay";
import { Badge } from "@shpyrd/ui/components/badge";
import { Breadcrumbs, BreadcrumbsItem } from "@shpyrd/ui/components/breadcrumbs";
import { Wordmark } from "@shpyrd/ui/components/brand";
import { Button } from "@shpyrd/ui/components/button";
import { NavList, NavListGroup, NavListItem } from "@shpyrd/ui/components/nav-list";
import { PageHeading } from "@shpyrd/ui/components/page-heading";
import { PageLayout, PageLayoutContent, PageLayoutHeader, PageLayoutSidebar } from "@shpyrd/ui/components/page-layout";
import { useTheme } from "@shpyrd/ui/lib/theme";
import { Stack } from "@shpyrd/ui/components/stack";
import { pages } from "@/src/pages";

// What is around every page of the gallery, as in design/ui: the pages at
// the side, the header over the page. The theme is the gallery's; each
// page is shown in both.
export function Shell({ children }: { children: React.ReactNode }) {
  const path = usePathname();
  const name = path.replace(/^\//, "");
  const here = pages[name];
  const [theme, setTheme] = useTheme();
  const next = { light: "dark", dark: "system", system: "light" } as const;
  const [menu, setMenu] = useState(false);
  useEffect(() => setMenu(false), [path]);

  const nav = (
    <NavList aria-label="Pages">
      <NavListItem asChild icon={<LayoutGrid />} aria-current={path === "/" ? "page" : undefined}>
        <Link href="/">Overview</Link>
      </NavListItem>
      <NavListGroup title="Pages">
        {Object.entries(pages).map(([slug, page]) => (
          <NavListItem key={slug} asChild aria-current={slug === name ? "page" : undefined}>
            <Link href={`/${slug}`}>{page.title}</Link>
          </NavListItem>
        ))}
      </NavListGroup>
    </NavList>
  );

  return (
    <PageLayout containerWidth="full" padding="none" columnGap="none" rowGap="none" className="min-h-svh">
      <PageLayoutSidebar aria-label="Gallery" width="small" divider="line" sticky hidden={{ narrow: true }} className="pb-4">
        <Stack direction="horizontal" align="center" gap="cozy" padding="normal">
          <Link href="/" aria-label="Overview">
            <Wordmark />
          </Link>
          <Badge variant="outline">design/pages</Badge>
        </Stack>
        {nav}
      </PageLayoutSidebar>

      <PageLayoutHeader divider="line" className="px-4 py-3 @3xl/page-layout:px-6">
        <Stack direction="horizontal" align="center" gap="cozy">
          <AnchoredOverlay
            open={menu}
            onOpenChange={setMenu}
            width="small"
            className="px-0 py-3"
            anchor={<Button variant="outline" size="icon" icon={<Menu />} aria-label="Pages" className="@3xl/page-layout:hidden" />}
          >
            {nav}
          </AnchoredOverlay>
          <Breadcrumbs>
            <BreadcrumbsItem asChild selected={!here}>
              <Link href="/">Overview</Link>
            </BreadcrumbsItem>
            {here && (
              <BreadcrumbsItem asChild selected>
                <Link href={`/${name}`}>{here.title}</Link>
              </BreadcrumbsItem>
            )}
          </Breadcrumbs>
          <Button variant="outline" size="sm" className="ml-auto" icon={theme === "dark" ? <Moon /> : <Sun />} onClick={() => setTheme(next[theme])}>
            Theme: {theme}
          </Button>
        </Stack>
      </PageLayoutHeader>

      <PageLayoutContent width="xlarge" padding="normal" className="grid content-start gap-10">
        {here && <PageHeading title={here.title} description={here.about} />}
        {children}
      </PageLayoutContent>
    </PageLayout>
  );
}
