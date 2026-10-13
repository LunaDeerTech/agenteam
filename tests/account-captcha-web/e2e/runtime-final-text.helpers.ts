import { expect, type Page, type Request, type Response, type Route } from "@playwright/test";
import { existsSync, readFileSync, writeFileSync } from "node:fs";
import { join, resolve } from "node:path";
import { createHash } from "node:crypto";
import { knowledgeSessionBinding } from "./knowledge-owner-read.native";
import { observeBrowserIO } from "./task-review.helpers";
import { observeSystemRunnerWire } from "./system-runner-management.helpers";
import type { TaskExecution } from "../../../web/src/api/task-executions";
import type { TaskExecutionOutput } from "../../../web/src/api/task-execution-output";

const digest = (value: string) => createHash("sha256").update(value).digest("hex");
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
type Material = { mode: string; origin: string; cookie: string; project_id: string; task_id: string; agent_id: string; execution: TaskExecution; runtime_view: TaskExecutionOutput; route: string; title: string; task_state: string; task_version: string };
export function runtimeTextMaterial(): Material {
  const value: Material = JSON.parse(readFileSync(join(process.env.AGENTEAM_AUTH_WEB_PRIVATE!, "runtime-final-text-material.json"), "utf8"));
  expect(value.mode).toBe("final-text-load-refresh-and-safe-render");
  expect(value.origin).toBe(process.env.AGENTEAM_AUTH_WEB_ORIGIN);
  for (const id of [value.project_id, value.task_id, value.agent_id, value.execution.id]) expect(id).toMatch(uuid);
  expect(value.execution).toMatchObject({project_id:value.project_id,task_id:value.task_id,agent_id:value.agent_id,status:"succeeded",purpose:"task/work"});
  expect(value.runtime_view.execution).toEqual(value.execution);
  expect(value.runtime_view.availability).toBe("available");
  expect(value.runtime_view.snapshot?.schema).toBe("durable_final_text_v1");
  expect(value.runtime_view.snapshot?.generation).toBe(value.execution.snapshot_id);
  expect(value.runtime_view.snapshot?.items).toHaveLength(1);
  expect(value.runtime_view.snapshot?.items[0]?.data.content).toContain("<script>globalThis.__runtime_text_executed = true</script>");
  expect(value.route).toMatch(new RegExp(`^/[^/]+/runtime-app-a/tasks/${value.task_id}$`));
  expect(value.cookie.length > 0).toBe(true);
  return value;
}

type NativeRow = {id:number;call_id:number;method:string;path:string;request_id:string;status:number;length:string;bytes:number;digest:string;typed_digest:string;fetch_returned:boolean;fetch_rejected:boolean;readers:number;reads:number;read_returns:number;eof:boolean;closed:boolean;release:number;reader_cancel:number;stream_cancel:number;error:boolean};
type NativeCall = {id:number;kind:string;bound:number;fulfilled:boolean;rejected:boolean;settled:boolean;current:boolean;not_busy:boolean;typed:unknown;typed_digest:string|null};
type Native = {retired:boolean;failed:boolean;pending:number;current:boolean;not_busy:boolean;rows:NativeRow[];calls:NativeCall[]};
type NativeHost = Window & { __taskReviewProof?: { consumers(...args: unknown[]): void; snapshot(): Native; finish(): Native } };
type Entry = { request:Request; uri:string; name:string; response?:Response; responseCount:number; finishedCount:number; failedCount:number; failure:"not_observed"|"net::ERR_ABORTED"|"other"|"unavailable"; checked:boolean };

// The shared observer retains each original public Promise, fetch, Response,
// reader and return. This new wrapper adds only the closed Execution contract.
export async function observeRuntimeText(page: Page, data: Material) {
  const evidence = process.env.AGENTEAM_RUNTIME_FINAL_TEXT_WEB_EVIDENCE!;
  const entries: Entry[] = [], proofs: Record<string, unknown>[] = [];
  const base = `/api/v1/projects/${data.project_id}`, task = `${base}/tasks/${data.task_id}`, list = `${task}/executions`, detail = `${list}/${data.execution.id}`, output = `${detail}/runtime-view`;
  const binding = await knowledgeSessionBinding(resolve(process.cwd(), "../.."), process.env.AGENTEAM_RUNTIME_FINAL_TEXT_WEB_DIST!);
  const entryURL = new URL(binding.entry,data.origin).href;
  const wire = await observeSystemRunnerWire(page, data.origin);
  let failed = false, retired = false, completed = false, stage = "entry", handlers = 0;
  let retirementAttempted = false, nativeRetired = false, routeRemoved = false;
  const classify = (request: Request) => {
    const url = new URL(request.url());
    if (url.origin !== data.origin) throw Error("EXECUTION_EXTERNAL_REQUEST");
    if (!url.pathname.startsWith("/api/")) return undefined;
    if (request.method() !== "GET" || request.postData() !== null) throw Error("EXECUTION_BROWSER_WRITE");
    if (url.pathname === list) { expect([...url.searchParams]).toEqual([["limit", "50"]]); return "executions"; }
    if (url.pathname === detail) { expect(url.search).toBe(""); return "execution"; }
    if (url.pathname === output) { expect(url.search).toBe(""); return "output"; }
    const exact: Record<string,string> = {
      "/api/v1/session":"session", "/api/v1/projects/resolve":"resolve", "/api/v1/projects":"projects",
      [base]:"project", [`${base}/milestones`]:"milestones", [`${base}/sprints`]:"sprints", [`${base}/tasks`]:"tasks", [task]:"task", [`${task}/blockers`]:"blockers", [`${task}/events`]:"events", [`${base}/agents`]:"agents", [`${base}/agents/${data.agent_id}`]:"agent",
    };
    if (exact[url.pathname]) return exact[url.pathname]!;
    if (url.pathname.startsWith(base+"/sprints/") && uuid.test(url.pathname.slice((base+"/sprints/").length))) return "sprint";
    if (url.pathname.startsWith(base+"/milestones/") && uuid.test(url.pathname.slice((base+"/milestones/").length))) return "milestone";
    throw Error("EXECUTION_UNOBSERVED_ENDPOINT");
  };
  const onRequest = (request: Request) => {
    try { const name = classify(request); if (!name) return; if (entries.length >= 64) throw Error("EXECUTION_REQUEST_BOUND"); const url = new URL(request.url()); entries.push({request,uri:url.pathname+url.search,name,responseCount:0,finishedCount:0,failedCount:0,failure:"not_observed",checked:false}); }
    catch { failed = true; }
  };
  const onResponse = (response: Response) => {
    const entry = entries.find(e => e.request === response.request()); if (!entry) return;
    entry.response = response; if (++entry.responseCount !== 1) failed = true;
  };
  const onFinished = (request: Request) => {const entry = entries.find(e => e.request === request);if (!entry) return;if (++entry.finishedCount !== 1 || entry.failedCount) failed = true;};
  const onFailed = (request: Request) => {
    const entry = entries.find(e => e.request === request);if (!entry) return;
    entry.failedCount++;
    const reason = request.failure()?.errorText;
    entry.failure = reason === "net::ERR_ABORTED" ? "net::ERR_ABORTED" : typeof reason === "string" ? "other" : "unavailable";
    if (entry.failedCount !== 1 || entry.finishedCount || entry.failure !== "net::ERR_ABORTED") failed = true;
  };
  const install = async (route: Route) => {
    handlers++;
    try {
      await page.evaluate(async ({binding,project,task}) => {
        const module = await import(/* @vite-ignore */ binding.asset);
        const use = module[binding.exportName];
        if (typeof use !== "function" || use() !== use()) throw Error("EXECUTION_SINGLETON");
        // Optional arguments preserve all existing profiles. Only this new
        // fixture binds history and final-output reads in the final opt-ins.
        const proof=(window as NativeHost).__taskReviewProof;
        if(!proof||typeof proof.consumers!=="function")throw Error("RUNTIME_NATIVE_CONSUMER_MISSING");
        proof.consumers(use(),project,false,false,undefined,task,undefined,undefined,undefined,undefined,false,undefined,undefined,false,false,false,false,false,false,undefined,false,false,undefined,false,undefined,undefined,undefined,undefined,task,task);
      },{binding,project:data.project_id,task:data.task_id});
      await route.continue();
    } catch {failed = true;await route.abort();} finally {handlers--;}
  };
  try {
    page.on("request",onRequest);page.on("response",onResponse);page.on("requestfinished",onFinished);page.on("requestfailed",onFailed);
    await observeBrowserIO(page, () => {});
    await page.route(entryURL,install);
  } catch(setupError) {
    try {
      try {await page.unroute(entryURL,install);}
      finally {
        page.off("request",onRequest);page.off("response",onResponse);page.off("requestfinished",onFinished);page.off("requestfailed",onFailed);
        await wire.close();
      }
    } finally {
      // No observer reached afterEach. Preserve setup failure after the actual
      // cleanup attempt, without inventing native retirement.
      throw setupError;
    }
  }
  const snapshot = () => page.evaluate(() => {
    const proof=(window as NativeHost).__taskReviewProof;
    if(!proof||typeof proof.snapshot!=="function")throw Error("RUNTIME_NATIVE_SNAPSHOT_MISSING");
    return proof.snapshot();
  });
  async function verify() {
    await expect.poll(async () => {
      const n = await snapshot();
      return !failed && !n.failed && !n.retired && n.pending === 0 && n.not_busy && n.calls.every(c => c.settled) &&
        n.rows.length === entries.length && entries.every(e => e.responseCount === 1 &&
          (e.finishedCount === 1 && e.failedCount === 0 || e.finishedCount === 0 && e.failedCount === 1 && e.failure === "net::ERR_ABORTED"));
    }).toBe(true);
    const native = await snapshot();
    for (const e of entries.filter(e=>!e.checked)) {
      const response = e.response!, headers = await response.allHeaders(), rid = headers["x-request-id"]!;
      expect(rid).toMatch(uuid);
      const rows = native.rows.filter(n=>n.request_id===rid);expect(rows).toHaveLength(1);const n=rows[0]!;
      const calls = native.calls.filter(c=>c.id===n.call_id);expect(calls).toHaveLength(1);const call=calls[0]!;
      expect(n).toMatchObject({method:"GET",path:new URL(e.request.url()).pathname,fetch_returned:true,fetch_rejected:false,readers:1,eof:true,closed:true,release:1,reader_cancel:0,stream_cancel:0,error:false});
      expect(n.reads).toBeGreaterThan(0);expect(n.reads).toBe(n.read_returns);expect(n.length).toMatch(/^(0|[1-9][0-9]*)$/);expect(Number(n.length)).toBe(n.bytes);
      expect(n.status).toBe(response.status());expect(n.digest).toMatch(/^[a-f0-9]{64}$/);
      expect(call).toMatchObject({bound:1,settled:true});
      expect(headers["cache-control"]).toBe("no-store");expect(Number(headers["content-length"])).toBe(n.bytes);
      const original=JSON.parse(readFileSync(join(evidence,`runtime-final-text-response-${rid}.json`),"utf8"));
      expect(original).toEqual({request_id:rid,method:"GET",url_sha256:digest(e.uri),status:response.status(),content_length:n.bytes,body_sha256:n.digest,original_eof:true,original_closed:true,handler_returned:true,request_host_matches:true});
      expect(response.status()).toBe(200);
      {
        expect(call).toMatchObject({fulfilled:true,rejected:false,current:true,not_busy:true});
        expect(n.typed_digest).toMatch(/^[a-f0-9]{64}$/);expect(call.typed_digest).toBe(n.typed_digest);
        if(e.name==="executions") {expect(call.kind).toBe("work.executions");expect(call.typed).toEqual({items:[data.execution],next_cursor:null});}
        else if(e.name==="execution") {expect(call.kind).toBe("work.execution");expect(call.typed).toEqual(data.execution);}
        else if(e.name==="output") {expect(call.kind).toBe("work.executionOutput");expect(call.typed).toEqual(data.runtime_view);}
      }
      if(e.finishedCount===1) expect(await response.finished()).toBeNull();
      else expect(e.failure).toBe("net::ERR_ABORTED");
      // A live ERR_ABORTED remains a failed browser terminal. It is acceptable
      // only with independent complete original consumer/reader/Go return and
      // actual CDP wire correlation for this very request, including auxiliaries.
      const wireProof=await wire.headers({method:"GET",url:e.request.url(),status:response.status(),requestID:rid});
      proofs.push({request_id:rid,name:e.name,status:response.status(),url_sha256:digest(e.uri),body_sha256:n.digest,typed_sha256:call.typed_digest,original_eof:true,reader_released:true,handler_returned:true,terminal:e.finishedCount===1?"finished":"failed",failure_kind:e.failure,wire:wireProof});
      e.checked=true;
    }
  }
  async function retire() {
    if(retirementAttempted)return;
    retirementAttempted=true;
    let native:Native|undefined;
    try {native=await page.evaluate(()=>{
      const proof=(window as NativeHost).__taskReviewProof;
      if(!proof||typeof proof.finish!=="function")throw Error("RUNTIME_NATIVE_FINISH_MISSING");
      return proof.finish();
    });expect(native.pending).toBe(0);expect(native.failed).toBe(false);expect(native.retired).toBe(true);nativeRetired=true;}
    finally {
      try { await page.unroute(entryURL,install);expect(handlers).toBe(0);routeRemoved=true; }
      finally {
        page.off("request",onRequest);page.off("response",onResponse);page.off("requestfinished",onFinished);page.off("requestfailed",onFailed);
        await wire.close();retired=nativeRetired&&routeRemoved&&wire.snapshot().actual_detach;
      }
    }
  }
  return {
    stage(value:string){if(!/^[a-z-]{1,48}$/.test(value))throw Error("EXECUTION_STAGE");stage=value;},
    verify,
    async held(){await expect.poll(()=>existsSync(join(process.env.AGENTEAM_AUTH_WEB_PRIVATE!,"reached-runtime-output"))).toBe(true);},
    release(){writeFileSync(join(process.env.AGENTEAM_AUTH_WEB_PRIVATE!,"release-runtime-output"),"release",{mode:0o600});},
    async finish(){
      await verify();
      const selected=entries.filter(e=>["executions","execution","output"].includes(e.name));expect(selected).toHaveLength(4);
      expect(selected.filter(e=>e.name==="executions")).toHaveLength(1);expect(selected.filter(e=>e.name==="execution")).toHaveLength(1);expect(selected.filter(e=>e.name==="output")).toHaveLength(2);expect(selected.every(e=>e.response?.status()===200)).toBe(true);
      wire.complete();
      await retire();completed=true;stage="complete";
      writeFileSync(join(process.env.AGENTEAM_AUTH_WEB_PRIVATE!,"runtime-final-text-result.json"),JSON.stringify({completed:true,execution_requests:4,original_requests:entries.length}),{mode:0o600});
    },
    async save(status:string,errorLocations:string[]){
      let native:Native|undefined,diagnostic="available";
      try {native=await snapshot();}catch{diagnostic="unavailable";}
      try {if(!retired)await retire();}catch{failed=true;}
      finally {
        if(!retired)failed=true;
        writeFileSync(join(evidence,"runtime-final-text-browser-proof.json"),JSON.stringify({stage,status,completed,retired,native_retired:nativeRetired,route_removed:routeRemoved,failed,error_locations:errorLocations.filter(v=>/^runtime-final-text\.(spec|helpers)\.ts:\d+:\d+$/.test(v)).slice(0,8),native: native?{failed:native.failed,pending:native.pending,not_busy:native.not_busy,retired:native.retired,requests:native.rows.length,settled:native.calls.every(c=>c.settled)}:{diagnostic},original_requests:entries.length,events:entries.map(e=>({name:e.name,url_sha256:digest(e.uri),status:e.response?.status(),response_count:e.responseCount,finished_count:e.finishedCount,failed_count:e.failedCount,failure_kind:e.failure,checked:e.checked})),proof:proofs,wire:wire.snapshot()},null,2),{mode:0o600});
      }
    },
  };
}

export async function runtimeTextScreenshot(page:Page,name:string){
  expect(name).toMatch(/^(wide-light|narrow-dark)-(loading|output)$/);
  await expect.poll(()=>page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1)).toBe(true);
  await page.screenshot({path:join(process.env.AGENTEAM_RUNTIME_FINAL_TEXT_WEB_EVIDENCE!,`runtime-final-text-${name}.png`),fullPage:true,animations:"disabled"});
}
