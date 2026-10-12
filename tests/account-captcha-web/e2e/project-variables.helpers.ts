import { expect, type Page, type Request } from "@playwright/test";
import { mkdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { observeTaskRequests } from "./task-review.helpers";

const id = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
export type ProjectVariablesMaterial = {
  mode: "create-update-clear-and-delete";
  cookie: string;
  project_id: string;
  route: string;
  draft: { name: string; description: string; value: string };
  changed_description: string;
  changed_value: string;
};
export function projectVariablesMaterial(): ProjectVariablesMaterial {
  const data = JSON.parse(readFileSync(join(process.env.AGENTEAM_AUTH_WEB_PRIVATE!, "project-variables-material.json"), "utf8"));
  expect(data.mode).toBe("create-update-clear-and-delete");
  expect(data.project_id).toMatch(id);
  expect(data.route).toMatch(/^\/[^/?#]+\/[^/?#]+\/settings\/variables$/);
  expect(data.draft).toEqual({name:"BROWSER_ORDINARY",description:"Ordinary project setting.",value:"public-fixture-initial\nKeep the complete value."});
  expect(data.changed_description).toBe("Updated ordinary project setting.");
  expect(data.changed_value).toBe("public-fixture-updated\nSecond complete line.");
  return data;
}
export async function observeProjectVariables(page:Page,data:ProjectVariablesMaterial){
  const prefix=`/api/v1/projects/${data.project_id}`;
  let target:string|undefined;
  let updates=0;
  let deletes=0;
  const classify=(request:Request):string|undefined=>{
    const url=new URL(request.url()),path=url.pathname,method=request.method();
    if(!path.startsWith("/api/"))return undefined;
    if(method==="POST"&&path===prefix+"/variables"&&!url.search){
      expect(target,"only one original Create").toBeUndefined();
      const body=request.postDataJSON();
      expect(body?.request?.variable_id).toMatch(id);
      expect(body).toEqual({request:{variable_id:body.request.variable_id,...data.draft}});
      target=body.request.variable_id;return "create";
    }
    if(method==="PATCH"&&target&&path===prefix+"/variables/"+target&&!url.search){
      expect(updates).toBeLessThan(2);
      expect(request.postDataJSON()).toEqual({expected_version:String(updates+1),request:updates===0?{description:data.changed_description,value:data.changed_value}:{value:""}});
      updates++;return "update";
    }
    if(method==="DELETE"&&target&&path===prefix+"/variables/"+target&&!url.search){
      expect(updates).toBe(2);expect(deletes).toBe(0);
      expect(request.postDataJSON()).toEqual({expected_version:"3"});
      deletes++;return "delete";
    }
    if(method!=="GET")throw Error("UNOBSERVED_PROJECT_VARIABLE_WRITE");
    const global:Record<string,string>={"/api/v1/session":"session","/api/v1/me":"profile","/api/v1/me/preferences":"preferences","/api/v1/auth/bootstrap":"bootstrap","/api/v1/projects/resolve":"resolve","/api/v1/projects":"projects"};
    if(global[path])return global[path];
    if(path===prefix&&!url.search)return "project";
    if(path===prefix+"/variables"){
      expect([...url.searchParams.entries()]).toEqual([["limit","50"]]);return "variables";
    }
    if(target&&path===prefix+"/variables/"+target&&!url.search)return "variable";
    throw Error("UNOBSERVED_PROJECT_VARIABLE_ENDPOINT");
  };
  const seen=await observeTaskRequests(page,data,{dist:process.env.AGENTEAM_PROJECT_VARIABLES_WEB_DIST!,evidence:process.env.AGENTEAM_PROJECT_VARIABLES_WEB_EVIDENCE!,inputHash:process.env.AGENTEAM_PROJECT_VARIABLES_WEB_INPUT_HASH!,prefix:"project-variables",endpoint:classify,lookupCommand:null,variableWrites:true});
  const receipts:Record<string,any>[]=[];
  return {...seen,
    targetID(){expect(target).toMatch(id);return target!;},
    requireDeletion(){
      expect(seen.requests("create")).toHaveLength(1);expect(seen.requests("update")).toHaveLength(2);expect(seen.requests("delete")).toHaveLength(1);expect(seen.requests("lookup")).toHaveLength(0);
      const result=seen.receipt("delete",0);
      expect(Object.keys(result).sort()).toEqual(["audit_id","changed","command","deleted","event_id"]);
      expect(result).toEqual({command:"project.variable.delete",changed:true,deleted:{id:target,project_id:data.project_id,type:"variable",version:"4",deleted_at:result.deleted.deleted_at},event_id:result.event_id,audit_id:result.audit_id});
      expect(result.deleted.deleted_at).toMatch(/^\d{4}-\d{2}-\d{2}T/);expect(result.event_id).toMatch(id);expect(result.audit_id).toMatch(id);
      expect(seen.variableRead("variables")).toEqual({items:[]});
      const writes=[...seen.requests("create"),...seen.requests("update"),...seen.requests("delete")];
      expect(new Set(writes.map(r=>r.headers()["idempotency-key"])).size).toBe(4);
    },
    requireVersion(version:1|2|3){
      expect(seen.requests("create")).toHaveLength(1);
      expect(seen.requests("update")).toHaveLength(version-1);
      expect(seen.requests("lookup")).toHaveLength(0);
      const result=seen.receipt(version===1?"create":"update",version===1?0:version-2);
      expect(Object.keys(result).sort()).toEqual(["audit_id","changed","command","event_id","variable"]);
      expect(result.command).toBe(version===1?"project.variable.create":"project.variable.update");
      expect(result.changed).toBe(true);expect(result.event_id).toMatch(id);expect(result.audit_id).toMatch(id);
      const v=result.variable;
      expect(Object.keys(v).sort()).toEqual(["created_at","description","id","name","project_id","type","updated_at","value","version"]);
      expect(v).toEqual({id:target,project_id:data.project_id,type:"variable",name:data.draft.name,description:version===1?data.draft.description:data.changed_description,value:version===1?data.draft.value:version===2?data.changed_value:"",version:String(version),created_at:version===1?v.created_at:receipts[0].variable.created_at,updated_at:version===1?v.created_at:v.updated_at});
      expect(typeof v.created_at).toBe("string");expect(typeof v.updated_at).toBe("string");
      if(receipts.length<version)receipts.push(result);
      else expect(result).toEqual(receipts[version-1]);
      const {value,...summary}=v;
      expect(seen.variableRead("variable")).toEqual(v);
      expect(seen.variableRead("variables")).toEqual({items:[summary]});
      expect(Object.keys(summary)).toHaveLength(8);
      const writes=[...seen.requests("create"),...seen.requests("update")];
      expect(new Set(writes.map(r=>r.headers()["idempotency-key"])).size).toBe(version);
      return result;
    },
  };
}
export async function projectVariablesScreenshot(page:Page,data:ProjectVariablesMaterial,name:string){
  await expect.poll(()=>page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1)).toBe(true);
  const directory=process.env.AGENTEAM_PROJECT_VARIABLES_WEB_EVIDENCE!;
  mkdirSync(directory,{recursive:true,mode:0o700});
  await page.screenshot({path:join(directory,`project-variables-${data.mode}-${name}.png`),fullPage:true,animations:"disabled"});
}
