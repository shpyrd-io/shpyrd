"use client";

import { Link, Navigate, Route, Routes, useLocation } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { Boxes, Building2, CreditCard, Cpu, ExternalLink, Database, HardDrive, LayoutDashboard, LogIn, Mail, Package, Receipt, Send, Settings, ShieldCheck, Users } from "lucide-react";
import { placeLinks, shown as visibleIn } from "@shpyrd/shared/links";
import { NavList, NavListGroup, NavListItem } from "@shpyrd/ui/components/nav-list";
import { api } from "@/api/api";
import { usePerms } from "@/lib/perms";
import { Frame } from "@/shell/frame";
import { Accounts } from "./accounts";
import { Backups } from "./backups";
import { Components } from "./components";
import { ConsoleUsers } from "./console-users";
import { CostDrains } from "./cost-drains";
import { Costs } from "./costs";
import { Mail as MailPage } from "./mail";
import { Overview } from "./overview";
import { Placement } from "./placement";
import { Registry } from "./registry";
import { Settings as SettingsPage } from "./settings";
import { SignIn } from "./sign-in";
import { Sizes } from "./sizes";
import { Storage } from "./storage";

// The console, in pages down a list at the side. Overview is the
// cluster as it is; Platform is what the operator runs for others;
// Cluster is the machinery under it.
// A page may need a role, an extension, or one of several capabilities or
// extensions.
// One may be a door: where the platform hosts one workspace, Workspace is
// a link straight to its address. Where it hosts many, the binary's own
// application lists them, and its link takes the door's place
// (GET /api/links).
type Page = {
  group: string;
  slug: string;
  title: string;
  icon: React.ReactElement;
  needs?: "view" | "admin";
  extension?: string;
  any?: { capabilities?: string[]; extensions?: string[] };
  door?: { capability: string; title: string };
};
const pages: Page[] = [
  {
    group: "",
    slug: "",
    title: "Overview",
    icon: <LayoutDashboard />,
    needs: "view",
  },
  {
    group: "Platform",
    slug: "workspaces",
    title: "Workspace",
    icon: <Building2 />,
    needs: "admin",
    door: { capability: "workspaces", title: "Workspace" },
  },
  {
    group: "Platform",
    slug: "console-users",
    title: "Console users",
    icon: <ShieldCheck />,
    needs: "admin",
  },
  {
    group: "Platform",
    slug: "accounts",
    title: "Accounts",
    icon: <Users />,
    needs: "admin",
    extension: "auth-local",
  },
  {
    group: "Platform",
    slug: "sign-in",
    title: "Sign-in",
    icon: <LogIn />,
    needs: "admin",
  },
  {
    group: "Platform",
    slug: "costs",
    title: "Costs",
    icon: <Receipt />,
    needs: "admin",
    extension: "costs",
  },
  {
    group: "Platform",
    slug: "cost-drains",
    title: "Cost drains",
    icon: <Send />,
    needs: "admin",
    extension: "costs",
  },
  {
    group: "Platform",
    slug: "settings",
    title: "Settings",
    icon: <Settings />,
    needs: "admin",
  },
  {
    group: "Cluster",
    slug: "sizes",
    title: "Instances",
    icon: <Cpu />,
    needs: "view",
  },
  {
    group: "Cluster",
    slug: "placement",
    title: "Placement",
    icon: <Boxes />,
    needs: "admin",
  },
  {
    group: "Cluster",
    slug: "registry",
    title: "Registry",
    icon: <Package />,
    needs: "admin",
  },
  {
    group: "Cluster",
    slug: "storage",
    title: "Storage",
    icon: <Database />,
    needs: "admin",
    extension: "object-storage",
  },
  {
    group: "Cluster",
    slug: "backups",
    title: "Backups",
    icon: <HardDrive />,
    needs: "admin",
  },
  {
    group: "Cluster",
    slug: "mail",
    title: "Email",
    icon: <Mail />,
    needs: "admin",
    extension: "mail",
  },
  {
    group: "Cluster",
    slug: "components",
    title: "Components",
    icon: <Boxes />,
    needs: "view",
  },
];

// The icons a link may name; another gets the external one.
const linkIcons: Record<string, React.ReactElement> = {
  "building-2": <Building2 />,
  "credit-card": <CreditCard />,
  receipt: <Receipt />,
};

export function Pages() {
  const { pathname } = useLocation();
  const perms = usePerms();
  const config = useQuery({
    queryKey: ["config"],
    queryFn: api.config,
    staleTime: 60_000,
  });
  const here = pathname.replace(/^\//, "").split("/")[0];
  const has = (p: Page) => {
    if (p.needs && !perms[p.needs]) return false;
    if (p.extension && !config.data?.extensions.includes(p.extension)) return false;
    if (p.any) return (p.any.capabilities ?? []).some((c) => config.data?.capabilities?.includes(c)) || (p.any.extensions ?? []).some((x) => config.data?.extensions.includes(x));
    return true;
  };
  // Without the workspaces capability, the one workspace's address: the
  // default one's, else the first the core lists.
  const one = !!config.data && !config.data.capabilities?.includes("workspaces");
  const list = useQuery({ queryKey: ["workspaces"], queryFn: api.workspaces, enabled: one && perms.admin, staleTime: 60_000 });
  const theWorkspace = list.data?.find((w) => w.slug === config.data?.defaultWorkspaceId) ?? list.data?.[0];
  // A door is only a door: without one workspace to go to, it is not there.
  const visible = (p: Page) => has(p) && (!p.door || (one && !!theWorkspace?.url));
  const links = useQuery({ queryKey: ["links"], queryFn: api.links, enabled: perms.view, staleTime: 60_000 });
  const groups = visibleIn(placeLinks(pages, links.data ?? []), visible);

  const entries = (group: (typeof groups)[number]) =>
    group.entries.map((e) =>
      e.link ? (
        // Another application: it opens as a page of its own, and has its
        // way back here.
        <NavListItem key={`link-${e.link.url}`} asChild icon={linkIcons[e.link.icon ?? ""] ?? <ExternalLink />}>
          <a href={e.link.url}>{e.link.label}</a>
        </NavListItem>
      ) : (
        <NavListItem key={e.page.slug} asChild icon={e.page.icon} aria-current={here === e.page.slug ? "page" : undefined}>
          {e.page.door ? (
            <a href={theWorkspace!.url} target="_blank" rel="noreferrer">
              {e.page.door.title}
              <ExternalLink className="ml-auto size-3.5 text-muted-foreground" aria-hidden />
            </a>
          ) : (
            <Link to={`/${e.page.slug}`}>{e.page.title}</Link>
          )}
        </NavListItem>
      ),
    );

  const nav = (
    <NavList aria-label="Console">
      {groups.map((group) =>
        group.group === "" ? (
          entries(group)
        ) : (
          <NavListGroup key={group.group} title={group.group}>
            {entries(group)}
          </NavListGroup>
        ),
      )}
    </NavList>
  );

  // The routes are always there; the list hides what the person may not
  // do, and so does each page. Roles come a moment after the first paint.
  return (
    <Frame
      nav={nav}
      crumbs={[
        { label: "Console", to: "/" },
        ...(here
          ? [
              {
                label: pages.find((p) => p.slug === here)?.title ?? here,
              },
            ]
          : []),
      ]}
    >
      <Routes>
        <Route index element={<Overview />} />
        <Route path="cluster" element={<Navigate to="/" replace />} />
        <Route path="console-users" element={<ConsoleUsers />} />
        <Route path="costs" element={<Costs />} />
        <Route path="cost-drains" element={<CostDrains />} />
        <Route path="accounts" element={<Accounts />} />
        <Route path="sign-in" element={<SignIn />} />
        <Route path="settings" element={<SettingsPage />} />
        <Route path="sizes" element={<Sizes />} />
        <Route path="registry" element={<Registry />} />
        <Route path="storage" element={<Storage />} />
        <Route path="backups" element={<Backups />} />
        <Route path="placement" element={<Placement />} />
        <Route path="mail" element={<MailPage />} />
        <Route path="components" element={<Components />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </Frame>
  );
}
