import {expect, type Page, type Request} from "@playwright/test";
import {mkdirSync, readFileSync, writeFileSync} from "node:fs";
import {join} from "node:path";
import {observeTaskRequests} from "./task-review.helpers";
const id=/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
export type KnowledgeTextMaterial={mode:"create-and-replace";cookie:string;project_id:string;route:string;plain:string;plain_updated:string;markdown:string;markdown_updated:string};
export function knowledgeTextMaterial():KnowledgeTextMaterial {
 const data=JSON.parse(readFileSync(join(process.env.AGENTEAM_AUTH_WEB_PRIVATE!,"knowledge-text-material.json"),"utf8"));
 expect(data.mode).toBe("create-and-replace");expect(data.project_id).toMatch(id);expect(data.route).toMatch(/^\/[^/?#]+\/knowledge-text\/knowledge$/);
 expect(data.plain).toBe("原始纯文本 世界\n<script>only text</script>\n");expect(data.plain_updated).toBe("更新纯文本 🌍\nSecond complete line.\n");
 expect(data.markdown).toBe("# Markdown 子文档\n\n**原始内容** and `<script>`\n");expect(data.markdown_updated).toBe("# Markdown 更新\n\n- 第一行\n- second 🌍\n");return data;
}
export async function observeKnowledgeText(page:Page,data:KnowledgeTextMaterial){
 const project=`/api/v1/projects/${data.project_id}`,prefix=project+"/knowledge/documents";
 const targets:string[]=[],writes:Request[]=[];
 const classify=(request:Request):string|undefined=>{
  const url=new URL(request.url()),path=url.pathname,method=request.method();if(!path.startsWith("/api/"))return undefined;
  if(method==="POST"){
   expect(writes.length).toBeLessThan(4);const n=writes.length,create=n%2===0,index=Math.floor(n/2);
   expect(url.search).toBe("");expect(path.startsWith(prefix+"/")).toBe(true);
   const parts=path.slice(prefix.length+1).split("/");expect(parts).toHaveLength(2);expect(parts[0]).toMatch(id);expect(parts[1]).toBe(create?"create-text":"replace-text");
   if(create){expect(targets).not.toContain(parts[0]);targets.push(parts[0]);}else expect(parts[0]).toBe(targets[index]);
   expect(request.postDataJSON()).toEqual(create?{parent_document_id:index===0?null:targets[0],title:index===0?"纯文本笔记":"Markdown 子文档",media_type:index===0?"text/plain":"text/markdown",text:index===0?data.plain:data.markdown}:{expected_version:"1",media_type:index===0?"text/plain":"text/markdown",text:index===0?data.plain_updated:data.markdown_updated});
   writes.push(request);return create?"create":"update";
  }
  expect(method).toBe("GET");
  const global:Record<string,string>={"/api/v1/session":"session","/api/v1/me":"profile","/api/v1/me/preferences":"preferences","/api/v1/auth/bootstrap":"bootstrap","/api/v1/projects/resolve":"resolve","/api/v1/projects":"projects"};if(global[path])return global[path];
  if(path===project&&!url.search)return "project";
  if(path===prefix+"/children"){
   expect([...url.searchParams.keys()]).toEqual(["parent_document_id","limit"]);expect(url.searchParams.get("limit")).toBe("50");expect(["null",...targets]).toContain(url.searchParams.get("parent_document_id"));return "documents";
  }
  for(const target of targets){
   if(path===prefix+"/"+target&&!url.search)return "document";
   if(path===prefix+"/"+target+"/ancestors"&&!url.search)return "ancestors";
   if(path===prefix+"/"+target+"/content"){
    expect([...url.searchParams.keys()]).toEqual(["byte_offset","max_bytes"]);expect(url.searchParams.get("byte_offset")).toBe("0");expect(["65536","1048576"]).toContain(url.searchParams.get("max_bytes"));return "content";
   }
  }
  throw Error("UNOBSERVED_KNOWLEDGE_TEXT_ENDPOINT");
 };
 const evidence=process.env.AGENTEAM_KNOWLEDGE_TEXT_WEB_EVIDENCE!,inputHash=process.env.AGENTEAM_KNOWLEDGE_TEXT_WEB_INPUT_HASH!;
 const seen=await observeTaskRequests(page,data,{dist:process.env.AGENTEAM_KNOWLEDGE_TEXT_WEB_DIST!,evidence,inputHash,prefix:"knowledge-text",endpoint:classify,lookupCommand:null,knowledgeTextWrites:true}).catch(error=>{
  const code=error instanceof Error && /^[A-Z_]{1,80}$/.test(error.message)?error.message:"OBSERVATION_INSTALL_FAILED";
  writeFileSync(join(evidence,"knowledge-text-stage.json"),JSON.stringify({stage:"observation-install",code}),{mode:0o600});
  throw error;
 });
 let completed=false,stage="entry";
 return {...seen,
  stage(value:string){if(!/^[a-z0-9-]{1,64}$/.test(value))throw Error("INVALID_KNOWLEDGE_STAGE");stage=value;},
  target(index:number){expect(targets[index]).toMatch(id);return targets[index]!;},
  current(index:0|1,version:1|2){
   const receipt=seen.receipt(version===1?"create":"update",index),target=targets[index];
   expect(Object.keys(receipt)).toEqual(["document"]);
   const doc=receipt.document;
   expect(Object.keys(doc).sort()).toEqual(["content_version","created_at","created_by","id","indexing_status","media_type","parent_document_id","project_id","source_kind","status","title","updated_at"]);
   expect(doc).toMatchObject({id:target,project_id:data.project_id,parent_document_id:index===0?null:targets[0],title:index===0?"纯文本笔记":"Markdown 子文档",source_kind:"text",media_type:index===0?"text/plain":"text/markdown",content_version:String(version)});
   expect(seen.knowledgeTextRead("document",target)).toEqual({active:doc});
   const text=index===0?(version===1?data.plain:data.plain_updated):(version===1?data.markdown:data.markdown_updated);
   expect(seen.knowledgeTextRead("content",target)).toEqual({document:doc,text:{text,next_byte_offset:String(Buffer.byteLength(text)),truncated:false}});
   expect(new Set(writes.map(r=>r.headers()["idempotency-key"])).size).toBe(writes.length);
   return doc;
  },
  async finish(){expect(writes).toHaveLength(4);expect(seen.requests("lookup")).toHaveLength(0);await seen.verify();await seen.endDocument();completed=true;},
  save(status:unknown,errors:readonly {stack?:string}[]=[]){
   seen.save(status as any);
   writeFileSync(join(evidence,"knowledge-text-stage.json"),JSON.stringify({stage,status,error_locations:errors.map(error=>error.stack?.match(/(?:knowledge-text\.(?:helpers|spec)|task-review\.helpers)\.ts:[0-9]+:[0-9]+/)?.[0]??"unavailable")}),{mode:0o600});
   if(completed&&status==="passed"){
    const proof=JSON.parse(readFileSync(join(evidence,"knowledge-text-create-and-replace.json"),"utf8"));
    writeFileSync(join(process.env.AGENTEAM_AUTH_WEB_PRIVATE!,"knowledge-text-result.json"),JSON.stringify({completed,input_hash:inputHash,original_requests:proof.original_requests,all_observed:proof.all_observed,lookups:proof.lookups}),{mode:0o600});
   }
  },
 };
}
export async function knowledgeTextScreenshot(page:Page,name:string){
 await expect.poll(()=>page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1)).toBe(true);
 const directory=process.env.AGENTEAM_KNOWLEDGE_TEXT_WEB_EVIDENCE!;mkdirSync(directory,{recursive:true,mode:0o700});
 await page.screenshot({path:join(directory,`knowledge-text-${name}.png`),fullPage:true,animations:"disabled"});
}
