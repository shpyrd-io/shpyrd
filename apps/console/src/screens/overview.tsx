"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Cpu, MemoryStick } from "lucide-react";
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@shpyrd/ui/components/card";
import { InfoTable, InfoTableItem } from "@shpyrd/ui/components/info-table";
import { Meter } from "@shpyrd/ui/components/meter";
import { PageHeading } from "@shpyrd/ui/components/page-heading";
import { ProgressBar } from "@shpyrd/ui/components/progress-bar";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@shpyrd/ui/components/select";
import { StatusBadge } from "@shpyrd/ui/components/status-badge";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@shpyrd/ui/components/table";
import { TimeChart } from "@shpyrd/ui/components/time-chart";
import { api } from "@/api/api";
import { ago, bytes, Failed, Loading } from "./shared";

// What the environment was built for, and so how load balancing, DNS,
// TLS and the registry are provided.
const profiles: Record<string, string> = {
  local: "A local kind cluster: a front door on the host, local names, a development CA, a registry in the cluster.",
  oci: "Oracle Cloud: an OCI load balancer, a wildcard DNS record, Let's Encrypt certificates, OCIR.",
};

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
  return (
    <>
      <PageHeading
        title="Cluster"
        description={`${c.nodes.length} ${c.nodes.length === 1 ? "node" : "nodes"}, ${c.apps} ${c.apps === 1 ? "project" : "projects"}, ${Object.entries(c.phases)
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
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Node</TableHead>
                <TableHead>Role</TableHead>
                <TableHead className="w-44">CPU used · reserved</TableHead>
                <TableHead className="w-44">Memory used · reserved</TableHead>
                <TableHead>Instances</TableHead>
                <TableHead className="text-right">Kubelet</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {c.nodes.map((n) => {
                const u = byName.get(n.name);
                const under = [n.instanceType, n.zone].filter(Boolean).join(" · ");
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
                    <TableCell className="text-xs">{n.roles}</TableCell>
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
