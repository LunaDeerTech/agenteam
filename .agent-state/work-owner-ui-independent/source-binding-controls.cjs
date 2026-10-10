// Offline controls for the one independent source_run seam. Execute the actual
// helper, formal schema and actual typed Project API; no HTTP/browser fixture.
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");
const crypto = require("node:crypto");
const root = path.resolve(__dirname, "../..");
const ts = require(root + "/web/node_modules/typescript");
const expect = require(
  root + "/tests/account-captcha-web/node_modules/@playwright/test",
).expect;
const author = "TestAccountProjectWorkPlanningWebOriginalRecovery";
const independent = "TestIndependentProjectWorkPlanningWebRecovery";
const id = (n) => `01900000-0000-7000-8000-${String(n).padStart(12, "0")}`;
const at = "2026-10-10T00:00:00.000000Z";
const unhandled = [];
process.on("unhandledRejection", (error) => unhandled.push(error));

(async () => {
  const { build } = await import(
    root + "/web/node_modules/vite/dist/node/index.js"
  );
  const bundle = await build({
    configFile: false,
    root,
    logLevel: "silent",
    build: {
      write: false,
      minify: false,
      lib: {
        entry: root + "/web/src/api/project-owner.ts",
        name: "ActualProjectAPI",
        formats: ["iife"],
      },
    },
  });
  const code = (Array.isArray(bundle) ? bundle : [bundle])
    .flatMap((part) => part.output)
    .find((part) => part.type === "chunk").code;
  const apiScope = vm.createContext({
    URL,
    URLSearchParams,
    Response,
    Request,
    Headers,
    ReadableStream,
    AbortController,
    TextEncoder,
    TextDecoder,
    crypto: globalThis.crypto,
    performance,
    setTimeout,
    clearTimeout,
  });
  vm.runInContext(code, apiScope);
  const helperPath =
    root + "/tests/account-captcha-web/e2e/project-work-planning.helpers.ts";
  const source = ts.createSourceFile(
    helperPath,
    fs.readFileSync(helperPath, "utf8"),
    ts.ScriptTarget.Latest,
    true,
  );
  const declaration = source.statements.filter(
    (node) =>
      ts.isFunctionDeclaration(node) &&
      node.name?.text === "projectRefreshBody",
  );
  assert.equal(declaration.length, 1);
  const helper = ts.transpileModule(
    declaration[0].getText(source).replace(/^export /, ""),
    { compilerOptions: { target: ts.ScriptTarget.ES2022 } },
  ).outputText;
  const parent = root + "/output/ai/work-owner-ui-independent";
  fs.mkdirSync(parent, { recursive: true });
  const directory = fs.mkdtempSync(parent + "/source-binding-");
  const scope = vm.createContext({
    expect,
    readdirSync: fs.readdirSync,
    readFileSync: fs.readFileSync,
    join: path.join,
    evidence: directory,
    repository: root,
    createHash: crypto.createHash,
    spawnSync: require("node:child_process").spawnSync,
    createProjectOwnerAPI: apiScope.ActualProjectAPI.createProjectOwnerAPI,
    URL,
    Response,
    AbortController,
  });
  vm.runInContext(helper, scope);
  const body = {
    id: id(10),
    owner_user_id: id(1),
    name: "independent",
    normalized_name: "independent",
    description: "",
    lifecycle: "archived",
    version: "2",
    current_sprint_id: null,
    created_at: at,
    updated_at: at,
    archived_at: at,
  };
  const response = {
    url: () => `https://owned.invalid/api/v1/projects/${id(10)}`,
    request: () => ({ method: () => "GET" }),
    status: () => 200,
    headerValue: async () => id(100),
  };
  function sidecar(sourceRun, value = body, change = () => {}) {
    for (const name of fs.readdirSync(directory))
      fs.unlinkSync(path.join(directory, name));
    const raw = JSON.stringify(value),
      hash = crypto.createHash("sha256").update(raw).digest("hex");
    const meta = {
      request_id: id(100),
      endpoint: `/api/v1/projects/${id(10)}`,
      method: "GET",
      status: 200,
      source_run: sourceRun,
      input_hash: "a".repeat(64),
      body_sha256: hash,
      body_file: `body-${hash}.json`,
      content_type: "application/json",
    };
    fs.writeFileSync(path.join(directory, meta.body_file), raw);
    change(meta);
    fs.writeFileSync(
      path.join(directory, "response-001.json"),
      JSON.stringify(meta),
    );
  }
  let count = 0;
  async function check(name, action) {
    await action();
    count++;
    console.log("PASS", name);
  }
  try {
    await check(
      "author default retains original binding and real schema/decoder",
      async () => {
        sidecar(author);
        assert.equal(
          (await scope.projectRefreshBody(response, id(10), id(1))).project
            .version,
          "2",
        );
      },
    );
    await check(
      "explicit independent source uses same original body and decoder",
      async () => {
        sidecar(independent);
        assert.equal(
          (await scope.projectRefreshBody(response, id(10), id(1), independent))
            .project.lifecycle,
          "archived",
        );
      },
    );
    for (const [name, storedSource, argument, value, change] of [
      ["independent body refused by author default", independent, undefined],
      ["author body refused by independent call", author, independent],
      [
        "unknown source remains refused even when caller and metadata agree",
        "other",
        "other",
      ],
      [
        "wrong body hash",
        independent,
        independent,
        body,
        (meta) => {
          meta.body_sha256 = "b".repeat(64);
        },
      ],
      [
        "schema extra field",
        independent,
        independent,
        { ...body, extra: true },
      ],
      [
        "actual typed owner mismatch",
        independent,
        independent,
        { ...body, owner_user_id: id(2) },
      ],
      [
        "actual typed target mismatch",
        independent,
        independent,
        { ...body, id: id(11) },
      ],
      [
        "same XID wrong route",
        independent,
        independent,
        body,
        (meta) => {
          meta.endpoint += "/tasks";
        },
      ],
    ]) {
      await check(name, async () => {
        sidecar(storedSource, value, change);
        await assert.rejects(
          scope.projectRefreshBody(response, id(10), id(1), argument),
        );
      });
    }
  } finally {
    fs.rmSync(directory, { recursive: true });
  }
  await new Promise(setImmediate);
  assert.equal(unhandled.length, 0);
  console.log(
    "PASS source binding controls",
    count,
    "unhandled",
    unhandled.length,
  );
})().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
