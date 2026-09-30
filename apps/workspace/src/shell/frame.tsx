"use client";

import { useEffect, useState } from "react";
import { Link, useLocation } from "react-router-dom";
import { Menu } from "lucide-react";
import { AnchoredOverlay } from "@shpyrd/ui/components/anchored-overlay";
import { useQuery } from "@tanstack/react-query";
import { LogoMark } from "@shpyrd/ui/components/brand";
import { Breadcrumbs, BreadcrumbsItem } from "@shpyrd/ui/components/breadcrumbs";
import { Button } from "@shpyrd/ui/components/button";
import {
  PageLayout,
  PageLayoutContent,
  PageLayoutHeader,
  PageLayoutSidebar,
} from "@shpyrd/ui/components/page-layout";
import { Stack } from "@shpyrd/ui/components/stack";
import { api } from "@/api/api";
import { PersonMenu, ThemeButton } from "./person";

export type Crumb = { label: string; to?: string };

// What is around a project and around the workspace: the navigation at
// the side, where the page is over it, the person at the right. The mark
// and the first crumb go back to the launcher.
export function Frame({
  nav,
  crumbs,
  children,
}: {
  nav: React.ReactNode;
  crumbs: Crumb[];
  children: React.ReactNode;
}) {
  const { pathname } = useLocation();
  const [menu, setMenu] = useState(false);
  useEffect(() => setMenu(false), [pathname]);
  const config = useQuery({ queryKey: ["config"], queryFn: api.config, staleTime: 60_000 });
  const workspace = config.data?.workspace;

  return (
    <PageLayout containerWidth="full" padding="none" columnGap="none" rowGap="none" className="min-h-svh">
      <PageLayoutSidebar aria-label="Navigation" width="small" divider="line" sticky hidden={{ narrow: true }} className="pb-4">
        <Stack direction="horizontal" align="center" gap="cozy" padding="normal">
          {/* The symbol of the workspace, its own or the platform's, and its name: back to the launcher. */}
          <Link to="/" aria-label="Launcher" className="flex min-w-0 items-center gap-2.5">
            {workspace?.branding?.logoUrl ? <img src={workspace.branding.logoUrl} alt="" className="size-7 shrink-0 object-contain" /> : <LogoMark className="size-7 shrink-0" />}
            <span className="truncate font-heading text-base font-medium">{workspace?.name ?? "shpyrd"}</span>
          </Link>
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
              <Button variant="outline" size="icon" icon={<Menu />} aria-label="Navigation" className="@3xl/page-layout:hidden" />
            }
          >
            {nav}
          </AnchoredOverlay>
          <Breadcrumbs className="min-w-0 [&_ol]:flex-nowrap [&_li]:min-w-0 [&_li>*]:truncate">
            {crumbs.map((crumb, i) =>
              crumb.to ? (
                <BreadcrumbsItem key={i} asChild selected={i === crumbs.length - 1}>
                  <Link to={crumb.to}>{crumb.label}</Link>
                </BreadcrumbsItem>
              ) : (
                <BreadcrumbsItem key={i} selected={i === crumbs.length - 1}>
                  {crumb.label}
                </BreadcrumbsItem>
              ),
            )}
          </Breadcrumbs>
          <Stack direction="horizontal" align="center" gap="tight" className="ml-auto shrink-0">
            <ThemeButton />
            <PersonMenu />
          </Stack>
        </Stack>
      </PageLayoutHeader>

      <PageLayoutContent width="large" padding="normal" className="grid content-start gap-6">
        {children}
      </PageLayoutContent>
    </PageLayout>
  );
}
