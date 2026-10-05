// The shapes the server answers with at the console door, kept to what
// the console reads.

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
  capabilities?: string[];
  workspace?: { slug: string; name: string; address?: string; ownedByOperator?: boolean; branding?: { logoUrl?: string; color?: string } };
  defaultWorkspaceId?: string;
  consoleHost?: string;
  consoleUrl?: string;
  door?: "console" | "workspace";
};

export type PlatformRole = "platform-admin" | "platform-viewer";

export type Identity = {
  subject: string;
  email?: string;
  name?: string;
  provider: string;
  admin: boolean;
  console?: boolean;
  roles?: { workspace?: string; platform?: PlatformRole | ""; projects?: Record<string, string>; enforced: boolean };
};

// A workspace as the console knows it: on the open-source platform, the
// one it hosts, where the sidebar's Workspace goes.
export type WorkspaceSummary = {
  slug: string;
  name: string;
  address?: string;
  url: string;
  status: "active" | "suspended" | string;
};


// Who may open the console: its own list, independent of every workspace.
// account is the auth-local account's state, "" when there is none.
export type ConsoleUser = { email: string; addedAt: string; addedBy?: string; account: "" | "active" | "pending" | "locked" };

// What the open-source platform's one workspace may use, and when its
// projects and databases sleep by default; null is none.
export type Limits = { projects?: number; instances?: number; cpu?: string; memory?: string; storage?: string };
export type SleepDefaults = { appsAfter?: string; appsResuming?: "page" | "wait" | ""; databasesAfter?: string };
export type WorkspaceSettings = {
  workspace: string;
  limits: Limits | null;
  sleep: SleepDefaults | null;
  usage?: { projects: number; instances: number; cpu: string; memory: string; storage: string } | null;
};

// Costs (enterprise): lines summed by a group, cost by currency.
export type CostKind = "estimated" | "real" | "usage";
export type CostGroup = "project" | "process" | "resource" | "service";
export type CostRow = { key: string; workspace?: string; project?: string; slug?: string; process?: string; resource?: string; service?: string; cost: Record<string, number>; lines: number };
export type CostSummary = { from: string; to: string; kind: CostKind; group: CostGroup; rows: CostRow[]; total: Record<string, number> };
export type CostDrain = { id: string; name: string; url: string; headers?: string[]; lastDeliveryAt?: string; sent: number; errors: number; message?: string; createdAt: string };
export type OCIStatus = { configured: boolean; tenancy?: string; region?: string };

export type ClusterSettings = { defaultWorkspaceId: string; consolePasswordSignIn: boolean };

export type ExtensionInfo = { name: string; description: string; enabled: boolean; component?: string };

export type Node = {
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
  // The node pool (platform, data or apps); absent on a single-pool cluster.
  pool?: string;
};

// A pod of the platform running outside the platform pool, where no
// selector of its own put it: on an apps or data node.
export type MisplacedPod = {
  namespace: string;
  name: string;
  // What made it: "Deployment/keda-operator".
  owner?: string;
  node: string;
  pool: string;
  // The CPU it reserves there: "250m".
  cpu?: string;
};

export type ClusterSummary = {
  install?: { profile: string; version: string; domain: string; updatedAt: string };
  components: { name: string; version?: string; appliedAt: string }[];
  extensions: ExtensionInfo[];
  nodes: Node[];
  apps: number;
  phases: Record<string, number>;
  externalLBAddress?: string;
  internalLBAddress?: string;
  misplaced?: MisplacedPod[];
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

export type Point = [time: number, value: number];
export type Series = { name: string; points: Point[]; tone?: "orange" | "blue" | "green" | "violet" | "neutral" };

// How the machines are doing: the whole, each, and along the time.
export type ClusterMetrics = { range: string; total: NodeUsage; nodes: NodeUsage[]; cpu: Series[]; memory: Series[] };

export type HelmRelease = { name: string; namespace: string; chart: string; chartVersion: string; appVersion?: string; status: string; revision: number; updatedAt: string };

export type RegistryInfo = {
  mode: "in-cluster" | "external";
  host: string;
  tls: boolean;
  ready: boolean;
  message?: string;
  storage?: { usedBytes: number; capacityBytes: number; size: string; backend?: "filesystem" | "s3"; bucket?: string; endpoint?: string; error?: string; measuredAt?: string };
  images?: { repositories: number; tags: number; largest: { name: string; tags: number }[]; error?: string };
  certificate?: { issuer: string; notAfter: string };
  gc?: { schedule: string; nextRun?: string; running: boolean; startedAt?: string; lastRun?: string; lastResult?: string; lastDuration?: string; reclaimedBytes: number; usedBytes: number };
};

export type ObjectStorageSummary = {
  backend?: "garage" | "gateway";
  endpoint: string;
  totalBytes: number;
  usedBytes: number;
  measuredAt?: string;
  message?: string;
  buckets: { namespace: string; name: string; bucket: string; phase: string; message?: string; usedBytes: number; objects: number; retentionDays?: number }[];
};

export type BackupRun = { name: string; status: "running" | "succeeded" | "failed"; started?: string; finished?: string; message?: string };
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
  archives: { name: string; size: number; modified: string }[];
  error?: string;
};

// The enterprise license (ee/licensing): on with a license in force, or
// unlocked where the build runs the platform itself.
// `issuer` is the billing app that issued it, where it renews online and
// where the customer's account is; none for a license issued by hand.
export type License = { id: string; customer: string; issuedAt: string; expiresAt: string; issuer?: string };
export type LicenseRenewal = { at: string; error?: string };
export type LicenseStatus = { active: boolean; unlocked?: boolean; license?: License; error?: string; renewal?: LicenseRenewal };

export type MailStatus = { configured: boolean; host?: string; port?: number; from?: string; security?: string; auth: boolean };


// connections: the clients a database or store of the size takes.
export type InstanceSize = { name: string; kind: "shared" | "dedicated"; cpu: string; memory: string; description?: string; connections?: number };
export type SizeList = { default: string; sizes: InstanceSize[] };
// The processes' sizes, and the lists of databases and stores: the same
// names, each list with its own memory and default.
export type SizeCatalog = SizeList & { postgres: SizeList; redis: SizeList };



export type LocalUser = { email: string; name?: string; createdAt: string };

// Whose sign-in methods: the console's own door, or the defaults every
// workspace offers until it brings its own.
export type MethodsScope = "console" | "platform";
export type LoginMethods = {
  password: boolean;
  connectors: { id: string; type: string; name: string; detail?: string; workspace?: string }[];
  kinds: string[];
  callback: string;
};
export type NewLoginMethod = { type: string; id?: string; name?: string; clientId: string; clientSecret: string; org?: string; hostedDomain?: string; tenant?: string; issuer?: string };
