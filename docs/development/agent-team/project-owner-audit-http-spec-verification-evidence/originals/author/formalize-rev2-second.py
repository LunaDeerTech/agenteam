"""Only the root-authorized formal card and new scratch evidence are written."""
from pathlib import Path
import json,hashlib,os,difflib,re,subprocess
P=Path(__file__).parent
ROOT=Path('/workspace/agenteam')
TARGET=ROOT/'docs/development/work-items/d04-project-owner-audit-http.md'
def sha(b):return hashlib.sha256(b).hexdigest()
def dump(name,x):(P/name).write_text(json.dumps(x,ensure_ascii=False,indent=2)+'\n')
assert not TARGET.exists() and not TARGET.is_symlink()
freeze=json.loads((P/'freeze-rev2.json').read_bytes())
for item in freeze['files']+freeze['unchanged_artifacts']:
 assert sha(Path(item['path']).read_bytes())==item['sha256'],item['path']
review=Path('/workspace/scratch/project-owner-audit-http-spec-verification/rev2/result.json')
assert sha(review.read_bytes())=='e2103767a9e8a280a3a8161608d235b052a2d587abf30bd6e245a78a0dcc3064'
inputs=json.loads((P/'inputs-rev2.json').read_bytes())
old=(P/'draft-rev2.md').read_text(); lines=old.splitlines(True)
header='修订：rev2；状态：完整规格独立 STATIC 已通过（rev1 完整审＋rev2 B1 差量），root 已采纳，现正式归位；**尚未授权本卡14技术实施、2文档末件或任何 Go／资源执行**。固定产品基线 `e4b1b89197da0e9027018fdb41f6ab3e04050b9d`（Project 模型凭据完整接受），固定来源与验收指纹见 [§9](#9-固定来源与规格验收定位)。Project 模型配置写入 rev2 完整产品接受后，须由 root 正式移交 account.go 与共同冻结输入再下发实施；这是调度与共享编译输入门槛，不是 Audit 查询功能缺失。\n'
replacements=[(lines[2],header),
 ('`docs/architecture/security-governance/audit.md`','[Audit 业务规则](../../architecture/security-governance/audit.md)'),
 ('`budget-static.py/json`','[静态核算原件与指纹](#9-固定来源与规格验收定位)'),
 ('实施须root另授；当前仅scratch规格。','实施须root另授；当前仅正式规格已接受。')]
oldlast=lines[-1]
newlast='本rev2技术含义与已接受候选相同；正式归位只更新状态、文档链接与自包含固定索引。独立 STATIC 与 root 采纳均是规格结论，尚未执行或验收本卡产品。原建议、rev1、B1首问题与rev2差量原件全部保留，见 [§9](#9-固定来源与规格验收定位)。后续实施仍以页首调度与另行授权为前置，不将文档归位视为实施或资源授权。\n'
replacements.insert(1,(oldlast,newlast))
formal=old
for a,b in replacements:
 assert a in formal,a[:50]
 formal=formal.replace(a,b)
# Technical delta is limited to five documented administrative/link substitutions.
normalized=formal
for a,b in reversed(replacements):normalized=normalized.replace(b,a)
assert normalized==old
append=['\n## 9. 固定来源与规格验收定位\n\n',
 '本卡 §1–8 是完整工程约束，不依赖外部 scratch 才能确定接口、字段、范围、预算与选择器。以下原件绝对路径用于证据定位；若运行环境被回收，以固定提交、逐源 Git blob／SHA-256 与本卡正文重建来源，不能改读活动工作树当已接受输入。scratch 不是产品代码或已运行测试的替代品。\n\n',
 '规格作者为 `usage_verification`，独立审查者为 `next_frontier`；rev1 仅 B1 两个可执行包入口路径需更正，rev2 已以固定 `cmd/agenteam/main.go`、`cmd/agenteam-runner/main.go` 关闭，其余技术语义与14＋2范围不变。root 已采纳完整版本组合 STATIC PASS。\n\n',
 '| 原件 | 绝对定位 | SHA-256 |\n|---|---|---|\n']
artifacts=[('接受的 rev2 候选',P/'draft-rev2.md'),('原 rev1 候选',P/'draft-rev1.md'),('rev1 → rev2 差量',P/'rev1-to-rev2.diff'),('rev2 作者冻结',P/'freeze-rev2.json'),('77项固定来源索引',P/'inputs-rev2.json'),('16路径范围',P/'scope16.json'),('精确选择器来源',P/'compatibility-selectors.json'),('静态核算脚本',P/'budget-static.py'),('静态核算结果',P/'budget-static.json'),('rev1 完整独审',Path('/workspace/scratch/project-owner-audit-http-spec-verification/rev1/result.json')),('rev2 独立差量／完整组合结论',review),('rev2 独立报告',review.parent/'review.md')]
for name,path in artifacts:append.append(f'| {name} | `{path}` | `{sha(path.read_bytes())}` |\n')
append.extend(['\n### 9.1 固定已接受来源\n\n',
 '以下77项均从产品提交 `e4b1b89197da0e9027018fdb41f6ab3e04050b9d` 读取。前40项继承已接受前沿分析，随后35项是本卡必要定点依赖，最后2项是B1路径更正的入口证明；未读取配置写入活动14源。当前链接只提供导航，不把其未来活动字节当作本表接受版本。治理随后如有正式修订遵守现行治理，技术复现仍使用表中固定内容。\n\n',
 '| # | 仓库路径 | 固定 Git blob | SHA-256 |\n|---|---|---|---|\n'])
for i,x in enumerate(inputs['sources'],1):
 path=ROOT/x['path'];assert path.exists(),x['path']
 rel=os.path.relpath(path,TARGET.parent)
 append.append(f'| {i} | [{x["path"]}]({rel}) | `{x["git_blob"]}` | `{x["sha256"]}` |\n')
append.extend(['\n### 9.2 静态工具来源与资源复用边界\n\n',
 '静态 MIME 推导读取 Go1.27.1 的两个标准库源文件，未执行 Go。源码定位与SHA如下；实际实施需重新冻结真实工具与所有执行输入。\n\n',
 '| 定位 | SHA-256 |\n|---|---|\n'])
for x in inputs['static_toolchain_sources']:append.append(f'| `{x["path"]}` | `{x["sha256"]}` |\n')
append.append('\n以下只作为已接受凭据实际图／fixture工具的最小复用入口。不能将原物理资源、PID或旧环境baseline复用于新轮；新卡有效图、实际新输入、候选、命令、空间、镜像、PID/starttime、daemon及7资源基线必须重新闭合。\n\n| 原件定位 | SHA-256 |\n|---|---|\n')
for x in inputs['accepted_reuse_entrypoints']:append.append(f'| `{x["path"]}` | `{x["sha256"]}` |\n')
formal+=''.join(append)
assert formal.endswith('\n') and '\r' not in formal and all(l==l.rstrip() for l in formal.splitlines())
# Open exclusively: no overwrite of any competing writer's file.
with TARGET.open('x',encoding='utf-8',newline='\n') as f:f.write(formal)
(P/'formal-rev2.md').write_text(formal)
(P/'draft-to-formal-rev2.diff').write_text(''.join(difflib.unified_diff(old.splitlines(True),formal.splitlines(True),fromfile='draft-rev2.md',tofile='docs/development/work-items/d04-project-owner-audit-http.md')))
links=[]
for label,target in re.findall(r'\[([^\]]+)\]\(([^\s)]+)\)',formal):
 file,sep,fragment=target.partition('#')
 dest=(TARGET.parent/file).resolve() if file else TARGET
 assert dest.exists(),target
 if fragment:
  assert dest==TARGET and fragment=='9-固定来源与规格验收定位',target
  assert '## 9. 固定来源与规格验收定位' in formal
 links.append({'label':label,'target':target,'resolved':str(dest),'fragment':fragment,'passed':True})
diffcheck=subprocess.run(['git','diff','--no-index','--check','--','/dev/null',str(TARGET)],cwd=ROOT,stdout=subprocess.PIPE,stderr=subprocess.STDOUT)
(P/'formal-diff-check.raw').write_bytes(diffcheck.stdout)
assert diffcheck.returncode==0,(diffcheck.returncode,diffcheck.stdout)
check={'kind':'formal-card-only administrative/link/provenance equivalence selfcheck','passed':True,'same_name_exclusive_creation':True,'source_draft_sha256':sha(old.encode()),'formal_card_sha256':sha(formal.encode()),'section1_to8_technical_equivalence':True,'equivalence_method':'Exact invertible allowlist of header, one architecture link, budget artifact link, specification-state sentence and final status paragraph; appended self-contained source section9','new_business_or_scope_change':False,'relative_links_and_fragments':len(links),'links':links,'fixed_source_entries':77,'technical_paths':14,'documentation_paths':2,'git_diff_no_index_check_exit':diffcheck.returncode,'format':'UTF-8 LF terminal newline no trailing spaces','no_other_repository_file_written':True,'no_product_go_resources_or_git_mutation':True,'governance_documentation_skill_sha256':sha((ROOT/'.agents/skills/agenteam-documentation/SKILL.md').read_bytes())}
dump('formal-rev2-check.json',check)
art=['formal-rev2.md','draft-to-formal-rev2.diff','formalize-rev2.py','formal-rev2-check.json','formal-diff-check.raw']
newfreeze={'kind':'formal Project Owner Audit specification rev2 placement freeze','formal_path':str(TARGET),'formal_sha256':sha(TARGET.read_bytes()),'accepted_draft':{'path':str(P/'draft-rev2.md'),'sha256':sha(old.encode())},'independent_spec_result':{'path':str(review),'sha256':sha(review.read_bytes())},'files':[{'path':str(P/n),'sha256':sha((P/n).read_bytes()),'bytes':(P/n).stat().st_size} for n in art],'status':'author stopped writing; awaiting narrow formal placement independent review','implementation_or_resource_authorization':False}
dump('formal-rev2.freeze.json',newfreeze)
for name in ['formal-rev2.md','draft-to-formal-rev2.diff','formal-rev2-check.json','formal-rev2.freeze.json']:print(name,sha((P/name).read_bytes()))
print('links',len(links),'bytes',len(formal.encode()))
