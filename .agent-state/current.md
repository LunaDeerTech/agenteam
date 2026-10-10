# Agent 配置核心当前检查点

- 树：`/workspace/agenteam-agent-configuration-core`，分支 `ai/agent-configuration-core`，创建基线 main `728cd45a`。Git 全由 root 操作，Agent 源/00032/本卡与本文件由 content 唯一写。
- 目标：完整 Human Owner Agent 配置核心；旧 C1 六源保持。root 已导入 Model 配置选择与 Secret Directory 六个只读前置源，当前不能把 metadata 能力视为引用写入或 Agent 初始化。
- 四共享新契约已格式化冻结并交 root：`contract/{command_identity,configuration,skill_initialization,tool_configuration}.go`。无新增上层依赖，Registry/Skills 仅消费正式保存版本。
- 第二可恢复片段：`db/migrations/00032_agent_configuration.sql`、`internal/central/agent/{store,repository,authority,owner_authorities}.go` 与 D10 主卡 §9。持久 planned command、完整 canonical pre/postimage、same-Store 私有 writer witness 和 Skills/Tool/Secret owner 回调已落源；未导出可伪造 witness 构造器，不写外域表。
- 新增严格创建/更新/receipt/Lookup 契约与4个基础测试 top 已保存（5a461523）；Unknown/grant 两个静审 must-fix 已窄修保存（ff6a010d）并独立静审接受，新增单top仍未运行。Model refs contract 已由root从1a235精确同步，只读消费。
- 下一可恢复片段为11新源：Agent `calls/calls_test/current/events/model_authority/mount_authority/reader/service` 与 contract `events/events_test/mount_configuration`。包括真实required依赖构造、Get/Lookup、实际函数退休、completed创建receipt门及原ctx的typed producer/owner checker；没有Mount实现，不开放默认root。
- 本轮只有 gofmt 与 diff whitespace 静态检查，实际 exit 0；尚未 Go 编译/测试、DDL/PG、native 或其他真实资源。源码是 WIP，不是 F1 接受。
- 下一步：Create/Update 原幂等与 Unknown 原子提交链、完整 provider 与 Agent Audit 适配和必要测试。Model 双角色引用、Secret 引用、Mount 与 Agent 事件闭集仍有真实缺口；缺 provider（含 false/空集）必须 unbound、不提交。00033 Registry/00034 Skills 归各 owner，不在本树代写。
- App/HTTP、Task 正向 Agent 绑定、Execution、动态 assignment、删除、Project participant、Object Runtime STOP 均未开放。长 Go 与真实窗口待 root 单独调度；当前本代理无运行进程/资源。
