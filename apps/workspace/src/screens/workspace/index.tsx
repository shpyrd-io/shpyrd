"use client";

import { Link, Navigate, Route, Routes, useLocation } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { ArrowLeft, CreditCard, ExternalLink, Globe, KeyRound, Link2, LogIn, Plug, Settings, SlidersHorizontal, Users, UsersRound, Waves } from "lucide-react";
import { placeLinks, shown as visibleIn } from "@shpyrd/shared/links";
import { NavList, NavListDivider, NavListGroup, NavListItem } from "@shpyrd/ui/components/nav-list";
import { api } from "@/api/api";
import { usePerms } from "@/lib/perms";
import { Frame } from "@/shell/frame";
import { Connections } from "./connections";
import { Drains } from "./drains";
import { Home } from "./home";
import { Globals } from "./globals";
import { People } from "./people";
import { MCP, WorkspaceDomains } from "./platform";
import { SignIn } from "./sign-in";
import { Teams } from "./teams";
import { AdvancedIndex } from "@/shell/advanced";

// What each page of the workspace's Advanced is for, in a line.
const lines: Record<string, string> = {
  people: "Everyone, their roles, and who is suspended.",
  teams: "Groups of people, to share apps with all of them at once.",
  "sign-in": "How people sign in: GitHub, Google, your company's SSO.",
  tokens: "Keys for scripts and CI that act without a person.",
  connections: "The agents and apps signed in as you.",
  globals: "Config vars every project receives.",
  drains: "Send the logs of every project to another service.",
  domains: "The workspace's own address, and the projects' under it.",
  mcp: "The name and the address agents connect to.",
};
import { Tokens } from "./tokens";

// The workspace itself: its name and plan, its people, and what the
// platform does for it. One page for each knob, down the list.
const pages = [
  { group: "", slug: "", title: "Overview", icon: <Settings /> },
  { group: "", slug: "advanced", title: "Advanced", icon: <SlidersHorizontal /> },
  { group: "Access", slug: "people", title: "People", icon: <Users />, needs: "admin" },
  { group: "Access", slug: "teams", title: "Teams", icon: <UsersRound />, needs: "admin" },
  { group: "Access", slug: "sign-in", title: "Sign-in", icon: <LogIn />, needs: "admin" },
  { group: "Access", slug: "tokens", title: "API tokens", icon: <KeyRound /> },
  { group: "Access", slug: "connections", title: "Connections", icon: <Link2 />, extension: "mcp" },
  { group: "Platform", slug: "globals", title: "Config vars", icon: <KeyRound />, needs: "admin" },
  { group: "Platform", slug: "drains", title: "Log drains", icon: <Waves />, needs: "admin" },
  { group: "Platform", slug: "domains", title: "Domains", icon: <Globe />, needs: "owner" },
  { group: "Platform", slug: "mcp", title: "MCP", icon: <Plug />, extension: "mcp" },
] as const;

// The icons a link may name; another gets the external one.
const linkIcons: Record<string, React.ReactElement> = {
  "credit-card": <CreditCard />,
};

export function WorkspacePages() {
  const { pathname } = useLocation();
  const perms = usePerms();
  const links = useQuery({ queryKey: ["links"], queryFn: api.links, staleTime: 60_000 });
  const here = pathname.replace("/workspace", "").replace(/^\//, "").split("/")[0];
  const config = useQuery({ queryKey: ["config"], queryFn: api.config, staleTime: 60_000 });
  // The MCP server is the enterprise's (ee/mcp): its pages where it is built in.
  const visible = (p: (typeof pages)[number]) => (!("needs" in p) || perms[p.needs]) && (!("extension" in p) || !!config.data?.extensions.includes(p.extension));
  // What extensions add goes in place: each link opens an application as
  // a page of its own, which has its way back here.
  const groups = visibleIn(placeLinks(pages, links.data ?? []), visible);

  const entries = (group: (typeof groups)[number]) =>
    group.entries.map((e) =>
      e.link ? (
        <NavListItem key={`link-${e.link.url}`} asChild icon={linkIcons[e.link.icon ?? ""] ?? <ExternalLink />}>
          <a href={e.link.url}>{e.link.label}</a>
        </NavListItem>
      ) : (
        <NavListItem key={e.page.slug} asChild icon={e.page.icon} aria-current={here === e.page.slug ? "page" : undefined}>
          <Link to={e.page.slug ? `/workspace/${e.page.slug}` : "/workspace"}>{e.page.title}</Link>
        </NavListItem>
      ),
    );

  const nav = (
    <NavList aria-label="Workspace">
      {/* Back to the launcher, where the logo goes too. */}
      <NavListItem asChild icon={<ArrowLeft />}>
        <Link to="/">Back to Launcher</Link>
      </NavListItem>
      <NavListDivider />
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
  // The overview has no side, like a project's; Advanced and its pages do.
  const simple = here === "";
  const advanced = pages.filter((p) => p.group !== "" && visible(p));
  return (
    <Frame nav={simple ? undefined : nav} crumbs={[{ label: "Projects", to: "/" }, { label: "Settings", to: "/workspace" }, ...(simple ? [] : [{ label: "Advanced", to: "/workspace/advanced" }])]}>
      <Routes>
        <Route index element={<Home />} />
        <Route path="advanced" element={<AdvancedIndex base="/workspace" description="Everything about the workspace that the overview leaves out." pages={advanced} lines={lines} />} />
        <Route path="people" element={<People />} />
        <Route path="teams" element={<Teams />} />
        <Route path="sign-in" element={<SignIn />} />
        <Route path="tokens" element={<Tokens />} />
        <Route path="connections" element={<Connections />} />
        <Route path="globals" element={<Globals />} />
        <Route path="drains" element={<Drains />} />
        <Route path="domains" element={<WorkspaceDomains />} />
        <Route path="mcp" element={<MCP />} />
        <Route path="*" element={<Navigate to="/workspace" replace />} />
      </Routes>
    </Frame>
  );
}
