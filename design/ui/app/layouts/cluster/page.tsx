"use client";

import { Cpu, MemoryStick, Server } from "lucide-react";
import { Badge } from "@shpyrd/ui/components/badge";
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@shpyrd/ui/components/card";
import { InfoTable, InfoTableItem } from "@shpyrd/ui/components/info-table";
import { InlineCode } from "@shpyrd/ui/components/inline-code";
import { Meter } from "@shpyrd/ui/components/meter";
import { PageHeading } from "@shpyrd/ui/components/page-heading";
import { ProgressBar } from "@shpyrd/ui/components/progress-bar";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@shpyrd/ui/components/select";
import { Stack } from "@shpyrd/ui/components/stack";
import { StatusBadge } from "@shpyrd/ui/components/status-badge";
import { Switch } from "@shpyrd/ui/components/switch";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@shpyrd/ui/components/table";
import { TimeChart } from "@shpyrd/ui/components/time-chart";
import { Section } from "../../section";
import { Frame } from "../frame";
import { cpu, memory } from "../../samples";

// The cluster: the machines under everything. What they are doing, what
// the processes have reserved on them, and how many there are, between
// the fewest the cluster keeps and the most it may grow to.

// The node pools, in the order they are listed, and what lands on each.
const pools: Record<string, string> = {
  platform: "The platform itself, and whatever has no pool of its own.",
  data: "Project databases and Redis.",
  apps: "Project processes, builds and one-off runs.",
};

const nodes = [
  { name: "10.0.1.14", shape: "VM.Standard.A1.Flex · sa-saopaulo-1-AD-1", arch: "arm64", pool: "platform", cpu: [62, 78], memory: [71, 84], cores: 4, gib: 24, pods: "23 / 110", kubelet: "v1.31.2", ready: true },
  { name: "10.0.1.27", shape: "VM.Standard.A1.Flex · sa-saopaulo-1-AD-1", arch: "arm64", pool: "platform", cpu: [48, 65], memory: [55, 70], cores: 4, gib: 24, pods: "19 / 110", kubelet: "v1.31.2", ready: true },
  { name: "10.0.1.31", shape: "VM.Standard.A1.Flex · sa-saopaulo-1-AD-2", arch: "arm64", pool: "data", cpu: [88, 92], memory: [81, 90], cores: 4, gib: 24, pods: "27 / 110", kubelet: "v1.31.2", ready: true },
  { name: "10.0.1.40", shape: "VM.Standard.A1.Flex · sa-saopaulo-1-AD-2", arch: "arm64", pool: "apps", cpu: [0, 0], memory: [0, 0], cores: 4, gib: 24, pods: "0 / 110", kubelet: "v1.31.2", ready: false },
];

const extensions = [
  { name: "auth-local", what: "Accounts with email and password, and the invitations.", on: true },
  { name: "mail", what: "The platform sends mail: invitations, alerts.", on: true },
  { name: "object-storage", what: "Buckets for the projects, on the cluster's storage.", on: false },
  { name: "logs-agent", what: "Logs are collected from every instance and kept.", on: true },
];

// Two thin bars: what is used, over what is reserved.
function Usage({ label, used, reserved, detail }: { label: string; used: number; reserved: number; detail: string }) {
  return (
    <Stack gap="tight">
      <Stack direction="horizontal" justify="space-between" className="font-mono text-[11px]">
        <span>{used}%</span>
        <span className="text-chart-2">{reserved}%</span>
      </Stack>
      <ProgressBar aria-label={`${label} used`} value={used} size="sm" tone={used >= 85 ? "error" : "orange"} />
      <ProgressBar aria-label={`${label} reserved`} value={reserved} size="sm" tone="blue" />
      <span className="text-[10px] text-muted-foreground">{detail}</span>
    </Stack>
  );
}

export default function Page() {
  return (
    <Section title="The cluster">
      <Frame app="console" here="cluster">
        <PageHeading
          as="h2"
          icon={<Server />}
          title="Cluster"
          description="The machines under every workspace, and the platform's own pieces on them."
        />

        <Card>
          <CardContent>
            <InfoTable columns={4}>
              <InfoTableItem label="Environment profile">
                oci{" "}
                <span className="text-xs text-muted-foreground">Oracle Cloud: OCI load balancer, wildcard DNS, Let's Encrypt</span>
              </InfoTableItem>
              <InfoTableItem label="shpyrd version" mono>
                v0.42.0
              </InfoTableItem>
              <InfoTableItem label="Domain" mono>
                acme.shpyrd.app
              </InfoTableItem>
              <InfoTableItem label="Base stack updated">
                3 days ago{" "}
                <span className="text-xs text-muted-foreground">last shpyrd cluster init</span>
              </InfoTableItem>
            </InfoTable>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Capacity</CardTitle>
            <CardDescription>
              <b>Used</b> is what the machines are doing now; <b>reserved</b> is what the running
              processes asked for, which is what limits how much more can be scheduled.
            </CardDescription>
            <CardAction>
              <Select defaultValue="1h">
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
            <div className="grid gap-x-8 gap-y-6 @3xl/page-layout:grid-cols-3">
              <Meter label="CPU" unit="cores" icon={<Cpu />} used={7.9} reserved={9.4} capacity={12} />
              <Meter label="Memory" unit="GiB" icon={<MemoryStick />} used={49.7} reserved={58.6} capacity={72} />
              <ProgressBar
                label="Nodes"
                max={8}
                segments={[
                  { label: "Always kept", value: 2, tone: "neutral" },
                  { label: "Added on demand", value: 1 },
                  { label: "Joining", value: 1, tone: "warning" },
                ]}
                format={(v) => `${v}`}
                className="content-start"
              />
            </div>
            <p className="-mt-2 text-xs text-muted-foreground">
              The cluster keeps at least 2 nodes and grows to at most 8, one at a time, when what is
              reserved passes 85% of what there is.
            </p>
            <div className="grid gap-6 border-t pt-6 @3xl/page-layout:grid-cols-2">
              <TimeChart
                title="CPU"
                description="Of each node's cores, whatever runs on it"
                unit="%"
                series={cpu.map((s, i) => ({ name: nodes[i].name, points: s.points }))}
              />
              <TimeChart
                title="Memory"
                description="Of each node's memory, whatever runs on it"
                unit="bytes"
                series={memory.map((s, i) => ({ name: nodes[i].name, points: s.points }))}
              />
            </div>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Node</TableHead>
                  <TableHead>Pool</TableHead>
                  <TableHead className="w-36">CPU used / reserved</TableHead>
                  <TableHead className="w-36">Memory used / reserved</TableHead>
                  <TableHead>Instances</TableHead>
                  <TableHead className="text-right">Kubelet</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {nodes.map((n) => (
                  <TableRow key={n.name}>
                    <TableCell>
                      <Stack direction="horizontal" align="center" gap="condensed">
                        <StatusBadge variant="secondary" type={n.ready ? "success" : "warning"} live={!n.ready}>
                          {n.name}
                        </StatusBadge>
                        <InlineCode>{n.arch}</InlineCode>
                      </Stack>
                      <div className="mt-1 text-[11px] text-muted-foreground">{n.shape}</div>
                    </TableCell>
                    <TableCell>
                      <Badge variant="secondary">{n.pool}</Badge>
                      <div className="mt-1 max-w-52 text-[11px] leading-snug whitespace-normal text-muted-foreground">{pools[n.pool]}</div>
                    </TableCell>
                    <TableCell>
                      <Usage label={`CPU of ${n.name}`} used={n.cpu[0]} reserved={n.cpu[1]} detail={`${n.cores} cores`} />
                    </TableCell>
                    <TableCell>
                      <Usage label={`Memory of ${n.name}`} used={n.memory[0]} reserved={n.memory[1]} detail={`${n.gib} GiB`} />
                    </TableCell>
                    <TableCell className="font-mono text-xs">{n.pods}</TableCell>
                    <TableCell className="text-right font-mono text-xs text-muted-foreground">{n.kubelet}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Extensions</CardTitle>
            <CardDescription>What the platform does beyond running projects. Each takes effect at once.</CardDescription>
          </CardHeader>
          <CardContent>
            <Table variant="secondary">
              <TableBody>
                {extensions.map((e) => (
                  <TableRow key={e.name}>
                    <TableCell className="w-40">
                      <InlineCode>{e.name}</InlineCode>
                    </TableCell>
                    <TableCell className="text-muted-foreground">{e.what}</TableCell>
                    <TableCell className="text-right">
                      <Switch aria-label={e.name} size="sm" defaultChecked={e.on} />
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      </Frame>
    </Section>
  );
}
