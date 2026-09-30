"use client";

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Camera, Link2, Minus, Plus, Trash2, Undo2, Unlink2 } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@shpyrd/ui/components/button";
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@shpyrd/ui/components/card";
import { ConfirmDialog } from "@shpyrd/ui/components/confirm-dialog";
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from "@shpyrd/ui/components/dialog";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@shpyrd/ui/components/dropdown-menu";
import { Field } from "@shpyrd/ui/components/field";
import { InlineCode } from "@shpyrd/ui/components/inline-code";
import { Input } from "@shpyrd/ui/components/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@shpyrd/ui/components/select";
import { Stack } from "@shpyrd/ui/components/stack";
import { StatusBadge } from "@shpyrd/ui/components/status-badge";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@shpyrd/ui/components/table";
import { api } from "@/api/api";
import type { Project, ResourceInfo, SnapshotInfo, VolumeInfo } from "@/api/types";
import type { Perms } from "@/lib/perms";
import { ago } from "@/lib/project";
import { busy } from "./heading";
import { Failed, Loading } from "./shared";

// What the project runs on: the size and count of each process, and
// the things beside it, a database, a cache, a volume.
export function Resources({ project, perms }: { project: Project; perms: Perms }) {
  return (
    <>
      <Processes project={project} perms={perms} />
      <Beside project={project} perms={perms} />
    </>
  );
}

type Draft = { size: string; replicas: number };

// The size and how many of each process, changed together and applied
// as one release.
function Processes({ project, perms }: { project: Project; perms: Perms }) {
  const queries = useQueryClient();
  const catalog = useQuery({ queryKey: ["sizes"], queryFn: api.sizes, staleTime: 60_000 });
  const names = Object.keys({ ...(project.spec.processes ?? { web: {} }), ...project.processes }).sort();
  const current = (name: string): Draft => ({
    size: project.spec.processes?.[name]?.size || project.processes?.[name]?.size || catalog.data?.default || "",
    replicas: project.spec.processes?.[name]?.replicas ?? project.processes?.[name]?.desired ?? 1,
  });
  const [draft, setDraft] = useState<Record<string, Draft>>({});
  const value = (name: string) => draft[name] ?? current(name);
  const changes = Object.fromEntries(
    names
      .map((name) => {
        const was = current(name);
        const is = value(name);
        const change: { size?: string; replicas?: number } = {};
        if (is.size && is.size !== was.size && is.size !== "custom") change.size = is.size;
        if (is.replicas !== was.replicas) change.replicas = is.replicas;
        return [name, change] as const;
      })
      .filter(([, change]) => Object.keys(change).length > 0),
  );
  const dirty = Object.keys(changes).length > 0;
  const waiting = busy(project);
  const apply = useMutation({
    mutationFn: () => api.applyProcesses(project.slug, changes),
    onSuccess: () => {
      queries.invalidateQueries({ queryKey: ["project", project.slug] });
      setDraft({});
      toast.success("Applying", { description: Object.values(changes).some((c) => c.size) ? "One release rolls out with the new sizes." : "The instances follow." });
    },
    onError: (e: Error) => toast.error(e.message),
  });
  return (
    <Card>
      <CardHeader>
        <CardTitle>Processes</CardTitle>
        <CardDescription>The size and how many instances of each. Changes are applied together: one release, one rollout.</CardDescription>
        {perms.resource && dirty && (
          <CardAction>
            <Stack direction="horizontal" gap="condensed">
              <Button size="sm" variant="outline" onClick={() => setDraft({})} disabled={apply.isPending}>
                Cancel
              </Button>
              <Button size="sm" disabled={apply.isPending || waiting} title={waiting ? "Wait for the current release to finish" : undefined} onClick={() => apply.mutate()}>
                Apply {Object.keys(changes).length}
              </Button>
            </Stack>
          </CardAction>
        )}
      </CardHeader>
      <CardContent>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Process</TableHead>
              <TableHead>Command</TableHead>
              <TableHead>Size</TableHead>
              <TableHead>Instances</TableHead>
              <TableHead>Now</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {names.map((name) => {
              const spec = project.spec.processes?.[name];
              const st = project.processes?.[name];
              const is = value(name);
              const was = current(name);
              const asleep = st?.sleep?.state === "asleep" || st?.sleep?.state === "sleeping";
              const changed = is.size !== was.size || is.replicas !== was.replicas;
              const pinned = st?.pinned;
              return (
                <TableRow key={name} data-state={changed ? "selected" : undefined}>
                  <TableCell className="font-medium">
                    {name}
                    {spec?.port && <span className="ml-1 font-mono text-[10px] text-muted-foreground">:{spec.port}</span>}
                  </TableCell>
                  <TableCell className="font-mono text-xs text-muted-foreground">{spec?.command ?? "from the image"}</TableCell>
                  <TableCell>
                    <Select value={is.size} disabled={!perms.resource || waiting || !catalog.data} onValueChange={(size) => setDraft({ ...draft, [name]: { ...is, size } })}>
                      <SelectTrigger size="sm" className="w-52" data-changed={is.size !== was.size || undefined}>
                        <SelectValue placeholder="size" />
                      </SelectTrigger>
                      <SelectContent>
                        {(catalog.data?.sizes ?? []).map((s) => (
                          <SelectItem key={s.name} value={s.name}>
                            <span className="font-mono text-xs">{s.name}</span>
                            <span className="ml-2 text-xs text-muted-foreground">
                              {s.cpu} CPU · {s.memory}
                            </span>
                          </SelectItem>
                        ))}
                        {was.size === "custom" && <SelectItem value="custom">custom</SelectItem>}
                      </SelectContent>
                    </Select>
                    {st?.cpu && st?.memory && (
                      <div className="mt-1 font-mono text-[11px] text-muted-foreground">
                        {st.cpu} CPU · {st.memory}
                      </div>
                    )}
                  </TableCell>
                  <TableCell>
                    <Stack direction="horizontal" align="center" gap="tight">
                      <Button variant="outline" size="icon-xs" icon={<Minus />} aria-label="One less" disabled={!perms.resource || waiting || is.replicas <= 0} onClick={() => setDraft({ ...draft, [name]: { ...is, replicas: is.replicas - 1 } })} />
                      <span className={`w-6 text-center font-mono text-xs ${is.replicas !== was.replicas ? "text-primary" : ""}`}>{is.replicas}</span>
                      <Button
                        variant="outline"
                        size="icon-xs"
                        icon={<Plus />}
                        aria-label="One more"
                        disabled={!perms.resource || waiting || (!!pinned && is.replicas >= 1)}
                        title={pinned ? `${pinned}: one instance only` : undefined}
                        onClick={() => setDraft({ ...draft, [name]: { ...is, replicas: is.replicas + 1 } })}
                      />
                    </Stack>
                  </TableCell>
                  <TableCell>
                    {!st ? (
                      <span className="text-xs text-muted-foreground">not deployed</span>
                    ) : asleep ? (
                      <StatusBadge variant="secondary" type="neutral">
                        asleep, wakes on the first request
                      </StatusBadge>
                    ) : (
                      <StatusBadge variant="secondary" type={(st.failing ?? 0) > 0 ? "error" : st.ready < st.desired ? "warning" : st.desired === 0 ? "neutral" : "success"} qty={`${st.ready}/${st.desired}`} live={st.ready < st.desired && (st.failing ?? 0) === 0}>
                        ready
                      </StatusBadge>
                    )}
                    {st?.sleep?.state === "unavailable" && st.sleep.message && <div className="mt-1 text-[11px] text-muted-foreground">{st.sleep.message}</div>}
                  </TableCell>
                </TableRow>
              );
            })}
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  );
}

const prefixOf = (kind: string) => (kind === "Postgres" ? "DATABASE" : kind === "Redis" ? "REDIS" : kind.toUpperCase());

// What the details of a resource say, in one line.
function detailsOf(r: ResourceInfo, volume?: VolumeInfo) {
  const d = r.details ?? {};
  if (r.kind === "App") return [d.release, d.build, r.endpoint?.replace(/^https:\/\//, "")].filter(Boolean).join(" · ");
  if (r.kind === "Volume") return [d.capacity && d.capacity !== d.size ? `${d.capacity} → ${d.size}` : d.size, d.mode, volume?.restoredFrom && `restored from ${volume.restoredFrom}`].filter(Boolean).join(" · ");
  return [
    d.engine,
    d.version && `v${d.version}`,
    d.size,
    d.storage,
    d.persistent === "true" && "persistent",
    d.instances && `${d.instances} instance${d.instances === "1" ? "" : "s"}`,
    d.backups && `backups ${d.backups}${d.lastBackup ? `, last ${ago(d.lastBackup)}` : ", none yet"}`,
    d.restoredFrom && `restored from ${d.restoredFrom}`,
    r.endpoint,
  ]
    .filter(Boolean)
    .join(" · ");
}

// Databases, caches and volumes of the project: made here, attached to
// the app, and what happens to their data.
function Beside({ project, perms }: { project: Project; perms: Perms }) {
  const queries = useQueryClient();
  const config = useQuery({ queryKey: ["config"], queryFn: api.config, staleTime: 60_000 });
  const resources = useQuery({ queryKey: ["resources", project.slug], queryFn: () => api.resources(project.slug), refetchInterval: 5_000 });
  const volumes = useQuery({ queryKey: ["volumes", project.slug], queryFn: () => api.volumes(project.slug), refetchInterval: 5_000 });
  const refresh = () => {
    queries.invalidateQueries({ queryKey: ["resources", project.slug] });
    queries.invalidateQueries({ queryKey: ["volumes", project.slug] });
    queries.invalidateQueries({ queryKey: ["project", project.slug] });
    queries.invalidateQueries({ queryKey: ["config-vars", project.slug] });
  };
  const told = (message: string) => () => {
    refresh();
    toast.success(message);
  };
  const failed = (e: Error) => toast.error(e.message);
  const remove = useMutation({ mutationFn: (r: ResourceInfo) => api.removeResource(project.slug, r.kind, r.name, r.attachedTo.length > 0), onSuccess: told("Removed"), onError: failed });
  const removeVolume = useMutation({ mutationFn: (v: VolumeInfo) => api.removeVolume(project.slug, v.name, v.mountedBy.length > 0), onSuccess: told("Volume removed"), onError: failed });
  const attach = useMutation({ mutationFn: (r: ResourceInfo) => api.attach(project.slug, { kind: r.kind, name: r.name }), onSuccess: (_, r) => told(`${r.kind} ${r.name} attached: ${prefixOf(r.kind)}_URL and friends are in the config vars`)(), onError: failed });
  const detach = useMutation({ mutationFn: (r: ResourceInfo) => api.detach(project.slug, r.kind, r.name), onSuccess: told("Detached"), onError: failed });
  const attached = (r: ResourceInfo) => !!project.spec.bindings?.some((b) => b.kind === r.kind && b.name === r.name);
  const volumeOf = (name: string) => volumes.data?.find((v) => v.name === name);
  const snapshots = config.data?.volumes?.snapshots ?? false;
  const list = (resources.data ?? []).filter((r) => r.kind !== "App");
  return (
    <Card>
      <CardHeader>
        <CardTitle>Beside the application</CardTitle>
        <CardDescription>Databases, caches and volumes of the project. An attached database or cache puts its connection details in the config vars, read-only, and releases the app.</CardDescription>
        {perms.resource && (
          <CardAction>
            <AddResource project={project} onDone={refresh} />
          </CardAction>
        )}
      </CardHeader>
      <CardContent>
        {resources.isLoading ? (
          <Loading />
        ) : resources.error ? (
          <Failed what="the resources" error={resources.error} />
        ) : list.length === 0 ? (
          <p className="text-sm text-muted-foreground">
            Nothing beside it yet. From the CLI: <InlineCode>shpyrd pg create db</InlineCode>, <InlineCode>shpyrd redis create cache</InlineCode>, <InlineCode>shpyrd attach db</InlineCode>.
          </p>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Kind</TableHead>
                <TableHead>Name</TableHead>
                <TableHead>State</TableHead>
                <TableHead>Details</TableHead>
                <TableHead>Used by</TableHead>
                <TableHead className="text-right" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {list.map((r) => {
                const volume = r.kind === "Volume" ? volumeOf(r.name) : undefined;
                const settled = /Bound|Running|Ready/.test(r.phase);
                return (
                  <TableRow key={`${r.kind}/${r.name}`}>
                    <TableCell>{r.kind}</TableCell>
                    <TableCell>
                      <InlineCode>{r.name}</InlineCode>
                    </TableCell>
                    <TableCell>
                      <StatusBadge type={settled ? "success" : /Fail/.test(r.phase) ? "error" : "warning"} live={/Pending|Restoring|Building/.test(r.phase)}>
                        {r.phase}
                      </StatusBadge>
                      {r.message && !settled && <div className="mt-1 max-w-64 text-[11px] text-muted-foreground">{r.message}</div>}
                    </TableCell>
                    <TableCell className="max-w-72 font-mono text-xs text-muted-foreground">{detailsOf(r, volume)}</TableCell>
                    <TableCell className="font-mono text-xs text-muted-foreground">{r.attachedTo.length ? r.attachedTo.join(", ") : volume?.mountedBy.length ? volume.mountedBy.join(", ") : "-"}</TableCell>
                    <TableCell className="text-right">
                      {perms.resource && (
                        <Stack direction="horizontal" gap="tight" justify="end" align="center">
                          {volume ? (
                            <>
                              {snapshots && <Snapshots slug={project.slug} volume={volume} onChanged={refresh} />}
                              <VolumeDialog slug={project.slug} resize={volume} onDone={refresh} />
                              <ConfirmDialog
                                trigger={<Button variant="ghost" size="icon-xs" icon={<Trash2 />} aria-label={`Remove the volume ${r.name}`} />}
                                variant="destructive"
                                title={`Remove the volume ${r.name}?`}
                                description={volume.mountedBy.length ? `It is mounted by ${volume.mountedBy.join(", ")}. Everything on it (${volume.size}) is lost with it.` : `Everything on it (${volume.size}) is lost with it.`}
                                confirmation={r.name}
                                action="Remove"
                                onConfirm={() => removeVolume.mutate(volume)}
                              />
                            </>
                          ) : (
                            <>
                              {r.bindable &&
                                (attached(r) ? (
                                  <Button variant="outline" size="xs" icon={<Unlink2 />} disabled={detach.isPending} onClick={() => detach.mutate(r)}>
                                    Detach
                                  </Button>
                                ) : (
                                  <Button variant="outline" size="xs" icon={<Link2 />} disabled={attach.isPending || !settled} title={`Puts ${prefixOf(r.kind)}_URL and friends in the config vars`} onClick={() => attach.mutate(r)}>
                                    Attach
                                  </Button>
                                ))}
                              <ConfirmDialog
                                trigger={<Button variant="ghost" size="icon-xs" icon={<Trash2 />} aria-label={`Remove ${r.name}`} />}
                                variant="destructive"
                                title={`Remove ${r.kind} ${r.name}?`}
                                description={r.attachedTo.length ? `It is attached to ${r.attachedTo.join(", ")}. ${r.data ? "It holds data: what is in it is lost with it." : ""}` : r.data ? "It holds data. What is in it is lost with it." : "The application loses it on the next release."}
                                confirmation={r.data ? r.name : undefined}
                                action="Remove"
                                onConfirm={() => remove.mutate(r)}
                              />
                            </>
                          )}
                        </Stack>
                      )}
                    </TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  );
}

// What can be made beside the application: a volume always; a database
// and a cache when the cluster has the extension.
function AddResource({ project, onDone }: { project: Project; onDone: () => void }) {
  const config = useQuery({ queryKey: ["config"], queryFn: api.config, staleTime: 60_000 });
  const has = (x: string) => config.data?.extensions.includes(x) ?? false;
  const [volume, setVolume] = useState(false);
  const [kind, setKind] = useState<"Postgres" | "Redis" | null>(null);
  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button size="sm" icon={<Plus />}>
            Add
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuItem onClick={() => setVolume(true)}>A volume</DropdownMenuItem>
          <DropdownMenuItem disabled={!has("postgres")} onClick={() => setKind("Postgres")}>
            A Postgres database {!has("postgres") && <span className="text-xs text-muted-foreground">needs the postgres extension</span>}
          </DropdownMenuItem>
          <DropdownMenuItem disabled={!has("redis")} onClick={() => setKind("Redis")}>
            A Redis or Valkey store {!has("redis") && <span className="text-xs text-muted-foreground">needs the redis extension</span>}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      <VolumeDialog slug={project.slug} open={volume} onOpenChange={setVolume} onDone={onDone} />
      {kind && <ResourceDialog slug={project.slug} kind={kind} onDone={onDone} onClose={() => setKind(null)} />}
    </>
  );
}

// A volume: made, or grown.
function VolumeDialog({ slug, resize, open: openProp, onOpenChange, onDone }: { slug: string; resize?: VolumeInfo; open?: boolean; onOpenChange?: (o: boolean) => void; onDone: () => void }) {
  const [openState, setOpenState] = useState(false);
  const open = openProp ?? openState;
  const setOpen = (o: boolean) => {
    setOpenState(o);
    onOpenChange?.(o);
  };
  const config = useQuery({ queryKey: ["config"], queryFn: api.config, staleTime: 60_000 });
  const least = config.data?.volumes?.minSize;
  const [name, setName] = useState("");
  const [size, setSize] = useState(resize?.size ?? least ?? "5Gi");
  const [shared, setShared] = useState(false);
  const save = useMutation({
    mutationFn: () => (resize ? api.resizeVolume(slug, resize.name, size.trim()) : api.createVolume(slug, { name: name.trim(), size: size.trim(), shared })),
    onSuccess: (v) => {
      toast.success(resize ? `${resize.name} grows to ${v.size}` : `The volume ${name} was made (${v.size})`, { description: v.note ?? (resize ? undefined : "Mount it from shpyrd.yaml and deploy.") });
      setOpen(false);
      setName("");
      onDone();
    },
    onError: (e: Error) => toast.error(e.message),
  });
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      {resize && (
        <DialogTrigger asChild>
          <Button variant="outline" size="xs">
            Resize
          </Button>
        </DialogTrigger>
      )}
      <DialogContent>
        <DialogHeader divider>
          <DialogTitle>{resize ? `Resize ${resize.name}` : "New volume"}</DialogTitle>
          <DialogDescription>
            {resize ? "A volume only grows, and only where the storage allows it." : "A disk that stays. Mount it from shpyrd.yaml: processes.<type>.volumes: [{name, path}]."}
          </DialogDescription>
        </DialogHeader>
        <form
          className="grid gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            save.mutate();
          }}
        >
          {!resize && (
            <Field label="Name">
              <Input value={name} onChange={(e) => setName(e.target.value.toLowerCase())} placeholder="data" autoFocus />
            </Field>
          )}
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="Size" hint={least ? `At least ${least} here; less is rounded up.` : undefined}>
              <Input value={size} onChange={(e) => setSize(e.target.value)} placeholder={least ?? "5Gi"} className="font-mono text-xs" />
            </Field>
            {!resize && (
              <Field label="Mode" hint={shared ? "Unsafe for SQLite: use Postgres for a database." : undefined}>
                <Select value={shared ? "shared" : "single"} onValueChange={(v) => setShared(v === "shared")}>
                  <SelectTrigger className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="single">One instance, block storage</SelectItem>
                    <SelectItem value="shared">Shared between instances</SelectItem>
                  </SelectContent>
                </Select>
              </Field>
            )}
          </div>
          <DialogFooter>
            <DialogClose asChild>
              <Button type="button" variant="outline">
                Cancel
              </Button>
            </DialogClose>
            <Button type="submit" disabled={save.isPending || !size.trim() || (!resize && !name.trim())}>
              {resize ? "Resize" : "Make it"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

// A Postgres or a Redis, with the few things that matter.
function ResourceDialog({ slug, kind, onDone, onClose }: { slug: string; kind: "Postgres" | "Redis"; onDone: () => void; onClose: () => void }) {
  const catalog = useQuery({ queryKey: ["sizes"], queryFn: api.sizes, staleTime: 60_000 });
  const config = useQuery({ queryKey: ["config"], queryFn: api.config, staleTime: 60_000 });
  const objectStorage = config.data?.extensions.includes("object-storage") ?? false;
  const [name, setName] = useState(kind === "Postgres" ? "db" : "cache");
  const [size, setSize] = useState("");
  const [storage, setStorage] = useState(kind === "Postgres" ? "5Gi" : "1Gi");
  const [version, setVersion] = useState("17");
  const [instances, setInstances] = useState("1");
  const [engine, setEngine] = useState("valkey");
  const [persistent, setPersistent] = useState(false);
  const [backups, setBackups] = useState("off");
  const save = useMutation({
    mutationFn: () => {
      const spec: Record<string, unknown> = {};
      if (size) spec.size = size;
      if (kind === "Postgres") {
        spec.version = version;
        spec.storage = storage.trim();
        spec.instances = Number(instances) || 1;
        if (backups !== "off") spec.backups = { retention: `${backups}d` };
      } else {
        spec.engine = engine;
        spec.persistent = persistent;
        if (persistent) spec.storage = storage.trim();
      }
      return api.createResource(slug, { kind, name: name.trim(), spec });
    },
    onSuccess: () => {
      toast.success(`Making ${kind} ${name.trim()}`, { description: "Attach it once it is ready." });
      onClose();
      onDone();
    },
    onError: (e: Error) => toast.error(e.message),
  });
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader divider>
          <DialogTitle>{kind === "Postgres" ? "New Postgres database" : "New Redis-compatible store"}</DialogTitle>
          <DialogDescription>
            {kind === "Postgres" ? "A PostgreSQL cluster in the project, run by CloudNativePG. Attached, it puts DATABASE_URL and friends in the config vars." : "Valkey or Redis in the project. A cache loses its data on restart; a persistent store keeps a file on a volume. Attached, it puts REDIS_URL and friends in the config vars."}
          </DialogDescription>
        </DialogHeader>
        <form
          className="grid gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            if (name.trim()) save.mutate();
          }}
        >
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="Name">
              <Input value={name} onChange={(e) => setName(e.target.value.toLowerCase())} autoFocus />
            </Field>
            <Field label="Instance size">
              <Select value={size || "default"} onValueChange={(v) => setSize(v === "default" ? "" : v)}>
                <SelectTrigger className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="default">the default, {catalog.data?.default ?? "…"}</SelectItem>
                  {(catalog.data?.sizes ?? []).map((s) => (
                    <SelectItem key={s.name} value={s.name}>
                      {s.name} · {s.cpu} CPU · {s.memory}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
          </div>
          {kind === "Postgres" ? (
            <>
              <div className="grid gap-4 sm:grid-cols-3">
                <Field label="PostgreSQL">
                  <Select value={version} onValueChange={setVersion}>
                    <SelectTrigger className="w-full">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {["17", "16", "15"].map((v) => (
                        <SelectItem key={v} value={v}>
                          {v}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </Field>
                <Field label="Storage">
                  <Input value={storage} onChange={(e) => setStorage(e.target.value)} placeholder="5Gi" className="font-mono text-xs" />
                </Field>
                <Field label="Instances">
                  <Select value={instances} onValueChange={setInstances}>
                    <SelectTrigger className="w-full">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="1">1</SelectItem>
                      <SelectItem value="2">2, high availability</SelectItem>
                      <SelectItem value="3">3, high availability</SelectItem>
                    </SelectContent>
                  </Select>
                </Field>
              </div>
              <Field label="Backups" hint={objectStorage ? "Continuous archiving and a daily base backup to the platform's object store; any point in the window can be restored into a new database." : "Needs the object-storage extension."}>
                <Select value={backups} onValueChange={setBackups}>
                  <SelectTrigger className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="off">Off</SelectItem>
                    {["7", "14", "30"].map((d) => (
                      <SelectItem key={d} value={d} disabled={!objectStorage}>
                        Daily, kept {d} days
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
            </>
          ) : (
            <div className="grid gap-4 sm:grid-cols-3">
              <Field label="Engine">
                <Select value={engine} onValueChange={setEngine}>
                  <SelectTrigger className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="valkey">Valkey</SelectItem>
                    <SelectItem value="redis">Redis</SelectItem>
                  </SelectContent>
                </Select>
              </Field>
              <Field label="Mode">
                <Select value={persistent ? "persistent" : "cache"} onValueChange={(v) => setPersistent(v === "persistent")}>
                  <SelectTrigger className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="cache">A cache</SelectItem>
                    <SelectItem value="persistent">Persistent, a queue</SelectItem>
                  </SelectContent>
                </Select>
              </Field>
              {persistent && (
                <Field label="Storage">
                  <Input value={storage} onChange={(e) => setStorage(e.target.value)} placeholder="1Gi" className="font-mono text-xs" />
                </Field>
              )}
            </div>
          )}
          <DialogFooter>
            <Button type="button" variant="outline" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" disabled={!name.trim() || save.isPending}>
              Make it
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

// The snapshots of a volume: taken, restored into a new volume or in
// place, removed.
function Snapshots({ slug, volume, onChanged }: { slug: string; volume: VolumeInfo; onChanged: () => void }) {
  const [open, setOpen] = useState(false);
  const queries = useQueryClient();
  const key = ["snapshots", slug, volume.name];
  const snapshots = useQuery({ queryKey: key, queryFn: () => api.snapshots(slug, volume.name), enabled: open, refetchInterval: (q) => (q.state.data?.some((s) => !s.ready) ? 3000 : false) });
  const refresh = () => {
    queries.invalidateQueries({ queryKey: key });
    onChanged();
  };
  const [name, setName] = useState("");
  const [restoring, setRestoring] = useState<SnapshotInfo | null>(null);
  const [to, setTo] = useState("");
  const take = useMutation({
    mutationFn: () => api.createSnapshot(slug, volume.name, name.trim() || undefined),
    onSuccess: (s) => {
      toast.success(`Taking ${s.name}`);
      setName("");
      refresh();
    },
    onError: (e: Error) => toast.error(e.message),
  });
  const remove = useMutation({
    mutationFn: (s: SnapshotInfo) => api.removeSnapshot(slug, volume.name, s.name),
    onSuccess: () => {
      toast.success("Snapshot removed");
      refresh();
    },
    onError: (e: Error) => toast.error(e.message),
  });
  const restore = useMutation({
    mutationFn: (body: { snapshot: string; to?: string }) => api.restoreVolume(slug, volume.name, body),
    onSuccess: (r) => {
      toast.success(r.message);
      setRestoring(null);
      if (r.inPlace) setOpen(false);
      refresh();
    },
    onError: (e: Error) => toast.error(e.message),
  });
  const list = snapshots.data ?? [];
  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        setOpen(o);
        if (!o) setRestoring(null);
      }}
    >
      <DialogTrigger asChild>
        <Button variant="outline" size="xs" icon={<Camera />}>
          Snapshots
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader divider>
          <DialogTitle>Snapshots of {volume.name}</DialogTitle>
          <DialogDescription>Copies of the disk at a point in time, taken by the storage. One is restored into a new volume, or in place: the instances mounting the volume stop while the disk is swapped. Neither is a release.</DialogDescription>
        </DialogHeader>
        {restoring ? (
          <form
            className="grid gap-4"
            onSubmit={(e) => {
              e.preventDefault();
              restore.mutate({ snapshot: restoring.name, to: to.trim() || undefined });
            }}
          >
            <Field label={`Restore ${restoring.name} into a new volume named`} hint={`Empty: restored in place; what is on ${volume.name} now is lost${volume.mountedBy.length ? `, and ${volume.mountedBy.join(", ")} stops while the disk is swapped` : ""}.`}>
              <Input value={to} onChange={(e) => setTo(e.target.value.toLowerCase())} placeholder={`${volume.name}-restored`} autoFocus />
            </Field>
            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => setRestoring(null)}>
                Back
              </Button>
              <Button type="submit" variant={to.trim() ? "default" : "destructive"} disabled={restore.isPending}>
                {to.trim() ? `Make ${to.trim()}` : `Restore ${volume.name} in place`}
              </Button>
            </DialogFooter>
          </form>
        ) : (
          <Stack gap="normal">
            {snapshots.isLoading ? (
              <Loading rows={2} />
            ) : snapshots.error ? (
              <Failed what="the snapshots" error={snapshots.error} />
            ) : list.length === 0 ? (
              <p className="text-sm text-muted-foreground">None yet. Take one before a risky release or a migration.</p>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Snapshot</TableHead>
                    <TableHead>Size</TableHead>
                    <TableHead>State</TableHead>
                    <TableHead>Taken</TableHead>
                    <TableHead className="text-right" />
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {list.map((s) => (
                    <TableRow key={s.name}>
                      <TableCell className="font-medium">{s.name}</TableCell>
                      <TableCell className="font-mono text-xs">{s.size ?? "-"}</TableCell>
                      <TableCell>
                        <StatusBadge type={s.ready ? "success" : "warning"} live={!s.ready}>
                          {s.ready ? "ready" : s.message || "in progress"}
                        </StatusBadge>
                      </TableCell>
                      <TableCell className="text-xs text-muted-foreground">{ago(s.createdAt)}</TableCell>
                      <TableCell className="text-right">
                        <Stack direction="horizontal" gap="tight" justify="end">
                          <Button
                            variant="outline"
                            size="xs"
                            icon={<Undo2 />}
                            disabled={!s.ready || volume.phase === "Restoring"}
                            onClick={() => {
                              setTo("");
                              setRestoring(s);
                            }}
                          >
                            Restore
                          </Button>
                          <ConfirmDialog
                            trigger={<Button variant="ghost" size="icon-xs" icon={<Trash2 />} aria-label={`Remove ${s.name}`} />}
                            variant="destructive"
                            title={`Remove the snapshot ${s.name}?`}
                            description="For good."
                            action="Remove"
                            onConfirm={() => remove.mutate(s)}
                          />
                        </Stack>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
            <form
              className="flex items-end gap-2"
              onSubmit={(e) => {
                e.preventDefault();
                take.mutate();
              }}
            >
              <Field label="New snapshot" hint="Empty: named after the volume and the time." className="flex-1">
                <Input value={name} onChange={(e) => setName(e.target.value.toLowerCase())} placeholder={`${volume.name}-before-migration`} />
              </Field>
              <Button type="submit" icon={<Camera />} disabled={take.isPending || volume.phase !== "Bound"} title={volume.phase !== "Bound" ? "Only a bound volume can be snapshotted" : undefined}>
                Take
              </Button>
            </form>
          </Stack>
        )}
      </DialogContent>
    </Dialog>
  );
}
