"use client";

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ArrowLeftRight,
  Camera,
  Gauge,
  KeyRound,
  Link2,
  Minus,
  MoonStar,
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
import { Slider } from "@shpyrd/ui/components/slider";
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
import { api, type AutoscaleChange } from "@/api/api";
import type {
  AutoscaleSpec,
  AutoscaleStatus,
  InstanceSize,
  Project,
  PublicConfig,
  ResourceInfo,
  SizeCatalog,
  SleepStatus,
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
  const sleep = useMutation({
    mutationFn: (policy: { after: string; resuming?: "page" | "wait" }) =>
      api.applyProcesses(project.slug, { web: { sleep: policy } }),
    onSuccess: (_, policy) => {
      queries.invalidateQueries({ queryKey: ["project", project.slug] });
      toast.success(sleepSetWords("web", policy));
    },
    onError: (e: Error) => toast.error(e.message),
  });
  // The bounds the web process follows its load between (RFC-0047), set
  // on their own like sleep: they are not a release.
  const autoscale = useMutation({
    mutationFn: (bounds: AutoscaleChange) =>
      api.applyProcesses(project.slug, { web: { autoscale: bounds } }),
    onSuccess: (_, bounds) => {
      queries.invalidateQueries({ queryKey: ["project", project.slug] });
      setDraft({});
      toast.success(autoscaleSetWords("web", bounds));
    },
    onError: (e: Error) => toast.error(e.message),
  });
  const apply = useMutation({
    mutationFn: (force?: boolean) => api.applyProcesses(project.slug, changes, force),
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
              {waiting ? (
                // The way back is never locked (#127): over a rollout in
                // progress the change asks first, then cancels it.
                <ConfirmDialog
                  trigger={
                    <Button size="sm" disabled={apply.isPending}>
                      Apply {Object.keys(changes).length}
                    </Button>
                  }
                  title="Apply over the rollout in progress?"
                  description={`A release is still rolling out (${project.status.message || project.status.phase}). Applying these changes cancels it: a new release rolls out with them.`}
                  action="Apply anyway"
                  onConfirm={() => apply.mutate(true)}
                />
              ) : (
                <Button
                  size="sm"
                  disabled={apply.isPending}
                  onClick={() => apply.mutate(false)}
                >
                  Apply {Object.keys(changes).length}
                </Button>
              )}
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
              const bounds = spec?.autoscale;
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
                      {bounds ? (
                        // The count follows the load: the bounds are the
                        // setting, the dialog changes them.
                        <span
                          className="font-mono text-xs"
                          title={`follows its load between ${bounds.min} and ${bounds.max} instances`}
                        >
                          {bounds.min}–{bounds.max}{" "}
                          <span className="font-sans text-[10px] text-muted-foreground">
                            auto
                          </span>
                        </span>
                      ) : (
                        <>
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
                        </>
                      )}
                      {name === "web" && perms.resource && !pinned && (
                        <AutoscaleDialog
                          what={project.slug}
                          current={bounds}
                          count={is.replicas}
                          busy={autoscale.isPending}
                          onSave={(change) => autoscale.mutate(change)}
                        />
                      )}
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
                    {name === "web" && st?.sleep && (
                      <Stack
                        direction="horizontal"
                        align="center"
                        gap="tight"
                        className="mt-1"
                      >
                        <span className="text-[11px] text-muted-foreground">
                          {sleepWords(st.sleep)}
                        </span>
                        {perms.resource && (
                          <SleepDialog
                            what={project.slug}
                            current={st.sleep}
                            modes
                            busy={sleep.isPending}
                            onSave={(policy) => sleep.mutate(policy)}
                          />
                        )}
                      </Stack>
                    )}
                    {name === "web" && st?.autoscale && (
                      <div className="mt-1 w-64 text-[11px] whitespace-normal text-muted-foreground">
                        {autoscaleWords(st.autoscale)}
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
      : kind === "ObjectBucket"
        ? "BUCKET"
        : kind.toUpperCase();

// The word people read for a kind: a bucket, not an ObjectBucket.
export const wordOf = (kind: string) =>
  kind === "ObjectBucket" ? "Bucket" : kind;

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
  if (r.kind === "ObjectBucket")
    return [
      d.capBytes && d.capBytes !== "0"
        ? `up to ${gbWords(Number(d.capBytes) / (1 << 30))}`
        : "no cap",
      d.public === "true" && "public read",
      d.region && `region ${d.region}`,
      d.retentionDays && d.retentionDays !== "0" && `kept ${d.retentionDays} days`,
      d.rotating && "key being rotated",
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
    r.kind === "Postgres" &&
      d.sleepSource &&
      (d.sleepAfter || d.sleepSource === "off") &&
      sleepWords(sleepOf(d)),
  ]
    .filter(Boolean)
    .join(" · ");
}

// A database's sleep read-back, from its details (#120).
export function sleepOf(d: Record<string, string>): SleepStatus {
  return {
    state: (d.sleepState || "awake") as SleepStatus["state"],
    after: d.sleepAfter,
    source: d.sleepSource as SleepStatus["source"],
    message: d.sleepMessage,
  };
}

// What a sleep policy says in a few words: the three states of #135 read
// apart, with the wake mode when it has one.
export function sleepWords(st: SleepStatus) {
  const mode = st.resuming ? ` (${st.resuming} mode)` : "";
  const now =
    st.state === "sleeping"
      ? "; asleep now"
      : st.state === "suspended"
        ? "; suspended now"
        : "";
  if (st.source === "off") return "never sleeps, whatever the workspace's default";
  if (st.source === "project") return `sleeps after ${st.after}${mode}, its own policy${now}`;
  if (st.after) return `sleeps after ${st.after}${mode}, the workspace's default${now}`;
  return "does not sleep: the workspace's default sets none";
}

// What the toast says once a policy is set.
function sleepSetWords(
  what: string,
  policy: { after: string; resuming?: "page" | "wait" },
) {
  const after = policy.after.toLowerCase();
  if (after === "off") return `${what} never sleeps now, whatever the workspace's default.`;
  if (after === "default" || after === "")
    return `${what} follows the workspace's default again.`;
  return `${what} sleeps after ${after}${policy.resuming ? ` (${policy.resuming} mode)` : ""}.`;
}

// The quiet periods a policy may name.
const quietPeriods = ["5m", "15m", "30m", "1h", "2h", "6h", "12h", "24h"];

// What the bounds do, in a few words: the targets in force, then what the
// cluster cannot do about them.
export function autoscaleWords(st: AutoscaleStatus) {
  const bounds = `follows its load between ${st.min} and ${st.max}: another instance past ${st.cpu}% CPU or ${st.concurrency} requests in flight`;
  if (st.state === "unavailable") return `${bounds}; not now: ${st.message ?? "the cluster cannot follow the bounds"}`;
  if (st.message) return `${bounds}; ${st.message}`;
  return bounds;
}

// A typed number, or what stands in for one that is not a number yet.
const num = (v: string, fallback: number) => {
  const n = Number.parseInt(v, 10);
  return Number.isFinite(n) ? n : fallback;
};
const clamp = (v: number, lo: number, hi: number) => Math.min(hi, Math.max(lo, v));

// What the toast says once the bounds are set.
function autoscaleSetWords(what: string, change: AutoscaleChange) {
  if (change.off) return `${what} keeps a fixed count again.`;
  return `${what} follows its load between ${change.min} and ${change.max} instances.`;
}

// The bounds of the web process (RFC-0047): a fixed count, or a minimum
// and a maximum it follows its load between, with the targets that add an
// instance when the defaults will not do.
export function AutoscaleDialog({
  what,
  current,
  count,
  busy = false,
  onSave,
}: {
  what: string;
  current?: AutoscaleSpec;
  count: number;
  busy?: boolean;
  onSave: (change: AutoscaleChange) => void;
}) {
  const [open, setOpen] = useState(false);
  const [mode, setMode] = useState<"fixed" | "auto">(current ? "auto" : "fixed");
  // The workspace's ceiling on instances is how far the line goes; a
  // maximum typed past it stretches the line, and the API says no.
  const workspace = useQuery({
    queryKey: ["workspace"],
    queryFn: api.workspace,
    staleTime: 60_000,
    enabled: open,
  });
  const ceiling = workspace.data?.limits?.instances;
  const [min, setMin] = useState(String(current?.min ?? Math.max(1, count)));
  const [max, setMax] = useState(String(current?.max ?? Math.max(2, count * 2)));
  const [cpu, setCpu] = useState(String(current?.cpu ?? 70));
  const [concurrency, setConcurrency] = useState(String(current?.concurrency ?? 20));
  const reset = () => {
    setMode(current ? "auto" : "fixed");
    setMin(String(current?.min ?? Math.max(1, count)));
    setMax(String(current?.max ?? Math.max(2, count * 2)));
    setCpu(String(current?.cpu ?? 70));
    setConcurrency(String(current?.concurrency ?? 20));
  };
  const n = (v: string) => Number.parseInt(v, 10);
  // How far the line goes: the ceiling, or 100 without one, stretched to
  // a bound typed past it so the thumb is never off the line.
  const span = Math.max(2, ceiling ?? 100, num(min, 1), num(max, 1));
  const valid =
    mode === "fixed" ||
    (n(min) >= 1 && n(max) >= n(min) && n(max) <= 100 && n(cpu) >= 1 && n(cpu) <= 100 && n(concurrency) >= 1);
  const change: AutoscaleChange =
    mode === "fixed"
      ? { off: true }
      : {
          min: n(min),
          max: n(max),
          ...(n(cpu) !== 70 ? { cpu: n(cpu) } : {}),
          ...(n(concurrency) !== 20 ? { concurrency: n(concurrency) } : {}),
        };
  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        setOpen(o);
        if (o) reset();
      }}
    >
      <DialogTrigger asChild>
        <Button variant="outline" size="xs" icon={<Gauge />} title="How many instances">
          Autoscale
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader divider>
          <DialogTitle>How many instances {what} runs</DialogTitle>
          <DialogDescription>
            A fixed count, or a count that follows the load: another instance
            when one passes the CPU target or carries more requests than the
            target, one fewer when the load has been lower for five minutes,
            always within the bounds. A sleep policy still takes it to zero.
          </DialogDescription>
        </DialogHeader>
        <form
          className="grid gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            if (!valid) return;
            onSave(change);
            setOpen(false);
          }}
        >
          <Field label="Instances">
            <Select value={mode} onValueChange={(v) => setMode(v as typeof mode)}>
              <SelectTrigger className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="fixed">A fixed count</SelectItem>
                <SelectItem value="auto">Follow the load, between two bounds</SelectItem>
              </SelectContent>
            </Select>
          </Field>
          {mode === "auto" && (
            <>
              <Field
                label="Bounds"
                hint={`shpyrd scale sets the minimum. The maximum counts against the workspace's instances${ceiling ? ` (${ceiling})` : ""}.`}
              >
                <div role="group" className="grid grid-cols-[5rem_1fr_5rem] items-center gap-x-3 gap-y-1">
                  <Input type="number" min={1} max={100} value={min} onChange={(e) => setMin(e.target.value)} aria-label="Minimum instances" />
                  <Slider
                    min={1}
                    max={span}
                    value={[clamp(num(min, 1), 1, span), clamp(num(max, 1), 1, span)]}
                    onValueChange={([lo, hi]) => {
                      setMin(String(lo));
                      setMax(String(hi));
                    }}
                    thumbLabels={["At least", "At most"]}
                  />
                  <Input type="number" min={1} max={100} value={max} onChange={(e) => setMax(e.target.value)} aria-label="Maximum instances" />
                  <span aria-hidden className="text-xs text-muted-foreground">
                    at least
                  </span>
                  <span />
                  <span aria-hidden className="text-right text-xs text-muted-foreground">
                    at most
                  </span>
                </div>
              </Field>
              <Field label="CPU target" hint="Percent of the size's CPU an instance may use before another is added (70).">
                <div role="group" className="flex items-center gap-3">
                  <Slider
                    className="flex-1"
                    min={1}
                    max={100}
                    value={[clamp(num(cpu, 70), 1, 100)]}
                    onValueChange={([v]) => setCpu(String(v))}
                    aria-label="CPU target slider"
                  />
                  <Input type="number" min={1} max={100} value={cpu} onChange={(e) => setCpu(e.target.value)} suffix="%" className="w-24" aria-label="CPU target" />
                </div>
              </Field>
              <Field label="Requests in flight" hint="Per instance, before another is added (20).">
                <div role="group" className="flex items-center gap-3">
                  <Slider
                    className="flex-1"
                    min={1}
                    max={Math.max(100, num(concurrency, 20))}
                    value={[clamp(num(concurrency, 20), 1, Math.max(100, num(concurrency, 20)))]}
                    onValueChange={([v]) => setConcurrency(String(v))}
                    aria-label="Requests in flight slider"
                  />
                  <Input type="number" min={1} value={concurrency} onChange={(e) => setConcurrency(e.target.value)} className="w-24" aria-label="Requests in flight per instance" />
                </div>
              </Field>
            </>
          )}
          <DialogFooter>
            <DialogClose asChild>
              <Button type="button" variant="outline">
                Cancel
              </Button>
            </DialogClose>
            <Button type="submit" disabled={busy || !valid}>
              Save
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

// The sleep of a web process or a dedicated database (#132): the three
// states of #135, a quiet period of its own with its wake mode for a web
// process, off, or the workspace's default.
export function SleepDialog({
  what,
  current,
  modes = false,
  busy = false,
  onSave,
}: {
  what: string;
  current: SleepStatus;
  modes?: boolean;
  busy?: boolean;
  onSave: (policy: { after: string; resuming?: "page" | "wait" }) => void;
}) {
  const [open, setOpen] = useState(false);
  const initial = current.source ?? "workspace";
  const [choice, setChoice] = useState<"workspace" | "project" | "off">(initial);
  const [after, setAfter] = useState(
    current.source === "project" && current.after ? current.after : "30m",
  );
  const [resuming, setResuming] = useState<"page" | "wait">(
    current.resuming === "page" ? "page" : "wait",
  );
  const reset = () => {
    setChoice(initial);
    setAfter(current.source === "project" && current.after ? current.after : "30m");
    setResuming(current.resuming === "page" ? "page" : "wait");
  };
  const policy =
    choice === "off"
      ? { after: "off" }
      : choice === "workspace"
        ? { after: "default" }
        : { after, ...(modes ? { resuming } : {}) };
  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        setOpen(o);
        if (o) reset();
      }}
    >
      <DialogTrigger asChild>
        <Button variant="outline" size="xs" icon={<MoonStar />} title="When it sleeps">
          Sleep
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader divider>
          <DialogTitle>When {what} sleeps</DialogTitle>
          <DialogDescription>
            Now it {sleepWords(current)}.{" "}
            {modes
              ? "A sleeping app scales to zero after the quiet period and wakes on the first request."
              : "A sleeping database stops after the quiet period without client connections and wakes on the first one; its volume and data stay."}
          </DialogDescription>
        </DialogHeader>
        <form
          className="grid gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            onSave(policy);
            setOpen(false);
          }}
        >
          <Field label="Policy">
            <Select
              value={choice}
              onValueChange={(v) => setChoice(v as typeof choice)}
            >
              <SelectTrigger className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="workspace">The workspace&apos;s default</SelectItem>
                <SelectItem value="project">A quiet period of its own</SelectItem>
                <SelectItem value="off">Never sleeps</SelectItem>
              </SelectContent>
            </Select>
          </Field>
          {choice === "project" && (
            <div className="grid gap-4 sm:grid-cols-2">
              <Field label="Quiet period">
                <Select value={after} onValueChange={setAfter}>
                  <SelectTrigger className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {quietPeriods.map((q) => (
                      <SelectItem key={q} value={q}>
                        {q}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
              {modes && (
                <Field
                  label="Waking"
                  hint="An app with a disk wakes behind the page: the disk takes a moment to reattach."
                >
                  <Select
                    value={resuming}
                    onValueChange={(v) => setResuming(v as "page" | "wait")}
                  >
                    <SelectTrigger className="w-full">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="page">A waking page</SelectItem>
                      <SelectItem value="wait">Hold the request</SelectItem>
                    </SelectContent>
                  </Select>
                </Field>
              )}
            </div>
          )}
          <DialogFooter>
            <DialogClose asChild>
              <Button type="button" variant="outline">
                Cancel
              </Button>
            </DialogClose>
            <Button type="submit" disabled={busy}>
              Save
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
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
  const sleepDatabase = useMutation({
    mutationFn: ({ r, after }: { r: ResourceInfo; after: string }) =>
      api.sleepPostgres(project.slug, r.name, after),
    onSuccess: (_, { r, after }) => {
      toast.success(sleepSetWords(r.name, { after }), {
        description:
          "The apps attached to it are re-released once when the policy appears or disappears.",
      });
      refresh();
    },
    onError: failed,
  });
  const rotate = useMutation({
    mutationFn: (r: ResourceInfo) => api.rotateBucket(project.slug, r.name),
    onSuccess: (answer) =>
      told(answer.note ?? "A new key is being written")(),
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
                    <TableCell>{wordOf(r.kind)}</TableCell>
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
                      {((r.kind === "Postgres" &&
                        r.details?.type === "shared") ||
                        (r.kind === "ObjectBucket" &&
                          r.details?.capBytes &&
                          r.details.capBytes !== "0")) && (
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
                              {r.kind === "Postgres" &&
                                settled &&
                                kindOf(r) === "dedicated" &&
                                r.details?.sleepSource && (
                                  <SleepDialog
                                    what={r.name}
                                    current={sleepOf(r.details)}
                                    busy={sleepDatabase.isPending}
                                    onSave={({ after }) =>
                                      sleepDatabase.mutate({ r, after })
                                    }
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
                              {r.kind === "ObjectBucket" && settled && (
                                <ConfirmDialog
                                  trigger={
                                    <Button
                                      variant="outline"
                                      size="xs"
                                      icon={<KeyRound />}
                                      disabled={
                                        rotate.isPending ||
                                        !!r.details?.rotating
                                      }
                                    >
                                      New key
                                    </Button>
                                  }
                                  title={`A new key for ${r.name}?`}
                                  description="The key the bucket has stops working and another is written in its place; the apps attached to it restart with it within a minute. Anything else that holds the old key must be given the new one."
                                  action="Issue a new key"
                                  onConfirm={() => rotate.mutate(r)}
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
                                title={`Remove ${wordOf(r.kind).toLowerCase()} ${r.name}?`}
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
  const [bucket, setBucket] = useState(false);
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
            disabled={!has("object-storage")}
            onClick={() => setBucket(true)}
          >
            A bucket{" "}
            {!has("object-storage") && (
              <span className="text-xs text-muted-foreground">
                needs the object-storage extension
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
      {bucket && (
        <BucketDialog
          slug={project.slug}
          onDone={onDone}
          onClose={() => setBucket(false)}
        />
      )}
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

// A bucket for the app's files (storage plan step 5): a name, a cap
// within the plan's per bucket (the plan's when left as it is), public
// read or not, and how long objects are kept. The count against the plan
// is said before the request; the server judges it again.
export function BucketDialog({
  slug,
  onDone,
  onClose,
}: {
  slug: string;
  onDone: () => void;
  onClose: () => void;
}) {
  const workspace = useQuery({
    queryKey: ["workspace"],
    queryFn: api.workspace,
    staleTime: 60_000,
  });
  const plan = workspace.data?.limits?.objectStorage;
  const have = workspace.data?.usage?.buckets ?? 0;
  const full = !!plan?.buckets && have + 1 > plan.buckets;
  const planCap = plan?.bucketCap ? gbOf(plan.bucketCap) : undefined;
  const [name, setName] = useState("uploads");
  const [size, setSize] = useState("");
  const [isPublic, setPublic] = useState(false);
  const [retention, setRetention] = useState("0");
  const asked = size.trim() ? gbOf(size.trim()) : undefined;
  const tooBig = !!planCap && !!asked && asked > planCap;
  const save = useMutation({
    mutationFn: () => {
      const spec: Record<string, unknown> = {};
      if (size.trim()) spec.maxBytes = size.trim();
      if (isPublic) spec.public = true;
      if (retention !== "0") spec.retentionDays = Number(retention);
      return api.createResource(slug, {
        kind: "ObjectBucket",
        name: name.trim(),
        spec,
      });
    },
    onSuccess: (r) => {
      toast.success(`Making bucket ${name.trim()}`, {
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
          <DialogTitle>New bucket</DialogTitle>
          <DialogDescription>
            S3 storage for the app&apos;s files, with a key that opens this
            bucket and nothing else. Attached, it puts BUCKET_ACCESS_KEY_ID,
            BUCKET_ENDPOINT_URL and friends in the config vars; with the
            prefix AWS, the names the SDKs read.
          </DialogDescription>
        </DialogHeader>
        {full && (
          <Alert variant="upsell">
            <AlertTitle>
              This would bring the workspace to {have + 1} of {plan?.buckets}{" "}
              buckets.
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
            if (name.trim() && !full && !tooBig) save.mutate();
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
            <Field
              label="Holds up to"
              hint={
                tooBig
                  ? `A bucket holds up to ${gbWords(planCap!)}.`
                  : planCap
                    ? `The plan's cap per bucket, ${gbWords(planCap)}, unless you ask for less.`
                    : "No cap unless you set one."
              }
            >
              <Input
                value={size}
                onChange={(e) => setSize(e.target.value)}
                placeholder={plan?.bucketCap ?? "no cap"}
                className="font-mono text-xs"
                aria-invalid={tooBig || undefined}
              />
            </Field>
          </div>
          <div className="grid gap-4 sm:grid-cols-2">
            <Field
              label="Objects are kept"
              hint="Objects older than this expire on their own."
            >
              <Select value={retention} onValueChange={setRetention}>
                <SelectTrigger className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {[
                    ["0", "Until deleted"],
                    ["7", "7 days"],
                    ["30", "30 days"],
                    ["90", "90 days"],
                    ["365", "A year"],
                  ].map(([value, words]) => (
                    <SelectItem key={value} value={value}>
                      {words}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
            <Field
              label="Public read"
              hint="Anyone with an object's address can read it; listing and writing still need the key."
            >
              <Select
                value={isPublic ? "public" : "private"}
                onValueChange={(v) => setPublic(v === "public")}
              >
                <SelectTrigger className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="private">
                    Private · a key, or a presigned address
                  </SelectItem>
                  <SelectItem value="public">
                    Public · an object by its address
                  </SelectItem>
                </SelectContent>
              </Select>
            </Field>
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={onClose}>
              Cancel
            </Button>
            <Button
              type="submit"
              disabled={full || tooBig || !name.trim() || save.isPending}
            >
              Make it
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
                          : instances === "1"
                            ? "One instance pauses for about 40 seconds when its machine is replaced; a second avoids it."
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
