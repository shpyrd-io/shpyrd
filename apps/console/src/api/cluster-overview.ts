import type { Node, Series } from "./types";

export type OverviewView = "compute" | "storage" | "network";
export type Values = Record<string, number | undefined>;
export type OverviewNode = Node & { values: Values; conditions: string[]; unschedulable: boolean; issues: string[]; misplaced: number; diskMount?: string };
export type OverviewPool = { name: string; nodes: number; ready: number; issues: number; missing: number; values: Values; coverage: Record<string, number>; distribution: Record<string, number> };
export type Overview = { snapshot: string; updatedAt: string; pools: OverviewPool[]; errors: string[]; install?: { profile: string; version: string; domain: string; updatedAt: string } };
export type NodePage = { snapshot: string; updatedAt: string; nodes: OverviewNode[]; total: number; next: number };
export type NodeQuery = { snapshot: string; pool: string; view: OverviewView; search?: string; zone?: string; status?: string; issues?: string; sort?: string; offset?: number; limit?: number };
export type OverviewChart = { id: string; title: string; unit: string; series: Series[]; error?: string; note?: string };
export type OverviewCharts = { charts: OverviewChart[]; updatedAt: string };
export type NodeDevice = { name: string; device: string; mount?: string; values: Values };
export type NodeDetail = { node: OverviewNode; devices: NodeDevice[]; errors: string[]; truncated: boolean; updatedAt: string; snapshotAt: string };
export type NodePods = { pods: { name: string; namespace: string; phase: string; createdAt: string }[]; continue: string };
export type OverviewAPI = {
  overview: (signal?: AbortSignal) => Promise<Overview>;
  overviewNodes: (query: NodeQuery, signal?: AbortSignal) => Promise<NodePage>;
  overviewCharts: (query: { pool: string; range: string; view: OverviewView; nodes?: string; device?: string; mount?: string }, signal?: AbortSignal) => Promise<OverviewCharts>;
  overviewNode: (query: { snapshot: string; node: string; view: OverviewView }, signal?: AbortSignal) => Promise<NodeDetail>;
  overviewPods: (query: { node: string; namespace?: string; phase?: string; continue?: string }, signal?: AbortSignal) => Promise<NodePods>;
};

export function queryString(query: object): string {
  return new URLSearchParams(Object.entries(query).filter(([, v]) => v !== undefined).map(([k, v]) => [k, String(v)])).toString();
}
