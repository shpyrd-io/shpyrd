import type { ArchiveProject, ProjectArchiveStatus, ProjectPlacement, PlacementMeasurement } from "@shpyrd/shared/api/project-archives";
import type {
  BackupInfo,
  ClusterMetrics,
  ClusterSettings,
  ClusterSummary,
  CreatedWorkspace,
  Economics,
  HelmRelease,
  Identity,
  LocalUser,
  LoginMethods,
  MailStatus,
  MethodsScope,
  NewLoginMethod,
  NewWorkspace,
  ObjectStorageSummary,
  Plan,
  PublicConfig,
  RegistryInfo,
  SizeCatalog,
  WorkspaceSummary,
} from "./types";

// Every call the console makes. Two answer it: the Backend, a shpyrd
// server at its console door; the Mock, JSON files. NEXT_PUBLIC_API_MODE
// picks one, and the build drops the other.

export type Api = {
  archiveProjects: () => Promise<ArchiveProject[]>;
  projectPlacement: (id: string) => Promise<ProjectPlacement>;
  measureProjectPlacement: (id: string, group: string) => Promise<PlacementMeasurement>;
  deleteRetainedVolume: (id: string, volume: string) => Promise<void>;
  moveProject: (id: string, body: { group: string; node: string; migrateToLocal?: boolean }) => Promise<void>;
  projectArchiveStatus: (id: string) => Promise<ProjectArchiveStatus>;
  backupProject: (id: string) => Promise<void>;
  restoreProject: (id: string, file: File) => Promise<void>;
  recoverProject: (id: string) => Promise<void>;
  config: () => Promise<PublicConfig>;
  me: () => Promise<Identity>;
  passwordLogin: (body: { email: string; password: string; next?: string }) => Promise<{ next: string }>;
  tokenLogin: (body: { token: string; next?: string }) => Promise<{ next: string }>;
  logout: () => Promise<{ redirect: string }>;
  // The workspaces the platform hosts.
  workspaces: () => Promise<WorkspaceSummary[]>;
  createWorkspace: (body: NewWorkspace) => Promise<CreatedWorkspace>;
  plans: () => Promise<Plan[]>;
  patchSettings: (body: Partial<ClusterSettings>) => Promise<ClusterSettings>;
  // The cluster.
  cluster: () => Promise<ClusterSummary>;
  clusterMetrics: (range: string) => Promise<ClusterMetrics>;
  helmReleases: () => Promise<HelmRelease[]>;
  registry: () => Promise<RegistryInfo>;
  registryGC: () => Promise<{ status: string }>;
  objectStorage: () => Promise<ObjectStorageSummary>;
  backups: () => Promise<BackupInfo>;
  runBackup: () => Promise<{ job: string; status: string }>;
  mailStatus: () => Promise<MailStatus>;
  mailTest: (to: string) => Promise<{ ok: boolean; to: string; took: string }>;
  economics: (month?: string) => Promise<Economics>;
  sizes: () => Promise<SizeCatalog>;
  saveSizes: (catalog: SizeCatalog) => Promise<SizeCatalog>;
  // The accounts of the auth-local extension.
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
