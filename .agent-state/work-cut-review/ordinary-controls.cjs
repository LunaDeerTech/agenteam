// Independent additions against frozen Work 0ad6e5b6. No network/browser.
const fs = require('node:fs');
const path = require('node:path');
const assert = require('node:assert/strict');
const Module = require('node:module');
const root = '/workspace/agenteam-work-ui';
const mode = process.argv[2] || 'owner';
assert(['owner', 'helper'].includes(mode));
const sourcePath = path.join(root, '.agent-state/work-owner-planning-ui',
  mode === 'owner' ? 'ordinary-consumer-owner-controls.cjs' : 'expected-incomplete-controls.cjs');
let source = fs.readFileSync(sourcePath, 'utf8');
if (mode === 'helper') {
  const original = 'output/ai/work-owner-planning-ui/implementation/expected-incomplete-503-controls.json';
  const output = '/workspace/agenteam-skills/output/ai/work-cut-review/ordinary-helper-controls.json';
  assert.equal(source.split(original).length, 2);
  fs.mkdirSync(path.dirname(output), { recursive: true });
  source = source.replace(original, output);
} else {
  const marker = '  const negativeFields = [';
  assert.equal(source.split(marker).length, 2);
  source = source.replace(marker, String.raw`
  for (const kind of ['milestone', 'structure']) {
    for (const held of ['reader', 'stream']) {
      await check('independent actual owner rejects a competing facade call without another fetch: ' + kind + '/' + held, async () => {
        const x = await setup(kind, { [held + 'Held']: true });
        try {
          const original = x.call();
          await x[held + 'Entered'].promise;
          await drain();
          assert.equal(x.settled(), false);
          assert.equal(x.count(), 1);
          await assert.rejects(x.auth.workPlanning.getMilestone(project, target), (e) => e.kind === 'busy');
          await drain();
          assert.equal(x.count(), 1, 'competing operation must not send HTTP');
          assert.equal(x.auth.state.busy, true);
          assert.equal(x.settled(), false);
          x[held === 'reader' ? 'releaseReader' : 'releaseStream']();
          await original;
          await drain();
          assert.equal(x.auth.state.busy, false);
          assert.equal(x.outcome(), 'fulfilled');
          const evidence = x.finish();
          assert.equal(evidence.public.calls.length, 2);
          assert.equal(evidence.public.calls[0].fulfilled, 1);
          assert.equal(evidence.public.calls[1].fulfilled, 0);
          assert.equal(evidence.public.calls[1].rejected, 1);
          assert.equal(evidence.public.calls[1].native_requests, 0);
          assert.equal(accepts(evidence), true, 'only the uniquely bound original completion qualifies');
        } finally { await x.cleanup(); }
      });
    }
  }
` + marker);
}
process.chdir(root);
const runner = new Module(sourcePath, module);
runner.filename = sourcePath;
runner.paths = Module._nodeModulePaths(path.dirname(sourcePath));
runner._compile(source, sourcePath);
