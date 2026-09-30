"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { Menu, Moon, Sun } from "lucide-react";
import { AnchoredOverlay } from "@shpyrd/ui/components/anchored-overlay";
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
// The marketing pages are not documents: they get the wordmark and a few
// links, not the whole documentation tree down the side.
const marketing = [
  { title: "How sharing works", href: "/how-sharing-works" },
  { title: "Bring an app", href: "/bring-an-app" },
  { title: "Docs", href: "/docs/getting-started" },
];

export function Shell({ children }: { children: React.ReactNode }) {
  const path = usePathname();
  const here = find(path);
  const isDocument = path.startsWith("/docs");
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
      {isDocument && (
        <PageLayoutSidebar
          aria-label="Site"
          width="small"
          divider="line"
          sticky
          hidden={{ narrow: true }}
          className="pb-4"
        >
          <Stack direction="horizontal" align="center" gap="cozy" padding="normal">
            <Link href="/" aria-label="shpyrd">
              <Wordmark />
            </Link>
          </Stack>
          {nav}
        </PageLayoutSidebar>
      )}

      <PageLayoutHeader divider="line" className="px-4 py-3 @3xl/page-layout:px-6">
        <Stack direction="horizontal" align="center" gap="cozy">
          {isDocument ? (
            <>
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
              {/* The sidebar holds the wordmark, and it is hidden on a narrow
                  screen: without this a reader who arrives on a document has
                  no way to the rest of the site. */}
              <Link href="/" aria-label="shpyrd" className="@3xl/page-layout:hidden">
                <Wordmark />
              </Link>
              <Breadcrumbs>
                {here && <BreadcrumbsItem>{here.group.title}</BreadcrumbsItem>}
                {here && (
                  <BreadcrumbsItem asChild selected>
                    <Link href={here.link.href}>{here.link.title}</Link>
                  </BreadcrumbsItem>
                )}
              </Breadcrumbs>
            </>
          ) : (
            <>
              <Link href="/" aria-label="shpyrd">
                <Wordmark />
              </Link>
              <Stack direction="horizontal" align="center" gap="condensed" className="ml-4">
                {marketing.map((link) => (
                  <Button key={link.href} variant="ghost" size="sm" asChild>
                    <Link href={link.href}>{link.title}</Link>
                  </Button>
                ))}
              </Stack>
            </>
          )}
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
        shpyrd is open source under MPL-2.0, and in beta.
      </PageLayoutFooter>
    </PageLayout>
  );
}
