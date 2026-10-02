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

// One workspace as the console lists them: the operator's own or a
// customer's, each a door of its own.
export type WorkspaceSummary = {
  slug: string;
  name: string;
  address?: string;
  url: string;
  status: "active" | "suspended" | string;
  owner?: "operator" | "customer";
  plan?: string;
  limits?: { projects?: number; instances?: number; cpu?: string; memory?: string; storage?: string };
  usage?: { projects: number; instances: number; cpu: string; memory: string; storage: string };
  owners: string[];
  // What the controller last saw at the workspace's door, and when it
  // first answered: before that moment a link to it leads nowhere.
  readiness?: { ready: boolean; checkedAt: string; checks: { name: string; ok: boolean; detail?: string }[] };
  readyAt?: string;
  createdAt: string;
};

// doorState says, in a word or two, whether a workspace's door answers:
// "ready", the first check that does not pass, or "not looked at yet".
export function doorState(w: WorkspaceSummary): { ready: boolean; text: string } {
  if (!w.readiness) return { ready: false, text: "not looked at yet" };
  if (w.readiness.ready) return { ready: true, text: "ready" };
  const failing = w.readiness.checks.find((c) => !c.ok);
  return { ready: false, text: failing ? `${failing.name}: ${failing.detail ?? "not yet"}` : "not ready" };
}

// What became of the first owner's invitation when a workspace was made.
// `setPasswordLink` is the other way in for someone without a password:
// where they choose one, good for a day.
export type InviteOutcome = { applied: boolean; link?: string; expiresAt?: string; setPasswordLink?: string; emailed: boolean; mailError?: string; error?: string };
export type CreatedWorkspace = WorkspaceSummary & { ownerInvitation?: InviteOutcome; ownerInvitationPending?: boolean };
export type NewWorkspace = { slug: string; name?: string; address?: string; owner?: string; operatorOwned?: boolean; plan?: string };

export type Plan = { id: string; name: string; currency: string; minMonthly: number };

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

export type ClusterSummary = {
  install?: { profile: string; version: string; domain: string; updatedAt: string };
  components: { name: string; version?: string; appliedAt: string }[];
  extensions: ExtensionInfo[];
  nodes: Node[];
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
  storage?: { usedBytes: number; capacityBytes: number; size: string };
  images?: { repositories: number; tags: number; largest: { name: string; tags: number }[]; error?: string };
  certificate?: { issuer: string; notAfter: string };
  gc?: { schedule: string; nextRun?: string; running: boolean; startedAt?: string; lastRun?: string; lastResult?: string; lastDuration?: string; reclaimedBytes: number; usedBytes: number };
};

export type ObjectStorageSummary = {
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

export type MailStatus = { configured: boolean; host?: string; port?: number; from?: string; security?: string; auth: boolean };

export type EconomicsRow = { workspace?: string; owner?: "operator" | "customer"; revenue: number; directCogs: number; sharedCogs: number; idleCogs: number; totalCogs: number; grossMargin: number; marginPct: number };
export type Economics = { month: string; workspaces: EconomicsRow[]; totals: EconomicsRow };

export type InstanceSize = { name: string; kind: "shared" | "dedicated"; cpu: string; memory: string; description?: string };
export type SizeCatalog = { default: string; sizes: InstanceSize[] };



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
