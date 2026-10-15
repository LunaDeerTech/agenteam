const fs=require('node:fs'),path=require('node:path');
const root=process.env.AGENTEAM_MEETING_WEB_PLAYWRIGHT;
if(require(path.join(root,'package.json')).version!=='1.56.1')throw new Error('LOCKED_PLAYWRIGHT_REQUIRED');
const {chromium,expect}=require(root);
if(process.argv.includes('--list')){console.log('TestMeetingM3ApprovalOwnerWeb: scoped approve / original lookup / reject / cancel A / B continues');process.exit(0)}
const input=JSON.parse(fs.readFileSync('input.json','utf8'));
let stage='launch',step='launch',diagnosticStatus=0,diagnosticCode='';
let executionReads=0,executionReadFailures=0,executionReadStatus=0,pageErrors=0,currentAgent=null;
(async()=>{
 let browser,page;
 try{
  browser=await chromium.launch({executablePath:process.env.AGENTEAM_AUTH_WEB_CHROMIUM,args:['--no-sandbox']});
  const context=await browser.newContext({viewport:{width:1440,height:900},colorScheme:'light'});
  await context.addCookies([{name:'agenteam_local_session',value:input.cookie,url:input.origin,httpOnly:true,sameSite:'Strict'}]);
  page=await context.newPage();page.setDefaultTimeout(8000);
  const executionRead=request=>request.method()==='GET'&&new URL(request.url()).origin===input.origin&&/\/meetings\/[^/]+\/contributions\/[^/]+\/execution$/.test(new URL(request.url()).pathname);
  page.on('response',response=>{if(executionRead(response.request())){executionReads++;executionReadStatus=response.status()}});
  page.on('requestfailed',request=>{if(executionRead(request))executionReadFailures++});
  page.on('pageerror',()=>{pageErrors++});
  stage='open';await page.goto(input.origin+input.route);await page.getByRole('heading',{name:'开始一场会议'}).waitFor();
  stage='choose';await page.getByRole('button',{name:'编排 Agent',exact:true}).click();
  const agents=page.getByRole('dialog',{name:'编排参会 Agent'});
  await agents.getByRole('checkbox',{name:/meeting-approval-agent/}).check();
  await agents.getByRole('checkbox',{name:/meeting-continuing-agent/}).check();
  await agents.getByRole('button',{name:'完成',exact:true}).click();
  await page.getByRole('button',{name:'发言模式',exact:true}).click();
  await page.getByRole('option',{name:'串行发言',exact:true}).click();
  await page.getByRole('textbox',{name:'会议首条消息',exact:true}).fill('Request the command approval, then let both participants contribute.');
  stage='start';const created=page.waitForResponse(r=>r.request().method()==='POST'&&r.url()===input.origin+'/api/v1/projects/'+input.project_id+'/meetings');
  await page.getByRole('button',{name:'发送',exact:true}).click();const response=await created;diagnosticStatus=response.status();
  if(response.status()!==200)throw new Error('START_REJECTED');
  const uuid=/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
  await page.waitForURL(url=>url.origin===input.origin&&url.pathname.startsWith(input.route+'/')&&uuid.test(url.pathname.slice(input.route.length+1)));
  const receipt={meeting_id:new URL(page.url()).pathname.slice(input.route.length+1)},sources=[];
  const current=()=>page.locator('.turn').last();
  const agent=()=>current().locator('article.contribution.agent').filter({has:page.getByText('meeting-approval-agent',{exact:true})});
  currentAgent=agent;
  const complete=async()=>{await expect(current().locator('.turn-heading > span').nth(1)).toHaveText('已完成')};
  const pending=async()=>{
   executionReads=0;executionReadFailures=0;executionReadStatus=0;
   step='open-current-execution';
   const read=page.waitForResponse(r=>r.request().method()==='GET'&&r.url().includes('/meetings/'+receipt.meeting_id+'/contributions/')&&r.url().endsWith('/execution')&&!sources.some(s=>r.url().includes('/contributions/'+s.contribution+'/')));
   await agent().getByRole('button',{name:'查看执行与审批',exact:true}).click();
   const result=await read;diagnosticStatus=result.status();
   if(result.status()!==200)throw new Error('EXECUTION_READ_REJECTED');
   const contributionID=new URL(result.url()).pathname.split('/').at(-2);
   const executionFacts=agent().locator('.execution-details > dl dd');await expect(executionFacts).toHaveCount(2);
   const executionID=await executionFacts.nth(0).innerText();
   if(!uuid.test(contributionID)||!uuid.test(executionID))throw new Error('EXECUTION_IDENTITY_INVALID');
   // The first read may still precede the approval checkpoint. Its original
   // current-Execution poll is the only source of the subsequent waiting view.
   step='wait-approval-reference';await agent().getByRole('button',{name:'查看待审批操作',exact:true}).waitFor();
   step='read-pending-approval';const request=page.waitForResponse(r=>r.request().method()==='GET'&&r.url().includes('/approvals/'));
   await agent().getByRole('button',{name:'查看待审批操作',exact:true}).click();
   const fetched=await request;diagnosticStatus=fetched.status();
   if(fetched.status()!==200)throw new Error('APPROVAL_READ_REJECTED');
   // Read the same public identity the Owner can inspect. The application's
   // typed approval reader checks the response; the original Go owner below
   // independently verifies this exact Approval/Execution/operation tuple.
   step='approval-enabled';await expect(agent().getByRole('button',{name:'批准此次操作',exact:true})).toBeEnabled();
   step='approval-identity';const panel=agent().getByRole('region',{name:'执行审批',exact:true});
   await panel.getByText('审批来源与操作身份',{exact:true}).click();
   const identity=panel.locator('details dd');await expect(identity).toHaveCount(3);
   const [approvalExecution,operationID,approvalID]=await identity.allTextContents();
   if(approvalExecution!==executionID||![operationID,approvalID].every(id=>uuid.test(id)))throw new Error('APPROVAL_SCOPE_CHANGED');
   sources.push({contribution:contributionID,execution:executionID,approval:approvalID,operation:operationID});
  };
  stage='approve';await pending();
  await page.screenshot({path:path.join(input.evidence,'approval-desktop-light.png'),fullPage:true});
  await agent().getByRole('button',{name:'批准此次操作',exact:true}).click();
  await expect(agent().getByRole('button',{name:'确认批准此次操作',exact:true})).toBeFocused();
  await agent().getByRole('button',{name:'确认批准此次操作',exact:true}).click();
  await agent().getByRole('button',{name:'核对原审批决定',exact:true}).waitFor();
  stage='lookup';const looked=page.waitForResponse(r=>r.request().method()==='POST'&&r.url().endsWith('/decision/lookup'));
  await agent().getByRole('button',{name:'核对原审批决定',exact:true}).click();const lookup=await looked;
  if(lookup.status()!==200)throw new Error('APPROVAL_LOOKUP_FAILED');
  await expect(agent().getByText(/已确认\s*批准\s*此次操作/)).toBeVisible();
  await page.getByText('A completed the approved command.',{exact:true}).waitFor();
  await page.getByText('B reviewed the approved contribution.',{exact:true}).waitFor();await complete();
  stage='reject';await page.getByRole('textbox',{name:'发送到会议',exact:true}).fill('Request again; I will reject this command.');
  step='send-second-turn';await page.getByRole('button',{name:'发送',exact:true}).click();step='second-turn-count';await expect(page.locator('.turn')).toHaveCount(2);await pending();
  step='choose-reject';await agent().getByRole('button',{name:'拒绝此次操作',exact:true}).click();
  step='confirm-reject';await agent().getByRole('button',{name:'确认拒绝此次操作',exact:true}).click();
  step='wait-rejected-contribution';await page.getByText('A continued after the rejection without running the command.',{exact:true}).waitFor();
  await page.getByText('B reviewed the rejected contribution.',{exact:true}).waitFor();await complete();
  stage='cancel';await page.getByRole('textbox',{name:'发送到会议',exact:true}).fill('Request once more; I will cancel only the first participant.');
  await page.getByRole('button',{name:'发送',exact:true}).click();await expect(page.locator('.turn')).toHaveCount(3);await pending();
  await agent().getByRole('button',{name:'取消此 Agent 发言',exact:true}).click();
  await page.getByRole('dialog',{name:'取消此 Agent 发言？',exact:true}).getByRole('button',{name:'取消此 Agent',exact:true}).click();
  await page.getByText('B continued after A was cancelled.',{exact:true}).waitFor();await complete();
  await expect(agent().getByRole('button',{name:'批准此次操作',exact:true})).toHaveCount(0);
  await expect(agent().getByText('已取消',{exact:true}).first()).toBeVisible();
  stage='reload';await page.reload();await page.getByText('B continued after A was cancelled.',{exact:true}).waitFor();
  await agent().getByRole('button',{name:'查看执行与审批',exact:true}).click();
  await expect(agent().getByRole('button',{name:'批准此次操作',exact:true})).toHaveCount(0);
  await expect(agent().getByText('已取消',{exact:true}).first()).toBeVisible();
  stage='layout';await page.emulateMedia({colorScheme:'dark',reducedMotion:'reduce'});await page.setViewportSize({width:390,height:844});
  await page.getByRole('button',{name:'详情',exact:true}).click();await page.getByRole('dialog',{name:'会议详情',exact:true}).waitFor();await page.keyboard.press('Escape');
  if(!await page.getByRole('button',{name:'详情',exact:true}).evaluate(el=>el===document.activeElement))throw new Error('DETAIL_FOCUS_NOT_RESTORED');
  if(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth))throw new Error('PAGE_OVERFLOW');
  await page.screenshot({path:path.join(input.evidence,'approval-mobile-dark.png'),fullPage:true});
  stage='close';await context.close();await browser.close();browser=null;
  fs.writeFileSync('result.json',JSON.stringify({completed:true,meeting:receipt.meeting_id,sources}),{mode:0o600});console.log('MEETING_BROWSER_COMPLETE');
 }catch(error){
  const message=String(error&&error.message||'');
  const kind=error&&error.name==='TimeoutError'?'TIMEOUT':/^[A-Z_]{1,64}$/.test(message)?message:message.includes('strict mode violation')?'STRICT_LOCATOR':message.includes('toBeEnabled')?'EXPECT_ENABLED':message.includes('toHaveCount')?'EXPECT_COUNT':message.includes('toBeFocused')?'EXPECT_FOCUSED':message.includes('toHaveText')?'EXPECT_TEXT':'UNCLASSIFIED';
  const line=Number((String(error&&error.stack||'').match(/meeting[^\s:]*\.(?:js|cjs):(\d+):/)||[])[1]||0);
  let executionVisible='absent',executionBusy=false,executionError=false,documentHidden=false;
  if(page)try{
   documentHidden=await page.evaluate(()=>document.hidden);
   if(currentAgent){
    const panel=currentAgent().locator('.execution-details');
    if(await panel.count()){
     executionBusy=await panel.getAttribute('aria-busy')==='true';
     executionError=await panel.getByText('执行状态未确认',{exact:true}).count()>0;
     for(const [label,value] of [['等待准备','created'],['正在准备','preparing'],['正在执行','running'],['等待工具操作审批','approval'],['等待用户决定','decision'],['执行成功','succeeded'],['执行失败','failed'],['已取消','cancelled']]){
      if(await panel.getByText(label,{exact:true}).count()){executionVisible=value;break}
     }
    }
   }
  }catch{}
  fs.writeFileSync('failure.json',JSON.stringify({stage,step,line,status:diagnosticStatus,code:diagnosticCode,error:kind,execution_reads:executionReads,execution_read_failures:executionReadFailures,execution_read_status:executionReadStatus,page_errors:pageErrors,execution_visible:executionVisible,execution_busy:executionBusy,execution_error:executionError,document_hidden:documentHidden}),{mode:0o600});
  if(page)await page.screenshot({path:path.join(input.evidence,'approval-failure.png'),fullPage:true}).catch(()=>{});
  if(browser)await browser.close().catch(()=>{});process.exitCode=1;
 }
})();
