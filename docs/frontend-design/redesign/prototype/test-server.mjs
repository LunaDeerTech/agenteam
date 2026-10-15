// Owned, fixture-only servers. No backend is started or contacted.
import { createServer as createHTTPServer } from "node:http";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { resolve, extname, sep } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const directory = resolve(fileURLToPath(new URL(".", import.meta.url)));
const repository = resolve(directory, "../../../..");
const port = Number(process.env.F0_PORT || 5188);
const legacyPort = Number(process.env.F0_LEGACY_PORT || 5189);
const includeLegacy = process.env.F0_LEGACY === "1";
const output = resolve(
  process.env.F0_OUTPUT || resolve(repository, "output/ai/frontend-f0"),
);
const lifecycle = {
  pid: process.pid,
  port,
  legacyPort: includeLegacy ? legacyPort : null,
  started: false,
  closed: false,
};
async function recordLifecycle() {
  await mkdir(output, { recursive: true });
  await writeFile(
    resolve(output, "server-lifecycle.json"),
    JSON.stringify(lifecycle, null, 2),
  );
}
for (const value of [port, legacyPort]) {
  if (!Number.isInteger(value) || value < 1024 || value > 65535)
    throw new Error("Invalid owned port");
}
const mime = {
  ".html": "text/html",
  ".js": "text/javascript",
  ".css": "text/css",
  ".svg": "image/svg+xml",
  ".pdf": "application/pdf",
  ".json": "application/json",
};
const staticServer = createHTTPServer(async (request, response) => {
  try {
    if (request.method !== "GET" && request.method !== "HEAD") {
      response.writeHead(405);
      response.end("Fixture only");
      return;
    }
    const pathname = decodeURIComponent(
      new URL(request.url, "http://127.0.0.1").pathname,
    );
    if (pathname === "/__f0_health") {
      response.writeHead(200, { "Content-Type": "text/plain" });
      response.end("F0 fixture-only");
      return;
    }
    const target = resolve(
      directory,
      "." + (pathname === "/" ? "/index.html" : pathname),
    );
    if (
      !target.startsWith(directory + sep) &&
      target !== resolve(directory, "index.html")
    ) {
      response.writeHead(403);
      response.end();
      return;
    }
    const body = await readFile(target);
    response.writeHead(200, {
      "Content-Type": mime[extname(target)] || "application/octet-stream",
      "Cache-Control": "no-store",
    });
    response.end(request.method === "HEAD" ? undefined : body);
  } catch {
    response.writeHead(404);
    response.end("Not found");
  }
});
let legacy;
let legacyHTTP;
async function shutdown() {
  await Promise.all([
    new Promise((done) => staticServer.close(done)),
    legacyHTTP && new Promise((done) => legacyHTTP.close(done)),
    legacy?.close(),
  ]);
  lifecycle.closed = true;
  await recordLifecycle();
  console.log("F0_SERVERS_CLOSED");
}
let closing = false;
async function stop() {
  if (closing) return;
  closing = true;
  try {
    await shutdown();
    process.exitCode = 0;
  } catch (error) {
    console.error(error);
    process.exitCode = 1;
  }
}
try {
  if (includeLegacy) {
    const modules = process.env.F0_WEB_NODE_MODULES;
    if (!modules)
      throw new Error(
        "F0_WEB_NODE_MODULES must name the matching locked external web install",
      );
    const { createServer } = await import(
      pathToFileURL(resolve(modules, "vite/dist/node/index.js"))
    );
    const { default: vue } = await import(
      pathToFileURL(resolve(modules, "@vitejs/plugin-vue/dist/index.mjs"))
    );
    legacy = await createServer({
      configFile: false,
      root: resolve(repository, "web"),
      plugins: [vue()],
      cacheDir: resolve(output, "vite"),
      server: { middlewareMode: true, hmr: false },
    });
    legacyHTTP = createHTTPServer((request, response) =>
      legacy.middlewares(request, response),
    );
    await new Promise((done, reject) => {
      legacyHTTP.once("error", reject);
      legacyHTTP.listen(legacyPort, "127.0.0.1", done);
    });
  }
  await new Promise((done, reject) => {
    staticServer.once("error", reject);
    staticServer.listen(port, "127.0.0.1", done);
  });
  // Register after the Vite/native build tooling has initialized its handlers.
  process.once("SIGTERM", stop);
  process.once("SIGINT", stop);
  lifecycle.started = true;
  await recordLifecycle();
  console.log(
    JSON.stringify({
      fixtureOnly: true,
      pid: process.pid,
      prototype: `http://127.0.0.1:${port}`,
      legacy: includeLegacy ? `http://127.0.0.1:${legacyPort}` : null,
    }),
  );
} catch (error) {
  console.error(error);
  await shutdown();
  process.exitCode = 1;
}
