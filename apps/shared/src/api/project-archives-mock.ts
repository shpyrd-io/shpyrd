import { ApiError } from "./error";
import { single, wait } from "./mock-store";
import type { ProjectArchiveStatus } from "./project-archives";

// Preview the timing and maintenance states without producing a fake
// recovery archive or reading the user's selected file.
export function mockProjectArchives(scope: string) {
  type Operation = { kind: "export" | "restore" | "move"; started: number };
  const kept = single<Record<string, Operation>>(`${scope}-archive-operations`, {});
  const active = new Set<string>();
  const phases = {
    export: ["preparing", "paused", "starting"],
    restore: ["validating", "staging", "starting"],
    move: ["preparing", "copying", "starting"],
  };
  const duration = 6_000;

  async function status(id: string): Promise<ProjectArchiveStatus> {
    const operation = (await kept.get())[id];
    const elapsed = operation ? Date.now() - operation.started : duration;
    if (!operation || elapsed >= duration) return { phase: "idle", preview: true };
    return {
      preview: true, active: true, kind: operation.kind,
      phase: phases[operation.kind][Math.min(2, Math.floor(elapsed / 2_000))]!,
      startedAt: new Date(operation.started).toISOString(),
    };
  }

  async function run(id: string, kind: Operation["kind"]) {
    if (active.has(id)) throw new ApiError(423, "A preview operation is already running for this project.");
    active.add(id);
    try {
      const all = await kept.get();
      if (all[id] && Date.now() - all[id].started < duration) throw new ApiError(423, "A preview operation is already running for this project.");
      all[id] = { kind, started: Date.now() };
      await kept.set(all);
      await wait(duration);
    } finally {
      active.delete(id);
    }
  }

  return {
    projectArchiveStatus: status,
    simulateMove: (id: string) => run(id, "move"),
    backupProject: (id: string) => run(id, "export"),
    restoreProject: (id: string, _file: File) => run(id, "restore"),
    recoverProject: (id: string) => run(id, "restore"),
  };
}
