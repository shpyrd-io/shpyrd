"use client";

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { BrowserRouter, Navigate, Route, Routes } from "react-router-dom";
import { Toaster } from "@shpyrd/ui/components/sonner";
import { TooltipProvider } from "@shpyrd/ui/components/tooltip";
import { Launcher } from "@/screens/launcher";
import { Invite } from "@/screens/invite";
import { Login, useDoor } from "@/screens/login";
import { ProjectPages } from "@/screens/project";
import { WorkspacePages } from "@/screens/workspace";

// The workspace application: what answers at a workspace's address.
// Three places: the launcher, where everyone lands; a project; the
// workspace itself. Nothing of the cluster: that is the console.

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

// The token of an invitation link, /invite/<token>, or "". The page is
// reachable signed out, so it comes before the door.
function inviteToken(): string {
  const m = /^\/invite\/([^/?#]+)/.exec(window.location.pathname);
  return m ? decodeURIComponent(m[1]!) : "";
}

function Door() {
  const door = useDoor();
  if (door.state === "waiting") return null;
  const invite = inviteToken();
  if (invite && door.state !== "failed") return <Invite token={invite} config={door.config} me={door.me} />;
  if (door.state === "failed")
    return (
      <div className="flex min-h-svh items-center justify-center p-6 text-sm text-muted-foreground">
        The server did not answer: {door.error?.message}
      </div>
    );
  if (door.state === "closed") return <Login config={door.config} />;
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/" element={<Launcher />} />
        <Route path="/projects/:slug/*" element={<ProjectPages />} />
        <Route path="/workspace/*" element={<WorkspacePages />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </BrowserRouter>
  );
}
