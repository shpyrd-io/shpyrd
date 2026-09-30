"use client";

import * as React from "react";
import { cn } from "cn";
import { project, scene, UNIT, VIEW } from "./shipyard/scene";
import { sprites } from "./shipyard/sprites";
import { createWorld, step } from "./shipyard/world";

// How much time a step of the yard takes, whatever the screen does.
const STEP = 1 / 60;

// A length of the drawing as a share of the width of the picture, which
// is what `cqw` counts: the picture is as wide as the room it is given.
const wide = (n: number) => `${((n / VIEW[2]) * 100).toFixed(3)}cqw`;
const across = (x: number) => wide(x - VIEW[0]);
const down = (y: number) => wide(y - VIEW[1]);

// A shipyard at work, seen from above: ships come and go, a crane takes
// containers off them and puts others on, trucks bring and take them. It
// is a picture that moves, not something to use: it stands still off the
// screen, in a tab that is not looked at, and for who asked for less motion.
function Shipyard({
  className,
  seed = 1,
  paused = false,
  ...props
}: Omit<React.ComponentProps<"div">, "children"> & {
  // The same seed gives the same yard.
  seed?: number;
  paused?: boolean;
}) {
  const [drawn, setDrawn] = React.useState(() => scene(createWorld(seed)));
  const ref = React.useRef<HTMLDivElement>(null);

  React.useEffect(() => {
    if (paused || !ref.current) return;
    const world = createWorld(seed);
    const still = window.matchMedia("(prefers-reduced-motion: reduce)");
    let frame = 0;
    let last = 0;
    let owed = 0;
    let seen = false;
    const tick = (now: number) => {
      frame = requestAnimationFrame(tick);
      owed += Math.min((now - last) / 1000, 0.1);
      last = now;
      while (owed >= STEP) {
        step(world, STEP);
        owed -= STEP;
      }
      setDrawn(scene(world));
    };
    const run = () => {
      cancelAnimationFrame(frame);
      if (!seen || document.hidden || still.matches) return;
      last = performance.now();
      frame = requestAnimationFrame(tick);
    };
    const watcher = new IntersectionObserver(([entry]) => {
      seen = entry.isIntersecting;
      run();
    });
    watcher.observe(ref.current);
    document.addEventListener("visibilitychange", run);
    still.addEventListener("change", run);
    return () => {
      cancelAnimationFrame(frame);
      watcher.disconnect();
      document.removeEventListener("visibilitychange", run);
      still.removeEventListener("change", run);
    };
  }, [seed, paused]);

  // The pieces keep their place in the page, so that the browser keeps
  // what it has drawn of each; which is over which is said by a number.
  const pieces = drawn.map((piece, rank) => ({ piece, rank: rank + 1 })).sort((a, b) => (a.piece.key < b.piece.key ? -1 : 1));

  return (
    <div
      ref={ref}
      data-slot="shipyard"
      role="img"
      aria-label="A shipyard at work: a crane moves containers between a ship and trucks."
      className={cn(
        "relative isolate aspect-square w-full overflow-hidden [container-type:inline-size] [mask-image:radial-gradient(closest-side,black_76%,transparent_100%)] dark:hue-rotate-180 dark:invert",
        className,
      )}
      {...props}
    >
      <Piece sprite="ground" at={[0, 0, 0]} rank={0} />
      {pieces.map(({ piece, rank }) =>
        piece.kind === "sprite" ? (
          <Piece key={piece.key} sprite={piece.sprite} at={piece.at} rank={rank} />
        ) : (
          <Line key={piece.key} from={piece.from} to={piece.to} tone={piece.tone} rank={rank} />
        ),
      )}
    </div>
  );
}

type Point = readonly [number, number, number];

// A piece: its file, at the scale of the yard, put where the piece stands.
function Piece({ sprite, at, rank }: { sprite: keyof typeof sprites; at: Point; rank: number }) {
  const { src, size, stands, unit } = sprites[sprite];
  const scale = UNIT / unit;
  const [x, y] = project(at);
  return (
    // eslint-disable-next-line @next/next/no-img-element
    <img
      src={src}
      alt=""
      draggable={false}
      className="pointer-events-none absolute top-0 left-0 max-w-none select-none"
      style={{
        width: wide(size[0] * scale),
        transform: `translate3d(${across(x - stands[0] * scale)}, ${down(y - stands[1] * scale)}, 0)`,
        zIndex: rank,
      }}
    />
  );
}

// What is too thin and moves too much to be a file: a cable, the barrier.
function Line({ from, to, tone, rank }: { from: Point; to: Point; tone: "cable" | "barrier"; rank: number }) {
  const [x1, y1] = project(from);
  const [x2, y2] = project(to);
  return (
    <div
      className={cn(
        "absolute top-0 left-0 origin-left rounded-full",
        tone === "barrier" ? "h-[max(2px,0.4cqw)] bg-[#ff4f00]" : "h-px bg-[#52525b]",
      )}
      style={{
        width: wide(Math.hypot(x2 - x1, y2 - y1)),
        transform: `translate3d(${across(x1)}, ${down(y1)}, 0) rotate(${Math.atan2(y2 - y1, x2 - x1).toFixed(4)}rad)`,
        zIndex: rank,
      }}
    />
  );
}

export { Shipyard, sprites as shipyardSprites };
