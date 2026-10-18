"use client";

import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Lock } from "lucide-react";
import { toast } from "sonner";
import { Alert, AlertDescription } from "@shpyrd/ui/components/alert";
import { Button } from "@shpyrd/ui/components/button";
import { Field } from "@shpyrd/ui/components/field";
import { Input } from "@shpyrd/ui/components/input";
import { ScrollArea } from "@shpyrd/ui/components/scroll-area";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@shpyrd/ui/components/select";
import { Skeleton } from "@shpyrd/ui/components/skeleton";
import { api } from "@/api/api";
import type { GitHubAccount, GitHubBranches, GitHubRepositories } from "@/api/types";
import { cancelGitHubLink, LinkCancelled, linkGitHub } from "@/lib/github-link";

// What New project makes from GitHub: a repository and its branch.
export type Picked = { fullName: string; branch: string };

// The slug a project made from a repository gets unless one is typed:
// its name, in the letters a slug takes.
export function slugFromRepo(fullName: string): string {
  const name = fullName.split("/").pop() ?? "";
  return name
    .toLowerCase()
    .replace(/[^a-z0-9-]+/g, "-")
    .replace(/^-+|-+$/g, "");
}

// The branches with the repository's default first, each once.
export function branchesInOrder(branches: string[], defaultBranch: string): string[] {
  return [defaultBranch, ...branches].filter((b, i, all) => b && all.indexOf(b) === i);
}

// Before the picker: the person links their own GitHub account, in a
// popup, and the dialog under it keeps what was typed (RFC-0054).
export function GitHubLinkStep({ onLinked }: { onLinked: () => void }) {
  const link = useMutation({ mutationFn: linkGitHub, onSuccess: onLinked, onError: (e: Error) => {
      if (!(e instanceof LinkCancelled)) toast.error(e.message);
    },
  });
  // Leaving the step ends the wait for GitHub.
  useEffect(() => cancelGitHubLink, []);
  return (
    <div className="grid justify-items-start gap-3 rounded-md border p-4">
      <p className="text-sm text-muted-foreground">
        Link your GitHub account to see the repositories you can read. What you typed stays here.
      </p>
      <Button type="button" onClick={() => link.mutate()}>
        Continue with GitHub
      </Button>
      {link.isPending && <p className="text-xs text-muted-foreground">Waiting for GitHub…</p>}
    </div>
  );
}

// The repositories the person can read in the workspace's GitHub accounts
// (RFC-0054): an account, a filter over a list that scrolls, then the
// branch; below, where to find a missing repository and how to add an
// account.
export function GitHubPicker({
  accounts,
  value,
  onChange,
  canAdd,
}: {
  accounts: GitHubAccount[];
  value?: Picked;
  onChange: (p: Picked | undefined) => void;
  canAdd: boolean;
}) {
  const [installation, setInstallation] = useState<number | undefined>(accounts[0]?.installation);
  const account = accounts.find((a) => a.installation === installation) ?? accounts[0];
  const [filter, setFilter] = useState("");
  const repos = useQuery({
    queryKey: ["github-repositories", account?.installation],
    queryFn: () => api.githubRepositories(account!.installation),
    enabled: !!account,
    retry: false,
    staleTime: 60_000,
    // The list may carry `truncated`; sharing the data again would copy
    // only its items.
    structuralSharing: false,
  });
  const truncated = (repos.data as GitHubRepositories | undefined)?.truncated === true;
  const list = useMemo(() => repos.data ?? [], [repos.data]);
  const shown = useMemo(() => {
    const f = filter.trim().toLowerCase();
    return f ? list.filter((r) => r.fullName.toLowerCase().includes(f)) : list;
  }, [filter, list]);
  const chosen = list.find((r) => r.fullName === value?.fullName);
  // A repository the list no longer has is not picked: GitHub took it
  // away meanwhile.
  useEffect(() => {
    if (value && repos.data && !repos.data.some((r) => r.fullName === value.fullName)) onChange(undefined);
  }, [value, repos.data, onChange]);
  const branches = useQuery({
    queryKey: ["github-branches", value?.fullName],
    queryFn: () => api.githubBranches(value!.fullName),
    enabled: !!chosen,
    staleTime: 60_000,
    structuralSharing: false,
  });
  const branchesCut = (branches.data as GitHubBranches | undefined)?.truncated === true;
  const addAccount = async () => {
    try {
      const { installURL } = await api.connectGitHub();
      window.open(installURL, "shpyrd-github-install", "popup,width=1020,height=760");
    } catch (e) {
      toast.error((e as Error).message);
    }
  };
  const below = (
    <div className="flex flex-wrap items-center justify-between gap-2">
      {account && (
        <Button type="button" variant="link" size="xs" onClick={() => window.open(account.manageURL, "_blank", "noopener")}>
          Can't find a repository?
        </Button>
      )}
      {canAdd ? (
        <Button type="button" variant="link" size="xs" onClick={addAccount}>
          Add GitHub account or organisation
        </Button>
      ) : (
        <p className="text-xs text-muted-foreground">A workspace admin adds accounts in Workspace settings → Git.</p>
      )}
    </div>
  );
  if (!account) {
    return (
      <div className="grid gap-3">
        <p className="text-sm text-muted-foreground">
          Your GitHub account cannot read any of this workspace's GitHub accounts. Ask for access on GitHub, or add your own.
        </p>
        {below}
      </div>
    );
  }
  return (
    <div className="grid gap-3">
      <Field label="Account">
        <Select
          value={String(account.installation)}
          onValueChange={(v) => {
            setInstallation(Number(v));
            onChange(undefined);
          }}
        >
          <SelectTrigger aria-label="GitHub account" className="w-full">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {accounts.map((a) => (
              <SelectItem key={a.installation} value={String(a.installation)}>
                {a.account}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </Field>
      <Field label="Repository">
        <Input value={filter} onChange={(e) => setFilter(e.target.value)} placeholder="Filter" aria-label="Filter the repositories" />
      </Field>
      {repos.error ? (
        <Alert variant="destructive">
          <AlertDescription>{(repos.error as Error).message}</AlertDescription>
        </Alert>
      ) : (
        <ScrollArea className="h-48 rounded-md border">
          <div className="grid p-1">
            {repos.isLoading
              ? [0, 1, 2].map((i) => <Skeleton key={i} className="m-1 h-6" />)
              : shown.map((r) => (
                  <Button
                    key={r.fullName}
                    type="button"
                    variant="ghost"
                    size="sm"
                    aria-pressed={r.fullName === value?.fullName}
                    className="w-full justify-between font-normal aria-pressed:bg-accent aria-pressed:text-accent-foreground"
                    onClick={() => onChange({ fullName: r.fullName, branch: r.defaultBranch })}
                  >
                    <span className="truncate font-mono text-xs">{r.fullName}</span>
                    {r.private && <Lock className="size-3 shrink-0 text-muted-foreground" aria-label="Private" />}
                  </Button>
                ))}
            {!repos.isLoading && shown.length === 0 && <p className="px-2 py-1.5 text-sm text-muted-foreground">No repository matches.</p>}
          </div>
        </ScrollArea>
      )}
      {truncated && <p className="text-xs text-muted-foreground">Showing the first {list.length}. Filter to narrow.</p>}
      {below}
      {chosen && value && (
        <Field label="Branch">
          {branches.isLoading ? (
            <Skeleton className="h-9 w-full" />
          ) : branches.error ? (
            <Alert variant="destructive">
              <AlertDescription>{(branches.error as Error).message}</AlertDescription>
            </Alert>
          ) : (
            <Select value={value.branch} onValueChange={(branch) => onChange({ ...value, branch })}>
              <SelectTrigger aria-label="Branch">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {branchesInOrder(branches.data ?? [], chosen.defaultBranch).map((b) => (
                  <SelectItem key={b} value={b}>
                    {b}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          )}
        </Field>
      )}
      {chosen && value && branchesCut && (
        <p className="text-xs text-muted-foreground">Showing the first {branches.data?.length}. Filter to narrow.</p>
      )}
    </div>
  );
}
