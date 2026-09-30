"use client";

import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { Avatar } from "@shpyrd/ui/components/avatar";
import { Button } from "@shpyrd/ui/components/button";
import { Card, CardAction, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@shpyrd/ui/components/card";
import { InfoTable, InfoTableItem } from "@shpyrd/ui/components/info-table";
import { Skeleton } from "@shpyrd/ui/components/skeleton";
import { Lock, Rocket, Settings2, Trash2, Undo2, type LucideIcon } from "lucide-react";
import { api } from "@/api/api";
import type { AuditEntry, Project } from "@/api/types";
import { ago } from "@/lib/project";

// What the project is made of, as one table of names and values, and
// the last things done to it. Everything else has a page of its own at
// the side.
export function Overview({ project }: { project: Project }) {
  const { spec, status } = project;
  const current = [...status.releases].sort((a, b) => b.number - a.number)[0];
  const audit = useQuery({ queryKey: ["audit", project.slug], queryFn: () => api.audit(project.slug) });
  return (
    <div className="grid items-start gap-4 @5xl/page-layout:grid-cols-3">
      <Card className="@5xl/page-layout:col-span-2">
        <CardHeader>
          <CardTitle>Summary</CardTitle>
          <CardDescription>Where the project comes from, what runs now, and on what.</CardDescription>
          <CardAction className="flex gap-3">
            <Link to={`/projects/${project.slug}/releases`} className="text-xs text-primary underline-offset-4 hover:underline">
              All releases
            </Link>
            <Link to={`/projects/${project.slug}/resources`} className="text-xs text-primary underline-offset-4 hover:underline">
              Sizes
            </Link>
          </CardAction>
        </CardHeader>
        <CardContent>
          <InfoTable columns={3}>
            {spec.source?.git && (
              <>
                <InfoTableItem label="Git" mono truncate span={2}>
                  {spec.source.git.url}
                </InfoTableItem>
                <InfoTableItem label="Revision" mono>
                  {spec.source.git.revision ?? "main"}
                </InfoTableItem>
              </>
            )}
            {spec.source?.blob && (
              <>
                <InfoTableItem label="Archive" mono>
                  {spec.source.blob.sha256?.slice(0, 12) ?? "-"}
                </InfoTableItem>
                <InfoTableItem label="Commit" mono>
                  {spec.source.blob.ref ?? "local checkout"}
                </InfoTableItem>
              </>
            )}
            {spec.source?.subPath && (
              <InfoTableItem label="Directory" mono>
                {spec.source.subPath}
              </InfoTableItem>
            )}
            {!spec.source && !spec.pinnedDigest && <InfoTableItem label="Source">nothing deployed yet</InfoTableItem>}
            {spec.pinnedDigest && (
              <InfoTableItem label="Pinned build" mono truncate>
                {spec.pinnedDigest}
              </InfoTableItem>
            )}
            {spec.build?.env?.length ? (
              <InfoTableItem label="Build env" mono truncate>
                {spec.build.env.map((e) => `${e.name}=${e.value ?? ""}`).join(" ")}
              </InfoTableItem>
            ) : null}
            {spec.source && (
              <InfoTableItem label="Build">
                {spec.build?.strategy === "dockerfile" ? `Dockerfile (${spec.build.dockerfile ?? "Dockerfile"})` : "Buildpacks"}
              </InfoTableItem>
            )}
            <InfoTableItem label="Release">{current ? `v${current.number} · ${current.description ?? ""}` : "none yet"}</InfoTableItem>
            <InfoTableItem label="Image" mono>
              {current?.build ? `build #${current.build}` : (status.digest?.slice(0, 12) ?? "-")}
            </InfoTableItem>
            {status.processTypes?.length ? (
              <InfoTableItem label="Image types" mono>
                {status.processTypes.join(", ")}
              </InfoTableItem>
            ) : null}
            {status.processTypes?.includes("release") && <InfoTableItem label="Release phase">runs before every rollout</InfoTableItem>}
            {Object.entries(spec.processes ?? {}).map(([name, p]) => (
              <InfoTableItem key={name} label={`Process ${name}`} mono>
                {p.size ?? "shared-s"} × {p.replicas ?? 1}
              </InfoTableItem>
            ))}
            {Object.keys(spec.processes ?? {}).length === 0 && <InfoTableItem label="Process web">not set yet</InfoTableItem>}
          </InfoTable>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Activity</CardTitle>
          <CardDescription>The last thing done to the project, from the dashboard, the API or the CLI.</CardDescription>
        </CardHeader>
        <CardContent>
          {audit.isLoading ? (
            <Skeleton className="h-12 w-full" />
          ) : audit.data && audit.data.length > 0 ? (
            <LastEntry entry={audit.data[0]} />
          ) : (
            <p className="text-sm text-muted-foreground">Nothing yet.</p>
          )}
        </CardContent>
        <CardFooter>
          <Button variant="outline" className="w-full" asChild>
            <Link to={`/projects/${project.slug}/activity`}>See all activity</Link>
          </Button>
        </CardFooter>
      </Card>
    </div>
  );
}

// The mark of each action, in its colour, on the corner of the actor.
const marks: Record<string, { icon: LucideIcon; tone: string; word: string }> = {
  deploy: { icon: Rocket, tone: "bg-success text-success-foreground", word: "Deploy" },
  rollback: { icon: Undo2, tone: "bg-warning text-warning-foreground", word: "Rollback" },
  destroy: { icon: Trash2, tone: "bg-destructive text-destructive-foreground", word: "Destroy" },
  access: { icon: Lock, tone: "bg-info text-info-foreground", word: "Access" },
};
const otherMark = { icon: Settings2, tone: "bg-info text-info-foreground", word: "" };

function LastEntry({ entry }: { entry: AuditEntry }) {
  const mark = marks[entry.action] ?? otherMark;
  const Icon = mark.icon;
  const word = mark.word || entry.action.charAt(0).toUpperCase() + entry.action.slice(1);
  return (
    <div className="flex items-center gap-4">
      <span className="relative flex shrink-0">
        <Avatar alt={entry.actor} size={40} />
        <span className={`absolute -right-1 -bottom-1 flex size-5 items-center justify-center rounded-full ring-2 ring-card ${mark.tone}`}>
          <Icon className="size-3" />
        </span>
      </span>
      <div className="min-w-0 text-sm">
        <div>
          <b className="font-medium">{word}</b> <span className="text-muted-foreground break-all">by {entry.actor}</span>
        </div>
        {entry.detail && <div className="truncate">{entry.detail}</div>}
        <div className="text-xs text-muted-foreground">{ago(entry.at)}</div>
      </div>
    </div>
  );
}
