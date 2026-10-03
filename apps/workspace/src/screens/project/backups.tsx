"use client";

import { ProjectBackups } from "@shpyrd/shared/project-backups";
import { api } from "@/api/api";
import type { Project } from "@/api/types";
import type { Perms } from "@/lib/perms";

export function Backups({ project, perms }: { project: Project; perms: Perms }) {
  const slug = project.slug;
  return <ProjectBackups name={project.displayName || slug} queryKey={slug} allowed={perms.destroy} actions={{
    status: () => api.projectArchiveStatus(slug), backup: () => api.backupProject(slug),
    restore: (file) => api.restoreProject(slug, file), recover: () => api.recoverProject(slug),
  }} />;
}
