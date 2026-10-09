// Reuse the frozen author's actual Vue/Session/Workspace fixture, but execute
// only this bounded independent set. No server, browser, PG or socket.
const fs = require("node:fs"),
  path = require("node:path"),
  Module = require("node:module"),
  assert = require("node:assert/strict");
const root = "/workspace/agenteam-work-ui";
const file = root + "/.agent-state/work-owner-planning-ui/project-refresh-completion-controls.cjs";
let source = fs.readFileSync(file, "utf8");
const marker = '  await check(\n    "production Vue owner; original Workspace Promise; typed archived publication",';
assert.equal(source.split(marker).length, 2);
source = source.slice(0, source.indexOf(marker));
const exposed = "      timers,\n      entered,";
assert.equal(source.split(exposed).length, 2);
source = source.replace(exposed, "      originalWorkspace, originalPublic, lastPublic: () => lastPublic,\n" + exposed);
source += String.raw`
  await check("independent actual canonical rename settles and publishes", async () => {
    const f = await setup({rename: true});
    try {
      await f.call(); await drain();
      const proof = report(f.w);
      assert.equal(f.w.location.pathname, "/owner/renamed/tasks/explore");
      assert.equal(f.workspace.detail.project.normalized_name, "renamed");
      assert.equal(native.workOrdinaryConsumption(proof, 1, id(100), true), true);
      assert.equal(native.workOrdinaryConsumption(proof, 1, id(100)), false);
      assert.equal(f.requests(), 1);
    } finally { f.close(); }
  });
  await check("independent explicit-adopt call remains outside exact no-arg refresh", async () => {
    const f = await setup();
    try {
      const promise = f.workspace.readCurrent(true);
      await promise; await drain();
      assert.equal(f.workspace.detail.project.lifecycle, "archived");
      assert.equal(f.w.__workPublicationDiagnostic.snapshot().calls.length, 0);
      assert.equal(native.workOrdinaryConsumption(report(f.w), 1, id(100), true), false);
      assert.equal(f.requests(), 1);
    } finally { f.close(); }
  });
  await check("independent direct facade preserves original Promise but cannot prove Workspace", async () => {
    const f = await setup();
    try {
      const promise = f.auth.projects.get(id(10));
      assert.equal(promise, f.lastPublic());
      const value = await promise; await drain();
      assert.equal(value.lifecycle, "archived");
      assert.equal(f.auth.state.busy, false);
      assert.equal(f.workspace.detail.project.lifecycle, "active");
      assert.equal(f.w.__workPublicationDiagnostic.snapshot().calls.length, 0);
      assert.equal(native.workOrdinaryConsumption(report(f.w), 1, id(100), true), false);
      assert.equal(f.requests(), 1);
    } finally { f.close(); }
  });
  await check("independent two actual reads sharing XID cannot supply a unique proof", async () => {
    const f = await setup();
    try {
      await f.call(); await drain();
      await f.call(); await drain();
      const n = f.w.__workNativeDiagnostic.snapshot();
      assert.equal(f.requests(), 2);
      assert.equal(n.requests.length, 2);
      assert.equal(f.w.__workPublicationDiagnostic.snapshot().calls.length, 2);
      const proof = report(f.w);
      // The author's convenience report projects one request for its one-read
      // fixture. Preserve both actual captured entries in this two-read probe.
      proof.documents[0].native.requests = Array.from(n.requests, (row) => ({...row}));
      assert.equal(native.workOrdinaryConsumption(proof, 1, id(100), true), false);
    } finally { f.close(); }
  });
  for (const key of ["workspace", "facade"]) await check("independent owned " + key + " hook collision fails retirement", async () => {
    const f = await setup();
    try {
      await f.call(); await drain();
      const replacement = function () { throw Error("replacement must not be invoked"); };
      if (key === "workspace") f.workspace.readCurrent = replacement;
      else f.auth.projects.get = replacement;
      const proof = report(f.w);
      assert.equal(proof.documents[0].publication.observer_failed, true);
      assert.equal(key === "workspace" ? f.workspace.readCurrent : f.auth.projects.get, replacement);
      assert.equal(native.workOrdinaryConsumption(proof, 1, id(100), true), false);
      // Fixture teardown repairs our own replacement only; it is not an
      // observer retirement proof and cannot turn the saved failed proof true.
      f.workspace.readCurrent = f.originalWorkspace;
      f.auth.projects.get = f.originalPublic;
    } finally { f.close(); }
  });
  await drain();
  assert.equal(unhandled.length, 0);
  console.log("PASS independent Project read controls", count, "unhandled", unhandled.length);
})().catch((e) => { console.error(e); process.exitCode = 1; });
`;
const compiled = new Module(file, module);
compiled.filename = file;
compiled.paths = Module._nodeModulePaths(path.dirname(file));
compiled._compile(source, file);
