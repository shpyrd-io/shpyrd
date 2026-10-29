"use client";

// The home's hero story in small (ship-story.tsx; Giovani, 2026-10-28), for
// slide 8: what does not change, whoever builds and wherever it runs. The
// chat with your favourite AI is asked to put the app online; the app
// appears beside it, flies into the orange cloud with the shpyrd mark, and
// the cloud answers with a tick and the app's address. The story is told by
// the slide's beats, from `step`: 0 the ask typed, 1 sent, 2 the AI at
// work, 3 the app rises out of the chat into the cloud, 4 live.
import { useLayoutEffect, useRef, useState } from "react";
import { Check, Sparkles } from "lucide-react";
import { AppWindow } from "@shpyrd/ui/components/app-window";
import { LogoMark } from "@shpyrd/ui/components/brand";
import {
  Conversation,
  ConversationMessage,
} from "@shpyrd/ui/components/conversation";
import { glass } from "@shpyrd/ui/lib/glass";
import { cn } from "@shpyrd/ui/lib/cn";
import { Composer, Typing, Url } from "@/components/shipping-chat";

const ask = "Put the expenses app online for the finance team.";
const address = "expenses.yourcompany.com";
const enter =
  "animate-in fade-in-0 slide-in-from-bottom-2 duration-normal ease-enter";

// The app being sent: a few rows of claims, as slide 6's app.
function Expenses() {
  return (
    <div className="grid content-start gap-1.5 p-3 text-[10px] leading-tight">
      <div className="text-xs font-semibold text-foreground">
        Expense claims
      </div>
      {["Team offsite", "Laptop stand", "Client dinner"].map((c) => (
        <div
          key={c}
          className="flex items-center justify-between rounded-md border border-foreground/8 bg-background px-2 py-1.5"
        >
          <span className="font-medium text-foreground">{c}</span>
          <span className="h-1 w-6 rounded-full bg-foreground/10" />
        </div>
      ))}
    </div>
  );
}

export function MiniShip({ step, lit }: { step: number; lit?: boolean }) {
  const agent = (
    <span className="inline-flex items-center gap-1.5">
      <Sparkles aria-hidden="true" className="size-3 text-primary" />
      Your favorite AI
    </span>
  );
  const sent = step >= 1;
  const app = step >= 2;
  const flying = step >= 3;
  const live = step >= 4;

  // The line from the chat up to the cloud, as the hero's lines run from
  // its chat (Giovani, 2026-10-28): light grey, an orange reflection running
  // up it, from the ask sent until the app is there. Measured in the box's
  // own pixels, by offsets, so the cloud's coming in does not move it.
  const root = useRef<HTMLDivElement>(null);
  const cloudBody = useRef<HTMLDivElement>(null);
  const chat = useRef<HTMLDivElement>(null);
  const [line, setLine] = useState<{
    x: number;
    y0: number;
    y1: number;
    cy: number;
  } | null>(null);
  useLayoutEffect(() => {
    const el = root.current;
    if (!el) return;
    const at = (e: HTMLElement | null) => {
      let x = 0;
      let y = 0;
      let n: HTMLElement | null = e;
      while (n && n !== el) {
        x += n.offsetLeft;
        y += n.offsetTop;
        n = n.offsetParent as HTMLElement | null;
      }
      return e ? { x, y, w: e.offsetWidth, h: e.offsetHeight } : null;
    };
    const measure = () => {
      const c = at(cloudBody.current);
      const t = at(chat.current);
      if (!c || !t) return;
      setLine({
        x: c.x + c.w / 2,
        y0: t.y,
        y1: c.y + c.h - 10,
        cy: c.y + c.h * 0.6,
      });
    };
    measure();
    const seen = new ResizeObserver(measure);
    seen.observe(el);
    return () => seen.disconnect();
  }, []);
  const linked = sent && !live;
  const len = line ? line.y0 - line.y1 : 0;

  return (
    // Stacked for a column of the slide (Giovani, 2026-10-28): the cloud
    // centred over the chat, the app rising out of the chat into it.
    <div ref={root} className="relative grid min-w-0 gap-9">
      {line && (
        <svg
          aria-hidden="true"
          className={cn(
            "pointer-events-none absolute inset-0 z-0 size-full overflow-visible transition-opacity duration-500",
            linked ? "opacity-100" : "opacity-0",
          )}
        >
          <path
            d={`M ${line.x} ${line.y0} V ${line.y1}`}
            fill="none"
            className="stroke-foreground/12 dark:stroke-foreground/20"
            strokeWidth={1.5}
          />
          {linked && (
            <g style={{ filter: "drop-shadow(0 0 4px rgb(255 79 0 / 0.55))" }}>
              {[
                { seg: 60, opacity: 0.15 },
                { seg: 36, opacity: 0.3 },
                { seg: 18, opacity: 0.55 },
                { seg: 8, opacity: 0.9 },
              ].map(({ seg, opacity }) => (
                <path
                  key={seg}
                  d={`M ${line.x} ${line.y0} V ${line.y1}`}
                  fill="none"
                  className="stroke-primary"
                  strokeWidth={1.5}
                  strokeOpacity={opacity}
                  strokeDasharray={`${seg} ${len + 120}`}
                >
                  {/* From the chat to the cloud, at the hero's pace (about
                      160 px a second), again and again. */}
                  <animate
                    attributeName="stroke-dashoffset"
                    from={seg / 2 + 40}
                    to={seg / 2 - len - 40}
                    dur={`${(len + 80) / 160}s`}
                    repeatCount="indefinite"
                  />
                </path>
              ))}
            </g>
          )}
        </svg>
      )}
      {/* The app, asked to go online, leaves the chat: out of its top,
          quickly straight up into the cloud, shrinking, gone once there
          (Giovani, 2026-10-28). */}
      {line && flying && (
        <div
          aria-hidden="true"
          className="pointer-events-none absolute z-[5] w-28 motion-safe:animate-[mini-ship-rise_900ms_ease-in_forwards] motion-reduce:hidden"
          style={
            {
              left: line.x,
              top: line.y0 + 8,
              "--rise": `${line.y0 + 8 + 48 - line.cy}px`,
            } as React.CSSProperties
          }
        >
          <div className={cn(glass, "rounded-2xl p-1.5")}>
            <AppWindow
              title="Expenses"
              detail="localhost:3000"
              className="h-24 rounded-xl border-white/80 bg-background shadow-none dark:border-white/10 dark:bg-card [&_[data-slot=app-window-bar]]:border-foreground/6 [&_[data-slot=app-window-bar]]:bg-transparent"
            >
              <Expenses />
            </AppWindow>
          </div>
        </div>
      )}

      {/* The cloud, larger, centred over the chat: the brand's orange with
          the shpyrd mark, a tick once the app is there, "Live" under it. */}
      <div
        className={cn(
          "relative z-10 grid justify-items-center transition-[opacity,scale,translate] duration-700 ease-out",
          sent
            ? "translate-y-0 scale-100 opacity-100"
            : "translate-y-2 scale-75 opacity-0",
        )}
      >
        <div
          ref={cloudBody}
          data-mini-cloud=""
          className="relative grid place-items-center"
        >
          <svg
            viewBox="0 0 160 104"
            aria-hidden="true"
            className="w-44 drop-shadow-[0_14px_28px_rgb(255_79_0/0.35)]"
          >
            <defs>
              <linearGradient id="mini-ship-cloud" x1="0" y1="0" x2="0" y2="1">
                <stop offset="0" stopColor="#ff7a3d" />
                <stop offset="1" stopColor="#ff4f00" />
              </linearGradient>
            </defs>
            <path
              fill="url(#mini-ship-cloud)"
              d="M40 100 C18 100 4 86 4 68 C4 50 18 37 36 37 C40 16 58 4 80 4 C104 4 122 20 126 42 C144 44 156 57 156 72 C156 88 143 100 126 100 Z"
            />
          </svg>
          <span className="absolute inset-0 grid place-items-center pt-5">
            {live ? (
              <Check
                aria-label="Live"
                className={cn("size-12 text-white", enter)}
                strokeWidth={3}
              />
            ) : (
              <LogoMark surface="orange" className="size-12" />
            )}
          </span>
        </div>
        <span
          className={cn(
            "mt-2 inline-flex items-center gap-1.5 rounded-full whitespace-nowrap bg-background/90 px-3 py-1 text-xs font-medium text-foreground shadow-[0_1px_3px_rgb(0_0_0/0.08)] ring-1 ring-foreground/8 transition-[opacity,translate] duration-500",
            live ? "translate-y-0 opacity-100" : "-translate-y-1 opacity-0",
          )}
        >
          <span className="relative grid size-2 place-items-center">
            {live && (
              <span className="absolute inset-0 animate-ping rounded-full bg-success opacity-70" />
            )}
            <span className="relative size-2 rounded-full bg-success" />
          </span>
          Live
        </span>
      </div>
      {/* The chat, as the hero's: a frame of glass, the window in it. */}
      <div
        ref={chat}
        className={cn(
          glass,
          "relative z-10 min-w-0 rounded-2xl p-2 transition-[box-shadow] duration-normal",
          lit && "ring-2 ring-primary",
        )}
      >
        <AppWindow
          title={agent}
          className={cn(
            "h-[14.5rem] rounded-2xl border-white/80 bg-background bg-linear-to-b from-background to-muted shadow-none dark:border-white/10 dark:from-card dark:to-background",
            "[&_[data-slot=app-window-bar]]:border-foreground/6 [&_[data-slot=app-window-bar]]:bg-transparent [&_[data-slot=app-window-footer]]:border-foreground/6",
          )}
          footer={
            <Composer
              text={step === 0 ? ask : null}
              placeholder="Message your AI…"
            />
          }
        >
          <div className="flex min-h-0 flex-1 flex-col justify-end overflow-hidden px-4 py-3 text-sm [mask-image:linear-gradient(to_bottom,transparent,black_2rem)]">
            <Conversation>
              {sent && (
                <ConversationMessage
                  from="person"
                  author="You"
                  className={enter}
                >
                  {ask}
                </ConversationMessage>
              )}
              {app && (
                <ConversationMessage
                  from="agent"
                  author={agent}
                  className={enter}
                >
                  {live ? (
                    <span>
                      Done. It&apos;s live at <Url>{`https://${address}`}</Url>,
                      behind your work sign-in.
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
