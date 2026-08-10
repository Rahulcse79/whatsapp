import { defineConfig } from "vitest/config";

// Node unit tests for the whole client core. Workspace @wa/* packages ship
// TypeScript from src, so inline them rather than externalizing.
export default defineConfig({
  test: {
    include: ["src/**/*.test.ts"],
    environment: "node",
    server: { deps: { inline: [/^@wa\//] } },
  },
});
