"use client";

import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Button } from "@shpyrd/ui/components/button";
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@shpyrd/ui/components/card";
import { Field } from "@shpyrd/ui/components/field";
import { InfoTable, InfoTableItem } from "@shpyrd/ui/components/info-table";
import { InlineCode } from "@shpyrd/ui/components/inline-code";
import { Input } from "@shpyrd/ui/components/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@shpyrd/ui/components/select";
import { StatusBadge } from "@shpyrd/ui/components/status-badge";
import { api } from "@/api/api";
import type { LicenseStatus, Limits, SleepDefaults, WorkspaceSettings } from "@/api/types";
import { Failed, Loading } from "./shared";

// The platform's own knobs: its workspace's limits and sleep defaults, and
// the enterprise license where the build has it. Who may open the console
// is under Console users.
export function Settings() {
  const config = useQuery({ queryKey: ["config"], queryFn: api.config, staleTime: 60_000 });
  if (config.isLoading) return <Loading rows={1} />;
  return (
    <div className="grid gap-6">
      {config.data?.extensions.includes("license") && <LicenseCard />}
      <WorkspaceCard />
    </div>
  );
}

// The form's fields, as text: empty is none.
type Form = { projects: string; instances: string; cpu: string; memory: string; storage: string; appsAfter: string; appsResuming: "page" | "wait"; databasesAfter: string };

export function formOf(s: Pick<WorkspaceSettings, "limits" | "sleep">): Form {
  const l = s.limits ?? {};
  const sl = s.sleep ?? {};
  return {
    projects: l.projects ? String(l.projects) : "",
    instances: l.instances ? String(l.instances) : "",
    cpu: l.cpu ?? "",
    memory: l.memory ?? "",
    storage: l.storage ?? "",
    appsAfter: sl.appsAfter ?? "",
    appsResuming: sl.appsResuming === "page" ? "page" : "wait",
    databasesAfter: sl.databasesAfter ?? "",
  };
}

// settingsOf turns the form back into what the API takes: what is empty is
// left out, and nothing left is null.
export function settingsOf(f: Form): { limits: Limits | null; sleep: SleepDefaults | null } {
  const limits: Limits = {};
  if (f.projects.trim()) limits.projects = Number(f.projects);
  if (f.instances.trim()) limits.instances = Number(f.instances);
  if (f.cpu.trim()) limits.cpu = f.cpu.trim();
  if (f.memory.trim()) limits.memory = f.memory.trim();
  if (f.storage.trim()) limits.storage = f.storage.trim();
  const sleep: SleepDefaults = {};
  if (f.appsAfter.trim()) {
    sleep.appsAfter = f.appsAfter.trim();
    sleep.appsResuming = f.appsResuming;
  }
  if (f.databasesAfter.trim()) sleep.databasesAfter = f.databasesAfter.trim();
  return { limits: Object.keys(limits).length ? limits : null, sleep: Object.keys(sleep).length ? sleep : null };
}

function WorkspaceCard() {
  const queries = useQueryClient();
  const current = useQuery({ queryKey: ["workspace-settings"], queryFn: api.workspaceSettings });
  const [form, setForm] = useState<Form | null>(null);
  useEffect(() => {
    if (current.data && !form) setForm(formOf(current.data));
  }, [current.data, form]);
  const save = useMutation({
    mutationFn: (f: Form) => api.putWorkspaceSettings(settingsOf(f)),
    onSuccess: (r) => {
      toast.success("The workspace's settings changed");
      queries.setQueryData(["workspace-settings"], r);
      setForm(formOf(r));
    },
    onError: (e: Error) => toast.error(e.message),
  });
  const set = (k: keyof Form) => (e: React.ChangeEvent<HTMLInputElement>) => setForm((f) => (f ? { ...f, [k]: e.target.value } : f));
  const u = current.data?.usage;
  return (
    <Card>
      <CardHeader>
        <CardTitle>Workspace</CardTitle>
        <CardDescription>
          What the projects of <InlineCode>{current.data?.workspace ?? "the workspace"}</InlineCode> may use together, and when the projects and databases that set nothing of their own go to sleep. An empty field is no limit. Also from the CLI: <InlineCode>shpyrd-ctl workspace limits</InlineCode>, <InlineCode>shpyrd-ctl workspace sleep</InlineCode>.
        </CardDescription>
      </CardHeader>
      <CardContent>
        {current.isLoading || !form ? (
          current.error ? <Failed what="the workspace's settings" error={current.error} /> : <Loading rows={2} />
        ) : (
          <form
            className="grid gap-6"
            onSubmit={(e) => {
              e.preventDefault();
              save.mutate(form);
            }}
          >
            <div className="grid gap-4 sm:grid-cols-5">
              <Field label="Projects" hint={u ? `${u.projects} now` : undefined}>
                <Input inputMode="numeric" value={form.projects} onChange={set("projects")} placeholder="no limit" />
              </Field>
              <Field label="Instances" hint={u ? `${u.instances} now` : undefined}>
                <Input inputMode="numeric" value={form.instances} onChange={set("instances")} placeholder="no limit" />
              </Field>
              <Field label="CPU" hint={u ? `${u.cpu} now` : undefined}>
                <Input value={form.cpu} onChange={set("cpu")} placeholder="no limit" />
              </Field>
              <Field label="Memory" hint={u ? `${u.memory} now` : undefined}>
                <Input value={form.memory} onChange={set("memory")} placeholder="no limit" />
              </Field>
              <Field label="Storage" hint={u ? `${u.storage} now` : undefined}>
                <Input value={form.storage} onChange={set("storage")} placeholder="no limit" />
              </Field>
            </div>
            <div className="grid gap-4 sm:grid-cols-3">
              <Field label="Apps sleep after" hint="5m to 24h; empty: never">
                <Input value={form.appsAfter} onChange={set("appsAfter")} placeholder="never" />
              </Field>
              <Field label="Waking up">
                <Select value={form.appsResuming} onValueChange={(v) => setForm((f) => (f ? { ...f, appsResuming: v as Form["appsResuming"] } : f))} disabled={!form.appsAfter.trim()}>
                  <SelectTrigger className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="wait">The request waits</SelectItem>
                    <SelectItem value="page">A page says it is waking</SelectItem>
                  </SelectContent>
                </Select>
              </Field>
              <Field label="Databases sleep after" hint="5m to 24h; empty: never">
                <Input value={form.databasesAfter} onChange={set("databasesAfter")} placeholder="never" />
              </Field>
            </div>
            <div>
              <Button type="submit" disabled={save.isPending}>
                Save
              </Button>
            </div>
          </form>
        )}
      </CardContent>
    </Card>
  );
}

const day = (iso: string) => iso.slice(0, 10);

// What the license state says, in words, and the badge beside the title.
export function licenseSummary(s: LicenseStatus): { badge: "success" | "neutral" | "error"; label: string; text: string } {
  if (s.unlocked) return { badge: "success", label: "on", text: "This build runs the platform itself: the enterprise features are on without a license." };
  if (s.error) return { badge: "error", label: "refused", text: `The license installed is refused: ${s.error}.` };
  if (!s.license) return { badge: "neutral", label: "none", text: "No license: the enterprise features are off." };
  if (s.active) return { badge: "success", label: "in force", text: `In force until ${day(s.license.expiresAt)}.` };
  return { badge: "error", label: "expired", text: `Expired on ${day(s.license.expiresAt)}: the enterprise features are off.` };
}

// renewalText says how the license renews and how the last try went.
export function renewalText(s: LicenseStatus): string {
  if (!s.license || s.error) return "";
  const how = s.license.issuer ? `Renews online at ${s.license.issuer}, a week before it expires.` : "Issued by hand: it renews offline, with a new license.";
  const r = s.renewal;
  if (!r) return how;
  return r.error ? `${how} The last renewal, ${when(r.at)}, failed: ${r.error}` : `${how} Last renewed ${when(r.at)}.`;
}

function when(at: string): string {
  return new Date(at).toLocaleString(undefined, { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });
}

function LicenseCard() {
  const queries = useQueryClient();
  const status = useQuery({ queryKey: ["license"], queryFn: api.license });
  const renew = useMutation({
    mutationFn: api.renewLicense,
    onSuccess: (r) => {
      toast.success(`Renewed: in force until ${day(r.license!.expiresAt)}`);
      queries.setQueryData(["license"], r);
    },
    onError: (e: Error) => {
      toast.error(e.message);
      queries.invalidateQueries({ queryKey: ["license"] });
    },
  });
  // The tab opens on the click, so no blocker stops it; the link of one
  // use goes into it once the billing app gave it.
  const billing = useMutation({
    mutationFn: async (tab: Window | null) => ({ tab, link: await api.billingLink() }),
    onSuccess: ({ tab, link }) => {
      if (tab) tab.location.href = link.url;
      else window.location.href = link.url;
    },
    onError: (e: Error) => toast.error(e.message),
  });
  const issuer = status.data?.license?.issuer;
  return (
    <Card>
      <CardHeader>
        <CardTitle>License</CardTitle>
        <CardDescription>
          The enterprise features are on while a license is in force, with no limits, until the day it expires. Install one with <InlineCode>shpyrd-ctl license set &lt;file&gt;</InlineCode>; the server reads it within a minute.
        </CardDescription>
        {status.data && (
          <CardAction className="flex items-center gap-2">
            {issuer && (
              <>
                <Button size="sm" variant="outline" disabled={renew.isPending} onClick={() => renew.mutate()}>
                  Renew now
                </Button>
                <Button
                  size="sm"
                  variant="outline"
                  disabled={billing.isPending}
                  onClick={() => {
                    const tab = window.open("", "_blank");
                    billing.mutate(tab, { onError: () => tab?.close() });
                  }}
                >
                  Open billing
                </Button>
              </>
            )}
            <StatusBadge type={licenseSummary(status.data).badge}>{licenseSummary(status.data).label}</StatusBadge>
          </CardAction>
        )}
      </CardHeader>
      <CardContent className="grid gap-4">
        {status.isLoading ? (
          <Loading rows={1} />
        ) : status.error || !status.data ? (
          <Failed what="the license" error={status.error} />
        ) : (
          <>
            <p className="text-sm text-muted-foreground">
              {licenseSummary(status.data).text} {renewalText(status.data)}
            </p>
            {status.data.license && (
              <InfoTable columns={3}>
                <InfoTableItem label="Customer">{status.data.license.customer}</InfoTableItem>
                <InfoTableItem label="Expires">{day(status.data.license.expiresAt)}</InfoTableItem>
                <InfoTableItem label="License" mono>
                  {status.data.license.id}
                </InfoTableItem>
              </InfoTable>
            )}
          </>
        )}
      </CardContent>
    </Card>
  );
}
