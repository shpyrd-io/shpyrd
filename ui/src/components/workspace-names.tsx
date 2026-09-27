import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Globe, ShieldCheck, Star, Trash2 } from "lucide-react";

import { api, type WorkspaceDomain, type WorkspaceInfo } from "@/lib/api";
import { usePerms } from "@/lib/me";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
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
 * The workspace's names (RFC-0033): its address under the platform's
 * workspaces domain, which owners may change (the old one redirects for
 * thirty days), and the company's own domains in CNAME mode.
 */

/** The address editor: <label>.<parent>, owners only. */
export function AddressField({ ws }: { ws: WorkspaceInfo }) {
  const qc = useQueryClient();
  const perms = usePerms();
  const address = ws.address ?? "";
  const dot = address.indexOf(".");
  const currentLabel = dot > 0 ? address.slice(0, dot) : address;
  const parent = dot > 0 ? address.slice(dot + 1) : "";
  const [label, setLabel] = useState(currentLabel);
  useEffect(() => setLabel(currentLabel), [currentLabel]);
  const move = useMutation({
    mutationFn: (l: string) => api.updateWorkspace({ address: l }),
    onSuccess: (w) => {
      toast.success(`The workspace answers at ${w.address} now`);
      qc.invalidateQueries({ queryKey: ["workspace"] });
      // This page is on the old host: follow the workspace.
      if (w.url)
        window.setTimeout(
          () => window.location.assign(w.url + "/workspace"),
          800,
        );
    },
    onError: (e: Error) => toast.error(e.message),
  });
  if (!address) return null;
  const dirty = label.trim() !== currentLabel;
  const valid = /^[a-z0-9]([-a-z0-9]{0,22}[a-z0-9])?$/.test(label.trim());
  return (
    <div className="grid gap-2 sm:max-w-md">
      <Label htmlFor="ws-address">Address</Label>
      <div className="flex items-center gap-2">
        <Input
          id="ws-address"
          value={label}
          disabled={!perms.owner}
          maxLength={24}
          className="font-mono text-xs"
          onChange={(e) => setLabel(e.target.value.toLowerCase())}
          onKeyDown={(e) => {
            if (e.key === "Enter" && dirty && valid)
              confirmMove(label.trim(), parent, move.mutate);
          }}
        />
        <span className="shrink-0 font-mono text-xs text-muted-foreground">
          .{parent}
        </span>
        {perms.owner && (
          <Button
            size="sm"
            disabled={!dirty || !valid || move.isPending}
            onClick={() => confirmMove(label.trim(), parent, move.mutate)}
          >
            Move
          </Button>
        )}
      </div>
      <p className="text-xs text-muted-foreground">
        {perms.owner
          ? "Owners may change it. The old address redirects to the new one for thirty days; apps move with it."
          : "Owners may change it."}
      </p>
    </div>
  );
}

function confirmMove(label: string, parent: string, go: (l: string) => void) {
  if (
    window.confirm(
      `Move the workspace to ${label}.${parent}? Every app moves to <app>.${label}.${parent}; the old address redirects for thirty days. People signed in here sign in again at the new address.`,
    )
  )
    go(label);
}

export function DomainsCard() {
  const qc = useQueryClient();
  const perms = usePerms();
  const ws = useQuery({ queryKey: ["workspace"], queryFn: api.workspace });
  const domains = useQuery({
    queryKey: ["workspace-domains"],
    queryFn: api.workspaceDomains,
    retry: false,
  });
  const refresh = () => {
    qc.invalidateQueries({ queryKey: ["workspace-domains"] });
    qc.invalidateQueries({ queryKey: ["workspace"] });
  };
  const [host, setHost] = useState("");
  const add = useMutation({
    mutationFn: () => api.addWorkspaceDomain(host.trim()),
    onSuccess: () => {
      toast.success("Domain added; publish the DNS records, then verify");
      setHost("");
      refresh();
    },
    onError: (e: Error) => toast.error(e.message),
  });
  const verify = useMutation({
    mutationFn: (d: WorkspaceDomain) => api.verifyWorkspaceDomain(d.host),
    onSuccess: (d) => {
      toast.success(
        `${d.host} verified: the dashboard and every app answer there in a few minutes, once certificates are issued`,
      );
      refresh();
    },
    onError: (e: Error) => toast.error(e.message),
  });
  const primary = useMutation({
    mutationFn: (d: WorkspaceDomain) =>
      api.setWorkspaceDomainPrimary(d.host, !d.primary),
    onSuccess: (d) => {
      toast.success(
        d.primary
          ? `${d.host} is the primary domain now`
          : `${d.host} is an alias again`,
      );
      refresh();
    },
    onError: (e: Error) => toast.error(e.message),
  });
  const remove = useMutation({
    mutationFn: (d: WorkspaceDomain) => api.removeWorkspaceDomain(d.host),
    onSuccess: refresh,
    onError: (e: Error) => toast.error(e.message),
  });
  if (ws.data && (ws.data.implicit || !ws.data.address)) return null;

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Globe className="size-4" /> Custom domains
        </CardTitle>
        <CardDescription>
          Serve the workspace and its apps under a name your company owns, such
          as <code className="text-xs">intranet.acme.com</code>: the apps become{" "}
          <code className="text-xs">expenses.intranet.acme.com</code>. Point the
          name and its wildcard at{" "}
          <code className="text-xs">{ws.data?.address}</code> with CNAME
          records, publish the TXT record to prove it, verify, and certificates
          are issued for every host. Make it primary and the dashboard and app
          links use it; the address keeps answering.
        </CardDescription>
      </CardHeader>
      <CardContent className="grid gap-4">
        {domains.isLoading && <Skeleton className="h-12 w-full" />}
        {domains.error && (
          <p className="text-sm text-destructive">
            {(domains.error as Error).message}
          </p>
        )}
        {domains.data && domains.data.length > 0 && (
          <div className="grid gap-4">
            {domains.data.map((d) => (
              <div key={d.host} className="rounded-md border p-3">
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <div className="flex items-center gap-2">
                    <span className="font-mono text-sm">{d.host}</span>
                    {d.verified ? (
                      <Badge variant="secondary">
                        <ShieldCheck data-icon="inline-start" /> verified
                      </Badge>
                    ) : (
                      <Badge variant="outline">waiting for DNS</Badge>
                    )}
                    {d.primary && (
                      <Badge>
                        <Star data-icon="inline-start" /> primary
                      </Badge>
                    )}
                  </div>
                  <div className="flex items-center gap-1">
                    {!d.verified && (
                      <Button
                        size="xs"
                        variant="outline"
                        disabled={verify.isPending}
                        onClick={() => verify.mutate(d)}
                      >
                        Verify
                      </Button>
                    )}
                    {d.verified && perms.owner && (
                      <Button
                        size="xs"
                        variant="outline"
                        disabled={primary.isPending}
                        onClick={() => primary.mutate(d)}
                      >
                        {d.primary ? "Make it an alias" : "Make it primary"}
                      </Button>
                    )}
                    <Button
                      variant="ghost"
                      size="icon"
                      aria-label={`Remove ${d.host}`}
                      disabled={remove.isPending}
                      onClick={() => {
                        if (
                          window.confirm(
                            `Remove ${d.host}? Its hosts stop answering.`,
                          )
                        )
                          remove.mutate(d);
                      }}
                    >
                      <Trash2 className="size-4" />
                    </Button>
                  </div>
                </div>
                <Table className="mt-3">
                  <TableHeader>
                    <TableRow>
                      <TableHead className="w-20">Type</TableHead>
                      <TableHead>Name</TableHead>
                      <TableHead>Value</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {d.records.map((r) => (
                      <TableRow key={r.type + r.name}>
                        <TableCell className="font-mono text-xs">
                          {r.type}
                        </TableCell>
                        <TableCell className="font-mono text-xs">
                          {r.name}
                        </TableCell>
                        <TableCell className="font-mono text-xs break-all select-all">
                          {r.value}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>
            ))}
          </div>
        )}
        <form
          className="flex flex-wrap items-center gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            if (host.trim()) add.mutate();
          }}
        >
          <Input
            placeholder="intranet.acme.com"
            value={host}
            onChange={(e) => setHost(e.target.value)}
            className="h-8 w-72 font-mono text-xs"
          />
          <Button
            type="submit"
            size="sm"
            disabled={!host.trim() || add.isPending}
          >
            Add domain
          </Button>
        </form>
      </CardContent>
    </Card>
  );
}
