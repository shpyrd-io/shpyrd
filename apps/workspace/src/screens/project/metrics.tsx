"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Cpu, MemoryStick } from "lucide-react";
import { Card, CardContent } from "@shpyrd/ui/components/card";
import { Meter } from "@shpyrd/ui/components/meter";
import { Stack } from "@shpyrd/ui/components/stack";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@shpyrd/ui/components/select";
import { Stat } from "@shpyrd/ui/components/stat";
import { TimeChart } from "@shpyrd/ui/components/time-chart";
import { api } from "@/api/api";
import type { Project } from "@/api/types";
import { Failed, Loading } from "./shared";

// How the project is doing: the numbers that matter, the room it takes,
// and how it went along the time.
export function Metrics({ project }: { project: Project }) {
  const [range, setRange] = useState("1h");
  const metrics = useQuery({ queryKey: ["metrics", project.slug, range], queryFn: () => api.metrics(project.slug, range), refetchInterval: 30_000 });
  if (metrics.isLoading) return <Loading rows={6} />;
  if (metrics.error || !metrics.data) return <Failed what="the metrics" error={metrics.error} />;
  const m = metrics.data;
  const last = (s?: { points: [number, number][] }) => s?.points.at(-1)?.[1] ?? 0;
  const trend = (s?: { points: [number, number][] }) => s?.points.slice(-24).map((p) => p[1]);
  const p95 = m.responseTime.find((s) => s.name.startsWith("95"));
  const ok = m.throughput.find((s) => s.name === "2xx");
  const bad = m.throughput.find((s) => s.name === "5xx");
  const requests = m.throughput.reduce((sum, s) => sum + last(s), 0);
  const sum = (series: { points: [number, number][] }[]) => series.reduce((s, x) => s + last(x), 0);
  const allocation = (series: { reference?: number; burst?: number }[]) => ({
    reserved: series.reduce((s, x) => s + (x.reference ?? 0), 0),
    capacity: series.reduce((s, x) => s + (x.burst ?? x.reference ?? 0), 0),
  });
  const cpu = allocation(m.cpu);
  const memory = allocation(m.memory);
  const GiB = 1 << 30;
  return (
    <>
      <Stack direction="horizontal" justify="end">
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
      </Stack>
      <Card>
        <CardContent>
          <div className="grid gap-x-8 gap-y-6 @xl/page-layout:grid-cols-3">
            <Stat label="Response time, 95th percentile" value={Math.round(last(p95))} unit="ms" trend={trend(p95)} />
            <Stat label="Requests" value={requests.toFixed(1)} unit="a second" trend={trend(ok)} tone="blue" />
            <Stat label="Failed requests" value={requests ? ((last(bad) / requests) * 100).toFixed(1) : "0"} unit="%" trend={trend(bad)} tone="error" />
          </div>
        </CardContent>
      </Card>
      <div className="grid gap-x-8 gap-y-6 @3xl/page-layout:grid-cols-2">
        <Meter label="CPU" unit="cores" icon={<Cpu />} used={Math.round(sum(m.cpu) * 100) / 100} reserved={Math.round(cpu.reserved * 100) / 100} capacity={Math.round(cpu.capacity * 100) / 100} />
        <Meter label="Memory" unit="GiB" icon={<MemoryStick />} used={Math.round((sum(m.memory) / GiB) * 100) / 100} reserved={Math.round((memory.reserved / GiB) * 100) / 100} capacity={Math.round((memory.capacity / GiB) * 100) / 100} />
      </div>
      <div className="grid gap-8 border-t pt-6 @5xl/page-layout:grid-cols-2">
        <TimeChart title="Response time" description="How long the requests took" unit="ms" palette="shades" series={m.responseTime} markers={m.releases} />
        <TimeChart title="Throughput" description="Requests a second, by the class of the response" unit="rps" arrangement="stacked" series={m.throughput} markers={m.releases} />
        <TimeChart title="CPU" description="By process, in cores" unit="cores" series={m.cpu} markers={m.releases} />
        <TimeChart title="Memory" description="By process" unit="bytes" series={m.memory} />
        <TimeChart title="Network" description="Bytes a second in and out, by process" unit="bytes/s" series={m.network} />
        <TimeChart title="Instances" description="Running, by process" unit="count" kind="step" series={m.instances} />
      </div>
    </>
  );
}
