// Independent additive review of the frozen four-cut method. No browser/socket.
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");
const assert = require("node:assert/strict");
const { execFileSync } = require("node:child_process");
const root = "/workspace/agenteam-work-ui";
const baseline = "152eb964";
const helperPath = "tests/account-captcha-web/e2e/project-work-planning.helpers.ts";
const controlsPath = ".agent-state/work-owner-planning-ui/expected-incomplete-controls.cjs";
const show = (name) => execFileSync("git", ["show", baseline + ":" + name], {
  cwd: root, encoding: "utf8",
});
const helper = show(helperPath);
let controls = show(controlsPath);
const marker = "  await new Promise((r) => setImmediate(r));\n  assert.equal(unhandled, 0);";
const position = controls.lastIndexOf(marker);
assert(position > 0);
controls = controls.slice(0, position) + `
  for (const kind of ["unforwarded-milestone-update", "lost-milestone-update", "lost-task-update", "lost-blocker-add"])
    await check("independent: failed tail settled then success event must reject: " + kind, async () => {
      const {e,q} = ownedCase(kind);
      const response = kind === "unforwarded-milestone-update"
        ? e.ownedResponse(q, new Promise(() => {}))
        : e.response(q, new Promise(() => {}));
      e.emit("requestfailed",q);
      await new Promise(resolve => setImmediate(resolve));
      e.emit("requestfinished",q);
      await assert.rejects(e.observed.verify(1));
      assert.equal(response.calls(),0);
      assert.equal(e.snapshot().requests[1].response_finished_at,null);
    });
  await check("independent: undeclared ordinary failure with resolved finished cannot use cut method", async () => {
    const e=observerEnv(), q=req();
    e.emit("request",q);
    const response=e.response(q,Promise.resolve(null));
    e.emit("requestfailed",q);
    await assert.rejects(e.observed.verify(0));
    assert.equal(response.calls(),1);
  });
  await check("independent: held read still owns one original finished and rejects its late failure", async () => {
    const e=observerEnv();
    const held=e.observed.declareIncomplete(spec("canceled-task-read"));
    const q=req("canceled-task-read");
    e.emit("request",q); held.authorizeCancellation();
    let reject;
    const finished=new Promise((_,r)=>reject=r);
    const response=e.response(q,finished);
    e.emit("requestfailed",q);
    await new Promise(resolve=>setImmediate(resolve));
    reject(Error("original held read finished rejected"));
    await new Promise(resolve=>setImmediate(resolve));
    await assert.rejects(e.observed.verify(1));
    assert.equal(response.calls(),1);
  });
` + controls.slice(position);
const output = "/workspace/agenteam-skills/output/ai/skills/work-cut-review";
fs.mkdirSync(output, { recursive: true });
const facade = Object.create(fs);
facade.readFileSync = (name, ...args) => name === helperPath ? helper : fs.readFileSync(name, ...args);
facade.writeFileSync = (name, data, ...args) => {
  assert.equal(name, "output/ai/work-owner-planning-ui/implementation/expected-incomplete-503-controls.json");
  return fs.writeFileSync(path.join(output,"independent-controls.json"), data, ...args);
};
process.chdir(root);
const run = vm.runInThisContext("(function(require){\n" + controls + "\n})", {
  filename: "independent-frozen-work-cuts.cjs",
});
run((name) => name === "fs" ? facade : require(name));
