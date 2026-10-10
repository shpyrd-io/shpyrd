import { defineConfig } from "vitest/config";

// The components are tested in a browser that is made up (jsdom): what
// they render and what they do when used, not how they look.
export default defineConfig({
  test: {
    environment: "jsdom",
    setupFiles: ["./src/test.ts"],
    include: ["src/**/*.test.{ts,tsx}", "scripts/**/*.test.mjs"],
  },
});
