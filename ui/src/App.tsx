import { BrowserRouter, Navigate, Route, Routes } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { useToken } from "@/lib/auth";
import { Layout } from "@/components/layout";
import { LoginPage } from "@/pages/login";
import { AppsPage } from "@/pages/apps";
import { AppDetailPage } from "@/pages/app-detail";
import { ClusterPage } from "@/pages/cluster";
import { WorkspacePage } from "@/pages/workspace";
import { InvitePage } from "@/pages/invite";

// inviteToken is the token of an invitation link (/invite/<token>), or
// "". The page is reachable signed out, so it is handled before the
// sign-in gate (RFC-0033).
function inviteToken(): string {
  const m = /^\/invite\/([^/?#]+)/.exec(window.location.pathname);
  return m ? decodeURIComponent(m[1]) : "";
}

export default function App() {
  const token = useToken();
  const config = useQuery({
    queryKey: ["config"],
    queryFn: api.config,
    staleTime: 60_000,
  });
  // Without a token, a session cookie may still sign us in (RFC-0007).
  const me = useQuery({
    queryKey: ["me", "cookie"],
    queryFn: api.me,
    enabled: !!config.data?.authRequired && !token,
    retry: false,
    staleTime: 60_000,
  });

  // Until we know whether the server wants a token, render nothing to
  // avoid flashing the login screen.
  if (config.isLoading) return null;
  const invite = inviteToken();
  if (config.data?.authRequired && !token) {
    if (me.isLoading) return null;
    if (invite) return <InvitePage token={invite} me={me.data ?? undefined} />;
    if (!me.data) return <LoginPage />;
  }
  if (invite) return <InvitePage token={invite} me={me.data ?? undefined} />;

  return (
    <BrowserRouter>
      <Routes>
        <Route element={<Layout />}>
          <Route path="/" element={<AppsPage />} />
          <Route path="/projects/:slug" element={<AppDetailPage />} />
          <Route path="/cluster" element={<ClusterPage />} />
          <Route path="/workspace" element={<WorkspacePage />} />
          <Route path="/workspace/:tab" element={<WorkspacePage />} />
          <Route
            path="/users"
            element={<Navigate to="/workspace/users" replace />}
          />
          <Route
            path="/teams"
            element={<Navigate to="/workspace/teams" replace />}
          />
          <Route path="*" element={<AppsPage />} />
        </Route>
      </Routes>
    </BrowserRouter>
  );
}
