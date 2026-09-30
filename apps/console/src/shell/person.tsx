"use client";

import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ChevronDown, ExternalLink, LogOut, Moon, Sun } from "lucide-react";
import { Avatar } from "@shpyrd/ui/components/avatar";
import { Button } from "@shpyrd/ui/components/button";
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
  const name = me.data?.name || me.data?.email || "…";
  const grafana = config.data?.grafanaUrl;
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" icon={<Avatar size={20} alt={name} />} iconEnd={<ChevronDown />} aria-label={name}>
          <span className="hidden max-w-40 truncate sm:inline">{name}</span>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuLabel className="font-normal">
          <div className="text-sm font-medium">{me.data?.name ?? name}</div>
          <div className="text-xs text-muted-foreground">
            {me.data?.provider === "token" ? "admin token" : `signed in with ${me.data?.provider ?? "…"}`}
          </div>
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        {grafana && (
          <DropdownMenuItem asChild>
            <a href={grafana} target="_blank" rel="noreferrer">
              <ExternalLink /> Grafana
            </a>
          </DropdownMenuItem>
        )}
        <DropdownMenuItem onClick={signOut}>
          <LogOut /> Sign out
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
