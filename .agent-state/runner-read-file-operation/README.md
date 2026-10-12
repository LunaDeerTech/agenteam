# 单次 read-file Operation 恢复

本片已由 main 非 squash 合并 `c7ad165b` 接受，任务最终提交为 `42aa5bf5`，相对原 main `19df3fea` 的 23 个提交全部可达，包括 `d40fc527` 的真实 codec 红源。包含完整历史与保全文档的 main `d2c3e5aa` 已推送成功，原工具 56219 实际 0。范围和四轮实际结果见[正式卡](../../docs/development/work-items/d18-runner-read-file-operation.md)；有限验收不包括 Transcript 消费、下一轮 Model 或生产默认绑定。

完整输出已从 `/workspace/agenteam-runner-read-file-operation/output/ai/runner-read-file-operation` 通过同文件系统 `renameat2(RENAME_NOREPLACE)` 无覆盖迁至：

`/workspace/agenteam/output/ai/retired-20261011/runner-read-file-operation`

136 个原文件共 350,560,682 字节，包含 checks 64 个文件、四套 compile 各 12 个文件及四套 native 各 6 个文件。原相对路径集合、SHA-256、大小、device、inode、mode 全部相同；无 symlink 或其它特殊文件。四个原 `projectvariable.test` 二进制均完整保留，实际 SHA 与各自编译结果一致。`retention/output-before.json` 保存原文件清单及旧、新根位置，`retention/result.json` 保存实际迁移结果。原配方的绝对路径没有改写。

`retention/tmp/rfo01` 至 `rfo04` 保存各原 `/tmp` 目录的 log、owned / request JSON 与 telemetry mode，保持原 nonce 和相对目录，共 16 个文件、29,238 字节。副本逐字相同，映射见 `retention/external-copies.json`；原 `/tmp/rfo01` 至 `/tmp/rfo04` 均未删除。

| 原验证输入 | main 可达源码 | 已保留结果 |
| --- | --- | --- |
| interfaces-unit01 / vet01 | `b1437e28` | 19 top race 与三包 vet 通过 |
| tool-unit01 / vet01 | `2d4f07ac` | 16 top / 34 sub race 与两包 vet 通过 |
| compile01 / native01 | `2d4f07ac` | 旧 fixture 参数 / Operation 数量断言 FAIL；原 TCP 尾仍有差额 |
| compile02 / native02 | `b952cd59` | 清 Mount 通过；happy 未得到预期 Unknown，原动态 Status 未记录 |
| codec-repro01 | `d40fc527` | 真实 Message codec 反例 FAIL，原产品未变 |
| codec-fixed01 / vet01 | `67329a12` | 窄修后 2 top / 7 sub race 与 vet 通过 |
| compile03 / native03 | `8d0781bf` | clear 通过；happy 到 terminal 元数据解析 FAIL，尚未正常 Lookup / Stop |
| compile04 / native04 | `b5532d77` | f.Progress fixture 修后，原两场景 whole PASS |

`retention/source-history.json` 已逐 Git blob 核对 11 份输入清单：四轮编译各 1,825 个已跟踪输入、接口 unit / vet 各 666 个、Tool 与 codec 的五份清单各 509 个，全部与上述历史提交相同，前后输入一致。共核对 1,830 个不同历史 blob，没有缺失或不匹配，不需额外源码恢复 patch。每轮编译另有固定 Go 和 MinIO 两项外部输入；每份 native 清单恰为对应编译输入加原二进制。

native01–03 和 codec 红的失败事实保持；native04 通过不解释 native01 TIME_WAIT 的归属，不补造 native02 未记录的 Status，也不把 native03 未执行的正常后验记作通过。保全材料是原运行证据，不能直接将旧 namespace 或路径当作现行可重跑配方。

当前 Result Consumption、Skill Assignment 的负责人确认无旧树绝对运行路径依赖；七个其它 worktree 的源码 / 脚本 / 运行元数据和 symlink 检查也未发现路径借用，仅两份协调文档仍提及旧位置。核对范围见 `retention/dependency-check.json`，不将其扩大为全宿主进程排他证明。共享 SkillHTTP 热 Go cache、SkillsMilestoneResume 只读 modules / MinIO 及固定工具链仍在原保护位置，没有随本次保全移动。

root 已在 main 推送成功后完成原 WorkTree 的普通 `worktree remove`，以及本地 `ai/runner-read-file-operation-20261011` 的普通 `branch -d`，两步均实际 0；本地 tip 为 `42aa5bf5`，没有使用强制删除。fresh 查询确认远端 tip 为 `4ac043e0683c8ae2374d4623c70b092b6515f47b` 后，精确 lease 删除原工具 99189 实际 0，随后 fresh `ls-remote` 成功且为空、explicit prune 实际 0。材料保全与 WorkTree / 本地 / 远端分支清理均已完成；全局累计及后续任务以[当前索引](../current.md)为准。

## 历史编译二进制归档补充

2026-10-12，本片 `compile01`、`compile02`、`compile03`、`compile04` 中的 四个 `projectvariable.test`（共 343,153,327 B）已纳入单个[无损归档与恢复说明](../../output/ai/retired-compile-binaries/archive01/README.md)。归档成员前缀为 `retired-20261011/runner-read-file-operation/`，其后保留原 `compileNN/projectvariable.test` 相对路径；精确原路径、成员名、SHA、size、mode、uid/gid、纳秒 mtime 及旧 compile 记录关系见 [before.json](../../output/ai/retired-compile-binaries/archive01/before.json)。恢复前先验证归档和成员摘要，再无覆盖还原所需二进制。

阶段1工具 31394 实际退出 0，完整解流验证全部九个成员；阶段2在再次核对归档、成员及原件身份/内容后，仅删除获授权的九个裸二进制，原工具 89889 实际退出 0。本片上述裸件现已从 retained 路径移除，完整字节仍在归档中；实际删除清单和容量观察见 [removal01/result.json](../../output/ai/retired-compile-binaries/removal01/result.json)。旧 compile/native 输入映射、结果、日志、FAIL 和保全证明均未改写。

前文文件数量、裸件保留及 inode 相同等叙述是原整体迁移时的历史事实。重新提取会分配新 inode，不能宣称重建原 inode 或改写旧运行记录；原失败边界、外部保护依赖与有限验收范围保持。
