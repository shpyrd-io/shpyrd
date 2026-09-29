import { getToken, setToken } from "./auth";

// ---- types mirroring pkg/api ----------------------------------------------

export type PublicConfig = {
  version: string;
  domain: string;
  httpsPort: string;
  grafanaUrl: string;
  dashboardUrl?: string;
  authRequired: boolean;
  metrics: boolean;
  auth: {
    token: boolean;
    /** Sign-in buttons (external providers); kind picks the icon. */
    providers: {
      id: string;
      label: string;
      kind?: string;
      /** The door the method belongs to (RFC-0080): console, platform or workspace. */
      realm?: string;
      workspace?: string;
    }[];
    /** Provider behind the email/password form, when one is enabled. */
    password?: { id: string; label: string; realm?: string };
    /** The workspace claimed email domains that route to a method: ask for the email first. */
    companyDomains?: boolean;
  };
  extensions: string[];
  /** What this server offers beyond the core ("workspaces", ...); empty on the open-source platform. */
  capabilities?: string[];
  /** The workspace answering at this host, with its look (RFC-0033); absent at the console. */
  workspace?: {
    slug: string;
    name: string;
    /** Where its dashboard answers; apps one label under (RFC-0080). */
    address?: string;
    /** One of the platform operator's own workspaces (RFC-0078). */
    ownedByOperator?: boolean;
    branding?: { logoUrl?: string; color?: string };
  };
  /** Which application answers at this host (RFC-0080): "console" or "workspace". */
  door?: "console" | "workspace";
  /** The console's URL: the way back from an operator workspace. */
  consoleUrl?: string;
  /** Slug of the operator's default workspace (RFC-0078). */
  defaultWorkspaceId?: string;
  consoleHost?: string;
  /** Storage rules of this cluster's profile (RFC-0060). */
  volumes?: { minSize?: string; snapshots: boolean };
};

export type Identity = {
  subject: string;
  email?: string;
  name?: string;
  groups?: string[];
  provider: string;
  admin: boolean;
  /** A platform admin by the console's roles (RFC-0080): an operator workspace shows the way to the console. */
  console?: boolean;
  roles?: {
    /** The person's role in the workspace (RFC-0033); absent without one. */
    workspace?: WorkspaceRole | "";
    platform?: "platform-admin" | "platform-viewer" | "";
    projects?: Record<
      string,
      "reader" | "user" | "viewer" | "developer" | "admin"
    >;
    enforced: boolean;
  };
};

/** Workspace roles (RFC-0033): owners name owners; admins administer; members create projects. */
export type WorkspaceRole = "owner" | "admin" | "member";
export const WORKSPACE_ROLES: WorkspaceRole[] = ["owner", "admin", "member"];

/** The workspace (RFC-0033): the tenant every project belongs to. */
export type WorkspaceInfo = {
  slug: string;
  name: string;
  /** One of the platform operator's own workspaces (RFC-0078, RFC-0080). */
  ownedByOperator: boolean;
  /** Apps live one label under it. */
  domain?: string;
  /** Host of the workspace's dashboard (RFC-0080: every workspace has one). */
  address?: string;
  /** Where this workspace's dashboard answers. */
  url?: string;
  status?: "active" | "suspended";
  /** The workspace's plan (ceilings) and what it uses today; absent without a plan. */
  limits?: {
    projects?: number;
    instances?: number;
    cpu?: string;
    memory?: string;
    storage?: string;
  };
  usage?: {
    projects: number;
    instances: number;
    cpu: string;
    memory: string;
    storage: string;
  };
  joinPolicy: "open" | "company" | "listed";
  /** Only the workspace's own sign-in methods are offered (not the platform's). */
  ownMethodsOnly: boolean;
  /** The workspace's look: logo URL and accent colour, when set. */
  branding?: { logoUrl?: string; color?: string };
  /** The MCP server assistants connect to (RFC-0032). */
  mcpName: string;
  mcpUrl: string;
  /** Emails of the workspace's owners. */
  owners: string[];
  createdAt: string;
  updatedAt: string;
};

/** A claimed email domain (RFC-0033): prove it with the TXT record. */
export type DomainClaim = {
  domain: string;
  connector?: string;
  verified: boolean;
  verifiedAt?: string;
  record: string;
  recordValue: string;
};

/** Login methods: Dex connectors plus the local password method. */
export type LoginMethods = {
  password: boolean;
  connectors: {
    id: string;
    type: string;
    name: string;
    detail?: string;
    workspace?: string;
  }[];
  kinds: string[];
  callback: string;
  /** The door these methods belong to (RFC-0080): console, platform or workspace. */
  realm?: string;
  /** The workspace (short id) whose own methods these are. */
  workspace?: string;
};

/** One workspace as the console lists them (RFC-0033 phase 8). */
export type WorkspaceSummary = {
  slug: string;
  name: string;
  address?: string;
  url: string;
  status: string;
  /** "operator" or "customer" (RFC-0078). */
  owner?: string;
  /** Billing plan the workspace is metered against (RFC-0075); absent when none, never for the operator's own. */
  plan?: string;
  limits?: WorkspaceInfo["limits"];
  usage?: WorkspaceInfo["usage"];
  owners: string[];
  createdAt: string;
};

export type ConnectorRequest = {
  type: string;
  id?: string;
  name?: string;
  clientId: string;
  clientSecret: string;
  org?: string;
  hostedDomain?: string;
  tenant?: string;
  issuer?: string;
};

/** A person of the workspace: signed in, or holding a role before signing in. */
export type Person = {
  email: string;
  name?: string;
  provider?: string;
  groups: string[];
  realm?: string;
  status: "active" | "suspended";
  /** Workspace role (RFC-0033); absent without one. */
  role?: WorkspaceRole;
  /** What a team gives them when they have no workspace role (the older way). */
  platformRole?: string;
  /** Absent until the person signs in for the first time. */
  firstSeenAt?: string;
  lastSeenAt?: string;
};

/** A pending invitation (RFC-0033); the link is never listed. */
export type Invitation = {
  id: string;
  email: string;
  role: WorkspaceRole;
  team?: string;
  invitedBy?: string;
  createdAt: string;
  expiresAt: string;
  expired: boolean;
};

/** What inviting produced: a link (shown once), or the role applied to someone known. */
export type InviteResult = {
  email: string;
  role: WorkspaceRole;
  team?: string;
  applied: boolean;
  invitation?: Invitation;
  link?: string;
  emailed: boolean;
  mailError?: string;
};

/** What the holder of an invitation link sees (public). */
export type InvitationPublic = {
  workspace: { slug: string; name: string; address?: string; ownedByOperator?: boolean };
  email: string;
  role: WorkspaceRole;
  team?: string;
  invitedBy?: string;
  expiresAt: string;
  expired: boolean;
  url: string;
};

/** RFC-0075: one five-minute usage bucket. */
export type UsageBucket = {
  workspaceId: string;
  project: string;
  component: string;
  metric: string;
  periodStart: string;
  periodEnd: string;
  quantity: number | null;
  unit: string;
  quality: "complete" | "partial" | "missing";
};

/** RFC-0075: a billing line in the month-to-date preview. */
export type BillingLine = {
  /** Project slug; the plan's minimum line has none. */
  project?: string;
  component: string;
  metric: string;
  quantity: number;
  unit: string;
  unitPrice: number;
  grossAmount: number;
};

/** RFC-0075: the month-to-date invoice preview. */
export type WorkspaceBillingView = {
  workspace: string;
  period: string;
  plan?: {
    name: string;
    currency: string;
    cpuHour: number;
    memoryGibHour: number;
    storageGibMonth: number;
    egressGib: number;
    minMonthly: number;
  };
  lines: BillingLine[];
  total: number;
  currency: string;
  projection: number;
  quality: string;
};

/** A custom domain of the workspace (RFC-0033 names), with the DNS records to publish. */
export type WorkspaceDomain = {
  host: string;
  verified: boolean;
  verifiedAt?: string;
  primary: boolean;
  records: { type: string; name: string; value: string }[];
  url: string;
};

/** An assistant a person connected through OAuth (RFC-0032). */
export type Connection = {
  id: string;
  client: string;
  clientId: string;
  email: string;
  scope: string;
  createdAt: string;
  expiresAt: string;
  lastUsedAt?: string;
};

/** RFC-0075: one workspace's economics row. */
export type EconomicsRow = {
  workspace?: string;
  /** "operator" or "customer" (RFC-0078): the operator's workspaces are expenses, never revenue. */
  owner?: string;
  revenue: number;
  directCogs: number;
  sharedCogs: number;
  idleCogs: number;
  totalCogs: number;
  grossMargin: number;
  marginPct: number;
};

/** The mail extension's status (RFC-0013): never the password. */
export type MailStatus = {
  configured: boolean;
  host?: string;
  port?: number;
  from?: string;
  security?: string;
  auth: boolean;
};

export type Team = {
  name: string;
  description?: string;
  members: string[];
  groups: string[];
  platformRole?: string;
  /** The built-in team of every person who signed in. */
  everyone?: boolean;
};

export type Member = {
  name: string;
  project: string;
  role: "reader" | "user" | "viewer" | "developer" | "admin";
  user?: string;
  team?: string;
};

export type AuditEntry = {
  time: string;
  actor: string;
  action: string;
  target?: string;
  detail?: string;
  from?: string;
  via: string;
  /** Where the actor's identity lives: workspace, operator (RFC-0033). */
  realm?: string;
};

export type LocalUser = { email: string; name?: string; createdAt: string };

export type ExtensionInfo = {
  name: string;
  description: string;
  enabled: boolean;
  component?: string;
};

export type ProcessStatus = {
  desired: number;
  ready: number;
  updated?: number;
  failing?: number;
  reason?: string;
  size?: string;
  cpu?: string;
  memory?: string;
  pinned?: string;
  /** RFC-0075: present when the process has a sleep policy. */
  sleep?: { state: string; message?: string };
};

export type VolumeInfo = {
  name: string;
  namespace: string;
  size: string;
  capacity?: string;
  shared: boolean;
  storageClass?: string;
  phase: "Pending" | "Bound" | "Failed" | "Restoring" | string;
  message?: string;
  mountedBy: string[];
  createdAt: string;
  /** Snapshot the current disk was restored from, if any. */
  restoredFrom?: string;
  /** Provider rule applied at creation (a size rounded up, for example). */
  note?: string;
};

export type SnapshotInfo = {
  name: string;
  volume: string;
  size?: string;
  ready: boolean;
  message?: string;
  createdAt: string;
};

export type RestoreVolumeResult = {
  volume: VolumeInfo;
  inPlace: boolean;
  message: string;
};

export type InstanceSize = {
  name: string;
  kind: "shared" | "dedicated";
  cpu: string;
  memory: string;
  description?: string;
};
export type SizeCatalog = { default: string; sizes: InstanceSize[] };

export type AppSummary = {
  /** Identifier used in URLs, the CLI and the hostname. */
  slug: string;
  /** Human name; equals the slug when none was given. */
  displayName: string;
  /** The launcher's one line under the name, and whether it is shown first. */
  description?: string;
  featured?: boolean;
  namespace: string;
  phase: string;
  message?: string;
  url?: string;
  digest?: string;
  release: number;
  source?: string;
  processes?: Record<string, ProcessStatus>;
  createdAt: string;
  /** "external" (public LB, default) or "internal" (private LB, RFC-0036). */
  exposure?: "external" | "internal";
  /** Who may open the app (RFC-0033). */
  access: "public" | "authenticated" | "identified";
  allow?: AllowEntry[];
};

export type Release = {
  number: number;
  digest: string;
  build?: number;
  source?: string;
  description?: string;
  createdAt: string;
  processes?: string[];
  kind: "deploy" | "config" | "rollback";
};

export type Condition = {
  type: string;
  status: string;
  reason?: string;
  message?: string;
  lastTransitionTime: string;
};

export type ProcessSpec = {
  replicas?: number;
  port?: number;
  command?: string[];
  args?: string[];
  size?: string;
  volumes?: { name: string; path: string }[];
};

export type AppDetail = {
  slug: string;
  displayName: string;
  description?: string;
  featured?: boolean;
  namespace: string;
  createdAt: string;
  spec: {
    source?: {
      git?: { url: string; revision?: string };
      blob?: { sha256?: string; ref?: string };
      subPath?: string;
    };
    pinnedDigest?: string;
    processes?: Record<string, ProcessSpec>;
    env?: { name: string; value?: string }[];
    domains?: string[];
    bindings?: { kind: string; name: string; prefix?: string }[];
    exposure?: "external" | "internal";
    access?: "public" | "authenticated" | "identified";
    build?: {
      strategy?: "buildpacks" | "dockerfile";
      env?: { name: string; value?: string }[];
      builder?: string;
      dockerfile?: string;
      target?: string;
    };
  };
  status: {
    phase: string;
    message?: string;
    digest?: string;
    url?: string;
    latestBuild?: string;
    releases: Release[];
    conditions?: Condition[];
    /** Custom domains' DNS and certificate state (RFC-0034). */
    domains?: DomainStatus[];
    /** The image's process types (RFC-0066); "release" runs before every rollout. */
    processTypes?: string[];
    /** The release command's run for the release rolling out (RFC-0066). */
    release?: ReleasePhaseStatus;
  };
  processes?: Record<string, ProcessStatus>;
};

export type ReleasePhaseStatus = {
  target: string;
  state: "Running" | "Succeeded" | "Failed";
  message?: string;
  job?: string;
};

export type DomainStatus = {
  host: string;
  dns: "ok" | "missing" | "wrong" | "unknown";
  target?: string;
  address?: string;
  certificate: "ready" | "issuing" | "failed" | "wildcard";
  message?: string;
};

export type DomainsResult = {
  host?: string;
  target: string;
  address?: string;
  domains: DomainStatus[];
  app: AppSummary;
};

export type Point = [number, number];
export type Series = {
  name: string;
  points: Point[];
  /** What this series is measured against in absolute mode (its allocation). */
  reference?: number;
  /** The burst ceiling above the allocation, for shared sizes that allow it. */
  burst?: number;
};
export type Chart = {
  id: string;
  title: string;
  unit: "rps" | "ms" | "cores" | "bytes" | "count" | "bytes/s" | string;
  kind: "line" | "stacked" | "step";
  series: Series[];
  error?: string;
  /** Whether `by=instance` grouping applies to this chart. */
  instanceCapable: boolean;
  /** Set when the chart hid data, e.g. replaced instances or a capped series. */
  note?: string;
};
export type MetricsResponse = {
  range: string;
  step: number;
  charts: Chart[];
  releases: { number: number; time: number; label: string }[];
};

export type MetricsQuery = {
  range: string;
  process?: string;
  by?: "process" | "instance";
  agg?: "none" | "sum" | "avg" | "max";
  mode?: "percent" | "total";
  replaced?: boolean;
};

export type BuildInfo = {
  name: string;
  number: number;
  strategy: "buildpacks" | "dockerfile";
  status: "Building" | "Succeeded" | "Failed";
  reason?: string;
  message?: string;
  digest?: string;
  source?: string;
  startedAt: string;
  completedAt?: string;
  stepsCompleted?: string[];
};

export type ConfigVar = { name: string; updatedAt?: string };
export type GlobalsResponse = { vars: ConfigVar[]; projects: number };

/** A log drain (RFC-0023): header names only, never values. */
export type Drain = {
  name: string;
  url: string;
  format: "json" | "syslog";
  processes?: string[];
  headers?: string[];
  cluster: boolean;
  phase: "Pending" | "Active" | "Failing";
  message?: string;
  lastDeliveryAt?: string;
  sent: number;
  errors: number;
  createdAt: string;
};
export type CreateDrain = {
  name?: string;
  url: string;
  format?: "json" | "syslog";
  headers?: Record<string, string>;
  processes?: string[];
};
export type BoundVar = { name: string; provider: string };

export type ResourceInfo = {
  kind: string;
  name: string;
  phase: string;
  message?: string;
  endpoint?: string;
  details?: Record<string, string>;
  attachedTo: string[];
  data: boolean;
  bindable?: boolean;
  createdAt: string;
};

export type LogLine = { t?: string; i: string; p: string; m: string };

export type ClusterSummary = {
  install?: {
    profile: string;
    version: string;
    domain: string;
    updatedAt: string;
  };
  components: { name: string; version?: string; appliedAt: string }[];
  extensions: ExtensionInfo[];
  nodes: {
    name: string;
    ready: boolean;
    roles: string;
    arch: string;
    os: string;
    kubeletVersion: string;
    cpu: string;
    memory: string;
    pods: string;
    instanceType?: string;
    zone?: string;
  }[];
  apps: number;
  phases: Record<string, number>;
  externalLBAddress?: string;
  internalLBAddress?: string;
};

export type NodeUsage = {
  name: string;
  cpuUsedPct: number;
  memoryUsedPct: number;
  cpuRequestedPct: number;
  memoryRequestedPct: number;
  cpuCores: number;
  memoryBytes: number;
  pods: number;
  podCapacity: number;
};

export type ClusterMetrics = {
  range: string;
  nodes: NodeUsage[];
  total: NodeUsage;
  charts: Chart[];
};

// The image registry (RFC-0059).
/** The platform's object store (RFC-0046). */
/** One entry in an app's allow list (RFC-0033 phase 5). */
export type AllowEntry = {
  project?: string;
  platform?: "actions" | "mcp";
};

/** A personal API token (RFC-0031). */
export type APIToken = {
  id: string;
  name: string;
  ownerEmail?: string;
  platformRole?: string;
  projectRoles?: Record<string, string>;
  createdAt: string;
  expiresAt?: string;
  lastUsedAt?: string;
};

/** One tile of the launcher: an app the caller may open (RFC-0033). */
export type LauncherApp = {
  slug: string;
  displayName: string;
  description?: string;
  featured?: boolean;
  url?: string;
  access: "public" | "authenticated" | "identified";
  phase: string;
  role?: string;
};

/** Platform backups (RFC-0037): the target, the schedule, the archives. */
export type BackupInfo = {
  enabled: boolean;
  target?: string;
  endpoint?: string;
  schedule?: string;
  keep?: number;
  accessKey: boolean;
  lastScheduled?: string;
  lastSuccessful?: string;
  runs: BackupRun[];
  archives: BackupArchive[];
  error?: string;
};

export type BackupRun = {
  name: string;
  status: "running" | "succeeded" | "failed";
  started?: string;
  finished?: string;
  message?: string;
};

export type BackupArchive = {
  name: string;
  size: number;
  modified: string;
};

export type ObjectStorageSummary = {
  endpoint: string;
  totalBytes: number;
  usedBytes: number;
  measuredAt?: string;
  message?: string;
  buckets: {
    namespace: string;
    name: string;
    bucket: string;
    phase: string;
    message?: string;
    usedBytes: number;
    objects: number;
    retentionDays?: number;
  }[];
};

export type RegistryInfo = {
  mode: "in-cluster" | "external";
  host: string;
  tls: boolean;
  ready: boolean;
  message?: string;
  storage?: { usedBytes: number; capacityBytes: number; size: string };
  images?: {
    repositories: number;
    tags: number;
    largest: { name: string; tags: number }[];
    error?: string;
  };
  certificate?: { issuer: string; notAfter: string };
  gc?: {
    schedule: string;
    nextRun?: string;
    running: boolean;
    startedAt?: string;
    lastRun?: string;
    lastResult?: string;
    lastDuration?: string;
    reclaimedBytes: number;
    usedBytes: number;
  };
};

/** A running instance of a project (RFC-0026). */
export type Instance = {
  name: string;
  process: string;
  pod: string;
  ready: boolean;
};

export type HelmRelease = {
  name: string;
  namespace: string;
  chart: string;
  chartVersion: string;
  appVersion?: string;
  status: string;
  revision: number;
  updatedAt: string;
};

// ---- client -----------------------------------------------------------------

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

function headers(extra?: HeadersInit): Headers {
  const h = new Headers(extra);
  const tok = getToken();
  if (tok) h.set("Authorization", `Bearer ${tok}`);
  // Cookie sessions prove intent with the CSRF cookie echoed as a header.
  const csrf = readCookie("shpyrd_csrf");
  if (csrf) h.set("X-Shpyrd-CSRF", csrf);
  return h;
}

function readCookie(name: string): string | null {
  const m = document.cookie.match(new RegExp("(?:^|; )" + name + "=([^;]*)"));
  return m ? decodeURIComponent(m[1]) : null;
}

async function handle(res: Response): Promise<Response> {
  if (res.status === 401) {
    setToken(null);
    throw new ApiError(401, "unauthorized");
  }
  if (!res.ok) {
    let msg = `${res.status} ${res.statusText}`;
    try {
      const body = await res.json();
      if (body?.error) msg = body.error;
    } catch {
      // not json
    }
    throw new ApiError(res.status, msg);
  }
  return res;
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const res = await handle(
    await fetch(path, { ...init, headers: headers(init.headers) }),
  );
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

function json(method: string, body: unknown): RequestInit {
  return {
    method,
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  };
}

/** Streams a text response line by line until aborted or finished. */
export async function apiStream(
  path: string,
  signal: AbortSignal,
  onLine: (line: string) => void,
): Promise<void> {
  const res = await handle(await fetch(path, { headers: headers(), signal }));
  const reader = res.body?.getReader();
  if (!reader) return;
  const dec = new TextDecoder();
  let buf = "";
  for (;;) {
    const { value, done } = await reader.read();
    if (done) break;
    buf += dec.decode(value, { stream: true });
    let i: number;
    while ((i = buf.indexOf("\n")) >= 0) {
      onLine(buf.slice(0, i));
      buf = buf.slice(i + 1);
    }
  }
  if (buf) onLine(buf);
}

/** Base path of a project: the server derives the namespace from the slug. */
const project = (slug: string) => `/api/projects/${encodeURIComponent(slug)}`;

/** WebSocket URL of a shell session on an instance. */
export function shellSocketURL(
  slug: string,
  instance: string,
  ticket: string,
): string {
  const proto = location.protocol === "https:" ? "wss:" : "ws:";
  const q = `instance=${encodeURIComponent(instance)}&ticket=${encodeURIComponent(ticket)}`;
  return `${proto}//${location.host}${project(slug)}/shell?${q}`;
}

export const api = {
  config: () => request<PublicConfig>("/api/config"),
  me: () => request<Identity>("/api/me"),
  /** The workspaces this platform hosts (the console; RFC-0033 phase 8, RFC-0080). */
  workspaces: () => request<WorkspaceSummary[]>("/api/workspaces"),
  createWorkspace: (body: {
    slug: string;
    name?: string;
    address?: string;
    owner?: string;
    operatorOwned?: boolean;
    /** Billing plan name; refused for operator workspaces. */
    plan?: string;
  }) => request<WorkspaceSummary>("/api/workspaces", json("POST", body)),
  /** Billing plans (RFC-0075; cluster admins). */
  plans: () =>
    request<{ id: string; name: string; currency: string; minMonthly: number }[]>(
      "/api/cluster/plans",
    ),
  /** Cluster-wide settings (RFC-0078, RFC-0080). */
  patchClusterSettings: (body: {
    defaultWorkspaceId?: string;
    consolePasswordSignIn?: boolean;
  }) =>
    request<{ defaultWorkspaceId: string; consolePasswordSignIn: boolean }>(
      "/api/cluster/settings",
      json("PATCH", body),
    ),
  logout: () =>
    request<{ redirect: string }>("/api/auth/logout", { method: "POST" }),
  /** Email/password sign-in on our own page (RFC-0012). Errors keep the
   * server's message: a wrong password is a 401 like any other, but here
   * it must not be mistaken for an expired session. */
  passwordLogin: async (body: {
    email: string;
    password: string;
    next?: string;
  }): Promise<{ next: string }> => {
    const res = await fetch("/api/auth/password", json("POST", body));
    if (!res.ok) {
      let msg = `${res.status} ${res.statusText}`;
      try {
        const b = await res.json();
        if (b?.error) msg = b.error;
      } catch {
        // not json
      }
      throw new ApiError(res.status, msg);
    }
    return (await res.json()) as { next: string };
  },
  tokenLogin: async (body: {
    token: string;
    next?: string;
  }): Promise<{ next: string }> => {
    const res = await fetch("/api/auth/token", json("POST", body));
    if (!res.ok) {
      let msg = `${res.status} ${res.statusText}`;
      try {
        const b = await res.json();
        if (b?.error) msg = b.error;
      } catch {
        // not json
      }
      throw new ApiError(res.status, msg);
    }
    return (await res.json()) as { next: string };
  },
  /** The sign-in method a claimed email domain routes to ("" when none). */
  authRoute: (email: string) =>
    request<{ provider: string; label?: string }>(
      `/api/auth/route?email=${encodeURIComponent(email)}`,
    ),
  loginUrl: (provider: string, next: string) =>
    `/api/auth/login?provider=${encodeURIComponent(provider)}&next=${encodeURIComponent(next)}`,
  workspace: () => request<WorkspaceInfo>("/api/workspace"),
  updateWorkspace: (body: {
    name?: string;
    joinPolicy?: string;
    ownMethodsOnly?: boolean;
    /** A new address label (or host under the same parent); owners only. */
    address?: string;
    /** Branding: a logo data URL ("" removes), an accent colour #rrggbb ("" resets). */
    logo?: string;
    color?: string;
    /** The name assistants show for the MCP server ("" resets). */
    mcpName?: string;
  }) => request<WorkspaceInfo>("/api/workspace", json("PATCH", body)),
  connections: () => request<Connection[]>("/api/workspace/connections"),
  revokeConnection: (id: string) =>
    request<void>(`/api/workspace/connections/${encodeURIComponent(id)}`, {
      method: "DELETE",
    }),
  workspaceDomains: () => request<WorkspaceDomain[]>("/api/workspace/domains"),
  /** Billing (RFC-0075). */
  billingCurrent: () =>
    request<WorkspaceBillingView>("/api/workspace/billing/current"),
  billingUsage: (project?: string, from?: string, to?: string) => {
    const q = new URLSearchParams();
    if (project) q.set("project", project);
    if (from) q.set("from", from);
    if (to) q.set("to", to);
    return request<UsageBucket[]>(`/api/workspace/usage?${q}`);
  },
  addWorkspaceDomain: (host: string) =>
    request<WorkspaceDomain>("/api/workspace/domains", json("POST", { host })),
  verifyWorkspaceDomain: (host: string) =>
    request<WorkspaceDomain>(
      `/api/workspace/domains/${encodeURIComponent(host)}/verify`,
      json("POST", {}),
    ),
  setWorkspaceDomainPrimary: (host: string, primary: boolean) =>
    request<WorkspaceDomain>(
      `/api/workspace/domains/${encodeURIComponent(host)}`,
      json("PATCH", { primary }),
    ),
  removeWorkspaceDomain: (host: string) =>
    request<void>(`/api/workspace/domains/${encodeURIComponent(host)}`, {
      method: "DELETE",
    }),
  domainClaims: () => request<DomainClaim[]>("/api/workspace/domain-claims"),
  claimDomain: (domain: string, connector: string) =>
    request<DomainClaim>(
      "/api/workspace/domain-claims",
      json("POST", { domain, connector }),
    ),
  verifyDomain: (domain: string) =>
    request<DomainClaim>(
      `/api/workspace/domain-claims/${encodeURIComponent(domain)}/verify`,
      { method: "POST" },
    ),
  unclaimDomain: (domain: string) =>
    request<void>(
      `/api/workspace/domain-claims/${encodeURIComponent(domain)}`,
      { method: "DELETE" },
    ),
  loginMethods: () => request<LoginMethods>("/api/auth/connectors"),
  addConnector: (body: ConnectorRequest) =>
    request<{ id: string }>("/api/auth/connectors", json("POST", body)),
  removeConnector: (id: string) =>
    request<void>(`/api/auth/connectors/${encodeURIComponent(id)}`, {
      method: "DELETE",
    }),
  /** A workspace's own sign-in methods (RFC-0033 per-workspace SSO). */
  workspaceLoginMethods: () =>
    request<LoginMethods>("/api/workspace/login-methods"),
  addWorkspaceConnector: (body: ConnectorRequest) =>
    request<{ id: string }>("/api/workspace/login-methods", json("POST", body)),
  removeWorkspaceConnector: (id: string) =>
    request<void>(`/api/workspace/login-methods/${encodeURIComponent(id)}`, {
      method: "DELETE",
    }),
  people: () => request<Person[]>("/api/workspace/people"),
  setPersonStatus: (email: string, status: "active" | "suspended") =>
    request<Person>(
      `/api/workspace/people/${encodeURIComponent(email)}`,
      json("PATCH", { status }),
    ),
  setPersonRole: (email: string, role: WorkspaceRole | "") =>
    request<Person>(
      `/api/workspace/people/${encodeURIComponent(email)}`,
      json("PATCH", { role }),
    ),
  invitations: () => request<Invitation[]>("/api/workspace/invitations"),
  invite: (body: { email: string; role: WorkspaceRole; team?: string }) =>
    request<InviteResult>("/api/workspace/invitations", json("POST", body)),
  revokeInvitation: (id: string) =>
    request<void>(`/api/workspace/invitations/${encodeURIComponent(id)}`, {
      method: "DELETE",
    }),
  /** Public: what a link holder was invited to. */
  invitation: (token: string) =>
    request<InvitationPublic>(`/api/invitations/${encodeURIComponent(token)}`),
  acceptInvitation: (token: string) =>
    request<{ workspace: string; role: WorkspaceRole; next: string }>(
      `/api/invitations/${encodeURIComponent(token)}/accept`,
      json("POST", {}),
    ),
  mailStatus: () => request<MailStatus>("/api/cluster/mail"),
  mailTest: (to: string) =>
    request<{ ok: boolean; to: string; took: string }>(
      "/api/cluster/mail/test",
      json("POST", { to }),
    ),
  /** Operator economics from OpenCost (RFC-0075): never shown to customers. */
  economics: (month?: string) => {
    const q = month ? `?month=${encodeURIComponent(month)}` : "";
    return request<{
      month: string;
      workspaces: EconomicsRow[];
      totals: EconomicsRow;
    }>(`/api/cluster/economics${q}`);
  },
  forgetPerson: (email: string) =>
    request<void>(`/api/workspace/people/${encodeURIComponent(email)}`, {
      method: "DELETE",
    }),
  teams: () => request<Team[]>("/api/teams"),
  putTeam: (body: Team) => request<Team>("/api/teams", json("POST", body)),
  deleteTeam: (name: string) =>
    request<void>(`/api/teams/${encodeURIComponent(name)}`, {
      method: "DELETE",
    }),
  members: (slug: string) => request<Member[]>(`${project(slug)}/members`),
  addMember: (
    slug: string,
    body: { role: string; user?: string; team?: string },
  ) => request<Member>(`${project(slug)}/members`, json("POST", body)),
  removeMember: (slug: string, name: string) =>
    request<void>(`${project(slug)}/members/${encodeURIComponent(name)}`, {
      method: "DELETE",
    }),
  audit: (slug: string, limit = 50) =>
    request<AuditEntry[]>(`${project(slug)}/audit?limit=${limit}`),
  users: () => request<LocalUser[]>("/api/users"),
  createUser: (body: { email: string; name?: string; password: string }) =>
    request<LocalUser>("/api/users", json("POST", body)),
  setUserPassword: (email: string, password: string) =>
    request<void>(
      `/api/users/${encodeURIComponent(email)}/password`,
      json("PUT", { password }),
    ),
  deleteUser: (email: string) =>
    request<void>(`/api/users/${encodeURIComponent(email)}`, {
      method: "DELETE",
    }),
  apps: () => request<AppSummary[]>("/api/projects"),
  createApp: (body: {
    /** Display name, any text; the slug is derived unless given. */
    name: string;
    slug?: string;
    domains?: string[];
    processes?: Record<string, ProcessSpec>;
    git?: { url: string; revision?: string };
    subPath?: string;
  }) => request<AppSummary>("/api/projects", json("POST", body)),
  app: (slug: string) => request<AppDetail>(project(slug)),
  renameApp: (slug: string, name: string) =>
    request<AppDetail>(project(slug), json("PATCH", { name })),
  /** Name, launcher description and featured flag (RFC-0033). */
  updateApp: (
    slug: string,
    body: { name?: string; description?: string; featured?: boolean },
  ) => request<AppDetail>(project(slug), json("PATCH", body)),
  deleteApp: (slug: string) =>
    request<{ status: string }>(project(slug), { method: "DELETE" }),
  deploy: (
    slug: string,
    body: {
      git?: { url: string; revision?: string };
      subPath?: string;
      image?: string;
      strategy?: "buildpacks" | "dockerfile";
      dockerfile?: string;
    },
  ) => request<AppSummary>(`${project(slug)}/deploy`, json("POST", body)),
  configVars: (slug: string) =>
    request<{ vars: ConfigVar[]; bound?: BoundVar[]; global?: ConfigVar[] }>(
      `${project(slug)}/secrets`,
    ),
  /** Log drains (RFC-0023): project scope and cluster scope. */
  drains: (slug: string) => request<Drain[]>(`${project(slug)}/drains`),
  createDrain: (slug: string, body: CreateDrain) =>
    request<Drain>(`${project(slug)}/drains`, json("POST", body)),
  deleteDrain: (slug: string, name: string) =>
    request<void>(`${project(slug)}/drains/${encodeURIComponent(name)}`, {
      method: "DELETE",
    }),
  clusterDrains: () => request<Drain[]>("/api/drains"),
  createClusterDrain: (body: CreateDrain) =>
    request<Drain>("/api/drains", json("POST", body)),
  deleteClusterDrain: (name: string) =>
    request<void>(`/api/drains/${encodeURIComponent(name)}`, {
      method: "DELETE",
    }),
  /** Global config vars (RFC-0016): names only, and how many projects get them. */
  globals: () => request<GlobalsResponse>("/api/globals"),
  updateGlobals: (body: {
    set?: Record<string, string>;
    unset?: string[];
    dotenv?: string;
  }) => request<GlobalsResponse>("/api/globals", json("PUT", body)),
  updateConfigVars: (
    slug: string,
    body: { set?: Record<string, string>; unset?: string[]; dotenv?: string },
  ) =>
    request<{ vars: ConfigVar[] }>(
      `${project(slug)}/secrets`,
      json("PUT", body),
    ),
  metrics: (slug: string, q: MetricsQuery) => {
    const p = new URLSearchParams({ range: q.range });
    if (q.process) p.set("process", q.process);
    if (q.by) p.set("by", q.by);
    if (q.agg && q.agg !== "none") p.set("agg", q.agg);
    if (q.mode) p.set("mode", q.mode);
    if (q.replaced) p.set("replaced", "true");
    return request<MetricsResponse>(`${project(slug)}/metrics?${p}`);
  },
  builds: (slug: string) => request<BuildInfo[]>(`${project(slug)}/builds`),
  buildLogsPath: (slug: string, build: string, follow: boolean) =>
    `${project(slug)}/builds/${encodeURIComponent(build)}/logs?follow=${follow}`,
  logsPath: (
    slug: string,
    q: { process?: string; tail?: number; follow?: boolean },
  ) => {
    const p = new URLSearchParams({
      format: "json",
      tail: String(q.tail ?? 200),
      follow: q.follow ? "true" : "false",
    });
    if (q.process) p.set("process", q.process);
    return `${project(slug)}/logs?${p}`;
  },
  scale: (slug: string, process: string, replicas: number) =>
    request<AppSummary>(
      `${project(slug)}/scale`,
      json("POST", { process, replicas }),
    ),
  resize: (slug: string, process: string, size: string) =>
    request<AppSummary>(
      `${project(slug)}/resize`,
      json("POST", { process, size }),
    ),
  applyProcesses: (
    slug: string,
    processes: Record<string, { size?: string; replicas?: number }>,
  ) =>
    request<AppSummary>(
      `${project(slug)}/processes`,
      json("POST", { processes }),
    ),
  sizes: () => request<SizeCatalog>("/api/sizes"),
  saveSizes: (catalog: SizeCatalog) =>
    request<SizeCatalog>("/api/sizes", json("PUT", catalog)),
  setExposure: (slug: string, exposure: "external" | "internal") =>
    request<AppSummary>(`${project(slug)}/exposure`, json("PUT", { exposure })),
  redeploy: (slug: string, action?: "restart" | "rebuild") =>
    request<{ action: string; message: string }>(
      `${project(slug)}/redeploy`,
      json("POST", action ? { action } : {}),
    ),
  rollback: (slug: string, release: number) =>
    request<AppSummary>(`${project(slug)}/rollback`, json("POST", { release })),
  resources: (slug: string) =>
    request<ResourceInfo[]>(`${project(slug)}/resources`),
  createResource: (
    slug: string,
    body: { kind: string; name: string; spec: Record<string, unknown> },
  ) => request<ResourceInfo>(`${project(slug)}/resources`, json("POST", body)),
  deleteResource: (slug: string, kind: string, name: string, force = false) =>
    request<void>(
      `${project(slug)}/resources/${kind}/${encodeURIComponent(name)}${force ? "?force=true" : ""}`,
      { method: "DELETE" },
    ),
  attach: (
    slug: string,
    body: { kind: string; name: string; prefix?: string },
  ) => request<AppSummary>(`${project(slug)}/bindings`, json("POST", body)),
  detach: (slug: string, kind: string, rname: string) =>
    request<AppSummary>(
      `${project(slug)}/bindings/${kind}/${encodeURIComponent(rname)}`,
      { method: "DELETE" },
    ),
  domains: (slug: string) => request<DomainsResult>(`${project(slug)}/domains`),
  addDomain: (slug: string, host: string) =>
    request<DomainsResult>(`${project(slug)}/domains`, json("POST", { host })),
  removeDomain: (slug: string, host: string) =>
    request<DomainsResult>(
      `${project(slug)}/domains/${encodeURIComponent(host)}`,
      {
        method: "DELETE",
      },
    ),
  volumes: (slug: string) => request<VolumeInfo[]>(`${project(slug)}/volumes`),
  createVolume: (
    slug: string,
    body: {
      name: string;
      size: string;
      storageClass?: string;
      shared?: boolean;
    },
  ) => request<VolumeInfo>(`${project(slug)}/volumes`, json("POST", body)),
  resizeVolume: (slug: string, name: string, size: string) =>
    request<VolumeInfo>(
      `${project(slug)}/volumes/${encodeURIComponent(name)}`,
      json("PUT", { size }),
    ),
  deleteVolume: (slug: string, name: string, force = false) =>
    request<void>(
      `${project(slug)}/volumes/${encodeURIComponent(name)}${force ? "?force=true" : ""}`,
      { method: "DELETE" },
    ),
  snapshots: (slug: string, volume: string) =>
    request<SnapshotInfo[]>(
      `${project(slug)}/volumes/${encodeURIComponent(volume)}/snapshots`,
    ),
  createSnapshot: (slug: string, volume: string, name?: string) =>
    request<SnapshotInfo>(
      `${project(slug)}/volumes/${encodeURIComponent(volume)}/snapshots`,
      json("POST", name ? { name } : {}),
    ),
  deleteSnapshot: (slug: string, volume: string, snapshot: string) =>
    request<void>(
      `${project(slug)}/volumes/${encodeURIComponent(volume)}/snapshots/${encodeURIComponent(snapshot)}`,
      { method: "DELETE" },
    ),
  restoreVolume: (
    slug: string,
    volume: string,
    body: { snapshot: string; to?: string },
  ) =>
    request<RestoreVolumeResult>(
      `${project(slug)}/volumes/${encodeURIComponent(volume)}/restore`,
      json("POST", body),
    ),
  cluster: () => request<ClusterSummary>("/api/cluster"),
  clusterMetrics: (range: string) =>
    request<ClusterMetrics>(`/api/cluster/metrics?range=${range}`),
  registry: () => request<RegistryInfo>("/api/cluster/registry"),
  objectStorage: () =>
    request<ObjectStorageSummary>("/api/cluster/object-storage"),
  allow: (slug: string) => request<AllowEntry[]>(`${project(slug)}/allow`),
  setAllow: (slug: string, entries: AllowEntry[]) =>
    request<AllowEntry[]>(`${project(slug)}/allow`, json("PUT", entries)),
  setAccess: (slug: string, access: string) =>
    request<AppSummary>(`${project(slug)}/access`, json("PUT", { access })),
  preview: (slug: string, body: { teams: string[]; anonymous?: boolean }) =>
    request<{ url: string }>(`${project(slug)}/preview`, json("POST", body)),
  launcher: () => request<LauncherApp[]>("/api/launcher"),
  tokens: () => request<APIToken[]>("/api/tokens"),
  createToken: (body: {
    name: string;
    platformRole?: string;
    projectRoles?: Record<string, string>;
    expiresIn?: string;
  }) =>
    request<APIToken & { token: string }>("/api/tokens", json("POST", body)),
  revokeToken: (id: string) =>
    request<void>(`/api/tokens/${encodeURIComponent(id)}`, {
      method: "DELETE",
    }),
  backups: () => request<BackupInfo>("/api/cluster/backups"),
  runBackup: () =>
    request<{ job: string; status: string }>("/api/cluster/backups", {
      method: "POST",
    }),
  registryGC: () =>
    request<{ status: string }>("/api/cluster/registry/gc", { method: "POST" }),
  helmReleases: () => request<HelmRelease[]>("/api/helm/releases"),
  instances: (slug: string) =>
    request<Instance[]>(`${project(slug)}/instances`),
  /** A one-time code for a shell socket: browsers cannot set headers on a
   * WebSocket, so the ticket is what authenticates it. */
  shellTicket: (slug: string, instance: string) =>
    request<{ ticket: string }>(
      `${project(slug)}/shell/ticket?instance=${encodeURIComponent(instance)}`,
      { method: "POST" },
    ),
};

/**
 * openURL is where to send a person to open an app: a public app at its
 * address; an identified or authenticated app through its sign-in bounce,
 * which is silent for a signed-in person and lets the app know who is
 * there (a plain visit to an identified app would arrive anonymous).
 */
export function openURL(
  url: string | undefined,
  access: string | undefined,
): string {
  if (!url) return "#";
  if (!access || access === "public") return url;
  return url.replace(/\/$/, "") + "/.shpyrd/signin?rd=%2F";
}
