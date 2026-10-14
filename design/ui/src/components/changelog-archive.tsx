"use client";

import { useId, useState, type ReactNode } from "react";
import { Button } from "./button";
import { GitCommitHorizontal } from "lucide-react";
import { Tile } from "./tile";

export function ChangelogArchive({ entries, title, loadMore }: {
  entries: { slug: string; year: string; content: ReactNode }[];
  title: string; loadMore: string;
}) {
  const id = useId();
  const [count, setCount] = useState(10);
  const visible = entries.slice(0, count);
  const years = [...new Set(visible.map((entry) => entry.year))];
  if (!entries.length) return null;
  return (
    <section aria-label={title} className="@container grid gap-10">
      <div className="relative grid gap-12 before:absolute before:inset-y-4 before:left-4 before:w-px before:-translate-x-1/2 before:bg-border">
      {years.map((year) => (
        <section key={year} aria-labelledby={`${id}-year-${year}`} className="relative grid gap-6 pl-12 @3xl:grid-cols-[7rem_minmax(0,1fr)] @3xl:gap-8 @3xl:pl-14">
          <Tile aria-hidden size="sm" variant="muted" className="absolute top-0 left-0 border"><GitCommitHorizontal /></Tile>
          <h2 id={`${id}-year-${year}`} className="pt-1 font-mono text-lg text-muted-foreground @3xl:sticky @3xl:top-6 @3xl:self-start">{year}</h2>
          <ul className="min-w-0">{visible.filter((entry) => entry.year === year).map((entry) => entry.content)}</ul>
        </section>
      ))}
      </div>
      {count < entries.length && <Button variant="outline" className="justify-self-center" onClick={() => setCount((current) => current + 10)}>{loadMore}</Button>}
    </section>
  );
}
