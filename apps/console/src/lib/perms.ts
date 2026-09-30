import { useQuery } from "@tanstack/react-query";
import { api } from "@/api/api";
import type { Identity } from "@/api/types";

// What the person may do at the console, from the platform role the
// server gives. Until roles are enforced, every signed-in person may do
// everything.
export type Perms = { loaded: boolean; me?: Identity; enforced: boolean; view: boolean; admin: boolean };

// The part with no query in it: what a role allows.
export function permsOf(roles?: Identity["roles"]): Pick<Perms, "enforced" | "view" | "admin"> {
  const enforced = roles?.enforced ?? true;
  const platform = roles?.platform ?? "";
  const admin = !enforced || platform === "platform-admin";
  return { enforced, view: admin || platform === "platform-viewer", admin };
}

export function usePerms(): Perms {
  const me = useQuery({ queryKey: ["me"], queryFn: api.me, staleTime: 60_000 });
  return { loaded: me.isSuccess, me: me.data, ...permsOf(me.data?.roles) };
}
