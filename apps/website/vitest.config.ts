import { fileURLToPath } from "node:url";
import { defineConfig } from "vitest/config";

// `@/` is src/, as in tsconfig.json. Components are tested in a browser
// that is made up (jsdom), with the file's `// @vitest-environment jsdom`.
export default defineConfig({
  resolve: { alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) } },
  test: { setupFiles: ["./src/test-setup.ts"] },
});
