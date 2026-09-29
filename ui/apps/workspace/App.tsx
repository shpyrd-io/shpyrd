import { BrowserRouter, Navigate, Route, Routes } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { useToken } from "@/lib/auth";
import { usePerms } from "@/lib/me";
import { basename } from "@/bootstrap";
import { Layout } from "@/components/layout";
import { LoginPage } from "@/pages/login";
import { AppsPage, useUserOnly } from "@/pages/apps";
import { LauncherPage } from "@/pages/launcher";
import { AppDetailPage } from "@/pages/app-detail";
import { WorkspacePage } from "@/pages/workspace";
import { InvitePage } from "@/pages/invite";

// The workspace application (RFC-0080): what answers at a workspace's
// address and hosts. Projects, deploys, the workspace's people, teams,
// tokens and its own sign-in methods. Nothing of the cluster: that is the
// console, another host and another application.

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
    <BrowserRouter basename={basename("workspace")}>
      <Routes>
        <Route element={<Shell />}>
          <Route path="/" element={<LauncherPage />} />
          <Route path="/projects" element={<AppsPage />} />
          <Route path="/projects/:slug" element={<AppDetailPage />} />
          <Route path="/workspace" element={<WorkspacePage />} />
          <Route path="/workspace/:tab" element={<WorkspacePage />} />
          <Route
            path="/teams"
            element={<Navigate to="/workspace/teams" replace />}
          />
          <Route path="*" element={<LauncherPage />} />
        </Route>
      </Routes>
    </BrowserRouter>
  );
}

// Shell is the workspace application's navigation. An operator workspace
// shows a platform admin the way to the console — the one place the two
// doors acknowledge each other (RFC-0080).
function Shell() {
  const config = useQuery({
    queryKey: ["config"],
    queryFn: api.config,
    staleTime: 60_000,
  });
  const perms = usePerms();
  const userOnly = useUserOnly(perms);
  const consoleUrl =
    config.data?.workspace?.ownedByOperator && perms.me?.console
      ? config.data.consoleUrl
      : undefined;
  return (
    <Layout
      nav={[
        { to: "/", label: "Apps" },
        { to: "/projects", label: "Projects", show: !userOnly },
        { to: "/workspace", label: "Workspace", show: perms.clusterAdmin },
      ]}
      consoleUrl={consoleUrl}
    />
  );
}
