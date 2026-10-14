"use client";

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { FileText, Plus, Trash2, X } from "lucide-react";
import { toast } from "sonner";
import {
  Alert,
  AlertDescription,
  AlertTitle,
} from "@shpyrd/ui/components/alert";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@shpyrd/ui/components/select";
import { Badge } from "@shpyrd/ui/components/badge";
import { Button } from "@shpyrd/ui/components/button";
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@shpyrd/ui/components/card";
import { ConfirmDialog } from "@shpyrd/ui/components/confirm-dialog";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@shpyrd/ui/components/dialog";
import { Field } from "@shpyrd/ui/components/field";
import { InlineCode } from "@shpyrd/ui/components/inline-code";
import { Input } from "@shpyrd/ui/components/input";
import { Stack } from "@shpyrd/ui/components/stack";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@shpyrd/ui/components/table";
import { Textarea } from "@shpyrd/ui/components/textarea";
import { api } from "@/api/api";
import type { ConfigChange, ConfigKind, Project } from "@/api/types";
import type { Perms } from "@/lib/perms";
import { ago } from "@/lib/project";
import { Failed, Loading } from "./shared";

const validName = (name: string) => /^[A-Za-z_][A-Za-z0-9_]*$/.test(name);

// Saved edits are pending until the user deploys them.
export function Config({ project, perms }: { project: Project; perms: Perms }) {
  const queries = useQueryClient();
  const vars = useQuery({
    queryKey: ["config-vars", project.slug],
    queryFn: () => api.configVars(project.slug),
    refetchInterval: 15_000,
  });
  const [name, setName] = useState("");
  const [value, setValue] = useState("");
  const [kind, setKind] = useState<ConfigKind>("secret");
  const [replacing, setReplacing] = useState<string | null>(null);
  const [replacement, setReplacement] = useState("");
  const [confirmation, setConfirmation] = useState<ConfigChange | null>(null);
  const change = useMutation({
    mutationFn: (c: ConfigChange) => api.changeConfigVars(project.slug, c),
    onSuccess: () => {
      queries.invalidateQueries({ queryKey: ["config-vars", project.slug] });
      setReplacing(null);
      setName("");
      setValue("");
      toast.success("Configuration saved", {
        description:
          "Redeploy to apply these changes. Your running app has not changed.",
      });
    },
    onError: (e: Error) => toast.error(e.message),
  });
  const own = [
    ...(vars.data?.vars ?? []).map((v) => ({ ...v, kind: "secret" as const })),
    ...(vars.data?.variables ?? []).map((v) => ({
      ...v,
      kind: "variable" as const,
    })),
  ].sort((a, b) => a.name.localeCompare(b.name));
  const global = (vars.data?.global ?? []).filter(
    (g) => !own.some((v) => v.name === g.name),
  );
  function submit(c: ConfigChange) {
    const names = [
      ...Object.keys(c.set ?? {}),
      ...(c.convert ?? []),
      ...(c.dotenv ?? "")
        .split("\n")
        .filter((l) => l.includes("=") && !l.trim().startsWith("#"))
        .map((l) =>
          l
            .split("=")[0]!
            .trim()
            .replace(/^export /, ""),
        ),
    ];
    if (
      c.kind === "variable" &&
      (c.convert?.length ||
        names.some(
          (n) =>
            /(_KEY|_SECRET|_TOKEN)$|PASSWORD|^(SECRET|TOKEN)$/i.test(n) ||
            vars.data?.vars.some((v) => v.name === n),
        ))
    )
      setConfirmation(c);
    else change.mutate(c);
  }
  return (
    <>
      <ConfirmDialog
        open={confirmation !== null}
        onOpenChange={(open) => {
          if (!open) setConfirmation(null);
        }}
        title="Make these values visible?"
        description="Variables can be read by everyone who can view this project. These names may contain credentials or currently hold secrets. Only continue if the values are safe to share."
        action="Save as variables"
        onConfirm={() => {
          if (confirmation)
            change.mutate({ ...confirmation, confirmPlain: true });
        }}
      />
      {vars.data?.pending && (
        <Alert variant="warning">
          <AlertTitle>Redeploy to apply your configuration changes</AlertTitle>
          <AlertDescription>
            Your edits are saved. Running processes and one-off commands still
            use the deployed configuration. Use Redeploy when you are ready.
          </AlertDescription>
        </Alert>
      )}
      <Card>
        <CardHeader>
          <CardTitle>Variables and secrets</CardTitle>
          <CardDescription>
            Variables are visible to project viewers; secret values stay hidden.
            Save your edits, then redeploy to apply them to every process.
          </CardDescription>
          {perms.config && (
            <CardAction>
              <FromDotenv
                pending={change.isPending}
                onSubmit={(dotenv, kind) => submit({ dotenv, kind })}
              />
            </CardAction>
          )}
        </CardHeader>
        <CardContent>
          <Stack gap="normal">
            {vars.isLoading ? (
              <Loading />
            ) : vars.error ? (
              <Failed what="the configuration" error={vars.error} />
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Name</TableHead>
                    <TableHead>Kind</TableHead>
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
                        <Badge variant="outline">
                          {v.kind === "secret" ? "Secret" : "Variable"}
                        </Badge>
                      </TableCell>
                      <TableCell>
                        {replacing === v.name ? (
                          <form
                            className="flex items-center gap-2"
                            onSubmit={(e) => {
                              e.preventDefault();
                              submit({
                                kind: v.kind,
                                set: { [v.name]: replacement },
                              });
                            }}
                          >
                            <Input
                              type={v.kind === "secret" ? "password" : "text"}
                              size="sm"
                              autoFocus
                              autoComplete="off"
                              value={replacement}
                              onChange={(e) => setReplacement(e.target.value)}
                              aria-label={`New value for ${v.name}`}
                              className="w-64"
                            />
                            <Button
                              type="submit"
                              size="xs"
                              disabled={change.isPending}
                            >
                              Save
                            </Button>
                            <Button
                              type="button"
                              size="icon-xs"
                              variant="ghost"
                              icon={<X />}
                              aria-label="Cancel editing"
                              onClick={() => setReplacing(null)}
                            />
                          </form>
                        ) : (
                          <span className="whitespace-pre-wrap break-all font-mono text-xs">
                            {v.kind === "secret"
                              ? "••••••••"
                              : v.value === ""
                                ? "(empty)"
                                : v.value}
                          </span>
                        )}
                      </TableCell>
                      <TableCell className="text-xs text-muted-foreground">
                        {v.updatedAt ? ago(v.updatedAt) : "-"}
                      </TableCell>
                      <TableCell className="text-right">
                        {perms.config && replacing !== v.name && (
                          <Stack
                            direction="horizontal"
                            gap="tight"
                            justify="end"
                          >
                            <Button
                              size="xs"
                              variant="outline"
                              disabled={change.isPending}
                              onClick={() => {
                                setReplacement(
                                  v.kind === "variable" ? (v.value ?? "") : "",
                                );
                                setReplacing(v.name);
                              }}
                            >
                              {v.kind === "variable" ? "Edit" : "Replace"}
                            </Button>
                            <Button
                              size="xs"
                              variant="ghost"
                              disabled={change.isPending}
                              onClick={() =>
                                submit({
                                  kind:
                                    v.kind === "secret" ? "variable" : "secret",
                                  convert: [v.name],
                                })
                              }
                            >
                              {v.kind === "secret"
                                ? "Make variable"
                                : "Make secret"}
                            </Button>
                            <ConfirmDialog
                              trigger={
                                <Button
                                  variant="ghost"
                                  size="icon-xs"
                                  disabled={change.isPending}
                                  icon={<Trash2 />}
                                  aria-label={`Remove ${v.name}`}
                                />
                              }
                              variant="destructive"
                              title={`Remove ${v.name}?`}
                              description="Saved now; removed from processes when you redeploy."
                              action="Remove"
                              onConfirm={() =>
                                submit({ kind: v.kind, unset: [v.name] })
                              }
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
                      <TableCell>Resource</TableCell>
                      <TableCell
                        colSpan={3}
                        className="text-xs text-muted-foreground"
                      >
                        Provided by {b.provider}, read-only; overrides project
                        configuration with the same name.
                      </TableCell>
                    </TableRow>
                  ))}
                  {global.map((g) => (
                    <TableRow key={`global-${g.name}`} className="bg-muted/30">
                      <TableCell>
                        <InlineCode>{g.name}</InlineCode>
                      </TableCell>
                      <TableCell>Workspace</TableCell>
                      <TableCell
                        colSpan={3}
                        className="text-xs text-muted-foreground"
                      >
                        Secret set for every project, read-only here; project
                        configuration with the same name overrides it.
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
            {perms.config && (
              <form
                className="grid gap-2 sm:grid-cols-[auto_1fr_1fr_auto]"
                onSubmit={(e) => {
                  e.preventDefault();
                  if (validName(name)) submit({ kind, set: { [name]: value } });
                }}
              >
                <KindPicker value={kind} onChange={setKind} />
                <Input
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  placeholder="NAME"
                  className="font-mono text-xs"
                  autoComplete="off"
                  aria-label="Name"
                />
                <Input
                  type={kind === "secret" ? "password" : "text"}
                  value={value}
                  onChange={(e) => setValue(e.target.value)}
                  placeholder="value"
                  autoComplete="off"
                  aria-label="Value"
                />
                <Button
                  type="submit"
                  icon={<Plus />}
                  disabled={!validName(name) || change.isPending}
                >
                  Save
                </Button>
              </form>
            )}
          </Stack>
        </CardContent>
      </Card>
      <Card>
        <CardHeader>
          <CardTitle>Plain environment</CardTitle>
          <CardDescription>
            What every process sees besides the config vars: what the platform
            sets, and what the spec says in the open.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Table variant="secondary">
            <TableBody>
              {(project.spec.env ?? []).map((e) => (
                <TableRow key={e.name}>
                  <TableCell className="w-48">
                    <InlineCode>{e.name}</InlineCode>
                  </TableCell>
                  <TableCell className="font-mono text-xs text-muted-foreground">
                    {e.value ?? "from a reference"}
                  </TableCell>
                </TableRow>
              ))}
              <TableRow>
                <TableCell className="w-48">
                  <InlineCode>PORT</InlineCode>
                </TableCell>
                <TableCell className="text-muted-foreground">
                  The port the web process has to answer on.
                </TableCell>
              </TableRow>
              {[
                ["RUNNING_IN_SHPYRD", "true: the process runs on shpyrd."],
                ["SHPYRD_PROJECT", "The slug of the project."],
                [
                  "SHPYRD_PROJECT_ID",
                  "The id of the project, which never changes.",
                ],
                [
                  "SHPYRD_PROJECT_NAME",
                  "The name of the project, as it is shown.",
                ],
                ["SHPYRD_WORKSPACE", "The slug of the workspace."],
                [
                  "SHPYRD_PROCESS",
                  "The name of the process the instance runs: web, worker, release.",
                ],
                [
                  "SHPYRD_RELEASE",
                  "The number of the release the instance belongs to; SHPYRD_RELEASE_VERSION is the same with a v: v5.",
                ],
                [
                  "SHPYRD_REVISION",
                  "The git commit the release was built from, or the digest of the archive; SHPYRD_PROJECT_REVISION and REVISION are the same. Absent for a prebuilt image.",
                ],
                [
                  "SHPYRD_ISSUER",
                  "Where the tokens of visitors are signed, for an app that checks who opens it.",
                ],
              ].map(([name, what]) => (
                <TableRow key={name}>
                  <TableCell>
                    <InlineCode>{name}</InlineCode>
                  </TableCell>
                  <TableCell className="text-muted-foreground">
                    {what}
                  </TableCell>
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
function FromDotenv({
  onSubmit,
  pending,
}: {
  onSubmit: (dotenv: string, kind: ConfigKind) => void;
  pending: boolean;
}) {
  const [open, setOpen] = useState(false);
  const [text, setText] = useState("");
  const [kind, setKind] = useState<ConfigKind>("secret");
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
          <DialogDescription>
            One per line, as NAME=value. Choose secrets to keep values hidden,
            or variables to show them to project viewers. Save now, then
            redeploy when ready.
          </DialogDescription>
        </DialogHeader>
        <KindPicker value={kind} onChange={setKind} />
        <Field
          label="Lines"
          hint="Names in letters, digits and underscores."
          error={text && !valid ? "Each line is NAME=value." : undefined}
        >
          <Textarea
            value={text}
            onChange={(e) => setText(e.target.value)}
            placeholder={"DATABASE_URL=postgres://…\nLOG_LEVEL=info"}
            className="min-h-40 font-mono text-xs"
            spellCheck={false}
          />
        </Field>
        <DialogFooter>
          <DialogClose asChild>
            <Button variant="outline">Cancel</Button>
          </DialogClose>
          <Button
            disabled={!valid || pending}
            onClick={() => {
              onSubmit(text, kind);
              setText("");
              setOpen(false);
            }}
          >
            Save {names.length}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function KindPicker({
  value,
  onChange,
}: {
  value: ConfigKind;
  onChange: (kind: ConfigKind) => void;
}) {
  return (
    <Select value={value} onValueChange={(v) => onChange(v as ConfigKind)}>
      <SelectTrigger aria-label="Configuration kind">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value="secret">Secret</SelectItem>
        <SelectItem value="variable">Variable</SelectItem>
      </SelectContent>
    </Select>
  );
}
