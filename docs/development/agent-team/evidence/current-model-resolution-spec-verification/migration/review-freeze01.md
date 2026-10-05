迁移排期三文档独立窄审：需两处最小修正后采纳。

固定 manifest SHA 2e1a3bf6550285ff062d517b1b9a44f0862502fedb6ba62703c2907a740a3bd2、delta SHA 11cf1be7a20185b2c0e4538a24bda51647f1c5cca6d7f33eaf650dca93128f06。三份 before/after 原字节和精确 delta 重建均过，R4 API 代码块未变。

M1：R4 活卡第39行 Initialize 仍写“确认 schema 17 与当前装配”。应指向本卡届时分配迁移的实际 schema/装配，避免以 Resolver 的 17 冒充 R4 已具备 completion_plan。

M2：R4 活卡第173行 lifecycle_completion_schema_test.go 范围仍写“fresh/populated prefix17、严格plan/受保护回滚”。应同步为届时已分配连续前缀及前向/隔离恢复，与新迁移段和 SchemaCompletionPlan 一致。

两处均为活动实施/测试条款，不是应保留原文的历史采纳段。原第205/209行的 rev1/rev2 历史编号应继续保留。其余 00017 转留 current Resolution、R4 不预18、一个迁移/34路径/13目标、正式 Source 拒 Down 及原 Artifact/领域绑定/Object 阻断保持，未发现新增硬问题。tasks/recovery 当前预留同步准确，Resolver 卡/structured/D26 没有被本次排期暗中授予业务通过或 SQL 权限。

本轮只读冻结三文档及已知事实；未读取活动 Resolver，未运行 Go/Docker/网络/Git，未写仓库或再委派。报告冻结，等作者经 root 授权修正后的有限 delta。
