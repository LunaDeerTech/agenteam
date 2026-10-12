# Agent Runner Mounts 历史材料恢复

原工作树 `/workspace/agenteam-agent-runner-mounts`，最终 tip `e9ddc230febbc984f4571e457843d11aadfea288`；后台正式交付基准为 main `d319a041`。36 个产品及专属技术文件与该主线版本逐字一致；历史两 harness 通过下列补丁恢复，不覆盖主线已合入的其他 profile。

## 原件位置

2026-10-11 将原树 `output/ai/agent-runner-mounts` 整目录以同文件系统、禁止覆盖的 rename 移至 [保全目录](../../output/ai/retired-20261011/agent-runner-mounts/)。原目录 inode、mode、uid、gid、mtime 在迁移时保持；未改原日志、输入 manifest、配方或二进制内容。迁移事实见 [preservation-record.json](../../output/ai/retired-20261011/agent-runner-mounts/preservation-record.json)。

- `/tmp/arm01`、`/tmp/arm02` 原目录保留；小副本分别在保全目录 `original-tmp/arm01`、`original-tmp/arm02`，已核各文件字节、模式与 mtime 相等。
- `checks/` 保留 unit01、stale/Lookup 修复与原 overlay 反例，`compile01/`、`compile02/`、`native01/`、`native02/` 按原目录实际内容保留；原二进制和实际 Wait 记录不另行生成。
- native01 首次 Runner Material 夹具断言失败仍为 FAIL，未进入 Mount 业务；native02 原 PG 两 sub 与资源尾 whole PASS。此后台片不单独证明默认 App 浏览器或物理 Runner 执行。

## 有界源码恢复

基于 main `d319a041` 建立**隔离恢复目录**，按表选择补丁。表中额外补丁是默认补丁之后的不同历史分支，不能依次全部叠加。补丁只恢复列明历史输入，不是产品修复，禁止应用到 main。

| 补丁 | 应用基础 | 恢复用途 |
| --- | --- | --- |
| [retired-inputs.patch](retired-inputs.patch) | main `d319a041` | `c189c9e7` 的 native02 输入：两原 harness，以及历史闭包尚不存在的两 Outbox stop 文件的删除差额。 |
| [retired-inputs-18daab3b.patch](retired-inputs-18daab3b.patch) | 默认补丁之后 | native01 原 Material 断言失败的唯一 PG fixture 差额。 |
| [retired-inputs-6f609a4a.patch](retired-inputs-6f609a4a.patch) | 默认补丁之后 | unit01 的 command store、reader 与当时尚不存在的 Lookup 专属测试差额。 |
| [retired-inputs-5f48159f.patch](retired-inputs-5f48159f.patch) | 默认补丁之后 | Lookup 反例使用的旧 reader；逐字等于原 `checks/lookup-repro-input/reader-before.go`。 |

执行形式为在隔离目录内运行 `patch --batch --fuzz=0 --forward -p1 -i <补丁绝对路径>`。恢复目录和原命令中的绝对路径需由恢复者显式映射；原 `overlay.json`、provenance 与命令记录保持原样，不能把搬迁后的路径冒作原运行路径。

本次四补丁都已在隔离临时目录实际应用，Wait 0、无 fuzz/offset，逐文件等于相应原 Git source；有记录的变更文件 SHA 同时匹配原 manifest。结果与原工具输出在 [restore-checks](../../output/ai/retired-20261011/agent-runner-mounts/restore-checks/)。通过 Git blob 身份比较，native02/native01 的各 1734 个 tracked source 与 unit01 的 1658 个 tracked source 均由命名 main 基准加对应补丁恢复，见 [source-closure-check.json](../../output/ai/retired-20261011/source-closure-check.json)。这不是重新运行原测试，也不把机器缓存、路径或二进制算成 Git 源码。

## 退役边界

原 owner 与当前作者已确认无此旧树 writer、后台或活动输入依赖。Go 热缓存、只读 module/MinIO 均在受保护的其他树，未迁移、删除或改写。本次只保全材料与恢复差额，未执行 Git 写入、工作树/分支删除、Go、native 或网络操作；普通 worktree/分支清理由主线程另行处理。

## 2026-10-12：已关闭编译产物的无损归档

下列退役裸 ELF 已从原保全位置精确移除，完整字节保存在 [archive02/run02](../../output/ai/retired-compile-binaries/archive02/run02/README.md) 的 gzip6/PAX 包中。恢复这些 binary 必须先解归档；原编译/native 记录、输入 maps、源码恢复材料和历史 FAIL/log 保持原样。此前迁移保全的 inode 相等结论仍是当时事实，不代表下列裸路径今天仍存在。

| 包内 member（相对 `output/ai/`） | 字节 | SHA-256 |
| --- | ---: | --- |
| `retired-20261011/agent-runner-mounts/compile01/projectvariable.test` | 80856117 | `5ede1373bac0228931eede420498de3f9f596b3d9f1b69e7a173cc04ae497002` |
| `retired-20261011/agent-runner-mounts/compile02/projectvariable.test` | 80855428 | `aff6898278360c048ae5115950e31465b5cd36d162b3469a00e06fd3213db42c` |

唯一归档为 [history-binaries.tar.gz](../../output/ai/retired-compile-binaries/archive02/run02/history-binaries.tar.gz)，720,004,425 B，SHA-256 `9f3da6012aacd41f3cc49a7e1b7d657ccf601e74368aee6ad52b75918e71a20b`。原 21 文件全集见 [specs](../../output/ai/retired-compile-binaries/archive02/run02/specs.json)；本任务只涉及上表条目。

归档前后全部原文件和编译/native 记录身份相等，解流逐项核对 SHA、size、mode、uid/gid、nsmtime 及 PAX 历史 dev/inode。原归档工具 71444 实际 0；删除前再次完整验证，原工具 79461 实际 0，仅 unlink 固定 21 个裸 ELF，不删除目录。证明见 [归档结果](../../output/ai/retired-compile-binaries/archive02/run02/result.json)、[移除结果](../../output/ai/retired-compile-binaries/removal02/result.json) 与 [精确移除清单](../../output/ai/retired-compile-binaries/removal02/deleted.jsonl)。归档适配首轮 60924 的字段名错误及原失败材料另存父目录，未算作产品或 PG 失败。

恢复时先验证归档摘要，再解到新的空目录，按 manifest/PAX 恢复原字节、权限、uid/gid（有权限时）及精确 nsmtime，并复核 SHA/size。解包会分配新 inode，不能伪造原 inode/ctime；原绝对路径和输入 map 不改写。重新执行需独立源和经授权的新输入绑定，不直接复用旧 namespace。此归档未碰当前 Write Receipt、任何 Command binary、热 cache 或只读 modules，也不证明 ENOSPC 根因或后继运行必然成功。
