// The shipyard of design/ui, without React, for a page that travels as one
// file: the same world and the same scene, drawn into the shipyard the
// page has (`[data-slot="shipyard"]`), the files of its pieces in this
// script. It is drawn as the component draws it (shipyard.tsx): a change
// there is made here too. Bundled by scripts/pack.mjs.
import { project, scene, UNIT, VIEW, type Drawn, type Point } from "../../ui/src/components/shipyard/scene";
import { sprites } from "../../ui/src/components/shipyard/sprites";
import { createWorld, step } from "../../ui/src/components/shipyard/world";

const STEP = 1 / 60;
const wide = (n: number) => `${((n / VIEW[2]) * 100).toFixed(3)}cqw`;
const across = (x: number) => wide(x - VIEW[0]);
const down = (y: number) => wide(y - VIEW[1]);

const yard = document.querySelector<HTMLElement>('[data-slot="shipyard"]');
if (yard) {
  const world = createWorld(1);
  const drawn = new Map<string, HTMLElement>();
  yard.replaceChildren();

  const image = (sprite: keyof typeof sprites) => {
    const img = document.createElement("img");
    img.src = sprites[sprite].src;
    img.alt = "";
    img.draggable = false;
    img.style.cssText = "position:absolute;top:0;left:0;max-width:none;pointer-events:none;user-select:none";
    return img;
  };

  const place = (img: HTMLElement, sprite: keyof typeof sprites, at: Point, rank: number) => {
    const { size, stands, unit } = sprites[sprite];
    const scale = UNIT / unit;
    const [x, y] = project(at);
    img.style.width = wide(size[0] * scale);
    img.style.transform = `translate3d(${across(x - stands[0] * scale)}, ${down(y - stands[1] * scale)}, 0)`;
    img.style.zIndex = String(rank);
  };

  const line = (piece: Extract<Drawn, { kind: "line" }>, el: HTMLElement, rank: number) => {
    const [x1, y1] = project(piece.from);
    const [x2, y2] = project(piece.to);
    el.style.cssText =
      "position:absolute;top:0;left:0;transform-origin:left;border-radius:9999px;" +
      (piece.tone === "barrier" ? "height:max(2px,0.4cqw);background:#ff4f00;" : "height:1px;background:#52525b;");
    el.style.width = wide(Math.hypot(x2 - x1, y2 - y1));
    el.style.transform = `translate3d(${across(x1)}, ${down(y1)}, 0) rotate(${Math.atan2(y2 - y1, x2 - x1).toFixed(4)}rad)`;
    el.style.zIndex = String(rank);
  };

  const ground = image("ground");
  place(ground, "ground", [0, 0, 0], 0);
  yard.append(ground);

  const draw = () => {
    const seen = new Set<string>();
    scene(world).forEach((piece, i) => {
      seen.add(piece.key);
      let el = drawn.get(piece.key);
      if (piece.kind === "sprite") {
        if (!el || el.dataset.sprite !== piece.sprite) {
          el?.remove();
          el = image(piece.sprite);
          el.dataset.sprite = piece.sprite;
          yard.append(el);
          drawn.set(piece.key, el);
        }
        place(el, piece.sprite, piece.at, i + 1);
      } else {
        if (!el) {
          el = document.createElement("div");
          yard.append(el);
          drawn.set(piece.key, el);
        }
        line(piece, el, i + 1);
      }
    });
    for (const [key, el] of drawn) {
      if (!seen.has(key)) {
        el.remove();
        drawn.delete(key);
      }
    }
  };

  // It moves only while it is looked at, and stands still for who asked
  // for less motion.
  const still = window.matchMedia("(prefers-reduced-motion: reduce)");
  let frame = 0;
  let last = 0;
  let owed = 0;
  const tick = (now: number) => {
    frame = requestAnimationFrame(tick);
    owed += Math.min((now - last) / 1000, 0.1);
    last = now;
    while (owed >= STEP) {
      step(world, STEP);
      owed -= STEP;
    }
    draw();
  };
  const run = () => {
    cancelAnimationFrame(frame);
    if (document.hidden || still.matches) return;
    last = performance.now();
    frame = requestAnimationFrame(tick);
  };
  draw();
  run();
  document.addEventListener("visibilitychange", run);
  still.addEventListener("change", run);
}
