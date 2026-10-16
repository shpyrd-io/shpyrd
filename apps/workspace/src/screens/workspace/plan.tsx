"use client";

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Alert, AlertDescription, AlertTitle } from "@shpyrd/ui/components/alert";
import { Button } from "@shpyrd/ui/components/button";
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@shpyrd/ui/components/card";
import { Field } from "@shpyrd/ui/components/field";
import { InlineCode } from "@shpyrd/ui/components/inline-code";
import { Input } from "@shpyrd/ui/components/input";
import { ProgressBar } from "@shpyrd/ui/components/progress-bar";
import { Skeleton } from "@shpyrd/ui/components/skeleton";
import { api } from "@/api/api";
import type { ProjectCap, WorkspaceInfo } from "@/api/types";
import { usePerms } from "@/lib/perms";

// How much of its plan the workspace takes, and the cap each project may
// have below it: a page of Advanced, for whoever minds the numbers.
export function Plan() {
  const perms = usePerms();
  const ws = useQuery({ queryKey: ["workspace"], queryFn: api.workspace });
  if (ws.isLoading) return <Skeleton className="h-64 w-full" />;
  if (ws.error || !ws.data)
    return (
      <Alert variant="destructive">
        <AlertTitle>Could not load the workspace</AlertTitle>
        <AlertDescription>{(ws.error as Error)?.message}</AlertDescription>
      </Alert>
    );
  const w = ws.data;
  const l = w.limits;
  const u = w.usage;
  const plan = [
    { label: "Projects", used: u?.projects ?? 0, of: l?.projects, unit: "" },
    { label: "Instances", used: u?.instances ?? 0, of: l?.instances, unit: "" },
    {
      label: "CPU",
      used: Number(u?.cpu ?? 0),
      of: Number(l?.cpu),
      unit: " cores",
    },
    {
      label: "Memory",
      used: gib(u?.memory) ?? 0,
      of: gib(l?.memory),
      unit: " GB",
    },
    // The three storage ceilings (storage plan step 4): block storage in
    // provisioned capacity, the buckets together, the databases.
    {
      label: "Block storage",
      used: gib(u?.storage) ?? 0,
      of: l?.allowDisks === false ? undefined : gib(l?.storage),
      unit: " GB",
    },
    {
      label: "Object storage",
      used: gib(u?.objectStorage) ?? 0,
      of: gib(l?.objectStorage?.total),
      unit: " GB",
    },
    {
      label: "Buckets",
      used: u?.buckets ?? 0,
      of: l?.objectStorage?.buckets,
      unit: "",
    },
    {
      label: "Databases",
      used: u?.databases ?? 0,
      of: l?.postgres?.databases,
      unit: "",
    },
  ].filter((row) => row.of && row.of > 0);
  // What the plan includes, said rather than counted.
  const kinds = l?.postgres?.types?.length
    ? l.postgres.types
    : (["shared", "dedicated"] as const);
  const included = [
    l?.allowDisks === false
      ? "Disks are not included; files go in a bucket."
      : null,
    l?.objectStorage?.bucketCap
      ? `A bucket holds up to ${gbWords(l.objectStorage.bucketCap)}.`
      : null,
    kinds.length === 1
      ? kinds[0] === "shared"
        ? `Shared databases, each up to ${gbWords(l?.postgres?.sharedCap ?? "1Gi")}; a dedicated one comes with a paid plan.`
        : "Dedicated databases, each a cluster of its own."
      : `Shared and dedicated databases, ${l?.postgres?.defaultType ?? "dedicated"} by default; a shared one holds up to ${gbWords(l?.postgres?.sharedCap ?? "1Gi")}.`,
    l?.postgres?.maxInstances
      ? `A dedicated database runs up to ${l.postgres.maxInstances} instances.`
      : null,
  ].filter(Boolean) as string[];
  const over = plan.filter((row) => row.of && row.used > row.of);

  return (
    <>
      {(plan.length > 0 || l) && (
        <Card>
          <CardHeader>
            <CardTitle>Plan</CardTitle>
            <CardDescription>
              What this workspace may use across all its projects. A change that
              would go over is refused with the number; what is already over
              keeps working.
            </CardDescription>
            <CardAction>
              <InlineCode>
                {w.owners.length} {w.owners.length === 1 ? "owner" : "owners"}
              </InlineCode>
            </CardAction>
          </CardHeader>
          <CardContent className="grid gap-x-8 gap-y-4 @xl/page-layout:grid-cols-2 @5xl/page-layout:grid-cols-4">
            {plan.map((row) => (
              <ProgressBar
                key={row.label}
                label={
                  row.used > (row.of ?? 0)
                    ? `${row.label} · ${Math.round((row.used / (row.of ?? 1)) * 100)}%`
                    : row.label
                }
                value={Math.min(row.used, row.of ?? row.used)}
                max={row.of}
                size="sm"
                tone={
                  row.used / (row.of ?? 1) >= 0.9
                    ? "error"
                    : row.used / (row.of ?? 1) >= 0.75
                      ? "warning"
                      : "orange"
                }
                format={(v) =>
                  `${row.used > (row.of ?? 0) && v === row.of ? row.used : v}${row.unit}`
                }
              />
            ))}
            {included.length > 0 && (
              <p className="col-span-full text-xs text-muted-foreground">
                {included.join(" ")}
              </p>
            )}
            {over.length > 0 && (
              <p className="col-span-full text-xs text-muted-foreground">
                Over the line on{" "}
                {over.map((r) => r.label.toLowerCase()).join(", ")}: nothing
                more fits there until something is freed, and everything that
                exists keeps working.
              </p>
            )}
          </CardContent>
        </Card>
      )}
      {l?.storage && perms.admin && <ProjectCaps workspace={w} />}
    </>
  );
}

// A cap per project (storage plan step 4): the customer's own choice
// below the workspace's ceiling, kept when the plan changes. Each
// project's namespace quota mirrors it; the project page reads its usage
// against it. Empty is no cap: the project shares the workspace's.
function ProjectCaps({ workspace }: { workspace: WorkspaceInfo }) {
  const queries = useQueryClient();
  const projects = useQuery({
    queryKey: ["projects"],
    queryFn: api.projects,
    staleTime: 60_000,
  });
  const capsOf = (caps: Record<string, ProjectCap> | undefined) =>
    Object.fromEntries(
      Object.entries(caps ?? {}).map(([slug, c]) => [slug, c.storage ?? ""]),
    );
  const [caps, setCaps] = useState<Record<string, string>>(() =>
    capsOf(workspace.projectCaps),
  );
  const save = useMutation({
    mutationFn: () =>
      api.putProjectCaps(
        Object.fromEntries(
          Object.entries(caps).map(([slug, storage]) => [
            slug,
            { storage: storage.trim() },
          ]),
        ),
      ),
    onSuccess: () => {
      toast.success("The projects' caps were saved");
      queries.invalidateQueries({ queryKey: ["workspace"] });
    },
    onError: (e: Error) => toast.error(e.message),
  });
  const was = capsOf(workspace.projectCaps);
  const dirty = (projects.data ?? []).some(
    (p) => (caps[p.slug] ?? "") !== (was[p.slug] ?? ""),
  );
  return (
    <Card>
      <CardHeader>
        <CardTitle>Caps per project</CardTitle>
        <CardDescription>
          A project with a cap claims no more block storage than it says, below
          the workspace's {gbWords(workspace.limits?.storage ?? "0")}. Without
          one, a project shares the workspace's ceiling with the others.
        </CardDescription>
      </CardHeader>
      <CardContent>
        <div className="grid gap-x-8 gap-y-3 @3xl/page-layout:grid-cols-2">
          {(projects.data ?? []).map((p) => (
            <Field
              key={p.slug}
              label={p.displayName}
              hint={caps[p.slug] ? undefined : "No cap of its own."}
            >
              <Input
                value={caps[p.slug] ?? ""}
                onChange={(e) => setCaps({ ...caps, [p.slug]: e.target.value })}
                placeholder="20Gi"
                className="font-mono text-xs"
              />
            </Field>
          ))}
          <div className="col-span-full">
            <Button
              size="sm"
              disabled={!dirty || save.isPending}
              onClick={() => save.mutate()}
            >
              Save
            </Button>
          </div>
        </div>
      </CardContent>
    </Card>
  );
}

// "5Gi" as "5 GB", "512Mi" as "512 MB": the plan's words.
function gbWords(q: string) {
  const g = gib(q);
  if (g === undefined) return q;
  if (g >= 1) return `${Number.isInteger(g) ? g : g.toFixed(1)} GB`;
  return `${Math.round(g * 1024)} MB`;
}

// "8.8Gi" as a number of GiB; a number as it is.
function gib(value?: string | number): number | undefined {
  if (value === undefined) return undefined;
  const text = String(value);
  const n = parseFloat(text);
  if (Number.isNaN(n)) return undefined;
  if (text.endsWith("Mi")) return n / 1024;
  if (text.endsWith("Ti")) return n * 1024;
  return n;
}
