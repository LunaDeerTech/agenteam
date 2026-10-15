# F0 完整页面样板

F0 已因用户实看反馈重开返修。用户未接受现有项目菜单、项目设置结构、下拉样式、页面层次及整体视觉品质，与 Linear 的差距和动效质感仍需改进；视觉系统数值待重新校准，F1—F8 正式产品迁移未开始。此前内部技术与交互检查的通过事实仍按当时输入保留，不代表用户已接受设计，也不能覆盖本轮修改后的新输入。

**当前 Agent 三 Tab 与独立保存候选已完成限定内部检查：基本配置手动保存，能力与资源按完整操作即时保存，记忆逐条操作；供用户实看，未获用户整体认可。** 方法与来源见[本地重设计技能](../../../../.agents/skills/agenteam-frontend-redesign/SKILL.md#从一个完整页面建立方法)及[来源说明](../../../development/agent-team/skill-sources.md#前端重设计方法的本地适配)。设置目录保留11个真实栏目，general、Agent 与 MCP 有样板内容，其余8项仍为静态栏目文字。MCP 本轮只改页头，对象结构保持；其他模板未获本轮整体验收，F0 未定稿。新 Tab 的代表视觉、跨 Tab 旅程、资源恢复与原生200%按各自实际输入检查，证据范围见下文；旧长表单与 general 的限定结果、用户去外框反馈仍保留原含义。

本样板使用依据当前字段构造的合成数据，业务动作只改变当前浏览器中的演示状态或显示演示反馈，不调用真实 API、不写入业务事实。PDF/文件下载使用本地生成内容，不能代证真实上传、解析、权限、恢复或生产接入。视觉候选与修订规则见[目标设计 §8](../target-design.md#8-f0-可实现视觉值与组件使用决定)；现有数值和下方页面结构均为本轮待核对、待重校的输入。

入口协议见[模块契约](module-contract.md)，原技术与交互结果见[作者浏览器检查](test-README.md)和[独立视觉审查](review/README.md)。新的内部审查须注明新输入及受影响范围；返修后如暂无新的用户认可，结论写为“已修订/内部审查结果，待用户实看反馈”，不把内部检查通过称为用户对信息结构与视觉品质的接受。

## 启动与参数

先获取并切换到 `ai/frontend-f0-refinement-20261015` 分支的最新提交；本轮暂不合并 main，仅更新 main 无法取得此示范。原型源码保存在仓库内 `docs/frontend-design/redesign/prototype/`，入口为 [index.html](index.html)，不是临时输出目录。只浏览样板需要 Python 3，不需要产品依赖。以下命令从该分支的仓库根目录执行，使用一个空闲的本机端口：

```sh
python3 -m http.server 5188 --bind 127.0.0.1 --directory docs/frontend-design/redesign/prototype
```

当前 Agent 列表打开 `http://127.0.0.1:5188/index.html?page=agents&scenario=normal&theme=light`；默认仅列表，不自动选择对象。general 入口仍为 `?page=settings&fixture=baseline&scenario=normal&theme=light`，MCP 为 `?page=mcp`；深色改为 `theme=dark`。端口 5188 仅为示例，可换任一空闲本机端口。旧项目列表仍可用 `?page=projects&theme=light&scenario=normal`。停止时在该终端按 Ctrl+C。ES modules 需要 HTTP 服务，直接打开本地 HTML 文件不能代替此入口。自动检查使用自己的服务与端口，完整命令、锁定依赖及退出记录见[测试说明](test-README.md)。

| 参数       | 可用值与用途                                                                                                                                                                                                            |
| ---------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `page`     | 原11类模板及 `settings`（general）、`agents` 候选，见下表；默认 `projects`。                                                                                                                                            |
| `theme`    | `light` / `dark`；默认浅色。                                                                                                                                                                                            |
| `scenario` | `normal` / `long` / `empty` / `loading` / `error` / `readonly` / `unknown`；默认正常；`long` 用于支持它的模板长内容演示。general / Agent 另支持 `conflict`（版本冲突）本地场景；Agent 详情可用 `missing` 查看无效目标。 |
| `accent`   | `neutral` / `indigo` / `teal`；当前默认 `indigo`；三种均为待重新校准的视觉候选，不作为已接受定稿。                                                                                                                      |
| `fixture`  | 默认 `design`；`baseline` 用于项目、任务、Provider 及 general 与当前正式 Vue 页的同输入对照。                                                                                                                           |
| 页面选择   | `page=tasks&detail=AT-142` 打开任务详情；`page=knowledge&doc=pdf`（另可选 `md / txt / docx`） 选择四格式示例；项目入口可携带 `project_name` / `project_lifecycle`。                                                     |

Agent 本轮使用下列非敏感对象参数，三 Tab 与独立保存已形成可浏览候选；限定检查不等于完整业务验收。详情替换列表主区，Memory 是同一对象详情内的记忆 Tab；未知 ID 显示无效目标并提供返回，不回退到首项：

| 入口                                               | 含义                                                                                                              |
| -------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------- |
| `?page=agents`                                     | Agent 列表，初始无选中详情                                                                                        |
| `?page=agents&agent=new`                           | 独立创建，默认基本配置；可切 `view=resources`，仅两个 Tab，无记忆 Tab。名称与主模型必填，成功后仅新增本地合成对象 |
| `?page=agents&agent=agent-reviewer`                | Reviewer 独立详情，默认基本配置；另有 `agent-architecture`、`agent-researcher`                                    |
| `?page=agents&agent=agent-reviewer&view=resources` | Reviewer 详情的能力与资源 Tab                                                                                     |
| `?page=agents&agent=agent-reviewer&view=memory`    | Reviewer 同一详情的记忆 Tab                                                                                       |
| `?page=memory`                                     | 旧链接按原映射兼容至 Architecture 的记忆 Tab；旧 `panel=configuration` 兼容至其基本配置，不作为新列表入口         |

详情统一使用上方独立“← Agent”返回列表、当前显示名/名称标题与实际描述；无描述时不显示说明行，技术名留在基本信息。其下三个 Tab 共用对象页头，不再设置独立“查看记忆”按钮或关闭回配置步骤。基本配置含身份、描述、指令、模型与审批，能力与资源含 Runner/Secret 引用及 Skills。已有 Agent 的基本配置独立手动保存/取消，切换 Tab 可保留基本配置草稿；能力与资源不共用该草稿，也没有整页保存。Runner 与目录组合完成后确认该条，Secret 选择、Skills 动作各按一次完整操作即时保存；Memory 沿现有能力逐条操作，不增加手动创建记忆 API。资源/Memory 的成功操作不能因取消基本配置而撤销，各自保留失败与 Unknown 恢复。

新 Agent 入口使用真实字段合同构造的3个合成对象（Architecture、Researcher、Reviewer），并模拟模型、指令、审批、Runner/Secret 引用、Skills 与 Memory；不调用真实 API、不代表真实 Agent 运行。旧正式 Vue fixture 只有1个 reviewer，与此输入不同；即使带 `fixture=baseline`，也不能将 Agent 图称为真实 Vue 的同输入前后对照。新 Tab 的列表返回、基本草稿与独立操作隔离、创建及缩放结果见[本轮定向检查](test-README.md#三-tab-与独立保存的最终定向检查)；旧长表单结果仅证明当时输入。创建尚无对象 ID，前两个 Tab 汇集一次创建输入，不表示资源已即时写入；有效创建只增加本地对象，刷新后重置。

资源恢复演示可在对象入口附加 `resource_result=error`（另可用 `unknown`、`conflict`、`version_jump`），在提交资源操作后触发对应本地结果；`resource_extra_secret=1` 为 Reviewer 添加缺页既有 Secret 引用，用于核对完整集合保留。它们都是合成检查参数，不代表真实服务返回。

页面底部“样板设置”可切换页面、主题、强调色和状态。URL 只承载非敏感展示选择；草稿、凭据和 OAuth 材料不进入 URL。刷新会重建内存数据，不作为持久化或跨 Session 恢复。

`fixture=baseline` 对齐 [interface-layout.fixtures.ts](../../../../tests/account-captcha-web/interface-layout.fixtures.ts) 的五项目、七状态任务、Provider 字段及 general 的 Atlas 名称/描述；旧 fixture 的模型集合为空。完整前后对照使用同视口、主题和展开状态，不能把默认 `design` 的更多内容称为同输入。历史11路由审计图也不代证本轮新增协作页。

general 的同内容基线是正式 Vue `/owner/atlas/settings/general`，不是旧 F0 的 MCP 设置样板；名称、描述和记录默认折叠态与该 fixture 对齐。正式基线 4/4、390 深色真实滚动补图 1/1 已通过，详见[基线说明](test-README.md#用户反馈后的基本信息页基线)；这些结果不证明新页或动效通过。`task-pages.js` / `task.css` 仍未接入，旧 `tasks` 样板继续提供原入口。

## 页面与参考结构

L1—L8 的定义和实际观察限制见 [Linear 官方参考](../linear-reference.md#1-一手来源与可观察内容)。下表记录现有样板的结构和参考映射，正在按用户反馈修订；不是已获用户认可的信息架构。不把本项目状态机或业务命令套成 Linear 的功能。

| `page`      | 完整任务与主要动作                                                                   | 参考映射及适配                                                                        |
| ----------- | ------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------- |
| `dashboard` | 系统首页；从现有项目名称、描述与生命周期进入项目或全部项目。                         | L1 降低导航权重，L3 上下文与资源；没有虚构统计或最近访问。                            |
| `projects`  | 连续项目目录、状态筛选、创建输入、受理后查询与进入条件。                             | L1 框架分层、L4 名称主导的连续行；保留 Owner 与项目生命周期。                         |
| `project`   | 项目概览；资料、状态与任务、会议、待办、知识、产物入口。                             | L1 分层、L3 概览；归档/归档中明确只读。                                               |
| `tasks`     | 七状态看板、当前 Sprint 规划、任务创建、详情正文/属性、草稿与返回。                  | L2 工作台、L5 局部横滚看板、L6 属性、L7 创建/草稿关闭；不增加拖拽或 Explore。         |
| `settings`  | 项目设置基本信息候选：名称/描述编辑、记录、菜单、保存反馈及恢复。                    | L1 框架层级、L8 成组设置；无归档/恢复/删除项目操作，旧检查不覆盖本轮共享页头变化。    |
| `agents`    | 初始列表→独立详情内基本配置/能力与资源/记忆 Tab；创建仅前两个 Tab。                  | L4 连续对象列表、L8 配置分组；非并排分栏，新 Tab 已完成限定内部检查，待用户实看。     |
| `providers` | Provider 目录→配置/模型；创建、就近保存、凭据输入、模型编辑和删除引用门禁。          | L1 信息层级、L6 对象属性、L8 成组设置；Provider、Model、Credential 各有独立命令语义。 |
| `knowledge` | 目录→阅读/原文编辑、四格式原文件、PDF、元信息、上传/替换/下载与恢复。                | L2 阅读主区、L3 资源、L6 元信息、L7 草稿关闭；窄屏默认正文、单层目录。                |
| `meetings`  | 消息与回复、引用/附件输入、Summary、Decision/Approval 与结果恢复。                   | L2 内容工作台、L6 上下文、L7 输入与草稿；会议、附件及有限 Realtime 采用本项目语义。   |
| `inbox`     | 项目内待处理/已处理事项、来源详情、回到来源会议处理。                                | L2 列表/详情、L6 决策上下文；不提供跨项目聚合或列表内独立审批。                       |
| `mcp`       | 配置→连接→认证/发现；项目配置编辑、连接操作、凭据与 OAuth 发起。                     | L8 成组设置、L6 对象状态；系统来源只读，连接、启用、认证和发现分别表达。              |
| `oauth`     | 授权返回、结果未知/停止恢复、回到当前项目连接。                                      | L7 单任务与返回、L8 恢复说明；不展示令牌或真实回调材料。                              |
| `memory`    | 旧 Architecture 记忆入口兼容；`page=agents&agent=…&view=memory` 选择同对象记忆 Tab。 | L2 记录/详情、L6 修订属性、L8 危险操作分区；不是新增产品路由。                        |

项目导航另提供 `page=artifacts` 配套只读入口：连续产物列表、来源筛选、文本原文和二进制无预览说明。它映射已有 [ProjectArtifactsAPI.list/read](../../../../web/src/api/project-artifacts.ts)，不增加上传能力，不计入原11类 F0 必需模板。

## 演示动作与真实实现接续

所有下列 API 都是后续产品实现的已有依赖，样板没有调用它们。刷新、过滤、目录切换、Tab、详情、草稿和分页演示仅处理内存数据；“查询原请求”也只展示恢复语义。正式接入必须复用对应 API、Session/Project guard 与请求 owner，保留版本、字段 presence、原 key、迟到结果和只读限制。

| 页面/动作                       | 当前代码中的真实依赖与接续边界                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| ------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 系统/项目首页、项目筛选/进入    | [project-owner.ts](../../../../web/src/api/project-owner.ts) 的 `list / resolve / get`；[useProjectWorkspace.ts](../../../../web/src/composables/useProjectWorkspace.ts) 管理目录与当前项目。系统首页不新增聚合服务。                                                                                                                                                                                                                                                                                          |
| general 基本信息编辑与恢复      | [ProjectGeneralSettings.vue](../../../../web/src/views/projects/ProjectGeneralSettings.vue) 与 [useProjectWorkspace.ts](../../../../web/src/composables/useProjectWorkspace.ts) 的 `save / cancelEdits / readCurrent / checkOriginal / replayOriginal`；[project-owner.ts](../../../../web/src/api/project-owner.ts) 的 `update / lookup`。样板仅模拟保存反馈，更名提醒、版本、字段 presence、冲突/Unknown 与离页保护沿真实合同，不能将本地成功视为服务端写入。                                                |
| 项目创建、受理后查看状态        | [project-creation.ts](../../../../web/src/api/project-creation.ts) 的 `create / get`；`accepted` 与可进入的 `ready` 分开，不能收到受理即宣称创建完成。                                                                                                                                                                                                                                                                                                                                                         |
| Task 看板、详情与创建/描述输入  | [work-review.ts](../../../../web/src/api/work-review.ts) 的 `tasks / task / blockers`；[work-task-planning.ts](../../../../web/src/api/work-task-planning.ts) 的 `create / lookup`；[work-task-edit.ts](../../../../web/src/api/work-task-edit.ts) 的 `update / lookup`。样板草稿不是独立评论；状态转移继续由 `work-review` 的 `transfer / lookup` 负责，执行/审批等完整功能留给 F5 保真。                                                                                                                     |
| Provider / Model / Credential   | [system-providers.ts](../../../../web/src/api/system-providers.ts) 的目录、读取、`createProvider / updateProvider / deleteProvider / lookupProviderCommand` 及 `createCredential / lookupCredentialCreate`；[system-models.ts](../../../../web/src/api/system-models.ts) 的目录、读取、`createModel / updateModel / deleteModel / getDeletionImpact / lookupModelCommand`。样板模型模态只演示字段组合，正式类型/协议与引用约束沿原 API；凭据提交后清空输入。                                                   |
| Knowledge 目录、正文与草稿      | [knowledge-owner.ts](../../../../web/src/api/knowledge-owner.ts) 的 `children / get / ancestors / readContent`；[knowledge-text.ts](../../../../web/src/api/knowledge-text.ts) 的 `write / lookup` 与 [useKnowledgeText.ts](../../../../web/src/composables/useKnowledgeText.ts) 的导航草稿门禁。标题查找不称全文或语义搜索。                                                                                                                                                                                  |
| Knowledge 文件及管理            | [knowledge-file.ts](../../../../web/src/api/knowledge-file.ts) 的 `write / lookup / source`；[useKnowledgeFile.ts](../../../../web/src/composables/useKnowledgeFile.ts) 管理文件意图和恢复。改名映射 [knowledge-commands.ts](../../../../web/src/api/knowledge-commands.ts) 的 `rename / lookup`，移动映射 [knowledge-move.ts](../../../../web/src/api/knowledge-move.ts) 的 `move / lookup`，子树删除映射 [knowledge-delete.ts](../../../../web/src/api/knowledge-delete.ts) 的 `preview / remove / lookup`。 |
| Meeting 消息、回复、控制及审批  | [meetings.ts](../../../../web/src/api/meetings.ts) 的读取与 `command / lookup`，[useProjectMeetings.ts](../../../../web/src/composables/useProjectMeetings.ts) 的 `submit / respond / control / replyToMessage`；执行审批使用 [execution-approval.ts](../../../../web/src/api/execution-approval.ts) 的 `decide / lookup` 和 [useMeetingExecutionApproval.ts](../../../../web/src/composables/useMeetingExecutionApproval.ts)。附件映射见下行；不新增 `update-meeting` / `comment-meeting` 命令。              |
| Meeting 附件、引用与取消        | [artifact-upload.ts](../../../../web/src/api/artifact-upload.ts) 的 `begin / payload / complete / lookup / cancel` 与 [artifact-upload-session.ts](../../../../web/src/composables/artifact-upload-session.ts) 的阶段管理；[meeting-composer.ts](../../../../web/src/composables/meeting-composer.ts) 保留结构化引用和选择范围。样板推进本地上传阶段、生成附件引用，不产生真实 Artifact；Unknown 查询不授予再次上传 payload。                                                                                  |
| Inbox 刷新、分页、查看来源      | [project-inbox.ts](../../../../web/src/api/project-inbox.ts) 的 `list`，仅 `open / resolved`；当前项目的 Decision/Proposal 回来源会议处理。                                                                                                                                                                                                                                                                                                                                                                    |
| MCP 配置、连接、发现、凭据      | [project-mcp.ts](../../../../web/src/api/project-mcp.ts) 的配置/连接/能力读取与写命令；[mcp-session.ts](../../../../web/src/composables/mcp-session.ts) 的 `start / lookup`，凭据由 [mcp-credential-session.ts](../../../../web/src/composables/mcp-credential-session.ts) 独立 `start / lookup`。系统配置只读；Unknown 与 stopped 不混同成功。                                                                                                                                                                |
| OAuth 发起、回调、查询与返回    | [mcp-oauth-session.ts](../../../../web/src/composables/mcp-oauth-session.ts) 的原授权会话、`completeReturn / lookup`；正式返回依赖同 Session、原意图和 code 清理。样板无真实 OAuth 跳转或令牌。                                                                                                                                                                                                                                                                                                                |
| Agent 创建、配置读取/编辑与恢复 | [agent-configuration.ts](../../../../web/src/api/agent-configuration.ts) 的 `create / lookup / getConfiguration / update / lookupUpdate` 与 [useProjectAgents.ts](../../../../web/src/composables/useProjectAgents.ts)；样板仅本地模拟，创建和更新保持各自原输入及恢复语义，Memory 命令独立见下行。                                                                                                                                                                                                            |
| Memory 读取、修订、删除与查询   | [agent-memory.ts](../../../../web/src/api/agent-memory.ts) 的 `list / history / delete / lookup`；[agent-memory-session.ts](../../../../web/src/composables/agent-memory-session.ts) 的 `start / lookup` 保留 `memory_id + expected_revision`，Unknown 时不能直接移除原记录。                                                                                                                                                                                                                                  |

## 状态、文件与键盘检查

七种场景覆盖同一完整页的正常内容、长内容、空集合、加载、读取失败、只读和结果未知。只读原因贴近对象；失败保留上下文并提供重新读取；Unknown 设计保留输入、原版本/请求和查询入口，不能以重新创建替代查询。按钮禁用、恢复反馈及跨对象草稿隔离均须实际检查，已发现问题及复验结论见独审入口。

Knowledge 可用 `doc=md / txt / pdf / docx` 查看格式边界：TXT、Markdown 保留原文；PDF 使用有效本地 PDF 和浏览器内嵌 viewer；DOCX 提供有效 OOXML 下载并展示解析中/失败，明确没有原版式预览。上传、替换和“原版本下载”都是本地文件演示，正式 1 MiB 限额、版本与来源校验仍由已有文件 API 执行。解析失败时仍保留取原文件的路径，不把合成解析状态称为真实处理完成。

公共模态由原生 `dialog`、Tab/Shift+Tab 循环、Escape/关闭按钮及回焦组成。任务与知识草稿在离开前提示保留，Provider Tab 需要方向键与正确焦点，窄屏目录和详情需要明确返回路径；底部样板栏不得遮挡操作。这些要求曾按上一轮输入完成浏览器检查和适用独立复验，原失败及修复依据继续保留；本轮修改后须补受影响检查。Knowledge 在原生 200% 的低高度视口下允许正文随页面滚动，真实滚轮可到达长文后部；不以等效 viewport 检查替代该次原生缩放证据。

## 复现、截图与 F1 接续

本轮三 Tab 的[定向测试](test-README.md#三-tab-与独立保存的最终定向检查)由未受影响的7项与修后资源代表链1项组成，覆盖新 Tab 身份/直链、基本草稿与独立操作互不回滚、创建、键盘/动效及本轮原生200%；异常版本的旧错误预期已撤回，修后保留原状态与待核命令，不合并称为全站矩阵通过。[独立审查](review/README.md#agent-三-tab-与独立保存职责限定内部通过)亲看三 Tab 的1440浅色/390深色六张正常整页，完成跨 Tab 主链，并在取消后焦点修正后按原窄屏现场复验；旧整页与保存结果仅按未受影响范围复用。各项输入、限制及代表图见对应入口，不作为用户接受或真实后端验收。

三 Tab 调整前的 Agent 候选曾通过正常整页与共同页头限定独审，首稿3项视觉问题按当时输入关闭；测试另完成有限状态、创建/配置/Memory 恢复与原生200%检查，以下均不代证新 Tab。Memory Unknown 曾误称保存并丢失恢复视图，修后原链已复验；原失败与原生缩放测试标签定位修正均保留在[测试说明](test-README.md#agent-独立详情与设置-header)。当时独审的输入差异和范围见[此前记录](review/README.md#此前-agent-列表独立详情与共享页头的限定记录)，不合并称为全站矩阵通过。

此前 general 从 `f0138630` 第二稿继续，经历纯格式整理、共享 `core.js` 菜单滚动竞态修复与去外框调整；以下是其限定历史证据。独审已关闭首稿3项视觉缺陷，并实际检查整页/菜单、描述保存与取消、改名确认、草稿离开、键盘回焦、窄屏可达，以及普通动效与 reduced motion；原生200%补查发现迟到滚动通知误关菜单，修复后原链连续3次覆盖并通过；编辑保存、更多菜单回焦、真实滚轮到末尾记录及无横溢/遮挡均已检查，共享菜单正常滚动关闭和键盘行为也已定向复验。作者检查保留原测试前提失败、真实菜单失败及修后复验事实。完整结果与最小截图分别见下述测试/独审入口，不将其扩写为其余设置栏目、旧11类模板或用户审美接受。

作者复现与同输入前后对照见[测试说明](test-README.md)，独立截图、交互复现和最小保留图见[独审入口](review/README.md)。本轮关注 Agent 列表、所选详情的三个 Tab 与 general、MCP 的共同页头；新 Agent 内容使用上文明确的合成输入，当前实现与检查状态以上文说明为准。general 的正式基线与原候选对照使用1440×960/390×844及相同主题、折叠状态，不扩写到 Agent。原项目、任务、Provider 及其余模板的历史图仍按原输入解释；详情、Unknown、长内容和窄屏文件阅读需使用对应状态。本轮原始截图及 trace 留在忽略的 `output/ai/frontend-f0-refinement-test/` 和 `output/ai/frontend-f0-refinement-review/`；旧材料仍在原 `output/ai/frontend-f0/` 和 `output/ai/frontend-f0-review/`，不把可再生输出另建永久报告。

F1 尚未开始；后续按[执行计划](../implementation-plan.md#5-f1共享基础应用框架及项目列表样板)接续：将本轮重新校准并说明接受状态的语义值、框架与组件行为落实到正式 Vue 共享基础，并以项目列表完成首个真实页面。沿原 API/composable 接线，检查真实导航、创建受理→就绪、权限/身份切换和恢复；样板脚本不直接搬进产品。当前先完成 F0 返修并取得用户实看反馈；F1 仍需后续阶段指令，不能据本样板自动宣布40叶迁移或任何真实业务验收通过。
