// Execute the actual finish function with explicit verifier/page/filesystem
// doubles. The real PW Promise close/join is tested by adapter controls.
const fs = require('node:fs'), vm = require('node:vm'), path = require('node:path'), assert = require('node:assert/strict');
const root = path.resolve(__dirname, '../..'), ts = require(root + '/web/node_modules/typescript');
const source = fs.readFileSync(root + '/tests/account-captcha-web/e2e/project-owner-models.spec.ts', 'utf8');
const ast = ts.createSourceFile('actual.ts', source, ts.ScriptTarget.Latest, true);
const finish = ast.statements.find(n => ts.isFunctionDeclaration(n) && n.name?.text === 'finish').getText(ast);
const code = ts.transpileModule(finish + '\nthis.finish=finish;', { compilerOptions: { target: ts.ScriptTarget.ES2024 } }).outputText;
const rows = [];
function fixture(fail) {
  const order = [], writes = [], page = { async evaluate(fn) { assert(fn.toString().includes('__projectModelsProbe.dispose()')); order.push('dispose'); if (fail === 'dispose') throw Error('closed'); } };
  const ctx = { Buffer, JSON, repository: root, directory: '/owned', protocol: 'owned', inputHash: 'fixed', join: path.join,
    async verifyBodies(p) { assert.equal(p,page); order.push('verify'); return { schema_ok:1,typed_client_ok:1 }; },
    readFileSync(name) { assert(name.endsWith('d27-project-owner-model-settings-ui.md')); return '\n```json\n{\n  "files": [],"checks_by_mode":{"authority":["safe_schema_client"],"read":["safe_schema_client"]}}\n```'; },
    exact(checks, expected) { order.push('checks'); assert.deepEqual(Object.keys(checks), [...expected]); },
    invariant(v, code) { assert(v, code); }, object: x=>x,
    async counts() { order.push('counts'); return { server:{started:1,finished:1},controls:{held:0,held_joined:0} }; },
    writeFileSync(name, value, options) { order.push('write'); writes.push({name,result:JSON.parse(value),options}); },
    renameSync(from,to) { order.push('publish'); assert.equal(from,to+'.tmp'); },
  };
  vm.runInNewContext(code,ctx);
  return { order, writes, page, run: (hook, mode='authority')=>ctx.finish(page,mode,{},0,hook) };
}
(async()=>{
  for (const mode of ['authority','read']) {
    const f=fixture();await f.run(undefined,mode);assert.deepEqual(f.order,['verify','checks','counts','write','publish','dispose']);assert.equal(f.writes[0].result.completed,true);rows.push('original no-hook '+mode+' ordering preserved');
  }
  const f=fixture();let release,started;const began=new Promise(r=>started=r);const pending=new Promise(r=>release=r);
  const actual=f.run(async()=>{f.order.push('close/join');started();await pending;});await began;assert.deepEqual(f.order,['verify','checks','counts','dispose','close/join']);assert.equal(f.writes.length,0);release();await actual;assert.deepEqual(f.order,['verify','checks','counts','dispose','close/join','write','publish']);rows.push('pending close/join blocks completion file and atomic publication');
  for (const mode of ['dispose','join']) {const f=fixture(mode);await assert.rejects(f.run(async()=>{throw Error('join');}));assert.equal(f.writes.length,0);rows.push(mode+' failure forbids completion publication');}
  const bindings=[...source.matchAll(/finish: \([^\n]*beforePublish[^\n]*/g)];assert.equal(bindings.length,1);assert(bindings[0][0].includes('"authority"'));
  const authority=fs.readFileSync(root+'/.agent-state/model-ui-recovery/authority-and-identity.ts','utf8');
  for(const text of ["denied(projects[key], false, ownerSession)","denied(projects.other, true, ownerSession)","denied(projects.admin_owned, true, ownerSession)","denied(projects.main, true, otherSession)","denied(projects.main, true, adminSession)","for (const key of ['deleting', 'pending'] as const)"])assert(authority.includes(text));
  assert.equal([...authority.matchAll(/=> denied\(/g)].length,5);rows.push('only six existing declared calls and authority-only hook binding');
  console.log(JSON.stringify({passed:rows.length,cases:rows,actual_finish_source:true,explicit_page_and_verifier_doubles:true,browser:false,network:false}));
})().catch(e=>{console.error(e);process.exitCode=1;});
