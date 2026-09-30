import { parseLogLine } from "@shpyrd/shared/logs";
import { json, request, stream } from "@shpyrd/shared/api/http";
import type { LogLine } from "@shpyrd/ui/components/log-view";
import type { Api } from "./api";
import type { APIToken, AuditEntry, Billing, DomainStatus, Metrics, Project, ProjectSummary, Series, Team } from "./types";

// The paths of the server, as ui/src/lib/api.ts calls them. Where the
// server's shape is not the one the screens read, it is turned into it
// here, so the Mock and the Backend answer the same thing.

const project = (slug: string) => `/api/projects/${encodeURIComponent(slug)}`;

// What the server writes in a log record, one per line.
type LogRecord = { t?: string; i?: string; p?: string; m: string };

// What the server answers for the metrics of a project.
type ChartSeries = { name: string; points: [number, number][]; reference?: number; burst?: number };
type Chart = { id: string; title: string; unit: string; kind: "line" | "stacked" | "step"; series: ChartSeries[]; error?: string; note?: string };
export type MetricsAnswer = { range: string; step: number; charts: Chart[]; releases: { number: number; time: number; label: string }[] };

// What the server answers for one project: the phase, the address and
// the access sit in `status` and `spec`, and the processes are only
// known by their status.
export type DetailAnswer = Omit<Project, "phase" | "message" | "url" | "release" | "access" | "exposure"> & {
  spec: Project["spec"] & { access?: Project["access"]; exposure?: Project["exposure"] };
};

export function toProject(d: DetailAnswer): Project {
  const releases = d.status.releases ?? [];
  const processes =
    d.spec.processes ??
    Object.fromEntries(Object.entries(d.processes ?? {}).map(([name, p]) => [name, { size: p.size, replicas: p.desired }]));
  return {
    ...d,
    spec: { ...d.spec, processes },
    status: { ...d.status, releases },
    phase: d.status.phase,
    message: d.status.message,
    url: d.status.url,
    release: releases.at(-1)?.number ?? 0,
    access: d.spec.access ?? "public",
    exposure: d.spec.exposure,
  };
}

// What the server answers for the domains of a project.
type DomainsAnswer = { host?: string; target: string; address?: string; domains: DomainStatus[] };

type BillingAnswer = {
  period: string;
  projection?: number;
  plan?: { name?: string; slug?: string };
  lines: { project?: string; component: string; metric: string; quantity: number; unit: string; unitPrice: number; grossAmount: number }[];
  total: number;
  currency: string;
};

// `17:04:12` from the time a record carries, or nothing.
function clock(t?: string): string | undefined {
  if (!t) return undefined;
  const d = new Date(t);
  return Number.isNaN(d.getTime()) ? t : d.toLocaleTimeString([], { hour12: false });
}

export function toLogLine(record: LogRecord): LogLine {
  const parsed = parseLogLine(record.m);
  return {
    time: clock(record.t ?? parsed.time),
    instance: record.i,
    level: parsed.structured || parsed.levelText ? parsed.level : undefined,
    levelText: parsed.levelText || undefined,
    message: parsed.message,
    fields: parsed.fields.length ? parsed.fields : undefined,
    raw: record.m,
  };
}

const tones: Record<string, Series["tone"]> = { web: "orange", worker: "blue" };

function chartOf(answer: MetricsAnswer, id: string): Series[] {
  const chart = answer.charts.find((c) => c.id === id);
  if (!chart || chart.error) return [];
  return chart.series.map((s) => ({ name: s.name, points: s.points, reference: s.reference, burst: s.burst, tone: tones[s.name] }));
}

export function toMetrics(answer: MetricsAnswer): Metrics {
  const times = answer.charts.flatMap((c) => c.series.flatMap((s) => s.points.map((p) => p[0])));
  const to = times.length ? Math.max(...times) : Math.floor(Date.now() / 1000);
  const from = times.length ? Math.min(...times) : to - 3600;
  return {
    from,
    to,
    responseTime: chartOf(answer, "latency"),
    throughput: chartOf(answer, "throughput"),
    cpu: chartOf(answer, "cpu"),
    memory: chartOf(answer, "memory"),
    network: chartOf(answer, "network"),
    instances: chartOf(answer, "instances"),
    releases: answer.releases.map((r) => ({ time: r.time, label: r.label })),
  };
}

const gone = { method: "DELETE" };

export const backend: Api = {
  config: () => request("/api/config"),
  me: () => request("/api/me"),
  passwordLogin: (body) => request("/api/auth/password", json("POST", body)),
  tokenLogin: (body) => request("/api/auth/token", json("POST", body)),
  logout: () => request("/api/auth/logout", { method: "POST" }),
  invitation: (token) => request(`/api/invitations/${encodeURIComponent(token)}`),
  acceptInvitation: (token) => request(`/api/invitations/${encodeURIComponent(token)}/accept`, json("POST", {})),
  workspace: () => request("/api/workspace"),
  updateWorkspace: (body) => request("/api/workspace", json("PATCH", body)),
  people: () => request("/api/workspace/people"),
  setPersonRole: (email, role) => request(`/api/workspace/people/${encodeURIComponent(email)}`, json("PATCH", { role })),
  setPersonStatus: (email, status) => request(`/api/workspace/people/${encodeURIComponent(email)}`, json("PATCH", { status })),
  removePerson: (email) => request(`/api/workspace/people/${encodeURIComponent(email)}`, gone),
  connections: () => request("/api/workspace/connections"),
  revokeConnection: (id) => request(`/api/workspace/connections/${encodeURIComponent(id)}`, gone),
  invitations: () => request("/api/workspace/invitations"),
  invite: (body) => request("/api/workspace/invitations", json("POST", body)),
  revokeInvitation: (id) => request(`/api/workspace/invitations/${encodeURIComponent(id)}`, gone),
  teams: () => request("/api/teams"),
  saveTeam: (team) => request<Team>("/api/teams", json("POST", { name: team.name, members: team.members, groups: team.groups ?? [] })),
  removeTeam: (name) => request(`/api/teams/${encodeURIComponent(name)}`, gone),
  tokens: () => request("/api/tokens"),
  createToken: async (body) => {
    const made = await request<APIToken & { token: string }>("/api/tokens", json("POST", body));
    const { token: secret, ...token } = made;
    return { token, secret };
  },
  revokeToken: (id) => request(`/api/tokens/${encodeURIComponent(id)}`, gone),
  workspaceDomains: () => request("/api/workspace/domains"),
  addWorkspaceDomain: (host) => request("/api/workspace/domains", json("POST", { host })),
  verifyWorkspaceDomain: (host) => request(`/api/workspace/domains/${encodeURIComponent(host)}/verify`, { method: "POST" }),
  setWorkspaceDomainPrimary: (host) => request(`/api/workspace/domains/${encodeURIComponent(host)}`, json("PATCH", { primary: true })),
  removeWorkspaceDomain: (host) => request(`/api/workspace/domains/${encodeURIComponent(host)}`, gone),
  loginMethods: () => request("/api/workspace/login-methods"),
  addLoginMethod: (body) => request("/api/workspace/login-methods", json("POST", body)),
  removeLoginMethod: (id) => request(`/api/workspace/login-methods/${encodeURIComponent(id)}`, gone),
  domainClaims: () => request("/api/workspace/domain-claims"),
  claimDomain: (domain, connector) => request("/api/workspace/domain-claims", json("POST", { domain, connector })),
  verifyDomainClaim: (domain) => request(`/api/workspace/domain-claims/${encodeURIComponent(domain)}/verify`, { method: "POST" }),
  unclaimDomain: (domain) => request(`/api/workspace/domain-claims/${encodeURIComponent(domain)}`, gone),
  billing: async (month) => {
    const r = await request<BillingAnswer>("/api/workspace/billing/current");
    const line = (l: BillingAnswer["lines"][number]) => ({ project: l.project, component: l.component, metric: l.metric, quantity: l.quantity, unit: l.unit, amount: l.grossAmount });
    if (month && month !== r.period) {
      // A month that closed: its lines as they were invoiced, none until
      // the platform closes months. The plan and the currency are today's.
      const closed = (await request<BillingAnswer["lines"] | null>(`/api/workspace/billing/invoices?month=${encodeURIComponent(month)}`)) ?? [];
      return { plan: r.plan?.name ?? r.plan?.slug ?? "", currency: r.currency, month, closed: true, total: closed.reduce((sum, l) => sum + l.grossAmount, 0), lines: closed.map(line) };
    }
    const billing: Billing = {
      plan: r.plan?.name ?? r.plan?.slug ?? "",
      currency: r.currency,
      month: r.period,
      total: r.total,
      projection: r.projection,
      lines: r.lines.map(line),
    };
    return billing;
  },
  sizes: () => request("/api/sizes"),
  projects: () => request("/api/projects"),
  project: async (slug) => toProject(await request<DetailAnswer>(project(slug))),
  createProject: ({ slug, displayName, description, git, subPath }) =>
    request<ProjectSummary>("/api/projects", json("POST", { name: displayName ?? slug, slug, description, git, subPath })),
  updateProject: async (slug, body) => toProject(await request<DetailAnswer>(project(slug), json("PATCH", body))),
  destroyProject: async (slug) => {
    await request(project(slug), gone);
  },
  deploy: (slug, body) => request(`${project(slug)}/deploy`, json("POST", body)),
  redeploy: (slug, action) => request(`${project(slug)}/redeploy`, json("POST", action ? { action } : {})),
  rollback: async (slug, release) => {
    await request(`${project(slug)}/rollback`, json("POST", { release }));
  },
  applyProcesses: async (slug, changes) => {
    await request(`${project(slug)}/processes`, json("POST", { processes: changes }));
    return backend.project(slug);
  },
  setExposure: async (slug, exposure) => {
    await request(`${project(slug)}/exposure`, json("PUT", { exposure }));
    return backend.project(slug);
  },
  setAccess: async (slug, access) => {
    await request(`${project(slug)}/access`, json("PUT", { access }));
    return backend.project(slug);
  },
  preview: (slug, body) => request(`${project(slug)}/preview`, json("POST", body)),
  audit: async (slug) => {
    const entries = await request<{ time: string; actor: string; action: string; target?: string; detail?: string; via?: string; realm?: string }[]>(`${project(slug)}/audit?limit=50`);
    return entries.map((e): AuditEntry => ({ at: e.time, actor: e.actor, action: e.action, target: e.target, detail: e.detail, via: e.via, realm: e.realm }));
  },
  builds: (slug) => request(`${project(slug)}/builds`),
  streamBuild: (slug, build, follow, signal, onLine) => stream(`${project(slug)}/builds/${encodeURIComponent(build)}/logs?follow=${follow}`, signal, onLine),
  configVars: (slug) => request(`${project(slug)}/secrets`),
  changeConfigVars: async (slug, change) => {
    await request(`${project(slug)}/secrets`, json("PUT", change));
  },
  resources: (slug) => request(`${project(slug)}/resources`),
  createResource: (slug, body) => request(`${project(slug)}/resources`, json("POST", body)),
  removeResource: (slug, kind, name, force = false) => request(`${project(slug)}/resources/${encodeURIComponent(kind)}/${encodeURIComponent(name)}${force ? "?force=true" : ""}`, gone),
  attach: async (slug, body) => {
    await request(`${project(slug)}/bindings`, json("POST", body));
  },
  detach: async (slug, kind, name) => {
    await request(`${project(slug)}/bindings/${encodeURIComponent(kind)}/${encodeURIComponent(name)}`, gone);
  },
  volumes: (slug) => request(`${project(slug)}/volumes`),
  createVolume: (slug, body) => request(`${project(slug)}/volumes`, json("POST", body)),
  resizeVolume: (slug, name, size) => request(`${project(slug)}/volumes/${encodeURIComponent(name)}`, json("PUT", { size })),
  removeVolume: (slug, name, force = false) => request(`${project(slug)}/volumes/${encodeURIComponent(name)}${force ? "?force=true" : ""}`, gone),
  snapshots: (slug, volume) => request(`${project(slug)}/volumes/${encodeURIComponent(volume)}/snapshots`),
  createSnapshot: (slug, volume, name) => request(`${project(slug)}/volumes/${encodeURIComponent(volume)}/snapshots`, json("POST", name ? { name } : {})),
  removeSnapshot: (slug, volume, snapshot) => request(`${project(slug)}/volumes/${encodeURIComponent(volume)}/snapshots/${encodeURIComponent(snapshot)}`, gone),
  restoreVolume: (slug, volume, body) => request(`${project(slug)}/volumes/${encodeURIComponent(volume)}/restore`, json("POST", body)),
  domains: async (slug) => {
    const r = await request<DomainsAnswer>(`${project(slug)}/domains`);
    return { target: r.target, domains: r.domains };
  },
  addDomain: async (slug, host) => {
    const r = await request<DomainsAnswer>(`${project(slug)}/domains`, json("POST", { host }));
    return r.domains.find((d) => d.host === host) ?? { host, dns: "unknown", certificate: "issuing" };
  },
  removeDomain: async (slug, host) => {
    await request(`${project(slug)}/domains/${encodeURIComponent(host)}`, gone);
  },
  drains: (slug) => request(`${project(slug)}/drains`),
  addDrain: (slug, body) => request(`${project(slug)}/drains`, json("POST", body)),
  removeDrain: (slug, name) => request(`${project(slug)}/drains/${encodeURIComponent(name)}`, gone),
  members: (slug) => request(`${project(slug)}/members`),
  setMember: (slug, member) => request(`${project(slug)}/members`, json("POST", { role: member.role, user: member.user, team: member.team })),
  removeMember: (slug, name) => request(`${project(slug)}/members/${encodeURIComponent(name)}`, gone),
  allow: (slug) => request(`${project(slug)}/allow`),
  setAllow: (slug, entries) => request(`${project(slug)}/allow`, json("PUT", entries)),
  streamLogs: (slug, query, signal, onLine) => {
    const q = new URLSearchParams({ format: "json", tail: String(query.tail ?? 200), follow: query.follow ? "true" : "false" });
    if (query.process) q.set("process", query.process);
    return stream(`${project(slug)}/logs?${q}`, signal, (line) => {
      try {
        onLine(toLogLine(JSON.parse(line) as LogRecord));
      } catch {
        onLine(toLogLine({ m: line }));
      }
    });
  },
  // In total mode the CPU is in cores and the memory in bytes, with the
  // allocation of each process beside them.
  metrics: async (slug, range) => toMetrics(await request<MetricsAnswer>(`${project(slug)}/metrics?range=${encodeURIComponent(range)}&mode=total`)),
  instances: (slug) => request(`${project(slug)}/instances`),
  shellTicket: (slug, instance) => request(`${project(slug)}/shell/ticket?instance=${encodeURIComponent(instance)}`, { method: "POST" }),
  // The socket goes where the calls go: the server refuses another
  // origin, and in development the dev server forwards the upgrade.
  shellSocket: async (slug, instance, ticket) => `${location.protocol === "https:" ? "wss:" : "ws:"}//${location.host}${project(slug)}/shell?instance=${encodeURIComponent(instance)}&ticket=${encodeURIComponent(ticket)}`,
};
