// What is drawn of the yard, and in which order: each piece where it
// stands, the ones further from the eye first.

import { type Sprite } from "./sprites";
import { BAYS, BED_Z, COLUMNS, DECK_Z, GATE_Y, LANE_X, LAYERS, SHIP_X, TOP_Z, type World } from "./world";

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
type Box = readonly [x0: number, y0: number, z0: number, x1: number, y1: number, z1: number];

export type Drawn =
  | { kind: "sprite"; key: string; sprite: Sprite; at: Point }
  | { kind: "line"; key: string; from: Point; to: Point; tone: "cable" | "barrier" };

// The room each piece takes, around the point where it stands. It is what
// says which piece is drawn over which, so a piece that another can be
// both before and behind is made of parts, each with its own room.
const ROOM: Record<Sprite, Box> = {
  container: [-0.5, -1, 0, 0.5, 1, 1],
  "truck-bed": [-0.5, -1.15, 0, 0.5, 1.1, BED_Z],
  "truck-cab": [-0.5, 1.15, 0, 0.5, 2.8, 1.45],
  "ship-hull": [-1.5, -5.55, 0, 1.5, 6.65, DECK_Z],
  "ship-bridge": [-1.25, -5.3, DECK_Z, 1.25, -4.2, 4.15],
  // The crane's parts, around the ground under the middle of its legs. The
  // girders are at 4.46-4.6, on the tops of the legs, the trolley rides on
  // them; the legs on the land side stand at x -0.91, those on the water
  // side at 0.91, at y ±1.04. What is over the girders (the frame on the
  // water-side legs, its stays, the house and the stays behind) is drawn
  // over the trolley.
  "crane-land-back": [-1.05, -1.3, 0, -0.76, 0.97, 4.46],
  "crane-girder-back": [-2.75, -1.16, 4.46, 6.26, -1.03, 4.6],
  "crane-sea-low": [0.76, -1.3, 0, 1.05, 1.3, 1],
  "crane-sea-b": [0.84, -1.2, 1, 0.98, -1, 4.46],
  "crane-girder-front": [-2.75, 0.9, 4.46, 6.26, 1.04, 4.6],
  // Under the girders, so that it is not in a loop with them, a truck and
  // the spreader.
  "crane-land-front": [-1.05, 0.8, 0, -0.76, 1.2, 4.46],
  "crane-sea-d": [0.84, 1, 1, 0.98, 1.2, 4.46],
  "crane-aframe": [0.85, -1.2, 4.76, 3.6, 1.2, 6.4],
  "crane-house": [-3.1, -1.2, 4.76, 0.84, 1.2, 6.4],
  // The two thin rails between the girders, over the trolley that hangs
  // from them.
  "crane-rails": [-2.75, -0.45, 4.57, 6.26, 0.27, 4.6],
  "crane-trolley": [-0.5, -0.95, 4.3, 0.5, 0.82, 4.46],
  "crane-spreader": [-0.5, -1, 0, 0.5, 1, 0.14],
  warehouse: [-2.3, -2.8, 0, 2.3, 2.8, 2.42],
  sign: [-1.2, -0.3, 0, 1.2, 0.3, 7],
  gate: [-0.36, -0.36, 0, 0.36, 0.36, 0.91],
  ground: [0, 0, 0, 0, 0, 0],
};

const CRANE: Sprite[] = [
  "crane-land-back",
  "crane-girder-back",
  "crane-sea-low",
  "crane-sea-b",
  "crane-girder-front",
  "crane-rails",
  "crane-land-front",
  "crane-sea-d",
  "crane-aframe",
  "crane-house",
];
// Where the cables hang from: under the trolley, which is between the
// girders, a little behind the middle of the legs.
const TROLLEY_Z = 4.3;
const CABLES_Y = -0.065;
// The spreader goes up to TOP_Z in the world, which was the height of the
// first crane. This one is lower: above the top of the stacks on a ship it
// is drawn closer, so that it stays under the girders. Below that, it is
// where the world says, onto a truck or a ship.
const STACKS = 2.8;
const UNDER_GIRDERS = 4;
const drawnHeight = (h: number) =>
  h <= STACKS ? h : STACKS + ((h - STACKS) * (UNDER_GIRDERS - STACKS)) / (TOP_Z - STACKS);
const BARRIER = 2.5;

// Whether two pieces cover each other on the drawing. What is drawn of a
// box has six sides, in three directions; two of them are apart when
// they are apart seen along any of the three.
function meet(a: Box, b: Box) {
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
// known of two pieces that are in the same room. Two pieces apart one way
// along one direction and the other way along another are never on the
// same line of sight: neither is behind the other, and saying one is makes
// loops with a third (a leg, a girder and the spreader), which the order
// then breaks in a different place from one moment to the next.
function behind(a: Box, b: Box): boolean | undefined | null {
  let found: boolean | undefined;
  for (const axis of [0, 1, 2]) {
    const back = a[axis + 3] <= b[axis] + near ? true : b[axis + 3] <= a[axis] + near ? false : undefined;
    if (back === undefined) continue;
    if (found === undefined) found = back;
    else if (found !== back) return null;
  }
  return found;
}

type Placed = { drawn: Drawn; box: Box };

// The pieces in the order they are drawn: each after all that it covers.
// Two that are in the same room are told apart by their middles.
function order(pieces: Placed[]): Drawn[] {
  const sorted = [...pieces].sort((a, b) => middle(a.box) - middle(b.box));
  const before: number[][] = sorted.map(() => []);
  for (let i = 0; i < sorted.length; i++) {
    for (let j = i + 1; j < sorted.length; j++) {
      if (!meet(sorted[i].box, sorted[j].box)) continue;
      const back = behind(sorted[i].box, sorted[j].box);
      if (back === null) continue;
      if (back ?? true) before[j].push(i);
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
function place(world: World): Placed[] {
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

  // The warehouse, the sign and the stacks are drawn in the ground; the
  // world still counts its stacks, so that the same seed gives the same yard.
  sprite("gate", "gate", [6.85, GATE_Y, 0]);

  const turn = (world.gate * 80 * Math.PI) / 180;
  // The top of the post beside the booth.
  const pivot: Point = [7.3, GATE_Y, 0.46];
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
  for (const part of CRANE) sprite(part, part, [LANE_X, crane.y, 0]);
  const h = drawnHeight(crane.h);
  if (crane.load) sprite(`c${crane.load.id}`, "container", [crane.x, crane.y, h - 1]);
  sprite("spreader", "crane-spreader", [crane.x, crane.y, h]);
  for (const dx of [-0.4, 0.4]) {
    for (const dy of [-0.6, 0.6]) {
      const x = crane.x + dx;
      const y = crane.y + CABLES_Y + dy;
      line(`cable${dx}${dy}`, [x, y, h + 0.14], [x, y, TROLLEY_Z], "cable");
    }
  }
  sprite("trolley", "crane-trolley", [crane.x, crane.y, 0]);

  return pieces;
}

// What is drawn of the yard, in the order it is drawn.
export function scene(world: World): Drawn[] {
  return order(place(world));
}
