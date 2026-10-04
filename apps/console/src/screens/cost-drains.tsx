"use client";

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@shpyrd/ui/components/button";
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@shpyrd/ui/components/card";
import { ConfirmDialog } from "@shpyrd/ui/components/confirm-dialog";
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from "@shpyrd/ui/components/dialog";
import { Field } from "@shpyrd/ui/components/field";
import { Input } from "@shpyrd/ui/components/input";
import { StatusBadge } from "@shpyrd/ui/components/status-badge";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@shpyrd/ui/components/table";
import { Textarea } from "@shpyrd/ui/components/textarea";
import { api } from "@/api/api";
import type { CostDrain } from "@/api/types";
import { LicenseNeeded } from "./costs";
import { Loading, when } from "./shared";

// Where the cost lines go (enterprise): one POST of JSON at a time, with
// the drain's headers, of the lines that changed since it last delivered.

// headersOf reads "Name=value" lines; a line without "=" is an error.
export function headersOf(text: string): { headers: Record<string, string>; error?: string } {
  const headers: Record<string, string> = {};
  for (const raw of text.split("\n")) {
    const line = raw.trim();
    if (!line) continue;
    const i = line.indexOf("=");
    if (i <= 0) return { headers, error: `"${line}": write each header as Name=value` };
    headers[line.slice(0, i).trim()] = line.slice(i + 1);
  }
  return { headers };
}

export function CostDrains() {
  const queries = useQueryClient();
  const drains = useQuery({ queryKey: ["cost-drains"], queryFn: api.costDrains, retry: false });
  const remove = useMutation({
    mutationFn: (d: CostDrain) => api.removeCostDrain(d.name),
    onSuccess: (_, d) => {
      toast.success(`${d.name} no longer receives the cost lines`);
      queries.invalidateQueries({ queryKey: ["cost-drains"] });
    },
    onError: (e: Error) => toast.error(e.message),
  });
  return (
    <Card>
      <CardHeader>
        <CardTitle>Cost drains</CardTitle>
        <CardDescription>
          Every cost line, sent elsewhere as it is written or revised: usage, OpenCost&apos;s estimate and the provider&apos;s bill, by workspace, project, process and resource. A receiver that fails gets the same lines again; each line&apos;s id says which it is.
        </CardDescription>
        <CardAction>
          <AddDrain onDone={() => queries.invalidateQueries({ queryKey: ["cost-drains"] })} />
        </CardAction>
      </CardHeader>
      <CardContent>
        {drains.isLoading ? (
          <Loading />
        ) : drains.error ? (
          <LicenseNeeded error={drains.error} what="Cost drains" />
        ) : drains.data!.length === 0 ? (
          <p className="text-sm text-muted-foreground">No cost drain: the lines stay here.</p>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Drain</TableHead>
                <TableHead>State</TableHead>
                <TableHead className="text-right">Sent</TableHead>
                <TableHead>Last delivery</TableHead>
                <TableHead className="text-right" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {drains.data!.map((d) => (
                <TableRow key={d.id}>
                  <TableCell>
                    <div className="font-medium">{d.name}</div>
                    <div className="font-mono text-xs text-muted-foreground">{d.url}</div>
                  </TableCell>
                  <TableCell>
                    {d.message ? (
                      <StatusBadge type="error" title={d.message}>
                        failing
                      </StatusBadge>
                    ) : (
                      <StatusBadge type={d.lastDeliveryAt ? "success" : "neutral"}>{d.lastDeliveryAt ? "delivering" : "waiting"}</StatusBadge>
                    )}
                  </TableCell>
                  <TableCell className="text-right tabular-nums">
                    {d.sent}
                    {d.errors > 0 && <span className="ml-1 text-xs text-muted-foreground">({d.errors} failed)</span>}
                  </TableCell>
                  <TableCell className="text-xs text-muted-foreground">{when(d.lastDeliveryAt)}</TableCell>
                  <TableCell className="text-right">
                    <ConfirmDialog
                      trigger={<Button variant="ghost" size="icon-xs" icon={<Trash2 />} aria-label={`Remove ${d.name}`} />}
                      variant="destructive"
                      title={`Stop sending to ${d.name}?`}
                      description="Lines written from now on stay here; what was sent stays with the receiver."
                      action="Remove"
                      onConfirm={() => remove.mutate(d)}
                    />
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

function AddDrain({ onDone }: { onDone: () => void }) {
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [url, setUrl] = useState("");
  const [headers, setHeaders] = useState("");
  const parsed = headersOf(headers);
  const add = useMutation({
    mutationFn: () => api.addCostDrain({ name: name.trim(), url: url.trim(), headers: parsed.headers }),
    onSuccess: (d) => {
      toast.success(`${d.name} receives the cost lines from now on`);
      setOpen(false);
      setName("");
      setUrl("");
      setHeaders("");
      onDone();
    },
    onError: (e: Error) => toast.error(e.message),
  });
  const valid = /^[a-z0-9]([-a-z0-9]{0,38}[a-z0-9])?$/.test(name.trim()) && /^https?:\/\/\S+$/.test(url.trim()) && !parsed.error;
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button size="sm" icon={<Plus />}>
          New drain
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader divider>
          <DialogTitle>Send the cost lines</DialogTitle>
          <DialogDescription>From now on: what was written before the drain existed is not sent. The headers are kept in the cluster and never shown again.</DialogDescription>
        </DialogHeader>
        <form
          className="grid gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            if (valid) add.mutate();
          }}
        >
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="Name">
              <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="finance" autoFocus />
            </Field>
            <Field label="URL">
              <Input value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://billing.example.com/costs" />
            </Field>
          </div>
          <Field label="Headers" hint="One per line, Name=value. Optional." error={parsed.error}>
            <Textarea rows={3} className="font-mono text-xs" value={headers} onChange={(e) => setHeaders(e.target.value)} placeholder="Authorization=Bearer …" />
          </Field>
          <DialogFooter>
            <DialogClose asChild>
              <Button type="button" variant="outline">
                Cancel
              </Button>
            </DialogClose>
            <Button type="submit" disabled={!valid || add.isPending}>
              Send
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
