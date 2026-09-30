"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@shpyrd/ui/components/card";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@shpyrd/ui/components/select";
import { api } from "@/api/api";
import { Loading } from "./shared";

// The platform's own knobs: which of the operator's workspaces is the
// default, the one commands over a kubeconfig act on and whose owners
// and admins are the platform admins.
export function Settings() {
  const queries = useQueryClient();
  const config = useQuery({ queryKey: ["config"], queryFn: api.config, staleTime: 60_000 });
  const list = useQuery({ queryKey: ["workspaces"], queryFn: api.workspaces });
  const setDefault = useMutation({
    mutationFn: (defaultWorkspaceId: string) => api.patchSettings({ defaultWorkspaceId }),
    onSuccess: (r) => {
      toast.success(`${r.defaultWorkspaceId} is the default workspace`);
      queries.invalidateQueries({ queryKey: ["config"] });
    },
    onError: (e: Error) => toast.error(e.message),
  });
  const own = (list.data ?? []).filter((w) => w.owner === "operator");
  return (
    <Card>
      <CardHeader>
        <CardTitle>Default workspace</CardTitle>
        <CardDescription>One of your workspaces: its owners and admins are this console's platform admins, and commands over a kubeconfig act on its projects. Sizes are under Cluster; its config vars and log drains are on its own pages, as every workspace's.</CardDescription>
      </CardHeader>
      <CardContent>
        {list.isLoading ? (
          <Loading rows={1} />
        ) : (
          <Select value={config.data?.defaultWorkspaceId ?? ""} disabled={setDefault.isPending || own.length < 2} onValueChange={(v) => setDefault.mutate(v)}>
            <SelectTrigger className="w-80">
              <SelectValue placeholder="Choose a workspace" />
            </SelectTrigger>
            <SelectContent>
              {own.map((w) => (
                <SelectItem key={w.slug} value={w.slug}>
                  {w.name} <span className="ml-2 font-mono text-xs text-muted-foreground">{w.address}</span>
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        )}
        {own.length < 2 && !list.isLoading && <p className="mt-2 text-xs text-muted-foreground">With one workspace of your own there is nothing to choose.</p>}
      </CardContent>
    </Card>
  );
}
