// Independent, offline increment against frozen Work ff12b21a.
// Execute only the added Blocker cases plus two original-source Resolve cases.
const fs = require('node:fs');
const path = require('node:path');
const assert = require('node:assert/strict');
const Module = require('node:module');
const root = '/workspace/agenteam-work-ui';
const mode = process.argv[2] || 'owner';
assert(['owner', 'helper'].includes(mode));
const sourcePath = path.join(root, '.agent-state/work-owner-planning-ui', mode === 'owner' ? 'ordinary-consumer-owner-controls.cjs' : 'expected-incomplete-controls.cjs');
let source = fs.readFileSync(sourcePath, 'utf8');
function replaceOnce(before, after) {
  assert.equal(source.split(before).length, 2, 'frozen source anchor');
  source = source.replace(before, after);
}
if (mode === 'helper') {
  replaceOnce('  async function check(name, fn) {', `  async function check(name, fn) {
    if (!/^ordinary.*blocker/.test(name)) return;`);
  const output = '/workspace/agenteam-skills/output/ai/work-cut-review/blocker-helper-controls.json';
  fs.mkdirSync(path.dirname(output), { recursive: true });
  replaceOnce('output/ai/work-owner-planning-ui/implementation/expected-incomplete-503-controls.json', output);
} else {
replaceOnce('  async function check(name, fn) {', `  async function check(name, fn) {
    if (!/lookup-blocker|Blocker Lookup|independent Blocker Resolve/.test(name)) return;`);
replaceOnce('command: "work.task.blocker.add",',
  'command: options.resolve ? "work.task.blocker.resolve" : "work.task.blocker.add",');
replaceOnce('request: {\n                    blocker_id: id(14),',
  'request: options.resolve ? { blocker_id: id(14), resolution_comment: "resolved" } : {\n                    blocker_id: id(14),');
replaceOnce('        if (options.wrongReceiptTask)', `        if (options.resolve && value.receipt) {
          value.receipt.blocker.resolved_at = options.unresolvedReceipt ? null : at;
          value.receipt.blocker.resolved_by = options.unresolvedReceipt ? null : value.receipt.blocker.created_by;
          value.receipt.blocker.resolution_comment = options.unresolvedReceipt ? null : "resolved";
        }
        if (options.wrongReceiptTask)`);
const marker = '  const negativeFields = [';
replaceOnce(marker, String.raw`
  for (const unresolvedReceipt of [false, true]) {
    await check('independent Blocker Resolve original strict receipt ' + (unresolvedReceipt ? 'rejects unresolved' : 'fulfills after held tail'), async () => {
      const x = await setup('lookup-blocker', { resolve: true, lookupStatus: 'committed', unresolvedReceipt, streamHeld: true });
      try {
        const original = x.call();
        await x.streamEntered.promise;
        await drain();
        assert.equal(x.settled(), false);
        assert.equal(x.auth.state.busy, true);
        await assert.rejects(x.auth.workPlanning.getTask(project, target), (e) => e.kind === 'busy');
        assert.equal(x.count(), 1);
        x.releaseStream();
        if (unresolvedReceipt) await assert.rejects(original);
        else {
          const result = await original;
          assert.equal(result.domain, 'blocker');
          assert.equal(result.value.status, 'committed');
          assert.equal(result.value.receipt.blocker.resolution_comment, 'resolved');
        }
        await drain();
        assert.equal(x.auth.state.busy, false);
        assert.equal(x.count(), 1);
        const e = x.finish();
        assert.equal(e.public.calls[0].target_id, target);
        assert.equal(e.public.calls[0].fulfilled, unresolvedReceipt ? 0 : 1);
        assert.equal(e.public.calls[0].rejected, unresolvedReceipt ? 1 : 0);
        assert.equal(accepts(e), !unresolvedReceipt);
      } finally { await x.cleanup(); }
    });
  }
` + marker);
}
process.chdir(root);
const runner = new Module(sourcePath, module);
runner.filename = sourcePath;
runner.paths = Module._nodeModulePaths(path.dirname(sourcePath));
runner._compile(source, sourcePath);
