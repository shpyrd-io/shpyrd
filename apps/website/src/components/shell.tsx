"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { ArchiveRestore, Bot, Boxes, Cloud, Cloudy, Compass, Database, Download, FileCode, GitPullRequest, Globe, Heart, LayoutDashboard, Lightbulb, LockKeyhole, Map as MapIcon, Menu, Moon, Network, Puzzle, Rocket, Ruler, ScrollText, ShieldCheck, SquareTerminal, Sun, SunMoon, UploadCloud } from "lucide-react";
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
import { features } from "@shpyrd/content/site/features";
import { solutionGroups } from "@shpyrd/content/site/solutions";
import { DeployButton } from "@/components/deploy-button";
import { icons } from "@/components/landing-icons";
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

// The repository's stars beside its mark, as supabase.com has them: a short
// number (95, 1.2K), quiet, no pill. Asked of the site's own route
// (app/api/github), which asks GitHub at most once an hour.
const compact = new Intl.NumberFormat("en", { notation: "compact", maximumFractionDigits: 1 });

// Getting Started (/getting-started) is one of the sections not shown yet (src/lib/sections.ts).
const marketing = [
  ...(sectionsLive ? [{ title: "Getting Started", href: "/getting-started" }] : []),
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
  const [stars, setStars] = useState<number | null>(null);
  useEffect(() => {
    fetch("/api/github")
      .then((res) => (res.ok ? res.json() : null))
      .then((body) => typeof body?.stars === "number" && setStars(body.stars))
      .catch(() => {});
  }, []);

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
  // The two menus of the copy brief (arquivos de copy, 2026-10-09): what
  // shpyrd does, six features; and who and what it is for, in three groups.
  const entry = (base: string) => (p: { slug: string; name: string; card: string; icon: keyof typeof icons }) => ({
    link: link({ title: p.name, href: `${base}/${p.slug}` }),
    description: p.card,
    icon: icons[p.icon],
  });
  const featuresMenu = {
    label: "Features",
    columns: [{ label: "Features", links: features.map(entry("/features")) }],
  };
  const newSolutionsMenu = {
    label: "Solutions",
    columns: solutionGroups.map((g) => ({ label: g.name, links: g.items.map(entry("/solutions")) })),
  };
  const links = [featuresMenu, newSolutionsMenu, ...marketing.map(link)];

  const siteNav = (
    <NavList aria-label="Site">
      <NavListGroup title="Features">
        {features.map((f) => (
          <NavListItem key={f.slug} asChild>
            <Link href={`/features/${f.slug}`}>{f.name}</Link>
          </NavListItem>
        ))}
      </NavListGroup>
      {solutionGroups.map((g) => (
        <NavListGroup key={g.name} title={`Solutions · ${g.name}`}>
          {g.items.map((p) => (
            <NavListItem key={p.slug} asChild>
              <Link href={`/solutions/${p.slug}`}>{p.name}</Link>
            </NavListItem>
          ))}
        </NavListGroup>
      ))}
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
          // Six words in the bar now (Features, Solutions, ...): they fold
          // into the menu below a wide bar.
          fold="wide"
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
              <Button
                variant="ghost"
                size={stars === null ? "icon" : "default"}
                asChild
                aria-label={stars === null ? "shpyrd on GitHub" : `shpyrd on GitHub, ${stars} stars`}
                className={cn(iconHover, stars !== null && "gap-1 px-2")}
              >
                <a href={github}>
                  <GitHubMark />
                  {stars !== null && (
                    <span className="text-xs text-muted-foreground tabular-nums transition-colors group-hover/button:text-primary">
                      {compact.format(stars)}
                    </span>
                  )}
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
                <DeployButton />
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
            action={<DeployButton />}
            columns={[
              // The two menus of the header, without Solutions-backup (it goes
              // after Giovani's design review): the features, and the
              // solutions in their three groups.
              { title: "Features", links: features.map((f) => <Link key={f.slug} href={`/features/${f.slug}`}>{f.name}</Link>) },
              ...solutionGroups.map((g) => ({
                title: g.name === "Who" ? "Solutions" : g.name,
                links: g.items.map((p) => <Link key={p.slug} href={`/solutions/${p.slug}`}>{p.name}</Link>),
              })),
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
            // Who it is for (Solutions), then the site's own pages; not
            // Solutions-backup, which goes after the design review.
            links={[
              ...solutionGroups[0].items.map((p) => ({ title: p.name, href: `/solutions/${p.slug}` })),
              ...marketing,
            ].map((l) => (
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
