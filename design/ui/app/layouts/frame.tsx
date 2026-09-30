"use client";

import { Boxes, ExternalLink, Globe, LayoutGrid, LogOut, Moon, Settings2, UserRound } from "lucide-react";
import { Avatar } from "@shpyrd/ui/components/avatar";
import { Badge } from "@shpyrd/ui/components/badge";
import { Wordmark } from "@shpyrd/ui/components/brand";
import { Breadcrumbs, BreadcrumbsItem } from "@shpyrd/ui/components/breadcrumbs";
import { Button } from "@shpyrd/ui/components/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@shpyrd/ui/components/dropdown-menu";
import { InlineCode } from "@shpyrd/ui/components/inline-code";
import { NavList, NavListItem } from "@shpyrd/ui/components/nav-list";
import {
  PageLayout,
  PageLayoutContent,
  PageLayoutHeader,
  PageLayoutSidebar,
} from "@shpyrd/ui/components/page-layout";
import { Stack } from "@shpyrd/ui/components/stack";
import { people } from "../people";

// What is around every screen: the brand and the navigation at the side,
// where the page is and who is signed in over it.
export function Frame({
  children,
  here,
  crumb,
  app = "workspace",
}: {
  children: React.ReactNode;
  here: string;
  crumb?: string;
  app?: "workspace" | "console";
}) {
  const [me] = people;
  const nav =
    app === "console"
      ? [
          ["cluster", "Cluster", <Boxes key="i" />],
          ["workspaces", "Workspaces", <LayoutGrid key="i" />],
          ["accounts", "Accounts", <UserRound key="i" />],
          ["settings", "Settings", <Settings2 key="i" />],
        ]
      : [
          ["apps", "Apps", <LayoutGrid key="i" />],
          ["projects", "Projects", <Boxes key="i" />],
          ["workspace", "Workspace", <Globe key="i" />],
        ];
  const title = nav.find(([slug]) => slug === here)?.[1];
  return (
    <PageLayout
      containerWidth="full"
      padding="none"
      columnGap="none"
      rowGap="none"
      className="overflow-hidden rounded-xl bg-background ring-1 ring-foreground/10"
    >
      <PageLayoutSidebar aria-label="Application" width="small" divider="line" className="pb-4">
        <Stack direction="horizontal" align="center" gap="cozy" padding="normal">
          <Wordmark />
          <Badge variant="outline">{app}</Badge>
        </Stack>
        <NavList aria-label={app}>
          {nav.map(([slug, label, icon]) => (
            <NavListItem
              key={slug as string}
              href={`#${slug}`}
              icon={icon as React.ReactElement}
              aria-current={slug === here ? "page" : undefined}
              onClick={(e) => e.preventDefault()}
            >
              {label}
            </NavListItem>
          ))}
        </NavList>
      </PageLayoutSidebar>

      <PageLayoutHeader divider="line" className="px-4 py-3">
        <Stack direction="horizontal" align="center" gap="cozy">
          <Breadcrumbs>
            <BreadcrumbsItem href="#" selected={!crumb} onClick={(e) => e.preventDefault()}>
              {title}
            </BreadcrumbsItem>
            {crumb && <BreadcrumbsItem selected>{crumb}</BreadcrumbsItem>}
          </Breadcrumbs>
          <Stack direction="horizontal" align="center" gap="tight" className="ml-auto">
            {app === "workspace" && (
              <Button variant="ghost" size="sm" iconEnd={<ExternalLink />}>
                Console
              </Button>
            )}
            <Button variant="ghost" size="icon-sm" icon={<Moon />} aria-label="Theme" />
            <InlineCode>v0.42.0</InlineCode>
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="ghost" size="sm" icon={<Avatar size={20} src={me.src} alt="" />}>
                  {me.alt}
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuLabel className="font-normal">
                  <div className="text-sm font-medium">{me.alt}</div>
                  <div className="text-xs text-muted-foreground">ana@acme.com</div>
                  <div className="text-xs text-muted-foreground">signed in with GitHub</div>
                </DropdownMenuLabel>
                <DropdownMenuSeparator />
                <DropdownMenuItem>
                  <LogOut /> Sign out
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </Stack>
        </Stack>
      </PageLayoutHeader>

      <PageLayoutContent as="div" width="large" padding="normal" className="grid content-start gap-6">
        {children}
      </PageLayoutContent>
    </PageLayout>
  );
}

