"use client";

import { useState, type ComponentProps } from "react";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { Cpu, MemoryStick } from "lucide-react";
import { Card, CardContent } from "@shpyrd/ui/components/card";
import { Label } from "@shpyrd/ui/components/label";
import { Meter } from "@shpyrd/ui/components/meter";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@shpyrd/ui/components/select";
import { Stack } from "@shpyrd/ui/components/stack";
import { Stat } from "@shpyrd/ui/components/stat";
import { Switch } from "@shpyrd/ui/components/switch";
import { TimeChart, type Marker, type Reference } from "@shpyrd/ui/components/time-chart";
import { metricValue } from "@shpyrd/ui/lib/format";
import { api } from "@/api/api";
import type { MetricChart, MetricsQuery, OOMKill, Project, Series } from "@/api/types";
import { Failed, Loading } from "./shared";

type By = NonNullable<MetricsQuery["by"]>;
type Agg = NonNullable<MetricsQuery["agg"]>;
type Mode = NonNullable<MetricsQuery["mode"]>;

const groupings: Record<Agg, string> = { none: "By instance", sum: "Sum of the instances", avg: "Average instance", max: "Busiest instance" };

// How the project is doing: the numbers that matter, the room it takes,
// and how it went along the time — by process, or instance by instance,
// against what each was given (RFC-0027).
export function Metrics({ project }: { project: Project }) {
  const [range, setRange] = useState("1h");
  const [process, setProcess] = useState("all");
  const [by, setBy] = useState<By>("process");
  const [agg, setAgg] = useState<Agg>("none");
  const [mode, setMode] = useState<Mode>("total");
  const [replaced, setReplaced] = useState(false);
  const processes = Object.keys({ ...project.spec.processes, ...project.processes }).sort();
  const instances = by === "instance";
  const percent = mode === "percent";
  // "all" is the control's word for no filter: a Select cannot hold an
  // empty value. What only applies to instances is not asked otherwise.
  const query: MetricsQuery = { range, process: process === "all" ? undefined : process, by, agg: instances ? agg : "none", mode, replaced: instances && replaced };
  const metrics = useQuery({ queryKey: ["metrics", project.slug, query], queryFn: () => api.metrics(project.slug, query), refetchInterval: 30_000, placeholderData: keepPreviousData });
  const m = metrics.data;
  const last = (s?: Series) => s?.points.at(-1)?.[1] ?? 0;
  const trend = (s?: Series) => s?.points.slice(-24).map((p) => p[1]);
  // The room it takes now: what every series says at the chart's latest
  // moment, and what those series were given. An instance that was
  // replaced ended earlier, so it neither uses nor reserves anything now.
  const now = (c: MetricChart) => {
    const latest = Math.max(0, ...c.series.flatMap((s) => s.points.map((p) => p[0])));
    const alive = c.series.map((s) => ({ ...s, value: s.points.find((p) => p[0] === latest)?.[1] })).filter((s) => s.value !== undefined);
    return {
      used: alive.reduce((sum, s) => sum + (s.value ?? 0), 0),
      reserved: alive.reduce((sum, s) => sum + (s.reference ?? 0), 0),
      capacity: alive.reduce((sum, s) => sum + (s.burst ?? s.reference ?? 0), 0),
    };
  };
  const sum = (c: MetricChart) => c.series.reduce((s, x) => s + last(x), 0);
  const round = (v: number) => Math.round(v * 100) / 100;
  const GiB = 1 << 30;
  const grouping = instances ? groupings[agg] : "By process";
  // What the controls cannot reach, said on the chart itself.
  const edge = process !== "all" || instances ? "Measured at the edge, so the process and instance controls do not apply" : undefined;
  return (
    <>
      <Stack direction="horizontal" wrap="wrap" align="center" gap="cozy">
        <Select value={process} onValueChange={setProcess}>
          <SelectTrigger className="w-36">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">Every process</SelectItem>
            {processes.map((p) => (
              <SelectItem key={p} value={p}>
                {p}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select value={by} onValueChange={(v) => setBy(v as By)}>
          <SelectTrigger className="w-32">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="process">By process</SelectItem>
            <SelectItem value="instance">By instance</SelectItem>
          </SelectContent>
        </Select>
        <Select value={agg} onValueChange={(v) => setAgg(v as Agg)} disabled={!instances}>
          <SelectTrigger className="w-40">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="none">Each instance</SelectItem>
            <SelectItem value="sum">Sum of the instances</SelectItem>
            <SelectItem value="avg">Average instance</SelectItem>
            <SelectItem value="max">Busiest instance</SelectItem>
          </SelectContent>
        </Select>
        <Select value={mode} onValueChange={(v) => setMode(v as Mode)}>
          <SelectTrigger className="w-44">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="total">In cores and bytes</SelectItem>
            <SelectItem value="percent">As a share of the size</SelectItem>
          </SelectContent>
        </Select>
        <Stack direction="horizontal" align="center" gap="condensed">
          <Label htmlFor="replaced" className={instances ? undefined : "text-muted-foreground"}>
            Replaced instances
          </Label>
          <Switch id="replaced" size="sm" checked={instances && replaced} onCheckedChange={setReplaced} disabled={!instances} statusLabel={false} />
        </Stack>
        <Select value={range} onValueChange={setRange}>
          <SelectTrigger className="ml-auto w-36">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="15m">Last 15 minutes</SelectItem>
            <SelectItem value="1h">Last hour</SelectItem>
            <SelectItem value="6h">Last 6 hours</SelectItem>
            <SelectItem value="24h">Last 24 hours</SelectItem>
            <SelectItem value="7d">Last 7 days</SelectItem>
          </SelectContent>
        </Select>
      </Stack>
      {metrics.isPending ? (
        <Loading rows={6} />
      ) : metrics.error || !m ? (
        <Failed what="the metrics" error={metrics.error} />
      ) : (
        // While another question is being answered, the last answer stays,
        // a little faded, rather than the page going blank.
        <div className={metrics.isPlaceholderData ? "grid gap-6 opacity-60 transition-opacity" : "grid gap-6 transition-opacity"}>
          <Card>
            <CardContent>
              <div className="grid gap-x-8 gap-y-6 @xl/page-layout:grid-cols-3">
                <Stat label="Response time, 95th percentile" value={Math.round(last(m.responseTime.series.find((s) => s.name.includes("95"))))} unit="ms" trend={trend(m.responseTime.series.find((s) => s.name.includes("95")))} />
                <Stat label="Requests" value={sum(m.throughput).toFixed(1)} unit="a second" trend={trend(m.throughput.series.find((s) => s.name === "2xx"))} tone="blue" />
                <Stat label="Failed requests" value={sum(m.throughput) ? ((last(m.throughput.series.find((s) => s.name === "5xx")) / sum(m.throughput)) * 100).toFixed(1) : "0"} unit="%" trend={trend(m.throughput.series.find((s) => s.name === "5xx"))} tone="error" />
              </div>
            </CardContent>
          </Card>
          {/* The room it takes is a number of cores and bytes; as a share,
              the charts say it against their 100% line instead. */}
          {!percent && (
            <div className="grid gap-x-8 gap-y-6 @3xl/page-layout:grid-cols-2">
              <Meter label="CPU" unit="cores" icon={<Cpu />} used={round(now(m.cpu).used)} reserved={round(now(m.cpu).reserved)} capacity={round(now(m.cpu).capacity)} />
              <Meter label="Memory" unit="GiB" icon={<MemoryStick />} used={round(now(m.memory).used / GiB)} reserved={round(now(m.memory).reserved / GiB)} capacity={round(now(m.memory).capacity / GiB)} />
            </div>
          )}
          <div className="grid gap-8 border-t pt-6 @5xl/page-layout:grid-cols-2">
            <Chart chart={m.responseTime} title="Response time" description="How long the requests took" aside={edge} unit="ms" palette="shades" range={range} markers={m.releases} />
            <Chart chart={m.throughput} title="Throughput" description="Requests a second, by the class of the response" aside={edge} unit="rps" arrangement="stacked" range={range} markers={m.releases} />
            <Chart chart={m.cpu} title="CPU" description={`${grouping}, ${percent ? "as a share of the CPU of its size" : "in cores"}`} unit="cores" range={range} markers={m.releases} references={references(m.cpu, percent)} />
            <Chart chart={m.memory} title="Memory" description={`${grouping}, ${percent ? "as a share of the memory of its size" : "in bytes"}`} unit="bytes" range={range} markers={[...m.releases, ...m.ooms.map(oomMarker)]} references={references(m.memory, percent)} />
            <Chart chart={m.network} title="Network" description={`Bytes a second in and out, ${instances ? grouping.toLowerCase() : process === "all" ? "for the whole project" : `for ${process}`}`} unit="bytes/s" range={range} markers={m.releases} />
            <Chart chart={m.instances} title="Instances" description="Running, by process" aside={instances ? "One number per process, so the instance controls do not apply" : undefined} unit="count" kind="step" range={range} markers={m.releases} />
          </div>
        </div>
      )}
    </>
  );
}

// One chart of the page: the library's TimeChart, with what the server
// said about the chart — a query that failed, data it hid — and what the
// controls cannot reach, said under its title. The failed state repeats
// the chart's own frame; a prop for it on TimeChart is owed to the library.
function Chart({ chart, title, description, aside, unit, ...rest }: Omit<ComponentProps<typeof TimeChart>, "series" | "unit" | "description"> & { chart: MetricChart; description: string; aside?: string; unit: string }) {
  const about = [description, aside, chart.note].filter(Boolean).join(". ");
  if (chart.error) {
    return (
      <figure className="grid gap-3">
        <figcaption className="grid gap-0.5">
          <div className="text-sm leading-snug font-medium">{title}</div>
          <div className="text-xs text-muted-foreground">{about}</div>
        </figcaption>
        <div className="flex h-44 items-center justify-center rounded-lg border border-dashed border-destructive/40 px-6 text-center text-xs text-muted-foreground">{chart.error}</div>
      </figure>
    );
  }
  return <TimeChart title={title} description={about} unit={chart.unit || unit} series={chart.series} {...rest} />;
}

// An instance killed for crossing its memory, as the Memory chart marks
// it (#119): in the ink of an error, apart from the releases, and saying
// what died and at what limit to who points at it.
export function oomMarker(k: OOMKill): Marker {
  const who = k.instance ?? (k.process === "release" ? "the release step" : k.process === "build" ? "the build" : k.process);
  let title = `${who} killed: out of memory`;
  if (k.limit > 0) title += ` at its ${metricValue(k.limit, "bytes")}${k.size ? ` (${k.size})` : ""}`;
  return { time: k.time, label: "OOM", tone: "error", title };
}

// The lines CPU and memory are measured against. As a share, the size is
// 100%. In units, one line per distinct allocation among the series drawn
// — and per burst ceiling above it — named by the processes that share it
// when there is more than one.
export function references(chart: MetricChart, percent: boolean): Reference[] {
  if (percent) return chart.series.length ? [{ value: 100, label: "the size", tone: "error" }] : [];
  const lines = new Map<number, Set<string>>();
  const bursts = new Map<number, Set<string>>();
  for (const s of chart.series) {
    const who = processOf(s.name);
    if (s.reference) (lines.get(s.reference) ?? lines.set(s.reference, new Set()).get(s.reference)!).add(who);
    if (s.burst && s.burst !== s.reference) (bursts.get(s.burst) ?? bursts.set(s.burst, new Set()).get(s.burst)!).add(who);
  }
  const named = (who: Set<string>, what: string, many: boolean) => (many ? `${[...who].filter(Boolean).sort().join(", ")} ${what}` : what);
  return [
    ...[...lines].map(([value, who]): Reference => ({ value, label: named(who, "allocation", lines.size > 1), tone: "neutral" })),
    ...[...bursts].map(([value, who]): Reference => ({ value, label: named(who, "burst", bursts.size > 1), tone: "warning" })),
  ];
}

// The process a series is drawn for, to name its line only: "web",
// "web.2", "web.2 in" and "sum web" all read as web; a replaced instance
// names none. The server keeps the process off the answer on purpose
// (RFC-0011), so this is a reading of the name, not a fact of the series.
function processOf(name: string): string {
  const words = name.split(" ").filter((w) => !["in", "out", "sum", "avg", "max"].includes(w));
  const head = words[0] ?? "";
  return head === "replaced" ? "" : (head.split(".")[0] ?? "");
}
