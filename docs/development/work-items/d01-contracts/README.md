# D01 契约索引与依赖

本组文档是 [D01 修订 1 / C01](../d01-cross-module-contracts.md) 的设计交付，基线 `19d300f`。业务规则以[架构](../../../architecture/README.md)及 [AT-0005 处置映射](../d00-baseline-audit-report.md#7-at-0005-决策处置映射)为依据；这里确定跨模块工程边界，不创建代码、数据库表或产品验收结论。

**本目录全部端口当前均未实现、未绑定。** 下表的“提供/绑定”是后续工作责任，不是能力已可调用。模块隔离测试可用显式测试替身；生产组合根缺少必需实现时报告 `DEPENDENCY_UNBOUND`，相关功能不能 ready、不能回退成功或空结果。

## 文档职责

| 文档 | 唯一负责的共同契约 |
| --- | --- |
| [基础与访问](foundation.md) | ID、时间、HTTP、身份/权限、幂等、事务、锁顺序、协议演进 |
| [领域与生命周期](domain-lifecycle.md) | 核心身份、状态归属、工作占用、Project/Meeting 删除、引用保留 |
| [资源与技能](resources-skills.md) | Object/Artifact、Agent 配置、Skills、Knowledge/Memory、目录端口 |
| [模型与工具](model-tool.md) | Model Request/Stream、逐 attempt 计量、Tool/Approval、Runner/MCP 边界 |
| [执行与编排](execution-orchestration.md) | Launch、只读 Launch 查询、Trigger、等待/恢复、技能输入确定点、Scheduler、Meeting 摘要 |
| [事实与实时](runtime-events.md) | Event/Audit/Transcript/投影分工、Outbox、Runtime 快照/流、水位、Inbox |
| [接口走查](walkthroughs.md) | 正常、拒绝、冲突、等待、未知结果、竞争、恢复验收输入及预期 |

这些文档中的 IDL 是拟实现的类型与签名规范。`Context` 是 Go `context.Context`；`Result<T>` 表示 `(T, error)`，错误只用所列稳定类型；`T?` 表示显式可空；`[]` 为有序数组；集合须标注规范排序。所有命令都有明确 Actor，不以 `Context` 的任意字符串 value 代替身份。领域内部表、SQL、限额数值、全部页面 API 仍由责任模块开工规格落实。

## 事实所有者与关联

| 事实 | 提供者 | 跨模块稳定关联 | 消费者 |
| --- | --- | --- | --- |
| User、Session、系统管理员资格 | D07 Identity | `user_id`、`session_id`；Project 只保存 Owner ID | D04–D05、D08、D17、D25–D27 |
| Project、Owner、名称路径、生命周期门禁 | D08 Project | `project_id`、`owner_user_id`、`version`；名称不作外键 | 所有项目模块 |
| Milestone、Sprint、Task、Blocker、TaskEvent | D11 Work | `project_id → milestone_id → sprint_id → task_id` | D21–D24、D25、D27 |
| Agent、能力、分配、项目变量；Skill/Revision | D10 Agent/Skill | `agent_id`、`skill_id + revision`、分配 change ID | D09、D15、D18–D24 |
| Model 配置、ResolvedModel、Invocation/Usage | D09 Model | live 可空引用 + 不可变身份/参数快照 | D10、D13–D14、D19、D21–D24 |
| StoredObject、受控字节引用；Artifact | D05 Object/Artifact | `stored_object_id`、业务 owner reference | D07、D10、D12、D17–D24 |
| KnowledgeDocument 与当前树 | D12 Knowledge | `document_id + content_version`，当前 parent 单独查询 | D13、D21、D24、D27 |
| Serving generation、IndexProfile | D13 Retrieval | `profile_id + generation_id`、源内容版本 | D12、D14、D21 |
| Memory canonical 内容、revision、namespace | D14 Memory | `(project_id, agent_id, memory_id)` | D21、D27；不存在跨 Agent namespace 默认访问 |
| Runner、Mount、连接与设备能力 | D15–D17 Runner | `runner_id`、`mount_id`、协议 attempt identity | D10、D18、D21、D28 |
| ToolSpec/Binding、Operation/Attempt/Result | D18 Tool | `tool_id + spec_revision`、`operation_id`、`attempt_id` | D19–D22、D25 |
| ApprovalRequest、一次性/可复用 Approval | D19 Governance | `approval_request_id`、`operation_id + fingerprint` | D18、D22、D24–D27 |
| MCP Config、Project Connection、目录/lease | D20 MCP | `config_id`、`connection_id`、runtime lease | D10、D18、D21–D22 |
| Execution、slot、Snapshot、Transcript、Checkpoint | D22 Executor | `execution_id`、typed Trigger、Launch key | D11、D18–D19、D23–D27 |
| Dispatch、SchedulerTaskRuntime、调度额度 | D23 Scheduler | `dispatch_id`、固定 Task/Agent/Sprint、Launch key | D11、D22、D25、D27 |
| Meeting、Turn、Contribution、Message、Decision、Summary | D24 Meeting | 四项 Trigger 引用；immutable Message/generation | D22、D25–D27 |
| Runtime/Timeline/Inbox read model、订阅 | D22/D24 生产；D25 集成 | 来源 ID、版本/水位；不反向成为业务事实 | D26–D27 |

跨模块历史引用不统一强制 live 外键：可删除来源保留 `source_type + source_id` 和必要安全快照，投影为 `source_deleted`。该状态不能授予已删资源正文访问。永久 Project 清理边界优先于项目内单个来源的历史保留，见[删除矩阵](domain-lifecycle.md#删除与保留矩阵)。

## 代码依赖与运行时依赖

拟采用 `internal/central/<领域>/contract` 保存该边界的公开类型/端口；领域实现与调用者共同依赖它。基础 `internal/central/foundation` 仅包含无业务的 ID、时间、错误、CommandMeta、事务句柄；不得纳入 Task、Meeting、Tool 等领域实体。`internal/runnerprotocol` 仅包含两端通信 DTO，不 import Central。目录随责任模块实际实现才创建，不生成空包。

契约的单向分层如下；同层不得隐式互相导入，复杂跨边界组合类型由上层契约或组合根适配器拥有：

| 层 | 契约包 | 允许直接依赖 |
| --- | --- | --- |
| 0 | `foundation`、`runnerprotocol` | 标准库；两者无领域依赖 |
| 1 | `identity/contract`、`event/contract` | foundation |
| 2 | `object/contract`、`project/contract`、`secret/contract` | 0–1；对象契约自己的清理结果由生命周期适配器转换，不反向 import Project；Secret 拥有稳定凭据引用及 lease DTO |
| 2 | `outbox/contract` | 仅 foundation、event/contract、identity/contract；拥有需 Actor/授权的 Append、Producer、Project、Handler、投递/生命周期/诊断端口；中立 Header/Event/EventType、codec 与纯 typed schema catalog 仍在第 1 层 event/contract，仅依赖 foundation，不 import 领域实现；具体实施见 [D06](../d06-transactional-outbox-design.md) |
| 3 | `model/contract`、`runner/contract` | 0–2；runner 可依赖 runnerprotocol；Model 自有消息 DTO 不 import Tool |
| 4 | `agent/contract`、`skill/contract`、`knowledge/contract`、`retrieval/contract`、`memory/contract` | 0–3；本层之间仅传稳定 ID/本领域 DTO，跨类型转换在适配器 |
| 5 | `tool/contract` | 0–4；包含 ToolAuthorization/ScopeResolver 与中立 WaitRef，不能 import Governance/Executor 实现 |
| 6 | `execution/contract` | 0–5；包含通用 Trigger/Wait/Occupancy/SkillIngress/Runtime 端口，不能 import Work/Meeting/View 实现 |
| 7 | `work/contract`、`scheduler/contract`、`meeting/contract`、`governance/contract`、`view/contract` | 0–6；同层以低层中立端口及组合根适配，不互引实现 |

[R1 低层资源身份](../d01-resource-identities.md)已在层 1 `identity/contract` 唯一定义 `ToolID`、`MountID`、`ProjectVariableID` 的 marker 与 Foundation alias。业务事实仍由 Tool、Mount 和 Variables owner 维护；Secret 变量沿同一变量身份，CredentialID 保持独立。ID 声明不提供目录、授权或引用保护，不允许 Agent 向上导入 ToolCatalog，RunnerProtocol 仍不 import Central。

`ProjectLifecycleParticipant` 放在 project contract，使用 `ScopeRef/StopReport/CleanupReport`，不返回 Execution、Meeting 实体。`WorkOccupancy` 放在 execution contract，仅接受稳定 Project/Task/Sprint ID 与调用方已验证的 Task 集；不 import Work。`PendingDispatchReader` 同在 execution contract，返回 Dispatch ID 与状态投影，不 import Scheduler。`TriggerContextProvider` 和 `WaitFactProvider` 同样由 execution contract 定义；Work/Meeting/Governance 实现接口，不要求 Executor 引用其实现。

`SkillAssignmentIngress` 放在 execution contract；Skill/Agent service 不直接依赖 Executor service。D10 服务内部定义接收固定 change DTO 的窄出口，由组合根适配为 D22 ingress；D22 的当前分配验证回调使用 skill contract。skill contract 自有 `SkillExecutionBindings` 窄查询接口，D22 提供适配实现以解析固定 binding；不让 Skill import execution contract。这样的运行时双向调用不等于代码环：两个实现不导入对方，转换器位于 Central 组合根。调用的事务/锁约束仍必须遵守，不用接口化掩盖递归调用。

| 运行调用 | 为什么需要 | 消除环的固定边界 | 真实组合验收 |
| --- | --- | --- | --- |
| Work → Execution/Scheduler | Complete/Move/Delete 占用保护 | `WorkOccupancy`、`PendingDispatchReader`，共享 Project 调度门禁锁 | D22/D23 |
| Scheduler → Work → 占用查询 | claim/reconciliation/失败补偿 | Work 命令显式接同一 Tx，查询只读、不回调 Scheduler mutation | D23 |
| Tool → Governance → Tool 元数据 | 逐调用授权和 Scope | tool contract 定义授权/ScopeResolver；Governance 不执行 Backend | D19，D21/D22 集成 |
| Tool/领域 → Executor；Executor → Tool | waiting/cancel/执行 | 可信执行身份 + `WaitRegistration`/控制接口；无 Loop 指针回传 | D22 |
| Meeting → Executor → Trigger/Wait Provider | 启动、决策与恢复 | execution contract Provider registry；D24 注册实现 | D24 |
| Skill 分配 → Executor → Skill 当前分配校验 | 下一轮可靠新增 | 同 Tx ingress + Agent gate；输入确定点查询正式分配 | D22 |
| Project → 各生命周期参与者 → Project gate | 阻断、停止、清理 | gate 检查允许内部收敛动作；参与者不调用外层生命周期命令 | D08 框架，各域绑定，D28 全量 |
| 事件生产者 → Outbox → View → 领域查询 | 投影与通知 | event contract；消费者只读 canonical 或调用独立幂等命令 | D06/D25 |

组合根位于 Central 启动装配，负责注册、适配与 mandatory dependency 检查。HTTP handler 不承担组合根职责；Runner 使用自己的装配，不依赖 Central 业务服务。依赖缺失先影响相应能力，不能把未接外部依赖的进程报告成完整产品 ready。

## 实现与绑定责任目录

| 工作项 | 本组契约的实现责任与真实绑定门槛 |
| --- | --- |
| D02 | foundation 类型、HTTP Problem、request tracing、进程/取消与组合根；不提前创建领域空实现 |
| D03 | 统一 Tx、锁 namespace、SQL 唯一性/提交未知分类、隔离数据库测试 |
| D04 | Secret lease、出站策略、Audit；D07/D08 补身份组合 |
| D05 | StoredObject/Artifact、引用保护/清理、传输 grant；D07/D08 补权限，D17 补直传 |
| D06 | typed Outbox、handler delivery、去重/重投；D25 补投影 bootstrap |
| D07 | Session/CSRF/System scope、撤销通知、用户身份；D17/D25 补长连接撤销 |
| D08 | Project Owner/路径/gate/生命周期进度；D10 绑定初始化技能；后续各域注册清理 |
| D09 | ResolvedModel、ModelCall/Stream、逐 Invocation 计量、唯一自动请求重试 |
| D10 | Agent canonical/config validation、变量、Skill 安装/分配/目录；后续 D15/D18/D20 补真实目录，D22 补运行绑定 |
| D11 | Work 状态命令、Tx 保护和 TaskEvent；D22/D23 绑定真实占用端口 |
| D12 | Knowledge 内容/树/确认范围、引用读取、删除与索引失效输入 |
| D13 | serving generation/profile、混合检索与索引投递；D14 补 Memory scope |
| D14 | Memory namespace、敏感输入、原子 proposal batch、一次已知冲突重算 |
| D15 | Runner enrollment/身份/连接/控制协议、目录与 Mount 身份 |
| D16 | Runner Workspace/path、文件命令进程、真实停止结果 |
| D17 | Data Channel、技能对象直传、Desktop/Tunnel、Session/Owner 代理授权 |
| D18 | Registry/ToolSpec/Binding、Operation/Attempt、统一 Runtime；D19 绑定授权 |
| D19 | ToolAuthorization/Scope、ApprovalRequest/Grant/WaitFact；D22 绑定恢复 |
| D20 | Project MCP Connection、发现/spec revision、credential/runtime lease、协议一致性 |
| D21 | 既有领域工具适配与 Skills 读取/准备/安装/分配；按所列正式端口接真实 Backend |
| D22 | Executor/Loop、Launch 查询、slot/等待/技能 ingress、Transcript/Checkpoint、Runtime producer |
| D23 | Dispatch、额度交接、串行 recovery、Work/Execution 真实事务组合 |
| D24 | Meeting Trigger/Decision/Turn、Summary 输入与生命周期、Meeting Tool 注册 |
| D25 | Realtime/Runtime/Inbox/Timeline 投影装配、快照竞态与撤销收敛 |
| D26 | typed HTTP/WebSocket client、Session、取消与错误/冲突策略 |
| D27 | 正式页面与管理入口，复用相同命令/错误/生命周期；全局搜索另立产品设计 |
| D28 | 全部 mandatory binding、真实存储/协议/恢复/部署组合验收 |

依赖未来模块的自身核心逻辑在当前模块完成，测试替身须表达拒绝/等待/未知/竞争；真正提供者完成时增加适配器及组合证据。不得以“日后绑定”延后自身状态机、持久化或异常处理。

## 变更控制与未定范围

跨模块字段、枚举、授权、状态或幂等含义变更，先提高本规格修订并列出提供者、消费者、旧持久化数据/事件/快照和 Runner 兼容影响，再修改实现；协调双方契约测试及重跑受影响走查。兼容新增字段也须确认默认值不会扩权。破坏性变更新 schema/protocol major 或显式迁移，不原地解释旧历史。

D01 不留待选的跨底层共同产品决定。D07 密码/挑战参数、D10 包格式资源限额、D13 词法实现及评测、D15–D17 协议帧/传输限额、各模块 SQL/索引和 D26–D27 全部路由 DTO 等是已明确责任的后续开工工程细节。实际支持 Provider、Runner Desktop、MCP 版本与部署依赖必须经对应模块验证，静态契约不证明兼容。任何需要改变既定产品含义的发现交回主线程，不在实现中默认补造。
