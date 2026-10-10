# Agent 配置核心当前检查点

- 树：`/workspace/agenteam-agent-configuration-core`，分支 `ai/agent-configuration-core`，创建基线 main `728cd45a`。Git 全由 root 操作，Agent 源/00032/本卡与本文件由 content 唯一写。
- 目标：完整 Human Owner Agent 配置核心；旧 C1 六源保持。root 已导入 Model 配置选择与 Secret Directory 六个只读前置源，当前不能把 metadata 能力视为引用写入或 Agent 初始化。
- 四共享新契约已格式化冻结并交 root：`contract/{command_identity,configuration,skill_initialization,tool_configuration}.go`。无新增上层依赖，Registry/Skills 仅消费正式保存版本。
- 第二可恢复片段：`db/migrations/00032_agent_configuration.sql`、`internal/central/agent/{store,repository,authority,owner_authorities}.go` 与 D10 主卡 §9。持久 planned command、完整 canonical pre/postimage、same-Store 私有 writer witness 和 Skills/Tool/Secret owner 回调已落源；未导出可伪造 witness 构造器，不写外域表。
- 本轮只有 gofmt 与 diff whitespace 静态检查，实际 exit 0；尚未 Go 编译/测试、DDL/PG、native 或其他真实资源。源码是 WIP，不是 F1 接受。
- 下一步：严格创建/更新/receipt/Lookup codec、服务原幂等与 Unknown/Stop/Drain 链、完整 provider 与 typed Event/Audit 适配和必要测试。Model 双角色引用、Secret 引用、Mount 与 Agent 事件闭集仍有真实缺口；缺 provider（含 false/空集）必须 unbound、不提交。00033 Registry/00034 Skills 归各 owner，不在本树代写。
- App/HTTP、Task 正向 Agent 绑定、Execution、动态 assignment、删除、Project participant、Object Runtime STOP 均未开放。长 Go 与真实窗口待 root 单独调度；当前本代理无运行进程/资源。
