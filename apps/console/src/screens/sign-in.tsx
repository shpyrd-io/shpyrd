"use client";

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@shpyrd/ui/components/button";
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@shpyrd/ui/components/card";
import { ConfirmDialog } from "@shpyrd/ui/components/confirm-dialog";
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from "@shpyrd/ui/components/dialog";
import { Field } from "@shpyrd/ui/components/field";
import { InlineCode } from "@shpyrd/ui/components/inline-code";
import { Input } from "@shpyrd/ui/components/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@shpyrd/ui/components/select";
import { Switch } from "@shpyrd/ui/components/switch";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@shpyrd/ui/components/table";
import { api } from "@/api/api";
import type { MethodsScope, NewLoginMethod } from "@/api/types";
import { Failed, Loading } from "./shared";

const kinds: Record<string, string> = { google: "Google", microsoft: "Microsoft", github: "GitHub", oidc: "OpenID Connect: Okta, Keycloak, Auth0…" };

// Who may open this console, and what a new workspace offers its people.
// The two never mix: the console can be locked to the organisation while
// workspaces stay as open as they want.
export function SignIn() {
  const config = useQuery({ queryKey: ["config"], queryFn: api.config, staleTime: 60_000 });
  if (!config.data?.extensions.includes("auth-local")) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>Sign-in methods</CardTitle>
          <CardDescription>
            Enable the <InlineCode>auth-local</InlineCode> extension to manage sign-in methods here: <InlineCode>shpyrd-ctl extensions enable auth-local</InlineCode>.
          </CardDescription>
        </CardHeader>
      </Card>
    );
  }
  if (!config.data?.extensions.includes("sso")) {
    return (
      <>
        <Card>
          <CardHeader>
            <CardTitle>Sign-in methods</CardTitle>
            <CardDescription>
              The console and every workspace sign people in with an email and a password. Signing in through an identity provider (Google Workspace, Microsoft Entra, GitHub or any OpenID Connect provider) comes with the enterprise license.
            </CardDescription>
          </CardHeader>
        </Card>
        <ConsolePassword />
      </>
    );
  }
  return (
    <>
      <Methods scope="console" />
      <ConsolePassword />
      <Methods scope="platform" />
    </>
  );
}

const words: Record<MethodsScope, { title: string; text: string }> = {
  console: { title: "Sign-in methods of this console", text: "Who may sign in here, to administer the platform. Add your organisation's identity provider, Google Workspace kept to your domain for instance, then switch the password off below, and the console has exactly these doors. Nothing here reaches a workspace." },
  platform: { title: "Default sign-in methods for workspaces", text: "How people sign in to workspaces that have not brought their own identity provider, and to every app behind sign-in. Every workspace's sign-in page offers these until it hides them." },
};

function Methods({ scope }: { scope: MethodsScope }) {
  const queries = useQueryClient();
  const methods = useQuery({ queryKey: ["login-methods", scope], queryFn: () => api.loginMethods(scope), retry: false });
  const refresh = () => {
    queries.invalidateQueries({ queryKey: ["login-methods", scope] });
    queries.invalidateQueries({ queryKey: ["config"] });
  };
  const remove = useMutation({
    mutationFn: (id: string) => api.removeLoginMethod(scope, id),
    onSuccess: () => {
      toast.success("Method removed", { description: "Who signed in through it keeps their session." });
      refresh();
    },
    onError: (e: Error) => toast.error(e.message),
  });
  return (
    <Card>
      <CardHeader>
        <CardTitle>{words[scope].title}</CardTitle>
        <CardDescription>{words[scope].text}</CardDescription>
        {methods.data && (
          <CardAction>
            <AddMethod scope={scope} kinds={methods.data.kinds} callback={methods.data.callback} onDone={refresh} />
          </CardAction>
        )}
      </CardHeader>
      <CardContent>
        {methods.isLoading ? (
          <Loading />
        ) : methods.error ? (
          <Failed what="the sign-in methods" error={methods.error} />
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Method</TableHead>
                <TableHead>Kind</TableHead>
                <TableHead>Only for</TableHead>
                <TableHead className="text-right" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {methods.data!.password && (
                <TableRow>
                  <TableCell className="font-medium">Email and password</TableCell>
                  <TableCell className="text-xs text-muted-foreground">accounts of the platform</TableCell>
                  <TableCell className="text-xs text-muted-foreground">-</TableCell>
                  <TableCell />
                </TableRow>
              )}
              {methods.data!.connectors.map((c) => (
                <TableRow key={c.id}>
                  <TableCell className="font-medium">
                    {c.name} <span className="ml-1 font-mono text-xs text-muted-foreground">{c.id}</span>
                  </TableCell>
                  <TableCell className="text-xs text-muted-foreground">{kinds[c.type]?.split(":")[0] ?? c.type}</TableCell>
                  <TableCell className="text-xs text-muted-foreground">{c.detail || "-"}</TableCell>
                  <TableCell className="text-right">
                    <ConfirmDialog
                      trigger={<Button variant="ghost" size="icon-xs" icon={<Trash2 />} aria-label={`Remove ${c.name}`} />}
                      variant="destructive"
                      title={`Remove ${c.name}?`}
                      description="Its button leaves the sign-in page. Who signed in through it keeps their session."
                      action="Remove"
                      onConfirm={() => remove.mutate(c.id)}
                    />
                  </TableCell>
                </TableRow>
              ))}
              {!methods.data!.password && methods.data!.connectors.length === 0 && (
                <TableRow>
                  <TableCell colSpan={4} className="text-center text-muted-foreground">
                    No method yet.
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  );
}

// The password form on the console's sign-in page: on until an identity
// provider is the door.
function ConsolePassword() {
  const queries = useQueryClient();
  const config = useQuery({ queryKey: ["config"], queryFn: api.config, staleTime: 60_000 });
  const on = !!config.data?.auth.password;
  const others = (config.data?.auth.providers ?? []).length;
  const toggle = useMutation({
    mutationFn: (consolePasswordSignIn: boolean) => api.patchSettings({ consolePasswordSignIn }),
    onSuccess: (r) => {
      toast.success(r.consolePasswordSignIn ? "The console offers email and password again" : "The console signs in through its identity providers only");
      queries.invalidateQueries({ queryKey: ["config"] });
    },
    onError: (e: Error) => toast.error(e.message),
  });
  return (
    <Card>
      <CardContent>
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="grid gap-0.5 text-sm">
            <span className="font-medium">Email and password at the console</span>
            <span className="text-xs text-muted-foreground">{on ? (others > 0 ? "On. Switch it off once your identity provider works, and the console has exactly the doors above." : "On. Add a method above before switching it off, or nobody could sign in.") : "Off: the console signs in through its identity providers only."}</span>
          </div>
          <Switch checked={on} disabled={toggle.isPending || (on && others === 0)} loading={toggle.isPending} onCheckedChange={(next) => toggle.mutate(next)} aria-label="Email and password at the console" />
        </div>
      </CardContent>
    </Card>
  );
}

function AddMethod({ scope, kinds: offered, callback, onDone }: { scope: MethodsScope; kinds: string[]; callback: string; onDone: () => void }) {
  const [open, setOpen] = useState(false);
  const blank: NewLoginMethod = { type: offered[0] ?? "google", clientId: "", clientSecret: "" };
  const [form, setForm] = useState<NewLoginMethod>(blank);
  const set = (k: keyof NewLoginMethod) => (e: React.ChangeEvent<HTMLInputElement>) => setForm({ ...form, [k]: e.target.value });
  const add = useMutation({
    mutationFn: () => api.addLoginMethod(scope, { ...form, id: form.id || undefined, name: form.name || undefined }),
    onSuccess: () => {
      toast.success("Method added", { description: "Its button is on the sign-in page now." });
      setOpen(false);
      setForm(blank);
      onDone();
    },
    onError: (e: Error) => toast.error(e.message),
  });
  const valid = form.clientId.trim() !== "" && form.clientSecret !== "" && (form.type !== "oidc" || !!form.issuer?.trim());
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button size="sm" icon={<Plus />}>
          Add
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader divider>
          <DialogTitle>Add a sign-in method</DialogTitle>
          <DialogDescription>
            Register an OAuth application at the provider with this callback, then paste its client id and secret: <InlineCode>{callback}</InlineCode>
          </DialogDescription>
        </DialogHeader>
        <form
          className="grid gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            if (valid) add.mutate();
          }}
        >
          <Field label="Provider">
            <Select value={form.type} onValueChange={(type) => setForm({ ...form, type })}>
              <SelectTrigger className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {offered.map((k) => (
                  <SelectItem key={k} value={k}>
                    {kinds[k] ?? k}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
          {form.type === "oidc" && (
            <Field label="Issuer">
              <Input value={form.issuer ?? ""} onChange={set("issuer")} placeholder="https://acme.okta.com" className="font-mono text-xs" />
            </Field>
          )}
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="Client id">
              <Input value={form.clientId} onChange={set("clientId")} autoComplete="off" className="font-mono text-xs" />
            </Field>
            <Field label="Client secret">
              <Input type="password" value={form.clientSecret} onChange={set("clientSecret")} autoComplete="off" className="font-mono text-xs" />
            </Field>
          </div>
          {form.type === "google" && (
            <Field label="Only this Google Workspace domain" hint="Empty: any Google account.">
              <Input value={form.hostedDomain ?? ""} onChange={set("hostedDomain")} placeholder="acme.com" />
            </Field>
          )}
          {form.type === "microsoft" && (
            <Field label="Only this Entra tenant" hint="Its id or its domain. Empty: any.">
              <Input value={form.tenant ?? ""} onChange={set("tenant")} placeholder="acme.com" />
            </Field>
          )}
          {form.type === "github" && (
            <Field label="Only members of this organisation" hint="Its teams become groups. Empty: any GitHub account.">
              <Input value={form.org ?? ""} onChange={set("org")} placeholder="acme" />
            </Field>
          )}
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="Button text" hint="Empty: the provider's name.">
              <Input value={form.name ?? ""} onChange={set("name")} placeholder={kinds[form.type]?.split(":")[0]} />
            </Field>
            <Field label="Identifier" hint="Empty: made from the provider.">
              <Input value={form.id ?? ""} onChange={set("id")} placeholder={form.type} className="font-mono text-xs" />
            </Field>
          </div>
          <DialogFooter>
            <DialogClose asChild>
              <Button type="button" variant="outline">
                Cancel
              </Button>
            </DialogClose>
            <Button type="submit" disabled={!valid || add.isPending}>
              Add
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

