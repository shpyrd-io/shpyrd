"use client";

import { useEffect, useRef, useState, type DependencyList } from "react";
import { Alert, AlertDescription, AlertTitle } from "@shpyrd/ui/components/alert";
import { Skeleton } from "@shpyrd/ui/components/skeleton";

// What every page of the project shows while it loads, and when it could not.
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

// How long something took, or has taken so far.
export function duration(start: string, end?: string) {
  const s = Math.max(0, Math.round(((end ? new Date(end).getTime() : Date.now()) - new Date(start).getTime()) / 1000));
  return s < 60 ? `${s}s` : `${Math.floor(s / 60)}m ${s % 60}s`;
}

// Lines that keep coming: what a stream gave so far, kept to the last
// `keep`, gathered every 100 ms so a fast stream does not redraw per line.
// `start` opens the stream, and is opened again when `deps` change; null
// opens nothing.
export function useStream<T>(start: ((signal: AbortSignal, push: (line: T) => void) => Promise<void>) | null, deps: DependencyList, keep = 5000) {
  const [lines, setLines] = useState<T[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [done, setDone] = useState(false);
  const [generation, setGeneration] = useState(0);
  const pending = useRef<T[]>([]);
  useEffect(() => {
    setLines([]);
    setError(null);
    setDone(false);
    if (!start) return;
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout> | null = null;
    const flush = () => {
      timer = null;
      if (pending.current.length === 0) return;
      const batch = pending.current;
      pending.current = [];
      setLines((prev) => (prev.length + batch.length > keep ? [...prev, ...batch].slice(-keep) : [...prev, ...batch]));
    };
    start(controller.signal, (line) => {
      pending.current.push(line);
      if (!timer) timer = setTimeout(flush, 100);
    })
      .then(() => {
        flush();
        if (!controller.signal.aborted) setDone(true);
      })
      .catch((e: Error) => {
        if (e.name !== "AbortError") setError(e.message);
      });
    return () => {
      controller.abort();
      if (timer) clearTimeout(timer);
      pending.current = [];
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [...deps, generation, keep]);
  return { lines, error, done, restart: () => setGeneration((g) => g + 1) };
}
