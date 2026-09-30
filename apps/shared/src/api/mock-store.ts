import { ApiError } from "./error";

// The Mock answers from JSON files, one per kind of thing, with one
// generic handler: list, find by name, change, remove. What a screen
// changes is kept in localStorage, so it survives a reload. It answers
// after a short wait, so loading states show.

const PREFIX = "shpyrd.mock.";

export function wait(ms = 150 + Math.random() * 250): Promise<void> {
  return new Promise((done) => setTimeout(done, ms));
}

export type Collection<T> = {
  list: () => Promise<T[]>;
  find: (key: string) => Promise<T>;
  set: (item: T) => Promise<T>;
  remove: (key: string) => Promise<void>;
  reset: () => void;
};

export function collection<T>(name: string, seed: T[], keyOf: (item: T) => string): Collection<T> {
  const storage = PREFIX + name;
  let items: T[] | null = null;

  const load = (): T[] => {
    if (items) return items;
    try {
      const kept = localStorage.getItem(storage);
      items = kept ? (JSON.parse(kept) as T[]) : structuredClone(seed);
    } catch {
      items = structuredClone(seed);
    }
    return items;
  };
  const save = () => {
    try {
      localStorage.setItem(storage, JSON.stringify(items));
    } catch {
      // storage may be closed to us; the change lives until the reload
    }
  };

  return {
    async list() {
      await wait();
      return structuredClone(load());
    },
    async find(key) {
      await wait();
      const item = load().find((i) => keyOf(i) === key);
      if (!item) throw new ApiError(404, `${name}: ${key} not found`);
      return structuredClone(item);
    },
    async set(item) {
      await wait();
      const all = load();
      const at = all.findIndex((i) => keyOf(i) === keyOf(item));
      if (at >= 0) all[at] = item;
      else all.push(item);
      save();
      return structuredClone(item);
    },
    async remove(key) {
      await wait();
      const all = load();
      const at = all.findIndex((i) => keyOf(i) === key);
      if (at < 0) throw new ApiError(404, `${name}: ${key} not found`);
      all.splice(at, 1);
      save();
    },
    reset() {
      items = null;
      try {
        localStorage.removeItem(storage);
      } catch {
        // nothing kept
      }
    },
  };
}

// One thing, not a list: the workspace, the person signed in.
export function single<T>(name: string, seed: T) {
  const c = collection<T>(name, [seed], () => name);
  return {
    get: () => c.find(name),
    set: (item: T) => c.set(item),
    reset: c.reset,
  };
}
