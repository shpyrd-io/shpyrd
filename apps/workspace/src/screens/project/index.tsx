"use client";

import { Link, Navigate, Route, Routes, useLocation, useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import {
  Activity as ActivityIcon,
  ChartBar,
  Globe,
  HardDrive,
  KeyRound,
  LayoutDashboard,
  Link2,
  Rocket,
  ScrollText,
  ShieldCheck,
  Terminal,
  UsersRound,
  Waves,
} from "lucide-react";
import { Alert, AlertDescription, AlertTitle } from "@shpyrd/ui/components/alert";
import { NavList, NavListGroup, NavListItem } from "@shpyrd/ui/components/nav-list";
import { Skeleton } from "@shpyrd/ui/components/skeleton";
import { api } from "@/api/api";
import { usePerms } from "@/lib/perms";
import { Frame } from "@/shell/frame";
import { Access, Activity, Connections, Roles } from "./access";
import { Config } from "./config";
import { DomainsCard } from "./domains";
import { Drains } from "./drains";
import { Heading } from "./heading";
import { Logs } from "./logs";
import { Metrics } from "./metrics";
import { Overview } from "./overview";
import { Releases } from "./releases";
import { Resources } from "./resources";
import { Shell } from "./shell";

// One project: what it is and what can be done with it, in pages down a
// list at the side. Overview is the one everyone opens; the rest is
// operating, configuring and access.
const pages = [
  { group: "", slug: "", title: "Overview", icon: <LayoutDashboard /> },
  { group: "Run", slug: "releases", title: "Releases", icon: <Rocket /> },
  { group: "Run", slug: "metrics", title: "Metrics", icon: <ChartBar /> },
  { group: "Run", slug: "logs", title: "Logs", icon: <ScrollText /> },
  { group: "Run", slug: "shell", title: "Shell", icon: <Terminal />, needs: "exec" },
  { group: "Configure", slug: "config", title: "Config", icon: <KeyRound /> },
  { group: "Configure", slug: "resources", title: "Resources", icon: <HardDrive /> },
  { group: "Configure", slug: "domains", title: "Domains", icon: <Globe /> },
  { group: "Configure", slug: "drains", title: "Drains", icon: <Waves /> },
  { group: "Access", slug: "access", title: "Access", icon: <ShieldCheck /> },
  { group: "Access", slug: "roles", title: "Roles", icon: <UsersRound />, needs: "members" },
  { group: "Access", slug: "connections", title: "Connections", icon: <Link2 /> },
  { group: "Access", slug: "activity", title: "Activity", icon: <ActivityIcon /> },
] as const;

export function ProjectPages() {
  const { slug = "" } = useParams();
  const { pathname } = useLocation();
  const perms = usePerms(slug);
  const project = useQuery({ queryKey: ["project", slug], queryFn: () => api.project(slug), refetchInterval: 5_000 });
  const base = `/projects/${slug}`;
  const here = pathname.replace(base, "").replace(/^\//, "").split("/")[0];
  const shown = pages.filter((p) => !("needs" in p) || perms[p.needs]);
  const groups = [...new Set(shown.map((p) => p.group))];

  const nav = (
    <NavList aria-label="Project">
      {groups.map((group) =>
        group === "" ? (
          shown.filter((p) => p.group === "").map((p) => (
            <NavListItem key={p.slug} asChild icon={p.icon} aria-current={here === p.slug ? "page" : undefined}>
              <Link to={base}>{p.title}</Link>
            </NavListItem>
          ))
        ) : (
          <NavListGroup key={group} title={group}>
            {shown.filter((p) => p.group === group).map((p) => (
              <NavListItem key={p.slug} asChild icon={p.icon} aria-current={here === p.slug ? "page" : undefined}>
                <Link to={`${base}/${p.slug}`}>{p.title}</Link>
              </NavListItem>
            ))}
          </NavListGroup>
        ),
      )}
    </NavList>
  );

  const name = project.data?.displayName ?? slug;
  return (
    <Frame nav={nav} crumbs={[{ label: "Projects", to: "/" }, { label: name, to: base }]}>
      {project.isLoading ? (
        <>
          <Skeleton className="h-8 w-64" />
          <Skeleton className="h-40 w-full" />
        </>
      ) : project.error || !project.data ? (
        <Alert variant="destructive">
          <AlertTitle>Could not load the project</AlertTitle>
          <AlertDescription>{(project.error as Error)?.message ?? "not found"}</AlertDescription>
        </Alert>
      ) : (
        <>
          <Heading project={project.data} perms={perms} />
          <Routes>
            <Route index element={<Overview project={project.data} />} />
            <Route path="releases" element={<Releases project={project.data} perms={perms} />} />
            <Route path="metrics" element={<Metrics project={project.data} />} />
            <Route path="logs" element={<Logs project={project.data} />} />
            <Route path="shell" element={<Shell project={project.data} />} />
            <Route path="config" element={<Config project={project.data} perms={perms} />} />
            <Route path="resources" element={<Resources project={project.data} perms={perms} />} />
            <Route
              path="domains"
              element={
                <DomainsCard
                  title="Domains"
                  description="Addresses of your own that reach the project. The project also answers at its own address."
                  queryKey={["domains", slug]}
                  list={() => api.domains(slug)}
                  add={(host) => api.addDomain(slug, host)}
                  remove={(host) => api.removeDomain(slug, host)}
                  canEdit={perms.config}
                />
              }
            />
            <Route path="drains" element={<Drains project={project.data} perms={perms} />} />
            <Route path="access" element={<Access project={project.data} perms={perms} />} />
            <Route path="roles" element={<Roles project={project.data} perms={perms} />} />
            <Route path="connections" element={<Connections project={project.data} perms={perms} />} />
            <Route path="activity" element={<Activity project={project.data} />} />
            <Route path="*" element={<Navigate to={base} replace />} />
          </Routes>
        </>
      )}
    </Frame>
  );
}
