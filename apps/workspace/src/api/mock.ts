import { ApiError } from "@shpyrd/shared/api/error";
import { collection, single, wait } from "@shpyrd/shared/api/mock-store";
import type { Api } from "./api";
import type {
  AllowEntry,
  APIToken,
  AuditEntry,
  Billing,
  BuildInfo,
  ConfigVar,
  Connection,
  DomainClaim,
  DomainStatus,
  Drain,
  Identity,
  Invitation,
  LoginMethods,
  Member,
  Person,
  Project,
  ProjectSummary,
  PublicConfig,
  ResourceInfo,
  SizeCatalog,
  SnapshotInfo,
  Team,
  VolumeInfo,
  WorkspaceDomain,
  WorkspaceInfo,
} from "./types";
import { buildOutput, liveLine, logsOf, metricsOf } from "@/lib/samples";
import audit from "../../mock/audit.json";
import config from "../../mock/config.json";
import me from "../../mock/me.json";
import projects from "../../mock/projects.json";
import things from "../../mock/things.json";
import workspace from "../../mock/workspace.json";

// Everything of a project beyond itself, and everything of the workspace
// beyond itself: one kept object, changed in place.
type ProjectThings = {
  configVars: ConfigVar[];
  builds: BuildInfo[];
  resources: ResourceInfo[];
  volumes: VolumeInfo[];
  snapshots: SnapshotInfo[];
  domains: { target: string; domains: DomainStatus[] };
  drains: Drain[];
  members: Member[];
  allow: AllowEntry[];
};
type Things = {
  projects: Record<string, ProjectThings>;
  people: Person[];
  connections: Connection[];
  invitations: Invitation[];
  teams: Team[];
  tokens: APIToken[];
  workspaceDomains: WorkspaceDomain[];
  loginMethods: LoginMethods;
  domainClaims: DomainClaim[];
  sizes: SizeCatalog;
  billing: Billing;
};

// The shape of the files changes with the application: what a browser
// kept from an older shape is left behind under the older name.
const shape = "2";
const projectsOf = collection<Project>(`projects${shape}`, projects as Project[], (p) => p.slug);
const workspaceOf = single<WorkspaceInfo>(`workspace${shape}`, workspace as WorkspaceInfo);
const seed = things as unknown as Things;
const kept = single<Things>(`things${shape}`, seed);
// What was kept from before this version of the files may lack what
// was added since, or hold an older shape: the seed fills it in.
const thingsOf = {
  async get(): Promise<Things> {
    const all = await kept.get();
    for (const key of Object.keys(seed) as (keyof Things)[]) {
      if (all[key] === undefined || Array.isArray(all[key]) !== Array.isArray(seed[key])) (all as Record<string, unknown>)[key] = structuredClone(seed[key]);
    }
    return all;
  },
  set: (all: Things) => kept.set(all),
};

const empty = (): ProjectThings => ({ configVars: [], builds: [], resources: [], volumes: [], snapshots: [], domains: { target: "acme.shpyrd.app", domains: [] }, drains: [], members: [], allow: [] });

async function ofProject(slug: string): Promise<[Things, ProjectThings]> {
  const all = await thingsOf.get();
  all.projects[slug] ??= empty();
  const p = all.projects[slug];
  p.volumes ??= [];
  p.snapshots ??= [];
  return [all, p];
}

const summary = ({ spec: _spec, status: _status, ...rest }: Project): ProjectSummary => rest;
const now = () => new Date().toISOString();
const id = () => Math.random().toString(36).slice(2, 10);
const who = () => (me as Identity).email;

// A release goes out: a number more, and the instances follow.
function release(p: Project, kind: Project["status"]["releases"][number]["kind"], description: string, build?: string | number) {
  const number = Math.max(0, ...p.status.releases.map((r) => r.number)) + 1;
  p.status.releases.push({ number, kind, description, build, createdAt: now() });
  p.release = number;
}

// A stream that waits for an abort: lines come every so often until then.
function until(signal: AbortSignal, every: number, tick: () => void): Promise<void> {
  return new Promise((done) => {
    const timer = setInterval(tick, every);
    signal.addEventListener("abort", () => {
      clearInterval(timer);
      done();
    });
  });
}

export const mock: Api = {
  config: async () => {
    await wait();
    const ws = await workspaceOf.get();
    return { ...(config as PublicConfig), workspace: { ...(config as PublicConfig).workspace!, name: ws.name, branding: ws.branding } };
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
  invitation: async (token) => {
    await wait();
    const all = await thingsOf.get();
    const ws = await workspaceOf.get();
    const i = all.invitations.find((x) => x.id === token);
    if (!i) throw new ApiError(404, "this invitation was used, withdrawn, or never was");
    return { workspace: { slug: ws.slug, name: ws.name, address: ws.address }, email: i.email, role: i.role, team: i.team, invitedBy: i.invitedBy, expiresAt: i.expiresAt, expired: i.expired, url: `/invite/${token}` };
  },
  acceptInvitation: async (token) => {
    const all = await thingsOf.get();
    const i = all.invitations.find((x) => x.id === token);
    if (!i) throw new ApiError(404, "this invitation was used, withdrawn, or never was");
    all.invitations = all.invitations.filter((x) => x.id !== token);
    await thingsOf.set(all);
    return { workspace: (await workspaceOf.get()).slug, role: i.role, next: "/" };
  },

  workspace: () => workspaceOf.get(),
  updateWorkspace: async (body) => {
    const current = await workspaceOf.get();
    const { logo, color, ...rest } = body;
    const branding = { ...current.branding };
    if (logo !== undefined) branding.logoUrl = logo || undefined;
    if (color !== undefined) branding.color = color || undefined;
    return workspaceOf.set({ ...current, ...rest, branding, updatedAt: now() });
  },
  people: async () => (await thingsOf.get()).people,
  setPersonRole: async (email, role) => {
    const all = await thingsOf.get();
    const p = all.people.find((x) => x.email === email);
    if (!p) throw new ApiError(404, `${email} is not here`);
    p.role = role;
    await thingsOf.set(all);
  },
  setPersonStatus: async (email, status) => {
    const all = await thingsOf.get();
    const person = all.people.find((p) => p.email === email);
    if (!person) throw new ApiError(404, "no such person");
    person.status = status;
    await thingsOf.set(all);
  },
  removePerson: async (email) => {
    const all = await thingsOf.get();
    all.people = all.people.filter((x) => x.email !== email);
    await thingsOf.set(all);
  },
  connections: async () => (await thingsOf.get()).connections,
  revokeConnection: async (connectionId) => {
    const all = await thingsOf.get();
    all.connections = all.connections.filter((c) => c.id !== connectionId);
    await thingsOf.set(all);
  },
  invitations: async () => (await thingsOf.get()).invitations,
  invite: async (body) => {
    const all = await thingsOf.get();
    const known = all.people.find((p) => p.email === body.email);
    if (known) {
      known.role = body.role;
      await thingsOf.set(all);
      return { email: body.email, role: body.role, team: body.team, applied: true, emailed: false };
    }
    const invitation: Invitation = { id: id(), ...body, invitedBy: who(), createdAt: now(), expiresAt: new Date(Date.now() + 7 * 86400e3).toISOString(), expired: false };
    all.invitations.push(invitation);
    await thingsOf.set(all);
    return { email: body.email, role: body.role, team: body.team, applied: false, invitation, link: `${location.origin}/invite/${invitation.id}`, emailed: false, mailError: "no mail is set up in the Mock" };
  },
  revokeInvitation: async (invitationId) => {
    const all = await thingsOf.get();
    all.invitations = all.invitations.filter((i) => i.id !== invitationId);
    await thingsOf.set(all);
  },
  teams: async () => (await thingsOf.get()).teams,
  saveTeam: async (team) => {
    const all = await thingsOf.get();
    const at = all.teams.findIndex((t) => t.name === team.name);
    if (at >= 0) all.teams[at] = team;
    else all.teams.push(team);
    await thingsOf.set(all);
    return team;
  },
  removeTeam: async (name) => {
    const all = await thingsOf.get();
    all.teams = all.teams.filter((t) => t.name !== name);
    await thingsOf.set(all);
  },
  tokens: async () => (await thingsOf.get()).tokens,
  createToken: async (body) => {
    const all = await thingsOf.get();
    const days = body.expiresIn ? Number(body.expiresIn.replace(/d$/, "")) : 0;
    const token: APIToken = { id: id(), name: body.name, ownerEmail: who(), platformRole: body.platformRole, projectRoles: body.projectRoles, createdAt: now(), expiresAt: days ? new Date(Date.now() + days * 86400e3).toISOString() : undefined };
    all.tokens.push(token);
    await thingsOf.set(all);
    return { token, secret: `shp_${id()}${id()}${id()}` };
  },
  revokeToken: async (tokenId) => {
    const all = await thingsOf.get();
    all.tokens = all.tokens.filter((t) => t.id !== tokenId);
    await thingsOf.set(all);
  },
  workspaceDomains: async () => (await thingsOf.get()).workspaceDomains,
  addWorkspaceDomain: async (host) => {
    const all = await thingsOf.get();
    const domain: WorkspaceDomain = { host, verified: false, primary: false, records: [{ type: "CNAME", name: host, value: "acme.shpyrd.app" }, { type: "TXT", name: `_shpyrd.${host}`, value: `shpyrd-verify=${id()}` }], url: `https://${host}` };
    all.workspaceDomains.push(domain);
    await thingsOf.set(all);
    return domain;
  },
  verifyWorkspaceDomain: async (host) => {
    const all = await thingsOf.get();
    const d = all.workspaceDomains.find((x) => x.host === host);
    if (!d) throw new ApiError(404, "no such domain");
    d.verified = true;
    d.verifiedAt = now();
    await thingsOf.set(all);
    return d;
  },
  setWorkspaceDomainPrimary: async (host) => {
    const all = await thingsOf.get();
    const d = all.workspaceDomains.find((x) => x.host === host);
    if (!d) throw new ApiError(404, "no such domain");
    if (!d.verified) throw new ApiError(400, "verify the domain first");
    for (const x of all.workspaceDomains) x.primary = x.host === host;
    await thingsOf.set(all);
    return d;
  },
  removeWorkspaceDomain: async (host) => {
    const all = await thingsOf.get();
    all.workspaceDomains = all.workspaceDomains.filter((d) => d.host !== host);
    await thingsOf.set(all);
  },
  loginMethods: async () => (await thingsOf.get()).loginMethods,
  addLoginMethod: async (body) => {
    const all = await thingsOf.get();
    const cid = body.id || `${body.type}-${id().slice(0, 4)}`;
    all.loginMethods.connectors.push({ id: cid, type: body.type, name: body.name || body.type, detail: body.hostedDomain || body.org || body.tenant || body.issuer, workspace: "acme" });
    await thingsOf.set(all);
    return { id: cid };
  },
  removeLoginMethod: async (methodId) => {
    const all = await thingsOf.get();
    all.loginMethods.connectors = all.loginMethods.connectors.filter((c) => c.id !== methodId);
    await thingsOf.set(all);
  },
  domainClaims: async () => (await thingsOf.get()).domainClaims,
  claimDomain: async (domain, connector) => {
    const all = await thingsOf.get();
    const claim: DomainClaim = { domain, connector: connector || undefined, verified: false, record: `_shpyrd.${domain}`, recordValue: `shpyrd-verify=${id()}` };
    all.domainClaims.push(claim);
    await thingsOf.set(all);
    return claim;
  },
  verifyDomainClaim: async (domain) => {
    const all = await thingsOf.get();
    const c = all.domainClaims.find((x) => x.domain === domain);
    if (!c) throw new ApiError(404, "no such claim");
    c.verified = true;
    c.verifiedAt = now();
    await thingsOf.set(all);
    return c;
  },
  unclaimDomain: async (domain) => {
    const all = await thingsOf.get();
    all.domainClaims = all.domainClaims.filter((c) => c.domain !== domain);
    await thingsOf.set(all);
  },
  billing: async (month) => {
    const billing = (await thingsOf.get()).billing;
    // The Mock has one month of usage, and shows it for any month asked.
    return month && month !== billing.month ? { ...billing, month, past: true, projection: undefined } : billing;
  },
  sizes: async () => (await thingsOf.get()).sizes,

  projects: async () => (await projectsOf.list()).map(summary),
  project: (slug) => projectsOf.find(slug),
  createProject: async (body) => {
    const all = await projectsOf.list();
    if (!/^[a-z0-9-]{2,}$/.test(body.slug)) throw new ApiError(400, "the slug has lowercase letters, digits and dashes");
    if (all.some((p) => p.slug === body.slug)) throw new ApiError(409, `a project named ${body.slug} exists`);
    const created: Project = { slug: body.slug, displayName: body.displayName || body.slug, description: body.description, namespace: `p-${body.slug}`, phase: "Pending", release: 0, access: "public", exposure: "external", createdAt: now(), spec: { source: body.git ? { git: body.git, subPath: body.subPath } : undefined }, status: { phase: "Pending", releases: [] } };
    await projectsOf.set(created);
    return summary(created);
  },
  updateProject: async (slug, body) => {
    const p = await projectsOf.find(slug);
    if (body.name !== undefined) p.displayName = body.name;
    if (body.description !== undefined) p.description = body.description || undefined;
    if (body.featured !== undefined) p.featured = body.featured;
    return projectsOf.set(p);
  },
  destroyProject: (slug) => projectsOf.remove(slug),
  deploy: async (slug, body) => {
    const [all, t] = await ofProject(slug);
    const p = await projectsOf.find(slug);
    p.spec.source = { git: body.git, subPath: body.subPath };
    p.spec.build = { strategy: body.strategy, dockerfile: body.dockerfile };
    const number = Math.max(0, ...t.builds.map((b) => b.number)) + 1;
    t.builds.unshift({ name: `${slug}-${number}`, number, strategy: body.strategy ?? "buildpacks", status: "Building", source: body.git?.revision ?? "main", startedAt: now() });
    p.phase = p.status.phase = "Building";
    p.status.latestBuild = String(number);
    p.status.message = `building #${number}`;
    await thingsOf.set(all);
    await projectsOf.set(p);
    // The build ends by itself, and the release goes out.
    setTimeout(async () => {
      const [all2, t2] = await ofProject(slug);
      const b = t2.builds.find((x) => x.number === number);
      if (b) {
        b.status = "Succeeded";
        b.completedAt = now();
        b.digest = `sha256:${id()}${id()}`;
      }
      await thingsOf.set(all2);
      const p2 = await projectsOf.find(slug);
      release(p2, "deploy", `Deploy of ${body.git?.revision ?? "main"}`, number);
      p2.phase = p2.status.phase = "Running";
      p2.status.message = undefined;
      await projectsOf.set(p2);
    }, 12_000);
    return summary(p);
  },
  redeploy: async (slug, action) => {
    await wait();
    const p = await projectsOf.find(slug);
    if (action === "restart") return { action: "restart", message: "Running the release command again" };
    if (p.phase === "Failed" && p.status.message?.startsWith("build failed")) return { action: "rebuild", message: "Building the same source again" };
    return { action: "redeploy", message: "Starting new instances of the current release" };
  },
  rollback: async (slug, to) => {
    const p = await projectsOf.find(slug);
    const target = p.status.releases.find((r) => r.number === to);
    if (!target) throw new ApiError(404, `v${to} is not a release of ${slug}`);
    release(p, "rollback", `Back to v${to}`, target.build);
    await projectsOf.set(p);
  },
  applyProcesses: async (slug, changes) => {
    const p = await projectsOf.find(slug);
    for (const [name, change] of Object.entries(changes)) {
      const spec = { ...p.spec.processes?.[name], ...(change.size ? { size: change.size } : {}), ...(change.replicas !== undefined ? { replicas: change.replicas } : {}) };
      p.spec.processes = { ...p.spec.processes, [name]: spec };
      const st = p.processes?.[name] ?? { desired: 1, ready: 1 };
      p.processes = { ...p.processes, [name]: { ...st, desired: spec.replicas ?? st.desired, ready: spec.replicas ?? st.ready, size: spec.size ?? st.size } };
    }
    if (Object.values(changes).some((c) => c.size)) release(p, "config", "Sizes changed", p.status.releases.at(-1)?.build);
    return projectsOf.set(p);
  },
  setExposure: async (slug, exposure) => projectsOf.set({ ...(await projectsOf.find(slug)), exposure }),
  setAccess: async (slug, access) => projectsOf.set({ ...(await projectsOf.find(slug)), access }),
  preview: async (slug, body) => {
    await wait();
    const p = await projectsOf.find(slug);
    return { url: `${p.url ?? `https://${slug}.acme.shpyrd.app`}/.shpyrd/preview?as=${body.anonymous ? "anonymous" : body.teams.join(",") || "nobody"}` };
  },
  audit: async (slug) => {
    await wait();
    return (audit as Record<string, AuditEntry[]>)[slug] ?? [];
  },
  builds: async (slug) => (await ofProject(slug))[1].builds,
  streamBuild: async (slug, build, follow, signal, onLine) => {
    await wait();
    const [, t] = await ofProject(slug);
    const b = t.builds.find((x) => x.name === build);
    const lines = b?.status === "Building" ? buildOutput.slice(0, 6) : buildOutput;
    for (const line of lines) onLine(line);
    if (!follow || b?.status !== "Building") return;
    let i = 6;
    await until(signal, 1500, () => {
      if (i < buildOutput.length) onLine(buildOutput[i++]!);
    });
  },
  configVars: async (slug) => {
    const [, t] = await ofProject(slug);
    const p = await projectsOf.find(slug);
    const bound = (p.spec.bindings ?? []).flatMap((b) => (b.kind === "Postgres" ? [{ name: "DATABASE_URL", provider: `${b.kind} ${b.name}` }] : b.kind === "Redis" ? [{ name: "REDIS_URL", provider: `${b.kind} ${b.name}` }] : []));
    return { vars: t.configVars, bound, global: [{ name: "SENTRY_DSN", updatedAt: "2026-08-01T10:00:00Z" }] };
  },
  changeConfigVars: async (slug, change) => {
    const [all, t] = await ofProject(slug);
    const set = { ...change.set };
    for (const line of (change.dotenv ?? "").split("\n")) {
      const at = line.indexOf("=");
      if (at > 0 && !line.trim().startsWith("#")) set[line.slice(0, at).trim()] = line.slice(at + 1).trim();
    }
    for (const name of Object.keys(set)) {
      const v = t.configVars.find((x) => x.name === name);
      if (v) v.updatedAt = now();
      else t.configVars.push({ name, updatedAt: now() });
    }
    for (const name of change.unset ?? []) t.configVars = t.configVars.filter((v) => v.name !== name);
    await thingsOf.set(all);
    const p = await projectsOf.find(slug);
    release(p, "config", change.unset?.length ? `${change.unset.join(", ")} removed` : `${Object.keys(set).join(", ")} set`, p.status.releases.at(-1)?.build);
    await projectsOf.set(p);
  },
  resources: async (slug) => (await ofProject(slug))[1].resources,
  createResource: async (slug, body) => {
    const [all, t] = await ofProject(slug);
    if (t.resources.some((r) => r.kind === body.kind && r.name === body.name)) throw new ApiError(409, `${body.kind} ${body.name} exists`);
    const details = Object.fromEntries(Object.entries(body.spec).filter(([, v]) => typeof v !== "object").map(([k, v]) => [k, String(v)]));
    const r: ResourceInfo = { kind: body.kind, name: body.name, phase: "Pending", details, attachedTo: [], data: true, bindable: true, createdAt: now() };
    t.resources.push(r);
    await thingsOf.set(all);
    setTimeout(async () => {
      const [all2, t2] = await ofProject(slug);
      const x = t2.resources.find((y) => y.kind === body.kind && y.name === body.name);
      if (x) x.phase = "Running";
      await thingsOf.set(all2);
    }, 6000);
    return r;
  },
  removeResource: async (slug, kind, name, force) => {
    const [all, t] = await ofProject(slug);
    const r = t.resources.find((x) => x.kind === kind && x.name === name);
    if (r?.attachedTo.length && !force) throw new ApiError(409, `${kind} ${name} is attached to ${r.attachedTo.join(", ")}`);
    t.resources = t.resources.filter((x) => x.kind !== kind || x.name !== name);
    await thingsOf.set(all);
  },
  attach: async (slug, body) => {
    const [all, t] = await ofProject(slug);
    const r = t.resources.find((x) => x.kind === body.kind && x.name === body.name);
    if (!r) throw new ApiError(404, `no ${body.kind} named ${body.name}`);
    if (!r.attachedTo.includes(slug)) r.attachedTo.push(slug);
    await thingsOf.set(all);
    const p = await projectsOf.find(slug);
    p.spec.bindings = [...(p.spec.bindings ?? []).filter((b) => b.kind !== body.kind || b.name !== body.name), { kind: body.kind, name: body.name }];
    release(p, "config", `${body.kind} ${body.name} attached`, p.status.releases.at(-1)?.build);
    await projectsOf.set(p);
  },
  detach: async (slug, kind, name) => {
    const [all, t] = await ofProject(slug);
    const r = t.resources.find((x) => x.kind === kind && x.name === name);
    if (r) r.attachedTo = r.attachedTo.filter((s) => s !== slug);
    await thingsOf.set(all);
    const p = await projectsOf.find(slug);
    p.spec.bindings = (p.spec.bindings ?? []).filter((b) => b.kind !== kind || b.name !== name);
    release(p, "config", `${kind} ${name} detached`, p.status.releases.at(-1)?.build);
    await projectsOf.set(p);
  },
  volumes: async (slug) => (await ofProject(slug))[1].volumes,
  createVolume: async (slug, body) => {
    const [all, t] = await ofProject(slug);
    if (t.volumes.some((v) => v.name === body.name)) throw new ApiError(409, `a volume named ${body.name} exists`);
    const v: VolumeInfo = { name: body.name, size: body.size, shared: !!body.shared, phase: "Bound", mountedBy: [], createdAt: now() };
    t.volumes.push(v);
    t.resources.push({ kind: "Volume", name: body.name, phase: "Bound", details: { size: body.size, mode: body.shared ? "shared" : "single" }, attachedTo: [], data: true, createdAt: now() });
    await thingsOf.set(all);
    return v;
  },
  resizeVolume: async (slug, name, size) => {
    const [all, t] = await ofProject(slug);
    const v = t.volumes.find((x) => x.name === name);
    if (!v) throw new ApiError(404, `no volume named ${name}`);
    v.size = size;
    const r = t.resources.find((x) => x.kind === "Volume" && x.name === name);
    if (r) r.details = { ...r.details, size };
    await thingsOf.set(all);
    return v;
  },
  removeVolume: async (slug, name, force) => {
    const [all, t] = await ofProject(slug);
    const v = t.volumes.find((x) => x.name === name);
    if (v?.mountedBy.length && !force) throw new ApiError(409, `${name} is mounted by ${v.mountedBy.join(", ")}`);
    t.volumes = t.volumes.filter((x) => x.name !== name);
    t.resources = t.resources.filter((x) => x.kind !== "Volume" || x.name !== name);
    await thingsOf.set(all);
  },
  snapshots: async (slug, volume) => (await ofProject(slug))[1].snapshots.filter((s) => s.volume === volume),
  createSnapshot: async (slug, volume, name) => {
    const [all, t] = await ofProject(slug);
    const s: SnapshotInfo = { name: name || `${volume}-${new Date().toISOString().slice(0, 16).replace(/[-:T]/g, "")}`, volume, size: t.volumes.find((v) => v.name === volume)?.size, ready: false, createdAt: now() };
    t.snapshots.push(s);
    await thingsOf.set(all);
    setTimeout(async () => {
      const [all2, t2] = await ofProject(slug);
      const x = t2.snapshots.find((y) => y.name === s.name);
      if (x) x.ready = true;
      await thingsOf.set(all2);
    }, 4000);
    return s;
  },
  removeSnapshot: async (slug, volume, snapshot) => {
    const [all, t] = await ofProject(slug);
    t.snapshots = t.snapshots.filter((s) => s.volume !== volume || s.name !== snapshot);
    await thingsOf.set(all);
  },
  restoreVolume: async (slug, volume, body) => {
    const [all, t] = await ofProject(slug);
    const v = t.volumes.find((x) => x.name === volume);
    if (!v) throw new ApiError(404, `no volume named ${volume}`);
    if (body.to) {
      const made: VolumeInfo = { ...v, name: body.to, mountedBy: [], restoredFrom: body.snapshot, createdAt: now() };
      t.volumes.push(made);
      t.resources.push({ kind: "Volume", name: body.to, phase: "Bound", details: { size: v.size, mode: v.shared ? "shared" : "single", restoredFrom: body.snapshot }, attachedTo: [], data: true, createdAt: now() });
      await thingsOf.set(all);
      return { volume: made, inPlace: false, message: `${body.to} was made from ${body.snapshot}` };
    }
    v.restoredFrom = body.snapshot;
    await thingsOf.set(all);
    return { volume: v, inPlace: true, message: `${volume} was restored from ${body.snapshot}` };
  },
  domains: async (slug) => (await ofProject(slug))[1].domains,
  addDomain: async (slug, host) => {
    const [all, p] = await ofProject(slug);
    const domain: DomainStatus = { host, dns: "missing", target: p.domains.target, certificate: "issuing", message: "Point a CNAME at the target; the certificate follows." };
    p.domains.domains.push(domain);
    await thingsOf.set(all);
    return domain;
  },
  removeDomain: async (slug, host) => {
    const [all, p] = await ofProject(slug);
    p.domains.domains = p.domains.domains.filter((d) => d.host !== host);
    await thingsOf.set(all);
  },
  drains: async (slug) => (await ofProject(slug))[1].drains,
  addDrain: async (slug, body) => {
    const [all, p] = await ofProject(slug);
    const name = body.name || new URL(body.url.replace(/^syslog(\+tls)?:/, "https:")).hostname.split(".")[0] || "drain";
    const drain: Drain = { name, url: body.url, format: body.format ?? (body.url.startsWith("syslog") ? "syslog" : "json"), processes: body.processes, headers: Object.keys(body.headers ?? {}), phase: "Pending", sent: 0, errors: 0, createdAt: now() };
    p.drains.push(drain);
    await thingsOf.set(all);
    return drain;
  },
  removeDrain: async (slug, name) => {
    const [all, p] = await ofProject(slug);
    p.drains = p.drains.filter((d) => d.name !== name);
    await thingsOf.set(all);
  },
  members: async (slug) => (await ofProject(slug))[1].members,
  setMember: async (slug, member) => {
    const [all, p] = await ofProject(slug);
    const at = p.members.findIndex((m) => m.name === member.name);
    if (at >= 0) p.members[at] = member;
    else p.members.push(member);
    await thingsOf.set(all);
  },
  removeMember: async (slug, name) => {
    const [all, p] = await ofProject(slug);
    p.members = p.members.filter((m) => m.name !== name);
    await thingsOf.set(all);
  },
  allow: async (slug) => (await ofProject(slug))[1].allow,
  setAllow: async (slug, entries) => {
    const [all, p] = await ofProject(slug);
    p.allow = entries;
    await thingsOf.set(all);
    return entries;
  },
  streamLogs: async (slug, query, signal, onLine) => {
    await wait();
    const p = await projectsOf.find(slug);
    const processes = Object.keys(p.spec.processes ?? { web: {} });
    for (const line of logsOf(slug, processes)) if (!query.process || line.instance?.startsWith(query.process + "-")) onLine(line);
    if (!query.follow) return;
    let n = 0;
    await until(signal, 2000, () => onLine(liveLine(slug, query.process ?? processes[0] ?? "web", n++)));
  },
  metrics: async (slug, range) => {
    await wait();
    const p = await projectsOf.find(slug);
    return metricsOf(slug, Object.keys(p.spec.processes ?? { web: {} }), range);
  },
  instances: async (slug) => {
    await wait();
    const p = await projectsOf.find(slug);
    return Object.entries(p.processes ?? {}).flatMap(([name, st]) => Array.from({ length: Math.max(st.desired, 1) }, (_, i) => ({ name: `${name}-${i + 1}`, process: name, pod: `${slug}-${name}-${id().slice(0, 5)}`, ready: i < st.ready })));
  },
  shellTicket: async () => {
    await wait();
    return { ticket: "mock" };
  },
  shellSocket: async () => null,
};
