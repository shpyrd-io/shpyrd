"use client";

import { useEffect, useRef, useState } from "react";
import { Check, Sparkles } from "lucide-react";
import { AppWindow } from "@shpyrd/ui/components/app-window";
import { LogoMark } from "@shpyrd/ui/components/brand";
import {
  ClaudeIcon,
  CursorIcon,
  OpenAIIcon,
  VSCodeIcon,
} from "@shpyrd/ui/components/brand-icons";
import {
  Conversation,
  ConversationMessage,
  type ConversationStep,
} from "@shpyrd/ui/components/conversation";
import { glass } from "@shpyrd/ui/lib/glass";
import { cn } from "@shpyrd/ui/lib/cn";
import { Composer, Typing, Url } from "@/components/shipping-chat";

// Slide 5 (Giovani, 2026-10-28): the home's hero story (ship-story.tsx),
// copied here and told further, as the slide's five steps: describe, the
// agent builds, deploy (the hero's three), then share (the app comes back,
// now live, and the Finance team joins it) and iterate (a change asked, the
// app's new column, release 2, a rollback one command away). The steps
// beside it follow the story, and choosing one plays the story to its end.
// The home's ShipStory is left as it is.

const ask =
  "Build me a CRM to manage my contacts and deals. Don't forget a nice kanban!";
const ship = "Send it to the cloud.";
const share = "Share it with the Finance team.";
const change = "Add a Lost column to the kanban.";
const ai = "Your favorite AI";

// The agents it works with, one on each line from the chat to the app.
const agentMarks = [
  { name: "Claude Code", Mark: ClaudeIcon },
  { name: "Codex", Mark: OpenAIIcon },
  { name: "Cursor", Mark: CursorIcon },
  { name: "VS Code", Mark: VSCodeIcon },
];
// The marks sit in one row just over the chat, the first flush with the
// chat's left side. From the chat's top each line rises straight through its
// mark, turns a rounded corner and runs straight to the app's left side; the
// outer line turns highest. Sizes in pixels: the mark, the room between
// marks, the room under the row, the radius of each corner.
const MARK = 48;
const MARK_GAP = 10;
const ROW_LIFT = 10;
const CORNER = 18;

// Calm enough to follow (Giovani, 2026-10-16): each step stays long enough to
// be read before the next one starts, about 28 s for the whole story.
const MOMENTS = [
  ["draft-ask", 3200, 0],
  ["sent-ask", 1200, 0],
  ["building", 4600, 1],
  ["built", 3000, 1],
  ["draft-ship", 2200, 2],
  ["sent-ship", 1800, 2],
  ["uploading", 3000, 2],
  ["live", 3600, 2],
  ["draft-share", 2400, 3],
  ["sent-share", 1200, 3],
  ["shared", 4200, 3],
  ["draft-change", 2400, 4],
  ["sent-change", 1200, 4],
  ["changing", 2600, 4],
  ["changed", 5200, 4],
] as const;
type Moment = (typeof MOMENTS)[number][0];
const at = (name: Moment) => MOMENTS.findIndex(([n]) => n === name);
// The slide's step each moment belongs to, and how long each step lasts.
export const stepOf = (moment: number) => MOMENTS[moment][2];
const firstOf = (step: number) => MOMENTS.findIndex((m) => m[2] === step);
const lastOf = (step: number) =>
  MOMENTS.length - 1 - [...MOMENTS].reverse().findIndex((m) => m[2] === step);
export const stepTime = (step: number) =>
  MOMENTS.filter((m) => m[2] === step).reduce((t, m) => t + m[1], 0);
const LAST = MOMENTS.length - 1;

const enter =
  "animate-in fade-in-0 slide-in-from-bottom-2 duration-normal ease-enter";

// The CRM being built: its menu, then three columns of the kanban, each card
// dropping in after the one before. Once built, one deal moves on.
function Crm({
  building,
  moved,
  lost,
}: {
  building: boolean;
  moved: boolean;
  lost?: boolean;
}) {
  const show = (delay: number, extra?: string) => ({
    className: cn(
      "transition-[opacity,translate] duration-700 ease-out",
      building ? "translate-y-0 opacity-100" : "translate-y-2 opacity-0",
      extra,
    ),
    style: { transitionDelay: building ? `${delay}ms` : "0ms" },
  });
  // In the dark the window is the card's grey, the columns a shade lighter
  // and the cards lighter again, so each layer reads.
  const columns = [
    // Globex moves on from Lead to Proposal: here by the layout itself, not
    // by sliding a card over the others, so every column stays in line
    // whether there are three or four (Giovani, 2026-10-28).
    {
      name: "Lead",
      cards: moved
        ? ["Umbrella", "Hooli", "Acme"]
        : ["Umbrella", "Globex", "Hooli", "Acme"],
    },
    {
      name: "Proposal",
      cards: moved
        ? ["Initech", "Stark", "Wayne", "Globex"]
        : ["Initech", "Stark", "Wayne"],
    },
    { name: "Won", cards: ["Umbrella", "Wonka"] },
    // Iterate: the column the change adds.
    ...(lost ? [{ name: "Lost", cards: ["Vandelay"] }] : []),
  ];
  let n = 0;
  return (
    <div className="flex h-full min-h-0 text-[11px] leading-tight">
      <div
        {...show(
          300,
          // Room for a fourth column: the menu narrower once Lost is added.
          cn(
            "shrink-0 border-r border-foreground/6 transition-[width] duration-700",
            lost ? "w-[16%] p-2" : "w-[24%] p-3",
          ),
        )}
      >
        <div className="mb-3 text-xs font-semibold text-foreground">CRM</div>
        {["Contacts", "Deals", "Kanban"].map((item, i) => (
          <div
            key={item}
            className={cn(
              "mb-1 rounded-md px-1.5 py-1",
              i === 2 ? "bg-primary/10 text-primary" : "text-muted-foreground",
            )}
          >
            {item}
          </div>
        ))}
      </div>
      <div
        className={cn(
          "grid min-w-0 flex-1 p-3",
          lost ? "grid-cols-4 gap-1.5" : "grid-cols-3 gap-2",
        )}
      >
        {columns.map((column, c) => (
          <div
            key={column.name}
            {...show(
              900 + c * 400,
              "rounded-2xl bg-foreground/[0.03] p-2 dark:bg-foreground/[0.05]",
            )}
          >
            <div className="mb-2 font-medium text-muted-foreground">
              {column.name}
            </div>
            <div className="grid content-start gap-1.5">
              {column.cards.map((card) => {
                n += 1;
                const arrived = moved && card === "Globex";
                const shown = show(
                  1900 + n * 340,
                  cn(
                    cn(
                      "truncate rounded-md border border-foreground/8 bg-background py-1.5",
                      lost ? "px-1.5" : "px-2",
                    ) +
                      "  shadow-[0_1px_2px_rgb(0_0_0/0.05)] dark:border-foreground/10 dark:bg-accent",
                    arrived &&
                      "animate-in fade-in-0 slide-in-from-left-4 duration-700 ease-out",
                  ),
                );
                return (
                  <div
                    key={card}
                    className={shown.className}
                    style={arrived ? undefined : shown.style}
                  >
                    <div className="truncate font-medium text-foreground">
                      {card}
                    </div>
                    <div className="mt-1 h-1 w-2/3 rounded-full bg-foreground/10" />
                  </div>
                );
              })}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

export function FlowStory({
  jump,
  onStep,
  paused = false,
  resume = 0,
}: {
  // Held where it is by the pause button; `resume` (changing) lets a story
  // held at a chosen step's end play on.
  paused?: boolean;
  resume?: number;
  // A step chosen by hand (n changes each time): play it from its start
  // and hold at its end.
  jump?: { step: number; n: number } | null;
  onStep?: (step: number, playing: boolean) => void;
}) {
  const [moment, setMoment] = useState(0);
  const [hold, setHold] = useState<number | null>(null);
  useEffect(() => {
    if (!jump) return;
    // A step below 0: from the beginning, playing through.
    if (jump.step < 0) {
      setMoment(0);
      setHold(null);
      return;
    }
    setMoment(firstOf(jump.step));
    setHold(lastOf(jump.step));
  }, [jump]);
  const [round, setRound] = useState(0);
  // Rendered when the application is built; the reader's preference is read in
  // an effect. For who asked for less motion it stays on its last moment.
  const [still, setStill] = useState<boolean | null>(null);

  useEffect(() => {
    const less = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    setStill(less);
    if (less) setMoment(LAST);
  }, []);

  // The lines are drawn in the pixels of their box, so the marks keep their
  // size and their row stays flush with the chat at any width.
  const box = useRef<HTMLDivElement>(null);
  const [size, setSize] = useState<{ w: number; h: number } | null>(null);
  useEffect(() => {
    const el = box.current;
    if (!el) return;
    const measure = () => setSize({ w: el.clientWidth, h: el.clientHeight });
    measure();
    const seen = new ResizeObserver(measure);
    seen.observe(el);
    return () => seen.disconnect();
  }, []);

  // Where the app flies to: from its centre to the cloud's, measured once the
  // cloud has appeared and before the app leaves.
  const app = useRef<HTMLDivElement>(null);
  const cloudBody = useRef<HTMLDivElement>(null);
  const [target, setTarget] = useState<{ x: number; y: number } | null>(null);
  useEffect(() => {
    if (MOMENTS[moment][0] !== "sent-ship") return;
    // After the cloud has finished coming in, so it is measured where it rests.
    const timer = window.setTimeout(() => {
      if (!app.current || !cloudBody.current) return;
      const a = app.current.getBoundingClientRect();
      const c = cloudBody.current.getBoundingClientRect();
      // A little under the box's middle: the cloud's body is low, its bumps on top.
      setTarget({
        x: c.left + c.width / 2 - (a.left + a.width / 2),
        y: c.top + c.height * 0.6 - (a.top + a.height / 2),
      });
    }, 1000);
    return () => window.clearTimeout(timer);
  }, [moment]);

  useEffect(() => {
    if (resume) setHold(null);
  }, [resume]);

  useEffect(() => {
    onStep?.(stepOf(moment), still === false && hold !== moment && !paused);
  }, [moment, hold, still, paused, onStep]);

  useEffect(() => {
    if (still !== false || hold === moment || paused) return;
    const timer = window.setTimeout(() => {
      if (moment < LAST) return setMoment(moment + 1);
      setRound((r) => r + 1);
      setMoment(0);
    }, MOMENTS[moment][1]);
    return () => window.clearTimeout(timer);
  }, [moment, still, hold, paused]);

  const name = MOMENTS[moment][0];
  const past = (m: Moment) => moment >= at(m);
  const draft =
    name === "draft-ask"
      ? ask
      : name === "draft-ship"
        ? ship
        : name === "draft-share"
          ? share
          : name === "draft-change"
            ? change
            : null;
  const building = past("building");
  const flying = past("uploading");
  const cloud = past("sent-ship");
  const live = past("live");
  // After it went live the app comes back, at its address, for the team.
  const back = past("draft-share");
  const shared = past("shared");
  const changed = past("changed");
  // The lines are there only from the ask to build until the app leaves for
  // the cloud; an orange reflection runs along them meanwhile.
  const linked = building && !flying;
  const moving = still === false && linked;

  const release: ConversationStep[] = [
    {
      label: "shpyrd deploy · release 2",
      status: changed ? "done" : "pending",
    },
    ...(changed
      ? [
          {
            label: "Release 1 kept: one command to roll back",
            status: "done",
          } as const,
        ]
      : []),
  ];
  const deploy: ConversationStep[] = [
    { label: "shpyrd deploy · release 1", status: live ? "done" : "pending" },
    ...(live
      ? [{ label: "crm.yourapp.com is live", status: "done" } as const]
      : []),
  ];

  const agent = (
    <span className="inline-flex items-center gap-1.5">
      <Sparkles aria-hidden="true" className="size-3 text-primary" />
      {ai}
    </span>
  );

  return (
    // The room at the top is padding, not the chat's margin: a margin would
    // pass through this box and move the app, the lines and the cloud too.
    <div className="relative md:pt-[11rem]">
      {/* The app the chat asked for, large, behind the chat at the top right.
          Asked to go to the cloud, it flies up into it. */}
      <div
        ref={app}
        aria-hidden="true"
        className={cn(
          "absolute top-[2.5rem] right-0 z-10 w-[60%] origin-center transition-[opacity,translate,scale] duration-1000 ease-out max-md:hidden",
          !building && "translate-y-3 scale-95 opacity-0",
          building &&
            (!flying || back) &&
            "translate-y-0 scale-100 opacity-100",
        )}
        // Into the cloud: its centre onto the cloud's, shrinking, passing
        // behind it (the cloud is in front), and gone only once there.
        style={
          flying && !back && target
            ? {
                translate: `${target.x}px ${target.y}px`,
                scale: "0.1",
                opacity: 0,
                transition:
                  "translate 2000ms ease-in-out, scale 2000ms ease-in-out, opacity 400ms ease-in 1600ms",
              }
            : undefined
        }
      >
        <div className={cn(glass, "relative rounded-2xl p-2")}>
          <AppWindow
            title="CRM"
            detail={back ? "crm.yourapp.com" : "localhost:3000"}
            className="h-[19rem] rounded-2xl border-white/80 bg-background shadow-none dark:border-white/10 dark:bg-card [&_[data-slot=app-window-bar]]:border-foreground/6 [&_[data-slot=app-window-bar]]:bg-transparent"
          >
            <Crm
              key={round}
              building={building}
              moved={past("draft-ship")}
              lost={changed}
            />
          </AppWindow>
          {/* Share: the team that may now open it. */}
          <span
            className={cn(
              "absolute -bottom-3 left-4 inline-flex items-center gap-2 rounded-full bg-background px-3 py-1 text-xs font-medium shadow-[0_1px_3px_rgb(0_0_0/0.08)] ring-1 ring-foreground/8 transition-[opacity,translate] duration-500",
              shared ? "translate-y-0 opacity-100" : "translate-y-1 opacity-0",
            )}
          >
            <span className="flex -space-x-1.5">
              {["bg-primary", "bg-info", "bg-success", "bg-warning"].map(
                (c) => (
                  <span
                    key={c}
                    className={cn(
                      "size-4 rounded-full ring-2 ring-background",
                      c,
                    )}
                  />
                ),
              )}
            </span>
            Finance team · 6 people
          </span>
          {/* Iterate: the new release. */}
          <span
            className={cn(
              "absolute -top-3 right-4 inline-flex items-center gap-1.5 rounded-full bg-background px-2.5 py-1 text-xs font-medium text-success shadow-[0_1px_3px_rgb(0_0_0/0.08)] ring-1 ring-success/30 transition-[opacity,translate] duration-500",
              changed
                ? "translate-y-0 opacity-100"
                : "-translate-y-1 opacity-0",
            )}
          >
            <span className="size-1.5 rounded-full bg-success" />
            Release 2 live
          </span>
        </div>
      </div>

      {/* The lines from the chat to the app, an agent it works with on each. */}
      <div
        ref={box}
        aria-hidden="true"
        className="absolute top-[1rem] left-0 z-15 h-[10rem] w-[40%] max-md:hidden"
      >
        {size && (
          <svg
            viewBox={`0 0 ${size.w} ${size.h}`}
            className={cn(
              "absolute inset-0 size-full overflow-visible transition-opacity duration-500",
              linked ? "opacity-100" : "opacity-0",
            )}
          >
            {agentMarks.map((_, i) => {
              const x = MARK / 2 + i * (MARK + MARK_GAP);
              // The highest turn near the top of the box, the lowest a corner
              // over the row of marks, the others evenly between.
              const top = 40;
              const low = size.h - ROW_LIFT - MARK - 10 - CORNER;
              const y = top + ((low - top) * i) / (agentMarks.length - 1);
              const d = `M ${x} ${size.h} V ${y + CORNER} A ${CORNER} ${CORNER} 0 0 1 ${x + CORNER} ${y} H ${size.w}`;
              // Its length, for the reflection: up, round the corner, across.
              const len =
                size.h -
                (y + CORNER) +
                (Math.PI / 2) * CORNER +
                (size.w - (x + CORNER));
              return (
                <g key={i}>
                  {/* The line as the site's loops draw theirs: one continuous
                      light grey line, an orange reflection running along it,
                      faded at both ends, with a soft glow, from the chat to
                      the app, while the app is being made. */}
                  <path
                    d={d}
                    fill="none"
                    className="stroke-foreground/12 dark:stroke-foreground/20"
                    strokeWidth={1.5}
                  />
                  {moving && (
                    // One glow round all the reflection's lengths.
                    <g
                      style={{
                        filter: "drop-shadow(0 0 4px rgb(255 79 0 / 0.55))",
                      }}
                    >
                      {[
                        { seg: 160, opacity: 0.12 },
                        { seg: 110, opacity: 0.2 },
                        { seg: 70, opacity: 0.32 },
                        { seg: 40, opacity: 0.5 },
                        { seg: 18, opacity: 0.85 },
                      ].map(({ seg, opacity }) => (
                        <path
                          key={seg}
                          d={d}
                          fill="none"
                          className="stroke-primary"
                          strokeWidth={1.5}
                          strokeOpacity={opacity}
                          strokeDasharray={`${seg} ${len + 200}`}
                        >
                          {/* From before the line's start to past its end, at
                            the loops' pace (about 160px a second). */}
                          <animate
                            attributeName="stroke-dashoffset"
                            from={seg / 2 + 90}
                            to={seg / 2 - len - 90}
                            dur={`${(len + 180) / 160}s`}
                            begin={`${i * 0.35}s`}
                            repeatCount="indefinite"
                          />
                        </path>
                      ))}
                    </g>
                  )}
                </g>
              );
            })}
          </svg>
        )}
        <div
          className="absolute bottom-[10px] left-0 flex"
          style={{ gap: MARK_GAP }}
        >
          {agentMarks.map(({ name: label, Mark }) => (
            <span
              key={label}
              title={label}
              className={cn(
                glass,
                "grid shrink-0 place-items-center rounded-full",
              )}
              style={{ width: MARK, height: MARK }}
            >
              <Mark aria-label={label} className="size-6" />
            </span>
          ))}
        </div>
      </div>

      {/* The cloud, only once the chat asks to send the app there: a full
          cloud in the brand's orange with the shpyrd mark in white. */}
      <div
        className={cn(
          "absolute -top-10 right-[10%] z-40 grid justify-items-center transition-[opacity,scale,translate] duration-700 ease-out",
          cloud && !back
            ? "translate-y-0 scale-100 opacity-100"
            : "translate-y-2 scale-75 opacity-0",
        )}
      >
        <div
          ref={cloudBody}
          className={cn(
            "relative grid place-items-center transition-[scale] duration-300",
            flying && !live && "scale-105",
          )}
        >
          <svg
            viewBox="0 0 160 104"
            aria-hidden="true"
            className="w-52 drop-shadow-[0_18px_36px_rgb(255_79_0/0.38)]"
          >
            <defs>
              <linearGradient id="flow-cloud" x1="0" y1="0" x2="0" y2="1">
                <stop offset="0" stopColor="#ff7a3d" />
                <stop offset="1" stopColor="#ff4f00" />
              </linearGradient>
            </defs>
            <path
              fill="url(#flow-cloud)"
              d="M40 100 C18 100 4 86 4 68 C4 50 18 37 36 37 C40 16 58 4 80 4 C104 4 122 20 126 42 C144 44 156 57 156 72 C156 88 143 100 126 100 Z"
            />
          </svg>
          <span className="absolute inset-0 grid place-items-center pt-5">
            {live ? (
              <Check
                aria-label="Live"
                className={cn("size-14 text-white", enter)}
                strokeWidth={3}
              />
            ) : (
              <LogoMark surface="orange" className="size-14" />
            )}
          </span>
        </div>
        <span
          className={cn(
            "mt-3 inline-flex items-center gap-2 rounded-full bg-background/90 px-3 py-1 text-xs font-medium text-foreground shadow-[0_1px_3px_rgb(0_0_0/0.08)] ring-1 ring-foreground/8 transition-[opacity,translate] duration-500",
            live ? "translate-y-0 opacity-100" : "-translate-y-1 opacity-0",
          )}
        >
          {/* A ready light before the address: green, pulsing, once live. */}
          <span className="relative grid size-2 place-items-center">
            {live && (
              <span className="absolute inset-0 animate-ping rounded-full bg-success opacity-70" />
            )}
            <span className="relative size-2 rounded-full bg-success" />
          </span>
          yourapp.com
        </span>
      </div>

      {/* The one chat, in front, below and to the left of the app. */}
      <div
        data-chat=""
        className={cn(glass, "relative z-20 w-[78%] rounded-2xl p-2.5 max-md:w-full")}
      >
        <AppWindow
          title={agent}
          detail="~/projects/crm"
          className={cn(
            "h-[20rem] rounded-2xl border-white/80 bg-background bg-linear-to-b from-background to-muted shadow-none dark:border-white/10 dark:from-card dark:to-background",
            "[&_[data-slot=app-window-bar]]:border-foreground/6 [&_[data-slot=app-window-bar]]:bg-transparent [&_[data-slot=app-window-footer]]:border-foreground/6",
          )}
          footer={<Composer text={draft} placeholder="Message your AI…" />}
        >
          <div className="flex min-h-0 flex-1 flex-col justify-end overflow-hidden px-5 py-5 [mask-image:linear-gradient(to_bottom,transparent,black_3rem)]">
            <Conversation key={round}>
              {past("sent-ask") && (
                <ConversationMessage
                  from="person"
                  author="You"
                  className={enter}
                >
                  {ask}
                </ConversationMessage>
              )}
              {building && (
                <ConversationMessage
                  from="agent"
                  author={agent}
                  className={enter}
                >
                  {past("built") ? (
                    <span>
                      Your CRM is built! Contacts, deals, and a kanban you can
                      drag them across. It&apos;s running at{" "}
                      <Url>http://localhost:3000</Url>
                    </span>
                  ) : (
                    <Typing />
                  )}
                </ConversationMessage>
              )}
              {past("sent-ship") && (
                <ConversationMessage
                  from="person"
                  author="You"
                  className={enter}
                >
                  {ship}
                </ConversationMessage>
              )}
              {flying && (
                <ConversationMessage
                  from="agent"
                  author={agent}
                  steps={deploy}
                  className={enter}
                >
                  {live ? (
                    <span>
                      Done. It&apos;s live at <Url>https://crm.yourapp.com</Url>
                      , behind your work sign-in.
                    </span>
                  ) : (
                    <Typing />
                  )}
                </ConversationMessage>
              )}
              {past("sent-share") && (
                <ConversationMessage
                  from="person"
                  author="You"
                  className={enter}
                >
                  {share}
                </ConversationMessage>
              )}
              {past("sent-share") && (
                <ConversationMessage
                  from="agent"
                  author={agent}
                  className={enter}
                >
                  {shared ? (
                    <span>
                      Shared. The Finance team can open it with their work
                      sign-in, nobody else.
                    </span>
                  ) : (
                    <Typing />
                  )}
                </ConversationMessage>
              )}
              {past("sent-change") && (
                <ConversationMessage
                  from="person"
                  author="You"
                  className={enter}
                >
                  {change}
                </ConversationMessage>
              )}
              {past("changing") && (
                <ConversationMessage
                  from="agent"
                  author={agent}
                  steps={release}
                  className={enter}
                >
                  {changed ? (
                    <span>
                      Added. Release 2 is live; release 1 is kept if you need it
                      back.
                    </span>
                  ) : (
                    <Typing />
                  )}
                </ConversationMessage>
              )}
            </Conversation>
          </div>
        </AppWindow>
      </div>
    </div>
  );
}
