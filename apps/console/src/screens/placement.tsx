"use client";

import { useState } from "react";
import { useIsMutating, useQuery } from "@tanstack/react-query";
import { ProjectBackups } from "@shpyrd/shared/project-backups";
import { Button } from "@shpyrd/ui/components/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@shpyrd/ui/components/card";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@shpyrd/ui/components/table";
import { api } from "@/api/api";
import { usePerms } from "@/lib/perms";
import { ProjectPlacement } from "./project-placement";
import { Failed, Loading } from "./shared";

export function Placement() {
  const perms = usePerms();
  const busy = useIsMutating({ mutationKey: ["project-archive-operation"] }) > 0;
  const projects = useQuery({ queryKey: ["archive-projects"], queryFn: api.archiveProjects, enabled: perms.admin, refetchInterval: 5_000 });
  const [selected, setSelected] = useState("");
  if (!perms.admin) return <p className="text-sm text-muted-foreground">Cluster administrators manage project maintenance here.</p>;
  if (projects.isLoading) return <Loading />;
  if (projects.error) return <Failed what="project placement" error={projects.error} />;
  const current = projects.data?.find((p) => p.id === selected);
  return <div className="grid gap-4">
    <Card>
      <CardHeader><CardTitle>Project placement</CardTitle><CardDescription>Where each project's processes and data run. Open a project to move processes and data, download a backup, or restore it during maintenance.</CardDescription></CardHeader>
      <CardContent>
        <Table><TableHeader><TableRow><TableHead>Workspace</TableHead><TableHead>Project</TableHead><TableHead>Nodes</TableHead><TableHead>Status</TableHead><TableHead className="w-36" /></TableRow></TableHeader>
          <TableBody>{projects.data?.map((p) => <TableRow key={p.id}><TableCell>{p.workspace}</TableCell><TableCell>{p.name}</TableCell><TableCell>{p.nodes?.join(", ") || "Not running"}</TableCell><TableCell>{p.phase}</TableCell><TableCell><Button variant="outline" size="sm" disabled={busy} onClick={() => setSelected(p.id)}>Manage</Button></TableCell></TableRow>)}</TableBody>
        </Table>
        {projects.data?.length === 0 && <p className="py-4 text-sm text-muted-foreground">No projects yet.</p>}
      </CardContent>
    </Card>
    {current && <div className="grid gap-2"><p className="text-sm font-medium">{current.workspace} / {current.name}</p><ProjectPlacement key={`placement:${current.id}`} id={current.id} name={current.name} /><ProjectBackups key={current.id} name={current.name} queryKey={`cluster:${current.id}`} actions={{
      status: () => api.projectArchiveStatus(current.id), backup: () => api.backupProject(current.id), restore: (file) => api.restoreProject(current.id, file), recover: () => api.recoverProject(current.id),
    }} /></div>}
  </div>;
}
