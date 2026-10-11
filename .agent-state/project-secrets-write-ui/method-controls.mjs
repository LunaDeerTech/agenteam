import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { createRequire } from "node:module";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";
import { spawn } from "node:child_process";

// Task-local controls execute the real selected helper AST. Synthetic request
// and proof inputs exercise predicates only; they are never native evidence.
const root = resolve(import.meta.dirname, "../..");
const output = process.env.PROJECT_SECRET_WRITE_CONTROL_OUTPUT;
assert(output && output.startsWith(resolve(root, "output/ai/project-secrets-write-ui") + "/"));
mkdirSync(output, { recursive: true, mode: 0o700 });
const require = createRequire(pathToFileURL(resolve(root, "web/package.json")));
const ts = require("typescript");
const sha = value => createHash("sha256").update(value).digest("hex");
const paths = [
  ".agent-state/project-secrets-write-ui/method-controls.mjs",
  "internal/central/app/project_secret_write_web_test.go",
  "tests/account-captcha-web/project-secret-write.config.js",
  "tests/account-captcha-web/e2e/project-secret-write.helpers.ts",
  "tests/account-captcha-web/e2e/project-secret-write.spec.ts",
  "tests/account-captcha-web/e2e/task-review.helpers.ts",
  "tests/account-captcha-web/e2e/knowledge-owner-read.native.ts",
  ".agent-state/work-owner-http/root_chain_driver.py",
  ".agent-state/task-planning-recovery/pg_only_supervisor.py",
  "web/package-lock.json", "tests/account-captcha-web/package-lock.json",
];
const inputs = Object.fromEntries(paths.map(path => [path, sha(readFileSync(resolve(root, path)))]));
inputs.node = sha(readFileSync(process.execPath));
inputs.typescript = sha(readFileSync(require.resolve("typescript")));
const inputHash = sha(JSON.stringify(inputs));
writeFileSync(resolve(output, "inputs.json"), JSON.stringify({ input_hash: inputHash, inputs }, null, 2));
const source = readFileSync(resolve(root, "tests/account-captcha-web/e2e/task-review.helpers.ts"), "utf8");
const file = ts.createSourceFile("task-review.helpers.ts", source, ts.ScriptTarget.Latest, true);
const all = [];
function visit(node) { all.push(node); ts.forEachChild(node, visit); }
visit(file);
const top = name => {
  const node = file.statements.find(n => ts.isFunctionDeclaration(n) && n.name?.text === name);
  assert(node, name); return node;
};
const observer = top("observeTaskRequests");
const nested = name => {
  const node = observer.body.statements.find(n => ts.isFunctionDeclaration(n) && n.name?.text === name);
  assert(node, name); return node;
};
const expectURL = pathToFileURL(resolve(root, "tests/account-captcha-web/node_modules/@playwright/test/index.mjs")).href;
async function moduleOf(text) {
  const result = ts.transpileModule(text, { reportDiagnostics: true, compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext } });
  assert.equal(result.diagnostics?.length ?? 0, 0);
  return import("data:text/javascript;base64," + Buffer.from(`import { expect } from ${JSON.stringify(expectURL)}; import { createHash } from "node:crypto"; import { join, resolve } from "node:path";\n${result.outputText}`).toString("base64"));
}
const rows = [];
async function control(name, body) {
  try { await body(); rows.push({ name, pass: true }); }
  catch (error) { rows.push({ name, pass: false, error_name: error?.name ?? "Error" }); }
  console.log(JSON.stringify(rows.at(-1)));
}
if (process.argv.includes("--paste-only")) {
  const stage = all.find(n => ts.isFunctionDeclaration(n) && n.name?.text === "stageSecretMaterial");
  const consume = all.find(n => ts.isFunctionDeclaration(n) && n.name?.text === "consumeSecretMaterial");
  assert(stage && consume);
  const probe = await moduleOf(`export function fixture(){let secretWriteEnabled=true;let pendingSecretMaterial;${stage.getText(file)}\n${consume.getText(file)}\nreturn {stage:stageSecretMaterial,consume:consumeSecretMaterial,pending:()=>pendingSecretMaterial!==undefined}}`);
  await control("paste-exact-original-bytes", () => { const p=probe.fixture(); p.stage("  one\r\n\ttwo\rthree\n  "); assert.equal(p.consume("  one\r\n\ttwo\rthree\n  ",true),true); assert.equal(p.pending(),false); });
  await control("paste-reject-normalized-crlf", () => { const p=probe.fixture(); p.stage("  one\r\n\ttwo\rthree\n  "); assert.equal(p.consume("  one\n\ttwo\nthree\n  ",true),false); assert.equal(p.pending(),false); });
  await control("paste-consumes-once", () => { const p=probe.fixture(); p.stage("once\r\n"); assert.equal(p.consume("once\r\n",true),true); assert.equal(p.consume("once\r\n",true),false); });
  await control("paste-metadata-has-no-pending-value", () => { const p=probe.fixture(); assert.equal(p.consume(undefined,false),true); p.stage("unexpected"); assert.equal(p.consume(undefined,false),false); assert.equal(p.pending(),false); });
await control("inputs-unchanged", () => { for (const path of paths) assert.equal(sha(readFileSync(resolve(root, path))), inputs[path], path); });
const result = { input_hash: inputHash, pid: process.pid, no_native: true, no_network: true, real_selected_helper_ast: true, synthetic_controls_only: true, passed: rows.filter(row => row.pass).length, failed: rows.filter(row => !row.pass).length, controls: rows };
writeFileSync(resolve(output, "result.json"), JSON.stringify(result, null, 2));
console.log(JSON.stringify({ completed: true, passed: result.passed, failed: result.failed, input_hash: inputHash }));
process.exitCode = result.failed ? 1 : 0;

} else {
const names = ["requireSecretWriteProfile", "secretWriteEvidenceValue", "secretWriteEvidenceProof", "requireUsageReadProfile", "requireAgentRunnerProfile", "requireAgentSecretProfile", "requireSystemRunnerProfile", "observeTaskRequests"];
const guards = await moduleOf(`let bindings=0; const knowledgeSessionBinding=async()=>{bindings++;throw Error("CONTROL_BINDING_REACHED")}; export function bindingCount(){return bindings}\n` + names.map(name=>top(name).getText(file)).join("\n"));
const id="01a12c02-d66a-79c4-8cf0-7f8d8a657c4f", project="01a12c02-d66a-79c4-8cf0-7f8d8a657c50";
const data={mode:"secret-write-normal",project_id:project,origin:"http://127.0.0.1:44301"};
const base={dist:"/owned/dist",evidence:"/owned/evidence",inputHash,endpoint(){}};
const profile={...base,prefix:"project-secret-write",lookupCommand:"project.secret_variable.update",secretWrite:true};
async function reaches(candidate,material,expected){const before=guards.bindingCount(); await assert.rejects(()=>guards.observeTaskRequests({},material,candidate),e=>expected?e.message==="CONTROL_BINDING_REACHED":e.message!=="CONTROL_BINDING_REACHED"); assert.equal(guards.bindingCount()-before,expected?1:0);}
await control("source-typescript-parse",()=>{for(const path of paths.filter(p=>/\.(ts|js|mjs)$/.test(p))){const source=ts.createSourceFile(path,readFileSync(resolve(root,path),"utf8"),ts.ScriptTarget.Latest,true);assert.equal(source.parseDiagnostics.length,0,path)}});
for(const [name,p,d] of [["normal",profile,data],["lost",{...profile,cut:"update"},{...data,mode:"secret-write-lost"}],["default",{...base,prefix:"task-review",lookupCommand:"work.task.transfer"},{mode:"review-complete"}],["system-independent",{...base,prefix:"system-runner-management",lookupCommand:"runner.create",systemRunnerManagement:true,systemRunnerWireHeaders(){}},{mode:"system-runner-normal"}],["allowlist-independent",{...base,prefix:"agent-secret-allowlist",lookupCommand:"agent.update",agentSecretAllowlist:true,agentSecretWireHeaders(){}},{mode:"secret-allowlist-normal"}]]) await control(`profile-positive-${name}`,()=>reaches(p,d,true));
for(const [name,patch] of Object.entries({missing_optin:{secretWrite:undefined},false_optin:{secretWrite:false},prefix:{prefix:"task-review"},command:{lookupCommand:"update"},normal_cut:{cut:"update"},other_read:{secretReads:true},other_write:{variableWrites:true},system:{systemRunnerManagement:true},allowlist:{agentSecretAllowlist:true},wire:{usageWireHeaders(){}},extra:{extra:true}})) await control(`profile-reject-${name}`,()=>reaches({...profile,...patch},data,false));
await control("profile-reject-lost-without-cut",()=>reaches(profile,{...data,mode:"secret-write-lost"},false));
await control("profile-reject-foreign-mode",()=>reaches(profile,{...data,mode:"other"},false));
const metadata={id,project_id:project,type:"secret",name:"BROWSER_SECRET",description:"创建说明",version:"1",created_at:"2030-01-01T00:00:00.000000Z",updated_at:"2030-01-01T00:00:00.000000Z"};
const receipt={command:"project.secret_variable.create",changed:true,variable:metadata,event_id:id,audit_id:project};
for(const [shape,value] of [["detail",metadata],["list",{items:[metadata]}],["receipt",receipt],["lookup",{status:"committed",receipt}]]){
 await control(`safe-response-positive-${shape}`,()=>assert.deepEqual(guards.secretWriteEvidenceValue(value,shape),value));
 for(const field of ["value","value_sha256","request","credential_id","payload_id"]) await control(`safe-response-reject-${shape}-${field}`,()=>assert.throws(()=>guards.secretWriteEvidenceValue({...value,[field]:"unpublishable"},shape),/^Error: SECRET_WRITE_UNSAFE_RESPONSE$/));
}
const proof={seq:1,request_id:id,go:{request_id:id,body_sha256:"a".repeat(64)},native:{request_id:id,digest:"a".repeat(64)},consumer:{id:1,typed:undefined}};
await control("safe-proof-positive",()=>assert.deepEqual(guards.secretWriteEvidenceProof(proof),{...proof,consumer:{id:1}}));
for(const part of ["go","native","consumer"])for(const field of ["value","request_body_sha256","value_digest","request"])
 await control(`safe-proof-reject-${part}-${field}`,()=>assert.throws(()=>guards.secretWriteEvidenceProof({...proof,[part]:{...proof[part],[field]:"unpublishable"}}),/^Error: SECRET_WRITE_UNSAFE_PROOF$/));
const helperSource=readFileSync(resolve(root,"tests/account-captcha-web/e2e/project-secret-write.helpers.ts"),"utf8");
const helperFile=ts.createSourceFile("project-secret-write.helpers.ts",helperSource,ts.ScriptTarget.Latest,true);
const classifier=helperFile.statements.find(n=>ts.isFunctionDeclaration(n)&&n.name?.text==="classifySecretWriteRequest");
const scope=await moduleOf(`const id=/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;\n${classifier.getText(helperFile)}`);
const prefix=`/api/v1/projects/${project}/secret-variables`, key="control-key";
const request=(method,path,body,headers={})=>({url:()=>data.origin+path,method:()=>method,headers:()=>headers,postDataBuffer:()=>body===undefined?null:Buffer.from(JSON.stringify(body))});
const state=()=>({creates:0,updates:0,lookups:0,lists:0,gets:0});
const create={request:{variable_id:id,name:"BROWSER_SECRET",description:"创建说明",value:"control-input"}};
await control("classifier-normal-full-sequence",()=>{const s=state();assert.equal(scope.classifySecretWriteRequest(request("GET",prefix+"?limit=50"),data,s),"secrets");assert.equal(scope.classifySecretWriteRequest(request("POST",prefix,create,{"idempotency-key":key}),data,s),"create");for(const [i,body] of [{name:"RENAMED_BROWSER_SECRET",description:"更新后的安全说明"},{value:"control-replacement"}].entries()){scope.classifySecretWriteRequest(request("GET",prefix+"?limit=50"),data,s);scope.classifySecretWriteRequest(request("GET",prefix+"/"+id),data,s);assert.equal(scope.classifySecretWriteRequest(request("PATCH",prefix+"/"+id,{expected_version:String(i+1),request:body},{"idempotency-key":key}),data,s),"update")} });
await control("classifier-lost-identity-only",()=>{const s={...state(),target:id,lists:1,gets:1},d={...data,mode:"secret-write-lost"};assert.equal(scope.classifySecretWriteRequest(request("PATCH",prefix+"/"+id,{expected_version:"1",request:{value:"control-input"}},{"idempotency-key":key}),d,s),"update");assert.equal(scope.classifySecretWriteRequest(request("POST",prefix+"/commands/lookup",{command:"project.secret_variable.update",target_id:id,expected_version:"1"},{"idempotency-key":key}),d,s),"lookup")});
for(const [name,method,path,body] of [["foreign-write","POST",`/api/v1/projects/${project}/variables`,create],["metadata-value","PATCH",prefix+"/"+id,{expected_version:"1",request:{name:"RENAMED_BROWSER_SECRET",description:"更新后的安全说明",value:"forbidden"}}],["lookup-full-request","POST",prefix+"/commands/lookup",{command:"project.secret_variable.update",target_id:id,expected_version:"1",request:{value:"forbidden"}}]])await control(`classifier-reject-${name}`,()=>assert.throws(()=>scope.classifySecretWriteRequest(request(method,path,body,{"idempotency-key":key}),data,{...state(),target:id,lists:1,gets:1,creates:1}),/^Error: SECRET_WRITE_REQUEST_SCOPE$/));
const requestBlock=all.find(n=>ts.isIfStatement(n)&&n.expression.getText(file)==="call && writeInputs.has(call)");assert(requestBlock);
const native=await moduleOf(`export async function run(call,original,parsed,progress){const row={};const key="control-key";const init={body:JSON.stringify(parsed),headers:{"idempotency-key":key}};const auth={projectSecretCommands:{progress}};const originalSecretWrites=[original];const writeInputs=new Map([[call,original]]);const canonical=v=>Array.isArray(v)?v.map(canonical):v&&typeof v==="object"?Object.fromEntries(Object.keys(v).sort().map(k=>[k,canonical(v[k])])):v;const tails=[];const digest=async value=>{if(new TextDecoder().decode(value)!==key)throw Error("BODY_DIGEST_FORBIDDEN");return "a".repeat(64)};const track=(promise,yes,no)=>tails.push(promise.then(yes,no));${requestBlock.getText(file)} await Promise.all(tails);return {call,row,original}}`);
for(const kind of ["create","update"]){await control(`native-${kind}-key-hash-only`,async()=>{const call={kind:`secretWrite.${kind}`},original={call,project,target:kind==="update"?id:undefined,expected:kind==="update"?"1":undefined,input:{...(kind==="create"?{name:"BROWSER_SECRET",description:"创建说明"}:{}),value:"control-input\r\n\t"}};const body=kind==="create"?{request:{variable_id:id,...original.input}}:{expected_version:"1",request:{...original.input}};const result=await native.run(call,original,body,{identity:{projectID:project,targetID:id},key});assert.equal(result.row.request_bound,true);assert.equal(result.row.error,undefined);assert.equal(result.original.input,undefined);assert(!Object.hasOwn(result.row,"request_body_sha256"));assert(!Object.hasOwn(result.call,"request_body_sha256"));})}
await control("native-reject-public-wire-drift",async()=>{const call={kind:"secretWrite.update"},original={call,project,target:id,expected:"1",input:{value:"original"}};const result=await native.run(call,original,{expected_version:"1",request:{value:"changed"}},{identity:{projectID:project,targetID:id},key});assert.equal(result.row.request_bound,false);assert.equal(result.original.input,undefined)});
await control("consumer-argument-order",()=>{const method=all.find(n=>ts.isMethodDeclaration(n)&&n.name.getText(file)==="consumers"),call=all.find(n=>ts.isCallExpression(n)&&ts.isPropertyAccessExpression(n.expression)&&n.expression.name.text==="consumers");assert.deepEqual(method.parameters.slice(-4).map(n=>n.name.getText(file)),["runnerMounts","systemRunnerManagement","secretWrite","knowledgeDelete"]);assert.deepEqual(call.arguments.slice(-4).map(n=>n.getText(file)),["runnerMounts","systemRunnerManagement","secretWrite","knowledgeDelete"])});
for (const mode of process.argv.includes("--controls-only") ? [] : ["secret-write-normal", "secret-write-lost"]) {
  const directory = resolve(output, mode); mkdirSync(directory, { mode: 0o700 });
  const command = [resolve(root, "tests/account-captcha-web/node_modules/@playwright/test/cli.js"), "test", "--config", resolve(root, "tests/account-captcha-web/project-secret-write.config.js"), "--list"];
  const env = { ...process.env, AGENTEAM_AUTH_WEB_ORIGIN: "http://127.0.0.1:44301", AGENTEAM_AUTH_WEB_PRIVATE: directory, AGENTEAM_AUTH_WEB_CHROMIUM: "/usr/bin/chromium", AGENTEAM_PROJECT_SECRET_WRITE_WEB_EVIDENCE: directory, AGENTEAM_PROJECT_SECRET_WRITE_WEB_CASE: mode, AGENTEAM_PROJECT_SECRET_WRITE_WEB_INPUT_HASH: inputHash, AGENTEAM_PROJECT_SECRET_WRITE_WEB_DIST: resolve(output, "unused-dist") };
  const child = spawn(process.execPath, command, { cwd: resolve(root, "tests/account-captcha-web"), env, stdio: ["ignore", "pipe", "pipe"] });
  let stdout = "", stderr = ""; child.stdout.on("data", value => stdout += value); child.stderr.on("data", value => stderr += value);
  console.log(JSON.stringify({ list_command: command, pid: child.pid, mode }));
  const actual = await new Promise((resolve, reject) => { child.once("error", reject); child.once("close", (code, signal) => resolve({ code, signal })); });
  writeFileSync(resolve(directory, "stdout.log"), stdout); writeFileSync(resolve(directory, "stderr.log"), stderr);
  writeFileSync(resolve(directory, "actual-wait.json"), JSON.stringify({ pid: child.pid, mode, command, input_hash: inputHash, actual_wait: true, ...actual }, null, 2));
  await control(`config-list-${mode}`, () => { assert.equal(actual.code, 0); assert.equal(actual.signal, null); assert(stdout.includes("Total: 1 test in 1 file")); assert(stdout.includes(`[${mode}]`)); assert(!stdout.includes(`[${mode === "secret-write-normal" ? "secret-write-lost" : "secret-write-normal"}]`)); });
}
await control("inputs-unchanged", () => { for (const path of paths) assert.equal(sha(readFileSync(resolve(root, path))), inputs[path], path); });
const result = { input_hash: inputHash, pid: process.pid, no_native: true, no_network: true, real_selected_helper_ast: true, synthetic_controls_only: true, passed: rows.filter(row => row.pass).length, failed: rows.filter(row => !row.pass).length, controls: rows };
writeFileSync(resolve(output, "result.json"), JSON.stringify(result, null, 2));
console.log(JSON.stringify({ completed: true, passed: result.passed, failed: result.failed, input_hash: inputHash }));
process.exitCode = result.failed ? 1 : 0;

}
