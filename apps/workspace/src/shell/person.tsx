"use client";

import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ChevronDown, ExternalLink, Link2, LogOut, Moon, Sun, Unlink } from "lucide-react";
import { toast } from "sonner";
import { Avatar } from "@shpyrd/ui/components/avatar";
import { Button } from "@shpyrd/ui/components/button";
import { ConfirmDialog } from "@shpyrd/ui/components/confirm-dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@shpyrd/ui/components/dropdown-menu";
import { useTheme } from "@shpyrd/ui/lib/theme";
import { api } from "@/api/api";
import { cancelGitHubLink, LinkCancelled, linkGitHub } from "@/lib/github-link";

// The small controls every screen has at the top right: the theme, and
// the person signed in with the way out.

export function ThemeButton() {
  const [theme, setTheme] = useTheme();
  return (
    <Button
      variant="ghost"
      size="icon"
      icon={theme === "dark" ? <Moon /> : <Sun />}
      aria-label={theme === "dark" ? "Light theme" : "Dark theme"}
      onClick={() => setTheme(theme === "dark" ? "light" : "dark")}
    />
  );
}

export function PersonMenu() {
  const queries = useQueryClient();
  const signOut = async () => {
    let to = "/";
    try {
      const r = await api.logout();
      if (r?.redirect) to = r.redirect;
    } finally {
      queries.clear();
      window.location.href = to;
    }
  };
  const me = useQuery({ queryKey: ["me"], queryFn: api.me, staleTime: 60_000 });
  const config = useQuery({ queryKey: ["config"], queryFn: api.config, staleTime: 60_000 });
  // The person's own GitHub account (RFC-0054); the admin token has none.
  const person = me.data !== undefined && me.data.provider !== "token";
  const github = useQuery({ queryKey: ["github-me"], queryFn: api.githubMe, staleTime: 60_000, enabled: person });
  const githubChanged = () => {
    queries.invalidateQueries({ queryKey: ["github-me"] });
    queries.invalidateQueries({ queryKey: ["github-accounts"] });
    queries.invalidateQueries({ queryKey: ["github-repositories"] });
  };
  const link = async () => {
    try {
      await linkGitHub();
    } catch (e) {
      if (!(e instanceof LinkCancelled)) toast.error((e as Error).message);
    } finally {
      githubChanged();
    }
  };
  // Leaving the menu ends a wait for GitHub.
  useEffect(() => cancelGitHubLink, []);
  const [unlinking, setUnlinking] = useState(false);
  const unlink = async () => {
    try {
      await api.unlinkGitHub();
      toast.success("GitHub unlinked", { description: "Your projects keep deploying." });
    } catch (e) {
      toast.error((e as Error).message);
    } finally {
      githubChanged();
    }
  };
  const name = me.data?.name || me.data?.email || "…";
  const consoleUrl = me.data?.console && config.data?.workspace?.ownedByOperator ? config.data.consoleUrl : undefined;
  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button variant="ghost" icon={<Avatar size={20} alt={name} />} iconEnd={<ChevronDown />} aria-label={name}>
            <span className="hidden max-w-40 truncate sm:inline">{name}</span>
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="min-w-56">
          <DropdownMenuLabel className="font-normal">
            <div className="text-sm font-medium">{me.data?.name ?? name}</div>
            <div className="text-xs text-muted-foreground">
              {me.data?.provider === "token" ? "admin token" : `signed in with ${me.data?.provider ?? "…"}`}
            </div>
          </DropdownMenuLabel>
          <DropdownMenuSeparator />
          {consoleUrl && (
            <DropdownMenuItem asChild>
              <a href={consoleUrl}>
                <ExternalLink /> Console
              </a>
            </DropdownMenuItem>
          )}
          {person &&
            (github.data ? (
              <>
                <DropdownMenuLabel className="truncate text-xs font-normal text-muted-foreground">GitHub: {github.data.login}</DropdownMenuLabel>
                <DropdownMenuItem onSelect={() => setUnlinking(true)}>
                  <Unlink /> Unlink GitHub
                </DropdownMenuItem>
              </>
            ) : (
              github.isSuccess && (
                <DropdownMenuItem onClick={link}>
                  <Link2 /> Link GitHub
                </DropdownMenuItem>
              )
            ))}
          {person && <DropdownMenuSeparator />}
          <DropdownMenuItem onClick={signOut}>
            <LogOut /> Sign out
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      <ConfirmDialog
        open={unlinking}
        onOpenChange={setUnlinking}
        title="Unlink GitHub?"
        description="You won't see your private repositories in New project until you link again. Projects keep deploying."
        action="Unlink"
        variant="destructive"
        onConfirm={unlink}
      />
    </>
  );
}
