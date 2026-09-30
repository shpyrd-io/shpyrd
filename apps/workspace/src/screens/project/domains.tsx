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
import { InlineCode } from "@shpyrd/ui/components/inline-code";
import { Input } from "@shpyrd/ui/components/input";
import { StatusBadge } from "@shpyrd/ui/components/status-badge";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@shpyrd/ui/components/table";
import type { DomainStatus } from "@/api/types";
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
  list: () => Promise<{ target: string; domains: DomainStatus[] }>;
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
            <AddDomain add={add} target={domains.data?.target} onDone={refresh} />
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
                <TableRow key={d.host}>
                  <TableCell>
                    <InlineCode>{d.host}</InlineCode>
                  </TableCell>
                  <TableCell>
                    <StatusBadge type={d.dns === "ok" ? "success" : d.dns === "unknown" ? "neutral" : "warning"} live={d.dns === "missing"}>
                      {d.dns === "ok" ? "Points here" : d.dns === "missing" ? "Waiting for the CNAME" : d.dns === "wrong" ? "Points elsewhere" : "Unknown"}
                    </StatusBadge>
                    {d.dns !== "ok" && d.target && (
                      <div className="mt-1 font-mono text-[11px] text-muted-foreground">
                        CNAME → {d.target}
                      </div>
                    )}
                  </TableCell>
                  <TableCell>
                    <StatusBadge type={d.certificate === "ready" || d.certificate === "wildcard" ? "success" : d.certificate === "failed" ? "error" : "warning"} live={d.certificate === "issuing"}>
                      {d.certificate === "wildcard" ? "Platform's" : d.certificate === "ready" ? "Ready" : d.certificate === "issuing" ? "Issuing" : "Failed"}
                    </StatusBadge>
                    {d.message && <div className="mt-1 text-[11px] text-muted-foreground">{d.message}</div>}
                  </TableCell>
                  <TableCell className="text-right">
                    {canEdit && d.certificate !== "wildcard" && (
                      <ConfirmDialog
                        trigger={<Button variant="ghost" size="icon-xs" icon={<Trash2 />} aria-label={`Remove ${d.host}`} />}
                        variant="destructive"
                        title={`Remove ${d.host}?`}
                        description="It stops answering here. The record at your registrar stays as it is."
                        action="Remove"
                        onConfirm={() => removeIt.mutate(d.host)}
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
  );
}

function AddDomain({ add, target, onDone }: { add: (host: string) => Promise<unknown>; target?: string; onDone: () => void }) {
  const [open, setOpen] = useState(false);
  const [host, setHost] = useState("");
  const valid = /^[a-z0-9.-]+\.[a-z]{2,}$/i.test(host.trim());
  const addIt = useMutation({
    mutationFn: () => add(host.trim().toLowerCase()),
    onSuccess: () => {
      onDone();
      setOpen(false);
      setHost("");
      toast.success("Domain added", { description: "Point its CNAME at the target; the certificate follows." });
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
            At your registrar, point a CNAME of it at <InlineCode>{target ?? "the target"}</InlineCode>. The certificate is issued once the record is seen.
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
