const fs=require('node:fs');
const base='/workspace/scratch/usage-http-verification/a02/';
const doc=JSON.parse(fs.readFileSync(base+'api/openapi/project-usage.json','utf8'));
const common=JSON.parse(fs.readFileSync(base+'api/openapi/common.json','utf8'));
const compiled=[];function walk(x,path){if(!x||typeof x!=='object')return;for(const [k,v] of Object.entries(x)){if(k==='pattern'){new RegExp(v,'u');compiled.push(path+'/pattern')}else walk(v,path+'/'+k)}}
walk(doc,'project');walk(common,'common');
const sets=[
 ['ProviderRequestID',false,['_.:-09AZaz','x'.repeat(256)],['x/y','x y','😀','x'.repeat(257)]],
 ['ProjectName',false,['A-._9','x'.repeat(64)],['a/','a%','é','x'.repeat(65)]],
 ['NormalizedProjectName',false,['a-._9'],['A']],
 ['Username',false,['admin','AdMin','A--z','x'.repeat(32)],['ab','-admin','admin-','a.b','x'.repeat(33)]],
 ['NonnegativeInt64String',true,['0','1','9007199254740993','9223372036854775807'],['01','+1','-1','1e2','9223372036854775808','9999999999999999999','１２']],
 ['PositiveInt64String',true,['1','9007199254740993','9223372036854775807'],['0','01','9223372036854775808']],
 ['Instant',true,['2026-10-07T01:02:03.123456Z'],['2026-10-07T01:02:03Z','2026-10-07T01:02:03.1234567Z']],
 ['InstantInput',true,['2026-10-07T01:02:03Z','2026-10-07T01:02:03.123456Z','2026-10-07T01:02:03+02:00'],['2026-10-07T01:02:03.1234567Z']]
];
let checked=0;const failed=[];
for(const [name,isCommon,valid,invalid] of sets){const s=(isCommon?common:doc).components.schemas[name];const allowed=new RegExp(s.pattern,'u');const forbidden=s.not?.pattern?new RegExp(s.not.pattern,'u'):null;
 for(const value of valid){for(const suffix of ['', '\n','\r','\u2028','\u2029','\0']){const v=value+suffix;const actual=allowed.test(v)&&!(forbidden?.test(v));checked++;if(actual!==(suffix===''))failed.push({name,value:v,wanted:suffix==='',actual});}}
 for(const value of invalid){checked++;if(allowed.test(value)&&!(forbidden?.test(value)))failed.push({name,value,wanted:false,actual:true});}}
// Explicitly distinguish UTF-16 code units from JSON Schema Unicode characters.
const unicode=[128,129].map(n=>({runes:n,codepoints:[...'😀'.repeat(n)].length,codeunits:'😀'.repeat(n).length,schemaMaxLength:doc.components.schemas.Name.maxLength}));
const result={node:process.version,compiledPatterns:compiled.length,patternCases:checked,unicode,failed};fs.writeFileSync(base+'node-result.json',JSON.stringify(result,null,2)+'\n');console.log(JSON.stringify(result,null,2));process.exit(failed.length?1:0);
