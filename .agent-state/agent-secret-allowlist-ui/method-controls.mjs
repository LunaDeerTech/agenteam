import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {createRequire} from 'node:module';
import {pathToFileURL} from 'node:url';
const root=process.cwd(), require=createRequire(root+'/tests/account-captcha-web/package.json'), ts=require('typescript');
console.log('NODE_CONTROL_START pid='+process.pid);
const sources=['tests/account-captcha-web/e2e/task-review.helpers.ts','tests/account-captcha-web/e2e/agent-secret-allowlist.helpers.ts','tests/account-captcha-web/e2e/agent-secret-allowlist.spec.ts','tests/account-captcha-web/agent-secret-allowlist.config.js'];
for(const path of sources){const source=readFileSync(path,'utf8');const result=ts.transpileModule(source,{fileName:path,reportDiagnostics:true,compilerOptions:{target:ts.ScriptTarget.ES2022,module:ts.ModuleKind.ESNext}});assert.deepEqual(result.diagnostics?.filter(d=>d.category===ts.DiagnosticCategory.Error),[]);}
const source=readFileSync(sources[0],'utf8'),file=ts.createSourceFile(sources[0],source,ts.ScriptTarget.Latest,true);
const names=['requireAgentSecretProfile','requireAgentRunnerProfile','requireSystemRunnerProfile','requireAgentRunnerHostEvidence','systemRunnerEvidenceValue'];
const selected=names.map(name=>file.statements.find(n=>ts.isFunctionDeclaration(n)&&n.name?.text===name));assert(selected.every(Boolean));
const js=ts.transpileModule(selected.map(n=>n.getText(file)).join('\n'),{compilerOptions:{target:ts.ScriptTarget.ES2022,module:ts.ModuleKind.ESNext}}).outputText;
const expectPath=pathToFileURL(root+'/tests/account-captcha-web/node_modules/@playwright/test/index.mjs').href;
const guards=await import('data:text/javascript;base64,'+Buffer.from(`import {expect} from ${JSON.stringify(expectPath)};\n${js}`).toString('base64'));
const id='01a12c02-d66a-79c4-8cf0-7f8d8a657c4f',candidate={dist:'/owned/dist',evidence:'/owned/evidence',inputHash:'a'.repeat(64),prefix:'agent-secret-allowlist',endpoint(){},lookupCommand:'agent.update',agentSecretAllowlist:true,agentSecretWireHeaders(){}},normal={mode:'secret-allowlist-normal'},lost={mode:'secret-allowlist-lost',agent_id:id};
for(const fn of [guards.requireAgentSecretProfile,guards.requireAgentRunnerProfile,guards.requireSystemRunnerProfile]){fn(candidate,normal);fn({...candidate,cut:'update'},lost);}
let rejects=0;
for(const patch of [{agentSecretAllowlist:undefined},{agentSecretAllowlist:false},{agentSecretWireHeaders:undefined},{agentSecretWireHeaders:true},{prefix:'agent-runner-mounts'},{lookupCommand:null},{lookupCommand:'agent.create'},{cut:'update'},{agentRunnerMounts:true},{systemRunnerManagement:true},{secretReads:true},{unknownOption:true}]){assert.throws(()=>guards.requireAgentSecretProfile({...candidate,...patch},normal));rejects++;}
for(const data of [{mode:'secret-allowlist-lost'},{...normal,agent_id:id},{mode:'runner-mounts-normal'}]){assert.throws(()=>guards.requireAgentSecretProfile(candidate,data));rejects++;}
assert.throws(()=>guards.requireAgentSecretProfile({prefix:'project-secrets',agentSecretWireHeaders(){}},normal));rejects++;
const wire={source:'cdp_network_extra_info',cdp_request_id:'9.1',request_id:id,method:'GET',status:200,host_present:true,host_matches:true};
const headers={request_fulfilled:true,response_fulfilled:true,cookie_present:true,host_present:false,host_matches:false,runner_wire:wire};
const proof={request_id:id,method:'GET',status:200,browser_ingress_host_matches:true,downstream_host_matches:true,handler_returned:true,handler_return_source:'owned_tls_bridge'};
guards.requireAgentRunnerHostEvidence(proof,headers);
for(const key of Object.keys(proof)){assert.throws(()=>guards.requireAgentRunnerHostEvidence({...proof,[key]:false},headers));rejects++;}
for(const key of ['request_fulfilled','response_fulfilled','cookie_present']){assert.throws(()=>guards.requireAgentRunnerHostEvidence(proof,{...headers,[key]:false}));rejects++;}
for(const key of ['source','host_present','host_matches','request_id','method','status']){assert.throws(()=>guards.requireAgentRunnerHostEvidence(proof,{...headers,runner_wire:{...wire,[key]:false}}));rejects++;}
assert(source.includes('profile.agentSecretAllowlist ? profile.agentSecretWireHeaders! : profile.agentRunnerWireHeaders!'));
assert(source.includes('requireAgentSecretProfile(profile, data);'));
assert(source.includes('consumer(auth.projectSecrets, "list", "secret.list"'));
assert(source.includes('profile.agentSecretAllowlist ? ["agent-secrets"] : []'));
const sensitive={receipt:{safe:true},token_available:true,enrollment_token:'never-retain'.padEnd(43,'x'),expires_at:'2026-10-11T22:00:00Z'};
const sanitized=guards.systemRunnerEvidenceValue(sensitive);assert(!JSON.stringify(sanitized).includes('never-retain'));
console.log(JSON.stringify({syntax_sources:sources.length,exact_profile_normal_and_lost:true,rejection_controls:rejects,actual_shared_wire_call:true,system_runner_redaction_retained:true,no_native:true}));
