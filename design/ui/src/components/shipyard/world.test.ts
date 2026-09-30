import { describe, expect, it } from "vitest";
import { scene } from "./scene";
import { createWorld, step, type World } from "./world";

const minutes = (world: World, n: number, each?: () => void) => {
  for (let i = 0; i < n * 60 * 60; i++) {
    step(world, 1 / 60);
    each?.();
  }
};

// Every container there is, wherever it is.
const containers = (world: World) => [
  ...world.yard.map((c) => c.id),
  ...world.ships.flatMap((s) => s.slots.flatMap((c) => (c ? [c.id] : []))),
  ...world.trucks.flatMap((t) => (t.load ? [t.load.id] : [])),
  ...(world.crane.load ? [world.crane.load.id] : []),
];

describe("the shipyard", () => {
  it("is the same yard for the same seed", () => {
    const a = createWorld(7);
    const b = createWorld(7);
    minutes(a, 1);
    minutes(b, 1);
    expect(scene(a)).toEqual(scene(b));
    expect(scene(createWorld(8))).not.toEqual(scene(createWorld(7)));
  });

  it("starts with a ship at the quay and the work going on", () => {
    const world = createWorld(1);
    expect(world.ships[0].state).toBe("moored");
    expect(world.trucks.length).toBeGreaterThan(0);
  });

  it("sees ships come and go, and trucks leave with what they came for", () => {
    const world = createWorld(1);
    const ships = new Set<number>();
    const trucks = new Set<number>();
    minutes(world, 10, () => {
      for (const ship of world.ships) ships.add(ship.id);
      for (const truck of world.trucks) trucks.add(truck.id);
    });
    expect(ships.size).toBeGreaterThan(4);
    expect(trucks.size).toBeGreaterThan(20);
  });

  it("never has a container in two places, nor two trucks in the same place", () => {
    const world = createWorld(3);
    minutes(world, 10, () => {
      const all = containers(world);
      expect(new Set(all).size).toBe(all.length);
      const along = world.trucks.map((t) => t.y).sort((a, b) => a - b);
      for (let i = 1; i < along.length; i++) expect(along[i] - along[i - 1]).toBeGreaterThan(3.3);
      const ships = world.ships.map((s) => s.y).sort((a, b) => a - b);
      for (let i = 1; i < ships.length; i++) expect(ships[i] - ships[i - 1]).toBeGreaterThan(14);
    });
  });

  it("leaves a ship with what the work said: nothing is left hanging", () => {
    const world = createWorld(5);
    minutes(world, 10, () => {
      for (const ship of world.ships) {
        if (ship.state !== "leaving") continue;
        expect(ship.done).toBe(ship.work.length);
        expect(world.trucks.every((truck) => truck.ship !== ship.id || truck.state === "out")).toBe(true);
      }
    });
  });

  it("is never long without a ship at the quay", () => {
    const world = createWorld(11);
    let empty = 0;
    let longest = 0;
    minutes(world, 10, () => {
      empty = world.ships.some((s) => s.state === "moored") ? 0 : empty + 1 / 60;
      longest = Math.max(longest, empty);
    });
    expect(longest).toBeLessThan(12);
  });

  it("keeps the crane over its own reach, and what it carries off the ground", () => {
    const world = createWorld(9);
    minutes(world, 10, () => {
      expect(world.crane.x).toBeGreaterThanOrEqual(8.5);
      expect(world.crane.x).toBeLessThanOrEqual(13.15);
      expect(world.crane.h).toBeGreaterThanOrEqual(1.45);
      expect(world.crane.h).toBeLessThanOrEqual(4.6);
    });
  });
});
