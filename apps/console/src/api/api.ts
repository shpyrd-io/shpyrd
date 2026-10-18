import type { OverviewAPI } from "./cluster-overview";
import type { Link } from "@shpyrd/shared/links";
import type { ArchiveProject, ProjectArchiveStatus, ProjectPlacement, PlacementMeasurement } from "@shpyrd/shared/api/project-archives";
import type {
  BackupInfo,
  ClusterMetrics,
  ClusterSettings,
  ClusterSummary,
  SchedulingSummary,
  ConsoleUser,
  CostDrain,
  CostQuery,
  CostExport,
  CostSummary,
  HelmRelease,
  Identity,
  LicenseStatus,
  LocalUser,
  LoginMethods,
  MailStatus,
  MethodsScope,
  NewLoginMethod,
  OCIStatus,
  ObjectStorageSummary,
  PostgresServers,
  PostgresTenant,
  PostgresTenantPage,
  PostgresTenantQuery,
  PublicConfig,
  RegistryInfo,
  SizeCatalog,
  WorkspaceSettings,
  WorkspaceSummary,
} from "./types";

// Every call the console makes. Two answer it: the Backend, a shpyrd
// server at its console door; the Mock, JSON files. NEXT_PUBLIC_API_MODE
// picks one, and the build drops the other.

export type Api = OverviewAPI & {
  // The shared Postgres servers, a page of one server's databases, and
  // a database found across every server. The console only reads here.
  postgresServers: () => Promise<PostgresServers>;
  postgresTenants: (server: string, query: PostgresTenantQuery) => Promise<PostgresTenantPage>;
  findPostgresTenants: (q: string) => Promise<PostgresTenant[]>;
  archiveProjects: () => Promise<ArchiveProject[]>;
  projectPlacement: (id: string) => Promise<ProjectPlacement>;
  measureProjectPlacement: (id: string, group: string) => Promise<PlacementMeasurement>;
  deleteRetainedVolume: (id: string, volume: string) => Promise<void>;
  moveProject: (id: string, body: { group: string; node: string; migrateToLocal?: boolean }) => Promise<void>;
  projectArchiveStatus: (id: string) => Promise<ProjectArchiveStatus>;
  backupProject: (id: string, options?: { files?: boolean }) => Promise<string[]>;
  restoreProject: (id: string, file: File) => Promise<void>;
  recoverProject: (id: string) => Promise<void>;
  config: () => Promise<PublicConfig>;
  me: () => Promise<Identity>;
  tokenLogin: (body: { token: string; next?: string }) => Promise<{ next: string }>;
  logout: () => Promise<{ redirect: string }>;
  // The workspaces the platform hosts: the open-source platform's one, as
  // the way to it.
  workspaces: () => Promise<WorkspaceSummary[]>;
  // What the extensions add to the console's sidebar.
  links: () => Promise<Link[]>;
  patchSettings: (body: Partial<ClusterSettings>) => Promise<ClusterSettings>;
  // The cluster.
  cluster: () => Promise<ClusterSummary>;
  clusterScheduling: () => Promise<SchedulingSummary>;
  clusterMetrics: (range: string) => Promise<ClusterMetrics>;
  helmReleases: () => Promise<HelmRelease[]>;
  registry: () => Promise<RegistryInfo>;
  registryGC: () => Promise<{ status: string }>;
  objectStorage: () => Promise<ObjectStorageSummary>;
  backups: () => Promise<BackupInfo>;
  runBackup: () => Promise<{ job: string; status: string }>;
  license: () => Promise<LicenseStatus>;
  // Renew now at the billing app that issued the license; a link of one
  // use to the customer's account there.
  renewLicense: () => Promise<LicenseStatus>;
  billingLink: () => Promise<{ url: string }>;
  costs: (q: CostQuery) => Promise<CostSummary>;
  createCostExport: (q: CostQuery) => Promise<CostExport>;
  costExport: (id: string) => Promise<CostExport>;
  costExportURL: (id: string) => Promise<string>;
  costDrains: () => Promise<CostDrain[]>;
  addCostDrain: (body: { name: string; url: string; headers?: Record<string, string> }) => Promise<CostDrain>;
  removeCostDrain: (name: string) => Promise<void>;
  ociStatus: () => Promise<OCIStatus>;
  putOCI: (body: { tenancy: string; user: string; fingerprint: string; region: string; key: string }) => Promise<OCIStatus>;
  deleteOCI: () => Promise<void>;
  workspaceSettings: () => Promise<WorkspaceSettings>;
  putWorkspaceSettings: (body: Pick<WorkspaceSettings, "limits" | "sleep">) => Promise<WorkspaceSettings>;
  mailStatus: () => Promise<MailStatus>;
  mailTest: (to: string) => Promise<{ ok: boolean; to: string; took: string }>;
  sizes: () => Promise<SizeCatalog>;
  saveSizes: (catalog: SizeCatalog) => Promise<SizeCatalog>;
  // The accounts of the auth-local extension.
  consoleUsers: () => Promise<ConsoleUser[]>;
  consolePrincipals: () => Promise<{id:string;email:string;name?:string;provider?:string}[]>;
  addConsoleUser: (body: { principalId: string }) => Promise<ConsoleUser>;
  removeConsoleUser: (email: string) => Promise<void>;
  users: () => Promise<LocalUser[]>;
  createUser: (body: { email: string; name?: string; password: string }) => Promise<LocalUser>;
  setUserPassword: (email: string, password: string) => Promise<void>;
  removeUser: (email: string) => Promise<void>;
  // The sign-in methods of the console, and the defaults of the workspaces.
  loginMethods: (scope: MethodsScope) => Promise<LoginMethods>;
  addLoginMethod: (scope: MethodsScope, body: NewLoginMethod) => Promise<{ id: string }>;
  removeLoginMethod: (scope: MethodsScope, id: string) => Promise<void>;
};

let mock: Promise<Api> | undefined;

function pick(): Promise<Api> {
  if (process.env.NEXT_PUBLIC_API_MODE === "mock") {
    mock ??= import("./mock").then((m) => m.mock);
    return mock;
  }
  return import("./backend").then((m) => m.backend);
}

// `api.cluster()` and the like: each call waits for the one picked.
export const api: Api = new Proxy({} as Api, {
  get(_, name: keyof Api) {
    return (...args: unknown[]) =>
      pick().then((chosen) => (chosen[name] as (...a: unknown[]) => unknown)(...args));
  },
});

export { ApiError } from "@shpyrd/shared/api/error";
