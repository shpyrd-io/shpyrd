import { useQuery } from "@tanstack/react-query";
import { api } from "@/api/api";
import type { Identity } from "@/api/types";

// What the person may do, from the roles the server gives. Until roles are
// enforced (no team yet), every signed-in person may do everything.

export type Perms = {
  loaded: boolean;
  me?: Identity;
  enforced: boolean;
  // In the workspace.
  owner: boolean;
  admin: boolean;
  create: boolean;
  // On a project, when one is given.
  view: boolean;
  deploy: boolean;
  config: boolean;
  exec: boolean;
  resource: boolean;
  members: boolean;
  destroy: boolean;
};

const projectRanks = { reader: 0, user: 0, viewer: 1, developer: 2, admin: 3 } as const;

export function usePerms(slug?: string): Perms {
  const me = useQuery({ queryKey: ["me"], queryFn: api.me, staleTime: 60_000 });
  const roles = me.data?.roles;
  const enforced = roles?.enforced ?? true;
  const workspace = roles?.workspace ?? "";
  const owner = !enforced || workspace === "owner";
  const admin = owner || workspace === "admin";
  const rank = slug ? (projectRanks[roles?.projects?.[slug] ?? "reader"] ?? 0) : 0;
  const atLeast = (n: number) => !enforced || admin || rank >= n;
  return {
    loaded: me.isSuccess,
    me: me.data,
    enforced,
    owner,
    admin,
    create: admin || workspace === "member",
    view: atLeast(1),
    deploy: atLeast(2),
    config: atLeast(2),
    exec: atLeast(2),
    resource: atLeast(3),
    members: atLeast(3),
    destroy: atLeast(3),
  };
}

// Someone whose roles only open apps: the launcher is all they see.
export function useUserOnly(perms: Perms): boolean {
  const projects = Object.values(perms.me?.roles?.projects ?? {});
  return perms.loaded && perms.enforced && !perms.admin && !perms.create && projects.every((r) => r === "user" || r === "reader");
}
