"use client";

import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Alert, AlertDescription, AlertTitle } from "@shpyrd/ui/components/alert";
import { Button } from "@shpyrd/ui/components/button";
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@shpyrd/ui/components/card";
import { ColorPicker, isHex } from "@shpyrd/ui/components/color-picker";
import { Field } from "@shpyrd/ui/components/field";
import { FileDrop } from "@shpyrd/ui/components/file-drop";
import { InlineCode } from "@shpyrd/ui/components/inline-code";
import { Input } from "@shpyrd/ui/components/input";
import { PageHeading } from "@shpyrd/ui/components/page-heading";
import { ProgressBar } from "@shpyrd/ui/components/progress-bar";
import { Skeleton } from "@shpyrd/ui/components/skeleton";
import { Stack } from "@shpyrd/ui/components/stack";
import { Link } from "react-router-dom";
import { StatusBadge } from "@shpyrd/ui/components/status-badge";
import { api } from "@/api/api";
import { usePerms } from "@/lib/perms";

const presets = ["#ff4f00", "#e11d48", "#d97706", "#16a34a", "#0284c7", "#7c3aed", "#0f172a", "#64748b", "#0d9488", "#db2777", "#4f46e5", "#171717"];
const logoTypes = "image/png,image/jpeg,image/svg+xml,image/webp,image/gif";

// What the workspace is: its name, where it answers, how it looks where
// its people arrive, and how much of its plan is taken.
export function General() {
  const perms = usePerms();
  const queries = useQueryClient();
  const ws = useQuery({ queryKey: ["workspace"], queryFn: api.workspace });
  const [name, setName] = useState("");
  const [color, setColor] = useState("");
  // A new logo as a data URL, "" to remove the one there is, null untouched.
  const [logo, setLogo] = useState<string | null>(null);
  const [preview, setPreview] = useState<string>();
  useEffect(() => {
    if (!ws.data) return;
    setName(ws.data.name);
    setColor(ws.data.branding?.color ?? "");
    setPreview(ws.data.branding?.logoUrl);
    setLogo(null);
  }, [ws.data]);
  const changed = {
    name: ws.data ? name.trim() !== ws.data.name && name.trim() !== "" : false,
    color: ws.data ? color !== (ws.data.branding?.color ?? "") : false,
    logo: logo !== null,
  };
  const dirty = changed.name || changed.color || changed.logo;
  const valid = color === "" || isHex(color);
  const save = useMutation({
    mutationFn: () => api.updateWorkspace({ ...(changed.name ? { name: name.trim() } : {}), ...(changed.color ? { color } : {}), ...(changed.logo ? { logo: logo! } : {}) }),
    onSuccess: () => {
      queries.invalidateQueries({ queryKey: ["workspace"] });
      queries.invalidateQueries({ queryKey: ["config"] });
      toast.success("Saved", { description: "The launcher and the sign-in page show it now." });
    },
    onError: (e: Error) => toast.error(e.message),
  });
  const pick = (file: File) => {
    const reader = new FileReader();
    reader.onload = () => {
      setLogo(String(reader.result));
      setPreview(String(reader.result));
    };
    reader.readAsDataURL(file);
  };

  if (ws.isLoading) return <Skeleton className="h-64 w-full" />;
  if (ws.error || !ws.data)
    return (
      <Alert variant="destructive">
        <AlertTitle>Could not load the workspace</AlertTitle>
        <AlertDescription>{(ws.error as Error)?.message}</AlertDescription>
      </Alert>
    );
  const w = ws.data;
  const plan = [
    { label: "Projects", used: w.usage?.projects ?? 0, of: w.limits?.projects, unit: "" },
    { label: "Instances", used: w.usage?.instances ?? 0, of: w.limits?.instances, unit: "" },
    { label: "CPU", used: Number(w.usage?.cpu ?? 0), of: Number(w.limits?.cpu), unit: " cores" },
    { label: "Memory", used: gib(w.usage?.memory) ?? 0, of: gib(w.limits?.memory), unit: " GiB" },
    { label: "Storage", used: gib(w.usage?.storage) ?? 0, of: gib(w.limits?.storage), unit: " GiB" },
  ].filter((row) => row.of && row.of > 0);

  return (
    <>
      <PageHeading
        title={w.name}
        description="Every project, team and person here belongs to this workspace."
        iconEnd={
          <StatusBadge type={w.status === "suspended" ? "warning" : "success"}>{w.status === "suspended" ? "Suspended" : "Active"}</StatusBadge>
        }
      />
      {perms.loaded && !perms.enforced && perms.me?.provider !== "token" && (
        <Alert variant="warning">
          <AlertTitle>Roles are not enforced yet</AlertTitle>
          <AlertDescription>
            Every signed-in person is an administrator until the first team exists. Make one under{" "}
            <Link to="/workspace/teams" className="font-medium text-foreground underline-offset-4 hover:underline">
              Teams
            </Link>
            .
          </AlertDescription>
        </Alert>
      )}
      <Card>
        <CardHeader>
          <CardTitle>Name and look</CardTitle>
          <CardDescription>
            What people see where they arrive: the name and the logo on the launcher and the sign-in page, the colour of the buttons and the marks.
            {w.address && (
              <>
                {" "}
                It answers at <InlineCode>{w.address}</InlineCode>; owners change that under{" "}
                <Link to="/workspace/domains" className="font-medium text-foreground underline-offset-4 hover:underline">
                  Domains
                </Link>
                .
              </>
            )}
          </CardDescription>
        </CardHeader>
        <CardContent>
          <div className="grid gap-x-8 gap-y-4 @3xl/page-layout:grid-cols-2">
            <Stack gap="normal">
              <Field label="Name">
                <Input value={name} onChange={(e) => setName(e.target.value)} disabled={!perms.admin} />
              </Field>
              <Field label="Accent colour" hint="Empty is the platform's.">
                <ColorPicker value={color} onChange={setColor} presets={presets} disabled={!perms.admin} />
              </Field>
            </Stack>
            <Field label="Logo" hint="PNG, SVG, JPEG, WebP or GIF, at most 256 KB. Without one, the platform's.">
              <FileDrop
                accept={logoTypes}
                maxSize={256 * 1024}
                preview={preview}
                disabled={!perms.admin}
                onChange={pick}
                onRemove={() => {
                  setLogo("");
                  setPreview(undefined);
                }}
              />
            </Field>
            {perms.admin && (
              <div className="col-span-full">
                <Button size="sm" disabled={!dirty || !valid || save.isPending} onClick={() => save.mutate()}>
                  Save
                </Button>
              </div>
            )}
          </div>
        </CardContent>
      </Card>
      {plan.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle>Plan</CardTitle>
            <CardDescription>What this workspace may use across all its projects. A change that would go over is refused with the number.</CardDescription>
            <CardAction>
              <InlineCode>{w.owners.length} {w.owners.length === 1 ? "owner" : "owners"}</InlineCode>
            </CardAction>
          </CardHeader>
          <CardContent className="grid gap-x-8 gap-y-4 @xl/page-layout:grid-cols-2 @5xl/page-layout:grid-cols-5">
            {plan.map((row) => (
              <ProgressBar
                key={row.label}
                label={row.label}
                value={row.used}
                max={row.of}
                size="sm"
                tone={row.used / (row.of ?? 1) >= 0.9 ? "error" : row.used / (row.of ?? 1) >= 0.75 ? "warning" : "orange"}
                format={(v) => `${v}${row.unit}`}
              />
            ))}
          </CardContent>
        </Card>
      )}
    </>
  );
}

// "8.8Gi" as a number of GiB; a number as it is.
function gib(value?: string | number): number | undefined {
  if (value === undefined) return undefined;
  const text = String(value);
  const n = parseFloat(text);
  if (Number.isNaN(n)) return undefined;
  if (text.endsWith("Mi")) return n / 1024;
  if (text.endsWith("Ti")) return n * 1024;
  return n;
}
