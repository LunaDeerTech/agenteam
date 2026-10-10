# 当前任务：D09 有限 text-only Model Runtime

- 工作树 `/workspace/agenteam-model-text-runtime`，分支 `ai/model-text-runtime`，起点 main `04455194`。本执行者唯一写本任务；全部 Git 操作由 root 负责。
- [有限 SPEC](../docs/development/work-items/d09-text-chat-runtime.md) 与 Object/Project 五技术路径经 cleanup 非作者有限审接受。首边界15top普通/race已实际通过。九个Runtime源、两基础test、00031与Secret router显式分派已落盘；Runtime首有限8top普通/race与model包vet已wholePASS，原命令/Wait/私有runtime尾齐。尚未PG/真实Secret/Usage/wire调用，不以pure通过代替组合接受。
- 目标：沿原 `mc.Chat/ModelStream`，一次真实 text-only attempt，串起持久 Call/attempt、00018 Usage、planned Secret read/release、原 OpenAI text adapter/D04、实际 Exchange join 与 terminal。ConsumerAuthority 必需但生产未绑定；契约 fixture 不证明 Agent/F1/Meeting 业务权限。
- v1 仅当前 Consumer plan 的 BoundedRetry、MaxAttempts=1、空 categories；AgentRetry、tools、execution-owned lease 等能力在 reservation/dispatch 前明确拒绝。重复/Unknown 保原 CallID，不自动 retry；不持久正文、不伪成功 replay。
- 全局迁移 `00031_model_logical_calls.sql` 已由 root 分配本任务独占，初稿在写尚未验；00001–30 不改。正式九个 Runtime 源及必要相邻测试、Secret router 显式新组合与真实隔离 fixture 写域见 SPEC §9。
- root已另授并已落盘：`object/process.go`+`process_current_test.go` 的纯当前ID读取；`project/audit_facts.go`+`model_access_audit.go`+`model_access_audit_test.go` 的 AccessProducer/currentGate 原Tx窄委派。它们不证明实际flock或Runtime事实provider已经完成；五路径停写可独立保存。
- SPEC §6 原 Model Secret read/release unbound 必须真实接入，不能仅注入 reader 冒闭环；新 Project provider 必须保护 authorized/sent 的原handoff到D04回调实际退出，不补consumer锁或另开Tx。
- 下一步：交cleanup稳定实际源窄审，并准备首条真实隔离链。纯控已证输入投影/拒绝/clone、Consumer策略在reserve前拒绝、Stop保留原held preflight至实际返回、唯一终帧及默认nested日志；没有证明实际Exchange/SQL同Tx/lease退休。生产ConsumerAuthority仍unbound。真实资源窗口尚未授予；任何Go前私有telemetry off、去旁路、只读shared mods、自有cache及同进程fresh≥5GiB。
- app/defaultroot、旧 wire/标准 DTO、tools 独立 STOP、Object runtime join STOP 与 Agent F1 边界保持。旧任务记录留原 topic，不复制入本任务 current。

最近实际检查：`.agent-state/model-text-runtime/core-checks.py core-01`，session72425/outer532825 actual0；ordinary Go532828、race Go533336、vet Go534044实际Wait0。普通/race各8top，包0.029s/1.048s；三阶段fresh分别5529034752/5476339712/5416013824B，原组absent、无adopted、runtime/组双空。原件 `output/ai/model-text-runtime/core-01/{ordinary.jsonl,race.jsonl,vet.jsonl,result.json}`。boundary-01原15top证据保持，不重复；无源码编译返修或自动重跑。后续Go仍须新空间门。
