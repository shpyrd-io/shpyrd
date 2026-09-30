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
import type { InstanceSize, SizeCatalog } from "@/api/types";
import { usePerms } from "@/lib/perms";
import { Failed, Loading } from "./shared";

const nameRe = /^[a-z0-9]([-a-z0-9]{0,30}[a-z0-9])?$/;
const cpuRe = /^\d+(\.\d+)?m?$/;
const memoryRe = /^\d+(\.\d+)?(Ki|Mi|Gi|Ti|K|M|G|T)?$/;

// The named allocations a process runs with. Each is edited in a form
// into a draft; the draft is saved as one, and takes hold on every
// process using a changed size as soon as it is.
export function Sizes() {
  const perms = usePerms();
  const queries = useQueryClient();
  const catalog = useQuery({ queryKey: ["sizes"], queryFn: api.sizes });
  // The edits in hand; null means none yet: the server's copy is shown.
  const [edits, setEdits] = useState<SizeCatalog | null>(null);
  const [editing, setEditing] = useState<{ index: number | null } | null>(null);
  const draft = edits ?? catalog.data ?? null;
  const save = useMutation({
    mutationFn: (c: SizeCatalog) => api.saveSizes(c),
    onSuccess: (c) => {
      toast.success("Sizes saved", {
        description: "The processes on a changed size are being resized.",
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
  const changed = (s: InstanceSize) => JSON.stringify(saved.sizes.find((x) => x.name === s.name) ?? null) !== JSON.stringify(s);
  const gone = saved.sizes.filter((s) => !draft.sizes.some((x) => x.name === s.name));
  const invalid = !draft.sizes.some((s) => s.name === draft.default);
  const put = (size: InstanceSize, at: number | null) =>
    setEdits({
      ...draft,
      sizes: at === null ? [...draft.sizes, size] : draft.sizes.map((s, i) => (i === at ? size : s)),
    });
  return (
    <Card>
      <CardHeader>
        <CardTitle>Instances</CardTitle>
        <CardDescription>
          Named CPU and memory allocations a process runs with. For a shared size the CPU is a ceiling: an eighth of it is guaranteed and the rest is borrowed from idle neighbours, so many small instances fit on a node. A dedicated size
          gets whole cores, requests equal to limits. Memory is never overcommitted.
        </CardDescription>
        {!readOnly && (
          <CardAction>
            <Stack direction="horizontal" gap="condensed">
              <Button size="sm" variant="outline" icon={<Plus />} onClick={() => setEditing({ index: null })}>
                Size
              </Button>
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
        <Stack gap="normal">
          {dirty && (
            <p className="text-sm text-muted-foreground">
              Not saved yet:{" "}
              {[
                draft.sizes.filter((s) => !saved.sizes.some((x) => x.name === s.name)).length && `${draft.sizes.filter((s) => !saved.sizes.some((x) => x.name === s.name)).length} added`,
                draft.sizes.filter((s) => saved.sizes.some((x) => x.name === s.name) && changed(s)).length && `${draft.sizes.filter((s) => saved.sizes.some((x) => x.name === s.name) && changed(s)).length} changed`,
                gone.length && `${gone.length} removed`,
                draft.default !== saved.default && "another default",
              ]
                .filter(Boolean)
                .join(", ")}
              .
            </p>
          )}
          <div className="overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="w-16">Default</TableHead>
                  <TableHead>Name</TableHead>
                  <TableHead>Kind</TableHead>
                  <TableHead>CPU (cores)</TableHead>
                  <TableHead>Memory</TableHead>
                  <TableHead>What it is for</TableHead>
                  <TableHead className="text-right" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {draft.sizes.map((s, i) => (
                  <TableRow key={s.name || i} data-state={changed(s) ? "selected" : undefined}>
                    <TableCell>
                      <input
                        type="radio"
                        name="default-size"
                        checked={draft.default === s.name}
                        onChange={() => setEdits({ ...draft, default: s.name })}
                        disabled={readOnly}
                        aria-label={`Make ${s.name} the default`}
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
                    <TableCell className="max-w-56 truncate text-sm text-muted-foreground" title={s.description}>
                      {s.description || "-"}
                    </TableCell>
                    <TableCell className="text-right">
                      {!readOnly && (
                        <Stack direction="horizontal" gap="tight" justify="end">
                          <Button size="icon-xs" variant="ghost" icon={<Pencil />} aria-label={`Edit ${s.name}`} onClick={() => setEditing({ index: i })} />
                          <Button
                            size="icon-xs"
                            variant="ghost"
                            icon={<Trash2 />}
                            aria-label={`Remove ${s.name}`}
                            disabled={draft.default === s.name}
                            title={draft.default === s.name ? "Choose another default first" : undefined}
                            onClick={() =>
                              setEdits({
                                ...draft,
                                sizes: draft.sizes.filter((_, j) => j !== i),
                              })
                            }
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
                    <TableCell colSpan={4} className="text-sm text-muted-foreground">
                      Gone when saved; the processes on it are resized to the default.
                    </TableCell>
                    <TableCell className="text-right">
                      <Button size="xs" variant="ghost" onClick={() => setEdits({ ...draft, sizes: [...draft.sizes, s] })}>
                        Keep it
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
          <p className="text-xs text-muted-foreground">
            A process picks a size in <InlineCode>shpyrd.yaml</InlineCode>, with <InlineCode>shpyrd resize web=shared-m</InlineCode>, or on its project's Resources page. Without one it gets the default.
          </p>
        </Stack>
      </CardContent>
      {editing && (
        <SizeDialog
          size={editing.index === null ? undefined : draft.sizes[editing.index]}
          taken={draft.sizes.map((s) => s.name).filter((_, j) => j !== editing.index)}
          onClose={() => setEditing(null)}
          onDone={(size) => put(size, editing.index)}
        />
      )}
    </Card>
  );
}

// One size, in a form: made, or changed. It goes into the draft, not
// to the server.
function SizeDialog({ size, taken, onClose, onDone }: { size?: InstanceSize; taken: string[]; onClose: () => void; onDone: (size: InstanceSize) => void }) {
  const [name, setName] = useState(size?.name ?? "");
  const [kind, setKind] = useState<InstanceSize["kind"]>(size?.kind ?? "shared");
  const [cpu, setCpu] = useState(size?.cpu ?? "0.5");
  const [memory, setMemory] = useState(size?.memory ?? "512Mi");
  const [description, setDescription] = useState(size?.description ?? "");
  const nameError = name === "" ? undefined : !nameRe.test(name) ? "Lowercase letters, digits and dashes, up to 32." : taken.includes(name) ? "There is a size with that name." : undefined;
  const cpuError = cpu !== "" && !cpuRe.test(cpu) ? "A number of cores, such as 0.5 or 2, or millicores such as 250m." : undefined;
  const memoryError = memory !== "" && !memoryRe.test(memory) ? "A quantity such as 512Mi or 4Gi." : undefined;
  const valid = name !== "" && cpu !== "" && memory !== "" && !nameError && !cpuError && !memoryError;
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader divider>
          <DialogTitle>{size ? `The size ${size.name}` : "New size"}</DialogTitle>
          <DialogDescription>{size ? "The change goes into the draft; Save puts it on the cluster, and resizes the processes on it." : "It goes into the draft; Save puts it on the cluster."}</DialogDescription>
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
            <Field label="Memory" error={memoryError}>
              <Input value={memory} onChange={(e) => setMemory(e.target.value)} placeholder="512Mi" className="font-mono text-xs" />
            </Field>
          </div>
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
