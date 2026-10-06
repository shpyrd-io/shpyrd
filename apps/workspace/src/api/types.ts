// The shapes the server answers with, kept to what the workspace
// application reads.

// A link an extension adds to the workspace's sidebar (GET /api/links).
export type { Link } from "@shpyrd/shared/links";

export type WorkspaceCapabilities = { internalExposure: boolean };

export type PublicConfig = {
  version: string;
  domain: string;
  authRequired: boolean;
  metrics: boolean;
  grafanaUrl?: string;
  auth: {
    token: boolean;
    providers: { id: string; label: string; kind?: string; realm?: string; workspace?: string }[];
    password?: { id: string; label: string };
  };
  extensions: string[];
  // What the server offers beyond the core: "workspaces". The
  // open-source core adds nothing.
  capabilities?: string[];
  workspace?: {
    capabilities?: WorkspaceCapabilities;
    slug: string;
    name: string;
    address?: string;
    // How its apps' addresses are written: prefix, the app's slug, suffix
    // ("acme-" and ".shpyrd.app"; or "" and ".intranet.acme.com").
    appHost?: { prefix: string; suffix: string };
    ownedByOperator?: boolean;
    branding?: { logoUrl?: string; color?: string };
  };
  door?: "console" | "workspace";
  consoleUrl?: string;
  // What the cluster can do with volumes: the least size, and whether
  // it takes snapshots.
  volumes?: { minSize?: string; snapshots: boolean; nodeLocal?: boolean };
};

export type WorkspaceRole = "owner" | "admin" | "member";
export type PlatformRole = "platform-admin" | "platform-viewer";

export type Identity = {
  subject: string;
  email?: string;
  name?: string;
  provider: string;
  admin: boolean;
  console?: boolean;
  roles?: {
    workspace?: WorkspaceRole | "";
    platform?: PlatformRole | "";
    projects?: Record<string, ProjectRole>;
    enforced: boolean;
  };
};

export type WorkspaceInfo = {
  capabilities?: WorkspaceCapabilities;
  slug: string;
  name: string;
  ownedByOperator: boolean;
  domain?: string;
  address?: string;
  url?: string;
  status?: "active" | "suspended";
  limits?: { projects?: number; instances?: number; cpu?: string; memory?: string; storage?: string };
  usage?: { projects: number; instances: number; cpu: string; memory: string; storage: string };
  joinPolicy: "open" | "company" | "listed";
  ownMethodsOnly: boolean;
  branding?: { logoUrl?: string; color?: string };
  mcpName: string;
  mcpUrl: string;
  owners: string[];
  createdAt: string;
  updatedAt: string;
};

export type SleepStatus = {
  state: "awake" | "sleeping" | "waking" | "unavailable";
  message?: string;
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
  // Why the process keeps to one instance, when it does.
  pinned?: string;
  sleep?: SleepStatus;
};

export type Exposure = "external" | "internal";
export type Access = "public" | "authenticated" | "identified";

export type Release = {
  number: number;
  kind: "deploy" | "config" | "rollback";
  description?: string;
  build?: string | number;
  digest?: string;
  processes?: string[];
  createdAt: string;
};

export type ProjectSummary = {
  slug: string;
  displayName: string;
  description?: string;
  featured?: boolean;
  namespace: string;
  phase: string;
  message?: string;
  url?: string;
  release: number;
  source?: string;
  processes?: Record<string, ProcessStatus>;
  createdAt: string;
  exposure?: Exposure;
  access: Access;
  // The symbol of its card on the launcher and its colour; the image it
  // sent in their place, and that image's type.
  icon?: string;
  iconColor?: string;
  iconUrl?: string;
  iconType?: string;
  // The first domain of its own that answers: what its card shows and
  // opens, before its address under the platform.
  domain?: string;
  // The teams that have access to it; only in the list.
  teams?: string[];
};

export type Project = ProjectSummary & {
  spec: {
    source?: {
      git?: { url: string; revision?: string };
      blob?: { sha256?: string; ref?: string };
      subPath?: string;
    };
    pinnedDigest?: string;
    processes?: Record<string, { size?: string; replicas?: number; command?: string; port?: number }>;
    build?: { strategy?: "buildpacks" | "dockerfile"; dockerfile?: string; env?: { name: string; value?: string }[] };
    env?: { name: string; value?: string }[];
    bindings?: { kind: string; name: string }[];
  };
  status: {
    phase: string;
    message?: string;
    digest?: string;
    url?: string;
    latestBuild?: string;
    releases: Release[];
    processTypes?: string[];
    // The release command of the release going out, while it runs or
    // after it failed.
    release?: { target: string; state: "Running" | "Succeeded" | "Failed"; message?: string };
    // Built is False when the last build failed; Ready's reason says what
    // stops the project (BuildFailed, ReleaseFailed, QuotaExceeded, ...).
    conditions?: { type: string; status: "True" | "False" | "Unknown"; reason?: string; message?: string }[];
  };
};

// buildFailed says the project's last build failed (its Built condition).
export const buildFailed = (status: { phase: string; conditions?: { type: string; status: string }[] }) =>
  status.phase === "Failed" && (status.conditions ?? []).some((c) => c.type === "Built" && c.status === "False");

export type DeployRequest = {
  git?: { url: string; revision?: string };
  subPath?: string;
  strategy?: "buildpacks" | "dockerfile";
  dockerfile?: string;
};

export type AuditEntry = {
  at: string;
  actor: string;
  action: string;
  target?: string;
  detail?: string;
  via?: string;
  realm?: string;
};

export type ConfigVar = { name: string; updatedAt?: string };
// The config vars of a project: its own, the ones its resources provide
// (read-only, over its own), and the cluster's (under its own).
export type ConfigVars = { vars: ConfigVar[]; bound?: { name: string; provider: string }[]; global?: ConfigVar[] };
export type ConfigChange = { set?: Record<string, string>; unset?: string[]; dotenv?: string };
// The config vars every project of the workspace receives: names and when
// each was set, never values, and how many projects receive them.
export type Globals = { vars: { name: string; updatedAt?: string }[]; projects: number };

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
};

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
  // What the platform decided on creation and why (a database's size).
  note?: string;
};

export type VolumeInfo = {
  name: string;
  namespace?: string;
  size: string;
  capacity?: string;
  shared: boolean;
  storageClass?: string;
  phase: "Pending" | "Bound" | "Failed" | "Restoring" | string;
  message?: string;
  mountedBy: string[];
  createdAt: string;
  restoredFrom?: string;
  note?: string;
};

export type SnapshotInfo = { name: string; volume: string; size?: string; ready: boolean; message?: string; createdAt: string };
export type RestoreVolumeResult = { volume: VolumeInfo; inPlace: boolean; message: string };

// connections: the clients a database or store of the size takes.
export type InstanceSize = { name: string; kind: "shared" | "dedicated"; cpu: string; memory: string; description?: string; connections?: number };
export type SizeList = { default: string; sizes: InstanceSize[] };
// The processes' sizes, and the lists of databases and stores: the same
// names, each list with its own memory and default.
export type SizeCatalog = SizeList & { postgres: SizeList; redis: SizeList };

export type Instance = { name: string; process: string; pod: string; ready: boolean };

export type DomainStatus = {
  host: string;
  dns: "ok" | "missing" | "wrong" | "unknown";
  target?: string;
  certificate: "ready" | "issuing" | "failed" | "wildcard";
  message?: string;
};

// A name of the workspace's own: proved by records, and one of them the
// address the workspace answers at first.
export type WorkspaceDomain = {
  host: string;
  verified: boolean;
  verifiedAt?: string;
  primary: boolean;
  records: { type: string; name: string; value: string }[];
  url: string;
};

export type Drain = {
  // The workspace whose lines it receives, for a drain of the workspace.
  workspace?: string;
  name: string;
  url: string;
  format: "json" | "syslog";
  processes?: string[];
  // The names of the headers sent along; their values are never read back.
  headers?: string[];
  cluster?: boolean;
  phase: "Pending" | "Active" | "Failing";
  message?: string;
  lastDeliveryAt?: string;
  sent: number;
  errors: number;
  createdAt: string;
};
export type CreateDrain = { name?: string; url: string; format?: "json" | "syslog"; headers?: Record<string, string>; processes?: string[] };

export type ProjectRole = "reader" | "user" | "viewer" | "developer" | "admin";

export type Member = { name: string; role: ProjectRole; user?: string; team?: string };

// Who may reach the project from inside the cluster: another project, or
// a caller of the platform (the MCP connector, the server acting for a
// person). Nothing may, until it is listed.
export type AllowEntry = { project?: string; platform?: "actions" | "mcp" };

// Software connected to the workspace as a person: an assistant through
// MCP. Each may be cut without touching the person.
export type Connection = {
  id: string;
  client: string;
  email: string;
  scope: string;
  createdAt: string;
  expiresAt: string;
  lastUsedAt?: string;
};

export type Person = {
  email: string;
  name?: string;
  provider?: string;
  status: "active" | "suspended";
  role?: WorkspaceRole;
  platformRole?: PlatformRole;
  lastSeenAt?: string;
};

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

// What inviting gave: a link to hand over, shown once, or the role put
// on someone already known.
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

// What the holder of an invitation link sees before signing in.
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

export type Team = { name: string; description?: string; members: string[]; groups?: string[]; platformRole?: string; everyone?: boolean };

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
export type NewToken = { name: string; platformRole?: string; projectRoles?: Record<string, string>; expiresIn?: string };

// The sign-in methods of the workspace: the password of the platform,
// and the providers, its own and the platform's.
export type LoginMethods = {
  password: boolean;
  connectors: { id: string; type: string; name: string; detail?: string; workspace?: string }[];
  kinds: string[];
  callback: string;
};
export type NewLoginMethod = {
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

// An email domain the company owns, proved by a record.
export type DomainClaim = { domain: string; connector?: string; verified: boolean; verifiedAt?: string; record: string; recordValue: string };

export type Point = [time: number, value: number];
// `reference` is the allocation the series is measured against (what the
// project pays for), `burst` the ceiling above it when the size has one.
export type Series = { name: string; points: Point[]; reference?: number; burst?: number; tone?: "orange" | "blue" | "green" | "violet" | "neutral" | "success" | "info" | "warning" | "error" };
export type Metrics = {
  from: number;
  to: number;
  responseTime: Series[];
  throughput: Series[];
  cpu: Series[];
  memory: Series[];
  // Bytes a second in and out, by process.
  network: Series[];
  instances: Series[];
  releases: { time: number; label: string }[];
};
