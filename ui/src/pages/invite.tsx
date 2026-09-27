import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Loader2, LogIn, UserPlus } from "lucide-react";
import { toast } from "sonner";

import { api, ApiError, type Identity } from "@/lib/api";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Wordmark } from "@/components/brand";

/**
 * The invitation link (RFC-0033): /invite/<token>. Shows what the holder
 * was invited to; signed out, it sends them to sign in and back here;
 * signed in as the invitee, one click accepts; signed in as someone else,
 * it says so. Signing in with the invited address accepts on its own, so
 * this page mostly confirms.
 */
export function InvitePage({
  token,
  me,
}: {
  token: string;
  me: Identity | undefined;
}) {
  const qc = useQueryClient();
  const invitation = useQuery({
    queryKey: ["invitation", token],
    queryFn: () => api.invitation(token),
    retry: false,
  });
  const accept = useMutation({
    mutationFn: () => api.acceptInvitation(token),
    onSuccess: (r) => {
      toast.success(`You joined as ${article(r.role)} ${r.role}`);
      qc.invalidateQueries();
      window.location.assign(r.next || "/");
    },
    onError: (e: Error) => toast.error(e.message),
  });

  const inv = invitation.data;
  const err = invitation.error as ApiError | Error | null;
  const mine = !!me?.email && !!inv && me.email.toLowerCase() === inv.email;
  const here = `/invite/${encodeURIComponent(token)}`;
  // Signing in with the invited address accepts on its own, so coming back
  // to the link finds it gone: a signed-in person with a role is in.
  const joined =
    err instanceof ApiError && err.status === 404 && !!me?.roles?.workspace;

  return (
    <div className="flex min-h-screen items-center justify-center bg-background p-4">
      <Card className="w-full max-w-md">
        <CardHeader>
          <CardTitle>
            <Wordmark className="h-8" />
          </CardTitle>
          <CardDescription>
            {inv ? (
              <>
                You were invited to <strong>{inv.workspace.name}</strong>.
              </>
            ) : (
              "An invitation"
            )}
          </CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4">
          {invitation.isLoading && <Skeleton className="h-20 w-full" />}
          {joined && me && (
            <div className="grid gap-3 text-sm">
              <p>
                You are in:{" "}
                <span className="font-mono text-xs">{me.email}</span> is{" "}
                {article(me.roles?.workspace ?? "")}{" "}
                <strong>{me.roles?.workspace}</strong> of this workspace.
              </p>
              <Button asChild size="lg">
                <a href="/">Open the dashboard</a>
              </Button>
            </div>
          )}
          {err && !joined && (
            <Alert variant="destructive">
              <AlertTitle>This link does not work</AlertTitle>
              <AlertDescription>{err.message}</AlertDescription>
            </Alert>
          )}
          {inv && (
            <div className="grid gap-3 text-sm">
              <p>
                {inv.invitedBy ? (
                  <>
                    <span className="font-mono text-xs">{inv.invitedBy}</span>{" "}
                    invited
                  </>
                ) : (
                  "An administrator invited"
                )}{" "}
                <span className="font-mono text-xs">{inv.email}</span> to join{" "}
                <strong>{inv.workspace.name}</strong> as {article(inv.role)}{" "}
                <strong>{inv.role}</strong>
                {inv.team ? (
                  <>
                    , in team <strong>{inv.team}</strong>
                  </>
                ) : null}
                .
              </p>
              <p className="text-xs text-muted-foreground">
                {roleHelp(inv.role)}
              </p>
              {inv.expired ? (
                <Alert variant="destructive">
                  <AlertTitle>This invitation has expired</AlertTitle>
                  <AlertDescription>
                    Ask {inv.invitedBy || "an administrator"} to invite you
                    again.
                  </AlertDescription>
                </Alert>
              ) : !me ? (
                <>
                  <p>
                    Sign in with <strong>{inv.email}</strong> to accept. Any
                    sign-in method works, as long as it gives that address.
                  </p>
                  <Button asChild size="lg">
                    <a href={`/?next=${encodeURIComponent(here)}`}>
                      <LogIn data-icon="inline-start" /> Sign in to accept
                    </a>
                  </Button>
                </>
              ) : mine ? (
                <Button
                  size="lg"
                  disabled={accept.isPending}
                  onClick={() => accept.mutate()}
                >
                  {accept.isPending ? (
                    <Loader2
                      data-icon="inline-start"
                      className="animate-spin"
                    />
                  ) : (
                    <UserPlus data-icon="inline-start" />
                  )}
                  Accept and join {inv.workspace.name}
                </Button>
              ) : (
                <>
                  <Alert>
                    <AlertTitle>Signed in as someone else</AlertTitle>
                    <AlertDescription>
                      You are signed in as{" "}
                      <span className="font-mono text-xs">
                        {me.email || me.name || "the admin token"}
                      </span>
                      ; this invitation is for{" "}
                      <span className="font-mono text-xs">{inv.email}</span>.
                      Sign out, then sign in with that address.
                    </AlertDescription>
                  </Alert>
                  <Button
                    variant="outline"
                    onClick={() =>
                      api
                        .logout()
                        .then((r) =>
                          window.location.assign(
                            r.redirect || `/?next=${encodeURIComponent(here)}`,
                          ),
                        )
                        .catch((e: Error) => toast.error(e.message))
                    }
                  >
                    Sign out
                  </Button>
                </>
              )}
              <p className="text-xs text-muted-foreground">
                Not expecting this? Ignore it: nothing happens until the invited
                address signs in.
              </p>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}

function article(role: string) {
  return /^[aeiou]/.test(role) ? "an" : "a";
}

function roleHelp(role: string) {
  switch (role) {
    case "owner":
      return "Owners administer the workspace and every project, and name other owners.";
    case "admin":
      return "Admins administer the workspace and every project.";
    default:
      return "Members create projects and administer the ones they create; teams and grants give access to the rest.";
  }
}
