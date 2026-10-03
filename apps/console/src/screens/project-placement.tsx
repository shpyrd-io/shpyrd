"use client";

import { useRef, useState } from "react";
import { useIsMutating, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { Alert, AlertDescription, AlertTitle } from "@shpyrd/ui/components/alert";
import { Badge } from "@shpyrd/ui/components/badge";
import { Button } from "@shpyrd/ui/components/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@shpyrd/ui/components/card";
import { ConfirmDialog } from "@shpyrd/ui/components/confirm-dialog";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@shpyrd/ui/components/select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@shpyrd/ui/components/table";
import { api } from "@/api/api";
import { groupTitle, placementCandidates, type PlacementOrder } from "./placement-capacity";
import { bytes, Failed, Loading } from "./shared";

const cores = (value: number | undefined) => value == null ? "Unknown" : `${Number((value / 1000).toFixed(3))} ${value === 1000 ? "core" : "cores"}`;
const size = (value: number | undefined) => value == null ? "Unknown" : value < 0 ? `−${bytes(-value)}` : bytes(value);

export function ProjectPlacement({ id, name }: { id: string; name: string }) {
  const queries = useQueryClient();
  const placement = useQuery({ queryKey: ["project-placement", id], queryFn: () => api.projectPlacement(id), refetchInterval: 5_000 });
  const status = useQuery({ queryKey: ["project-archive", `cluster:${id}`], queryFn: () => api.projectArchiveStatus(id), refetchInterval: 2_000 });
  const mutationKey = ["project-archive-operation", `cluster:${id}`];
  const pending = useIsMutating({ mutationKey }) > 0;
  const submitting = useRef(false);
  const [groupID, setGroupID] = useState("");
  const [target, setTarget] = useState("");
  const [order, setOrder] = useState<PlacementOrder>("balanced");
  const [message, setMessage] = useState("");
  const preview = status.data?.preview === true;
  const measure = useMutation({ mutationKey, mutationFn: (group: string) => api.measureProjectPlacement(id, group) });
  const move = useMutation({
    mutationKey,
    mutationFn: (body: { group: string; node: string; migrateToLocal?: boolean }) => api.moveProject(id, body),
    onSuccess: (_, body) => { setTarget(""); measure.reset(); setMessage(preview ? "Preview finished. The example placement changed; no real data was moved." : body.migrateToLocal ? "Migration completed. The project is serving requests from local storage. Review the retained provider disks below." : "Movement completed. The project is serving requests again."); },
    onSettled: () => { submitting.current = false; void queries.invalidateQueries(); },
  });
  const remove = useMutation({ mutationKey, mutationFn: (volume: string) => api.deleteRetainedVolume(id, volume), onSettled: () => { void queries.invalidateQueries(); } });
  const disabled = pending || status.isLoading || !!status.error || status.data?.phase !== "idle";
  if (placement.isLoading) return <Loading />;
  if (placement.error || !placement.data) return <Failed what="placement groups" error={placement.error} />;
  const { groups, nodes } = placement.data;
  const original = groups.find((group) => group.id === groupID) ?? groups[0];
  const measured = original && measure.data?.group === original.id ? measure.data : undefined;
  const measurementFresh = measured && Date.now() - Date.parse(measured.measuredAt) < 5 * 60_000;
  const group = original && measurementFresh ? { ...original, diskUsedBytes: measured.diskUsedBytes } : original;
  const candidates = group ? placementCandidates(group, nodes, order) : [];
  const recommended = group ? placementCandidates(group, nodes, "balanced").find((candidate) => !candidate.reason && candidate.known) : undefined;
  const selected = candidates.find((candidate) => candidate.node.name === target && !candidate.reason);
  const migrating = group?.needsMigration === true;
  const title = group ? groupTitle(group) : "";
  return <Card>
    <CardHeader><CardTitle>Plan a move</CardTitle><CardDescription>Compare the space your workload needs with the capacity left on each destination. Processes sharing a volume move together; databases move separately.</CardDescription></CardHeader>
    <CardContent className="grid gap-5">
      {group ? <>
        <div className="grid gap-2">
          <label htmlFor={`move-group-${id}`} className="text-sm font-medium">What to move</label>
          <Select value={group.id} onValueChange={(value) => { setGroupID(value); setTarget(""); measure.reset(); }} disabled={disabled}>
            <SelectTrigger id={`move-group-${id}`} className="w-full sm:max-w-lg"><SelectValue /></SelectTrigger>
            <SelectContent>{groups.map((item) => <SelectItem key={item.id} value={item.id}>{groupTitle(item)}</SelectItem>)}</SelectContent>
          </Select>
          <p className="text-sm text-muted-foreground">Currently on {group.nodes.join(", ") || "no node"} · {group.pool || "shared"} pool{group.volumes.length > 0 ? ` · Volumes: ${group.volumes.join(", ")}` : ""}</p>
          <p className="text-xs text-muted-foreground">A manual move pins this group to the selected node. Estimates cover current replicas; future scaling needs additional headroom.</p>
        </div>
        {migrating && <Alert><AlertTitle>Migrate provider storage to local disk</AlertTitle><AlertDescription>Currently using {group.storageClasses?.join(", ") || "provider storage"}. Files and mount paths are preserved. Shared volumes will share one node. Old provider disks remain retained and billable until you delete them below after checking the project.</AlertDescription></Alert>}
        <div className="grid gap-4 rounded-lg border p-4 sm:grid-cols-3">
          <div><p className="text-sm text-muted-foreground">CPU to move</p><p className="text-lg font-medium">{cores(group.cpuRequestedMillicores)}</p><p className="text-xs text-muted-foreground">Reserved across current pods</p></div>
          <div><p className="text-sm text-muted-foreground">Memory to move</p><p className="text-lg font-medium">{size(group.memoryRequestedBytes)}</p><p className="text-xs text-muted-foreground">Reserved across current pods</p></div>
          <div><p className="text-sm text-muted-foreground">Data to copy · estimate</p><p className="text-lg font-medium">{size(group.diskUsedBytes)}</p><p className="text-xs text-muted-foreground">{group.diskUsedBytes === 0 && !group.database && !group.volumes.length ? "No persistent data" : measured ? `Measured ${new Date(measured.measuredAt).toLocaleTimeString()}${measurementFresh ? "" : " · expired"}` : group.diskUsedBytes == null ? "Measure if volume metrics are unavailable" : "Reported by volume metrics"}</p></div>
        </div>
        {(group.database || group.volumes.length > 0) && <div className="flex flex-wrap items-center gap-3">
          <Button variant="outline" size="sm" disabled={disabled} icon={measure.isPending ? <Loader2 className="animate-spin" aria-hidden="true" /> : undefined} onClick={() => measure.mutate(group.id)}>{measure.isPending ? "Measuring…" : preview ? "Simulate measurement" : "Measure data"}</Button>
          <p className="text-xs text-muted-foreground">Read-only scan; the project stays online. File sizes can change while it runs.</p>
        </div>}
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div><p className="text-sm font-medium">Capacity left after this move</p><p className="text-xs text-muted-foreground">Recommendation balances CPU and memory headroom. Disk moves need at least 1 GiB spare.</p></div>
          <Select value={order} onValueChange={(value) => setOrder(value as PlacementOrder)} disabled={disabled}>
            <SelectTrigger aria-label="Sort destinations" className="w-52"><SelectValue /></SelectTrigger>
            <SelectContent><SelectItem value="balanced">Balanced headroom</SelectItem><SelectItem value="cpu">Most free CPU</SelectItem><SelectItem value="memory">Most free memory</SelectItem><SelectItem value="disk">Most free disk</SelectItem></SelectContent>
          </Select>
        </div>
        <Table><TableHeader><TableRow><TableHead>Destination · {group.pool || "shared"} pool</TableHead><TableHead>CPU left</TableHead><TableHead>Memory left</TableHead><TableHead>Disk left</TableHead><TableHead /></TableRow></TableHeader>
          <TableBody>{candidates.map((candidate) => {
            const { node, cpu, memory, disk, reason, current, known } = candidate;
            return <TableRow key={node.name} className={selected?.node.name === node.name ? "bg-muted" : undefined}>
              <TableCell><div className="flex flex-wrap items-center gap-2"><span className="font-medium">{node.name}</span>{recommended?.node.name === node.name && <Badge variant="secondary">Recommended</Badge>}</div>{reason ? <p className="text-xs text-muted-foreground">{reason}</p> : !known && <p className="text-xs text-muted-foreground">Incomplete metrics · capacity unverified</p>}</TableCell>
              <TableCell>{current && !migrating ? "—" : cores(cpu)}<p className="text-xs text-muted-foreground">{cores(node.cpuMillicores)} total</p></TableCell>
              <TableCell>{current && !migrating ? "—" : size(memory)}<p className="text-xs text-muted-foreground">{bytes(node.memoryBytes)} total</p></TableCell>
              <TableCell>{current && !migrating ? "—" : size(disk)}<p className="text-xs text-muted-foreground">{size(node.diskAvailableBytes)} free now</p></TableCell>
              <TableCell><Button variant="outline" size="sm" aria-label={`Select ${node.name}`} aria-pressed={selected?.node.name === node.name} disabled={disabled || !!reason} onClick={() => setTarget(node.name)}>{selected?.node.name === node.name ? "Selected" : "Select"}</Button></TableCell>
            </TableRow>;
          })}</TableBody>
        </Table>
        {!candidates.some((candidate) => !candidate.reason) && <p className="text-sm text-muted-foreground">No eligible destination in this pool. Add capacity or free a node before moving this group.</p>}
        <div className="flex flex-wrap items-center justify-between gap-4 rounded-lg border p-4">
          <div className="max-w-2xl"><p className="text-sm font-medium">{selected ? `${title} → ${selected.node.name}` : "Select a destination to review the move"}</p><p className="text-sm text-muted-foreground">The entire project pauses during the move. HTTP requests receive 503 with Retry-After until verification succeeds. Capacity is checked again before copying.</p></div>
          <ConfirmDialog title={preview ? `Preview ${migrating ? "migrating" : "moving"} ${title}?` : `Pause ${name} and ${migrating ? "migrate" : "move"} ${title}?`}
            description={preview ? "Simulate maintenance and update the example placement. No real project or data will change." : `Move ${cores(group.cpuRequestedMillicores)}, ${size(group.memoryRequestedBytes)} memory and approximately ${size(group.diskUsedBytes)} of data to ${selected?.node.name ?? "the selected node"}. All processes in ${name} will stop, including those on other nodes, until data verification completes. ${selected && !selected.known ? "Some capacity metrics are unavailable; the estimate is incomplete. " : ""}This can take several minutes. ${migrating ? "The old provider disks will be retained for separate deletion; they stop receiving writes when the project resumes." : ""}`}
            action={preview ? (migrating ? "Simulate migration" : "Simulate move") : migrating ? "Pause and migrate" : "Pause and move"} onConfirm={() => {
              if (disabled || submitting.current || !selected) return;
              submitting.current = true; setMessage(""); move.mutate({ group: group.id, node: selected.node.name, migrateToLocal: migrating });
            }} trigger={<Button disabled={disabled || !selected} icon={move.isPending ? <Loader2 className="animate-spin" aria-hidden="true" /> : undefined}>{move.isPending ? (migrating ? "Migrating…" : "Moving…") : preview ? (migrating ? "Preview migration" : "Preview move") : migrating ? "Pause and migrate" : "Pause and move"}</Button>} />
        </div>
      </> : <p className="text-sm text-muted-foreground">No processes, volumes or databases to move.</p>}
      {!!placement.data.retainedVolumes?.length && <div className="grid gap-3 border-t pt-5">
        <div><p className="font-medium">Retained provider disks</p><p className="text-sm text-muted-foreground">These copies stopped receiving writes at migration. Check the running project before deleting them permanently. Provider charges continue until deletion completes.</p></div>
        {placement.data.retainedVolumes.map((volume) => <div key={volume.name} className="flex flex-wrap items-center justify-between gap-3 rounded-lg border p-3">
          <div className="min-w-0"><p className="break-all text-sm font-medium">{volume.claim} · {volume.capacity}</p><p className="break-all text-xs text-muted-foreground">{volume.name} · {volume.storageClass}</p></div>
          <ConfirmDialog variant="destructive" title={`Delete old disk for ${volume.claim}?`} description="Permanently delete this retained provider disk. It contains the files from before migration, not any writes since then. The project's current local volume will remain in use." action={preview ? "Simulate deletion" : "Delete old disk"} onConfirm={() => { if (!disabled && !volume.deleting) remove.mutate(volume.name); }} trigger={<Button variant="destructive" size="sm" disabled={disabled || volume.deleting} icon={remove.isPending && remove.variables === volume.name ? <Loader2 className="animate-spin" aria-hidden="true" /> : undefined}>{volume.deleting ? "Deleting…" : preview ? "Preview deletion" : "Delete old disk"}</Button>} />
        </div>)}
      </div>}
      {remove.error && <Alert variant="destructive"><AlertTitle>Could not delete retained disk</AlertTitle><AlertDescription>{remove.error.message}</AlertDescription></Alert>}
      {message && <p role="status" className="text-sm">{message}</p>}
      {measure.error && <Alert variant="destructive"><AlertTitle>Could not measure data</AlertTitle><AlertDescription>{measure.error.message}</AlertDescription></Alert>}
      {move.error && <Alert variant="destructive"><AlertTitle>Movement did not finish</AlertTitle><AlertDescription>{move.error.message} Check the maintenance status below before retrying.</AlertDescription></Alert>}
    </CardContent>
  </Card>;
}
