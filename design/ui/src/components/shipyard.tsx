"use client";

import * as React from "react";
import { cn } from "cn";
import { project, scene, VIEW } from "./shipyard/scene";
import { sprites } from "./shipyard/sprites";
import { createWorld, step } from "./shipyard/world";

// How much time a step of the yard takes, whatever the screen does.
const STEP = 1 / 60;

const round = (n: number) => Math.round(n * 100) / 100;

// A shipyard at work, seen from above: ships come and go, a crane takes
// containers off them and puts others on, trucks bring and take them. It
// is a picture that moves, not something to use: it stands still off the
// screen, in a tab that is not looked at, and for who asked for less motion.
function Shipyard({
  className,
  seed = 1,
  paused = false,
  ...props
}: Omit<React.ComponentProps<"svg">, "children"> & {
  // The same seed gives the same yard.
  seed?: number;
  paused?: boolean;
}) {
  const [drawn, setDrawn] = React.useState(() => scene(createWorld(seed)));
  const ref = React.useRef<SVGSVGElement>(null);

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

  return (
    <svg
      ref={ref}
      data-slot="shipyard"
      role="img"
      aria-label="A shipyard at work: a crane moves containers between a ship and trucks."
      viewBox={VIEW.join(" ")}
      className={cn(
        "aspect-square w-full [mask-image:radial-gradient(closest-side,black_76%,transparent_100%)] dark:hue-rotate-180 dark:invert",
        className,
      )}
      {...props}
    >
      <Piece sprite="ground" at={[0, 0, 0]} />
      {drawn.map((piece) => {
        if (piece.kind === "sprite") return <Piece key={piece.key} sprite={piece.sprite} at={piece.at} />;
        const [x1, y1] = project(piece.from);
        const [x2, y2] = project(piece.to);
        return (
          <line
            key={piece.key}
            x1={round(x1)}
            y1={round(y1)}
            x2={round(x2)}
            y2={round(y2)}
            strokeLinecap="round"
            className={piece.tone === "barrier" ? "stroke-[#ff4f00] stroke-[5]" : "stroke-[#52525b] stroke-1"}
          />
        );
      })}
    </svg>
  );
}

// A piece, its file put where the piece stands.
function Piece({ sprite, at }: { sprite: keyof typeof sprites; at: readonly [number, number, number] }) {
  const { src, box } = sprites[sprite];
  const [x, y] = project(at);
  return <image href={src} x={round(x + box[0])} y={round(y + box[1])} width={box[2]} height={box[3]} />;
}

export { Shipyard, sprites as shipyardSprites };
