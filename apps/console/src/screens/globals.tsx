"use client";

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@shpyrd/ui/components/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@shpyrd/ui/components/card";
import { ConfirmDialog } from "@shpyrd/ui/components/confirm-dialog";
import { InlineCode } from "@shpyrd/ui/components/inline-code";
import { Input } from "@shpyrd/ui/components/input";
import { Stack } from "@shpyrd/ui/components/stack";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@shpyrd/ui/components/table";
import { api } from "@/api/api";
import type { GlobalsChange } from "@/api/types";
import { ago, Failed, Loading } from "./shared";

const validName = (name: string) => /^[A-Za-z_][A-Za-z0-9_]*$/.test(name);

// Config vars every project receives, first in the environment so a
// project's own var wins. Values are write-only. A change is a release
// in every project, so it asks first.
export function Globals() {
  const queries = useQueryClient();
  const globals = useQuery({ queryKey: ["globals"], queryFn: api.globals });
  const [name, setName] = useState("");
  const [value, setValue] = useState("");
  const [pending, setPending] = useState<GlobalsChange | null>(null);
  const save = useMutation({
    mutationFn: (c: GlobalsChange) => api.changeGlobals(c),
    onSuccess: (r, c) => {
      toast.success(c.set ? `${Object.keys(c.set).join(", ")} set` : `${c.unset?.join(", ")} removed`, { description: `Releasing to ${r.projects} ${r.projects === 1 ? "project" : "projects"}.` });
      queries.setQueryData(["globals"], r);
      setPending(null);
      setName("");
      setValue("");
    },
    onError: (e: Error) => toast.error(e.message),
  });
  const projects = globals.data?.projects ?? 0;
  const valid = validName(name) && value !== "";
  return (
    <Card>
      <CardHeader>
        <CardTitle>Global config vars</CardTitle>
        <CardDescription>
          Injected into every process of every project, {projects} {projects === 1 ? "project" : "projects"} today. A project's own var of the same name wins, and what an attached resource provides wins over both. Values are never shown; a change is a release in each project.
        </CardDescription>
      </CardHeader>
      <CardContent>
        <Stack gap="normal">
          {globals.isLoading ? (
            <Loading />
          ) : globals.error ? (
            <Failed what="the global vars" error={globals.error} />
          ) : globals.data!.vars.length === 0 ? (
            <p className="text-sm text-muted-foreground">
              None. <InlineCode>shpyrd globals set NAME=value</InlineCode> does the same from the CLI.
            </p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Value</TableHead>
                  <TableHead>Changed</TableHead>
                  <TableHead className="text-right" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {globals.data!.vars.map((v) => (
                  <TableRow key={v.name}>
                    <TableCell>
                      <InlineCode>{v.name}</InlineCode>
                    </TableCell>
                    <TableCell className="font-mono text-xs text-muted-foreground">••••••••</TableCell>
                    <TableCell className="text-xs text-muted-foreground">{v.updatedAt ? ago(v.updatedAt) : "-"}</TableCell>
                    <TableCell className="text-right">
                      <Button size="icon-xs" variant="ghost" icon={<Trash2 />} aria-label={`Remove ${v.name}`} onClick={() => setPending({ unset: [v.name] })} />
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
          <form
            className="grid gap-2 sm:grid-cols-[1fr_1fr_auto]"
            onSubmit={(e) => {
              e.preventDefault();
              if (valid) setPending({ set: { [name]: value } });
            }}
          >
            <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="OPENAI_API_KEY" className="font-mono text-xs" autoComplete="off" aria-label="Name" aria-invalid={name !== "" && !validName(name)} />
            <Input type="password" value={value} onChange={(e) => setValue(e.target.value)} placeholder="value" autoComplete="off" aria-label="Value" />
            <Button type="submit" icon={<Plus />} disabled={!valid || save.isPending}>
              Set
            </Button>
          </form>
          <ConfirmDialog
            open={pending !== null}
            onOpenChange={(o) => !o && setPending(null)}
            title={pending?.set ? `Set ${Object.keys(pending.set).join(", ")} for every project?` : `Remove ${pending?.unset?.join(", ")} from every project?`}
            description={`${projects} ${projects === 1 ? "project gets" : "projects get"} a new release and restart with the new configuration. A project that sets the same name itself keeps its own value.`}
            action={pending?.set ? "Set and release" : "Remove and release"}
            variant={pending?.set ? "default" : "destructive"}
            onConfirm={() => pending && save.mutate(pending)}
          />
        </Stack>
      </CardContent>
    </Card>
  );
}
