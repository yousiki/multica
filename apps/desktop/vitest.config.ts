import { resolve } from "path";
import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";

// `@` is set up as an alias to src/renderer/src in electron.vite.config.ts
// so the renderer code can use clean imports. Vitest builds its own module
// graph, so it needs the same alias re-declared here — otherwise tests that
// touch any renderer file (e.g. navigation.tsx → "@/stores/tab-store")
// fail to resolve at transform time.
export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      "@": resolve(__dirname, "src/renderer/src"),
    },
  },
  test: {
    globals: true,
    include: ["src/**/*.test.{ts,tsx}", "scripts/**/*.test.mjs"],
    environment: "jsdom",
    setupFiles: ["./test/setup.ts"],
    passWithNoTests: true,
  },
});
