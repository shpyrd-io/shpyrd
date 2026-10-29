"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Unplug } from "lucide-react";
import { toast } from "sonner";
import { Badge } from "@shpyrd/ui/components/badge";
import { Button } from "@shpyrd/ui/components/button";
import { Card, CardContent, CardHeader, CardTitle } from "@shpyrd/ui/components/card";
import { ConfirmDialog } from "@shpyrd/ui/components/confirm-dialog";
import { InlineCode } from "@shpyrd/ui/components/inline-code";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@shpyrd/ui/components/table";
import { api } from "@/api/api";
import type { GitHubInstallation } from "@/api/types";
import { go } from "@/lib/go";
import { usePerms } from "@/lib/perms";
import { ago } from "@/lib/project";
import { Failed, Loading } from "../project/shared";

// The workspace's GitHub (RFC-0054): an account or organisation connected
// once, and anyone here picks its repositories when making a project;
// private ones build, and every push deploys through the App.
export function Git() {
  const perms = usePerms();
  const queries = useQueryClient();
  const github = useQuery({ queryKey: ["github"], queryFn: api.github });
  // One connect call gives both: GitHub's authorize page, which says who
  // the person is so the server lists the installations they own (one may
  // exist already), and its install page, for a new account.
  const connect = useMutation({
    mutationFn: async (to: "authorize" | "install") => ({ to, ...(await api.connectGitHub()) }),
    onSuccess: ({ to, authorizeURL, installURL }) => go(to === "install" ? installURL : authorizeURL),
    onError: (e: Error) => toast.error(e.message),
  });
  const disconnect = useMutation({
    mutationFn: (i: GitHubInstallation) => api.disconnectGitHub(i.id),
    onSuccess: (_, i) => {
      queries.invalidateQueries({ queryKey: ["github"] });
      queries.invalidateQueries({ queryKey: ["github-repositories"] });
      toast.success(`${i.account} disconnected`, { description: "The App stays installed on GitHub until it is removed there." });
    },
    onError: (e: Error) => toast.error(e.message),
  });
  const data = github.data;
  return (
    <Card>
      <CardHeader>
        <CardTitle>GitHub</CardTitle>
      </CardHeader>
      <CardContent className="grid gap-4">
        {github.isLoading ? (
          <Loading />
        ) : github.error ? (
          <Failed what="the GitHub connections" error={github.error} />
        ) : !data?.configured ? (
          <p className="text-sm text-muted-foreground">{withCode(data?.note ?? "GitHub is not set up on this platform.")}</p>
        ) : (
          <>
            {perms.admin && (
              <div className="flex flex-wrap items-center gap-4">
                <Button onClick={() => connect.mutate("authorize")} disabled={connect.isPending}>
                  Connect GitHub
                </Button>
                <Button variant="link" size="sm" className="px-0" onClick={() => connect.mutate("install")} disabled={connect.isPending}>
                  Install the App on another account
                </Button>
              </div>
            )}
            {data.installations.length === 0 ? (
              <p className="text-sm text-muted-foreground">Nothing connected yet.</p>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Account</TableHead>
                    <TableHead>Kind</TableHead>
                    <TableHead>Connected by</TableHead>
                    <TableHead />
                    <TableHead className="text-right" />
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {data.installations.map((i) => (
                    <TableRow key={i.id}>
                      <TableCell className="font-medium">
                        <span className="flex items-center gap-2">
                          {i.account}
                          {i.suspended && <Badge variant="destructive">Suspended</Badge>}
                        </span>
                      </TableCell>
                      <TableCell>{i.type === "Organization" ? "Organisation" : "Personal account"}</TableCell>
                      <TableCell className="text-xs text-muted-foreground">
                        {i.connectedBy} · {ago(i.createdAt)}
                      </TableCell>
                      <TableCell>
                        <Button asChild variant="link" size="sm" className="px-0">
                          <a href={i.manageURL} target="_blank" rel="noreferrer">
                            Manage on GitHub
                          </a>
                        </Button>
                      </TableCell>
                      <TableCell className="text-right">
                        {perms.admin && (
                          <ConfirmDialog
                            trigger={<Button variant="ghost" size="icon-xs" icon={<Unplug />} aria-label={`Disconnect ${i.account}`} />}
                            variant="destructive"
                            title={`Disconnect ${i.account}?`}
                            description="Its repositories stop building through the App and its pushes stop deploying; projects with a deploy key or a token go on with it. The App stays installed on GitHub."
                            action="Disconnect"
                            onConfirm={() => disconnect.mutate(i)}
                          />
                        )}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </>
        )}
      </CardContent>
    </Card>
  );
}

// withCode sets what the server's sentence quotes in backticks, a
// command, as code.
function withCode(text: string) {
  return text.split(/(`[^`]+`)/).map((part, i) =>
    part.length > 2 && part.startsWith("`") && part.endsWith("`") ? <InlineCode key={i}>{part.slice(1, -1)}</InlineCode> : part,
  );
}
