# F0 完整页面样板

F0 已因用户实看反馈重开返修。用户未接受现有项目菜单、项目设置结构、下拉样式、页面层次及整体视觉品质，与 Linear 的差距和动效质感仍需改进；视觉系统数值待重新校准，F1—F8 正式产品迁移未开始。此前内部技术与交互检查的通过事实仍按当时输入保留，不代表用户已接受设计，也不能覆盖本轮修改后的新输入。

本样板使用依据当前字段构造的合成数据，业务动作只改变当前浏览器中的演示状态或显示演示反馈，不调用真实 API、不写入业务事实。PDF/文件下载使用本地生成内容，不能代证真实上传、解析、权限、恢复或生产接入。视觉候选与修订规则见[目标设计 §8](../target-design.md#8-f0-可实现视觉值与组件使用决定)；现有数值和下方页面结构均为本轮待核对、待重校的输入。

入口协议见[模块契约](module-contract.md)，原技术与交互结果见[作者浏览器检查](test-README.md)和[独立视觉审查](review/README.md)。新的内部审查须注明新输入及受影响范围；返修后如暂无新的用户认可，结论写为“已修订/内部审查结果，待用户实看反馈”，不把内部检查通过称为用户对信息结构与视觉品质的接受。

## 启动与参数

只浏览样板需要 Python 3，不需要产品依赖。以下命令从仓库根目录执行，使用一个空闲的本机端口：

```sh
python3 -m http.server 5188 --bind 127.0.0.1 --directory docs/frontend-design/redesign/prototype
```

打开 `http://127.0.0.1:5188/index.html?page=projects&theme=light&scenario=normal&accent=indigo`。停止时在该终端按 Ctrl+C。ES modules 需要 HTTP 服务，直接打开本地 HTML 文件不能代替此入口。自动检查使用自己的服务与端口，完整命令、锁定依赖及退出记录见[测试说明](test-README.md)。

| 参数       | 可用值与用途                                                                                                                                                        |
| ---------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `page`     | 下表的11个必需页面；默认 `projects`。                                                                                                                               |
| `theme`    | `light` / `dark`；默认浅色。                                                                                                                                        |
| `scenario` | `normal` / `long` / `empty` / `loading` / `error` / `readonly` / `unknown`；默认正常。`long` 保留正常任务并增加长名称、长正文。                                     |
| `accent`   | `neutral` / `indigo` / `teal`；当前默认 `indigo`；三种均为待重新校准的视觉候选，不作为已接受定稿。                                                                  |
| `fixture`  | 默认 `design`；`baseline` 仅用于项目、任务、Provider 三类与当前 Vue 页的同输入对照。                                                                                |
| 页面选择   | `page=tasks&detail=AT-142` 打开任务详情；`page=knowledge&doc=pdf`（另可选 `md / txt / docx`） 选择四格式示例；项目入口可携带 `project_name` / `project_lifecycle`。 |

页面底部“样板设置”可切换页面、主题、强调色和状态。URL 只承载非敏感展示选择；草稿、凭据和 OAuth 材料不进入 URL。刷新会重建内存数据，不作为持久化或跨 Session 恢复。

`fixture=baseline` 对齐 [interface-layout.fixtures.ts](../../../../tests/account-captcha-web/interface-layout.fixtures.ts) 的五项目、七状态任务及 Provider 字段；旧 fixture 的模型集合为空。完整前后对照使用同视口、主题和展开状态，不能把默认 `design` 的更多内容称为同输入。历史11路由审计图也不代证本轮新增协作页。

## 页面与参考结构

L1—L8 的定义和实际观察限制见 [Linear 官方参考](../linear-reference.md#1-一手来源与可观察内容)。下表记录现有样板的结构和参考映射，正在按用户反馈修订；不是已获用户认可的信息架构。不把本项目状态机或业务命令套成 Linear 的功能。

| `page`      | 完整任务与主要动作                                                          | 参考映射及适配                                                                        |
| ----------- | --------------------------------------------------------------------------- | ------------------------------------------------------------------------------------- |
| `dashboard` | 系统首页；从现有项目名称、描述与生命周期进入项目或全部项目。                | L1 降低导航权重，L3 上下文与资源；没有虚构统计或最近访问。                            |
| `projects`  | 连续项目目录、状态筛选、创建输入、受理后查询与进入条件。                    | L1 框架分层、L4 名称主导的连续行；保留 Owner 与项目生命周期。                         |
| `project`   | 项目概览；资料、状态与任务、会议、待办、知识、产物入口。                    | L1 分层、L3 概览；归档/归档中明确只读。                                               |
| `tasks`     | 七状态看板、当前 Sprint 规划、任务创建、详情正文/属性、草稿与返回。         | L2 工作台、L5 局部横滚看板、L6 属性、L7 创建/草稿关闭；不增加拖拽或 Explore。         |
| `providers` | Provider 目录→配置/模型；创建、就近保存、凭据输入、模型编辑和删除引用门禁。 | L1 信息层级、L6 对象属性、L8 成组设置；Provider、Model、Credential 各有独立命令语义。 |
| `knowledge` | 目录→阅读/原文编辑、四格式原文件、PDF、元信息、上传/替换/下载与恢复。       | L2 阅读主区、L3 资源、L6 元信息、L7 草稿关闭；窄屏默认正文、单层目录。                |
| `meetings`  | 消息与回复、引用/附件输入、Summary、Decision/Approval 与结果恢复。          | L2 内容工作台、L6 上下文、L7 输入与草稿；会议、附件及有限 Realtime 采用本项目语义。   |
| `inbox`     | 项目内待处理/已处理事项、来源详情、回到来源会议处理。                       | L2 列表/详情、L6 决策上下文；不提供跨项目聚合或列表内独立审批。                       |
| `mcp`       | 配置→连接→认证/发现；项目配置编辑、连接操作、凭据与 OAuth 发起。            | L8 成组设置、L6 对象状态；系统来源只读，连接、启用、认证和发现分别表达。              |
| `oauth`     | 授权返回、结果未知/停止恢复、回到当前项目连接。                             | L7 单任务与返回、L8 恢复说明；不展示令牌或真实回调材料。                              |
| `memory`    | Agent 内嵌面板；当前记录、来源与修订、删除确认、Unknown 查询与关闭。        | L2 记录/详情、L6 修订属性、L8 危险操作分区；不是新增产品路由。                        |

项目导航另提供 `page=artifacts` 配套只读入口：连续产物列表、来源筛选、文本原文和二进制无预览说明。它映射已有 [ProjectArtifactsAPI.list/read](../../../../web/src/api/project-artifacts.ts)，不增加上传能力，也不将其计作第12个 F0 必需页。

## 演示动作与真实实现接续

所有下列 API 都是后续产品实现的已有依赖，样板没有调用它们。刷新、过滤、目录切换、Tab、详情、草稿和分页演示仅处理内存数据；“查询原请求”也只展示恢复语义。正式接入必须复用对应 API、Session/Project guard 与请求 owner，保留版本、字段 presence、原 key、迟到结果和只读限制。

| 页面/动作                      | 当前代码中的真实依赖与接续边界                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| ------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 系统/项目首页、项目筛选/进入   | [project-owner.ts](../../../../web/src/api/project-owner.ts) 的 `list / resolve / get`；[useProjectWorkspace.ts](../../../../web/src/composables/useProjectWorkspace.ts) 管理目录与当前项目。系统首页不新增聚合服务。                                                                                                                                                                                                                                                                                          |
| 项目创建、受理后查看状态       | [project-creation.ts](../../../../web/src/api/project-creation.ts) 的 `create / get`；`accepted` 与可进入的 `ready` 分开，不能收到受理即宣称创建完成。                                                                                                                                                                                                                                                                                                                                                         |
| Task 看板、详情与创建/描述输入 | [work-review.ts](../../../../web/src/api/work-review.ts) 的 `tasks / task / blockers`；[work-task-planning.ts](../../../../web/src/api/work-task-planning.ts) 的 `create / lookup`；[work-task-edit.ts](../../../../web/src/api/work-task-edit.ts) 的 `update / lookup`。样板草稿不是独立评论；状态转移继续由 `work-review` 的 `transfer / lookup` 负责，执行/审批等完整功能留给 F5 保真。                                                                                                                     |
| Provider / Model / Credential  | [system-providers.ts](../../../../web/src/api/system-providers.ts) 的目录、读取、`createProvider / updateProvider / deleteProvider / lookupProviderCommand` 及 `createCredential / lookupCredentialCreate`；[system-models.ts](../../../../web/src/api/system-models.ts) 的目录、读取、`createModel / updateModel / deleteModel / getDeletionImpact / lookupModelCommand`。样板模型模态只演示字段组合，正式类型/协议与引用约束沿原 API；凭据提交后清空输入。                                                   |
| Knowledge 目录、正文与草稿     | [knowledge-owner.ts](../../../../web/src/api/knowledge-owner.ts) 的 `children / get / ancestors / readContent`；[knowledge-text.ts](../../../../web/src/api/knowledge-text.ts) 的 `write / lookup` 与 [useKnowledgeText.ts](../../../../web/src/composables/useKnowledgeText.ts) 的导航草稿门禁。标题查找不称全文或语义搜索。                                                                                                                                                                                  |
| Knowledge 文件及管理           | [knowledge-file.ts](../../../../web/src/api/knowledge-file.ts) 的 `write / lookup / source`；[useKnowledgeFile.ts](../../../../web/src/composables/useKnowledgeFile.ts) 管理文件意图和恢复。改名映射 [knowledge-commands.ts](../../../../web/src/api/knowledge-commands.ts) 的 `rename / lookup`，移动映射 [knowledge-move.ts](../../../../web/src/api/knowledge-move.ts) 的 `move / lookup`，子树删除映射 [knowledge-delete.ts](../../../../web/src/api/knowledge-delete.ts) 的 `preview / remove / lookup`。 |
| Meeting 消息、回复、控制及审批 | [meetings.ts](../../../../web/src/api/meetings.ts) 的读取与 `command / lookup`，[useProjectMeetings.ts](../../../../web/src/composables/useProjectMeetings.ts) 的 `submit / respond / control / replyToMessage`；执行审批使用 [execution-approval.ts](../../../../web/src/api/execution-approval.ts) 的 `decide / lookup` 和 [useMeetingExecutionApproval.ts](../../../../web/src/composables/useMeetingExecutionApproval.ts)。附件映射见下行；不新增 `update-meeting` / `comment-meeting` 命令。              |
| Meeting 附件、引用与取消       | [artifact-upload.ts](../../../../web/src/api/artifact-upload.ts) 的 `begin / payload / complete / lookup / cancel` 与 [artifact-upload-session.ts](../../../../web/src/composables/artifact-upload-session.ts) 的阶段管理；[meeting-composer.ts](../../../../web/src/composables/meeting-composer.ts) 保留结构化引用和选择范围。样板推进本地上传阶段、生成附件引用，不产生真实 Artifact；Unknown 查询不授予再次上传 payload。                                                                                  |
| Inbox 刷新、分页、查看来源     | [project-inbox.ts](../../../../web/src/api/project-inbox.ts) 的 `list`，仅 `open / resolved`；当前项目的 Decision/Proposal 回来源会议处理。                                                                                                                                                                                                                                                                                                                                                                    |
| MCP 配置、连接、发现、凭据     | [project-mcp.ts](../../../../web/src/api/project-mcp.ts) 的配置/连接/能力读取与写命令；[mcp-session.ts](../../../../web/src/composables/mcp-session.ts) 的 `start / lookup`，凭据由 [mcp-credential-session.ts](../../../../web/src/composables/mcp-credential-session.ts) 独立 `start / lookup`。系统配置只读；Unknown 与 stopped 不混同成功。                                                                                                                                                                |
| OAuth 发起、回调、查询与返回   | [mcp-oauth-session.ts](../../../../web/src/composables/mcp-oauth-session.ts) 的原授权会话、`completeReturn / lookup`；正式返回依赖同 Session、原意图和 code 清理。样板无真实 OAuth 跳转或令牌。                                                                                                                                                                                                                                                                                                                |
| Memory 读取、修订、删除与查询  | [agent-memory.ts](../../../../web/src/api/agent-memory.ts) 的 `list / history / delete / lookup`；[agent-memory-session.ts](../../../../web/src/composables/agent-memory-session.ts) 的 `start / lookup` 保留 `memory_id + expected_revision`，Unknown 时不能直接移除原记录。                                                                                                                                                                                                                                  |

## 状态、文件与键盘检查

七种场景覆盖同一完整页的正常内容、长内容、空集合、加载、读取失败、只读和结果未知。只读原因贴近对象；失败保留上下文并提供重新读取；Unknown 设计保留输入、原版本/请求和查询入口，不能以重新创建替代查询。按钮禁用、恢复反馈及跨对象草稿隔离均须实际检查，已发现问题及复验结论见独审入口。

Knowledge 可用 `doc=md / txt / pdf / docx` 查看格式边界：TXT、Markdown 保留原文；PDF 使用有效本地 PDF 和浏览器内嵌 viewer；DOCX 提供有效 OOXML 下载并展示解析中/失败，明确没有原版式预览。上传、替换和“原版本下载”都是本地文件演示，正式 1 MiB 限额、版本与来源校验仍由已有文件 API 执行。解析失败时仍保留取原文件的路径，不把合成解析状态称为真实处理完成。

公共模态由原生 `dialog`、Tab/Shift+Tab 循环、Escape/关闭按钮及回焦组成。任务与知识草稿在离开前提示保留，Provider Tab 需要方向键与正确焦点，窄屏目录和详情需要明确返回路径；底部样板栏不得遮挡操作。这些要求曾按上一轮输入完成浏览器检查和适用独立复验，原失败及修复依据继续保留；本轮修改后须补受影响检查。Knowledge 在原生 200% 的低高度视口下允许正文随页面滚动，真实滚轮可到达长文后部；不以等效 viewport 检查替代该次原生缩放证据。

## 复现、截图与 F1 接续

作者复现与同输入前后对照见[测试说明](test-README.md)，独立截图、交互复现和最小保留图见[独审入口](review/README.md)。先查看项目、任务、Provider 的1440×960浅色与390×844深色整页，再查看其余模板；详情、Unknown、长内容和窄屏文件阅读需使用对应状态。原始截图及 trace 留在忽略的 `output/ai/frontend-f0/` 和 `output/ai/frontend-f0-review/`，不把可再生输出另建永久报告。

F1 尚未开始；后续按[执行计划](../implementation-plan.md#5-f1共享基础应用框架及项目列表样板)接续：将本轮重新校准并说明接受状态的语义值、框架与组件行为落实到正式 Vue 共享基础，并以项目列表完成首个真实页面。沿原 API/composable 接线，检查真实导航、创建受理→就绪、权限/身份切换和恢复；样板脚本不直接搬进产品。当前先完成 F0 返修并取得用户实看反馈；F1 仍需后续阶段指令，不能据本样板自动宣布40叶迁移或任何真实业务验收通过。
