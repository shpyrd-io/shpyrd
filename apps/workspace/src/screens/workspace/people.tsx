"use client";

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Ban, Copy, Plus, Trash2, UserRoundCheck } from "lucide-react";
import { toast } from "sonner";
import { Avatar } from "@shpyrd/ui/components/avatar";
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
import { api } from "@/api/api";
import type { InviteResult, WorkspaceRole } from "@/api/types";
import { usePerms } from "@/lib/perms";
import { ago } from "@/lib/project";
import { Failed, Loading, when } from "../project/shared";

const roles: Record<WorkspaceRole, string> = { owner: "everything, the workspace itself included", admin: "every project and person", member: "makes projects, operates their own" };

// Who is here, with what role, and who was asked in.
export function People() {
  const perms = usePerms();
  const queries = useQueryClient();
  const people = useQuery({ queryKey: ["people"], queryFn: api.people });
  const invitations = useQuery({ queryKey: ["invitations"], queryFn: api.invitations });
  const refresh = () => {
    queries.invalidateQueries({ queryKey: ["people"] });
    queries.invalidateQueries({ queryKey: ["invitations"] });
  };
  const setRole = useMutation({
    mutationFn: ({ email, role }: { email: string; role: WorkspaceRole }) => api.setPersonRole(email, role),
    onSuccess: () => {
      refresh();
      toast.success("Role set");
    },
    onError: (e: Error) => toast.error(e.message),
  });
  const setStatus = useMutation({
    mutationFn: ({ email, status }: { email: string; status: "active" | "suspended" }) => api.setPersonStatus(email, status),
    onSuccess: (_, { status }) => {
      refresh();
      toast.success(status === "suspended" ? "Suspended: they cannot sign in until let back" : "Let back in");
    },
    onError: (e: Error) => toast.error(e.message),
  });
  const remove = useMutation({
    mutationFn: (email: string) => api.removePerson(email),
    onSuccess: () => {
      refresh();
      toast.success("Removed from the workspace");
    },
    onError: (e: Error) => toast.error(e.message),
  });
  const revoke = useMutation({
    mutationFn: (id: string) => api.revokeInvitation(id),
    onSuccess: () => {
      refresh();
      toast.success("Invitation withdrawn");
    },
    onError: (e: Error) => toast.error(e.message),
  });
  return (
    <>
      <Card>
        <CardHeader>
          <CardTitle>People</CardTitle>
          <CardDescription>Everyone who signed in here, and what each may do in the workspace.</CardDescription>
          <CardAction>
            <Invite onDone={refresh} canOwner={perms.owner} />
          </CardAction>
        </CardHeader>
        <CardContent>
          {people.isLoading ? (
            <Loading />
          ) : people.error ? (
            <Failed what="the people" error={people.error} />
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Person</TableHead>
                  <TableHead>Role</TableHead>
                  <TableHead>Signs in with</TableHead>
                  <TableHead>Last seen</TableHead>
                  <TableHead className="text-right" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {people.data!.map((p) => (
                  <TableRow key={p.email}>
                    <TableCell>
                      <Stack direction="horizontal" align="center" gap="condensed">
                        <Avatar size={24} alt={p.name ?? p.email} />
                        <div>
                          <div className="font-medium">{p.name ?? p.email}</div>
                          <div className="text-xs text-muted-foreground">{p.email}</div>
                        </div>
                        {p.status === "suspended" && <StatusBadge type="warning">Suspended</StatusBadge>}
                        {p.platformRole && <StatusBadge type="info">{p.platformRole}</StatusBadge>}
                      </Stack>
                    </TableCell>
                    <TableCell>
                      <Select value={p.role ?? "member"} disabled={(p.role === "owner" && !perms.owner) || setRole.isPending} onValueChange={(role) => setRole.mutate({ email: p.email, role: role as WorkspaceRole })}>
                        <SelectTrigger size="sm" className="w-28">
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          {(Object.keys(roles) as WorkspaceRole[]).map((r) => (
                            <SelectItem key={r} value={r} disabled={r === "owner" && !perms.owner}>
                              {r}
                            </SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">{p.provider ?? "-"}</TableCell>
                    <TableCell className="text-xs text-muted-foreground">{p.lastSeenAt ? ago(p.lastSeenAt) : "never"}</TableCell>
                    <TableCell className="text-right">
                      {(p.role !== "owner" || perms.owner) && p.role !== "owner" && (
                        <Button
                          variant="outline"
                          size="xs"
                          icon={p.status === "suspended" ? <UserRoundCheck /> : <Ban />}
                          disabled={setStatus.isPending}
                          onClick={() => setStatus.mutate({ email: p.email, status: p.status === "suspended" ? "active" : "suspended" })}
                        >
                          {p.status === "suspended" ? "Let back in" : "Suspend"}
                        </Button>
                      )}
                      {(p.role !== "owner" || perms.owner) && (
                        <ConfirmDialog
                          trigger={<Button variant="ghost" size="icon-xs" icon={<Trash2 />} aria-label={`Remove ${p.email}`} />}
                          variant="destructive"
                          title={`Remove ${p.name ?? p.email}?`}
                          description="They lose every role here. Their account stays, on the platform."
                          action="Remove"
                          onConfirm={() => remove.mutate(p.email)}
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
      <Card>
        <CardHeader>
          <CardTitle>Invitations</CardTitle>
          <CardDescription>Asked in, not yet here. An invitation lasts a week.</CardDescription>
        </CardHeader>
        <CardContent>
          {invitations.isLoading ? (
            <Loading rows={1} />
          ) : (invitations.data ?? []).length === 0 ? (
            <p className="text-sm text-muted-foreground">None open.</p>
          ) : (
            <Table variant="secondary">
              <TableBody>
                {invitations.data!.map((i) => (
                  <TableRow key={i.id}>
                    <TableCell className="font-medium">{i.email}</TableCell>
                    <TableCell>
                      {i.role}
                      {i.team && <span className="text-xs text-muted-foreground"> · {i.team}</span>}
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">by {i.invitedBy ?? "-"}</TableCell>
                    <TableCell className="text-xs text-muted-foreground">{i.expired ? "expired" : `until ${when(i.expiresAt)}`}</TableCell>
                    <TableCell className="text-right">
                      <Button variant="ghost" size="icon-xs" icon={<Trash2 />} aria-label="Withdraw" onClick={() => revoke.mutate(i.id)} />
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </>
  );
}

function Invite({ onDone, canOwner }: { onDone: () => void; canOwner: boolean }) {
  const [open, setOpen] = useState(false);
  const [email, setEmail] = useState("");
  const [role, setRole] = useState<WorkspaceRole>("member");
  const [team, setTeam] = useState("");
  const [result, setResult] = useState<InviteResult | null>(null);
  const teams = useQuery({ queryKey: ["teams"], queryFn: api.teams, enabled: open });
  const choices = (teams.data ?? []).filter((t) => !t.everyone);
  const valid = /^\S+@\S+\.\S+$/.test(email);
  const invite = useMutation({
    mutationFn: () => api.invite({ email: email.trim(), role, team: team || undefined }),
    onSuccess: (r) => {
      onDone();
      setResult(r);
      if (r.applied) toast.success(`${r.email} was already here: now ${r.role}`);
    },
    onError: (e: Error) => toast.error(e.message),
  });
  const close = (next: boolean) => {
    setOpen(next);
    if (!next) {
      setResult(null);
      setEmail("");
      setRole("member");
      setTeam("");
    }
  };
  return (
    <Dialog open={open} onOpenChange={close}>
      <DialogTrigger asChild>
        <Button size="sm" icon={<Plus />}>
          Invite
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader divider>
          <DialogTitle>Invite someone</DialogTitle>
          <DialogDescription>They join with the role you choose the moment they sign in with this address. The link works for a week.</DialogDescription>
        </DialogHeader>
        {result ? (
          <Stack gap="normal">
            {result.applied ? (
              <p className="text-sm">
                <InlineCode>{result.email}</InlineCode> had signed in before, so the role applied at once: they are <b className="font-medium">{result.role}</b>
                {result.team && (
                  <>
                    {" "}
                    and in the team <b className="font-medium">{result.team}</b>
                  </>
                )}
                .
              </p>
            ) : (
              <>
                <p className="text-sm">{result.emailed ? "An email with the link went out. Here it is too, in case it is easier to hand over:" : "No email went out: hand them this link."}</p>
                {result.link && <Input readOnly value={result.link} className="font-mono text-xs" onFocus={(e) => e.currentTarget.select()} />}
                {result.mailError && <p className="text-xs text-muted-foreground">{result.mailError}</p>}
              </>
            )}
            <DialogFooter>
              {result.link && (
                <Button
                  variant="outline"
                  icon={<Copy />}
                  onClick={() => {
                    void navigator.clipboard?.writeText(result.link!);
                    toast.success("Copied");
                  }}
                >
                  Copy the link
                </Button>
              )}
              <Button onClick={() => close(false)}>Done</Button>
            </DialogFooter>
          </Stack>
        ) : (
          <>
            <Stack gap="normal">
              <Field label="Email">
                <Input type="email" value={email} onChange={(e) => setEmail(e.target.value)} placeholder="carla@acme.com" autoFocus />
              </Field>
              <Field label="Role" hint={roles[role]}>
                <Select value={role} onValueChange={(v) => setRole(v as WorkspaceRole)}>
                  <SelectTrigger className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {(Object.keys(roles) as WorkspaceRole[]).map((r) => (
                      <SelectItem key={r} value={r} disabled={r === "owner" && !canOwner}>
                        {r}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
              {choices.length > 0 && (
                <Field label="Team" hint="Optional: they are in it from the start.">
                  <Select value={team || "none"} onValueChange={(v) => setTeam(v === "none" ? "" : v)}>
                    <SelectTrigger className="w-full">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="none">No team</SelectItem>
                      {choices.map((t) => (
                        <SelectItem key={t.name} value={t.name}>
                          {t.name}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </Field>
              )}
            </Stack>
            <DialogFooter>
              <DialogClose asChild>
                <Button variant="outline">Cancel</Button>
              </DialogClose>
              <Button disabled={!valid || invite.isPending} onClick={() => invite.mutate()}>
                Invite
              </Button>
            </DialogFooter>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}
