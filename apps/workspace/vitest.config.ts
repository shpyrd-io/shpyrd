import { fileURLToPath } from "node:url";
import { defineConfig } from "vitest/config";

// The application is tested in a browser that is made up (jsdom), in the
// Mock: the api answers from the JSON files, and localStorage keeps what
// a test changes until the next resets it.
export default defineConfig({
  test: {
    environment: "jsdom",
    include: ["src/**/*.test.{ts,tsx}"],
    env: { NEXT_PUBLIC_API_MODE: "mock" },
  },
  resolve: {
    alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) },
  },
});
