"use client";

// The tour's deck: one slide the whole window, moved through with the arrow
// keys, space, the buttons at the foot or the dots; Home and End go to the
// first and the last, the number keys to slides 1 to 9, and the count at the
// foot opens the list of every slide. The slide's number is in
// the address (#3), so a link opens the deck where it was.
import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
} from "react";
import {
  ChartNoAxesColumnIncreasing,
  ChevronLeft,
  ChevronRight,
  ChevronsUpDown,
  Cloud,
  Compass,
  Fingerprint,
  Flag,
  Layers,
  LayoutGrid,
  Maximize2,
  Minimize2,
  Rocket,
  ScanSearch,
  TriangleAlert,
  Users,
} from "lucide-react";
import { glass } from "@shpyrd/ui/lib/glass";
import { cn } from "@shpyrd/ui/lib/cn";
import { Button } from "@shpyrd/ui/components/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@shpyrd/ui/components/dropdown-menu";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@shpyrd/ui/components/tooltip";
import { Wordmark } from "@shpyrd/ui/components/brand";
import { slides as names } from "@/content/tour-v3";
import { BinaryOcean } from "@/components/binary-ocean";
import { signUp } from "@/lib/signup";
import { IconPrism, type IconName } from "./icon-prism";

// Each slide's label icon, by its place in the deck. The opening and the
// close have none (Giovani, 2026-10-27).
const prisms: Record<number, IconName> = {
  1: "triangle-alert",
  2: "scan-search",
  3: "layers",
  4: "rocket",
  5: "fingerprint",
  6: "chart",
  7: "cloud",
  8: "layout-grid",
  9: "users",
};

// Each slide in the list at the foot, by its label's icon; the close, with
// none, by a flag.
const menuIcons = [
  <Compass key="0" />,
  <TriangleAlert key="1" />,
  <ScanSearch key="2" />,
  <Layers key="3" />,
  <Rocket key="4" />,
  <Fingerprint key="5" />,
  <ChartNoAxesColumnIncreasing key="6" />,
  <Cloud key="7" />,
  <LayoutGrid key="8" />,
  <Users key="9" />,
  <Flag key="10" />,
];

const pad = (n: number) => String(n).padStart(2, "0");

// A phone: the slides stack their parts, each slide as tall as itself.
const phone = () =>
  typeof window !== "undefined" &&
  window.matchMedia("(max-width: 767px)").matches;

export function Deck({
  slides,
}: {
  slides: ((go: (to: number) => void) => React.ReactNode)[];
}) {
  const [at, setAt] = useState(0);
  const [full, setFull] = useState(false);
  const last = slides.length - 1;

  // The tallest slide sets one height for all (Giovani, 2026-10-27): that
  // height is centred in the window, as the slides were, and every slide
  // starts at its top, so the label and heading stay put from one slide to
  // the next. Measured once with every slide laid in the same cell, out of
  // sight, and again when the window or the fonts change.
  const [tallest, setTallest] = useState<number | null>(null);
  const [measuring, setMeasuring] = useState(true);
  const measure = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    if (!measuring || !measure.current) return;
    // On a phone each slide is as tall as itself (Giovani, 2026-10-29): the
    // slides stack their parts there, and the tallest would leave the
    // others a long empty page.
    setTallest(phone() ? null : measure.current.offsetHeight);
    setMeasuring(false);
  }, [measuring]);
  // On a phone the next slide starts at its top.
  useEffect(() => {
    if (phone()) window.scrollTo({ top: 0 });
  }, [at]);
  useEffect(() => {
    let timer = 0;
    const again = () => {
      window.clearTimeout(timer);
      timer = window.setTimeout(() => setMeasuring(true), 150);
    };
    window.addEventListener("resize", again);
    document.fonts?.ready.then(again);
    return () => {
      window.removeEventListener("resize", again);
      window.clearTimeout(timer);
    };
  }, []);

  const go = useCallback(
    (to: number) => {
      const next = Math.max(0, Math.min(last, to));
      setAt(next);
      window.history.replaceState(
        null,
        "",
        next === 0 ? window.location.pathname : `#${next + 1}`,
      );
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
      if (
        ["ArrowRight", "PageDown"].includes(e.key) ||
        (e.key === " " && !target?.closest("button, a"))
      ) {
        e.preventDefault();
        go(at + 1);
      } else if (["ArrowLeft", "PageUp"].includes(e.key)) {
        e.preventDefault();
        go(at - 1);
      } else if (e.key === "Home") go(0);
      else if (e.key === "End") go(last);
      // A slide by its number, 1 to 9, as in a presentation app.
      else if (/^[1-9]$/.test(e.key) && Number(e.key) <= slides.length)
        go(Number(e.key) - 1);
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
    document.fullscreenElement
      ? document.exitFullscreen()
      : document.documentElement.requestFullscreen();

  return (
    // The new home's ground (2026-10-20): its breath of grey, no grid.
    <div className="relative isolate flex min-h-svh flex-col overflow-hidden bg-(--page-ground)">
      {/* The home page's binary ocean under the first slide, as the home
          draws it (Giovani, 2026-10-27: it read stronger here): its own
          horizon, behind the slide's height as behind the home's hero,
          faded in at the top and out at the foot by the home's mask. Was
          the foot 55% of the window, the horizon high (0.2) and no fade at
          the foot, so its nearest, largest digits stood full at the bottom. */}
      <div
        aria-hidden="true"
        className={cn(
          "pointer-events-none absolute inset-x-0 top-6 -z-10 h-[calc(76svh+4rem)] transition-opacity duration-slow ease-move [mask-image:linear-gradient(to_bottom,transparent,black_15%,black_70%,transparent)]",
          at === 0 ? "opacity-100" : "opacity-0",
        )}
      >
        {at === 0 && <BinaryOcean className="size-full" />}
      </div>
      {/* Behind every slide with an icon in its label, that icon made
          solid, in characters, large, up and to the right behind the
          slide's right edge, turning to the pointer (Giovani, 2026-10-27;
          first slide 2's warning sign, then every slide's). The slide's
          neighbours are drawn too, still, so one fades into the next. */}
      {Object.entries(prisms).map(([n, icon]) => {
        const i = Number(n);
        return (
          <div
            key={i}
            aria-hidden="true"
            className={cn(
              "pointer-events-none absolute top-[calc(50%-300px)] left-[calc(50%+246px)] -z-10 size-[44rem] -translate-y-1/2 transition-opacity duration-slow ease-move",
              at === i ? "opacity-100" : "opacity-0",
            )}
          >
            {Math.abs(at - i) <= 1 && (
              <IconPrism icon={icon} active={at === i} className="size-full" />
            )}
          </div>
        );
      })}
      {/* How far into the deck: a line along the top. */}
      <div
        aria-hidden="true"
        className="absolute inset-x-0 top-0 h-0.5 bg-border"
      >
        <div
          className="h-full bg-primary transition-[width] duration-slow ease-move"
          style={{ width: `${((at + 1) / slides.length) * 100}%` }}
        />
      </div>

      <header className="flex items-center justify-between px-6 pt-5 sm:px-8">
        <a href="/" className="flex items-center gap-2" aria-label="shpyrd">
          <Wordmark className="h-6 w-auto" />
        </a>
        {/* The way in, always at hand, as in the site's header. */}
        <span className="flex items-center gap-2">
          <Button size="sm" asChild>
            <a href={signUp}>Start free</a>
          </Button>
          <Button
            variant="ghost"
            size="icon"
            onClick={toggleFull}
            aria-label={full ? "Leave full screen" : "Full screen"}
          >
            {full ? <Minimize2 /> : <Maximize2 />}
          </Button>
        </span>
      </header>

      <main className="relative flex flex-1 items-center px-6 py-8 sm:px-8">
        {measuring && (
          <div
            aria-hidden="true"
            className="pointer-events-none invisible absolute inset-x-6 top-0 sm:inset-x-8"
          >
            <div ref={measure} className="mx-auto grid w-full max-w-6xl">
              {slides.map((slide, i) => (
                <div key={i} className="[grid-area:1/1]">
                  {slide(() => {})}
                </div>
              ))}
            </div>
          </div>
        )}
        <section
          key={at}
          aria-roledescription="slide"
          aria-label={`${at + 1} of ${slides.length}: ${names[at]}`}
          style={{ minHeight: tallest ?? undefined }}
          // The slide comes in as the site's sections do (Giovani, 2026-10-27):
          // its label, heading and line, then its blocks, each rising 16px into
          // place over 700ms, 80ms apart (.deck-rise, src/styles/global.css);
          // the slide itself does not move.
          className="deck-rise mx-auto w-full max-w-6xl"
        >
          {slides[at](go)}
        </section>
      </main>

      <TooltipProvider delayDuration={150}>
        <footer className="grid grid-cols-[1fr_auto_1fr] items-center gap-4 border-t border-border/60 bg-(--page-ground) px-6 py-4 sm:px-8">
          <Button
            variant="outline"
            size="icon-xl"
            className="justify-self-start"
            onClick={() => go(at - 1)}
            disabled={at === 0}
            aria-label="Previous slide"
          >
            <ChevronLeft />
          </Button>
          {/* Held still as the slides change (Giovani, 2026-10-27): the dots
              in the middle column, centred under the slide, the count in the
              column after them, so only the count's words change length. */}
          <div className="flex justify-center">
            {/* The dots, a little larger (Giovani, 2026-10-27): under the
                pointer one grows and shows its slide's number inside, and
                the balloon over it says the slide's name. */}
            <nav aria-label="Slides" className="hidden items-center sm:flex">
              {slides.map((_, i) => (
                <Tooltip key={i}>
                  <TooltipTrigger asChild>
                    <button
                      type="button"
                      onClick={() => go(i)}
                      aria-label={`${i + 1}: ${names[i]}`}
                      aria-current={i === at ? "step" : undefined}
                      className="group flex size-7 items-center justify-center"
                    >
                      <span
                        className={cn(
                          "flex h-2 items-center justify-center rounded-full transition-[width,height,background-color] duration-normal ease-move group-hover:h-5 group-focus-visible:h-5",
                          i === at
                            ? "w-6 bg-primary text-primary-foreground"
                            : "w-2 bg-foreground/20 text-foreground group-hover:w-5 group-hover:bg-foreground/10 group-focus-visible:w-5",
                        )}
                      >
                        <span className="text-[10px] leading-none font-medium tabular-nums opacity-0 transition-opacity duration-fast group-hover:opacity-100 group-focus-visible:opacity-100">
                          {i + 1}
                        </span>
                      </span>
                    </button>
                  </TooltipTrigger>
                  <TooltipContent side="top">{names[i]}</TooltipContent>
                </Tooltip>
              ))}
            </nav>
          </div>
          <div className="flex items-center justify-between gap-4">
            {/* Where we are, and every slide from it: the count opens the list
              of all slides, to go straight to any (Giovani, 2026-10-27). */}
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button
                  variant="ghost"
                  size="sm"
                  className="text-xs font-normal text-muted-foreground tabular-nums"
                  iconEnd={<ChevronsUpDown />}
                >
                  {pad(at + 1)} / {pad(slides.length)} · {names[at]}
                </Button>
              </DropdownMenuTrigger>
              {/* As the site's main menu (Giovani, 2026-10-27): its glass
                  panel, room inside, a small grey heading, and each slide as
                  a page there, its icon in a tile that turns orange in white
                  under the pointer; in two columns, the slides in order. */}
              <DropdownMenuContent
                side="top"
                align="center"
                sideOffset={12}
                className={cn(
                  glass,
                  "w-auto max-h-none overflow-visible p-4 text-foreground",
                )}
              >
                <DropdownMenuLabel className="px-3 pt-2 pb-2 text-xs font-medium text-muted-foreground">
                  Go to slide
                </DropdownMenuLabel>
                <div className="grid grid-flow-col grid-rows-6 gap-x-6 gap-y-1.5">
                  {names.map((name, i) => (
                    <DropdownMenuItem
                      key={i}
                      onSelect={() => go(i)}
                      data-active={i === at || undefined}
                      className="group/slide w-60 gap-4 rounded-lg p-3 text-sm outline-none transition-colors duration-fast ease-move focus:bg-muted data-[active]:bg-muted/60"
                    >
                      <span
                        aria-hidden="true"
                        className="grid size-10 shrink-0 place-items-center rounded-md bg-muted text-muted-foreground ring-1 ring-foreground/5 transition-colors group-focus/slide:bg-background group-focus/slide:text-primary group-data-[active]/slide:text-primary [&_svg]:size-5"
                      >
                        {menuIcons[i]}
                      </span>
                      <span className="grid gap-1">
                        <span className="font-medium text-foreground">
                          {name}
                        </span>
                        <span className="text-xs leading-snug text-muted-foreground tabular-nums">
                          Slide {pad(i + 1)}
                        </span>
                      </span>
                    </DropdownMenuItem>
                  ))}
                </div>
              </DropdownMenuContent>
            </DropdownMenu>
            <p className="sr-only" aria-live="polite">
              {at + 1} of {slides.length}: {names[at]}
            </p>
            <Button
              size="icon-xl"
              onClick={() => go(at + 1)}
              disabled={at === last}
              aria-label="Next slide"
            >
              <ChevronRight />
            </Button>
          </div>
        </footer>
      </TooltipProvider>
    </div>
  );
}
