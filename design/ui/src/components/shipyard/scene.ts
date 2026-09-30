// What is drawn of the yard, and in which order: each piece where it
// stands, the ones further from the eye first.

import { type Sprite } from "./sprites";
import { BED_Z, COLUMNS, BAYS, DECK_Z, GATE_Y, LANE_X, LAYERS, QUAY_X, SHIP_X, type World } from "./world";

// A unit of the yard, in units of the drawing: the same as in the files.
const UNIT = 50;
const ACROSS = Math.cos(Math.PI / 6) * UNIT;
const DOWN = 0.5 * UNIT;

export type Point = readonly [x: number, y: number, z: number];

// Where a point of the yard is on the drawing.
export function project([x, y, z]: Point): [number, number] {
  return [(x - y) * ACROSS, (x + y) * DOWN - z * UNIT];
}

// The part of the drawing that is shown: a square, with the crane a
// little under and to the right of its middle.
const SIZE = 1300;
const [middleX, middleY] = project([2.25, -2.75, 0]);
export const VIEW = [middleX - SIZE / 2, middleY - SIZE / 2, SIZE, SIZE] as const;

export type Drawn =
  | { kind: "sprite"; key: string; sprite: Sprite; at: Point; order: number }
  | { kind: "line"; key: string; from: Point; to: Point; tone: "cable" | "barrier"; order: number };

// What is on land, the hull of a ship, what is on it, what hangs from the
// crane, and the crane over everything.
const LAND = 1000;
const HULL = 2000;
const SEA = 3000;
const CABLE = 4000;
const OVER = 5000;

const LEGS = [7, 9.9];
const LEG_APART = 1.4;
const BEAM_Z = 5.6;
const BARRIER = 2.5;

// Further along x, y or z is nearer the eye.
const depth = ([x, y, z]: Point) => x + y + z;

export function scene(world: World): Drawn[] {
  const drawn: Drawn[] = [];
  const sprite = (key: string, name: Sprite, at: Point, layer?: number) =>
    drawn.push({ kind: "sprite", key, sprite: name, at, order: (layer ?? (at[0] > QUAY_X ? SEA : LAND)) + depth(at) });

  sprite("warehouse", "warehouse", [2.4, -5.6, 0]);
  sprite("sign", "sign", [0.4, -8.7, 0]);
  sprite("gate", "gate", [6.5, GATE_Y, 0]);
  for (const c of world.yard) sprite(`c${c.id}`, "container", [c.x, c.y, c.z]);

  const turn = (world.gate * 80 * Math.PI) / 180;
  const pivot: Point = [7.2, GATE_Y, 0.7];
  drawn.push({
    kind: "line",
    key: "barrier",
    from: pivot,
    to: [pivot[0] + BARRIER * Math.cos(turn), GATE_Y, pivot[2] + BARRIER * Math.sin(turn)],
    tone: "barrier",
    order: LAND + depth(pivot),
  });

  for (const truck of world.trucks) {
    sprite(`t${truck.id}`, "truck", [LANE_X, truck.y, 0]);
    if (truck.load) sprite(`c${truck.load.id}`, "container", [LANE_X, truck.y, BED_Z]);
  }

  for (const s of world.ships) {
    sprite(`s${s.id}`, "ship", [SHIP_X, s.y, 0], HULL);
    BAYS.forEach((bay, b) =>
      COLUMNS.forEach((column, c) => {
        for (let layer = 0; layer < LAYERS; layer++) {
          const container = s.slots[(b * COLUMNS.length + c) * LAYERS + layer];
          if (container) sprite(`c${container.id}`, "container", [SHIP_X + column, s.y + bay, DECK_Z + layer]);
        }
      }),
    );
  }

  const { crane } = world;
  for (const x of LEGS) {
    for (const side of [-1, 1]) sprite(`leg${x}${side}`, "crane-leg", [x, crane.y + side * LEG_APART, 0], LAND);
  }
  if (crane.load) sprite(`c${crane.load.id}`, "container", [crane.x, crane.y, crane.h - 1]);
  sprite("spreader", "crane-spreader", [crane.x, crane.y, crane.h]);
  for (const dx of [-0.4, 0.4]) {
    for (const dy of [-0.85, 0.85]) {
      drawn.push({
        kind: "line",
        key: `cable${dx}${dy}`,
        from: [crane.x + dx, crane.y + dy, crane.h + 0.14],
        to: [crane.x + dx, crane.y + dy, BEAM_Z],
        tone: "cable",
        order: CABLE,
      });
    }
  }
  sprite("crane-top", "crane-top", [LANE_X, crane.y, 0], OVER);
  sprite("crane-trolley", "crane-trolley", [crane.x, crane.y, 0], OVER + 1000);

  return drawn.sort((a, b) => a.order - b.order);
}
