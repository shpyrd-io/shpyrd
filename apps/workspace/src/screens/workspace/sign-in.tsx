"use client";

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { Badge } from "@shpyrd/ui/components/badge";
import { Button } from "@shpyrd/ui/components/button";
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@shpyrd/ui/components/card";
import { ConfirmDialog } from "@shpyrd/ui/components/confirm-dialog";
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from "@shpyrd/ui/components/dialog";
import { Field } from "@shpyrd/ui/components/field";
import { InlineCode } from "@shpyrd/ui/components/inline-code";
import { Input } from "@shpyrd/ui/components/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@shpyrd/ui/components/select";
import { Stack } from "@shpyrd/ui/components/stack";
import { StatusBadge } from "@shpyrd/ui/components/status-badge";
import { Switch } from "@shpyrd/ui/components/switch";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@shpyrd/ui/components/table";
import { api } from "@/api/api";
import type { DomainClaim, NewLoginMethod, WorkspaceInfo } from "@/api/types";
import { Failed, Loading } from "../project/shared";

const policies: Record<WorkspaceInfo["joinPolicy"], { label: string; text: string }> = {
  open: { label: "Anyone who signs in", text: "Whoever signs in through one of the methods is in; they still need a role to see or open anything." },
  company: { label: "Emails of a claimed domain", text: "New people come in only through the method of a verified company domain below. Who is already in stays." },
  listed: { label: "Invited or listed people", text: "New people come in only if an administrator invited them, listed them in a team, or gave them a role first." },
};

const kinds: Record<string, string> = { google: "Google", microsoft: "Microsoft", github: "GitHub", oidc: "OpenID Connect: Okta, Keycloak, Auth0…" };

// How people get in: through what, who is let in, and the domains the
// company owns.
export function SignIn() {
  return (
    <>
      <Methods />
      <JoinPolicy />
      <DomainClaims />
    </>
  );
}

// The sign-in methods: the platform's, offered to every workspace, and
// the workspace's own identity provider.
function Methods() {
  const queries = useQueryClient();
  const config = useQuery({ queryKey: ["config"], queryFn: api.config, staleTime: 60_000 });
  const ws = useQuery({ queryKey: ["workspace"], queryFn: api.workspace });
  const methods = useQuery({ queryKey: ["login-methods"], queryFn: api.loginMethods, retry: false });
  const refresh = () => {
    queries.invalidateQueries({ queryKey: ["login-methods"] });
    queries.invalidateQueries({ queryKey: ["config"] });
    queries.invalidateQueries({ queryKey: ["workspace"] });
  };
  const remove = useMutation({
    mutationFn: (id: string) => api.removeLoginMethod(id),
    onSuccess: () => {
      toast.success("Method removed", { description: "Who signed in through it keeps their session." });
      refresh();
    },
    onError: (e: Error) => toast.error(e.message),
  });
  const platformToo = useMutation({
    mutationFn: (ownMethodsOnly: boolean) => api.updateWorkspace({ ownMethodsOnly }),
    onSuccess: (w) => {
      toast.success(w.ownMethodsOnly ? "Only the workspace's own methods are offered now" : "The platform's methods are offered again");
      refresh();
    },
    onError: (e: Error) => toast.error(e.message),
  });
  if (!config.data?.extensions.includes("auth-local")) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>Sign-in methods</CardTitle>
          <CardDescription>
            The operator has not turned on the managing of sign-in methods. Ask them to enable the <InlineCode>auth-local</InlineCode> extension.
          </CardDescription>
        </CardHeader>
      </Card>
    );
  }
  const platform = (config.data?.auth.providers ?? []).filter((p) => (p.realm ?? "platform") === "platform");
  const own = methods.data?.connectors ?? [];
  return (
    <Card>
      <CardHeader>
        <CardTitle>Sign-in methods</CardTitle>
        <CardDescription>How people sign in here, and to every app behind sign-in. Bring the company's identity provider, Google Workspace, Microsoft Entra, GitHub or any OpenID Connect provider, so nobody needs one more password; its groups map to teams.</CardDescription>
        {methods.data && (
          <CardAction>
            <AddMethod kinds={methods.data.kinds} callback={methods.data.callback} onDone={refresh} />
          </CardAction>
        )}
      </CardHeader>
      <CardContent>
        <Stack gap="normal">
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
                  <TableHead>Whose</TableHead>
                  <TableHead>Only for</TableHead>
                  <TableHead className="text-right" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {(methods.data!.password || config.data?.auth.password) && (
                  <TableRow>
                    <TableCell className="font-medium">Email and password</TableCell>
                    <TableCell className="text-xs text-muted-foreground">accounts of the platform</TableCell>
                    <TableCell>
                      <Badge variant="outline">the platform</Badge>
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">-</TableCell>
                    <TableCell />
                  </TableRow>
                )}
                {platform.map((p) => (
                  <TableRow key={p.id}>
                    <TableCell className="font-medium">{p.label}</TableCell>
                    <TableCell className="text-xs text-muted-foreground">{kinds[p.kind ?? ""]?.split(":")[0] ?? p.kind}</TableCell>
                    <TableCell>
                      <Badge variant="outline">the platform</Badge>
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">-</TableCell>
                    <TableCell />
                  </TableRow>
                ))}
                {own.map((c) => (
                  <TableRow key={c.id}>
                    <TableCell className="font-medium">
                      {c.name} <span className="ml-1 font-mono text-xs text-muted-foreground">{c.id}</span>
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">{kinds[c.type]?.split(":")[0] ?? c.type}</TableCell>
                    <TableCell>
                      <Badge>this workspace</Badge>
                    </TableCell>
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
              </TableBody>
            </Table>
          )}
          {ws.data && (
            <div className="flex flex-wrap items-center justify-between gap-3 rounded-md border p-3">
              <div className="grid gap-0.5 text-sm">
                <span className="font-medium">Also offer the platform's methods</span>
                <span className="text-xs text-muted-foreground">
                  {ws.data.ownMethodsOnly ? "Off: only the workspace's own methods open the door." : own.length ? "On. Switch it off once the company's method works, so people sign in only through it." : "On. Add a method of your own before switching it off, or nobody could sign in."}
                </span>
              </div>
              <Switch checked={!ws.data.ownMethodsOnly} disabled={platformToo.isPending || (!ws.data.ownMethodsOnly && own.length === 0)} loading={platformToo.isPending} onCheckedChange={(on) => platformToo.mutate(!on)} aria-label="Also offer the platform's methods" />
            </div>
          )}
        </Stack>
      </CardContent>
    </Card>
  );
}

function AddMethod({ kinds: offered, callback, onDone }: { kinds: string[]; callback: string; onDone: () => void }) {
  const [open, setOpen] = useState(false);
  const blank: NewLoginMethod = { type: offered[0] ?? "google", clientId: "", clientSecret: "" };
  const [form, setForm] = useState<NewLoginMethod>(blank);
  const set = (k: keyof NewLoginMethod) => (e: React.ChangeEvent<HTMLInputElement>) => setForm({ ...form, [k]: e.target.value });
  const add = useMutation({
    mutationFn: () => api.addLoginMethod({ ...form, id: form.id || undefined, name: form.name || undefined }),
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

// What happens when someone signs in for the first time.
function JoinPolicy() {
  const queries = useQueryClient();
  const ws = useQuery({ queryKey: ["workspace"], queryFn: api.workspace });
  const setPolicy = useMutation({
    mutationFn: (joinPolicy: WorkspaceInfo["joinPolicy"]) => api.updateWorkspace({ joinPolicy }),
    onSuccess: () => {
      queries.invalidateQueries({ queryKey: ["workspace"] });
      toast.success("Who may join: set");
    },
    onError: (e: Error) => toast.error(e.message),
  });
  return (
    <Card>
      <CardHeader>
        <CardTitle>Who may join</CardTitle>
        <CardDescription>{ws.data ? policies[ws.data.joinPolicy].text : "…"}</CardDescription>
      </CardHeader>
      <CardContent>
        {ws.isLoading ? (
          <Loading rows={1} />
        ) : (
          <Select value={ws.data?.joinPolicy} disabled={setPolicy.isPending} onValueChange={(v) => setPolicy.mutate(v as WorkspaceInfo["joinPolicy"])}>
            <SelectTrigger className="w-72">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {(Object.keys(policies) as WorkspaceInfo["joinPolicy"][]).map((p) => (
                <SelectItem key={p} value={p}>
                  {policies[p].label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        )}
      </CardContent>
    </Card>
  );
}

// The email domains the company owns, proved by a record.
function DomainClaims() {
  const queries = useQueryClient();
  const config = useQuery({ queryKey: ["config"], queryFn: api.config, staleTime: 60_000 });
  const claims = useQuery({ queryKey: ["domain-claims"], queryFn: api.domainClaims, retry: false });
  const [domain, setDomain] = useState("");
  const [connector, setConnector] = useState("any");
  const refresh = () => queries.invalidateQueries({ queryKey: ["domain-claims"] });
  const claim = useMutation({
    mutationFn: () => api.claimDomain(domain.trim().toLowerCase(), connector === "any" ? "" : connector),
    onSuccess: () => {
      toast.success("Domain claimed", { description: "Publish the TXT record, then verify." });
      setDomain("");
      refresh();
    },
    onError: (e: Error) => toast.error(e.message),
  });
  const verify = useMutation({
    mutationFn: (d: DomainClaim) => api.verifyDomainClaim(d.domain),
    onSuccess: (d) => {
      toast.success(`${d.domain} verified`);
      refresh();
    },
    onError: (e: Error) => toast.error(e.message),
  });
  const unclaim = useMutation({ mutationFn: (d: DomainClaim) => api.unclaimDomain(d.domain), onSuccess: refresh, onError: (e: Error) => toast.error(e.message) });
  const providers = (config.data?.auth.providers ?? []).map((p) => ({ id: p.id, name: p.label + (p.workspace ? "" : ", the platform's") }));
  const nameOf = (id: string) => providers.find((p) => p.id === id)?.name ?? id;
  const valid = /^[a-z0-9.-]+\.[a-z]{2,}$/i.test(domain.trim());
  return (
    <Card>
      <CardHeader>
        <CardTitle>Company domains</CardTitle>
        <CardDescription>Prove you own an email domain with a DNS record. Accounts of a verified domain count as the company's people and, when a method is chosen, sign in only through it: no company address behind a password someone made up.</CardDescription>
      </CardHeader>
      <CardContent>
        <Stack gap="normal">
          {claims.isLoading ? (
            <Loading rows={1} />
          ) : claims.error ? (
            <Failed what="the domains" error={claims.error} />
          ) : (claims.data ?? []).length === 0 ? (
            <p className="text-sm text-muted-foreground">None claimed yet.</p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Domain</TableHead>
                  <TableHead>Sign in through</TableHead>
                  <TableHead>Record</TableHead>
                  <TableHead className="text-right" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {claims.data!.map((d) => (
                  <TableRow key={d.domain}>
                    <TableCell>
                      <InlineCode>{d.domain}</InlineCode>
                    </TableCell>
                    <TableCell className="text-xs">{d.connector ? nameOf(d.connector) : <span className="text-muted-foreground">any method</span>}</TableCell>
                    <TableCell className="font-mono text-[11px] text-muted-foreground">
                      {d.record} TXT {d.recordValue}
                    </TableCell>
                    <TableCell className="text-right">
                      <Stack direction="horizontal" gap="tight" justify="end" align="center">
                        {d.verified ? (
                          <StatusBadge type="success">verified</StatusBadge>
                        ) : (
                          <Button size="xs" variant="outline" disabled={verify.isPending} onClick={() => verify.mutate(d)}>
                            Verify
                          </Button>
                        )}
                        <ConfirmDialog
                          trigger={<Button variant="ghost" size="icon-xs" icon={<Trash2 />} aria-label={`Remove ${d.domain}`} />}
                          variant="destructive"
                          title={`Remove ${d.domain}?`}
                          description="Its accounts stop counting as the company's."
                          action="Remove"
                          onConfirm={() => unclaim.mutate(d)}
                        />
                      </Stack>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
          <form
            className="grid gap-2 sm:grid-cols-[1fr_auto_auto]"
            onSubmit={(e) => {
              e.preventDefault();
              if (valid) claim.mutate();
            }}
          >
            <Input value={domain} onChange={(e) => setDomain(e.target.value)} placeholder="acme.com" aria-label="Domain" />
            <Select value={connector} onValueChange={setConnector}>
              <SelectTrigger className="w-64">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="any">any method</SelectItem>
                {providers.map((p) => (
                  <SelectItem key={p.id} value={p.id}>
                    {p.name} only
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Button type="submit" icon={<Plus />} disabled={!valid || claim.isPending}>
              Claim
            </Button>
          </form>
        </Stack>
      </CardContent>
    </Card>
  );
}
