"use client";

import { Fragment, useEffect, useId, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ChevronDown, ChevronRight } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@shpyrd/ui/components/dialog";
import { toast } from "sonner";
import { ApiError } from "@shpyrd/shared/api/error";
import {
  Alert,
  AlertDescription,
  AlertTitle,
} from "@shpyrd/ui/components/alert";
import { Button } from "@shpyrd/ui/components/button";
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@shpyrd/ui/components/card";
import { Field } from "@shpyrd/ui/components/field";
import { InlineCode } from "@shpyrd/ui/components/inline-code";
import { Input } from "@shpyrd/ui/components/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@shpyrd/ui/components/select";
import { StatusBadge } from "@shpyrd/ui/components/status-badge";
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
import type {
  CostGroup,
  CostKind,
  CostQuery,
  CostRow,
  CostSummary,
} from "@/api/types";
import { Failed, Loading } from "./shared";

// What the cluster uses and costs (enterprise): lines summed by project,
// process, resource or service for a month. Estimated is OpenCost's
// allocation at list prices; real is the provider's bill; the lines carry
// the node's or volume's OCID, so whoever receives them reconciles the two.

// money says amounts by currency, the way the lines carry them.
export function money(m: Record<string, number>): string {
  const keys = Object.keys(m).sort();
  if (keys.length === 0) return "-";
  return keys
    .map((k) => `${m[k].toFixed(2)} ${k || "(currency unspecified)"}`)
    .join(" · ");
}

export const categoryNames: Record<string, string> = {
  project: "Project-attributed",
  system: "System services",
  idle: "Idle capacity",
  unattributed: "Unattributed costs",
};
export const groupNames: Record<CostGroup, string> = {
  workspace: "Workspace",
  project: "Project",
  workload: "Workload",
  process: "Process",
  resourceType: "Resource type",
  resource: "Resource",
  service: "Provider service",
};
const workloadNames: Record<string, string> = {
  applications: "Applications",
  databases: "Databases",
  builds: "Builds",
  storage: "Storage",
  other: "Other workloads",
};
export function rowName(r: CostRow, group: CostGroup): string {
  if (group === "workload")
    return workloadNames[r.workload ?? ""] ?? r.name ?? "Other workloads";
  return (
    r.name ||
    (group === "resource"
      ? r.resource
      : group === "process"
        ? r.process
        : group === "service"
          ? r.service
          : r.slug || r.project) ||
    categoryNames[r.category ?? "unattributed"]
  );
}
export function share(
  cost: number | undefined,
  total: number | undefined,
): string {
  return cost === undefined || total === undefined || total <= 0
    ? "—"
    : `${((cost / total) * 100).toFixed(1)}%`;
}
export function childQuery(q: CostQuery, r: CostRow): CostQuery | null {
  const group = q.group ?? "project";
  const next: Record<CostGroup, CostGroup | null> = {
    workspace: "project",
    project: "workload",
    workload: "process",
    process: "resource",
    resourceType: "resource",
    service: "resourceType",
    resource: null,
  };
  if (!next[group]) return null;
  const result: CostQuery = { ...q, group: next[group]!, offset: 0 };
  const ownership = ["workspace", "project", "workload", "process"].includes(
    group,
  );
  if (ownership) {
    result.workspace = r.workspace || "__none__";
    result.category = r.category;
  }
  if (["project", "workload", "process"].includes(group))
    result.project = r.project || "__none__";
  if (["workload", "process"].includes(group)) result.workload = r.workload;
  if (group === "process") result.process = r.process || "__none__";
  if (group === "resourceType")
    result.resourceType = r.resourceType || "__none__";
  if (group === "service") result.service = r.service || "__none__";
  return result;
}

// monthRange is the first day of a month and the first of the next.
export function monthRange(month: string): { from: string; to: string } {
  const [y, m] = month.split("-").map(Number);
  const next =
    m === 12 ? `${y + 1}-01` : `${y}-${String(m + 1).padStart(2, "0")}`;
  return { from: `${month}-01`, to: `${next}-01` };
}

// LicenseNeeded is what a page of the enterprise shows without a license.
export function LicenseNeeded({
  error,
  what,
}: {
  error: unknown;
  what: string;
}) {
  if (error instanceof ApiError && error.status === 402) {
    return (
      <Alert>
        <AlertTitle>Available with a license</AlertTitle>
        <AlertDescription>
          {what} come with the enterprise license. Install one with{" "}
          <InlineCode>shpyrd-ctl license set &lt;file&gt;</InlineCode>; its
          status is under Settings.
        </AlertDescription>
      </Alert>
    );
  }
  return <Failed what={what.toLowerCase()} error={error} />;
}

function dateOnly(d: Date) {
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
}
function nextDay(day: string) {
  const d = new Date(`${day}T00:00:00Z`);
  d.setUTCDate(d.getUTCDate() + 1);
  return d.toISOString().slice(0, 10);
}
function timestamp(value?: string) {
  return value ? new Date(value).toLocaleString() : "Not recorded";
}

export function Costs() {
  const [from, setFrom] = useState(
    () => `${dateOnly(new Date()).slice(0, 7)}-01`,
  );
  const [through, setThrough] = useState(() => dateOnly(new Date()));
  const [kind, setKind] = useState<CostKind>("estimated");
  const [group, setGroup] = useState<CostGroup>("project");
  const [workspace, setWorkspace] = useState("");
  const [category, setCategory] = useState("");
  const [search, setSearch] = useState("");
  const [debouncedSearch, setDebouncedSearch] = useState("");
  const [currency, setCurrency] = useState("");
  const [sort, setSort] = useState("cost_desc");
  useEffect(() => {
    const timer = setTimeout(() => setDebouncedSearch(search), 300);
    return () => clearTimeout(timer);
  }, [search]);
  const valid =
    !!from &&
    !!through &&
    through >= from &&
    (Date.parse(through) - Date.parse(from)) / 86400000 < 366;
  const q: CostQuery = {
    from,
    to: valid ? nextDay(through) : undefined,
    kind,
    group,
    workspace,
    category,
    search: debouncedSearch,
    currency,
    sort,
    limit: 50,
  };
  const costs = useQuery({
    queryKey: ["costs", q],
    queryFn: () => api.costs(q),
    enabled: valid,
    retry: false,
  });
  const data = costs.data;
  const selectedCurrency = currency || data?.currency || "__none__";
  return (
    <div className="grid gap-6">
      <Card>
        <CardHeader>
          <CardTitle>Cluster costs</CardTitle>
          <CardDescription>
            Explore costs by workspace, project, workload, or infrastructure.
          </CardDescription>
          <CardAction>
            <Dialog>
              <DialogTrigger asChild>
                <Button variant="outline" size="sm">
                  Cost settings
                </Button>
              </DialogTrigger>
              <DialogContent className="max-h-[85vh] overflow-y-auto sm:max-w-2xl">
                <DialogHeader>
                  <DialogTitle>Cost settings</DialogTitle>
                </DialogHeader>
                <OCICard />
              </DialogContent>
            </Dialog>
          </CardAction>
        </CardHeader>
        <CardContent className="grid gap-6">
          <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
            <Field label="From">
              <Input
                type="date"
                value={from}
                onChange={(e) => setFrom(e.target.value)}
              />
            </Field>
            <Field label="Through">
              <Input
                type="date"
                value={through}
                onChange={(e) => setThrough(e.target.value)}
              />
            </Field>
            <Choice
              label="Source"
              value={kind}
              onChange={(v) => {
                setKind(v as CostKind);
                setCurrency("");
              }}
              choices={{
                estimated: "Estimated (OpenCost)",
                real: "Provider-reported cost",
              }}
            />
            <Choice
              label="Group by"
              value={group}
              onChange={(v) => setGroup(v as CostGroup)}
              choices={groupNames}
            />
          </div>
          <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
            <Choice
              label="Workspace"
              value={workspace || "all"}
              onChange={(v) => setWorkspace(v === "all" ? "" : v)}
              choices={{
                all: "All workspaces",
                ...Object.fromEntries(
                  (data?.workspaces ?? []).map((w) => [w.id, w.name]),
                ),
              }}
            />
            <Choice
              label="Category"
              value={category || "all"}
              onChange={(v) => setCategory(v === "all" ? "" : v)}
              choices={{ all: "All categories", ...categoryNames }}
            />
            <Field label="Search">
              <Input
                type="search"
                maxLength={200}
                placeholder="Workspace, project, resource…"
                value={search}
                onChange={(e) => setSearch(e.target.value)}
              />
            </Field>
            <Choice
              label="Sort"
              value={sort}
              onChange={setSort}
              choices={{
                cost_desc: "Highest cost",
                cost_asc: "Lowest cost",
                name: "Name",
              }}
            />
          </div>
          {!valid ? (
            <p role="alert" className="text-sm text-destructive">
              Choose a valid date range of up to 366 days.
            </p>
          ) : costs.isLoading ? (
            <Loading />
          ) : costs.error ? (
            <LicenseNeeded error={costs.error} what="Costs" />
          ) : (
            data && (
              <>
                <div className="flex flex-wrap items-center justify-between gap-4 text-sm text-muted-foreground">
                  <div>
                    <p>
                      Data through: {timestamp(data.collection.through)} · Last
                      successful collection:{" "}
                      {timestamp(data.collection.collectedAt)}
                    </p>
                    <p>
                      {data.incomplete
                        ? "This period is incomplete. More costs may arrive."
                        : "Collection has reached the end of this period. Source corrections may still arrive."}
                    </p>
                  </div>
                  <ExportButton
                    key={JSON.stringify(q)}
                    query={{ ...q, currency: selectedCurrency }}
                  />
                </div>
                <div className="grid grid-cols-2 gap-3 lg:grid-cols-5">
                  {[
                    ["total", "Period total"],
                    ...Object.entries(categoryNames),
                  ].map(([key, label]) => (
                    <div key={key} className="rounded-lg border p-4">
                      <p className="text-sm text-muted-foreground">{label}</p>
                      <p className="mt-1 font-semibold tabular-nums">
                        {money(
                          key === "total"
                            ? data.total
                            : (data.categories[key] ?? {}),
                        )}
                      </p>
                    </div>
                  ))}
                </div>
                {data.currencies.length > 1 && (
                  <div className="max-w-sm">
                    <Choice
                      label="Currency for sorting and percentages"
                      value={selectedCurrency}
                      onChange={setCurrency}
                      choices={Object.fromEntries(
                        data.currencies.map((c) => [
                          c || "__none__",
                          c || "Unspecified currency",
                        ]),
                      )}
                    />
                  </div>
                )}
                <CostTable
                  key={JSON.stringify(q)}
                  query={{ ...q, currency: selectedCurrency }}
                  initial={data}
                />
              </>
            )
          )}
          <details className="text-sm text-muted-foreground">
            <summary className="cursor-pointer">How costs are reported</summary>
            <div className="mt-3 grid gap-2">
              <p>
                Estimates use OpenCost allocations at configured list prices and
                are collected every 15 minutes. Provider-reported costs are
                collected daily and may settle later. These sources are separate
                and are never added together.
              </p>
              <p>
                System services are recognized cluster components. Idle capacity
                is unused capacity reported by OpenCost. Unattributed costs have
                no confirmed project owner. A known workspace is preserved even
                when project ownership is missing.
              </p>
              <p>
                Dates use UTC day boundaries. Totals cover all matching results,
                including other pages. Currencies are never converted or summed
                together. Cost records are collection records, not a count of
                resources.
              </p>
            </div>
          </details>
        </CardContent>
      </Card>
    </div>
  );
}
function Choice({
  label,
  value,
  onChange,
  choices,
}: {
  label: string;
  value: string;
  onChange: (v: string) => void;
  choices: Record<string, string>;
}) {
  const id = useId();
  return (
    <Field label={label} id={id} className="min-w-0">
      <Select value={value} onValueChange={onChange}>
        <SelectTrigger id={id} className="min-w-0 w-full overflow-hidden">
          <SelectValue className="min-w-0">
            <span className="truncate">{choices[value] ?? value}</span>
          </SelectValue>
        </SelectTrigger>
        <SelectContent>
          {Object.entries(choices).map(([key, name]) => (
            <SelectItem key={key} value={key}>
              {name}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </Field>
  );
}
function ExportButton({ query }: { query: CostQuery }) {
  const [id, setId] = useState("");
  const create = useMutation({
    mutationFn: () => api.createCostExport(query),
    onSuccess: (job) => setId(job.id),
    onError: (e: Error) => toast.error(e.message),
  });
  const job = useQuery({
    queryKey: ["cost-export", id],
    queryFn: () => api.costExport(id),
    enabled: !!id,
    refetchInterval: (q) =>
      q.state.data?.status === "ready" ||
      q.state.data?.status === "failed" ||
      q.state.error
        ? false
        : 2000,
    retry: false,
  });
  const download = useQuery({
    queryKey: ["cost-export-url", id],
    queryFn: () => api.costExportURL(id),
    enabled: !!id && job.data?.status === "ready",
    retry: false,
  });
  const pending =
    create.isPending ||
    (!!id &&
      !job.error &&
      job.data?.status !== "ready" &&
      job.data?.status !== "failed");
  return (
    <div className="flex flex-wrap items-center gap-2">
      <Button
        variant="outline"
        size="sm"
        disabled={pending}
        onClick={() => create.mutate()}
      >
        {pending ? "Preparing export…" : "Export CSV"}
      </Button>
      {job.data?.status === "ready" && download.data && (
        <a
          className="underline"
          download="cluster-costs.csv"
          href={download.data}
        >
          Download CSV
        </a>
      )}
      {(job.data?.status === "failed" || job.error || download.error) && (
        <span role="alert">Export failed or expired. Try again.</span>
      )}
    </div>
  );
}
function CostTable({
  query,
  initial,
}: {
  query: CostQuery;
  initial?: CostSummary;
}) {
  const [offset, setOffset] = useState(0);
  const [expanded, setExpanded] = useState<string | null>(null);
  const pageQuery = { ...query, offset };
  const page = useQuery({
    queryKey: ["costs", pageQuery],
    queryFn: () => api.costs(pageQuery),
    initialData: offset === 0 ? initial : undefined,
    retry: false,
  });
  if (page.isLoading) return <Loading />;
  if (page.error) return <Failed what="cost breakdown" error={page.error} />;
  const data = page.data!;
  const group = query.group ?? "project";
  const currency =
    query.currency === "__none__" ? "" : query.currency || data.currency;
  const showWorkspace = ["project", "workload", "process"].includes(group);
  const columns = showWorkspace ? 4 : 3;
  return (
    <div className="grid min-w-0 gap-3">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{groupNames[group]}</TableHead>
            {showWorkspace && <TableHead>Workspace</TableHead>}
            <TableHead className="text-right">Cost</TableHead>
            <TableHead className="text-right">
              % of total{currency ? ` (${currency})` : ""}
            </TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {data.rows.map((r) => {
            const child = childQuery(pageQuery, r);
            const open = expanded === r.key;
            return (
              <Fragment key={r.key}>
                <TableRow>
                  <TableCell>
                    <Button
                      variant="ghost"
                      size="sm"
                      className="max-w-full justify-start"
                      aria-expanded={open}
                      onClick={() => setExpanded(open ? null : r.key)}
                    >
                      {open ? <ChevronDown /> : <ChevronRight />}
                      <span className="truncate">{rowName(r, group)}</span>
                    </Button>
                    {["workload", "process"].includes(group) && r.project && (
                      <p className="text-xs text-muted-foreground">
                        Project: {r.slug || r.project}
                      </p>
                    )}
                    {r.category &&
                      r.category !== "project" &&
                      rowName(r, group) !== categoryNames[r.category] && (
                        <p className="text-xs text-muted-foreground">
                          {categoryNames[r.category]}
                        </p>
                      )}
                    {r.resource && (
                      <p
                        className="max-w-xs truncate font-mono text-xs text-muted-foreground"
                        title={r.resource}
                      >
                        {r.resourceType || "Resource"} · {r.resource}
                      </p>
                    )}
                  </TableCell>
                  {showWorkspace && (
                    <TableCell className="text-muted-foreground">
                      {r.workspaceName || r.workspace || "—"}
                    </TableCell>
                  )}
                  <TableCell className="text-right tabular-nums">
                    {money(r.cost)}
                  </TableCell>
                  <TableCell className="text-right tabular-nums text-muted-foreground">
                    {share(r.cost[currency], data.total[currency])}
                  </TableCell>
                </TableRow>
                {open && (
                  <TableRow>
                    <TableCell colSpan={columns}>
                      <div className="grid gap-3 border-l-2 py-3 pl-3">
                        <p className="text-sm text-muted-foreground">
                          {rowName(r, group)} · {r.lines.toLocaleString()} cost
                          records
                          {child
                            ? " · Percentages below are within this group"
                            : ""}
                        </p>
                        {child ? (
                          <CostTable
                            key={JSON.stringify(child)}
                            query={child}
                          />
                        ) : (
                          <div className="grid gap-2 text-sm">
                            <p className="break-all font-mono">
                              {r.resource ||
                                "No resource identifier was recorded."}
                            </p>
                            {r.resource && (
                              <Button
                                variant="outline"
                                size="sm"
                                className="w-fit"
                                onClick={() =>
                                  navigator.clipboard
                                    .writeText(r.resource!)
                                    .then(
                                      () => toast.success("Resource ID copied"),
                                      () =>
                                        toast.error(
                                          "Could not copy resource ID",
                                        ),
                                    )
                                }
                              >
                                Copy resource ID
                              </Button>
                            )}
                            <p>Resource type: {r.resourceType || "Unknown"}</p>
                            <p>Cost: {money(r.cost)}</p>
                          </div>
                        )}
                      </div>
                    </TableCell>
                  </TableRow>
                )}
              </Fragment>
            );
          })}
          {data.rows.length === 0 && (
            <TableRow>
              <TableCell colSpan={columns}>
                No costs match these filters.
              </TableCell>
            </TableRow>
          )}
          <TableRow>
            <TableCell
              colSpan={showWorkspace ? 2 : 1}
              className="font-semibold"
            >
              Filtered total · {data.count.toLocaleString()} groups
            </TableCell>
            <TableCell className="text-right font-semibold tabular-nums">
              {money(data.total)}
            </TableCell>
            <TableCell />
          </TableRow>
        </TableBody>
      </Table>
      <div className="flex flex-wrap items-center justify-between gap-3 text-sm text-muted-foreground">
        <span>
          {data.count ? offset + 1 : 0}–
          {Math.min(offset + data.rows.length, data.count)} of{" "}
          {data.count.toLocaleString()}
        </span>
        <div className="flex gap-2">
          <Button
            variant="outline"
            size="sm"
            disabled={offset === 0}
            onClick={() => {
              setExpanded(null);
              setOffset(Math.max(0, offset - (query.limit ?? 50)));
            }}
          >
            Previous
          </Button>
          <Button
            variant="outline"
            size="sm"
            disabled={offset + data.rows.length >= data.count}
            onClick={() => {
              setExpanded(null);
              setOffset(offset + (query.limit ?? 50));
            }}
          >
            Next
          </Button>
        </div>
      </div>
    </div>
  );
}

// The bill from OCI: an API key of a user who may read usage and search
// resources. The key never comes back.
function OCICard() {
  const queries = useQueryClient();
  const status = useQuery({
    queryKey: ["costs-oci"],
    queryFn: api.ociStatus,
    retry: false,
  });
  const [form, setForm] = useState({
    tenancy: "",
    user: "",
    fingerprint: "",
    region: "",
    key: "",
  });
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
  const set =
    (k: keyof typeof form) =>
    (e: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) =>
      setForm((f) => ({ ...f, [k]: e.target.value }));
  const valid = Object.values(form).every((v) => v.trim() !== "");
  return (
    <Card>
      <CardHeader>
        <CardTitle>OCI cost source</CardTitle>
        <CardDescription>
          Provider-reported costs per resource and day. Connect a read-only OCI
          account with permission to read usage and search resources.
        </CardDescription>
        {status.data && (
          <CardAction>
            <StatusBadge type={status.data.configured ? "success" : "neutral"}>
              {status.data.configured ? "configured" : "not configured"}
            </StatusBadge>
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
              Tenancy <InlineCode>{status.data!.tenancy}</InlineCode> in{" "}
              <InlineCode>{status.data!.region}</InlineCode>.
            </p>
            <Button
              variant="outline"
              size="sm"
              onClick={() => remove.mutate()}
              disabled={remove.isPending}
            >
              Disconnect OCI
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
                <Input
                  value={form.tenancy}
                  onChange={set("tenancy")}
                  placeholder="ocid1.tenancy.oc1.."
                />
              </Field>
              <Field label="User OCID">
                <Input
                  value={form.user}
                  onChange={set("user")}
                  placeholder="ocid1.user.oc1.."
                />
              </Field>
              <Field label="Fingerprint">
                <Input
                  value={form.fingerprint}
                  onChange={set("fingerprint")}
                  placeholder="aa:bb:cc:…"
                />
              </Field>
              <Field label="Home region">
                <Input
                  value={form.region}
                  onChange={set("region")}
                  placeholder="us-ashburn-1"
                />
              </Field>
            </div>
            <Field
              label="Private key"
              hint="PEM. Kept in the cluster, never shown again."
            >
              <Textarea
                rows={4}
                className="font-mono text-xs"
                value={form.key}
                onChange={set("key")}
                placeholder="-----BEGIN PRIVATE KEY-----"
              />
            </Field>
            <div>
              <Button type="submit" disabled={!valid || save.isPending}>
                Connect OCI
              </Button>
            </div>
          </form>
        )}
      </CardContent>
    </Card>
  );
}
