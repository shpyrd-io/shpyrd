import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { KeyRound } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { LoginMethodsCard } from "@/components/signin-settings";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";

/**
 * Sign-in at the console (RFC-0080): the console's own methods, whether
 * the password form stays on its login page, and the defaults every
 * workspace offers until it brings its own identity provider. Three
 * scopes, three cards; nothing the console offers reaches a workspace.
 */
export function ConsoleSignInPage() {
  const config = useQuery({
    queryKey: ["config"],
    queryFn: api.config,
    staleTime: 60_000,
  });
  const authLocal = !!config.data?.extensions?.includes("auth-local");
  return (
    <div className="grid gap-6">
      <div>
        <h1 className="flex items-center gap-2 text-2xl font-semibold">
          <KeyRound className="size-6" /> Sign-in
        </h1>
        <p className="text-sm text-muted-foreground">
          Who may open this console, and what a new workspace offers its
          people. The two never mix: lock the console to your organisation and
          leave workspaces as open as they want.
        </p>
      </div>
      {authLocal ? (
        <>
          <LoginMethodsCard scope="console" />
          <ConsolePasswordCard />
          <LoginMethodsCard scope="platform" />
        </>
      ) : (
        <Card>
          <CardHeader>
            <CardTitle>Sign-in methods</CardTitle>
            <CardDescription>
              Enable the <code>auth-local</code> extension to manage sign-in
              methods here (<code>shpyrd-ctl extensions enable auth-local</code>
              ).
            </CardDescription>
          </CardHeader>
        </Card>
      )}
    </div>
  );
}

/** The password form on the console's login page: on until an identity provider is the door. */
function ConsolePasswordCard() {
  const qc = useQueryClient();
  const config = useQuery({
    queryKey: ["config"],
    queryFn: api.config,
    staleTime: 60_000,
  });
  const on = !!config.data?.auth?.password;
  const otherMethods = (config.data?.auth?.providers ?? []).length;
  const toggle = useMutation({
    mutationFn: (consolePasswordSignIn: boolean) =>
      api.patchClusterSettings({ consolePasswordSignIn }),
    onSuccess: (r) => {
      toast.success(
        r.consolePasswordSignIn
          ? "The console offers email and password again"
          : "The console signs in through its identity providers only",
      );
      qc.invalidateQueries({ queryKey: ["config"] });
    },
    onError: (e: Error) => toast.error(e.message),
  });
  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">Email and password at the console</CardTitle>
        <CardDescription>
          {on
            ? otherMethods > 0
              ? "On. Switch it off once your identity provider works, and the console has exactly the doors above."
              : "On. Add a method above before switching it off, or nobody could sign in."
            : "Off: the console signs in through its identity providers only."}
        </CardDescription>
      </CardHeader>
      <CardContent>
        <Button
          size="sm"
          variant="outline"
          disabled={toggle.isPending || (on && otherMethods === 0)}
          onClick={() => toggle.mutate(!on)}
        >
          {on ? "Switch off" : "Switch on"}
        </Button>
      </CardContent>
    </Card>
  );
}
