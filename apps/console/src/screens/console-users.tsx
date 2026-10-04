"use client";

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";
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
import type { ConsoleUser } from "@/api/types";
import { usePerms } from "@/lib/perms";
import { ago, Failed, Loading } from "./shared";

// Who may open this console: its own list, by email, independent of every
// workspace. All of them are its admins for now.
export function ConsoleUsers() {
  const perms = usePerms();
  const queries = useQueryClient();
  const users = useQuery({ queryKey: ["console-users"], queryFn: api.consoleUsers });
  const refresh = () => queries.invalidateQueries({ queryKey: ["console-users"] });
  const remove = useMutation({
    mutationFn: (u: ConsoleUser) => api.removeConsoleUser(u.email),
    onSuccess: (_, u) => {
      toast.success(`${u.email} may no longer open the console`);
      refresh();
    },
    onError: (e: Error) => toast.error(e.message),
  });
  return (
    <Card>
      <CardHeader>
        <CardTitle>Console users</CardTitle>
        <CardDescription>
          Who may open this console. No workspace's roles reach it: being here is what lets someone in, with an email and password account or the console's sign-in methods. Also from the CLI: <InlineCode>shpyrd-ctl console-users add you@example.com</InlineCode>.
        </CardDescription>
        <CardAction>
          <AddDialog onDone={refresh} />
        </CardAction>
      </CardHeader>
      <CardContent>
        {users.isLoading ? (
          <Loading />
        ) : users.error ? (
          <Failed what="the console users" error={users.error} />
        ) : (users.data ?? []).length === 0 ? (
          <p className="text-sm text-muted-foreground">Nobody yet: the console opens to the kubeconfig and the admin token only.</p>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Email</TableHead>
                <TableHead>Account</TableHead>
                <TableHead>Added</TableHead>
                <TableHead className="text-right" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {users.data!.map((u) => {
                const self = perms.me?.email?.toLowerCase() === u.email;
                return (
                  <TableRow key={u.email}>
                    <TableCell>
                      <Stack direction="horizontal" align="center" gap="condensed">
                        <span className="font-medium">{u.email}</span>
                        {self && <Badge variant="outline">you</Badge>}
                      </Stack>
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">{accountWords(u.account)}</TableCell>
                    <TableCell className="text-xs text-muted-foreground">
                      {ago(u.addedAt)}
                      {u.addedBy && u.addedBy !== "migration" ? ` by ${u.addedBy}` : u.addedBy === "migration" ? ", from the default workspace" : ""}
                    </TableCell>
                    <TableCell className="text-right">
                      <ConfirmDialog
                        trigger={<Button variant="ghost" size="icon-xs" icon={<Trash2 />} aria-label={`Remove ${u.email}`} disabled={self} title={self ? "Not yourself" : undefined} />}
                        variant="destructive"
                        title={`Take ${u.email} off the console?`}
                        description="Their account stays, and with it what they may do in workspaces."
                        action="Take off"
                        onConfirm={() => remove.mutate(u)}
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

// What the state of someone's account means for signing in here.
export function accountWords(account: ConsoleUser["account"]): string {
  switch (account) {
    case "active":
      return "email and password";
    case "pending":
      return "no password yet";
    case "locked":
      return "locked for now";
    default:
      return "sign-in methods only";
  }
}

// Someone added to the list, with an account made for them when a password
// is given.
function AddDialog({ onDone }: { onDone: () => void }) {
  const [open, setOpen] = useState(false);
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const reset = () => {
    setEmail("");
    setPassword("");
  };
  const add = useMutation({
    mutationFn: () => api.addConsoleUser({ email: email.trim(), password: password || undefined }),
    onSuccess: (u) => {
      toast.success(`${u.email} may open the console`);
      setOpen(false);
      reset();
      onDone();
    },
    onError: (e: Error) => toast.error(e.message),
  });
  const valid = /^\S+@\S+\.\S+$/.test(email.trim()) && (password === "" || password.length >= 8);
  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        setOpen(o);
        if (!o) reset();
      }}
    >
      <DialogTrigger asChild>
        <Button size="sm" icon={<Plus />}>
          Add someone
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader divider>
          <DialogTitle>Add someone to the console</DialogTitle>
          <DialogDescription>They sign in with an account they already have, or with the password you give here, which makes one.</DialogDescription>
        </DialogHeader>
        <form
          className="grid gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            if (valid) add.mutate();
          }}
        >
          <Field label="Email">
            <Input type="email" value={email} onChange={(e) => setEmail(e.target.value)} autoFocus />
          </Field>
          <Field label="Password" hint="Optional: only for someone without an account. At least 8 characters.">
            <Input type="password" autoComplete="new-password" value={password} onChange={(e) => setPassword(e.target.value)} />
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
