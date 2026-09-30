"use client";

import { Link, Navigate, Route, Routes, useLocation } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { CreditCard, Globe, KeyRound, Link2, LogIn, Plug, Settings, Users, UsersRound } from "lucide-react";
import { NavList, NavListGroup, NavListItem } from "@shpyrd/ui/components/nav-list";
import { api } from "@/api/api";
import { usePerms } from "@/lib/perms";
import { Frame } from "@/shell/frame";
import { Connections } from "./connections";
import { General } from "./general";
import { Globals } from "./globals";
import { People } from "./people";
import { Billing, MCP, WorkspaceDomains } from "./platform";
import { SignIn } from "./sign-in";
import { Teams } from "./teams";
import { Tokens } from "./tokens";

// The workspace itself: its name and plan, its people, and what the
// platform does for it. One page for each knob, down the list.
const pages = [
  { group: "", slug: "", title: "General", icon: <Settings /> },
  { group: "Access", slug: "people", title: "People", icon: <Users />, needs: "admin" },
  { group: "Access", slug: "teams", title: "Teams", icon: <UsersRound />, needs: "admin" },
  { group: "Access", slug: "sign-in", title: "Sign-in", icon: <LogIn />, needs: "admin" },
  { group: "Access", slug: "tokens", title: "API tokens", icon: <KeyRound /> },
  { group: "Access", slug: "connections", title: "Connections", icon: <Link2 /> },
  { group: "Platform", slug: "globals", title: "Config vars", icon: <KeyRound />, needs: "admin" },
  { group: "Platform", slug: "domains", title: "Domains", icon: <Globe />, needs: "owner" },
  { group: "Platform", slug: "billing", title: "Billing", icon: <CreditCard />, needs: "admin", capability: "billing" },
  { group: "Platform", slug: "mcp", title: "MCP", icon: <Plug /> },
] as const;

export function WorkspacePages() {
  const { pathname } = useLocation();
  const perms = usePerms();
  const config = useQuery({ queryKey: ["config"], queryFn: api.config, staleTime: 60_000 });
  const here = pathname.replace("/workspace", "").replace(/^\//, "").split("/")[0];
  // A page may need a role, or a capability of the platform: Billing is
  // the cloud layer's; a self-hosted platform prices nothing.
  const shown = pages.filter((p) => (!("needs" in p) || perms[p.needs]) && (!("capability" in p) || config.data?.capabilities?.includes(p.capability)));
  const groups = [...new Set(shown.map((p) => p.group))];

  const nav = (
    <NavList aria-label="Workspace">
      {groups.map((group) =>
        group === "" ? (
          shown.filter((p) => p.group === "").map((p) => (
            <NavListItem key={p.slug} asChild icon={p.icon} aria-current={here === p.slug ? "page" : undefined}>
              <Link to="/workspace">{p.title}</Link>
            </NavListItem>
          ))
        ) : (
          <NavListGroup key={group} title={group}>
            {shown.filter((p) => p.group === group).map((p) => (
              <NavListItem key={p.slug} asChild icon={p.icon} aria-current={here === p.slug ? "page" : undefined}>
                <Link to={`/workspace/${p.slug}`}>{p.title}</Link>
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
    <Frame nav={nav} crumbs={[{ label: "Workspace", to: "/workspace" }]}>
      <Routes>
        <Route index element={<General />} />
        <Route path="people" element={<People />} />
        <Route path="teams" element={<Teams />} />
        <Route path="sign-in" element={<SignIn />} />
        <Route path="tokens" element={<Tokens />} />
        <Route path="connections" element={<Connections />} />
        <Route path="globals" element={<Globals />} />
        <Route path="domains" element={<WorkspaceDomains />} />
        <Route path="billing" element={<Billing />} />
        <Route path="mcp" element={<MCP />} />
        <Route path="*" element={<Navigate to="/workspace" replace />} />
      </Routes>
    </Frame>
  );
}
