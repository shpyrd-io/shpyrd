"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { ArchiveRestore, Bot, Boxes, Briefcase, Cloud, Cloudy, Compass, Database, Download, FileCode, GitPullRequest, Globe, Handshake, Heart, LayoutDashboard, Lightbulb, LockKeyhole, Map as MapIcon, Menu, Moon, Network, Puzzle, Rocket, Ruler, ScrollText, ShieldCheck, SquareTerminal, Sun, SunMoon, Trophy, UploadCloud, UsersRound, Wrench } from "lucide-react";
import { AnchoredOverlay } from "@shpyrd/ui/components/anchored-overlay";
import { Breadcrumbs, BreadcrumbsItem } from "@shpyrd/ui/components/breadcrumbs";
import { LogoMark, Wordmark } from "@shpyrd/ui/components/brand";
import { Button } from "@shpyrd/ui/components/button";
import { Footer } from "@shpyrd/ui/components/footer";
import { MinimalFooter } from "@shpyrd/ui/components/minimal-footer";
import { NavList, NavListGroup, NavListItem } from "@shpyrd/ui/components/nav-list";
import {
  PageLayout,
  PageLayoutFooter,
  PageLayoutHeader,
  PageLayoutSidebar,
} from "@shpyrd/ui/components/page-layout";
import { SiteHeader } from "@shpyrd/ui/components/site-header";
import { glass } from "@shpyrd/ui/lib/glass";
import { cn } from "@shpyrd/ui/lib/cn";
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
// The legal pages, on their own site.
const legal = [
  { title: "Legal", href: "https://legal.shpyrd.io/" },
  { title: "Terms of Service", href: "https://legal.shpyrd.io/global/terms-of-service" },
  { title: "Privacy Policy", href: "https://legal.shpyrd.io/global/privacy-policy" },
];
const discord = site.discord.href;

// The icons of the header turn orange under the pointer, with no box behind.

// An icon for each page of the documentation, beside its name in the side
// panel: after the side menu of legal.shpyrd.io.
const docIcons: Record<string, React.ReactElement> = {
  "/docs/getting-started": <Rocket />,
  "/docs/concepts": <Lightbulb />,
  "/docs/tour": <Compass />,
  "/docs/deploying": <UploadCloud />,
  "/docs/shpyrd-yaml": <FileCode />,
  "/docs/resources": <Boxes />,
  "/docs/databases": <Database />,
  "/docs/domains": <Globe />,
  "/docs/app-access": <LockKeyhole />,
  "/docs/access": <ShieldCheck />,
  "/docs/mcp": <Bot />,
  "/docs/logs": <ScrollText />,
  "/docs/dashboard": <LayoutDashboard />,
  "/docs/cli": <SquareTerminal />,
  "/docs/installation": <Download />,
  "/docs/oracle-cloud": <Cloud />,
  "/docs/aws": <Cloudy />,
  "/docs/extensions": <Puzzle />,
  "/docs/backups": <ArchiveRestore />,
  "/docs/architecture-guide": <Network />,
  "/docs/design-principles": <Ruler />,
  "/docs/roadmap": <MapIcon />,
  "/docs/how-to-contribute": <GitPullRequest />,
  "https://donate.stripe.com/9B63cxfbwg8H31OgPX2ZO01": <Heart />,
};

const iconHover = "hover:bg-transparent hover:text-primary dark:hover:bg-transparent";

// What is around every page: the pages of the site at the side, where the
// page is over it, and the foot. All of it is made of design/ui; a page
// brings its content and, when it has one, the pane beside it.
// The marketing pages are not documents: they get the wordmark and a few
// links, not the whole documentation tree down the side.
// The homepage is for people who build with AI; the solutions are for the
// other people who choose shpyrd, each on a page of its own.
const solutions = [
  { title: "For developers", href: "/for/developers", icon: <SquareTerminal />, description: "Push code, get a URL, on shpyrd cloud." },
  { title: "For IT teams", href: "/for/it", icon: <UsersRound />, description: "One accepted place for the apps people build." },
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
  { title: "Pricing", href: "/pricing" },
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
              icon={docIcons[link.href]}
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
      // The aurora behind the call to action runs on under the foot: the
      // page cuts it off at its own bottom rather than growing for it.
      className="relative min-h-svh overflow-y-clip"
    >
      {isDocument && (
        <PageLayoutSidebar
          aria-label="Site"
          width="small"
          sticky
          hidden={{ narrow: true }}
          // Floats like the header: held off the edges, a panel of glass the
          // height of the window, scrolling inside itself.
          // Over the bar beside it, so the bar does not cut its shadow; 1.5rem
          // of room on its right, the panel keeping its 15rem.
          className="relative z-50 py-4 pr-6 pl-4 @3xl/page-layout:w-66 @3xl/page-layout:overflow-visible"
        >
          <div className={cn(glass, "flex h-full flex-col overflow-y-auto pb-4")}>
            <Stack direction="horizontal" align="center" gap="cozy" padding="normal">
              <Link href="/" aria-label="shpyrd">
                <Wordmark />
              </Link>
            </Stack>
            {nav}
          </div>
        </PageLayoutSidebar>
      )}

      <PageLayoutHeader className="sticky top-0 z-40">
        <SiteHeader
          as="div"
          sticky={false}
          // A document has its own floating panel at the side; its bar is
          // left loose over the page, with only a faint line under it.
          variant={isDocument ? "bar" : "floating"}
          // The line runs as far as what is in the bar: from the breadcrumbs to
          // the end of the last button, not from edge to edge. The bar is held
          // down so its words sit on the same line as the logo in the side panel.
          className={
            isDocument
              ? "border-b-0 bg-background/70 px-4 pt-4.5 md:px-6 [&>div:first-child]:border-b [&>div:first-child]:border-foreground/8 dark:[&>div:first-child]:border-foreground/20 [&>div:first-child]:px-0"
              : undefined
          }
          // A document has the sidebar at its left, and the whole room beside
          // it; the other pages hold their content to "xlarge", so the bar does.
          width={isDocument ? "full" : "xlarge"}
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
              <Button variant="ghost" size="icon" asChild aria-label="shpyrd on GitHub" className={iconHover}>
                <a href={github}>
                  <GitHubMark />
                </a>
              </Button>
              <Button variant="ghost" size="icon" asChild aria-label="shpyrd on Discord" className={iconHover}>
                <a href={discord}>
                  <DiscordMark />
                </a>
              </Button>
              <Button
                variant="ghost"
                size="icon"
                className={iconHover}
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
                <AddToAgent manual={false} />
              </div>
            </>
          }
        />
      </PageLayoutHeader>

      {children}

      <PageLayoutFooter>
        {path === "/" ? (
          // The home's foot is the full one: the brand and the way in, the
          // site's map in columns, the fine print. Every other page has the
          // small one.
          <Footer
            as="div"
            width="xlarge"
            className="border-t-0 px-4 md:px-6 [&>div:first-child]:max-w-[calc(80rem-3rem)] [&>div:first-child]:border-t [&>div:first-child]:border-foreground/8 [&>div:first-child]:px-0 dark:[&>div:first-child]:border-foreground/20"
            logo={
              <Link href="/" aria-label="shpyrd">
                <Wordmark className="h-6" />
              </Link>
            }
            tagline="You built it. We ship it: online, behind a sign-in, for the people who need it."
            action={<AddToAgent manual={false} />}
            columns={[
              ...(sectionsLive
                ? [
                    { title: "Solutions", links: solutions.map((l) => <Link key={l.href} href={l.href}>{l.title.replace(/^For (\w)/, (_, c: string) => c.toUpperCase())}</Link>) },
                    { title: "Use cases", links: useCases.map((l) => <Link key={l.href} href={l.href}>{l.title}</Link>) },
                  ]
                : []),
              {
                title: "Product",
                links: [
                  ...marketing.map((l) => <Link key={l.href} href={l.href}>{l.title}</Link>),
                  <Link key="/docs/roadmap" href="/docs/roadmap">Roadmap</Link>,
                ],
              },
              {
                title: "Community",
                links: [
                  <a key="gh" href={github}>GitHub</a>,
                  <a key="dc" href={discord}>Discord</a>,
                ],
              },
            ]}
            // The fine print, and the legal pages beside it, as on every
            // other page's foot.
            note={
              <span className="flex flex-wrap items-center gap-x-4 gap-y-1">
                <span>© 2026 shpyrd. All rights reserved.</span>
                {legal.map((l) => (
                  <a key={l.href} href={l.href} className="underline-offset-4 transition-colors hover:text-primary">
                    {l.title}
                  </a>
                ))}
              </span>
            }
            backToTop
          />
        ) : (
        <MinimalFooter
            as="div"
            // Loose like a document's bar, on every page: a faint line over it,
            // as wide as what is in it, rather than a panel of glass.
            variant="line"
            // Off the docs, it lines up with the page's text: the 80rem of the
            // page less the room the page keeps on each side.
            className={cn(
              "border-t-0 px-4 md:px-6 [&>div:first-child]:border-t [&>div:first-child]:border-foreground/8 dark:[&>div:first-child]:border-foreground/20 [&>div:first-child]:px-0",
              !isDocument && "[&>div:first-child]:max-w-[calc(80rem-3rem)]",
            )}
            width={isDocument ? "full" : "xlarge"}
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
            // The fine print, and the legal pages beside it.
            note={
              <span className="flex flex-wrap items-center gap-x-4 gap-y-1">
                <span>© 2026. shpyrd. All rights reserved.</span>
                {legal.map((l) => (
                  <a key={l.href} href={l.href} className="underline-offset-4 transition-colors hover:text-primary">
                    {l.title}
                  </a>
                ))}
              </span>
            }
            backToTop
          />
        )}
      </PageLayoutFooter>
    </PageLayout>
  );
}
