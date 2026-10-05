# Current Model Resolution 库级验收

**受限 current-selection Resolver 库及迁移 00017 已独立 PASS，主线程采纳并提交推送 `4295df7d51c1f171df78ab3f0d9cef2fd241a505`，远端同 SHA 由主线程确认。** direct/platform.memory 选择、持久 preparation、不可变 snapshot/binding 与同 Tx Secret planned Acquire 已形成可验证结果。默认生产根 `Resolution=nil`，尚无实际业务 consumer 正向装配；本结果不代表完整 D09。

输入是 `be0bd07b1dc1fcd91ad217c9bdbfe5a14003ce74` 加[最终精确 20 路径](evidence/current-model-resolution-verification/author/candidate-freeze-04/manifest.json)，不是整个后续提交。最终 manifest SHA-256 为 `60a278f7472cc62bad9a1d09c880d054e76c599ba97c9e56a0b1faa0e7a54a2d`；9 生产及 SQL 与 [production-review-02](evidence/current-model-resolution-verification/author/production-review-02/manifest.json)逐字一致。[恢复卡](../work-items/recovery-d09-current-model-resolution.md)的 §1–11 技术条款保持。作者和独立验收的[原报告、命令、输入及资源](evidence/current-model-resolution-verification/README.md)已持久归档。

## 实际验证与复用

| 有效真实组 | 固定输入 | 顶层/子例 | 结果 |
| --- | --- | --- | --- |
| 作者六个新业务组 | candidate03；相应源至最终版未变 | 6/35 | 从原 new-01 明确复用完整业务组 |
| 作者完整 Schema 复验 | candidate04 | 1/5 | PASS，包 7.068s |
| 作者旧 Model | candidate04 | 8/19 | PASS，包 21.597s |
| 作者 Secret + MCP | candidate04 | 7/43 | PASS，包 39.355s |
| 作者 Account | candidate04 | 4/7 | PASS，包 9.367s |
| 独立风险探针 | candidate04 + 私有 overlay | 2/4 | PASS，包 6.105s，driver 61.992s，exit 0，无 FAIL/SKIP |

作者合计 **26 distinct 顶层/109 子例**，是分版本、按未变输入复用的有效结果，不称最终版一次全组通过。原 new-01 是六顶层/39 子例 PASS，另 Schema 父组及一个子例 FAIL；39 个绿子例中有四个属于失败 Schema 父组，合计时排除它们，再计完整 Schema 五子例。精确分布见[作者原索引](evidence/current-model-resolution-verification/author/final-delivery.json)。

原 unit/race/vet、integration compile/vet、两命令 build 均由实际 argv/env/exit 与 raw 绑定；修订后按影响复验并保留旧记录。独立 race compile-only 18.849s、vet 10.616s 均 exit 0，见[compile 元数据](evidence/current-model-resolution-verification/verification/logs/compile-01.json)与[vet 元数据](evidence/current-model-resolution-verification/verification/logs/vet-01.json)。空成功日志不单独作为通过证明。全部真实组沿原 driver、Go 1.27.1、离线只读 module、`-race -count=1 -timeout=6m` 和任务自有 PG17/PG16/MinIO；未增加网络故障方法或延长预算。

证据复建有一处明确限制：中间成功检查里的两份新测试字节（fixture `48a751d4d7a60fdb4ac385426f3d9c35c750f723c82a33fc9fa3c58d74389f39`、selection `ff09c65f8602e02eb7201bbdb21e2f077287a7e15ecfa6548e1f44ec7d7148c4`）当时只留 input SHA，现无对应原文件；涉及 integration-compile-03、integration-vet-01、final-integration-compile-01。原命令、哈希和实际退出记录仍完整保留，不假称这些中间版本已逐字重建。review02-vet 和 commands-build 元数据也记录过这两份文件，但 argv 未选择其测试包。最终 20 的直接 compile/vet、所有关键原失败/修复链及分版本真实 26/109 和独立 2/4 输入均可重建，后续真实 driver 的编译和执行结果不受此归档限制影响。

独立 [2 顶层/4 子例原日志](evidence/current-model-resolution-verification/verification/fixture-runs/independent-01/driver.log)补证：

- 真实 Account 当前 Session 撤销在 prepared、committed 和 live Model 不存在三种情况下均先返回精确 `SESSION_REVOKED`，本域、input、lease 完整事实不变，无 Secret Apply。该 fixture 验真实权限端口，不宣称完成生产 consumer 或 HTTP 登录链。
- 用普通第二 Tx 的真实 consumer record EX holder/waiter，读 `pg_locks`/`pg_blocking_pids` 确认等待；放行后重新验证撤权。完整 union 仅一次，精确 `FORBIDDEN`，零 Secret Apply 和零四类副作用。
- 在真实 Secret Apply 后、私有 witness 仍 active/verified 时，exact Tx grant 成功；错 owner/action 及全新父 context、不同物理 backend 的 Tx 精确拒绝。负例明确 outer rollback，之后另启合法事务成功，不把 poisoned Tx 当正常后续提交。
- 同 Tx 实际读到 input/snapshot/binding/lease 与 committed phase 后再注错，四类事实全部回滚、preparation 保留；随后同计划正常提交。没有读取凭据材料或产生 SecretResolve Audit。

## 原失败及修订

[生产 P1/P2](evidence/current-model-resolution-verification/static/production/review01.md.txt)是静态发现：锁后缺 preparation 重读，以及持久稳定 Actor DTO/canonical identity 复核不足。[review02](evidence/current-model-resolution-verification/static/production/review02.md.txt)确认当前权限之后完整重读、变化时 Tx 外重规划，以及稳定 Actor 闭集和 identity 重算；坏本域事实校验缺口不扩大表述为已证明的越权漏洞。

[T1/T2/T3](evidence/current-model-resolution-verification/static/readiness/review.md.txt)及[第二轮旧 txContextKey](evidence/current-model-resolution-verification/static/readiness/review-candidate02.md.txt)同为静态发现。测试修正合法 Project 时间/恢复与精确 gate、全新 bounded 父 context 下真实同 Tx witness 拒绝、完整旧 constraint catalog；[candidate03](evidence/current-model-resolution-verification/static/readiness/review-candidate03.md.txt)通过后才运行，不能追称存在动态原红。

唯一真实 Schema 首红是第二次 Migrate 的 `MIGRATION_HISTORY_DIVERGED`：测试第一次失败后更换了 Source checksum，触发 D03 的既有保护。[首轮 raw](evidence/current-model-resolution-verification/author/fixture-runs/new-01/driver.log)没有记录第一次 Fault 的 SQLSTATE，**不能追认为 22012**。仅修新 schema 测试，改用缺失测试函数的固定 Source，真实观察 `42883`、pending=1/applied=0/goose=0 和原 checksum；只补测试依赖后，用同 runner/Source 重试。修后验证 applied=1/goose=1、同 checksum、三表完整，失败与成功后旧行和全部旧约束均精确不变。[归因](evidence/current-model-resolution-verification/static/readiness/new01-schema-diagnosis.md.txt)、[窄修](evidence/current-model-resolution-verification/author/candidate-freeze-04/delta.patch)和[复验 raw](evidence/current-model-resolution-verification/author/fixture-runs/schema-01/driver.log)均保留；此前 readiness 静态漏查该重试前置也不倒写。

四次更早的非零编译原件一并保存：固定 snapshot 漏带嵌入资源、误用既有 Secret UsageResult API，以及新 fixture 的未定义载体/角色和 QueryRow 返回类型。每次都有实际命令、原 input SHA、原字节与错误日志；未覆盖失败或把后来的通过归给旧输入。

## 资源与能力边界

作者五轮共 20 容器/15 网络，独立一轮 4 容器/3 网络；各轮均有存活时 nonce 标签观察、两遍 actual exactID absent、原 2 容器/4 网络 ID/name/labels 不变。作者跟踪 397、独立跟踪 130 个进程记录均退出，进程组及 runtime/gotmp 空，事件观察器和 driver 已退出。独立[资源原件](evidence/current-model-resolution-verification/verification/fixture-runs/independent-01/resource-handoff.json)及[窗口交回](evidence/current-model-resolution-verification/verification/resource-window-handoff.json)保留；主线程复核实际资源后采纳。

生产 root 仍不绑定 Resolution；AgentRun/Memory/Tool 严格 fixture 不代表这些真实域已交付。本结果没有 serving_snapshot、Invocation/Usage、凭据材料读取、Provider 发送、lease read/release/retire 或 Project cleanup；`/readyz` 仍 503，Summary 初值仍待用户决定，暂停的 Object 任务及其阻断未被解除。Unknown 只证明真实 Store commit/rollback 后结果装饰、原返回 attempt/cause、真实后态和普通 writer mutex 屏障；不声称物理 COMMIT ACK 丢失或原 ROLLBACK backend 持锁已经验证。

本次文档归档没有重跑业务、Go、Docker、Git 或网络。原件指纹、精确局部源码重建、相对链接和格式检查见[归档检查](evidence/current-model-resolution-verification/archive-checks.json)；证据只保存必要文本与局部源码定位，没有 binary、cache、凭据或完整树。
