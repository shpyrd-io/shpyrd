"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Lock } from "lucide-react";
import { Alert, AlertDescription } from "@shpyrd/ui/components/alert";
import { Button } from "@shpyrd/ui/components/button";
import { Field } from "@shpyrd/ui/components/field";
import { Input } from "@shpyrd/ui/components/input";
import { ScrollArea } from "@shpyrd/ui/components/scroll-area";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@shpyrd/ui/components/select";
import { Skeleton } from "@shpyrd/ui/components/skeleton";
import { api } from "@/api/api";
import type { GitHubRepository } from "@/api/types";

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

// The repositories the workspace's GitHub connections reach (RFC-0054):
// a filter over a list that scrolls, then the branch.
export function GitHubPicker({
  repositories,
  value,
  onChange,
}: {
  repositories: GitHubRepository[];
  value?: Picked;
  onChange: (p: Picked) => void;
}) {
  const [filter, setFilter] = useState("");
  const shown = useMemo(() => {
    const f = filter.trim().toLowerCase();
    return f ? repositories.filter((r) => r.fullName.toLowerCase().includes(f)) : repositories;
  }, [filter, repositories]);
  const chosen = repositories.find((r) => r.fullName === value?.fullName);
  const branches = useQuery({
    queryKey: ["github-branches", value?.fullName],
    queryFn: () => api.githubBranches(value!.fullName),
    enabled: !!chosen,
    staleTime: 60_000,
  });
  return (
    <div className="grid gap-3">
      <Field label="Repository">
        <Input value={filter} onChange={(e) => setFilter(e.target.value)} placeholder="Filter" aria-label="Filter the repositories" />
      </Field>
      <ScrollArea className="h-48 rounded-md border">
        <div className="grid p-1">
          {shown.map((r) => (
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
          {shown.length === 0 && <p className="px-2 py-1.5 text-sm text-muted-foreground">No repository matches.</p>}
        </div>
      </ScrollArea>
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
    </div>
  );
}
