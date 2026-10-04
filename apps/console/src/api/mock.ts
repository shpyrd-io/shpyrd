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
  CostSummary,
  HelmRelease,
  Identity,
  LicenseStatus,
  LocalUser,
  LoginMethods,
  MailStatus,
  NodeUsage,
  OCIStatus,
  ObjectStorageSummary,
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

const placementOf = collection<ProjectPlacement & { id: string }>("console-project-placement-v3", placement, (item) => item.id);
const archiveActions = mockProjectArchives("console");

const archiveProjectsOf = collection<ArchiveProject>("console-archive-projects", archiveProjects, (project) => project.id);

// Everything the console reads, in one kept object, changed in place.
type Things = {
  usage: { total: NodeUsage; nodes: NodeUsage[] };
  helm: HelmRelease[];
  registry: RegistryInfo;
  storage: ObjectStorageSummary;
  backups: BackupInfo;
  license: LicenseStatus;
  costs: Record<string, CostSummary>;
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
const shape = "1";
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
  passwordLogin: async ({ email, password, next }) => {
    await wait();
    if (!email || !password) throw new ApiError(401, "the email or the password is not right");
    return { next: next ?? "/" };
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
  costs: async (q) => {
    await wait();
    const all = await thingsOf.get();
    const kind = q.kind ?? "estimated";
    const group = q.group ?? "project";
    const found = all.costs[`${kind}/${group}`] ?? { ...all.costs["estimated/project"], rows: [], total: {} };
    return { ...found, kind, group };
  },
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
  addConsoleUser: async ({ email, password }) => {
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
