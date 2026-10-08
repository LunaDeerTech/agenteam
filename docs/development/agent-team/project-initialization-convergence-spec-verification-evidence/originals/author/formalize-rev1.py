from pathlib import Path
import hashlib,json,os,re,difflib
b=Path('/workspace/scratch/project-create-frontier4089');repo=Path('/workspace/agenteam')
dest=repo/'docs/development/work-items/d08-project-initialization-convergence.md'
assert not dest.exists(), 'authorized new path must not exist'
draft=(b/'draft-rev1.md').read_text();assert hashlib.sha256(draft.encode()).hexdigest()=='34346798c4e526bf56c08cea24c4fe0ede8f59a73cb1fc8c9bbe222479db4f7b'
inputs=json.loads((b/'inputs01.json').read_text());assert len(inputs['sources'])==57
replacements=[]
for match in list(re.finditer(r'\]\((fixed/[^)]+)\)',draft)):
 old=match.group(1);new=os.path.relpath(repo/old.removeprefix('fixed/'),dest.parent)
 if (old,new) not in replacements:replacements.append((old,new))
replacements.extend([
 ('stopped-source-check.json','#停止源同字节核对'),
 ('**active-only 为本草案明确的工程收敛边界，待独立 STATIC 确认，不表述成新用户产品决定。**','**active-only 已经完整规格独立 STATIC 与 root 采纳，属于工程收敛边界，不是新增用户产品决定。**'),
 ('只提议以下 **5 个新技术源 + 1 个后授文档末件**','本卡限定以下 **5 个新技术源 + 1 个后授文档末件**'),
])
body=draft[draft.index('## 1.'):draft.index('## 7.')]
for old,new in replacements:body=body.replace(old,new)
header='''# D08/D10 Project 初始化收敛授权库（rev1）

2026-10-08。状态：**完整规格独立 STATIC PASS，root 已采纳（含 active-only 工程边界）；正式归位末件待核，尚未授权产品实施、Go 或资源。** 固定产品基线 `4089d13128da8680955005d9507c8a7da74af1f1`。本卡归位不证明产品已经实现；原 scratch、独审与固定来源指纹见 §8。

'''
tail='''## 7. 接受状态与后继交接

root 已读并采纳完整独立 STATIC PASS；active-only 与精确事实/错误表已作为工程规格接受，没有新增用户产品问题。正式归位只调整状态与来源定位，末件窄审仍待完成。当前未授权产品实施或执行 Go/真实资源；后续仍由 root 分配五源唯一写权及所需冻结、执行窗口。发现范围外公共依赖或共享源缺口时必须报告，不能默认扩大写权。

未来产品交付五技术源与后授 README 末件，应明确“Project 初始化收敛授权库已验，未被生产 Skills/root 消费”。无默认模型猜测：Meeting Summary 继续由系统管理员统一 initial/update/首标题，项目不复制或覆盖。生产 Resolution、Invocations、D24与完整D08/D10仍未绑定/未完成，ready503与三停止保留。正式规格接受不替代产品实现、作者自测或产品独立验收。

## 8. 固定来源与审查定位

本节仅保存可复核的路径、Git blob 与 SHA-256，不复制源码。产品来源统一固定于 `4089d13128da8680955005d9507c8a7da74af1f1`；后继入口文档变化不回写这些历史指纹。只读按固定 commit:path 可重建源，正式链接用于定位路径，不能以活动工作区字节代替固定输入。

### 规格与独立审查

原件根 `/workspace/scratch/project-create-frontier4089/`：

| 原件 | SHA-256 |
| --- | --- |
| `draft-rev1.md` | `34346798c4e526bf56c08cea24c4fe0ede8f59a73cb1fc8c9bbe222479db4f7b` |
| `inputs01.json`（57 源） | `057d9f57e0e7f2786444fcc685c11cda23557587c7bb9dab7bd45f478176367e` |
| `freeze-rev1.json` | `1edf2e50ddb0e90ceca74e9ff2e21767329d54cb1f8e1f37784ef23c7e288d48` |
| `checks-rev1.json` | `727c29a1c9336f288dd6ca74dc960790186b52fb29bf6faecc03a88ec1fc1cbe` |

独立审查根 `/workspace/scratch/project-initialization-convergence-verification/rev1/`：`review.md` SHA-256 `54a8e0c5d4621ba10608ab7fc4a7a1530b218f8c077785232f2bd5b49d5b954c`；`result.json` SHA-256 `a583df8a99b3a9d492766c0a8cbbbadd6b8f75c52301d5e5ebfc1a0b503d6d61`。结论为完整 scratch rev1 STATIC PASS、无必修，包含 active-only；没有 Go、资源或产品动态执行。后续持久归档由 root 另授，不创建尚不存在的归档链接。

### 停止源同字节核对

回归基线 `42e3f7d59ceb65cb22cdd6b639608caa8830fae2` 与产品基线 `4089d13128da8680955005d9507c8a7da74af1f1` 的下列 blob 相同。原 `stopped-source-check.json` SHA-256 为 `8fac130c6cc3f61388c33e73caf9852ee6277c3b1741549d1d5a2c6310f16639`，位于上述原件根；此次只读核源，没有重跑停止的 regression。

| 路径 | 两基线相同 Git blob |
| --- | --- |
'''
stop=json.loads((b/'stopped-source-check.json').read_text())
for x in stop['unchanged_critical_sources']:
 path=x['path'];link=os.path.relpath(repo/path,dest.parent)
 tail+=f"| [{path}]({link}) | `{x['old_and_current_blob']}` |\n"
tail+='\n### 固定来源\n\n| 路径 | SHA-256 | Git blob |\n| --- | --- | --- |\n'
for v in inputs['sources']:
 path=v['path'];link=os.path.relpath(repo/path,dest.parent)
 assert hashlib.sha256(Path(v['snapshot']).read_bytes()).hexdigest()==v['sha256']
 tail+=f"| [{path}]({link}) | `{v['sha256']}` | `{v['git_blob']}` |\n"
formal=header+body+tail
# Prove exact reverse of every allowed §1–6 edit before writing.
rev=body
for old,new in reversed(replacements):rev=rev.replace(new,old)
assert rev==draft[draft.index('## 1.'):draft.index('## 7.')]
dest.write_text(formal)
(b/'formal-rev1.snapshot.md').write_text(formal)
(b/'scratch-to-formal-rev1.diff').write_text(''.join(difflib.unified_diff(draft.splitlines(True),formal.splitlines(True),fromfile='scratch/draft-rev1.md',tofile='docs/development/work-items/d08-project-initialization-convergence.md')))
(b/'formal-replacements.json').write_text(json.dumps({'scope':'section1-through-section6 reversible source links and administrative state only','replacements':replacements},ensure_ascii=False,indent=2)+'\n')
print('wrote',dest,'bytes',len(formal.encode()),'sha256',hashlib.sha256(formal.encode()).hexdigest())
