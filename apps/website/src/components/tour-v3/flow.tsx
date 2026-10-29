"use client";

// Slide 5: from the prompt to the team. Since 2026-10-28 (Giovani) the home
// hero's story, told on to the slide's five steps (flow-story.tsx); the step
// it is on lights up beside it, and choosing a step plays the story to it.
import { useCallback, useLayoutEffect, useRef, useState } from "react";
import {
  Bot,
  ChevronLeft,
  ChevronRight,
  MessageSquareText,
  Pause,
  Play,
  Rocket,
  Share2,
  Undo2,
} from "lucide-react";
import { cn } from "@shpyrd/ui/lib/cn";
import { Button } from "@shpyrd/ui/components/button";
import { flow } from "@/content/tour-v3";
import { FlowStory, stepTime } from "./flow-story";
import { Intro, Label, panel, mouseOnly } from "./parts";

// The controls under the story, as slide 4's.
const stepButton = "hover:border-primary/40 hover:text-primary";

const icons = [
  <MessageSquareText key="0" />,
  <Bot key="1" />,
  <Rocket key="2" />,
  <Share2 key="3" />,
  <Undo2 key="4" />,
];

export function Flow() {
  // The step the story is at, told by the story itself; a step chosen by
  // hand is played from its start and held at its end.
  const [step, setStep] = useState(0);
  const [playing, setPlaying] = useState(true);
  const [jump, setJump] = useState<{ step: number; n: number } | null>(null);
  const [paused, setPaused] = useState(false);
  const [resume, setResume] = useState(0);
  const choose = (i: number) => {
    setPaused(false);
    setJump((j) => ({ step: (i + 5) % 5, n: (j?.n ?? 0) + 1 }));
  };
  const toggle = () => {
    if (playing) return setPaused(true);
    setPaused(false);
    setResume((r) => r + 1);
  };
  const follow = useCallback((s: number, p: boolean) => {
    setStep(s);
    setPlaying(p);
  }, []);

  // The thread from the step's right edge, across the gap, to the chat's
  // left edge: straight across when the step faces the chat, else down (or
  // up) the middle of the gap with rounded corners, as on slide 4.
  const row = useRef<HTMLDivElement>(null);
  const [thread, setThread] = useState<string | null>(null);
  useLayoutEffect(() => {
    const box = row.current;
    if (!box) return;
    const measure = () => {
      const o = box.getBoundingClientRect();
      const from = box
        .querySelector(`[data-step="${step}"]`)
        ?.getBoundingClientRect();
      const chat = box.querySelector("[data-chat]")?.getBoundingClientRect();
      if (!from || !chat) return;
      const x0 = from.right - o.left;
      const y0 = from.top + from.height / 2 - o.top;
      const x1 = chat.left - o.left;
      // Every thread ends at the same point (Giovani, 2026-10-28): level with
      // the chat window's three dots, its title bar's middle.
      const bar =
        box
          .querySelector("[data-chat] [data-slot=app-window-bar]")
          ?.getBoundingClientRect() ?? chat;
      const y1 = bar.top + bar.height / 2 - o.top;
      const mx = (x0 + x1) / 2;
      const dir = Math.sign(y1 - y0);
      const r = Math.min(12, Math.abs(y1 - y0) / 2, (x1 - x0) / 2);
      setThread(
        `M ${x0} ${y0} L ${mx - r} ${y0} Q ${mx} ${y0} ${mx} ${y0 + dir * r} ` +
          `L ${mx} ${y1 - dir * r} Q ${mx} ${y1} ${mx + r} ${y1} L ${x1} ${y1}`,
      );
    };
    measure();
    // The row and the chat both, and once more as the slide settles in.
    const seen = new ResizeObserver(measure);
    seen.observe(box);
    const chat = box.querySelector("[data-chat]");
    if (chat) seen.observe(chat);
    const late = [300, 1200].map((t) => window.setTimeout(measure, t));
    return () => {
      seen.disconnect();
      late.forEach((t) => window.clearTimeout(t));
    };
  }, [step]);

  return (
    <div className="grid gap-8">
      <Intro
        label={<Label icon={<Rocket />}>{flow.label}</Label>}
        heading={flow.heading}
        lead={flow.lead}
      />

      {/* The steps wide enough for each on one line (Giovani, 2026-10-28):
          the longest is 301 px of words; the chat window takes the rest. */}
      {/* The steps as wide as step 3's line needs, the longest (Giovani,
          2026-10-28): its words, icon, room and timing bar; 48 px to the
          story. A dotted grey thread, as slide 4's, from the step the story
          is at to the chat. */}
      <div
        ref={row}
        className="relative grid items-start gap-4 lg:grid-cols-[29rem_1fr] lg:gap-12"
      >
        {thread && (
          <svg
            aria-hidden="true"
            className="pointer-events-none absolute inset-0 z-30 size-full overflow-visible max-lg:hidden"
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
        {/* Each step in its horizontal card, its icon and words as slide 4's
            stops (Giovani, 2026-10-28): the route's 64 px rounded box, white
            with the icon orange once reached, grey ahead, filled orange on
            the step the story is at; its number in a badge at the corner;
            the name 16 px semibold, the line 14 px, orange on the current
            step. Under the pointer the box lifts and its edge turns orange. */}
        <ol className="grid gap-3">
          {flow.steps.map((s, i) => {
            const here = i === step;
            const done = i < step;
            return (
              <li key={s.title}>
                {/* Each step can be chosen (Giovani, 2026-10-28). */}
                <button
                  type="button"
                  {...mouseOnly}
                  onClick={() => choose(i)}
                  aria-current={here ? "step" : undefined}
                  data-step={i}
                  className={cn(
                    panel,
                    "group relative flex w-full cursor-pointer items-center gap-4 overflow-hidden px-3 py-3 text-left transition-[border-color,background-color] duration-normal ease-move hover:border-primary/40 focus-visible:ring-2 focus-visible:ring-ring",
                    here && "border-primary/50 bg-primary/5",
                  )}
                >
                  <span
                    className={cn(
                      "relative flex size-16 shrink-0 items-center justify-center rounded-2xl ring-1 transition-[background-color,color,box-shadow,translate] duration-normal ease-move group-hover:-translate-y-0.5 [&_svg]:size-7",
                      here
                        ? "bg-primary text-primary-foreground ring-primary shadow-[0_8px_20px_-6px_rgb(255_79_0/0.55)]"
                        : "bg-background ring-foreground/10 shadow-[inset_0_1px_0_rgb(255_255_255/0.65),0_8px_20px_rgb(20_20_30/0.06)] group-hover:ring-primary/40 dark:bg-card",
                      !here &&
                        (done
                          ? "text-primary"
                          : "text-muted-foreground group-hover:text-primary"),
                    )}
                  >
                    {icons[i]}
                    <span
                      className={cn(
                        "absolute -top-2 -right-2 grid size-5 place-items-center rounded-full text-[10px] font-semibold tabular-nums ring-2 ring-background transition-colors",
                        here
                          ? "bg-foreground text-background"
                          : "bg-muted text-muted-foreground",
                      )}
                    >
                      {i + 1}
                    </span>
                  </span>
                  <span className="grid gap-0.5">
                    <span className="text-base font-semibold text-foreground">
                      {s.title}
                    </span>
                    <span
                      className={cn(
                        "text-sm transition-colors lg:whitespace-nowrap",
                        here ? "text-primary" : "text-muted-foreground",
                      )}
                    >
                      {s.body}
                    </span>
                  </span>
                  {/* How long until the next step. */}
                  {here && playing && (
                    <span
                      aria-hidden="true"
                      className="absolute inset-y-3 right-2 w-1 overflow-hidden rounded-full bg-primary/15"
                    >
                      <span
                        key={`${jump?.n ?? 0}-${step}`}
                        className="block w-full rounded-full bg-primary motion-safe:animate-[tour-fill_linear_forwards]"
                        style={{ animationDuration: `${stepTime(step)}ms` }}
                      />
                    </span>
                  )}
                </button>
              </li>
            );
          })}
        </ol>

        {/* The home's hero story, told to its end (flow-story.tsx). */}
        <FlowStory
          jump={jump}
          onStep={follow}
          paused={paused}
          resume={resume}
        />
      </div>

      {/* Going along it by hand, as on slide 4 (Giovani, 2026-10-28): back,
          play or pause, next, centred under the slide. */}
      <div className="-mt-2 flex items-center justify-center gap-2">
        <Button
          variant="outline"
          size="icon-lg"
          className={stepButton}
          {...mouseOnly}
          onClick={() => choose(step - 1)}
          aria-label="Previous step"
        >
          <ChevronLeft />
        </Button>
        <Button
          variant="outline"
          size="icon-lg"
          className={stepButton}
          {...mouseOnly}
          onClick={toggle}
          aria-label={playing ? "Pause" : "Play"}
        >
          {playing ? (
            <Pause className="fill-current" />
          ) : (
            <Play className="translate-x-px fill-current" />
          )}
        </Button>
        <Button
          variant="outline"
          size="icon-lg"
          className={stepButton}
          {...mouseOnly}
          onClick={() => choose(step + 1)}
          aria-label="Next step"
        >
          <ChevronRight />
        </Button>
      </div>
    </div>
  );
}
