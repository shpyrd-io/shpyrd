"use client";

import { useEffect, useRef, useState, useSyncExternalStore } from "react";
import { cn } from "cn";
import { Button } from "@shpyrd/ui/components/button";
import { placeIn, roll } from "@shpyrd/ui/lib/roll";
import {
  type BrandIconProps,
  ClaudeIcon,
  CursorIcon,
  OpenAIIcon,
  VSCodeIcon,
} from "@shpyrd/ui/components/brand-icons";
import { addToAgent, agents } from "@shpyrd/content/site/agents";
import { type Platform, platformOf } from "@/lib/platform";

// An agent's mark, from design/ui's brand icons. "color" is the brand's own
// fill, for where it stands on the page; "ink" is the colour of the text
// around it, for the orange button, where Claude's own orange would vanish.
const marks: Record<string, React.ComponentType<BrandIconProps>> = {
  "claude-code": ClaudeIcon,
  claude: ClaudeIcon,
  codex: OpenAIIcon,
  cursor: CursorIcon,
  vscode: VSCodeIcon,
};

export function AgentMark({
  id,
  className,
  tone = "color",
}: {
  id: string;
  className?: string;
  tone?: "color" | "ink";
}) {
  const Mark = marks[id];
  if (!Mark) return null;
  // The name is always written beside the mark, so the mark says nothing more.
  return (
    <Mark
      aria-hidden="true"
      className={className}
      {...(tone === "ink" ? { fill: "currentColor" } : {})}
    />
  );
}

// The developer action: connect shpyrd to the agent you already use. A click
// downloads the shpyrd installer for the reader's computer, which installs the
// CLI and connects it to their agents; under it, the way to do it by hand.
//
// The client name changes on its own, which is the page's one piece of motion
// nobody asked for — it carries the fact that this works with more than one
// agent rather than decorating. It stops while the pointer or the keyboard is
// on the button, and entirely when the reader has asked for less motion.
const HOLD = 4500;

// One clock for every button on the page, so the one in the header and the one
// in the hero always show the same agent and roll at the same moment. Anything
// that holds one of them - a pointer over it, focus in it, its panel open -
// holds them all, or they would fall out of step. It ticks only while at least
// one button is moving.
const first = { index: 0, leaving: -1 };
let current = first;
let holds = 0;
let running = 0;
// Something on the page that keeps its own time (the hero's chat, which moves
// on when a conversation ends) drives the clock instead of the interval.
let driven = 0;
let timer: number | undefined;
const listeners = new Set<() => void>();

export const clock = {
  subscribe(listener: () => void) {
    listeners.add(listener);
    return () => void listeners.delete(listener);
  },
  now: () => current,
  // Where the page is built, and when it first draws in the browser.
  atStart: () => first,
  // A button that moves keeps the clock going while it is on the page.
  run() {
    if (++running === 1) {
      timer = window.setInterval(() => {
        if (holds > 0 || driven > 0) return;
        current = { index: (current.index + 1) % agents.length, leaving: current.index };
        listeners.forEach((l) => l());
      }, HOLD);
    }
    return () => {
      if (--running === 0) window.clearInterval(timer);
    };
  },
  // Roll to one agent now, as a tick would: the reader picked it.
  show(index: number) {
    if (index === current.index) return;
    current = { index, leaving: current.index };
    listeners.forEach((l) => l());
  },
  // Take the clock over: the interval stops moving it until let go. The one
  // driving moves it with `next`, which waits while a button is held.
  drive() {
    driven++;
    let on = true;
    return () => {
      if (on) driven--;
      on = false;
    };
  },
  next() {
    if (holds > 0) return;
    current = { index: (current.index + 1) % agents.length, leaving: current.index };
    listeners.forEach((l) => l());
  },
  // Hold every button still; the function it returns lets go, once.
  hold() {
    holds++;
    let held = true;
    return () => {
      if (held) holds--;
      held = false;
    };
  },
};

// Each agent rolls into place as a word does (design/ui's roll), and its
// mark settles a beat after the words land (`ease-enter`, delayed): it
// turns into place from where it leaves or waits.
const turn = cn(
  "inline-flex transition-[rotate,scale] duration-slow ease-enter",
  "group-data-[place=here]/agent:rotate-0 group-data-[place=here]/agent:scale-100 group-data-[place=here]/agent:delay-150",
  "group-data-[place=leaving]/agent:rotate-90 group-data-[place=leaving]/agent:scale-60",
  "group-data-[place=waiting]/agent:-rotate-[120deg] group-data-[place=waiting]/agent:scale-40 group-data-[place=waiting]/agent:transition-none",
);

// The agent the clock is showing now, for anything on the page that should
// name the same one as the buttons.
export function useCurrentAgent() {
  const { index } = useSyncExternalStore(clock.subscribe, clock.now, clock.atStart);
  return { index, agent: agents[index] };
}

export function AddToAgent({
  manual = true,
  size,
}: {
  manual?: boolean;
  // The button's size, from the library: "lg" where it leads a page.
  size?: React.ComponentProps<typeof Button>["size"];
}) {
  // The agent showing, and the one on its way out (none, at first).
  const { index, leaving } = useSyncExternalStore(clock.subscribe, clock.now, clock.atStart);
  // Rendered when the application is built, where there is no matchMedia and
  // no navigator; the reader's preference and computer are read in effects.
  const [still, setStill] = useState(true);
  const [platform, setPlatform] = useState<Platform | null>(null);
  // What this button is holding the clock for: the pointer, and focus.
  const hovered = useRef<(() => void) | null>(null);
  const focused = useRef<(() => void) | null>(null);
  useEffect(() => {
    const less = window.matchMedia("(prefers-reduced-motion: reduce)");
    const apply = () => setStill(less.matches);
    apply();
    less.addEventListener("change", apply);
    return () => less.removeEventListener("change", apply);
  }, []);
  useEffect(() => setPlatform(platformOf(navigator)), []);

  useEffect(() => (still ? undefined : clock.run()), [still]);
  // Let go of whatever is still held when the button leaves the page.
  useEffect(
    () => () => {
      hovered.current?.();
      focused.current?.();
    },
    [],
  );

  // Until the computer is known, and where there is no installer for it, the
  // button goes to the manual install.
  const installer = platform ? addToAgent.installers[platform] : null;
  const label = `${addToAgent.label} ${agents[index].name}: ${
    installer ? `download the shpyrd installer for ${installer.name}` : "install shpyrd"
  }`;

  return (
    <div
      className="inline-grid justify-items-center gap-3"
      onMouseEnter={() => (hovered.current ??= clock.hold())}
      onMouseLeave={() => {
        hovered.current?.();
        hovered.current = null;
      }}
      // Only focus from the keyboard holds the roll: someone tabbing to the
      // button is reading it. A click focuses it too, and that should not
      // keep it still.
      onFocusCapture={(event) => {
        if (event.target.matches(":focus-visible")) focused.current ??= clock.hold();
      }}
      onBlurCapture={() => {
        focused.current?.();
        focused.current = null;
      }}
    >
      <Button asChild size={size}>
        <a href={installer?.href ?? addToAgent.manual.href} aria-label={label}>
          <span className="inline-flex items-center gap-1.5">
            {addToAgent.label}
            {/* Two columns: "Add to", which never moves, and the agent,
                centred in the room the longest of them needs. The agent is
                its mark and its name, together, and they change as one: the
                one showing rolls up and out while the next rolls in from
                under it, and its mark turns into place a beat after the
                words land, like a board of departures settling. They all sit
                in one cell, so the button keeps one width and nothing beside
                it moves; the cell clips what is above and below. */}
            <span className="-my-1 grid justify-items-center overflow-hidden py-1">
              {agents.map((agent, i) => {
                const place = placeIn(i, index, leaving);
                return (
                  <span
                    key={agent.id}
                    aria-hidden={place === "here" ? undefined : "true"}
                    data-place={place}
                    className={cn(
                      "group/agent col-start-1 row-start-1 flex w-max items-center gap-1.5 font-semibold",
                      roll,
                    )}
                  >
                    <span className={turn}>
                      <AgentMark id={agent.id} tone="ink" className="size-4" />
                    </span>
                    {agent.name}
                  </span>
                );
              })}
            </span>
          </span>
        </a>
      </Button>
      {manual && (
        <a
          href={addToAgent.manual.href}
          className="text-xs text-muted-foreground underline underline-offset-4 hover:text-foreground"
        >
          {addToAgent.manual.label}
        </a>
      )}
    </div>
  );
}
