"use client";

import type { Project } from "@/api/types";
import { AdvancedIndex, type AdvancedPage } from "@/shell/advanced";

// What each page of a project's Advanced is for, in a line.
const lines: Record<string, string> = {
  releases: "Every version and every build, with their logs.",
  backups: "Take the whole project with you, or bring it back.",
  metrics: "Response times, traffic, CPU and memory over time.",
  logs: "Everything it prints, filtered by process and level.",
  shell: "A terminal inside a running instance.",
  config: "Secrets, plain variables and what services add.",
  resources: "Databases, volumes, and the size and count of each process.",
  domains: "Your own addresses, and where visitors land.",
  drains: "Send the logs to another service.",
  access: "Who may open it: anyone, who signs in, or named people.",
  roles: "Who may deploy, change settings or destroy it.",
  connections: "What reaches the project from outside: tokens and agents.",
  activity: "Everything done to it, by whom and from where.",
};

export function Advanced({ project, shown }: { project: Project; shown: readonly AdvancedPage[] }) {
  return (
    <AdvancedIndex
      base={`/projects/${project.slug}`}
      description={`Everything about ${project.displayName} that the overview leaves out. Your agent can do all of it too.`}
      pages={shown}
      lines={lines}
    />
  );
}
