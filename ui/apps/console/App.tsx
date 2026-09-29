import { BrowserRouter, Navigate, Route, Routes } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { useToken } from "@/lib/auth";
import { usePerms } from "@/lib/me";
import { basename } from "@/bootstrap";
import { Layout } from "@/components/layout";
import { LoginPage } from "@/pages/login";
import { ClusterPage } from "@/pages/cluster";
import { UsersPage } from "@/pages/users";
import { WorkspacesPage } from "./workspaces";
import { ConsoleSignInPage } from "./signin";
import { ConsoleSettingsPage } from "./settings";

// The console (RFC-0080): the operator's application, at the console host
// only. The cluster, the workspaces it hosts, the accounts, the console's
// own sign-in methods and the defaults workspaces get. No project ever
// renders here: projects live in workspaces, another host and another
// application.

export default function App() {
  const token = useToken();
  const config = useQuery({
    queryKey: ["config"],
    queryFn: api.config,
    staleTime: 60_000,
  });
  const me = useQuery({
    queryKey: ["me", "cookie"],
    queryFn: api.me,
    enabled: !!config.data?.authRequired && !token,
    retry: false,
    staleTime: 60_000,
  });
  if (config.isLoading) return null;
  if (config.data?.authRequired && !token) {
    if (me.isLoading) return null;
    if (!me.data) return <LoginPage />;
  }
  return (
    <BrowserRouter basename={basename("console")}>
      <Routes>
        <Route element={<Shell />}>
          <Route path="/" element={<ClusterPage />} />
          <Route path="/cluster" element={<Navigate to="/" replace />} />
          <Route path="/workspaces" element={<WorkspacesPage />} />
          <Route path="/accounts" element={<UsersPage />} />
          <Route path="/signin" element={<ConsoleSignInPage />} />
          <Route path="/settings" element={<ConsoleSettingsPage />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Route>
      </Routes>
    </BrowserRouter>
  );
}

function Shell() {
  const config = useQuery({
    queryKey: ["config"],
    queryFn: api.config,
    staleTime: 60_000,
  });
  const perms = usePerms();
  const accounts =
    !!config.data?.extensions?.includes("auth-local") && perms.clusterAdmin;
  return (
    <Layout
      tag="console"
      grafana
      nav={[
        { to: "/", label: "Cluster", show: perms.clusterView },
        { to: "/workspaces", label: "Workspaces", show: perms.clusterAdmin },
        { to: "/accounts", label: "Accounts", show: accounts },
        { to: "/signin", label: "Sign-in", show: perms.clusterAdmin },
        { to: "/settings", label: "Settings", show: perms.clusterAdmin },
      ]}
    />
  );
}
