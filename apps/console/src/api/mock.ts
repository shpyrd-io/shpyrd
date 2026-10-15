import { exploreMockCosts, createMockCostExport, mockCostExport, mockCostExportURL } from "./costs-mock";
import { overviewMock } from "./cluster-overview-mock";
import { ApiError } from "@shpyrd/shared/api/error";
import { mockProjectArchives } from "@shpyrd/shared/api/project-archives-mock";
import { collection, single, wait } from "@shpyrd/shared/api/mock-store";
import type { ArchiveProject, ProjectPlacement } from "@shpyrd/shared/api/project-archives";
import type { Link } from "@shpyrd/shared/links";
import type { Api } from "./api";
import type {
  BackupInfo,
  ClusterSettings,
  ClusterSummary,
  ConsoleUser,
  CostDrain,
  HelmRelease,
  Identity,
  LicenseStatus,
  LocalUser,
  LoginMethods,
  MailStatus,
  NodeUsage,
  OCIStatus,
  ObjectStorageSummary,
  PostgresServer,
  PostgresServers,
  PostgresTenant,
  PublicConfig,
  RegistryInfo,
  SizeCatalog,
  WorkspaceSettings,
  WorkspaceSummary,
} from "./types";
import { byNode } from "@/lib/samples";
import cluster from "../../mock/cluster.json";
import config from "../../mock/config.json";
import me from "../../mock/me.json";
import things from "../../mock/things.json";
import placement from "../../mock/project-placement.json";
import archiveProjects from "../../mock/archive-projects.json";
import postgresServers from "../../mock/postgres-servers.json";
import postgresTenants from "../../mock/postgres-tenants.json";

const placementOf = collection<ProjectPlacement & { id: string }>("console-project-placement-v3", placement, (item) => item.id);
const archiveActions = mockProjectArchives("console");

const archiveProjectsOf = collection<ArchiveProject>("console-archive-projects", archiveProjects, (project) => project.id);
// The shared Postgres fleet: the three servers of the file, then twenty
// more made up the same way every time, so the page is drawn for the
// thousands of databases it is for. Every database on every server is
// made up from its server's number, so a search finds the same ones.
function seeded(seed: number) {
  let a = seed >>> 0;
  return () => {
    a = (a + 0x6d2b79f5) >>> 0;
    let t = a;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}
const workspaces = ["acme", "fluxyr", "northwind", "igormorales", "contoso", "umbrella", "initech", "globex", "hooli", "vandelay", "wonka", "stark"];
const projects = ["shop", "api", "worker", "site", "crm", "billing", "reports", "warehouse", "agent", "docs"];
let fleetMemo: PostgresServers | undefined;
function fleet(): PostgresServers {
  if (fleetMemo) return fleetMemo;
  const seed = postgresServers as unknown as PostgresServers;
  const servers: PostgresServer[] = [...seed.servers];
  for (let i = 4; i <= 23; i++) {
    const r = seeded(i * 7919);
    const full = r() < 0.75;
    const tenants = full ? 200 : 120 + Math.floor(r() * 70);
    const sizeBytes = (r() < 0.5 ? 50 : 100) * (1 << 30) * 1.074;
    const used = Math.floor(sizeBytes * (0.2 + r() * 0.65));
    const p95 = Math.floor(r() * 320);
    const degraded = i === 17;
    const dumpsFailed = i % 9 === 0 ? 1 + Math.floor(r() * 3) : 0;
    servers.push({
      name: `pg-shared-${i}`, version: "17", ready: degraded ? 1 : 2, instances: 2, phase: degraded ? "Failing over" : "Cluster in healthy state",
      tenants, limit: 200, open: !full && !degraded,
      disk: { usedBytes: used, sizeBytes: Math.floor(sizeBytes) },
      connections: { p95, max: 400 }, pooler: p95 >= 240 ? { on: true, since: "2026-10-0" + (1 + (i % 9)) + "T10:00:00Z", ready: 2 } : { on: false },
      archiving: "ok", lastBaseBackup: "2026-10-15T02:00:00Z",
      lastDumps: { at: `2026-10-15T03:${String(7 + ((i * 7) % 53)).padStart(2, "0")}:00Z`, dumped: tenants - dumpsFailed, failed: dumpsFailed, took: `${40 + Math.floor(r() * 30)}s` },
      kept: Math.floor(r() * 4), converting: r() < 0.2 ? 1 : 0, createdAt: `2026-0${1 + (i % 9)}-1${i % 9}T12:00:00Z`,
    });
  }
  const attention = { paused: 0, over: 0, near: 0, degraded: servers.filter((s) => !/healthy/i.test(s.phase)).length, dumpsFailed: servers.filter((s) => (s.lastDumps?.failed ?? 0) > 0).length };
  for (const s of servers) for (const t of tenantsOf(s.name)) if (t.state) attention[t.state]++;
  fleetMemo = { servers, dedicated: seed.dedicated, attention, measuredAt: seed.measuredAt };
  return fleetMemo;
}
const tenantsMemo = new Map<string, PostgresTenant[]>();
function tenantsOf(server: string): PostgresTenant[] {
  const kept = tenantsMemo.get(server);
  if (kept) return kept;
  const seed = postgresServers as unknown as PostgresServers;
  const s = [...seed.servers, ...(fleetMemo?.servers ?? [])].find((x) => x.name === server);
  const n = Number(server.replace(/\D/g, "")) || 0;
  const named = (postgresTenants as { tenants: PostgresTenant[] }).tenants.filter((t) => t.server === server);
  const r = seeded(n * 104729);
  const made: PostgresTenant[] = [];
  const count = s?.tenants ?? (n > 3 ? 200 : 0);
  for (let i = named.length; i < count; i++) {
    const used = Math.floor(r() * (r() < 0.08 ? 1700 : 900) * 1_000_000);
    const pct = used / (1 << 30);
    made.push({ workspace: workspaces[Math.floor(r() * workspaces.length)], project: projects[Math.floor(r() * projects.length)], name: i % 3 ? "db" : "main", database: `db_${((n * 2654435761 + i * 40503) >>> 0).toString(16).padStart(8, "0").slice(-8)}`, server, phase: "Ready", capBytes: 1 << 30, usedBytes: used, state: pct >= 1.5 ? "paused" : pct >= 1 ? "over" : pct >= 0.8 ? "near" : "", keep: r() < 0.1 ? 14 : r() < 0.05 ? 30 : 7, lastDump: "2026-10-15T03:07:00Z" });
  }
  const all = [...named, ...made];
  tenantsMemo.set(server, all);
  return all;
}

// Everything the console reads, in one kept object, changed in place.
type Things = {
  usage: { total: NodeUsage; nodes: NodeUsage[] };
  helm: HelmRelease[];
  registry: RegistryInfo;
  storage: ObjectStorageSummary;
  backups: BackupInfo;
  license: LicenseStatus;
  costDrains: CostDrain[];
  oci: OCIStatus;
  workspaceSettings: WorkspaceSettings;
  mail: MailStatus;
  sizes: SizeCatalog;
  consoleUsers: ConsoleUser[];
  users: LocalUser[];
  methods: { console: LoginMethods; platform: LoginMethods };
  workspaces: WorkspaceSummary[];
  links: Link[];
  settings: ClusterSettings;
};

// The shape of the files changes with the application: what a browser
// kept from an older shape is left behind under the older name.
const shape = "2";
const seed = things as unknown as Things;
const kept = single<Things>(`console-things${shape}`, seed);
const thingsOf = {
  async get(): Promise<Things> {
    const all = await kept.get();
    for (const key of Object.keys(seed) as (keyof Things)[]) {
      if (all[key] === undefined) (all as Record<string, unknown>)[key] = structuredClone(seed[key]);
    }
    return all;
  },
  set: (all: Things) => kept.set(all),
};

const now = () => new Date().toISOString();
const id = () => Math.random().toString(36).slice(2, 10);

export const mock: Api = {
  ...overviewMock,
  archiveProjects: async () => {
    const projects = await archiveProjectsOf.list();
    const placements = await placementOf.list();
    return projects.map((project) => { const placement = placements.find((item) => item.id === project.id); return placement ? { ...project, nodes: [...new Set(placement.groups.flatMap((group) => group.nodes))] } : project; });
  },
  ...archiveActions,
  projectPlacement: placementOf.find,
  measureProjectPlacement: async (id, key) => {
    await wait();
    const project = await placementOf.find(id);
    const group = project.groups.find((group) => group.id === key);
    if (!group) throw new ApiError(404, "Placement group not found.");
    return { group: key, diskUsedBytes: group.diskUsedBytes ?? 0, measuredAt: new Date().toISOString() };
  },
  deleteRetainedVolume: async (id, volume) => {
    await wait();
    const project = await placementOf.find(id);
    project.retainedVolumes = project.retainedVolumes?.filter((item) => item.name !== volume);
    await placementOf.set(project);
  },
  moveProject: async (id, body) => {
    const project = await placementOf.find(id);
    const group = project.groups.find((group) => group.id === body.group);
    const node = project.nodes.find((node) => node.name === body.node);
    if (!group || !node || !node.eligible || (group.pool && group.pool !== node.pool)) throw new ApiError(409, "Choose an eligible node in this group's pool.");
    if (group.needsMigration && !body.migrateToLocal) throw new ApiError(409, "Confirm migration to local storage.");
    await archiveActions.simulateMove(id);
    if (group.needsMigration) {
      project.retainedVolumes ??= [];
      for (const claim of group.database ? [group.database] : group.volumes) project.retainedVolumes.push({ name: `old-${claim}-${Date.now()}`, claim, storageClass: group.storageClasses?.[0] ?? "provider", capacity: "50Gi", retainedAt: now() });
    }
    for (const source of project.nodes.filter((n) => group.nodes.includes(n.name))) {
      source.cpuRequestedMillicores = Math.max(0, source.cpuRequestedMillicores - (group.cpuRequestedMillicores ?? 0));
      source.memoryRequestedBytes = Math.max(0, source.memoryRequestedBytes - (group.memoryRequestedBytes ?? 0));
      if (!group.needsMigration && source.diskAvailableBytes != null) source.diskAvailableBytes += group.diskUsedBytes ?? 0;
    }
    node.cpuRequestedMillicores += group.cpuRequestedMillicores ?? 0;
    node.memoryRequestedBytes += group.memoryRequestedBytes ?? 0;
    if (node.diskAvailableBytes != null) node.diskAvailableBytes -= group.diskUsedBytes ?? 0;
    group.nodes = [node.name];
    group.needsMigration = false;
    group.storageClasses = ["shpyrd-local"];
    await placementOf.set(project);
    const summary = await archiveProjectsOf.find(id);
    summary.nodes = [...new Set(project.groups.flatMap((group) => group.nodes))];
    await archiveProjectsOf.set(summary);
  },
  config: async () => {
    await wait();
    const all = await thingsOf.get();
    const c = config as PublicConfig;
    return { ...c, defaultWorkspaceId: all.settings.defaultWorkspaceId, auth: { ...c.auth, password: all.settings.consolePasswordSignIn ? c.auth.password : undefined, providers: all.methods.console.connectors.map((x) => ({ id: x.id, label: x.name, kind: x.type, realm: "console" })) } };
  },
  me: async () => {
    await wait();
    return me as Identity;
  },
  tokenLogin: async ({ token, next }) => {
    await wait();
    if (!token) throw new ApiError(401, "the token is not right");
    return { next: next ?? "/" };
  },
  logout: async () => {
    await wait();
    return { redirect: "/" };
  },

  workspaces: async () => (await thingsOf.get()).workspaces,
  links: async () => (await thingsOf.get()).links,
  patchSettings: async (body) => {
    const all = await thingsOf.get();
    all.settings = { ...all.settings, ...body };
    await thingsOf.set(all);
    return all.settings;
  },

  clusterScheduling: async () => (cluster as ClusterSummary).scheduling ?? { pending: [] },
  cluster: async () => {
    await wait();
    return cluster as ClusterSummary;
  },
  clusterMetrics: async (range) => {
    await wait();
    const all = await thingsOf.get();
    const nodes = all.usage.nodes;
    return {
      range,
      total: all.usage.total,
      nodes,
      cpu: byNode(nodes.map((n) => ({ name: n.name, base: n.cpuUsedPct })), range, 3),
      memory: byNode(nodes.map((n) => ({ name: n.name, base: n.memoryUsedPct })), range, 11),
    };
  },
  helmReleases: async () => (await thingsOf.get()).helm,
  registry: async () => (await thingsOf.get()).registry,
  registryGC: async () => {
    const all = await thingsOf.get();
    all.registry.gc = { ...all.registry.gc!, running: true, startedAt: now() };
    await thingsOf.set(all);
    setTimeout(async () => {
      const all2 = await thingsOf.get();
      all2.registry.gc = { ...all2.registry.gc!, running: false, lastRun: now(), lastResult: "ok", lastDuration: "2m48s", reclaimedBytes: 3221225472 };
      await thingsOf.set(all2);
    }, 8000);
    return { status: "started" };
  },
  objectStorage: async () => (await thingsOf.get()).storage,
  postgresServers: async () => {
    await wait();
    return fleet();
  },
  postgresTenants: async (server, query) => {
    await wait();
    const size = query.size ?? 50;
    const page = Math.max(1, query.page ?? 1);
    const q = (query.q ?? "").trim().toLowerCase();
    const all = tenantsOf(server).filter((t) => (!query.state || t.state === query.state) && (!q || `${t.workspace} ${t.project} ${t.name} ${t.database}`.toLowerCase().includes(q)));
    return { items: all.slice((page - 1) * size, page * size), total: all.length, page, size };
  },
  findPostgresTenants: async (q) => {
    await wait();
    const needle = q.trim().toLowerCase();
    if (needle.length < 2) return [];
    const found: PostgresTenant[] = [];
    for (const s of fleet().servers) {
      for (const t of tenantsOf(s.name)) {
        if (`${t.workspace} ${t.project} ${t.name} ${t.database}`.toLowerCase().includes(needle)) found.push(t);
        if (found.length >= 50) return found;
      }
    }
    return found;
  },
  backups: async () => (await thingsOf.get()).backups,
  runBackup: async () => {
    const all = await thingsOf.get();
    const name = `backup-${now().slice(0, 10).replace(/-/g, "")}-${id().slice(0, 4)}`;
    all.backups.runs.unshift({ name, status: "running", started: now() });
    await thingsOf.set(all);
    setTimeout(async () => {
      const all2 = await thingsOf.get();
      const run = all2.backups.runs.find((r) => r.name === name);
      if (run) {
        run.status = "succeeded";
        run.finished = now();
      }
      all2.backups.lastSuccessful = now();
      all2.backups.archives.unshift({ name: `shpyrd-${now().replace(/[-:]/g, "").slice(0, 15)}Z.tar.age`, size: 1510000000, modified: now() });
      await thingsOf.set(all2);
    }, 8000);
    return { job: name, status: "running" };
  },
  license: async () => (await thingsOf.get()).license,
  renewLicense: async () => {
    const all = await thingsOf.get();
    const l = all.license.license;
    if (!l?.issuer) throw new ApiError(502, "this license was issued by hand: it renews offline, with a new one (shpyrd-ctl license set)");
    const next = new Date(Date.parse(l.expiresAt) + 30 * 86400e3).toISOString();
    all.license = { ...all.license, active: true, license: { ...l, id: `lic_${id().slice(0, 22)}`, issuedAt: now(), expiresAt: next }, renewal: { at: now() } };
    await thingsOf.set(all);
    return all.license;
  },
  billingLink: async () => {
    const l = (await thingsOf.get()).license.license;
    if (!l?.issuer) throw new ApiError(502, "this license was issued by hand: it names no billing app");
    return { url: `${l.issuer}/c/session?t=mock` };
  },
  costs: async (q) => { await wait(); return exploreMockCosts(q); },
  createCostExport: createMockCostExport,
  costExport: mockCostExport,
  costExportURL: mockCostExportURL,
  costDrains: async () => (await thingsOf.get()).costDrains,
  addCostDrain: async (body) => {
    await wait();
    const all = await thingsOf.get();
    if (!/^[a-z0-9]([-a-z0-9]{0,38}[a-z0-9])?$/.test(body.name)) throw new ApiError(400, "name: lowercase letters, digits and dashes, 40 at most");
    if (all.costDrains.some((d) => d.name === body.name)) throw new ApiError(409, "a cost drain with this name exists");
    const d: CostDrain = { id: id(), name: body.name, url: body.url, headers: Object.keys(body.headers ?? {}).sort(), sent: 0, errors: 0, createdAt: now() };
    all.costDrains.push(d);
    await thingsOf.set(all);
    return d;
  },
  removeCostDrain: async (name) => {
    await wait();
    const all = await thingsOf.get();
    all.costDrains = all.costDrains.filter((d) => d.name !== name);
    await thingsOf.set(all);
  },
  ociStatus: async () => (await thingsOf.get()).oci,
  putOCI: async (body) => {
    await wait();
    const all = await thingsOf.get();
    all.oci = { configured: true, tenancy: body.tenancy, region: body.region };
    await thingsOf.set(all);
    return all.oci;
  },
  deleteOCI: async () => {
    const all = await thingsOf.get();
    all.oci = { configured: false };
    await thingsOf.set(all);
  },
  workspaceSettings: async () => (await thingsOf.get()).workspaceSettings,
  putWorkspaceSettings: async (body) => {
    await wait();
    const all = await thingsOf.get();
    all.workspaceSettings = { ...all.workspaceSettings, limits: body.limits, sleep: body.sleep };
    await thingsOf.set(all);
    return all.workspaceSettings;
  },
  mailStatus: async () => (await thingsOf.get()).mail,
  mailTest: async (to) => {
    await wait(900);
    if (!to.includes("@")) throw new ApiError(400, "that is not an address");
    return { ok: true, to, took: "412ms" };
  },
  sizes: async () => (await thingsOf.get()).sizes,
  saveSizes: async (catalog) => {
    const all = await thingsOf.get();
    all.sizes = catalog;
    await thingsOf.set(all);
    return catalog;
  },

  consoleUsers: async () => (await thingsOf.get()).consoleUsers,
  consolePrincipals: async () => [],
  addConsoleUser: async ({ principalId }) => {
 const email = principalId, password = undefined;
    await wait();
    const all = await thingsOf.get();
    const address = email.trim().toLowerCase();
    if (!/^\S+@\S+\.\S+$/.test(address)) throw new ApiError(400, "a console user is an email");
    const account = all.users.find((u) => u.email === address);
    if (password && account) throw new ApiError(409, `${address} already has an account: leave the password out, or change it under Accounts`);
    if (password) all.users.push({ email: address, createdAt: now() });
    let user = all.consoleUsers.find((u) => u.email === address);
    if (!user) {
      user = { email: address, addedAt: now(), addedBy: (me as Identity).email, account: account || password ? "active" : "" };
      all.consoleUsers.push(user);
      all.consoleUsers.sort((a, b) => a.email.localeCompare(b.email));
    }
    await thingsOf.set(all);
    return user;
  },
  removeConsoleUser: async (email) => {
    await wait();
    const all = await thingsOf.get();
    if (email === (me as Identity).email) throw new ApiError(400, "you cannot take yourself off the console");
    if (!all.consoleUsers.some((u) => u.email === email)) throw new ApiError(404, `${email} is not a console user`);
    all.consoleUsers = all.consoleUsers.filter((u) => u.email !== email);
    await thingsOf.set(all);
  },
  users: async () => (await thingsOf.get()).users,
  createUser: async (body) => {
    const all = await thingsOf.get();
    if (all.users.some((u) => u.email === body.email)) throw new ApiError(409, `${body.email} has an account`);
    const user: LocalUser = { email: body.email, name: body.name, createdAt: now() };
    all.users.push(user);
    await thingsOf.set(all);
    return user;
  },
  setUserPassword: async (email) => {
    const all = await thingsOf.get();
    if (!all.users.some((u) => u.email === email)) throw new ApiError(404, "no such account");
  },
  removeUser: async (email) => {
    const all = await thingsOf.get();
    all.users = all.users.filter((u) => u.email !== email);
    await thingsOf.set(all);
  },

  loginMethods: async (scope) => (await thingsOf.get()).methods[scope],
  addLoginMethod: async (scope, body) => {
    const all = await thingsOf.get();
    const cid = body.id || `${body.type}-${id().slice(0, 4)}`;
    all.methods[scope].connectors.push({ id: cid, type: body.type, name: body.name || body.type, detail: body.hostedDomain || body.org || body.tenant || body.issuer });
    await thingsOf.set(all);
    return { id: cid };
  },
  removeLoginMethod: async (scope, methodId) => {
    const all = await thingsOf.get();
    all.methods[scope].connectors = all.methods[scope].connectors.filter((c) => c.id !== methodId);
    await thingsOf.set(all);
  },
};
