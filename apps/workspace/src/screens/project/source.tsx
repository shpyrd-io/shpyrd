"use client";

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Copy } from "lucide-react";
import { toast } from "sonner";
import { Alert, AlertDescription, AlertTitle } from "@shpyrd/ui/components/alert";
import { Button } from "@shpyrd/ui/components/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@shpyrd/ui/components/dialog";
import { InfoTable, InfoTableItem } from "@shpyrd/ui/components/info-table";
import { InlineCode } from "@shpyrd/ui/components/inline-code";
import { Skeleton } from "@shpyrd/ui/components/skeleton";
import { Switch } from "@shpyrd/ui/components/switch";
import { api } from "@/api/api";
import type { Project, SourceStatus } from "@/api/types";
import type { Perms } from "@/lib/perms";
import { ago } from "@/lib/project";

const isCommit = (rev: string) => /^[0-9a-f]{7,40}$/i.test(rev);
const short = (sha?: string) => sha?.slice(0, 7) ?? "";
const subject = (message?: string) => message?.split("\n")[0].trim() ?? "";

// Where the project's code comes from when it is a git repository: the
// branch, the commit that runs, when shpyrd last looked, and whether a
// push deploys (RFC-0018). Nothing for any other source.
export function GitSource({ project, perms }: { project: Project; perms: Perms }) {
  const git = project.spec.source?.git;
  if (!git) return null;
  const branch = git.revision ?? "main";
  const source = project.status.source;
  const autoDeploy = git.autoDeploy !== false;
  if (isCommit(branch)) {
    return (
      <div className="grid gap-3">
        <InfoTable columns={3}>
          <InfoTableItem label="Repository" mono truncate span={2}>
            {git.url}
          </InfoTableItem>
          <InfoTableItem label="Commit" mono>
            {branch}
          </InfoTableItem>
        </InfoTable>
        <p className="text-xs text-muted-foreground">Pinned to a commit: a push deploys nothing.</p>
      </div>
    );
  }
  const checked = source?.checkedAt
    ? `${ago(source.checkedAt)}${source.via === "push" || source.via === "poll" ? ` · via ${source.via}` : ""}`
    : "not yet";
  return (
    <div className="grid gap-3">
      <InfoTable columns={3}>
        <InfoTableItem label="Repository" mono truncate span={2}>
          {git.url}
        </InfoTableItem>
        <InfoTableItem label="Branch" mono>
          {branch}
        </InfoTableItem>
        <InfoTableItem label="Commit" span={2}>
          <CommitLine source={source} />
        </InfoTableItem>
        <InfoTableItem label="Last checked">{checked}</InfoTableItem>
        <InfoTableItem label="Auto-deploy">
          <AutoDeploySwitch slug={project.slug} on={autoDeploy} allowed={perms.config} />
        </InfoTableItem>
      </InfoTable>
      {source?.skipped && (
        <p className="text-xs text-muted-foreground">
          Skipped {short(source.skipped)}: {(source.latest === source.skipped && subject(source.message)) || "a skip marker"}
        </p>
      )}
      {!autoDeploy && source?.latest && source.latest !== source.commit && (
        <p className="text-xs text-muted-foreground">
          Latest on {branch}: {short(source.latest)}, not deployed: auto-deploy is off.
        </p>
      )}
      {source?.error && (
        <Alert variant="destructive">
          <AlertTitle>shpyrd cannot read the branch</AlertTitle>
          <AlertDescription>{source.error}</AlertDescription>
        </Alert>
      )}
      <SourceAccess slug={project.slug} canConfig={perms.config} />
    </div>
  );
}

// The commit that runs, with its subject and author when a push said them.
// Without status.source shpyrd does not watch the branch (a URL it cannot
// read, such as git://): the build follows the branch by itself.
function CommitLine({ source }: { source?: SourceStatus }) {
  if (!source) return <span className="text-muted-foreground">The build follows the branch</span>;
  if (!source.commit)
    return <span className="text-muted-foreground">Reading the branch's latest commit.</span>;
  const known = source.latest === source.commit;
  return (
    <span className="flex min-w-0 items-baseline gap-2">
      <span className="shrink-0 font-mono text-xs">{short(source.commit)}</span>
      {known && subject(source.message) && <span className="truncate">{subject(source.message)}</span>}
      {known && source.author && <span className="shrink-0 text-muted-foreground">by {source.author}</span>}
    </span>
  );
}

function AutoDeploySwitch({ slug, on, allowed }: { slug: string; on: boolean; allowed: boolean }) {
  const queries = useQueryClient();
  const set = useMutation({
    mutationFn: (autoDeploy: boolean) => api.updateProject(slug, { autoDeploy }),
    onSuccess: (p) => {
      queries.setQueryData(["project", slug], p);
      toast.success(p.spec.source?.git?.autoDeploy === false ? "Pushes are recorded, not deployed" : "Every push deploys again");
    },
    onError: (e: Error) => toast.error(e.message),
  });
  return (
    <Switch
      size="sm"
      checked={on}
      disabled={!allowed || set.isPending}
      loading={set.isPending}
      onCheckedChange={(v) => set.mutate(v)}
      aria-label="Auto-deploy"
    />
  );
}

// How pushes reach shpyrd: through the GitHub App, with nothing to set
// up, or through the project's own webhook (RFC-0054).
function SourceAccess({ slug, canConfig }: { slug: string; canConfig: boolean }) {
  const auth = useQuery({ queryKey: ["git-auth", slug], queryFn: () => api.projectGitAuth(slug), staleTime: 60_000 });
  if (auth.data?.using === "github-app")
    return (
      <p className="text-xs text-muted-foreground">
        Through the GitHub App ({auth.data.githubAccount}): every push arrives by itself, nothing to set up.
      </p>
    );
  return canConfig ? <DeployOnPush slug={slug} /> : null;
}

// The webhook a git host calls, to copy into it. Its secret is shown only
// to those who may change the project.
function DeployOnPush({ slug }: { slug: string }) {
  const [open, setOpen] = useState(false);
  const queries = useQueryClient();
  // The secret is not kept once the dialog closes.
  const hooks = useQuery({ queryKey: ["git-hooks", slug], queryFn: () => api.projectGitHooks(slug), enabled: open, gcTime: 0 });
  const toggle = (next: boolean) => {
    setOpen(next);
    if (!next) queries.removeQueries({ queryKey: ["git-hooks", slug] });
  };
  const copy = async (what: string, text: string) => {
    try {
      if (!navigator.clipboard) throw new Error("no clipboard");
      await navigator.clipboard.writeText(text);
      toast.success(`${what} copied`);
    } catch {
      toast.error(`Could not copy the ${what.toLowerCase()}: select it and copy it by hand`);
    }
  };
  return (
    <Dialog open={open} onOpenChange={toggle}>
      <DialogTrigger asChild>
        <Button variant="link" size="sm" className="justify-self-start px-0">
          Deploy on push
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Deploy on push</DialogTitle>
          <DialogDescription>
            Add this webhook to the repository and every push to the branch deploys within seconds. Without it, shpyrd checks the
            branch every few minutes.
          </DialogDescription>
        </DialogHeader>
        {hooks.isLoading ? (
          <Skeleton className="h-24 w-full" />
        ) : hooks.error ? (
          <Alert variant="destructive">
            <AlertDescription>{(hooks.error as Error).message}</AlertDescription>
          </Alert>
        ) : hooks.data ? (
          <div className="grid gap-3 text-sm">
            <Value label="Payload URL" value={hooks.data.url} onCopy={() => void copy("URL", hooks.data.url)} copyLabel="Copy the URL" />
            <Value label="Secret" value={hooks.data.secret} onCopy={() => void copy("Secret", hooks.data.secret)} copyLabel="Copy the secret" />
            <ul className="grid gap-1 text-muted-foreground">
              <li>
                <b className="font-medium text-foreground">GitHub</b>: Settings, Webhooks, Add webhook. Content type{" "}
                <InlineCode>application/json</InlineCode>, the secret above, “Just the push event”.
              </li>
              <li>
                <b className="font-medium text-foreground">GitLab</b>: Settings, Webhooks. The secret as the Secret token, Push events.
              </li>
              <li>
                <b className="font-medium text-foreground">Gitea</b>: Settings, Webhooks, Add webhook, Gitea. Content type{" "}
                <InlineCode>application/json</InlineCode>, the secret above, Push events.
              </li>
            </ul>
            <p className="text-xs text-muted-foreground">
              From a terminal: <InlineCode>shpyrd git-hooks show --project {slug}</InlineCode>
            </p>
          </div>
        ) : null}
      </DialogContent>
    </Dialog>
  );
}

function Value({ label, value, onCopy, copyLabel }: { label: string; value: string; onCopy: () => void; copyLabel: string }) {
  return (
    <div className="grid gap-1">
      <span className="text-xs text-muted-foreground">{label}</span>
      <span className="flex items-start gap-1">
        <InlineCode className="break-all">{value}</InlineCode>
        <Button variant="ghost" size="icon-xs" icon={<Copy />} aria-label={copyLabel} onClick={onCopy} />
      </span>
    </div>
  );
}
