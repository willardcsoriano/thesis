import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    // Only the TypeScript sources; never compiled copies such as .esm-check/.
    include: ["test/**/*.test.ts"],
  },
});
