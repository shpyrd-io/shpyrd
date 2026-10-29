"use client";

import { useEffect, useRef, useState } from "react";
import { Box, Layers, Plane, Wind } from "lucide-react";
import { SectionIntro } from "@shpyrd/ui/components/section-intro";
import { cn } from "@shpyrd/ui/lib/cn";
import { testimonials } from "@/content/home";

// A proposal for the home's testimonials (Giovani, 2026-10-19, after the
// "Depoimentos" block of stripe.com/br/payments/ai): one story at a time in a
// large card that takes the colour of the customer it is about, their quote on
// the left and their mark large on the right; under it the customers' marks,
// the one being told in its colour and the others in grey. The stories turn
// by themselves, a bar under the card filling as each one plays; a mark
// chosen, or the pointer on the card, takes over. Still for who asked for
// less motion.
//
// The people and the companies are the draft testimonials' (fictional until
// the real ones come); each company is given a colour and a mark here.

const brands: Record<string, { color: string; Mark: React.ComponentType<{ className?: string }> }> = {
  Northwind: { color: "#0b6e99", Mark: Wind },
  Contoso: { color: "#2e5bff", Mark: Box },
  Fabrikam: { color: "#7c3aed", Mark: Layers },
  Tailspin: { color: "#0f9d58", Mark: Plane },
};

const stories = [testimonials.feature, ...testimonials.items].map((t) => ({ ...t, ...(brands[t.company] ?? brands.Northwind) }));

// How long each story stays.
const HOLD = 7000;

function Wordmark({ company, Mark, className }: { company: string; Mark: React.ComponentType<{ className?: string }>; className?: string }) {
  return (
    <span className={cn("inline-flex items-center gap-2 font-heading font-semibold tracking-tight", className)}>
      <Mark className="size-[1.1em]" />
      {company.toLowerCase()}
    </span>
  );
}

export function CustomerStories() {
  const [at, setAt] = useState(0);
  const [held, setHeld] = useState(false);
  const [still, setStill] = useState(true);
  // Each turn restarts the bar, from empty.
  const [turn, setTurn] = useState(0);
  const timer = useRef<number | undefined>(undefined);

  useEffect(() => setStill(window.matchMedia("(prefers-reduced-motion: reduce)").matches), []);

  useEffect(() => {
    if (still || held) return;
    timer.current = window.setTimeout(() => {
      setAt((i) => (i + 1) % stories.length);
      setTurn((t) => t + 1);
    }, HOLD);
    return () => window.clearTimeout(timer.current);
  }, [at, turn, held, still]);

  const choose = (i: number) => {
    setAt(i);
    setTurn((t) => t + 1);
  };

  const story = stories[at];

  return (
    <section className="grid gap-12">
      <SectionIntro
        variant="xlarge"
        align="center"
        label={<span className="font-medium text-primary">{testimonials.label}</span>}
        heading={testimonials.heading}
        description={testimonials.description}
      />

      <div className="grid gap-8">
        {/* The story: the card in the customer's colour, turning to the next
            one's as the stories change. */}
        <div
          data-reveal-item=""
          onPointerEnter={() => setHeld(true)}
          onPointerLeave={() => setHeld(false)}
          className="relative isolate grid min-h-[22rem] overflow-hidden rounded-2xl text-white transition-[background-color] duration-700 ease-out md:grid-cols-[minmax(0,3fr)_minmax(0,2fr)]"
          style={{ backgroundColor: story.color }}
        >
          {/* Depth: a light from the top right and the faint grid of the
              site's windows, over the colour. */}
          <div
            aria-hidden="true"
            className="pointer-events-none absolute inset-0 -z-10 bg-[radial-gradient(ellipse_70%_90%_at_85%_20%,rgb(255_255_255/0.22),transparent_60%),linear-gradient(rgb(255_255_255/0.06)_1px,transparent_1px),linear-gradient(90deg,rgb(255_255_255/0.06)_1px,transparent_1px)] bg-[length:100%_100%,24px_24px,24px_24px]"
          />

          <figure key={`q-${at}-${turn}`} className="m-0 grid content-center gap-6 p-8 sm:p-12 animate-in fade-in-0 slide-in-from-bottom-2 duration-500">
            <blockquote className="m-0 font-heading text-xl leading-snug text-balance sm:text-2xl">
              <span aria-hidden="true" className="mr-1 opacity-60">“</span>
              {story.quote}
              <span aria-hidden="true" className="ml-0.5 opacity-60">”</span>
            </blockquote>
            <figcaption className="text-sm">
              <span className="font-semibold">{story.name}</span>
              <span className="opacity-75">, {story.role}, {story.company}</span>
            </figcaption>
          </figure>

          {/* Their mark, large, where the photo will go. */}
          <div key={`m-${at}-${turn}`} className="relative hidden place-items-center md:grid animate-in fade-in-0 zoom-in-95 duration-700">
            <Wordmark company={story.company} Mark={story.Mark} className="text-5xl text-white drop-shadow-[0_6px_20px_rgb(0_0_0/0.18)]" />
          </div>

          {/* How far this story has played, one segment for each. */}
          <div className="absolute inset-x-0 bottom-5 flex justify-center gap-1.5">
            {stories.map((s, i) => (
              <span key={s.company} className="h-0.5 w-8 overflow-hidden rounded-full bg-white/30">
                <span
                  key={i === at ? `${turn}-${held}` : i}
                  className="block h-full rounded-full bg-white"
                  style={{
                    width: i < at ? "100%" : i === at && (still || held) ? "100%" : "0%",
                    animation: i === at && !still && !held ? `stories-fill ${HOLD}ms linear forwards` : undefined,
                  }}
                />
              </span>
            ))}
          </div>
        </div>

        {/* The customers: the one being told in its colour, the others grey;
            each a way to its story. */}
        <div role="tablist" aria-label="Customer stories" className="grid grid-cols-2 gap-4 sm:grid-cols-4">
          {stories.map((s, i) => (
            <button
              key={s.company}
              role="tab"
              type="button"
              aria-selected={i === at}
              onClick={() => choose(i)}
              className={cn(
                "grid place-items-center rounded-xl py-3 text-2xl transition-[color,opacity] duration-500",
                i === at ? "opacity-100" : "text-muted-foreground opacity-60 hover:opacity-100",
              )}
              style={i === at ? { color: s.color } : undefined}
            >
              <Wordmark company={s.company} Mark={s.Mark} />
            </button>
          ))}
        </div>
      </div>

    </section>
  );
}
