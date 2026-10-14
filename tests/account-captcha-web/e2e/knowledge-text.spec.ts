import {test,expect,type Page} from "@playwright/test";
import {knowledgeTextMaterial,observeKnowledgeText,knowledgeTextScreenshot} from "./knowledge-text.helpers";
const observations=new WeakMap<Page,Awaited<ReturnType<typeof observeKnowledgeText>>>();
test.afterEach(async({page},info)=>{const seen=observations.get(page);try{await seen?.retire();}finally{seen?.save(info.status,info.errors);}});
async function press(page:Page,name:string){const button=page.getByRole("button",{name,exact:true});await expect(button).toBeEnabled();await button.focus();await expect(button).toBeFocused();await page.keyboard.press("Enter");}
test("creates and replaces plain root and Markdown child text [create-and-replace]",async({page})=>{
 const data=knowledgeTextMaterial();await page.setViewportSize({width:1440,height:960});await page.emulateMedia({colorScheme:"light",reducedMotion:"reduce"});
 await page.context().addCookies([{name:"agenteam_local_session",value:data.cookie,url:process.env.AGENTEAM_AUTH_WEB_ORIGIN!,httpOnly:true,sameSite:"Lax",secure:false}]);
 const seen=await observeKnowledgeText(page,data);observations.set(page,seen);
 await page.goto(data.route);await expect(page.getByRole("region",{name:"项目知识库",exact:true})).toBeVisible();
 for(const index of [0,1] as const){
  seen.stage(index===0?"plain-create-open":"markdown-create-open");
  await press(page,index===0?"新建文本":"新建子文档");
  const create=page.getByRole("dialog",{name:"新建文本",exact:true});await expect(create).toBeVisible();
  const title=create.getByRole("textbox",{name:"标题",exact:true});await expect(title).toBeFocused();await title.fill(index===0?"纯文本笔记":"Markdown 子文档");
  await press(page,"文本格式");await page.getByRole("option",{name:index===0?"纯文本":"Markdown",exact:true}).click();
  await create.getByRole("textbox",{name:"正文",exact:true}).fill(index===0?data.plain:data.markdown);
  seen.stage(index===0?"plain-create-save":"markdown-create-save");
  await press(page,"保存文本");await expect(create.getByText("保存已确认；已读取当前文档、正文与目录。",{exact:true})).toBeVisible();
  seen.stage(index===0?"plain-create-proof":"markdown-create-proof");await seen.verify();seen.current(index,1);
  await press(page,"关闭编辑器");await expect(create).toHaveCount(0);await expect(page.locator(".ui-overlay")).toHaveCount(0);
  await expect(page.locator(".knowledge-document h2[tabindex='-1']")).toHaveText(index===0?"纯文本笔记":"Markdown 子文档");
  await expect.poll(()=>page.locator("pre.canonical-text").textContent()).toBe(index===0?data.plain:data.markdown);
  seen.stage(index===0?"plain-edit-open":"markdown-edit-open");await press(page,"编辑正文");const edit=page.getByRole("dialog",{name:"编辑正文",exact:true});await expect(edit).toBeVisible();
  const body=edit.getByRole("textbox",{name:"正文",exact:true});await expect(body).toHaveValue(index===0?data.plain:data.markdown);await body.fill(index===0?data.plain_updated:data.markdown_updated);
  seen.stage(index===0?"plain-edit-save":"markdown-edit-save");await press(page,"保存文本");await expect(edit.getByText("保存已确认；已读取当前文档、正文与目录。",{exact:true})).toBeVisible();
  seen.stage(index===0?"plain-edit-proof":"markdown-edit-proof");await seen.verify();seen.current(index,2);await press(page,"关闭编辑器");await expect(edit).toHaveCount(0);await expect(page.locator(".ui-overlay")).toHaveCount(0);
  await expect.poll(()=>page.locator("pre.canonical-text").textContent()).toBe(index===0?data.plain_updated:data.markdown_updated);
  if(index===1){
   seen.stage("narrow-document-drawer");await press(page,"文档树");
   const drawer=page.getByRole("dialog",{name:"文档树",exact:true});await expect(drawer).toBeVisible();
   const selected=drawer.getByRole("tree",{name:"知识库文档"}).locator(`[data-tree-id="${seen.target(index)}"]`);
   await expect(selected).toBeVisible();await expect(selected).toHaveAttribute("aria-selected","true");
   await selected.focus();await expect(selected).toBeFocused();await page.keyboard.press("Escape");
   await expect(drawer).toHaveCount(0);await expect(page.getByRole("button",{name:"文档树",exact:true})).toBeFocused();
  }else{
   const selected=page.getByRole("tree",{name:"知识库文档"}).locator(`[data-tree-id="${seen.target(index)}"]`);
   await expect(selected).toBeVisible();await expect(selected).toHaveAttribute("aria-selected","true");
  }
  if(index===0){await expect(page.locator("html")).toHaveAttribute("data-theme","light");await knowledgeTextScreenshot(page,"1440-light-plain");await page.setViewportSize({width:390,height:844});await page.emulateMedia({colorScheme:"dark",reducedMotion:"reduce"});}
 }
 await expect(page.locator("html")).toHaveAttribute("data-theme","dark");expect(await page.evaluate(()=>matchMedia("(prefers-reduced-motion: reduce)").matches)).toBe(true);
 seen.stage("final-publication");await knowledgeTextScreenshot(page,"390-dark-markdown");await seen.finish();
});
