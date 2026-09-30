"use client";

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Copy, ExternalLink, Plus } from "lucide-react";
import { toast } from "sonner";
import { Badge } from "@shpyrd/ui/components/badge";
import { Button } from "@shpyrd/ui/components/button";
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@shpyrd/ui/components/card";
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from "@shpyrd/ui/components/dialog";
import { Field } from "@shpyrd/ui/components/field";
import { InlineCode } from "@shpyrd/ui/components/inline-code";
import { Input } from "@shpyrd/ui/components/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@shpyrd/ui/components/select";
import { Stack } from "@shpyrd/ui/components/stack";
import { StatusBadge } from "@shpyrd/ui/components/status-badge";
import { Switch } from "@shpyrd/ui/components/switch";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@shpyrd/ui/components/table";
import { api } from "@/api/api";
import { ago, Failed, Loading } from "./shared";

// The workspaces the platform hosts: the operator's own and the
// customers'. Each is a door of its own, at its address; the console
// lists them and, with the capability, makes them.
export function Workspaces() {
  const config = useQuery({
    queryKey: ["config"],
    queryFn: api.config,
    staleTime: 60_000,
  });
  const list = useQuery({
    queryKey: ["workspaces"],
    queryFn: api.workspaces,
    refetchInterval: 30_000,
  });
  const canCreate = !!config.data?.capabilities?.includes("workspaces");
  const priced = !!config.data?.capabilities?.includes("billing");
  const def = config.data?.defaultWorkspaceId;
  return (
    <>
      <Card>
        <CardHeader>
          <CardTitle>Workspaces</CardTitle>
          <CardDescription>Every workspace answers at its own address, with its own people, sign-in methods and projects. Yours are marked operator: their costs are the platform's, and they are never invoiced.</CardDescription>
          {canCreate && (
            <CardAction>
              <NewWorkspace />
            </CardAction>
          )}
        </CardHeader>
        <CardContent>
          {list.isLoading ? (
            <Loading />
          ) : list.error ? (
            <Failed what="the workspaces" error={list.error} />
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Workspace</TableHead>
                  <TableHead>Address</TableHead>
                  <TableHead>Owner</TableHead>
                  {priced && <TableHead>Plan</TableHead>}
                  <TableHead>State</TableHead>
                  <TableHead>Made</TableHead>
                  <TableHead className="text-right" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {list.data!.map((w) => (
                  <TableRow key={w.slug}>
                    <TableCell>
                      <div className="font-medium">
                        {w.name}
                        {w.slug === def && (
                          <Badge variant="secondary" className="ml-2">
                            default
                          </Badge>
                        )}
                      </div>
                      <div className="font-mono text-xs text-muted-foreground">{w.slug}</div>
                    </TableCell>
                    <TableCell className="font-mono text-xs">{w.address ?? "-"}</TableCell>
                    <TableCell className="text-sm">{w.owner === "operator" ? <Badge variant="outline">operator</Badge> : <span className="text-muted-foreground">{w.owners.length ? w.owners.join(", ") : "customer"}</span>}</TableCell>
                    {priced && (
                      <TableCell className="text-sm">
                        {w.owner === "operator" ? <span className="text-muted-foreground">never invoiced</span> : w.plan ? <InlineCode>{w.plan}</InlineCode> : <StatusBadge type="warning">no plan</StatusBadge>}
                      </TableCell>
                    )}
                    <TableCell>
                      <StatusBadge type={w.status === "active" ? "success" : "error"}>{w.status}</StatusBadge>
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">{ago(w.createdAt)}</TableCell>
                    <TableCell className="text-right">
                      {w.url && (
                        <Button variant="ghost" size="xs" iconEnd={<ExternalLink />} asChild>
                          <a href={w.url} target="_blank" rel="noreferrer">
                            Open
                          </a>
                        </Button>
                      )}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
      {!canCreate && (
        <Card>
          <CardHeader>
            <CardTitle>One workspace</CardTitle>
            <CardDescription>The open-source platform hosts the workspace made at install. Hosting many, each with its own address and plan, is the cloud layer's capability.</CardDescription>
          </CardHeader>
        </Card>
      )}
    </>
  );
}

// A workspace made: at a slug under the workspaces domain, for the
// operator or for a customer with a first owner and a plan.
function NewWorkspace() {
  const queries = useQueryClient();
  const [open, setOpen] = useState(false);
  const [slug, setSlug] = useState("");
  const [name, setName] = useState("");
  const [owner, setOwner] = useState("");
  const [operator, setOperator] = useState(false);
  const [plan, setPlan] = useState<string | undefined>(undefined);
  const [link, setLink] = useState("");
  const [setPasswordLink, setSetPasswordLink] = useState("");
  const plans = useQuery({
    queryKey: ["plans"],
    queryFn: api.plans,
    enabled: open,
  });
  // A customer's workspace is priced from birth: the first plan is chosen
  // when the operator has defined any.
  const chosen = plan ?? plans.data?.[0]?.name ?? "";
  const validSlug = /^[a-z0-9-]{2,}$/.test(slug.trim());
  const create = useMutation({
    mutationFn: () =>
      api.createWorkspace({
        slug: slug.trim(),
        name: name.trim() || undefined,
        owner: operator ? undefined : owner.trim(),
        operatorOwned: operator,
        plan: operator || !chosen ? undefined : chosen,
      }),
    onSuccess: (w) => {
      queries.invalidateQueries({ queryKey: ["workspaces"] });
      const inv = w.ownerInvitation;
      if (!inv) {
        toast.success(`${w.slug} made`, {
          description: `It answers at ${w.address}.`,
        });
        return close(false);
      }
      if (inv.error) {
        toast.warning(`${w.slug} made; the owner could not be invited`, {
          description: `${inv.error}. They hold the owner role; invite again with shpyrd-ctl workspaces invite.`,
        });
        return close(false);
      }
      if (inv.emailed || inv.applied) {
        toast.success(`${w.slug} made`, {
          description: inv.applied ? `${owner.trim()} is known to the platform and may sign in now.` : `An invitation went by email to ${owner.trim()}.`,
        });
        return close(false);
      }
      // No mail is set up: the links are shown once, here.
      setLink(inv.link ?? "");
      setSetPasswordLink(inv.setPasswordLink ?? "");
    },
    onError: (e: Error) => toast.error(e.message),
  });
  const close = (next: boolean) => {
    setOpen(next);
    if (!next) {
      setSlug("");
      setName("");
      setOwner("");
      setOperator(false);
      setPlan(undefined);
      setLink("");
      setSetPasswordLink("");
    }
  };
  return (
    <Dialog open={open} onOpenChange={close}>
      <DialogTrigger asChild>
        <Button size="sm" icon={<Plus />}>
          New workspace
        </Button>
      </DialogTrigger>
      <DialogContent>
        {link ? (
          <>
            <DialogHeader divider>
              <DialogTitle>{slug.trim()} made</DialogTitle>
              <DialogDescription>No mail is set up, so nothing went out. Hand this link to {owner.trim()}; it is shown once and opens their workspace, where they set a password or sign in with a method it offers.</DialogDescription>
            </DialogHeader>
            <Input readOnly value={link} className="font-mono text-xs" onFocus={(e) => e.currentTarget.select()} />
            {setPasswordLink && (
              <>
                <p className="text-sm text-muted-foreground">They have no password yet; this link lets them choose one (24 hours):</p>
                <Input readOnly value={setPasswordLink} className="font-mono text-xs" onFocus={(e) => e.currentTarget.select()} />
              </>
            )}
            <DialogFooter>
              <Button
                variant="outline"
                icon={<Copy />}
                onClick={() => {
                  void navigator.clipboard?.writeText(link);
                  toast.success("Copied");
                }}
              >
                Copy the link
              </Button>
              <Button onClick={() => close(false)}>Done</Button>
            </DialogFooter>
          </>
        ) : (
          <>
            <DialogHeader divider>
              <DialogTitle>New workspace</DialogTitle>
              <DialogDescription>It answers at its address within a minute, with its front door and certificate. A customer's workspace needs its first owner; one of yours is owned by every platform admin.</DialogDescription>
            </DialogHeader>
            <Stack gap="normal">
              <Field label="Slug" hint="Lowercase letters, digits and dashes." error={slug && !validSlug ? "Only lowercase letters, digits and dashes." : undefined}>
                <Input value={slug} onChange={(e) => setSlug(e.target.value.toLowerCase())} placeholder="acme" autoFocus />
              </Field>
              <Field label="Name">
                <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="Acme Corp" />
              </Field>
              <div className="flex items-center justify-between gap-3 rounded-md border p-3">
                <div className="grid gap-0.5 text-sm">
                  <span className="font-medium">One of the operator's own</span>
                  <span className="text-xs text-muted-foreground">Its costs are the platform's; every platform admin owns it.</span>
                </div>
                <Switch checked={operator} onCheckedChange={setOperator} aria-label="One of the operator's own" />
              </div>
              {!operator && (
                <>
                  <Field label="First owner" hint="They are invited by email, or given a link to hand over.">
                    <Input type="email" value={owner} onChange={(e) => setOwner(e.target.value)} placeholder="ana@acme.com" />
                  </Field>
                  <Field label="Plan" hint={plans.data && plans.data.length === 0 ? "No plan yet: the usage is not priced until one is made with shpyrd-ctl plans create and assigned." : undefined}>
                    {plans.data && plans.data.length > 0 ? (
                      <Select value={chosen} onValueChange={setPlan}>
                        <SelectTrigger className="w-full">
                          <SelectValue placeholder="Choose a plan" />
                        </SelectTrigger>
                        <SelectContent>
                          {plans.data.map((p) => (
                            <SelectItem key={p.id} value={p.name}>
                              {p.name}{" "}
                              <span className="ml-2 text-xs text-muted-foreground">
                                from {p.minMonthly.toFixed(2)} {p.currency} a month
                              </span>
                            </SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                    ) : (
                      <Input readOnly value="none" className="text-muted-foreground" />
                    )}
                  </Field>
                </>
              )}
            </Stack>
            <DialogFooter>
              <DialogClose asChild>
                <Button variant="outline">Cancel</Button>
              </DialogClose>
              <Button disabled={create.isPending || !validSlug || (!operator && !/^\S+@\S+\.\S+$/.test(owner))} onClick={() => create.mutate()}>
                Make it
              </Button>
            </DialogFooter>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}
