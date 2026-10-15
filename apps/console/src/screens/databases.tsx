"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronLeft, ChevronRight, Layers, Search } from "lucide-react";
import { Button } from "@shpyrd/ui/components/button";
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@shpyrd/ui/components/card";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@shpyrd/ui/components/dialog";
import { InlineCode } from "@shpyrd/ui/components/inline-code";
import { Input } from "@shpyrd/ui/components/input";
import { ProgressBar } from "@shpyrd/ui/components/progress-bar";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@shpyrd/ui/components/select";
import { Stack } from "@shpyrd/ui/components/stack";
import { Stat } from "@shpyrd/ui/components/stat";
import { StatusBadge } from "@shpyrd/ui/components/status-badge";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@shpyrd/ui/components/table";
import type { PostgresServer, PostgresTenant, PostgresTenantQuery } from "@/api/types";
import { api } from "@/api/api";
import { ago, bytes, Failed, Loading } from "./shared";

// The databases of the platform (storage plan, step 3), for a fleet of
// thousands: what needs attention across every server, a database found
// by whose it is or by its hashed name, the servers as a dense table that
// sorts and filters, and each server's databases a page at a time. The
// console only reads here: a pooler, a drop date, a cap are the
// platform's or the owner's to change.

const gib = (b: number) => Math.round((b / (1 << 30)) * 10) / 10;
const percent = (part: number, whole: number) => (whole > 0 ? Math.round((part / whole) * 100) : 0);
const stateWords: Record<PostgresTenant["state"], string> = { "": "Ready", near: "Near cap", over: "Over cap", paused: "Paused" };
const stateType = (state: PostgresTenant["state"]) => (state === "paused" ? "error" : state ? "warning" : "success");

// A number over its capacity with a thin bar beside it, as a table cell
// holds it.
function Share({ words, pct, tone }: { words: string; pct: number; tone: "orange" | "blue" | "warning" | "error" | "neutral" }) {
  return <ProgressBar inline size="sm" label={words} value={Math.min(pct, 100)} tone={tone} className="w-auto [&>span:last-child]:w-12" />;
}

// A database's row: whose, its hashed name, its cap, its copies, its state.
function TenantRows({ tenants, server = false }: { tenants: PostgresTenant[]; server?: boolean }) {
  return (
    <>
      {tenants.map((t) => {
        const pct = percent(t.usedBytes, t.capBytes);
        return (
          <TableRow key={`${t.server}/${t.database}`}>
            <TableCell className="text-xs">
              {t.workspace}
              <span className="text-muted-foreground"> / {t.project}</span>
            </TableCell>
            <TableCell>
              <InlineCode>{t.name}</InlineCode>
              <div className="font-mono text-[11px] text-muted-foreground">{t.database}</div>
            </TableCell>
            {server && (
              <TableCell>
                <InlineCode>{t.server}</InlineCode>
              </TableCell>
            )}
            <TableCell>
              <Share words={`${bytes(t.usedBytes)} of ${bytes(t.capBytes)}`} pct={pct} tone={pct >= 150 ? "error" : pct >= 80 ? "warning" : "orange"} />
            </TableCell>
            <TableCell className="text-xs">{t.keep}</TableCell>
            <TableCell>
              <StatusBadge type={stateType(t.state)}>{stateWords[t.state]}</StatusBadge>
            </TableCell>
          </TableRow>
        );
      })}
    </>
  );
}

function TenantHead({ server = false }: { server?: boolean }) {
  return (
    <TableHeader>
      <TableRow>
        <TableHead>Workspace / project</TableHead>
        <TableHead>Database</TableHead>
        {server && <TableHead>Server</TableHead>}
        <TableHead>Cap</TableHead>
        <TableHead>Copies</TableHead>
        <TableHead>State</TableHead>
      </TableRow>
    </TableHeader>
  );
}

// A database found across every server, by workspace, project, name or
// hashed name: an operator with a customer's question starts here.
function Finder() {
  const [typed, setTyped] = useState("");
  const [q, setQ] = useState("");
  const found = useQuery({ queryKey: ["postgres-find", q], queryFn: () => api.findPostgresTenants(q), enabled: q.trim().length >= 2 });
  return (
    <Card>
      <CardHeader>
        <CardTitle>Find a database</CardTitle>
        <CardDescription>By its workspace, project or name, or by the hashed name a server knows it by. Every server is searched.</CardDescription>
      </CardHeader>
      <CardContent className="grid gap-4">
        <form
          className="flex gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            setQ(typed);
          }}
        >
          <Input value={typed} onChange={(e) => setTyped(e.target.value)} placeholder="acme / shop, db_3488978c…" className="max-w-sm" aria-label="Find a database" />
          <Button type="submit" variant="outline" icon={<Search />} disabled={typed.trim().length < 2}>
            Find
          </Button>
        </form>
        {found.isLoading ? (
          <Loading rows={2} />
        ) : found.error ? (
          <Failed what="the search" error={found.error} />
        ) : found.data ? (
          found.data.length === 0 ? (
            <p className="text-sm text-muted-foreground">No database matches “{q}”.</p>
          ) : (
            <>
              <Table>
                <TenantHead server />
                <TableBody>
                  <TenantRows tenants={found.data} server />
                </TableBody>
              </Table>
              {found.data.length >= 50 && <p className="text-xs text-muted-foreground">The first 50 matches; narrow the search for the rest.</p>}
            </>
          )
        ) : null}
      </CardContent>
    </Card>
  );
}

// A server's databases, a page at a time, narrowed by state or by a word.
function Tenants({ server, onClose }: { server: PostgresServer; onClose: () => void }) {
  const [page, setPage] = useState(1);
  const [state, setState] = useState<PostgresTenantQuery["state"]>("");
  const [typed, setTyped] = useState("");
  const [q, setQ] = useState("");
  const query: PostgresTenantQuery = { page, size: 50, state, q };
  const tenants = useQuery({ queryKey: ["postgres-tenants", server.name, query], queryFn: () => api.postgresTenants(server.name, query), placeholderData: (prev) => prev });
  const pages = tenants.data ? Math.max(1, Math.ceil(tenants.data.total / tenants.data.size)) : 1;
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-4xl">
        <DialogHeader divider>
          <DialogTitle>Databases on {server.name}</DialogTitle>
          <DialogDescription>
            {server.tenants} of {server.limit}; {server.kept} kept behind a drop date{server.converting ? `, ${server.converting} converting` : ""}. The hashed names are what the server knows them by.
          </DialogDescription>
        </DialogHeader>
        <Stack direction="horizontal" gap="tight" wrap="wrap">
          <Select
            value={state || "all"}
            onValueChange={(v) => {
              setState(v === "all" ? "" : (v as PostgresTenantQuery["state"]));
              setPage(1);
            }}
          >
            <SelectTrigger className="w-44" aria-label="State">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {[
                ["all", "Every state"],
                ["paused", "Connections paused"],
                ["over", "Over their cap"],
                ["near", "Near their cap"],
              ].map(([v, w]) => (
                <SelectItem key={v} value={v}>
                  {w}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <form
            className="flex gap-2"
            onSubmit={(e) => {
              e.preventDefault();
              setQ(typed);
              setPage(1);
            }}
          >
            <Input value={typed} onChange={(e) => setTyped(e.target.value)} placeholder="workspace, project, name…" className="w-56" aria-label="Narrow the list" />
            <Button type="submit" variant="outline" size="icon" icon={<Search />} aria-label="Narrow the list" />
          </form>
        </Stack>
        {tenants.isLoading ? (
          <Loading />
        ) : tenants.error || !tenants.data ? (
          <Failed what="the databases" error={tenants.error} />
        ) : (
          <>
            <div className="max-h-[55vh] overflow-auto">
              <Table>
                <TenantHead />
                <TableBody>
                  <TenantRows tenants={tenants.data.items} />
                </TableBody>
              </Table>
            </div>
            <Stack direction="horizontal" justify="space-between" align="center" className="text-xs text-muted-foreground">
              <span>
                {tenants.data.total} database{tenants.data.total === 1 ? "" : "s"}
                {state || q ? " match" : ""} · page {page} of {pages}
              </span>
              <Stack direction="horizontal" gap="tight">
                <Button variant="outline" size="icon-xs" icon={<ChevronLeft />} aria-label="Previous page" disabled={page <= 1} onClick={() => setPage(page - 1)} />
                <Button variant="outline" size="icon-xs" icon={<ChevronRight />} aria-label="Next page" disabled={page >= pages} onClick={() => setPage(page + 1)} />
              </Stack>
            </Stack>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}

export type Filter = "all" | "open" | "full" | "unhealthy" | "dumps";
export type Sort = "name" | "fill" | "disk" | "connections";

const filters: [Filter, string][] = [
  ["all", "Every server"],
  ["open", "Open to new databases"],
  ["full", "Full"],
  ["unhealthy", "Not healthy"],
  ["dumps", "Dumps failed"],
];
const sorts: [Sort, string][] = [
  ["name", "By name"],
  ["fill", "Fullest first"],
  ["disk", "Disk fullest first"],
  ["connections", "Busiest first"],
];

const healthy = (s: PostgresServer) => /healthy/i.test(s.phase);

// The servers the table shows: those the filter keeps, whose name has the
// typed part, in the order asked. By name is by the server's number.
export function arrange(all: PostgresServer[], filter: Filter, sort: Sort, name: string): PostgresServer[] {
  const kept = all.filter((s) => {
    if (name && !s.name.includes(name.trim())) return false;
    switch (filter) {
      case "open":
        return s.open;
      case "full":
        return s.tenants >= s.limit;
      case "unhealthy":
        return !healthy(s) || s.ready < s.instances;
      case "dumps":
        return (s.lastDumps?.failed ?? 0) > 0 || !s.lastDumps;
      default:
        return true;
    }
  });
  const by: Record<Sort, (a: PostgresServer, b: PostgresServer) => number> = {
    name: (a, b) => (Number(a.name.replace(/\D/g, "")) || 0) - (Number(b.name.replace(/\D/g, "")) || 0),
    fill: (a, b) => b.tenants / b.limit - a.tenants / a.limit,
    disk: (a, b) => b.disk.usedBytes / b.disk.sizeBytes - a.disk.usedBytes / a.disk.sizeBytes,
    connections: (a, b) => b.connections.p95 / b.connections.max - a.connections.p95 / a.connections.max,
  };
  return [...kept].sort(by[sort]);
}

export function Databases() {
  const servers = useQuery({ queryKey: ["postgres-servers"], queryFn: api.postgresServers, refetchInterval: 30_000 });
  const [open, setOpen] = useState<PostgresServer | null>(null);
  const [filter, setFilter] = useState<Filter>("all");
  const [sort, setSort] = useState<Sort>("name");
  const [name, setName] = useState("");
  const rows = useMemo(() => arrange(servers.data?.servers ?? [], filter, sort, name), [servers.data, filter, sort, name]);
  if (servers.isLoading) return <Loading />;
  if (servers.error || !servers.data) return <Failed what="the database servers" error={servers.error} />;
  const r = servers.data;
  const shared = r.servers.reduce((n, s) => n + s.tenants, 0);
  const openServers = r.servers.filter((s) => s.open);
  const room = openServers.reduce((n, s) => n + s.limit - s.tenants, 0);
  const a = r.attention;
  const attention = a.paused + a.over + a.degraded + a.dumpsFailed;
  return (
    <>
      <div className="grid gap-4 @2xl/page-layout:grid-cols-2 @5xl/page-layout:grid-cols-4">
        <Card size="sm">
          <CardContent>
            <Stat label="Shared databases" value={shared.toLocaleString("en")} unit={`on ${r.servers.length} servers`} />
          </CardContent>
        </Card>
        <Card size="sm">
          <CardContent>
            <Stat label="Room for new ones" value={room.toLocaleString("en")} unit={`${openServers.length} server${openServers.length === 1 ? "" : "s"} open`} />
          </CardContent>
        </Card>
        <Card size="sm">
          <CardContent>
            <Stat label="Needs attention" value={attention} unit={attention ? [a.paused && `${a.paused} paused`, a.over && `${a.over} over cap`, a.degraded && `${a.degraded} server${a.degraded === 1 ? "" : "s"} not healthy`, a.dumpsFailed && `dumps failed on ${a.dumpsFailed}`].filter(Boolean).join(" · ") : "nothing"} tone={attention ? "warning" : undefined} />
          </CardContent>
        </Card>
        <Card size="sm">
          <CardContent>
            <Stat label="Dedicated clusters" value={r.dedicated.clusters} unit={`${r.dedicated.ready} ready · ${r.dedicated.backedUp} backed up · ${gib(r.dedicated.diskBytes)} GiB`} />
          </CardContent>
        </Card>
      </div>
      <Finder />
      <Card>
        <CardHeader>
          <CardTitle>Shared servers</CardTitle>
          <CardDescription>
            Each holds up to 200 databases of the workspaces, two instances on the data pool. A server grows its disk at 70 % and gets a pooler when its connections run high; the next server opens when one fills.
          </CardDescription>
          {r.measuredAt && <CardAction className="text-xs text-muted-foreground">measured {ago(r.measuredAt)}</CardAction>}
        </CardHeader>
        <CardContent className="grid gap-4">
          <Stack direction="horizontal" gap="tight" wrap="wrap">
            <Select value={filter} onValueChange={(v) => setFilter(v as Filter)}>
              <SelectTrigger className="w-52" aria-label="Which servers">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {filters.map(([v, w]) => (
                  <SelectItem key={v} value={v}>
                    {w}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Select value={sort} onValueChange={(v) => setSort(v as Sort)}>
              <SelectTrigger className="w-44" aria-label="Order">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {sorts.map(([v, w]) => (
                  <SelectItem key={v} value={v}>
                    {w}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="pg-shared-7" className="w-36" aria-label="A server by name" />
            <span className="self-center text-xs text-muted-foreground">
              {rows.length} of {r.servers.length}
            </span>
          </Stack>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Server</TableHead>
                <TableHead>State</TableHead>
                <TableHead>Databases</TableHead>
                <TableHead>Disk</TableHead>
                <TableHead>Connections</TableHead>
                <TableHead className="text-right" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.map((s) => {
                const fill = percent(s.tenants, s.limit);
                const disk = percent(s.disk.usedBytes, s.disk.sizeBytes);
                const conn = percent(s.connections.p95, s.connections.max);
                const coming = !healthy(s) && s.ready < s.instances;
                const dumps = s.lastDumps;
                return (
                  <TableRow key={s.name}>
                    <TableCell>
                      <InlineCode>{s.name}</InlineCode>
                    </TableCell>
                    <TableCell>
                      <StatusBadge type={healthy(s) ? "success" : coming ? "warning" : "error"} live={coming}>
                        {healthy(s) ? "Healthy" : coming ? "Coming up" : "Degraded"}
                      </StatusBadge>
                      <div className="mt-0.5 max-w-56 text-[11px] whitespace-normal text-muted-foreground">
                        {s.ready}/{s.instances} · {s.archiving === "ok" ? `backup ${s.lastBaseBackup ? ago(s.lastBaseBackup) : "none"}` : `archiving ${s.archiving}`} ·{" "}
                        <span className={dumps && dumps.failed > 0 ? "text-destructive" : undefined}>{dumps ? `dumps ${ago(dumps.at)}${dumps.failed ? `, ${dumps.failed} failed` : ""}` : "no dumps yet"}</span>
                      </div>
                    </TableCell>
                    <TableCell>
                      <Share words={`${s.tenants}/${s.limit}`} pct={fill} tone={fill >= 100 ? "neutral" : fill >= 90 ? "warning" : "orange"} />
                      {(s.kept > 0 || s.converting > 0 || !s.open) && (
                        <div className="mt-0.5 text-[11px] whitespace-nowrap text-muted-foreground">
                          {[!s.open && (s.tenants >= s.limit ? "full" : "closed"), s.kept > 0 && `${s.kept} kept`, s.converting > 0 && `${s.converting} converting`].filter(Boolean).join(" · ")}
                        </div>
                      )}
                    </TableCell>
                    <TableCell>
                      <Share words={`${gib(s.disk.usedBytes)}/${gib(s.disk.sizeBytes)} GiB`} pct={disk} tone={disk >= 85 ? "error" : disk >= 70 ? "warning" : "orange"} />
                      {disk >= 70 && <div className="mt-0.5 text-[11px] whitespace-nowrap text-muted-foreground">{disk >= 85 ? "past 85 %" : "growing"}</div>}
                    </TableCell>
                    <TableCell>
                      <Share words={`${s.connections.p95}/${s.connections.max}`} pct={conn} tone={conn >= 60 ? "warning" : "blue"} />
                      <div className="mt-0.5 text-[11px] whitespace-nowrap text-muted-foreground">{s.pooler.on ? (s.pooler.ready ? "pooler on" : "pooler coming up") : conn >= 60 ? "pooler due" : "no pooler"}</div>
                    </TableCell>
                    <TableCell className="text-right">
                      <Button variant="outline" size="icon-xs" icon={<Layers />} aria-label={`The databases on ${s.name}`} title="Its databases" onClick={() => setOpen(s)} />
                    </TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        </CardContent>
      </Card>
      {open && <Tenants server={open} onClose={() => setOpen(null)} />}
    </>
  );
}
