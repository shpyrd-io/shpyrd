// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from "vitest";
import { collection, single } from "./mock-store";

type Thing = { name: string; n: number };

describe("the mock store", () => {
  beforeEach(() => localStorage.clear());

  it("lists the seed, finds by name, and does not hand out what it keeps", async () => {
    const things = collection<Thing>("things", [{ name: "a", n: 1 }], (t) => t.name);
    const listed = await things.list();
    expect(listed).toEqual([{ name: "a", n: 1 }]);
    listed[0]!.n = 99;
    expect((await things.find("a")).n).toBe(1);
  });

  it("keeps a change across a fresh collection, and forgets it on reset", async () => {
    const first = collection<Thing>("kept", [{ name: "a", n: 1 }], (t) => t.name);
    await first.set({ name: "a", n: 2 });
    await first.set({ name: "b", n: 3 });
    const again = collection<Thing>("kept", [{ name: "a", n: 1 }], (t) => t.name);
    expect((await again.list()).map((t) => `${t.name}${t.n}`)).toEqual(["a2", "b3"]);
    again.reset();
    expect(await collection<Thing>("kept", [{ name: "a", n: 1 }], (t) => t.name).list()).toEqual([{ name: "a", n: 1 }]);
  });

  it("fails to find or remove what is not there", async () => {
    const things = collection<Thing>("none", [], (t) => t.name);
    await expect(things.find("x")).rejects.toThrow("not found");
    await expect(things.remove("x")).rejects.toThrow("not found");
  });

  it("keeps one thing on its own", async () => {
    const one = single<Thing>("one", { name: "a", n: 1 });
    await one.set({ name: "a", n: 5 });
    expect((await single<Thing>("one", { name: "a", n: 1 }).get()).n).toBe(5);
  });
});
