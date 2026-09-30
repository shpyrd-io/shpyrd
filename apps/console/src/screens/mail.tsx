"use client";

import { useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Send } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@shpyrd/ui/components/button";
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@shpyrd/ui/components/card";
import { InfoTable, InfoTableItem } from "@shpyrd/ui/components/info-table";
import { InlineCode } from "@shpyrd/ui/components/inline-code";
import { Input } from "@shpyrd/ui/components/input";
import { StatusBadge } from "@shpyrd/ui/components/status-badge";
import { api } from "@/api/api";
import { Failed, Loading } from "./shared";

// How the platform sends email: the sender invitations go out through,
// and a test message sent from the server, which proves the settings,
// the network path and the address at once.
export function Mail() {
  const status = useQuery({ queryKey: ["mail"], queryFn: api.mailStatus, retry: false });
  const [to, setTo] = useState("");
  const test = useMutation({
    mutationFn: () => api.mailTest(to.trim()),
    onSuccess: (r) => toast.success(`A test message reached ${r.to}`, { description: `It took ${r.took}.` }),
    onError: (e: Error) => toast.error(e.message),
  });
  if (status.isLoading) return <Loading />;
  if (status.error || !status.data) return <Failed what="the mail settings" error={status.error} />;
  const s = status.data;
  return (
    <Card>
      <CardHeader>
        <CardTitle>Email</CardTitle>
        <CardDescription>
          Invitation links and password resets go out through this sender. Set it with <InlineCode>shpyrd-ctl mail set --host … --from …</InlineCode>; the password stays in the cluster.
        </CardDescription>
        <CardAction>
          <StatusBadge type={s.configured ? "success" : "neutral"}>{s.configured ? "set up" : "not set up"}</StatusBadge>
        </CardAction>
      </CardHeader>
      <CardContent className="grid gap-6">
        {!s.configured ? (
          <p className="text-sm text-muted-foreground">No sender yet: an invitation shows its link to whoever invites, to hand over.</p>
        ) : (
          <>
            <InfoTable columns={3}>
              <InfoTableItem label="Server" mono>
                {s.host}:{s.port} <span className="font-sans text-muted-foreground">({s.security})</span>
              </InfoTableItem>
              <InfoTableItem label="From" mono>
                {s.from}
              </InfoTableItem>
              <InfoTableItem label="Authentication">{s.auth ? "user and password" : "none"}</InfoTableItem>
            </InfoTable>
            <form
              className="flex flex-wrap items-center gap-2"
              onSubmit={(e) => {
                e.preventDefault();
                if (to.trim()) test.mutate();
              }}
            >
              <Input type="email" placeholder="you@example.com" className="w-64" value={to} onChange={(e) => setTo(e.target.value)} aria-label="Send a test message to" />
              <Button type="submit" variant="outline" icon={<Send />} disabled={test.isPending || !to.trim()}>
                Send a test message
              </Button>
            </form>
          </>
        )}
      </CardContent>
    </Card>
  );
}
