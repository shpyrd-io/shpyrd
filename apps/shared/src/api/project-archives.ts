import { request } from "./http";

export type ProjectArchiveStatus = {
  phase: string;
  preview?: boolean;
  id?: string;
  kind?: "export" | "restore" | "move";
  active?: boolean;
  startedAt?: string;
  resource?: string;
  error?: string;
};

export type ArchiveProject = { id: string; slug: string; name: string; workspace: string; phase: string; nodes: string[] | null };

export type ProjectArchiveActions = {
  status: () => Promise<ProjectArchiveStatus>;
  backup: () => Promise<void>;
  restore: (file: File) => Promise<void>;
  recover: () => Promise<void>;
};

export function projectArchiveActions(path: string): ProjectArchiveActions {
  return {
    status: () => request(path),
    backup: async () => {
      const { ticket } = await request<{ ticket: string }>(`${path}/export`, { method: "POST" });
      // Let the browser stream the download to disk. response.blob() would
      // hold a multi-gigabyte archive in the browser's memory.
      window.location.assign(`${path}/download?ticket=${encodeURIComponent(ticket)}`);
    },
    restore: async (file) => {
      await request(`${path}/restore`, { method: "POST", body: file, headers: { "Content-Type": "application/gzip" } });
    },
    recover: async () => { await request(`${path}/recover`, { method: "POST" }); },
  };
}

export type PlacementGroup = { needsMigration?: boolean; storageClasses?: string[]; id: string; processes: string[]; volumes: string[]; database?: string; nodes: string[]; pool: string; cpuRequestedMillicores?: number; memoryRequestedBytes?: number; diskUsedBytes?: number };
export type PlacementNode = { name: string; hostname: string; pool: string; eligible: boolean; reason?: string; architecture?: string; cpuMillicores: number; memoryBytes: number; cpuRequestedMillicores: number; memoryRequestedBytes: number; diskAvailableBytes?: number };
export type RetainedVolume = { name: string; claim: string; storageClass: string; capacity: string; retainedAt: string; deleting?: boolean };
export type ProjectPlacement = { retainedVolumes?: RetainedVolume[]; groups: PlacementGroup[]; nodes: PlacementNode[] };

export type PlacementMeasurement = { group: string; diskUsedBytes: number; measuredAt: string };
