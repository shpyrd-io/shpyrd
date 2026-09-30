"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { LogIn, UserRoundPlus } from "lucide-react";
import { toast } from "sonner";
import { Alert, AlertDescription, AlertTitle } from "@shpyrd/ui/components/alert";
import { LogoMark, Wordmark } from "@shpyrd/ui/components/brand";
import { Button } from "@shpyrd/ui/components/button";
import { InlineCode } from "@shpyrd/ui/components/inline-code";
import { Skeleton } from "@shpyrd/ui/components/skeleton";
import { Stack } from "@shpyrd/ui/components/stack";
import { api, ApiError } from "@/api/api";
import type { Identity, PublicConfig } from "@/api/types";
import { useBrandColor } from "@/lib/branding";

const article = (role: string) => (/^[aeiou]/.test(role) ? "an" : "a");
const roleWords: Record<string, string> = {
  owner: "Owners administer the workspace and every project, and name other owners.",
  admin: "Admins administer the workspace and every project.",
  member: "Members make projects and administer the ones they make; teams and roles give the rest.",
};

// An invitation link: what the holder was invited to. Signed out, it
// sends them to sign in and back; signed in as the invited, one click
// accepts; signed in as someone else, it says so. Signing in with the
// invited address accepts by itself, so this page mostly confirms.
export function Invite({ token, config, me }: { token: string; config: PublicConfig; me?: Identity }) {
  const queries = useQueryClient();
  useBrandColor(config.workspace?.branding?.color);
  const invitation = useQuery({ queryKey: ["invitation", token], queryFn: () => api.invitation(token), retry: false });
  const accept = useMutation({
    mutationFn: () => api.acceptInvitation(token),
    onSuccess: (r) => {
      toast.success(`You are in, as ${article(r.role)} ${r.role}`);
      queries.invalidateQueries();
      window.location.assign(r.next || "/");
    },
    onError: (e: Error) => toast.error(e.message),
  });
  const inv = invitation.data;
  const error = invitation.error as Error | null;
  const mine = !!me?.email && !!inv && me.email.toLowerCase() === inv.email.toLowerCase();
  const here = `/invite/${encodeURIComponent(token)}`;
  // Signing in with the invited address accepts by itself, so coming
  // back finds the invitation gone: a signed-in person with a role is in.
  const joined = error instanceof ApiError && error.status === 404 && !!me?.roles?.workspace;
  return (
    <div className="relative flex min-h-svh items-center justify-center bg-background px-6">
      <div className="grid w-full max-w-md justify-items-center gap-6 text-center">
        {config.workspace?.branding?.logoUrl ? <img src={config.workspace.branding.logoUrl} alt="" className="h-14 max-w-40 object-contain" /> : <LogoMark className="size-14" />}
        <div className="grid gap-1">
          <h1 className="font-heading text-2xl font-medium">{inv ? inv.workspace.name : (config.workspace?.name ?? "An invitation")}</h1>
          <p className="text-sm text-muted-foreground">{inv ? "You were invited here." : "An invitation"}</p>
        </div>
        <Stack gap="normal" className="w-full text-left">
          {invitation.isLoading && <Skeleton className="h-24 w-full" />}
          {joined && me && (
            <>
              <p className="text-sm">
                You are in: <InlineCode>{me.email}</InlineCode> is {article(me.roles?.workspace ?? "")} <b className="font-medium">{me.roles?.workspace}</b> of this workspace.
              </p>
              <Button size="xl" className="w-full" asChild>
                <a href="/">Open the workspace</a>
              </Button>
            </>
          )}
          {error && !joined && (
            <Alert variant="destructive">
              <AlertTitle>This link does not work</AlertTitle>
              <AlertDescription>{error.message}</AlertDescription>
            </Alert>
          )}
          {inv && (
            <>
              <p className="text-sm">
                {inv.invitedBy ? <InlineCode>{inv.invitedBy}</InlineCode> : "An administrator"} invited <InlineCode>{inv.email}</InlineCode> to join <b className="font-medium">{inv.workspace.name}</b> as {article(inv.role)} <b className="font-medium">{inv.role}</b>
                {inv.team && (
                  <>
                    , in the team <b className="font-medium">{inv.team}</b>
                  </>
                )}
                .
              </p>
              <p className="text-xs text-muted-foreground">{roleWords[inv.role]}</p>
              {inv.expired ? (
                <Alert variant="destructive">
                  <AlertTitle>This invitation has expired</AlertTitle>
                  <AlertDescription>Ask {inv.invitedBy || "an administrator"} to invite you again.</AlertDescription>
                </Alert>
              ) : !me ? (
                <>
                  <p className="text-sm">
                    Sign in with <b className="font-medium">{inv.email}</b> to accept. Any method works, as long as it gives that address.
                  </p>
                  <Button size="xl" className="w-full" icon={<LogIn />} asChild>
                    <a href={`/?next=${encodeURIComponent(here)}`}>Sign in to accept</a>
                  </Button>
                </>
              ) : mine ? (
                <Button size="xl" className="w-full" icon={<UserRoundPlus />} disabled={accept.isPending} onClick={() => accept.mutate()}>
                  Accept and join {inv.workspace.name}
                </Button>
              ) : (
                <>
                  <Alert variant="warning">
                    <AlertTitle>Signed in as someone else</AlertTitle>
                    <AlertDescription>
                      You are signed in as <InlineCode>{me.email || me.name || "the admin token"}</InlineCode>; this invitation is for <InlineCode>{inv.email}</InlineCode>. Sign out, then sign in with that address.
                    </AlertDescription>
                  </Alert>
                  <Button
                    variant="outline"
                    size="lg"
                    className="w-full"
                    onClick={() =>
                      api
                        .logout()
                        .then((r) => window.location.assign(r.redirect || `/?next=${encodeURIComponent(here)}`))
                        .catch((e: Error) => toast.error(e.message))
                    }
                  >
                    Sign out
                  </Button>
                </>
              )}
              <p className="text-xs text-muted-foreground">Not expecting this? Ignore it: nothing happens until the invited address signs in.</p>
            </>
          )}
        </Stack>
      </div>
      <Wordmark className="absolute bottom-5 left-5 h-5 opacity-60" />
    </div>
  );
}
