"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Cpu, MemoryStick } from "lucide-react";
import { Alert, AlertDescription, AlertTitle } from "@shpyrd/ui/components/alert";
import { Badge } from "@shpyrd/ui/components/badge";
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@shpyrd/ui/components/card";
import { InfoTable, InfoTableItem } from "@shpyrd/ui/components/info-table";
import { InlineCode } from "@shpyrd/ui/components/inline-code";
import { Meter } from "@shpyrd/ui/components/meter";
import { PageHeading } from "@shpyrd/ui/components/page-heading";
import { ProgressBar } from "@shpyrd/ui/components/progress-bar";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@shpyrd/ui/components/select";
import { StatusBadge } from "@shpyrd/ui/components/status-badge";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@shpyrd/ui/components/table";
import { TimeChart } from "@shpyrd/ui/components/time-chart";
import { api } from "@/api/api";
import type { MisplacedPod, Node } from "@/api/types";
import { ago, bytes, Failed, Loading } from "./shared";

// What the environment was built for, and so how load balancing, DNS,
// TLS and the registry are provided.
const profiles: Record<string, string> = {
  local: "A local kind cluster: a front door on the host, local names, a development CA, a registry in the cluster.",
  oci: "Oracle Cloud: an OCI load balancer, a wildcard DNS record, Let's Encrypt certificates, OCIR.",
};

// The node pools (RFC-0077), in the order they are listed, and what the
// controller puts on each. A node without a pool is on a single-pool
// cluster, where everything lands everywhere.
const pools: Record<string, string> = {
  platform: "The platform itself, and whatever has no pool of its own.",
  data: "Project databases, Redis and object storage.",
  apps: "Project processes, builds and one-off runs.",
};
const poolOrder = (n: Node) => {
  const i = Object.keys(pools).indexOf(n.pool ?? "");
  return i < 0 ? Object.keys(pools).length : i;
};

// "2 platform, 1 data and 2 apps": how many nodes each pool has, in the
// order above; empty when no node has a pool.
function poolCounts(nodes: Node[]): string {
  const counts = new Map<string, number>();
  for (const n of nodes) if (n.pool) counts.set(n.pool, (counts.get(n.pool) ?? 0) + 1);
  const parts = [...counts].sort(([a], [b]) => poolOrder({ pool: a } as Node) - poolOrder({ pool: b } as Node)).map(([pool, n]) => `${n} ${pool}`);
  return parts.length > 1 ? `${parts.slice(0, -1).join(", ")} and ${parts[parts.length - 1]}` : (parts[0] ?? "");
}

// The platform's pods outside the platform pool, by node: the pool, what
// made them ("keda/keda-operator") and the CPU they reserve there.
export function misplacedByNode(pods: MisplacedPod[]): { node: string; pool: string; owners: string[]; millicores: number }[] {
  const nodes = new Map<string, { node: string; pool: string; owners: string[]; millicores: number }>();
  for (const p of pods) {
    const n = nodes.get(p.node) ?? { node: p.node, pool: p.pool, owners: [], millicores: 0 };
    const owner = `${p.namespace}/${p.owner?.split("/")[1] ?? p.name}`;
    if (!n.owners.includes(owner)) n.owners.push(owner);
    n.millicores += Number.parseInt(p.cpu ?? "", 10) || 0;
    nodes.set(p.node, n);
  }
  return [...nodes.values()];
}

// The cluster as it is: what was installed, where it answers, and how
// full the machines are.
export function Overview() {
  const [range, setRange] = useState("1h");
  const config = useQuery({ queryKey: ["config"], queryFn: api.config, staleTime: 60_000 });
  const cluster = useQuery({ queryKey: ["cluster"], queryFn: api.cluster, refetchInterval: 30_000 });
  const metrics = useQuery({ queryKey: ["cluster-metrics", range], queryFn: () => api.clusterMetrics(range), refetchInterval: 30_000, enabled: config.data?.metrics !== false });
  if (cluster.isLoading) return <Loading rows={6} />;
  if (cluster.error || !cluster.data) return <Failed what="the cluster" error={cluster.error} />;
  const c = cluster.data;
  const m = metrics.data;
  const serverVersion = config.data?.version;
  const installer = c.install?.version;
  const byName = new Map((m?.nodes ?? []).map((n) => [n.name, n]));
  const failing = c.nodes.filter((n) => !n.ready).length;
  const nodes = [...c.nodes].sort((a, b) => poolOrder(a) - poolOrder(b) || a.name.localeCompare(b.name));
  const byPool = poolCounts(nodes);
  return (
    <>
      <PageHeading
        title="Cluster"
        description={`${c.nodes.length} ${c.nodes.length === 1 ? "node" : "nodes"}${byPool ? ` (${byPool})` : ""}, ${c.apps} ${c.apps === 1 ? "project" : "projects"}, ${Object.entries(c.phases)
          .map(([phase, n]) => `${n} ${phase.toLowerCase()}`)
          .join(", ")}.`}
        iconEnd={failing ? <StatusBadge type="error">{`${failing} ${failing === 1 ? "node" : "nodes"} not ready`}</StatusBadge> : <StatusBadge type="success">Every node ready</StatusBadge>}
      />
      <Card>
        <CardHeader>
          <CardTitle>Installed</CardTitle>
          <CardDescription>What shpyrd cluster init put in place, and where the cluster answers.</CardDescription>
        </CardHeader>
        <CardContent>
          <InfoTable columns={4}>
            <InfoTableItem label="Environment">{c.install?.profile ?? "-"}</InfoTableItem>
            <InfoTableItem label="Version" mono>
              {serverVersion ?? installer ?? "-"}
            </InfoTableItem>
            <InfoTableItem label="Domain" mono>
              {c.install?.domain ?? "-"}
            </InfoTableItem>
            <InfoTableItem label="Base stack updated">{c.install?.updatedAt ? ago(c.install.updatedAt) : "-"}</InfoTableItem>
            {c.externalLBAddress && (
              <InfoTableItem label="Public front door" mono>
                {c.externalLBAddress}
              </InfoTableItem>
            )}
            {c.internalLBAddress && (
              <InfoTableItem label="Private front door" mono>
                {c.internalLBAddress}
              </InfoTableItem>
            )}
            {c.install?.profile && profiles[c.install.profile] && (
              <InfoTableItem label="What that means" span={2}>
                {profiles[c.install.profile]}
              </InfoTableItem>
            )}
            {serverVersion && installer && installer !== serverVersion && <InfoTableItem label="Installed with">the {installer} CLI; the server runs {serverVersion}</InfoTableItem>}
          </InfoTable>
        </CardContent>
      </Card>
      <Card>
        <CardHeader>
          <CardTitle>Capacity</CardTitle>
          <CardDescription>Used is what the machines are doing right now. Reserved is what the running processes asked for, which is what limits how much more can be scheduled.</CardDescription>
          <CardAction>
            <Select value={range} onValueChange={setRange}>
              <SelectTrigger size="sm" className="w-36">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="1h">Last hour</SelectItem>
                <SelectItem value="6h">Last 6 hours</SelectItem>
                <SelectItem value="24h">Last 24 hours</SelectItem>
                <SelectItem value="7d">Last 7 days</SelectItem>
              </SelectContent>
            </Select>
          </CardAction>
        </CardHeader>
        <CardContent className="grid gap-6">
          {metrics.error && <Failed what="the metrics" error={metrics.error} />}
          {metrics.isLoading && <Loading rows={2} />}
          {m && (
            <>
              <div className="grid gap-x-8 gap-y-6 @3xl/page-layout:grid-cols-2">
                <Meter label="CPU" unit="cores" icon={<Cpu />} used={Math.round((m.total.cpuUsedPct / 100) * m.total.cpuCores * 100) / 100} reserved={Math.round((m.total.cpuRequestedPct / 100) * m.total.cpuCores * 100) / 100} capacity={m.total.cpuCores} />
                <Meter label="Memory" unit="GiB" icon={<MemoryStick />} used={gib((m.total.memoryUsedPct / 100) * m.total.memoryBytes)} reserved={gib((m.total.memoryRequestedPct / 100) * m.total.memoryBytes)} capacity={gib(m.total.memoryBytes)} />
              </div>
              <div className="grid gap-8 @5xl/page-layout:grid-cols-2">
                <TimeChart title="CPU used" description="Of each node's cores, whatever runs on it" unit="%" series={m.cpu} />
                <TimeChart title="Memory used" description="Of each node's memory, whatever runs on it" unit="%" series={m.memory} />
              </div>
            </>
          )}
          {c.misplaced && c.misplaced.length > 0 && (
            <Alert variant="warning">
              <AlertTitle>{`${c.misplaced.length} ${c.misplaced.length === 1 ? "pod" : "pods"} of the platform outside the platform pool`}</AlertTitle>
              <AlertDescription>
                <p>
                  They take room and pods from the projects, and keep the autoscaler from removing an idle node. <InlineCode>shpyrd cluster init</InlineCode> pins them; each moves when its workload rolls.
                </p>
                <ul className="grid gap-1">
                  {misplacedByNode(c.misplaced).map((n) => (
                    <li key={n.node}>
                      <span className="font-mono text-foreground">{n.node}</span> ({n.pool}, {n.millicores}m reserved): {n.owners.join(", ")}
                    </li>
                  ))}
                </ul>
              </AlertDescription>
            </Alert>
          )}
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Node</TableHead>
                <TableHead>Pool</TableHead>
                <TableHead className="w-44">CPU used · reserved</TableHead>
                <TableHead className="w-44">Memory used · reserved</TableHead>
                <TableHead>Instances</TableHead>
                <TableHead className="text-right">Kubelet</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {nodes.map((n) => {
                const u = byName.get(n.name);
                const under = [n.instanceType, n.zone].filter(Boolean).join(" · ");
                // The Kubernetes role matters only when it is not a plain worker.
                const role = n.roles === "worker" ? "" : n.roles;
                return (
                  <TableRow key={n.name}>
                    <TableCell>
                      <div className="flex items-center gap-2 font-medium">
                        <StatusBadge type={n.ready ? "success" : "error"} variant="secondary">
                          {n.ready ? "ready" : "not ready"}
                        </StatusBadge>
                        <span className="font-mono text-xs">{n.name}</span>
                        <span className="font-mono text-[10px] text-muted-foreground">{n.arch}</span>
                      </div>
                      {under && <div className="mt-0.5 text-[11px] text-muted-foreground">{under}</div>}
                    </TableCell>
                    <TableCell>
                      {n.pool ? (
                        <>
                          <Badge variant="secondary">{n.pool}</Badge>
                          <div className="mt-1 max-w-52 text-[11px] leading-snug whitespace-normal text-muted-foreground">{[pools[n.pool], role].filter(Boolean).join(" · ")}</div>
                        </>
                      ) : (
                        <>
                          <span className="text-xs text-muted-foreground">-</span>
                          <div className="mt-0.5 max-w-52 text-[11px] leading-snug whitespace-normal text-muted-foreground">{[role, "No pools: everything lands here."].filter(Boolean).join(" · ")}</div>
                        </>
                      )}
                    </TableCell>
                    <TableCell>
                      {u ? (
                        <div className="grid gap-1">
                          <ProgressBar value={u.cpuUsedPct} max={100} size="sm" tone={u.cpuUsedPct >= 85 ? "error" : "orange"} format={(v) => `${v.toFixed(0)}%`} />
                          <ProgressBar value={u.cpuRequestedPct} max={100} size="sm" tone="blue" format={(v) => `${v.toFixed(0)}%`} />
                          <span className="text-[10px] text-muted-foreground">{u.cpuCores} cores</span>
                        </div>
                      ) : (
                        "-"
                      )}
                    </TableCell>
                    <TableCell>
                      {u ? (
                        <div className="grid gap-1">
                          <ProgressBar value={u.memoryUsedPct} max={100} size="sm" tone={u.memoryUsedPct >= 85 ? "error" : "orange"} format={(v) => `${v.toFixed(0)}%`} />
                          <ProgressBar value={u.memoryRequestedPct} max={100} size="sm" tone="blue" format={(v) => `${v.toFixed(0)}%`} />
                          <span className="text-[10px] text-muted-foreground">{bytes(u.memoryBytes)}</span>
                        </div>
                      ) : (
                        "-"
                      )}
                    </TableCell>
                    <TableCell className="font-mono text-xs">{u ? `${u.pods} / ${u.podCapacity}` : "-"}</TableCell>
                    <TableCell className="text-right font-mono text-xs text-muted-foreground">{n.kubeletVersion}</TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        </CardContent>
      </Card>
    </>
  );
}

const gib = (b: number) => Math.round((b / (1 << 30)) * 10) / 10;
