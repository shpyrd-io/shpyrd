import { ApiError } from "@shpyrd/shared/api/error";
import { single, wait } from "@shpyrd/shared/api/mock-store";
import type { Api } from "./api";
import type {
  BackupInfo,
  ClusterSettings,
  ClusterSummary,
  Drain,
  Economics,
  Globals,
  HelmRelease,
  Identity,
  LocalUser,
  LoginMethods,
  MailStatus,
  NodeUsage,
  ObjectStorageSummary,
  Plan,
  PublicConfig,
  RegistryInfo,
  SizeCatalog,
  WorkspaceSummary,
} from "./types";
import { byNode } from "@/lib/samples";
import cluster from "../../mock/cluster.json";
import config from "../../mock/config.json";
import me from "../../mock/me.json";
import things from "../../mock/things.json";

// Everything the console reads, in one kept object, changed in place.
type Things = {
  usage: { total: NodeUsage; nodes: NodeUsage[] };
  helm: HelmRelease[];
  registry: RegistryInfo;
  storage: ObjectStorageSummary;
  backups: BackupInfo;
  mail: MailStatus;
  economics: Economics;
  sizes: SizeCatalog;
  globals: Globals;
  drains: Drain[];
  users: LocalUser[];
  methods: { console: LoginMethods; platform: LoginMethods };
  workspaces: WorkspaceSummary[];
  plans: Plan[];
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
  createWorkspace: async (body) => {
    const all = await thingsOf.get();
    if (!/^[a-z0-9-]{2,}$/.test(body.slug)) throw new ApiError(400, "the slug has lowercase letters, digits and dashes");
    if (all.workspaces.some((w) => w.slug === body.slug)) throw new ApiError(409, `a workspace named ${body.slug} exists`);
    const made: WorkspaceSummary = {
      slug: body.slug,
      name: body.name || body.slug,
      address: body.address || `${body.slug}.shpyrd.app`,
      url: `https://${body.address || `${body.slug}.shpyrd.app`}`,
      status: "active",
      owner: body.operatorOwned ? "operator" : "customer",
      plan: body.operatorOwned ? undefined : body.plan,
      owners: body.operatorOwned ? (me as Identity).email ? [(me as Identity).email!] : [] : body.owner ? [body.owner] : [],
      createdAt: now(),
    };
    all.workspaces.push(made);
    await thingsOf.set(all);
    if (body.operatorOwned) return made;
    // Mail is set up in the Mock: the first owner was emailed.
    return { ...made, ownerInvitation: { applied: false, emailed: all.mail.configured, link: all.mail.configured ? undefined : `${made.url}/invite/${id()}`, expiresAt: new Date(Date.now() + 7 * 86400e3).toISOString() } };
  },
  plans: async () => (await thingsOf.get()).plans,
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
  mailStatus: async () => (await thingsOf.get()).mail,
  mailTest: async (to) => {
    await wait(900);
    if (!to.includes("@")) throw new ApiError(400, "that is not an address");
    return { ok: true, to, took: "412ms" };
  },
  economics: async (month) => {
    await wait();
    const e = (await thingsOf.get()).economics;
    return month ? { ...e, month } : e;
  },
  sizes: async () => (await thingsOf.get()).sizes,
  saveSizes: async (catalog) => {
    const all = await thingsOf.get();
    all.sizes = catalog;
    await thingsOf.set(all);
    return catalog;
  },
  globals: async () => (await thingsOf.get()).globals,
  changeGlobals: async (change) => {
    const all = await thingsOf.get();
    for (const name of Object.keys(change.set ?? {})) {
      const v = all.globals.vars.find((x) => x.name === name);
      if (v) v.updatedAt = now();
      else all.globals.vars.push({ name, updatedAt: now() });
    }
    for (const name of change.unset ?? []) all.globals.vars = all.globals.vars.filter((v) => v.name !== name);
    await thingsOf.set(all);
    return all.globals;
  },
  drains: async () => (await thingsOf.get()).drains,
  addDrain: async (body) => {
    const all = await thingsOf.get();
    const name = body.name || new URL(body.url.replace(/^syslog(\+tls)?:/, "https:")).hostname.split(".")[0] || "drain";
    const drain: Drain = { name, url: body.url, format: body.format ?? (body.url.startsWith("syslog") ? "syslog" : "json"), headers: Object.keys(body.headers ?? {}), cluster: true, phase: "Pending", sent: 0, errors: 0, createdAt: now() };
    all.drains.push(drain);
    await thingsOf.set(all);
    return drain;
  },
  removeDrain: async (name) => {
    const all = await thingsOf.get();
    all.drains = all.drains.filter((d) => d.name !== name);
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
