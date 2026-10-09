"use client";

// The site's main action: "Deploy your vibecoded app", "Publish your
// site"…, whole phrases taking turns, and a click to the sign-up, with the
// plan of where it was clicked (free, unless a plan's card says). The
// phrases roll as the agents of AddToAgent do, on a clock every such button
// on the page shares, so they change together.
import { useEffect, useRef, useState, useSyncExternalStore } from "react";
import { cn } from "cn";
import { Button } from "@shpyrd/ui/components/button";
import { placeIn, roll } from "@shpyrd/ui/lib/roll";
import { deploy } from "@shpyrd/content/site/offer";
import { signUpFor } from "@/lib/signup";

// How long each phrase stays.
const HOLD = 2600;

const first = { index: 0, leaving: -1 };
let current = first;
let holds = 0;
let running = 0;
let timer: number | undefined;
const listeners = new Set<() => void>();

const clock = {
  subscribe(listener: () => void) {
    listeners.add(listener);
    return () => void listeners.delete(listener);
  },
  now: () => current,
  atStart: () => first,
  run() {
    if (++running === 1) {
      timer = window.setInterval(() => {
        if (holds > 0) return;
        current = { index: (current.index + 1) % deploy.length, leaving: current.index };
        listeners.forEach((l) => l());
      }, HOLD);
    }
    return () => {
      if (--running === 0) window.clearInterval(timer);
    };
  },
  // A pointer over a button, or keyboard focus in it, holds them all still.
  hold() {
    holds++;
    let held = true;
    return () => {
      if (held) holds--;
      held = false;
    };
  },
};

export function DeployButton({
  size,
  plan = "free",
}: {
  size?: React.ComponentProps<typeof Button>["size"];
  // The plan the sign-up starts on: a pricing card's own.
  plan?: string;
}) {
  const { index, leaving } = useSyncExternalStore(clock.subscribe, clock.now, clock.atStart);
  // Built without a browser: the reader's preference is read in an effect.
  const [still, setStill] = useState(true);
  const hovered = useRef<(() => void) | null>(null);
  const focused = useRef<(() => void) | null>(null);
  useEffect(() => {
    const less = window.matchMedia("(prefers-reduced-motion: reduce)");
    const apply = () => setStill(less.matches);
    apply();
    less.addEventListener("change", apply);
    return () => less.removeEventListener("change", apply);
  }, []);
  useEffect(() => (still ? undefined : clock.run()), [still]);
  useEffect(
    () => () => {
      hovered.current?.();
      focused.current?.();
    },
    [],
  );

  return (
    <Button asChild size={size}>
      <a
        href={signUpFor(plan)}
        aria-label={`${deploy[index].verb} your ${deploy[index].thing} on shpyrd`}
        onMouseEnter={() => (hovered.current ??= clock.hold())}
        onMouseLeave={() => {
          hovered.current?.();
          hovered.current = null;
        }}
        onFocus={(e) => {
          if (e.currentTarget.matches(":focus-visible")) focused.current ??= clock.hold();
        }}
        onBlur={() => {
          focused.current?.();
          focused.current = null;
        }}
      >
        {/* Every phrase in one cell, centred in the room the longest needs,
            so the button keeps one width and nothing beside it moves. The
            cell clips the phrase leaving above and the next waiting below.
            The thing is a size up from the words before it: what changes most. */}
        <span className="-my-1 grid justify-items-center overflow-hidden py-1">
          {deploy.map(({ verb, thing }, i) => {
            const place = placeIn(i, index, leaving);
            return (
              <span
                key={`${verb} ${thing}`}
                aria-hidden={place === "here" ? undefined : "true"}
                data-place={place}
                className={cn("col-start-1 row-start-1 inline-flex w-max items-baseline gap-1.5", roll)}
              >
                {`${verb} your `}
                <span className="text-[1.125em] font-semibold">{thing}</span>
              </span>
            );
          })}
        </span>
      </a>
    </Button>
  );
}
