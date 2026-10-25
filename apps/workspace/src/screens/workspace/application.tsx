"use client";

import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus, RefreshCw, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@shpyrd/ui/components/button";
import { Card, CardAction, CardContent, CardHeader, CardTitle } from "@shpyrd/ui/components/card";
import { ConfirmDialog } from "@shpyrd/ui/components/confirm-dialog";
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from "@shpyrd/ui/components/dialog";
import { Field } from "@shpyrd/ui/components/field";
import { Input } from "@shpyrd/ui/components/input";
import { Stack } from "@shpyrd/ui/components/stack";
import { StatusBadge } from "@shpyrd/ui/components/status-badge";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@shpyrd/ui/components/table";
import { api } from "@/api/api";
import type { WorkspaceUI } from "@/api/types";
import { usePerms } from "@/lib/perms";
import { ago } from "@/lib/project";
import { Failed, Loading } from "../project/shared";

// The application this workspace's door serves (RFC-0084): the
// platform's own, or a build of the workspace's own published to a
// registry, named by a reference. And the read credentials of the
// registries it is pulled from, one per host.
export function Application() {
  return (
    <>
      <Chosen />
      <RegistryCredentials />
    </>
  );
}

const states: Record<NonNullable<WorkspaceUI["status"]>["state"], { type: "success" | "error" | "warning" | "neutral"; word: string }> = {
  serving: { type: "success", word: "Serving" },
  failed: { type: "error", word: "Not served" },
  pending: { type: "neutral", word: "Not loaded yet" },
  disabled: { type: "warning", word: "Not allowed here" },
};

function Chosen() {
  const perms = usePerms();
  const queries = useQueryClient();
  const workspace = useQuery({ queryKey: ["workspace"], queryFn: api.workspace });
  const chosen = workspace.data?.ui;
  const [ref, setRef] = useState("");
  useEffect(() => setRef(chosen?.ref ?? ""), [chosen?.ref]);
  const done = (message: string) => {
    queries.invalidateQueries({ queryKey: ["workspace"] });
    queries.invalidateQueries({ queryKey: ["config"] });
    toast.success(message);
  };
  const set = useMutation({
    mutationFn: (ui: string) => api.updateWorkspace({ ui }),
    onSuccess: (_, ui) => done(ui ? "The door serves the application" : "The door serves the platform's own application"),
    onError: (e: Error) => toast.error(e.message),
  });
  const reload = useMutation({
    mutationFn: () => api.reloadWorkspaceUI(),
    onSuccess: () => done("Loaded again on the next request"),
    onError: (e: Error) => toast.error(e.message),
  });
  const status = chosen?.status;
  const look = status ? states[status.state] : undefined;
  return (
    <Card>
      <CardHeader>
        <CardTitle>Application</CardTitle>
        {chosen && look && (
          <CardAction>
            <StatusBadge type={look.type} live={status?.state === "pending"}>
              {look.word}
            </StatusBadge>
          </CardAction>
        )}
      </CardHeader>
      <CardContent>
        {workspace.isLoading ? (
          <Loading />
        ) : workspace.error ? (
          <Failed what="the workspace" error={workspace.error} />
        ) : (
          <Stack gap="normal">
            {chosen ? (
              <p className="text-sm text-muted-foreground">
                {status?.name || status?.version ? (
                  <>
                    <span className="font-medium text-foreground">{[status.name, status.version].filter(Boolean).join(" ")}</span>,{" "}
                  </>
                ) : null}
                set {chosen.setBy ? `by ${chosen.setBy} ` : ""}
                {chosen.setAt ? ago(chosen.setAt) : ""}.{status?.error ? ` ${status.error}` : ""}
              </p>
            ) : (
              <p className="text-sm text-muted-foreground">The platform's own application.</p>
            )}
            <Field label="Reference" hint="oci://host/repository:tag or @sha256:digest. A tag is settled to its digest now; what the door serves never moves on its own. Empty is the platform's own.">
              <Input value={ref} onChange={(e) => setRef(e.target.value)} placeholder="oci://ghcr.io/acme/workspace-ui:2.3.1" disabled={!perms.admin} className="font-mono" />
            </Field>
            {perms.admin && (
              <div className="flex flex-wrap items-center gap-2">
                <Button size="sm" disabled={ref.trim() === (chosen?.ref ?? "") || set.isPending} onClick={() => set.mutate(ref.trim())}>
                  {ref.trim() ? "Serve it" : "Serve the platform's own"}
                </Button>
                {chosen && (
                  <>
                    <Button size="sm" variant="outline" disabled={set.isPending} onClick={() => set.mutate("")}>
                      Reset to default
                    </Button>
                    <Button size="sm" variant="ghost" icon={<RefreshCw />} disabled={reload.isPending} onClick={() => reload.mutate()}>
                      Load again
                    </Button>
                  </>
                )}
              </div>
            )}
          </Stack>
        )}
      </CardContent>
    </Card>
  );
}

function RegistryCredentials() {
  const perms = usePerms();
  const queries = useQueryClient();
  const tokens = useQuery({ queryKey: ["registry-tokens"], queryFn: api.registryTokens });
  const refresh = () => queries.invalidateQueries({ queryKey: ["registry-tokens"] });
  const remove = useMutation({
    mutationFn: (host: string) => api.unsetRegistryToken(host),
    onSuccess: (_, host) => {
      refresh();
      toast.success(`Credential for ${host} removed`);
    },
    onError: (e: Error) => toast.error(e.message),
  });
  return (
    <Card>
      <CardHeader>
        <CardTitle>Registry credentials</CardTitle>
        {perms.admin && (
          <CardAction>
            <AddCredential onDone={refresh} />
          </CardAction>
        )}
      </CardHeader>
      <CardContent>
        {tokens.isLoading ? (
          <Loading />
        ) : tokens.error ? (
          <Failed what="the credentials" error={tokens.error} />
        ) : !tokens.data?.length ? (
          <p className="text-sm text-muted-foreground">None. Public repositories need none.</p>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Host</TableHead>
                <TableHead>User</TableHead>
                <TableHead>Set by</TableHead>
                <TableHead className="text-right" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {tokens.data.map((t) => (
                <TableRow key={t.host}>
                  <TableCell className="font-mono text-xs">{t.host}</TableCell>
                  <TableCell>{t.username}</TableCell>
                  <TableCell className="text-xs text-muted-foreground">
                    {t.updatedBy ? `${t.updatedBy} · ` : ""}
                    {ago(t.updatedAt)}
                  </TableCell>
                  <TableCell className="text-right">
                    {perms.admin && (
                      <ConfirmDialog
                        trigger={<Button variant="ghost" size="icon-xs" icon={<Trash2 />} aria-label={`Remove the credential for ${t.host}`} />}
                        variant="destructive"
                        title={`Remove the credential for ${t.host}?`}
                        description="A private repository there can no longer be pulled; an application already loaded keeps serving until it is loaded again."
                        action="Remove"
                        onConfirm={() => remove.mutate(t.host)}
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

function AddCredential({ onDone }: { onDone: () => void }) {
  const [open, setOpen] = useState(false);
  const [host, setHost] = useState("");
  const [username, setUsername] = useState("");
  const [token, setToken] = useState("");
  const add = useMutation({
    mutationFn: () => api.setRegistryToken(host.trim().toLowerCase(), { username: username.trim(), token: token.trim() }),
    onSuccess: (t) => {
      setOpen(false);
      setHost("");
      setUsername("");
      setToken("");
      onDone();
      toast.success(`Credential for ${t.host} set`);
    },
    onError: (e: Error) => toast.error(e.message),
  });
  const valid = host.trim() !== "" && username.trim() !== "" && token.trim() !== "";
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button size="sm" icon={<Plus />}>
          Add credential
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Add a registry credential</DialogTitle>
          <DialogDescription>A read-only token for one registry host. It replaces the host's credential if one is set.</DialogDescription>
        </DialogHeader>
        <form
          className="grid gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            if (valid) add.mutate();
          }}
        >
          <Field label="Host">
            <Input value={host} onChange={(e) => setHost(e.target.value)} placeholder="ghcr.io" autoFocus className="font-mono" />
          </Field>
          <Field label="User name">
            <Input value={username} onChange={(e) => setUsername(e.target.value)} placeholder="acme-bot" />
          </Field>
          <Field label="Token">
            <Input type="password" value={token} onChange={(e) => setToken(e.target.value)} className="font-mono" />
          </Field>
          <DialogFooter>
            <DialogClose asChild>
              <Button type="button" variant="outline">
                Cancel
              </Button>
            </DialogClose>
            <Button type="submit" disabled={!valid || add.isPending}>
              Add
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
