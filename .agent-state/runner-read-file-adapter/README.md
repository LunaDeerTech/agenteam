# read-file 首次编译失败的最小恢复材料

`unit01-recovery.patch` 将已交付的修复版两个产品 Go 文件恢复为首次 unit01 的实际输入。它是 863a162e → 311d7777 的有限反向差额，旧字节已逐项核对原 `unit01-inputs-before.json`。只在独立恢复快照中应用；它会重新引入已解决的 `mount/contract → agent/contract → mount/contract` 循环引用，不用于当前产品。

原 unit01 执行六包定向 race：工具 10167 actual1，Go 568623 /24.308s，62 个相关输入前后不变；Central 三包 setup FAIL，Runner 三包 12 top PASS。原命令、环境、前后输入、日志、进程与实际退出保存在任务证据的 `checks/unit01-*`、`checks/unit01.log` 和 `checks/basic01-tool-terminal.json`；以后整体保全到 retired 目录时保持这些相对路径。

单独 PG fixture 的对应 helper 也在后续修复中改动，但不属于 unit01 编译/运行输入，恢复这个实际失败无需携带其旧版本。原失败尚未运行 vet 或 PG；该补丁不补造任何未发生的动态结果。

同目录 `profile-controls.py` 是原冻结 selector 的历史方法控制，基于 7a0e817d 的旧 harness；主线后来新增其他合法 profile 时，不用其旧完整 AST 比较冒充当前组合检查。当前组合以本次主线 integration 原件与实际保留的字典并集为准。

## 2026-10-12：已关闭编译产物的无损归档

下列退役裸 ELF 已从原保全位置精确移除，完整字节保存在 [archive02/run02](../../output/ai/retired-compile-binaries/archive02/run02/README.md) 的 gzip6/PAX 包中。恢复这些 binary 必须先解归档；原编译/native 记录、输入 maps、源码恢复材料和历史 FAIL/log 保持原样。此前迁移保全的 inode 相等结论仍是当时事实，不代表下列裸路径今天仍存在。

| 包内 member（相对 `output/ai/`） | 字节 | SHA-256 |
| --- | ---: | --- |
| `retired-20261011/runner-read-file-adapter/compile01/projectvariable.test` | 82376749 | `9d3b0bae455cce17ff1675be839acee2caab35b9d3077c590af357703939bafd` |

唯一归档为 [history-binaries.tar.gz](../../output/ai/retired-compile-binaries/archive02/run02/history-binaries.tar.gz)，720,004,425 B，SHA-256 `9f3da6012aacd41f3cc49a7e1b7d657ccf601e74368aee6ad52b75918e71a20b`。原 21 文件全集见 [specs](../../output/ai/retired-compile-binaries/archive02/run02/specs.json)；本任务只涉及上表条目。

归档前后全部原文件和编译/native 记录身份相等，解流逐项核对 SHA、size、mode、uid/gid、nsmtime 及 PAX 历史 dev/inode。原归档工具 71444 实际 0；删除前再次完整验证，原工具 79461 实际 0，仅 unlink 固定 21 个裸 ELF，不删除目录。证明见 [归档结果](../../output/ai/retired-compile-binaries/archive02/run02/result.json)、[移除结果](../../output/ai/retired-compile-binaries/removal02/result.json) 与 [精确移除清单](../../output/ai/retired-compile-binaries/removal02/deleted.jsonl)。归档适配首轮 60924 的字段名错误及原失败材料另存父目录，未算作产品或 PG 失败。

恢复时先验证归档摘要，再解到新的空目录，按 manifest/PAX 恢复原字节、权限、uid/gid（有权限时）及精确 nsmtime，并复核 SHA/size。解包会分配新 inode，不能伪造原 inode/ctime；原绝对路径和输入 map 不改写。重新执行需独立源和经授权的新输入绑定，不直接复用旧 namespace。此归档未碰当前 Write Receipt、任何 Command binary、热 cache 或只读 modules，也不证明 ENOSPC 根因或后继运行必然成功。
