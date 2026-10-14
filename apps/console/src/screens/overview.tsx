"use client";

import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ArrowLeft, RefreshCw } from "lucide-react";
import { Alert, AlertDescription, AlertTitle } from "@shpyrd/ui/components/alert";
import { Badge } from "@shpyrd/ui/components/badge";
import { Button } from "@shpyrd/ui/components/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@shpyrd/ui/components/card";
import { Checkbox } from "@shpyrd/ui/components/checkbox";
import { Input } from "@shpyrd/ui/components/input";
import { PageHeading } from "@shpyrd/ui/components/page-heading";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@shpyrd/ui/components/select";
import { StatusBadge } from "@shpyrd/ui/components/status-badge";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@shpyrd/ui/components/table";
import { Tabs, TabsList, TabsTrigger } from "@shpyrd/ui/components/tabs";
import { TimeChart } from "@shpyrd/ui/components/time-chart";
import type { SchedulingSummary } from "@/api/types";
import { api } from "@/api/api";
import type { NodeDevice, OverviewNode, OverviewPool, OverviewView, Values } from "@/api/cluster-overview";
import { ago, bytes, Failed, Loading } from "./shared";

const polling = { staleTime: 30_000, refetchInterval: 30_000, refetchIntervalInBackground: false, retry: 1, retryDelay: 5000 };
const purposes: Record<string, string> = { platform: "Platform services", data: "Databases and stores", apps: "Processes, builds and one-off runs" };
export const poolName = (name: string, mixed = false) => name || (mixed ? "Unassigned" : "Default");
export const number = (value: number | undefined) => value === undefined ? "Unavailable" : value.toLocaleString(undefined, { maximumFractionDigits: 1 });
export const percent = (value: number | undefined) => value === undefined ? "Unavailable" : `${value.toFixed(0)}%`;
const size = (value: number | undefined) => value === undefined ? "Unavailable" : bytes(value);
const throughput = (value: number | undefined) => value === undefined ? "Unavailable" : `${bytes(value)}/s`;
const difference = (a: number | undefined, b: number | undefined) => a === undefined || b === undefined ? undefined : a - b;

function Choice({ label, value, onChange, options }: { label: string; value: string; onChange: (value: string) => void; options: [string, string][] }) {
  return <Select value={value} onValueChange={onChange}><SelectTrigger aria-label={label} className="w-auto min-w-32"><SelectValue /></SelectTrigger><SelectContent>{options.map(([key, title]) => <SelectItem key={key} value={key}>{title}</SelectItem>)}</SelectContent></Select>;
}
function ViewTabs({ value, onChange, pods = false }: { value: string; onChange: (v: string) => void; pods?: boolean }) {
  return <Tabs value={value} onValueChange={onChange}><TabsList variant="line"><TabsTrigger value="compute">Compute</TabsTrigger><TabsTrigger value="storage">Storage & I/O</TabsTrigger><TabsTrigger value="network">Network</TabsTrigger>{pods && <TabsTrigger value="pods">Pods</TabsTrigger>}</TabsList></Tabs>;
}
function Coverage({ pool, metric }: { pool: OverviewPool; metric: string }) {
  return <span className="mt-1 block text-xs text-muted-foreground">{pool.coverage[metric] ?? 0}/{pool.nodes} nodes reporting</span>;
}
function PoolValue({ pool, metric }: { pool: OverviewPool; metric: string }) {
  return <><span className="font-mono">{percent(pool.values[metric])}</span><Coverage pool={pool} metric={metric} /></>;
}
function Charts({ pool, range, view, nodes = "", device, mount }: { pool: string; range: string; view: OverviewView; nodes?: string; device?: string; mount?: string }) {
  const charts = useQuery({ queryKey: ["overview-charts", pool, range, view, nodes, device, mount], queryFn: ({ signal }) => api.overviewCharts({ pool, range, view, nodes: nodes || undefined, device, mount }, signal), ...polling });
  if (charts.isLoading) return <Loading rows={2} />;
  if (charts.error) return <Failed what="the charts" error={charts.error} />;
  return <div className="grid gap-6 @5xl/page-layout:grid-cols-2">{charts.data?.charts.map(chart => <div key={chart.id}>
    {chart.error ? <Alert variant="warning"><AlertTitle>{chart.title} unavailable</AlertTitle><AlertDescription>{chart.error}</AlertDescription></Alert> : chart.series.length === 0 ? <Card><CardHeader><CardTitle>{chart.title}</CardTitle><CardDescription>No telemetry in this range.</CardDescription></CardHeader></Card> : <TimeChart title={chart.title} unit={chart.unit} series={chart.series.map(s => ({ ...s, name: s.name || "Default" }))} range={range} kind="line" description={nodes ? "Selected nodes · I/O rates averaged over 2 minutes" : "Pool aggregate · includes all reporting nodes"} />}
    {chart.note && <p className="text-xs text-muted-foreground">{chart.note}</p>}
  </div>)}</div>;
}

export function Overview() {
  const [pool, setPool] = useState<string | null>(null);
  const [range, setRange] = useState("1h");
  const [node, setNode] = useState<string | null>(null);
  const scheduling = useQuery({ queryKey: ["cluster-scheduling"], queryFn: api.clusterScheduling, ...polling });
  const overview = useQuery({ queryKey: ["cluster-overview"], queryFn: ({ signal }) => api.overview(signal), ...polling });
  const data = overview.data;
  const attention = useQuery({ queryKey: ["overview-attention", data?.snapshot], queryFn: ({ signal }) => api.overviewNodes({ snapshot: data!.snapshot, pool: "*", view: "compute", issues: "true", sort: "severity", limit: 10 }, signal), enabled: !!data, staleTime: Infinity, retry: 1 });
  if (overview.isLoading) return <Loading rows={6} />;
  if (!data) return <Failed what="the cluster" error={overview.error} />;
  const selected = data.pools.find(p => p.name === pool) ?? data.pools[0];
  const total = data.pools.reduce((sum, p) => sum + p.nodes, 0);
  const ready = data.pools.reduce((sum, p) => sum + p.ready, 0);
  const mixed = data.pools.some(p => !!p.name);
  const stale = Date.now() - new Date(data.updatedAt).getTime() > 90_000;
  return <>
    {process.env.NEXT_PUBLIC_API_MODE === "mock" && <Badge variant="secondary">Mock preview · illustrative metrics</Badge>}
    <PageHeading title="Cluster" description={`${number(total)} nodes · ${data.pools.length} pools · Updated ${ago(data.updatedAt)}`} iconEnd={<StatusBadge type={ready === total && total > 0 ? "success" : "warning"}>{ready}/{total} nodes ready</StatusBadge>} />
    <SchedulingAlert scheduling={scheduling.error ? { pending: [], error: "Could not check pending instances. Scheduling status is unavailable." } : scheduling.data} />
    <div className="flex flex-wrap items-center justify-end gap-3">
      <Choice label="Time range" value={range} onChange={setRange} options={[["1h", "Last hour"], ["6h", "Last 6 hours"], ["24h", "Last 24 hours"], ["7d", "Last 7 days"]]} />
      <Button variant="secondary" onClick={() => { void overview.refetch(); }} disabled={overview.isFetching}><RefreshCw />Refresh</Button>
    </div>
    {overview.error && <Failed what="the latest overview" error={overview.error} />}
    {stale && <Alert variant="warning"><AlertTitle>Snapshot is stale</AlertTitle><AlertDescription>Showing the last available node snapshot from {ago(data.updatedAt)}. Capacity and health may have changed.</AlertDescription></Alert>}
    {data.errors.map(e => <Alert key={e} variant="warning"><AlertTitle>Incomplete telemetry</AlertTitle><AlertDescription>{e}</AlertDescription></Alert>)}
    {!node && <><Card><CardHeader><CardTitle>Needs attention <Badge variant="secondary">{attention.data?.total ?? "…"} nodes</Badge></CardTitle><CardDescription>Node conditions, current reservation thresholds and storage headroom. Activity alone does not indicate a fault.</CardDescription></CardHeader><CardContent>
      {attention.error && <Failed what="the attention items" error={attention.error} />}
      {attention.isLoading && <Loading rows={2} />}
      {attention.data?.total === 0 && <p className="text-sm text-muted-foreground">No issues detected in this snapshot.</p>}
      <div className="grid gap-2">{attention.data?.nodes.map(n => <div key={n.name} className="flex flex-wrap items-center justify-between gap-2 border-b border-border pb-2"><div className="text-sm"><span className="font-mono">{n.name}</span><span className="ml-2 text-muted-foreground">{poolName(n.pool ?? "", mixed)} · {n.issues.join(" · ")}</span></div><Button variant="secondary" size="sm" onClick={() => { setPool(n.pool ?? ""); setNode(n.name); }}>Inspect</Button></div>)}</div>
      {(attention.data?.total ?? 0) > 10 && <p className="mt-3 text-xs text-muted-foreground">Showing 10 affected nodes. Use “Issues only” in each pool to inspect every result.</p>}
    </CardContent></Card>
    <Card><CardHeader><CardTitle>Capacity by pool</CardTitle><CardDescription>Host usage and pod requests have separate denominators. Free capacity in another pool cannot satisfy this pool’s workloads.</CardDescription></CardHeader><CardContent>
      <div className="grid gap-4 @5xl/page-layout:hidden">{data.pools.map(p => <div key={p.name} className="rounded-lg border border-border p-4">
        <div className="mb-4 flex items-center justify-between gap-2"><Button variant="secondary" onClick={() => { setPool(p.name); setNode(null); }}>{poolName(p.name, mixed)}</Button><span className="text-sm">{p.ready}/{p.nodes} ready · {p.issues} with issues</span></div>
        <dl className="grid grid-cols-2 gap-4 text-sm">{[["CPU busy", "cpuBusyPct"], ["CPU requested", "cpuRequestedPct"], ["Memory used", "memoryUsedPct"], ["Memory requested", "memoryRequestedPct"]].map(([label, key]) => <div key={key}><dt className="text-muted-foreground">{label}</dt><dd><PoolValue pool={p} metric={key} /></dd></div>)}<div><dt className="text-muted-foreground">Pod slots</dt><dd>{number(p.values.pods)} / {number(p.values.podCapacity)}<Coverage pool={p} metric="podUsedPct" /></dd></div><div><dt className="text-muted-foreground">Low disk space</dt><dd>{p.distribution.diskAvailablePct ?? 0} nodes</dd></div></dl>
      </div>)}</div>
      <div className="hidden @5xl/page-layout:block"><Table><TableHeader><TableRow>{["Pool", "Ready", "CPU busy", "CPU requested", "Memory used", "Memory requested", "Pod slots", "Attention"].map(t => <TableHead key={t}>{t}</TableHead>)}</TableRow></TableHeader><TableBody>
        {data.pools.map(p => <TableRow key={p.name}><TableCell><Button variant="secondary" size="sm" onClick={() => { setPool(p.name); setNode(null); }}>{poolName(p.name, mixed)}</Button><p className="mt-1 text-xs text-muted-foreground">{purposes[p.name] ?? "Shared node capacity"}</p></TableCell><TableCell>{p.ready}/{p.nodes}</TableCell>{["cpuBusyPct", "cpuRequestedPct", "memoryUsedPct", "memoryRequestedPct"].map(k => <TableCell key={k}><PoolValue pool={p} metric={k} /></TableCell>)}<TableCell>{number(p.values.pods)} / {number(p.values.podCapacity)}<Coverage pool={p} metric="podUsedPct" /></TableCell><TableCell>{p.issues} nodes<p className="text-xs text-muted-foreground">{p.missing} without complete compute telemetry</p></TableCell></TableRow>)}
      </TableBody></Table></div>
      {!data.pools.length && <p className="text-sm text-muted-foreground">No nodes registered.</p>}
    </CardContent></Card>
    </>}
    {node ? <NodeInspection key={node} name={node} snapshot={data.snapshot} range={range} onBack={() => setNode(null)} /> : selected && <PoolInspection key={selected.name} pool={selected} snapshot={data.snapshot} range={range} mixed={mixed} onInspect={setNode} />}
    <details className="rounded-lg border border-border p-4"><summary className="cursor-pointer text-sm font-medium">Installation details</summary><dl className="mt-4 grid gap-4 text-sm @3xl/page-layout:grid-cols-3">{Object.entries(data.install ?? {}).map(([k, v]) => <div key={k}><dt className="text-muted-foreground">{k}</dt><dd className="break-all font-mono">{v}</dd></div>)}</dl></details>
  </>;
}

function PoolInspection({ pool, snapshot, range, mixed, onInspect }: { pool: OverviewPool; snapshot: string; range: string; mixed: boolean; onInspect: (node: string) => void }) {
  const [view, setView] = useState<OverviewView>("compute");
  const [search, setSearch] = useState("");
  const [query, setQuery] = useState("");
  const [zone, setZone] = useState("");
  const [status, setStatus] = useState("all");
  const [issues, setIssues] = useState(false);
  const [sort, setSort] = useState("cpuRequestedPct");
  const [page, setPage] = useState({ snapshot, offset: 0 });
  const [compare, setCompare] = useState<string[]>([]);
  useEffect(() => {
    if (search === query) return;
    const timer = setTimeout(() => { setQuery(search); setPage(p => ({ ...p, offset: 0 })); }, 300);
    return () => clearTimeout(timer);
  }, [search, query]);
  // Freeze later pages at their original snapshot; new snapshots only refresh page one.
  const activeSnapshot = page.offset === 0 ? snapshot : page.snapshot;
  const nodes = useQuery({ queryKey: ["overview-nodes", activeSnapshot, pool.name, view, query, zone, status, issues, sort, page.offset], queryFn: ({ signal }) => api.overviewNodes({ snapshot: activeSnapshot, pool: pool.name, view, search: query, zone, status: status === "all" ? undefined : status, issues: String(issues), sort, offset: page.offset }, signal), staleTime: Infinity, retry: 1 });
  const reset = () => setPage({ snapshot, offset: 0 });
  return <Card><CardHeader><CardTitle>{poolName(pool.name, mixed)}</CardTitle><CardDescription>{purposes[pool.name] ?? "Nodes in this pool"} · {pool.nodes} nodes</CardDescription></CardHeader><CardContent className="grid gap-6">
    <div className="flex flex-wrap gap-3 text-sm"><Badge variant="secondary">{pool.distribution.cpuBusyPct ?? 0} nodes ≥85% CPU</Badge><Badge variant="secondary">{pool.distribution.cpuRequestedPct ?? 0} nodes ≥85% CPU requests</Badge><Badge variant="secondary">{pool.distribution.memoryUsedPct ?? 0} nodes ≥85% memory</Badge><Badge variant="secondary">{pool.distribution.diskAvailablePct ?? 0} nodes &lt;10% disk available ({pool.coverage.diskAvailablePct ?? 0}/{pool.nodes} reporting)</Badge></div>
    <ViewTabs value={view} onChange={v => { setView(v as OverviewView); setSort(v === "storage" ? "diskAvailablePct" : v === "network" ? "receiveBytes" : "cpuRequestedPct"); reset(); }} />
    {view === "compute" && <p className="text-xs text-muted-foreground">Busy/used: physical host capacity. Requests: scheduler requests against allocatable capacity, with a container/init-plus-overhead estimate when scheduler metrics are unavailable. That estimate may differ for native sidecars or pod-level resources. Unrequested resources do not guarantee placement.</p>}
    {view === "storage" && <p className="text-xs text-muted-foreground">Space is shown for each node’s most constrained filesystem. I/O totals include sd/vd/xvd and NVMe disks; layered devices and partitions are excluded. Persistent volume capacity is separate.</p>}
    {view === "network" && <p className="text-xs text-muted-foreground">Sum of eth/en host interfaces, including internal traffic. Loopback, bridges and pod interfaces are excluded. This is traffic volume, not link utilization.</p>}
    <Charts pool={pool.name} range={range} view={view} nodes={compare.join(",")} />
    <div className="flex flex-wrap items-center gap-3"><Input className="w-56" aria-label="Search nodes" placeholder="Search node…" value={search} onChange={e => setSearch(e.target.value)} /><Input className="w-44" aria-label="Filter by zone" placeholder="Exact zone…" value={zone} onChange={e => { setZone(e.target.value); reset(); }} /><Choice label="Node status" value={status} onChange={v => { setStatus(v); reset(); }} options={[["all", "All statuses"], ["ready", "Ready"], ["not-ready", "Not ready"]]} /><Choice label="Sort nodes" value={sort} onChange={v => { setSort(v); reset(); }} options={view === "compute" ? [["cpuRequestedPct", "Highest CPU requests"], ["cpuBusyPct", "Highest CPU usage"], ["memoryUsedPct", "Highest memory usage"], ["podUsedPct", "Highest pod occupancy"], ["name", "Node name"]] : view === "storage" ? [["diskAvailablePct", "Lowest available space"], ["readIOPS", "Highest read IOPS"], ["writeIOPS", "Highest write IOPS"], ["name", "Node name"]] : [["receiveBytes", "Highest receive rate"], ["transmitBytes", "Highest transmit rate"], ["name", "Node name"]]} /><label className="flex items-center gap-2 text-sm"><Checkbox checked={issues} onCheckedChange={v => { setIssues(v === true); reset(); }} />Issues only</label></div>
    <div className="flex items-center gap-3 text-xs text-muted-foreground">Select up to five nodes to compare. {compare.length > 0 && <Button size="sm" variant="secondary" onClick={() => setCompare([])}>Clear comparison ({compare.length})</Button>}</div>
    {nodes.isLoading && <Loading rows={5} />}{nodes.error && <><Failed what="the nodes; refresh to start a new snapshot" error={nodes.error} /><Button variant="secondary" onClick={reset}>Return to current snapshot</Button></>}
    {nodes.data && <><Table><TableHeader><TableRow><TableHead>Compare</TableHead><TableHead>Node</TableHead>{columns[view].map(c => <TableHead key={c.label}>{c.label}</TableHead>)}</TableRow></TableHeader><TableBody>{nodes.data.nodes.map(n => <TableRow key={n.name}><TableCell><Checkbox aria-label={`Compare ${n.name}`} checked={compare.includes(n.name)} disabled={compare.length >= 5 && !compare.includes(n.name)} onCheckedChange={v => setCompare(v === true ? [...compare, n.name] : compare.filter(x => x !== n.name))} /></TableCell><TableCell><Button variant="secondary" size="sm" onClick={() => onInspect(n.name)}>{n.name}</Button><div className="mt-2 flex items-center gap-2"><StatusBadge type={n.ready ? "success" : "error"}>{n.ready ? "Ready" : "Not ready"}</StatusBadge>{n.unschedulable && <Badge variant="secondary">Cordoned</Badge>}</div><p className="mt-1 max-w-72 text-xs whitespace-normal text-muted-foreground">{n.zone} · {n.instanceType}</p>{n.issues.length > 0 && <p className="mt-1 max-w-72 text-xs whitespace-normal text-warning">{n.issues.join(" · ")}</p>}</TableCell>{columns[view].map(c => <TableCell className="font-mono text-xs" key={c.label}>{c.render(n.values, n)}</TableCell>)}</TableRow>)}</TableBody></Table>
      {nodes.data.total === 0 && <p className="text-sm text-muted-foreground">No nodes match these filters.</p>}
      <div className="flex flex-wrap items-center justify-between gap-3 text-sm text-muted-foreground"><span>{nodes.data.total ? `${page.offset + 1}–${page.offset + nodes.data.nodes.length}` : "0"} of {number(nodes.data.total)} · Snapshot {ago(nodes.data.updatedAt)}</span><div className="flex gap-2"><Button variant="secondary" disabled={page.offset === 0} onClick={() => setPage({ snapshot: activeSnapshot, offset: Math.max(0, page.offset - 50) })}>Previous</Button><Button variant="secondary" disabled={nodes.data.next < 0} onClick={() => setPage({ snapshot: activeSnapshot, offset: nodes.data!.next })}>Next 50</Button></div></div>
    </>}
  </CardContent></Card>;
}

const columns: Record<OverviewView, { label: string; render: (v: Values, n: OverviewNode) => React.ReactNode }[]> = {
  compute: [
    { label: "CPU busy", render: v => percent(v.cpuBusyPct) }, { label: "CPU requested", render: v => percent(v.cpuRequestedPct) },
    { label: "Memory used", render: v => percent(v.memoryUsedPct) }, { label: "Memory requested", render: v => percent(v.memoryRequestedPct) }, { label: "Pod slots", render: v => `${number(v.pods)} / ${number(v.podCapacity)}` },
  ],
  storage: [
    { label: "Mount", render: (_, n) => n.diskMount ?? "Unavailable" }, { label: "Used / total", render: v => `${size(difference(v.diskSize, v.diskFree))} / ${size(v.diskSize)}` }, { label: "Free / available", render: v => `${size(v.diskFree)} / ${size(v.diskAvailable)}` },
    { label: "Read / write IOPS", render: v => `${number(v.readIOPS)} / ${number(v.writeIOPS)}` }, { label: "Read / write", render: v => `${throughput(v.readBytes)} / ${throughput(v.writeBytes)}` },
  ],
  network: [
    { label: "Receive", render: v => throughput(v.receiveBytes) }, { label: "Transmit", render: v => throughput(v.transmitBytes) }, { label: "Errors Rx / Tx /s", render: v => `${number(v.receiveErrors)} / ${number(v.transmitErrors)}` }, { label: "Drops Rx / Tx /s", render: v => `${number(v.receiveDrops)} / ${number(v.transmitDrops)}` },
  ],
};

function NodeInspection({ name, snapshot, range, onBack }: { name: string; snapshot: string; range: string; onBack: () => void }) {
  const [view, setView] = useState<OverviewView | "pods">("compute");
  const [deviceName, setDeviceName] = useState("all");
  const detail = useQuery({ queryKey: ["overview-node", snapshot, name, view], queryFn: ({ signal }) => api.overviewNode({ snapshot, node: name, view: view as OverviewView }, signal), enabled: view !== "pods", staleTime: 30_000, retry: 1 });
  const node = detail.data?.node;
  const device = detail.data?.devices.find(d => d.name === deviceName);
  return <Card><CardHeader><Button className="w-fit" variant="secondary" size="sm" onClick={onBack}><ArrowLeft />Back to pool</Button><CardTitle>{name}</CardTitle><CardDescription>{node ? `${node.instanceType ?? node.arch} · ${node.zone ?? "No zone"} · ${node.kubeletVersion} · ${node.unschedulable ? "Scheduling disabled" : "Scheduling enabled"}` : "Node inspection"}</CardDescription></CardHeader><CardContent className="grid gap-6">
    <ViewTabs value={view} onChange={v => { setView(v as typeof view); setDeviceName("all"); }} pods />
    {view === "pods" ? <Pods key={name} node={name} /> : <>
      {detail.isLoading && <Loading rows={4} />}{detail.error && <Failed what="the node details" error={detail.error} />}
      {detail.data && <>
        <p className="text-xs text-muted-foreground">Node snapshot {ago(detail.data.snapshotAt)} · Device query {ago(detail.data.updatedAt)}</p>
        {detail.data.errors.map(e => <Alert key={e} variant="warning"><AlertTitle>Incomplete telemetry</AlertTitle><AlertDescription>{e}</AlertDescription></Alert>)}
        {node && <div className="flex flex-wrap gap-2"><StatusBadge type={node.ready ? "success" : "error"}>{node.ready ? "Ready" : "Not ready"}</StatusBadge>{node.conditions.map(c => <Badge key={c} variant="secondary">{c}</Badge>)}{node.issues.map(i => <Badge key={i} variant="secondary">{i}</Badge>)}</div>}
        {view === "compute" && node && <Table><TableHeader><TableRow>{columns.compute.map(c => <TableHead key={c.label}>{c.label}</TableHead>)}</TableRow></TableHeader><TableBody><TableRow>{columns.compute.map(c => <TableCell key={c.label}>{c.render(node.values, node)}</TableCell>)}</TableRow></TableBody></Table>}
        {view === "compute" && node && <p className="text-sm text-muted-foreground">CPU: {number(node.values.cpuCapacity)} host cores / {number(node.values.cpuAllocatable)} allocatable. Memory: {size(node.values.memoryCapacity)} host / {size(node.values.memoryAllocatable)} allocatable.</p>}
        {view === "storage" && <StorageDevices devices={detail.data.devices} />}
        {view === "network" && <Table><TableHeader><TableRow><TableHead>Interface</TableHead>{columns.network.map(c => <TableHead key={c.label}>{c.label}</TableHead>)}</TableRow></TableHeader><TableBody>{detail.data.devices.map(d => <TableRow key={d.name}><TableCell>{d.name}</TableCell>{columns.network.map(c => <TableCell key={c.label}>{c.render(d.values, node!)}</TableCell>)}</TableRow>)}</TableBody></Table>}
        {view !== "compute" && detail.data.devices.length === 0 && <p className="text-sm text-muted-foreground">No device telemetry available.</p>}
        {detail.data.truncated && <Alert variant="warning"><AlertTitle>Device limit reached</AlertTitle><AlertDescription>Showing a bounded selection of devices. This is not the full node inventory.</AlertDescription></Alert>}
      </>}
      {view !== "compute" && !!detail.data?.devices.length && <Choice label="Chart device" value={deviceName} onChange={setDeviceName} options={[["all", "All eligible host devices"], ...detail.data.devices.map(d => [d.name, d.name] as [string,string])]} />}
      <Charts pool="" range={range} view={view} nodes={name} device={device?.device} mount={device?.mount} />
    </>}
  </CardContent></Card>;
}
function StorageDevices({ devices }: { devices: NodeDevice[] }) {
  const filesystems = devices.filter(d => d.values.size !== undefined);
  const disks = devices.filter(d => d.values.size === undefined);
  const latency = (v: Values, direction: string) => v[direction + "Seconds"] === undefined || !v[direction + "IOPS"] ? "Unavailable" : `${(1000 * v[direction + "Seconds"]! / v[direction + "IOPS"]!).toFixed(1)} ms`;
  return <>
    <p className="text-xs text-muted-foreground">Available space excludes filesystem reservations. Free and available can differ. Filesystems are not summed; duplicate mounts cannot inflate a capacity total.</p>
    <Table><TableHeader><TableRow>{["Filesystem", "Used", "Free", "Available", "Total", "Inodes used"].map(x => <TableHead key={x}>{x}</TableHead>)}</TableRow></TableHeader><TableBody>{filesystems.map(d => <TableRow key={d.name}><TableCell>{d.name}</TableCell><TableCell>{size(difference(d.values.size, d.values.free))}</TableCell><TableCell>{size(d.values.free)}</TableCell><TableCell>{size(d.values.available)}</TableCell><TableCell>{size(d.values.size)}</TableCell><TableCell>{percent(d.values.inodes && d.values.inodesFree !== undefined ? 100 * (1 - d.values.inodesFree / d.values.inodes) : undefined)}</TableCell></TableRow>)}</TableBody></Table>
    <Table><TableHeader><TableRow>{["Device", "Read / write IOPS", "Read / write throughput", "Read / write latency"].map(x => <TableHead key={x}>{x}</TableHead>)}</TableRow></TableHeader><TableBody>{disks.map(d => <TableRow key={d.name}><TableCell>{d.name}</TableCell><TableCell>{number(d.values.readIOPS)} / {number(d.values.writeIOPS)}</TableCell><TableCell>{throughput(d.values.readBytes)} / {throughput(d.values.writeBytes)}</TableCell><TableCell>{latency(d.values, "read")} / {latency(d.values, "write")}</TableCell></TableRow>)}</TableBody></Table>
  </>;
}
function Pods({ node }: { node: string }) {
  const [namespace, setNamespace] = useState("");
  const [filter, setFilter] = useState("");
  const [phase, setPhase] = useState("all");
  const [cursor, setCursor] = useState("");
  const pods = useQuery({ queryKey: ["overview-pods", node, filter, phase, cursor], queryFn: ({ signal }) => api.overviewPods({ node, namespace: filter || undefined, phase: phase === "all" ? undefined : phase, continue: cursor || undefined }, signal), staleTime: 30_000, retry: 1 });
  return <><p className="text-sm text-muted-foreground">Pods assigned to this node only. Up to 50 per page; no cluster-wide project or pod inventory is loaded.</p><form className="flex flex-wrap gap-3" onSubmit={e => { e.preventDefault(); setFilter(namespace); setCursor(""); }}><Input aria-label="Pod namespace" className="w-56" placeholder="Exact namespace…" value={namespace} onChange={e => setNamespace(e.target.value)} /><Button variant="secondary" type="submit">Filter</Button><Choice label="Pod phase" value={phase} onChange={v => { setPhase(v); setCursor(""); }} options={[["all", "All phases"], ...["Running", "Pending", "Succeeded", "Failed", "Unknown"].map(p => [p, p] as [string, string])]} /></form>
    {pods.isLoading && <Loading rows={3} />}{pods.error && <Failed what="the node pods" error={pods.error} />}
    {pods.data && <><Table><TableHeader><TableRow><TableHead>Namespace</TableHead><TableHead>Pod</TableHead><TableHead>Phase</TableHead></TableRow></TableHeader><TableBody>{pods.data.pods.map(p => <TableRow key={`${p.namespace}/${p.name}`}><TableCell>{p.namespace}</TableCell><TableCell className="font-mono text-xs">{p.name}</TableCell><TableCell>{p.phase}</TableCell></TableRow>)}</TableBody></Table>{!pods.data.pods.length && <p className="text-sm text-muted-foreground">No pods match these filters.</p>}<div className="flex gap-2"><Button variant="secondary" disabled={!cursor} onClick={() => setCursor("")}>First page</Button><Button variant="secondary" disabled={!pods.data.continue} onClick={() => setCursor(pods.data!.continue)}>Next 50</Button></div></>}
  </>;
}

export function SchedulingAlert({ scheduling }: { scheduling?: SchedulingSummary }) {
  if (!scheduling) return null;
  if (scheduling.error) return (
    <Alert variant="warning">
      <AlertTitle>Scheduling status unavailable</AlertTitle>
      <AlertDescription>{scheduling.error}</AlertDescription>
    </Alert>
  );
  if (!scheduling.pending.length) return null;
  const capacity = scheduling.pending.filter((p) => p.capacity).length;
  return (
    <Alert variant="warning">
      <AlertTitle>{capacity ? "Instances waiting for cluster resources" : "Instances waiting for scheduling"}</AlertTitle>
      <AlertDescription>
        <p>{scheduling.pending.length} pending {scheduling.pending.length === 1 ? "instance cannot" : "instances cannot"} be placed on a node.</p>
        {capacity > 0 && <p>The scheduler reports a resource shortage for {capacity} {capacity === 1 ? "instance" : "instances"}. Add or free capacity in the required pool, or review resource requests. Other placement constraints may also need attention.</p>}
        {!capacity && <p>Review the placement constraints below; adding capacity alone may not resolve them.</p>}
        <ul className="grid gap-2">
          {scheduling.pending.map((p) => (
            <li key={p.namespace + "/" + p.name}>
              <span className="font-mono text-foreground">{p.namespace}/{p.name}</span>
              {p.pool && <span> ({p.pool} pool)</span>}
              <p className="break-words">{p.message || "The scheduler has not reported a reason yet."}</p>
            </li>
          ))}
        </ul>
      </AlertDescription>
    </Alert>
  );
}
