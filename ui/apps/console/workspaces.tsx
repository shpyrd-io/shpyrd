import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Building2, ExternalLink, Plus } from "lucide-react";
import { toast } from "sonner";
import { api, type WorkspaceSummary } from "@/lib/api";
import { ago } from "@/lib/format";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

/**
 * The workspaces this platform hosts (RFC-0033 phase 8, RFC-0080): the
 * operator's own and the customers'. Each is a door of its own, at its
 * address; the console only lists them and, on a platform with the
 * workspaces capability, creates them.
 */
export function WorkspacesPage() {
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
  const [creating, setCreating] = useState(false);
  const def = config.data?.defaultWorkspaceId;

  return (
    <div className="grid gap-6">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="flex items-center gap-2 text-2xl font-semibold">
            <Building2 className="size-6" /> Workspaces
          </h1>
          <p className="text-sm text-muted-foreground">
            Every workspace answers at its own address, with its own people,
            sign-in methods and projects. Yours are marked operator: their costs
            are the platform's and they are never invoiced.
          </p>
        </div>
        {canCreate && (
          <Button onClick={() => setCreating(true)}>
            <Plus /> New workspace
          </Button>
        )}
      </div>
      <Card>
        <CardContent className="pt-6">
          {list.isLoading && <Skeleton className="h-24 w-full" />}
          {list.error && (
            <p className="text-sm text-destructive">
              {(list.error as Error).message}
            </p>
          )}
          {list.data && (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Workspace</TableHead>
                  <TableHead>Address</TableHead>
                  <TableHead>Owner</TableHead>
                  <TableHead>Plan</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Created</TableHead>
                  <TableHead className="w-20" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {list.data.map((w) => (
                  <WorkspaceRow key={w.slug} w={w} isDefault={w.slug === def} />
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
      {!canCreate && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">One workspace</CardTitle>
            <CardDescription>
              The open-source platform hosts the workspace created at install.
              Hosting many, each with its own address and plan, is the cloud
              layer&apos;s capability.
            </CardDescription>
          </CardHeader>
        </Card>
      )}
      {creating && <CreateWorkspaceDialog onClose={() => setCreating(false)} />}
    </div>
  );
}

function WorkspaceRow({
  w,
  isDefault,
}: {
  w: WorkspaceSummary;
  isDefault: boolean;
}) {
  return (
    <TableRow>
      <TableCell>
        <div className="font-medium">
          {w.name}{" "}
          {isDefault && (
            <Badge variant="secondary" className="ml-1">
              default
            </Badge>
          )}
        </div>
        <div className="font-mono text-xs text-muted-foreground">{w.slug}</div>
      </TableCell>
      <TableCell className="font-mono text-xs">{w.address ?? "—"}</TableCell>
      <TableCell className="text-sm">
        {w.owner === "operator" ? (
          <Badge variant="outline">operator</Badge>
        ) : (
          <span className="text-muted-foreground">
            {w.owners.length > 0 ? w.owners.join(", ") : "customer"}
          </span>
        )}
      </TableCell>
      <TableCell className="text-sm">
        {w.owner === "operator" ? (
          <span className="text-muted-foreground">never invoiced</span>
        ) : w.plan ? (
          <span className="font-mono text-xs">{w.plan}</span>
        ) : (
          <span className="text-amber-600">no plan</span>
        )}
      </TableCell>
      <TableCell>
        <Badge variant={w.status === "active" ? "secondary" : "destructive"}>
          {w.status}
        </Badge>
      </TableCell>
      <TableCell className="text-xs text-muted-foreground">
        {ago(w.createdAt)}
      </TableCell>
      <TableCell>
        {w.url && (
          <Button variant="ghost" size="sm" asChild>
            <a href={w.url} target="_blank" rel="noreferrer">
              Open <ExternalLink data-icon="inline-end" />
            </a>
          </Button>
        )}
      </TableCell>
    </TableRow>
  );
}

function CreateWorkspaceDialog({ onClose }: { onClose: () => void }) {
  const qc = useQueryClient();
  const [slug, setSlug] = useState("");
  const [name, setName] = useState("");
  const [owner, setOwner] = useState("");
  const [operator, setOperator] = useState(false);
  const [plan, setPlan] = useState<string | undefined>(undefined);
  const plans = useQuery({ queryKey: ["plans"], queryFn: api.plans });
  // A customer workspace is priced from birth: the first plan is
  // preselected when the operator has defined any.
  const chosenPlan =
    plan ?? (plans.data && plans.data.length > 0 ? plans.data[0].name : "");
  const create = useMutation({
    mutationFn: () =>
      api.createWorkspace({
        slug: slug.trim(),
        name: name.trim() || undefined,
        owner: operator ? undefined : owner.trim(),
        operatorOwned: operator,
        plan: operator || !chosenPlan ? undefined : chosenPlan,
      }),
    onSuccess: (w) => {
      toast.success(`Workspace ${w.slug} created at ${w.address}`);
      qc.invalidateQueries({ queryKey: ["workspaces"] });
      onClose();
    },
    onError: (e: Error) => toast.error(e.message),
  });
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>New workspace</DialogTitle>
          <DialogDescription>
            It answers at <code>{slug || "<slug>"}.</code>the workspaces domain
            within a minute, with its front door and certificate. A
            customer&apos;s workspace needs its first owner; one of yours is
            owned by every platform admin.
          </DialogDescription>
        </DialogHeader>
        <div className="grid gap-3">
          <div className="grid gap-1.5">
            <Label htmlFor="ws-slug">Slug</Label>
            <Input
              id="ws-slug"
              value={slug}
              onChange={(e) => setSlug(e.target.value.toLowerCase())}
              placeholder="acme"
            />
          </div>
          <div className="grid gap-1.5">
            <Label htmlFor="ws-name">Name</Label>
            <Input
              id="ws-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="Acme Corp"
            />
          </div>
          <label className="flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              checked={operator}
              onChange={(e) => setOperator(e.target.checked)}
            />
            One of the operator&apos;s own workspaces
          </label>
          {!operator && (
            <div className="grid gap-1.5">
              <Label htmlFor="ws-owner">First owner (email)</Label>
              <Input
                id="ws-owner"
                type="email"
                value={owner}
                onChange={(e) => setOwner(e.target.value)}
                placeholder="ana@acme.com"
              />
            </div>
          )}
          {!operator && (
            <div className="grid gap-1.5">
              <Label htmlFor="ws-plan">Billing plan</Label>
              {plans.data && plans.data.length > 0 ? (
                <Select value={chosenPlan} onValueChange={setPlan}>
                  <SelectTrigger id="ws-plan">
                    <SelectValue placeholder="Pick a plan" />
                  </SelectTrigger>
                  <SelectContent>
                    {plans.data.map((p) => (
                      <SelectItem key={p.id} value={p.name}>
                        {p.name}
                        <span className="ml-2 text-xs text-muted-foreground">
                          min {p.minMonthly.toFixed(2)} {p.currency}/mo
                        </span>
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              ) : (
                <p className="text-xs text-muted-foreground">
                  No billing plans yet: usage will not be priced until one is
                  created (<code>shpyrd-ctl plans create</code>) and assigned.
                </p>
              )}
            </div>
          )}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            Cancel
          </Button>
          <Button
            disabled={
              create.isPending || !slug.trim() || (!operator && !owner.trim())
            }
            onClick={() => create.mutate()}
          >
            Create
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
