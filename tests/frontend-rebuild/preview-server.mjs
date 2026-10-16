import { spawn } from "node:child_process";
import { cpSync, existsSync, mkdirSync, writeFileSync } from "node:fs";
import path from "node:path";
import { build } from "../../web/node_modules/vite/dist/node/index.js";
import vue from "../../web/node_modules/@vitejs/plugin-vue/dist/index.mjs";

const root = process.cwd();
const output = path.resolve(
  process.env.VUE_TEST_OUTPUT || "output/ai/frontend-vue-rebuild-test/browser",
);
const port = Number(process.env.VUE_TEST_PORT || 5242);
if (!Number.isInteger(port) || port < 1024 || port > 65535) {
  throw new Error("VUE_TEST_PORT must be an unprivileged TCP port");
}
if (!existsSync(path.join(root, "web/dist/index.html"))) {
  throw new Error("Build the new web application before browser verification");
}
mkdirSync(output, { recursive: true });
// The production bundle is copied, never rewritten. Test-only components are
// compiled from the current public source into an isolated preview subtree.
const previewRoot = path.join(output, "preview-dist");
cpSync(path.join(root, "web/dist"), previewRoot, { recursive: true });
await build({
  configFile: false,
  root: path.join(root, "web/tests/browser"),
  base: "/__test__/",
  plugins: [vue()],
  build: { outDir: path.join(previewRoot, "__test__"), emptyOutDir: true },
});
const child = spawn(
  process.execPath,
  [
    path.join(root, "web/node_modules/vite/bin/vite.js"),
    "preview",
    "--host",
    "127.0.0.1",
    "--port",
    String(port),
    "--strictPort",
    "--outDir",
    previewRoot,
  ],
  { cwd: path.join(root, "web"), stdio: "inherit" },
);
const lifecycle = {
  wrapperPID: process.pid,
  previewPID: child.pid,
  port,
  started: false,
  closed: false,
};
const record = () =>
  writeFileSync(
    path.join(output, "preview-lifecycle.json"),
    JSON.stringify(lifecycle, null, 2) + "\n",
  );
let stopping = false;
child.on("spawn", () => {
  lifecycle.started = true;
  record();
});
for (const signal of ["SIGTERM", "SIGINT"]) {
  process.on(signal, () => {
    stopping = true;
    child.kill(signal);
  });
}
child.on("error", (error) => {
  lifecycle.error = error.message;
  record();
  process.exitCode = 1;
});
child.on("close", (code, signal) => {
  lifecycle.closed = true;
  lifecycle.exitCode = code;
  lifecycle.signal = signal;
  record();
  process.exitCode = stopping ? 0 : (code ?? 1);
});
