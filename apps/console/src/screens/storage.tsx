"use client";

import { useQuery } from "@tanstack/react-query";
import { Database } from "lucide-react";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@shpyrd/ui/components/card";
import { InlineCode } from "@shpyrd/ui/components/inline-code";
import { Meter } from "@shpyrd/ui/components/meter";
import { StatusBadge } from "@shpyrd/ui/components/status-badge";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@shpyrd/ui/components/table";
import { api } from "@/api/api";
import { bytes, Failed, Loading } from "./shared";

// The platform's object store: the room on its volume, and the buckets
// the extensions asked for, each with a credential that opens only it.
export function Storage() {
  const storage = useQuery({ queryKey: ["object-storage"], queryFn: api.objectStorage, refetchInterval: 60_000 });
  if (storage.isLoading) return <Loading />;
  if (storage.error || !storage.data) return <Failed what="the object storage" error={storage.error} />;
  const r = storage.data;
  const gib = (b: number) => Math.round((b / (1 << 30)) * 10) / 10;
  return (
    <>
      <Card>
        <CardHeader>
          <CardTitle>Object storage</CardTitle>
          <CardDescription>S3-compatible storage for platform files and backups. Each consumer gets an isolated bucket and credential.</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4">
          <p className="text-sm">
            Endpoint: <InlineCode>{r.endpoint}</InlineCode>
          </p>
          {r.totalBytes > 0 ? <Meter label="Volume" unit="GiB" icon={<Database />} used={gib(r.usedBytes)} capacity={gib(r.totalBytes)} /> : <p className="text-sm text-muted-foreground">{r.message || (r.measuredAt ? `${bytes(r.usedBytes)} stored${r.backend === "gateway" ? " in cloud object storage" : ""}.` : "Measuring.")}</p>}
        </CardContent>
      </Card>
      <Card>
        <CardHeader>
          <CardTitle>Buckets</CardTitle>
          <CardDescription>What the extensions keep here, and how much of it.</CardDescription>
        </CardHeader>
        <CardContent>
          {r.buckets.length === 0 ? (
            <p className="text-sm text-muted-foreground">None yet. They appear when an extension needs storage.</p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Bucket</TableHead>
                  <TableHead>Whose</TableHead>
                  <TableHead className="text-right">Used</TableHead>
                  <TableHead className="text-right">Objects</TableHead>
                  <TableHead>Kept</TableHead>
                  <TableHead>State</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {r.buckets.map((b) => (
                  <TableRow key={`${b.namespace}/${b.name}`}>
                    <TableCell className="font-mono text-xs">{b.bucket || "-"}</TableCell>
                    <TableCell className="text-xs">
                      {b.namespace}/{b.name}
                    </TableCell>
                    <TableCell className="text-right font-mono text-xs">{bytes(b.usedBytes)}</TableCell>
                    <TableCell className="text-right font-mono text-xs">{b.objects}</TableCell>
                    <TableCell className="text-xs">{b.retentionDays ? `${b.retentionDays} days` : "for good"}</TableCell>
                    <TableCell>
                      <StatusBadge type={b.phase === "Ready" ? "success" : b.phase === "Failed" ? "error" : "warning"} live={b.phase === "Pending"}>
                        {b.phase}
                      </StatusBadge>
                      {b.message && <div className="mt-1 text-[11px] text-muted-foreground">{b.message}</div>}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </>
  );
}
