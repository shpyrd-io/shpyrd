import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Settings2 } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/lib/api";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";

/**
 * Cluster settings (RFC-0078, RFC-0080): which of the operator's
 * workspaces is the default — the one project commands over a kubeconfig
 * act on, the one whose owners and admins are the platform admins.
 */
export function ConsoleSettingsPage() {
  const qc = useQueryClient();
  const config = useQuery({
    queryKey: ["config"],
    queryFn: api.config,
    staleTime: 60_000,
  });
  const list = useQuery({ queryKey: ["workspaces"], queryFn: api.workspaces });
  const setDefault = useMutation({
    mutationFn: (defaultWorkspaceId: string) =>
      api.patchClusterSettings({ defaultWorkspaceId }),
    onSuccess: (r) => {
      toast.success(`${r.defaultWorkspaceId} is the default workspace`);
      qc.invalidateQueries({ queryKey: ["config"] });
    },
    onError: (e: Error) => toast.error(e.message),
  });
  const operatorOwned = (list.data ?? []).filter((w) => w.owner === "operator");
  return (
    <div className="grid gap-6">
      <div>
        <h1 className="flex items-center gap-2 text-2xl font-semibold">
          <Settings2 className="size-6" /> Settings
        </h1>
        <p className="text-sm text-muted-foreground">
          The platform&apos;s own knobs. Sizes, global variables and drains are
          on the Cluster page.
        </p>
      </div>
      <Card>
        <CardHeader>
          <CardTitle className="text-base">Default workspace</CardTitle>
          <CardDescription>
            One of your workspaces: its owners and admins are this
            console&apos;s platform admins, and commands over a kubeconfig act
            on its projects.
          </CardDescription>
        </CardHeader>
        <CardContent>
          {list.isLoading ? (
            <Skeleton className="h-9 w-64" />
          ) : (
            <Select
              value={config.data?.defaultWorkspaceId ?? ""}
              disabled={setDefault.isPending || operatorOwned.length < 2}
              onValueChange={(v) => setDefault.mutate(v)}
            >
              <SelectTrigger className="w-72">
                <SelectValue placeholder="Choose a workspace" />
              </SelectTrigger>
              <SelectContent>
                {operatorOwned.map((w) => (
                  <SelectItem key={w.slug} value={w.slug}>
                    {w.name}{" "}
                    <span className="font-mono text-xs text-muted-foreground">
                      {w.address}
                    </span>
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
