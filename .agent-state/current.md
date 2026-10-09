# 当前分支：普通 Project Variables Owner 后端

- 分支 `ai/project-variables`，工作树 `/workspace/agenteam-project-variables`，正式基线 `f1c94ee5`；root 创建并负责 Git 保存与交付。
- 唯一 SPEC：[普通 Project Variables Owner 服务与 HTTP](../docs/development/work-items/d10-project-variables-owner-http.md)。有限独审唯一缺口已补明：删除版本通过现成 CommandMeta.ExpectedVersion 严格输入、捕获并绑定原意图；差异冻结待窄复核，尚非最终接受。产品未实施，迁移 `00024` 预留未写，不预消费后继 Knowledge 00025 / Runner 00026。
- 目标为已有 initialized Project 当前 Human Owner 的普通变量 CRUD、有界分页、expected_version、原 key Lookup/显式重放、正式 HTTP 与默认根退出。复用 main 的 Account、Project、Store、Audit、Outbox/cursor，不依赖新 Skills、Model UI 或 Object 业务能力。
- 排除 Secret/白名单、Agent F1/Prompt/Runtime/Runner 注入、UI、Project 创建及完整生命周期参与者。不宣称完整 D10 或 Agent F1 完成，不解除其他模块既有停止项。
- 当前仅本文件及新卡的文档工作；无新产品/迁移/测试实现，无 PG/native/browser/网络运行或产品验证。可重建产物放 `output/ai/project-variables/`。
- 卡与本文件唯一 writer 为 `service_delivery/blocker_implementation`；卡中共享产品路径仅为待协调建议，独审接受后负责人再派写权。UI 已完整交回 service_delivery，不在此树复制其 WIP 或 Model 流水。
- 全局产品状态见[任务台账](../docs/development/agent-team/tasks.md)。本文件只作分支恢复。下一步冻结两文档交独立 SPEC 审查；子代理不执行 Git 写操作或创建工作树。
