"use client";

import { useEffect, useRef, useState } from "react";
import {
  CalendarClock, Cloud, FileSignature, FileSpreadsheet, Laptop, Link2, Lock, LockOpen, Megaphone, Moon, Package,
  Percent, Receipt, ShieldCheck, Smile, TrendingUp, UserPlus,
} from "lucide-react";
import { LogoMark } from "@shpyrd/ui/components/brand";
import { AppWindow } from "@shpyrd/ui/components/app-window";
import { glass } from "@shpyrd/ui/lib/glass";
import { cn } from "@shpyrd/ui/lib/cn";
import { apps } from "@/content/tour";

// A proposal for the "For businesses" picture (Giovani, 2026-10-19), told the
// way the hero's chat is: a short story in two steps, slow enough to read.
//   1. Today: the company's apps lie in four separate windows (a laptop,
//      someone's own cloud, links passed around, spreadsheets), every one
//      open to anyone.
//   2. On shpyrd: they fly into one window, the company's, behind one
//      sign-in; each locked, each labelled with its team.
// It ends there, on the apps in order (Giovani, 2026-10-19).
// The words beside it are the block's own; only the picture changes. Still,
// on its last step, for who asked for less motion.

const looks: Record<string, [React.ReactElement, string]> = {
  expenses: [<Receipt key="r" />, "text-chart-1"],
  onboarding: [<UserPlus key="u" />, "text-chart-4"],
  pipeline: [<TrendingUp key="t" />, "text-chart-2"],
  "nps-board": [<Smile key="s" />, "text-chart-3"],
  campaigns: [<Megaphone key="m" />, "text-chart-1"],
  contracts: [<FileSignature key="f" />, "text-chart-4"],
  inventory: [<Package key="p" />, "text-chart-2"],
  commissions: [<Percent key="c" />, "text-chart-3"],
  shifts: [<CalendarClock key="k" />, "text-muted-foreground"],
};
const asleep = new Set(["onboarding", "contracts", "shifts"]);

// The four places the apps live today, as [left, top, width, height] in % of
// the picture, with what they are.
const places = [
  // Out of line on purpose (Giovani, 2026-10-19): each a little higher or
  // lower, larger or smaller, as windows left open anyhow; upright.
  { label: "on a laptop", Icon: Laptop, box: [1, 8, 44, 41] },
  { label: "someone's own cloud", Icon: Cloud, box: [50, 16, 48, 37] },
  { label: "links passed around", Icon: Link2, box: [4, 50, 41, 44] },
  { label: "spreadsheets", Icon: FileSpreadsheet, box: [48, 54, 50, 42] },
] as const;

// Where each app is, in order of `apps`: [left, top, rotate] while scattered
// (inside its place), then its cell in the company's window.
const scattered: [number, number, number][] = [
  [5, 21, -5], [16, 34, 2], // laptop: expenses, onboarding
  [54, 29, 5], // cloud: pipeline
  [8, 62, 4], [19, 75, -2], // links: nps-board, campaigns
  [50, 64, -4], [71, 70, 5], // spreadsheets: contracts, inventory
  [66, 40, -4], // cloud: commissions
  [53, 79, 3], // spreadsheets: shifts
];
// The place each app lies in while scattered, in the same order.
const homes = [0, 0, 1, 2, 2, 3, 3, 1, 3];
// In the company's window, three columns of cards 26% wide with 3% between
// them; the rows are spaced by the same gap, worked out from the cards'
// height at whatever size the picture is drawn (see `rowTops`).
const cols = [8, 37, 66];
const GAP = 3; // % of the picture's width, between columns and between rows.
// The windows' top, in % of the picture's height: the same room under the
// title in both scenes (the top four windows start here too).
const TOP = 12;

// Every window here as the site draws its windows (the home hero's): a frame
// of glass, and in it the window, light, rounded, its bar faint.
const frame = cn(glass, "rounded-2xl p-2.5");
const windowLook =
  "rounded-2xl border-white/80 bg-background bg-linear-to-b from-background to-muted shadow-none dark:border-white/10 dark:from-card dark:to-background [&_[data-slot=app-window-bar]]:border-foreground/6 [&_[data-slot=app-window-bar]]:bg-transparent";

// The steps, and how long each stays.
const STEPS = [
  ["today", 4200],
  ["gathered", 6000],
] as const;
type Step = (typeof STEPS)[number][0];

export function CompanyApps({ className }: { className?: string }) {
  const [step, setStep] = useState<Step>("today");
  const [still, setStill] = useState<boolean | null>(null);

  useEffect(() => {
    const less = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    setStill(less);
    if (less) setStep("gathered");
  }, []);

  // The picture's size and a card's height, to space the rows by the same
  // gap in pixels as the columns.
  const box = useRef<HTMLDivElement>(null);
  const [rowTops, setRowTops] = useState<number[]>([27, 47, 67]);
  // The company window's foot, in px from the picture's: as far under the
  // last row as the cards are from its sides.
  const [winBottom, setWinBottom] = useState<number | null>(null);
  // The same foot in % of the picture's height: the four windows of the
  // first scene are fitted between the window's top and this, so both
  // scenes take the same height (Giovani, 2026-10-20).
  const [footPct, setFootPct] = useState<number | null>(null);
  // A card's height in % of the picture's: a scattered card is fitted by its
  // foot, so it stays in its window as the window gets shorter.
  const [cardPct, setCardPct] = useState(0);
  // A window's bar and frame in %, under which a scattered card starts, so
  // the place's name stays in sight.
  const [barPct, setBarPct] = useState(0);
  // How far the whole picture moves so the window is centred on the words.
  const [shift, setShift] = useState(0);
  const shiftRef = useRef(0);
  // How far the company's window reaches under the picture's foot, where
  // the cards need more room than the picture's shape gives (a phone): the
  // picture keeps that much room under it, so the words below stay clear
  // (Giovani, 2026-10-29).
  const [overhang, setOverhang] = useState(0);
  useEffect(() => {
    const el = box.current;
    if (!el) return;
    const lay = () => {
      const card = el.querySelector<HTMLElement>("[data-card]");
      const win = el.querySelector<HTMLElement>("[data-company]");
      const bar = win?.querySelector<HTMLElement>("[data-slot=app-window-bar]");
      if (!card || !win || !bar) return;
      const { width, height } = el.getBoundingClientRect();
      // The window's own edge, inside its frame (10px) and border (1px).
      const inset = 11;
      const winLeft = (2 / 100) * width + inset;
      const winTop = (TOP / 100) * height + inset;
      // As much room above the cards (under the bar) and under them as
      // beside them.
      const side = (cols[0] / 100) * width - winLeft;
      const gap = (GAP / 100) * width;
      const first = winTop + bar.offsetHeight + side;
      const tops = [0, 1, 2].map((r) => first + r * (card.offsetHeight + gap));
      const bottom = tops[2] + card.offsetHeight + side + inset;
      setRowTops(tops.map((t) => (t / height) * 100));
      setWinBottom(height - bottom);
      setFootPct((bottom / height) * 100);
      setCardPct((card.offsetHeight / height) * 100);
      setBarPct(((bar.offsetHeight + inset) / height) * 100);
      setOverhang(Math.max(0, bottom - height));
      // Centre the company's window on the words beside it (their label to
      // their buttons), whatever the title over it adds: the picture moves by
      // the difference.
      const words = el.closest("[data-slot=river]")?.querySelector("h2")?.parentElement;
      // Only beside the words: on a phone they stand under the picture, and
      // centring it on them would lay it over them.
      const wr = words?.getBoundingClientRect();
      const br = el.getBoundingClientRect();
      const beside = !!wr && (wr.left >= br.right - 1 || wr.right <= br.left + 1);
      if (!beside) {
        shiftRef.current = 0;
        setShift(0);
      }
      if (words && beside) {
        const kids = [...words.children].map((k) => k.getBoundingClientRect());
        const textMid = (Math.min(...kids.map((k) => k.top)) + Math.max(...kids.map((k) => k.bottom))) / 2;
        const boxTop = el.getBoundingClientRect().top - shiftRef.current;
        const winMid = boxTop + winTop - inset + (bottom - (winTop - inset)) / 2;
        shiftRef.current = textMid - winMid;
        setShift(textMid - winMid);
      }
    };
    lay();
    // Again once the fonts are in: the words' height changes with them.
    document.fonts?.ready.then(lay);
    const seen = new ResizeObserver(lay);
    seen.observe(el);
    const words = el.closest("[data-slot=river]")?.querySelector("h2")?.parentElement;
    // Each line of the words, as the column itself may keep its size while
    // they reflow (fonts arriving, the window narrowing).
    if (words) [...words.children].forEach((k) => seen.observe(k));
    const late = window.setTimeout(lay, 1200);
    window.addEventListener("resize", lay);
    return () => {
      seen.disconnect();
      window.clearTimeout(late);
      window.removeEventListener("resize", lay);
    };
  }, []);

  useEffect(() => {
    if (still !== false) return;
    const i = STEPS.findIndex(([s]) => s === step);
    const timer = window.setTimeout(() => setStep(STEPS[(i + 1) % STEPS.length][0]), STEPS[i][1]);
    return () => window.clearTimeout(timer);
  }, [step, still]);

  const today = step === "today";
  // The first scene's windows reach from TOP to 96% of the picture; fitted
  // into the company window's height, from TOP to its foot, as are the apps
  // scattered in them.
  const REACH = 96;
  const fit = (y: number) => (footPct === null ? y : TOP + ((y - TOP) * (footPct - TOP)) / (REACH - TOP));

  return (
    <div
      ref={box}
      style={{ translate: `0 ${shift}px`, marginBottom: overhang || undefined }}
      className={cn(
        // No frame of its own: at first four windows, then the one.
        "relative aspect-[5/4]",
        className,
      )}
    >
      {/* The caption at the top: what is being shown. */}
      <p
        key={step}
        className="absolute inset-x-0 top-3 z-20 flex items-center justify-center gap-1.5 font-mono text-[9.5px] whitespace-nowrap text-muted-foreground animate-in fade-in-0 duration-500 sm:text-[11px]"
      >
        {/* As the original's first slide says it, with its open lock. */}
        {today ? (
          <>
            <LockOpen className="size-3.5 text-destructive" />
            Today: 47 apps · 9 places · 0 rules · No standard
          </>
        ) : (
          <>
            <ShieldCheck className="size-3.5 text-primary" />
            On shpyrd: One place · One sign-in · Roles · Sleep
          </>
        )}
      </p>

      {/* 1. The four places, each a window of its own, drawn as every
          window on the site is (the hero's: a frame of glass, the window
          light inside it, its bar faint): its name in the bar, in red, with
          what it is. They shrink away as the apps leave. */}
      {places.map(({ label, Icon, box: [l, t, w, h] }, i) => (
        <div
          key={label}
          aria-hidden="true"
          className={cn(
            frame,
            "absolute transition-[opacity,scale] duration-700 ease-out",
            today ? "scale-100 opacity-100" : "scale-90 opacity-0",
          )}
          style={{
            left: `${l}%`,
            top: `${fit(t + 4)}%`,
            width: `${w}%`,
            height: `${fit(t + h) - fit(t + 4)}%`,
            transitionDelay: today ? `${i * 80}ms` : "0ms",
          }}
        >
          <AppWindow
            title={
              <span className="inline-flex items-center gap-1 text-destructive">
                <Icon className="size-3" />
                {label}
              </span>
            }
            className={cn(windowLook, "h-full text-[10px] whitespace-nowrap [&_[data-slot=app-window-bar]]:py-1.5")}
          >
            {null}
          </AppWindow>
        </div>
      ))}

      {/* 2. The company's window on shpyrd, the same frame and window, its
          one sign-in in the bar. */}
      <div
        data-company
        aria-hidden="true"
        className={cn(
          frame,
          "absolute inset-x-[2%] top-[12%] transition-[opacity,scale,box-shadow] duration-700 ease-out",
          winBottom === null && "bottom-[5%]",
          today ? "scale-95 opacity-0" : "scale-100 opacity-100",
        )}
        style={winBottom === null ? undefined : { bottom: winBottom }}
      >
        <AppWindow
          title={
            <span className="inline-flex items-center gap-1.5 whitespace-nowrap">
              <LogoMark className="size-3.5" /> yourapp · apps
            </span>
          }
          // On a phone the bar keeps its name and the sign-in only.
          detail={<span className="max-sm:hidden">on shpyrd</span>}
          className={cn(windowLook, "h-full text-[11px]")}
        >
          {null}
        </AppWindow>
        {/* The one door, in the window's bar, at its right. */}
        <span className="absolute top-[15px] right-[18px] inline-flex items-center gap-1 rounded-full bg-primary px-2 py-0.5 text-[11px] font-medium text-white">
          <Lock className="size-3" /> Sign in<span className="max-sm:hidden"> · yourapp.com</span>
        </span>
      </div>

      {/* The apps: scattered and open, then each in its cell, locked. */}
      {apps.map((app, i) => {
        const [icon, ink] = looks[app.name];
        const [sx, sy, r] = scattered[i];
        const gx = cols[i % 3];
        const gy = rowTops[Math.floor(i / 3)];
        const sleeping = !today && asleep.has(app.name);
        return (
          <div
            key={app.name}
            data-card
            className={cn(
              "absolute z-10 grid w-[26%] gap-1 rounded-2xl border bg-background px-2 py-1.5 shadow-[0_1px_2px_rgb(0_0_0/0.05)] dark:bg-accent transition-[left,top,rotate,scale,opacity,box-shadow,border-color] duration-1000 ease-[cubic-bezier(0.22,1,0.36,1)]",
              today ? "border-destructive/30" : "border-foreground/8 dark:border-foreground/10",
            )}
            style={{
              left: `${today ? sx : gx}%`,
              top: `${today ? Math.max(fit(sy + cardPct) - cardPct, fit(places[homes[i]].box[1] + 4) + barPct) : gy}%`,
              rotate: today ? `${r}deg` : "0deg",
              // A little smaller while scattered, in their shorter windows.
              scale: today && footPct !== null ? "0.88" : "1",
              transformOrigin: "top left",
              transitionDelay: today ? "0ms" : `${i * 70}ms`,
            }}
          >
            <div className="flex items-center justify-between">
              <span className={cn("[&_svg]:size-3.5", ink)}>{icon}</span>
              {today ? (
                <LockOpen className="size-3 text-destructive" />
              ) : sleeping ? (
                <Moon className="size-3 text-muted-foreground" />
              ) : (
                <Lock className="size-3 text-primary" />
              )}
            </div>
            <span className="truncate text-[11px] font-medium text-foreground">{app.name}</span>
            <span className="flex items-center gap-1 text-[10px] text-muted-foreground">
              <span
                className={cn(
                  "size-1.5 rounded-full",
                  today ? "bg-destructive" : sleeping ? "bg-muted-foreground" : "bg-success",
                )}
              />
              {today ? "anyone with the link" : `${app.team}${sleeping ? " · asleep" : ""}`}
            </span>
          </div>
        );
      })}


    </div>
  );
}
