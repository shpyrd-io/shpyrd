"use client";

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ArrowLeftRight,
  Camera,
  Link2,
  Minus,
  Plus,
  Trash2,
  Undo2,
  Unlink2,
} from "lucide-react";
import { toast } from "sonner";
import {
  Alert,
  AlertDescription,
  AlertTitle,
} from "@shpyrd/ui/components/alert";
import { Button } from "@shpyrd/ui/components/button";
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@shpyrd/ui/components/card";
import { ConfirmDialog } from "@shpyrd/ui/components/confirm-dialog";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@shpyrd/ui/components/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@shpyrd/ui/components/dropdown-menu";
import { Field } from "@shpyrd/ui/components/field";
import { InlineCode } from "@shpyrd/ui/components/inline-code";
import { Input } from "@shpyrd/ui/components/input";
import { ProgressBar } from "@shpyrd/ui/components/progress-bar";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@shpyrd/ui/components/select";
import { Stack } from "@shpyrd/ui/components/stack";
import { StatusBadge } from "@shpyrd/ui/components/status-badge";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@shpyrd/ui/components/table";
import { api } from "@/api/api";
import type {
  InstanceSize,
  Project,
  PublicConfig,
  ResourceInfo,
  SizeCatalog,
  SnapshotInfo,
  VolumeInfo,
} from "@/api/types";
import type { Perms } from "@/lib/perms";
import { ago } from "@/lib/project";
import { busy } from "./heading";
import { Failed, Loading } from "./shared";

// What the project runs on: the size and count of each process, and
// the things beside it, a database, a cache, a volume.
export function Resources({
  project,
  perms,
}: {
  project: Project;
  perms: Perms;
}) {
  const usage = useQuery({
    queryKey: ["usage", project.slug],
    queryFn: () => api.projectUsage(project.slug),
    staleTime: 30_000,
  });
  return (
    <>
      {usage.data?.ceiling && <StorageUsed usage={usage.data} />}
      <Processes project={project} perms={perms} />
      <Beside project={project} perms={perms} />
    </>
  );
}

// What the project's disks claim against the ceiling that applies to it
// (storage plan step 4): its own cap, or the workspace's, shared with the
// other projects. Over the line, everything keeps working and nothing
// more fits.
function StorageUsed({
  usage,
}: {
  usage: { storage: string; ceiling?: string; own?: boolean };
}) {
  const used = gbOf(usage.storage);
  const of = gbOf(usage.ceiling ?? "0");
  const pct = of > 0 ? Math.round((used / of) * 100) : 0;
  return (
    <Card>
      <CardHeader>
        <CardTitle>Storage</CardTitle>
        <CardDescription>
          {usage.own
            ? "This project has a cap of its own, set under the workspace's Plan."
            : "The workspace's ceiling, shared with its other projects."}{" "}
          Disks and dedicated databases count by the capacity they claim.
        </CardDescription>
      </CardHeader>
      <CardContent>
        <ProgressBar
          label={`${gbWords(used)} of ${gbWords(of)}${pct > 100 ? ` · ${pct}%` : ""}`}
          value={Math.min(used, of)}
          max={of}
          size="sm"
          tone={pct >= 100 ? "error" : pct >= 80 ? "warning" : "orange"}
          format={(v) => gbWords(v)}
        />
      </CardContent>
    </Card>
  );
}

type Draft = { size: string; replicas: number };

// The size and how many of each process, changed together and applied
// as one release.
function Processes({ project, perms }: { project: Project; perms: Perms }) {
  const queries = useQueryClient();
  const catalog = useQuery({
    queryKey: ["sizes"],
    queryFn: api.sizes,
    staleTime: 60_000,
  });
  const names = Object.keys({
    ...(project.spec.processes ?? { web: {} }),
    ...project.processes,
  }).sort();
  const current = (name: string): Draft => ({
    size:
      project.spec.processes?.[name]?.size ||
      project.processes?.[name]?.size ||
      catalog.data?.default ||
      "",
    replicas:
      project.spec.processes?.[name]?.replicas ??
      project.processes?.[name]?.desired ??
      1,
  });
  const [draft, setDraft] = useState<Record<string, Draft>>({});
  const value = (name: string) => draft[name] ?? current(name);
  const changes = Object.fromEntries(
    names
      .map((name) => {
        const was = current(name);
        const is = value(name);
        const change: { size?: string; replicas?: number } = {};
        if (is.size && is.size !== was.size && is.size !== "custom")
          change.size = is.size;
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
      toast.success("Applying", {
        description: Object.values(changes).some((c) => c.size)
          ? "One release rolls out with the new sizes."
          : "The instances follow.",
      });
    },
    onError: (e: Error) => toast.error(e.message),
  });
  return (
    <Card>
      <CardHeader>
        <CardTitle>Processes</CardTitle>
        <CardDescription>
          The size and how many instances of each. Changes are applied together:
          one release, one rollout.
        </CardDescription>
        {perms.resource && dirty && (
          <CardAction>
            <Stack direction="horizontal" gap="condensed">
              <Button
                size="sm"
                variant="outline"
                onClick={() => setDraft({})}
                disabled={apply.isPending}
              >
                Cancel
              </Button>
              <Button
                size="sm"
                disabled={apply.isPending || waiting}
                title={
                  waiting ? "Wait for the current release to finish" : undefined
                }
                onClick={() => apply.mutate()}
              >
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
              const asleep = st?.sleep?.state === "sleeping";
              const changed =
                is.size !== was.size || is.replicas !== was.replicas;
              const pinned = st?.pinned;
              return (
                <TableRow
                  key={name}
                  data-state={changed ? "selected" : undefined}
                >
                  <TableCell className="font-medium">
                    {name}
                    {spec?.port && (
                      <span className="ml-1 font-mono text-[10px] text-muted-foreground">
                        :{spec.port}
                      </span>
                    )}
                  </TableCell>
                  <TableCell className="font-mono text-xs text-muted-foreground">
                    {spec?.command ?? "from the image"}
                  </TableCell>
                  <TableCell>
                    <Select
                      value={is.size}
                      disabled={!perms.resource || waiting || !catalog.data}
                      onValueChange={(size) =>
                        setDraft({ ...draft, [name]: { ...is, size } })
                      }
                    >
                      <SelectTrigger
                        size="sm"
                        className="w-52"
                        data-changed={is.size !== was.size || undefined}
                      >
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
                        {was.size === "custom" && (
                          <SelectItem value="custom">custom</SelectItem>
                        )}
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
                      <Button
                        variant="outline"
                        size="icon-xs"
                        icon={<Minus />}
                        aria-label="One less"
                        disabled={
                          !perms.resource || waiting || is.replicas <= 0
                        }
                        onClick={() =>
                          setDraft({
                            ...draft,
                            [name]: { ...is, replicas: is.replicas - 1 },
                          })
                        }
                      />
                      <span
                        className={`w-6 text-center font-mono text-xs ${is.replicas !== was.replicas ? "text-primary" : ""}`}
                      >
                        {is.replicas}
                      </span>
                      <Button
                        variant="outline"
                        size="icon-xs"
                        icon={<Plus />}
                        aria-label="One more"
                        disabled={
                          !perms.resource ||
                          waiting ||
                          (!!pinned && is.replicas >= 1)
                        }
                        title={
                          pinned ? `${pinned}: one instance only` : undefined
                        }
                        onClick={() =>
                          setDraft({
                            ...draft,
                            [name]: { ...is, replicas: is.replicas + 1 },
                          })
                        }
                      />
                    </Stack>
                  </TableCell>
                  <TableCell>
                    {!st ? (
                      <span className="text-xs text-muted-foreground">
                        not deployed
                      </span>
                    ) : asleep ? (
                      <StatusBadge variant="secondary" type="neutral">
                        asleep, wakes on the first request
                      </StatusBadge>
                    ) : (
                      <StatusBadge
                        variant="secondary"
                        type={
                          (st.failing ?? 0) > 0
                            ? "error"
                            : st.ready < st.desired
                              ? "warning"
                              : st.desired === 0
                                ? "neutral"
                                : "success"
                        }
                        qty={`${st.ready}/${st.desired}`}
                        live={st.ready < st.desired && (st.failing ?? 0) === 0}
                      >
                        ready
                      </StatusBadge>
                    )}
                    {st?.sleep?.state === "unavailable" && st.sleep.message && (
                      <div className="mt-1 text-[11px] text-muted-foreground">
                        {st.sleep.message}
                      </div>
                    )}
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

const prefixOf = (kind: string) =>
  kind === "Postgres"
    ? "DATABASE"
    : kind === "Redis"
      ? "REDIS"
      : kind.toUpperCase();

// What the details of a resource say, in one line; a database's or a
// store's address goes on a line of its own, beside it.
export function detailsOf(r: ResourceInfo, volume?: VolumeInfo) {
  const d = r.details ?? {};
  if (r.kind === "App")
    return [d.release, d.build, r.endpoint?.replace(/^https:\/\//, "")]
      .filter(Boolean)
      .join(" · ");
  if (r.kind === "Volume")
    return [
      d.capacity && d.capacity !== d.size
        ? `${d.capacity} → ${d.size}`
        : d.size,
      d.mode,
      volume?.restoredFrom && `restored from ${volume.restoredFrom}`,
      volume?.resize &&
        volume.resize.phase !== "completed" &&
        `growth to ${volume.resize.to} ${volume.resize.phase}${volume.resize.message ? `: ${volume.resize.message}` : ""}`,
    ]
      .filter(Boolean)
      .join(" · ");
  if (r.kind === "Postgres" && d.type === "shared")
    return [
      "shared",
      d.version && `PostgreSQL ${d.version}`,
      d.pooler === "on" && "via the pooler",
      d.backups &&
        `${d.backups.replace(/^nightly, kept (\d+)$/, "$1 nightly copies")}${d.lastBackup ? `, last ${ago(d.lastBackup)}` : ", none yet"}`,
      d.restoredFrom && `restored from ${d.restoredFrom}`,
      d.converted,
    ]
      .filter(Boolean)
      .join(" · ");
  return [
    r.kind === "Postgres" && (d.type === "dedicated" || !d.type) && "dedicated",
    d.engine,
    d.version && `v${d.version}`,
    d.size,
    d.storage,
    d.persistent === "true" && "persistent",
    d.instances && `${d.instances} instance${d.instances === "1" ? "" : "s"}`,
    d.backups &&
      `backups ${d.backups}${d.lastBackup ? `, last ${ago(d.lastBackup)}` : ", none yet"}`,
    d.restoredFrom && `restored from ${d.restoredFrom}`,
  ]
    .filter(Boolean)
    .join(" · ");
}

// A shared database's cap: what it holds over what it may, a thin bar
// with its words.
function CapGauge({ d }: { d: Record<string, string> }) {
  const used = Number(d.usedBytes ?? 0);
  const cap = Number(d.capBytes ?? 0);
  if (!cap) return null;
  const pct = Math.round((used / cap) * 100);
  return (
    <ProgressBar
      inline
      size="sm"
      label={`${sizeOf(used)} of ${sizeOf(cap)} · ${pct}%`}
      value={Math.min(pct, 100)}
      tone={pct >= 150 ? "error" : pct >= 80 ? "warning" : "orange"}
      className="mt-1 w-auto [&>span:last-child]:w-16"
    />
  );
}

// "312 MiB", "1.5 GiB".
function sizeOf(n: number) {
  if (n <= 0) return "nothing";
  if (n >= 1 << 30)
    return `${(n / (1 << 30)).toFixed(n % (1 << 30) === 0 ? 0 : 1)} GiB`;
  if (n >= 1 << 20) return `${Math.round(n / (1 << 20))} MiB`;
  return `${Math.round(n / 1024)} KiB`;
}

// The kind of a database: shared on the platform's server, or a cluster
// of its own.
export const kindOf = (r: ResourceInfo) =>
  r.details?.type === "shared" ? "shared" : "dedicated";

// Databases, caches and volumes of the project: made here, attached to
// the app, and what happens to their data.
function Beside({ project, perms }: { project: Project; perms: Perms }) {
  const queries = useQueryClient();
  const config = useQuery({
    queryKey: ["config"],
    queryFn: api.config,
    staleTime: 60_000,
  });
  const resources = useQuery({
    queryKey: ["resources", project.slug],
    queryFn: () => api.resources(project.slug),
    refetchInterval: 5_000,
  });
  const volumes = useQuery({
    queryKey: ["volumes", project.slug],
    queryFn: () => api.volumes(project.slug),
    refetchInterval: 5_000,
  });
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
  const remove = useMutation({
    mutationFn: (r: ResourceInfo) =>
      api.removeResource(project.slug, r.kind, r.name, r.attachedTo.length > 0),
    onSuccess: told("Removed"),
    onError: failed,
  });
  const removeVolume = useMutation({
    mutationFn: (v: VolumeInfo) =>
      api.removeVolume(project.slug, v.name, v.mountedBy.length > 0),
    onSuccess: told("Volume removed"),
    onError: failed,
  });
  const attach = useMutation({
    mutationFn: (r: ResourceInfo) =>
      api.attach(project.slug, { kind: r.kind, name: r.name }),
    onSuccess: (_, r) =>
      told(
        `${r.kind} ${r.name} attached: ${prefixOf(r.kind)}_URL and friends are in the config vars`,
      )(),
    onError: failed,
  });
  const detach = useMutation({
    mutationFn: (r: ResourceInfo) => api.detach(project.slug, r.kind, r.name),
    onSuccess: told("Detached"),
    onError: failed,
  });
  const attached = (r: ResourceInfo) =>
    !!project.spec.bindings?.some(
      (b) => b.kind === r.kind && b.name === r.name,
    );
  const volumeOf = (name: string) => volumes.data?.find((v) => v.name === name);
  const snapshots = config.data?.volumes?.snapshots ?? false;
  const list = (resources.data ?? []).filter((r) => r.kind !== "App");
  return (
    <Card>
      <CardHeader>
        <CardTitle>Beside the application</CardTitle>
        <CardDescription>
          Databases, caches and volumes of the project. An attached database or
          cache puts its connection details in the config vars, read-only, and
          releases the app.
        </CardDescription>
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
            Nothing beside it yet. From the CLI:{" "}
            <InlineCode>shpyrd pg create db</InlineCode>,{" "}
            <InlineCode>shpyrd redis create cache</InlineCode>,{" "}
            <InlineCode>shpyrd attach db</InlineCode>.
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
                const volume =
                  r.kind === "Volume" ? volumeOf(r.name) : undefined;
                const settled = /Bound|Running|Ready/.test(r.phase);
                // A shared database over its cap says so while Ready.
                const paused =
                  settled &&
                  r.kind === "Postgres" &&
                  !!r.message &&
                  /paused/i.test(r.message);
                return (
                  <TableRow key={`${r.kind}/${r.name}`}>
                    <TableCell>{r.kind}</TableCell>
                    <TableCell>
                      <InlineCode>{r.name}</InlineCode>
                    </TableCell>
                    <TableCell>
                      <StatusBadge
                        type={
                          paused
                            ? "warning"
                            : settled
                              ? "success"
                              : /Fail/.test(r.phase)
                                ? "error"
                                : "warning"
                        }
                        live={/Pending|Restoring|Building|Converting/.test(
                          r.phase,
                        )}
                      >
                        {paused ? "Paused" : r.phase}
                      </StatusBadge>
                      {r.message && (!settled || paused) && (
                        <div className="mt-1 w-48 text-[11px] whitespace-normal text-muted-foreground">
                          {r.message}
                        </div>
                      )}
                    </TableCell>
                    <TableCell className="max-w-72 font-mono text-xs whitespace-normal text-muted-foreground">
                      {detailsOf(r, volume)}
                      {r.kind === "Postgres" &&
                        r.details?.type === "shared" && (
                          <CapGauge d={r.details} />
                        )}
                      {r.kind !== "Volume" && r.endpoint && (
                        <div className="truncate" title={r.endpoint}>
                          {r.endpoint}
                        </div>
                      )}
                    </TableCell>
                    <TableCell className="font-mono text-xs text-muted-foreground">
                      {r.attachedTo.length
                        ? r.attachedTo.join(", ")
                        : volume?.mountedBy.length
                          ? volume.mountedBy.join(", ")
                          : "-"}
                    </TableCell>
                    <TableCell className="text-right">
                      {perms.resource && (
                        <Stack
                          direction="horizontal"
                          gap="tight"
                          justify="end"
                          align="center"
                        >
                          {volume ? (
                            <>
                              {snapshots &&
                                volume.storageClass !== "shpyrd-local" && (
                                  <Snapshots
                                    slug={project.slug}
                                    volume={volume}
                                    onChanged={refresh}
                                  />
                                )}
                              {volume.storageClass !== "shpyrd-local" && (
                                <VolumeDialog
                                  slug={project.slug}
                                  resize={volume}
                                  onDone={refresh}
                                />
                              )}
                              <ConfirmDialog
                                trigger={
                                  <Button
                                    variant="ghost"
                                    size="icon-xs"
                                    icon={<Trash2 />}
                                    aria-label={`Remove the volume ${r.name}`}
                                  />
                                }
                                variant="destructive"
                                title={`Remove the volume ${r.name}?`}
                                description={
                                  volume.mountedBy.length
                                    ? `It is mounted by ${volume.mountedBy.join(", ")}. Everything on it (${volume.size}) is lost with it.`
                                    : `Everything on it (${volume.size}) is lost with it.`
                                }
                                confirmation={r.name}
                                action="Remove"
                                onConfirm={() => removeVolume.mutate(volume)}
                              />
                            </>
                          ) : (
                            <>
                              {(r.kind === "Redis" ||
                                (r.kind === "Postgres" &&
                                  kindOf(r) === "dedicated")) && (
                                <SizeDialog
                                  slug={project.slug}
                                  resource={r}
                                  onDone={refresh}
                                />
                              )}
                              {r.kind === "Postgres" && settled && (
                                <ConvertDialog
                                  slug={project.slug}
                                  resource={r}
                                  config={config.data}
                                  onDone={refresh}
                                />
                              )}
                              {r.bindable &&
                                (attached(r) ? (
                                  <Button
                                    variant="outline"
                                    size="xs"
                                    icon={<Unlink2 />}
                                    disabled={detach.isPending}
                                    onClick={() => detach.mutate(r)}
                                  >
                                    Detach
                                  </Button>
                                ) : (
                                  <Button
                                    variant="outline"
                                    size="xs"
                                    icon={<Link2 />}
                                    disabled={attach.isPending || !settled}
                                    title={`Puts ${prefixOf(r.kind)}_URL and friends in the config vars`}
                                    onClick={() => attach.mutate(r)}
                                  >
                                    Attach
                                  </Button>
                                ))}
                              <ConfirmDialog
                                trigger={
                                  <Button
                                    variant="ghost"
                                    size="icon-xs"
                                    icon={<Trash2 />}
                                    aria-label={`Remove ${r.name}`}
                                  />
                                }
                                variant="destructive"
                                title={`Remove ${r.kind} ${r.name}?`}
                                description={
                                  r.attachedTo.length
                                    ? `It is attached to ${r.attachedTo.join(", ")}. ${r.data ? "It holds data: what is in it is lost with it." : ""}`
                                    : r.data
                                      ? "It holds data. What is in it is lost with it."
                                      : "The application loses it on the next release."
                                }
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
function AddResource({
  project,
  onDone,
}: {
  project: Project;
  onDone: () => void;
}) {
  const config = useQuery({
    queryKey: ["config"],
    queryFn: api.config,
    staleTime: 60_000,
  });
  const workspace = useQuery({
    queryKey: ["workspace"],
    queryFn: api.workspace,
    staleTime: 60_000,
  });
  const has = (x: string) => config.data?.extensions.includes(x) ?? false;
  const disks = workspace.data?.limits?.allowDisks !== false;
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
          <DropdownMenuItem disabled={!disks} onClick={() => setVolume(true)}>
            A volume{" "}
            {!disks && (
              <span className="text-xs text-muted-foreground">
                not included in the free workspace
              </span>
            )}
          </DropdownMenuItem>
          <DropdownMenuItem
            disabled={!has("postgres")}
            onClick={() => setKind("Postgres")}
          >
            A Postgres database{" "}
            {!has("postgres") && (
              <span className="text-xs text-muted-foreground">
                needs the postgres extension
              </span>
            )}
          </DropdownMenuItem>
          <DropdownMenuItem
            disabled={!has("redis")}
            onClick={() => setKind("Redis")}
          >
            A Redis or Valkey store{" "}
            {!has("redis") && (
              <span className="text-xs text-muted-foreground">
                needs the redis extension
              </span>
            )}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      <VolumeDialog
        slug={project.slug}
        open={volume}
        onOpenChange={setVolume}
        onDone={onDone}
      />
      {kind && (
        <ResourceDialog
          slug={project.slug}
          kind={kind}
          onDone={onDone}
          onClose={() => setKind(null)}
        />
      )}
    </>
  );
}

// A volume: made, or grown.
export function VolumeDialog({
  slug,
  resize,
  open: openProp,
  onOpenChange,
  onDone,
}: {
  slug: string;
  resize?: VolumeInfo;
  open?: boolean;
  onOpenChange?: (o: boolean) => void;
  onDone: () => void;
}) {
  const [openState, setOpenState] = useState(false);
  const open = openProp ?? openState;
  const setOpen = (o: boolean) => {
    setOpenState(o);
    onOpenChange?.(o);
  };
  const config = useQuery({
    queryKey: ["config"],
    queryFn: api.config,
    staleTime: 60_000,
  });
  const workspace = useQuery({
    queryKey: ["workspace"],
    queryFn: api.workspace,
    staleTime: 60_000,
  });
  const usage = useQuery({
    queryKey: ["usage", slug],
    queryFn: () => api.projectUsage(slug),
    staleTime: 30_000,
    enabled: open,
  });
  const least = config.data?.volumes?.minSize;
  const nodeLocal = resize
    ? resize.storageClass === "shpyrd-local"
    : config.data?.volumes?.nodeLocal;
  const [name, setName] = useState("");
  const [size, setSize] = useState(resize?.size ?? least ?? "5Gi");
  const [shared, setShared] = useState(false);
  // What may still be added (storage plan step 4): the least of what the
  // workspace's ceiling and the project's own cap leave; the server judges
  // the same way, so the dialog says it before the request.
  const allowance = allowanceOf(
    workspace.data?.limits?.storage,
    workspace.data?.usage?.storage,
    usage.data,
  );
  const asked = gbOf(size) - (resize ? gbOf(resize.size) : 0);
  const over = allowance !== undefined && asked > allowance + 1e-9;
  const noDisks = !resize && workspace.data?.limits?.allowDisks === false;
  const save = useMutation({
    mutationFn: () =>
      resize
        ? api.resizeVolume(slug, resize.name, size.trim())
        : api.createVolume(slug, {
            name: name.trim(),
            size: size.trim(),
            shared,
          }),
    onSuccess: (v) => {
      toast.success(
        resize
          ? `${resize.name} grows to ${v.size}`
          : `The volume ${name} was made (${v.size})`,
        {
          description:
            v.note ??
            (resize ? undefined : "Mount it from shpyrd.yaml and deploy."),
        },
      );
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
          <DialogTitle>
            {resize ? `Resize ${resize.name}` : "New volume"}
          </DialogTitle>
          <DialogDescription>
            {resize
              ? "A volume only grows, and only where the storage allows it."
              : "A disk that stays. Mount it from shpyrd.yaml: processes.<type>.volumes: [{name, path}]."}
          </DialogDescription>
        </DialogHeader>
        {noDisks && (
          <Alert variant="upsell">
            <AlertTitle>
              Disks are not included in the free workspace
            </AlertTitle>
            <AlertDescription>
              Use a bucket for files, or upgrade.
            </AlertDescription>
          </Alert>
        )}
        {!noDisks && usage.data?.ceiling && (
          <p className="text-xs text-muted-foreground" data-testid="allowance">
            {resize ? `${resize.name} is ${gbWords(gbOf(resize.size))}. ` : ""}
            {allowance !== undefined && allowance > 0
              ? `You can add up to ${gbWords(allowance)}`
              : "Nothing more fits"}
            {usage.data.own
              ? ` under this project's cap of ${gbWords(gbOf(usage.data.ceiling))}.`
              : ` under the workspace's ${gbWords(gbOf(usage.data.ceiling))}.`}
          </p>
        )}
        <form
          className="grid gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            save.mutate();
          }}
        >
          {!resize && (
            <Field label="Name">
              <Input
                value={name}
                onChange={(e) => setName(e.target.value.toLowerCase())}
                placeholder="data"
                autoFocus
              />
            </Field>
          )}
          <div className="grid gap-4 sm:grid-cols-2">
            <Field
              label="Size"
              hint={
                over
                  ? resize
                    ? `This would bring the ${usage.data?.own ? "project" : "workspace"} past its ceiling.`
                    : `This would bring the ${usage.data?.own ? "project" : "workspace"} past its ceiling.`
                  : nodeLocal
                    ? "Planning size, not a hard limit. Free space is shared with other volumes on this node."
                    : least
                      ? `At least ${least} here; less is rounded up.`
                      : undefined
              }
            >
              <Input
                value={size}
                onChange={(e) => setSize(e.target.value)}
                placeholder={least ?? "5Gi"}
                className="font-mono text-xs"
                aria-invalid={over || undefined}
              />
            </Field>
            {!resize && (
              <Field
                label="Mode"
                hint={
                  shared
                    ? nodeLocal
                      ? "Instances sharing this volume run on the same node. Use Postgres for a shared database."
                      : "Unsafe for SQLite: use Postgres for a database."
                    : undefined
                }
              >
                <Select
                  value={shared ? "shared" : "single"}
                  onValueChange={(v) => setShared(v === "shared")}
                >
                  <SelectTrigger className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="single">
                      {nodeLocal
                        ? "One instance, node disk"
                        : "One instance, block storage"}
                    </SelectItem>
                    <SelectItem value="shared">
                      Shared between instances
                    </SelectItem>
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
            <Button
              type="submit"
              disabled={
                save.isPending ||
                !size.trim() ||
                (!resize && !name.trim()) ||
                over ||
                noDisks
              }
            >
              {resize ? "Resize" : "Make it"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

// The sizes of a Postgres or a Redis: lists of their own, with the
// processes' names (#57).
const sizesOf = (catalog: SizeCatalog | undefined, kind: string) =>
  kind === "Postgres" ? catalog?.postgres : catalog?.redis;

// A size as a choice reads: name · CPU · memory · connections.
function SizeOption({ size }: { size: InstanceSize }) {
  return (
    <>
      {size.name} · {size.cpu} CPU · {size.memory}
      {size.connections ? (
        <span className="text-xs text-muted-foreground">
          {" "}
          · {size.connections} connections
        </span>
      ) : null}
    </>
  );
}

// A Postgres or a Redis, with the few things that matter.
export function ResourceDialog({
  slug,
  kind,
  onDone,
  onClose,
}: {
  slug: string;
  kind: "Postgres" | "Redis";
  onDone: () => void;
  onClose: () => void;
}) {
  const catalog = useQuery({
    queryKey: ["sizes"],
    queryFn: api.sizes,
    staleTime: 60_000,
  });
  const config = useQuery({
    queryKey: ["config"],
    queryFn: api.config,
    staleTime: 60_000,
  });
  const objectStorage =
    config.data?.extensions.includes("object-storage") ?? false;
  // The kinds of database the plan allows: one, or a choice.
  const rules = config.data?.postgres;
  const workspace = useQuery({
    queryKey: ["workspace"],
    queryFn: api.workspace,
    staleTime: 60_000,
  });
  const plan = workspace.data?.limits?.postgres;
  const kinds = rules?.types?.length ? rules.types : (["dedicated"] as const);
  // The count against the plan (storage plan step 4): said before the
  // request, judged again by the server.
  const have = workspace.data?.usage?.databases ?? 0;
  const full =
    kind === "Postgres" && !!plan?.databases && have + 1 > plan.databases;
  const maxInstances = plan?.maxInstances ?? 3;
  const [pickedType, setPickedType] = useState<"shared" | "dedicated">();
  const type =
    kind === "Postgres"
      ? (pickedType ?? rules?.defaultType ?? kinds[0])
      : "dedicated";
  const shared = kind === "Postgres" && type === "shared";
  const cap = rules?.sharedCap ?? "1Gi";
  const [copies, setCopies] = useState("7");
  const list = sizesOf(catalog.data, kind);
  const [name, setName] = useState(kind === "Postgres" ? "db" : "cache");
  const [picked, setPicked] = useState<string>();
  const size = picked ?? list?.default ?? "";
  const [storage, setStorage] = useState(kind === "Postgres" ? "5Gi" : "1Gi");
  const [version, setVersion] = useState("17");
  const [instances, setInstances] = useState("1");
  const [engine, setEngine] = useState("valkey");
  const [persistent, setPersistent] = useState(false);
  const [backups, setBackups] = useState("off");
  const save = useMutation({
    mutationFn: () => {
      const spec: Record<string, unknown> = {};
      if (shared) {
        spec.type = "shared";
        if (copies !== "7") spec.backups = { retention: `${copies}d` };
      } else if (kind === "Postgres") {
        spec.type = "dedicated";
        if (size) spec.size = size;
        spec.version = version;
        spec.storage = storage.trim();
        spec.instances = Number(instances) || 1;
        if (backups !== "off") spec.backups = { retention: `${backups}d` };
      } else {
        if (size) spec.size = size;
        spec.engine = engine;
        spec.persistent = persistent;
        if (persistent) spec.storage = storage.trim();
      }
      return api.createResource(slug, { kind, name: name.trim(), spec });
    },
    onSuccess: (r) => {
      toast.success(`Making ${kind} ${name.trim()}`, {
        description: r.note
          ? `${r.note} Attach it once it is ready.`
          : "Attach it once it is ready.",
        duration: r.note ? 12_000 : undefined,
      });
      onClose();
      onDone();
    },
    onError: (e: Error) => toast.error(e.message),
  });
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader divider>
          <DialogTitle>
            {kind === "Postgres"
              ? "New Postgres database"
              : "New Redis-compatible store"}
          </DialogTitle>
          <DialogDescription>
            {kind === "Postgres"
              ? shared
                ? `A database on the platform's shared server: always on, holds up to ${humanCap(cap)}, PostgreSQL 17, a nightly copy kept. Attached, it puts DATABASE_URL and friends in the config vars.`
                : "A PostgreSQL cluster of the project's own, with a disk, run by CloudNativePG. Attached, it puts DATABASE_URL and friends in the config vars."
              : "Valkey or Redis in the project. A cache loses its data on restart; a persistent store keeps a file on a volume. Attached, it puts REDIS_URL and friends in the config vars."}
          </DialogDescription>
        </DialogHeader>
        {/* The form waits for the config: a select whose items change
            under it (the plan's kinds, backups needing an extension) loses
            its value in Radix. */}
        {!config.data ? (
          <Loading rows={3} />
        ) : (
          <>
            {full && (
              <Alert variant="upsell">
                <AlertTitle>
                  This would bring the workspace to {have + 1} of{" "}
                  {plan?.databases} databases.
                </AlertTitle>
                <AlertDescription>
                  Remove one, or move to a plan with more.
                </AlertDescription>
              </Alert>
            )}
            <form
              className="grid gap-4"
              onSubmit={(e) => {
                e.preventDefault();
                if (name.trim() && !full) save.mutate();
              }}
            >
              <div className="grid gap-4 sm:grid-cols-2">
                <Field label="Name">
                  <Input
                    value={name}
                    onChange={(e) => setName(e.target.value.toLowerCase())}
                    autoFocus
                  />
                </Field>
                {kind === "Postgres" && (
                  <Field
                    label="Kind"
                    hint={
                      kinds.length === 1
                        ? kinds[0] === "shared"
                          ? "Your plan includes a shared database. A dedicated database comes with a paid plan."
                          : "The workspace's plan has dedicated databases."
                        : undefined
                    }
                  >
                    <Select
                      value={type}
                      onValueChange={(v) =>
                        setPickedType(v as "shared" | "dedicated")
                      }
                      disabled={kinds.length === 1}
                    >
                      <SelectTrigger className="w-full">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        {kinds.map((k) => (
                          <SelectItem key={k} value={k}>
                            {k === "shared"
                              ? `Shared · on the platform's server, up to ${humanCap(cap)}`
                              : "Dedicated · a cluster of its own, with a disk"}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </Field>
                )}
                {!shared && (
                  <Field label="Instance size">
                    <Select
                      value={size}
                      onValueChange={setPicked}
                      disabled={!list}
                    >
                      <SelectTrigger className="w-full">
                        <SelectValue placeholder="size" />
                      </SelectTrigger>
                      <SelectContent>
                        {(list?.sizes ?? []).map((s) => (
                          <SelectItem key={s.name} value={s.name}>
                            <SizeOption size={s} />
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </Field>
                )}
              </div>
              {shared ? (
                <>
                  <p className="text-xs text-muted-foreground">
                    No size, version or disk to choose: the server is PostgreSQL
                    17 with two instances, and the cap of {humanCap(cap)} is
                    what the database is metered by. pgvector is installed;
                    PostgreSQL's trusted extensions are yours to create. At 80 %
                    of the cap the status warns; at 150 % connections pause
                    until it is back under 95 %.
                  </p>
                  <Field
                    label="Copies kept"
                    hint="A copy is taken every night; the newest ones stay. Any of them restores into a new database."
                  >
                    {/* Keyed: React would otherwise reuse this Select for the
                    Backups one of the other kind, and Radix reads the value
                    of the swapped items as empty. */}
                    <Select
                      key="copies"
                      value={copies}
                      onValueChange={setCopies}
                    >
                      <SelectTrigger className="w-full">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        {["7", "14", "30"].map((d) => (
                          <SelectItem key={d} value={d}>
                            {d} nights
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </Field>
                </>
              ) : kind === "Postgres" ? (
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
                      <Input
                        value={storage}
                        onChange={(e) => setStorage(e.target.value)}
                        placeholder="5Gi"
                        className="font-mono text-xs"
                      />
                    </Field>
                    <Field
                      label="Instances"
                      hint={
                        maxInstances < 3
                          ? `A dedicated database here runs up to ${maxInstances} instance${maxInstances === 1 ? "" : "s"}.`
                          : undefined
                      }
                    >
                      <Select value={instances} onValueChange={setInstances}>
                        <SelectTrigger className="w-full">
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          {[
                            ["1", "1"],
                            ["2", "2, high availability"],
                            ["3", "3, high availability"],
                          ].map(([value, words]) => (
                            <SelectItem
                              key={value}
                              value={value}
                              disabled={Number(value) > maxInstances}
                            >
                              {words}
                            </SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                    </Field>
                  </div>
                  <Field
                    label="Backups"
                    hint={
                      objectStorage
                        ? "Continuous archiving and a daily base backup to the platform's object store; any point in the window can be restored into a new database."
                        : "Needs the object-storage extension."
                    }
                  >
                    <Select
                      key="backups"
                      value={backups}
                      onValueChange={setBackups}
                    >
                      <SelectTrigger className="w-full">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        {/* One array of items: a Radix Select whose children
                        mix an element and an array reads its value as
                        empty when the native select fires first. */}
                        {[
                          ["off", "Off", false],
                          ["7", "Daily, kept 7 days", !objectStorage],
                          ["14", "Daily, kept 14 days", !objectStorage],
                          ["30", "Daily, kept 30 days", !objectStorage],
                        ].map(([value, words, off]) => (
                          <SelectItem
                            key={String(value)}
                            value={String(value)}
                            disabled={Boolean(off)}
                          >
                            {words}
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
                    <Select
                      value={persistent ? "persistent" : "cache"}
                      onValueChange={(v) => setPersistent(v === "persistent")}
                    >
                      <SelectTrigger className="w-full">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="cache">A cache</SelectItem>
                        <SelectItem value="persistent">
                          Persistent, a queue
                        </SelectItem>
                      </SelectContent>
                    </Select>
                  </Field>
                  {persistent && (
                    <Field label="Storage">
                      <Input
                        value={storage}
                        onChange={(e) => setStorage(e.target.value)}
                        placeholder="1Gi"
                        className="font-mono text-xs"
                      />
                    </Field>
                  )}
                </div>
              )}
              <DialogFooter>
                <Button type="button" variant="outline" onClick={onClose}>
                  Cancel
                </Button>
                <Button
                  type="submit"
                  disabled={
                    full || !name.trim() || (!shared && !size) || save.isPending
                  }
                >
                  Make it
                </Button>
              </DialogFooter>
            </form>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}

// "1 GB" for 1Gi: the cap in words.
// "20Gi" as GB, "512Mi" as a fraction of one; a number as it is.
export function gbOf(q: string | undefined) {
  if (!q) return 0;
  const n = parseFloat(q);
  if (Number.isNaN(n)) return 0;
  if (q.endsWith("Mi")) return n / 1024;
  if (q.endsWith("Ti")) return n * 1024;
  return n;
}

// 5 as "5 GB", 0.5 as "512 MB".
export function gbWords(g: number) {
  return g >= 1 || g === 0
    ? `${Number.isInteger(g) ? g : g.toFixed(1)} GB`
    : `${Math.round(g * 1024)} MB`;
}

// What may still be added, in GB (storage plan step 4): the least of
// what the workspace's ceiling leaves and what the project's own cap
// leaves; undefined when nothing bounds it.
export function allowanceOf(
  ceiling: string | undefined,
  used: string | undefined,
  project?: { storage: string; ceiling?: string; own?: boolean },
) {
  const rooms: number[] = [];
  if (ceiling) rooms.push(Math.max(0, gbOf(ceiling) - gbOf(used)));
  if (project?.own && project.ceiling)
    rooms.push(Math.max(0, gbOf(project.ceiling) - gbOf(project.storage)));
  return rooms.length ? Math.min(...rooms) : undefined;
}

export function humanCap(q: string) {
  const m = /^(\d+(?:\.\d+)?)(Gi|Mi|G|M)?$/.exec(q.trim());
  if (!m) return q;
  const n = Number(m[1]);
  return m[2]?.startsWith("M") ? `${n} MB` : `${n} GB`;
}

// A database moved between the shared server and a cluster of its own,
// under its name. The words say what follows before anything moves;
// the server's own words come back with the answer.
export function ConvertDialog({
  slug,
  resource,
  config,
  onDone,
}: {
  slug: string;
  resource: ResourceInfo;
  config?: PublicConfig;
  onDone: () => void;
}) {
  const catalog = useQuery({
    queryKey: ["sizes"],
    queryFn: api.sizes,
    staleTime: 60_000,
  });
  const from = kindOf(resource);
  const to = from === "shared" ? "dedicated" : "shared";
  const allowed =
    !config?.postgres?.types?.length || config.postgres.types.includes(to);
  const cap = config?.postgres?.sharedCap ?? "1Gi";
  const list = catalog.data?.postgres;
  const [open, setOpen] = useState(false);
  const [storage, setStorage] = useState("5Gi");
  const [picked, setPicked] = useState<string>();
  const size = picked ?? list?.default ?? "";
  const convert = useMutation({
    mutationFn: () =>
      api.convertPostgres(
        slug,
        resource.name,
        to === "dedicated"
          ? {
              to,
              size: size || undefined,
              storage: storage.trim() || undefined,
            }
          : { to },
      ),
    onSuccess: (r) => {
      toast.success(`Converting ${r.name} to a ${r.to} database`, {
        description: r.note,
        duration: 15_000,
      });
      setOpen(false);
      onDone();
    },
    onError: (e: Error) => toast.error(e.message),
  });
  if (!allowed) return null;
  const words =
    to === "dedicated"
      ? [
          `${resource.name} becomes a cluster of its own with a ${storage || "5Gi"} disk, priced as a dedicated database.`,
          "Its user and database names stay; DATABASE_URL changes only its host, and the apps bound to it restart once.",
        ]
      : [
          `${resource.name} moves onto the platform's shared server with a ${humanCap(cap)} cap; its disk goes.`,
          "It gets new user and database names, so the apps bound to it restart once with the new credentials. Only the extensions a shared database can hold may be installed: PostgreSQL's trusted ones and pgvector.",
        ];
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button
          variant="outline"
          size="xs"
          icon={<ArrowLeftRight />}
          title={`Convert to a ${to} database`}
        >
          Convert
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader divider>
          <DialogTitle>
            Convert {resource.name} to a {to} database?
          </DialogTitle>
          <DialogDescription>
            Connections pause while the data is copied, a few minutes per
            gigabyte; a copy of the source stays restorable for seven days. A
            conversion that cannot land puts the database back as it was and
            says why.
          </DialogDescription>
        </DialogHeader>
        <form
          className="grid gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            convert.mutate();
          }}
        >
          {to === "dedicated" && (
            <div className="grid gap-4 sm:grid-cols-2">
              <Field label="Disk">
                <Input
                  value={storage}
                  onChange={(e) => setStorage(e.target.value)}
                  placeholder="5Gi"
                  className="font-mono text-xs"
                />
              </Field>
              <Field label="Instance size">
                <Select value={size} onValueChange={setPicked} disabled={!list}>
                  <SelectTrigger className="w-full">
                    <SelectValue placeholder="size" />
                  </SelectTrigger>
                  <SelectContent>
                    {(list?.sizes ?? []).map((s) => (
                      <SelectItem key={s.name} value={s.name}>
                        <SizeOption size={s} />
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
            </div>
          )}
          <ul className="grid list-disc gap-1 pl-4 text-xs text-muted-foreground">
            {words.map((w) => (
              <li key={w}>{w}</li>
            ))}
          </ul>
          <DialogFooter>
            <DialogClose asChild>
              <Button type="button" variant="outline">
                Cancel
              </Button>
            </DialogClose>
            <Button type="submit" disabled={convert.isPending}>
              Convert
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

// Another size for a Postgres or a Redis, from its kind's list. A
// database's instances restart one at a time; a store restarts, a cache
// comes back empty.
function SizeDialog({
  slug,
  resource,
  onDone,
}: {
  slug: string;
  resource: ResourceInfo;
  onDone: () => void;
}) {
  const catalog = useQuery({
    queryKey: ["sizes"],
    queryFn: api.sizes,
    staleTime: 60_000,
  });
  const list = sizesOf(catalog.data, resource.kind);
  const current = resource.details?.size ?? "";
  const [open, setOpen] = useState(false);
  const [picked, setPicked] = useState<string>();
  const size = picked ?? current;
  const save = useMutation({
    mutationFn: () =>
      api.resizeResource(slug, resource.kind, resource.name, size),
    onSuccess: (r) => {
      toast.success(`${resource.name} goes to ${size}`, {
        description: r.note,
        duration: 12_000,
      });
      setOpen(false);
      onDone();
    },
    onError: (e: Error) => toast.error(e.message),
  });
  const what =
    resource.kind === "Postgres"
      ? "Its instances restart with the new size, one at a time; with one instance the database is unavailable for a moment."
      : resource.details?.persistent === "true"
        ? "It restarts with the new size and reloads its file."
        : "It restarts with the new size and comes back empty: it is a cache.";
  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        setOpen(o);
        setPicked(undefined);
      }}
    >
      <DialogTrigger asChild>
        <Button variant="outline" size="xs">
          Resize
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader divider>
          <DialogTitle>Resize {resource.name}</DialogTitle>
          <DialogDescription>{what}</DialogDescription>
        </DialogHeader>
        <form
          className="grid gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            save.mutate();
          }}
        >
          <Field label="Instance size">
            <Select value={size} onValueChange={setPicked} disabled={!list}>
              <SelectTrigger className="w-full">
                <SelectValue placeholder="size" />
              </SelectTrigger>
              <SelectContent>
                {(list?.sizes ?? []).map((s) => (
                  <SelectItem key={s.name} value={s.name}>
                    <SizeOption size={s} />
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
          <DialogFooter>
            <DialogClose asChild>
              <Button type="button" variant="outline">
                Cancel
              </Button>
            </DialogClose>
            <Button
              type="submit"
              disabled={!size || size === current || save.isPending}
            >
              Resize
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

// The snapshots of a volume: taken, restored into a new volume or in
// place, removed.
function Snapshots({
  slug,
  volume,
  onChanged,
}: {
  slug: string;
  volume: VolumeInfo;
  onChanged: () => void;
}) {
  const [open, setOpen] = useState(false);
  const queries = useQueryClient();
  const key = ["snapshots", slug, volume.name];
  const snapshots = useQuery({
    queryKey: key,
    queryFn: () => api.snapshots(slug, volume.name),
    enabled: open,
    refetchInterval: (q) =>
      q.state.data?.some((s) => !s.ready) ? 3000 : false,
  });
  const refresh = () => {
    queries.invalidateQueries({ queryKey: key });
    onChanged();
  };
  const [name, setName] = useState("");
  const [restoring, setRestoring] = useState<SnapshotInfo | null>(null);
  const [to, setTo] = useState("");
  const take = useMutation({
    mutationFn: () =>
      api.createSnapshot(slug, volume.name, name.trim() || undefined),
    onSuccess: (s) => {
      toast.success(`Taking ${s.name}`);
      setName("");
      refresh();
    },
    onError: (e: Error) => toast.error(e.message),
  });
  const remove = useMutation({
    mutationFn: (s: SnapshotInfo) =>
      api.removeSnapshot(slug, volume.name, s.name),
    onSuccess: () => {
      toast.success("Snapshot removed");
      refresh();
    },
    onError: (e: Error) => toast.error(e.message),
  });
  const restore = useMutation({
    mutationFn: (body: { snapshot: string; to?: string }) =>
      api.restoreVolume(slug, volume.name, body),
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
          <DialogDescription>
            Copies of the disk at a point in time, taken by the storage. One is
            restored into a new volume, or in place: the instances mounting the
            volume stop while the disk is swapped. Neither is a release.
          </DialogDescription>
        </DialogHeader>
        {restoring ? (
          <form
            className="grid gap-4"
            onSubmit={(e) => {
              e.preventDefault();
              restore.mutate({
                snapshot: restoring.name,
                to: to.trim() || undefined,
              });
            }}
          >
            <Field
              label={`Restore ${restoring.name} into a new volume named`}
              hint={`Empty: restored in place; what is on ${volume.name} now is lost${volume.mountedBy.length ? `, and ${volume.mountedBy.join(", ")} stops while the disk is swapped` : ""}.`}
            >
              <Input
                value={to}
                onChange={(e) => setTo(e.target.value.toLowerCase())}
                placeholder={`${volume.name}-restored`}
                autoFocus
              />
            </Field>
            <DialogFooter>
              <Button
                type="button"
                variant="outline"
                onClick={() => setRestoring(null)}
              >
                Back
              </Button>
              <Button
                type="submit"
                variant={to.trim() ? "default" : "destructive"}
                disabled={restore.isPending}
              >
                {to.trim()
                  ? `Make ${to.trim()}`
                  : `Restore ${volume.name} in place`}
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
              <p className="text-sm text-muted-foreground">
                None yet. Take one before a risky release or a migration.
              </p>
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
                      <TableCell className="font-mono text-xs">
                        {s.size ?? "-"}
                      </TableCell>
                      <TableCell>
                        <StatusBadge
                          type={s.ready ? "success" : "warning"}
                          live={!s.ready}
                        >
                          {s.ready ? "ready" : s.message || "in progress"}
                        </StatusBadge>
                      </TableCell>
                      <TableCell className="text-xs text-muted-foreground">
                        {ago(s.createdAt)}
                      </TableCell>
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
                            trigger={
                              <Button
                                variant="ghost"
                                size="icon-xs"
                                icon={<Trash2 />}
                                aria-label={`Remove ${s.name}`}
                              />
                            }
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
              <Field
                label="New snapshot"
                hint="Empty: named after the volume and the time."
                className="flex-1"
              >
                <Input
                  value={name}
                  onChange={(e) => setName(e.target.value.toLowerCase())}
                  placeholder={`${volume.name}-before-migration`}
                />
              </Field>
              <Button
                type="submit"
                icon={<Camera />}
                disabled={take.isPending || volume.phase !== "Bound"}
                title={
                  volume.phase !== "Bound"
                    ? "Only a bound volume can be snapshotted"
                    : undefined
                }
              >
                Take
              </Button>
            </form>
          </Stack>
        )}
      </DialogContent>
    </Dialog>
  );
}
