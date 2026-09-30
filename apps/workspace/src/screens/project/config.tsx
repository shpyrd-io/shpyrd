"use client";

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { FileText, Plus, Trash2, X } from "lucide-react";
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
import { Textarea } from "@shpyrd/ui/components/textarea";
import { api } from "@/api/api";
import type { ConfigChange, Project } from "@/api/types";
import type { Perms } from "@/lib/perms";
import { ago } from "@/lib/project";
import { Failed, Loading } from "./shared";

const validName = (name: string) => /^[A-Za-z_][A-Za-z0-9_]*$/.test(name);

// The config vars: given to every process as environment variables.
// Values are write-only: replaced or removed, never read back. Beside
// them, what the resources provide and what the cluster sets.
export function Config({ project, perms }: { project: Project; perms: Perms }) {
  const queries = useQueryClient();
  const vars = useQuery({ queryKey: ["config-vars", project.slug], queryFn: () => api.configVars(project.slug), refetchInterval: 15_000 });
  const change = useMutation({
    mutationFn: (c: ConfigChange) => api.changeConfigVars(project.slug, c),
    onSuccess: (_, c) => {
      queries.invalidateQueries({ queryKey: ["config-vars", project.slug] });
      queries.invalidateQueries({ queryKey: ["project", project.slug] });
      setReplacing(null);
      const n = Object.keys(c.set ?? {}).length + (c.dotenv ? c.dotenv.split("\n").filter((l) => l.includes("=")).length : 0);
      toast.success(c.unset?.length ? `${c.unset.join(", ")} removed` : `${n} set`, { description: "A new release rolls out with it." });
    },
    onError: (e: Error) => toast.error(e.message),
  });
  const [name, setName] = useState("");
  const [value, setValue] = useState("");
  const [replacing, setReplacing] = useState<string | null>(null);
  const [replacement, setReplacement] = useState("");
  const own = vars.data?.vars ?? [];
  const global = (vars.data?.global ?? []).filter((g) => !own.some((v) => v.name === g.name));
  const overridden = new Set((vars.data?.global ?? []).map((g) => g.name));
  return (
    <>
      <Card>
        <CardHeader>
          <CardTitle>Config vars</CardTitle>
          <CardDescription>Injected into every process as environment variables. Values are write-only: replaced or removed, never read back. Each change is a release: the instances restart with it.</CardDescription>
          {perms.config && (
            <CardAction>
              <FromDotenv pending={change.isPending} onSubmit={(dotenv) => change.mutate({ dotenv })} />
            </CardAction>
          )}
        </CardHeader>
        <CardContent>
          <Stack gap="normal">
            {vars.isLoading ? (
              <Loading />
            ) : vars.error ? (
              <Failed what="the config vars" error={vars.error} />
            ) : own.length + (vars.data?.bound?.length ?? 0) + global.length === 0 ? (
              <p className="text-sm text-muted-foreground">None yet. The process sees only what the platform sets, under Plain environment.</p>
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
                  {own.map((v) => (
                    <TableRow key={v.name}>
                      <TableCell>
                        <InlineCode>{v.name}</InlineCode>
                      </TableCell>
                      <TableCell>
                        {replacing === v.name ? (
                          <form
                            className="flex items-center gap-2"
                            onSubmit={(e) => {
                              e.preventDefault();
                              change.mutate({ set: { [v.name]: replacement } });
                            }}
                          >
                            <Input type="password" size="sm" autoFocus autoComplete="off" value={replacement} onChange={(e) => setReplacement(e.target.value)} placeholder="The new value" className="w-64" />
                            <Button type="submit" size="xs" disabled={change.isPending}>
                              Save
                            </Button>
                            <Button type="button" size="icon-xs" variant="ghost" icon={<X />} aria-label="Keep the value" onClick={() => setReplacing(null)} />
                          </form>
                        ) : (
                          <span className="font-mono text-xs text-muted-foreground">
                            ••••••••
                            {overridden.has(v.name) && (
                              <Badge variant="outline" className="ml-2 font-sans">
                                over the cluster's
                              </Badge>
                            )}
                          </span>
                        )}
                      </TableCell>
                      <TableCell className="text-xs text-muted-foreground">{v.updatedAt ? ago(v.updatedAt) : "-"}</TableCell>
                      <TableCell className="text-right">
                        {perms.config && replacing !== v.name && (
                          <Stack direction="horizontal" gap="tight" justify="end">
                            <Button
                              size="xs"
                              variant="outline"
                              onClick={() => {
                                setReplacement("");
                                setReplacing(v.name);
                              }}
                            >
                              Replace
                            </Button>
                            <ConfirmDialog
                              trigger={<Button variant="ghost" size="icon-xs" icon={<Trash2 />} aria-label={`Remove ${v.name}`} />}
                              variant="destructive"
                              title={`Remove ${v.name}?`}
                              description="The processes restart without it."
                              action="Remove"
                              onConfirm={() => change.mutate({ unset: [v.name] })}
                            />
                          </Stack>
                        )}
                      </TableCell>
                    </TableRow>
                  ))}
                  {vars.data?.bound?.map((b) => (
                    <TableRow key={`bound-${b.name}`} className="bg-muted/30">
                      <TableCell>
                        <InlineCode>{b.name}</InlineCode>
                      </TableCell>
                      <TableCell className="text-xs text-muted-foreground">
                        provided by <span className="font-medium text-foreground">{b.provider}</span>, read-only; it wins over a var of the same name
                      </TableCell>
                      <TableCell className="text-xs text-muted-foreground">-</TableCell>
                      <TableCell />
                    </TableRow>
                  ))}
                  {global.map((g) => (
                    <TableRow key={`global-${g.name}`} className="bg-muted/30">
                      <TableCell>
                        <InlineCode>{g.name}</InlineCode>
                      </TableCell>
                      <TableCell className="text-xs text-muted-foreground">
                        set by the <span className="font-medium text-foreground">cluster</span> for every project, read-only; a var of the same name goes over it
                      </TableCell>
                      <TableCell className="text-xs text-muted-foreground">{g.updatedAt ? ago(g.updatedAt) : "-"}</TableCell>
                      <TableCell />
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
            {perms.config && (
              <form
                className="grid gap-2 sm:grid-cols-[1fr_1fr_auto]"
                onSubmit={(e) => {
                  e.preventDefault();
                  if (!validName(name)) return;
                  change.mutate({ set: { [name]: value } });
                  setName("");
                  setValue("");
                }}
              >
                <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="NAME" className="font-mono text-xs" autoComplete="off" aria-label="Name" />
                <Input type="password" value={value} onChange={(e) => setValue(e.target.value)} placeholder="value" autoComplete="off" aria-label="Value" />
                <Button type="submit" size="default" icon={<Plus />} disabled={!validName(name) || change.isPending}>
                  Set
                </Button>
              </form>
            )}
          </Stack>
        </CardContent>
      </Card>
      <Card>
        <CardHeader>
          <CardTitle>Plain environment</CardTitle>
          <CardDescription>What every process sees besides the config vars: what the platform sets, and what the spec says in the open.</CardDescription>
        </CardHeader>
        <CardContent>
          <Table variant="secondary">
            <TableBody>
              {(project.spec.env ?? []).map((e) => (
                <TableRow key={e.name}>
                  <TableCell className="w-48">
                    <InlineCode>{e.name}</InlineCode>
                  </TableCell>
                  <TableCell className="font-mono text-xs text-muted-foreground">{e.value ?? "from a reference"}</TableCell>
                </TableRow>
              ))}
              <TableRow>
                <TableCell className="w-48">
                  <InlineCode>PORT</InlineCode>
                </TableCell>
                <TableCell className="text-muted-foreground">The port the web process has to answer on.</TableCell>
              </TableRow>
              {[
                ["RUNNING_IN_SHPYRD", "true: the process runs on shpyrd."],
                ["SHPYRD_PROJECT", "The slug of the project."],
                ["SHPYRD_PROJECT_ID", "The id of the project, which never changes."],
                ["SHPYRD_PROJECT_NAME", "The name of the project, as it is shown."],
                ["SHPYRD_WORKSPACE", "The slug of the workspace."],
                ["SHPYRD_PROCESS", "The name of the process the instance runs: web, worker, release."],
                ["SHPYRD_RELEASE", "The number of the release the instance belongs to; SHPYRD_RELEASE_VERSION is the same with a v: v5."],
                ["SHPYRD_REVISION", "The git commit the release was built from, or the digest of the archive; SHPYRD_PROJECT_REVISION and REVISION are the same. Absent for a prebuilt image."],
                ["SHPYRD_ISSUER", "Where the tokens of visitors are signed, for an app that checks who opens it."],
              ].map(([name, what]) => (
                <TableRow key={name}>
                  <TableCell>
                    <InlineCode>{name}</InlineCode>
                  </TableCell>
                  <TableCell className="text-muted-foreground">{what}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </CardContent>
      </Card>
    </>
  );
}

// Many at once, pasted as a .env file.
function FromDotenv({ onSubmit, pending }: { onSubmit: (dotenv: string) => void; pending: boolean }) {
  const [open, setOpen] = useState(false);
  const [text, setText] = useState("");
  const names = text
    .split("\n")
    .map((l) => l.trim())
    .filter((l) => l && !l.startsWith("#") && l.includes("="))
    .map((l) => l.slice(0, l.indexOf("=")).trim());
  const valid = names.length > 0 && names.every(validName);
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button size="sm" variant="outline" icon={<FileText />}>
          From a .env
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader divider>
          <DialogTitle>Set from a .env</DialogTitle>
          <DialogDescription>One per line, as NAME=value. A name that exists is replaced, the others stay. Nothing is echoed back.</DialogDescription>
        </DialogHeader>
        <Field label="Lines" hint="Names in letters, digits and underscores." error={text && !valid ? "Each line is NAME=value." : undefined}>
          <Textarea value={text} onChange={(e) => setText(e.target.value)} placeholder={"DATABASE_URL=postgres://…\nLOG_LEVEL=info"} className="min-h-40 font-mono text-xs" spellCheck={false} />
        </Field>
        <DialogFooter>
          <DialogClose asChild>
            <Button variant="outline">Cancel</Button>
          </DialogClose>
          <Button
            disabled={!valid || pending}
            onClick={() => {
              onSubmit(text);
              setText("");
              setOpen(false);
            }}
          >
            Set {names.length} and release
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
