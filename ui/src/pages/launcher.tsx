import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { ExternalLink, Globe2, Lock, Search, Star } from "lucide-react";

import { api, openURL, type LauncherApp } from "@/lib/api";
import { usePerms } from "@/lib/me";
import { PhaseBadge } from "@/components/phase-badge";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { useUserOnly } from "@/pages/apps";

/**
 * The launcher (RFC-0033): where everyone lands after signing in. The
 * workspace's apps as tiles — every app the person may open by role, and
 * every public app — the featured ones first and larger, with a search
 * over names and descriptions. People who build have a link to their
 * projects; people who only use apps see nothing else.
 */
export function LauncherPage() {
  const perms = usePerms();
  const userOnly = useUserOnly(perms);
  const config = useQuery({
    queryKey: ["config"],
    queryFn: api.config,
    staleTime: 60_000,
  });
  const apps = useQuery({
    queryKey: ["launcher"],
    queryFn: api.launcher,
    refetchInterval: 30_000,
  });
  const [q, setQ] = useState("");
  const shown = useMemo(() => {
    const needle = q.trim().toLowerCase();
    const all = apps.data ?? [];
    if (!needle) return all;
    return all.filter((a) =>
      [a.displayName, a.slug, a.description ?? ""].some((s) =>
        s.toLowerCase().includes(needle),
      ),
    );
  }, [apps.data, q]);
  const featured = shown.filter((a) => a.featured);
  const rest = shown.filter((a) => !a.featured);
  const ws = config.data?.workspace;
  const brand = ws?.branding;

  return (
    <div className="grid gap-6">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div className="flex items-center gap-4">
          {brand?.logoUrl && (
            <img
              src={brand.logoUrl}
              alt=""
              className="h-12 max-w-40 object-contain"
            />
          )}
          <div>
            <h1 className="text-2xl font-semibold">
              {ws?.name ?? "Your apps"}
            </h1>
            <p className="text-sm text-muted-foreground">
              {userOnly
                ? "The apps you can open. Ask a project admin if one you need is missing."
                : "The apps of this workspace, as their users see them."}
              {!userOnly && (
                <>
                  {" "}
                  <Link to="/projects" className="underline underline-offset-4">
                    Manage projects
                  </Link>
                  .
                </>
              )}
            </p>
          </div>
        </div>
        {(apps.data?.length ?? 0) > 5 && (
          <div className="relative w-full sm:w-72">
            <Search className="pointer-events-none absolute left-2.5 top-2.5 size-4 text-muted-foreground" />
            <Input
              placeholder="Search apps"
              className="pl-8"
              value={q}
              onChange={(e) => setQ(e.target.value)}
              autoFocus
            />
          </div>
        )}
      </div>
      {apps.isLoading && <Skeleton className="h-24 w-full" />}
      {apps.data && apps.data.length === 0 && (
        <p className="text-sm text-muted-foreground">No apps yet.</p>
      )}
      {apps.data && apps.data.length > 0 && shown.length === 0 && (
        <p className="text-sm text-muted-foreground">Nothing matches “{q}”.</p>
      )}
      {featured.length > 0 && (
        <div className="grid gap-4 sm:grid-cols-2">
          {featured.map((a) => (
            <Tile key={a.slug} app={a} featured />
          ))}
        </div>
      )}
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {rest.map((a) => (
          <Tile key={a.slug} app={a} />
        ))}
      </div>
    </div>
  );
}

function Tile({ app: a, featured }: { app: LauncherApp; featured?: boolean }) {
  return (
    <a
      href={openURL(a.url, a.access)}
      target="_blank"
      rel="noopener"
      className={
        "group rounded-lg border bg-card transition-colors hover:border-primary " +
        (featured ? "p-6" : "p-4")
      }
    >
      <div className="flex items-start justify-between gap-2">
        <div className={featured ? "text-lg font-semibold" : "font-medium"}>
          {a.displayName}
        </div>
        <div className="flex items-center gap-1 text-muted-foreground">
          {featured && (
            <Star className="size-4 text-primary" aria-label="featured" />
          )}
          {a.access === "public" ? (
            <Globe2 className="size-4" aria-label="public" />
          ) : (
            <Lock className="size-4" aria-label="sign-in required" />
          )}
        </div>
      </div>
      {a.description && (
        <p className="mt-1 text-sm text-muted-foreground">{a.description}</p>
      )}
      <div className="mt-1 truncate font-mono text-xs text-muted-foreground">
        {a.url?.replace(/^https?:\/\//, "") ?? "not published yet"}
      </div>
      <div className="mt-3 flex items-center justify-between text-xs text-muted-foreground">
        <PhaseBadge phase={a.phase} />
        <span className="inline-flex items-center gap-1 group-hover:text-foreground">
          Open <ExternalLink className="size-3" />
        </span>
      </div>
    </a>
  );
}
