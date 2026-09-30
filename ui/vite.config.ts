import path from "node:path";
import { defineConfig } from "vitest/config";
import type { ProxyOptions } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

// Development mode (`npm run dev`): Vite serves the applications with hot
// reload and proxies the API to a shpyrd server, so the UI is developed
// without rebuilding or uploading anything.
//
//   SHPYRD_DEV_API=https://shpyrd.shpyrd.test npm run dev
//
// points it at the server of a cluster (a local kind one, or any door of a
// platform you can sign in to). The server sees the target's host, so the
// door — console or workspace — follows the URL given; sign in with email
// and password or the admin token (sign-in through an external provider
// redirects back to the real host, not to Vite). Without the variable the
// proxy targets a `go run ./cmd/shpyrd-server` on localhost:8080.
const api = process.env.SHPYRD_DEV_API ?? "http://localhost:8080";
const proxied: ProxyOptions = {
  target: api,
  changeOrigin: true,
  // Local clusters present certificates from a development CA.
  secure: false,
  ws: true,
  // Cookies come back for the host the browser sees (localhost), so
  // sessions and the CSRF cookie work through the proxy.
  cookieDomainRewrite: "",
  cookiePathRewrite: "/",
  configure(proxy) {
    // The platform marks its cookies Secure (it is HTTPS). Vite serves
    // plain http://localhost, where Safari drops them; Chrome and Firefox
    // treat localhost as secure and would keep them. Strip the flag.
    proxy.on("proxyRes", (res) => {
      const raw = res.headers["set-cookie"];
      if (Array.isArray(raw)) {
        res.headers["set-cookie"] = raw.map((c) =>
          c.replace(/;\s*Secure/gi, ""),
        );
      }
    });
  },
};

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": path.resolve(import.meta.dirname, "./src"),
    },
  },
  server: {
    proxy: {
      "/api": proxied,
      // Server-rendered pages: password reset and invitation activation.
      "/account": proxied,
    },
  },
  build: {
    // Embedded into the server binary by ui/embed.go. Two applications
    // (RFC-0080): the console and the workspace one, each its own entry;
    // the server serves dist/apps/console/index.html at the console host
    // and dist/apps/workspace/index.html everywhere else. Chunks are
    // shared under dist/assets.
    outDir: "dist",
    emptyOutDir: true,
    rollupOptions: {
      input: {
        console: path.resolve(import.meta.dirname, "apps/console/index.html"),
        workspace: path.resolve(
          import.meta.dirname,
          "apps/workspace/index.html",
        ),
      },
    },
  },
  test: {
    // Pure-logic unit tests (parsers, formatters); no DOM needed.
    environment: "node",
    include: ["src/**/*.test.ts", "src/**/*.test.tsx"],
  },
});
