import type { LogLine } from "@shpyrd/ui/components/log-view";
import type { ProjectArchiveStatus } from "@shpyrd/shared/api/project-archives";
import type {
  ConvertInfo,
  Access,
  AllowEntry,
  APIToken,
  AuditEntry,
  BuildInfo,
  ConfigChange,
  ConfigVars,
  Globals,
  Connection,
  CreateDrain,
  DeployRequest,
  DomainClaim,
  DomainStatus,
  Drain,
  Exposure,
  GitHooks,
  GitHubAccounts,
  GitHubBranches,
  GitHubConnections,
  GitHubLink,
  GitHubRepositories,
  ProjectGitAuth,
  Identity,
  Link,
  Instance,
  Invitation,
  InvitationPublic,
  InviteResult,
  LoginMethods,
  Member,
  Metrics,
  NewLoginMethod,
  NewToken,
  Person,
  Project,
  ProjectRole,
  ProjectSummary,
  PublicConfig,
  ResourceInfo,
  RestoreVolumeResult,
  SizeCatalog,
  SnapshotInfo,
  Team,
  VolumeInfo,
  WorkspaceDomain,
  DomainsAnswer,
  WorkspaceInfo,
  WorkspaceRole,
  ProjectCap,
  ProjectUsage,
} from "./types";

// Every call the screens make. Two answer it: the Backend, a shpyrd
// server; the Mock, JSON files. NEXT_PUBLIC_API_MODE picks one, and the
// build drops the other.

export type NewProject = {
  slug: string;
  exposure?: Exposure;
  displayName?: string;
  description?: string;
  git?: { url: string; revision?: string };
  subPath?: string;
};

// What the shell and the logs need beyond a request: a line as it comes,
// until the signal aborts.
export type LogQuery = { process?: string; tail?: number; follow?: boolean };

export type Api = {
  projectArchiveStatus: (slug: string) => Promise<ProjectArchiveStatus>;
  backupProject: (slug: string) => Promise<void>;
  restoreProject: (slug: string, file: File) => Promise<void>;
  recoverProject: (slug: string) => Promise<void>;
  config: () => Promise<PublicConfig>;
  me: () => Promise<Identity>;
  links: () => Promise<Link[]>;
  // The door: a password or a token opens a session; a provider is a
  // link (`/api/auth/login?provider=`), not a call.
  tokenLogin: (body: {
    token: string;
    next?: string;
  }) => Promise<{ next: string }>;
  logout: () => Promise<{ redirect: string }>;
  // An invitation link, before and after signing in.
  invitation: (token: string) => Promise<InvitationPublic>;
  acceptInvitation: (
    token: string,
  ) => Promise<{ workspace: string; role: WorkspaceRole; next: string }>;
  // The workspace.
  workspace: () => Promise<WorkspaceInfo>;
  // The projects' own storage caps, replaced (storage plan step 4).
  putProjectCaps: (caps: Record<string, ProjectCap>) => Promise<WorkspaceInfo>;
  // The logo is a data URL, "" to remove it; the colour #rrggbb, "" to reset.
  updateWorkspace: (body: {
    name?: string;
    joinPolicy?: WorkspaceInfo["joinPolicy"];
    ownMethodsOnly?: boolean;
    logo?: string;
    color?: string;
    mcpName?: string;
  }) => Promise<WorkspaceInfo>;
  people: () => Promise<Person[]>;
  setPersonRole: (email: string, role: WorkspaceRole) => Promise<void>;
  setPersonStatus: (email: string, status: Person["status"]) => Promise<void>;
  removePerson: (email: string) => Promise<void>;
  connections: () => Promise<Connection[]>;
  revokeConnection: (id: string) => Promise<void>;
  invitations: () => Promise<Invitation[]>;
  invite: (body: {
    email: string;
    role: WorkspaceRole;
    team?: string;
  }) => Promise<InviteResult>;
  revokeInvitation: (id: string) => Promise<void>;
  teams: () => Promise<Team[]>;
  saveTeam: (team: Team) => Promise<Team>;
  removeTeam: (name: string) => Promise<void>;
  tokens: () => Promise<APIToken[]>;
  createToken: (body: NewToken) => Promise<{ token: APIToken; secret: string }>;
  revokeToken: (id: string) => Promise<void>;
  workspaceDomains: () => Promise<WorkspaceDomain[]>;
  addWorkspaceDomain: (host: string) => Promise<WorkspaceDomain>;
  verifyWorkspaceDomain: (host: string) => Promise<WorkspaceDomain>;
  setWorkspaceDomainRedirect: (
    host: string,
    redirectApps: boolean,
  ) => Promise<WorkspaceDomain>;
  setWorkspaceDomainPrimary: (host: string) => Promise<WorkspaceDomain>;
  removeWorkspaceDomain: (host: string) => Promise<void>;
  loginMethods: () => Promise<LoginMethods>;
  addLoginMethod: (body: NewLoginMethod) => Promise<{ id: string }>;
  removeLoginMethod: (id: string) => Promise<void>;
  domainClaims: () => Promise<DomainClaim[]>;
  claimDomain: (domain: string, connector: string) => Promise<DomainClaim>;
  verifyDomainClaim: (domain: string) => Promise<DomainClaim>;
  unclaimDomain: (domain: string) => Promise<void>;
  // This month without a month; the one given otherwise (YYYY-MM).
  sizes: () => Promise<SizeCatalog>;
  // The projects.
  projects: () => Promise<ProjectSummary[]>;
  project: (slug: string) => Promise<Project>;
  createProject: (body: NewProject) => Promise<ProjectSummary>;
  updateProject: (
    slug: string,
    body: {
      name?: string;
      description?: string;
      featured?: boolean;
      icon?: string;
      iconColor?: string;
      // Whether a push to the project's branch deploys.
      autoDeploy?: boolean;
      // The commits a push deploys: only those that change these globs,
      // and only once their other checks pass (the GitHub App's).
      watchPaths?: string[];
      waitForChecks?: boolean;
    },
  ) => Promise<Project>;
  // The project's webhook for deploy on push: its URL and secret.
  projectGitHooks: (slug: string) => Promise<GitHooks>;
  // What the project's builds clone with: a GitHub App, a deploy key, a token.
  projectGitAuth: (slug: string) => Promise<ProjectGitAuth>;
  // The workspace's GitHub (RFC-0054): its connections; Connect sends the
  // browser to GitHub's authorize page, which says who the person is so the
  // server lists the installations they own; the install page is for a new
  // account. Both carry the same single-use state.
  github: () => Promise<GitHubConnections>;
  connectGitHub: () => Promise<{ installURL: string; authorizeURL: string }>;
  disconnectGitHub: (id: number) => Promise<void>;
  // The person's own GitHub account (RFC-0054): null when none is linked.
  // linkGitHub answers GitHub's authorize page for a popup; an empty one
  // means linked already (the Mock links at once).
  githubMe: () => Promise<GitHubLink | null>;
  linkGitHub: (returnTo: string) => Promise<{ authorizeURL: string }>;
  unlinkGitHub: () => Promise<void>;
  // What the person reads through the workspace's GitHub: a 409 with the
  // code "github_link_required" while they have no link. A list the server
  // cut says so in `truncated`.
  githubAccounts: () => Promise<GitHubAccounts>;
  githubRepositories: (installation?: number) => Promise<GitHubRepositories>;
  githubBranches: (fullName: string) => Promise<GitHubBranches>;
  // The project's own image for its card, as a data URL; and back to its symbol.
  setProjectIcon: (slug: string, dataUrl: string) => Promise<Project>;
  removeProjectIcon: (slug: string) => Promise<Project>;
  destroyProject: (slug: string) => Promise<void>;
  deploy: (slug: string, body: DeployRequest) => Promise<ProjectSummary>;
  // The current release again: new instances of it, or, after a failed
  // build, the same source built again; "restart" runs the release
  // command again. The server says which it did.
  redeploy: (
    slug: string,
    action?: "restart" | "rebuild",
  ) => Promise<{ action: string; message: string }>;
  rollback: (slug: string, release: number) => Promise<void>;
  applyProcesses: (
    slug: string,
    changes: Record<string, { size?: string; replicas?: number }>,
  ) => Promise<Project>;
  setExposure: (slug: string, exposure: Exposure) => Promise<Project>;
  setAccess: (slug: string, access: Access) => Promise<Project>;
  // A link that opens the app as a team would, or as nobody.
  preview: (
    slug: string,
    body: { teams: string[]; anonymous?: boolean },
  ) => Promise<{ url: string }>;
  audit: (slug: string) => Promise<AuditEntry[]>;
  builds: (slug: string) => Promise<BuildInfo[]>;
  // What a build printed, line by line, following it while it builds.
  streamBuild: (
    slug: string,
    build: string,
    follow: boolean,
    signal: AbortSignal,
    onLine: (line: string) => void,
  ) => Promise<void>;
  // The config vars of the workspace, which every project receives; a
  // change is a release in each.
  globals: () => Promise<Globals>;
  changeGlobals: (change: ConfigChange) => Promise<Globals>;
  configVars: (slug: string) => Promise<ConfigVars>;
  changeConfigVars: (slug: string, change: ConfigChange) => Promise<void>;
  resources: (slug: string) => Promise<ResourceInfo[]>;
  createResource: (
    slug: string,
    body: { kind: string; name: string; spec: Record<string, unknown> },
  ) => Promise<ResourceInfo>;
  // Another size of its kind's list for a Postgres or a Redis; the note
  // says what follows.
  resizeResource: (
    slug: string,
    kind: string,
    name: string,
    size: string,
  ) => Promise<ResourceInfo>;
  // A database moved between the shared server and a cluster of its own,
  // under its name; the note says what follows.
  convertPostgres: (
    slug: string,
    name: string,
    body: { to: "shared" | "dedicated"; size?: string; storage?: string },
  ) => Promise<ConvertInfo>;
  removeResource: (
    slug: string,
    kind: string,
    name: string,
    force?: boolean,
  ) => Promise<void>;
  attach: (
    slug: string,
    body: { kind: string; name: string; prefix?: string },
  ) => Promise<void>;
  detach: (slug: string, kind: string, name: string) => Promise<void>;
  volumes: (slug: string) => Promise<VolumeInfo[]>;
  createVolume: (
    slug: string,
    body: { name: string; size: string; shared?: boolean },
  ) => Promise<VolumeInfo>;
  // The project's block storage against the ceiling that applies to it.
  projectUsage: (slug: string) => Promise<ProjectUsage>;
  resizeVolume: (
    slug: string,
    name: string,
    size: string,
  ) => Promise<VolumeInfo>;
  removeVolume: (slug: string, name: string, force?: boolean) => Promise<void>;
  snapshots: (slug: string, volume: string) => Promise<SnapshotInfo[]>;
  createSnapshot: (
    slug: string,
    volume: string,
    name?: string,
  ) => Promise<SnapshotInfo>;
  removeSnapshot: (
    slug: string,
    volume: string,
    snapshot: string,
  ) => Promise<void>;
  restoreVolume: (
    slug: string,
    volume: string,
    body: { snapshot: string; to?: string },
  ) => Promise<RestoreVolumeResult>;
  domains: (slug: string) => Promise<DomainsAnswer>;
  setDomainRedirect: (
    slug: string,
    redirectTo: string,
  ) => Promise<DomainsAnswer>;
  addDomain: (slug: string, host: string) => Promise<DomainStatus>;
  removeDomain: (slug: string, host: string) => Promise<void>;
  // The drains of the workspace: every project's lines, labelled with it.
  workspaceDrains: () => Promise<Drain[]>;
  addWorkspaceDrain: (body: CreateDrain) => Promise<Drain>;
  removeWorkspaceDrain: (name: string) => Promise<void>;
  drains: (slug: string) => Promise<Drain[]>;
  addDrain: (slug: string, body: CreateDrain) => Promise<Drain>;
  removeDrain: (slug: string, name: string) => Promise<void>;
  members: (slug: string) => Promise<Member[]>;
  setMember: (slug: string, member: Member) => Promise<void>;
  removeMember: (slug: string, name: string) => Promise<void>;
  allow: (slug: string) => Promise<AllowEntry[]>;
  setAllow: (slug: string, entries: AllowEntry[]) => Promise<AllowEntry[]>;
  // The lines of the project as they come; with `follow` they keep coming.
  streamLogs: (
    slug: string,
    query: LogQuery,
    signal: AbortSignal,
    onLine: (line: LogLine) => void,
  ) => Promise<void>;
  metrics: (slug: string, range: string) => Promise<Metrics>;
  // A shell into an instance: the instances, a ticket for one, and the
  // socket the ticket opens. The Mock has no socket: it answers null and
  // the screen makes a shell up.
  instances: (slug: string) => Promise<Instance[]>;
  shellTicket: (slug: string, instance: string) => Promise<{ ticket: string }>;
  shellSocket: (
    slug: string,
    instance: string,
    ticket: string,
  ) => Promise<string | null>;
};

export type { ProjectRole };

let mock: Promise<Api> | undefined;

function pick(): Promise<Api> {
  if (process.env.NEXT_PUBLIC_API_MODE === "mock") {
    mock ??= import("./mock").then((m) => m.mock);
    return mock;
  }
  return import("./backend").then((m) => m.backend);
}

// `api.projects()` and the like: each call waits for the one picked.
export const api: Api = new Proxy({} as Api, {
  get(_, name: keyof Api) {
    return (...args: unknown[]) =>
      pick().then((chosen) =>
        (chosen[name] as (...a: unknown[]) => unknown)(...args),
      );
  },
});

export { ApiError } from "@shpyrd/shared/api/error";
