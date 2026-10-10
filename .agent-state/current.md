# 当前任务：D09 有限 text-only Model Runtime

- 工作树 `/workspace/agenteam-model-text-runtime`，分支 `ai/model-text-runtime`，起点 main `04455194`。本执行者唯一写本任务；全部 Git 操作由 root 负责。
- [有限 SPEC](../docs/development/work-items/d09-text-chat-runtime.md) 已冻结，待 cleanup 非作者接口审。当前只有本卡与本文件变化；无产品、迁移或测试实现，无 Go、PG、网络或 socket 运行。
- 目标：沿原 `mc.Chat/ModelStream`，一次真实 text-only attempt，串起持久 Call/attempt、00018 Usage、planned Secret read/release、原 OpenAI text adapter/D04、实际 Exchange join 与 terminal。ConsumerAuthority 必需但生产未绑定；契约 fixture 不证明 Agent/F1/Meeting 业务权限。
- v1 仅当前 Consumer plan 的 BoundedRetry、MaxAttempts=1、空 categories；AgentRetry、tools、execution-owned lease 等能力在 reservation/dispatch 前明确拒绝。重复/Unknown 保原 CallID，不自动 retry；不持久正文、不伪成功 replay。
- 全局迁移 `00031_model_logical_calls.sql` 已由 root 分配本任务独占，尚未创建；00001–30 不改。正式九个 Runtime 源及必要相邻测试、Secret router 显式新组合与真实隔离 fixture 写域见 SPEC §9。
- 待审接缝：SPEC §8 的 `ProcessGuard.CurrentProcess()` 只读候选；尚未获 Object 源写授权。SPEC §6 明确原 Model Secret read/release unbound，必须真实接入，不能仅注入 reader 冒闭环。
- 下一步：cleanup 审冻结 SPEC，必要修订后 root 保存并协调实现授权；基础正常/明确失败自测后准备首条真实隔离链。真实资源窗口尚未授予；首次 Go 前私有 telemetry off、去旁路、只读 shared mods、自有 cache及同进程 fresh≥5GiB。
- app/defaultroot、旧 wire/标准 DTO、tools 独立 STOP、Object runtime join STOP 与 Agent F1 边界保持。旧任务记录留原 topic，不复制入本任务 current。
