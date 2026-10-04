"use client";

import { useEffect, useState, type ReactElement } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Copy, Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { Badge } from "@shpyrd/ui/components/badge";
import {
  ClaudeIcon,
  CopilotIcon,
  CursorIcon,
  GeminiIcon,
  OpenAIIcon,
  VSCodeIcon,
  WarpIcon,
} from "@shpyrd/ui/components/brand-icons";
import { Button } from "@shpyrd/ui/components/button";
import { ConfirmDialog } from "@shpyrd/ui/components/confirm-dialog";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@shpyrd/ui/components/card";
import { Field } from "@shpyrd/ui/components/field";
import { IDE } from "@shpyrd/ui/components/ide";
import { InlineCode } from "@shpyrd/ui/components/inline-code";
import { Input } from "@shpyrd/ui/components/input";
import { Stack } from "@shpyrd/ui/components/stack";
import { StatusBadge } from "@shpyrd/ui/components/status-badge";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@shpyrd/ui/components/table";
import {
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from "@shpyrd/ui/components/tabs";
import { api } from "@/api/api";
import { usePerms } from "@/lib/perms";
import { Failed, Loading } from "../project/shared";

// The workspace is an MCP server: the name the assistants show, its
// address, and how each assistant is pointed at it. Every one signs the
// person in through the workspace: no token to make or paste.
export function MCP() {
  const perms = usePerms();
  const queries = useQueryClient();
  const ws = useQuery({ queryKey: ["workspace"], queryFn: api.workspace });
  const [name, setName] = useState("");
  useEffect(() => setName(ws.data?.mcpName ?? ""), [ws.data?.mcpName]);
  const rename = useMutation({
    mutationFn: (mcpName: string) => api.updateWorkspace({ mcpName }),
    onSuccess: () => {
      toast.success("Renamed", {
        description: "The assistants show it at their next connection.",
      });
      queries.invalidateQueries({ queryKey: ["workspace"] });
    },
    onError: (e: Error) => toast.error(e.message),
  });
  if (ws.isLoading || !ws.data) return <Loading />;
  const w = ws.data;
  const dirty = name.trim() !== w.mcpName && name.trim() !== "";
  const url = w.mcpUrl;
  const copy = () => {
    void navigator.clipboard?.writeText(url);
    toast.success("Address copied");
  };
  return (
    <>
      <div className="grid items-start gap-4 @5xl/page-layout:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle>Assistant name</CardTitle>
            <CardDescription>
              How the assistants name this workspace when they list what they
              are connected to, and what you say to address it: “check on{" "}
              {w.mcpName} the metrics of hello-world”.
            </CardDescription>
          </CardHeader>
          <CardContent>
            <Field label="Assistant name" hint="At most 60 characters.">
              <Stack direction="horizontal" gap="condensed">
                <Input
                  value={name}
                  maxLength={60}
                  disabled={!perms.admin}
                  onChange={(e) => setName(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === "Enter" && dirty) rename.mutate(name.trim());
                  }}
                />
                {perms.admin && (
                  <Button
                    disabled={!dirty || rename.isPending}
                    onClick={() => rename.mutate(name.trim())}
                  >
                    Save
                  </Button>
                )}
              </Stack>
            </Field>
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle>Address</CardTitle>
            <CardDescription>
              Where the assistants reach the workspace. It speaks MCP over HTTP;
              an assistant given it signs you in through the workspace and sees
              what you see, within your roles. It changes nothing, for now.
            </CardDescription>
          </CardHeader>
          <CardContent>
            <Stack direction="horizontal" gap="condensed">
              <Input
                readOnly
                value={url.replace(/^https?:\/\//, "")}
                prefix={url.startsWith("https://") ? "https://" : "http://"}
                className="font-mono text-xs"
                onFocus={(e) => e.currentTarget.select()}
                aria-label="MCP address"
              />
              <Button variant="outline" icon={<Copy />} onClick={copy}>
                Copy
              </Button>
            </Stack>
          </CardContent>
        </Card>
      </div>
      <Card>
        <CardHeader>
          <CardTitle>Add it to your AI</CardTitle>
          <CardDescription>
            Web assistants take the address as a connector. Command-line agents
            and editors take it from a command or a file; each then asks you to
            sign in once.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Clients url={url} />
        </CardContent>
      </Card>
    </>
  );
}

// How each assistant is pointed at the workspace: the steps, and the
// command or the file when there is one. The key `shpyrd` is what the
// file calls the server; the assistants show the name above.
function Clients({ url }: { url: string }) {
  const json = (o: unknown) => JSON.stringify(o, null, 2);
  const clients: {
    id: string;
    name: string;
    icon: ReactElement;
    group: string;
    steps: string[];
    files?: { name: string; code: string; language: string }[];
  }[] = [
    {
      id: "claude",
      icon: <ClaudeIcon />,
      name: "Claude",
      group: "On the web",
      steps: [
        "In Claude, open Settings, then Connectors, and choose Add custom connector.",
        "Give it a name and paste the address, then add it.",
        "Press Connect, sign in with your account here and allow it.",
        "In a conversation, turn the connector on and ask about your projects by name.",
      ],
    },
    {
      id: "chatgpt",
      icon: <OpenAIIcon />,
      name: "ChatGPT",
      group: "On the web",
      steps: [
        "In ChatGPT, open Settings, then Connectors, and turn on developer mode under Advanced.",
        "Choose Create, give it a name and paste the address; leave the authentication on OAuth.",
        "Create it, sign in with your account here and allow it.",
        "In a conversation, choose the connector under the tools and ask about your projects.",
      ],
    },
    {
      id: "claude-code",
      icon: <ClaudeIcon />,
      name: "Claude Code",
      group: "In the terminal",
      steps: [
        "Add it with the command, or put the file in the project.",
        "In a terminal, run claude and then /mcp to sign in.",
        "Ask about your projects by name.",
      ],
      files: [
        {
          name: "Terminal",
          code: `claude mcp add --scope project --transport http shpyrd "${url}"`,
          language: "sh",
        },
        {
          name: ".mcp.json",
          code: json({ mcpServers: { shpyrd: { type: "http", url } } }),
          language: "json",
        },
      ],
    },
    {
      id: "codex",
      icon: <OpenAIIcon />,
      name: "Codex",
      group: "In the terminal",
      steps: [
        "Add it with the command, or put the lines in the file.",
        "Run codex mcp login shpyrd to sign in, then /mcp inside Codex to see it.",
      ],
      files: [
        {
          name: "Terminal",
          code: `codex mcp add shpyrd --url "${url}"`,
          language: "sh",
        },
        {
          name: "~/.codex/config.toml",
          code: `[mcp_servers.shpyrd]\nurl = "${url}"`,
          language: "toml",
        },
      ],
    },
    {
      id: "gemini",
      icon: <GeminiIcon />,
      name: "Gemini CLI",
      group: "In the terminal",
      steps: [
        "Add it with the command, or put the file in the project.",
        "Inside Gemini, run /mcp auth shpyrd to sign in.",
      ],
      files: [
        {
          name: "Terminal",
          code: `gemini mcp add -t http shpyrd "${url}"`,
          language: "sh",
        },
        {
          name: ".gemini/settings.json",
          code: json({ mcpServers: { shpyrd: { httpUrl: url } } }),
          language: "json",
        },
      ],
    },
    {
      id: "copilot",
      icon: <CopilotIcon />,
      name: "GitHub Copilot CLI",
      group: "In the terminal",
      steps: [
        "Add it with the command, or put the lines in the file.",
        "Run copilot -i /mcp to sign in.",
      ],
      files: [
        {
          name: "Terminal",
          code: `copilot mcp add --transport http shpyrd "${url}"`,
          language: "sh",
        },
        {
          name: "~/.copilot/mcp-config.json",
          code: json({ mcpServers: { shpyrd: { type: "http", url } } }),
          language: "json",
        },
      ],
    },
    {
      id: "cursor",
      icon: <CursorIcon />,
      name: "Cursor",
      group: "In the editor",
      steps: [
        "Put the file in the project, or in your home folder for every project.",
        "In Cursor Settings, under MCP, the server appears: press Login and sign in with your account here.",
      ],
      files: [
        {
          name: ".cursor/mcp.json",
          code: json({ mcpServers: { shpyrd: { url } } }),
          language: "json",
        },
      ],
    },
    {
      id: "vscode",
      icon: <VSCodeIcon />,
      name: "VS Code",
      group: "In the editor",
      steps: [
        "Put the file in the project.",
        "Run MCP: List Servers from the command palette, start shpyrd and sign in when asked.",
      ],
      files: [
        {
          name: ".vscode/mcp.json",
          code: json({ servers: { shpyrd: { type: "http", url } } }),
          language: "json",
        },
      ],
    },
    {
      id: "warp",
      icon: <WarpIcon />,
      name: "Warp",
      group: "In the editor",
      steps: [
        "Put the lines in the file, or add the server under Settings, AI, MCP servers.",
        "Start it there and sign in when asked.",
      ],
      files: [
        {
          name: "~/.warp/.mcp.json",
          code: json({ mcpServers: { shpyrd: { url } } }),
          language: "json",
        },
      ],
    },
  ];
  const groups = [...new Set(clients.map((c) => c.group))];
  return (
    <Tabs defaultValue={clients[0]!.id}>
      <TabsList variant="line" className="max-w-full overflow-x-auto">
        {clients.map((c) => (
          <TabsTrigger key={c.id} value={c.id} icon={c.icon}>
            {c.name}
          </TabsTrigger>
        ))}
      </TabsList>
      {clients.map((c) => (
        <TabsContent key={c.id} value={c.id} className="grid gap-4 pt-4">
          <div>
            <Badge variant="outline">{c.group}</Badge>
          </div>
          <ol className="grid list-decimal gap-1 pl-5 text-sm">
            {c.steps.map((step) => (
              <li key={step}>{step}</li>
            ))}
          </ol>
          {c.files && <IDE files={c.files} tabs />}
        </TabsContent>
      ))}
      <p className="sr-only">{groups.join(", ")}</p>
    </Tabs>
  );
}

// Names of the workspace's own: proved by records; the primary one is
// the address it answers at first, and its apps live one label under it.
export function WorkspaceDomains() {
  const perms = usePerms();
  const queries = useQueryClient();
  const domains = useQuery({
    queryKey: ["workspace-domains"],
    queryFn: api.workspaceDomains,
    refetchInterval: 15_000,
  });
  const refresh = () => {
    queries.invalidateQueries({ queryKey: ["workspace-domains"] });
    queries.invalidateQueries({ queryKey: ["workspace"] });
    queries.invalidateQueries({ queryKey: ["config"] });
  };
  const failed = (e: Error) => toast.error(e.message);
  const verify = useMutation({
    mutationFn: (host: string) => api.verifyWorkspaceDomain(host),
    onSuccess: (d) => {
      toast.success(
        d.verified ? `${d.host} verified` : `${d.host} is not verified yet`,
        {
          description: d.verified
            ? undefined
            : "The records were not seen. DNS takes a while.",
        },
      );
      refresh();
    },
    onError: failed,
  });
  const primary = useMutation({
    mutationFn: (host: string) => api.setWorkspaceDomainPrimary(host),
    onSuccess: (d) => {
      toast.success(`${d.host} is the address now`);
      refresh();
    },
    onError: failed,
  });
  const remove = useMutation({
    mutationFn: (host: string) => api.removeWorkspaceDomain(host),
    onSuccess: () => {
      toast.success("Domain removed");
      refresh();
    },
    onError: failed,
  });
  const [host, setHost] = useState("");
  const valid = /^[a-z0-9.-]+\.[a-z]{2,}$/i.test(host.trim());
  const add = useMutation({
    mutationFn: () => api.addWorkspaceDomain(host.trim().toLowerCase()),
    onSuccess: () => {
      toast.success("Domain added", {
        description: "Publish its records, then verify.",
      });
      setHost("");
      refresh();
    },
    onError: failed,
  });
  return (
    <Card>
      <CardHeader>
        <CardTitle>Domains</CardTitle>
        <CardDescription>
          Names of your own the workspace answers at, such as intranet.acme.com;
          its apps become one label under it. The primary one is its address.
        </CardDescription>
      </CardHeader>
      <CardContent>
        <Stack gap="normal">
          {domains.isLoading ? (
            <Loading />
          ) : domains.error ? (
            <Failed what="the domains" error={domains.error} />
          ) : (domains.data ?? []).length === 0 ? (
            <p className="text-sm text-muted-foreground">
              None yet. It answers at the platform's address until one is added.
            </p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Domain</TableHead>
                  <TableHead>Records</TableHead>
                  <TableHead>State</TableHead>
                  <TableHead className="text-right" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {domains.data!.map((d) => (
                  <TableRow key={d.host}>
                    <TableCell>
                      <InlineCode>{d.host}</InlineCode>
                      {d.primary && (
                        <Badge variant="secondary" className="ml-2">
                          the address
                        </Badge>
                      )}
                    </TableCell>
                    <TableCell className="font-mono text-[11px] text-muted-foreground">
                      {d.records.length === 0
                        ? "the platform's"
                        : d.records.map((r) => (
                            <div key={`${r.type}${r.name}`}>
                              {r.name} {r.type} {r.value}
                            </div>
                          ))}
                    </TableCell>
                    <TableCell>
                      <StatusBadge
                        type={d.verified ? "success" : "warning"}
                        live={!d.verified}
                      >
                        {d.verified ? "Verified" : "Waiting for the records"}
                      </StatusBadge>
                    </TableCell>
                    <TableCell className="text-right">
                      {perms.owner && (
                        <Stack
                          direction="horizontal"
                          gap="tight"
                          justify="end"
                          align="center"
                        >
                          {!d.verified && (
                            <Button
                              size="xs"
                              variant="outline"
                              disabled={verify.isPending}
                              onClick={() => verify.mutate(d.host)}
                            >
                              Verify
                            </Button>
                          )}
                          {d.verified && !d.primary && (
                            <Button
                              size="xs"
                              variant="outline"
                              disabled={primary.isPending}
                              onClick={() => primary.mutate(d.host)}
                            >
                              Make it the address
                            </Button>
                          )}
                          {!d.primary && d.records.length > 0 && (
                            <ConfirmDialog
                              trigger={
                                <Button
                                  variant="ghost"
                                  size="icon-xs"
                                  icon={<Trash2 />}
                                  aria-label={`Remove ${d.host}`}
                                />
                              }
                              variant="destructive"
                              title={`Remove ${d.host}?`}
                              description="It stops answering here. The records at your registrar stay as they are."
                              action="Remove"
                              onConfirm={() => remove.mutate(d.host)}
                            />
                          )}
                        </Stack>
                      )}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
          {perms.owner && (
            <form
              className="grid gap-2 sm:grid-cols-[1fr_auto]"
              onSubmit={(e) => {
                e.preventDefault();
                if (valid) add.mutate();
              }}
            >
              <Input
                value={host}
                onChange={(e) => setHost(e.target.value)}
                placeholder="intranet.acme.com"
                aria-label="Domain"
              />
              <Button
                type="submit"
                icon={<Plus />}
                disabled={!valid || add.isPending}
              >
                Add
              </Button>
            </form>
          )}
        </Stack>
      </CardContent>
    </Card>
  );
}
