// Independent additions to Work's frozen native diagnostic controls.
const fs = require("node:fs");
const vm = require("node:vm");
const assert = require("node:assert/strict");
const { execFileSync } = require("node:child_process");
const root = "/workspace/agenteam-work-ui";
process.chdir(root);
// The actual controls below read these files. Refuse a different source set.
execFileSync("git", ["diff", "--exit-code", "d3322e3e", "--",
  "tests/account-captcha-web/e2e/project-work-planning.native.ts",
  "tests/account-captcha-web/e2e/project-work-planning.publication.ts",
  ".agent-state/work-owner-planning-ui/native-diagnostic-controls.cjs",
], { cwd: root, stdio: "pipe" });
const file = ".agent-state/work-owner-planning-ui/native-diagnostic-controls.cjs";
let code = fs.readFileSync(file,"utf8");
const marker = "  await tick();\n  assert.equal(unhandled, 0);";
const index = code.lastIndexOf(marker);
assert(index > 0);
code = code.slice(0,index) + `
  await check("independent nonextensible actual reader preserves getReader success and original read",async()=>{
    const actualStream=new ReadableStream({start(controller){controller.enqueue(Uint8Array.of(1,2,3));controller.close();}});
    const reader=actualStream.getReader(), read=reader.read;
    assert.equal(Object.hasOwn(reader,"read"),false);
    Object.preventExtensions(reader);let acquisitions=0;
    const f=native();f.stream.getReader=()=>{acquisitions++;return reader;};
    try {
      await f.window.fetch(endpoint);let returned;
      assert.doesNotThrow(()=>{returned=f.stream.getReader();});
      assert.equal(returned,reader);assert.equal(acquisitions,1);
      assert.equal(reader.read,read);assert.equal(Object.hasOwn(reader,"read"),false);
      const data=await reader.read();assert.equal(data.done,false);
      assert.deepEqual(Array.from(data.value),[1,2,3]);
      assert.equal((await reader.read()).done,true);
      const end=f.finish();assert.equal(end.observer_failed,true);
      assert.equal(end.requests[0].read_calls,0);assert.equal(end.requests[0].read_done,false);
    }finally{f.finish();reader.releaseLock();}
  });
  await check("independent locked PW evaluate turns inner synchronous throw into rejection",async()=>{
    const {Page}=require("/workspace/agenteam-work-ui/tests/account-captcha-web/node_modules/playwright-core/lib/client/page.js");
    const original=Error("PRIVATE-INNER-EVALUATE-ERROR");
    let called=0,promise;
    assert.doesNotThrow(()=>{
      promise=Page.prototype.evaluate.call({_mainFrame:{evaluate(){called++;throw original;}}},()=>null);
    });
    assert.equal(called,1);
    assert(promise && typeof promise.then==="function");
    await assert.rejects(promise,error=>error===original);
  });
  for (const mode of ["cancel","streamCancel"])
    await check("independent original " + mode + " Promise identity/call count/rejection", async () => {
      const hold=deferred(), original=Error("PRIVATE-CANCEL-ERROR");
      let calls=0;
      const f=native({[mode]:()=>{calls++;return hold.promise;}});
      try {
        await f.window.fetch(endpoint);
        const reader=f.stream.getReader();
        const returned=mode==="cancel"?reader.cancel("private-reason"):f.stream.cancel("private-reason");
        assert.equal(returned,hold.promise);
        assert.equal(calls,1);
        hold.reject(original);
        await assert.rejects(returned,error=>error===original);
        await tick();
        const row=f.row();
        const prefix=mode==="cancel"?"reader_cancel":"stream_cancel";
        assert.equal(row[prefix+"_calls"],1);
        assert.equal(row[prefix+"_settled"],1);
        assert.equal(row[prefix+"_rejected"],1);
        assert.equal(row.eof_before_interruption,false);
        assert(!JSON.stringify(f.window.__workNativeDiagnostic.snapshot()).includes("PRIVATE-CANCEL-ERROR"));
      } finally {f.finish();}
    });
  await check("independent synchronous read throw preserves original error and never records settlement",async()=>{
    const original=Error("PRIVATE-READ-ERROR");let reads=0;
    const f=native({read:()=>{reads++;throw original;}});
    try {
      await f.window.fetch(endpoint);const r=f.stream.getReader();
      assert.throws(()=>r.read(),error=>error===original);
      assert.equal(reads,1);assert.equal(f.row().read_calls,1);
      assert.equal(f.row().read_settled,0);assert.equal(f.row().failure,"read-threw");
      assert.equal(f.row().read_done,false);
    }finally{f.finish();}
  });
  await check("independent cancel after retirement stays original and cannot improve ended observation",async()=>{
    const hold=deferred();let calls=0;
    const f=native({cancel:()=>{calls++;return hold.promise;}});
    await f.window.fetch(endpoint);const r=f.stream.getReader();
    const captured=r.cancel;
    const end=f.finish();const before=JSON.stringify(end);
    const p=captured();assert.equal(p,hold.promise);assert.equal(calls,1);
    hold.resolve();await p;await tick();
    assert.equal(JSON.stringify(end),before);
    assert.equal(f.row().reader_cancel_calls,0);
  });
  await check("independent rejected end sample cannot become an end witness",async()=>{
    const f=await nodeDiagnostic();f.request();
    f.handlers.push(()=>Promise.reject(Error("PRIVATE-EVALUATE-ERROR")));
    await f.diagnostic.finish();f.assertRetired();
    assert.equal(f.data().sample_joined,true);
    assert.equal(f.data().end_snapshot_observed,false);
    assert.equal(f.data().documents[0].source,"sample");
    assert(!f.writes().at(-1).includes("PRIVATE-EVALUATE-ERROR"));
  });
  await check("independent late original PW events during held finish cannot bind or complete",async()=>{
    const f=await nodeDiagnostic(), req=f.request(null), held=deferred();
    f.handlers.push(()=>held.promise);
    const finish=f.diagnostic.finish();await tick();
    // Listeners are still installed while the actual end evaluate is pending.
    assert.equal(f.page.listenerCount("response"),1);
    f.page.emit("response",{request:()=>req,headers:()=>({"x-request-id":requestID}),status:()=>200});
    f.page.emit("requestfailed",req);f.page.emit("requestfinished",req);
    f.request();
    held.resolve(f.snapshot);await finish;f.assertRetired();
    const data=f.data();assert.equal(data.requests.length,1);
    assert.equal(data.requests[0].request_id,null);
    assert.equal(data.requests[0].failed_at,null);
    assert.equal(data.requests[0].finished_event_at,null);
    assert.equal(data.documents[0].native.requests[0].bound_original_request,false);
    assert.equal(data.documents[0].native.requests[0].content_length_matches_eof,false);
    const saved=f.writes().at(-1);
    f.page.emit("response",{request:()=>req,headers:()=>({"x-request-id":requestID}),status:()=>200});
    await tick();assert.equal(f.writes().at(-1),saved);
  });
` + code.slice(index);
const run = vm.runInThisContext("(function(require){\n"+code+"\n})",{filename:"independent-work-native-controls.cjs"});
run(require);
