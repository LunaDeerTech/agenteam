# agenteam 开发计划

本计划用于指导后续 AI 从基础契约开始，按真实依赖完成 agenteam 的正式开发。推进单位是职责明确的模块；每个模块先设计数据结构、接口和状态规则，再实现、调试和验收，最后接入其他模块。没有未满足依赖的模块和完整结果任务允许并行，不以模块编号规定全局串行。不以提前出现可操作界面作为早期交付目标。

计划依据是[当前架构](../architecture/README.md)、[前端设计](../frontend-design/README.md)和[仓库结构](repository-structure.md)。它规定开发顺序和验收要求，不替代架构专题，也不把尚未确定的实现选择写成既定事实。`docs/draft.md` 仅作历史参考。

AT-0005 已逐项确认审计方向；正式文档归位和验收见[同步任务卡](work-items/d00-decision-sync.md)，逐项责任见[审计处置映射](work-items/d00-baseline-audit-report.md#7-at-0005-决策处置映射)。下列开工门槛落实已确认规则的具体规格，不重复把已选方案列为产品待选，也不代表 D01 或产品模块已经验收。

## 当前基线与计划范围

2026-10-07 当前增量：D09 [OpenAI Embeddings float wire](work-items/d09-openai-embeddings-wire.md)已按[验证归档](agent-team/openai-embeddings-wire-verification.md)独立接受，十六路径产品提交推送 `2debfdde347d8c9262ab83d3fe5e18e947a983e1`、远端一致。固定c142十五技术源加README末件，914依赖无漂移；六真实轮按版本组合通过，不冒主树动态重跑。原容量指标失败、启动闭包阻断及辅助原件缺口保留。本结果不完成D09或绑定Nonchat/InvocationID/Usage/root，不改变模块顺序、Summary待决、ready503、E01未开始及三停止任务。

2026-10-07 当前增量：D27 [System 运行信息只读 UI](work-items/d27-system-runtime-information-ui.md)已按[验收记录](agent-team/system-runtime-information-ui-verification.md)独立接受，35 路径产品提交推送 `e1f8cefbeaf3203c4e0388f9ea9dbf11e2e2959b`、远端一致。它只读取 Central 缓存观测并提供显式重读/取消，不新增探测、写口、自动轮询或 ready 成功。原两真实失败与各阶段/分版本证据保留；本结果不改变下述模块顺序和完整门槛。D08–D28/E01 整体仍未完成，E01 未开始，Summary 待决与三项停止任务保持，当前后继以团队台账为准。

规划日期：2026-10-03。计划制定时核对基线为 `fe5b833`，环境检出分支为 `work`、工作区干净；D00 已由主线程建立 `main`，后续工作沿用该分支，当前提交与进度以任务台账和 Git 为准。恢复开发时核对真实分支与基线，由主线程处理环境差异，不让子 agent 自行切换分支或创建 worktree。

已存在的成果：Vue 3、TypeScript、Vite、路由与应用壳、公共组件、主题、开发专用 Debug 展示，以及主线程统筹的开发团队和项目技能。后端已完成 D02 B01 的根 Go module、基础标量、HTTP 边界和公共 schema；Central/Runner 独立入口、配置与生命周期也已完成 D02 验收；D03 数据库迁移、事务锁及 Central 数据库启动/健康/关闭已完成验收；后续 D04–D06 基础和 D07 账户/System HTTP、Profile/Avatar、SMTP/受限日志及正式根装配也已通过适用验收，B37 已提交 `022dcea`，操作文档已独立核准，D07 当前范围已完成，关闭文档已提交推送 `0ed8085`。已有前端成果应复用，D26/D27 产品页面仍待实现，不把 Debug 演示状态当成业务实现。

范围覆盖现有架构文档规定的第一阶段：单 Central、多 Runner、PostgreSQL/pgvector、MinIO、账号与项目、工作管理、模型、Agent、Knowledge/Memory、统一工具、治理、MCP、执行、调度、会议、实时与 Inbox，以及正式前端和部署。Dashboard 仅保留设计规定的容器；全局搜索业务范围未定，不纳入已确定功能验收。首版不新增 Redis、多 Central、微服务、公开注册、项目成员矩阵、跨项目检索或 Runner OS sandbox。

本轮用户已明确授权持续完成 D01–D28 的全部能力，按独立可验证的小块及时提交、同步 `main` 并推送。最终交付还必须通过 D28 之后的 **E01 平台实战与游戏复刻验收**：使用实际运行的 agenteam 组织 Agent 协作开发 Minecraft 或 Terraria 的复刻，达到所选原版参考版本完整内容的加权覆盖率至少 50%，覆盖核心玩法与主要系统，保留平台任务、协作、执行、审核和产物证据，并实际试玩、修复暴露的平台问题。决定来源、冻结规则与可复现验收算法见 [E01 工作项](work-items/platform-game-acceptance.md)；当前游戏、参考版本与完整清单尚未选择或冻结。

## 推进规则

1. **先定契约，再写实现。** 跨模块边界先统一；当前模块的字段、函数签名、命令、查询、错误、事务和验收场景必须在开工前明确。
2. **按真实依赖并行，模块分别完整验收。** 不再限制一个活动模块；共同正式契约稳定、所需前置能力已有验收证据、文件与迁移及共享资源所有权明确的模块和完整结果任务可并行。独立可验证的小块通过验收后及时提交到 `main` 并推送，模块仍按实际状态登记；不能因为小块已提交或正常路径可运行就跳过异常、并发、幂等和恢复设计，未完成卡保留在所属工作项内。
3. **模块完整与产品集成分开验收。** 模块按真实存储及协议契约完成自己的职责；依赖未来模块的地方使用明确端口。后续只增加适配器、注册和集成验证，不回头补此前遗漏的核心逻辑。
4. **设计冻结的是边界，不是全部未来代码。** 不提前创建所有空包、万能 Service 或整套空 handler。模块开工时再落实内部包结构和 SQL，遵守全局迁移序列。
5. **核对任务实际需要的依赖。** 只消费上游已验收子能力的完整结果任务可在上游模块整体完成前开工，但须记录具体能力、验收证据、正式契约修订和后续集成责任；所需能力未实现时不能以接口声明代替满足依赖。相邻模块只使用已确认的服务接口、事件或 read model，不直接修改对方数据表。不能为了运行演示引入临时旁路。
6. **验收以证据为准。** 状态机、权限、数据库竞争、外部协议和重启恢复使用适当的行为测试。假实现仅用于隔离测试，不作为生产交付。
7. **修改设计须同步影响。** 主线程先记录原因、受影响模块、兼容与迁移方案，更新架构和任务卡，再实施变更。工程选择由主线程在授权范围内决定；产品含义未定、实质范围变更或必须获得用户授权时才请求用户决策。

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

完成门槛：规格与实现一致；该模块当前范围全部实现；适用检查通过；真实依赖的集成证据齐全；无静默成功、无核心 TODO、无伪数据生产路径；操作说明和台账更新；专门验收负责人完成适用独立验证及证据分析，主线程审查结论、最终整合并创建本地提交。未运行的检查标记“未执行”及原因，不能标记“通过”。外部依赖无法验证时保留“待集成验证”，阻止依赖该能力的阶段通过。并行调度不降低模块完成门槛。

## 依赖顺序与循环依赖处理

下表是模块依赖图和规划优先级，不是全局串行队列。依赖栏表示完整模块集成需要的直接前置工作项，传递依赖同样须核对。模块中的完整结果任务可按上文规则只消费已有验收证据的稳定子能力，明确其余能力的真实绑定和验收责任；尚未满足的依赖仍阻止相关实现或集成通过。所有实现工作项还共同依赖 D00 与 D01。

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
| 11 | E01 | 平台实战、游戏复刻至少 50% 与实际试玩验收 | D28 |

D00–D28 的模块职责、实际依赖与完成门槛保持不变；编号先后不再增加额外串行限制。例如 D08 所需能力验收后，D09 与 D12 可在契约和所有权满足时并行；不能据此跳过各自依赖或集成。E01 是 D28 后的最终交付硬门槛，不替代任何平台模块；E01 未通过时，不能因 D28 通过而宣称本轮用户目标已全部完成。

这里有几组逻辑上的循环依赖，需要在 D01 固定端口，按以下方式实现：

- **Work Management 与 Executor/Scheduler：** D11 通过执行占用、pending dispatch 查询端口实施 Sprint/Task 保护规则。D11 用契约测试验证占用与并发行为；D22/D23 提供真实适配器并做数据库竞争测试。生产装配必须注入真实适配器，不能默认为“没有执行”。
- **Tool Runtime 与 Governance：** D18 依赖授权端口，验证 allow/deny/waiting 的调度行为；D19 实现完整授权和审批规则，再验证真实组合。不在 D18 内复制授权逻辑。
- **Tool Runtime 与 Executor/Loop：** Tool Runtime 接收执行身份、固定快照和控制回调，不 import Executor 内部实现；Executor/Loop 通过端口调用 Runtime。D22 负责等待、恢复、取消的真实执行绑定。
- **Meeting 与 Executor：** D01 固定 TriggerContextProvider 与 Decision 控制输入契约。D22 用受控 Trigger fixture 验证执行引擎；D24 注册 Meeting provider 和 Meeting Tool，验证真实会话流程。
- **事件生产者与 Inbox/Realtime：** D06 完成投递机制，后续模块提交自己拥有的 typed event。D25 实现消费者、投影重建和前端同步；新投影从 canonical Source of Truth 重建，再处理增量事件。D06 明确 handler 注册与失败重投；首期 Outbox/delivery、Dispatch 与 Audit 历史不自动过期或清理，领域明确授权的永久删除仍按生命周期矩阵执行。Outbox 不充当 Event Store，不新增通用历史回放框架。
- **Artifact 服务与 Tool：** D05 完成对象和 Artifact 的业务服务，D21 仅提供模型调用适配器；不能让 Agent 直接操作 MinIO 或创建 Signed URL。
- **Project/Agent Skills 与执行：** D01 固定技能初始化、安装/分配、目录/读取、Runner 准备及运行中新增绑定端口；D08 的项目初始化由 D10 绑定真实内置技能服务，D05 提供对象能力，D17/D21 完成传输/工具，D22 完成下一轮应用及恢复。各阶段用正式端口验证自身边界，必要能力缺失明确失败，不以空实现跳过内置技能初始化或远程准备。

端口是正式设计边界，不是临时 stub。测试替身必须覆盖成功、失败、竞争和未知结果，并在台账列出真实绑定的后续工作项。

## 阶段 0 先确定系统契约

### D00 核对设计与实现基线

输入：[架构总览](../architecture/README.md)、所有模块专题、[架构衔接清单](../frontend-design/architecture-follow-ups.md)、前端源码、[团队流程](agent-team/README.md)。

交付：实现清单、设计待定项清单、文档冲突清单、依赖能力与工具可用性清单。区分“已有实现”“仅有设计”“待定”“本阶段明确不做”。核对 Go/Node/PostgreSQL/pgvector/MinIO 的版本约束及测试条件，不直接沿用机器已安装版本作项目决定。

验收：每个一级架构模块都有归属；已有前端组件和 Debug 被正确保留；所有未决项关联到 D01 或某个模块开工门槛。此项不写业务代码。

### D01 固定跨模块契约

已确认的共同方向见[基础契约约定](../architecture/platform-infrastructure/foundation-contracts.md)：UUIDv7、UTC/微秒、Problem Details、cursor、expected_version、追踪 request_id 与业务 idempotency_key 分离。同键不同语义输入拒绝；契约按职责组织，避免循环引用和万能共享包。

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
- Project 归档/永久删除及用户名路径；项目 Skill 版本、分配与下一轮绑定；Meeting Trigger 的 meeting_id/turn_id/contribution_id/participant_id 及领域校验端口。
- Model System 统一拥有同一 Agent 逻辑模型调用的自动请求重试，Loop 不叠加；逐真实 attempt 用量、partial stream、取消和 watchdog 边界。
- Runtime item seq 只排序，Execution 更新进度独立；快照水位对应实际内容，覆盖订阅前已发送但尚未 flush 的间隙。固定 snapshot/read/stream/resync 契约，不逐 token 写库、不新增持久 delta 日志。
- Scheduler 通过最小只读端口按原 Launch key 核对未知结果，不能把会创建 Execution 的 launch 当查询；Meeting summary 以实际有序输入版本参与幂等，历史回复替换等下轮正常 finalize。

不要求在本项预写全部数据库表、函数实现或所有 UI API；每个模块的具体规格在自身开工前补齐。验收用接口走查验证调用方和提供方对正常、冲突、拒绝、等待及 unknown outcome 的理解一致，依赖矩阵无未解释循环。

## 阶段 1 完成平台基础

### D02 工程与程序生命周期

依据：[部署运行](../architecture/platform-infrastructure/deployment-runtime.md)、[仓库结构](repository-structure.md)。

实现：根 Go module、两个入口、Central/Runner 独立装配、环境部署配置加载与校验、结构化日志和关联 ID、取消与 shutdown 接口、net/http + ServeMux 与基础 HTTP 错误处理及测试命令。优先核验环境候选版本，不满足要求时更换并固定版本；Runner 不依赖 Central 业务包。

验收：两个入口可构建，错误配置明确失败，signal 能有序停止；示例配置无凭据。数据库、对象存储尚未完成时，不把服务报告为完整 ready。前端工程保持独立可运行。

### D03 PostgreSQL 与全局迁移

实现：pgx + 显式 SQL、连接管理、transaction abstraction、Goose 全局 SQL migration、版本及 pgvector 校验、迁移锁、与项目固定版本一致的隔离测试容器、mandatory dependency 检查和 read/write 诊断。数据库约束作为正确性保障，不依赖内存锁代替，不由 ORM 自动维护生产结构。

验收：空库初始化、重复启动、已有库升级、迁移失败、并发迁移、断连与停机处理；失败不得带着半完成 schema ready。只创建当前模块所需 schema，后续迁移顺序追加。

### D04 Secret 出站与 Audit

依据：[部署密钥](../architecture/platform-infrastructure/deployment-runtime.md)、[出站策略](../architecture/platform-infrastructure/outbound-network-policy.md)、[Audit](../architecture/security-governance/audit.md)。

依次完成三个职责：AES-256-GCM envelope encryption、环境变量注入的版本化 master key ring 与分批重新保护数据密钥；受控 HTTP/TLS/DNS/redirect/响应限制及管理员 DB 出站网段/可选端口/显式 HTTP 配置；append-oriented Audit 写入与分页查询基础。DB 仅存受保护密钥和迁移元数据，自动迁移不回写部署主密钥。身份/系统授权在 D07、Project 查询授权在 D08 接入，不以管理员角色代替 Owner。

验收：密钥缺失、解密错误与中断迁移恢复；Secret 不进入普通日志或 DTO；出站策略更新对新请求/重试/重定向立即生效，在途请求继续且复用连接不绕过检查；private network、DNS rebinding、TLS 与凭据作用域。Audit 只记录敏感/安全行为，不复制完整工具记录，所有 ToolOperation/Attempt/Transcript 仍由责任模块保存。SMTP 非 HTTP 访问适配在 D07 实现。

### D05 对象存储与 Artifact

依据：[Object Storage](../architecture/platform-infrastructure/object-storage.md)、[Artifact](../architecture/tool-system/artifact-tools.md)。

实现：StoredObject、引用、上传/下载/预览、对象状态、大小与内容校验、MinIO streaming、数据库与对象写入一致性、未引用对象清理、Artifact 服务、权限解析端口和短期 Signed URL。确定附件、头像、文档、不可变 Skill 包/派生文件、MCP 内容和执行大 payload 的引用契约。Runner 按单对象/操作短期授权直连可达存储端点，签名材料不进入模型。

验收：上传中断、对象写成但事务失败、metadata 存在但 payload 缺失、删除仍有引用、跨 scope 访问、大文件不整块加载内存。D07/D08 接入真实身份和项目授权后补做组合验收。

### D06 Outbox 与事件投递

依据：[Internal Domain Events](../architecture/platform-infrastructure/internal-domain-events.md)。

实现：typed envelope、业务事务内 outbox 写入、dispatcher、handler identity、独立 delivery state、at-least-once、去重、retry/dead-letter、排序限制、失败 redelivery 与重启恢复。不同领域 payload 由对应领域拥有；投影从 canonical tables 按模块重建，不通过回放全部历史事件重建系统。

验收：事务回滚无事件；提交后崩溃可重投；重复投递无重复副作用；一个 handler 失败不丢其他 handler 的结果；明确晚注册消费者与 canonical rebuild 的衔接规则，D25 验证实际投影重建。事件不能承担业务事实的唯一存储责任。

## 阶段 2 完成账号与核心业务事实

### D07 账号 Session SMTP 与个人资料

依据：[账号生命周期](../architecture/platform-infrastructure/authentication/account-lifecycle.md)、[SMTP](../architecture/platform-infrastructure/authentication/smtp-delivery.md)。

开工前落实：Argon2id 参数/并发限制、15–128 字符密码与弱密码检测、Cookie/Origin/CSRF、Session/重置期限配置及撤销事务、GoCaptcha + go-captcha-vue 内嵌挑战协议、SMTP 三模式/有限重试/非 HTTP 出站适配、静态 JPG/PNG/WebP 头像校验与当前对象清理。Session 空闲/绝对默认 7 天/30 天，重置默认 30 分钟，失败挑战阈值默认 5 次，均为管理员系统配置；不加入登录等待/冷却。

实现：幂等初始化邮箱 `admin@mail.com`、username `admin`，邀请注册时设置全站唯一 username；登录退出、固定 24 小时邀请过期/撤销/原子兑换、密码修改与统一恢复、SMTP 配置/测试/重发、无 SMTP 的受控后台日志投递、用户名/显示名/头像/账号主题偏好。初始密码日志丢失也走普通忘记密码；有效重置链接重发不延时，普通改密换发当前会话并撤销其他，重置撤销全部。无 SMTP 不阻断 ready，配置后投递失败不改为日志渠道。

验收：重启不覆盖密码；并发邀请与兑换；撤销/过期竞态；reset 单次消费；公开响应不泄漏邮箱存在性；退出后的 HTTP/WS 身份失效；凭据不回显；个人偏好与 Debug 分离。 D07 完成当前后端事实、HTTP 与正式端口；D25 负责 WS 撤销消费者，D26 负责账号/Profile 客户端，D27 负责 System 页面。当前业务与测试门槛已由验收负责人按 T01–T14 核通过，最后源码为 `022dcea`；操作文档已独立核准，D07 当前范围已完成，关闭文档已提交推送 `0ed8085`，见[D07 当前进度](work-items/d07-account-session-smtp.md#当前进度)。

### D08 Project 与 Owner

开工前落实：[Project](../architecture/project-work-management/README.md) canonical 字段、Owner 内名称唯一、`/{username}/{project_name}` 解析、cursor/version、归档/恢复与不可恢复永久删除的停止和清理契约。改名后旧链接立即失效、无别名；归档和处理中保留名称，永久删除完成后释放。

实现：Owner 由服务端身份绑定；本人项目查询/改名；归档或删除先禁新启动、停止并确认已有执行，归档只读且可恢复但不重启已取消执行，删除按确认路径永久清理。Project-scoped Audit 随永久清理、System Audit 保留。系统授权与对象权限真实装配；后续领域停止/清理适配器逐项注册，本项完成持久化进度、生命周期端口和失败规则。

验收：跨用户读取/修改/下载/订阅拒绝，管理员无 Owner 旁路；名称规范化/创建/改名竞争；停止/归档/恢复/删除互斥及重启恢复；永久删除完整路径与稳定 ID/expected_version 共同校验。未知停止结果或缺少必要清理绑定不能算成功，不递归误删共享挂载/外部副作用。

### D09 Model System 与 Token Usage

依据：[Model System](../architecture/platform-infrastructure/model-system/README.md)、[Token Usage](../architecture/platform-infrastructure/model-token-usage.md)。

实现：System/Project Provider 与 ModelConfig、固定 Provider protocol、selector、credential reference、Resolver 和不可变 snapshot；统一 Chat request/stream/tool calling/reasoning/structured output；OpenAI-compatible Chat Completions 与 Anthropic Messages adapter；embedding、optional reranker、optional image_generation 独立边界与本阶段选定 adapter；Model Invocation usage 持久化与查询聚合。

非 chat 首期为 OpenAI Embeddings、Jina Rerank、OpenAI Image Generation 及经真实验证的兼容 profiles，分别固定 schema/conformance，不凭 `/v1/rerank` 路径判断协议。Model System 统一负责 Agent 同一逻辑调用的自动请求重试，逐真实 attempt 记录 Invocation/Usage；会议辅助调用归 meeting + purpose，Agent 会议发言仍归 Agent、不重复记账。实现引用替换与旧 snapshot 保护；optional 未配置和调用失败分别处理，不静默换模型或估算 usage。

验收：流中断、无效 Tool JSON、usage 缺失、错误规范化、取消、超时、配置更新不改变旧 snapshot；两个 Chat protocol 均有 adapter conformance 测试，可用真实 Provider 做受控 smoke。真实凭据从环境读取，不写文档、fixture 或测试输出。

### D10 Agent 配置与项目变量

依据：[Agent](../architecture/agent-management.md)、[Skills](../architecture/agent-skills.md)、[项目变量](../architecture/project-work-management/project-environment-variables.md)。

实现：Agent 配置/预设、model_ref/reasoning_effort 校验、description/instructions、Platform Prompt 版本、allowed Tool stable ID、Mount 和 Secret 引用、default/auto/allow 模式、普通变量与 Secret 白名单。Core Agent Tools 不可被普通开关关闭，但每次调用仍授权。

Agent 标识名按 Project 唯一，规则同 username、允许与 User 同名，另有可选显示名。Preset 模型可选且工具只推荐平台内置项，失效推荐跳过；实际 Agent 必须有合法模型。完成项目 Skill/不可变 revision、统一安装、Agent 分配及正文授权：Add Skills 不可单独删除，与 install-skill 均默认启用但可禁用；assign-skill 普通可选，管理目录可查全项目但正文按自身分配。安装不自动分配，同名仅显式更新发布新版本。

Skill 包不在 Central 执行脚本、不自动装依赖。实现 D08 初始化绑定与对象发布/清理；新增技能通过正式持久变更端口在下一模型轮次应用，移除只改配置、后续调用正常报错，不编排已有执行。Runner 准备和执行恢复分别由 D17/D21/D22 真实绑定并验收。

Tool/Mount 引用通过 D01 的目录端口验证，D15/D18/D20 接入真实目录；不可因目录未实现而默认接受未知引用。实现配置引用替换、删除保护与 snapshot 输入读取。

验收：Owner 校验；普通变量与 Secret 的可见性区别；Secret plaintext 不进入 Model Context；删改配置不改写历史 snapshot；配置页面字段与服务一致。当前 Agent 配置不保存可变 runtime 状态。

### D11 Work Management 领域

依据：[Task Domain](../architecture/project-work-management/task-domain-model.md)、[状态机](../architecture/project-work-management/task-state-machine.md)、[Blocker](../architecture/project-work-management/task-blocker-dependency.md)、[Timeline](../architecture/project-work-management/task-event-timeline.md)、[Sprint](../architecture/project-work-management/sprint-lifecycle.md)。

连续任务卡顺序：Milestone/Sprint 结构 → Task canonical/查询/rank → 状态转换与 reviewer → Blocker/依赖图 → Sprint lifecycle → TaskEvent 与 context/query 接口 → 综合验收。

实现强制层级、唯一 Current Sprint、expected_version、业务幂等 replay（与传输 request_id 分离）、终态不可变、审核交接、blocker 原子转换、依赖循环检测、Sprint rollover、事件同事务写入和 Kanban/Explore read model。纯 rank 重整不推进业务 version/TaskEvent，真实重排正常推进；与 D23 的 Busy claim 补偿协调逻辑位置，不能写回失效旧 rank。执行占用和 pending dispatch 保护通过固定端口实现。

验收：Task 必须经过 `in_review` 才能 `done`；重复命令先解析幂等再校验 version；`cancelled` 依赖不自动满足；blocked 最后 blocker 解除与恢复原子化；并发 start-sprint 至多一个 current；有执行/dispatch 时 complete/move 拒绝；领域事件不混入执行流日志。

## 阶段 3 完成 Knowledge 与 Memory

### D12 Knowledge 文档与文档树

依据：[文档领域](../architecture/knowledge-memory/knowledge-document-domain.md)、[知识布局](../frontend-design/layouts/knowledge-base.md)。

开工前关闭：parent identity、根与同项目单父约束、stable sort/分页、移动防环与并发、子树 soft delete 一致性、历史引用投影、内容 version 与 indexing state 分离。

实现：canonical Markdown/text/file 文档、create/update/read/list/preview/download、当前 parent 关系、根/子节点按需 cursor 查询、全项目标题查找与祖先路径、确认范围后的子树删除、索引失效与清理端口。每个父节点自身也有正文；移动不增内容 version、不重建索引、不记录结构版本/历史。事务内复查父子/防环/删除范围，不能仅靠内容 version 检测移动竞争。

验收：跨项目 parent、循环、并发移动、子树删除期间更新竞争；正文更新后旧 version 不能继续 serving；索引失败不丢正文；权限先于预览和 Signed URL。

### D13 索引与混合检索

依据：[Indexing](../architecture/knowledge-memory/knowledge-indexing.md)、[Retrieval](../architecture/knowledge-memory/retrieval-runtime.md)。

先建立中英混合项目文档 benchmark，记录候选 lexical backend、指标、结果与选择，再引入正式依赖。实现 Markdown/text/PDF/DOCX parser、section-aware chunker、locator、IndexProfile/job/generation、embedding、lexical abstraction、dense + lexical + equal-weight RRF、optional reranker、过滤、结果预算与评测。

参数默认值/上限由管理员系统配置统一维护，无 Project 覆盖；索引期变化创建新 IndexProfile 并重建，查询期变化无需重建。equal-weight RRF 不变，不开放任意权重或词法后端 UI 热切换；无可提取文本的扫描 PDF 明确失败，首期不新增 OCR。

验收：scope 在召回前过滤；重复 job、部分失败、删除清理、重启重建；文档变更淘汰旧版本；仅 profile 变化可继续旧 generation serving 再原子切换；模型变化不混用 embedding space。保存可重跑的检索数据集与对照指标，参数由评测确定。

### D14 Agent Memory

依据：[Memory Domain](../architecture/knowledge-memory/agent-memory-domain.md)、[Memory Runtime](../architecture/knowledge-memory/agent-memory-runtime.md)。

实现：`project_id + agent_id` namespace、类型与 revision/provenance、active/superseded/deleted、retain extraction/consolidation、recall、只读 reflect、用户管理与索引更新。使用平台 memory_model_ref，不继承 Agent chat model。

明确未提交的 revision/目标状态冲突后重读当前 Memory，重新 consolidation 一次再原子提交；再次冲突返回工具错误。未知提交结果沿原幂等契约查询，不假定回滚重算；保留额外模型调用证据与用量。验证用户删除与 retain 等合法竞争，不使用同 Agent 双 Execution 作为合法并发前提；无自动 retain/TTL。

验收：跨 Agent 拒绝；Secret 在任何 model/embedding/index 调用前处理；consolidation 竞争可追踪；superseded/deleted 不召回；reflect 无写入；执行完成不会自动生成 Memory。检索共用 D13 基础，不复制检索引擎。

## 阶段 4 完成 Runner

### D15 身份与 Control Channel

依据：[Runner Management](../architecture/runner/runner-management.md)、[Control Protocol](../architecture/runner/control-protocol.md)。

实现共享 `internal/runnerprotocol/` 类型、协议版本、enrollment/Ed25519/challenge、单连接、WSS outbound、hello/capability/heartbeat、request/response/stream/deadline/cancel、重连、轮换与 pending request outcome。

首期正式支持 Linux 与 macOS，Windows 延后；最低系统/CPU/协议库版本及真实支持矩阵在规格固定。hello 只上报实际可用能力，不以交叉编译代替平台运行验收。

验收：错误身份、重复连接、版本不兼容、heartbeat 丢失、deadline、断线、响应丢失；断线时 unknown outcome 不能伪装失败后直接重试。顶层状态仅 online/offline/incompatible；Runner 不 import Project/Task/Meeting 业务包。

### D16 Workspace 文件命令与进程

依据：[Workspace](../architecture/runner/agent-workspace.md)、[Execution Runtime](../architecture/runner/execution-runtime.md)。

实现 Mount 到固定路径映射、workspace lifecycle、文件 read/write/edit/list/grep/find、argv 直接执行、Linux/macOS 默认 Bash shell、cwd 校验、临时环境注入、stdout/stderr streaming、masking、取消进程树、managed process scope/状态/ring buffer/cursor。报告实际解释器/版本，缺失明确失败，不静默换 shell；技能包托管目录与普通工作文件分开。

验收：path traversal、symlink containment、越 Mount 访问、并发文件更新、取消与断连、Runner 重启和进程状态、输出 overflow、Secret masking。命令采用 trusted-host：cwd 合法不代表 OS sandbox；不得在文档或 UI 承诺命令无法访问 Workspace 外部。

### D17 数据通道 Desktop 与 Tunnel

依据：[Data Channel](../architecture/runner/data-channel.md)、[Desktop/Tunnel](../architecture/runner/desktop-tunnel.md)。

实现按需 outbound binary channel、一次性认证、checksum/size/progress/cancel/temp cleanup、upload/download/transfer；Runner 通过短期 scoped 凭据直连对象存储，固定 Skill revision 完整包按需校验/落地，不经 Central 自动回退中转。Desktop 首期覆盖 macOS 原生、Linux X11/Wayland 的截图和输入，规格固定真实后端/权限矩阵，不静默替代成 headless。

Tunnel 地址/路由由 Central 创建，经 Runner 主动反向通道代理端口；HTTP 请求/WS upgrade 复用 Session 并校验当前 Project Owner，已建立 WS 也响应会话失效。无管理员旁路或分享链接，平台身份材料不透传目标服务；保持 process binding、有限 TTL、关闭/断连不自动恢复。

验收：大文件中断/校验失败/临时文件回收；无桌面设备明确不可用，有桌面环境执行真实能力验收；Tunnel 无权限、到期、Runner 断连和 process 退出关闭。设备可选能力未测试时记录设备限制，不把空方法算实现。

## 阶段 5 完成工具和治理

### D18 Registry 与统一 Runtime

依据：[Definition/Registry](../architecture/tool-system/tool-definition-registry.md)、[Operation/Attempt](../architecture/tool-system/tool-execution.md)、[Result/Backend](../architecture/tool-system/tool-result-backend.md)。

实现 stable ID、immutable spec revision、binding、Execution Tool Set snapshot、model-visible 无冲突映射、JSON schema validation、Operation/Attempt 独立持久化、dispatcher、结果预算/Artifact 外部化、timeout/cancel/parallelism/retry/idempotency。

工具按需在自身 schema 声明可选 timeout，不增加全工具包装/保留参数或平台统一 Operation 强制期限，MCP 参数保持原语义。内部可信 Context/取消与业务参数仍分离；期限不终结人工等待，重试不能刷新已开始的期限。Attempt 通过 operation_id 关联独立实体，不嵌为 JSON 数组。

验收：无效参数不执行 backend；technical retry operation 不变、attempt 改变；新 model call 即使同参仍为新 operation；unknown + non-idempotent 不自动重试；backend 结果与模型可见结果分层；授权端口返回 deny/waiting 时不执行。

### D19 Authorization 与 Approval

依据：[治理](../architecture/security-governance/README.md)、[默认策略](../architecture/security-governance/default-approval-policy.md)、[Scope](../architecture/security-governance/approval-scope.md)、[一次性审批](../architecture/security-governance/one-time-approval-retry-idempotency.md)、[自动审批模型](../architecture/security-governance/auto-approval-model/README.md)。

实现 Capability/Execution Policy/scope/固定规则、Approval Match、default/auto/allow、Tool-defined ScopeResolver、One-time 与 Reusable Approval、Request/approve/reject/revoke、待处理查询、事务与 outbox、统一用户动作 API。提供等待解析端口给 D22，D25 消费审批事件形成 Inbox。

人工审批持久等待有权用户明确处理，刷新/离线/Session 过期/正常重启不自动超时、跳过或放行；显式停止或领域生命周期失效按取消契约处理。自动审批模型请求有有限 timeout，审批模块不增加应用重试或备用模型；错误转人工后迟到结果不能覆盖人工状态。

验收：allow 模式仍不能绕过基础权限；auto model 失败进入人工审批；一次性审批绑定 operation/fingerprint 不绑定 attempt；新调用不能复用；Reusable revoke 后立即失配；批准当前等待操作后直接继续，不重复审批；并发批准/拒绝只形成一个决议；所有入口共享同一权威对象。

### D20 MCP

依据：[MCP](../architecture/mcp-integration/README.md)及其 Config/Protocol/Discovery/Execution/Resource 专题。

实现 System/Project Config、Project Connection、credential binding、无认证/static/OAuth 2.1、Streamable HTTP、runtime lease/reconciliation、discovery 原子 diff/schema/revision、Tool adapter、Resource/template catalog、materialization 与对象引用。Config 仅 profile，凭据与发现目录属于 Project Connection；无系统级连接/已发现工具库。优先官方 Go SDK，核验主协议 2026-07-28 与兼容 2025-11-25 的固定版本和 conformance，不假定 SDK 已支持。首期不支持 stdio、旧 HTTP+SSE、Prompts、Elicitation、Tasks。

验收：连接认证与 Tool Authorization 分离；connected 不冒充 discovery 成功；refresh 失败保留上一成功定义；一个 invalid tool 不拖垮有效 tool；新 tool 不扩大已有 Agent 权限；annotation 的 default_enabled 仅初始建议；超时/取消/unknown outcome、credentials、Resource scope 和大小限制均验证。

### D21 领域 Tool 与 Backend 适配

依据：[工具目录](../architecture/tool-system/README.md)、[Artifact Tools](../architecture/tool-system/artifact-tools.md)。

接入已完成的 Project/Milestone/Sprint/Task/Blocker/Agent/Knowledge/Memory/Artifact Builtin、Core MCP Resource Tools、Runner adapters 和 optional generate-image。新增技能读取/准备/安装/分配适配，共用 UI 的正式安装/分配服务；固定读取/准备工具的暴露规则，确保下一轮技能有合法使用入口但不热改 Tool Set。Add Skills 的第三方查找/下载必须有已授权的真实能力，不用提示词冒充后端。每个 Tool 调用领域服务，区分 operation/业务幂等与传输 request_id，传递 expected_version，不复制状态机。Scheduler/Meeting Tool 在 D23/D24 随其服务实现后注册。

验收：逐 Tool 核对 schema、风险、scope、idempotency、错误与结果预算；Builtin/Runner/MCP 走同一授权入口；Core tools 不可关闭但仍隔离；image selector 不可用时不暴露可执行 generate-image。台账维护设计 Tool 目录与实现映射，不漏能力、不注册空 backend。

## 阶段 6 完成统一执行引擎

### D22 Executor 与 Loop

依据：[Executor](../architecture/agent-executor/README.md)和[Agent Loop](../architecture/agent-loop/README.md)全部专题。

将 Executor/Loop 作为一个完整执行工作项，内部顺序：Execution 数据/slot/launch → immutable context/snapshot 与 Trigger provider registry → Transcript/Model context → TurnProcessor/Tool batch → waiting/resume/cancel → budget/compaction/doom-loop/watchdog → checkpoint/restart recovery → 综合验收。这样不把执行生命周期与恢复遗留到未来阶段。

实现幂等 launch、Agent 全局 slot 原子约束、preparing failure、canonical transcript 与完整 call/result pairing、固定 Prompt components、模型流与工具结果、等待对象关联、resume 幂等、安全 checkpoint、压缩原子激活和生命周期查询。Runtime semantic update 端口本项固定，D25 实现展示投影。

Launch 同键同语义返回原结果、不同语义拒绝，再由真正新请求竞争 slot；提供按原 Launch key 的只读结果查询端口。Meeting 来源四项身份由 Meeting Provider 校验，Executor 不直接查业务表。Model System 拥有请求自动重试，Loop 不叠加，真实 attempts 与用量/部分流保持可追踪。

保留 startup Snapshot/工具定义不可变，运行中新增 Skill binding 在下一 Model Request 输入确定点可靠应用，checkpoint/Transcript/compaction 可重建固定版本；分配不解除 waiting，移除不编排执行而由后续资源授权返回正常错误。Runtime 更新进度独立于条目创建 seq，保证实际快照/stream 衔接所需的生产与读取端口。

验收：Task/Meeting 类 Trigger 并发启动同 Agent 最多一个非终态执行；waiting 保留 slot；terminal 不复活；审批恢复继续同一个执行；取消不暗中重做已有副作用；waiting 内存释放与恢复；请求中断、输出截断、doom loop、context overflow 与压缩无进展；在模型、工具、等待、checkpoint 各边界注入 Central 重启，验证不重复不安全动作。

Execution succeeded 不等于 Task done；Loop 不直接写项目领域表。执行总时长不设架构未规定的强制终止，不添加未经设计的 max turns 或通用 steering queue。

## 阶段 7 完成调度与会议

### D23 Scheduler

依据：[Scheduler](../architecture/scheduler/README.md)、[Loop](../architecture/scheduler/scheduler-loop.md)、[Dispatch](../architecture/scheduler/scheduler-dispatch.md)、[Reconciliation](../architecture/scheduler/task-reconciliation.md)。

实现 Project-local 调度、Current Sprint、固定组顺序与组内排序、并发额度、claim transaction、durable dispatch、Executor launch idempotency、relaunch cooldown、pause/resume、依赖恢复、用户取消保护和重启恢复；注册 Scheduler Tool，真实绑定 D11 占用查询端口与 TaskContextProvider。

未知 Launch 沿现有串行 traversal/recovery/pacing，按原 Dispatch/key 查询 Executor 持久事实；无法确认则继续保留待确认，不因有限启动重试耗尽而 failed/blocked，不反复换 key 或把 Launch 当纯查询，不新增检查服务/独立恢复 worker/专门人工兜底。Task 已 done/cancelled 的旧 pending 仍须沿同一路径核对；暂停不借核对执行新 Launch 或 Task mutation。

验收：同 Task 不重复 claim/launch；pending 预占额度、关联 Execution 后不双计数；Scheduler 额度计 created/preparing/running，Agent slot 另含 waiting；Current Sprint 切换/complete 与 claim 竞争；Busy 补偿不覆盖用户变化或失效旧 rank；读取现有 assignee、不自选 Agent、不由 Execution 终态解释 Task 结果；普通确认失败处理和 relaunch cooldown 仍保留。

### D24 Meeting

依据：[Meeting](../architecture/meeting/README.md)下 Domain/Turn/Context/Reference/Timeline 专题。

内部顺序：Session/participant/message/reference → Turn/contribution → sequential/parallel/queue/interrupt → Trigger provider 与 Executor binding → Decision/Approval waiting → retry/regenerate/finalize/summary → Timeline 来源接口 → 综合验收。注册 Meeting Tool 与模型引用替换适配器。

实现 proposed/active/archive、无独立 MeetingProposal、结构化 inline reference 与 mention、文件/Task/Knowledge 引用、Contribution Execution 幂等、Agent busy/waiting_for_agent、Decision answer/skip、统一审批引用、首轮标题和四字段 summary 原子提交、后续 rolling summary、archive/delete。

验收：mention 不自动等于参与编排；sequential 可见前序结果，parallel 固定 references/summary/有效消息 generation，不复制全部引用正文；queue/interrupt、忙 Agent、等待恢复、取消/retry/regenerate。Summary 幂等包含实际有序输入版本，不能仅用末条 ID；历史回复成功替换后显示待更新，等下一正常 Turn finalize，不额外刷新/重开历史 finalize。摘要失败有限重试后仍 finalizing、queued 等待；首轮标题/摘要原子提交，之后不重写标题。

Meeting archive 仅组织/可见性，不套用 Project 停止策略；hard delete 禁止新活动并停止本会议尚未完成的执行/收尾后清理，保留独立 Execution/Approval/Audit，不停止无关任务。验证 finalizing 取消、迟到结果不复活、明确失败/待处理及订阅失效，不新增删除修复平台。

## 阶段 8 完成投影与实时集成

### D25 Runtime View Realtime 与 Human Inbox

依据：[Runtime View](../architecture/agent-executor/runtime-view.md)、[Realtime](../architecture/platform-infrastructure/realtime.md)、[Inbox](../architecture/platform-infrastructure/human-inbox.md)、[Meeting Timeline](../architecture/meeting/meeting-timeline-realtime.md)。

依次完成：Runtime Item snapshot/update/detail/recovery → authenticated WS subscription/fan-out/backpressure → Inbox stable identity/action routing/rebuild → Task/Meeting/Approval 联合投影。平台只负责传输和投影，不复制领域状态机。

开工前补齐具体接口：本人跨项目 Inbox 聚合与稳定分页/订阅授权（管理员不越权）、管理员只读系统版本/依赖 readiness、Provider 明确可公开的 reasoning 摘要与敏感 payload 过滤。私有推理/metadata 不进入展示，无公开内容时只显示状态/耗时。

验收：每 Execution 更新进度与 item seq 分离，快照水位对应实际内容；覆盖同一旧 item 的 delta/completed，以及订阅前已发送但未 flush 间隙；重复/乱序/增量缺口、分页筛选、重启和 resync。保持流式和批量快照，不逐 token 写库或持久化 delta 日志。验证背压/权限丢失/对象删除、Inbox 从事实源重建与不可忽略 Approval；动作重新授权，Realtime 不承担业务正确性的可靠消息保障。

## 阶段 9 完成正式前端

### D26 客户端认证与应用框架

依据：[前端开发](frontend/README.md)、[应用框架](../frontend-design/layouts/application-shell.md)、[账号入口](../frontend-design/layouts/account-entry.md)、[项目工作区](../frontend-design/layouts/project-workspace.md)。

实现统一 typed API client、Session/CSRF/人机挑战、错误/version 冲突、取消与加载状态、realtime 同步/refetch、用户名/项目名路径与带类型的对象深链接、系统与项目双导航、账号页面、个人设置。改名后旧链接不跳转；登录返回目标重新授权。复用已确认 UI components/tokens，账号主题服务端持久化、保存/取消和预览回退；保持 Debug 仅开发构建。

验收：真实登录/退出/过期与导航恢复；API 错误不显示保存成功；直接对象链接优先；项目导航不污染系统页面；主题持久化；生产资源不含 Debug。运行工程检查，并进行真实浏览器键盘/焦点、浅深色、窄屏、reduced motion/overflow 检查。

### D27 业务页面与设置

依据：[前端布局索引](../frontend-design/README.md)。

连续完成页面组，每组完整验收后再下一组：系统项目列表/项目工作区 → 三类设置（个人已由 D26 完成，补齐系统/项目）→ Task Kanban/Explore/detail → Knowledge tree/editor/preview → Meeting composer/messages/details → Inbox 与统一 Approval/Decision → 跨页面回归。

设置覆盖 Model/Provider/selector、Agent 名称/模板/capability、项目 Skills 导入/显式更新/分配、Secret/Mount、Runner、MCP、项目归档/恢复/永久删除、变量/调度/summary 配置、账号邀请/SMTP、认证期限/挑战阈值/出站规则/检索参数与只读系统运行信息。布局规定的 Dashboard 保留容器；全局搜索保留已定容器，检索范围需另立设计工作项。

验收每页的正常、加载、空、失败、无权限、删除、version 冲突和重连状态；显式保存与未保存提示，失败保留输入；Task 切换保持 Sprint；知识树按需分页/全项目标题定位、子树删除明示当前范围；Meeting ordered inline nodes/草稿参与者转换和摘要待更新正确；多个页面处理同一个持久审批对象。

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
| 导入/获取 Skill 包 → 安装 → 分配 → 下一轮读取 → 多 Runner 准备/使用 | 安装与分配分开，默认启用可禁用、固定 revision、无凭据暴露、真实存储端点可达 |
| Owner 登录 → Central Tunnel → Session 撤销/Runner 断连 | 复用身份、HTTP/WS 均授权、不透传平台凭据，入口及时关闭且不自动恢复 |
| Runner/Central/Provider/MinIO/PostgreSQL 故障注入 | 明确失败或可解释恢复，不丢事实、不盲重试 |
| Project 归档/恢复/永久删除；Meeting/文档/Agent/MCP/Model 删除与引用替换 | 停止边界、名称释放、历史投影、索引/对象/Runner 资源清理符合各领域契约 |

部署验收使用授权的测试环境。本轮 `main` 同步与推送已有用户明确授权；发布或其他实际部署按对应用户授权执行。

门槛：所有前置模块通过、Tool/页面/事件覆盖映射无缺口、未完成端口真实绑定、适用数据库/协议/前端/端到端验收通过、无未说明的高影响故障或恢复缺口。记录可选设备/Provider 的实际验证范围，不把未测能力说成已验收。

## 阶段 11 完成平台实战与游戏验收

### E01 平台组织游戏复刻与最终验收

依据：[E01 工作项](work-items/platform-game-acceptance.md)。D28 及全部前置模块通过后，先确定 Minecraft 或 Terraria、确切原版参考版本、完整内容清单、权重、关键系统门槛与可复现算法并冻结；当前仅归位已确认目标，不提前开发游戏或宣称已选择版本。

通过实际运行的平台建立 Project、任务与 Agent 协作，真实执行实现、测试和审核，交付可运行游戏及逐项证据。以冻结的完整分母计算加权覆盖率，未实现、未验证和未通过的内容计零，禁止重复计分；总覆盖率须至少 50%，核心玩法与主要系统门槛须全部通过。实际试玩必须覆盖连续核心循环与保存恢复，发现的平台问题须修复并重跑受影响流程。

E01 完成还要求平台任务、Agent 协作、Execution、审核、代码与可玩产物可相互追溯，第三方能够按记录重建、试玩并复算结果。界面展示、截图或在平台外完成后补录任务不能代替平台实战证据。完整开工、评分、返修与完成要求以 E01 工作项为准。

## AI 如何执行和恢复

沿用[主线程统筹的开发团队](agent-team/README.md)：主线程专注用户沟通、工程决定、拆解调度、返修协调、最终整合与提交；测试计划、证据分析、故障归因和独立验收由专门验收负责人承担。六种角色按需使用，全部保持 `gpt-6-astra / max`，子 agent 不再委派。并行度由真实依赖、稳定正式契约及文件、迁移和共享资源所有权决定，不设单活动模块或常态两个、最多三个子任务的限制。当前工具提供含主线程在内的 7 个总席位，按实际可用席位调度，简单任务一个执行者。

每次恢复先读取 `AGENTS.md`、本计划、[台账](agent-team/tasks.md)、各活动工作项规格与实际 Git 差异。计划编号 D00–D28 表示模块及规划优先级，E01 表示其后的最终实战验收，团队台账 AT 编号表示实际执行记录，三者关联，不互相替代。

执行步骤：

1. 找到尚未完成且真实依赖已满足的工作项或完整结果任务，优先处理关键依赖，并核对各自的直接及传递依赖证据；不能凭上一会话的口头总结宣布通过。
2. 主线程安排模块规格并关闭影响该项的未决接口，采纳设计或调研角色的建议。使用[任务模板](agent-team/task-template.md)按完整结果拆分，引用共享规则和接口，维护跨任务依赖、授权文件、全局迁移及共享资源唯一所有权和状态。
3. 按需下发可执行任务，执行者完成实现、自测与局部文档同步；在授权范围内自主排查，缺少决定、越出范围或无新证据的阻塞交回主线程。必要返修仍属于当前模块，单卡完成不代表整个模块完成。
4. 在交付范围停止写入或形成精确冻结副本后按风险验证：低影响调整由作者自查加主线程审查；业务变更由专门验收负责人安排为独立审查范围，由未参与相应实现的实例分析证据和失败，高风险关键场景必须独立验证。需要多个验证实例时由主线程分派。主线程核对实际 diff、关键边界、集成及验收结论；输入及依赖未变的通过检查可复用，相关修改后重跑受影响检查。
5. 相关写入全部停止且当前独立可验证的小块通过验收后，主线程更新台账、提交到 `main` 并及时推送。记录规格路径/修订、卡片进度、实际检查及稳定输入证据、限制、未完成端口绑定、阻塞、提交定位、推送结果、未提交文件和下一步。小块提交不代表模块完成；完整模块通过门槛后才标记“已完成”，其他依赖已满足的任务可继续并行；发现上游 bug 先返修上游，不在下游复制补丁。

状态使用“待开始 / 设计中 / 实现中 / 验收中 / 阻塞 / 已完成”。只有通过门槛才标记已完成；各模块和任务分别记录阶段、依赖与所有权，不以并行开工改写未完成事实。本轮 D01–D28 和 E01 的持续推进及独立可验证小块的 `main` 提交、同步与推送已有用户授权，满足依赖后连续推进，不逐模块或逐卡重复确认；尚未落实的游戏版本与完整清单在 E01 正式执行前形成并冻结，不把当前空缺写成已定选择。用户调整需求或调度所有权变化时，保留改动、暂停受影响步骤，更新规格和修订号后重新下发；无依赖的任务可继续。中途交接先确认子 agent 与命令停止写入，并同步台账，不依赖聊天中的完成声明。

检查命令必须随工程初始化确定。后端建立后至少有对应范围的 Go test/vet/build；并发与恢复模块使用 race 和真实数据库测试。前端使用现有 npm 脚本。纯文档变更只检查链接、内容一致性和 `git diff --check`，不声称产品行为通过。

后续可给 AI 的启动指令：

> 按 docs/development/development-plan.md 持续完成 D01–D28，再通过 E01 平台实战与游戏复刻验收。先读取 AGENTS.md、团队台账、活动工作项规格和实际仓库状态，从真实依赖已满足的未完成任务恢复。由主线程专注产品沟通、规格协调、调度和最终整合；先定正式契约与验收，再按真实依赖和文件、迁移、资源唯一所有权安排独立模块或完整结果任务并行，最多使用当前工具含主线程在内的 7 个总席位。全部代理使用 gpt-6-astra/max，子 agent 不再委派。执行者自测，专门验收负责人分析测试证据与故障并独立验证，稳定输入的检查证据可复用；各模块完成门槛不降低。保留已有前端基础，不用临时 stub 或演示数据充当正式实现。独立可验证的小块验收后同步证据、台账、main 提交并及时推送，沿用已有用户授权；E01 先冻结完整分母和权重，再通过平台实际组织游戏开发、试玩、计分与平台返修。未决产品规则和实质范围问题由主线程处理。

## 当前启动点

计划制定时 D00–D28 尚未执行，规划阅读不等于 D00 完整审计。当前 D00 已按 [规格](work-items/d00-baseline-audit.md)完成审计、独立验收与主线程审查，交付和验证边界见 [审计报告](work-items/d00-baseline-audit-report.md)及 [任务台账 AT-0004](agent-team/tasks.md#at-0004d00-设计与实现基线核对)。AT-0005 已完成 P01–P25、C01–C08 的决定归位、独立文档验收及开发门槛同步；讨论中的历史待定状态不再作为重问依据。D01 已按[规格](work-items/d01-cross-module-contracts.md)完成[跨模块契约](work-items/d01-contracts/README.md)、W01–W43 接口走查与独立验收；该阶段尚未实现生产端口，后续绑定见各模块记录。D02 已按[规格](work-items/d02-engineering-foundation.md)完成 B01 基础与 HTTP、B02 配置/入口/生命周期，实现及真实进程独立验收通过。D03 已按[规格](work-items/d03-postgresql-foundation.md)完成 B01 数据库/事务锁/迁移与 B02 入口集成，真实PG17.8/vector0.8.1、进程取消/健康/关闭及独立验收通过。D04 已按[规格](work-items/d04-security-foundation.md)完成 B01 Audit/cursor、B02 Secret/轮换和 B03 受控出站/入口，真实PG/网络/进程完整检查及独立验收通过。D05 已按[主卡](work-items/d05-object-storage-artifact.md)与设计修订6完成B01对象可靠存储、B02 Artifact/浏览器下载、B03 Runner单对象传输及Central对象初始化/恢复/健康/停机，完整真实PG/MinIO/TLS/进程无过滤兼容、独立风险探针与原缺陷复验均通过，root审查采纳。D06 已按[主卡](work-items/d06-transactional-outbox.md)修订2/设计修订5完成持久事件、可运行投递/恢复、当前授权重投/诊断、Project生命周期与Central共享ProcessGuard组合，B01/B02及必要上游返修经独立审查、原缺陷复验和单次无过滤兼容通过。D07 当前范围已完成，首个未完成项为 **D08 Project 与 Owner**。[D07主卡](work-items/d07-account-session-smtp.md)修订2已采纳B03所依设计修订6与B04补充rev7（含持久投递端口、准入及恢复闭环），B01身份与安全基础及必要D03取消归因修复已通过独立原探针、check-go和包级串行无过滤12包完整验收；默认并行历史超时/初始化错误及未定根因保留，不反推为负载已证。B02邀请、恢复与挑战已通过独立核心审查、真实挑战/浏览器、修后check-go及61项三组穷尽补验，结合原无过滤其余11包通过采纳组合兼容；原Account累计6m超时、57014及历史浏览器失败证据保留，不称单次无过滤全绿或唯一根因已知。B03持久SMTP投递已按冻结rev6及补遗独立采纳，精确64源提交推送`ffa65f0`；原R01/R02真实红以同字节probe闭环，修后A24/B32/Mail31/原五及旧372十一包各一次通过，保原race/每包6m与断言，no-tests不算兼容。21外层nonce及owned资源清零、V全停；原历史红/分组组合限制保留，不声称一次无过滤全绿。B04正式设计rev7及后续最小补口已归位（该阶段补口提交`8566ea5`），C0契约两源`06346b8`、A1头像/profile纯块八源`59b38c8`、配置与测试环境五源`0f2b9ee`、B1纯HTTP十一源`e8941e8`均已采纳并提交推送；A1真实PNG初红及B1/config准备与实现失败均保留，具体证据及限制见[D07纯块记录](work-items/d07-account-session-smtp.md#b04纯块采纳与后段候选并行)。独立V另对已提交`e8941e8`只读快照的Central/Runner各构建一次通过，没有执行产物或启动服务。00012 `61b4df4`、CAPTCHA 两源 `b9b0b86` 与 A2 Profile/Avatar/Runtime 27 源 `da5caab` 均已独立验收并提交。B37 最后精确 37 源已独立采纳并提交推送 `022dcea`，真实 Account/Profile/Avatar/System HTTP、SMTP/Mail/受限日志与唯一 `account.mail-enqueue` 根装配已成立。验收负责人 T01–T14 关闭核对 `/tmp/agenteam-d07-close-gates-evpk9gwi/gate-map.md`（SHA `e6a31ef1ac305bc6694843d2fa0391c8e1197ca4037b3073162d09f278fefef0`）确认当前业务/测试无未闭合门槛；本次十二文档/样例已归位，通过作者格式、链接及字段自查和验收负责人独立核准；D07 当前范围已完成，关闭文档已提交推送 `0ed8085`。完整 check-go、旧域完整包与受影响补验、Account95 穷尽分组、Mail31/library67、真实 app/Sink 及最后899/900交集和正常双 binary 的实际证据见[D07 终局记录](work-items/d07-account-session-smtp.md#b04-终局采纳与文档关闭)。legacy18 原 8 PASS/3 FAIL 永久保留；两个历史 SMTP setup/D05 签发准备原因未知，后验通过不反推原因。process29 PASS/exact3 清零、内核 outer exit0 可恢复，但原 owner wait 未观察、observer SIGKILL9，不能称原 runner joined；CAPTCHA 原随机样本未保存，固定 SDK 机制修复不能唯一回溯原样本。这是批准的组合兼容，不称一次单体全绿。B03 原566及各阶段冻结输入仍各自成立，D09 C0 后续纯契约提交不混入 B37 证据。当前 identity/System/avatar 已绑定，Project/Runner 和未来领域生产 provider 仍按后续责任模块接入；D25 WS、D26/D27 页面与 ready503 是明确后续边界，不作 D07 缺口。D08–D28/E01尚未完成。

[D08主卡](work-items/d08-project-owner.md)与[设计rev2](work-items/d08-project-owner-design.md)已通过独立规格审查，初次归位提交`08d119d`；B01正式契约/纯规则库已验并提交推送`199554b`，原Retry联合型与completed-delete输出反例闭环记录保留。B02七个纯契约/角色/Audit结果提交`769ec8c`，其余21源、00013与fixture接缝已在固定`da5caab`（含已验A2/00012）上通过独立验收并提交推送`6319d03`。首轮三红与四文件窄修证据保留；修后唯一完整Project组及原字节独立1主6子probe在race/count1/每包6m下通过，Project95.075s，实际20业务主例，其他包no-tests不计兼容。报告与指纹见[D08 B02记录](work-items/d08-project-owner.md#b02-独立验收与提交)。B03 C0 四源 `16595ad` 与 00014 独立迁移两源 `30f5c29` 已验；A/P 主体仍未整体采纳。A 的原 nullable 问题已窄修，但 next23 的 Transfer 收敛后段仍红；P 原 11 项及新增 4 项边界按明确组合局部通过，实际 P+D/Object checker 尚未验。Object fact checker 新三文件及一个旧 hook 由 P 独占实施，不算已绑定。先前各轮原始失败、局部通过和所有权交接保留在[任务台账](agent-team/tasks.md#b03-当前局部验证与所有权)。D08 完整生命周期、HTTP/app 和 D10 生产初始化仍未完成；资源仅由 acceptance 明确交接。

E01 当前为待开始，仅完成最终目标与验收规则的持久记录。D01 已完成静态契约验收，D02–D06 已完整验收，D07 当前业务、测试与文档门槛均通过，当前范围已完成；D09 C0 `e6e94c4` 与 B01-K System 配置 `26622bc`/`543511c` 已验，不代表完整 D09 完成。其他真实依赖已满足的任务可按本计划并行调度，不改变各模块未完成状态。新增 E01 不构成游戏开工、版本选择、清单冻结或任何产品能力已通过验收的证据。

[D09主卡](work-items/d09-model-system-token-usage.md)与[设计](work-items/d09-model-system-token-usage-design.md)初次归位为 `43b8886`，C0 已验 `e6e94c4`。[B01-K System 配置](work-items/d09-b01-system-configuration.md)已完整独立验收：Audit 六源 `26622bc`、System 二十九源（含 00015）`543511c` 已推送，作者 12 个真实顶层及独立 CredentialRef 跨 scope/receipt 后 Session 撤销 1 个顶层通过。00015 沿已验 00014 `30f5c29` 完成连续迁移与旧 Audit 兼容；原红与证据边界见 B01-K 卡。Project 分支、Resolver/Usage、Provider 调用、根 HTTP 不在该完成范围，D09 模块未完成。

[D12主卡](work-items/d12-knowledge-documents.md)、[设计](work-items/d12-knowledge-documents-design.md)与 B01 十二源已验 `914fd84`；正式 [B02 实施卡](work-items/d12-b02-knowledge-service.md)及 C1 cursor.Text 已验 `71dc176`，C2 Knowledge Audit 闭集已验 `f401c15`，C3 五路径共享清理已独立验收并提交 `231a384`。C3 首轮 fixture 四红及修后四项/旧六项组合记录保留，不称单次全绿。Knowledge 主体由 `parallel_plan` 实施；真实 Knowledge 授权、事务、对象 lease、A/P 与 Object checker 组合仍待完成，D13 不作为 canonical 写的全局前置。D12 模块未完成。D10 S01 的真实 builtin/包载体纯块由 V 完成作者验证、待独立验收，生产初始化仍须真实 D08/D05 与生命周期组合。当前所有权见[任务台账摘要](agent-team/tasks.md#当前恢复点与并行所有权)。
