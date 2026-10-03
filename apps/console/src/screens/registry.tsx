"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Package } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@shpyrd/ui/components/button";
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@shpyrd/ui/components/card";
import { InfoTable, InfoTableItem } from "@shpyrd/ui/components/info-table";
import { InlineCode } from "@shpyrd/ui/components/inline-code";
import { Meter } from "@shpyrd/ui/components/meter";
import { StatusBadge } from "@shpyrd/ui/components/status-badge";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@shpyrd/ui/components/table";
import { api } from "@/api/api";
import { ago, bytes, Failed, Loading, when } from "./shared";

// Where the built images live: whether it answers, how full it is,
// what it holds, and when space is reclaimed.
export function Registry() {
  const queries = useQueryClient();
  const registry = useQuery({ queryKey: ["registry"], queryFn: api.registry, refetchInterval: (q) => (q.state.data?.gc?.running ? 5_000 : 60_000) });
  const gc = useMutation({
    mutationFn: () => api.registryGC(),
    onSuccess: () => {
      toast.success("Collecting", { description: "The registry is read-only until it finishes: pulls work, builds wait." });
      queries.invalidateQueries({ queryKey: ["registry"] });
    },
    onError: (e: Error) => toast.error(e.message),
  });
  if (registry.isLoading) return <Loading />;
  if (registry.error || !registry.data) return <Failed what="the registry" error={registry.error} />;
  const r = registry.data;
  const used = r.storage?.usedBytes ?? 0;
  const cap = r.storage?.capacityBytes ?? 0;
  const gib = (b: number) => Math.round((b / (1 << 30)) * 10) / 10;
  return (
    <>
      <Card>
        <CardHeader>
          <CardTitle>Registry</CardTitle>
          <CardDescription>Builds push to it and nodes pull from it. When it is down, builds and new instances on nodes without the image wait.</CardDescription>
          {r.mode === "in-cluster" && (
            <CardAction>
              <Button size="sm" variant="outline" disabled={gc.isPending || r.gc?.running} onClick={() => gc.mutate()} title="Reclaims the space of removed images. The registry is read-only for a few minutes: pulls work, builds wait.">
                {r.gc?.running ? "Collecting…" : "Collect now"}
              </Button>
            </CardAction>
          )}
        </CardHeader>
        <CardContent className="grid gap-6">
          <InfoTable columns={3}>
            <InfoTableItem label="Mode">
              {r.mode === "in-cluster" ? "In the cluster" : "External"} <InlineCode>{r.host}</InlineCode>
              {r.tls && <span className="text-muted-foreground"> · TLS from the platform's CA</span>}
            </InfoTableItem>
            <InfoTableItem label="Health">
              <StatusBadge type={r.ready ? "success" : "error"}>{r.ready ? "ready" : "not ready"}</StatusBadge>
              {r.message && <span className="ml-2 text-muted-foreground">{r.message}</span>}
            </InfoTableItem>
            {r.certificate && (
              <InfoTableItem label="Certificate">
                From {r.certificate.issuer}, until {new Date(r.certificate.notAfter).toLocaleDateString("en-GB")}; renewed by itself
              </InfoTableItem>
            )}
            {r.images && (
              <InfoTableItem label="Images">
                {r.images.error && r.images.repositories === 0 ? <span className="text-muted-foreground">not available</span> : `${r.images.repositories} repositories · ${r.images.tags} tags`}
              </InfoTableItem>
            )}
            {r.gc && (
              <InfoTableItem label="Garbage collection" span={2}>
                {r.gc.schedule ? (
                  <>
                    <InlineCode>{r.gc.schedule}</InlineCode> UTC{r.gc.nextRun && <span className="text-muted-foreground">, next {when(r.gc.nextRun)}</span>}
                  </>
                ) : (
                  <span className="text-muted-foreground">no schedule</span>
                )}
                <div className="text-xs text-muted-foreground">{r.gc.running ? `running since ${r.gc.startedAt ? when(r.gc.startedAt) : "now"}` : r.gc.lastRun ? `last ${ago(r.gc.lastRun)}: ${r.gc.lastResult === "ok" ? `${bytes(r.gc.reclaimedBytes)} reclaimed in ${r.gc.lastDuration}` : r.gc.lastResult}` : "never run"}</div>
              </InfoTableItem>
            )}
          </InfoTable>
          {r.storage &&
            (r.storage.backend === "s3" ? (
              <div className="text-sm">
                <p>Images in <InlineCode>s3://{r.storage.bucket}/docker/</InlineCode></p>
                {r.storage.endpoint && <p className="text-xs text-muted-foreground">{r.storage.endpoint}</p>}
                <p className="text-muted-foreground">{r.storage.error ? "Storage usage is unavailable." : `${bytes(used)} stored`}</p>
              </div>
            ) : cap > 0 ? (
              <Meter label="Storage" unit="GiB" icon={<Package />} used={gib(used)} capacity={gib(cap)} />
            ) : (
              <p className="text-sm text-muted-foreground">{r.storage.size} asked for; the use needs the monitoring component.</p>
            ))}
          {cap > 0 && used / cap >= 0.8 && (
            <p className="text-xs text-muted-foreground">
              Nearly full. Grow it with <InlineCode>shpyrd cluster init --set SHPYRD_REGISTRY_SIZE=&lt;size&gt;</InlineCode>.
            </p>
          )}
        </CardContent>
      </Card>
      {r.images && r.images.largest.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle>The largest repositories</CardTitle>
            <CardDescription>By the number of tags they hold.</CardDescription>
          </CardHeader>
          <CardContent>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Repository</TableHead>
                  <TableHead className="text-right">Tags</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {r.images.largest.map((x) => (
                  <TableRow key={x.name}>
                    <TableCell className="font-mono text-xs">{x.name}</TableCell>
                    <TableCell className="text-right font-mono text-xs">{x.tags}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      )}
    </>
  );
}
