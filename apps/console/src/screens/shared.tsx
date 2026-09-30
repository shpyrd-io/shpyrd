"use client";

import { Alert, AlertDescription, AlertTitle } from "@shpyrd/ui/components/alert";
import { Skeleton } from "@shpyrd/ui/components/skeleton";

// What every page shows while it loads, and when it could not.
export function Loading({ rows = 3 }: { rows?: number }) {
  return (
    <div className="grid gap-2">
      {Array.from({ length: rows }, (_, i) => (
        <Skeleton key={i} className="h-9 w-full" />
      ))}
    </div>
  );
}

export function Failed({ what, error }: { what: string; error: unknown }) {
  return (
    <Alert variant="destructive">
      <AlertTitle>Could not load {what}</AlertTitle>
      <AlertDescription>{(error as Error)?.message ?? "unknown"}</AlertDescription>
    </Alert>
  );
}

export function when(iso?: string) {
  if (!iso) return "-";
  const d = new Date(iso);
  return d.toLocaleString("en-GB", { day: "2-digit", month: "short", hour: "2-digit", minute: "2-digit" });
}

export function ago(iso: string): string {
  const s = Math.max(0, Math.floor((Date.now() - new Date(iso).getTime()) / 1000));
  if (s < 60) return `${s}s ago`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ago`;
  const h = Math.floor(m / 60);
  if (h < 48) return `${h}h ago`;
  return `${Math.floor(h / 24)}d ago`;
}

// "1.4 GiB", "512 MiB", "38 KiB".
export function bytes(n: number): string {
  if (!Number.isFinite(n) || n < 0) return "-";
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  let v = n;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v >= 100 || i === 0 ? Math.round(v) : v.toFixed(1)} ${units[i]}`;
}
