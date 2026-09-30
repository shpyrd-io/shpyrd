"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Button } from "@shpyrd/ui/components/button";
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@shpyrd/ui/components/card";
import { InfoTable, InfoTableItem } from "@shpyrd/ui/components/info-table";
import { InlineCode } from "@shpyrd/ui/components/inline-code";
import { StatusBadge } from "@shpyrd/ui/components/status-badge";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@shpyrd/ui/components/table";
import { api } from "@/api/api";
import { ago, bytes, Failed, Loading, when } from "./shared";

// Encrypted archives of the platform's state in the provider's object
// storage, on a schedule; a button runs one now. The passphrase stays
// out of the browser.
export function Backups() {
  const queries = useQueryClient();
  const backups = useQuery({ queryKey: ["backups"], queryFn: api.backups, refetchInterval: (q) => (q.state.data?.runs.some((r) => r.status === "running") ? 5_000 : 60_000) });
  const run = useMutation({
    mutationFn: () => api.runBackup(),
    onSuccess: () => {
      toast.success("Backup started");
      queries.invalidateQueries({ queryKey: ["backups"] });
    },
    onError: (e: Error) => toast.error(e.message),
  });
  if (backups.isLoading) return <Loading />;
  if (backups.error || !backups.data) return <Failed what="the backups" error={backups.error} />;
  const b = backups.data;
  const running = b.runs.some((r) => r.status === "running");
  const last = b.runs[0];
  return (
    <>
      <Card>
        <CardHeader>
          <CardTitle>Platform backups</CardTitle>
          <CardDescription>
            Projects, config vars, resources, sources, accounts and teams, as an encrypted archive in the provider's object storage. A new cluster restores from it with <InlineCode>shpyrd cluster restore</InlineCode>. What is on volumes and in databases is not in it.
          </CardDescription>
          {b.enabled && (
            <CardAction>
              <Button size="sm" variant="outline" disabled={running || run.isPending} onClick={() => run.mutate()}>
                {running ? "Running…" : "Back up now"}
              </Button>
            </CardAction>
          )}
        </CardHeader>
        <CardContent className="grid gap-4">
          {!b.enabled ? (
            <p className="text-sm text-muted-foreground">
              Not set up. Give <InlineCode>shpyrd cluster init</InlineCode> a target: <InlineCode>--backup-target s3://bucket/prefix</InlineCode>.
            </p>
          ) : (
            <InfoTable columns={4}>
              <InfoTableItem label="Target" mono truncate>
                {b.target}
              </InfoTableItem>
              <InfoTableItem label="Schedule">
                <InlineCode>{b.schedule}</InlineCode> UTC, keeping {b.keep}
              </InfoTableItem>
              <InfoTableItem label="Last good backup">{b.lastSuccessful ? ago(b.lastSuccessful) : "none yet"}</InfoTableItem>
              <InfoTableItem label="Last run">
                {last ? (
                  <>
                    <StatusBadge type={last.status === "failed" ? "error" : last.status === "running" ? "warning" : "success"} live={last.status === "running"}>
                      {last.status}
                    </StatusBadge>
                    {last.started && <span className="ml-2 text-muted-foreground">{ago(last.started)}</span>}
                    {last.message && <div className="mt-1 text-xs text-destructive">{last.message}</div>}
                  </>
                ) : (
                  "none yet"
                )}
              </InfoTableItem>
            </InfoTable>
          )}
          {b.error && <p className="text-sm text-destructive">The target cannot be listed: {b.error}</p>}
          {b.enabled && (
            <p className="text-xs text-muted-foreground">
              The passphrase that opens these archives is printed by <InlineCode>shpyrd cluster backup key</InlineCode>; keep it outside the cluster.
            </p>
          )}
        </CardContent>
      </Card>
      {b.enabled && (
        <Card>
          <CardHeader>
            <CardTitle>Archives</CardTitle>
            <CardDescription>The last ones at the target, newest first.</CardDescription>
          </CardHeader>
          <CardContent>
            {b.archives.length === 0 ? (
              <p className="text-sm text-muted-foreground">None yet.</p>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Archive</TableHead>
                    <TableHead className="text-right">Size</TableHead>
                    <TableHead className="text-right">Made</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {b.archives.slice(0, 14).map((a) => (
                    <TableRow key={a.name}>
                      <TableCell className="font-mono text-xs">{a.name}</TableCell>
                      <TableCell className="text-right font-mono text-xs">{bytes(a.size)}</TableCell>
                      <TableCell className="text-right text-xs text-muted-foreground" title={when(a.modified)}>
                        {ago(a.modified)}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </CardContent>
        </Card>
      )}
    </>
  );
}
