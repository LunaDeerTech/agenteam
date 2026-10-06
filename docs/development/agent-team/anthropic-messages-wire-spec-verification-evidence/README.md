# Anthropic Messages 规格证据

本目录支持[规格采纳报告](../anthropic-messages-wire-spec-verification.md)，只记录 **STATIC PASS**，不授予实施或产品执行权限。原始 Markdown/Python/SDK 文件加 `.txt` 后缀作为证据保存，字节未修改；历史绝对路径只是来源标识。

## 离线复核

在仓库根目录运行：

```sh
python3 docs/development/agent-team/anthropic-messages-wire-spec-verification-evidence/verify-evidence.py
```

脚本只读本目录并计算 JSON、SHA、Git 对象身份和差量；不需要 `.git`、原 scratch、网络或额外包，不启动子进程，不导入 SDK，不运行 Go/Docker/产品测试。输出证明归档完整性，不重新证明规格语义或任何运行行为。[本次命令记录](archive-checks.json)保存实际 argv、时间、退出码及 stdout/stderr 指纹。

## 原件定位

| 归档 | 内容 |
| --- | --- |
| [archive-manifest.json](archive-manifest.json) | 精确原件 SHA/长度、27 个 Git locator、交付身份；`origin_map` 将历史绝对路径映射到当前归档。独立重复的规格副本复用同字节作者原件，九条映射全部核对 |
| [official/LICENSE.txt](official/LICENSE.txt) 与 `official/` | 官方 SDK 66 个完整选定文件，共 297,810 bytes，包含完整 MIT 许可；没有 SDK 安装、缓存或 bare 仓库 |
| [author/source-manifest.json](author/source-manifest.json) | 固定官方仓库、commit、URL、Git blob、SHA256、字节与获取时间；[原公开获取命令](author/fetch-commands.json)及 stdout/stderr 一并保存 |
| [git-proofs.json](git-proofs.json) | 4 个 commit 原对象和 44 个必要祖先 tree 对象的 base64；只证明所选文件的 commit/path/blob 关系，不复制其余文件内容 |
| [author/baseline-inputs.json](author/baseline-inputs.json) 与 `git-inputs/` | 接受基线 25 文件、状态基线 2 文件的原字节；当前活动 tools 实现未作输入 |
| [author/field-manifest.json](author/field-manifest.json) | 9 组来源事实、75 个原行定位；SDK 字段、官方 fixture 与项目保守规则分别解释 |
| `author/spec-freeze01/` 至 `author/spec-freeze03/`、`author/spec-adopted01/` | 三个规格输入、两次窄修及最后页首采纳差量；每版 manifest/checks 原字节。采纳稿技术 §1–11 与 freeze03 一致 |
| [independent/review.md.txt](independent/review.md.txt)、[final-checks.json](independent/final-checks.json)、[evidence-index.json](independent/evidence-index.json) | 独立最终报告、修订链与证据索引；原静审命令/结果/脚本以不可执行文本保留 |

143 个原件总计 1,145,669 bytes。新增离线脚本与路径证明属于本次归档核对，不冒充独立者原命令。独立首个元数据检查因误要求 LICENSE 含字面标题而 exit1，修正检查器后 exit0；原官方文件从未修改，首失败记录与旧检查器保留。F1/F2 是规格静态歧义，不是动态产品红例。

原文内 `/workspace/scratch/...` 与原相对链接保留以维持指纹；查阅时使用 manifest 的映射，不按历史路径运行旧脚本。当前主卡已有 Git 交付，但本归档自身提交由这组文件的 Git 历史定位。

[文档检查记录](document-checks.json)分别记录新写文档的链接/格式通过与原件格式例外：官方 commit 的签名空白/无末尾换行、三份 unified diff 的空白上下文行、四份官方 SSE fixture 的无末尾换行均原样保留。不能为消除 whitespace 提示改写这些原件。
