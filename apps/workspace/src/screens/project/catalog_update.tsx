"use client";

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Alert, AlertDescription } from "@shpyrd/ui/components/alert";
import { Button } from "@shpyrd/ui/components/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@shpyrd/ui/components/card";
import { Field } from "@shpyrd/ui/components/field";
import { Input } from "@shpyrd/ui/components/input";
import { Stack } from "@shpyrd/ui/components/stack";
import { api } from "@/api/api";
import type { CatalogUpdateInput, CatalogVersion, Project } from "@/api/types";
import type { Perms } from "@/lib/perms";
import { Failed, Loading } from "./shared";

// A version of the entry as a person reads it: "v1.4.0 (3f2a9c1b8d7e)".
export function versionText(v?: CatalogVersion): string {
  if (!v) return "unknown";
  return v.revision ? `${v.revision} (${v.shortCommit})` : v.shortCommit;
}

// What a project from the catalog says of its entry: the version it runs,
// and, when the catalog publishes another, what applying it does, with a
// field for each input it asks. Nothing it generates is shown.
export function CatalogUpdate({ project, perms }: { project: Project; perms: Perms }) {
  const catalog = project.catalog;
  if (!catalog) return null;
  const state = catalog.state;
  return (
    <Card>
      <CardHeader>
        <CardTitle>Catalog</CardTitle>
        <CardDescription>
          Made from the catalog entry {catalog.entry}. It runs {versionText(catalog.running)}
          {state === "up-to-date" && ", the version the catalog offers"}.
        </CardDescription>
      </CardHeader>
      <CardContent>
        {state === "updating" && (
          <p className="text-sm text-muted-foreground">Updating to {versionText(catalog.offered)}: the new version is rolling out.</p>
        )}
        {state === "update-available" && <Offer project={project} perms={perms} />}
        {!state && <p className="text-sm text-muted-foreground">The catalog offers no other version.</p>}
      </CardContent>
    </Card>
  );
}

function Offer({ project, perms }: { project: Project; perms: Perms }) {
  const queries = useQueryClient();
  const preview = useQuery({ queryKey: ["catalog-update", project.slug], queryFn: () => api.catalogUpdate(project.slug) });
  const [values, setValues] = useState<Record<string, string>>({});
  const apply = useMutation({
    mutationFn: () => api.applyCatalogUpdate(project.slug, { digest: preview.data?.to?.digest ?? "", inputs: given(values) }),
    onSuccess: (result) => {
      queries.invalidateQueries({ queryKey: ["project", project.slug] });
      queries.invalidateQueries({ queryKey: ["projects"] });
      queries.invalidateQueries({ queryKey: ["catalog-update", project.slug] });
      if (result.changed) toast.success(`Updating to ${versionText(result.to)}`, { description: result.notes?.join(" ") });
      else toast.info(result.reason ?? "Nothing was changed");
    },
    onError: (e: Error) => toast.error(e.message),
  });
  if (preview.isLoading) return <Loading rows={2} />;
  if (preview.isError || !preview.data) return <Failed what="the update" error={preview.error} />;
  const p = preview.data;
  const asked = p.inputs.filter((i) => i.action === "ask");
  const makes = p.volumes.length > 0 || p.resources.length > 0;
  const missing = asked.some((i) => i.required && !values[i.name]?.trim());
  const allowed = perms.deploy && perms.config && (!makes || perms.resource);
  return (
    <Stack gap="normal">
      <Alert>
        <AlertDescription>
          The catalog offers {versionText(p.to)}. Applying it moves the code to that commit and deploys it. It keeps every config var, variable, replica count and size the project has, and removes nothing.
        </AlertDescription>
      </Alert>
      <ul className="grid gap-1 text-sm">
        {p.buildChanged && <li>Changes the build settings.</li>}
        {p.overlayChanged && <li>Changes the files the entry lays over the code.</li>}
        {p.volumes.length > 0 && <li>Makes volumes: {p.volumes.join(", ")}.</li>}
        {p.resources.length > 0 && <li>Makes resources: {p.resources.join(", ")}.</li>}
        {p.processesAdded.length > 0 && <li>Adds processes: {p.processesAdded.join(", ")}.</li>}
        {p.processesChanged.length > 0 && <li>Changes processes: {p.processesChanged.join(", ")}.</li>}
        {p.envAdded.length > 0 && <li>Adds variables: {p.envAdded.join(", ")}.</li>}
        {p.inputs.filter((i) => i.action === "generate").map((i) => (
          <li key={i.name}>Generates {i.name}.</li>
        ))}
        {p.inputs.filter((i) => i.action === "restore").map((i) => (
          <li key={i.name}>Takes back {i.name}, the value the project had.</li>
        ))}
        {p.inputs.filter((i) => i.action === "default").map((i) => (
          <li key={i.name}>Sets {i.name}{i.default ? ` to ${i.default}` : " to its default"}.</li>
        ))}
      </ul>
      {p.envKept.length > 0 && (
        <p className="text-sm text-muted-foreground">Kept at the project&apos;s value, not the new version&apos;s: {p.envKept.join(", ")}.</p>
      )}
      {allowed ? (
        <>
          {asked.map((i) => (
            <InputField key={i.name} input={i} value={values[i.name] ?? ""} disabled={apply.isPending} onChange={(v) => setValues((cur) => ({ ...cur, [i.name]: v }))} />
          ))}
          <div>
            <Button disabled={missing || apply.isPending} onClick={() => apply.mutate()}>
              Update to {versionText(p.to)}
            </Button>
          </div>
        </>
      ) : (
        <p className="text-sm text-muted-foreground">
          {perms.deploy && perms.config
            ? "This update makes volumes or resources, which needs the permission to manage resources."
            : "Applying an update needs the permission to deploy the project and change its config."}
        </p>
      )}
    </Stack>
  );
}

function InputField({ input, value, disabled, onChange }: { input: CatalogUpdateInput; value: string; disabled: boolean; onChange: (v: string) => void }) {
  return (
    <Field label={input.label ?? input.name} required={input.required} hint={input.name}>
      <Input type={input.secret ? "password" : "text"} autoComplete="off" value={value} disabled={disabled} onChange={(e) => onChange(e.target.value)} />
    </Field>
  );
}

// What was typed, without the fields left empty.
function given(values: Record<string, string>): Record<string, string> {
  return Object.fromEntries(Object.entries(values).filter(([, v]) => v.trim() !== ""));
}
