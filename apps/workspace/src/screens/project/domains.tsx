"use client";

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Copy, Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { Badge } from "@shpyrd/ui/components/badge";
import { Button } from "@shpyrd/ui/components/button";
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@shpyrd/ui/components/card";
import { ConfirmDialog } from "@shpyrd/ui/components/confirm-dialog";
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from "@shpyrd/ui/components/dialog";
import { Field } from "@shpyrd/ui/components/field";
import { InlineCode } from "@shpyrd/ui/components/inline-code";
import { Input } from "@shpyrd/ui/components/input";
import { StatusBadge } from "@shpyrd/ui/components/status-badge";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@shpyrd/ui/components/table";
import type { DnsRecord, DomainStatus } from "@/api/types";
import { Failed, Loading } from "./shared";

// Addresses of your own that reach something: a project, or the
// workspace. Each needs a record pointed at the target; the certificate
// follows by itself.
export function DomainsCard({
  title,
  description,
  queryKey,
  list,
  add,
  remove,
  canEdit,
}: {
  title: string;
  description: string;
  queryKey: unknown[];
  list: () => Promise<{ target: string; address?: string; domains: DomainStatus[] }>;
  add: (host: string) => Promise<unknown>;
  remove: (host: string) => Promise<unknown>;
  canEdit: boolean;
}) {
  const queries = useQueryClient();
  const domains = useQuery({ queryKey, queryFn: list, refetchInterval: 15_000 });
  const refresh = () => queries.invalidateQueries({ queryKey });
  const removeIt = useMutation({
    mutationFn: remove,
    onSuccess: () => {
      refresh();
      toast.success("Domain removed");
    },
    onError: (e: Error) => toast.error(e.message),
  });
  return (
    <Card>
      <CardHeader>
        <CardTitle>{title}</CardTitle>
        <CardDescription>{description}</CardDescription>
        {canEdit && (
          <CardAction>
            <AddDomain add={add} target={domains.data?.target} address={domains.data?.address} onDone={refresh} />
          </CardAction>
        )}
      </CardHeader>
      <CardContent>
        {domains.isLoading ? (
          <Loading />
        ) : domains.error ? (
          <Failed what="the domains" error={domains.error} />
        ) : (domains.data?.domains ?? []).length === 0 ? (
          <p className="text-sm text-muted-foreground">None yet. It answers at its own address until one is added.</p>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Domain</TableHead>
                <TableHead>Record</TableHead>
                <TableHead>Certificate</TableHead>
                <TableHead className="text-right" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {domains.data!.domains.map((d) => (
                <DomainRows key={d.host} domain={d} canEdit={canEdit} onRemove={() => removeIt.mutate(d.host)} />
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  );
}

// One domain: its row, and under it, while the domain does not point here
// yet, the record to publish.
function DomainRows({ domain: d, canEdit, onRemove }: { domain: DomainStatus; canEdit: boolean; onRemove: () => void }) {
  const record: DnsRecord | undefined = d.record ?? (d.target ? { type: "CNAME", name: d.host, value: d.target } : undefined);
  return (
    <>
      <TableRow>
        <TableCell>
          <InlineCode>{d.host}</InlineCode>
        </TableCell>
        <TableCell>
          <StatusBadge type={d.dns === "ok" ? "success" : d.dns === "unknown" ? "neutral" : "warning"} live={d.dns === "missing"}>
            {d.dns === "ok" ? "Points here" : d.dns === "missing" ? "Waiting for the record" : d.dns === "wrong" ? "Points elsewhere" : "Not checked yet"}
          </StatusBadge>
        </TableCell>
        <TableCell>
          <StatusBadge type={d.certificate === "ready" || d.certificate === "wildcard" ? "success" : d.certificate === "failed" ? "error" : "warning"} live={d.certificate === "issuing"}>
            {d.certificate === "wildcard" ? "Platform's" : d.certificate === "ready" ? "Ready" : d.certificate === "issuing" ? "Issuing" : "Failed"}
          </StatusBadge>
          {d.certificate === "failed" && d.message && <div className="mt-1 text-[11px] text-muted-foreground">{d.message}</div>}
        </TableCell>
        <TableCell className="text-right">
          {canEdit && d.certificate !== "wildcard" && (
            <ConfirmDialog
              trigger={<Button variant="ghost" size="icon-xs" icon={<Trash2 />} aria-label={`Remove ${d.host}`} />}
              variant="destructive"
              title={`Remove ${d.host}?`}
              description="It stops answering here. The record at your DNS provider stays as it is."
              action="Remove"
              onConfirm={onRemove}
            />
          )}
        </TableCell>
      </TableRow>
      {d.dns !== "ok" && d.certificate !== "wildcard" && record && (
        <TableRow className="hover:bg-transparent">
          <TableCell colSpan={4} className="pt-0 whitespace-normal">
            <RecordsPanel records={[record]}>
              {d.dns === "wrong"
                ? "It points somewhere else. Change its record at your DNS provider to:"
                : "Publish this record at your DNS provider. The certificate is issued once the record is seen."}
            </RecordsPanel>
          </TableCell>
        </TableRow>
      )}
    </>
  );
}

// What to publish at the DNS provider: a sentence, then the records.
export function RecordsPanel({ records, children }: { records: DnsRecord[]; children: React.ReactNode }) {
  return (
    <div className="grid gap-3 rounded-lg border bg-muted/40 p-3">
      <p className="text-sm text-muted-foreground">{children}</p>
      <DnsRecords records={records} />
    </div>
  );
}

// Records to publish, one per line: type, name and value, the name and the
// value each a click away from the clipboard.
export function DnsRecords({ records }: { records: DnsRecord[] }) {
  const copy = (text: string) => {
    void navigator.clipboard?.writeText(text);
    toast.success("Copied", { description: text });
  };
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead className="w-24">Type</TableHead>
          <TableHead className="w-1/2">Name</TableHead>
          <TableHead>Value</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {records.map((r) => (
          <TableRow key={`${r.type} ${r.name}`}>
            <TableCell>
              <Badge variant="outline" className="font-mono">
                {r.type}
              </Badge>
            </TableCell>
            {[r.name, r.value].map((text, i) => (
              <TableCell key={i} className="whitespace-normal">
                {text ? (
                  <span className="inline-flex items-center gap-1">
                    <InlineCode className="break-all">{text}</InlineCode>
                    <Button variant="ghost" size="icon-xs" icon={<Copy />} aria-label={`Copy ${text}`} onClick={() => copy(text)} />
                  </span>
                ) : (
                  // The front door's address, where the platform cannot see
                  // it (a cluster with no load balancer reporting one).
                  <span className="text-xs text-muted-foreground">The platform's address: ask whoever runs it.</span>
                )}
              </TableCell>
            ))}
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}

function AddDomain({ add, target, address, onDone }: { add: (host: string) => Promise<unknown>; target?: string; address?: string; onDone: () => void }) {
  const [open, setOpen] = useState(false);
  const [host, setHost] = useState("");
  const valid = /^[a-z0-9.-]+\.[a-z]{2,}$/i.test(host.trim());
  const addIt = useMutation({
    mutationFn: () => add(host.trim().toLowerCase()),
    onSuccess: () => {
      onDone();
      setOpen(false);
      setHost("");
      toast.success("Domain added", { description: "Publish its record, shown in the list; the certificate follows." });
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
          <DialogTitle>Add a domain</DialogTitle>
          <DialogDescription>
            A name under your domain, such as app.example.com, points here with a CNAME to <InlineCode>{target ?? "the project's address"}</InlineCode>.
            The domain itself, such as example.com, cannot have a CNAME: it takes an A record{address ? <> to <InlineCode>{address}</InlineCode></> : null}.
            The list shows the record to publish once it is added.
          </DialogDescription>
        </DialogHeader>
        <Field label="Domain" hint="Without the scheme: app.example.com." error={host && !valid ? "That is not a host name." : undefined}>
          <Input value={host} onChange={(e) => setHost(e.target.value)} placeholder="app.example.com" autoFocus />
        </Field>
        <DialogFooter>
          <DialogClose asChild>
            <Button variant="outline">Cancel</Button>
          </DialogClose>
          <Button disabled={!valid || addIt.isPending} onClick={() => addIt.mutate()}>
            Add
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
