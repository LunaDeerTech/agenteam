# Skill 当前分配与 Execution pending ingress

状态：以 main `19df3fea` 建立独立源码候选，尚未编译、自测或真实联调。交付边界是当前 Human Owner 的 Skill 分配命令及其同事务持久 ingress；不执行 Model，不产生下一轮 RoundInput，不改变默认生产未绑定及已有 STOP。

## 正式依据与分域

依据 [资源与技能：分配、目录与读取](d01-contracts/resources-skills.md#分配目录与读取)、[执行与编排：技能变更与下一轮输入确定点](d01-contracts/execution-orchestration.md#技能变更与下一轮输入确定点)及 [Agent 配置](d10-agent-configuration.md)。分配的 expected_version 是唯一 Agent.config_version，当前分配序列是 Skills 的独立单调序列。

- Skill 负责人 `skill_package_review`：分配合同、命令/重放/当前读取、原初始化与初始 capture 兼容、固定 revision 保护、本卡与 current。全局前向迁移 `00067` 仅此作者写入；不改旧 00034/00051。
- Agent 负责人 `skill_assignment_agent`：真实 Owner 授权、同 Store 私有 plan/live Tx witness、完整配置 version 推进。Tool、Mount、Model、Secret 对应引用的 version 推进由各事实 owner 的窄端口完成；`skills_milestone_review` 负责这四域接缝。Skill 不写 Agent 或四域私表。
- Execution 负责人 `recovery_lead`：真实 active slot 观察、immutable queued/no_active ingress receipt 与 pending。同事务调用 Skill 引用 owner，不实施 DetermineRoundInput，不改 ToolResult consumer 或 Transcript。
- `runtime_recovery` 独立审查；root 负责所有 Git、迁移号协调和实际工具链/联调窗口。

## 命令与恢复

首片只接受当前 Human Owner，使用完整原 DTO、目标 Project/Agent 和原 key。命令身份固定 namespace `project`、owner IDs `[project_id,agent_id]`、命令 `skill.assign` / `skill.remove`。历史 receipt 只证明原命令，读取和重放仍先检查当前 Owner/Project/Agent；相同 key 不同完整意图拒绝。

Assign 接收 SkillID 与显式 enabled；Remove 接收原 AssignmentID。先检查原命令重放，再检查 expected Agent version。已有 enabled grant 的相同 Assign 不创建新授权或推进版本；配置真正改变时 Agent version 与 Skill sequence 各推进一次。已禁用 grant 重新启用采用新 AssignmentID，旧 identity 保留为历史；Remove 后再 Assign 同样创建新 identity，迟到旧 change 不能恢复授权。Remove/disable 只撤销当前配置，不取消运行、不删除旧上下文和历史保护引用。

启用的新 grant 在 Skill gate 内捕获该 Skill 的实际 immutable current revision、目录 metadata 与 package SHA；分配当前配置本身仍指向稳定 Skill。调用 Execution ingress 必须与 Agent version、完整引用版本、Skill assignment/change、command receipt 及适用的审计事件同事务提交。端口缺失或任一步失败使整个事务失败；Unknown 保原 key 和完整意图，仅显式 Lookup 原持久事实，不自动重发。

## 同事务端口

`skill/contract/assignments.go` 保存 Skill 自有的 `RuntimeAssignmentChange`、`AssignmentRuntimeSink` 与 `AssignmentChangeAuthority`。Execution 可直接实现窄 sink，无 Skill → Execution 实现依赖。

原调用先预收完整锁 union；至少含当前 User、命令、Project SH、ProjectSchedule EX、Agent EX、目标 Skill SH，以及 Agent 各引用 owner 的实际锁。Agent discovery 不以旧 expected_version 拒绝可恢复重放；final 仍重验当前授权、canonical Agent 与原 plan。Agent 提供两个同步 callback，不向 Skill 借出版本推进能力：prepare 在真实授权后先查原 key，已完成重放返回 false；非重放才校 expected，无变化可写新 key 的 no-op receipt 并返回 false；实际变化仅冻结 Skill 变更候选并返回 true。Agent 自己完成 CAS 与四域引用版本推进且全部实际返回后，才调用 finalize(new AgentRef) 写对应 Skill mutation/change/receipt/ingress。四域仅变更所需的检查不能提前阻断合法的已完成历史重放。

Ingress 不 Acquire 新锁、不另开 Tx、不调用 Model。Agent EX 与 Launch、preparing、终态释放的同 Agent gate 排序。active 的唯一事实是 created/preparing/running/waiting 的 canonical slot；cancel_requested 不等于 free。没有 active 时必须由真实观察得到 `no_active_execution`，不能把 nil provider 或读取错误当空闲。

每个 change_id 的 ingress receipt 不可变：第一次的 queued Execution 或 no_active 决定原样重放，后来启动的新 Execution 不能吸收旧 change。queued 行仅表示 pending，不代表已进入某轮或 waiting 已恢复。

`RequireAssignmentChangeInTx` 读取同 Tx 实际 Skill change 并核完整 DTO；`ProtectAssignmentChangeInTx` 写入/重验 Skill 自域的 exact Execution/change/revision/package/Object 映射。新表通过真实 immutable revision 身份外键保护，原 revision → Object 引用继续由 Skill 持有，清理必须检查这些保护行。不存在 caller 自报 DTO 即获得引用或授权的路径。

## 初始化与当前集合兼容

旧 `AgentInitializationReceipt` 的 sequence=1、初始 assignment identity 和创建 provenance 保留；不能改写已提交 receipt 或初始 capture digest 来冒充动态配置。旧初始化记录的历史读取与新的当前 assignment 集合读取分开校验。新增 current sequence 与授予/移除事实，不以多出一行破坏原创建 receipt。

当前 `loadAgentInitialization` 与 `loadSkillCaptureSource` 仅支持初始化的 0/1 集合，`execution_binding_heads` 亦限制 binding_count 为 0/1。本片必须同步扩展实际当前集合和新 preparing capture，对所有当前有效分配按 SkillID 唯一排序、捕获合法 current revision 并保护原映射；无动态变更的旧编码保持兼容。历史 Execution capture 不随之后分配漂移，旧 initial-only RoundInput 读取不得被宽松 current 查询替代。新增 pending 的应用、cutoff、skills_added ControlInput 与下一轮 Transcript 留后继正式 DetermineRoundInput 片。

## 最小真实链与未验证范围

计划先用真实 Owner、Agent、已发布 Skill 和 Execution Launch(created)，不调用 Model：正式 Assign 同时推进版本/序列并写 pending/ref；同 key Lookup 不重复；真实无 active 返回并持久 no_active；sink 失败回滚整个命令。随后检查初始化重放/新 preparing 当前集合与固定历史 capture，及 Remove/重新分配的新 identity。纯替身不代替 Agent 当前授权、Execution slot 或 revision/Object 引用。

目前只有源码/接口准备，没有动态通过结论。工具链和真实 PG 仅由 root 在实际资源窗口安排。原 Object runtime join、OpenAI tools A/B、SPA concurrent-publication、Jina/Image STOP、生产 initializer/默认执行绑定及未完成 Operation/ToolResult 的状态均保持。
