"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { Bot, Briefcase, Handshake, Menu, Moon, ShieldCheck, SquareTerminal, Sun, SunMoon, Trophy, Wrench } from "lucide-react";
import { AnchoredOverlay } from "@shpyrd/ui/components/anchored-overlay";
import { Breadcrumbs, BreadcrumbsItem } from "@shpyrd/ui/components/breadcrumbs";
import { LogoMark, Wordmark } from "@shpyrd/ui/components/brand";
import { Button } from "@shpyrd/ui/components/button";
import { MinimalFooter } from "@shpyrd/ui/components/minimal-footer";
import { NavList, NavListGroup, NavListItem } from "@shpyrd/ui/components/nav-list";
import {
  PageLayout,
  PageLayoutFooter,
  PageLayoutHeader,
  PageLayoutSidebar,
} from "@shpyrd/ui/components/page-layout";
import { SiteHeader } from "@shpyrd/ui/components/site-header";
import { Stack } from "@shpyrd/ui/components/stack";
import { useTheme } from "@shpyrd/ui/lib/theme";
import { find, navigation } from "@shpyrd/content/navigation";
import * as site from "@shpyrd/content/site/offer";
import { AddToAgent } from "@/components/add-to-agent";
import { sectionsLive, signInLive } from "@/lib/sections";
import { DiscordMark, GitHubMark } from "@/components/marks";

// Where the code is, and where the community talks; the header and the foot
// both point there.
const github = "https://github.com/shpyrd-io/shpyrd";
const discord = site.discord.href;

// What is around every page: the pages of the site at the side, where the
// page is over it, and the foot. All of it is made of design/ui; a page
// brings its content and, when it has one, the pane beside it.
// The marketing pages are not documents: they get the wordmark and a few
// links, not the whole documentation tree down the side.
// The homepage is for people who build with AI; the solutions are for the
// other people who choose shpyrd, each on a page of its own.
const solutions = [
  { title: "For developers", href: "/for/developers", icon: <SquareTerminal />, description: "Push code, get a URL, on shpyrd cloud." },
  { title: "For IT teams", href: "/for/it", icon: <ShieldCheck />, description: "One accepted place for the apps people build." },
  { title: "For FDE partners", href: "/for/fde-partners", icon: <Handshake />, description: "The same setup behind every client delivery." },
];

// What people put in it, one page each: the other way into the same product.
const useCases = [
  { title: "Internal tools", href: "/use-cases/internal-tools", icon: <Wrench />, description: "Trackers, dashboards and approvals, in your team's day." },
  { title: "Apps from a hackathon", href: "/use-cases/hackathon-apps", icon: <Trophy />, description: "Keep the few people want, from Monday on." },
  { title: "Agents and workers", href: "/use-cases/agents-and-workers", icon: <Bot />, description: "What runs without a web page, off your laptop." },
  { title: "Client apps", href: "/use-cases/client-apps", icon: <Briefcase />, description: "Built by you, opened with their own sign-in." },
];

// How sharing works is one of the sections not shown yet (src/lib/sections.ts).
const marketing = [
  ...(sectionsLive ? [{ title: "How sharing works", href: "/how-sharing-works" }] : []),
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

  // A solution's proposals live under its address, so they mark it too.
  const link = (l: { title: string; href: string }) => (
    <Link
      key={l.href}
      href={l.href}
      aria-current={path === l.href || path.startsWith(`${l.href}/`) ? "page" : undefined}
    >
      {l.title}
    </Link>
  );
  // The Solutions menu waits with the sections (src/lib/sections.ts).
  const solutionsMenu = {
      label: "Solutions",
      columns: [
        {
          label: "For",
          // The column is headed "For", so its items do not say it again.
          links: solutions.map((l) => ({
            link: link({ ...l, title: l.title.replace(/^For /, "") }),
            description: l.description,
            icon: l.icon,
          })),
        },
        {
          label: "What you're shipping",
          links: useCases.map((l) => ({ link: link(l), description: l.description, icon: l.icon })),
        },
      ],
    };
  const links = [...(sectionsLive ? [solutionsMenu] : []), ...marketing.map(link)];

  const siteNav = (
    <NavList aria-label="Site">
      {sectionsLive && (
      <NavListGroup title="Solutions">
        {solutions.map((link) => (
          <NavListItem key={link.href} asChild>
            <Link href={link.href}>{link.title}</Link>
          </NavListItem>
        ))}
      </NavListGroup>
      )}
      {sectionsLive && (
      <NavListGroup title="What you're shipping">
        {useCases.map((link) => (
          <NavListItem key={link.href} asChild>
            <Link href={link.href}>{link.title}</Link>
          </NavListItem>
        ))}
      </NavListGroup>
      )}
      <NavListGroup title="shpyrd">
        {marketing.map((link) => (
          <NavListItem key={link.href} asChild>
            <Link href={link.href}>{link.title}</Link>
          </NavListItem>
        ))}
      </NavListGroup>
    </NavList>
  );

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

      <PageLayoutHeader className="sticky top-0 z-40">
        <SiteHeader
          as="div"
          sticky={false}
          // A document has the sidebar at its left, and the whole room beside
          // it; the other pages hold their content to "large", so the bar does.
          width={isDocument ? "full" : "large"}
          start={
            isDocument ? (
              <>
                <AnchoredOverlay
                  open={menu}
                  onOpenChange={setMenu}
                  width="small"
                  className="px-0 py-3"
                  anchor={
                    <Button
                      variant="ghost"
                      size="icon"
                      icon={<Menu />}
                      aria-label="Documentation"
                      className="@3xl/page-layout:hidden"
                    />
                  }
                >
                  {siteNav}
                  {nav}
                </AnchoredOverlay>
                {/* The sidebar holds the wordmark, and it is hidden on a narrow
                    screen: without this a reader who arrives on a document has
                    no way to the rest of the site. */}
                <Link href="/" aria-label="shpyrd" className="@3xl/page-layout:hidden">
                  <Wordmark />
                </Link>
                <Breadcrumbs className="hidden @xl/site-header:flex">
                  {here && <BreadcrumbsItem>{here.group.title}</BreadcrumbsItem>}
                  {here && (
                    <BreadcrumbsItem asChild selected>
                      <Link href={here.link.href}>{here.link.title}</Link>
                    </BreadcrumbsItem>
                  )}
                </Breadcrumbs>
              </>
            ) : (
              <Link href="/" aria-label="shpyrd">
                <Wordmark />
              </Link>
            )
          }
          // A document has the documentation down its side and the site in its
          // menu; two menu buttons on a phone would be one too many.
          links={isDocument ? [] : links}
          actions={
            <>
              <Button variant="ghost" size="icon" asChild aria-label="shpyrd on GitHub">
                <a href={github}>
                  <GitHubMark />
                </a>
              </Button>
              <Button variant="ghost" size="icon" asChild aria-label="shpyrd on Discord">
                <a href={discord}>
                  <DiscordMark />
                </a>
              </Button>
              <Button
                variant="ghost"
                size="icon"
                aria-label={`Theme: ${theme}`}
                title={`Theme: ${theme}`}
                icon={theme === "dark" ? <Moon /> : theme === "light" ? <Sun /> : <SunMoon />}
                onClick={() => setTheme(next[theme])}
              />
              {/* Back to the dashboard, for whoever has a workspace already.
                  Hidden until it has somewhere to go (src/lib/sections.ts). */}
              {signInLive && (
                <Button variant="outline" asChild className="ml-2">
                  <a href="#" onClick={(event) => event.preventDefault()}>
                    Sign in
                  </a>
                </Button>
              )}
              {/* The way in, where a phone has room for it. Under that it is
                  the hero's to offer. */}
              <div className="ml-2 hidden @xl/site-header:block">
                <AddToAgent />
              </div>
            </>
          }
        />
      </PageLayoutHeader>

      {children}

      <PageLayoutFooter>
        <MinimalFooter
          as="div"
          width={isDocument ? "full" : "large"}
          links={[...(sectionsLive ? solutions : []), ...marketing].map((l) => (
            <Link key={l.href} href={l.href}>
              {l.title}
            </Link>
          ))}
          social={[
            { label: "shpyrd on GitHub", href: github, icon: <GitHubMark /> },
            { label: "shpyrd on Discord", href: discord, icon: <DiscordMark /> },
          ]}
          logo={
            <Link href="/" aria-label="shpyrd">
              <LogoMark className="size-5" />
            </Link>
          }
          note="shpyrd is open source under MPL-2.0, and in beta."
          backToTop
        />
      </PageLayoutFooter>
    </PageLayout>
  );
}
