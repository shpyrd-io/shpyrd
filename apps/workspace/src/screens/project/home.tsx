"use client";

import { Link } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowRight, Database, Globe, KeyRound, Rocket, Settings2, ShieldCheck, Undo2 } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@shpyrd/ui/components/button";
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@shpyrd/ui/components/card";
import { ConfirmDialog } from "@shpyrd/ui/components/confirm-dialog";
import { LogView, type LogLine } from "@shpyrd/ui/components/log-view";
import { Skeleton } from "@shpyrd/ui/components/skeleton";
import { Stat } from "@shpyrd/ui/components/stat";
import { StatusBadge } from "@shpyrd/ui/components/status-badge";
import { Timeline, TimelineItem } from "@shpyrd/ui/components/timeline";
import { api } from "@/api/api";
import type { Project, Release } from "@/api/types";
import type { Perms } from "@/lib/perms";
import { ago } from "@/lib/project";
import { busy } from "./heading";
import { Essential, Essentials } from "@/shell/essential";
import { useStream } from "./shared";

// The project for someone who builds with an agent: is it up, what went
// out last and how to take it back, what it is saying, and the four
// things people change. The rest is Advanced, one link away.
export function Home({ project, perms }: { project: Project; perms: Perms }) {
  const base = `/projects/${project.slug}`;
  const releases = [...project.status.releases].sort((a, b) => b.number - a.number);
  return (
    <>
      <Health project={project} />
      <div className="grid gap-4 @3xl/page-layout:grid-cols-2">
        <Releases project={project} releases={releases} perms={perms} />
        <Tail project={project} />
      </div>
      <Settings project={project} />
      <Card size="sm" variant="dashed" asChild>
        <Link to={`${base}/advanced`}>
          <CardContent className="flex items-center gap-3">
            <div className="min-w-0 flex-1">
              <div className="font-medium">Advanced</div>
              <div className="text-muted-foreground">Shell, sizes and scaling, backups, log drains, roles, connections and the full history.</div>
            </div>
            <ArrowRight className="size-4 shrink-0 text-muted-foreground" />
          </CardContent>
        </Link>
      </Card>
    </>
  );
}

// How it is doing in the last day, in three numbers anyone reads.
function Health({ project }: { project: Project }) {
  const metrics = useQuery({ queryKey: ["metrics", project.slug, "24h"], queryFn: () => api.metrics(project.slug, "24h"), refetchInterval: 60_000 });
  const m = metrics.data;
  const last = (s?: { points: [number, number][] }) => s?.points.at(-1)?.[1] ?? 0;
  const trend = (s?: { points: [number, number][] }) => s?.points.slice(-24).map((p) => p[1]);
  const p95 = m?.responseTime.find((s) => s.name.startsWith("95"));
  const ok = m?.throughput.find((s) => s.name === "2xx");
  const bad = m?.throughput.find((s) => s.name === "5xx");
  const requests = m?.throughput.reduce((sum, s) => sum + last(s), 0) ?? 0;
  const errors = requests ? (last(bad) / requests) * 100 : 0;
  return (
    <Card>
      <CardContent>
        {metrics.isLoading ? (
          <Skeleton className="h-14 w-full" />
        ) : (
          // A line between the three, so each trend reads as its own number's.
          <div className="grid gap-4 @xl/page-layout:grid-cols-3 @xl/page-layout:gap-0 @xl/page-layout:divide-x">
            <Stat className={cell} label="Visits a minute" value={Math.round(requests * 60)} trend={trend(ok)} tone="blue" />
            <Stat className={cell} label="Speed" value={Math.round(last(p95))} unit="ms" trend={trend(p95)} />
            <Stat className={cell} label="Errors" value={errors.toFixed(1)} unit="%" trend={trend(bad)} tone="error" />
          </div>
        )}
      </CardContent>
    </Card>
  );
}

// Each number a cell; the trend right after the number, not at the far
// edge. Stat has no prop for that yet: owed to design/ui.
const cell = "@xl/page-layout:px-6 @xl/page-layout:first:pl-0 @xl/page-layout:last:pr-0 [&>div:nth-child(2)]:justify-start [&>div:nth-child(2)]:gap-5";

const kinds: Record<Release["kind"], string> = { deploy: "New version", config: "Settings changed", rollback: "Rolled back" };
const kindIcons: Record<Release["kind"], React.ReactElement> = { deploy: <Rocket />, config: <Settings2 />, rollback: <Undo2 /> };

// The last few releases, newest first. Each older one can be put back.
function Releases({ project, releases, perms }: { project: Project; releases: Release[]; perms: Perms }) {
  const queries = useQueryClient();
  const rollback = useMutation({
    mutationFn: (n: number) => api.rollback(project.slug, n),
    // The button stays busy until the list shows the new release.
    onSuccess: async (_, n) => {
      await queries.refetchQueries({ queryKey: ["project", project.slug] });
      toast.success(`Back to v${n}`);
    },
    onError: (e: Error) => toast.error(e.message),
  });
  const current = releases[0]?.number;
  return (
    <Card>
      <CardHeader>
        <CardTitle>Releases</CardTitle>
        {busy(project) && perms.deploy && <CardDescription>A new release is going out. Going back waits until it is done.</CardDescription>}
        <CardAction>
          <Link to={`/projects/${project.slug}/releases`} className="text-xs text-primary underline-offset-4 hover:underline">
            All releases
          </Link>
        </CardAction>
      </CardHeader>
      <CardContent>
        {releases.length === 0 ? (
          <p className="text-muted-foreground">Nothing deployed yet. Ask your agent to deploy it.</p>
        ) : (
          <Timeline clip>
            {releases.slice(0, 4).map((r) => (
              <TimelineItem
                key={r.number}
                condensed
                className="items-center"
                icon={kindIcons[r.kind]}
                actions={
                  r.number === current ? (
                    <StatusBadge type="success">Live</StatusBadge>
                  ) : (
                    perms.deploy && (
                      <ConfirmDialog
                        trigger={
                          <Button variant="outline" size="xs" icon={<Undo2 />} disabled={busy(project) || rollback.isPending}>
                            Go back
                          </Button>
                        }
                        title={`Go back to v${r.number}?`}
                        description="That version takes over again, with the settings it had. Nothing is lost: you can come back to the newer one the same way."
                        action={`Go back to v${r.number}`}
                        onConfirm={() => rollback.mutate(r.number)}
                      />
                    )
                  )
                }
              >
                <strong>{r.description ?? kinds[r.kind]}</strong>
                <div className="text-xs">
                  v{r.number} · {kinds[r.kind]} · {ago(r.createdAt)}
                </div>
              </TimelineItem>
            ))}
          </Timeline>
        )}
      </CardContent>
    </Card>
  );
}

// The last lines it printed, as they come.
function Tail({ project }: { project: Project }) {
  const logs = useStream<LogLine>((signal, push) => api.streamLogs(project.slug, { tail: 40, follow: true }, signal, push), [project.slug]);
  return (
    <Card>
      <CardHeader>
        <CardTitle>Logs</CardTitle>
        <CardAction>
          <Link to={`/projects/${project.slug}/logs`} className="text-xs text-primary underline-offset-4 hover:underline">
            All logs
          </Link>
        </CardAction>
      </CardHeader>
      <CardContent>
        <LogView lines={logs.lines} height={236} empty={logs.done ? "Nothing printed yet" : "Waiting for lines…"} />
      </CardContent>
    </Card>
  );
}

const accessWords = { public: "Anyone with the link", authenticated: "People who sign in", identified: "Only the people you name" } as const;

// The four things people change, each with how it is now.
function Settings({ project }: { project: Project }) {
  const base = `/projects/${project.slug}`;
  const config = useQuery({ queryKey: ["config-vars", project.slug], queryFn: () => api.configVars(project.slug) });
  const domains = useQuery({ queryKey: ["domains", project.slug], queryFn: () => api.domains(project.slug) });
  const secrets = (config.data?.vars.length ?? 0) + (config.data?.variables?.length ?? 0);
  const own = domains.data?.domains[0]?.host;
  const bindings = project.spec.bindings ?? [];
  return (
    <Essentials>
      <Essential to={`${base}/config`} icon={KeyRound} title="Secrets and settings" now={config.isLoading ? "…" : secrets ? `${secrets} set` : "None yet"} />
      <Essential to={`${base}/domains`} icon={Globe} title="Domain" now={domains.isLoading ? "…" : (own ?? "Add your own")} />
      <Essential to={`${base}/resources`} icon={Database} title="Database" now={bindings.length ? bindings.map((b) => b.name).join(", ") : "Add one"} />
      <Essential to={`${base}/access`} icon={ShieldCheck} title="Who can open it" now={accessWords[project.access] ?? project.access} />
    </Essentials>
  );
}
