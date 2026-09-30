"use client";

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { BrowserRouter } from "react-router-dom";
import { Toaster } from "@shpyrd/ui/components/sonner";
import { TooltipProvider } from "@shpyrd/ui/components/tooltip";
import { Login, useDoor } from "@/screens/login";
import { Pages } from "@/screens";

// The console: the operator's application, at the console host. The
// cluster, the workspaces it hosts, the accounts, the console's own
// sign-in and the platform's knobs. No project renders here: projects
// live in workspaces, another host and another application.

const queries = new QueryClient({
  defaultOptions: { queries: { retry: false, staleTime: 10_000 } },
});

export default function App() {
  return (
    <QueryClientProvider client={queries}>
      <TooltipProvider>
        <Door />
        <Toaster position="bottom-right" />
      </TooltipProvider>
    </QueryClientProvider>
  );
}

function Door() {
  const door = useDoor();
  if (door.state === "waiting") return null;
  if (door.state === "failed")
    return (
      <div className="flex min-h-svh items-center justify-center p-6 text-sm text-muted-foreground">
        The server did not answer: {door.error?.message}
      </div>
    );
  if (door.state === "closed") return <Login config={door.config} />;
  return (
    <BrowserRouter>
      <Pages />
    </BrowserRouter>
  );
}
