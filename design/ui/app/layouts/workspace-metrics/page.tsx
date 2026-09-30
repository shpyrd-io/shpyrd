"use client";

import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@shpyrd/ui/components/card";
import { InlineCode } from "@shpyrd/ui/components/inline-code";
import { PageHeading } from "@shpyrd/ui/components/page-heading";
import { ProgressBar } from "@shpyrd/ui/components/progress-bar";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@shpyrd/ui/components/select";
import { Stack } from "@shpyrd/ui/components/stack";
import { Stat } from "@shpyrd/ui/components/stat";
import { StatusBadge } from "@shpyrd/ui/components/status-badge";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@shpyrd/ui/components/table";
import { TimeChart } from "@shpyrd/ui/components/time-chart";
import { Section } from "../../section";
import { Frame } from "../frame";
import { cpu, instances, memory, throughput, trend, trendOfErrors, trendOfRequests } from "../../samples";

// The metrics of a workspace: every project of it, together and one by
// one, and how much of the plan is taken.

const byProject = [
  { name: "Hello World", slug: "hello-world", requests: "31.4", p95: "223", errors: "0.9", cpu: 1.2, memory: 1.7, cpuOf: 2, memoryOf: 2.5 },
  { name: "Billing", slug: "billing", requests: "9.8", p95: "412", errors: "2.4", cpu: 0.9, memory: 2.9, cpuOf: 2, memoryOf: 4 },
  { name: "Docs 001", slug: "docs001", requests: "6.1", p95: "88", errors: "0.0", cpu: 0.2, memory: 0.4, cpuOf: 1, memoryOf: 1 },
  { name: "Reports", slug: "reports", requests: "0.9", p95: "1,340", errors: "0.0", cpu: 2.1, memory: 3.8, cpuOf: 3, memoryOf: 4 },
];

const ofPlan = [
  { label: "Projects", used: 5, of: 10, unit: "" },
  { label: "Instances", used: 9, of: 20, unit: "" },
  { label: "CPU", used: 4.4, of: 8, unit: " cores" },
  { label: "Memory", used: 8.8, of: 16, unit: " GiB" },
  { label: "Storage", used: 120, of: 500, unit: " GiB" },
];

// The series of the samples, named after projects.
const requestsByProject = throughput.map((s, i) => ({ name: byProject[i].name, points: s.points }));
const memoryByProject = memory.map((s, i) => ({ name: byProject[i].name, points: s.points }));
const cpuByProject = cpu.map((s, i) => ({ name: byProject[i].name, points: s.points }));
const instancesByProject = instances.map((s, i) => ({ name: byProject[i].name, points: s.points }));

export default function Page() {
  return (
    <Section title="The metrics of a workspace">
      <Frame here="workspace" crumb="Metrics">
        <PageHeading
          as="h2"
          title="Metrics"
          description="Every project of Acme together, over the last hour."
          actions={
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
          }
        />

        <div className="grid gap-x-8 gap-y-6 @xl/page-layout:grid-cols-2 @5xl/page-layout:grid-cols-4">
          <Stat
            label="Requests"
            value="48.2"
            unit="a second"
            delta={{ value: "8%", direction: "up", against: "against yesterday" }}
            trend={trendOfRequests}
            tone="blue"
          />
          <Stat
            label="Response time, 95th percentile"
            value="223"
            unit="ms"
            delta={{ value: "12%", direction: "down", good: true, against: "against yesterday" }}
            trend={trend}
          />
          <Stat
            label="Failed requests"
            value="1.4"
            unit="%"
            delta={{ value: "0.6", direction: "up", good: false, against: "against yesterday" }}
            trend={trendOfErrors}
            tone="error"
          />
          <Stat label="Instances" value={9} unit="of 20" delta={{ value: "2", direction: "up", against: "since yesterday" }} />
        </div>

        <Card>
          <CardHeader>
            <CardTitle>Of the plan</CardTitle>
            <CardDescription>
              What the workspace may use across all its projects. A change that would go over is
              refused with the number.
            </CardDescription>
            <CardAction>
              <StatusBadge type="info">Team</StatusBadge>
            </CardAction>
          </CardHeader>
          <CardContent className="grid gap-x-8 gap-y-4 @xl/page-layout:grid-cols-2 @3xl/page-layout:grid-cols-3 @5xl/page-layout:grid-cols-5">
            {ofPlan.map((row) => (
              <ProgressBar
                key={row.label}
                label={row.label}
                value={row.used}
                max={row.of}
                tone={row.used / row.of >= 0.9 ? "error" : row.used / row.of >= 0.75 ? "warning" : "orange"}
                size="sm"
                format={(v) => `${v}${row.unit}`}
              />
            ))}
          </CardContent>
        </Card>

        <div className="grid gap-6 @3xl/page-layout:grid-cols-2">
          <TimeChart
            title="Requests"
            description="A second, by project"
            unit="rps"
            arrangement="stacked"
            series={requestsByProject}
          />
          <TimeChart
            title="Instances"
            description="Running, by project"
            unit="count"
            kind="step"
            series={instancesByProject}
          />
          <TimeChart title="CPU" description="Cores, by project" unit="cores" series={cpuByProject} />
          <TimeChart title="Memory" description="By project" unit="bytes" series={memoryByProject} />
        </div>

        <Card>
          <CardHeader>
            <CardTitle>By project</CardTitle>
            <CardDescription>The same hour, one line for each.</CardDescription>
          </CardHeader>
          <CardContent>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Project</TableHead>
                  <TableHead className="text-right">Requests/s</TableHead>
                  <TableHead className="text-right">p95</TableHead>
                  <TableHead className="text-right">Failed</TableHead>
                  <TableHead>CPU</TableHead>
                  <TableHead>Memory</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {byProject.map((p) => (
                  <TableRow key={p.slug}>
                    <TableCell>
                      <span className="font-medium">{p.name}</span> <InlineCode>{p.slug}</InlineCode>
                    </TableCell>
                    <TableCell className="text-right font-mono text-xs">{p.requests}</TableCell>
                    <TableCell className="text-right font-mono text-xs">{p.p95} ms</TableCell>
                    <TableCell className="text-right font-mono text-xs">
                      <span className={Number(p.errors) >= 2 ? "text-destructive" : undefined}>{p.errors}%</span>
                    </TableCell>
                    <TableCell>
                      <Stack direction="horizontal" align="center" gap="condensed">
                        <ProgressBar inline aria-label={`CPU of ${p.name}`} value={p.cpu} max={p.cpuOf} size="sm" className="w-16" />
                        <span className="font-mono text-[11px] whitespace-nowrap text-muted-foreground">
                          {p.cpu} / {p.cpuOf}
                        </span>
                      </Stack>
                    </TableCell>
                    <TableCell>
                      <Stack direction="horizontal" align="center" gap="condensed">
                        <ProgressBar inline aria-label={`Memory of ${p.name}`} value={p.memory} max={p.memoryOf} size="sm" tone="blue" className="w-16" />
                        <span className="font-mono text-[11px] whitespace-nowrap text-muted-foreground">
                          {p.memory} / {p.memoryOf} GiB
                        </span>
                      </Stack>
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
