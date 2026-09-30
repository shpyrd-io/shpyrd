"use client";

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowRight, KeyRound, Lock, UserRound } from "lucide-react";
import { toast } from "sonner";
import { Alert, AlertDescription, AlertTitle } from "@shpyrd/ui/components/alert";
import { LogoMark, Wordmark } from "@shpyrd/ui/components/brand";
import { Button } from "@shpyrd/ui/components/button";
import { Field } from "@shpyrd/ui/components/field";
import { Input } from "@shpyrd/ui/components/input";
import { Stack } from "@shpyrd/ui/components/stack";
import { api, ApiError } from "@/api/api";
import { useBrandColor } from "@/lib/branding";
import type { PublicConfig } from "@/api/types";

// The door: a whole page in the plain style, with the ways in the
// workspace offers. A provider sends the browser away and back; the
// password and the token are asked here.
export function Login({ config }: { config: PublicConfig }) {
  const queries = useQueryClient();
  // Back to where the person was going: what the address asks for, else
  // this page. Only a path of this site; the server checks too.
  const asked = new URLSearchParams(window.location.search).get("next");
  const next = asked && asked.startsWith("/") && !asked.startsWith("//") ? asked : window.location.pathname === "/" ? "/" : window.location.pathname + window.location.search;
  const providers = config.auth.providers;
  const [way, setWay] = useState<"password" | "token">("password");
  const done = () => queries.invalidateQueries({ queryKey: ["me"] });
  useBrandColor(config.workspace?.branding?.color);
  return (
    <div className="relative flex min-h-svh items-center justify-center bg-background px-6">
      <div className="grid w-full max-w-sm justify-items-center gap-6 text-center">
        {config.workspace?.branding?.logoUrl ? (
          <img src={config.workspace.branding.logoUrl} alt="" className="h-14 max-w-40 object-contain" />
        ) : (
          <LogoMark className="size-14" />
        )}
        <div className="grid gap-1">
          <h1 className="font-heading text-2xl font-medium">{config.consoleHost ?? "shpyrd console"}</h1>
          <p className="text-sm text-muted-foreground">Sign in to administer the platform.</p>
        </div>
        <Stack gap="normal" className="w-full text-left">
          {providers.map((p) => (
            <Button key={p.id} size="xl" variant="outline" className="w-full" iconEnd={<ArrowRight />} asChild>
              <a href={`/api/auth/login?provider=${encodeURIComponent(p.id)}&next=${encodeURIComponent(next)}`}>Continue with {p.label}</a>
            </Button>
          ))}
          {providers.length > 0 && (config.auth.password || config.auth.token) && (
            <div className="flex items-center gap-3 text-xs text-muted-foreground">
              <span className="h-px flex-1 bg-border" />
              or
              <span className="h-px flex-1 bg-border" />
            </div>
          )}
          {way === "password" && config.auth.password && <Password next={next} onDone={done} />}
          {way === "token" && config.auth.token && <Token next={next} onDone={done} />}
          {config.auth.password && config.auth.token && (
            <button type="button" className="text-xs text-muted-foreground underline-offset-4 hover:underline" onClick={() => setWay(way === "password" ? "token" : "password")}>
              {way === "password" ? "Sign in with a token instead" : "Sign in with email and password instead"}
            </button>
          )}
          {!config.auth.password && !config.auth.token && providers.length === 0 && (
            <Alert variant="warning">
              <AlertTitle>No way in</AlertTitle>
              <AlertDescription>This workspace offers no sign-in method yet. Ask its owner.</AlertDescription>
            </Alert>
          )}
        </Stack>
      </div>
      <Wordmark className="absolute bottom-5 left-5 h-5 opacity-60" />
    </div>
  );
}

function Password({ next, onDone }: { next: string; onDone: () => void }) {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const signIn = useMutation({
    mutationFn: () => api.passwordLogin({ email: email.trim(), password, next }),
    onSuccess: (r) => (r.next && r.next !== window.location.pathname + window.location.search ? window.location.assign(r.next) : onDone()),
    onError: (e: Error) => setError(e instanceof ApiError && e.status === 401 ? "The email or the password is not right." : e.message),
  });
  return (
    <form
      className="grid gap-4"
      onSubmit={(e) => {
        e.preventDefault();
        setError(null);
        signIn.mutate();
      }}
    >
      <Field label="Email address" error={error ?? undefined}>
        <Input size="lg" type="email" icon={<UserRound />} divider placeholder="you@example.com" value={email} onChange={(e) => setEmail(e.target.value)} autoComplete="email" autoFocus />
      </Field>
      <Field label="Password">
        <Input size="lg" type="password" icon={<Lock />} divider placeholder="Password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="current-password" />
      </Field>
      <Button type="submit" size="xl" className="w-full" iconEnd={<ArrowRight />} disabled={!email.trim() || !password || signIn.isPending}>
        Sign in
      </Button>
    </form>
  );
}

function Token({ next, onDone }: { next: string; onDone: () => void }) {
  const [token, setToken] = useState("");
  const signIn = useMutation({
    mutationFn: () => api.tokenLogin({ token: token.trim(), next }),
    onSuccess: (r) => (r.next && r.next !== window.location.pathname + window.location.search ? window.location.assign(r.next) : onDone()),
    onError: (e: Error) => toast.error(e.message),
  });
  return (
    <form
      className="grid gap-4"
      onSubmit={(e) => {
        e.preventDefault();
        signIn.mutate();
      }}
    >
      <Field label="Token" hint="The admin token, or one made under API tokens.">
        <Input size="lg" type="password" icon={<KeyRound />} divider placeholder="shp_…" value={token} onChange={(e) => setToken(e.target.value)} autoFocus className="font-mono" />
      </Field>
      <Button type="submit" size="xl" className="w-full" iconEnd={<ArrowRight />} disabled={!token.trim() || signIn.isPending}>
        Sign in
      </Button>
    </form>
  );
}

// Nothing is shown until the server says whether a session is needed, so
// the sign-in page never flashes for someone who is signed in.
export function useDoor() {
  const config = useQuery({ queryKey: ["config"], queryFn: api.config, staleTime: 60_000 });
  const me = useQuery({ queryKey: ["me"], queryFn: api.me, staleTime: 60_000, retry: false, enabled: !!config.data?.authRequired });
  if (config.isLoading) return { state: "waiting" as const };
  if (config.error || !config.data) return { state: "failed" as const, error: config.error as Error };
  if (!config.data.authRequired) return { state: "open" as const, config: config.data, me: me.data };
  if (me.isLoading) return { state: "waiting" as const };
  if (me.error) return { state: "closed" as const, config: config.data, me: undefined };
  return { state: "open" as const, config: config.data, me: me.data };
}
