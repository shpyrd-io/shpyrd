"use client";

import { useEffect, useRef, useState } from "react";
import { Check, Sparkles } from "lucide-react";
import { AppWindow } from "@shpyrd/ui/components/app-window";
import { LogoMark } from "@shpyrd/ui/components/brand";
import { ClaudeIcon, CursorIcon, OpenAIIcon, VSCodeIcon } from "@shpyrd/ui/components/brand-icons";
import { Conversation, ConversationMessage, type ConversationStep } from "@shpyrd/ui/components/conversation";
import { glass } from "@shpyrd/ui/lib/glass";
import { cn } from "@shpyrd/ui/lib/cn";
import { Composer, Typing, Url } from "@/components/shipping-chat";

// A proposal for the home's picture (Giovani, 2026-10-16): one chat with
// "your favourite AI", not a window for each agent. Behind it, at the top
// right, the app it is asked for builds itself, large; lines run from the
// chat to that app, an agent it works with on each. Asked to send the app to
// the cloud, a cloud bearing the shpyrd mark appears, the app flies up into
// it, and the cloud answers with a tick and the app's address.

const ask = "Build me a CRM to manage my contacts and deals. Don't forget a nice kanban!";
const ship = "Send it to the cloud.";
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
  ["draft-ask", 3800],
  ["sent-ask", 1400],
  ["building", 5200],
  ["built", 3800],
  ["draft-ship", 2600],
  ["sent-ship", 1800],
  ["uploading", 3000],
  ["live", 6000],
] as const;
type Moment = (typeof MOMENTS)[number][0];
const at = (name: Moment) => MOMENTS.findIndex(([n]) => n === name);
const LAST = MOMENTS.length - 1;

const enter = "animate-in fade-in-0 slide-in-from-bottom-2 duration-normal ease-enter";

// The CRM being built: its menu, then three columns of the kanban, each card
// dropping in after the one before. Once built, one deal moves on.
function Crm({ building, moved }: { building: boolean; moved: boolean }) {
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
    { name: "Lead", cards: ["Umbrella", "Globex", "Hooli", "Pied Piper"] },
    { name: "Proposal", cards: ["Initech", "Stark", "Wayne"] },
    { name: "Won", cards: ["Umbrella", "Wonka"] },
  ];
  let n = 0;
  return (
    <div className="flex h-full min-h-0 text-[11px] leading-tight">
      <div {...show(300, "w-[24%] shrink-0 border-r border-foreground/6 p-3")}>
        <div className="mb-3 text-xs font-semibold text-foreground">CRM</div>
        {["Contacts", "Deals", "Kanban"].map((item, i) => (
          <div
            key={item}
            className={cn("mb-1 rounded-md px-1.5 py-1", i === 2 ? "bg-primary/10 text-primary" : "text-muted-foreground")}
          >
            {item}
          </div>
        ))}
      </div>
      <div className="grid min-w-0 flex-1 grid-cols-3 gap-2 p-3">
        {columns.map((column, c) => (
          <div key={column.name} {...show(900 + c * 400, "rounded-2xl bg-foreground/[0.03] p-2 dark:bg-foreground/[0.05]")}>
            <div className="mb-2 font-medium text-muted-foreground">{column.name}</div>
            <div className="grid gap-1.5">
              {column.cards.map((card) => {
                n += 1;
                // Globex is the deal that moves on, from Lead to Proposal.
                const moving = card === "Globex";
                const shown = show(
                  1900 + n * 340,
                  cn(
                    "rounded-md border border-foreground/8 bg-background px-2 py-1.5 shadow-[0_1px_2px_rgb(0_0_0/0.05)] dark:border-foreground/10 dark:bg-accent",
                    moving && "relative z-10 transition-[opacity,translate] duration-1000",
                    moving && moved && "shadow-[0_6px_14px_rgb(0_0_0/0.12)]",
                  ),
                );
                // One column right (its width, the gap and the columns'
                // padding), two cards down: under the three already in Proposal.
                const style =
                  moving && moved
                    ? { transitionDelay: "0ms", translate: "calc(100% + 1.5rem) calc(200% + 0.75rem)" }
                    : shown.style;
                return (
                  <div key={card} className={shown.className} style={style}>
                    <div className="font-medium text-foreground">{card}</div>
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

export function ShipStory() {
  const [moment, setMoment] = useState(0);
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
      setTarget({ x: c.left + c.width / 2 - (a.left + a.width / 2), y: c.top + c.height * 0.6 - (a.top + a.height / 2) });
    }, 1000);
    return () => window.clearTimeout(timer);
  }, [moment]);

  useEffect(() => {
    if (still !== false) return;
    const timer = window.setTimeout(() => {
      if (moment < LAST) return setMoment(moment + 1);
      setRound((r) => r + 1);
      setMoment(0);
    }, MOMENTS[moment][1]);
    return () => window.clearTimeout(timer);
  }, [moment, still]);

  const name = MOMENTS[moment][0];
  const past = (m: Moment) => moment >= at(m);
  const draft = name === "draft-ask" ? ask : name === "draft-ship" ? ship : null;
  const building = past("building");
  const flying = past("uploading");
  const cloud = past("sent-ship");
  const live = past("live");
  // The lines are there only from the ask to build until the app leaves for
  // the cloud; an orange reflection runs along them meanwhile.
  const linked = building && !flying;
  const moving = still === false && linked;

  const deploy: ConversationStep[] = [
    { label: "shpyrd deploy · release 1", status: live ? "done" : "pending" },
    ...(live ? [{ label: "crm.yourapp.com is live", status: "done" } as const] : []),
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
    <div className="relative md:pt-[14rem]">
      {/* The app the chat asked for, large, behind the chat at the top right.
          Asked to go to the cloud, it flies up into it. */}
      <div
        ref={app}
        aria-hidden="true"
        className={cn(
          "absolute top-[2.5rem] right-0 z-10 w-[60%] origin-center transition-[opacity,translate,scale] duration-1000 ease-out max-md:hidden",
          !building && "translate-y-3 scale-95 opacity-0",
          building && !flying && "translate-y-0 scale-100 opacity-100",
        )}
        // Into the cloud: its centre onto the cloud's, shrinking, passing
        // behind it (the cloud is in front), and gone only once there.
        style={
          flying && target
            ? {
                translate: `${target.x}px ${target.y}px`,
                scale: "0.1",
                opacity: 0,
                transition: "translate 2000ms ease-in-out, scale 2000ms ease-in-out, opacity 400ms ease-in 1600ms",
              }
            : undefined
        }
      >
        <div className={cn(glass, "rounded-2xl p-2")}>
          <AppWindow
            title="CRM"
            detail="localhost:3000"
            className="h-[19rem] rounded-2xl border-white/80 bg-background shadow-none dark:border-white/10 dark:bg-card [&_[data-slot=app-window-bar]]:border-foreground/6 [&_[data-slot=app-window-bar]]:bg-transparent"
          >
            <Crm key={round} building={building} moved={past("draft-ship")} />
          </AppWindow>
        </div>
      </div>

      {/* The lines from the chat to the app, an agent it works with on each. */}
      <div ref={box} aria-hidden="true" className="absolute top-[1rem] left-0 z-15 h-[13rem] w-[40%] max-md:hidden">
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
              const len = size.h - (y + CORNER) + (Math.PI / 2) * CORNER + (size.w - (x + CORNER));
              return (
                <g key={i}>
                  {/* The line as the site's loops draw theirs: one continuous
                      light grey line, an orange reflection running along it,
                      faded at both ends, with a soft glow, from the chat to
                      the app, while the app is being made. */}
                  <path d={d} fill="none" className="stroke-foreground/12 dark:stroke-foreground/20" strokeWidth={1.5} />
                  {moving && (
                    // One glow round all the reflection's lengths.
                    <g style={{ filter: "drop-shadow(0 0 4px rgb(255 79 0 / 0.55))" }}>
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
        <div className="absolute bottom-[10px] left-0 flex" style={{ gap: MARK_GAP }}>
          {agentMarks.map(({ name: label, Mark }) => (
            <span
              key={label}
              title={label}
              className={cn(glass, "grid shrink-0 place-items-center rounded-full")}
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
          cloud ? "translate-y-0 scale-100 opacity-100" : "translate-y-2 scale-75 opacity-0",
        )}
      >
        <div ref={cloudBody} className={cn("relative grid place-items-center transition-[scale] duration-300", flying && !live && "scale-105")}>
          <svg viewBox="0 0 160 104" aria-hidden="true" className="w-52 drop-shadow-[0_18px_36px_rgb(255_79_0/0.38)]">
            <defs>
              <linearGradient id="ship-cloud" x1="0" y1="0" x2="0" y2="1">
                <stop offset="0" stopColor="#ff7a3d" />
                <stop offset="1" stopColor="#ff4f00" />
              </linearGradient>
            </defs>
            <path
              fill="url(#ship-cloud)"
              d="M40 100 C18 100 4 86 4 68 C4 50 18 37 36 37 C40 16 58 4 80 4 C104 4 122 20 126 42 C144 44 156 57 156 72 C156 88 143 100 126 100 Z"
            />
          </svg>
          <span className="absolute inset-0 grid place-items-center pt-5">
            {live ? (
              <Check aria-label="Live" className={cn("size-14 text-white", enter)} strokeWidth={3} />
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
            {live && <span className="absolute inset-0 animate-ping rounded-full bg-success opacity-70" />}
            <span className="relative size-2 rounded-full bg-success" />
          </span>
          yourapp.com
        </span>
      </div>

      {/* The one chat, in front, below and to the left of the app. */}
      <div className={cn(glass, "relative z-20 w-[78%] rounded-2xl p-2.5")}>
        <AppWindow
          title={agent}
          detail="~/projects/crm"
          className={cn(
            "h-[22rem] rounded-2xl border-white/80 bg-background bg-linear-to-b from-background to-muted shadow-none dark:border-white/10 dark:from-card dark:to-background",
            "[&_[data-slot=app-window-bar]]:border-foreground/6 [&_[data-slot=app-window-bar]]:bg-transparent [&_[data-slot=app-window-footer]]:border-foreground/6",
          )}
          footer={<Composer text={draft} placeholder="Message your AI…" />}
        >
          <div className="flex min-h-0 flex-1 flex-col justify-end overflow-hidden px-5 py-5 [mask-image:linear-gradient(to_bottom,transparent,black_3rem)]">
            <Conversation key={round}>
              {past("sent-ask") && (
                <ConversationMessage from="person" author="You" className={enter}>
                  {ask}
                </ConversationMessage>
              )}
              {building && (
                <ConversationMessage from="agent" author={agent} className={enter}>
                  {past("built") ? (
                    <span>
                      Your CRM is built! Contacts, deals, and a kanban you can drag them across. It&apos;s running
                      at <Url>http://localhost:3000</Url>
                    </span>
                  ) : (
                    <Typing />
                  )}
                </ConversationMessage>
              )}
              {past("sent-ship") && (
                <ConversationMessage from="person" author="You" className={enter}>
                  {ship}
                </ConversationMessage>
              )}
              {flying && (
                <ConversationMessage from="agent" author={agent} steps={deploy} className={enter}>
                  {live ? (
                    <span>
                      Done. It&apos;s live at <Url>https://crm.yourapp.com</Url>, behind your work sign-in.
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
