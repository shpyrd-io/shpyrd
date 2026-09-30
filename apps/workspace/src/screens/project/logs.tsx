"use client";

import { useState } from "react";
import { RefreshCw, Search } from "lucide-react";
import { Button } from "@shpyrd/ui/components/button";
import { Input } from "@shpyrd/ui/components/input";
import { Label } from "@shpyrd/ui/components/label";
import { LogView, type LogLevel, type LogLine } from "@shpyrd/ui/components/log-view";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@shpyrd/ui/components/select";
import { Stack } from "@shpyrd/ui/components/stack";
import { Switch } from "@shpyrd/ui/components/switch";
import { api } from "@/api/api";
import type { Project } from "@/api/types";
import { Failed, useStream } from "./shared";

// The lines of the project as they come, with what to keep of them.
// Live, the stream stays open and the view follows; paused, the last
// lines are read once.
export function Logs({ project }: { project: Project }) {
  const [filter, setFilter] = useState("");
  const [level, setLevel] = useState<LogLevel>("debug");
  const [process, setProcess] = useState("all");
  const [raw, setRaw] = useState(false);
  const [live, setLive] = useState(true);
  const processes = Object.keys({ ...project.spec.processes, ...project.processes }).sort();
  const logs = useStream<LogLine>((signal, push) => api.streamLogs(project.slug, { process: process === "all" ? undefined : process, tail: 300, follow: live }, signal, push), [project.slug, process, live]);
  return (
    <>
      <Stack direction="horizontal" wrap="wrap" align="center" gap="cozy">
        <Select value={process} onValueChange={setProcess}>
          <SelectTrigger className="w-40">
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
        <Select value={level} onValueChange={(v) => setLevel(v as LogLevel)}>
          <SelectTrigger className="w-40">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="debug">Everything</SelectItem>
            <SelectItem value="info">Info and up</SelectItem>
            <SelectItem value="warn">Warnings and up</SelectItem>
            <SelectItem value="error">Errors only</SelectItem>
          </SelectContent>
        </Select>
        <Input type="search" icon={<Search />} placeholder="Filter" value={filter} onChange={(e) => setFilter(e.target.value)} className="w-56" />
        <Button variant="outline" size="icon" icon={<RefreshCw />} aria-label="Read again" title="Read again" onClick={logs.restart} />
        <Stack direction="horizontal" align="center" gap="normal" className="ml-auto">
          <Stack direction="horizontal" align="center" gap="condensed">
            <Label htmlFor="live">Live</Label>
            <Switch id="live" size="sm" checked={live} onCheckedChange={setLive} statusLabel={false} />
          </Stack>
          <Stack direction="horizontal" align="center" gap="condensed">
            <Label htmlFor="raw">Raw</Label>
            <Switch id="raw" size="sm" checked={raw} onCheckedChange={setRaw} statusLabel={false} />
          </Stack>
          <span className="text-xs text-muted-foreground">{logs.lines.length} lines</span>
        </Stack>
      </Stack>
      {logs.error ? <Failed what="the logs" error={new Error(logs.error)} /> : <LogView lines={logs.lines} follow={live} filter={filter} level={level} raw={raw} height={560} empty={logs.done ? "No line yet" : "Waiting for lines…"} />}
    </>
  );
}
