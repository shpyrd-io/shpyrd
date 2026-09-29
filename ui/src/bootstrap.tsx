import { StrictMode, type ComponentType } from "react";
import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { Toaster } from "@/components/ui/sonner";
import { consumeTokenFragment } from "@/lib/auth";
import "./index.css";

/**
 * mount renders one of the two applications (RFC-0080): the console at
 * the console host, the workspace application everywhere else. Both share
 * this bootstrap, the client, the components and the theme.
 */
export function mount(App: ComponentType) {
  consumeTokenFragment();
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: 1, refetchOnWindowFocus: false },
    },
  });
  createRoot(document.getElementById("root")!).render(
    <StrictMode>
      <QueryClientProvider client={queryClient}>
        <App />
        <Toaster richColors position="bottom-right" />
      </QueryClientProvider>
    </StrictMode>,
  );
}

/**
 * basename is the router's base path: "/" when the server serves the
 * application at the host's root (every install), the application's own
 * directory under the Vite dev server (`npm run dev` opens
 * /apps/console/ and /apps/workspace/).
 */
export function basename(app: "console" | "workspace"): string {
  const dev = `/apps/${app}`;
  return window.location.pathname.startsWith(dev + "/") ||
    window.location.pathname === dev
    ? dev
    : "/";
}
