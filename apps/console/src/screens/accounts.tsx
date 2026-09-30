"use client";

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { KeyRound, Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { Avatar } from "@shpyrd/ui/components/avatar";
import { Badge } from "@shpyrd/ui/components/badge";
import { Button } from "@shpyrd/ui/components/button";
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@shpyrd/ui/components/card";
import { ConfirmDialog } from "@shpyrd/ui/components/confirm-dialog";
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from "@shpyrd/ui/components/dialog";
import { Field } from "@shpyrd/ui/components/field";
import { InlineCode } from "@shpyrd/ui/components/inline-code";
import { Input } from "@shpyrd/ui/components/input";
import { Stack } from "@shpyrd/ui/components/stack";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@shpyrd/ui/components/table";
import { api } from "@/api/api";
import type { LocalUser } from "@/api/types";
import { usePerms } from "@/lib/perms";
import { ago, Failed, Loading } from "./shared";

// The accounts of the auth-local extension: who signs in with an email
// and a password. Every account is an administrator until roles arrive.
export function Accounts() {
  const perms = usePerms();
  const queries = useQueryClient();
  const users = useQuery({ queryKey: ["users"], queryFn: api.users, retry: false });
  const refresh = () => queries.invalidateQueries({ queryKey: ["users"] });
  const remove = useMutation({
    mutationFn: (u: LocalUser) => api.removeUser(u.email),
    onSuccess: (_, u) => {
      toast.success(`${u.email} removed`, { description: "Their sessions end when they expire." });
      refresh();
    },
    onError: (e: Error) => toast.error(e.message),
  });
  return (
    <Card>
      <CardHeader>
        <CardTitle>Accounts</CardTitle>
        <CardDescription>
          Who signs in here with an email and a password. Also from the CLI: <InlineCode>shpyrd users add you@example.com</InlineCode>.
        </CardDescription>
        <CardAction>
          <UserDialog onDone={refresh} />
        </CardAction>
      </CardHeader>
      <CardContent>
        {users.isLoading ? (
          <Loading />
        ) : users.error ? (
          <Failed what="the accounts" error={users.error} />
        ) : (users.data ?? []).length === 0 ? (
          <p className="text-sm text-muted-foreground">No account yet.</p>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Account</TableHead>
                <TableHead>Made</TableHead>
                <TableHead className="text-right" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {users.data!.map((u) => {
                const self = perms.me?.email?.toLowerCase() === u.email.toLowerCase();
                return (
                  <TableRow key={u.email}>
                    <TableCell>
                      <Stack direction="horizontal" align="center" gap="condensed">
                        <Avatar size={24} alt={u.name ?? u.email} />
                        <div>
                          <div className="font-medium">{u.name ?? u.email}</div>
                          <div className="text-xs text-muted-foreground">{u.email}</div>
                        </div>
                        {self && <Badge variant="outline">you</Badge>}
                      </Stack>
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">{ago(u.createdAt)}</TableCell>
                    <TableCell className="text-right">
                      <Stack direction="horizontal" gap="tight" justify="end">
                        <UserDialog onDone={refresh} passwordOf={u} />
                        <ConfirmDialog
                          trigger={<Button variant="ghost" size="icon-xs" icon={<Trash2 />} aria-label={`Remove ${u.email}`} disabled={self} title={self ? "Not the account you are signed in with" : undefined} />}
                          variant="destructive"
                          title={`Remove ${u.email}?`}
                          description="Their sessions end when they expire. The roles they hold in workspaces stay with the address."
                          action="Remove"
                          onConfirm={() => remove.mutate(u)}
                        />
                      </Stack>
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

// A new account, or a new password for one.
function UserDialog({ onDone, passwordOf }: { onDone: () => void; passwordOf?: LocalUser }) {
  const [open, setOpen] = useState(false);
  const [email, setEmail] = useState("");
  const [name, setName] = useState("");
  const [password, setPassword] = useState("");
  const [repeat, setRepeat] = useState("");
  const reset = () => {
    setEmail("");
    setName("");
    setPassword("");
    setRepeat("");
  };
  const save = useMutation({
    mutationFn: () => (passwordOf ? api.setUserPassword(passwordOf.email, password) : api.createUser({ email: email.trim(), name: name.trim() || undefined, password }).then(() => undefined)),
    onSuccess: () => {
      toast.success(passwordOf ? `The password of ${passwordOf.email} changed` : `${email.trim()} made`);
      setOpen(false);
      reset();
      onDone();
    },
    onError: (e: Error) => toast.error(e.message),
  });
  const valid = password.length >= 8 && password === repeat && (passwordOf || /^\S+@\S+\.\S+$/.test(email.trim()));
  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        setOpen(o);
        if (!o) reset();
      }}
    >
      <DialogTrigger asChild>
        {passwordOf ? (
          <Button variant="outline" size="xs" icon={<KeyRound />}>
            Password
          </Button>
        ) : (
          <Button size="sm" icon={<Plus />}>
            New account
          </Button>
        )}
      </DialogTrigger>
      <DialogContent>
        <DialogHeader divider>
          <DialogTitle>{passwordOf ? `A new password for ${passwordOf.email}` : "New account"}</DialogTitle>
          <DialogDescription>{passwordOf ? "It takes effect at their next sign-in." : "The person signs in with this email and password. A password has at least 8 characters."}</DialogDescription>
        </DialogHeader>
        <form
          className="grid gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            if (valid) save.mutate();
          }}
        >
          {!passwordOf && (
            <div className="grid gap-4 sm:grid-cols-2">
              <Field label="Email">
                <Input type="email" value={email} onChange={(e) => setEmail(e.target.value)} autoFocus />
              </Field>
              <Field label="Name" hint="Optional.">
                <Input value={name} onChange={(e) => setName(e.target.value)} />
              </Field>
            </div>
          )}
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="Password">
              <Input type="password" autoComplete="new-password" value={password} onChange={(e) => setPassword(e.target.value)} autoFocus={!!passwordOf} />
            </Field>
            <Field label="Once more" error={repeat && password !== repeat ? "They do not match." : undefined}>
              <Input type="password" autoComplete="new-password" value={repeat} onChange={(e) => setRepeat(e.target.value)} />
            </Field>
          </div>
          <DialogFooter>
            <DialogClose asChild>
              <Button type="button" variant="outline">
                Cancel
              </Button>
            </DialogClose>
            <Button type="submit" disabled={!valid || save.isPending}>
              {passwordOf ? "Change it" : "Make it"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
