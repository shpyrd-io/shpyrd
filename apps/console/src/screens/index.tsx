"use client";

import { Link, Navigate, Route, Routes, useLocation } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { Boxes, Building2, Cpu, ExternalLink, Database, HardDrive, KeyRound, LayoutDashboard, LogIn, Mail, Package, Settings, TrendingUp, Users, Waves } from "lucide-react";
import { NavList, NavListGroup, NavListItem } from "@shpyrd/ui/components/nav-list";
import { api } from "@/api/api";
import { usePerms } from "@/lib/perms";
import { Frame } from "@/shell/frame";
import { Accounts } from "./accounts";
import { Backups } from "./backups";
import { Components } from "./components";
import { Drains } from "./drains";
import { Economics } from "./economics";
import { Globals } from "./globals";
import { Mail as MailPage } from "./mail";
import { Overview } from "./overview";
import { Registry } from "./registry";
import { Settings as SettingsPage } from "./settings";
import { SignIn } from "./sign-in";
import { Sizes } from "./sizes";
import { Storage } from "./storage";
import { Workspaces } from "./workspaces";

// The console, in pages down a list at the side. Overview is the
// cluster as it is; Platform is what the operator runs for others;
// Cluster is the machinery under it.
// A page may need a role, an extension, or one of several: the billing
// capability of the cloud layer, or the opencost extension, for Economics.
// One may be a door without a capability: where the platform hosts only
// the workspace made at install, Workspaces is a link, Workspace, straight
// to that workspace's address; there is nothing to list.
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
    title: "Workspaces",
    icon: <Building2 />,
    needs: "admin",
    door: { capability: "workspaces", title: "Workspace" },
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
    slug: "economics",
    title: "Economics",
    icon: <TrendingUp />,
    needs: "admin",
    any: { capabilities: ["billing"], extensions: ["opencost"] },
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
    slug: "globals",
    title: "Global vars",
    icon: <KeyRound />,
    needs: "admin",
  },
  {
    group: "Cluster",
    slug: "drains",
    title: "Drains",
    icon: <Waves />,
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
  const isDoor = (p: Page) => !!p.door && one;
  const shown = pages.filter((p) => has(p) && (!isDoor(p) || !!theWorkspace?.url));
  const groups = [...new Set(shown.map((p) => p.group))];

  const nav = (
    <NavList aria-label="Console">
      {groups.map((group) =>
        group === "" ? (
          shown
            .filter((p) => p.group === "")
            .map((p) => (
              <NavListItem key={p.slug} asChild icon={p.icon} aria-current={here === p.slug ? "page" : undefined}>
                <Link to="/">{p.title}</Link>
              </NavListItem>
            ))
        ) : (
          <NavListGroup key={group} title={group}>
            {shown
              .filter((p) => p.group === group)
              .map((p) => (
                <NavListItem key={p.slug} asChild icon={p.icon} aria-current={here === p.slug ? "page" : undefined}>
                  {isDoor(p) ? (
                    <a href={theWorkspace!.url} target="_blank" rel="noreferrer">
                      {p.door!.title}
                      <ExternalLink className="ml-auto size-3.5 text-muted-foreground" aria-hidden />
                    </a>
                  ) : (
                    <Link to={`/${p.slug}`}>{p.title}</Link>
                  )}
                </NavListItem>
              ))}
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
        <Route path="workspaces" element={one ? <Navigate to="/" replace /> : <Workspaces />} />
        <Route path="accounts" element={<Accounts />} />
        <Route path="sign-in" element={<SignIn />} />
        <Route path="economics" element={<Economics />} />
        <Route path="settings" element={<SettingsPage />} />
        <Route path="sizes" element={<Sizes />} />
        <Route path="globals" element={<Globals />} />
        <Route path="drains" element={<Drains />} />
        <Route path="registry" element={<Registry />} />
        <Route path="storage" element={<Storage />} />
        <Route path="backups" element={<Backups />} />
        <Route path="mail" element={<MailPage />} />
        <Route path="components" element={<Components />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </Frame>
  );
}
