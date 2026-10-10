# 当前任务：D09 有限 text-only Model Runtime

- 工作树 `/workspace/agenteam-model-text-runtime`，分支 `ai/model-text-runtime`，起点 main `04455194`。本执行者唯一写本任务；全部 Git 操作由 root 负责。
- [有限 SPEC](../docs/development/work-items/d09-text-chat-runtime.md) 与 Object/Project 五技术路径经 cleanup 非作者有限审接受。首边界15top普通/race已实际通过。九个Runtime源、输入基础test、00031与Secret router显式分派首稿已落盘并gofmt；Chat/Stream/Usage/Secret退休/Access回调已补齐方法，尚未编译或验证契约组合。无 PG、网络或 socket 运行。
- 目标：沿原 `mc.Chat/ModelStream`，一次真实 text-only attempt，串起持久 Call/attempt、00018 Usage、planned Secret read/release、原 OpenAI text adapter/D04、实际 Exchange join 与 terminal。ConsumerAuthority 必需但生产未绑定；契约 fixture 不证明 Agent/F1/Meeting 业务权限。
- v1 仅当前 Consumer plan 的 BoundedRetry、MaxAttempts=1、空 categories；AgentRetry、tools、execution-owned lease 等能力在 reservation/dispatch 前明确拒绝。重复/Unknown 保原 CallID，不自动 retry；不持久正文、不伪成功 replay。
- 全局迁移 `00031_model_logical_calls.sql` 已由 root 分配本任务独占，初稿在写尚未验；00001–30 不改。正式九个 Runtime 源及必要相邻测试、Secret router 显式新组合与真实隔离 fixture 写域见 SPEC §9。
- root已另授并已落盘：`object/process.go`+`process_current_test.go` 的纯当前ID读取；`project/audit_facts.go`+`model_access_audit.go`+`model_access_audit_test.go` 的 AccessProducer/currentGate 原Tx窄委派。它们不证明实际flock或Runtime事实provider已经完成；五路径停写可独立保存。
- SPEC §6 原 Model Secret read/release unbound 必须真实接入，不能仅注入 reader 冒闭环；新 Project provider 必须保护 authorized/sent 的原handoff到D04回调实际退出，不补consumer锁或另开Tx。
- 下一步：先保存Runtime首稿，必要编译修与正常/明确失败基础控，再交cleanup实际源窄审及准备首条真实隔离链。当前已保护准入前原Consumer/SQL回调归属，Stop取消、Drain等待原admission；材料只在Exchange实际Joined后Destroy。以上是实现意图，未编译/运行不计PASS。真实资源窗口尚未授予；首次Go前私有telemetry off、去旁路、只读shared mods、自有cache及同进程fresh≥5GiB。
- app/defaultroot、旧 wire/标准 DTO、tools 独立 STOP、Object runtime join STOP 与 Agent F1 边界保持。旧任务记录留原 topic，不复制入本任务 current。

最近实际检查：固定 `.agent-state/model-text-runtime/boundary-checks.py boundary-01`，session65398/outer498714；ordinary Go498717与race Go504312实际Wait0，各15top（Object2/Project13），原组absent/无adopted/runtime双空/组双空，outer0。原件 `output/ai/model-text-runtime/boundary-01/{ordinary.jsonl,race.jsonl,result.json}`；这不证明真实flock/PG或Runtime授权。检查后available约5.078GB低于下一次Go 5GiB，尚未再启动；继续源实现，后续Go须重新同进程门控。
