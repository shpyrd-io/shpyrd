"use client";

// "Not the one who built it?" as the reference Giovani gave (2026-11-11,
// iugu.com/split-pagamentos): the three ways as buttons on the left, the
// film of the one chosen on the right, as tall as the three buttons. They
// take their turn by themselves, a bar down the chosen button's side
// filling as it plays (as on the manifesto's slide 5); under the pointer or
// the keyboard's focus a button is chosen and held; a click goes to its
// page. Still for who asked for less motion.
import { useEffect, useLayoutEffect, useRef, useState } from "react";
import Link from "next/link";
import { cn } from "@shpyrd/ui/lib/cn";
import { glass } from "@shpyrd/ui/lib/glass";
import { PointerEdge } from "@/components/pointer-edge";

type Item = { href: string; title: string; body: string };

// How long each way plays before the next.
const HOLD = 8000;

export function AlsoTabs({
  items,
  icons,
  films,
  className,
}: {
  className?: string;
  items: Item[];
  icons: Record<string, React.ReactElement>;
  films: Record<string, React.ReactElement>;
}) {
  const [at, setAt] = useState(0);
  // Held while the pointer or the focus is on a button; still until the
  // block is on the screen, and for who asked for less motion.
  const [held, setHeld] = useState(false);
  const [still, setStill] = useState(true);
  // Each turn its own, so the bar starts again on the same button.
  const [turn, setTurn] = useState(0);
  const root = useRef<HTMLDivElement>(null);
  const card = useRef<HTMLDivElement>(null);
  // A dotted grey thread from the chosen button to the film, as slide 5's
  // (flow.tsx): out of the button's right side, a rounded turn, and to the
  // middle of the film's card.
  const [thread, setThread] = useState<string | null>(null);
  useLayoutEffect(() => {
    const box = root.current;
    if (!box || !card.current) return;
    const measure = () => {
      const o = box.getBoundingClientRect();
      const from = box
        .querySelector(`[data-way="${at}"]`)
        ?.getBoundingClientRect();
      const to = card.current!.getBoundingClientRect();
      if (!from) return;
      const x0 = from.right - o.left;
      const y0 = from.top + from.height / 2 - o.top;
      // To the film's card, its left edge, at its middle (Giovani,
      // 2026-11-11): the card, not a piece of the film inside it.
      const x1 = to.left - o.left;
      const y1 = to.top + to.height / 2 - o.top;
      const mx = (x0 + x1) / 2;
      const dir = Math.sign(y1 - y0);
      const k = Math.min(12, Math.abs(y1 - y0) / 2, (x1 - x0) / 2);
      setThread(
        `M ${x0} ${y0} L ${mx - k} ${y0} Q ${mx} ${y0} ${mx} ${y0 + dir * k} ` +
          `L ${mx} ${y1 - dir * k} Q ${mx} ${y1} ${mx + k} ${y1} L ${x1} ${y1}`,
      );
    };
    measure();
    const seen = new ResizeObserver(measure);
    seen.observe(box);
    return () => seen.disconnect();
  }, [at]);

  useEffect(() => {
    const el = root.current;
    if (!el || window.matchMedia("(prefers-reduced-motion: reduce)").matches)
      return;
    const sight = new IntersectionObserver(
      ([e]) => setStill(!e.isIntersecting),
      { threshold: 0.4 },
    );
    sight.observe(el);
    return () => sight.disconnect();
  }, []);

  const next = () => {
    start((at + 1) % items.length);
    setTurn((t) => t + 1);
  };
  const choose = (i: number) => {
    if (i !== at) start(i);
    setHeld(true);
  };
  // How many times each way has been chosen: a new count starts its film
  // again.
  const [runs, setRuns] = useState(() => items.map(() => 0));
  const start = (i: number) => {
    setAt(i);
    setRuns((r) => r.map((n, k) => (k === i ? n + 1 : n)));
  };

  return (
    <div
      ref={root}
      className={cn(
        "relative grid gap-6 lg:grid-cols-[minmax(0,1fr)_minmax(0,1.35fr)] lg:gap-12",
        className,
      )}
    >
      {thread && (
        <svg
          aria-hidden="true"
          className="pointer-events-none absolute inset-0 z-10 size-full overflow-visible max-lg:hidden"
        >
          <path
            d={thread}
            fill="none"
            className="stroke-muted-foreground/40 transition-[d] duration-slow ease-move"
            strokeWidth={1.5}
            strokeDasharray="1 5"
            strokeLinecap="round"
            style={{ d: `path("${thread}")` }}
          />
        </svg>
      )}
      {/* Under the pointer each button grows, its edge lit round the
          pointer and a soft orange shadow under it, as the feature blocks
          above (src/styles/feature-blocks.css). */}
      <PointerEdge />
      <ul
        className="edge-lit grid content-start gap-3"
        onMouseLeave={() => {
          setHeld(false);
          setTurn((t) => t + 1);
        }}
      >
        {items.map((a, i) => {
          const here = i === at;
          return (
            <li key={a.href}>
              {/* As the steps of the manifesto's slide 5 (Giovani,
                  2026-11-11): a card, its icon in the route's 64px box (no
                  number: they are not steps), filled orange on the chosen one,
                  the card tinted and edged orange, its line orange; the time
                  it plays in a bar down its right side. */}
              <Link
                href={a.href}
                data-way={i}
                data-tile=""
                onMouseEnter={() => choose(i)}
                onFocus={() => choose(i)}
                onBlur={() => {
                  setHeld(false);
                  setTurn((t) => t + 1);
                }}
                className={cn(
                  glass,
                  "group/also relative flex items-center gap-4 overflow-hidden px-3 py-3 pr-6 shadow-none transition-[box-shadow,background-color] duration-500 ease-out dark:shadow-none",
                  here &&
                    "bg-primary/5 ring-primary/50 dark:bg-primary/10 dark:ring-primary/50",
                )}
              >
                <span
                  aria-hidden="true"
                  className={cn(
                    "relative flex size-16 shrink-0 items-center justify-center rounded-2xl ring-1 transition-[background-color,color,box-shadow,translate] duration-500 ease-out group-hover/also:-translate-y-0.5 [&_svg]:size-7",
                    here
                      ? "bg-primary text-primary-foreground shadow-[inset_0_1px_0_rgb(255_255_255/0),0_8px_20px_-6px_rgb(255_79_0/0.55)] ring-primary"
                      : "bg-background text-muted-foreground shadow-[inset_0_1px_0_rgb(255_255_255/0.65),0_8px_20px_rgb(20_20_30/0.06)] ring-foreground/10 group-hover/also:text-primary group-hover/also:ring-primary/40 dark:bg-card",
                  )}
                >
                  {icons[a.href]}
                </span>
                <span className="grid gap-0.5">
                  <span className="text-base font-semibold text-foreground">
                    {a.title}
                  </span>
                  <span
                    className={cn(
                      "text-sm transition-colors duration-500 ease-out",
                      here ? "text-primary" : "text-muted-foreground",
                    )}
                  >
                    {a.body}
                  </span>
                </span>
                {/* How long until the next: always there, shown on the
                    chosen one, so it fades rather than pops. */}
                <span
                  aria-hidden="true"
                  className={cn(
                    "absolute inset-y-3 right-2 w-1 overflow-hidden rounded-full bg-primary/15 transition-opacity duration-500 ease-out",
                    here ? "opacity-100" : "opacity-0",
                  )}
                >
                  {here && (
                    <span
                      key={`${turn}-${held}-${still}`}
                      className="block w-full rounded-full bg-primary"
                      style={{
                        height: still || held ? "100%" : "0%",
                        animation:
                          !still && !held
                            ? `also-fill ${HOLD}ms linear forwards`
                            : undefined,
                      }}
                      onAnimationEnd={next}
                    />
                  )}
                </span>
              </Link>
            </li>
          );
        })}
      </ul>
      {/* The film of the one chosen, in a card of the home's glass, as tall
          as the three buttons beside it (on a phone, under them, 4:3); it
          starts again at each turn. Rests on the page: no shadow. */}
      <div
        ref={card}
        className={cn(
          glass,
          "relative p-2.5 shadow-none max-lg:aspect-[4/3] dark:shadow-none",
        )}
      >
        {/* Every film laid in the card, the chosen one shown: the one left
            fades out as the next fades in, rather than the card going blank
            between them. A film starts again each time it is chosen. */}
        {items.map((a, i) => (
          <div
            key={`${a.href}-${runs[i]}`}
            aria-hidden="true"
            className={cn(
              "stage absolute inset-2.5 rounded-2xl border border-white/80 transition-[opacity,visibility] duration-500 ease-out dark:border-white/10",
              i === at
                ? "visible opacity-100 animate-in fade-in duration-500"
                : "invisible opacity-0",
            )}
          >
            {films[a.href]}
          </div>
        ))}
      </div>
    </div>
  );
}
