import { ApiError } from "@shpyrd/shared/api/error";
import { mockProjectArchives } from "@shpyrd/shared/api/project-archives-mock";
import { collection, single, wait } from "@shpyrd/shared/api/mock-store";
import { buildFailed } from "./types";
import type { Api } from "./api";
import type {
  Globals,
  AllowEntry,
  APIToken,
  AuditEntry,
  BuildInfo,
  ConfigVar,
  Connection,
  DnsRecord,
  DomainClaim,
  DomainStatus,
  Drain,
  GitHubAccounts,
  GitHubConnections,
  GitHubLink,
  GitHubRepository,
  Identity,
  Invitation,
  LoginMethods,
  Member,
  Person,
  RegistryToken,
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
  SleepStatus,
} from "./types";
import { buildOutput, liveLine, logsOf, metricsOf } from "@/lib/samples";
import audit from "../../mock/audit.json";
import config from "../../mock/config.json";
import me from "../../mock/me.json";
import projects from "../../mock/projects.json";
import things from "../../mock/things.json";
import workspace from "../../mock/workspace.json";

// The record that points a domain at target, as the server writes it: a
// CNAME, or an A to the front door at a zone apex. The server asks the public
// suffix list; two labels (acme.com) is near enough for the Mock.
const frontDoor = "203.0.113.10";
function recordFor(host: string, target: string): DnsRecord {
  return host.split(".").length === 2
    ? { type: "A", name: host, value: frontDoor }
    : { type: "CNAME", name: host, value: target };
}

// Everything of a project beyond itself, and everything of the workspace
// beyond itself: one kept object, changed in place.
type ProjectThings = {
  configVars: ConfigVar[];
  configValues?: Record<string, string>;
  configPending?: boolean;
  builds: BuildInfo[];
  resources: ResourceInfo[];
  volumes: VolumeInfo[];
  snapshots: SnapshotInfo[];
  domains: { target: string; domains: DomainStatus[]; redirectTo?: string };
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
  workspaceDrains: Drain[];
  loginMethods: LoginMethods;
  domainClaims: DomainClaim[];
  sizes: SizeCatalog;
  globals: Globals;
  github: GitHubConnections;
  githubLink: GitHubLink | null;
  githubRepositories: GitHubRepository[];
  githubBranches: Record<string, string[]>;
  registryTokens: RegistryToken[];
};

// The shape of the files changes with the application: what a browser
// kept from an older shape is left behind under the older name.
const shape = "7";
const projectsOf = collection<Project>(
  `projects${shape}`,
  projects as Project[],
  (p) => p.slug,
);
const workspaceOf = single<WorkspaceInfo>(
  `workspace${shape}`,
  workspace as WorkspaceInfo,
);
const seed = things as unknown as Things;
const kept = single<Things>(`things${shape}`, seed);
// What was kept from before this version of the files may lack what
// was added since, or hold an older shape: the seed fills it in.
const thingsOf = {
  async get(): Promise<Things> {
    const all = await kept.get();
    for (const key of Object.keys(seed) as (keyof Things)[]) {
      if (
        all[key] === undefined ||
        Array.isArray(all[key]) !== Array.isArray(seed[key])
      )
        (all as Record<string, unknown>)[key] = structuredClone(seed[key]);
    }
    return all;
  },
  set: (all: Things) => kept.set(all),
};

const empty = (): ProjectThings => ({
  configVars: [],
  builds: [],
  resources: [],
  volumes: [],
  snapshots: [],
  domains: { target: "acme.shpyrd.app", domains: [] },
  drains: [],
  members: [],
  allow: [],
});

// The read-back of a sleep policy as the server writes it (#120): the
// project's own, an explicit off, or the workspace's default (15m, page
// mode, in the Mock's workspace).
function sleepReadBack(
  own: { after?: string; resuming?: "page" | "wait" | "" } | undefined,
  was: { state?: string } | undefined,
): SleepStatus {
  const state = (was?.state as "awake" | "sleeping" | undefined) ?? "awake";
  if (own?.after === "off") return { state: "awake", source: "off" };
  if (own?.after)
    return { state, after: own.after, resuming: own.resuming || "wait", source: "project" };
  return { state, after: "15m", resuming: "page", source: "workspace" };
}

async function ofProject(slug: string): Promise<[Things, ProjectThings]> {
  const all = await thingsOf.get();
  all.projects[slug] ??= empty();
  const p = all.projects[slug];
  p.volumes ??= [];
  p.snapshots ??= [];
  return [all, p];
}

const summary = ({
  spec: _spec,
  status: _status,
  ...rest
}: Project): ProjectSummary => rest;
// The first domain of a project's own that answers: its DNS points here
// and its certificate is ready.
const domainOf = (all: Things, slug: string) =>
  all.projects[slug]?.domains.domains.find(
    (d) =>
      d.dns === "ok" &&
      (d.certificate === "ready" || d.certificate === "wildcard"),
  )?.host;
const now = () => new Date().toISOString();
const id = () => Math.random().toString(36).slice(2, 10);
const who = () => (me as Identity).email;

// A release goes out: a number more, and the instances follow.
function release(
  p: Project,
  kind: Project["status"]["releases"][number]["kind"],
  description: string,
  build?: string | number,
) {
  const number = Math.max(0, ...p.status.releases.map((r) => r.number)) + 1;
  p.status.releases.push({
    number,
    kind,
    description,
    build,
    createdAt: now(),
  });
  p.release = number;
}

// A stream that waits for an abort: lines come every so often until then.
function until(
  signal: AbortSignal,
  every: number,
  tick: () => void,
): Promise<void> {
  return new Promise((done) => {
    const timer = setInterval(tick, every);
    signal.addEventListener("abort", () => {
      clearInterval(timer);
      done();
    });
  });
}

// The plan the Mock plays (storage plan step 4): the paid workspace of
// workspace.json, or the free one, with no disks, one shared database
// and one bucket, when the browser says `shpyrd.mock.plan = free`.
const freePlan: Pick<WorkspaceInfo, "limits" | "usage" | "projectCaps"> = {
  limits: {
    projects: 2,
    instances: 2,
    cpu: "2",
    memory: "512Mi",
    allowDisks: false,
    objectStorage: { buckets: 1, bucketCap: "1Gi", total: "1Gi" },
    postgres: {
      types: ["shared"],
      defaultType: "shared",
      sharedCap: "1Gi",
      databases: 1,
    },
  },
  usage: {
    projects: 1,
    instances: 1,
    cpu: "0.5",
    memory: "256Mi",
    storage: "0",
    buckets: 0,
    objectStorage: "0",
    databases: 1,
    sharedDatabases: 1,
  },
  projectCaps: undefined,
};
const planOf = () => {
  try {
    return localStorage.getItem("shpyrd.mock.plan") === "free"
      ? "free"
      : "paid";
  } catch {
    return "paid";
  }
};
const currentWorkspace = async (): Promise<WorkspaceInfo> => {
  const ws = await workspaceOf.get();
  return planOf() === "free" ? { ...ws, ...freePlan } : ws;
};

// "20Gi", "512Mi", "0" as GiB.
// 5 as "5 GB", 0.5 as "512 MB": the server's words for a quantity.
const gbWordsOf = (g: number) =>
  g >= 1 || g === 0
    ? `${Number.isInteger(g) ? g : g.toFixed(1)} GB`
    : `${Math.round(g * 1024)} MB`;
const gib = (q: string | undefined) => {
  const n = parseFloat(q ?? "0");
  if (Number.isNaN(n)) return 0;
  if (q?.endsWith("Mi")) return n / 1024;
  if (q?.endsWith("Ti")) return n * 1024;
  return n;
};
const gbWords = (g: number) => `${Number.isInteger(g) ? g : g.toFixed(1)} GB`;

// What the project's disks claim: its volumes and its dedicated databases.
const projectStorageOf = (t: ProjectThings) =>
  t.volumes.reduce((sum, v) => sum + gib(v.size), 0) +
  t.resources
    .filter((r) => r.kind === "Postgres" && r.details?.type !== "shared")
    .reduce(
      (sum, r) =>
        sum + gib(r.details?.storage) * Number(r.details?.instances ?? 1),
      0,
    );

// The server's judgement, in its words: a new disk against the plan's
// switch, the workspace's ceiling and the project's own cap.
const admitDisk = async (
  slug: string,
  t: ProjectThings,
  extra: number,
  fresh: boolean,
) => {
  const ws = await currentWorkspace();
  const l = ws.limits ?? {};
  if (fresh && l.allowDisks === false)
    throw new ApiError(
      400,
      "Disks are not included in the free workspace; use a bucket for files, or upgrade.",
    );
  if (l.storage) {
    const used = gib(ws.usage?.storage);
    if (used + extra > gib(l.storage) && extra > 0)
      throw new ApiError(
        400,
        `This would bring the workspace to ${gbWords(used + extra)} of ${gbWords(gib(l.storage))}.`,
      );
  }
  const cap = ws.projectCaps?.[slug]?.storage;
  if (cap) {
    const used = projectStorageOf(t);
    if (used + extra > gib(cap) && extra > 0)
      throw new ApiError(
        400,
        `This would bring the project to ${gbWords(used + extra)} of ${gbWords(gib(cap))}.`,
      );
  }
};

// The server's check of watch paths (pkg/gitauth/watchpaths.go), in its
// words: at most 20 patterns of up to 200 characters, each one readable.
function watchPathsError(paths: string[]): string | undefined {
  if (paths.length > 20)
    return `a project takes at most 20 watch paths; there are ${paths.length}`;
  for (const p of paths) {
    // Characters, as the server counts them, not UTF-16 units.
    const chars = [...p];
    if (chars.length > 200)
      return `a watch path is at most 200 characters; ${JSON.stringify(chars.slice(0, 40).join(""))}… is longer`;
    const raw = p.replace(/^!/, "").replace(/^\//, "");
    if (raw === "") return `the watch path ${JSON.stringify(p)} names no file or folder`;
    const parts = raw.endsWith("/") ? raw.slice(0, -1).split("/") : raw.split("/");
    if (parts.some((s) => s === ""))
      return `the watch path ${JSON.stringify(p)} has an empty part (//)`;
    if (parts.some((s) => s !== "**" && s.includes("**")))
      return `in the watch path ${JSON.stringify(p)}, ** stands alone between slashes, as in services/**/api`;
    if (parts.some(unreadable))
      return `the watch path ${JSON.stringify(p)} is not a pattern shpyrd can read: check its [ ] and \\`;
  }
}

// A part path.Match refuses (a port of Go's matchChunk and getEsc, which
// check the whole pattern even when nothing is matched): a trailing \, or
// a class whose elements start with - or ], run out, or are not closed.
function unreadable(part: string): boolean {
  const chars = [...part];
  let i = 0;
  // One class element: a character, escaped or not; Go needs more after it.
  const esc = (): boolean => {
    if (i >= chars.length || chars[i] === "-" || chars[i] === "]") return false;
    if (chars[i] === "\\" && ++i >= chars.length) return false;
    i++;
    return i < chars.length;
  };
  while (i < chars.length) {
    if (chars[i] === "\\") {
      if (++i >= chars.length) return true;
      i++;
    } else if (chars[i] === "[") {
      i++;
      if (chars[i] === "^") i++;
      for (let n = 0; ; n++) {
        if (chars[i] === "]" && n > 0) {
          i++;
          break;
        }
        if (!esc()) return true;
        if (chars[i] === "-") {
          i++;
          if (!esc()) return true;
        }
      }
    } else i++;
  }
  return false;
}

export const mock: Api = {
  ...mockProjectArchives("workspace"),
  config: async () => {
    await wait();
    const ws = await currentWorkspace();
    const rules = ws.limits?.postgres;
    const postgres = rules
      ? {
          types: rules.types?.length
            ? rules.types
            : (["shared", "dedicated"] as const),
          defaultType: rules.defaultType ?? "dedicated",
          sharedCap: rules.sharedCap ?? "1Gi",
        }
      : (config as PublicConfig).postgres;
    return {
      ...(config as PublicConfig),
      postgres: postgres as PublicConfig["postgres"],
      workspace: {
        ...(config as PublicConfig).workspace!,
        name: ws.name,
        branding: ws.branding,
        capabilities: ws.capabilities,
      },
    };
  },
  me: async () => {
    await wait();
    return me as Identity;
  },
  links: async () => [
    {
      section: "Cloud",
      label: "Billing",
      url: "/.shpyrd/gate?name=billing",
      icon: "credit-card",
    },
  ],

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
    if (!i)
      throw new ApiError(
        404,
        "this invitation was used, withdrawn, or never was",
      );
    return {
      workspace: { slug: ws.slug, name: ws.name, address: ws.address },
      email: i.email,
      role: i.role,
      team: i.team,
      invitedBy: i.invitedBy,
      expiresAt: i.expiresAt,
      expired: i.expired,
      url: `/invite/${token}`,
    };
  },
  acceptInvitation: async (token) => {
    const all = await thingsOf.get();
    const i = all.invitations.find((x) => x.id === token);
    if (!i)
      throw new ApiError(
        404,
        "this invitation was used, withdrawn, or never was",
      );
    all.invitations = all.invitations.filter((x) => x.id !== token);
    await thingsOf.set(all);
    return {
      workspace: (await workspaceOf.get()).slug,
      role: i.role,
      next: "/",
    };
  },

  workspace: async () => {
    await wait();
    return currentWorkspace();
  },
  putProjectCaps: async (caps) => {
    await wait();
    const ws = await workspaceOf.get();
    const kept = Object.fromEntries(
      Object.entries(caps).filter(([, c]) => c.storage?.trim()),
    );
    ws.projectCaps = Object.keys(kept).length ? kept : undefined;
    await workspaceOf.set(ws);
    return currentWorkspace();
  },
  updateWorkspace: async (body) => {
    const current = await workspaceOf.get();
    const { logo, color, ui, ...rest } = body;
    const branding = { ...current.branding };
    if (logo !== undefined) branding.logoUrl = logo || undefined;
    if (color !== undefined) branding.color = color || undefined;
    // An application of the workspace's own (RFC-0084): the Mock
    // settles a tag to a made-up digest and reports it serving.
    let chosen = current.ui;
    if (ui !== undefined) {
      if (!ui.trim()) chosen = undefined;
      else {
        const ref = ui.trim();
        if (!/^oci:\/\/[^/]+\/.+/.test(ref) && !ref.startsWith("dir:/"))
          throw new ApiError(400, "unknown source: oci://host/repository@sha256:... or dir:/path");
        const settled = ref.includes("@sha256:") ? ref : `${ref.replace(/:[^/:]+$/, "")}@sha256:${"0".repeat(64)}`;
        chosen = { ref: settled, setBy: "ana@acme.test", setAt: now(), status: { state: "serving", at: now(), name: "Acme", version: "2.3.1" } };
      }
    }
    return workspaceOf.set({ ...current, ...rest, branding, ui: chosen, updatedAt: now() });
  },
  reloadWorkspaceUI: async () => {
    await wait();
    return currentWorkspace();
  },
  registryTokens: async () => (await thingsOf.get()).registryTokens,
  setRegistryToken: async (host, body) => {
    const all = await thingsOf.get();
    const token: RegistryToken = { host, username: body.username, updatedBy: "ana@acme.test", updatedAt: now() };
    all.registryTokens = [...all.registryTokens.filter((t) => t.host !== host), token].sort((a, b) => a.host.localeCompare(b.host));
    await thingsOf.set(all);
    return token;
  },
  unsetRegistryToken: async (host) => {
    const all = await thingsOf.get();
    all.registryTokens = all.registryTokens.filter((t) => t.host !== host);
    await thingsOf.set(all);
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
      return {
        email: body.email,
        role: body.role,
        team: body.team,
        applied: true,
        emailed: false,
      };
    }
    const invitation: Invitation = {
      id: id(),
      ...body,
      invitedBy: who(),
      createdAt: now(),
      expiresAt: new Date(Date.now() + 7 * 86400e3).toISOString(),
      expired: false,
    };
    all.invitations.push(invitation);
    await thingsOf.set(all);
    return {
      email: body.email,
      role: body.role,
      team: body.team,
      applied: false,
      invitation,
      link: `${location.origin}/invite/${invitation.id}`,
      emailed: false,
      mailError: "no mail is set up in the Mock",
    };
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
    const token: APIToken = {
      id: id(),
      name: body.name,
      ownerEmail: who(),
      platformRole: body.platformRole,
      projectRoles: body.projectRoles,
      createdAt: now(),
      expiresAt: days
        ? new Date(Date.now() + days * 86400e3).toISOString()
        : undefined,
    };
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
    const domain: WorkspaceDomain = {
      host,
      verified: false,
      primary: false,
      records: [
        recordFor(host, "acme.shpyrd.cloud"),
        { type: "CNAME", name: `*.${host}`, value: "acme.shpyrd.cloud" },
        {
          type: "TXT",
          name: `_shpyrd-verify.${host}`,
          value: `shpyrd-verify=${id()}`,
        },
      ],
      url: `https://${host}`,
    };
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
  setWorkspaceDomainRedirect: async (host, redirectApps) => {
    const all = await thingsOf.get();
    const d = all.workspaceDomains.find((x) => x.host === host);
    if (!d || (redirectApps && (!d.verified || !d.primary)))
      throw new ApiError(400, "Choose a verified primary domain first.");
    for (const x of all.workspaceDomains) x.redirectApps = redirectApps;
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
    all.loginMethods.connectors.push({
      id: cid,
      type: body.type,
      name: body.name || body.type,
      detail: body.hostedDomain || body.org || body.tenant || body.issuer,
      workspace: "acme",
    });
    await thingsOf.set(all);
    return { id: cid };
  },
  removeLoginMethod: async (methodId) => {
    const all = await thingsOf.get();
    all.loginMethods.connectors = all.loginMethods.connectors.filter(
      (c) => c.id !== methodId,
    );
    await thingsOf.set(all);
  },
  domainClaims: async () => (await thingsOf.get()).domainClaims,
  claimDomain: async (domain, connector) => {
    const all = await thingsOf.get();
    const claim: DomainClaim = {
      domain,
      connector: connector || undefined,
      verified: false,
      record: `_shpyrd.${domain}`,
      recordValue: `shpyrd-verify=${id()}`,
    };
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
  globals: async () => (await thingsOf.get()).globals,
  changeGlobals: async (change) => {
    const all = await thingsOf.get();
    for (const [name, value] of Object.entries(change.set ?? {})) {
      if (!/^[A-Za-z_][A-Za-z0-9_]*$/.test(name))
        throw new ApiError(400, `${name} is not a name`);
      if (value === "") throw new ApiError(400, `${name} has no value`);
      const v = all.globals.vars.find((x) => x.name === name);
      if (v) v.updatedAt = now();
      else all.globals.vars.push({ name, updatedAt: now() });
    }
    for (const name of change.unset ?? [])
      all.globals.vars = all.globals.vars.filter((v) => v.name !== name);
    all.globals.vars.sort((a, b) => a.name.localeCompare(b.name));
    await thingsOf.set(all);
    return all.globals;
  },
  sizes: async () => (await thingsOf.get()).sizes,

  // As the server answers the list: with the teams that have access to
  // each, and the first domain of its own that answers.
  projects: async () => {
    const all = await thingsOf.get();
    return (await projectsOf.list()).map((p) => {
      const teams = [
        ...new Set(
          (all.projects[p.slug]?.members ?? []).flatMap((m) =>
            m.team ? [m.team] : [],
          ),
        ),
      ].sort();
      return {
        ...summary(p),
        teams: teams.length ? teams : undefined,
        domain: domainOf(all, p.slug),
      };
    });
  },
  project: async (slug) => {
    const p = await projectsOf.find(slug);
    return { ...p, domain: domainOf(await thingsOf.get(), slug) };
  },
  createProject: async (body) => {
    if (
      body.exposure === "internal" &&
      !(await workspaceOf.get()).capabilities?.internalExposure
    )
      throw new ApiError(
        403,
        "Internal exposure is not enabled for this workspace.",
      );
    const all = await projectsOf.list();
    if (!/^[a-z0-9-]{2,}$/.test(body.slug))
      throw new ApiError(
        400,
        "the slug has lowercase letters, digits and dashes",
      );
    if (all.some((p) => p.slug === body.slug))
      throw new ApiError(409, `a project named ${body.slug} exists`);
    const created: Project = {
      slug: body.slug,
      displayName: body.displayName || body.slug,
      description: body.description,
      namespace: `p-${body.slug}`,
      phase: "Pending",
      release: 0,
      access: "public",
      exposure: body.exposure ?? "external",
      createdAt: now(),
      spec: {
        source: body.git ? { git: body.git, subPath: body.subPath } : undefined,
      },
      status: { phase: "Pending", releases: [] },
    };
    await projectsOf.set(created);
    return summary(created);
  },
  updateProject: async (slug, body) => {
    const p = await projectsOf.find(slug);
    if (body.name !== undefined) p.displayName = body.name;
    if (body.description !== undefined)
      p.description = body.description || undefined;
    if (body.featured !== undefined) p.featured = body.featured;
    if (body.icon !== undefined) p.icon = body.icon || undefined;
    if (body.iconColor !== undefined) p.iconColor = body.iconColor || undefined;
    if (body.autoDeploy !== undefined) {
      const git = p.spec.source?.git;
      if (!git)
        throw new ApiError(
          400,
          "the project does not deploy from a git repository: auto-deploy follows a git branch",
        );
      git.autoDeploy = body.autoDeploy;
    }
    if (body.watchPaths !== undefined || body.waitForChecks !== undefined) {
      const git = p.spec.source?.git;
      if (!git)
        throw new ApiError(
          400,
          "the project does not deploy from a git repository: watch paths and wait for CI follow a git branch",
        );
      if (body.watchPaths !== undefined) {
        const paths = body.watchPaths.map((l) => l.trim()).filter(Boolean);
        const bad = watchPathsError(paths);
        if (bad) throw new ApiError(422, bad);
        git.watchPaths = paths.length ? paths : undefined;
      }
      if (body.waitForChecks !== undefined)
        git.waitForChecks = body.waitForChecks || undefined;
    }
    return projectsOf.set(p);
  },
  projectGitHooks: async (slug) => {
    await projectsOf.find(slug);
    return {
      url: `https://acme.shpyrd.app/api/hooks/git/${slug}/5e0c2a7b9d4f61385e0c2a7b9d4f61385e0c2a7b9d4f61385e0c2a7b9d4f6138`,
      secret: "9a41d0c7e2b85f369a41d0c7e2b85f369a41d0c7e2b85f369a41d0c7e2b85f36",
    };
  },
  projectGitAuth: async (slug) => {
    const url = (await projectsOf.find(slug)).spec.source?.git?.url;
    // The fixtures' URLs come with and without the scheme.
    const owner = url?.match(/^(?:https:\/\/)?github\.com\/([^/]+)\//i)?.[1]?.toLowerCase();
    return owner === "acme" ? { url, using: "github-app", githubAccount: "acme" } : { url, using: "none" };
  },
  github: async () => (await thingsOf.get()).github,
  // GitHub would ask where to install, then send the browser back to the
  // Git page: the Mock connects acme-labs and sends it there at once.
  connectGitHub: async () => {
    const all = await thingsOf.get();
    if (!all.github.configured) throw new ApiError(409, all.github.note ?? "GitHub is not set up on this platform.");
    if (!all.github.installations.some((i) => i.id === 77)) {
      all.github.installations.push({
        id: 77,
        account: "acme-labs",
        type: "Organization",
        connectedBy: "you@acme.test",
        createdAt: now(),
        suspended: false,
        manageURL: "https://github.com/organizations/acme-labs/settings/installations/77",
      });
      await thingsOf.set(all);
    }
    return { installURL: "/workspace/git", authorizeURL: "/workspace/git" };
  },
  disconnectGitHub: async (id) => {
    const all = await thingsOf.get();
    all.github.installations = all.github.installations.filter((i) => i.id !== id);
    await thingsOf.set(all);
  },
  githubMe: async () => (await thingsOf.get()).githubLink,
  // GitHub would ask the person to authorize the App in a popup: the Mock
  // links Ana at once.
  linkGitHub: async () => {
    const all = await thingsOf.get();
    all.githubLink = { login: "ana-souza", linkedAt: now() };
    await thingsOf.set(all);
    return { authorizeURL: "" };
  },
  unlinkGitHub: async () => {
    const all = await thingsOf.get();
    all.githubLink = null;
    await thingsOf.set(all);
  },
  githubAccounts: async () => {
    const all = await thingsOf.get();
    if (!all.github.configured) throw new ApiError(409, all.github.note ?? "GitHub is not set up on this platform.");
    const connected = all.github.installations.length;
    if (connected > 0 && !all.githubLink)
      throw new ApiError(409, "Link your GitHub account first: Continue with GitHub in New project, or run shpyrd github link", "github_link_required");
    const accounts: GitHubAccounts["accounts"] = all.github.installations.map((i) => ({
      installation: i.id,
      account: i.account,
      type: i.type,
      suspended: i.suspended,
      manageURL: i.manageURL,
    }));
    return { accounts, connected };
  },
  githubRepositories: async (installation) => {
    const all = await thingsOf.get();
    if (!all.github.configured) throw new ApiError(409, all.github.note ?? "GitHub is not set up on this platform.");
    if (!all.github.installations.length) return [];
    if (!all.githubLink)
      throw new ApiError(409, "Link your GitHub account first: Continue with GitHub in New project, or run shpyrd github link", "github_link_required");
    const live = new Set(all.github.installations.map((i) => i.id));
    return all.githubRepositories.filter((r) => live.has(r.installation) && (installation === undefined || r.installation === installation));
  },
  githubBranches: async (fullName) => {
    const branches = (await thingsOf.get()).githubBranches[fullName];
    if (!branches) throw new ApiError(404, `No GitHub connection of this workspace reaches ${fullName.split("/")[0]}.`);
    if (!(await thingsOf.get()).githubLink)
      throw new ApiError(409, "Link your GitHub account first: Continue with GitHub in New project, or run shpyrd github link", "github_link_required");
    return branches;
  },
  // The Mock keeps the image itself, as the data URL it came as.
  setProjectIcon: async (slug, dataUrl) => {
    const type = /^data:([^;,]+)/.exec(dataUrl)?.[1];
    if (!type || !["image/svg+xml", "image/png", "image/webp"].includes(type))
      throw new ApiError(400, "icon must be an SVG, a PNG or a WebP image");
    const p = await projectsOf.find(slug);
    p.iconUrl = dataUrl;
    p.iconType = type;
    return projectsOf.set(p);
  },
  removeProjectIcon: async (slug) => {
    const p = await projectsOf.find(slug);
    p.iconUrl = p.iconType = undefined;
    return projectsOf.set(p);
  },
  destroyProject: (slug) => projectsOf.remove(slug),
  deploy: async (slug, body) => {
    const [all, t] = await ofProject(slug);
    const p = await projectsOf.find(slug);
    p.spec.source = { git: body.git, subPath: body.subPath };
    p.spec.build = { strategy: body.strategy, dockerfile: body.dockerfile };
    const number = Math.max(0, ...t.builds.map((b) => b.number)) + 1;
    t.builds.unshift({
      name: `${slug}-${number}`,
      number,
      strategy: body.strategy ?? "buildpacks",
      status: "Building",
      source: body.git?.revision ?? "main",
      startedAt: now(),
    });
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
      release(
        p2,
        "deploy",
        `Deploy of ${body.git?.revision ?? "main"}`,
        number,
      );
      p2.phase = p2.status.phase = "Running";
      p2.status.message = undefined;
      await projectsOf.set(p2);
    }, 12_000);
    return summary(p);
  },
  redeploy: async (slug, action) => {
    await wait();
    const p = await projectsOf.find(slug);
    const [all, t] = await ofProject(slug);
    if (t.configPending) {
      t.configPending = false;
      await thingsOf.set(all);
      release(
        p,
        "config",
        "Apply saved configuration",
        p.status.releases.at(-1)?.build,
      );
      await projectsOf.set(p);
    }
    if (action === "restart")
      return {
        action: "restart",
        message: "Running the release command again",
      };
    if (buildFailed({ phase: p.phase, conditions: p.status.conditions }))
      return { action: "rebuild", message: "Building the same source again" };
    return {
      action: "redeploy",
      message: "Starting new instances of the current release",
    };
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
      const spec = {
        ...p.spec.processes?.[name],
        ...(change.size ? { size: change.size } : {}),
        ...(change.replicas !== undefined ? { replicas: change.replicas } : {}),
      };
      // Sleep (#135): "off" is stored, "default" clears the project's own.
      if (change.sleep) {
        const after = change.sleep.after.toLowerCase();
        if (after === "default" || after === "") delete spec.sleep;
        else spec.sleep = after === "off" ? { after: "off" } : { after, resuming: change.sleep.resuming ?? "wait" };
      }
      p.spec.processes = { ...p.spec.processes, [name]: spec };
      const st = p.processes?.[name] ?? { desired: 1, ready: 1 };
      p.processes = {
        ...p.processes,
        [name]: {
          ...st,
          desired: spec.replicas ?? st.desired,
          ready: spec.replicas ?? st.ready,
          size: spec.size ?? st.size,
          ...(change.sleep && name === "web"
            ? { sleep: sleepReadBack(spec.sleep, st.sleep) }
            : {}),
        },
      };
    }
    if (Object.values(changes).some((c) => c.size))
      release(p, "config", "Sizes changed", p.status.releases.at(-1)?.build);
    return projectsOf.set(p);
  },
  setExposure: async (slug, exposure) => {
    const project = await projectsOf.find(slug);
    if (
      exposure === "internal" &&
      project.exposure !== "internal" &&
      !(await workspaceOf.get()).capabilities?.internalExposure
    )
      throw new ApiError(
        403,
        "Internal exposure is not enabled for this workspace.",
      );
    return projectsOf.set({ ...project, exposure });
  },
  setAccess: async (slug, access) =>
    projectsOf.set({ ...(await projectsOf.find(slug)), access }),
  setMaintenance: async (slug, body) => {
    await wait();
    const p = await projectsOf.find(slug);
    const maintenance = body.enabled
      ? { enabled: true, message: body.message, since: p.maintenance?.since ?? new Date().toISOString(), by: p.maintenance?.by ?? me.email }
      : undefined;
    return projectsOf.set({ ...p, maintenance, spec: { ...p.spec, maintenance } });
  },
  preview: async (slug, body) => {
    await wait();
    const p = await projectsOf.find(slug);
    return {
      url: `${p.url ?? `https://${slug}.acme.shpyrd.app`}/.shpyrd/preview?as=${body.anonymous ? "anonymous" : body.teams.join(",") || "nobody"}`,
    };
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
    const lines =
      b?.status === "Building" ? buildOutput.slice(0, 6) : buildOutput;
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
    const bound = (p.spec.bindings ?? []).flatMap((b) =>
      b.kind === "Postgres"
        ? [{ name: "DATABASE_URL", provider: `${b.kind} ${b.name}` }]
        : b.kind === "Redis"
          ? [{ name: "REDIS_URL", provider: `${b.kind} ${b.name}` }]
          : [],
    );
    return {
      vars: t.configVars.filter((v) => v.value === undefined),
      variables: t.configVars.filter((v) => v.value !== undefined),
      pending: t.configPending,
      bound,
      global: [{ name: "SENTRY_DSN", updatedAt: "2026-08-01T10:00:00Z" }],
    };
  },
  changeConfigVars: async (slug, change) => {
    const [all, t] = await ofProject(slug);
    const set = { ...change.set };
    for (const line of (change.dotenv ?? "").split("\n")) {
      const at = line.indexOf("=");
      if (at > 0 && !line.trim().startsWith("#"))
        set[line.slice(0, at).trim()] = line.slice(at + 1).trim();
    }
    const plain = change.kind === "variable";
    for (const name of [...Object.keys(set), ...(change.convert ?? [])]) {
      const existing = t.configVars.find((v) => v.name === name);
      if (plain && existing && existing.value === undefined)
        throw new ApiError(
          400,
          `${name} is a secret: saved secrets cannot be made visible`,
        );
      if (
        plain &&
        !change.confirmPlain &&
        (change.convert?.includes(name) ||
          /(_KEY|_SECRET|_TOKEN)$|PASSWORD|^(SECRET|TOKEN)$/i.test(name) ||
          (existing && existing.value === undefined))
      )
        throw new ApiError(
          400,
          `Confirm that ${name} can be visible to project viewers`,
        );
    }
    t.configValues ??= {};
    for (const name of change.convert ?? []) {
      const existing = t.configVars.find((v) => v.name === name);
      if (!existing) throw new ApiError(400, `${name} does not exist`);
      set[name] = t.configValues[name] ?? existing.value ?? "mock value";
    }
    for (const [name, value] of Object.entries(set)) {
      t.configValues[name] = value;
      t.configVars = t.configVars.filter((v) => v.name !== name);
      t.configVars.push({
        name,
        updatedAt: now(),
        ...(plain ? { value } : {}),
      });
    }
    for (const name of change.unset ?? []) {
      t.configVars = t.configVars.filter(
        (v) => v.name !== name || (v.value !== undefined) !== plain,
      );
    }
    t.configPending = true;
    await thingsOf.set(all);
  },
  resources: async (slug) => (await ofProject(slug))[1].resources,
  createResource: async (slug, body) => {
    const [all, t] = await ofProject(slug);
    if (t.resources.some((r) => r.kind === body.kind && r.name === body.name))
      throw new ApiError(409, `${body.kind} ${body.name} exists`);
    // The plan's rules for databases and disks, in the server's words.
    const ws = await currentWorkspace();
    const rules = ws.limits?.postgres;
    if (body.kind === "Postgres") {
      const type =
        (body.spec.type as string | undefined) ??
        rules?.defaultType ??
        "dedicated";
      const kinds = rules?.types?.length
        ? rules.types
        : ["shared", "dedicated"];
      if (!kinds.includes(type as "shared" | "dedicated"))
        throw new ApiError(
          400,
          type === "dedicated"
            ? "Your plan includes a shared database. A dedicated database comes with a paid plan."
            : "This workspace's plan has dedicated databases; a shared one is not among its kinds.",
        );
      if (
        rules?.maxInstances &&
        Number(body.spec.instances ?? 1) > rules.maxInstances
      )
        throw new ApiError(
          400,
          `A dedicated database here runs up to ${rules.maxInstances} instances.`,
        );
      const have = ws.usage?.databases ?? 0;
      if (rules?.databases && have + 1 > rules.databases)
        throw new ApiError(
          400,
          `This would bring the workspace to ${have + 1} of ${rules.databases} databases.`,
        );
      if (type === "dedicated")
        await admitDisk(
          slug,
          t,
          gib((body.spec.storage as string | undefined) ?? "5Gi") *
            Number(body.spec.instances ?? 1),
          true,
        );
    }
    if (body.kind === "Redis" && body.spec.persistent)
      await admitDisk(
        slug,
        t,
        gib((body.spec.storage as string | undefined) ?? "1Gi"),
        true,
      );
    if (body.kind === "Postgres" && body.spec.type === "shared") {
      const cap = (config as PublicConfig).postgres?.sharedCap ?? "1Gi";
      const kept = String(
        (body.spec.backups as { retention?: string } | undefined)?.retention ??
          "7d",
      ).replace(/d$/, "");
      const r: ResourceInfo = {
        kind: "Postgres",
        name: body.name,
        phase: "Pending",
        message: "your database is being placed on shared server pg-shared-2",
        details: {
          type: "shared",
          version: "17",
          storage: cap,
          used: "0",
          usedBytes: "0",
          capBytes: String(1 << 30),
          pooler: "off",
          backups: `nightly, kept ${kept}`,
        },
        attachedTo: [],
        data: true,
        bindable: true,
        createdAt: now(),
        note: `Your database can hold up to ${cap.replace(/Gi$/, " GB")}.`,
      };
      t.resources.push(r);
      await thingsOf.set(all);
      setTimeout(async () => {
        const [all2, t2] = await ofProject(slug);
        const x = t2.resources.find(
          (y) => y.kind === "Postgres" && y.name === body.name,
        );
        if (x) {
          x.phase = "Ready";
          x.message = "PostgreSQL 17, shared, on pg-shared-2";
          x.endpoint = `${body.name}.p-${slug}:5432`;
        }
        await thingsOf.set(all2);
      }, 6000);
      return r;
    }
    if (body.kind === "ObjectBucket") {
      // The plan's rules for buckets, in the server's words: how many,
      // what one may hold, what they may hold together.
      const o = ws.limits?.objectStorage;
      const asked = body.spec.maxBytes as string | undefined;
      const capGb = asked ? gib(asked) : o?.bucketCap ? gib(o.bucketCap) : 0;
      if (o?.bucketCap && capGb > gib(o.bucketCap))
        throw new ApiError(400, `A bucket holds up to ${gbWordsOf(gib(o.bucketCap))}.`);
      const have = ws.usage?.buckets ?? 0;
      if (o?.buckets && have + 1 > o.buckets)
        throw new ApiError(400, `This would bring the workspace to ${have + 1} of ${o.buckets} buckets.`);
      const held = gib(ws.usage?.objectStorage ?? "0");
      if (o?.total && held + capGb > gib(o.total))
        throw new ApiError(400, `This would bring the workspace to ${gbWordsOf(held + capGb)} of ${gbWordsOf(gib(o.total))} of object storage.`);
      const r: ResourceInfo = {
        kind: "ObjectBucket",
        name: body.name,
        phase: "Pending",
        details: {
          capBytes: String(Math.round(capGb * (1 << 30))),
          usedBytes: "0",
          objects: "0",
          public: String(body.spec.public === true),
          retentionDays: String(body.spec.retentionDays ?? 0),
          region: "us-east-1",
        },
        attachedTo: [],
        data: true,
        bindable: true,
        createdAt: now(),
        note: capGb ? `Your bucket can hold up to ${gbWordsOf(capGb)}.` : undefined,
      };
      t.resources.push(r);
      await thingsOf.set(all);
      setTimeout(async () => {
        const [all2, t2] = await ofProject(slug);
        const x = t2.resources.find(
          (y) => y.kind === "ObjectBucket" && y.name === body.name,
        );
        if (x) {
          x.phase = "Ready";
          x.endpoint = "http://object-storage.shpyrd-system.svc:3901";
          x.details = {
            ...x.details,
            bucket: `shpyrd-${slug.replace(/[^a-z0-9]/g, "").padEnd(8, "0")}${body.name.replace(/[^a-z0-9]/g, "").padEnd(20, "0")}`,
            publicEndpoint: "https://buckets.shpyrd.cloud",
          };
        }
        await thingsOf.set(all2);
      }, 4000);
      return r;
    }
    const list =
      body.kind === "Postgres" ? all.sizes.postgres : all.sizes.redis;
    const size = list.sizes.find(
      (x) => x.name === (body.spec.size || list.default),
    );
    if (!size)
      throw new ApiError(
        400,
        `unknown size "${body.spec.size}" for a ${body.kind} (sizes: ${list.sizes.map((x) => x.name).join(", ")})`,
      );
    const details = Object.fromEntries(
      Object.entries({ ...body.spec, size: size.name })
        .filter(([, v]) => typeof v !== "object")
        .map(([k, v]) => [k, String(v)]),
    );
    const r: ResourceInfo = {
      kind: body.kind,
      name: body.name,
      phase: "Pending",
      details,
      attachedTo: [],
      data: true,
      bindable: true,
      createdAt: now(),
    };
    t.resources.push(r);
    await thingsOf.set(all);
    if (!body.spec.size)
      r.note = `It has the size ${size.name}, the default: ${size.cpu} CPU, ${size.memory} of memory, ${size.connections} connections.`;
    setTimeout(async () => {
      const [all2, t2] = await ofProject(slug);
      const x = t2.resources.find(
        (y) => y.kind === body.kind && y.name === body.name,
      );
      if (x) x.phase = "Running";
      await thingsOf.set(all2);
    }, 6000);
    return r;
  },
  resizeResource: async (slug, kind, name, size) => {
    const [all, t] = await ofProject(slug);
    const r = t.resources.find(
      (x) => x.kind.toLowerCase() === kind.toLowerCase() && x.name === name,
    );
    if (!r) throw new ApiError(404, `no ${kind} named ${name}`);
    const list = r.kind === "Postgres" ? all.sizes.postgres : all.sizes.redis;
    const s = list.sizes.find((x) => x.name === size);
    if (!s)
      throw new ApiError(
        400,
        `unknown size "${size}" for a ${r.kind} (sizes: ${list.sizes.map((x) => x.name).join(", ")})`,
      );
    r.details = { ...r.details, size };
    await thingsOf.set(all);
    const restart =
      r.kind === "Redis"
        ? "It restarts with it: a cache comes back empty, a persistent store reloads its file."
        : "Its instances restart with it, one at a time; with one instance the database is unavailable for a moment.";
    return {
      ...r,
      note: `${name} now has the size ${size}: ${s.cpu} CPU, ${s.memory} of memory, ${s.connections} connections. ${restart}`,
    };
  },
  rotateBucket: async (slug, name) => {
    const [all, t] = await ofProject(slug);
    const r = t.resources.find(
      (x) => x.kind === "ObjectBucket" && x.name === name,
    );
    if (!r) throw new ApiError(404, `no bucket "${name}" in this project`);
    if (r.phase !== "Ready")
      throw new ApiError(409, "The bucket is not ready yet; rotate its key once it is.");
    const stamp = now();
    r.details = { ...r.details, rotating: stamp };
    await thingsOf.set(all);
    setTimeout(async () => {
      const [all2, t2] = await ofProject(slug);
      const x = t2.resources.find(
        (y) => y.kind === "ObjectBucket" && y.name === name,
      );
      if (x?.details) {
        const { rotating: _rotating, ...rest } = x.details;
        x.details = rest;
      }
      await thingsOf.set(all2);
    }, 5000);
    return { note: `A new key for ${name} is being written; the apps bound to it restart with it within a minute.` };
  },
  sleepPostgres: async (slug, name, after) => {
    await wait();
    const [all, t] = await ofProject(slug);
    const r = t.resources.find((x) => x.kind === "Postgres" && x.name === name);
    if (!r) throw new ApiError(404, `no database named ${name}`);
    if (r.details?.type === "shared")
      throw new ApiError(400, "Shared databases have no sleep setting; they are always on.");
    const want = after.toLowerCase();
    const st = sleepReadBack(
      want === "default" || want === "" ? undefined : { after: want },
      { state: r.details?.sleepState === "sleeping" ? "sleeping" : "awake" },
    );
    r.details = {
      ...r.details,
      sleepState: st.state,
      sleepAfter: st.after ?? "",
      sleepSource: st.source ?? "workspace",
    };
    await thingsOf.set(all);
    return { name };
  },
  convertPostgres: async (slug, name, body) => {
    await wait();
    const [all, t] = await ofProject(slug);
    const r = t.resources.find((x) => x.kind === "Postgres" && x.name === name);
    if (!r) throw new ApiError(404, `no database named ${name}`);
    const from = r.details?.type === "shared" ? "shared" : "dedicated";
    if (from === body.to)
      throw new ApiError(409, `${name} is a ${from} database already.`);
    if (r.phase !== "Ready" && r.phase !== "Running")
      throw new ApiError(
        409,
        `${name} is ${r.phase.toLowerCase()}; convert it once it is ready.`,
      );
    const cap = (config as PublicConfig).postgres?.sharedCap ?? "1Gi";
    const note =
      body.to === "dedicated"
        ? `${name} becomes a cluster of its own with a ${body.storage ?? "5Gi"} disk, priced as a dedicated database; its user and database names stay; DATABASE_URL changes only its host, and the apps bound to it restart once; connections pause while the data is copied (a few minutes per gigabyte), and a copy of the source stays restorable for seven days.`
        : `${name} moves onto the platform's shared server with a ${cap} cap; its disk goes; it gets new user and database names, so the apps bound to it restart once with the new credentials; only the extensions a shared database can hold may be installed (PostgreSQL's trusted ones and pgvector); connections pause while the data is copied (a few minutes per gigabyte), and a copy of the source stays restorable for seven days.`;
    r.phase = "Converting";
    r.message = `Converting to a ${body.to} database: preparing (step 1 of 3)`;
    await thingsOf.set(all);
    const steps = [
      "waiting for the new cluster (step 1 of 3)",
      "copying the data (step 2 of 3); connections are paused meanwhile",
      "switching the credentials over (step 3 of 3)",
    ];
    steps.forEach((words, i) =>
      setTimeout(
        async () => {
          const [all2, t2] = await ofProject(slug);
          const x = t2.resources.find(
            (y) => y.kind === "Postgres" && y.name === name,
          );
          if (x) x.message = `Converting to a ${body.to} database: ${words}`;
          await thingsOf.set(all2);
        },
        3000 * (i + 1),
      ),
    );
    setTimeout(async () => {
      const [all2, t2] = await ofProject(slug);
      const x = t2.resources.find(
        (y) => y.kind === "Postgres" && y.name === name,
      );
      if (!x) return;
      x.phase = "Ready";
      if (body.to === "dedicated") {
        x.details = {
          type: "dedicated",
          version: x.details?.version ?? "17",
          size: body.size ?? all2.sizes.postgres.default,
          storage: body.storage ?? "5Gi",
          instances: "1",
        };
        x.message = `PostgreSQL ${x.details.version}, 1/1 instance(s) ready`;
        x.endpoint = `${name}-rw.p-${slug}:5432`;
      } else {
        x.details = {
          type: "shared",
          version: "17",
          storage: cap,
          used: "0",
          usedBytes: "0",
          capBytes: String(1 << 30),
          pooler: "off",
          backups: "nightly, kept 7",
        };
        x.message = "PostgreSQL 17, shared, on pg-shared-2";
        x.endpoint = `${name}.p-${slug}:5432`;
      }
      await thingsOf.set(all2);
    }, 13000);
    return { name, from, to: body.to, note };
  },
  removeResource: async (slug, kind, name, force) => {
    const [all, t] = await ofProject(slug);
    const r = t.resources.find((x) => x.kind === kind && x.name === name);
    if (r?.attachedTo.length && !force)
      throw new ApiError(
        409,
        `${kind} ${name} is attached to ${r.attachedTo.join(", ")}`,
      );
    t.resources = t.resources.filter((x) => x.kind !== kind || x.name !== name);
    await thingsOf.set(all);
  },
  attach: async (slug, body) => {
    const [all, t] = await ofProject(slug);
    const r = t.resources.find(
      (x) => x.kind === body.kind && x.name === body.name,
    );
    if (!r) throw new ApiError(404, `no ${body.kind} named ${body.name}`);
    if (!r.attachedTo.includes(slug)) r.attachedTo.push(slug);
    await thingsOf.set(all);
    const p = await projectsOf.find(slug);
    p.spec.bindings = [
      ...(p.spec.bindings ?? []).filter(
        (b) => b.kind !== body.kind || b.name !== body.name,
      ),
      { kind: body.kind, name: body.name },
    ];
    release(
      p,
      "config",
      `${body.kind} ${body.name} attached`,
      p.status.releases.at(-1)?.build,
    );
    await projectsOf.set(p);
  },
  detach: async (slug, kind, name) => {
    const [all, t] = await ofProject(slug);
    const r = t.resources.find((x) => x.kind === kind && x.name === name);
    if (r) r.attachedTo = r.attachedTo.filter((s) => s !== slug);
    await thingsOf.set(all);
    const p = await projectsOf.find(slug);
    p.spec.bindings = (p.spec.bindings ?? []).filter(
      (b) => b.kind !== kind || b.name !== name,
    );
    release(
      p,
      "config",
      `${kind} ${name} detached`,
      p.status.releases.at(-1)?.build,
    );
    await projectsOf.set(p);
  },
  volumes: async (slug) => (await ofProject(slug))[1].volumes,
  projectUsage: async (slug) => {
    await wait();
    const [, t] = await ofProject(slug);
    const ws = await currentWorkspace();
    const own = ws.projectCaps?.[slug]?.storage;
    return {
      storage: `${projectStorageOf(t)}Gi`,
      ceiling: own ?? ws.limits?.storage,
      own: !!own,
    };
  },
  createVolume: async (slug, body) => {
    const [all, t] = await ofProject(slug);
    if (t.volumes.some((v) => v.name === body.name))
      throw new ApiError(409, `a volume named ${body.name} exists`);
    await admitDisk(slug, t, gib(body.size), true);
    const v: VolumeInfo = {
      name: body.name,
      size: body.size,
      shared: !!body.shared,
      phase: "Bound",
      mountedBy: [],
      createdAt: now(),
    };
    t.volumes.push(v);
    t.resources.push({
      kind: "Volume",
      name: body.name,
      phase: "Bound",
      details: { size: body.size, mode: body.shared ? "shared" : "single" },
      attachedTo: [],
      data: true,
      createdAt: now(),
    });
    await thingsOf.set(all);
    return v;
  },
  resizeVolume: async (slug, name, size) => {
    const [all, t] = await ofProject(slug);
    const v = t.volumes.find((x) => x.name === name);
    if (!v) throw new ApiError(404, `no volume named ${name}`);
    if (gib(size) < gib(v.size))
      throw new ApiError(400, `volumes cannot shrink (currently ${v.size})`);
    await admitDisk(slug, t, gib(size) - gib(v.size), false);
    v.size = size;
    v.resize = {
      phase: "pending",
      to: size,
      message: "the provider is growing the disk",
    };
    const r = t.resources.find((x) => x.kind === "Volume" && x.name === name);
    if (r) r.details = { ...r.details, size };
    await thingsOf.set(all);
    setTimeout(async () => {
      const [all2, t2] = await ofProject(slug);
      const grown = t2.volumes.find((x) => x.name === name);
      if (grown) {
        grown.capacity = size;
        grown.resize = { phase: "completed", to: size };
      }
      await thingsOf.set(all2);
    }, 6000);
    return v;
  },
  removeVolume: async (slug, name, force) => {
    const [all, t] = await ofProject(slug);
    const v = t.volumes.find((x) => x.name === name);
    if (v?.mountedBy.length && !force)
      throw new ApiError(
        409,
        `${name} is mounted by ${v.mountedBy.join(", ")}`,
      );
    t.volumes = t.volumes.filter((x) => x.name !== name);
    t.resources = t.resources.filter(
      (x) => x.kind !== "Volume" || x.name !== name,
    );
    await thingsOf.set(all);
  },
  snapshots: async (slug, volume) =>
    (await ofProject(slug))[1].snapshots.filter((s) => s.volume === volume),
  createSnapshot: async (slug, volume, name) => {
    const [all, t] = await ofProject(slug);
    const s: SnapshotInfo = {
      name:
        name ||
        `${volume}-${new Date().toISOString().slice(0, 16).replace(/[-:T]/g, "")}`,
      volume,
      size: t.volumes.find((v) => v.name === volume)?.size,
      ready: false,
      createdAt: now(),
    };
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
    t.snapshots = t.snapshots.filter(
      (s) => s.volume !== volume || s.name !== snapshot,
    );
    await thingsOf.set(all);
  },
  restoreVolume: async (slug, volume, body) => {
    const [all, t] = await ofProject(slug);
    const v = t.volumes.find((x) => x.name === volume);
    if (!v) throw new ApiError(404, `no volume named ${volume}`);
    if (body.to) {
      const made: VolumeInfo = {
        ...v,
        name: body.to,
        mountedBy: [],
        restoredFrom: body.snapshot,
        createdAt: now(),
      };
      t.volumes.push(made);
      t.resources.push({
        kind: "Volume",
        name: body.to,
        phase: "Bound",
        details: {
          size: v.size,
          mode: v.shared ? "shared" : "single",
          restoredFrom: body.snapshot,
        },
        attachedTo: [],
        data: true,
        createdAt: now(),
      });
      await thingsOf.set(all);
      return {
        volume: made,
        inPlace: false,
        message: `${body.to} was made from ${body.snapshot}`,
      };
    }
    v.restoredFrom = body.snapshot;
    await thingsOf.set(all);
    return {
      volume: v,
      inPlace: true,
      message: `${volume} was restored from ${body.snapshot}`,
    };
  },
  domains: async (slug) => {
    const data = (await ofProject(slug))[1].domains;
    // Older localStorage fixtures predate redirect eligibility.
    for (const d of data.domains)
      d.redirectEligible ??= d.dns === "ok" && d.certificate === "ready";
    return data;
  },
  setDomainRedirect: async (slug, redirectTo) => {
    const [all, p] = await ofProject(slug);
    if (
      redirectTo &&
      !p.domains.domains.some(
        (d) =>
          d.host === redirectTo &&
          (d.redirectEligible ?? (d.dns === "ok" && d.certificate === "ready")),
      )
    )
      throw new ApiError(
        400,
        "Choose a project domain whose DNS and certificate are ready.",
      );
    p.domains.redirectTo = redirectTo;
    await thingsOf.set(all);
    return p.domains;
  },
  addDomain: async (slug, host) => {
    const [all, p] = await ofProject(slug);
    const record = recordFor(host, p.domains.target);
    const domain: DomainStatus = {
      host,
      dns: "missing",
      target: p.domains.target,
      certificate: "issuing",
      record,
      message: `create a DNS record: ${record.type} ${host} -> ${record.value}`,
    };
    p.domains.domains.push(domain);
    await thingsOf.set(all);
    return domain;
  },
  removeDomain: async (slug, host) => {
    const [all, p] = await ofProject(slug);
    p.domains.domains = p.domains.domains.filter((d) => d.host !== host);
    if (p.domains.redirectTo === host) p.domains.redirectTo = "";
    await thingsOf.set(all);
  },
  workspaceDrains: async () => (await thingsOf.get()).workspaceDrains,
  addWorkspaceDrain: async (body) => {
    const all = await thingsOf.get();
    const name =
      body.name ||
      new URL(body.url.replace(/^syslog(\+tls)?:/, "https:")).hostname.replace(
        /\./g,
        "-",
      );
    if (all.workspaceDrains.some((d) => d.name === name))
      throw new ApiError(409, `a drain named ${name} exists`);
    const drain: Drain = {
      name,
      url: body.url,
      format:
        body.format ?? (body.url.startsWith("syslog") ? "syslog" : "json"),
      processes: body.processes,
      headers: Object.keys(body.headers ?? {}),
      workspace: "acme",
      phase: "Pending",
      sent: 0,
      errors: 0,
      createdAt: now(),
    };
    all.workspaceDrains.push(drain);
    await thingsOf.set(all);
    return drain;
  },
  removeWorkspaceDrain: async (name) => {
    const all = await thingsOf.get();
    all.workspaceDrains = all.workspaceDrains.filter((d) => d.name !== name);
    await thingsOf.set(all);
  },
  drains: async (slug) => (await ofProject(slug))[1].drains,
  addDrain: async (slug, body) => {
    const [all, p] = await ofProject(slug);
    const name =
      body.name ||
      new URL(body.url.replace(/^syslog(\+tls)?:/, "https:")).hostname.split(
        ".",
      )[0] ||
      "drain";
    const drain: Drain = {
      name,
      url: body.url,
      format:
        body.format ?? (body.url.startsWith("syslog") ? "syslog" : "json"),
      processes: body.processes,
      headers: Object.keys(body.headers ?? {}),
      phase: "Pending",
      sent: 0,
      errors: 0,
      createdAt: now(),
    };
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
    for (const line of logsOf(slug, processes))
      if (!query.process || line.instance?.startsWith(query.process + "-"))
        onLine(line);
    if (!query.follow) return;
    let n = 0;
    await until(signal, 2000, () =>
      onLine(liveLine(slug, query.process ?? processes[0] ?? "web", n++)),
    );
  },
  metrics: async (slug, range) => {
    await wait();
    const p = await projectsOf.find(slug);
    return metricsOf(slug, Object.keys(p.spec.processes ?? { web: {} }), range);
  },
  instances: async (slug) => {
    await wait();
    const p = await projectsOf.find(slug);
    return Object.entries(p.processes ?? {}).flatMap(([name, st]) =>
      Array.from({ length: Math.max(st.desired, 1) }, (_, i) => ({
        name: `${name}-${i + 1}`,
        process: name,
        pod: `${slug}-${name}-${id().slice(0, 5)}`,
        ready: i < st.ready,
      })),
    );
  },
  shellTicket: async () => {
    await wait();
    return { ticket: "mock" };
  },
  shellSocket: async () => null,
};
