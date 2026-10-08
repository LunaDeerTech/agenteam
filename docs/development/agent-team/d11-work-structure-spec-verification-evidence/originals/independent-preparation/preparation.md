# D11 Work structure — 独立规格审查准备，STOP

已读冻结 frontier 28034095 与 backend 只读准备 8dd31825，并仅补核必要既有架构/Authority/Outbox/Tx/锁接缝。未读活动正式卡。此记录只是后续完整 SPEC 的审查重点，不是正式规格通过或实施授权；00021_work_structure.sql 只记 root 预留，18 路径仍待正式卡冻结。

正式卡重点核对：

1. Milestone／Sprint 的真实 canonical 所有权、同 Project 父子约束和 typed SprintID 复用；不重复 current_sprint_id，也不由 Work 直接修改 Project pointer。首卡的 planned 创建、字段更新、结构读取与人工顺序须自足，不能借 Task fixture 关系充当持久事实。
2. 精确 DTO/字段限额、Unicode、nullable/可选差异、命令 semantic 与 receipt；当前 Human Session/Owner/Project gate 在历史前，原同义结果与 expected_version 的先后清晰。同 key 异义、归档重放、COMMIT Unknown 和取消边界要可执行且不靠客户端推断提交。
3. rank 编码、位置/锚点、组容量、rebalance、排序 generation、cursor 与业务 version/updated_at/event 的关系需全部冻结；completed 历史只读如何约束排序/维护要明确，不能把 Task 的规则未经说明直接外推到结构。
4. Work producer 与 Project-owned Work gate 各证自身事实。现 Project/events.go 仅 Project/Model，Work 仍未绑定；新闭集须完整绑定 Actor/Event/Project/issuer/CurrentAccess+NewFact。Outbox 先当前授权、再历史、再新事实的现有顺序不变；Work 不能以 Project gate 替代自己同 Tx 的 command/canonical 验证。
5. 构造/typed catalog 注册和 Outbox seal 的顺序必须可实现，不能依赖循环 nil backfill。首次 AcquireAll 收齐 command/User/Project/RankGroup/必要 Sprint 与 Outbox registry/event 锁，按既有规范排序、不补低序锁或升级；同时核 NormalizeLocks 原始数量界、容量与一次事件数。
6. placement 只实现真实结构读事实；Task membership、Sprint 删除正向、start/complete/rollover、Execution/Dispatch、Work lifecycle cleanup 继续明确 Unbound。不能把未实现当 empty、借 work_claims 作 Task canonical，或注册只清两表的完整 Work participant。
7. 新结构/命令结果/typed Outbox 同 Tx 原子性、同父并发与分页、跨项目/旧 Session/非 Owner/Agent/Service/未初始化/生命周期反例，须落到精确新旧 top、strict fixture 与独立不同构造的 PG 验收。正向 initializer 只证明隔离组合，不冒生产 Skills/Create HTTP、dispatcher、执行适配或全 D11。
8. 逐项核 17 技术＋README 末件、00021 唯一写权、Project 共享入口及 Go/cache/PG 资源预算与真实 wait/退役。对象运行时、通用工具运行时、SPA 三停止及 Image/Jina 现状不因本卡改变。

当前未发现必须为准备阶段扩大产品写域的理由；正式卡尚未读，尚无 D 项或通过结论。只写本目录两件，没有产品/仓库文档改动、Go/Node/SQL/网络/Git/资源操作、全树复核或委派。待 root 交接固定正式卡后执行一次完整 SPEC STATIC。
