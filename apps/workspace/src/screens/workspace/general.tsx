"use client";

import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Pencil } from "lucide-react";
import { toast } from "sonner";
import { Alert, AlertDescription, AlertTitle } from "@shpyrd/ui/components/alert";
import { Button } from "@shpyrd/ui/components/button";
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from "@shpyrd/ui/components/dialog";
import { ColorPicker, isHex } from "@shpyrd/ui/components/color-picker";
import { Field } from "@shpyrd/ui/components/field";
import { FileDrop } from "@shpyrd/ui/components/file-drop";
import { Input } from "@shpyrd/ui/components/input";
import { Link } from "react-router-dom";
import { api } from "@/api/api";
import { usePerms } from "@/lib/perms";

const presets = ["#ff4f00", "#e11d48", "#d97706", "#16a34a", "#0284c7", "#7c3aed", "#0f172a", "#64748b", "#0d9488", "#db2777", "#4f46e5", "#171717"];
const logoTypes = "image/png,image/jpeg,image/svg+xml,image/webp,image/gif";

// How the workspace looks where its people arrive: its name, its logo
// and its colour. A pencil beside the name opens it, as on a project.
export function NameAndLook() {
  const queries = useQueryClient();
  const ws = useQuery({ queryKey: ["workspace"], queryFn: api.workspace });
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [color, setColor] = useState("");
  // A new logo as a data URL, "" to remove the one there is, null untouched.
  const [logo, setLogo] = useState<string | null>(null);
  const [preview, setPreview] = useState<string>();
  useEffect(() => {
    if (!ws.data || !open) return;
    setName(ws.data.name);
    setColor(ws.data.branding?.color ?? "");
    setPreview(ws.data.branding?.logoUrl);
    setLogo(null);
    // When it opens, or when the workspace arrives after it did: what is
    // being typed is not reset by a refresh.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, !ws.data]);
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
      setOpen(false);
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
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button variant="ghost" size="icon-xs" icon={<Pencil />} aria-label="Name and look" className="text-muted-foreground" />
      </DialogTrigger>
      <DialogContent className="max-h-[calc(100svh-2rem)] overflow-y-auto">
        <DialogHeader divider>
          <DialogTitle>Name and look</DialogTitle>
          <DialogDescription>The name, logo and colour people see on the launcher and the sign-in page.</DialogDescription>
        </DialogHeader>
        <form
          className="grid gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            if (dirty && valid) save.mutate();
          }}
        >
          <Field label="Name">
            <Input value={name} onChange={(e) => setName(e.target.value)} autoFocus />
          </Field>
          <Field label="Logo" hint="PNG, SVG, JPEG, WebP or GIF, at most 256 KB. Without one, the platform's.">
            <FileDrop
              accept={logoTypes}
              maxSize={256 * 1024}
              preview={preview}
              onChange={pick}
              onRemove={() => {
                setLogo("");
                setPreview(undefined);
              }}
            />
          </Field>
          <Field label="Accent colour" hint="Empty is the platform's.">
            <ColorPicker value={color} onChange={setColor} presets={presets} />
          </Field>
          <DialogFooter>
            <DialogClose asChild>
              <Button type="button" variant="outline">
                Cancel
              </Button>
            </DialogClose>
            <Button type="submit" disabled={!dirty || !valid || save.isPending}>
              Save
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

// Until the first team exists, everyone is an administrator: said once,
// over the workspace.
export function RolesNotEnforced() {
  const perms = usePerms();
  return (
    <>
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
    </>
  );
}
