import { defineConfig } from "vitest/config";

// Only the framework-free core is unit-tested (RN screens need a device/e2e
// harness — test-strategy §4). Workspace @wa/* packages ship TypeScript from
// src, so inline them instead of externalizing (Node can't require their .ts).
export default defineConfig({
  test: {
    include: ["src/**/*.test.ts"],
    environment: "node",
    server: { deps: { inline: [/^@wa\//] } },
  },
});
