# D08 R3 独立验收

2026-10-05，`skill_verification` 独立验收通过，建议采纳本块。主线程已核 16 源后精确提交并推送 `3e399c3044fbd256994ad4bb6f184a0a26787ea0`；远端相同由主线程确认。本报告只验生命周期事实 Authority、Object typed port 和 Outbox Inspect 窄修，不表示整个 D08/D05 或生命周期终态已完成。

## 固定输入

- 规格为 `ed7985ad881c3cf54b577ded892c74b5f5a5fbd5` 的 [R3 卡](../work-items/recovery-d08-lifecycle-authority.md)，卡 SHA-256 `689cb7342fb563c0c742ea21c5442757f627e4d1c4554a848a5dc9358cda321a`；旧输入为 `49c6589c3919cad62a4bae2c993b5fd953d36b1e`。
- 作者 [16 源清单](evidence/d08-r3-verification/author-frozen-inputs.json) SHA-256 `d0e5fd1c03cf6b9a22c74ae299b324d983a537a90bab8dd329d79f572d8b25d9`；[作者原报告](evidence/d08-r3-verification/author-author-report.md) SHA-256 `b140c8137a5692286d6c57832c7ed8cb6dee1a2ddcf292e14ddbdc13edc2c268`。
- 独立执行树由固定基线 `git archive` 加这 16 个文件构造，未纳入 S2/Secret 活动源码。归档指纹 `ea13c7fa1c564e48a0c259cbf0b5bda6fa85e961ce9aec9b90b2979b82ac7ce7`；go.mod/go.sum 与原基线相同。完整记录见 [fixed-inputs.json](evidence/d08-r3-verification/fixed-inputs.json)。最终独立快照、作者树、主树及采纳提交的 16 源全部匹配。

## 结论与证据

静读 7 个生产文件及相关契约/loader，没有发现阻断。具体 facts 的同 Store 约束、不可变版本声明、整份 manifest 与当前 pointer/owner/gate/固定接受版本校验均符合卡。Object request 的 actor/cause 绑定由既有 opaque contract 验证；archive 完成版本约束由 R2 loader 消费的 contract 验证。删除最小 receipt 不臆造版本，后续 Outbox 本域 receipt 精确验证 action/version。清理授权仍明确 unbound。

| 证据 | 实际结果与边界 |
| --- | --- |
| 作者纯检查 | Project/Outbox 单测 race、vet；两真实测试包 compile-only、integration vet 通过。相关生产与包内单测在首轮真实检查至最终冻结之间均未变化，复用同源证据，不重复无关整树 |
| 作者修后真实组 | 原驱动 race/count=1/timeout=6m：Project 38 顶层通过，127.804s；Outbox 21 顶层通过，60.848s；Outbox 包内另 2 顶层通过。完整 B02/R2、生命周期/stop 与恢复回归包含在此组。child-only `TestProjectB02ClaimChild` skip 单列，不计业务通过 |
| 独立纯检查 | 新探针 integration 编译、integration vet，以及 `cmd/agenteam`、`cmd/agenteam-runner` 两构建通过 |
| 独立真实组 | `^TestProjectR3Independent`：3 顶层、15 子例全部通过，Project 11.057s；驱动 exit 0，58.590s。原 race/count=1/timeout=6m 不变，无探针失败 |

作者命令/原日志见 [checks](evidence/d08-r3-verification/author-checks.json)、[修后命令](evidence/d08-r3-verification/author-r3-real-regression-command.json)、[修后原日志](evidence/d08-r3-verification/author-r3-real-regression.log)。独立命令/原日志见 [纯检查](evidence/d08-r3-verification/pure-checks.json)、[真实命令](evidence/d08-r3-verification/independent-fixture-command.json)、[真实原日志](evidence/d08-r3-verification/independent-fixture.log)。无测试包的 `no tests to run` 不计行为覆盖。

独立补验以下作者证据之外的重点组合：

1. **真实 facts 的双计划与撤权。** 正式 Create/Begin 得到 operation，测试拥有的 phase fixture 设置 stopping；两份独立 private plan 都严格委派真实 Project Authority。Inspect/Stop 分别附加不同 EX 锁并设置 SH/EX 重叠锁，验证单次完整 union、不弱化 EX；逐一确认 preflight/gate/progress/report 四个精确物理 Tx 内均为 Inspect→Stop。Stop Discover 后用另一真实 Tx 持 Project EX 改 cleaning 或清 pointer，继续立即拒绝，Outbox 完整行/attempt/claim/processed 快照不变。
2. **终态确实只读。** failed Project 与删除最小 receipt 的精确终态 Inspect，在 PostgreSQL `SET TRANSACTION READ ONLY` 下成功，只有一个 committed observe Tx，Stop Discover 为零。又分别在 authorize/gate/progress/report 入口以真实 EX 事务转成 terminal，目标 Tx 只验 Inspect、短路成功；终态变更后的完整 Outbox 快照不再改变。
3. **精确目标 COMMIT Unknown。** resolver、observe、双权限 authorize 各覆盖实际 commit/rollback 两结局。代理按 backend PID 选择连接，只有目标 Tx 的精确 Project/Outbox 事实、原接受版本、Project SH 和验证步骤全部成立后才 arm；截获原真实 COMMIT 帧，保留原后台事务。六例都观测到目标 PID 仍持 SH、原 CommitResult 为 Unknown，无 actor/Stopped 或 Outbox 写入。释放原 COMMIT 或真实 rollback 后，以 Project EX 清 pointer，重试均重新拒绝。未把模拟返回值或早期事务误算为目标 Unknown。

原 callback 取消、精确 capture/attempt/fence、原 writer 与 actual join、Archive CanonicalConverge/不同项目隔离及恢复错误优先级，复用作者同源真实 Outbox 组。其普通 delivery provider 是正式测试边界；它与真实 Project facts 的权限/receipt 组合分别记证，不能合称生产普通 DeliverProject 已绑定。独立 probe 没有另造生产 allow，也没有重跑全部旧组。

## 失败记录与边界

作者首次纯检查的 `LockRequest` 不可比较编译错误、空 AccessDependencies 在 contract 层提前拒绝，以及首次真实轮的 archived/updated 时间 fixture、不合法 archive/completed receipt、poison 事务与 callback 错误分层断言，均保留原始日志。首轮至最终只改变 4 个新增 integration 文件，7 个生产文件未变化；另补真实 phase/pointer 规划竞态和同 Tx 验证计数。修后上述整组已经重跑通过，并非拼接首次剩余通过来冒充整组修后通过。原 patch 以无损 JSON 字符串保存，见 [首轮修订](evidence/d08-r3-verification/author-first-repair.patch.json) 和 [规划竞态补验](evidence/d08-r3-verification/author-phase-race-coverage.patch.json)。

独立准备脚本曾在组装 overlay 路径时发生一次 Python Path 拼接错误，发生于编译/fixture 之前，修正后编译及实际组一次通过；[诊断记录](evidence/d08-r3-verification/preparation-diagnostic.json)保留此区别。

R3 不推进 accepted→stopping，不实现 stopped 证据生成、Restore/Retry、archive/delete 完成、cleanup/finalizer、普通 delivery/requeue、Audit checker、Project HTTP 或生产 root。测试 SQL 设置的 phase/terminal 输入只证明 Authority 消费规则；Object 测试只证明正式 typed grant，不证明 D05 Service 实际停止。已 committed preflight 的精确取消保持原协议，后续 gate 失败不追溯撤销此前合法取消。

## 复现与资源交还

精确 Go 为 `/workspace/toolchains/go1.27.1/bin/go`；MinIO 为正式 source-built `RELEASE.2025-10-15T17-29-55Z`，两二进制 SHA、离线锁定依赖、环境覆盖和清除项均在真实命令记录中。原驱动提供 PG 17.8、PG 16.12/vector 0.8.1；独立选择器实际业务覆盖限于 Project 测试包。

可复跑的完整 probe 源码随证据保存，SHA-256 分别为 `018eb4dafc45bae6b7890ed6aefc20a2586598a03d6b82604f8629e2b95e2dba`（[主探针](evidence/d08-r3-verification/vr3_independent_test.go.txt)）和 `2942fec96f10cea7ee49d3a3feab9aad98012ad93ed76c2b2ab2c7b648ca0279`（[精确 PID 代理](evidence/d08-r3-verification/vr3_commit_proxy_test.go.txt)）。[reproduce.py](evidence/d08-r3-verification/reproduce.py)默认只重建固定输入，已实际验证重建与指纹；`--pure` 运行纯检查，获得独占 fixture 窗口后可用 `--fixture` 运行原选择器。复现脚本不依赖原 tmp 快照存续，不将缓存/二进制放入仓库。

独立轮 4 容器、3 网络由 driver nonce 清理后再次 exact-ID inspect absent；既有容器/网络基线不变，本任务进程 0，runtime 目录空，见 [清零与输入末检](evidence/d08-r3-verification/independent-cleanup-and-inputs.json)。作者两轮各自 4 容器/3 网络清零记录也保留。本窗口已交回主线程并转交 Secret 作者；验收者不再启动 fixture。报告与证据冻结后 all-stop，不修改其它状态卡或台账。
