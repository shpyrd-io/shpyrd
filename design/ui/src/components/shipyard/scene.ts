// What is drawn of the yard, and in which order: each piece where it
// stands, the ones further from the eye first.

import { type Sprite } from "./sprites";
import { BAYS, BED_Z, COLUMNS, DECK_Z, GATE_Y, LANE_X, LAYERS, SHIP_X, type World } from "./world";

// A unit of the yard, the width of a container, in units of the drawing.
export const UNIT = 50;
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

// The room a piece takes in the yard: from one corner to the opposite one.
export type Box = readonly [x0: number, y0: number, z0: number, x1: number, y1: number, z1: number];

export type Drawn =
  | { kind: "sprite"; key: string; sprite: Sprite; at: Point }
  | { kind: "line"; key: string; from: Point; to: Point; tone: "cable" | "barrier" };

// The room each piece takes, around the point where it stands. It is what
// says which piece is drawn over which, so a piece that another can be
// both before and behind is made of parts, each with its own room.
const ROOM: Record<Sprite, Box> = {
  container: [-0.5, -1, 0, 0.5, 1, 1],
  "truck-bed": [-0.5, -1.15, 0, 0.5, 1.1, BED_Z],
  "truck-cab": [-0.5, 1.1, 0, 0.5, 2.05, 1.45],
  "ship-hull": [-1.5, -7.4, 0, 1.5, 6, DECK_Z],
  "ship-bridge": [-1.35, -7.15, DECK_Z, 1.35, -5.05, 3.7],
  "crane-leg": [-0.22, -0.4, 0, 0.22, 0.4, 5.2],
  "crane-beam": [-3, -0.12, 5.2, 5.8, 0.12, 5.6],
  "crane-tie": [-0.1, -1.28, 5.25, 0.1, 1.28, 5.5],
  "crane-trolley": [-0.6, -1.45, 5.6, 0.6, 1.9, 5.85],
  "crane-cabin": [-0.4, 1.82, 4.3, 0.4, 2.5, 5.6],
  "crane-spreader": [-0.5, -1, 0, 0.5, 1, 0.14],
  warehouse: [-2.3, -2.8, 0, 2.3, 2.8, 2.42],
  sign: [-1.2, -0.3, 0, 1.2, 0.3, 7],
  gate: [-0.55, -0.55, 0, 0.55, 0.55, 1.4],
  ground: [0, 0, 0, 0, 0, 0],
};

const LEGS = [7, 9.9];
const LEG_APART = 1.4;
const BEAM_Z = 5.6;
// Where the ties are along the beams, from the lane.
const TIES = [-2.9, 5.7];
const BARRIER = 2.5;

// Whether two pieces cover each other on the drawing. What is drawn of a
// box has six sides, in three directions; two of them are apart when
// they are apart seen along any of the three.
export function meet(a: Box, b: Box) {
  return (
    a[0] - a[4] < b[3] - b[1] &&
    b[0] - b[4] < a[3] - a[1] &&
    a[0] - a[5] < b[3] - b[2] &&
    b[0] - b[5] < a[3] - a[2] &&
    a[1] - a[5] < b[4] - b[2] &&
    b[1] - b[5] < a[4] - a[2]
  );
}

const near = 0.01;
const middle = (box: Box) => box[0] + box[1] + box[2] + box[3] + box[4] + box[5];

// Whether a is behind b, seen from where the yard is looked at: further
// back across the quay, further back along it, or under it. It is not
// known of two pieces that are in the same room.
export function behind(a: Box, b: Box): boolean | undefined {
  for (const axis of [0, 1, 2]) {
    if (a[axis + 3] <= b[axis] + near) return true;
    if (b[axis + 3] <= a[axis] + near) return false;
  }
  return undefined;
}

export type Placed = { drawn: Drawn; box: Box };

// The pieces in the order they are drawn: each after all that it covers.
// Two that are in the same room are told apart by their middles.
function order(pieces: Placed[]): Drawn[] {
  const sorted = [...pieces].sort((a, b) => middle(a.box) - middle(b.box));
  const before: number[][] = sorted.map(() => []);
  for (let i = 0; i < sorted.length; i++) {
    for (let j = i + 1; j < sorted.length; j++) {
      if (!meet(sorted[i].box, sorted[j].box)) continue;
      if (behind(sorted[i].box, sorted[j].box) ?? true) before[j].push(i);
      else before[i].push(j);
    }
  }
  const out: Drawn[] = [];
  const seen = new Uint8Array(sorted.length);
  const visit = (i: number) => {
    if (seen[i]) return;
    seen[i] = 1;
    for (const k of before[i]) visit(k);
    out.push(sorted[i].drawn);
  };
  for (let i = 0; i < sorted.length; i++) visit(i);
  return out;
}

// Every piece of the yard, where it stands and the room it takes.
export function place(world: World): Placed[] {
  const pieces: Placed[] = [];
  const sprite = (key: string, name: Sprite, at: Point) => {
    const room = ROOM[name];
    pieces.push({
      drawn: { kind: "sprite", key, sprite: name, at },
      box: [at[0] + room[0], at[1] + room[1], at[2] + room[2], at[0] + room[3], at[1] + room[4], at[2] + room[5]],
    });
  };
  const line = (key: string, from: Point, to: Point, tone: "cable" | "barrier") =>
    pieces.push({
      drawn: { kind: "line", key, from, to, tone },
      box: [
        Math.min(from[0], to[0]) - 0.03,
        Math.min(from[1], to[1]) - 0.03,
        Math.min(from[2], to[2]),
        Math.max(from[0], to[0]) + 0.03,
        Math.max(from[1], to[1]) + 0.03,
        Math.max(from[2], to[2]),
      ],
    });

  sprite("warehouse", "warehouse", [2.4, -5.6, 0]);
  sprite("sign", "sign", [0.4, -8.7, 0]);
  sprite("gate", "gate", [6.5, GATE_Y, 0]);
  for (const c of world.yard) sprite(`c${c.id}`, "container", [c.x, c.y, c.z]);

  const turn = (world.gate * 80 * Math.PI) / 180;
  const pivot: Point = [7.2, GATE_Y, 0.7];
  line("barrier", pivot, [pivot[0] + BARRIER * Math.cos(turn), GATE_Y, pivot[2] + BARRIER * Math.sin(turn)], "barrier");

  for (const truck of world.trucks) {
    sprite(`t${truck.id}`, "truck-bed", [LANE_X, truck.y, 0]);
    sprite(`t${truck.id}cab`, "truck-cab", [LANE_X, truck.y, 0]);
    if (truck.load) sprite(`c${truck.load.id}`, "container", [LANE_X, truck.y, BED_Z]);
  }

  for (const s of world.ships) {
    sprite(`s${s.id}`, "ship-hull", [SHIP_X, s.y, 0]);
    sprite(`s${s.id}bridge`, "ship-bridge", [SHIP_X, s.y, 0]);
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
    for (const side of [-1, 1]) sprite(`leg${x}${side}`, "crane-leg", [x, crane.y + side * LEG_APART, 0]);
  }
  if (crane.load) sprite(`c${crane.load.id}`, "container", [crane.x, crane.y, crane.h - 1]);
  sprite("spreader", "crane-spreader", [crane.x, crane.y, crane.h]);
  for (const dx of [-0.4, 0.4]) {
    for (const dy of [-0.85, 0.85]) {
      const x = crane.x + dx;
      const y = crane.y + dy;
      line(`cable${dx}${dy}`, [x, y, crane.h + 0.14], [x, y, BEAM_Z], "cable");
    }
  }
  for (const side of [-1, 1]) sprite(`beam${side}`, "crane-beam", [LANE_X, crane.y + side * LEG_APART, 0]);
  for (const x of TIES) sprite(`tie${x}`, "crane-tie", [LANE_X + x, crane.y, 0]);
  sprite("trolley", "crane-trolley", [crane.x, crane.y, 0]);
  sprite("cabin", "crane-cabin", [crane.x, crane.y, 0]);

  return pieces;
}

// What is drawn of the yard, in the order it is drawn.
export function scene(world: World): Drawn[] {
  return order(place(world));
}
