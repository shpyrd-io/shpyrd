"use client";

import { useQuery } from "@tanstack/react-query";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@shpyrd/ui/components/card";
import { InlineCode } from "@shpyrd/ui/components/inline-code";
import { StatusBadge } from "@shpyrd/ui/components/status-badge";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@shpyrd/ui/components/table";
import { api } from "@/api/api";
import { ago, Failed, Loading } from "./shared";

// What the cluster is made of: the extensions switched on, the
// components the installer put in place, and the charts under them.
export function Components() {
  const cluster = useQuery({ queryKey: ["cluster"], queryFn: api.cluster, refetchInterval: 60_000 });
  const helm = useQuery({ queryKey: ["helm"], queryFn: api.helmReleases, refetchInterval: 60_000 });
  if (cluster.isLoading) return <Loading rows={6} />;
  if (cluster.error || !cluster.data) return <Failed what="the cluster" error={cluster.error} />;
  const c = cluster.data;
  return (
    <>
      <Card>
        <CardHeader>
          <CardTitle>Extensions</CardTitle>
          <CardDescription>
            Optional capabilities compiled into shpyrd and switched on per cluster with <InlineCode>shpyrd extensions enable &lt;name&gt;</InlineCode>.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Extension</TableHead>
                <TableHead>State</TableHead>
                <TableHead>Component</TableHead>
                <TableHead>What it adds</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {(c.extensions ?? []).map((x) => (
                <TableRow key={x.name}>
                  <TableCell className="font-medium">{x.name}</TableCell>
                  <TableCell>
                    <StatusBadge type={x.enabled ? "success" : "neutral"}>{x.enabled ? "on" : "off"}</StatusBadge>
                  </TableCell>
                  <TableCell className="font-mono text-xs text-muted-foreground">{x.component || "-"}</TableCell>
                  <TableCell className="text-xs text-muted-foreground">{x.description}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </CardContent>
      </Card>
      <div className="grid gap-4 @5xl/page-layout:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle>Components</CardTitle>
            <CardDescription>Put in place by shpyrd cluster init: {c.components.length}.</CardDescription>
          </CardHeader>
          <CardContent>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Component</TableHead>
                  <TableHead>Version</TableHead>
                  <TableHead className="text-right">Applied</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {c.components.map((comp) => (
                  <TableRow key={comp.name}>
                    <TableCell className="font-medium">{comp.name}</TableCell>
                    <TableCell className="font-mono text-xs text-muted-foreground">{comp.version || "-"}</TableCell>
                    <TableCell className="text-right text-xs text-muted-foreground">{ago(comp.appliedAt)}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle>Helm releases</CardTitle>
            <CardDescription>The charts installed in the cluster.</CardDescription>
          </CardHeader>
          <CardContent>
            {helm.isLoading ? (
              <Loading />
            ) : helm.error ? (
              <Failed what="the Helm releases" error={helm.error} />
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Release</TableHead>
                    <TableHead>Chart</TableHead>
                    <TableHead>State</TableHead>
                    <TableHead className="text-right">Updated</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {(helm.data ?? []).map((r) => (
                    <TableRow key={`${r.namespace}/${r.name}`}>
                      <TableCell className="font-medium">
                        {r.name}
                        <div className="text-[11px] text-muted-foreground">{r.namespace}</div>
                      </TableCell>
                      <TableCell className="font-mono text-xs">
                        {r.chart}-{r.chartVersion}
                      </TableCell>
                      <TableCell>
                        <StatusBadge type={r.status === "deployed" ? "success" : "error"}>{r.status}</StatusBadge>
                      </TableCell>
                      <TableCell className="text-right text-xs text-muted-foreground">{ago(r.updatedAt)}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </CardContent>
        </Card>
      </div>
    </>
  );
}
