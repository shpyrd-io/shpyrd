"use client";

import { useId } from "react";
import { Check, Folder, GitCommitHorizontal } from "lucide-react";
import { LogoMark } from "@shpyrd/ui/components/brand";
import { cn } from "@shpyrd/ui/lib/cn";
import { glass } from "@shpyrd/ui/lib/glass";
import { FilmPulse, filmInset, useFilm, useSize } from "@/components/film";
import { GitHubMark } from "@/components/marks";

// The film of the feature "Publish from your folder or GitHub", as Giovani
// told it step by step (2026-10-20):
// 1. Only the two boxes: the folder and GitHub, each a grey icon on its
//    light box.
// 2. Under them, in one column, what is to be sent comes in one after the
//    other, in grey: the first upload, then two changes.
// 3. One after the other they turn orange.
// 4. All orange, both icons turn orange with an orange glow behind their
//    boxes, two grey lines rise from them
//    and the shpyrd cloud appears at the top, its mark in white.
// 5. The boxes send: an orange pulse runs up their lines to the cloud, the
//    folder's and GitHub's in turn, as the light runs on the site's lines.
// 6. The pulses in, the mark turns into a check: the cloud has the files.
// 7. Only then, under the cloud, the hero's address: "yourapp.com".
// 8. The address goes, the cloud goes, the lines go; and again.

const MOMENTS = [
  ["boxes", 900],
  ["up-1", 500],
  ["up-2", 500],
  ["up-3", 700],
  ["lit-1", 350],
  ["lit-2", 350],
  ["lit-3", 600],
  ["send", 1100],
  ["pulse-1", 900],
  ["pulse-2", 900],
  ["pulse-3", 900],
  ["pulse-4", 900],
  ["received", 900],
  ["online", 2400],
  ["leave", 900],
] as const;
type Moment = (typeof MOMENTS)[number][0];

const ups = ["first upload", "Fix checkout button", "Add dark mode"] as const;

const piece = cn(glass, "rounded-2xl shadow-[0_6px_12px_-8px_rgb(20_20_30/0.25),0_1px_3px_-1px_rgb(20_20_30/0.08)]");
const enter = "animate-in fade-in-0 zoom-in-95 duration-normal ease-enter";

// The boxes stand in two columns of this width, so their middles are half of
// it either side of the card's, where their lines start.
const COLUMN = 104;

export function PublishFilm() {
  const { root, name, past, round, still } = useFilm(MOMENTS);
  const gradient = useId();
  const [strip, { w, h }] = useSize<HTMLDivElement>();

  // For who asked for less motion, the story at a glance: all sent, online.
  const at = (m: Moment) => still || past(m);
  const leaving = !still && name === "leave";
  const shown = (i: number) => at(`up-${i + 1}` as Moment);
  const lit = (i: number) => at(`lit-${i + 1}` as Moment);
  const sending = at("send") && !leaving;
  const cloud = at("send") && !leaving;
  // Which line carries a pulse now: the folder's, then GitHub's, in turn.
  const pulse = still ? null : name === "pulse-1" || name === "pulse-3" ? -1 : name === "pulse-2" || name === "pulse-4" ? 1 : null;
  const received = at("received");
  const online = at("online") && !leaving;

  // From each box's middle up into the cloud, 14px either side of its
  // middle: past the cloud's foot (the drawing has a little room under it)
  // and on under the cloud, which lies over the lines, so they meet it.
  const INTO = 18;
  const line = (side: -1 | 1) => {
    const from = w / 2 + (side * COLUMN) / 2;
    const to = w / 2 + side * 14;
    return `M ${from} ${h} V ${h * 0.6} C ${from} ${h * 0.25}, ${to} ${h * 0.35}, ${to} 0 V ${-INTO}`;
  };

  const boxes = [
    { key: "folder", icon: <Folder /> },
    {
      key: "git",
      // The mark in a box of the icon's size, so it sits in the middle as
      // the folder does.
      icon: (
        <span className="grid size-[55%] place-items-center [&_svg]:size-full!">
          <GitHubMark />
        </span>
      ),
    },
  ];

  return (
    // 20px inside its stage: the cloud at the top, the boxes and what they
    // send at the foot, the lines between.
    <div ref={root} className={cn(filmInset, "[container-type:size]")}>
      <div key={round} className="flex size-full flex-col items-center">
        {/* The cloud, as the hero's, once the files are on their way. */}
        <div
          className={cn(
            "relative z-20 w-[min(50cqw,38cqh,13rem)] shrink-0 [container-type:inline-size] transition-[opacity,scale,translate] duration-700 ease-out",
            cloud ? "translate-y-0 scale-100 opacity-100" : "translate-y-2 scale-90 opacity-0",
          )}
        >
          <svg viewBox="0 0 160 104" aria-hidden="true" className="w-full drop-shadow-[0_18px_36px_rgb(255_79_0/0.38)]">
            <defs>
              <linearGradient id={gradient} x1="0" y1="0" x2="0" y2="1">
                <stop offset="0" stopColor="#ff7a3d" />
                <stop offset="1" stopColor="#ff4f00" />
              </linearGradient>
            </defs>
            <path
              fill={`url(#${gradient})`}
              d="M40 100 C18 100 4 86 4 68 C4 50 18 37 36 37 C40 16 58 4 80 4 C104 4 122 20 126 42 C144 44 156 57 156 72 C156 88 143 100 126 100 Z"
            />
          </svg>
          <span className="absolute inset-0 grid place-items-center pt-[9.6cqw]">
            {received ? (
              <Check aria-label="Received" className={cn("size-[27cqw] text-white", enter)} strokeWidth={3} />
            ) : (
              <LogoMark surface="orange" className="size-[27cqw]" />
            )}
          </span>
        </div>

        {/* The two grey lines rising from the boxes to the cloud, and over
            them, under the cloud, the address once it is online. */}
        <div ref={strip} className="relative min-h-16 w-full flex-1">
          {w > 0 && (
            <svg aria-hidden="true" viewBox={`0 0 ${w} ${h}`} className="absolute inset-0 size-full overflow-visible">
              {/* The pulse, from the box into the cloud. */}
              {pulse !== null && <FilmPulse key={name} d={line(pulse)} />}
              {([-1, 1] as const).map((side) => (
                <path
                  key={side}
                  d={line(side)}
                  fill="none"
                  pathLength={1}
                  strokeDasharray="1 1"
                  strokeWidth={1.5}
                  className="stroke-foreground/20 transition-[stroke-dashoffset,opacity] duration-1000 ease-out"
                  // Drawn from the box up, as the path starts at the box.
                  style={{ strokeDashoffset: sending ? 0 : 1, opacity: leaving ? 0 : 1 }}
                />
              ))}
            </svg>
          )}
          <span
            className={cn(
              "absolute top-3 left-1/2 z-10 inline-flex -translate-x-1/2 items-center gap-2 rounded-full bg-background px-3 py-1 text-xs font-medium whitespace-nowrap text-foreground shadow-[0_1px_3px_rgb(0_0_0/0.08)] ring-1 ring-foreground/8 transition-[opacity,translate] duration-500",
              online ? "translate-y-0 opacity-100" : "-translate-y-1 opacity-0",
            )}
          >
            <span className="relative grid size-2 place-items-center">
              {online && !still && <span className="absolute inset-0 animate-ping rounded-full bg-success opacity-70" />}
              <span className="relative size-2 rounded-full bg-success" />
            </span>
            yourapp.com
          </span>
        </div>

        {/* The folder and GitHub: a grey icon on a light box each; while they
            send the icon turns orange and an orange glow rises behind the
            box. */}
        <div className="flex">
          {boxes.map((b) => (
            <div key={b.key} className="grid place-items-center" style={{ width: COLUMN }}>
              <span
                className={cn(
                  piece,
                  "grid size-[clamp(3.25rem,14cqh,4.25rem)] place-items-center transition-[color,box-shadow] duration-500 [&>svg]:size-[55%]",
                  sending
                    ? "text-primary shadow-[0_0_22px_-2px_rgb(255_79_0/0.55),0_1px_3px_-1px_rgb(20_20_30/0.08)]"
                    : "text-muted-foreground",
                )}
              >
                {b.icon}
              </span>
            </div>
          ))}
        </div>

        {/* What is to be sent, in one column under both: in grey as it comes,
            then orange, one after the other. */}
        <ul className="mt-6 grid h-14 shrink-0 content-start justify-items-center gap-0.5">
          {ups.map((text, i) =>
            shown(i) ? (
              <li
                key={text}
                className={cn(
                  "inline-flex items-center gap-1 text-[10.5px] whitespace-nowrap transition-colors duration-300 animate-in fade-in-0 slide-in-from-top-1 duration-normal",
                  lit(i) ? "text-primary" : "text-muted-foreground",
                )}
              >
                <GitCommitHorizontal className="size-3 shrink-0" />
                {text}
              </li>
            ) : null,
          )}
        </ul>
      </div>
    </div>
  );
}
