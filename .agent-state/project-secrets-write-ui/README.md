# Project Secret Write UI 早期失败输入恢复

用途仅为解释和恢复本片原失败；不是新的通过记录，也不批准自动重跑或恢复 STOP。正式边界见[任务卡](../../docs/development/work-items/d10-project-secrets-write-ui.md)。

## 可精确恢复的产品源码

`early-product-inputs.patch` 以组合修后 `e1e09454f49e99c6b823352178685865c7b32e60` 为应用基线，仅把以下四个文件恢复到首轮反例时的 `8c08c570b354657c9ed539203a9d817882e7fe0d` 原字节：

- `web/src/composables/useSession.ts`
- `web/src/composables/useProjectSecretEditor.ts`
- `web/src/composables/useProjectSecrets.ts`
- `web/src/components/projects/ProjectSecretEditor.vue`

在私有恢复 checkout 中应用：

```sh
patch --batch --fuzz=0 -p1 < .agent-state/project-secrets-write-ui/early-product-inputs.patch
```

已在私有临时目录实际应用，退出 0，四文件逐字等于旧 Git 对象。该历史 Session 会同时撤下后来合入的 Knowledge 删除增量，以恢复首轮实际文件；不要将恢复结果覆盖当前开发或生产输入。基线后若这四文件变动，应先核限定差额，不使用 fuzz 强套。

只研究 checking 的发布顺序时，可仅恢复上述 `useProjectSecrets.ts`：旧文件在这处先发布 reactive scope、后写 route。其单文件恢复也已实际应用并逐字比对；搭配当前其余源码只构成定向行为复现，不能称 checking01–03 全部旧 WIP 原字节。

## 原件与无法补造的范围

原命令、日志、PID/实际 Wait 保留于 `output/ai/project-secrets-write-ui/checks/`：`review-repro01` 为 3 FAIL/1 PASS；`unit01` 为 42 PASS/1 FAIL；`checking01`–`checking03` 为恢复失败的后续观测。原 WIP 测试与部分中间产品编辑没有逐字快照或逐项 SHA；最终测试源经过格式化和诊断收敛，不能反推后宣称还原那些运行的全部输入。不要改写原 FAIL。

`build02` 还保留一个页面测试 helper 类型错误。以本文件基线的 `web/src/tests/project-secret-write-page.spec.ts` 为准，将唯一 `function button(w: Pick<VueWrapper, 'findAll'>, label: string)` 改回 `function button(w: VueWrapper, label: string)`，已在内存验证所得整个文件逐字等于 `0661f376`；这不改变产品或断言。修后 `build03` 与 `combined01` 的原结果另存，不覆盖该失败。

其余最终方法控制由本目录受跟踪的 `method-controls.mjs` 恢复；方法变更及原执行范围仍以各自 invocation/result 为准，不把后增 paste 联证或组合控制倒算成早期 68 项已覆盖。依赖、构建资产及原运行输出保持 ignored，仅保留原件；不把材料或请求摘要写入恢复文件。

## 首次浏览器定位失败

`native01-inputs.patch` 仅将 `tests/account-captcha-web/e2e/project-secret-write.spec.ts` 从 `c964e62d39cf1863395b914776feef706dd0a971` 恢复到 `e764c08155af31627159c09f2d3450d77363d9cf` 的原定位写法。已在私有目录实际 `patch --batch --fuzz=0 -p1`，退出 0，整个文件逐字等于旧 Git 对象，SHA256 `4fb15a414346505a0990778098cfd97254bacb941195ddef59937191cdb4b3dc` 与原 native01 输入记录一致。其余产品、Go、observer 和原 dist 不变；无需恢复整树。

原 native01 是完整退出的 wholeFAIL：normal 停在空目录可见性断言；lost 已过 seed 详情，停于 required 材料字段的 exact label 焦点定位。没有发生写入/Lookup，没有失败截图或 raw locator count，不补造。原件在 `output/ai/project-secrets-write-ui/native01/`，后续定向 DOM 反例和修后结果在 checks；保留原结果，不用后续成功覆盖。

## 正式交付与本机保全

本片已随 main `22d0a0b6` 正式有限交付；native02 的 22 组联证、4 张图和完整退出门已独立接受，原 native01 及基础检查 FAIL 均保留。旧 task-current 不覆盖 main 全局索引。

原 `output/ai/project-secrets-write-ui/` 已同文件系统无覆盖迁至 main 的 `output/ai/retired-20261011/project-secrets-write-ui/`，其原有相对结构、inode 和内容保留。下列补充材料在该目录内：

- `retained-inputs/output/ai/knowledge-delete-ui/method-secret-merge01/`：实际组合控制的原输出，不漏收第二个输出根。
- `retained-inputs/web/dist/`：最后构建的 84 个物理资产；原 `dist-02/` 与早期构建资产仍在完整任务输出内。
- `retention/tmp/psw-406773/` 和 `retention/tmp/psw-064a37/`：各轮实际命名的 log、owned.json、request.json 逐字副本；原 `/tmp` 未移动或改写。精确来源、目标及核对结果见 `retention/migration.json`。

保全只调整本机位置，不重写原 invocation 中的绝对路径或原 FAIL。root 已完成原 worktree 普通移除；本地分支仅因 squash 非祖先被 `-d` 拒绝后，针对已交分支窄 `-D`。远端以最终 tip `7a6d683b691a836a477945a0422d208a8526f514` 的精确 lease 删除成功，成功的 fresh ls-remote 为空、explicit prune 实际退出0，worktree 列表已无原树。共享 SkillHTTP cache 与 SkillsMilestoneResume modules 未动。跨设备先恢复正式 main 源码与本目录必要差额，再按锁文件恢复依赖；本机 ignored 输出不等于已上传远端。

## 2026-10-12：已关闭编译产物的无损归档

下列退役裸 ELF 已从原保全位置精确移除，完整字节保存在 [archive02/run02](../../output/ai/retired-compile-binaries/archive02/run02/README.md) 的 gzip6/PAX 包中。恢复这些 binary 必须先解归档；原编译/native 记录、输入 maps、源码恢复材料和历史 FAIL/log 保持原样。此前迁移保全的 inode 相等结论仍是当时事实，不代表下列裸路径今天仍存在。

| 包内 member（相对 `output/ai/`） | 字节 | SHA-256 |
| --- | ---: | --- |
| `retired-20261011/project-secrets-write-ui/compile-app01/project-secret-write-app-race.test` | 77858588 | `8d90b1e3af6ce4396b80fa21355f97b7d675ff0af0354dccab1f4e7251b17347` |

唯一归档为 [history-binaries.tar.gz](../../output/ai/retired-compile-binaries/archive02/run02/history-binaries.tar.gz)，720,004,425 B，SHA-256 `9f3da6012aacd41f3cc49a7e1b7d657ccf601e74368aee6ad52b75918e71a20b`。原 21 文件全集见 [specs](../../output/ai/retired-compile-binaries/archive02/run02/specs.json)；本任务只涉及上表条目。

归档前后全部原文件和编译/native 记录身份相等，解流逐项核对 SHA、size、mode、uid/gid、nsmtime 及 PAX 历史 dev/inode。原归档工具 71444 实际 0；删除前再次完整验证，原工具 79461 实际 0，仅 unlink 固定 21 个裸 ELF，不删除目录。证明见 [归档结果](../../output/ai/retired-compile-binaries/archive02/run02/result.json)、[移除结果](../../output/ai/retired-compile-binaries/removal02/result.json) 与 [精确移除清单](../../output/ai/retired-compile-binaries/removal02/deleted.jsonl)。归档适配首轮 60924 的字段名错误及原失败材料另存父目录，未算作产品或 PG 失败。

恢复时先验证归档摘要，再解到新的空目录，按 manifest/PAX 恢复原字节、权限、uid/gid（有权限时）及精确 nsmtime，并复核 SHA/size。解包会分配新 inode，不能伪造原 inode/ctime；原绝对路径和输入 map 不改写。重新执行需独立源和经授权的新输入绑定，不直接复用旧 namespace。此归档未碰当前 Write Receipt、任何 Command binary、热 cache 或只读 modules，也不证明 ENOSPC 根因或后继运行必然成功。
