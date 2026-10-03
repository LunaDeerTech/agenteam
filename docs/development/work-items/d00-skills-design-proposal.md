# Skills 跨 Central、对象存储与 Runner 设计草案

状态：历史设计提案，保留讨论过程与工程建议；正式归位由 [AT-0005 同步任务卡](d00-decision-sync.md)执行，归位后的业务规则以 [Skills 架构](../../architecture/agent-skills.md)为准，具体接口仍待 D01/责任模块，不是已验收实现。用户在 P07-07 选择以 `SKILL.md` 为入口的技能包，并要求结合对象存储与远程 Runner 设计；用户在 P07-08 进一步选择 Runner 直连对象存储（B）；P07-09 已选择仅项目级技能库；P07-10 确定技能逐 Agent 可见、用户 UI 仅导入；P07-11 指定不可删除的 Add Skills 引导技能与统一安装工具，P07-13 明确二者默认启用且用户可按 Agent 禁用；P07-14 新增可选 assign-skill 分配工具，支持同项目 Agent 技能配置；具体存储模型与端口仍需落实。

关联：[决策记录](d00-audit-decisions.md)、[Agent Management](../../architecture/agent-management.md)、[Object Storage](../../architecture/platform-infrastructure/object-storage.md)、[Execution Context](../../architecture/agent-executor/execution-context.md)、[Agent Workspace](../../architecture/runner/agent-workspace.md)、[Data Channel](../../architecture/runner/data-channel.md)。

## 1. 已知事实与建议边界

- Agent Loop 和模型上下文在 Central 中运行，Runner 提供远程文件、命令与进程环境。读取技能说明不要求 Runner 已上线，也不意味着执行其中脚本。
- 技能是独立业务资源，通过 ObjectStorageService 使用 StoredObject；不能仅因文件性质将其作为 Artifact 或 KnowledgeDocument，继承不匹配的权限和生命周期。
- MinIO 保存技能包实际字节，PostgreSQL 保存业务元数据、版本、引用及文件索引。Agent 不获得 bucket/key、MinIO 凭据或 Signed URL。
- Agent 配置保存技能稳定引用；Execution 启动时固定初始技能版本，运行中新分配技能通过 §3a 的显式变更记录追加绑定。已经绑定的版本不因库更新而自动变化，技能移除对运行中执行按 P07-19 不额外干预，后续调用走正常授权/错误处理；包删除与历史对象保留矩阵仍须在规格明确。
- 技能包不创建新的执行环境。脚本使用当前 Execution 已获授权的 Mount、Runner Tools 与审批规则，不在 Central 执行，不自动安装依赖或启动新容器。

## 2. 包与版本的建议模型

一个上传包包含根入口 `SKILL.md`，以及可选的 `scripts/`、`references/`、`assets/` 等相对目录。例如：

```text
report-helper/
├── SKILL.md
├── scripts/build-report.py
├── references/format.md
└── assets/template.html
```

建议每次发布生成不可变 revision；稳定技能资源包含名称、简介、scope、启用状态和当前 revision。revision 包含原始包 StoredObject 引用、校验值、经验证的文件清单与入口信息。

原始包是该 revision 的内容事实来源。为按需读取单个说明或参考文件，可以生成派生文件对象/缓存，但必须绑定 revision 与文件校验值、可从原包重建，不能独立编辑造成两套正文。具体包格式、标准 frontmatter 支持范围及缓存形式在规格固定，不能宣称已支持全部 Agent Skills 扩展。

上传阶段不执行包中内容。先检查结构、入口、大小及文件清单，再发布可引用 revision；存储成功但发布失败的对象须纳入可恢复清理。解包拒绝越界路径、绝对路径、符号/硬链接、特殊设备文件、重复/规范化碰撞路径以及超出资源限制的包。限制压缩和解压体积、文件数量、单文件大小与处理时间。

P07-09 已确定 Skill 仅属于 Project，不提供系统技能库，系统 Preset 不保存项目技能推荐。用户 UI 提供导入入口，Agent 通过项目内置 Add Skills 引导技能获取/编写标准包，再调用获授权的统一安装能力；标识、字段及索引这里只是职责建议，不是已确认数据库 schema。

## 3. Central 如何加载说明

1. 创建 Execution 时，按正式 Skill 查询端口解析 Agent 的技能引用，固定技能 ID/revision、名称、简介及入口引用。
2. 初始模型上下文只包含本次推荐/选择技能的名称/简介和读取方法，不一次性注入所有 `SKILL.md` 与附件。P07-10 已确定按 Agent 的技能引用过滤可见列表，不开放项目全库；目录摘要可见不代表完整正文自动注入。
3. 需要某个技能时，模型调用有业务语义的读取工具。建议能力类似 `read-skill`，输入技能身份及包内相对文件路径；它不是原始对象读取 API。
4. Central 校验 Project scope、当前 Execution 的有效授权与已生效的固定 revision，从 ObjectStorageService 读取对应内容，返回经过大小/媒体校验的说明或参考文本。运行中新技能通过 §3a 的显式绑定生效，不能修改已经固定的版本冒充同一输入。
5. 返回内容进入正式 ToolOperation/Attempt 与 Transcript；大结果遵循现有外部化/投影规则。二进制素材不作为说明文本塞入上下文。

纯说明型技能可在没有 Runner 的情况下使用；指向脚本/素材的相对引用必须能解析到同一 revision，不把 Central 本地路径返回给模型。技能说明不能授予工具权限或修改系统安全规则。

读取工具名称、schema、是否随已授权技能条件暴露、与 Core Tools 的关系，需要 D18/D21 正式确定；此草案不擅自增加不可关闭的 Core Tool。

## 3a. 下一轮生效与 Execution 快照的兼容设计（P07-17）

用户希望新分配技能在当前执行下一轮可用，并要求核对现有架构。核对结论：现有文档确有需要修订的契约，不能称为直接支持。

| 依据 | 当前规则 | 对新需求的影响 |
| --- | --- | --- |
| Execution Context §2、§10 | 启动 Context 不可变，启动后 Agent 配置变化不影响当前 Execution | Agent capability 包括技能配置，直接读取最新配置替换快照会违反现有规则。 |
| Execution Domain Model §4 | preparing 完成后 Snapshot 是不可变事实 | 保留启动事实，新增内容应记录为运行中的事实。 |
| Tool Definition & Registry §6 | ToolSpec/schema/binding 固定，配置变化默认影响新 Execution | 新技能必须通过本次已有的读取/准备/命令工具使用，不自动增加工具或修改 schema。 |
| Loop Runtime §4–5、§18–19 | 稳定轮次边界消费 typed control input，不提供任意 Steering Queue | 可扩展正式技能变更输入；不是让模型直接读表或任意插入用户 Prompt。 |
| Transcript Context §18、§40–41 | 每轮从稳定状态生成视图，Transcript/Checkpoint 可恢复 | 可将已生效的技能绑定加入运行状态并持久化，支持恢复与解释当时输入。 |

建议补充以下明确规则，以落实用户希望的 A：

1. **保留启动快照。** 初始 Agent/Model/Tool/Prompt/Skill 引用仍不可变；允许后续 Skill assignment 通过专用运行时契约追加技能绑定，形成“初始状态 + 已确认变更”，不直接重写启动配置。
2. **可信变更入口。** UI 与 assign-skill 仍调用同一服务。通过跨模块正式事务端口，为目标活动 Execution 持久化技能分配变更；至少带变更 identity、目标 Agent/Execution、assignment revision 和新增 skill ID/不可变 revision。具体字段/锁序由 D01/D10/D22 固定，不依赖无持久化保障的内存通知。
3. **精确定义下一轮。** 在下一次 Model Request 的输入确定点，消费此前已提交且尚未应用的技能变更。已经发出的模型请求和当前 Tool Batch 保持完整；在输入确定点之后提交的变更进入再下一轮。同一批 Tool Call 不因先后完成时差获得不同的新增技能集合。
4. **可靠交付。** Agent 配置提交与活动 Execution 变更投递须具有一致性保障，启动/准备与分配并发不漏变更；不以可能延迟的纯异步通知承诺严格下一轮生效。幂等/乱序、assignment revision 去重及提交后响应丢失按正式端口处理。
5. **版本固定。** 新绑定记录本次采用的 skill revision/manifest identity；读取 SKILL.md、附件与 Runner 落地使用同一版本。重复通知不重复绑定，已绑定版本不会因发布新包或恢复执行被替换成最新内容。
6. **模型可见与恢复。** Executor 将已验证变更转为正式 typed control input（名称暂定 skill_assignment_changed），在 Canonical Transcript 留下必要事实；下一轮模型得知新增技能名称/简介，完整内容仍按需读取。Checkpoint 保存绑定状态/变更位置，恢复不重新猜测当时配置；Compaction 后仍能重建当前有效技能目录，不能只依赖一条可能被摘要省略的提示。
7. **固定工具边界。** 此机制只追加技能资源绑定。本次 Execution 必须已有必要的通用技能读取/准备工具，以及实际要调用的命令等工具；缺少时明确报告限制，新技能不会自动授予 Tool、Secret、Mount 或突破 Execution Policy。技能工具的首版暴露规则须确保目标使用场景有真实入口，不能用动态技能需求暗中改写 Tool Set。
8. **生命周期一致。** 正在 waiting 的执行记录待处理变更，但分配技能不解决原审批/Decision，也不自动将 waiting 变为 running。已终态执行不复活；后续新 Execution 按新配置准备快照。
9. **移除与升级。** P07-18 确认显式升级发布新版本、运行中保留旧绑定。P07-19 确认移除/禁用不主动干预运行中的执行：不取消、不改写历史、不增加专用撤回通知；后续读取/准备按普通资源/权限检查返回 ToolError，由 Agent 处理。已读取内容与落地文件不主动收回。延迟的新增绑定在应用前必须确认分配仍有效，不能恢复已移除的授权。此需求不扩展为所有配置实时更新。

这是一项有限的架构扩展。正式落地需同步 Agent Management、Execution Context/Domain/Lifecycle、Loop Runtime、Transcript/Checkpoint 和技能工具规格；当前仍是讨论草案，原架构正文尚未修改。

验收须覆盖自分配/秘书分配/用户分配、已有请求与并发 Tool Batch、分配与启动/终止竞争、waiting、不重复投递、恢复/压缩后的有效目录、版本一致性、缺少工具权限及跨项目拒绝。D01 固定变更端口，D10 产生分配事实，D22 实现投递/轮次应用/恢复；D18/D21 绑定技能读取与执行。

## 4. Runner 如何获得脚本与素材

建议提供有业务语义的技能文件准备能力，例如 `prepare-skill`：

1. 模型明确选择当前已授权 Mount；Central 校验 Project、Agent、Execution、Skill revision、最新 Mount 有效性及此次 Execution Policy。
2. 仅传输所需的固定 revision，默认采用完整包落地以保留脚本相对导入、参考资料与素材的目录关系。具体传输格式由规范化 manifest 固定，不信任上传包提供的目标绝对路径。
3. Runner 在该 Agent 的 Workspace 中建立平台管理的技能版本目录，建议相对路径为 `.agenteam/skills/<skill-id>/<revision>/`；最终路径与保留目录规则待 Runner 规格确认。
4. 使用临时目录、校验 manifest/文件摘要、受控解包并原子发布完整目录。取消、断连、磁盘不足等失败清理临时结果，不能暴露半套包为成功。
5. Tool Result 只返回 Mount identity、技能 revision、工作区相对路径和明确结果，不返回下载凭据或宿主机绝对路径。多个 Runner 各自按需准备，不因创建 Agent 向所有 Runner 分发。
6. 模型通过现有命令/进程工具执行脚本，执行仍走正式权限、风险分类、审批、取消和结果记录。准备文件成功不等于脚本执行已被批准。

同一 Mount/revision 的重复准备应能校验后复用，失败可恢复。托管目录采用不可变版本路径且与普通工作文件分开，防止意外覆盖；首版 Runner 是 trusted-host，无 OS sandbox，不能将文件只读属性宣称为对恶意命令的隔离保障。

包中入口声明和脚本扩展名不保证 Runner 有所需解释器/工具。缺少环境能力明确返回错误；不在准备阶段隐式执行安装脚本。

## 5. 已决定的文件传输路径（P07-08：B）

| 方案 | 数据路径 | 部署与设计影响 |
| --- | --- | --- |
| A：Central 中转，未采纳 | ObjectStorageService → Central → 已认证 Data Channel → Runner | Runner 只需能访问 Central；MinIO 可保持 Docker 内网。Central 承担流量，必须流式传输并设置背压/取消/大小限制。需要显式调整 Data Channel §16 对对象直传的固定边界，不能视为现有实现。 |
| B：Runner 直连对象存储，已选择 | Central 授权并下发单对象短期访问材料 → Runner 下载对象 | 保留现有 Data Channel §16–17 对对象直传的设计；需提供 Runner 可达的对象存储端点及相应部署/TLS 配置。短期访问材料只在受信任协议中传递，不进入模型、普通日志或持久化缓存。 |

现有架构规定 Data Channel 处理 Runner/Central 文件传输，对象传输由控制通道协商后 Runner 直连存储。用户选择 B，保留该固定边界；A 仅作取舍记录，不实施，也不作为自动回退。B 不等于提供匿名公开 bucket，存储端点可经内网/VPN 或受保护 HTTPS 对 Runner 可达，不要求暴露管理控制台。部署端点/TLS 配置遵循环境变量原则，并验证上传/下载实际可达性。

两个方案都使用 ObjectStorageService 的正式能力，不让业务模块或模型直接操作 MinIO，也不把二进制 payload 塞进 Control Channel。

## 6. 生命周期、授权与验收边界

- 包版本与运行中 Execution 引用应持久化保护；新发布不覆盖运行中版本。P07-19 已确定移除 Agent 分配不主动干预运行，后续调用按正常权限/资源检查报错；项目库包删除与历史 payload 的保留/清理矩阵仍需规格确定。
- 移除 Agent 技能引用不直接递归删除整个 Workspace。托管缓存清理只处理已验证归属且没有活动使用的技能版本，不扩大到用户文件或共享目录。
- 项目归档停止运行并只读，项目永久删除按正式资源清理矩阵处理项目技能、对象与托管文件；首版没有系统技能。不能因为 Runner 不在线声称物理文件已清理。
- 校验正常说明读取、纯文本技能无 Runner、跨项目越权、固定 revision、并发发布/删除、包越界/压缩资源限制、模型上下文无凭据、相对素材引用、多 Runner 落地、断连/取消/重启、部分传输、环境缺失与脚本审批。

## 6a. Add Skills 引导与统一安装（P07-09–P07-11）

最终方向为：

```text
项目内置 Add Skills（不可删除）
├── 先查找第三方现有技能 → 获取合适的技能文件
└── 没有合适技能 → 编写或总结经验 → 生成技能文件
                  ↓
           形成标准技能包
                  ↓
Agent 的 install-skill Tool ─┐
用户的上传/导入界面 ─────────┴─→ Skill Domain 安装服务
                                ↓
                        校验 → 对象存储 → 发布版本
```

- Add Skills 名称暂定；它是项目内置受保护技能，而不是系统共享技能库。单独删除受禁止，项目永久删除仍可清理它；P07-13 覆盖最初不可禁用要求：所有 Agent 默认启用，但可按 Agent 禁用；内置内容升级/编辑政策需在规格明确。
- 指导内容描述标准包结构、优先复用现有技能、选择/获取/编写、素材和脚本相对路径、验证与安装步骤。读取指引不授予网络、文件、命令或其他工具权限。
- 所有技能按 Agent 可用引用过滤；Add Skills 默认加入但可由用户在某个 Agent 上禁用，项目内置资源本身不可单独删除。UI 管理目录与 Agent 可见目录分别授权；新查询按当前配置过滤可见目录；P07-19 不要求主动清除运行中已经取得的旧内容或改写旧上下文。完整说明仍按需读取，不要求每次执行都进行获取新技能的流程。
- 查找/下载/编写由引导技能使用 Agent 已获得的正式能力完成，不保留先前两个专门 create-skill/import-skill 工具提案。首版若缺少搜索或文件获取能力，必须明确实现责任、正式端口与真实验收，不用 Prompt、任意私有 API 调用或临时脚本旁路冒充能力。
- 一个统一安装工具（暂称 install-skill）接收受控标准包材料。候选输入为当前 Project 的平台包文件引用或已授权 Mount 中的包引用；具体 typed source_ref、来源绑定、文件完整性及无 Runner 的文本包生成方案在规格冻结。
- 用户 UI 只提供技能包上传/导入，不增加人工创建/编写流程；上传入口与 Agent Tool 都调用同一业务安装服务，使用一致的格式、校验、版本和错误规则。
- 所有新包都先检查结构、入口与内容限制，再发布；安装不执行包内安装/构建脚本。P07-18 已选择同名安装默认冲突，显式选择更新并校验目标/预期版本后才发布新 revision；运行中绑定的旧 revision 保留。P07-16 已明确安装不自动分配。UI 与工具共用冲突/更新/幂等语义。
- 第三方来源、精确版本与内容摘要用于追溯；来源信息不能替代包验证或夹带明文凭据，Agent 不持有 MinIO 凭据/Signed URL。
- 安装返回正式项目技能身份/revision，HTTP 下载或单个对象写入成功都不能单独代表安装成功。校验、对象写入或数据库发布失败须恢复/清理，重复调用幂等与变参冲突在规格明确。
- P07-13 确认安装工具是默认启用、可按 Agent 禁用的普通工具，不加入不可关闭 Core Tools。技能与工具按各自配置管理，显式禁用不得被模板复制、重启或读取引导暗中重新开启。实际安装仍校验 Capability、审批、Execution Policy 与项目归档/删除门禁。
- P07-14 新增普通可选 Builtin Tool（建议 assign-skill），复用用户后台勾选技能的同一分配服务，允许获授权的秘书 Agent 给当前 Project 的其他 Agent 配置技能，不属于不可禁用 Core Tools；未确定全体默认开启。
- 包安装/发布与 Agent 技能分配分为两个领域动作：install-skill 返回库中技能身份/revision，assign-skill 更新目标 Agent 的技能引用；管理工具不能顺带修改模型、工具、Mount 或 Secret 授权。
- 所有目标 Agent/Skill 都校验当前 Project，分配服务落实目标版本、幂等和并发冲突、资源生命周期及写门禁。添加/移除的具体操作 schema 与运行中生效范围在规格明确，不让完整列表覆盖请求悄悄丢失用户并发修改。
- P07-15 已选择 A：获有效 assign-skill 权限的 Agent 可查询当前项目全部合法技能的名称/简介并分配，不要求自己先拥有该技能。正文/脚本/素材仍按自身技能配置校验，管理目录查询不代替内容授权；未授权管理工具的 Agent 继续只看已分配技能。查询和分配均限制当前 Project 并校验 Execution Policy。
- P07-16 已选择 A：安装只发布到项目库，不自动分配给发起 Agent；后续通过 assign-skill 或后台配置明确分配，用户上传同样处理。安装成功但分配失败保留库中包并分别反馈结果；Add Skills 指引须说明这两步。P07-17 用户希望下一轮生效，架构核对与显式运行时绑定方案见 §3a，具体接口及验收仍须落实。

## 7. 对开发顺序的影响

D01 必须新增技能资源/版本/内置引导初始化/统一安装发布/Agent 技能分配/查询/读取/文件准备与清理的正式端口，以及 Execution 中固定技能身份的契约。D05 提供真实对象能力，D10 承担技能业务事实和 Agent 引用、内置指导内容/版本与统一安装服务，D08 的项目初始化通过正式端口在 D10 集成绑定；具体内部包按职责组织，不集中堆在 Agent Service。

D15–D17 提供真实 Runner 协议、路径解析及文件传输，D18/D21 提供正式工具适配与授权，D22 固定初始及运行中新增的 Execution 技能版本、消费正式分配变更并完成真实调用与恢复集成，D27 提供管理界面，D28 验证 Docker 与远程 Runner 的部署路径。技能正文读取可在 Central 侧先完成，远程文件准备只能以正式端口声明并由后续模块绑定，不能用成功 stub 验收。

责任分配为待审设计建议；正式接受后须同步 Agent Management、对象存储消费者说明、Execution Context、Tool Registry、Runner Workspace/Data Channel、前端设置与开发计划/任务卡。当前没有上传接口、依赖安装、数据库迁移或产品实现。
