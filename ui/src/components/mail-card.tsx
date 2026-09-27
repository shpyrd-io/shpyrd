import { useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { Loader2, Send } from "lucide-react";

import { api } from "@/lib/api";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";

/**
 * Email delivery (RFC-0013): the sender the platform uses for invitations,
 * as `shpyrd-ctl mail set` configured it, and a test message sent from the
 * server so the settings, the network path and the sender address are all
 * proven at once.
 */
export function MailCard() {
  const status = useQuery({
    queryKey: ["mail"],
    queryFn: api.mailStatus,
    retry: false,
  });
  const [to, setTo] = useState("");
  const test = useMutation({
    mutationFn: () => api.mailTest(to.trim()),
    onSuccess: (r) =>
      toast.success(`Delivered a test message to ${r.to} (${r.took})`),
    onError: (e: Error) => toast.error(e.message),
  });
  const s = status.data;

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          Email
          {s && (
            <Badge variant={s.configured ? "secondary" : "outline"}>
              {s.configured ? "configured" : "not configured"}
            </Badge>
          )}
        </CardTitle>
        <CardDescription>
          How the platform sends email: invitation links go out through this
          sender. Set it with{" "}
          <code className="text-xs">shpyrd-ctl mail set --host … --from …</code>
          ; the password stays in the cluster.
        </CardDescription>
      </CardHeader>
      <CardContent className="grid gap-4">
        {status.isLoading && <Skeleton className="h-12 w-full" />}
        {status.error && (
          <p className="text-sm text-destructive">
            {(status.error as Error).message}
          </p>
        )}
        {s && !s.configured && (
          <p className="text-sm text-muted-foreground">
            No sender yet: invitations show their link to whoever invites, to
            pass along.
          </p>
        )}
        {s && s.configured && (
          <dl className="grid gap-x-6 gap-y-3 text-sm sm:grid-cols-3">
            <div>
              <dt className="text-xs text-muted-foreground">Server</dt>
              <dd className="font-mono text-xs">
                {s.host}:{s.port}{" "}
                <span className="text-muted-foreground">({s.security})</span>
              </dd>
            </div>
            <div>
              <dt className="text-xs text-muted-foreground">From</dt>
              <dd className="font-mono text-xs">{s.from}</dd>
            </div>
            <div>
              <dt className="text-xs text-muted-foreground">Authentication</dt>
              <dd>{s.auth ? "user and password" : "none"}</dd>
            </div>
          </dl>
        )}
        {s && s.configured && (
          <form
            className="flex flex-wrap items-center gap-2"
            onSubmit={(e) => {
              e.preventDefault();
              if (to.trim()) test.mutate();
            }}
          >
            <Input
              type="email"
              placeholder="you@example.com"
              className="w-64"
              value={to}
              onChange={(e) => setTo(e.target.value)}
              required
            />
            <Button
              type="submit"
              size="sm"
              variant="outline"
              disabled={test.isPending || !to.trim()}
            >
              {test.isPending ? (
                <Loader2 data-icon="inline-start" className="animate-spin" />
              ) : (
                <Send data-icon="inline-start" />
              )}
              Send a test message
            </Button>
          </form>
        )}
      </CardContent>
    </Card>
  );
}
