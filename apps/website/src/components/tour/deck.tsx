"use client";

// The tour's deck: one slide the whole window, moved through with the arrow
// keys, space, the buttons at the foot or the dots. The slide's number is in
// the address (#3), so a link opens the deck where it was.
import { useCallback, useEffect, useState } from "react";
import { ChevronLeft, ChevronRight, Maximize2, Minimize2 } from "lucide-react";
import { cn } from "@shpyrd/ui/lib/cn";
import { Button } from "@shpyrd/ui/components/button";
import { Wordmark } from "@shpyrd/ui/components/brand";
import { slides as names } from "@shpyrd/content/site/tour";
import { BinaryOcean } from "@/components/binary-ocean";

const pad = (n: number) => String(n).padStart(2, "0");

export function Deck({ slides }: { slides: ((go: (to: number) => void) => React.ReactNode)[] }) {
  const [at, setAt] = useState(0);
  const [full, setFull] = useState(false);
  const last = slides.length - 1;

  const go = useCallback(
    (to: number) => {
      const next = Math.max(0, Math.min(last, to));
      setAt(next);
      window.history.replaceState(null, "", next === 0 ? window.location.pathname : `#${next + 1}`);
    },
    [last],
  );

  // The address's slide, read once the page is in the browser.
  useEffect(() => {
    const n = Number(window.location.hash.slice(1));
    if (n >= 1 && n <= slides.length) setAt(n - 1);
  }, [slides.length]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      // Keys a slide's own control took (tabs move with the arrows) stay its own.
      if (e.defaultPrevented || e.metaKey || e.ctrlKey || e.altKey) return;
      // What has the focus: an element, or nothing in particular (the window).
      const target = e.target instanceof Element ? e.target : null;
      if (target?.closest("input, textarea, select, [role=slider]")) return;
      if (["ArrowRight", "PageDown"].includes(e.key) || (e.key === " " && !target?.closest("button, a"))) {
        e.preventDefault();
        go(at + 1);
      } else if (["ArrowLeft", "PageUp"].includes(e.key)) {
        e.preventDefault();
        go(at - 1);
      } else if (e.key === "Home") go(0);
      else if (e.key === "End") go(last);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [at, go, last]);

  useEffect(() => {
    const onChange = () => setFull(Boolean(document.fullscreenElement));
    document.addEventListener("fullscreenchange", onChange);
    return () => document.removeEventListener("fullscreenchange", onChange);
  }, []);
  const toggleFull = () =>
    document.fullscreenElement ? document.exitFullscreen() : document.documentElement.requestFullscreen();

  return (
    <div className="relative isolate flex min-h-svh flex-col overflow-hidden bg-background">
      {/* A faint grid under everything, fading out toward the foot. */}
      <div
        aria-hidden="true"
        className="pointer-events-none absolute inset-0 -z-10 bg-[linear-gradient(to_right,var(--color-foreground)_1px,transparent_1px),linear-gradient(to_bottom,var(--color-foreground)_1px,transparent_1px)] bg-[size:48px_48px] opacity-[0.04] [mask-image:radial-gradient(ellipse_at_top,black_40%,transparent_85%)]"
      />
      {/* The home page's binary ocean under the first slide. */}
      <div
        aria-hidden="true"
        className={cn(
          "pointer-events-none absolute inset-x-0 bottom-0 -z-10 h-[55%] transition-opacity duration-slow ease-move [mask-image:linear-gradient(to_bottom,transparent,black_35%)]",
          at === 0 ? "opacity-100" : "opacity-0",
        )}
      >
        {at === 0 && <BinaryOcean horizon={0.2} className="size-full" />}
      </div>
      {/* How far into the deck: a line along the top. */}
      <div aria-hidden="true" className="absolute inset-x-0 top-0 h-0.5 bg-border">
        <div className="h-full bg-primary transition-[width] duration-slow ease-move" style={{ width: `${((at + 1) / slides.length) * 100}%` }} />
      </div>

      <header className="flex items-center justify-between px-6 pt-5 sm:px-8">
        <a href="/" className="flex items-center gap-2" aria-label="shpyrd">
          <Wordmark className="h-6 w-auto" />
        </a>
        <Button variant="ghost" size="icon" onClick={toggleFull} aria-label={full ? "Leave full screen" : "Full screen"}>
          {full ? <Minimize2 /> : <Maximize2 />}
        </Button>
      </header>

      <main className="flex flex-1 items-center px-6 py-8 sm:px-8">
        <section
          key={at}
          aria-roledescription="slide"
          aria-label={`${at + 1} of ${slides.length}: ${names[at]}`}
          className="mx-auto w-full max-w-6xl animate-in fade-in slide-in-from-bottom-2 duration-slow ease-enter"
        >
          {slides[at](go)}
        </section>
      </main>

      <footer className="flex items-center justify-between gap-4 border-t border-border/60 bg-background px-6 py-4 sm:px-8">
        <Button variant="outline" size="icon" onClick={() => go(at - 1)} disabled={at === 0} aria-label="Previous slide">
          <ChevronLeft />
        </Button>
        <div className="flex items-center gap-5">
          <nav aria-label="Slides" className="hidden items-center gap-1.5 sm:flex">
            {slides.map((_, i) => (
              <button
                key={i}
                type="button"
                onClick={() => go(i)}
                aria-label={`${i + 1}: ${names[i]}`}
                aria-current={i === at ? "step" : undefined}
                className={cn(
                  "h-1.5 rounded-full transition-[width,background-color] duration-normal ease-move",
                  i === at ? "w-6 bg-primary" : "w-1.5 bg-foreground/20 hover:bg-foreground/40",
                )}
              />
            ))}
          </nav>
          <p className="text-xs text-muted-foreground tabular-nums" aria-live="polite">
            {pad(at + 1)} / {pad(slides.length)} · {names[at]}
          </p>
        </div>
        <Button size="icon" onClick={() => go(at + 1)} disabled={at === last} aria-label="Next slide">
          <ChevronRight />
        </Button>
      </footer>
    </div>
  );
}
