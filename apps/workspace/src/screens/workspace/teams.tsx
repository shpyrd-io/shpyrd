"use client";

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Pencil, Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { AvatarStack } from "@shpyrd/ui/components/avatar-stack";
import { Badge } from "@shpyrd/ui/components/badge";
import { Button } from "@shpyrd/ui/components/button";
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@shpyrd/ui/components/card";
import { ConfirmDialog } from "@shpyrd/ui/components/confirm-dialog";
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from "@shpyrd/ui/components/dialog";
import { Field } from "@shpyrd/ui/components/field";
import { Input } from "@shpyrd/ui/components/input";
import { Stack } from "@shpyrd/ui/components/stack";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@shpyrd/ui/components/table";
import { Textarea } from "@shpyrd/ui/components/textarea";
import { api } from "@/api/api";
import type { Team } from "@/api/types";
import { Failed, Loading } from "../project/shared";

// The teams: names projects give roles to, so that people come and go
// without touching every project.
export function Teams() {
  const queries = useQueryClient();
  const teams = useQuery({ queryKey: ["teams"], queryFn: api.teams });
  const refresh = () => queries.invalidateQueries({ queryKey: ["teams"] });
  const remove = useMutation({
    mutationFn: (name: string) => api.removeTeam(name),
    onSuccess: () => {
      refresh();
      toast.success("Team removed");
    },
    onError: (e: Error) => toast.error(e.message),
  });
  return (
    <Card>
      <CardHeader>
        <CardTitle>Teams</CardTitle>
        <CardDescription>A project gives a role to a team, and everyone in it has it. The team everyone is in has every person who signs in.</CardDescription>
        <CardAction>
          <EditTeam onDone={refresh} />
        </CardAction>
      </CardHeader>
      <CardContent>
        {teams.isLoading ? (
          <Loading />
        ) : teams.error ? (
          <Failed what="the teams" error={teams.error} />
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Team</TableHead>
                <TableHead>Members</TableHead>
                <TableHead>Platform</TableHead>
                <TableHead className="text-right" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {teams.data!.map((t) => (
                <TableRow key={t.name}>
                  <TableCell>
                    <div className="font-medium">{t.name}</div>
                    {t.description && <div className="text-xs text-muted-foreground">{t.description}</div>}
                  </TableCell>
                  <TableCell>
                    {t.everyone ? (
                      <Badge variant="secondary">everyone who signs in</Badge>
                    ) : t.members.length === 0 ? (
                      <span className="text-xs text-muted-foreground">nobody yet</span>
                    ) : (
                      <Stack direction="horizontal" align="center" gap="condensed">
                        <AvatarStack avatars={t.members.map((m) => ({ alt: m }))} size={24} />
                        <span className="text-xs text-muted-foreground">{t.members.length}</span>
                      </Stack>
                    )}
                  </TableCell>
                  <TableCell>{t.platformRole ? <Badge variant="outline">{t.platformRole}</Badge> : <span className="text-xs text-muted-foreground">-</span>}</TableCell>
                  <TableCell className="text-right">
                    {!t.everyone && (
                      <Stack direction="horizontal" justify="end" gap="tight">
                        <EditTeam team={t} onDone={refresh} />
                        <ConfirmDialog
                          trigger={<Button variant="ghost" size="icon-xs" icon={<Trash2 />} aria-label={`Remove ${t.name}`} />}
                          variant="destructive"
                          title={`Remove the team ${t.name}?`}
                          description="Every role it holds on a project goes with it."
                          action="Remove"
                          onConfirm={() => remove.mutate(t.name)}
                        />
                      </Stack>
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

function EditTeam({ team, onDone }: { team?: Team; onDone: () => void }) {
  const [open, setOpen] = useState(false);
  const [name, setName] = useState(team?.name ?? "");
  const [description, setDescription] = useState(team?.description ?? "");
  const [members, setMembers] = useState((team?.members ?? []).join("\n"));
  const [groups, setGroups] = useState((team?.groups ?? []).join("\n"));
  const lines = (text: string) => text.split("\n").map((m) => m.trim()).filter(Boolean);
  const valid = /^[a-z0-9-]{2,}$/.test(name);
  const save = useMutation({
    mutationFn: () => api.saveTeam({ ...team, name, description: description.trim() || undefined, members: lines(members), groups: lines(groups) }),
    onSuccess: () => {
      onDone();
      setOpen(false);
      toast.success(team ? "Team saved" : "Team made");
    },
    onError: (e: Error) => toast.error(e.message),
  });
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        {team ? <Button variant="ghost" size="icon-xs" icon={<Pencil />} aria-label={`Edit ${team.name}`} /> : <Button size="sm" icon={<Plus />}>New team</Button>}
      </DialogTrigger>
      <DialogContent>
        <DialogHeader divider>
          <DialogTitle>{team ? `The team ${team.name}` : "New team"}</DialogTitle>
          <DialogDescription>The members are emails of people of the workspace, one per line.</DialogDescription>
        </DialogHeader>
        <Stack gap="normal">
          <Field label="Name" hint="Lowercase letters, digits and dashes.">
            <Input value={name} disabled={!!team} onChange={(e) => setName(e.target.value.toLowerCase())} placeholder="support" autoFocus={!team} />
          </Field>
          <Field label="What it is for">
            <Input value={description} onChange={(e) => setDescription(e.target.value)} placeholder="Opens the apps of the clients." />
          </Field>
          <Field label="Members">
            <Textarea value={members} onChange={(e) => setMembers(e.target.value)} placeholder={"ana@acme.com\nmarcelo@acme.com"} className="min-h-24 font-mono text-xs" />
          </Field>
          <Field label="Groups of the identity provider" hint="Whoever signs in with one of these groups is in the team, one per line.">
            <Textarea value={groups} onChange={(e) => setGroups(e.target.value)} placeholder={"engineering\nacme/ops"} className="min-h-16 font-mono text-xs" />
          </Field>
        </Stack>
        <DialogFooter>
          <DialogClose asChild>
            <Button variant="outline">Cancel</Button>
          </DialogClose>
          <Button disabled={!valid || save.isPending} onClick={() => save.mutate()}>
            {team ? "Save" : "Make the team"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
