"use client";

import { useState } from "react";
import { Search } from "lucide-react";
import { Input } from "@shpyrd/ui/components/input";
import { Label } from "@shpyrd/ui/components/label";
import { LogView, TextLogView, type LogLevel } from "@shpyrd/ui/components/log-view";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@shpyrd/ui/components/select";
import { Stack } from "@shpyrd/ui/components/stack";
import { Switch } from "@shpyrd/ui/components/switch";
import { Section } from "../../section";
import { build, lines } from "../../logs";

export default function Page() {
  const [filter, setFilter] = useState("");
  const [level, setLevel] = useState<LogLevel>("debug");
  const [raw, setRaw] = useState(false);
  return (
    <>
      <Section title="The lines of an application, with the controls an application would put over them">
        <Stack direction="horizontal" wrap="wrap" align="center" gap="cozy">
          <Input
            type="search"
            icon={<Search />}
            placeholder="Filter"
            value={filter}
            onChange={(event) => setFilter(event.target.value)}
            className="w-56"
          />
          <Select value={level} onValueChange={(v) => setLevel(v as LogLevel)}>
            <SelectTrigger className="w-36">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="debug">Everything</SelectItem>
              <SelectItem value="info">Info and up</SelectItem>
              <SelectItem value="warn">Warnings and up</SelectItem>
              <SelectItem value="error">Errors only</SelectItem>
            </SelectContent>
          </Select>
          <Stack direction="horizontal" align="center" gap="condensed">
            <Label htmlFor="raw">Raw</Label>
            <Switch id="raw" size="sm" checked={raw} onCheckedChange={setRaw} statusLabel={false} />
          </Stack>
        </Stack>
        <LogView lines={lines} filter={filter} level={level} raw={raw} height={360} />
      </Section>
      <Section title="Nothing yet">
        <LogView lines={[]} height={120} />
      </Section>
      <Section title="The output of a build">
        <TextLogView lines={build} height={360} />
      </Section>
    </>
  );
}
