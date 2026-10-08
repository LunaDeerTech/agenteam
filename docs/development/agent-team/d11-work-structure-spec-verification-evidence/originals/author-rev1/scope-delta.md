# 候选 28034095 → 正式 D11 rev1 的规格差量

方向来源是已封 `/workspace/scratch/architecture-d11-structure-frontier01/candidate.md`，SHA 28034095f87c68dff09a88fc37e5ce7e10e7817cac07e24259617c9715876c68。本文件是规则对应说明，不把研究候选当旧产品版本，也不复制整份候选或 SDK/Go 图。

| 候选待冻结项 | 正式卡位置/具体决定 |
| --- | --- |
| 字段/caps/typed ID/版本 | §2–3：保既有 SprintID；UUIDv7/稳定 Actor history；title256 scalar/1024B、description32768B；Milestone version；对象/receipt/命令 JSON 闭集；groups4096、page50/200 |
| rank 与纯 rebalance | §2.2：32hex uint128 midpoint、必要时同组均分；一 before 锚点/尾部；维护不改业务版本/时间/事件；group generation；完成 Sprint 不接业务重排 |
| current/历史/newmutation | §4.1：直接从 D01 foundation 正式顺序归位；当前 Session/Owner/Read→原 identity/semantic/receipt→Mutate/存在/target/version/completed/anchor/cap；非 Model 特例；no-op/跨 Actor/Project/ID collision 明列 |
| 同 Tx/分页/锁 union | §3：HMAC current-authority分页、两种组和 stale；同 Store活Tx；ProjectEX+既有RankGroup/SprintAggregate；原始Normalize输入≤512，PrepareAppend锁首次全并入；不新增MilestoneAggregate |
| Unknown 与真实命令事实 | §2.3/4.2–4.3：五表、planned/complete区分；原输入/plan_revision与有限3轮重规划；只读≤3s确认；原writer串行Lookup；不自动重执行；Activity/业务/Event/receipt同final Tx，实际join |
| Work producer + Project gate | §5：恰work.milestone_changed/work.sprint_changed；2typed payload；私有issuer、currentAccess/NewFact、真实command/canonical；Project仅两个入口精确分派；旧Model/Project/Lifecycle保留 |
| 后继未绑/清理 | §7：只实现真实placement读口，不虚造Task/occupancy/Delete接口；Task canonical才有membership；D22/D23占用/历史；Work整体cleanup与pointer writer/生产均待 |
| 路径/资源/验收 | §6/8：保持18路径，00021唯一占号；6新PGtop+7旧pure/5旧PG与2不同构造独验；45s离线/120s含cleanup/6m包/75sTCP/5GiB/有限最多原7ID基准；root串行窗口 |

新增数值/算法/DTO/事件名属于本张工程规格决定，须独审/root接受，不声称在候选或旧运行中已存在。没有扩产品到Task/删除/生命周期/HTTP/production root。唯一既有产品源是 project/events.go；其余新域源、迁移和README按表移交。

B02 fixture边界按实际源码及作者独立只读准备补准：持久Account/Session测试行通过真Authority，不等于Bootstrap/Login；正常Project Create使用持久test-only Skill receipt，不调用Object runtime；过程停止证明仍fail-closed。Read placement不是mutation授权；编译包含纯Object keyring也不表示重开停止任务。
