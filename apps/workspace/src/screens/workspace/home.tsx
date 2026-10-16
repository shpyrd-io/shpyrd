"use client";

import { Link, useNavigate } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowRight, CreditCard, Globe, KeyRound, LogIn, Terminal, UsersRound } from "lucide-react";
import { AvatarStack } from "@shpyrd/ui/components/avatar-stack";
import { LogoMark } from "@shpyrd/ui/components/brand";
import { Button } from "@shpyrd/ui/components/button";
import { Card, CardAction, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@shpyrd/ui/components/card";
import { LauncherCard } from "@shpyrd/ui/components/launcher-card";
import { PageHeading } from "@shpyrd/ui/components/page-heading";
import { StatusBadge } from "@shpyrd/ui/components/status-badge";
import { api } from "@/api/api";
import { CopyLine } from "@/lib/copy-line";
import { Essential, Essentials } from "@/shell/essential";
import { usePerms } from "@/lib/perms";
import { addressOf, hostOf, phaseOf } from "@/lib/project";
import { lookOf } from "@/lib/tile";
import { Carousel } from "@/shell/carousel";
import { NameAndLook, RolesNotEnforced } from "./general";
import { Invite } from "./people";

// The workspace for someone who builds with an agent: who is in it, how
// the agent gets in, how much of the plan is taken, and how it looks.
// The rest is Advanced, one link away.
export function Home() {
  const perms = usePerms();
  const ws = useQuery({ queryKey: ["workspace"], queryFn: api.workspace });
  const links = useQuery({ queryKey: ["links"], queryFn: api.links, staleTime: 60_000 });
  const billing = links.data?.find((l) => l.icon === "credit-card");
  return (
    <>
      <PageHeading
        title={ws.data?.name ?? "Workspace"}
        icon={ws.data?.branding?.logoUrl ? <img src={ws.data.branding.logoUrl} alt="" className="size-9 object-contain" /> : <LogoMark className="size-9" />}
        description={ws.data?.address}
        actions={
          billing && (
            <Button variant="outline" size="sm" icon={<CreditCard />} asChild>
              <a href={billing.url}>{billing.label}</a>
            </Button>
          )
        }
        iconEnd={
          <>
            {perms.admin && <NameAndLook />}
            {ws.data && <StatusBadge type={ws.data.status === "suspended" ? "warning" : "success"}>{ws.data.status === "suspended" ? "Suspended" : "Active"}</StatusBadge>}
          </>
        }
      />
      <RolesNotEnforced />
      <ProjectStrip />
      <div className="grid gap-4 @3xl/page-layout:grid-cols-2">
        {perms.admin && <People />}
        <Agent url={ws.data?.url} />
      </div>
      {perms.admin && <Settings />}
      <Card size="sm" variant="dashed" asChild>
        <Link to="/workspace/advanced">
          <CardContent className="flex items-center gap-3">
            <div className="min-w-0 flex-1">
              <div className="font-medium">Advanced</div>
              <div className="text-muted-foreground">API tokens, connections, log drains, the MCP server and everyone's roles.</div>
            </div>
            <ArrowRight className="size-4 shrink-0 text-muted-foreground" />
          </CardContent>
        </Link>
      </Card>
    </>
  );
}

// Who is in the workspace, at a glance, and the way to add one more.
function People() {
  const perms = usePerms();
  const queries = useQueryClient();
  const people = useQuery({ queryKey: ["people"], queryFn: api.people });
  const invitations = useQuery({ queryKey: ["invitations"], queryFn: api.invitations });
  const active = (people.data ?? []).filter((p) => p.status === "active");
  const waiting = (invitations.data ?? []).filter((i) => !i.expired);
  const refresh = () => {
    queries.invalidateQueries({ queryKey: ["people"] });
    queries.invalidateQueries({ queryKey: ["invitations"] });
  };
  return (
    <Card>
      <CardHeader>
        <CardTitle>People</CardTitle>
        <CardDescription>Everyone here can open the projects shared with them. To share one, ask your agent: “share it with Ana”.</CardDescription>
        <CardAction>
          <Invite onDone={refresh} canOwner={perms.owner} />
        </CardAction>
      </CardHeader>
      <CardContent className="flex items-center gap-3">
        <AvatarStack avatars={active.map((p) => ({ alt: p.name ?? p.email }))} size={32} max={6} />
        <div className="text-sm">
          <div>
            {active.length} {active.length === 1 ? "person" : "people"}
          </div>
          {waiting.length > 0 && (
            <div className="text-xs text-muted-foreground">
              {waiting.length} {waiting.length === 1 ? "invitation" : "invitations"} waiting
            </div>
          )}
        </div>
      </CardContent>
      <CardFooter className="mt-auto">
        <Link to="/workspace/people" className="text-xs text-primary underline-offset-4 hover:underline">
          Everyone and their roles
        </Link>
      </CardFooter>
    </Card>
  );
}

// How the agent gets in: sign it in once, then say what you want.
function Agent({ url }: { url?: string }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Your agent</CardTitle>
        <CardDescription>Sign the shpyrd CLI in once from your computer. Then ask your agent in your own words: “put this app online”, “why is it slow?”.</CardDescription>
      </CardHeader>
      <CardContent>
        <CopyLine icon={<Terminal />} text={`shpyrd login --url ${url ?? "https://…"}`} mono />
      </CardContent>
      <CardFooter className="mt-auto">
        <a href="https://shpyrd.io/docs/getting-started" target="_blank" rel="noreferrer" className="text-xs text-primary underline-offset-4 hover:underline">
          First time? Set up your agent
        </a>
      </CardFooter>
    </Card>
  );
}

// The four things an administrator changes, each with how it is now.
function Settings() {
  const perms = usePerms();
  const teams = useQuery({ queryKey: ["teams"], queryFn: api.teams });
  const methods = useQuery({ queryKey: ["login-methods"], queryFn: api.loginMethods });
  const globals = useQuery({ queryKey: ["globals"], queryFn: api.globals });
  const domains = useQuery({ queryKey: ["workspace-domains"], queryFn: api.workspaceDomains, enabled: perms.owner });
  const ws = useQuery({ queryKey: ["workspace"], queryFn: api.workspace });
  const named = (teams.data ?? []).filter((t) => !t.everyone);
  const ways = [...(methods.data?.connectors.map((c) => c.name) ?? []), ...(methods.data?.password ? ["Email and password"] : [])];
  const own = domains.data?.find((d) => d.primary && d.verified)?.host;
  const vars = globals.data?.vars.length ?? 0;
  return (
    <Essentials>
      <Essential to="/workspace/teams" icon={UsersRound} title="Teams" now={teams.isLoading ? "…" : named.length ? `${named.length} ${named.length === 1 ? "team" : "teams"}` : "None yet"} />
      <Essential to="/workspace/sign-in" icon={LogIn} title="Sign-in" now={methods.isLoading ? "…" : ways.length > 1 ? `${ways[0]} and ${ways.length - 1} more` : (ways[0] ?? "The platform's")} />
      <Essential to="/workspace/globals" icon={KeyRound} title="Shared config vars" now={globals.isLoading ? "…" : vars ? `${vars} set, in every project` : "None yet"} />
      {perms.owner && <Essential to="/workspace/domains" icon={Globe} title="Domain" now={domains.isLoading ? "…" : (own ?? ws.data?.address ?? "Add your own")} />}
    </Essentials>
  );
}

// The projects, as small cards in a row that goes sideways. A card opens
// the project here, not the application: this is where it is managed.
function ProjectStrip() {
  const navigate = useNavigate();
  const projects = useQuery({ queryKey: ["projects"], queryFn: api.projects, refetchInterval: 15_000 });
  if (!projects.data?.length) return null;
  return (
    <Carousel
      heading="Projects"
      action={
        <Link to="/" className="text-xs text-primary underline-offset-4 hover:underline">
          All projects
        </Link>
      }
    >
        {projects.data.map((p) => {
          const address = addressOf(p);
          return (
            <div
              key={p.slug}
              role="listitem"
              onClickCapture={(e) => {
                e.preventDefault();
                navigate(`/projects/${p.slug}`);
              }}
            >
              <LauncherCard size="sm" name={p.displayName} {...lookOf(p)} url={hostOf(address)} href={`/projects/${p.slug}`} phase={phaseOf(p)} />
            </div>
          );
        })}
    </Carousel>
  );
}
