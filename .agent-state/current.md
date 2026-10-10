# Agent 配置核心当前检查点

- 树：`/workspace/agenteam-agent-configuration-core`，分支 `ai/agent-configuration-core`，创建基线 main `728cd45a`。Git 全由 root 操作，Agent 源/00032/本卡与本文件由 content 唯一写。
- 目标：完整 Human Owner Agent 配置核心；旧 C1 六源保持。root 已导入 Model 配置选择与 Secret Directory 六个只读前置源，当前不能把 metadata 能力视为引用写入或 Agent 初始化。
- 四共享新契约已格式化冻结并交 root：`contract/{command_identity,configuration,skill_initialization,tool_configuration}.go`。无新增上层依赖，Registry/Skills 仅消费正式保存版本。
- 第二可恢复片段：`db/migrations/00032_agent_configuration.sql`、`internal/central/agent/{store,repository,authority,owner_authorities}.go` 与 D10 主卡 §9。持久 planned command、完整 canonical pre/postimage、same-Store 私有 writer witness 和 Skills/Tool/Secret owner 回调已落源；未导出可伪造 witness 构造器，不写外域表。
- 新增严格创建/更新/receipt/Lookup 契约与4个基础测试 top 已保存（5a461523）；Unknown/grant 两个静审 must-fix 已窄修保存（ff6a010d）并独立静审接受，新增单top仍未运行。Model refs contract 已由root从1a235精确同步，只读消费。
- 第四片段11新技术源及两记录已由root保存为 de6da040，且独立静审有限接受：Agent `calls/calls_test/current/events/model_authority/mount_authority/reader/service` 与 contract `events/events_test/mount_configuration`。包括真实required依赖构造、Get/Lookup、实际函数退休、completed创建receipt门及原ctx的typed producer/owner checker；没有Mount实现，不开放默认root。
- 第五片段候选：7新源 `command_plan.go`、`command_store.go`、`command_dependencies.go`、`command_audit.go`、`commands.go`、`command_plan_test.go`、`command_lifetime_test.go`，另 `repository.go` 接入严格持久请求与完整计划关系校验。Create/Update 已调用正式 typed ports，真实 canonical→refs/创建初始化→Audit/Outbox→Activity→receipt 在同 final Tx；任何失败由原 Store 整体回滚。默认 Tool 真 ID 先写入持久 revision 再发现 refs；completed replay 不重初始化。原 Unknown 仅一次独立≤3s确认，Stop取消但仍等原物理调用返回；新生命周期控明确为受控 CommitResult，不冒真实 SQL。
- 本轮只有 gofmt 与 diff whitespace 静态检查，实际 exit 0；尚未 Go 编译/测试、DDL/PG、native 或其他真实资源。源码是 WIP，不是 F1 接受。
- 下一步：等待共享 Audit/Project writer freeze，执行本 Agent/contract 两包的新10top基础 pure/race 与定向 vet，保首编译/测试真实结果；再准备同 Store 真实链。Model 双角色引用、Secret 引用、Mount 与 Agent 事件闭集仍未全部组装或验收；缺 provider（含 false/空集）必须 unbound、不提交。00033 Registry/00034 Skills 归各 owner，不在本树代写。
- App/HTTP、Task 正向 Agent 绑定、Execution、动态 assignment、删除、Project participant、Object Runtime STOP 均未开放。长 Go 与真实窗口待 root 单独调度；当前本代理无运行进程/资源。

## Mount 配置 provider 候选

- root 分配独立 `internal/central/mount/**` 与 `00035_mount_configuration.sql`，不改当前首次检查的 Agent/contract、Audit/contract、Project 四包。新增 `store.go/types.go/repository.go/configuration.go/configuration_test.go` 已格式化；本段与 D10 §10 一并冻结待 root 保存。
- `NewConfiguration(Store, ProjectAuthority, MountReferenceOwnerAuthority)` 实际消费 Agent 的既有 Mount 窄口；Discover 收齐 Command/User/Project/Agent 与 owner 的原锁并集，final 只核原 Store Tx/held locks/current Owner。Apply 必须收到真实 Agent canonical writer 的原 ctx/Tx witness，再写本域 head/完整 refs。创建 `[]` 实际建立 version1 head；后续 Agent-only 升版同样推进该 head，缺 head/旧集合不符拒绝。
- 已有定义按同 Project/Agent、active、逻辑 workspace 与原版本重验；不读 Runner 在线状态或宿主路径。MountCreate、普通 Owner 选择系统 Runner 的目录权限、physical ensure 与非空端到端创建仍未绑定，不把受控定义行当真实创建验收。
- 4 个基础测试 top 已落源码：空集实际 head/原 witness；计划/当前权限/旧集合拒绝；已有定义变化/失效/安全 workspace；原 Unknown/cancel/安全存储错误。其 Store/Owner 明确受控，不代表真实 PG、真实 Session 或真实 Agent witness。仅 gofmt/diffcheck 实际0；尚未编译/运行或执行00035。新包无后台工作，所有原调用由调用方生命周期拥有。
