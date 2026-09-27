import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Palette } from "lucide-react";

import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

/**
 * The workspace's look (RFC-0033 branding): a logo and an accent colour,
 * shown on the launcher and the login page instead of the platform's.
 */
export function BrandingCard() {
  const qc = useQueryClient();
  const ws = useQuery({ queryKey: ["workspace"], queryFn: api.workspace });
  const [color, setColor] = useState("");
  const [logo, setLogo] = useState<string | null>(null); // a new data URL, "" to remove, null untouched
  const [preview, setPreview] = useState<string | undefined>(undefined);
  useEffect(() => {
    setColor(ws.data?.branding?.color ?? "");
    setPreview(ws.data?.branding?.logoUrl);
    setLogo(null);
  }, [ws.data?.branding?.color, ws.data?.branding?.logoUrl]);
  const save = useMutation({
    mutationFn: () =>
      api.updateWorkspace({
        ...(logo !== null ? { logo } : {}),
        color: color.trim(),
      }),
    onSuccess: () => {
      toast.success("Look saved; the launcher and the login page use it now");
      qc.invalidateQueries({ queryKey: ["workspace"] });
      qc.invalidateQueries({ queryKey: ["config"] });
    },
    onError: (e: Error) => toast.error(e.message),
  });
  const pick = (file: File | undefined) => {
    if (!file) return;
    if (file.size > 256 * 1024) {
      toast.error("The logo must be at most 256 KB");
      return;
    }
    const reader = new FileReader();
    reader.onload = () => {
      const url = String(reader.result);
      setLogo(url);
      setPreview(url);
    };
    reader.readAsDataURL(file);
  };
  const dirty =
    logo !== null || color.trim() !== (ws.data?.branding?.color ?? "");
  const validColor =
    color.trim() === "" || /^#[0-9a-fA-F]{6}$/.test(color.trim());

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Palette className="size-4" /> Look
        </CardTitle>
        <CardDescription>
          Your logo and colour on the launcher and the login page, where your
          people arrive. PNG, SVG, JPEG or WebP, at most 256 KB; the colour is
          the accent (buttons, links).
        </CardDescription>
      </CardHeader>
      <CardContent className="grid gap-4">
        <div className="flex flex-wrap items-center gap-4">
          <div className="flex h-16 w-40 items-center justify-center rounded-md border bg-muted/30 p-2">
            {preview ? (
              <img
                src={preview}
                alt="logo"
                className="max-h-12 max-w-36 object-contain"
              />
            ) : (
              <span className="text-xs text-muted-foreground">no logo</span>
            )}
          </div>
          <div className="grid gap-2">
            <Label htmlFor="brand-logo">Logo</Label>
            <div className="flex items-center gap-2">
              <Input
                id="brand-logo"
                type="file"
                accept="image/png,image/jpeg,image/svg+xml,image/webp,image/gif"
                className="w-72"
                onChange={(e) => pick(e.target.files?.[0])}
              />
              {preview && (
                <Button
                  size="sm"
                  variant="ghost"
                  onClick={() => {
                    setLogo("");
                    setPreview(undefined);
                  }}
                >
                  Remove
                </Button>
              )}
            </div>
          </div>
        </div>
        <div className="grid gap-2 sm:max-w-xs">
          <Label htmlFor="brand-color">Accent colour</Label>
          <div className="flex items-center gap-2">
            <input
              type="color"
              aria-label="Pick a colour"
              value={validColor && color.trim() ? color.trim() : "#ff4f00"}
              onChange={(e) => setColor(e.target.value)}
              className="size-9 cursor-pointer rounded border bg-transparent"
            />
            <Input
              id="brand-color"
              placeholder="#ff4f00 (the platform's)"
              value={color}
              onChange={(e) => setColor(e.target.value)}
              className="font-mono text-xs"
              aria-invalid={!validColor}
            />
            {color && (
              <Button size="sm" variant="ghost" onClick={() => setColor("")}>
                Reset
              </Button>
            )}
          </div>
        </div>
        <div>
          <Button
            size="sm"
            disabled={!dirty || !validColor || save.isPending}
            onClick={() => save.mutate()}
          >
            Save
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}
