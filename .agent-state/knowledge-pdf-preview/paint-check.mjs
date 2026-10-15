// Public PDF/CSS first-paint probe. No App, credentials, or database.
// node paint-check.mjs <repo> <output-dir> <locked-node_modules> <chromium>
import { createServer } from 'node:http';
import { writeFile, readFile } from 'node:fs/promises';
import { join, isAbsolute } from 'node:path';
import { pathToFileURL } from 'node:url';
import { createRequire } from 'node:module';
const [repo,output,modules,executable]=process.argv.slice(2);
if([repo,output,modules,executable].some(x=>!x||!isAbsolute(x)))throw new Error('EXPLICIT_ABSOLUTE_PROBE_PATHS_REQUIRED');
const {chromium}=await import(pathToFileURL(join(modules,'playwright/index.mjs')).href);
const require=createRequire(import.meta.url);
const {PNG}=require(join(modules,'playwright-core/lib/utilsBundle.js'));
const text="Mercury's orbital period is 87 Earth days.";
const stream=`BT /F1 12 Tf 72 720 Td (${text}) Tj ET\n`;
const objects=['<< /Type /Catalog /Pages 2 0 R >>','<< /Type /Pages /Kids [3 0 R] /Count 1 >>','<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>','<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>',`<< /Length ${stream.length} >>\nstream\n${stream}endstream`];
let pdf='%PDF-1.4\n'; const offsets=[];
for(const [i,obj] of objects.entries()){offsets.push(pdf.length);pdf+=`${i+1} 0 obj\n${obj}\nendobj\n`;}
const xref=pdf.length;pdf+=`xref\n0 ${objects.length+1}\n0000000000 65535 f \n`;
for(const offset of offsets)pdf+=`${String(offset).padStart(10,'0')} 00000 n \n`;
pdf+=`trailer\n<< /Size ${objects.length+1} /Root 1 0 R >>\nstartxref\n${xref}\n%%EOF\n`;
const source=await readFile(join(repo,'web/src/views/projects/ProjectKnowledgeView.vue'),'utf8');
const css=source.match(/\.file-preview\s*\{[^}]+\}/)[0];
const server=createServer((req,res)=>{res.writeHead(200,{'Content-Type':'text/html'});res.end(`<!doctype html><meta charset="utf-8"><title>Public PDF render timing</title><style>body{margin:0;background:#f7f7f9}main{margin:449px 0 0 454px;width:800px}${css}</style><main></main>`)});
await new Promise(r=>server.listen(0,'127.0.0.1',r));
let browser;
const target=join(output,'pdf-paint');
try{
 browser=await chromium.launch({executablePath:executable,headless:true,args:['--no-sandbox']});
 const page=await browser.newPage({viewport:{width:1440,height:960}});
 await page.goto(`http://127.0.0.1:${server.address().port}`);
 await page.evaluate(pdf=>{const el=document.createElement('object');el.type='application/pdf';el.className='file-preview';el.title='当前 PDF 文档预览';el.data=URL.createObjectURL(new Blob([pdf],{type:'application/pdf'}));document.querySelector('main').append(el)},pdf);
 await page.waitForFunction(()=>document.querySelector('object').contentDocument?.readyState==='complete',{},{timeout:1000});
 const start=Date.now();
 let count=0,white=0,raw;
 do{
  raw=await page.screenshot({animations:'disabled'}); count++;
  const png=PNG.sync.read(raw);white=0;
  for(let y=510;y<900;y+=3)for(let x=510;x<1190;x+=3){const p=(y*png.width+x)*4;if(png.data[p]>235&&png.data[p+1]>235&&png.data[p+2]>235)white++}
  if(count===1)await writeFile(`${target}-initial.png`,raw);
 }while(white<5000&&Date.now()-start<2500);
 await writeFile(`${target}-final.png`,raw);
 const facts={captures:count,elapsed_ms:Date.now()-start,white_samples:white,paint_observed:white>5000,type:await page.locator('object').evaluate(el=>el.contentDocument?.contentType)};
 await writeFile(`${target}.json`,JSON.stringify(facts));console.log(JSON.stringify(facts));
 if(!facts.paint_observed)process.exitCode=1;
}finally{if(browser)await browser.close();await new Promise(r=>server.close(r));}
