import { fileURLToPath } from "node:url";
import { createServer } from "../../web/node_modules/vite/dist/node/index.js";
import vue from "../../web/node_modules/@vitejs/plugin-vue/dist/index.mjs";

// UI-only browser fixture: official Vue source, isolated writable Vite cache.
const root = fileURLToPath(new URL("../../web", import.meta.url));
const cacheDir = fileURLToPath(
  new URL("../../output/ai/project-list-polish-ui/vite", import.meta.url),
);
const server = await createServer({
  configFile: false,
  root,
  plugins: [vue()],
  cacheDir,
  server: { host: "127.0.0.1", port: 5175, strictPort: true },
});
await server.listen();
let stopping = false;
async function stop() {
  if (stopping) return;
  stopping = true;
  await server.close();
  process.exit(0);
}
process.once("SIGTERM", stop);
process.once("SIGINT", stop);
