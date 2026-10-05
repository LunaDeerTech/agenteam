# Invocation / Usage ledger 规格审查与实施移交

[rev1 规格](../work-items/recovery-d09-invocation-usage-ledger.md)已独立静审 **STATIC PASS**，主线程采纳并提交推送 `9aad5f5d26bf5af066390a8bd35b91d57d6b8c8b`。随后已授权 `recovery_handoff`（backend_worker）实施卡内精确 20 路径，独立验收负责人为 `restore_test_dependencies`。`00018_model_invocation_usage.sql` 已纳入实施授权，但业务、迁移、性能和真实运行均尚未验收；本档案只保存规格及审查证据。

固定业务输入为 `4295df7d51c1f171df78ab3f0d9cef2fd241a505`。被审卡 SHA-256 为 `d53835cb79618b5cea487e8d84161cdc2e4f3a9900c6d35a86a258f75f795cab`；[独立报告原件](evidence/invocation-usage-ledger-spec/review.md) SHA-256 为 `1fd29dfd4e7c9892440c8d6abbcb074f9e5f9964ec6b7535d3ba744f32576de0`。采纳提交中的卡 SHA-256 为 `565bf9f2e0e819ede0286a47b2b5bc69aadb65c63b90538c7bff4289306724ca`。该次差量仅首段行政状态；本次再同步实施授权并追加移交说明，§1–10 技术原文保持逐字。

## 审查结论与证据

独立审查核对完整卡、19 个原输入及 13 个必要接缝指纹，未发现必须修改的规格条款。已明确 C0 兼容、可信事实与当前准入/技术终局分离、旧观察回执、同 Tx 三表原子性、nullable 统计、完整锁与查询、历史 FK 删除和真实 fixture 可达性。原子 SQL、2s 查询预算、强反例及全部运行结果仍是实施后的验收门槛，静审不替代这些检查。

| 固定原件 | 用途 |
| --- | --- |
| [被审 rev1](evidence/invocation-usage-ledger-spec/rev1.md)、[19 输入](evidence/invocation-usage-ledger-spec/inputs.json)、[作者检查](evidence/invocation-usage-ledger-spec/checks.json)、[自查](evidence/invocation-usage-ledger-spec/self-review.md)、[原冻结索引](evidence/invocation-usage-ledger-spec/freeze.json) | 原规格、Git blob/SHA/字节数及当时格式/链接核对 |
| [独立 input checks](evidence/invocation-usage-ledger-spec/input-checks.json)、[13 接缝](evidence/invocation-usage-ledger-spec/additional-inputs.json)、[review checks](evidence/invocation-usage-ledger-spec/review-checks.json)、[独立交付索引](evidence/invocation-usage-ledger-spec/delivery-index.json) | 独立固定输入方法、结论与未运行范围；保留原读取位置，不把它们视为当前活动源码 |
| [采纳行政差量](evidence/invocation-usage-ledger-spec/administrative-delta.patch)、[行政检查](evidence/invocation-usage-ledger-spec/administrative-checks.json) | 从被审卡到接受提交，仅第3行变化，§1–10 原字节相同 |
| [独立最小计划](evidence/invocation-usage-ledger-spec/acceptance-plan.md)、[计划冻结记录](evidence/invocation-usage-ledger-spec/ready-plan.json) | 主线程仅采纳风险计划：暂定 2 top / 3 sub，第三候选仅在作者数值/并发证据不足时加入；尚无探针、冻结 selector 或运行，不是验收结果 |

[来源索引](evidence/invocation-usage-ledger-spec/sources.json)保存原路径、同 SHA 去重别名及精确 Git locator；19+13 个源码/规格输入以固定 `commit:path` 重建，不复制源树。加上原规格引用的文档，共 38 个去重 Git 对象；采纳后的完整卡直接由 `9aad5f5:docs/development/work-items/recovery-d09-invocation-usage-ledger.md` 重建，避免再存一份近似整卡。原行政 patch 可在独立目录应用到归档 rev1，输出应与该 Git 对象逐字相等。

原件的“候选/未授权”保持其冻结时事实，当前权限以本页和正式卡页首为准。原 rev1 的 17 个相对链接保留原字节，由来源索引映射回固定 Git 上下文；不将重定位后的路径误当仓库现行入口。`administrative-delta.patch` 第4、7行是原 unified diff 的空白上下文标记，只对这个精确原件保留尾空白例外，不归一化原始证据。

## 实施边界

实施范围是 9 生产源、1 新迁移、10 测试源，包含 Invocation 当前行、逐观察幂等回执和可重建 Execution 用量统计；后者不是未决 Project Summary。生产 InvocationFacts owner、call/Provider 编排、Model credential read/release 分派、Outbound 委派、Process/lease/lifecycle、HTTP/root 仍未绑定，严格测试端口不能变成生产 allow。DB COMMIT Unknown 与 dispatch unknown 分离，未观察回执不能证明原 writer 终局或授予重发。

既有暂停 Object 任务及 Artifact 最终共享 guard 阻断、Summary 产品问题、`ready=false` / 503 均保持。本归档未读取活动实现作为验收证据，未运行 Go、SQL、Docker、网络或 Provider，也未修改 AGENTS、tasks、recovery 或任何业务文件。[归档检查](evidence/invocation-usage-ledger-spec/archive-checks.json)仅证明小型原件/固定 Git 身份、链接和行政差量一致；后续业务验收另交独立负责人。
