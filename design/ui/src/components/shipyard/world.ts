// The shipyard as numbers: where everything is and what it is doing.
// Nothing here draws, and nothing here looks at a clock: `step` is told
// how much time went by. The same seed gives the same yard, second for
// second.
//
// A unit is the width of a container. x goes across the quay, towards the
// water; y goes along it, the way the trucks and the ships go; z goes up.

// Where things are.
export const LANE_X = 8.5;
export const QUAY_X = 10.2;
export const SHIP_X = 12.6;
export const BERTH_Y = 3;
export const GATE_Y = -8;
export const WAIT_Y = -4.8;
// The places of a ship: three bays along it, two columns across, two high.
export const BAYS = [-2.3, 0, 2.3];
export const COLUMNS = [-0.55, 0.55];
export const LAYERS = 2;
export const DECK_Z = 0.8;
export const BED_Z = 0.45;
// How high the crane holds the top of a container while it carries it.
export const TOP_Z = 4.6;

// Where things come from and go to, out of sight.
const TRUCK_IN = -20;
const TRUCK_OUT = 20;
const SHIP_IN = -24;
const SHIP_OUT = 34;

// How fast things go, in units a second, and how they speed up and brake.
const TRUCK_SPEED = 3.4;
const TRUCK_PUSH = 2.5;
const TRUCK_BRAKE = 3;
const TRUCK_GAP = 4.2;
// From the middle of the bed of a truck to its nose.
const TRUCK_NOSE = 2.05;
const SHIP_SPEED = 3;
const SHIP_PUSH = 0.7;
const SHIP_BRAKE = 0.45;
// From the middle of a ship to the middle of the one that waits behind it.
const SHIP_GAP = 15;
const GANTRY_SPEED = 1.2;
const TROLLEY_SPEED = 2.6;
const HOIST_SPEED = 2.8;
const GRIP = 0.35;
const GATE_TIME = 0.7;

export type Container = { id: number };

// One thing the crane has to do for a ship: take a container off it, to
// a truck, or put on it the one a truck brings.
export type Move = { kind: "unload" | "load"; bay: number; column: number };

export type Ship = {
  id: number;
  y: number;
  v: number;
  state: "arriving" | "moored" | "leaving";
  slots: (Container | null)[];
  work: Move[];
  // How many moves already have a truck on its way, and how many are done.
  sent: number;
  done: number;
};

export type Truck = {
  id: number;
  y: number;
  v: number;
  // in: on its way to the line where it waits. called: on its way to the
  // crane. served: under the crane. out: on its way out.
  state: "in" | "called" | "served" | "out";
  load: Container | null;
  // The ship and the move it came for, and where the crane wants it.
  ship: number;
  move: number;
  stop: number;
};

type Step =
  | { kind: "go"; x?: number; y?: number; h?: number }
  | { kind: "wait"; t: number }
  | { kind: "truck" }
  | { kind: "grab"; from: "ship" | "truck" }
  | { kind: "release"; to: "ship" | "truck" };

export type Crane = {
  // The trolley across the quay, the whole crane along it, and the height
  // of what takes the container.
  x: number;
  y: number;
  h: number;
  load: Container | null;
  steps: Step[];
  truck: number;
  slot: number;
};

export type World = {
  seed: number;
  ids: number;
  // The one furthest ahead first: at most one leaving, one at the quay or
  // on its way to it, one waiting behind.
  ships: Ship[];
  // Seconds until the next ship, and until the next truck may come.
  nextShip: number;
  nextTruck: number;
  trucks: Truck[];
  crane: Crane;
  // The barrier: 0 is down, 1 is up.
  gate: number;
  yard: { id: number; x: number; y: number; z: number }[];
};

// A number from 0 to 1 that is always the same for the same seed.
function random(world: World) {
  world.seed = (world.seed + 0x6d2b79f5) | 0;
  let t = Math.imul(world.seed ^ (world.seed >>> 15), 1 | world.seed);
  t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
  return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
}

const pick = (world: World, n: number) => Math.floor(random(world) * n);

export const slot = (bay: number, column: number, layer: number) =>
  (bay * COLUMNS.length + column) * LAYERS + layer;

function height(ship: Ship, bay: number, column: number) {
  let layers = 0;
  while (layers < LAYERS && ship.slots[slot(bay, column, layers)]) layers++;
  return layers;
}

// A ship with some containers on it, and the list of what is to be done
// with it: every move is one that can be done when its turn comes.
function ship(world: World, y: number): Ship {
  const slots: (Container | null)[] = Array(BAYS.length * COLUMNS.length * LAYERS).fill(null);
  const heights: number[] = [];
  for (let bay = 0; bay < BAYS.length; bay++) {
    for (let column = 0; column < COLUMNS.length; column++) {
      const layers = pick(world, LAYERS + 1);
      heights.push(layers);
      for (let layer = 0; layer < layers; layer++) slots[slot(bay, column, layer)] = { id: world.ids++ };
    }
  }
  const work: Move[] = [];
  const moves = 4 + pick(world, 3);
  for (let i = 0; i < moves; i++) {
    const stack = pick(world, heights.length);
    const kind =
      heights[stack] === 0 ? "load" : heights[stack] === LAYERS ? "unload" : random(world) < 0.5 ? "load" : "unload";
    heights[stack] += kind === "load" ? 1 : -1;
    work.push({ kind, bay: Math.floor(stack / COLUMNS.length), column: stack % COLUMNS.length });
  }
  return { id: world.ids++, y, v: 0, state: "arriving", slots, work, sent: 0, done: 0 };
}

// The yard as it is after a while: a ship at the quay, the work going on.
export function createWorld(seed = 1): World {
  const world: World = {
    seed,
    ids: 1,
    ships: [],
    nextShip: 0,
    nextTruck: 0,
    trucks: [],
    crane: { x: LANE_X, y: BERTH_Y, h: TOP_Z, load: null, steps: [], truck: 0, slot: 0 },
    gate: 0,
    yard: [],
  };
  for (const x of [1.2, 2.4, 3.6, 4.8]) {
    for (const y of [3, 5.2, 7.4, 9.6]) {
      const layers = pick(world, 3);
      for (let z = 0; z < layers; z++) world.yard.push({ id: world.ids++, x, y, z });
    }
  }
  world.ships.push({ ...ship(world, BERTH_Y), state: "moored" });
  for (let i = 0; i < 16 * 60; i++) step(world, 1 / 60);
  return world;
}

// A value on its way to where it is wanted, slowing down as it gets there.
function toward(value: number, target: number, speed: number, dt: number) {
  const left = target - value;
  const most = Math.min(speed, 0.5 + Math.abs(left) * 4) * dt;
  return Math.abs(left) <= most ? target : value + Math.sign(left) * most;
}

// How fast something may go when it has to stand still `left` further on.
const braking = (left: number, brake: number) => Math.sqrt(2 * brake * Math.max(0, left));

function sail(world: World, dt: number) {
  world.ships.forEach((s, i) => {
    if (s.state === "arriving") {
      // As far as the quay, or as far as behind the ship that is there.
      const ahead = world.ships[i - 1];
      const stop = ahead ? Math.min(BERTH_Y, ahead.y - SHIP_GAP) : BERTH_Y;
      s.v = Math.min(s.v + SHIP_PUSH * dt, SHIP_SPEED, braking(stop - s.y, SHIP_BRAKE) + 0.05);
      s.y = Math.min(s.y + s.v * dt, Math.max(s.y, stop));
      if (s.y === stop) s.v = 0;
      if (s.y === BERTH_Y) s.state = "moored";
    } else if (s.state === "moored") {
      if (s.done === s.work.length && world.crane.steps.length === 0) s.state = "leaving";
    } else {
      s.v = Math.min(s.v + SHIP_PUSH * dt, SHIP_SPEED);
      s.y += s.v * dt;
    }
  });
  world.ships = world.ships.filter((s) => s.y < SHIP_OUT);

  // The next ship comes when the one at the quay is nearly done, so that
  // the quay is not left empty for long.
  const busy = world.ships.filter((s) => s.state !== "leaving");
  const nearlyDone = busy.length === 1 && busy[0].work.length - busy[0].done <= 2;
  if (busy.length === 0 || nearlyDone) {
    world.nextShip -= dt;
    if (world.nextShip <= 0) {
      world.ships.push(ship(world, SHIP_IN));
      world.nextShip = 1 + random(world) * 3;
    }
  }
}

// A truck for the next move of the ship that has none yet: empty to take
// a container away, loaded to bring one.
function dispatch(world: World, dt: number) {
  const s = world.ships.find((ship) => ship.state !== "leaving" && ship.sent < ship.work.length);
  world.nextTruck -= dt;
  if (!s || world.nextTruck > 0) return;
  if (world.trucks.some((truck) => truck.state === "in" || truck.y < TRUCK_IN + TRUCK_GAP)) return;
  world.trucks.push({
    id: world.ids++,
    y: TRUCK_IN,
    v: TRUCK_SPEED,
    state: "in",
    load: s.work[s.sent].kind === "load" ? { id: world.ids++ } : null,
    ship: s.id,
    move: s.sent,
    stop: WAIT_Y,
  });
  s.sent++;
  world.nextTruck = 1 + random(world) * 2.5;
}

function drive(world: World, dt: number) {
  // The one furthest ahead first, so that each knows where the next is.
  world.trucks.sort((a, b) => b.y - a.y);
  world.trucks.forEach((truck, i) => {
    if (truck.state === "served") return;
    let stop = truck.state === "out" ? Infinity : truck.stop;
    const barrier = GATE_Y - TRUCK_NOSE - 0.4;
    if (truck.y <= barrier && world.gate < 0.9) stop = Math.min(stop, barrier);
    if (i > 0) stop = Math.min(stop, world.trucks[i - 1].y - TRUCK_GAP);
    truck.v = Math.min(truck.v + TRUCK_PUSH * dt, TRUCK_SPEED, braking(stop - truck.y, TRUCK_BRAKE));
    truck.y += truck.v * dt;
    if (stop - truck.y < 0.01) {
      truck.y = Math.max(truck.y, Math.min(stop, truck.y + 0.01));
      truck.v = 0;
      if (truck.state === "called" && stop === truck.stop) truck.state = "served";
    }
  });
  world.trucks = world.trucks.filter((truck) => truck.y < TRUCK_OUT);

  // The barrier goes up for a truck that is coming, and down behind it.
  const coming = world.trucks.some((truck) => truck.y > GATE_Y - 5.2 && truck.y < GATE_Y + 1.6);
  world.gate = Math.max(0, Math.min(1, world.gate + ((coming ? 1 : -1) * dt) / GATE_TIME));
}

// The crane takes the next move of the ship, once its truck is on the way.
function plan(world: World) {
  const { crane } = world;
  const s = world.ships.find((ship) => ship.state === "moored");
  if (crane.steps.length || !s || s.done === s.work.length) return;
  const truck = world.trucks.find((t) => t.state === "in" && t.ship === s.id && t.move === s.done);
  if (!truck) return;
  const move = s.work[s.done];
  const layers = height(s, move.bay, move.column);
  const layer = move.kind === "unload" ? layers - 1 : layers;
  const x = SHIP_X + COLUMNS[move.column];
  const y = s.y + BAYS[move.bay];
  const onShip = DECK_Z + layer + 1;
  const onTruck = BED_Z + 1;
  truck.state = "called";
  truck.stop = y;
  crane.truck = truck.id;
  crane.slot = slot(move.bay, move.column, layer);
  crane.steps =
    move.kind === "unload"
      ? [
          { kind: "go", x, y, h: TOP_Z },
          { kind: "go", h: onShip },
          { kind: "wait", t: GRIP },
          { kind: "grab", from: "ship" },
          { kind: "go", h: TOP_Z },
          { kind: "go", x: LANE_X },
          { kind: "truck" },
          { kind: "go", h: onTruck },
          { kind: "wait", t: GRIP },
          { kind: "release", to: "truck" },
          { kind: "go", h: TOP_Z },
        ]
      : [
          { kind: "go", x: LANE_X, y, h: TOP_Z },
          { kind: "truck" },
          { kind: "go", h: onTruck },
          { kind: "wait", t: GRIP },
          { kind: "grab", from: "truck" },
          { kind: "go", h: TOP_Z },
          { kind: "go", x },
          { kind: "go", h: onShip },
          { kind: "wait", t: GRIP },
          { kind: "release", to: "ship" },
          { kind: "go", h: TOP_Z },
        ];
}

function lift(world: World, dt: number) {
  const { crane } = world;
  const s = world.ships.find((ship) => ship.state === "moored");
  const now = crane.steps[0];
  if (!now || !s) return;
  const truck = world.trucks.find((t) => t.id === crane.truck);
  let over = false;
  if (now.kind === "go") {
    if (now.x !== undefined) crane.x = toward(crane.x, now.x, TROLLEY_SPEED, dt);
    if (now.y !== undefined) crane.y = toward(crane.y, now.y, GANTRY_SPEED, dt);
    if (now.h !== undefined) crane.h = toward(crane.h, now.h, HOIST_SPEED, dt);
    over =
      (now.x === undefined || crane.x === now.x) &&
      (now.y === undefined || crane.y === now.y) &&
      (now.h === undefined || crane.h === now.h);
  } else if (now.kind === "wait") {
    now.t -= dt;
    over = now.t <= 0;
  } else if (now.kind === "truck") {
    over = truck?.state === "served";
  } else if (now.kind === "grab") {
    if (now.from === "ship") {
      crane.load = s.slots[crane.slot];
      s.slots[crane.slot] = null;
    } else if (truck) {
      crane.load = truck.load;
      truck.load = null;
      truck.state = "out";
    }
    over = true;
  } else {
    if (now.to === "ship") s.slots[crane.slot] = crane.load;
    else if (truck) {
      truck.load = crane.load;
      truck.state = "out";
    }
    crane.load = null;
    over = true;
  }
  if (over) {
    crane.steps.shift();
    if (crane.steps.length === 0) s.done++;
  }
}

// The yard, `dt` seconds later.
export function step(world: World, dt: number) {
  sail(world, dt);
  dispatch(world, dt);
  plan(world);
  lift(world, dt);
  drive(world, dt);
}
