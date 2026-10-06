import { useQuery } from "@tanstack/react-query";
import { api } from "@/api/api";

// Availability comes from the server, never the edition or the person's role.
// Until it is known, offer no action that enables internal exposure.
export function useInternalExposure(): boolean {
  const config = useQuery({ queryKey: ["config"], queryFn: api.config, staleTime: 30_000, refetchInterval: 30_000 });
  return config.data?.workspace?.capabilities?.internalExposure === true;
}
