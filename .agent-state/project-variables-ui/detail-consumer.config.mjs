import config from "./authority-consumer.config.mjs";
import { fileURLToPath } from "node:url";
import vue from "../../web/node_modules/@vitejs/plugin-vue/dist/index.mjs";
export default {
  ...config,
  plugins: [vue()],
  cacheDir: fileURLToPath(
    new URL(
      "../../output/ai/project-variables-ui/implementation/detail-consumer-vite-cache",
      import.meta.url,
    ),
  ),
  test: {
    ...config.test,
    include: [
      fileURLToPath(
        new URL("./detail-consumer-controls.spec.ts", import.meta.url),
      ),
    ],
  },
};
