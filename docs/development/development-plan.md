# agenteam 开发计划

本计划用于指导后续 AI 从基础契约开始，按依赖顺序完成 agenteam 的正式开发。推进单位是职责明确的模块；每个模块先设计数据结构、接口和状态规则，再实现、调试和验收，最后接入其他模块。不以提前出现可操作界面作为早期交付目标。

计划依据是[当前架构](../architecture/README.md)、[前端设计](../frontend-design/README.md)和[仓库结构](repository-structure.md)。它规定开发顺序和验收要求，不替代架构专题，也不把尚未确定的实现选择写成既定事实。`docs/draft.md` 仅作历史参考。

## 当前基线与计划范围

规划日期：2026-10-03。核对基线：`fe5b833`。本次执行环境检出分支为 `work`，工作区初始干净；团队文档要求后续正式开发使用 `main`。恢复开发时核对真实分支与基线，由主线程处理环境差异，不让子 agent 自行切换分支或创建 worktree。

已存在的成果：Vue 3、TypeScript、Vite、路由与应用壳、公共组件、主题、开发专用 Debug 展示，以及串行开发团队和项目技能。后端、Runner、迁移、部署与集成测试目录仍是骨架；没有根 Go module 和正式业务 API。已有前端成果应复用，不把 Debug 演示状态当成业务实现。

范围覆盖现有架构文档规定的第一阶段：单 Central、多 Runner、PostgreSQL/pgvector、MinIO、账号与项目、工作管理、模型、Agent、Knowledge/Memory、统一工具、治理、MCP、执行、调度、会议、实时与 Inbox，以及正式前端和部署。Dashboard 仅保留设计规定的容器；全局搜索业务范围未定，不纳入已确定功能验收。首版不新增 Redis、多 Central、微服务、公开注册、项目成员矩阵、跨项目检索或 Runner OS sandbox。

## 推进规则

1. **先定契约，再写实现。** 跨模块边界先统一；当前模块的字段、函数签名、命令、查询、错误、事务和验收场景必须在开工前明确。
2. **模块完成后再推进下一模块。** 一个工作项可以拆成连续的小任务卡，但不能因为正常路径可运行就跳过异常、并发、幂等和恢复设计。未完成卡保留在当前工作项内。
3. **模块完整与产品集成分开验收。** 模块按真实存储及协议契约完成自己的职责；依赖未来模块的地方使用明确端口。后续只增加适配器、注册和集成验证，不回头补此前遗漏的核心逻辑。
4. **设计冻结的是边界，不是全部未来代码。** 不提前创建所有空包、万能 Service 或整套空 handler。模块开工时再落实内部包结构和 SQL，遵守全局迁移序列。
5. **依赖满足才允许开工。** 相邻模块只使用已确认的服务接口、事件或 read model，不直接修改对方数据表。不能为了运行演示引入临时旁路。
6. **验收以证据为准。** 状态机、权限、数据库竞争、外部协议和重启恢复使用适当的行为测试。假实现仅用于隔离测试，不作为生产交付。
7. **修改设计须同步影响。** 主线程先记录原因、受影响模块、兼容与迁移方案，更新架构和任务卡，再实施变更。一般实现选择由主线程在授权范围内决定；产品含义未定或超出范围时才请求用户决策。

## 每个模块的开工与完成标准

每个工作项首先建立模块规格，存入后续新增的 `docs/development/work-items/`，文件使用本计划工作项编号和 kebab-case 名称。详细业务规则继续归位到 `docs/architecture/`，规格引用它们，避免形成第二套规则。

模块规格必须包含：

| 项目 | 必须明确的内容 |
| --- | --- |
| 职责 | 本模块拥有的事实、对外能力、不承担的相邻职责 |
| 数据 | canonical 字段、ID、枚举、引用、约束、索引、version、删除和保留策略 |
| 服务接口 | 具体方法签名、输入输出、调用身份、错误、幂等键与取消语义 |
| HTTP 与协议 | 路径、方法、DTO、状态码、分页、上传限制；不需要 HTTP 的模块明确标注 |
| 事件 | 类型、payload、版本、生产事务、消费者、去重和乱序处理 |
| 一致性 | 事务边界、锁与唯一约束、外部副作用、unknown outcome、补偿或 reconciliation |
| 验收 | 正常、异常、权限、竞争、重试、恢复场景及可执行命令 |
| 依赖 | 已完成证据、未实现端口、集成位置与责任工作项 |

完成门槛：规格与实现一致；该模块当前范围全部实现；适用检查通过；真实依赖的集成证据齐全；无静默成功、无核心 TODO、无伪数据生产路径；操作说明和台账更新；主线程独立审查并创建本地提交。未运行的检查标记“未执行”及原因，不能标记“通过”。外部依赖无法验证时保留“待集成验证”，阻止依赖该能力的阶段通过。

## 依赖顺序与循环依赖处理

默认严格按照下表序列推进。依赖栏表示直接前置工作项；其传递依赖同样必须完成。所有实现工作项还共同依赖 D00 与 D01。

| 阶段 | 工作项 | 目标 | 直接依赖 |
| --- | --- | --- | --- |
| 0 | D00 | 设计与实现基线核对 | 无 |
| 0 | D01 | 全系统模块边界与契约基线 | D00 |
| 1 | D02 | Go 工程、配置与程序生命周期 | D01 |
| 1 | D03 | PostgreSQL 与迁移基础 | D02 |
| 1 | D04 | Secret、出站访问与 Audit 基础 | D03 |
| 1 | D05 | 对象存储与 Artifact 服务 | D04 |
| 1 | D06 | Transactional Outbox 与事件投递 | D03 |
| 2 | D07 | 账号、Session、SMTP 与个人资料 | D04、D05、D06 |
| 2 | D08 | Project 与 Owner 授权 | D07 |
| 2 | D09 | Model System 与 Token Usage | D04、D08 |
| 2 | D10 | Agent 配置与项目变量 | D09 |
| 2 | D11 | Milestone、Sprint、Task 领域 | D06、D10 |
| 3 | D12 | Knowledge 文档与文档树 | D05、D08 |
| 3 | D13 | 索引、混合检索与评测 | D09、D12 |
| 3 | D14 | Agent Memory | D10、D13 |
| 4 | D15 | Runner 身份、控制协议与连接 | D04、D10 |
| 4 | D16 | Workspace、文件、命令与进程 | D15 |
| 4 | D17 | Data Channel、Desktop 与 Tunnel | D05、D16 |
| 5 | D18 | Tool Registry 与统一 Tool Runtime | D05、D10、D14、D17 |
| 5 | D19 | Tool Authorization 与 Approval | D06、D09、D18 |
| 5 | D20 | MCP 连接、发现、工具与资源 | D19 |
| 5 | D21 | 已有领域的 Builtin 与 Runner 适配器 | D11、D14、D17、D20 |
| 6 | D22 | Executor、Loop、Transcript 与恢复 | D21 |
| 7 | D23 | Scheduler 与 Task 执行集成 | D11、D22 |
| 7 | D24 | Meeting 领域与 Turn 编排 | D23 |
| 8 | D25 | Runtime View、Realtime 与 Human Inbox 集成 | D06、D07、D19、D22、D24 |
| 9 | D26 | 正式前端客户端、认证与导航 | D25 |
| 9 | D27 | 正式业务页面与设置 | D26 |
| 10 | D28 | 完整系统集成、部署与交付验收 | D27 |

这里有几组逻辑上的循环依赖，需要在 D01 固定端口，按以下方式实现：

- **Work Management 与 Executor/Scheduler：** D11 通过执行占用、pending dispatch 查询端口实施 Sprint/Task 保护规则。D11 用契约测试验证占用与并发行为；D22/D23 提供真实适配器并做数据库竞争测试。生产装配必须注入真实适配器，不能默认为“没有执行”。
- **Tool Runtime 与 Governance：** D18 依赖授权端口，验证 allow/deny/waiting 的调度行为；D19 实现完整授权和审批规则，再验证真实组合。不在 D18 内复制授权逻辑。
- **Tool Runtime 与 Executor/Loop：** Tool Runtime 接收执行身份、固定快照和控制回调，不 import Executor 内部实现；Executor/Loop 通过端口调用 Runtime。D22 负责等待、恢复、取消的真实执行绑定。
- **Meeting 与 Executor：** D01 固定 TriggerContextProvider 与 Decision 控制输入契约。D22 用受控 Trigger fixture 验证执行引擎；D24 注册 Meeting provider 和 Meeting Tool，验证真实会话流程。
- **事件生产者与 Inbox/Realtime：** D06 完成投递机制，后续模块提交自己拥有的 typed event。D25 实现消费者、投影重建和前端同步；新投影从 canonical Source of Truth 重建，再处理增量事件。D06 明确 handler 注册、失败重投与留存规则，Outbox 不充当 Event Store，不新增通用历史回放框架。
- **Artifact 服务与 Tool：** D05 完成对象和 Artifact 的业务服务，D21 仅提供模型调用适配器；不能让 Agent 直接操作 MinIO 或创建 Signed URL。

端口是正式设计边界，不是临时 stub。测试替身必须覆盖成功、失败、竞争和未知结果，并在台账列出真实绑定的后续工作项。

## 阶段 0 先确定系统契约

### D00 核对设计与实现基线

输入：[架构总览](../architecture/README.md)、所有模块专题、[架构衔接清单](../frontend-design/architecture-follow-ups.md)、前端源码、[团队流程](agent-team/README.md)。

交付：实现清单、设计待定项清单、文档冲突清单、依赖能力与工具可用性清单。区分“已有实现”“仅有设计”“待定”“本阶段明确不做”。核对 Go/Node/PostgreSQL/pgvector/MinIO 的版本约束及测试条件，不直接沿用机器已安装版本作项目决定。

验收：每个一级架构模块都有归属；已有前端组件和 Debug 被正确保留；所有未决项关联到 D01 或某个模块开工门槛。此项不写业务代码。

### D01 固定跨模块契约

交付：模块职责与允许依赖矩阵、公共 ID/时间/错误/分页/version/request_id 约定、服务端口目录、HTTP/Runner/Realtime 契约组织方式、状态与事件归属、并发与删除矩阵，以及版本化变更流程。

必须先确定的关键契约：

- `Project → Milestone → Sprint → Task`、Agent、Execution、Meeting、Knowledge、Memory、Approval、Artifact 的关联身份和事实归属。
- User identity、Project Owner、System scope、Execution capability snapshot 与逐调用授权边界。
- Task、Sprint、Execution、Meeting Turn、ToolOperation/Attempt、Approval 的状态枚举和原子转换责任。
- Agent execution slot、Task active execution、pending dispatch 的占用与查询接口；`created/preparing/running/waiting` 都占用 Agent slot。
- Model Request/Stream、ToolSpec/Binding/Result、TriggerContextProvider、Executor launch/resume/cancel 和等待控制输入的具体类型。
- TaskEvent、Domain Event、Audit、Transcript、Runtime View、Meeting Timeline、Inbox 的职责区别；明确恢复读取哪个事实源。
- HTTP 认证、写请求保护、错误 envelope、cursor、expected_version、幂等以及前端 DTO 的维护方式。
- 模块所需但文档尚未定义的接口细节，列出所属工作项；影响多个底层模块的选择在本项关闭。

不要求在本项预写全部数据库表、函数实现或所有 UI API；每个模块的具体规格在自身开工前补齐。验收用接口走查验证调用方和提供方对正常、冲突、拒绝、等待及 unknown outcome 的理解一致，依赖矩阵无未解释循环。

## 阶段 1 完成平台基础

### D02 工程与程序生命周期

依据：[部署运行](../architecture/platform-infrastructure/deployment-runtime.md)、[仓库结构](repository-structure.md)。

实现：根 Go module、两个入口、Central/Runner 独立装配、配置加载与校验、结构化日志和关联 ID、取消与 shutdown 接口、基础 HTTP 错误处理及测试命令。先记录选用库、版本与理由；Runner 不依赖 Central 业务包。

验收：两个入口可构建，错误配置明确失败，signal 能有序停止；示例配置无凭据。数据库、对象存储尚未完成时，不把服务报告为完整 ready。前端工程保持独立可运行。

### D03 PostgreSQL 与全局迁移

实现：连接管理、transaction abstraction、全局 migration runner、版本及 pgvector 校验、迁移锁、单元/集成测试数据库隔离、mandatory dependency 检查和 read/write 诊断。数据库约束作为正确性保障，不依赖内存锁代替。

验收：空库初始化、重复启动、已有库升级、迁移失败、并发迁移、断连与停机处理；失败不得带着半完成 schema ready。只创建当前模块所需 schema，后续迁移顺序追加。

### D04 Secret 出站与 Audit

依据：[部署密钥](../architecture/platform-infrastructure/deployment-runtime.md)、[出站策略](../architecture/platform-infrastructure/outbound-network-policy.md)、[Audit](../architecture/security-governance/audit.md)。

依次完成三个职责：envelope encryption、部署 master key 与 credential reference；受控 HTTP/TLS/DNS/redirect/响应限制；append-oriented Audit 写入与分页查询基础。Project 查询授权在 D08 接入，不以管理员角色代替 Owner。

验收：密钥缺失与解密错误明确失败；Secret 不进入普通日志或 DTO；private network、DNS rebinding、重定向与凭据作用域按设计验证；Audit 不复制 prompt、Tool arguments 或输出。SMTP 的非 HTTP 访问适配在 D07 明确并实现。

### D05 对象存储与 Artifact

依据：[Object Storage](../architecture/platform-infrastructure/object-storage.md)、[Artifact](../architecture/tool-system/artifact-tools.md)。

实现：StoredObject、引用、上传/下载/预览、对象状态、大小与内容校验、MinIO streaming、数据库与对象写入一致性、未引用对象清理、Artifact 服务、权限解析端口和短期 Signed URL。确定附件、头像、文档、MCP 内容和执行大 payload 的引用契约。

验收：上传中断、对象写成但事务失败、metadata 存在但 payload 缺失、删除仍有引用、跨 scope 访问、大文件不整块加载内存。D07/D08 接入真实身份和项目授权后补做组合验收。

### D06 Outbox 与事件投递

依据：[Internal Domain Events](../architecture/platform-infrastructure/internal-domain-events.md)。

实现：typed envelope、业务事务内 outbox 写入、dispatcher、handler identity、独立 delivery state、at-least-once、去重、retry/dead-letter、排序限制、失败 redelivery 与重启恢复。不同领域 payload 由对应领域拥有；投影从 canonical tables 按模块重建，不通过回放全部历史事件重建系统。

验收：事务回滚无事件；提交后崩溃可重投；重复投递无重复副作用；一个 handler 失败不丢其他 handler 的结果；明确晚注册消费者与 canonical rebuild 的衔接规则，D25 验证实际投影重建。事件不能承担业务事实的唯一存储责任。

## 阶段 2 完成账号与核心业务事实

### D07 账号 Session SMTP 与个人资料

依据：[账号生命周期](../architecture/platform-infrastructure/authentication/account-lifecycle.md)、[SMTP](../architecture/platform-infrastructure/authentication/smtp-delivery.md)。

开工前关闭：密码哈希与策略、Session cookie/有效期/撤销、认证请求保护、reset token 期限与重复申请规则、初始化日志输出失败恢复、SMTP timeout/retry/TLS/出站校验、头像媒体范围与生命周期。

实现：幂等初始化 `admin@mail.com`、登录退出、邀请与原子兑换、固定 24 小时邀请过期/撤销/清理、密码修改与恢复、SMTP 配置/测试/重发、无 SMTP 的受控后台日志投递、显示名/头像/主题偏好。无 SMTP 不阻断 ready；配置后投递失败不暗中改为日志渠道。

验收：重启不覆盖密码；并发邀请与兑换；撤销/过期竞态；reset 单次消费；公开响应不泄漏邮箱存在性；退出后的 HTTP/WS 身份失效；凭据不回显；个人偏好与 Debug 分离。

### D08 Project 与 Owner

开工前关闭：Project canonical 字段、创建、列表、更新 version、stable pagination、soft delete/permanent purge 的阻断和清理契约。依据：[安全与治理](../architecture/security-governance/README.md)、[系统页面](../frontend-design/layouts/system-pages.md)。

实现：Owner 由服务端身份绑定；本人项目查询和资料更新；可复用 Owner 校验；系统管理员的系统配置权限；Project Audit 查询与对象权限真实装配。后续领域删除适配器逐项注册，但本项先完成其生命周期端口和失败规则。

验收：跨用户读取、修改、下载、订阅均拒绝；管理员不能访问他人项目；客户端不能伪造 Owner；版本冲突不覆盖；未注册的必要清理能力不能被当作成功删除。

### D09 Model System 与 Token Usage

依据：[Model System](../architecture/platform-infrastructure/model-system/README.md)、[Token Usage](../architecture/platform-infrastructure/model-token-usage.md)。

实现：System/Project Provider 与 ModelConfig、固定 Provider protocol、selector、credential reference、Resolver 和不可变 snapshot；统一 Chat request/stream/tool calling/reasoning/structured output；OpenAI-compatible Chat Completions 与 Anthropic Messages adapter；embedding、optional reranker、optional image_generation 独立边界与本阶段选定 adapter；Model Invocation usage 持久化与查询聚合。

开工前明确非 chat adapter 的协议及支持范围，不写一个万能 adapter。实现删除与引用替换契约，后续 Agent/Meeting 注册真实引用适配器。未配置 optional selector 与功能调用失败分别处理；不静默换模型，不用 token 估算补填 usage。

验收：流中断、无效 Tool JSON、usage 缺失、错误规范化、取消、超时、配置更新不改变旧 snapshot；两个 Chat protocol 均有 adapter conformance 测试，可用真实 Provider 做受控 smoke。真实凭据从环境读取，不写文档、fixture 或测试输出。

### D10 Agent 配置与项目变量

依据：[Agent](../architecture/agent-management.md)、[项目变量](../architecture/project-work-management/project-environment-variables.md)。

实现：Agent 配置/预设、model_ref/reasoning_effort 校验、description/instructions、Platform Prompt 版本、allowed Tool stable ID、Mount 和 Secret 引用、default/auto/allow 模式、普通变量与 Secret 白名单。Core Agent Tools 不可被普通开关关闭，但每次调用仍授权。

Tool/Mount 引用通过 D01 的目录端口验证，D15/D18/D20 接入真实目录；不可因目录未实现而默认接受未知引用。实现配置引用替换、删除保护与 snapshot 输入读取。

验收：Owner 校验；普通变量与 Secret 的可见性区别；Secret plaintext 不进入 Model Context；删改配置不改写历史 snapshot；配置页面字段与服务一致。当前 Agent 配置不保存可变 runtime 状态。

### D11 Work Management 领域

依据：[Task Domain](../architecture/project-work-management/task-domain-model.md)、[状态机](../architecture/project-work-management/task-state-machine.md)、[Blocker](../architecture/project-work-management/task-blocker-dependency.md)、[Timeline](../architecture/project-work-management/task-event-timeline.md)、[Sprint](../architecture/project-work-management/sprint-lifecycle.md)。

连续任务卡顺序：Milestone/Sprint 结构 → Task canonical/查询/rank → 状态转换与 reviewer → Blocker/依赖图 → Sprint lifecycle → TaskEvent 与 context/query 接口 → 综合验收。

实现强制层级、唯一 Current Sprint、expected_version、request_id replay、终态不可变、审核交接、blocker 原子转换、依赖循环检测、Sprint rollover、事件同事务写入和 Kanban/Explore read model。执行占用和 pending dispatch 保护通过固定端口实现。

验收：Task 必须经过 `in_review` 才能 `done`；重复命令先解析幂等再校验 version；`cancelled` 依赖不自动满足；blocked 最后 blocker 解除与恢复原子化；并发 start-sprint 至多一个 current；有执行/dispatch 时 complete/move 拒绝；领域事件不混入执行流日志。

## 阶段 3 完成 Knowledge 与 Memory

### D12 Knowledge 文档与文档树

依据：[文档领域](../architecture/knowledge-memory/knowledge-document-domain.md)、[知识布局](../frontend-design/layouts/knowledge-base.md)。

开工前关闭：parent identity、根与同项目单父约束、stable sort/分页、移动防环与并发、子树 soft delete 一致性、历史引用投影、内容 version 与 indexing state 分离。

实现：canonical Markdown/text/file 文档、create/update/read/list/preview/download、树查询/调整父节点、确认范围后的子树删除、索引失效与清理端口。每个父节点自身也有正文。

验收：跨项目 parent、循环、并发移动、子树删除期间更新竞争；正文更新后旧 version 不能继续 serving；索引失败不丢正文；权限先于预览和 Signed URL。

### D13 索引与混合检索

依据：[Indexing](../architecture/knowledge-memory/knowledge-indexing.md)、[Retrieval](../architecture/knowledge-memory/retrieval-runtime.md)。

先建立中英混合项目文档 benchmark，记录候选 lexical backend、指标、结果与选择，再引入正式依赖。实现 Markdown/text/PDF/DOCX parser、section-aware chunker、locator、IndexProfile/job/generation、embedding、lexical abstraction、dense + lexical + equal-weight RRF、optional reranker、过滤、结果预算与评测。

验收：scope 在召回前过滤；重复 job、部分失败、删除清理、重启重建；文档变更淘汰旧版本；仅 profile 变化可继续旧 generation serving 再原子切换；模型变化不混用 embedding space。保存可重跑的检索数据集与对照指标，参数由评测确定。

### D14 Agent Memory

依据：[Memory Domain](../architecture/knowledge-memory/agent-memory-domain.md)、[Memory Runtime](../architecture/knowledge-memory/agent-memory-runtime.md)。

实现：`project_id + agent_id` namespace、类型与 revision/provenance、active/superseded/deleted、retain extraction/consolidation、recall、只读 reflect、用户管理与索引更新。使用平台 memory_model_ref，不继承 Agent chat model。

验收：跨 Agent 拒绝；Secret 在任何 model/embedding/index 调用前处理；consolidation 竞争可追踪；superseded/deleted 不召回；reflect 无写入；执行完成不会自动生成 Memory。检索共用 D13 基础，不复制检索引擎。

## 阶段 4 完成 Runner

### D15 身份与 Control Channel

依据：[Runner Management](../architecture/runner/runner-management.md)、[Control Protocol](../architecture/runner/control-protocol.md)。

实现共享 `internal/runnerprotocol/` 类型、协议版本、enrollment/Ed25519/challenge、单连接、WSS outbound、hello/capability/heartbeat、request/response/stream/deadline/cancel、重连、轮换与 pending request outcome。

验收：错误身份、重复连接、版本不兼容、heartbeat 丢失、deadline、断线、响应丢失；断线时 unknown outcome 不能伪装失败后直接重试。顶层状态仅 online/offline/incompatible；Runner 不 import Project/Task/Meeting 业务包。

### D16 Workspace 文件命令与进程

依据：[Workspace](../architecture/runner/agent-workspace.md)、[Execution Runtime](../architecture/runner/execution-runtime.md)。

实现 Mount 到固定路径映射、workspace lifecycle、文件 read/write/edit/list/grep/find、argv/shell、cwd 校验、临时环境注入、stdout/stderr streaming、masking、取消进程树、managed process scope/状态/ring buffer/cursor。

验收：path traversal、symlink containment、越 Mount 访问、并发文件更新、取消与断连、Runner 重启和进程状态、输出 overflow、Secret masking。命令采用 trusted-host：cwd 合法不代表 OS sandbox；不得在文档或 UI 承诺命令无法访问 Workspace 外部。

### D17 数据通道 Desktop 与 Tunnel

依据：[Data Channel](../architecture/runner/data-channel.md)、[Desktop/Tunnel](../architecture/runner/desktop-tunnel.md)。

实现按需 outbound binary channel、一次性认证、checksum/size/progress/cancel/temp cleanup、upload/download/transfer、受控对象存储传输；Desktop capability 探测及 screenshot/automation/browser-use/computer-use 的确定支持范围；expose-port、process binding、TTL、鉴权与关闭。

验收：大文件中断/校验失败/临时文件回收；无桌面设备明确不可用，有桌面环境执行真实能力验收；Tunnel 无权限、到期、Runner 断连和 process 退出关闭。设备可选能力未测试时记录设备限制，不把空方法算实现。

## 阶段 5 完成工具和治理

### D18 Registry 与统一 Runtime

依据：[Definition/Registry](../architecture/tool-system/tool-definition-registry.md)、[Operation/Attempt](../architecture/tool-system/tool-execution.md)、[Result/Backend](../architecture/tool-system/tool-result-backend.md)。

实现 stable ID、immutable spec revision、binding、Execution Tool Set snapshot、model-visible 无冲突映射、JSON schema validation、Operation/Attempt 独立持久化、dispatcher、结果预算/Artifact 外部化、timeout/cancel/parallelism/retry/idempotency。

验收：无效参数不执行 backend；technical retry operation 不变、attempt 改变；新 model call 即使同参仍为新 operation；unknown + non-idempotent 不自动重试；backend 结果与模型可见结果分层；授权端口返回 deny/waiting 时不执行。

### D19 Authorization 与 Approval

依据：[治理](../architecture/security-governance/README.md)、[默认策略](../architecture/security-governance/default-approval-policy.md)、[Scope](../architecture/security-governance/approval-scope.md)、[一次性审批](../architecture/security-governance/one-time-approval-retry-idempotency.md)、[自动审批模型](../architecture/security-governance/auto-approval-model/README.md)。

实现 Capability/Execution Policy/scope/固定规则、Approval Match、default/auto/allow、Tool-defined ScopeResolver、One-time 与 Reusable Approval、Request/approve/reject/revoke、待处理查询、事务与 outbox、统一用户动作 API。提供等待解析端口给 D22，D25 消费审批事件形成 Inbox。

验收：allow 模式仍不能绕过基础权限；auto model 失败进入人工审批；一次性审批绑定 operation/fingerprint 不绑定 attempt；新调用不能复用；Reusable revoke 后立即失配；批准当前等待操作后直接继续，不重复审批；并发批准/拒绝只形成一个决议；所有入口共享同一权威对象。

### D20 MCP

依据：[MCP](../architecture/mcp-integration/README.md)及其 Config/Protocol/Discovery/Execution/Resource 专题。

实现 System/Project Config、Project Connection、credential binding、无认证/static/OAuth 2.1、Streamable HTTP、runtime lease/reconciliation、discovery 原子 diff/schema/revision、Tool adapter、Resource/template catalog、materialization 与对象引用。第一阶段不支持 stdio、Prompts、Elicitation、Tasks。

验收：连接认证与 Tool Authorization 分离；connected 不冒充 discovery 成功；refresh 失败保留上一成功定义；一个 invalid tool 不拖垮有效 tool；新 tool 不扩大已有 Agent 权限；annotation 的 default_enabled 仅初始建议；超时/取消/unknown outcome、credentials、Resource scope 和大小限制均验证。

### D21 领域 Tool 与 Backend 适配

依据：[工具目录](../architecture/tool-system/README.md)、[Artifact Tools](../architecture/tool-system/artifact-tools.md)。

接入已完成的 Project/Milestone/Sprint/Task/Blocker/Agent/Knowledge/Memory/Artifact Builtin、Core MCP Resource Tools、Runner adapters 和 optional generate-image。每个 Tool 调用领域服务，传递固定 operation/request_id 与 expected_version，不复制状态机。Scheduler/Meeting Tool 在 D23/D24 随其服务实现后注册。

验收：逐 Tool 核对 schema、风险、scope、idempotency、错误与结果预算；Builtin/Runner/MCP 走同一授权入口；Core tools 不可关闭但仍隔离；image selector 不可用时不暴露可执行 generate-image。台账维护设计 Tool 目录与实现映射，不漏能力、不注册空 backend。

## 阶段 6 完成统一执行引擎

### D22 Executor 与 Loop

依据：[Executor](../architecture/agent-executor/README.md)和[Agent Loop](../architecture/agent-loop/README.md)全部专题。

将 Executor/Loop 作为一个完整执行工作项，内部顺序：Execution 数据/slot/launch → immutable context/snapshot 与 Trigger provider registry → Transcript/Model context → TurnProcessor/Tool batch → waiting/resume/cancel → budget/compaction/doom-loop/watchdog → checkpoint/restart recovery → 综合验收。这样不把执行生命周期与恢复遗留到未来阶段。

实现幂等 launch、Agent 全局 slot 原子约束、preparing failure、canonical transcript 与完整 call/result pairing、固定 Prompt components、模型流与工具结果、等待对象关联、resume 幂等、安全 checkpoint、压缩原子激活和生命周期查询。Runtime semantic update 端口本项固定，D25 实现展示投影。

验收：Task/Meeting 类 Trigger 并发启动同 Agent 最多一个非终态执行；waiting 保留 slot；terminal 不复活；审批恢复继续同一个执行；取消不暗中重做已有副作用；waiting 内存释放与恢复；请求中断、输出截断、doom loop、context overflow 与压缩无进展；在模型、工具、等待、checkpoint 各边界注入 Central 重启，验证不重复不安全动作。

Execution succeeded 不等于 Task done；Loop 不直接写项目领域表。执行总时长不设架构未规定的强制终止，不添加未经设计的 max turns 或通用 steering queue。

## 阶段 7 完成调度与会议

### D23 Scheduler

依据：[Scheduler](../architecture/scheduler/README.md)、[Loop](../architecture/scheduler/scheduler-loop.md)、[Dispatch](../architecture/scheduler/scheduler-dispatch.md)、[Reconciliation](../architecture/scheduler/task-reconciliation.md)。

实现 Project-local 调度、Current Sprint、固定组顺序与组内排序、并发额度、claim transaction、durable dispatch、Executor launch idempotency、relaunch cooldown、pause/resume、依赖恢复、用户取消保护和重启恢复；注册 Scheduler Tool，真实绑定 D11 占用查询端口与 TaskContextProvider。

验收：同 Task 不重复 claim/launch；pending dispatch 计入并发；launch 响应丢失可查回结果；Current Sprint 切换/complete 与 claim 竞争；paused 不新增派发；用户 cancel 后不无条件 relaunch；Execution 终态不直接推 Task done；两次访问与持久化 dispatch 恢复符合设计。

### D24 Meeting

依据：[Meeting](../architecture/meeting/README.md)下 Domain/Turn/Context/Reference/Timeline 专题。

内部顺序：Session/participant/message/reference → Turn/contribution → sequential/parallel/queue/interrupt → Trigger provider 与 Executor binding → Decision/Approval waiting → retry/regenerate/finalize/summary → Timeline 来源接口 → 综合验收。注册 Meeting Tool 与模型引用替换适配器。

实现 proposed/active/archive、无独立 MeetingProposal、结构化 inline reference 与 mention、文件/Task/Knowledge 引用、Contribution Execution 幂等、Agent busy/waiting_for_agent、Decision answer/skip、统一审批引用、首轮标题和四字段 summary 原子提交、后续 rolling summary、archive/delete。

验收：mention 不自动等于参与编排；sequential 可见前序结果，parallel 使用同轮固定上下文；queue/interrupt、忙 Agent、等待恢复、取消/retry/regenerate；首轮生成失败不半提交、后续不改标题；删除后引用有安全投影；会议不替代 Task 执行或私有审批状态机。

## 阶段 8 完成投影与实时集成

### D25 Runtime View Realtime 与 Human Inbox

依据：[Runtime View](../architecture/agent-executor/runtime-view.md)、[Realtime](../architecture/platform-infrastructure/realtime.md)、[Inbox](../architecture/platform-infrastructure/human-inbox.md)、[Meeting Timeline](../architecture/meeting/meeting-timeline-realtime.md)。

依次完成：Runtime Item snapshot/update/detail/recovery → authenticated WS subscription/fan-out/backpressure → Inbox stable identity/action routing/rebuild → Task/Meeting/Approval 联合投影。平台只负责传输和投影，不复制领域状态机。

开工前补齐：本人跨项目 Inbox 聚合、稳定分页/订阅授权、系统运行信息 read model、不可信 reasoning 与敏感 payload 的展示范围。

验收：snapshot/stream race、断线与 Central 重启后 refetch、重复/乱序更新、背压、权限丢失/对象删除；Inbox 重建不重复，dismiss 规则正确，Approval 不可忽略；动作重新校验来源状态；历史与执行日志查询不混用。Realtime 不提供业务 correctness 所需的可靠消息保障。

## 阶段 9 完成正式前端

### D26 客户端认证与应用框架

依据：[前端开发](frontend/README.md)、[应用框架](../frontend-design/layouts/application-shell.md)、[账号入口](../frontend-design/layouts/account-entry.md)、[项目工作区](../frontend-design/layouts/project-workspace.md)。

实现统一 typed API client、身份/session、错误/version 冲突、取消与加载状态、realtime 同步/refetch、对象深链接、系统与项目双导航、账号页面、个人设置。复用当前 UI components、token 和主题服务；保持 Debug 仅开发构建。

验收：真实登录/退出/过期与导航恢复；API 错误不显示保存成功；直接对象链接优先；项目导航不污染系统页面；主题持久化；生产资源不含 Debug。运行工程检查，并进行真实浏览器键盘/焦点、浅深色、窄屏、reduced motion/overflow 检查。

### D27 业务页面与设置

依据：[前端布局索引](../frontend-design/README.md)。

连续完成页面组，每组完整验收后再下一组：系统项目列表/项目工作区 → 三类设置（个人已由 D26 完成，补齐系统/项目）→ Task Kanban/Explore/detail → Knowledge tree/editor/preview → Meeting composer/messages/details → Inbox 与统一 Approval/Decision → 跨页面回归。

设置覆盖设计中的 Model/Provider/selector、Agent/capability/Secret/Mount、Runner、MCP、项目资料/变量/调度/summary 配置、账号邀请/SMTP 与系统运行信息。布局规定的 Dashboard 保留容器；全局搜索保留已定容器，检索范围需另立设计工作项。

验收每页的正常、加载、空、失败、无权限、删除、version 冲突和重连状态；显式保存与未保存提示；Task 视图切换保持 Sprint；知识树展开不改变选择、子树删除明示范围；会议中央输入框和 reference 正确；多个页面处理同一个审批对象。

禁止用 Debug fixture 代替业务 API；前端不复制领域授权/状态机，不自行把执行完成显示为 Task done。每组运行适用测试与浏览器验收，最后运行完整 `npm run check --prefix web`。

## 阶段 10 完成系统交付

### D28 集成部署与交付验收

依据：[部署运行](../architecture/platform-infrastructure/deployment-runtime.md)及各模块验收规格。

实现正式嵌入式前端资源、History fallback、Docker Compose、环境/secret 示例、startup validation、liveness/readiness/dependency diagnostics、graceful shutdown、升级迁移和运维说明。API、缺失资源及服务端错误不能 fallback 成前端 HTML。

完成以下真实组合验收：

| 流程 | 关键结果 |
| --- | --- |
| 首次部署 → 初始化 → 邀请 → 登录 → 创建 Project | 重启幂等，权限和 SMTP/log 渠道符合规则 |
| 建 Agent/Mount → 创建 Sprint/Task → Scheduler → Runner → Review | claim 不重复，执行有证据，Task 审核后才 done |
| Tool 请求审批 → Inbox/Task/Meeting 任一入口批准 → Resume | 同一审批事实，同一 execution/operation，无重复副作用 |
| Knowledge 上传/改正文 → index/rebuild → query；Memory retain/recall/reflect | version/generation 正确，scope 隔离，reflect 只读 |
| Meeting sequential/parallel → decision → cancel/retry → summary | 可见性、等待与标题/摘要一致 |
| MCP refresh/credential rotation → call/resource read | 稳定身份，旧 snapshot 不被篡改，新发现不扩大权限 |
| Runner/Central/Provider/MinIO/PostgreSQL 故障注入 | 明确失败或可解释恢复，不丢事实、不盲重试 |
| Project/文档/Agent/MCP/Model 删除与引用替换 | 删除保护、历史投影、索引/对象清理符合各领域契约 |

完整 backup tooling 不是现有首版架构的已定范围；交付至少说明 PostgreSQL/MinIO 一致恢复边界，不能宣称只有一个存储备份就可恢复整个系统。部署验收使用授权的测试环境；对外推送、发布或实际部署另依用户指令执行。

门槛：所有前置模块通过、Tool/页面/事件覆盖映射无缺口、未完成端口真实绑定、适用数据库/协议/前端/端到端验收通过、无未说明的高影响故障或恢复缺口。记录可选设备/Provider 的实际验证范围，不把未测能力说成已验收。

## AI 如何执行和恢复

沿用[串行团队](agent-team/README.md)：主线程负责设计、任务卡、诊断、审查与本地提交；每次最多一个活动子 agent，角色固定 `backend_worker`、`frontend_worker`、`verification_worker`，使用 `gpt-6.1-sol / low`，子 agent 不再委派。

每次恢复先读取 `AGENTS.md`、本计划、[台账](agent-team/tasks.md)、当前工作项规格与实际 Git 差异。计划编号 D00–D28 表示模块顺序，团队台账 AT 编号表示实际执行记录，两者关联，不互相替代。

执行步骤：

1. 找到首个未完成工作项，核对它的直接及传递依赖证据；不能凭上一会话的口头总结宣布通过。
2. 主线程完成当前模块规格并关闭影响该项的未决接口；使用[任务卡模板](agent-team/task-template.md)拆成顺序小卡，明确授权文件、版本、步骤、技能、命令、预期及停止条件。
3. 开发角色执行当前卡；验收角色接续检查；必要返修仍属于当前模块。小卡完成并不意味着整个模块完成。
4. 主线程核对实际 diff 与证据；按团队约定提交当前交付。完整模块通过门槛后，才更新为“已完成”并推进下一工作项。
5. 每项记录规格路径、卡片进度、真实验证、未完成端口绑定、阻塞、对应提交的定位方式和下一步。发现上游 bug 时先返修上游，不在下游复制补丁。

状态使用“待开始 / 设计中 / 实现中 / 验收中 / 阻塞 / 已完成”。只有通过门槛才标记已完成；每次仅有一个活动模块。用户授权一个工作项后，范围内任务卡连续推进，不逐卡重复确认。用户调整需求时保留改动、暂停受影响步骤，更新规格和修订号。

检查命令必须随工程初始化确定。后端建立后至少有对应范围的 Go test/vet/build；并发与恢复模块使用 race 和真实数据库测试。前端使用现有 npm 脚本。纯文档变更只检查链接、内容一致性和 `git diff --check`，不声称产品行为通过。

后续可给 AI 的启动指令：

> 按 docs/development/development-plan.md 推进。先读取 AGENTS.md、团队台账、当前工作项规格和实际仓库状态，从首个未完成工作项开始。先完成模块契约和验收设计，再按串行任务卡实现；模块完整验收后才推进下一项。保留已有前端基础，不用临时 stub 或演示数据充当正式实现。每项同步证据、台账和本地提交，遇到未决产品规则或范围扩展由主线程处理。

## 当前启动点

本次交付的是开发计划，D00–D28 尚未作为开发工作项执行。本轮阅读仅作为规划输入，不等于 D00 的完整契约审计。正式开发从 **D00 基线核对 → D01 跨模块契约 → D02 工程基础** 开始；当前不提前实现业务模块。
