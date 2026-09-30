"use client";

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Copy, Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@shpyrd/ui/components/button";
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@shpyrd/ui/components/card";
import { ConfirmDialog } from "@shpyrd/ui/components/confirm-dialog";
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from "@shpyrd/ui/components/dialog";
import { Field } from "@shpyrd/ui/components/field";
import { InlineCode } from "@shpyrd/ui/components/inline-code";
import { Input } from "@shpyrd/ui/components/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@shpyrd/ui/components/select";
import { Stack } from "@shpyrd/ui/components/stack";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@shpyrd/ui/components/table";
import { api } from "@/api/api";
import type { PlatformRole, ProjectRole } from "@/api/types";
import { usePerms } from "@/lib/perms";
import { ago } from "@/lib/project";
import { Failed, Loading, when } from "../project/shared";

const projectRoles: ProjectRole[] = ["reader", "user", "viewer", "developer", "admin"];
const platformRoles: PlatformRole[] = ["platform-viewer", "platform-admin"];
const rank = (order: readonly string[], role?: string) => order.indexOf(role ?? "");

// Credentials for CI, scripts and integrations, with at most the roles
// of who makes them. The value is shown once.
export function Tokens() {
  const queries = useQueryClient();
  const tokens = useQuery({ queryKey: ["tokens"], queryFn: api.tokens });
  const refresh = () => queries.invalidateQueries({ queryKey: ["tokens"] });
  const revoke = useMutation({
    mutationFn: (id: string) => api.revokeToken(id),
    onSuccess: () => {
      refresh();
      toast.success("Token revoked");
    },
    onError: (e: Error) => toast.error(e.message),
  });
  return (
    <Card>
      <CardHeader>
        <CardTitle>API tokens</CardTitle>
        <CardDescription>
          For CI, scripts and integrations. Store the value in <InlineCode>SHPYRD_TOKEN</InlineCode> or pass it to <InlineCode>shpyrd login --token</InlineCode>. A token has at most the roles you have when you make it.
        </CardDescription>
        <CardAction>
          <NewToken onDone={refresh} />
        </CardAction>
      </CardHeader>
      <CardContent>
        {tokens.isLoading ? (
          <Loading />
        ) : tokens.error ? (
          <Failed what="the tokens" error={tokens.error} />
        ) : (tokens.data ?? []).length === 0 ? (
          <p className="text-sm text-muted-foreground">None yet.</p>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Token</TableHead>
                <TableHead>Whose</TableHead>
                <TableHead>May</TableHead>
                <TableHead>Last used</TableHead>
                <TableHead>Until</TableHead>
                <TableHead className="text-right" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {tokens.data!.map((t) => {
                const expired = t.expiresAt ? new Date(t.expiresAt) < new Date() : false;
                return (
                  <TableRow key={t.id}>
                    <TableCell className="font-medium">
                      {t.name} <span className="ml-1 font-mono text-xs text-muted-foreground">{t.id.slice(0, 8)}</span>
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">{t.ownerEmail ?? "-"}</TableCell>
                    <TableCell className="text-xs">
                      {t.platformRole ? (
                        <InlineCode>{t.platformRole}</InlineCode>
                      ) : t.projectRoles ? (
                        Object.entries(t.projectRoles).map(([p, r]) => (
                          <span key={p} className="mr-2">
                            {p} <span className="text-muted-foreground">as {r}</span>
                          </span>
                        ))
                      ) : (
                        <span className="text-muted-foreground">what its owner may</span>
                      )}
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">{t.lastUsedAt ? ago(t.lastUsedAt) : "never"}</TableCell>
                    <TableCell className={`text-xs ${expired ? "text-destructive" : "text-muted-foreground"}`}>{t.expiresAt ? (expired ? "expired" : when(t.expiresAt)) : "no end"}</TableCell>
                    <TableCell className="text-right">
                      <ConfirmDialog
                        trigger={<Button variant="ghost" size="icon-xs" icon={<Trash2 />} aria-label={`Revoke ${t.name}`} />}
                        variant="destructive"
                        title={`Revoke ${t.name}?`}
                        description="Whatever uses it stops working at once."
                        action="Revoke"
                        onConfirm={() => revoke.mutate(t.id)}
                      />
                    </TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  );
}

function NewToken({ onDone }: { onDone: () => void }) {
  const perms = usePerms();
  const projects = useQuery({ queryKey: ["projects"], queryFn: api.projects });
  const mine = perms.me?.roles?.projects ?? {};
  const myPlatform = perms.me?.roles?.platform || undefined;
  // A platform admin, or anyone while roles are not enforced, may give any role.
  const anyRole = myPlatform === "platform-admin" || !perms.enforced || perms.admin;
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [scope, setScope] = useState<"platform" | "project">(myPlatform ? "platform" : "project");
  const [platformRole, setPlatformRole] = useState<string>(myPlatform ?? "platform-viewer");
  const [project, setProject] = useState("");
  const [projectRole, setProjectRole] = useState<string>("developer");
  const [expires, setExpires] = useState("90d");
  const [secret, setSecret] = useState<string | null>(null);
  const platformChoices = platformRoles.filter((r) => anyRole || rank(platformRoles, r) <= rank(platformRoles, myPlatform));
  const topRole = anyRole ? "admin" : mine[project];
  const projectChoices = projectRoles.filter((r) => rank(projectRoles, r) <= rank(projectRoles, topRole));
  const projectChoicesShown = projectChoices.length ? projectChoices : projectRoles;
  const create = useMutation({
    mutationFn: () => api.createToken({ name: name.trim(), platformRole: scope === "platform" ? platformRole : undefined, projectRoles: scope === "project" && project ? { [project]: projectRole } : undefined, expiresIn: expires === "never" ? undefined : expires }),
    onSuccess: (r) => {
      onDone();
      setSecret(r.secret);
    },
    onError: (e: Error) => toast.error(e.message),
  });
  const close = (next: boolean) => {
    setOpen(next);
    if (!next) {
      setSecret(null);
      setName("");
      setProject("");
    }
  };
  const canCreate = platformChoices.length > 0 || Object.keys(mine).length > 0 || anyRole;
  return (
    <Dialog open={open} onOpenChange={close}>
      <DialogTrigger asChild>
        <Button size="sm" icon={<Plus />} disabled={!canCreate} title={canCreate ? undefined : "You have no role to give a token"}>
          New token
        </Button>
      </DialogTrigger>
      <DialogContent>
        {secret ? (
          <>
            <DialogHeader divider>
              <DialogTitle>The token {name}</DialogTitle>
              <DialogDescription>This is the only time it is shown. Copy it now.</DialogDescription>
            </DialogHeader>
            <Input readOnly value={secret} className="font-mono text-xs" onFocus={(e) => e.currentTarget.select()} />
            <p className="text-xs text-muted-foreground">
              Set <InlineCode>SHPYRD_TOKEN</InlineCode> in your CI, or run <InlineCode>shpyrd login --url {typeof window === "undefined" ? "" : window.location.origin} --token …</InlineCode>
            </p>
            <DialogFooter>
              <Button
                variant="outline"
                icon={<Copy />}
                onClick={() => {
                  void navigator.clipboard?.writeText(secret);
                  toast.success("Copied");
                }}
              >
                Copy
              </Button>
              <Button onClick={() => close(false)}>Done</Button>
            </DialogFooter>
          </>
        ) : (
          <>
            <DialogHeader divider>
              <DialogTitle>New token</DialogTitle>
              <DialogDescription>It has the role you give it, never more than yours, and ends when you say. Name it after where it lives.</DialogDescription>
            </DialogHeader>
            <Stack gap="normal">
              <Field label="Name">
                <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="ci-deploy" autoFocus />
              </Field>
              {platformChoices.length > 0 && (
                <Field label="Scope">
                  <Select value={scope} onValueChange={(v) => setScope(v as typeof scope)}>
                    <SelectTrigger className="w-full">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="platform">The whole workspace</SelectItem>
                      <SelectItem value="project">One project</SelectItem>
                    </SelectContent>
                  </Select>
                </Field>
              )}
              {scope === "platform" && platformChoices.length > 0 ? (
                <Field label="Role">
                  <Select value={platformRole} onValueChange={setPlatformRole}>
                    <SelectTrigger className="w-full">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {platformChoices.map((r) => (
                        <SelectItem key={r} value={r}>
                          {r === "platform-admin" ? "platform-admin: everything" : "platform-viewer: reads everything"}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </Field>
              ) : (
                <div className="grid gap-4 sm:grid-cols-2">
                  <Field label="Project">
                    <Select
                      value={project}
                      onValueChange={(v) => {
                        setProject(v);
                        if (!anyRole) setProjectRole(mine[v] ?? "user");
                      }}
                    >
                      <SelectTrigger className="w-full">
                        <SelectValue placeholder="Choose a project" />
                      </SelectTrigger>
                      <SelectContent>
                        {(anyRole ? (projects.data ?? []).map((p) => p.slug) : Object.keys(mine).sort()).map((slug) => (
                          <SelectItem key={slug} value={slug}>
                            {slug}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </Field>
                  <Field label="Role">
                    <Select value={projectRole} onValueChange={setProjectRole}>
                      <SelectTrigger className="w-full">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        {projectChoicesShown.map((r) => (
                          <SelectItem key={r} value={r}>
                            {r}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </Field>
                </div>
              )}
              <Field label="Lasts">
                <Select value={expires} onValueChange={setExpires}>
                  <SelectTrigger className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="30d">30 days</SelectItem>
                    <SelectItem value="90d">90 days</SelectItem>
                    <SelectItem value="365d">A year</SelectItem>
                    <SelectItem value="3650d">Ten years</SelectItem>
                  </SelectContent>
                </Select>
              </Field>
            </Stack>
            <DialogFooter>
              <DialogClose asChild>
                <Button variant="outline">Cancel</Button>
              </DialogClose>
              <Button disabled={!name.trim() || create.isPending || (scope === "project" && !project)} onClick={() => create.mutate()}>
                Create
              </Button>
            </DialogFooter>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}
