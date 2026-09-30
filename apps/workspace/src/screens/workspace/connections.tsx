"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Trash2 } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@shpyrd/ui/components/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@shpyrd/ui/components/card";
import { ConfirmDialog } from "@shpyrd/ui/components/confirm-dialog";
import { InlineCode } from "@shpyrd/ui/components/inline-code";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@shpyrd/ui/components/table";
import { api } from "@/api/api";
import { usePerms } from "@/lib/perms";
import { ago } from "@/lib/project";
import { Failed, Loading, when } from "../project/shared";

// The software connected to the workspace as a person: an assistant
// through MCP, a pipeline. A person sees their own; an admin, everyone's.
// Cutting one does not touch the person.
export function Connections() {
  const perms = usePerms();
  const queries = useQueryClient();
  const connections = useQuery({ queryKey: ["connections"], queryFn: api.connections });
  const revoke = useMutation({
    mutationFn: (id: string) => api.revokeConnection(id),
    onSuccess: () => {
      queries.invalidateQueries({ queryKey: ["connections"] });
      toast.success("Connection cut");
    },
    onError: (e: Error) => toast.error(e.message),
  });
  return (
    <Card>
      <CardHeader>
        <CardTitle>Connections</CardTitle>
        <CardDescription>
          {perms.admin ? "Every assistant and pipeline connected to the workspace as one of its people." : "The assistants and pipelines connected as you."} Cutting one does not touch the person.
        </CardDescription>
      </CardHeader>
      <CardContent>
        {connections.isLoading ? (
          <Loading />
        ) : connections.error ? (
          <Failed what="the connections" error={connections.error} />
        ) : (connections.data ?? []).length === 0 ? (
          <p className="text-sm text-muted-foreground">None. An assistant connects through MCP, with the address under MCP.</p>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Client</TableHead>
                <TableHead>As</TableHead>
                <TableHead>May</TableHead>
                <TableHead>Last used</TableHead>
                <TableHead>Until</TableHead>
                <TableHead className="text-right" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {connections.data!.map((c) => (
                <TableRow key={c.id}>
                  <TableCell className="font-medium">{c.client}</TableCell>
                  <TableCell>{c.email}</TableCell>
                  <TableCell>
                    <InlineCode>{c.scope}</InlineCode>
                  </TableCell>
                  <TableCell className="text-xs text-muted-foreground">{c.lastUsedAt ? ago(c.lastUsedAt) : "never"}</TableCell>
                  <TableCell className="text-xs text-muted-foreground">{when(c.expiresAt)}</TableCell>
                  <TableCell className="text-right">
                    <ConfirmDialog
                      trigger={<Button variant="ghost" size="icon-xs" icon={<Trash2 />} aria-label={`Cut ${c.client}`} />}
                      variant="destructive"
                      title={`Cut ${c.client}?`}
                      description="It stops acting for the person within the hour."
                      action="Cut"
                      onConfirm={() => revoke.mutate(c.id)}
                    />
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  );
}
