# 已提交 RoundInput 投影：方法与恢复

产品范围见[本片卡](../../docs/development/work-items/d22-next-round-projection.md)。`run-basic-checks.py` 复用上游 68 的记录方式，仅选择本片 **8 个新 top**：Execution reader 4、Execution contract 1、AgentLoop 3。成功且精确命中后，只 vet 这三个包；不重跑原基础矩阵，不启动 PG、Model、Runner 或 HTTP。实际basic01为8 top /42 sub race与三包vet通过，原22796 actual0，657输入六份相同；不把受控基础当真实PG证明。

以下为原基础调用，仅在root明确安排的新资源窗口用于复现，fresh输出不得覆盖原件：

```sh
python3 -B .agent-state/next-round-projection/run-basic-checks.py --output output/ai/next-round-projection/checks/basic01
```

输出目录必须不存在，失败不自动重试。脚本先分别执行 race/plain `go list -deps -test -json`，从实际编译字段收集本仓库输入（含被选中包的测试），强制包含六个本片 Go 源及 66–68 migrations。发现前先绑定候选源，再只保留实际闭包，防止发现期间输入变化；不把无关依赖包的 `TestGoFiles` 冒充编译输入。每一阶段保存 command、PID、实际 `Popen.wait`、原日志、前后输入；任何失败或输入变化立即停止。race 使用 `-p 1 -count=1 -timeout=10m -json`，逐包逐名校验 8 个 top 各运行并通过一次，额外或缺失 top 均拒绝。最后才运行原三包 vet。

固定 Go 为 `/workspace/toolchains/go1.27.1/bin/go`，并绑定其 SHA；`GOTOOLCHAIN=local`、`GOPROXY=off`、`GOSUMDB=off`、`-mod=readonly`、`GOMAXPROCS=1`。共享热缓存只用 `agenteam-skill-package-http/output/ai/skill-package-http/go-build`，只读 modules 来自 `agenteam-skills-milestone-resume/output/ai/skills-milestone-resume/go-mod`，必须按 root 的实际 writer 交接使用。闭包记录是仓库源、嵌入文件、脚本和 Go 二进制，不声称保存整个外部 toolchain/module tree。

本基础方法不拥有PG profile、七资源或TCP门；原真实方法见[pg-method.md](pg-method.md)。组合 `272c74fa` 的compile03/list实际0，1901编译输入前后相同，runtime1902仅增加87,980,429 B的实物binary（SHA `b961923cf557d7a2dfaa8cf5fcbaa48f7aee7d76250547f16c63606e4de88ec8`）。native03原72578亲收actual0，sup827584 /113.320s；1top1sub23.50s及全部Wait/七资源双absence/desc-private-runtime双空/原自动TCP双空/输入不变已获唯一主审有限接受。实际只读已提交RoundInput历史、完整投影及深拷贝稳定性通过，新增Model/WSS为0；不生成下一ModelRequest、不授执行或接默认生产。

原native01、02 wholeFAIL完整保留。两轮均0.00s在继承fixture的MkdirTemp前置失败，未到投影；01原errno缺失，02实际ENOSPC。第二次失败点Statfs诊断已由compile03编入，03未触发该分支。后来mkdir成功、释放容量或第三轮通过都不解释原ENOSPC原因，不补造01原errno；全部旧STOP保持。

原 `output/ai/next-round-projection/` 已由独立保全作者以同文件系统 `renameat2(RENAME_NOREPLACE)` 整体迁至 main `output/ai/retired-20261012/next-round-projection/`。106个普通文件、271,750,879 B、25个目录（含根）的路径、SHA、size、device/inode、mode及纳秒mtime前后逐项相同；全部checks、compile01–03三个binary、native01–03配方/结果及两轮FAIL原样保留。源output已不存在；root在main `095d3359` push13678实际0后，已完成Projection普通worktree remove与普通branch -d（原最终7b08aa00），原85841实际0。

证明单独保存在 main `output/ai/retired-20261012/next-round-projection-preservation/`：`output-before.json`、`output-after.json`、`preservation-result.json`、`source-recovery.json`和`process-audit.json`，没有改写原output内的记录。`/tmp/nrp01`、`/tmp/nrp02`、`/tmp/nrp03` 各4个原件（log、owned/request、4 B telemetry）共12文件已排他复制至该证明目录对应子目录，字节/mtime/mode保持，原tmp全部留存；main原 `projection-parent-mkdir01.json` 也另存副本且原位保留。实际保全工具331a6b退出0。

23份原输入map的1,905个仓库历史文件版本（1,902路径）已逐字匹配原 `c30623db`、`5b04b4fc`、`272c74fa` 三个检查点，未发现缺失版本；三个实物binary均匹配原map。审计时这些检查点只在原分支历史；root随后完整历史合入main `b0467431`，并实际确认这三个检查点及最终文档 `7b08aa00` 均main祖先（c2c99a实际0）。源码历史可达及main推送已闭合；root以原远端tip `272c74fa` exact-lease删除原19766实际0，后继fresh查询为空、fetch/prune及local/tracking为空原91249实际0。Projection树/本地/远端均清理完成，没有重写或删除原保全材料。外部Go与MinIO的原SHA已核且未移动，SkillHTTP热cache、SkillsMilestoneResume只读modules/MinIO保持保护；没有声称保存整个外部toolchain/module tree或故障时宿主状态。

原执行者确认全部运行实际退出；当前可见 `/proc` 未见原树进程路径，已知PID均不存在。扫描存在1,410次权限拒绝观察，故该项只证明原Wait与当前可见范围，不宣称覆盖不可见namespace或未知owner。未见独有untracked成果或输出外ignored材料，当前作者确认无下游绝对路径运行依赖。原01未知errno、02ENOSPC原因未明及全部历史STOP始终保留。

## 历史编译二进制归档补充

2026-10-12，本片 `compile01`、`compile02`、`compile03` 中的 三个 `projectvariable.test`（共 263,912,885 B）已纳入单个[无损归档与恢复说明](../../output/ai/retired-compile-binaries/archive01/README.md)。归档成员前缀为 `retired-20261012/next-round-projection/`，其后保留原 `compileNN/projectvariable.test` 相对路径；精确原路径、成员名、SHA、size、mode、uid/gid、纳秒 mtime 及旧 compile 记录关系见 [before.json](../../output/ai/retired-compile-binaries/archive01/before.json)。恢复前先验证归档和成员摘要，再无覆盖还原所需二进制。

阶段1工具 31394 实际退出 0，完整解流验证全部九个成员；阶段2在再次核对归档、成员及原件身份/内容后，仅删除获授权的九个裸二进制，原工具 89889 实际退出 0。本片上述裸件现已从 retained 路径移除，完整字节仍在归档中；实际删除清单和容量观察见 [removal01/result.json](../../output/ai/retired-compile-binaries/removal01/result.json)。旧 compile/native 输入映射、结果、日志、FAIL 和保全证明均未改写。

前文文件数量、裸件保留及 inode 相同等叙述是原整体迁移时的历史事实。重新提取会分配新 inode，不能宣称重建原 inode 或改写旧运行记录；原失败边界、外部保护依赖与有限验收范围保持。
