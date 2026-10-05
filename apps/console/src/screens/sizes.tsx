"use client";

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Pencil, Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { Badge } from "@shpyrd/ui/components/badge";
import { Button } from "@shpyrd/ui/components/button";
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@shpyrd/ui/components/card";
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@shpyrd/ui/components/dialog";
import { Field } from "@shpyrd/ui/components/field";
import { InlineCode } from "@shpyrd/ui/components/inline-code";
import { Input } from "@shpyrd/ui/components/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@shpyrd/ui/components/select";
import { Stack } from "@shpyrd/ui/components/stack";
import { StatusBadge } from "@shpyrd/ui/components/status-badge";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@shpyrd/ui/components/table";
import { api } from "@/api/api";
import type { InstanceSize, SizeCatalog, SizeList } from "@/api/types";
import { usePerms } from "@/lib/perms";
import { Failed, Loading } from "./shared";

const nameRe = /^[a-z0-9]([-a-z0-9]{0,30}[a-z0-9])?$/;
const cpuRe = /^\d+(\.\d+)?m?$/;
const memoryRe = /^\d+(\.\d+)?(Ki|Mi|Gi|Ti|K|M|G|T)?$/;

// The three lists of sizes: processes, databases and stores, with the
// same names, each with its own memory and default (#57).
type Which = "processes" | "postgres" | "redis";
const lists: { which: Which; title: string; about: string }[] = [
  { which: "processes", title: "Processes", about: "A process picks a size in shpyrd.yaml, with shpyrd resize web=shared-m, or on its project's Resources page." },
  { which: "postgres", title: "Postgres", about: "A database picks one with shpyrd pg create --size or when it is made on the Resources page, and changes it with shpyrd pg resize. PostgreSQL's settings follow the memory and the connections." },
  { which: "redis", title: "Redis", about: "A store picks one with shpyrd redis create --size or on the Resources page, and changes it with shpyrd redis resize. Its memory ceiling is three quarters of the size's." },
];
const listOf = (c: SizeCatalog, which: Which): SizeList => (which === "processes" ? { default: c.default, sizes: c.sizes } : c[which]);
const withList = (c: SizeCatalog, which: Which, l: SizeList): SizeCatalog => (which === "processes" ? { ...c, default: l.default, sizes: l.sizes } : { ...c, [which]: l });

// The named allocations processes, databases and stores run with. Each
// is edited in a form into a draft; the draft is saved as one, and takes
// hold on everything using a changed size as soon as it is.
export function Sizes() {
  const perms = usePerms();
  const queries = useQueryClient();
  const catalog = useQuery({ queryKey: ["sizes"], queryFn: api.sizes });
  // The edits in hand; null means none yet: the server's copy is shown.
  const [edits, setEdits] = useState<SizeCatalog | null>(null);
  const [editing, setEditing] = useState<{ which: Which; index: number | null } | null>(null);
  const draft = edits ?? catalog.data ?? null;
  const save = useMutation({
    mutationFn: (c: SizeCatalog) => api.saveSizes(c),
    onSuccess: (c) => {
      toast.success("Sizes saved", {
        description: "What runs on a changed size is being resized.",
      });
      queries.setQueryData(["sizes"], c);
      setEdits(null);
    },
    onError: (e: Error) => toast.error(e.message),
  });
  if (catalog.isLoading || !draft) return catalog.error ? <Failed what="the sizes" error={catalog.error} /> : <Loading />;
  const readOnly = !perms.admin;
  const saved = catalog.data!;
  const dirty = JSON.stringify(draft) !== JSON.stringify(saved);
  const invalid = lists.some(({ which }) => !listOf(draft, which).sizes.some((s) => s.name === listOf(draft, which).default));
  return (
    <Card>
      <CardHeader>
        <CardTitle>Instances</CardTitle>
        <CardDescription>
          Named CPU and memory allocations processes, databases and stores run with. Each has a list of its own with the same names, as Heroku's add-ons have plans of their own: a Postgres shared-s is not a process shared-s. For a
          shared size the CPU is a ceiling: an eighth of it is guaranteed and the rest is borrowed from idle neighbours. A dedicated size gets whole cores. Memory is never overcommitted.
        </CardDescription>
        {!readOnly && (
          <CardAction>
            <Stack direction="horizontal" gap="condensed">
              {dirty && (
                <Button size="sm" variant="ghost" disabled={save.isPending} onClick={() => setEdits(null)}>
                  Discard
                </Button>
              )}
              <Button size="sm" disabled={!dirty || invalid || save.isPending} onClick={() => save.mutate(draft)}>
                Save
              </Button>
            </Stack>
          </CardAction>
        )}
      </CardHeader>
      <CardContent>
        <Stack gap="spacious">
          {lists.map(({ which, title, about }) => (
            <SizeTable
              key={which}
              which={which}
              title={title}
              about={about}
              list={listOf(draft, which)}
              saved={listOf(saved, which)}
              readOnly={readOnly}
              onChange={(l) => setEdits(withList(draft, which, l))}
              onEdit={(index) => setEditing({ which, index })}
            />
          ))}
        </Stack>
      </CardContent>
      {editing && (
        <SizeDialog
          which={editing.which}
          size={editing.index === null ? undefined : listOf(draft, editing.which).sizes[editing.index]}
          taken={listOf(draft, editing.which)
            .sizes.map((s) => s.name)
            .filter((_, j) => j !== editing.index)}
          onClose={() => setEditing(null)}
          onDone={(size) => {
            const l = listOf(draft, editing.which);
            setEdits(withList(draft, editing.which, { ...l, sizes: editing.index === null ? [...l.sizes, size] : l.sizes.map((s, i) => (i === editing.index ? size : s)) }));
          }}
        />
      )}
    </Card>
  );
}

// One list: its sizes, its default, what is not saved yet.
function SizeTable({ which, title, about, list, saved, readOnly, onChange, onEdit }: { which: Which; title: string; about: string; list: SizeList; saved: SizeList; readOnly: boolean; onChange: (l: SizeList) => void; onEdit: (index: number | null) => void }) {
  const store = which !== "processes";
  const changed = (s: InstanceSize) => JSON.stringify(saved.sizes.find((x) => x.name === s.name) ?? null) !== JSON.stringify(s);
  const added = list.sizes.filter((s) => !saved.sizes.some((x) => x.name === s.name)).length;
  const edited = list.sizes.filter((s) => saved.sizes.some((x) => x.name === s.name) && changed(s)).length;
  const gone = saved.sizes.filter((s) => !list.sizes.some((x) => x.name === s.name));
  const pending = [added && `${added} added`, edited && `${edited} changed`, gone.length && `${gone.length} removed`, list.default !== saved.default && "another default"].filter(Boolean);
  return (
    <Stack gap="condensed">
      <Stack direction="horizontal" justify="space-between" align="center">
        <h3 className="text-sm font-semibold">{title}</h3>
        {!readOnly && (
          <Button size="xs" variant="outline" icon={<Plus />} onClick={() => onEdit(null)}>
            Size
          </Button>
        )}
      </Stack>
      <p className="text-xs text-muted-foreground">
        {about} Without one it gets the default.
        {pending.length > 0 && <span className="text-foreground"> Not saved yet: {pending.join(", ")}.</span>}
      </p>
      <div className="overflow-x-auto">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-16">Default</TableHead>
              <TableHead>Name</TableHead>
              <TableHead>Kind</TableHead>
              <TableHead>CPU (cores)</TableHead>
              <TableHead>Memory</TableHead>
              {store && <TableHead>Connections</TableHead>}
              <TableHead>What it is for</TableHead>
              <TableHead className="text-right" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {list.sizes.map((s, i) => (
              <TableRow key={s.name || i} data-state={changed(s) ? "selected" : undefined}>
                <TableCell>
                  <input
                    type="radio"
                    name={`default-size-${which}`}
                    checked={list.default === s.name}
                    onChange={() => onChange({ ...list, default: s.name })}
                    disabled={readOnly}
                    aria-label={`Make ${s.name} the default of ${which}`}
                    className="accent-primary"
                  />
                </TableCell>
                <TableCell>
                  <InlineCode>{s.name}</InlineCode>
                  {!saved.sizes.some((x) => x.name === s.name) && (
                    <StatusBadge type="info" variant="secondary" className="ml-2">
                      new
                    </StatusBadge>
                  )}
                </TableCell>
                <TableCell>
                  <Badge variant={s.kind === "dedicated" ? "default" : "outline"}>{s.kind}</Badge>
                </TableCell>
                <TableCell className="font-mono text-xs">{s.cpu}</TableCell>
                <TableCell className="font-mono text-xs">{s.memory}</TableCell>
                {store && <TableCell className="font-mono text-xs">{s.connections || "-"}</TableCell>}
                <TableCell className="max-w-56 truncate text-sm text-muted-foreground" title={s.description}>
                  {s.description || "-"}
                </TableCell>
                <TableCell className="text-right">
                  {!readOnly && (
                    <Stack direction="horizontal" gap="tight" justify="end">
                      <Button size="icon-xs" variant="ghost" icon={<Pencil />} aria-label={`Edit ${s.name}`} onClick={() => onEdit(i)} />
                      <Button
                        size="icon-xs"
                        variant="ghost"
                        icon={<Trash2 />}
                        aria-label={`Remove ${s.name}`}
                        disabled={list.default === s.name}
                        title={list.default === s.name ? "Choose another default first" : undefined}
                        onClick={() => onChange({ ...list, sizes: list.sizes.filter((_, j) => j !== i) })}
                      />
                    </Stack>
                  )}
                </TableCell>
              </TableRow>
            ))}
            {gone.map((s) => (
              <TableRow key={`gone-${s.name}`} className="opacity-60">
                <TableCell />
                <TableCell>
                  <InlineCode className="line-through">{s.name}</InlineCode>
                  <StatusBadge type="error" variant="secondary" className="ml-2">
                    removed
                  </StatusBadge>
                </TableCell>
                <TableCell colSpan={store ? 5 : 4} className="text-sm text-muted-foreground">
                  Gone when saved{which === "processes" ? "; the processes on it are resized to the default." : "."}
                </TableCell>
                <TableCell className="text-right">
                  <Button size="xs" variant="ghost" onClick={() => onChange({ ...list, sizes: [...list.sizes, s] })}>
                    Keep it
                  </Button>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
    </Stack>
  );
}

// One size, in a form: made, or changed. It goes into the draft, not
// to the server.
function SizeDialog({ which, size, taken, onClose, onDone }: { which: Which; size?: InstanceSize; taken: string[]; onClose: () => void; onDone: (size: InstanceSize) => void }) {
  const store = which !== "processes";
  const [name, setName] = useState(size?.name ?? "");
  const [connections, setConnections] = useState(size?.connections ? String(size.connections) : "");
  const [kind, setKind] = useState<InstanceSize["kind"]>(size?.kind ?? "shared");
  const [cpu, setCpu] = useState(size?.cpu ?? "0.5");
  const [memory, setMemory] = useState(size?.memory ?? "512Mi");
  const [description, setDescription] = useState(size?.description ?? "");
  const nameError = name === "" ? undefined : !nameRe.test(name) ? "Lowercase letters, digits and dashes, up to 32." : taken.includes(name) ? "There is a size with that name." : undefined;
  const cpuError = cpu !== "" && !cpuRe.test(cpu) ? "A number of cores, such as 0.5 or 2, or millicores such as 250m." : undefined;
  const memoryError = memory !== "" && !memoryRe.test(memory) ? "A quantity such as 512Mi or 4Gi." : undefined;
  const connectionsError = connections !== "" && !/^\d+$/.test(connections) ? "A whole number." : undefined;
  const valid = name !== "" && cpu !== "" && memory !== "" && !nameError && !cpuError && !memoryError && !connectionsError;
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader divider>
          <DialogTitle>{size ? `The ${which === "processes" ? "" : `${which === "postgres" ? "Postgres" : "Redis"} `}size ${size.name}` : `New ${which === "processes" ? "process" : which === "postgres" ? "Postgres" : "Redis"} size`}</DialogTitle>
          <DialogDescription>{size ? "The change goes into the draft; Save puts it on the cluster, and resizes what runs on it." : "It goes into the draft; Save puts it on the cluster."}</DialogDescription>
        </DialogHeader>
        <form
          className="grid gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            if (!valid) return;
            onDone({
              name,
              kind,
              cpu,
              memory,
              description: description.trim() || undefined,
              ...(store && connections ? { connections: Number(connections) } : {}),
            });
            onClose();
          }}
        >
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="Name" hint="Lowercase letters, digits and dashes." error={nameError}>
              <Input value={name} disabled={!!size} onChange={(e) => setName(e.target.value.toLowerCase())} placeholder="shared-m" className="font-mono text-xs" autoFocus={!size} />
            </Field>
            <Field label="Kind" hint={kind === "shared" ? "The CPU is a ceiling; an eighth is guaranteed." : "Whole cores; requests equal to limits."}>
              <Select value={kind} onValueChange={(v) => setKind(v as InstanceSize["kind"])}>
                <SelectTrigger className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="shared">shared</SelectItem>
                  <SelectItem value="dedicated">dedicated</SelectItem>
                </SelectContent>
              </Select>
            </Field>
          </div>
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="CPU" hint="In cores." error={cpuError}>
              <Input value={cpu} onChange={(e) => setCpu(e.target.value)} placeholder="0.5" className="font-mono text-xs" autoFocus={!!size} />
            </Field>
            <Field label="Memory" hint={which === "postgres" ? "At least 128Mi." : undefined} error={memoryError}>
              <Input value={memory} onChange={(e) => setMemory(e.target.value)} placeholder="512Mi" className="font-mono text-xs" />
            </Field>
          </div>
          {store && (
            <Field label="Connections" hint={which === "postgres" ? "PostgreSQL's max_connections; its memory settings are shared among them." : "Redis's maxclients."} error={connectionsError}>
              <Input value={connections} onChange={(e) => setConnections(e.target.value)} placeholder={which === "postgres" ? "40" : "400"} className="font-mono text-xs" />
            </Field>
          )}
          <Field label="What it is for" hint="Optional: a line beside the name where sizes are chosen.">
            <Input value={description} onChange={(e) => setDescription(e.target.value)} placeholder="Small sites and APIs" />
          </Field>
          <DialogFooter>
            <DialogClose asChild>
              <Button type="button" variant="outline">
                Cancel
              </Button>
            </DialogClose>
            <Button type="submit" disabled={!valid}>
              {size ? "Change it" : "Add it"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
