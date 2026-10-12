# Runner write-file Operation 方法

本目录保存已获独立验收的显式 write-file Operation 首链方法。基础检查及真实 PG/认证 TLS/WSS 的 `native01` 已通过；完整开发历史已合入 main `fff1f829`，包含该历史的 main `6a27bc3d` 已由 root 推送成功（原 1815 实际 0 / `0d33eb`）。本片使用受控 Model JSONCaller，Project 沿原持久 test Skills receipt；Owner、Registry、Preparation、Execution、Tool、Mount 与 Runner/Manager 走正式调用，不将这些测试边界称为真实 Model tools HTTP 或生产 Skills 完成。生产默认注册、后续 Transcript 消费及下一轮 Model 不在范围内。

精确入口 `TestWriteFileOperation`，两个场景：

- `committed-attempt-write-and-lookup`：真实 Attempt COMMIT 后 hold 原返回，零 wire；原返回后同持久 RequestID 仅一次 WSS 写入，512 KiB 独立分块读取完整比对；最终真实 COMMIT 后丢回执，仅原 Lookup 恢复，不再 Execute。
- `revoke-before-claim-no-dispatch`：Attempt 原返回后，在 Claim 原事务开始及取锁前暂停；普通 Owner 正式撤销 Mount，原旧票零 wire，原文件不变，Attempt 以 `not_dispatched` 退休。

`profile-controls.py` 只检查新 profile 和旧 AST/有效映射保留，以及真实原 gate 的正反行为；不启动资源。`run-pg-checks.py compile --output <fresh>` 使用原完整输入枚举、离线 max1/-p1/race/integration-c 和精确 list；`prepare --compile <passed> --output <fresh> --namespace <fresh-absolute>` 只生成原 supervisor 配方。生成目录中的 `record-native.py` 仅在 root 授独占窗口后运行一次，亲收外层 actual Wait；原七资源、全部尾门和 75 秒 HOST_TCP delta 双空规则原样保留。失败原件不覆盖，不自动重试。

Execution/Loop 的显式 write 构造已接入。`run-basic-checks.py` 只选 30 个新 top 和 14 个本轮受影响旧 top（7 个测试包），另 vet 8 个实际包；沿原 `go list -deps -test -json` 实际源码/测试/embed 闭包及每阶段前后稳定记录，不运行 PG/native。基础与 PG 均须 root 另授窗口后执行，未将源码完整记为通过。root 已将全部已提交冻结的同一分支从临时源码树迁到 `/workspace/agenteam-runner-write-file-operation`，基础首次执行已在此合规工作树完成；原 Identity 逐祖先 `0022` 拒绝门、driver 自动 cwd 及环境覆盖保持。旧 read-file helper、已验 Runner write WSS/local 场景不改、不重复扩大矩阵。全部 Git 由 root 保存。

`methods01` 的原 profile 1 正例、7 反例及旧 AST/有效映射保留检查实际通过。`basic01` 在 Tool runtime 测试的未使用 import 处编译失败，该包 10 个 top 未运行；其余 6 包 34 个 top、109 个 sub 实际通过，无业务测试失败，8 包 vet 当轮未启动。原整轮 FAIL、日志与输入记录保留。默认 `--scope full` 仍选择全部 44 个 top；修复后 `basic02 --scope runtime-followup` 仅补原 runtime 10 个 top、36 个 sub 与原全部 8 包 vet，实际全 0。两轮各自 829 项输入前后相同，轮间仅获授权的脚本选项和 unused import 修复改变；复用其余六包结果，合计 44 top / 145 sub 分阶段 PASS、8 包 vet PASS，不将 `basic01` 改称通过。

`compile01` 的原 race/integration 编译和精确 list 实际退出 0；技术输入 `c1530e95`，1,928 项编译输入 before/after/after-list 相同，包含 85 个同包 Go 文件与迁移69。运行闭包仅增加实际 binary 为 1,929 项；binary SHA256 `9c24f9c94bf99911652789d55351f5db571650ad793f40a024ba14978a71b171`、88,806,596 B。原工具 94027 完整退出 0，未用 list 代替业务执行。

`native01` 原工具 34580 实际退出 0：Go 858887 与 driver 857092 原 Wait 0，supervisor 857048 原 Wait 0 / 165.703 s。`TestWriteFileOperation` 两 sub 全 PASS（top 68.27 s，分别 36.08 s / 32.19 s）；首条真实 Attempt COMMIT hold-return 期间零发送，原返回后同 RID 唯一 WSS 写入，512 KiB 独立完整读取，最终真实 COMMIT 丢回执后仅 Lookup 恢复；第二条正式 Owner 撤销 Mount 后原票零发送、原文件不变、`not_dispatched` 退休。原七资源十四次精确 absence、descendants/private/runtime 双空、自动 HOST_TCP 75 秒门内差额双空及 1,929 项输入不变全部通过，唯一主审已接受该有限 whole 结果。新阶段没有后继 Model 或 Transcript 消费，readback 仅为测试后验，不参与产品提交状态判定。

完整 output 已于保全工具 94730 actual 0（PID 862381 / 1.702 s）用同文件系统 `NOREPLACE` 迁至 `/workspace/agenteam/output/ai/retired-20261012/runner-write-file-operation/`：95 个原文件、97,809,643 B、15 个目录（含根）的 path/SHA/size/dev/inode/mode/uid/gid/nsmtime 全部保持，设备号 27；原 `basic01` FAIL、后继结果与实际 binary 均在。旁目录 `runner-write-file-operation-preservation/` 的 `output-before.json`、`output-after.json`、`result.json`、`tool-terminal.json` 保存迁移证明；`source-history.json` 将 25 份 map 的 17,934 个仓库项、1,931 个唯一 blob 逐 SHA 对应到 `79d88efc` / `c1530e95`。正式合并后，[main 历史补证](../../output/ai/retired-20261012/runner-write-file-operation-preservation/main-history-supplement.json)已核这两个输入提交与最终任务提交 `98083527` 均为 `fff1f829` 祖先；2,760 个历史 ref/path 绑定及全部 1,931 个 blob 从 main 可达历史读取，SHA 与原 map 相同，原补核 `ec1fd3` 实际 0。原迁移记录与输入 map 未改写。

原 `/tmp/rwfo01/pg-cc48de80e47f4502a2625b69f80ad9db.log` 及同名目录的 `owned.json`、`request.json`、`go-config/go/telemetry/mode` 共四文件、7,292 B 已 exclusive 复制到上述证明目录的 `external/rwfo01/`，保留原相对结构，原 tmp 未改；核验见 `external-copies.json`。`process-audit.json` 只证明定向可见范围内 17 个原已知 PID 缺席，不冒称全主机扫描。迁后原工作树 untracked/ignored 为零，后核工具 2c8a36 actual 0 见 `post-preservation-check.json`；工具链、热 Go cache 与只读 modules 未迁移。原 invocation 的历史绝对路径不改写，未来执行须从源码和现存依赖生成 fresh 配方。

root 在正式 main 推送成功后，已普通移除原 `/workspace/agenteam-runner-write-file-operation` 工作树及本地 `ai/runner-write-file-operation-20261012` 分支，两步实际 0（`bfcaa3`）。远端 fresh 查询 `cb992a` 核定 tip 为 `98083527258e0478f83694c90f228f7f9c36b208`，随后 atomic exact-lease 删除原工具 11874 实际 0（`d6485e`）；原工具 16888 实际 0（`6e2531`）确认 fresh 远端为空、fetch/prune 成功及对应 tracking 引用为空。本片正式交付、原件保全和树/本地/远端清理均已完成；上述旧路径只用于恢复原调用身份，后继不依赖已删除工作树。

## 2026-10-12：已关闭编译产物的无损归档

下列退役裸 ELF 已从原保全位置精确移除，完整字节保存在 [archive02/run02](../../output/ai/retired-compile-binaries/archive02/run02/README.md) 的 gzip6/PAX 包中。恢复这些 binary 必须先解归档；原编译/native 记录、输入 maps、源码恢复材料和历史 FAIL/log 保持原样。此前迁移保全的 inode 相等结论仍是当时事实，不代表下列裸路径今天仍存在。

| 包内 member（相对 `output/ai/`） | 字节 | SHA-256 |
| --- | ---: | --- |
| `retired-20261012/runner-write-file-operation/compile01/projectvariable.test` | 88806596 | `9c24f9c94bf99911652789d55351f5db571650ad793f40a024ba14978a71b171` |

唯一归档为 [history-binaries.tar.gz](../../output/ai/retired-compile-binaries/archive02/run02/history-binaries.tar.gz)，720,004,425 B，SHA-256 `9f3da6012aacd41f3cc49a7e1b7d657ccf601e74368aee6ad52b75918e71a20b`。原 21 文件全集见 [specs](../../output/ai/retired-compile-binaries/archive02/run02/specs.json)；本任务只涉及上表条目。

归档前后全部原文件和编译/native 记录身份相等，解流逐项核对 SHA、size、mode、uid/gid、nsmtime 及 PAX 历史 dev/inode。原归档工具 71444 实际 0；删除前再次完整验证，原工具 79461 实际 0，仅 unlink 固定 21 个裸 ELF，不删除目录。证明见 [归档结果](../../output/ai/retired-compile-binaries/archive02/run02/result.json)、[移除结果](../../output/ai/retired-compile-binaries/removal02/result.json) 与 [精确移除清单](../../output/ai/retired-compile-binaries/removal02/deleted.jsonl)。归档适配首轮 60924 的字段名错误及原失败材料另存父目录，未算作产品或 PG 失败。

恢复时先验证归档摘要，再解到新的空目录，按 manifest/PAX 恢复原字节、权限、uid/gid（有权限时）及精确 nsmtime，并复核 SHA/size。解包会分配新 inode，不能伪造原 inode/ctime；原绝对路径和输入 map 不改写。重新执行需独立源和经授权的新输入绑定，不直接复用旧 namespace。此归档未碰当前 Write Receipt、任何 Command binary、热 cache 或只读 modules，也不证明 ENOSPC 根因或后继运行必然成功。
