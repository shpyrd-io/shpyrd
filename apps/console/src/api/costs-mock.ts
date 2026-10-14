import { ApiError } from "@shpyrd/shared/api/error";
import type { CostExport, CostQuery, CostRow, CostSummary } from "./types";
import sample from "../../mock/cost-lines.json";

const categoryNames: Record<string, string> = {
  project: "Project-attributed",
  system: "System services",
  idle: "Idle capacity",
  unattributed: "Unattributed costs",
};
const jobs = new Map<string, { job: CostExport; csv: string }>();
export function exploreMockCosts(q: CostQuery): CostSummary {
  const group = q.group ?? "project";
  const all = sample as (CostRow & { resourceName?: string })[];
  const groups = new Map<string, CostRow>();
  const total: Record<string, number> = {};
  const categories: Record<string, Record<string, number>> = {};
  // The fixture represents one recorded day; other ranges intentionally show no data.
  const rows =
    (q.from && q.from > "2026-10-13") || (q.to && q.to <= "2026-10-13")
      ? []
      : all;
  for (const line of rows) {
    if (q.kind === "usage") continue;
    const l =
      q.kind === "real"
        ? {
            ...line,
            workspace: "",
            workspaceName: "",
            project: "",
            slug: "",
            category: "unattributed",
            service:
              line.resourceType === "volume" ? "Block Storage" : "Compute",
          }
        : line;
    const fields = [
      "workspace",
      "project",
      "category",
      "workload",
      "process",
      "resourceType",
      "resource",
      "service",
    ] as const;
    if (
      fields.some((k) => q[k] && (q[k] === "__none__" ? !!l[k] : q[k] !== l[k]))
    )
      continue;
    if (
      q.search &&
      ![
        l.workspaceName,
        l.slug,
        l.project,
        l.process,
        l.resource,
        l.resourceName,
        l.service,
      ]
        .join(" ")
        .toLowerCase()
        .includes(q.search.toLowerCase())
    )
      continue;
    const r: CostRow = { key: "", cost: {}, lines: 0 };
    if (["workspace", "project", "workload", "process"].includes(group)) {
      r.workspace = l.workspace;
      r.workspaceName = l.workspaceName;
      r.category = l.category;
      r.name = l.workspaceName || categoryNames[l.category ?? "unattributed"];
      if (group !== "workspace") {
        r.project = l.project;
        r.slug = l.slug;
        r.name = l.slug || categoryNames[l.category ?? "unattributed"];
      }
      if (["workload", "process"].includes(group)) {
        r.workload = l.workload;
        r.name = l.workload;
      }
      if (group === "process") {
        r.process = l.process;
        r.name = l.process;
      }
    } else if (group === "service") {
      r.service = l.service;
      r.name = l.service || "Unspecified provider service";
    } else {
      r.resourceType = l.resourceType;
      r.name = l.resourceType || "Unknown resource type";
      if (group === "resource") {
        r.resource = l.resource;
        r.name = l.resourceName || l.resource || "Unknown resource";
      }
    }
    r.key = JSON.stringify(fields.map((k) => r[k] || ""));
    const current = groups.get(r.key) ?? r;
    current.lines += l.lines;
    const category = (categories[l.category ?? "unattributed"] ??= {});
    for (const [currency, amount] of Object.entries(l.cost)) {
      current.cost[currency] = (current.cost[currency] ?? 0) + amount;
      total[currency] = (total[currency] ?? 0) + amount;
      category[currency] = (category[currency] ?? 0) + amount;
    }
    groups.set(r.key, current);
  }
  const currencies = Object.keys(total).sort();
  const currency =
    q.currency === "__none__"
      ? ""
      : q.currency ||
        (currencies.includes("USD") ? "USD" : currencies[0]) ||
        "";
  const sorted = [...groups.values()].sort(
    (a, b) =>
      (q.sort === "name"
        ? (a.name ?? "").localeCompare(b.name ?? "")
        : ((a.cost[currency] ?? 0) - (b.cost[currency] ?? 0)) *
          (q.sort === "cost_asc" ? 1 : -1)) || a.key.localeCompare(b.key),
  );
  const offset = q.offset ?? 0,
    limit = q.limit ?? 50;
  return {
    from: q.from ?? "2026-10-01",
    to: q.to ?? "2026-11-01",
    kind: q.kind ?? "estimated",
    group,
    rows: sorted.slice(offset, offset + limit),
    total,
    categories,
    count: sorted.length,
    offset,
    limit,
    currency,
    currencies,
    collection: {
      through: "2026-10-13T21:00:00Z",
      collectedAt: "2026-10-13T21:05:00Z",
    },
    incomplete: (q.to ?? "2026-11-01") > "2026-10-13",
    workspaces: [
      { id: "production", name: "Production" },
      { id: "staging", name: "Staging" },
    ],
  };
}
export async function createMockCostExport(q: CostQuery): Promise<CostExport> {
  const job: CostExport = {
    id: crypto.randomUUID(),
    status: "ready",
    expiresAt: new Date(Date.now() + 86400000).toISOString(),
  };
  const data = exploreMockCosts({ ...q, limit: 100000, offset: 0 });
  const escape = (s: string) =>
    `"${(/^[\s]*[=+@-]/.test(s) ? "'" + s : s).replaceAll('"', '""')}"`;
  const csv = [
    "Name,Workspace,Currency,Cost,Cost records",
    ...data.rows.flatMap((r) =>
      Object.entries(r.cost).map(([c, v]) =>
        [r.name ?? "", r.workspaceName ?? "", c, String(v), String(r.lines)]
          .map(escape)
          .join(","),
      ),
    ),
  ].join("\n");
  jobs.set(job.id, { job, csv });
  return job;
}
export async function mockCostExport(id: string) {
  const data = jobs.get(id);
  if (!data) throw new ApiError(404, "Export expired");
  return data.job;
}
export async function mockCostExportURL(id: string) {
  return `data:text/csv;charset=utf-8,${encodeURIComponent(jobs.get(id)?.csv ?? "")}`;
}
