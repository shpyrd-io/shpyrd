import { json, request } from "@shpyrd/shared/api/http";
import type { Api } from "./api";
import type { ClusterMetrics, NodeUsage, Series } from "./types";

// The paths of the server at the console door. Where the server's shape
// is not the one the screens read, it is turned into it here.

const gone = { method: "DELETE" };

// What the server answers for the metrics of the cluster: charts by node.
export type ChartAnswer = { id: string; series: { name: string; points: [number, number][] }[]; error?: string };
export type MetricsAnswer = { range: string; total: NodeUsage; nodes: NodeUsage[]; charts: ChartAnswer[] };

const tones: Series["tone"][] = ["orange", "blue", "green", "violet", "neutral"];

function seriesOf(answer: MetricsAnswer, id: string): Series[] {
  const chart = answer.charts.find((c) => c.id === id);
  if (!chart || chart.error) return [];
  return chart.series.map((s, i) => ({ name: s.name, points: s.points, tone: tones[i % tones.length] }));
}

export function toClusterMetrics(answer: MetricsAnswer): ClusterMetrics {
  return { range: answer.range, total: answer.total, nodes: answer.nodes, cpu: seriesOf(answer, "cpu"), memory: seriesOf(answer, "memory") };
}

// The console's own methods answer at the door-scoped route; the
// platform's defaults at the platform one.
const methods = (scope: "console" | "platform") => (scope === "console" ? "/api/workspace/login-methods" : "/api/auth/connectors");

export const backend: Api = {
  config: () => request("/api/config"),
  me: () => request("/api/me"),
  passwordLogin: (body) => request("/api/auth/password", json("POST", body)),
  tokenLogin: (body) => request("/api/auth/token", json("POST", body)),
  logout: () => request("/api/auth/logout", { method: "POST" }),
  workspaces: () => request("/api/workspaces"),
  createWorkspace: (body) => request("/api/workspaces", json("POST", body)),
  plans: () => request("/api/cluster/plans"),
  patchSettings: (body) => request("/api/cluster/settings", json("PATCH", body)),
  cluster: () => request("/api/cluster"),
  clusterMetrics: async (range) => toClusterMetrics(await request<MetricsAnswer>(`/api/cluster/metrics?range=${encodeURIComponent(range)}`)),
  helmReleases: () => request("/api/helm/releases"),
  registry: () => request("/api/cluster/registry"),
  registryGC: () => request("/api/cluster/registry/gc", { method: "POST" }),
  objectStorage: () => request("/api/cluster/object-storage"),
  backups: () => request("/api/cluster/backups"),
  runBackup: () => request("/api/cluster/backups", { method: "POST" }),
  mailStatus: () => request("/api/cluster/mail"),
  mailTest: (to) => request("/api/cluster/mail/test", json("POST", { to })),
  economics: (month) => request(`/api/cluster/economics${month ? `?month=${encodeURIComponent(month)}` : ""}`),
  sizes: () => request("/api/sizes"),
  saveSizes: (catalog) => request("/api/sizes", json("PUT", catalog)),
  users: () => request("/api/users"),
  createUser: (body) => request("/api/users", json("POST", body)),
  setUserPassword: (email, password) => request(`/api/users/${encodeURIComponent(email)}/password`, json("PUT", { password })),
  removeUser: (email) => request(`/api/users/${encodeURIComponent(email)}`, gone),
  loginMethods: (scope) => request(methods(scope)),
  addLoginMethod: (scope, body) => request(methods(scope), json("POST", body)),
  removeLoginMethod: (scope, id) => request(`${methods(scope)}/${encodeURIComponent(id)}`, gone),
};
