"use client";

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus, Trash2, X } from "lucide-react";
import { toast } from "sonner";
import { Alert, AlertDescription, AlertTitle } from "@shpyrd/ui/components/alert";
import { Badge } from "@shpyrd/ui/components/badge";
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
import type { CreateDrain, Project } from "@/api/types";
import type { Perms } from "@/lib/perms";
import { ago } from "@/lib/project";
import { Failed, Loading } from "./shared";

// Where the lines go besides here: a drain sends them to a service of
// your own, as JSON over HTTPS or as syslog.
export function Drains({ project, perms }: { project: Project; perms: Perms }) {
  const queries = useQueryClient();
  const config = useQuery({ queryKey: ["config"], queryFn: api.config, staleTime: 60_000 });
  const agent = config.data?.extensions.includes("logs-agent") ?? true;
  const drains = useQuery({ queryKey: ["drains", project.slug], queryFn: () => api.drains(project.slug), refetchInterval: 15_000 });
  const refresh = () => queries.invalidateQueries({ queryKey: ["drains", project.slug] });
  const remove = useMutation({
    mutationFn: (name: string) => api.removeDrain(project.slug, name),
    onSuccess: () => {
      refresh();
      toast.success("Drain removed");
    },
    onError: (e: Error) => toast.error(e.message),
  });
  return (
    <>
      {!agent && (
        <Alert variant="warning">
          <AlertTitle>Nothing is forwarded</AlertTitle>
          <AlertDescription>
            The logs-agent extension is off. Drains start sending once the operator runs <InlineCode>shpyrd extensions enable logs-agent</InlineCode>.
          </AlertDescription>
        </Alert>
      )}
      <Card>
        <CardHeader>
          <CardTitle>Log drains</CardTitle>
          <CardDescription>Every line of the project, sent as it is written to a service of your own. The values of the headers are never read back.</CardDescription>
          {perms.resource && (
            <CardAction>
              <AddDrain slug={project.slug} processes={Object.keys({ ...project.spec.processes, ...project.processes }).sort()} onDone={refresh} />
            </CardAction>
          )}
        </CardHeader>
        <CardContent>
          {drains.isLoading ? (
            <Loading />
          ) : drains.error ? (
            <Failed what="the drains" error={drains.error} />
          ) : (drains.data ?? []).length === 0 ? (
            <p className="text-sm text-muted-foreground">
              None. The lines stay here, on the Logs page, for a week. <InlineCode>shpyrd drains add &lt;url&gt;</InlineCode> does the same from the CLI.
            </p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Drain</TableHead>
                  <TableHead>Where</TableHead>
                  <TableHead>State</TableHead>
                  <TableHead className="text-right">Sent</TableHead>
                  <TableHead className="text-right" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {drains.data!.map((d) => (
                  <TableRow key={d.name}>
                    <TableCell className="font-medium">
                      {d.name}
                      {d.cluster && (
                        <Badge variant="outline" className="ml-2">
                          the cluster's
                        </Badge>
                      )}
                      <div className="text-[11px] text-muted-foreground">
                        {d.format}
                        {d.processes?.length ? ` · ${d.processes.join(", ")}` : ""}
                        {d.headers?.length ? ` · headers ${d.headers.join(", ")}` : ""}
                      </div>
                    </TableCell>
                    <TableCell className="max-w-64 truncate font-mono text-xs text-muted-foreground" title={d.url}>
                      {d.url}
                    </TableCell>
                    <TableCell>
                      <StatusBadge type={d.phase === "Active" ? "success" : d.phase === "Failing" ? "error" : "warning"} live={d.phase === "Pending"}>
                        {d.phase}
                      </StatusBadge>
                      {d.message && <div className="mt-1 text-[11px] text-destructive">{d.message}</div>}
                    </TableCell>
                    <TableCell className="text-right font-mono text-xs">
                      {d.sent.toLocaleString("en")}
                      {d.errors > 0 && <span className="text-destructive"> · {d.errors} failed</span>}
                      <div className="font-sans text-[11px] text-muted-foreground">{d.lastDeliveryAt ? ago(d.lastDeliveryAt) : "never"}</div>
                    </TableCell>
                    <TableCell className="text-right">
                      {perms.resource && !d.cluster && (
                        <ConfirmDialog
                          trigger={<Button variant="ghost" size="icon-xs" icon={<Trash2 />} aria-label={`Remove ${d.name}`} />}
                          variant="destructive"
                          title={`Remove ${d.name}?`}
                          description="The lines stop going there."
                          action="Remove"
                          onConfirm={() => remove.mutate(d.name)}
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
    </>
  );
}

function AddDrain({ slug, processes, onDone }: { slug: string; processes: string[]; onDone: () => void }) {
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [url, setUrl] = useState("");
  const [format, setFormat] = useState<"auto" | "json" | "syslog">("auto");
  const [headers, setHeaders] = useState<{ key: string; value: string }[]>([]);
  const [chosen, setChosen] = useState<string[]>([]);
  const valid = (name === "" || /^[a-z0-9-]{2,}$/.test(name)) && /^(https?|syslog(\+tls)?):\/\/[^\s/]+/.test(url.trim());
  const reset = () => {
    setName("");
    setUrl("");
    setFormat("auto");
    setHeaders([]);
    setChosen([]);
  };
  const add = useMutation({
    mutationFn: () => {
      const body: CreateDrain = { name: name || undefined, url: url.trim(), format: format === "auto" ? undefined : format, processes: chosen.length ? chosen : undefined };
      const sent = Object.fromEntries(headers.filter((h) => h.key.trim()).map((h) => [h.key.trim(), h.value]));
      if (Object.keys(sent).length) body.headers = sent;
      return api.addDrain(slug, body);
    },
    onSuccess: (d) => {
      onDone();
      setOpen(false);
      reset();
      toast.success(`Drain ${d.name} added`, { description: "The first lines go out within a minute." });
    },
    onError: (e: Error) => toast.error(e.message),
  });
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button size="sm" icon={<Plus />}>
          Add
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader divider>
          <DialogTitle>Add a drain</DialogTitle>
          <DialogDescription>An HTTPS endpoint takes the lines as JSON, one object each; a syslog server takes them as RFC 5424 syslog.</DialogDescription>
        </DialogHeader>
        <Stack gap="normal">
          <Field label="URL">
            <Input value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://… or syslog+tls://host:6514" className="font-mono text-xs" autoFocus />
          </Field>
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="Name" hint="Empty: taken from the host.">
              <Input value={name} onChange={(e) => setName(e.target.value.toLowerCase())} placeholder="datadog" />
            </Field>
            <Field label="Format">
              <Select value={format} onValueChange={(v) => setFormat(v as typeof format)}>
                <SelectTrigger className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="auto">From the URL</SelectItem>
                  <SelectItem value="json">JSON, one object per line</SelectItem>
                  <SelectItem value="syslog">Syslog</SelectItem>
                </SelectContent>
              </Select>
            </Field>
          </div>
          {processes.length > 1 && (
            <Field label="Processes" hint="None chosen: every process.">
              <Stack direction="horizontal" wrap="wrap" gap="tight">
                {processes.map((p) => {
                  const on = chosen.includes(p);
                  return (
                    <Button key={p} type="button" size="xs" variant={on ? "default" : "outline"} onClick={() => setChosen(on ? chosen.filter((x) => x !== p) : [...chosen, p])}>
                      {p}
                    </Button>
                  );
                })}
              </Stack>
            </Field>
          )}
          <Field label="Headers" hint="Sent with every request; the values are write-only.">
            <Stack gap="condensed">
              {headers.map((h, i) => (
                <Stack key={i} direction="horizontal" gap="condensed" align="center">
                  <Input value={h.key} onChange={(e) => setHeaders(headers.map((x, j) => (j === i ? { ...x, key: e.target.value } : x)))} placeholder="Authorization" className="font-mono text-xs" aria-label="Header name" />
                  <Input type="password" value={h.value} onChange={(e) => setHeaders(headers.map((x, j) => (j === i ? { ...x, value: e.target.value } : x)))} placeholder="Bearer …" className="font-mono text-xs" autoComplete="off" aria-label="Header value" />
                  <Button type="button" variant="ghost" size="icon-xs" icon={<X />} aria-label="Remove the header" onClick={() => setHeaders(headers.filter((_, j) => j !== i))} />
                </Stack>
              ))}
              <div>
                <Button type="button" variant="outline" size="xs" icon={<Plus />} onClick={() => setHeaders([...headers, { key: "", value: "" }])}>
                  Header
                </Button>
              </div>
            </Stack>
          </Field>
        </Stack>
        <DialogFooter>
          <DialogClose asChild>
            <Button variant="outline">Cancel</Button>
          </DialogClose>
          <Button disabled={!valid || add.isPending} onClick={() => add.mutate()}>
            Add
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
