import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Bot, Copy, Trash2 } from "lucide-react";

import { api, type Connection } from "@/lib/api";
import { ago } from "@/lib/format";
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
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

/**
 * AI assistants (RFC-0032): the workspace's MCP server. Anyone adds it to
 * Claude as a custom connector; the assistant signs them in through the
 * workspace and asks about their projects — within their roles, read-only
 * for now. Admins name the server; everyone sees and revokes what they
 * connected.
 */
export function MCPCard() {
  const qc = useQueryClient();
  const perms = usePerms();
  const ws = useQuery({ queryKey: ["workspace"], queryFn: api.workspace });
  const connections = useQuery({
    queryKey: ["connections"],
    queryFn: api.connections,
    retry: false,
  });
  const [name, setName] = useState("");
  useEffect(() => setName(ws.data?.mcpName ?? ""), [ws.data?.mcpName]);
  const rename = useMutation({
    mutationFn: (n: string) => api.updateWorkspace({ mcpName: n }),
    onSuccess: () => {
      toast.success(
        "The assistants show the new name at their next connection",
      );
      qc.invalidateQueries({ queryKey: ["workspace"] });
    },
    onError: (e: Error) => toast.error(e.message),
  });
  const revoke = useMutation({
    mutationFn: (c: Connection) => api.revokeConnection(c.id),
    onSuccess: (_, c) => {
      toast.success(`${c.client} disconnected`);
      qc.invalidateQueries({ queryKey: ["connections"] });
    },
    onError: (e: Error) => toast.error(e.message),
  });
  const url = ws.data?.mcpUrl ?? "";
  const dirty = ws.data ? name.trim() !== ws.data.mcpName : false;

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Bot className="size-4" /> AI assistants
        </CardTitle>
        <CardDescription>
          This workspace is an MCP server. Add it to Claude and ask things like
          “check on {ws.data?.mcpName ?? "my workspace"} the metrics of project
          X”: the assistant signs you in through the workspace and sees what you
          see — projects, status, logs and metrics — and changes nothing (for
          now).
        </CardDescription>
      </CardHeader>
      <CardContent className="grid gap-5">
        <div className="grid gap-2 sm:max-w-md">
          <Label htmlFor="mcp-url">Server URL</Label>
          <div className="flex items-center gap-2">
            <Input
              id="mcp-url"
              readOnly
              value={url}
              className="font-mono text-xs"
            />
            <Button
              variant="outline"
              size="icon"
              aria-label="Copy URL"
              onClick={() =>
                navigator.clipboard
                  .writeText(url)
                  .then(() => toast.success("URL copied"))
                  .catch(() => toast.error("Could not copy"))
              }
            >
              <Copy className="size-4" />
            </Button>
          </div>
          <ol className="list-decimal pl-5 text-xs text-muted-foreground">
            <li>
              In Claude: Settings › Connectors › <em>Add custom connector</em>,
              paste the URL, then <em>Connect</em>.
            </li>
            <li>Sign in with your account here and allow the connection.</li>
            <li>
              In a conversation, enable the connector and ask about your
              projects by name.
            </li>
          </ol>
        </div>
        {perms.clusterAdmin && (
          <div className="grid gap-2 sm:max-w-md">
            <Label htmlFor="mcp-name">Name shown by assistants</Label>
            <div className="flex gap-2">
              <Input
                id="mcp-name"
                value={name}
                maxLength={60}
                onChange={(e) => setName(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter" && dirty) rename.mutate(name.trim());
                }}
              />
              <Button
                size="sm"
                disabled={!dirty || rename.isPending}
                onClick={() => rename.mutate(name.trim())}
              >
                Save
              </Button>
            </div>
          </div>
        )}
        <div className="grid gap-2">
          <Label>Your connected assistants</Label>
          {connections.data && connections.data.length === 0 && (
            <p className="text-sm text-muted-foreground">
              None yet. Once you connect one it appears here, and you can
              disconnect it any time.
            </p>
          )}
          {connections.data && connections.data.length > 0 && (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Assistant</TableHead>
                  <TableHead>May</TableHead>
                  <TableHead>Connected</TableHead>
                  <TableHead>Last used</TableHead>
                  <TableHead className="w-16" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {connections.data.map((c) => (
                  <TableRow key={c.id}>
                    <TableCell className="font-medium">{c.client}</TableCell>
                    <TableCell>
                      {c.scope.split(" ").map((sc) => (
                        <Badge key={sc} variant="secondary" className="mr-1">
                          {sc === "projects:read"
                            ? "read"
                            : sc === "projects:write"
                              ? "change"
                              : sc}
                        </Badge>
                      ))}
                    </TableCell>
                    <TableCell className="text-muted-foreground">
                      {ago(c.createdAt)}
                    </TableCell>
                    <TableCell className="text-muted-foreground">
                      {c.lastUsedAt ? ago(c.lastUsedAt) : "—"}
                    </TableCell>
                    <TableCell className="text-right">
                      <Button
                        variant="ghost"
                        size="icon"
                        aria-label={`Disconnect ${c.client}`}
                        disabled={revoke.isPending}
                        onClick={() => revoke.mutate(c)}
                      >
                        <Trash2 className="size-4" />
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </div>
      </CardContent>
    </Card>
  );
}
