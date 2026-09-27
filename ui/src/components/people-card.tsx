import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Copy, Mail, RefreshCw, Trash2, UserPlus } from "lucide-react";

import {
  api,
  WORKSPACE_ROLES,
  type Invitation,
  type InviteResult,
  type Person,
  type WorkspaceRole,
} from "@/lib/api";
import { ago } from "@/lib/format";
import { usePerms } from "@/lib/me";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

/**
 * People and their workspace roles (RFC-0033): everyone who has signed in
 * or holds a role, with the role set from this list; owners name owners.
 * Invitations bring new people in: a link shown once, emailed when the
 * platform can send mail.
 */
export function PeopleCard() {
  const qc = useQueryClient();
  const perms = usePerms();
  const people = useQuery({
    queryKey: ["people"],
    queryFn: api.people,
    retry: false,
  });
  const refresh = () => {
    qc.invalidateQueries({ queryKey: ["people"] });
    qc.invalidateQueries({ queryKey: ["workspace"] });
    qc.invalidateQueries({ queryKey: ["me"] });
  };
  const forget = useMutation({
    mutationFn: (p: Person) => api.forgetPerson(p.email),
    onSuccess: (_, p) => {
      toast.success(`Forgot ${p.email}`);
      refresh();
    },
    onError: (e: Error) => toast.error(e.message),
  });
  const status = useMutation({
    mutationFn: (p: Person) =>
      api.setPersonStatus(
        p.email,
        p.status === "suspended" ? "active" : "suspended",
      ),
    onSuccess: (r) => {
      toast.success(
        r.status === "suspended"
          ? `Suspended ${r.email}`
          : `Reactivated ${r.email}`,
      );
      refresh();
    },
    onError: (e: Error) => toast.error(e.message),
  });
  const role = useMutation({
    mutationFn: ({ p, role }: { p: Person; role: WorkspaceRole | "" }) =>
      api.setPersonRole(p.email, role),
    onSuccess: (r) => {
      toast.success(
        r.role
          ? `${r.email} is now ${article(r.role)} ${r.role}`
          : `${r.email} has no workspace role`,
      );
      refresh();
    },
    onError: (e: Error) => toast.error(e.message),
  });

  const owners = people.data?.filter((p) => p.role === "owner").length ?? 0;

  return (
    <div className="grid gap-6">
      <Card>
        <CardHeader>
          <div className="flex items-start justify-between gap-4">
            <div className="grid gap-1.5">
              <CardTitle>People</CardTitle>
              <CardDescription>
                Everyone who has signed in to this workspace or holds a role in
                it. <strong>Owners</strong> and <strong>admins</strong>{" "}
                administer the workspace and every project; only owners name
                owners. <strong>Members</strong> may create projects and
                administer the ones they create; project grants and teams give
                everything else. Suspending someone switches their access off at
                once; forgetting someone removes the sign-in record, their role
                and grants stay.
              </CardDescription>
            </div>
            <InviteDialog onDone={refresh} />
          </div>
        </CardHeader>
        <CardContent>
          {people.isLoading && <Skeleton className="h-24 w-full" />}
          {people.error && (
            <p className="text-sm text-destructive">
              {(people.error as Error).message}
            </p>
          )}
          {people.data && people.data.length === 0 && (
            <p className="text-sm text-muted-foreground">
              Nobody has signed in through an account yet (the admin token is
              not a person). Invite someone to give them a role.
            </p>
          )}
          {people.data && people.data.length > 0 && (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Email</TableHead>
                  <TableHead>Name</TableHead>
                  <TableHead>Role</TableHead>
                  <TableHead>Signed in with</TableHead>
                  <TableHead className="text-right">Last seen</TableHead>
                  <TableHead className="w-48" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {people.data.map((p) => (
                  <TableRow key={p.email}>
                    <TableCell className="font-mono text-xs">
                      {p.email}
                    </TableCell>
                    <TableCell>
                      {p.name || (
                        <span className="text-muted-foreground">—</span>
                      )}
                    </TableCell>
                    <TableCell>
                      <RoleSelect
                        value={p.role ?? ""}
                        // Owners may set anything; admins may not touch owners
                        // or name them. The last owner cannot step down.
                        disabled={
                          role.isPending ||
                          (!perms.owner && p.role === "owner") ||
                          p.status === "suspended"
                        }
                        allowOwner={perms.owner}
                        lockedOwner={p.role === "owner" && owners === 1}
                        onChange={(r) => role.mutate({ p, role: r })}
                      />
                      {!p.role && p.platformRole && (
                        <p className="mt-1 text-xs text-muted-foreground">
                          {p.platformRole} through a team
                        </p>
                      )}
                    </TableCell>
                    <TableCell>
                      {p.provider ? (
                        <Badge variant="secondary">{p.provider}</Badge>
                      ) : (
                        <span className="text-xs text-muted-foreground">
                          not yet
                        </span>
                      )}
                      {p.realm && p.realm !== "workspace" && (
                        <Badge variant="outline" className="ml-1">
                          {p.realm}
                        </Badge>
                      )}
                    </TableCell>
                    <TableCell className="text-right text-muted-foreground">
                      {p.lastSeenAt ? ago(p.lastSeenAt) : "never"}
                    </TableCell>
                    <TableCell className="text-right">
                      <div className="flex items-center justify-end gap-1">
                        {p.status === "suspended" && (
                          <Badge variant="destructive">suspended</Badge>
                        )}
                        {p.lastSeenAt && (
                          <>
                            <Button
                              variant="ghost"
                              size="xs"
                              disabled={status.isPending}
                              onClick={() => status.mutate(p)}
                            >
                              {p.status === "suspended"
                                ? "Reactivate"
                                : "Suspend"}
                            </Button>
                            <Button
                              variant="ghost"
                              size="icon"
                              aria-label={`Forget ${p.email}`}
                              onClick={() => forget.mutate(p)}
                            >
                              <Trash2 className="size-4" />
                            </Button>
                          </>
                        )}
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
      <InvitationsCard onChange={refresh} />
    </div>
  );
}

function article(role: string) {
  return /^[aeiou]/.test(role) ? "an" : "a";
}

function RoleSelect({
  value,
  disabled,
  allowOwner,
  lockedOwner,
  onChange,
}: {
  value: WorkspaceRole | "";
  disabled?: boolean;
  allowOwner: boolean;
  lockedOwner: boolean;
  onChange: (role: WorkspaceRole | "") => void;
}) {
  return (
    <Select
      value={value || "none"}
      disabled={disabled}
      onValueChange={(v) => onChange(v === "none" ? "" : (v as WorkspaceRole))}
    >
      <SelectTrigger
        className="h-7 w-32 text-xs"
        title={
          lockedOwner
            ? "The workspace needs an owner: name another owner first"
            : undefined
        }
      >
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {WORKSPACE_ROLES.map((r) => (
          <SelectItem
            key={r}
            value={r}
            disabled={
              (r === "owner" && !allowOwner) || (lockedOwner && r !== "owner")
            }
          >
            {r}
          </SelectItem>
        ))}
        <SelectItem value="none" disabled={lockedOwner}>
          no role
        </SelectItem>
      </SelectContent>
    </Select>
  );
}

function InviteDialog({ onDone }: { onDone: () => void }) {
  const perms = usePerms();
  const teams = useQuery({
    queryKey: ["teams"],
    queryFn: api.teams,
    retry: false,
  });
  const [open, setOpen] = useState(false);
  const [email, setEmail] = useState("");
  const [role, setRole] = useState<WorkspaceRole>("member");
  const [team, setTeam] = useState("");
  const [result, setResult] = useState<InviteResult | null>(null);
  const invite = useMutation({
    mutationFn: () =>
      api.invite({ email: email.trim(), role, team: team || undefined }),
    onSuccess: (r) => {
      setResult(r);
      onDone();
      if (r.applied) {
        toast.success(
          `${r.email} already had access: now ${article(r.role)} ${r.role}`,
        );
      }
    },
    onError: (e: Error) => toast.error(e.message),
  });
  const reset = () => {
    setResult(null);
    setEmail("");
    setRole("member");
    setTeam("");
  };
  const teamChoices = (teams.data ?? []).filter((t) => !t.everyone);

  return (
    <Dialog
      open={open}
      onOpenChange={(v) => {
        setOpen(v);
        if (!v) reset();
      }}
    >
      <DialogTrigger asChild>
        <Button size="sm" variant="outline" className="shrink-0">
          <UserPlus data-icon="inline-start" /> Invite
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Invite someone</DialogTitle>
          <DialogDescription>
            They join with the role you choose the moment they sign in with this
            address. The link works for 7 days.
          </DialogDescription>
        </DialogHeader>
        {result ? (
          <InviteOutcome result={result} onClose={() => setOpen(false)} />
        ) : (
          <form
            className="grid gap-3"
            onSubmit={(e) => {
              e.preventDefault();
              if (email.trim()) invite.mutate();
            }}
          >
            <div className="grid gap-2">
              <Label htmlFor="invite-email">Email</Label>
              <Input
                id="invite-email"
                type="email"
                placeholder="ada@example.com"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                required
                autoFocus
              />
            </div>
            <div className="grid gap-2">
              <Label>Role</Label>
              <Select
                value={role}
                onValueChange={(v) => setRole(v as WorkspaceRole)}
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="member">
                    member — creates projects, administers their own
                  </SelectItem>
                  <SelectItem value="admin">
                    admin — administers the workspace and every project
                  </SelectItem>
                  <SelectItem value="owner" disabled={!perms.owner}>
                    owner — admin, and names owners
                  </SelectItem>
                </SelectContent>
              </Select>
            </div>
            {teamChoices.length > 0 && (
              <div className="grid gap-2">
                <Label>Team (optional)</Label>
                <Select
                  value={team || "none"}
                  onValueChange={(v) => setTeam(v === "none" ? "" : v)}
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="none">no team</SelectItem>
                    {teamChoices.map((t) => (
                      <SelectItem key={t.name} value={t.name}>
                        {t.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            )}
            <DialogFooter>
              <Button
                type="submit"
                disabled={invite.isPending || !email.trim()}
              >
                {invite.isPending ? "Inviting…" : "Invite"}
              </Button>
            </DialogFooter>
          </form>
        )}
      </DialogContent>
    </Dialog>
  );
}

function InviteOutcome({
  result,
  onClose,
}: {
  result: InviteResult;
  onClose: () => void;
}) {
  if (result.applied) {
    return (
      <div className="grid gap-3">
        <p className="text-sm">
          <span className="font-mono text-xs">{result.email}</span> had signed
          in before, so the role applied at once: they are{" "}
          {article(result.role)} <strong>{result.role}</strong>
          {result.team ? (
            <>
              {" "}
              and in team <strong>{result.team}</strong>
            </>
          ) : null}
          .
        </p>
        <DialogFooter>
          <Button onClick={onClose}>Done</Button>
        </DialogFooter>
      </div>
    );
  }
  return (
    <div className="grid gap-3">
      {result.emailed ? (
        <p className="flex items-center gap-2 text-sm">
          <Mail className="size-4 text-muted-foreground" />
          The link was emailed to{" "}
          <span className="font-mono text-xs">{result.email}</span>. You can
          also pass it along yourself:
        </p>
      ) : (
        <p className="text-sm">
          {result.mailError ? (
            <>
              The email could not be sent ({result.mailError}). Send this link
              to <span className="font-mono text-xs">{result.email}</span>{" "}
              yourself:
            </>
          ) : (
            <>
              This platform does not send email yet, so send this link to{" "}
              <span className="font-mono text-xs">{result.email}</span>{" "}
              yourself. It is shown once:
            </>
          )}
        </p>
      )}
      <LinkBox link={result.link ?? ""} />
      <p className="text-xs text-muted-foreground">
        Signing in with that address accepts the invitation, link or no link.
        Invite the same address again for a new link.
      </p>
      <DialogFooter>
        <Button onClick={onClose}>Done</Button>
      </DialogFooter>
    </div>
  );
}

function LinkBox({ link }: { link: string }) {
  return (
    <div className="flex items-start gap-2">
      <pre className="min-w-0 flex-1 rounded bg-muted p-3 text-xs break-all select-all">
        {link}
      </pre>
      <Button
        variant="outline"
        size="icon"
        aria-label="Copy link"
        onClick={() =>
          navigator.clipboard
            .writeText(link)
            .then(() => toast.success("Link copied"))
            .catch(() => toast.error("Could not copy; select the link instead"))
        }
      >
        <Copy className="size-4" />
      </Button>
    </div>
  );
}

function InvitationsCard({ onChange }: { onChange: () => void }) {
  const qc = useQueryClient();
  const perms = usePerms();
  const invitations = useQuery({
    queryKey: ["invitations"],
    queryFn: api.invitations,
    retry: false,
  });
  const refresh = () => {
    qc.invalidateQueries({ queryKey: ["invitations"] });
    onChange();
  };
  const [resent, setResent] = useState<InviteResult | null>(null);
  const revoke = useMutation({
    mutationFn: (inv: Invitation) => api.revokeInvitation(inv.id),
    onSuccess: (_, inv) => {
      toast.success(`Revoked the invitation of ${inv.email}`);
      refresh();
    },
    onError: (e: Error) => toast.error(e.message),
  });
  const resend = useMutation({
    mutationFn: (inv: Invitation) =>
      api.invite({ email: inv.email, role: inv.role, team: inv.team }),
    onSuccess: (r) => {
      setResent(r);
      refresh();
    },
    onError: (e: Error) => toast.error(e.message),
  });

  if (invitations.data && invitations.data.length === 0) return null;
  return (
    <Card>
      <CardHeader>
        <CardTitle>Invitations</CardTitle>
        <CardDescription>
          People invited who have not signed in yet. Inviting again makes a new
          link (and email); revoking closes the door until they are invited
          again.
        </CardDescription>
      </CardHeader>
      <CardContent>
        {invitations.isLoading && <Skeleton className="h-16 w-full" />}
        {invitations.error && (
          <p className="text-sm text-destructive">
            {(invitations.error as Error).message}
          </p>
        )}
        {invitations.data && invitations.data.length > 0 && (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Email</TableHead>
                <TableHead>Role</TableHead>
                <TableHead>Team</TableHead>
                <TableHead>Invited by</TableHead>
                <TableHead className="text-right">Expires</TableHead>
                <TableHead className="w-40" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {invitations.data.map((inv) => (
                <TableRow key={inv.id}>
                  <TableCell className="font-mono text-xs">
                    {inv.email}
                  </TableCell>
                  <TableCell>
                    <Badge variant="secondary">{inv.role}</Badge>
                  </TableCell>
                  <TableCell className="text-xs text-muted-foreground">
                    {inv.team || "—"}
                  </TableCell>
                  <TableCell className="font-mono text-xs text-muted-foreground">
                    {inv.invitedBy || "—"}
                  </TableCell>
                  <TableCell className="text-right text-muted-foreground">
                    {inv.expired ? (
                      <Badge variant="destructive">expired</Badge>
                    ) : (
                      until(inv.expiresAt)
                    )}
                  </TableCell>
                  <TableCell className="text-right">
                    <div className="flex items-center justify-end gap-1">
                      <Button
                        variant="ghost"
                        size="xs"
                        disabled={
                          resend.isPending ||
                          (inv.role === "owner" && !perms.owner)
                        }
                        onClick={() => resend.mutate(inv)}
                      >
                        <RefreshCw data-icon="inline-start" /> New link
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon"
                        aria-label={`Revoke the invitation of ${inv.email}`}
                        disabled={revoke.isPending}
                        onClick={() => revoke.mutate(inv)}
                      >
                        <Trash2 className="size-4" />
                      </Button>
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>
      <Dialog open={!!resent} onOpenChange={(v) => !v && setResent(null)}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>New invitation link</DialogTitle>
            <DialogDescription>
              The previous link stopped working.
            </DialogDescription>
          </DialogHeader>
          {resent && (
            <InviteOutcome result={resent} onClose={() => setResent(null)} />
          )}
        </DialogContent>
      </Dialog>
    </Card>
  );
}

function until(iso: string) {
  const ms = new Date(iso).getTime() - Date.now();
  const days = Math.round(ms / 86_400_000);
  if (days >= 2) return `in ${days} days`;
  const hours = Math.round(ms / 3_600_000);
  if (hours >= 1) return `in ${hours} h`;
  return "soon";
}
