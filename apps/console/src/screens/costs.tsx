"use client";

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { ApiError } from "@shpyrd/shared/api/error";
import { Alert, AlertDescription, AlertTitle } from "@shpyrd/ui/components/alert";
import { Button } from "@shpyrd/ui/components/button";
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@shpyrd/ui/components/card";
import { Field } from "@shpyrd/ui/components/field";
import { InlineCode } from "@shpyrd/ui/components/inline-code";
import { Input } from "@shpyrd/ui/components/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@shpyrd/ui/components/select";
import { StatusBadge } from "@shpyrd/ui/components/status-badge";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@shpyrd/ui/components/table";
import { Textarea } from "@shpyrd/ui/components/textarea";
import { api } from "@/api/api";
import type { CostGroup, CostKind, CostRow } from "@/api/types";
import { Failed, Loading } from "./shared";

// What the cluster uses and costs (enterprise): lines summed by project,
// process, resource or service for a month. Estimated is OpenCost's
// allocation at list prices; real is the provider's bill; the lines carry
// the node's or volume's OCID, so whoever receives them reconciles the two.

// money says amounts by currency, the way the lines carry them.
export function money(m: Record<string, number>): string {
  const keys = Object.keys(m).sort();
  if (keys.length === 0) return "-";
  return keys.map((k) => `${m[k].toFixed(2)} ${k}`).join(" + ");
}

// rowName is how a row reads: a project's slug, the platform when there is
// no project.
export function rowName(r: CostRow, group: CostGroup): string {
  const project = r.slug || r.project || "the platform";
  switch (group) {
    case "process":
      return `${project} · ${r.process ?? "-"}`;
    case "resource":
      return r.resource || "(none)";
    case "service":
      return r.service || "(none)";
    default:
      return project;
  }
}

// monthRange is the first day of a month and the first of the next.
export function monthRange(month: string): { from: string; to: string } {
  const [y, m] = month.split("-").map(Number);
  const next = m === 12 ? `${y + 1}-01` : `${y}-${String(m + 1).padStart(2, "0")}`;
  return { from: `${month}-01`, to: `${next}-01` };
}

// LicenseNeeded is what a page of the enterprise shows without a license.
export function LicenseNeeded({ error, what }: { error: unknown; what: string }) {
  if (error instanceof ApiError && error.status === 402) {
    return (
      <Alert>
        <AlertTitle>Available with a license</AlertTitle>
        <AlertDescription>
          {what} come with the enterprise license. Install one with <InlineCode>shpyrd-ctl license set &lt;file&gt;</InlineCode>; its status is under Settings.
        </AlertDescription>
      </Alert>
    );
  }
  return <Failed what={what.toLowerCase()} error={error} />;
}

export function Costs() {
  const [month, setMonth] = useState(() => new Date().toISOString().slice(0, 7));
  const [kind, setKind] = useState<CostKind>("estimated");
  const [group, setGroup] = useState<CostGroup>("project");
  const range = monthRange(month);
  const costs = useQuery({ queryKey: ["costs", month, kind, group], queryFn: () => api.costs({ ...range, kind, group }), retry: false });
  return (
    <div className="grid gap-6">
      <Card>
        <CardHeader>
          <CardTitle>Costs</CardTitle>
          <CardDescription>
            What the cluster costs, line by line. Estimated is OpenCost&apos;s share of each node and volume at list prices, read every quarter of an hour, the hour under way included; real is the provider&apos;s bill, read once a day. The cost drains send every line, with the node&apos;s or volume&apos;s OCID, to whoever reconciles them.
          </CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4">
          <div className="grid gap-4 sm:grid-cols-3">
            <Field label="Month">
              <Input type="month" value={month} onChange={(e) => setMonth(e.target.value || month)} />
            </Field>
            <Field label="Costs">
              <Select value={kind} onValueChange={(v) => setKind(v as CostKind)}>
                <SelectTrigger className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="estimated">Estimated (OpenCost)</SelectItem>
                  <SelectItem value="real">Real (the bill)</SelectItem>
                </SelectContent>
              </Select>
            </Field>
            <Field label="By">
              <Select value={group} onValueChange={(v) => setGroup(v as CostGroup)}>
                <SelectTrigger className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="project">Project</SelectItem>
                  <SelectItem value="process">Process</SelectItem>
                  <SelectItem value="resource">Resource</SelectItem>
                  <SelectItem value="service">Service</SelectItem>
                </SelectContent>
              </Select>
            </Field>
          </div>
          {costs.isLoading ? (
            <Loading />
          ) : costs.error ? (
            <LicenseNeeded error={costs.error} what="Costs" />
          ) : costs.data!.rows.length === 0 ? (
            <p className="text-sm text-muted-foreground">Nothing recorded for this month yet.</p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{group === "process" ? "Project · process" : group[0].toUpperCase() + group.slice(1)}</TableHead>
                  <TableHead className="text-right">Cost</TableHead>
                  <TableHead className="text-right">Lines</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {costs.data!.rows.map((r) => (
                  <TableRow key={r.key}>
                    <TableCell className={group === "resource" ? "font-mono text-xs" : "font-medium"}>{rowName(r, group)}</TableCell>
                    <TableCell className="text-right tabular-nums">{money(r.cost)}</TableCell>
                    <TableCell className="text-right tabular-nums text-muted-foreground">{r.lines}</TableCell>
                  </TableRow>
                ))}
                <TableRow>
                  <TableCell className="font-semibold">Total</TableCell>
                  <TableCell className="text-right font-semibold tabular-nums">{money(costs.data!.total)}</TableCell>
                  <TableCell />
                </TableRow>
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
      <OCICard />
    </div>
  );
}

// The bill from OCI: an API key of a user who may read usage and search
// resources. The key never comes back.
function OCICard() {
  const queries = useQueryClient();
  const status = useQuery({ queryKey: ["costs-oci"], queryFn: api.ociStatus, retry: false });
  const [form, setForm] = useState({ tenancy: "", user: "", fingerprint: "", region: "", key: "" });
  const save = useMutation({
    mutationFn: () => api.putOCI(form),
    onSuccess: (r) => {
      toast.success(`The bill of ${r.region} is read once a day`);
      setForm({ tenancy: "", user: "", fingerprint: "", region: "", key: "" });
      queries.setQueryData(["costs-oci"], r);
    },
    onError: (e: Error) => toast.error(e.message),
  });
  const remove = useMutation({
    mutationFn: api.deleteOCI,
    onSuccess: () => queries.setQueryData(["costs-oci"], { configured: false }),
    onError: (e: Error) => toast.error(e.message),
  });
  const set = (k: keyof typeof form) => (e: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) => setForm((f) => ({ ...f, [k]: e.target.value }));
  const valid = Object.values(form).every((v) => v.trim() !== "");
  return (
    <Card>
      <CardHeader>
        <CardTitle>The bill from OCI</CardTitle>
        <CardDescription>The real costs, per resource and day, with each resource&apos;s tags. An API key of a user who may read usage and search resources; read-only.</CardDescription>
        {status.data && (
          <CardAction>
            <StatusBadge type={status.data.configured ? "success" : "neutral"}>{status.data.configured ? "read daily" : "not set"}</StatusBadge>
          </CardAction>
        )}
      </CardHeader>
      <CardContent>
        {status.isLoading ? (
          <Loading rows={1} />
        ) : status.error ? (
          <LicenseNeeded error={status.error} what="The bill" />
        ) : status.data!.configured ? (
          <div className="flex flex-wrap items-center justify-between gap-4">
            <p className="text-sm text-muted-foreground">
              Tenancy <InlineCode>{status.data!.tenancy}</InlineCode> in <InlineCode>{status.data!.region}</InlineCode>.
            </p>
            <Button variant="outline" size="sm" onClick={() => remove.mutate()} disabled={remove.isPending}>
              Stop reading the bill
            </Button>
          </div>
        ) : (
          <form
            className="grid gap-4"
            onSubmit={(e) => {
              e.preventDefault();
              if (valid) save.mutate();
            }}
          >
            <div className="grid gap-4 sm:grid-cols-2">
              <Field label="Tenancy OCID">
                <Input value={form.tenancy} onChange={set("tenancy")} placeholder="ocid1.tenancy.oc1.." />
              </Field>
              <Field label="User OCID">
                <Input value={form.user} onChange={set("user")} placeholder="ocid1.user.oc1.." />
              </Field>
              <Field label="Fingerprint">
                <Input value={form.fingerprint} onChange={set("fingerprint")} placeholder="aa:bb:cc:…" />
              </Field>
              <Field label="Home region">
                <Input value={form.region} onChange={set("region")} placeholder="us-ashburn-1" />
              </Field>
            </div>
            <Field label="Private key" hint="PEM. Kept in the cluster, never shown again.">
              <Textarea rows={4} className="font-mono text-xs" value={form.key} onChange={set("key")} placeholder="-----BEGIN PRIVATE KEY-----" />
            </Field>
            <div>
              <Button type="submit" disabled={!valid || save.isPending}>
                Read the bill
              </Button>
            </div>
          </form>
        )}
      </CardContent>
    </Card>
  );
}
