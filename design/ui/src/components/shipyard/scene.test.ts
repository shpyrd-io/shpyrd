import { describe, expect, it } from "vitest";
import { behind, meet, place, scene } from "./scene";
import { createWorld, LANE_X, step, type World } from "./world";

// Two minutes of the yard, looked at six times a second.
function watch(seed: number, see: (world: World) => void) {
  const world = createWorld(seed);
  for (let i = 0; i < 2 * 60 * 60; i++) {
    step(world, 1 / 60);
    if (i % 10 === 0) see(world);
  }
}

describe("the order the shipyard is drawn in", () => {
  it("never draws a piece over one that is before it", () => {
    for (const seed of [1, 2, 3]) {
      watch(seed, (world) => {
        const placed = place(world);
        const at = new Map(scene(world).map((piece, i) => [piece.key, i]));
        for (const a of placed) {
          for (const b of placed) {
            if (a === b || !meet(a.box, b.box) || behind(a.box, b.box) !== true) continue;
            if (at.get(a.drawn.key)! > at.get(b.drawn.key)!) {
              throw new Error(`${a.drawn.key} is behind ${b.drawn.key} and is drawn over it`);
            }
          }
        }
      });
    }
  });

  it("has no two pieces in the same room", () => {
    for (const seed of [4, 5, 6]) {
      watch(seed, (world) => {
        const placed = place(world);
        for (const a of placed) {
          for (const b of placed) {
            if (a === b || !meet(a.box, b.box) || behind(a.box, b.box) !== undefined) continue;
            throw new Error(`${a.drawn.key} and ${b.drawn.key} are in the same room`);
          }
        }
      });
    }
  });

  it("draws what hangs over the lane behind the legs by the water and before the others", () => {
    watch(1, (world) => {
      if (world.crane.x !== LANE_X) return;
      const keys = scene(world).map((piece) => piece.key);
      expect(keys.indexOf("leg9.91")).toBeGreaterThan(keys.indexOf("spreader"));
      expect(keys.indexOf("leg7-1")).toBeLessThan(keys.indexOf("spreader"));
    });
  });

  it("draws a container on a truck over its bed and behind its cab", () => {
    watch(2, (world) => {
      const keys = scene(world).map((piece) => piece.key);
      for (const truck of world.trucks) {
        if (!truck.load) continue;
        expect(keys.indexOf(`c${truck.load.id}`)).toBeGreaterThan(keys.indexOf(`t${truck.id}`));
        expect(keys.indexOf(`c${truck.load.id}`)).toBeLessThan(keys.indexOf(`t${truck.id}cab`));
      }
    });
  });

  it("draws everything once", () => {
    watch(5, (world) => {
      const keys = scene(world).map((piece) => piece.key);
      expect(new Set(keys).size).toBe(keys.length);
    });
  });
});
