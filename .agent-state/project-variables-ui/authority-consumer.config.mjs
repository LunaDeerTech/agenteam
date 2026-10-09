// Pure Vitest runner; no development server or external transport.
import path from "node:path";
import { fileURLToPath } from "node:url";
const directory = path.dirname(fileURLToPath(import.meta.url));
const repository = path.resolve(directory, "../..");
export default {
  root: path.join(repository, "web"),
  cacheDir: path.join(
    repository,
    "output/ai/project-variables-ui/implementation/authority-consumer-vite-cache",
  ),
  resolve: {
    alias: {
      vitest: path.join(repository, "web/node_modules/vitest/dist/index.js"),
      "@vue/test-utils": path.join(
        repository,
        "web/node_modules/@vue/test-utils/dist/vue-test-utils.esm-bundler.mjs",
      ),
    },
  },
  test: {
    environment: "jsdom",
    include: [path.join(directory, "authority-consumer-controls.spec.ts")],
    restoreMocks: true,
    maxWorkers: 1,
  },
};
