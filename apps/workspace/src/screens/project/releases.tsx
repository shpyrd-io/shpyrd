"use client";

import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Badge } from "@shpyrd/ui/components/badge";
import { Button } from "@shpyrd/ui/components/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@shpyrd/ui/components/card";
import { InlineCode } from "@shpyrd/ui/components/inline-code";
import { TextLogView } from "@shpyrd/ui/components/log-view";
import { StatusBadge } from "@shpyrd/ui/components/status-badge";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@shpyrd/ui/components/table";
import { api } from "@/api/api";
import type { Project } from "@/api/types";
import type { Perms } from "@/lib/perms";
import { ago } from "@/lib/project";
import { busy } from "./heading";
import { duration, Failed, Loading, useStream, when } from "./shared";

// What went out, and what was built to go out. A rollback releases an
// earlier one again, exactly as it was.
export function Releases({ project, perms }: { project: Project; perms: Perms }) {
  const queries = useQueryClient();
  const releases = [...project.status.releases].sort((a, b) => b.number - a.number);
  const building = project.status.phase === "Building";
  const builds = useQuery({ queryKey: ["builds", project.slug], queryFn: () => api.builds(project.slug), refetchInterval: building ? 5_000 : 30_000 });
  const [chosen, setChosen] = useState<string | null>(null);
  // The build that is open: the one chosen, else the one going on.
  const list = builds.data ?? [];
  const open = chosen ? list.find((b) => b.name === chosen) : building ? list.find((b) => b.status === "Building") : undefined;
  const follow = open?.status === "Building";
  useEffect(() => {
    if (chosen && !list.some((b) => b.name === chosen)) setChosen(null);
  }, [chosen, list]);
  const output = useStream<string>(open ? (signal, push) => api.streamBuild(project.slug, open.name, follow, signal, push) : null, [project.slug, open?.name, follow]);
  const rollback = useMutation({
    mutationFn: (n: number) => api.rollback(project.slug, n),
    onSuccess: (_, n) => {
      queries.invalidateQueries({ queryKey: ["project", project.slug] });
      toast.success(`Rolling back to v${n}`, { description: "Its build and its config vars, as they were." });
    },
    onError: (e: Error) => toast.error(e.message),
  });
  return (
    <>
      <Card>
        <CardHeader>
          <CardTitle>Releases</CardTitle>
          <CardDescription>A release is a build and its config. Deploys make new builds; config changes and rollbacks reuse them.</CardDescription>
        </CardHeader>
        <CardContent>
          {releases.length === 0 ? (
            <p className="text-sm text-muted-foreground">Nothing released yet.</p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Release</TableHead>
                  <TableHead>What changed</TableHead>
                  <TableHead>Build</TableHead>
                  <TableHead>When</TableHead>
                  <TableHead className="text-right" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {releases.map((r, i) => (
                  <TableRow key={r.number}>
                    <TableCell className="font-mono text-xs">
                      v{r.number}{" "}
                      {i === 0 && (
                        <Badge variant="secondary" className="ml-1">
                          current
                        </Badge>
                      )}
                    </TableCell>
                    <TableCell>
                      <Badge variant="outline" className="mr-2">
                        {r.kind}
                      </Badge>
                      {r.description}
                      {r.processes?.length ? <span className="ml-2 text-xs text-muted-foreground">{r.processes.join(", ")}</span> : null}
                    </TableCell>
                    <TableCell className="font-mono text-xs text-muted-foreground">{r.build ? `#${r.build}` : (r.digest?.slice(0, 12) ?? "-")}</TableCell>
                    <TableCell className="text-xs text-muted-foreground" title={when(r.createdAt)}>
                      {ago(r.createdAt)}
                    </TableCell>
                    <TableCell className="text-right">
                      <Button variant="outline" size="xs" disabled={i === 0 || !perms.deploy || rollback.isPending || busy(project)} onClick={() => rollback.mutate(r.number)}>
                        Rollback
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
      <Card>
        <CardHeader>
          <CardTitle>Builds</CardTitle>
          <CardDescription>Each deploy builds an image from the source. Open one to read what it printed; one going on is followed as it prints.</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4">
          {builds.isLoading ? (
            <Loading />
          ) : builds.error ? (
            <Failed what="the builds" error={builds.error} />
          ) : list.length === 0 ? (
            <p className="text-sm text-muted-foreground">No build yet.</p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Build</TableHead>
                  <TableHead>How</TableHead>
                  <TableHead>Source</TableHead>
                  <TableHead>Used by</TableHead>
                  <TableHead>Took</TableHead>
                  <TableHead className="text-right" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {list.map((b) => {
                  const usedBy = project.status.releases.filter((r) => String(r.build) === String(b.number)).map((r) => `v${r.number}`);
                  return (
                    <TableRow key={b.name} data-state={open?.name === b.name ? "selected" : undefined}>
                      <TableCell>
                        <StatusBadge type={b.status === "Succeeded" ? "success" : b.status === "Failed" ? "error" : "info"} live={b.status === "Building"} qty={`#${b.number}`}>
                          {b.status}
                        </StatusBadge>
                        {b.reason && b.status === "Failed" && <div className="mt-1 text-[11px] text-muted-foreground">{b.reason.toLowerCase()}</div>}
                      </TableCell>
                      <TableCell className="text-xs">{b.strategy}</TableCell>
                      <TableCell className="font-mono text-xs text-muted-foreground">{b.source ?? "-"}</TableCell>
                      <TableCell className="font-mono text-xs text-muted-foreground">{usedBy.length ? usedBy.join(", ") : "-"}</TableCell>
                      <TableCell className="text-xs text-muted-foreground" title={when(b.startedAt)}>
                        {duration(b.startedAt, b.completedAt)}
                        {!b.completedAt && ", so far"}
                        {b.message && b.status === "Failed" && <span className="text-destructive"> · {b.message}</span>}
                      </TableCell>
                      <TableCell className="text-right">
                        <Button variant="outline" size="xs" onClick={() => setChosen(open?.name === b.name ? null : b.name)}>
                          {open?.name === b.name ? "Hide" : "Output"}
                        </Button>
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          )}
          {open && (
            <div className="grid gap-2">
              <div className="flex items-center gap-2 text-xs text-muted-foreground">
                <InlineCode>{open.name}</InlineCode>
                {open.digest && <span className="ml-auto font-mono">{open.digest}</span>}
              </div>
              {output.error ? <Failed what="the output of the build" error={new Error(output.error)} /> : <TextLogView lines={output.lines} follow={follow} height={360} empty={follow ? "Waiting for the build to print…" : "Nothing was printed."} />}
            </div>
          )}
        </CardContent>
      </Card>
    </>
  );
}
