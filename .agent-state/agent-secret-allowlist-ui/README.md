# Agent Secret allowlist 历史反例源码恢复

本目录仅保留两个已实际失败阶段所需的最小源码差额。起点为已接受产品 `a52610da` 的对应文件；本片合入 main 后可直接使用同字节文件。无需保留旧功能分支，也不恢复整个历史 checkout。先在隔离副本核对下列起点 SHA，再从副本根执行对应命令；不要在当前产品上应用。

## 原控制输入 controls01

```sh
patch --batch --fuzz=0 -p1 < .agent-state/agent-secret-allowlist-ui/controls01.patch
```

该补丁只把 `method-controls.mjs` 的末项合成 SystemRunner fixture 改回错误的 `enrollment.token` 形状。修后正常控制源本身已受跟踪；补丁恢复结果逐字等于原输出保存的 `methods/controls01-original.mjs`。原执行是经 stdin 运行 Node，文件是当时 stdin 源码的保全件，不虚称历史上曾执行此文件。原整组 actual1 保留，不能把其前面已执行的控制改记为整组通过。

控制还依赖本片原 shared helper（包含 SystemRunner6d 的显式 opt-in）及同 lock 的 harness 依赖。恢复这一个文件不证明未来 helper 变化后的重跑等价；无需为材料保全再次执行控制。

## 实际 checking 恢复缺陷

```sh
patch --batch --fuzz=0 -p1 < .agent-state/agent-secret-allowlist-ui/checking-repro01.patch
```

该补丁只将 `knownSecretIDs` 从响应式 Set 改回普通 Set，恢复结果逐字等于 `38192117:web/src/composables/useProjectAgents.ts`。原 `checking-repro01` actual1（4.739s）证明同 Session checking 后保留选择、恢复再修改 Runner 并成功重读目录时，校验消息仍缓存“请重新读取”。现已受跟踪的 `web/src/tests/agent-secret-allowlist-page.spec.ts` 保留当时同一反例，其在 `38192117` 与 `a52610da` 字节相同；不需另存旧测试文件。后续原 page03 九项、build02、binding02 与 native01 成功不覆盖该 FAIL。

两个补丁对应不同历史阶段，分别应用；它们不是某一时点完整源码快照。恢复制作时均在独立私有临时目录实际 `patch --fuzz=0` 返回 0，再逐字比较上述原件；没有运行 Go、Node 或产品测试。

## 输入身份

- `controls01.patch` → `.agent-state/agent-secret-allowlist-ui/method-controls.mjs`
  - 起点 SHA256：`c9465c03c7a0c742a7eec7bea6b88bb069d5ce6bcfe7229c4e38fde04ae64342`
  - 恢复 SHA256：`ba50556df5d95bf00bb0a5efde367fddeecf8d6d8b82b15f5d06a293ba27a241`（4868 bytes）
- `checking-repro01.patch` → `web/src/composables/useProjectAgents.ts`
  - 起点 SHA256：`d9092b0ad7d4228dc81144bff6d1f65ca014e74986e836757ea7bd9631061f51`
  - 恢复 SHA256：`e5f2f39898e398ec7fca32f5c4343a06bd0c245403075ccd1f42ccf3517e42ca`（40610 bytes）

## unit01 与原件边界

unit01 的整轮实际 34 PASS/1 FAIL 来自新页 fixture 使用“描述”而现有编辑标签是“Agent 描述”。当时 invocation 仅记录提交基准及未提交 fixture 调整，未记录逐项源码 SHA，也没有完整原源码字节快照。因此不制作声称还原 unit01 全输入的补丁；从后来文件反改一个标签只能构造相同错误，不能当作恢复了历史字节。此限制与原 FAIL 一并保留。

完整本地原件目前仍在工作树 `output/ai/agent-secret-allowlist-ui/`：checks 中原 unit01/checking-repro01 与修后结果、methods 中原 controls01 源/观察结果及 controls02、两代 dist、compile-web01 和 native01 全证据。原监督日志为 `/tmp/asaw01/ui-206e564776804426.log`。本步骤尚未迁移输出或依赖，绝对路径记录不改写；正式退役时由 root 安排完整原件保全及实际恢复位置说明。静态 PW 定位修正没有动态 FAIL，不保存无须接续的 WIP；不复制全树或创建 archive 分支。

## 2026-10-12：已关闭编译产物的无损归档

下列退役裸 ELF 已从原保全位置精确移除，完整字节保存在 [archive02/run02](../../output/ai/retired-compile-binaries/archive02/run02/README.md) 的 gzip6/PAX 包中。恢复这些 binary 必须先解归档；原编译/native 记录、输入 maps、源码恢复材料和历史 FAIL/log 保持原样。此前迁移保全的 inode 相等结论仍是当时事实，不代表下列裸路径今天仍存在。

| 包内 member（相对 `output/ai/`） | 字节 | SHA-256 |
| --- | ---: | --- |
| `retired-20261011/agent-secret-allowlist-ui/compile-web01/projectvariable.test` | 81662502 | `9d064e0639c83aa84a4260de550fda6962f78d9b1cfb5e8789687c00a24a0213` |

唯一归档为 [history-binaries.tar.gz](../../output/ai/retired-compile-binaries/archive02/run02/history-binaries.tar.gz)，720,004,425 B，SHA-256 `9f3da6012aacd41f3cc49a7e1b7d657ccf601e74368aee6ad52b75918e71a20b`。原 21 文件全集见 [specs](../../output/ai/retired-compile-binaries/archive02/run02/specs.json)；本任务只涉及上表条目。

归档前后全部原文件和编译/native 记录身份相等，解流逐项核对 SHA、size、mode、uid/gid、nsmtime 及 PAX 历史 dev/inode。原归档工具 71444 实际 0；删除前再次完整验证，原工具 79461 实际 0，仅 unlink 固定 21 个裸 ELF，不删除目录。证明见 [归档结果](../../output/ai/retired-compile-binaries/archive02/run02/result.json)、[移除结果](../../output/ai/retired-compile-binaries/removal02/result.json) 与 [精确移除清单](../../output/ai/retired-compile-binaries/removal02/deleted.jsonl)。归档适配首轮 60924 的字段名错误及原失败材料另存父目录，未算作产品或 PG 失败。

恢复时先验证归档摘要，再解到新的空目录，按 manifest/PAX 恢复原字节、权限、uid/gid（有权限时）及精确 nsmtime，并复核 SHA/size。解包会分配新 inode，不能伪造原 inode/ctime；原绝对路径和输入 map 不改写。重新执行需独立源和经授权的新输入绑定，不直接复用旧 namespace。此归档未碰当前 Write Receipt、任何 Command binary、热 cache 或只读 modules，也不证明 ENOSPC 根因或后继运行必然成功。
