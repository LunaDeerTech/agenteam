# Execution Mount Capture 历史材料恢复

原工作树 `/workspace/agenteam-execution-mount-capture`，最终 tip `f55f53fd53b010a0302ea469dc6b87e2f1233bb5`；正式交付基准为 main `562d3538`。20 个产品及专属技术文件逐字一致；主线两 harness 还保有已交 AgentRunner UI profile，历史恢复只在隔离目录进行。

## 原件位置

2026-10-11 将原树 `output/ai/execution-mount-capture` 整目录以同文件系统、禁止覆盖的 rename 移至 [保全目录](../../output/ai/retired-20261011/execution-mount-capture/)，迁移时保持 inode、mode、uid、gid、mtime。现有 main `output/ai/execution-mount-capture/merge-method01.json` 未覆盖、未合并。迁移事实见 [preservation-record.json](../../output/ai/retired-20261011/execution-mount-capture/preservation-record.json)。

`/tmp/emc01` 原目录保留，小副本在保全目录 `original-tmp/emc01`；逐文件字节、模式、mtime 相等。整个 checks、原 unit01 FAIL、unit02、App compile、PG compile/list、native01 的输入/二进制/实际 Wait/资源尾及原 `run-check.py`、`run-check02.py`、`run-compile.py`、`prepare-native.py` 都随目录保存，不重造记录。

native01 原 1 top/2 sub 及全资源尾 whole PASS 保持。Unknown 场景是业务已实际 committed 后的受控返回丢失，不是物理 PG Unknown；本片不声称默认 App、物理 workspace 或 executor 运行已验。

## 有界源码恢复

从 main `562d3538` 建立**隔离恢复目录**，选择下表；禁止将历史补丁应用 main。

| 补丁 | 应用基础 | 用途 |
| --- | --- | --- |
| [retired-inputs.patch](retired-inputs.patch) | main `562d3538` | 还原 `f55f53fd` 的两个原 harness，并删除原 PG 同包闭包中尚不存在的 AgentRunner UI Go fixture，恢复原 72 个同包 Go 输入范围。 |
| [retired-inputs-806e0a68.patch](retired-inputs-806e0a68.patch) | 默认补丁之后 | 原 unit01 FAIL 的三个 preparation/runner 测试差额。 |

在隔离目录实际使用 `patch --batch --fuzz=0 --forward -p1 -i <补丁绝对路径>`，两补丁均 Wait 0、无 fuzz/offset，恢复文件逐字等历史 Git source；有原 manifest 的变更文件 SHA 同时匹配。结果见 [restore-checks](../../output/ai/retired-20261011/execution-mount-capture/restore-checks/)。原 native01 的 1744 个 tracked source 与 unit01 的 1679 个 tracked source 已用 Git blob 身份核对恢复，无全树重新 hash，见 [source-closure-check.json](../../output/ai/retired-20261011/source-closure-check.json)。

现有 `profile-controls.py` 未改。该控制记录中的历史 BASE `806e0a68` 两 harness 字节等于可达 main `33591391` 对应文件，别名已记入上述 source-closure 原件，无需额外重复保存整份 harness。原控制/配方内的旧 ref 和绝对路径保持历史值；恢复者须在隔离环境明确映射，不声称搬迁后原命令可直接运行。

## 退役边界

Runtime 与当前作者确认只从 main/新任务树工作，无旧树 source/output/binary/deps 活动依赖，也无旧 writer 或后台。热 Go cache、只读 module/MinIO 位于受保护的其他树，本次未动。这里只保全现存原件及必要历史差额，未重新执行 Go/测试/native/网络，未写 Git 或删除工作树/分支；最终清理由主线程另行处理。

## 2026-10-12：已关闭编译产物的无损归档

下列退役裸 ELF 已从原保全位置精确移除，完整字节保存在 [archive02/run02](../../output/ai/retired-compile-binaries/archive02/run02/README.md) 的 gzip6/PAX 包中。恢复这些 binary 必须先解归档；原编译/native 记录、输入 maps、源码恢复材料和历史 FAIL/log 保持原样。此前迁移保全的 inode 相等结论仍是当时事实，不代表下列裸路径今天仍存在。

| 包内 member（相对 `output/ai/`） | 字节 | SHA-256 |
| --- | ---: | --- |
| `retired-20261011/execution-mount-capture/compile01/projectvariable.test` | 81106213 | `fa55ed6c1df64e27cdb4c1431b0fc57c7fa7bcd765024e09d164af7aaa4e6b60` |

唯一归档为 [history-binaries.tar.gz](../../output/ai/retired-compile-binaries/archive02/run02/history-binaries.tar.gz)，720,004,425 B，SHA-256 `9f3da6012aacd41f3cc49a7e1b7d657ccf601e74368aee6ad52b75918e71a20b`。原 21 文件全集见 [specs](../../output/ai/retired-compile-binaries/archive02/run02/specs.json)；本任务只涉及上表条目。

归档前后全部原文件和编译/native 记录身份相等，解流逐项核对 SHA、size、mode、uid/gid、nsmtime 及 PAX 历史 dev/inode。原归档工具 71444 实际 0；删除前再次完整验证，原工具 79461 实际 0，仅 unlink 固定 21 个裸 ELF，不删除目录。证明见 [归档结果](../../output/ai/retired-compile-binaries/archive02/run02/result.json)、[移除结果](../../output/ai/retired-compile-binaries/removal02/result.json) 与 [精确移除清单](../../output/ai/retired-compile-binaries/removal02/deleted.jsonl)。归档适配首轮 60924 的字段名错误及原失败材料另存父目录，未算作产品或 PG 失败。

恢复时先验证归档摘要，再解到新的空目录，按 manifest/PAX 恢复原字节、权限、uid/gid（有权限时）及精确 nsmtime，并复核 SHA/size。解包会分配新 inode，不能伪造原 inode/ctime；原绝对路径和输入 map 不改写。重新执行需独立源和经授权的新输入绑定，不直接复用旧 namespace。此归档未碰当前 Write Receipt、任何 Command binary、热 cache 或只读 modules，也不证明 ENOSPC 根因或后继运行必然成功。
