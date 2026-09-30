"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { Menu, Moon, Sun } from "lucide-react";
import { AnchoredOverlay } from "@shpyrd/ui/components/anchored-overlay";
import { Badge } from "@shpyrd/ui/components/badge";
import { Breadcrumbs, BreadcrumbsItem } from "@shpyrd/ui/components/breadcrumbs";
import { Wordmark } from "@shpyrd/ui/components/brand";
import { Button } from "@shpyrd/ui/components/button";
import { NavList, NavListGroup, NavListItem } from "@shpyrd/ui/components/nav-list";
import {
  PageLayout,
  PageLayoutFooter,
  PageLayoutHeader,
  PageLayoutSidebar,
} from "@shpyrd/ui/components/page-layout";
import { Stack } from "@shpyrd/ui/components/stack";
import { useTheme } from "@shpyrd/ui/lib/theme";
import { find, navigation } from "@shpyrd/content/navigation";

// What is around every page: the pages of the site at the side, where the
// page is over it, and the foot. All of it is made of design/ui; a page
// brings its content and, when it has one, the pane beside it.
export function Shell({ children }: { children: React.ReactNode }) {
  const path = usePathname();
  const here = find(path);
  const [theme, setTheme] = useTheme();
  const next = { light: "dark", dark: "system", system: "light" } as const;
  const [menu, setMenu] = useState(false);
  useEffect(() => setMenu(false), [path]);

  const nav = (
    <NavList aria-label="Documentation">
      {navigation.map((group) => (
        <NavListGroup key={group.title} title={group.title}>
          {group.links.map((link) => (
            <NavListItem
              key={link.href}
              asChild
              aria-current={here?.link === link ? "page" : undefined}
            >
              <Link href={link.href}>{link.title}</Link>
            </NavListItem>
          ))}
        </NavListGroup>
      ))}
    </NavList>
  );

  return (
    <PageLayout
      containerWidth="full"
      padding="none"
      columnGap="none"
      rowGap="none"
      className="min-h-svh"
    >
      <PageLayoutSidebar
        aria-label="Site"
        width="small"
        divider="line"
        sticky
        hidden={{ narrow: true }}
        className="pb-4"
      >
        <Stack direction="horizontal" align="center" gap="cozy" padding="normal">
          <Link href="/" aria-label="Getting started">
            <Wordmark />
          </Link>
          <Badge variant="outline">example</Badge>
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
            anchor={
              <Button
                variant="outline"
                size="icon"
                icon={<Menu />}
                aria-label="Documentation"
                className="@3xl/page-layout:hidden"
              />
            }
          >
            {nav}
          </AnchoredOverlay>
          <Breadcrumbs>
            {here && <BreadcrumbsItem>{here.group.title}</BreadcrumbsItem>}
            {here && (
              <BreadcrumbsItem asChild selected>
                <Link href={here.link.href}>{here.link.title}</Link>
              </BreadcrumbsItem>
            )}
          </Breadcrumbs>
          <Button
            variant="outline"
            size="sm"
            className="ml-auto"
            icon={theme === "dark" ? <Moon /> : <Sun />}
            onClick={() => setTheme(next[theme])}
          >
            Theme: {theme}
          </Button>
        </Stack>
      </PageLayoutHeader>

      {children}

      <PageLayoutFooter
        divider="line"
        className="px-4 py-4 text-sm text-muted-foreground @3xl/page-layout:px-6"
      >
        An example of a site made of design/ui. The texts are those of shpyrd.io, read from
        apps/website as they are.
      </PageLayoutFooter>
    </PageLayout>
  );
}
