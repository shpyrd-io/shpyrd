"use client";

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ExternalLink, Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { Badge } from "@shpyrd/ui/components/badge";
import { Button } from "@shpyrd/ui/components/button";
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@shpyrd/ui/components/card";
import { ConfirmDialog } from "@shpyrd/ui/components/confirm-dialog";
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from "@shpyrd/ui/components/dialog";
import { Field } from "@shpyrd/ui/components/field";
import { InlineCode } from "@shpyrd/ui/components/inline-code";
import { Input } from "@shpyrd/ui/components/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@shpyrd/ui/components/select";
import { Stack } from "@shpyrd/ui/components/stack";
import { StatusBadge } from "@shpyrd/ui/components/status-badge";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@shpyrd/ui/components/table";
import { Timeline, TimelineItem } from "@shpyrd/ui/components/timeline";
import { api, type ProjectRole } from "@/api/api";
import type { Access as AccessMode, AllowEntry, Member, Project } from "@/api/types";
import type { Perms } from "@/lib/perms";
import { ago } from "@/lib/project";
import { Failed, Loading, when } from "./shared";

const modes: Record<AccessMode, { label: string; text: string }> = {
  public: { label: "Anyone", text: "The app answers to anyone on the internet, or on the local network when it is internal." },
  authenticated: { label: "Who signs in", text: "The app asks for a sign-in of this workspace before it answers." },
  identified: { label: "Named people", text: "Only the people and teams named under Roles, with the role user or above." },
};

// Who may open the app, and a way to see it as a team would.
export function Access({ project, perms }: { project: Project; perms: Perms }) {
  const queries = useQueryClient();
  const teams = useQuery({ queryKey: ["teams"], queryFn: api.teams, enabled: perms.deploy && project.access !== "public" });
  const [asTeams, setAsTeams] = useState<string[]>([]);
  const [confirmPublic, setConfirmPublic] = useState(false);
  const preview = useMutation({
    mutationFn: (body: { teams: string[]; anonymous?: boolean }) => api.preview(project.slug, body),
    onSuccess: (r) => window.open(r.url, "_blank", "noopener"),
    onError: (e: Error) => toast.error(e.message),
  });
  const set = useMutation({
    mutationFn: (access: AccessMode) => api.setAccess(project.slug, access),
    onSuccess: (_, access) => {
      queries.invalidateQueries({ queryKey: ["project", project.slug] });
      toast.success(`Access: ${modes[access].label.toLowerCase()}`);
    },
    onError: (e: Error) => toast.error(e.message),
  });
  return (
    <Card>
      <CardHeader>
        <CardTitle>Who may open it</CardTitle>
        <CardDescription>{modes[project.access].text}</CardDescription>
      </CardHeader>
      <CardContent>
        <Stack gap="normal">
          <Stack direction="horizontal" align="center" gap="cozy" wrap="wrap">
            <Select
              value={project.access}
              disabled={!perms.members || set.isPending}
              onValueChange={(v) => {
                if (v === "public") setConfirmPublic(true);
                else set.mutate(v as AccessMode);
              }}
            >
              <SelectTrigger className="w-64">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {(Object.keys(modes) as AccessMode[]).map((m) => (
                  <SelectItem key={m} value={m}>
                    {modes[m].label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            {project.access === "public" && <StatusBadge type="warning">Anyone on the internet may open it</StatusBadge>}
          </Stack>
          <ConfirmDialog
            open={confirmPublic}
            onOpenChange={setConfirmPublic}
            title="Open it to anyone?"
            description="Anyone on the internet will be able to open the app, with no sign-in."
            action="Open to anyone"
            onConfirm={() => set.mutate("public")}
          />
          {project.access !== "public" && perms.deploy && (
            <div className="grid gap-3 rounded-md border p-4">
              <p className="text-sm">
                <b className="font-medium">Open as</b> <span className="text-muted-foreground">sees the app the way a team does. The app gets a preview identity, and the preview is audited.</span>
              </p>
              <Stack direction="horizontal" wrap="wrap" align="center" gap="tight">
                {(teams.data ?? []).map((t) => {
                  const on = asTeams.includes(t.name);
                  return (
                    <Button key={t.name} size="xs" variant={on ? "default" : "outline"} onClick={() => setAsTeams(on ? asTeams.filter((n) => n !== t.name) : [...asTeams, t.name])}>
                      {t.name}
                    </Button>
                  );
                })}
                <Button size="xs" variant="secondary" icon={<ExternalLink />} disabled={preview.isPending} onClick={() => preview.mutate({ teams: asTeams })}>
                  Open as {asTeams.length ? asTeams.join(", ") : "a member of no team"}
                </Button>
                <Button size="xs" variant="ghost" disabled={preview.isPending} onClick={() => preview.mutate({ teams: [], anonymous: true })}>
                  Open as nobody
                </Button>
              </Stack>
            </div>
          )}
          {project.access !== "public" && (
            <p className="text-xs text-muted-foreground">
              The app reads who is there from the <InlineCode>X-Shpyrd-User</InlineCode>, <InlineCode>X-Shpyrd-Teams</InlineCode> and <InlineCode>X-Shpyrd-Roles</InlineCode> headers, or checks the bearer token against <InlineCode>/.well-known/jwks.json</InlineCode>.
            </p>
          )}
        </Stack>
      </CardContent>
    </Card>
  );
}

const roles: Record<ProjectRole, string> = {
  reader: "reads the app, nothing more",
  user: "opens the app",
  viewer: "sees the project",
  developer: "deploys and configures",
  admin: "everything, members included",
};

// Who operates the project, and who only uses it.
export function Roles({ project, perms }: { project: Project; perms: Perms }) {
  const queries = useQueryClient();
  const members = useQuery({ queryKey: ["members", project.slug], queryFn: () => api.members(project.slug) });
  const refresh = () => queries.invalidateQueries({ queryKey: ["members", project.slug] });
  const set = useMutation({
    mutationFn: (m: Member) => api.setMember(project.slug, m),
    onSuccess: () => {
      refresh();
      toast.success("Role set");
    },
    onError: (e: Error) => toast.error(e.message),
  });
  const remove = useMutation({
    mutationFn: (name: string) => api.removeMember(project.slug, name),
    onSuccess: () => {
      refresh();
      toast.success("Removed");
    },
    onError: (e: Error) => toast.error(e.message),
  });
  return (
    <Card>
      <CardHeader>
        <CardTitle>Roles</CardTitle>
        <CardDescription>Who can open the app (user) and who can operate the project. Admins of the workspace can do everything anyway.</CardDescription>
        {perms.members && (
          <CardAction>
            <AddMember onAdd={(m) => set.mutate(m)} />
          </CardAction>
        )}
      </CardHeader>
      <CardContent>
        {members.isLoading ? (
          <Loading />
        ) : members.error ? (
          <Failed what="the members" error={members.error} />
        ) : (members.data ?? []).length === 0 ? (
          <p className="text-sm text-muted-foreground">Nobody named yet. Only the admins of the workspace reach it.</p>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Who</TableHead>
                <TableHead>Role</TableHead>
                <TableHead className="text-right" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {members.data!.map((m) => (
                <TableRow key={m.name}>
                  <TableCell>
                    {m.team ? (
                      <>
                        <InlineCode>team</InlineCode> {m.team}
                      </>
                    ) : (
                      m.user
                    )}
                  </TableCell>
                  <TableCell>
                    <Stack direction="horizontal" align="center" gap="condensed">
                      <Select value={m.role} disabled={!perms.members || set.isPending} onValueChange={(role) => set.mutate({ ...m, role: role as ProjectRole })}>
                        <SelectTrigger size="sm" className="w-32">
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          {(Object.keys(roles) as ProjectRole[]).map((r) => (
                            <SelectItem key={r} value={r}>
                              {r}
                            </SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                      <span className="text-xs text-muted-foreground">{roles[m.role]}</span>
                    </Stack>
                  </TableCell>
                  <TableCell className="text-right">
                    {perms.members && (
                      <ConfirmDialog
                        trigger={<Button variant="ghost" size="icon-xs" icon={<Trash2 />} aria-label={`Remove ${m.name}`} />}
                        variant="destructive"
                        title={`Remove ${m.team ?? m.user}?`}
                        description="They lose their way into the project."
                        action="Remove"
                        onConfirm={() => remove.mutate(m.name)}
                      />
                    )}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  );
}

function AddMember({ onAdd }: { onAdd: (m: Member) => void }) {
  const [open, setOpen] = useState(false);
  const [kind, setKind] = useState<"user" | "team">("user");
  const [who, setWho] = useState("");
  const [role, setRole] = useState<ProjectRole>("user");
  const teams = useQuery({ queryKey: ["teams"], queryFn: api.teams, enabled: open });
  const team = kind === "team";
  const valid = team ? who.length > 0 : /^\S+@\S+\.\S+$/.test(who);
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button size="sm" icon={<Plus />}>
          Add
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader divider>
          <DialogTitle>Give someone a role</DialogTitle>
          <DialogDescription>A person of the workspace, by email, or one of its teams.</DialogDescription>
        </DialogHeader>
        <Stack gap="normal">
          <Field label="Who">
            <Stack direction="horizontal" gap="condensed">
              <Select
                value={kind}
                onValueChange={(v) => {
                  setKind(v as typeof kind);
                  setWho("");
                }}
              >
                <SelectTrigger className="w-28">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="user">A person</SelectItem>
                  <SelectItem value="team">A team</SelectItem>
                </SelectContent>
              </Select>
              {team ? (
                <Select value={who} onValueChange={setWho}>
                  <SelectTrigger className="flex-1">
                    <SelectValue placeholder="Choose a team" />
                  </SelectTrigger>
                  <SelectContent>
                    {(teams.data ?? []).map((t) => (
                      <SelectItem key={t.name} value={t.name}>
                        {t.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              ) : (
                <Input value={who} onChange={(e) => setWho(e.target.value.trim())} placeholder="ana@acme.com" autoFocus className="flex-1" />
              )}
            </Stack>
          </Field>
          <Field label="Role" hint={roles[role]}>
            <Select value={role} onValueChange={(v) => setRole(v as ProjectRole)}>
              <SelectTrigger className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {(Object.keys(roles) as ProjectRole[]).map((r) => (
                  <SelectItem key={r} value={r}>
                    {r}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
        </Stack>
        <DialogFooter>
          <DialogClose asChild>
            <Button variant="outline">Cancel</Button>
          </DialogClose>
          <Button
            disabled={!valid}
            onClick={() => {
              onAdd(team ? { name: `team:${who}`, team: who, role } : { name: who, user: who, role });
              setOpen(false);
              setWho("");
            }}
          >
            Add
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// Who may reach the project from inside the cluster. Projects are kept
// apart: nothing reaches one but the ingress and the monitoring, until
// another project, or a caller of the platform, is let in here.
const callers: Record<NonNullable<AllowEntry["platform"]>, string> = { mcp: "the MCP connector", actions: "the server, acting for a person" };

export function Connections({ project, perms }: { project: Project; perms: Perms }) {
  const queries = useQueryClient();
  const allow = useQuery({ queryKey: ["allow", project.slug], queryFn: () => api.allow(project.slug) });
  const projects = useQuery({ queryKey: ["projects"], queryFn: api.projects, enabled: perms.members });
  const [kind, setKind] = useState<"project" | "platform">("project");
  const [value, setValue] = useState("");
  const set = useMutation({
    mutationFn: (entries: AllowEntry[]) => api.setAllow(project.slug, entries),
    onSuccess: () => {
      queries.invalidateQueries({ queryKey: ["allow", project.slug] });
      setValue("");
      toast.success("Connections updated");
    },
    onError: (e: Error) => toast.error(e.message),
  });
  const entries = allow.data ?? [];
  const same = (a: AllowEntry, b: AllowEntry) => a.project === b.project && a.platform === b.platform;
  const add = () => {
    const entry: AllowEntry = kind === "project" ? { project: value } : { platform: value as AllowEntry["platform"] };
    if (entries.some((e) => same(e, entry))) return toast.error("Already let in");
    set.mutate([...entries, entry]);
  };
  const others = (projects.data ?? []).filter((p) => p.slug !== project.slug && !entries.some((e) => e.project === p.slug));
  return (
    <Card>
      <CardHeader>
        <CardTitle>Connections</CardTitle>
        <CardDescription>Who may reach the project from inside the cluster. Nothing may, until it is listed here: not another project, not the platform acting for someone. No release is needed.</CardDescription>
      </CardHeader>
      <CardContent>
        <Stack gap="normal">
          {allow.isLoading ? (
            <Loading />
          ) : allow.error ? (
            <Failed what="the connections" error={allow.error} />
          ) : entries.length === 0 ? (
            <p className="text-sm text-muted-foreground">Nothing is let in. Only the ingress and the monitoring reach the project.</p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Let in</TableHead>
                  <TableHead>What it is</TableHead>
                  <TableHead className="text-right" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {entries.map((e) => (
                  <TableRow key={e.project ?? e.platform}>
                    <TableCell className="font-medium">
                      <InlineCode>{e.project ?? e.platform}</InlineCode>
                    </TableCell>
                    <TableCell className="text-muted-foreground">{e.project ? "a project of the workspace" : callers[e.platform!]}</TableCell>
                    <TableCell className="text-right">
                      {perms.members && (
                        <Button variant="ghost" size="icon-xs" icon={<Trash2 />} aria-label={`Shut out ${e.project ?? e.platform}`} disabled={set.isPending} onClick={() => set.mutate(entries.filter((x) => !same(x, e)))} />
                      )}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
          {perms.members && (
            <Stack direction="horizontal" gap="condensed" wrap="wrap" align="end">
              <Field label="Let in">
                <Select
                  value={kind}
                  onValueChange={(v) => {
                    setKind(v as "project" | "platform");
                    setValue("");
                  }}
                >
                  <SelectTrigger size="sm" className="w-32">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="project">A project</SelectItem>
                    <SelectItem value="platform">The platform</SelectItem>
                  </SelectContent>
                </Select>
              </Field>
              <Field label={kind === "project" ? "Which" : "As what"}>
                <Select value={value} onValueChange={setValue}>
                  <SelectTrigger size="sm" className="w-64">
                    <SelectValue placeholder={kind === "project" ? "Choose a project" : "Choose a caller"} />
                  </SelectTrigger>
                  <SelectContent>
                    {kind === "project"
                      ? others.map((p) => (
                          <SelectItem key={p.slug} value={p.slug}>
                            {p.displayName}
                          </SelectItem>
                        ))
                      : (Object.keys(callers) as (keyof typeof callers)[]).filter((c) => !entries.some((e) => e.platform === c)).map((c) => (
                          <SelectItem key={c} value={c}>
                            {c} · {callers[c]}
                          </SelectItem>
                        ))}
                  </SelectContent>
                </Select>
              </Field>
              <Button size="sm" icon={<Plus />} disabled={!value || set.isPending} onClick={add}>
                Let in
              </Button>
            </Stack>
          )}
        </Stack>
      </CardContent>
    </Card>
  );
}

// Everything done to the project, from the dashboard, the API and the CLI.
export function Activity({ project }: { project: Project }) {
  const audit = useQuery({ queryKey: ["audit", project.slug], queryFn: () => api.audit(project.slug) });
  return (
    <Card>
      <CardHeader>
        <CardTitle>Activity</CardTitle>
        <CardDescription>Who did what on this project, from the dashboard, the API and the CLI.</CardDescription>
      </CardHeader>
      <CardContent>
        {audit.isLoading ? (
          <Loading />
        ) : audit.error ? (
          <Failed what="the activity" error={audit.error} />
        ) : (audit.data ?? []).length === 0 ? (
          <p className="text-sm text-muted-foreground">Nothing yet.</p>
        ) : (
          <Timeline clip>
            {audit.data!.map((entry, i) => (
              <TimelineItem key={i} type={entry.action === "rollback" ? "warning" : entry.action === "deploy" ? "success" : entry.action === "destroy" ? "error" : "info"}>
                <div>
                  <b>{entry.actor}</b> {entry.action}
                  {entry.target && entry.target !== project.slug && <span> {entry.target}</span>}
                  {entry.detail && <span className="text-muted-foreground"> · {entry.detail}</span>}
                  {entry.realm && entry.realm !== "workspace" && (
                    <Badge variant="outline" className="ml-2">
                      {entry.realm}
                    </Badge>
                  )}
                </div>
                <div className="text-xs text-muted-foreground" title={when(entry.at)}>
                  {ago(entry.at)}
                  {entry.via && <span className="uppercase"> · {entry.via}</span>}
                </div>
              </TimelineItem>
            ))}
          </Timeline>
        )}
      </CardContent>
    </Card>
  );
}
