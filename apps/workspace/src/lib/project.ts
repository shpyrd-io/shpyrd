import type { Phase } from "@shpyrd/ui/components/launcher-card";
import { order, type Tone } from "@shpyrd/ui/lib/chart";
import type { ProjectSummary } from "@/api/types";

// How a project is read for the launcher card: its light, where it
// answers, whether it is locked, and the ink it keeps.

export function phaseOf(p: ProjectSummary): Phase {
  const processes = Object.values(p.processes ?? {});
  if (processes.some((s) => s.sleep?.state === "asleep")) return "sleeping";
  switch (p.phase) {
    case "Running":
      return "running";
    case "Failed":
      return "failed";
    default:
      return "deploying";
  }
}

export const phaseWords: Record<Phase, string> = {
  running: "Running",
  deploying: "Deploying",
  failed: "Failed",
  sleeping: "Asleep",
};

export function toneOf(slug: string): Tone {
  let h = 0;
  for (const c of slug) h = (h * 31 + c.charCodeAt(0)) >>> 0;
  return order[h % order.length];
}

// Where to send someone to open the app: a public one at its address; one
// behind sign-in through its sign-in bounce, silent for who is signed in,
// so the app knows who is there.
export function openUrl(url: string | undefined, access: string | undefined) {
  if (!url) return "#";
  if (!access || access === "public") return url;
  return url.replace(/\/$/, "") + "/.shpyrd/signin?rd=%2F";
}

// The address without the scheme, as it is written on a card.
export function hostOf(url?: string) {
  return url?.replace(/^https?:\/\//, "");
}

export function ago(iso: string): string {
  const s = Math.max(0, Math.floor((Date.now() - new Date(iso).getTime()) / 1000));
  if (s < 60) return `${s}s ago`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ago`;
  const h = Math.floor(m / 60);
  if (h < 48) return `${h}h ago`;
  return `${Math.floor(h / 24)}d ago`;
}
