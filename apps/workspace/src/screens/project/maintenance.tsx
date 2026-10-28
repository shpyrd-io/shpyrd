"use client";

import { useEffect, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Button } from "@shpyrd/ui/components/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@shpyrd/ui/components/card";
import { Field } from "@shpyrd/ui/components/field";
import { Stack } from "@shpyrd/ui/components/stack";
import { Switch } from "@shpyrd/ui/components/switch";
import { Textarea } from "@shpyrd/ui/components/textarea";
import { api } from "@/api/api";
import type { Project } from "@/api/types";
import type { Perms } from "@/lib/perms";
import { ago } from "@/lib/project";

export const messageMax = 500;

// A page in front of the app (RFC-0020): visitors get a 503 and the
// message, programs a Retry-After, and nobody is let through, the team
// included. The processes keep running, so the team works through the
// console. The switch takes effect at once, with the message as typed.
export function Maintenance({ project, perms }: { project: Project; perms: Perms }) {
  const queries = useQueryClient();
  const current = project.maintenance;
  const on = current?.enabled ?? false;
  const [message, setMessage] = useState(current?.message ?? "");
  useEffect(() => setMessage(current?.message ?? ""), [current?.message]);
  const set = useMutation({
    mutationFn: (body: { enabled: boolean; message?: string }) => api.setMaintenance(project.slug, body),
    onSuccess: (_, body) => {
      queries.invalidateQueries({ queryKey: ["project", project.slug] });
      queries.invalidateQueries({ queryKey: ["projects"] });
      toast.success(body.enabled ? (on ? "The page says it now" : "The maintenance page is up") : "The app answers again");
    },
    onError: (e: Error) => toast.error(e.message),
  });
  const changed = on && message.trim() !== (current?.message ?? "");
  return (
    <Card>
      <CardHeader>
        <CardTitle>Maintenance</CardTitle>
        <CardDescription>
          {on
            ? `Visitors see the page${current?.since ? ` since ${ago(current.since)}` : ""}${current?.by ? `, put up by ${current.by}` : ""}. The processes keep running.`
            : "A page in front of the app while you work on it: visitors get a 503 and your message, programs a Retry-After. Nobody is let through; the processes keep running, and releases, the shell and one-off commands go on."}
        </CardDescription>
      </CardHeader>
      <CardContent>
        <Stack gap="normal">
          <Switch
            aria-label="Maintenance page"
            checked={on}
            loading={set.isPending}
            disabled={!perms.deploy}
            onCheckedChange={(next) => set.mutate({ enabled: next, message: message.trim() })}
          />
          <Field label="Message" hint={`What the page says under the project's name. Plain text, up to ${messageMax} characters.`}>
            <Textarea
              value={message}
              maxLength={messageMax}
              disabled={!perms.deploy || set.isPending}
              placeholder="Back at 15:00 UTC."
              onChange={(e) => setMessage(e.target.value)}
            />
          </Field>
          {changed && (
            <div>
              <Button size="sm" variant="secondary" disabled={set.isPending} onClick={() => set.mutate({ enabled: true, message: message.trim() })}>
                Change what the page says
              </Button>
            </div>
          )}
        </Stack>
      </CardContent>
    </Card>
  );
}
