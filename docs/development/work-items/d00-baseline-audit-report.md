# D00 设计与实现基线审计报告

日期：2026-10-03；任务：D00-01 修订 2 / AT-0004；执行：verification_worker。依据 [任务卡](d00-baseline-audit.md)、[开发计划](../development-plan.md)、[架构总览](../../architecture/README.md)、[前端衔接清单](../../frontend-design/architecture-follow-ups.md)与 agenteam-verification 技能。仅新增本报告；未修改源码、架构、依赖或环境，未执行 Git 写操作。

审计结论：现有交付为前端组件基础、工程与文档，全部业务模块仍仅有设计。全部 77 个架构 Markdown 已逐文件阅读，覆盖 12 个一级专题；下文给出每文件范围与归属。发现的冲突和未决项需在 D01 或对应模块开工前关闭；本报告不替架构作决定。D00-01 后已通过 D00-02 只读验收与主线程独立审查，完成状态和本地提交定位见任务卡与台账。

## 1. 实现清单

Git：实际分支 main，HEAD 80a9d31；五条历史为 80a9d31、b4a92fe、fe5b833、446f4c8、7b29850。开发计划 fe5b833 为规划基线，当前任务卡 80a9d31 为执行基线，属于时间差而非设计冲突。开始及结束前已有台账修改、work-items 未跟踪文件，保留主线程改动。

| 范围 | 实际事实 | 状态 / 后续归属 |
| --- | --- | --- |
| web | Vue/TS/Vite 独立工程，package.json、lock、type-check/test/build/format/check 脚本存在 | 已有基础；D26/D27 接业务 |
| App/router | AppShell + SystemNav + RouterView；DEV 条件才注册 lazy /debug，生产根路径 EmptyView，其他路径 NotFoundView | 已有骨架；生产隔离源码有条件，未重新构建验收 |
| UI | ui/index.ts 导出 33 个 Vue 组件及 types，含表单、树、消息、表格、浮层、反馈；共享 composables | 已实现基础控件，不能充当业务状态机 |
| 主题 | useTheme 支持 light/dark/system，matchMedia 与 document dataset；mode 为模块内存 ref，没有账号存储 | 已有主题切换；D07/D26 持久化 |
| 样式 | tokens.css/base.css/components.css 为共享样式来源；已确认规范；tests/styles.spec.ts 对照 token、contrast、禁止 Debug 导入 | 保留已确认样式；D26/D27 按真实页面复查 |
| Debug | 演示 fixture、样式/组件展示；UiSearchDialog 本地过滤，Composer 字符串输入 | 已有演示；不是搜索、Meeting inline schema 或业务 API |
| cmd/internal | cmd/agenteam、cmd/agenteam-runner、internal/central、internal/runner、internal/runnerprotocol 仅 .gitkeep | 骨架；D02 开工，无 Go module、正式端口或生产 stub |
| db/deploy/scripts/tests | db/migrations、deploy、scripts、tests 仅 .gitkeep | 骨架；D03/D28；没有迁移、Compose 或真实集成测试 |
| 业务能力 | 账号、Owner、任务、会议、Knowledge/Memory、模型、工具、审批、MCP、Runner、执行、实时、Inbox 均无产品实现 | 仅设计；D07–D25 顺序实现 |

[历史前端验证记录](../frontend/verification.md)针对 2026-10-01 组件/Debug，在 Node 24.20.0、npm 11.19.0 与 Chromium 环境记录类型、格式、单元、构建及浏览器验证；本次环境不同，未重新运行，不能扩展为业务验收。当前源码可见原生语义、共享浮层和反馈接口，实际视觉、交互、生产产物仍以该历史记录的限定范围为准。正式页面须复用 [公共组件接口](../frontend/components.md)，对象深链接、双导航、三类设置、错误/version 状态由 D26/D27 完成。

## 2. 设计待定项及开工门槛

| ID | 必须关闭的选择 / 契约 | 责任门槛 |
| --- | --- | --- |
| P01 | 公共 ID/时间/error/cursor/version/request_id、服务签名/DTO/事务、跨模块取消等待、事件 ownership、删除引用矩阵；当前为概念模型而非正式代码接口 | D01；各模块按规格补齐，不预写所有表 |
| P02 | Go compiler/module 版本、HTTP/SQL/migration/crypto 库；PostgreSQL/pgvector/MinIO 镜像固定版本和测试条件 | D02/D03/D05；机器版本不作选择 |
| P03 | Secret encryption/key rotation、HTTP DNS/redirect/TLS/大小限制、Audit action/index；SMTP 非 HTTP 出站适配 | D04/D07；真实拒绝、脱敏与失败测试 |
| P04 | 密码哈希/策略、Session cookie/期限/撤销/请求保护、reset TTL/重复请求、初始化敏感日志失败恢复、SMTP timeout/retry/TLS、头像媒体/生命周期 | D07；邀请 24h 已定，不重新选择 |
| P05 | Project canonical/version/分页/soft delete/purge 清理端口，系统 scope 与 Owner 查询授权 | D08；管理员不能访问他人项目 |
| P06 | 非 chat embedding/rerank/image adapter 协议和支持范围、Provider conformance、引用替换、usage consumer identity（Meeting 调用须完整覆盖） | D09 / D01 / D24；usage 按真实请求尝试，无估算/静默 fallback |
| P07 | Agent 预设/配置引用、Mount/Tool 目录查询与 Secret 白名单解析；目录尚无实现不能默认接受 | D10，真实目录 D15/D18/D20 |
| P08 | Task/Sprint 执行占用与 pending dispatch 端口、锁顺序、rank/rebalance、幂等 replay 与 version、防环及删除引用保护的具体 SQL | D01/D11，真实绑定 D22/D23；review 后 done、waiting 仍占 Agent slot |
| P09 | Knowledge parent identity/排序分页/并发防环/子树删除/history projection；文档 version 与索引 generation 区分 | D12；前端树需求已有，架构存储契约尚需补齐 |
| P10 | 中英 lexical backend（PostgreSQL native FTS/PGroonga/pg_search 候选）、parser/chunker/预算/检索参数和评测集 | D13；先 benchmark 后选择；equal-weight RRF 已定，rerank 可选 |
| P11 | Memory extraction/consolidation schema、revision 原子竞争/索引刷新、Secret 调用前处理；并发例子的触发来源需澄清 | D14 / D01；同 Agent 双 Execution 不合法，用户/后台竞争仍需验证；reflect 只读、无自动 retain/TTL |
| P12 | Runner protocol 具体 version/wire schema、challenge TTL/clock window/heartbeat，WSS 库；Workspace symlink/OS shell/kill tree/ring cursor | D15/D16；trusted-host 无 OS sandbox |
| P13 | Data frame 编码/size/progress/grace参数、Desktop 实际 backend/设备支持、Tunnel gateway 外部认证/TTL/续期与端口 scope | D17；无 raw public TCP、永久 Tunnel、自动恢复 |
| P14 | JSON schema canonical subset/validator、model-visible naming、fingerprint、optional Agent timeout 如何在模型调用通道传入、统一 retry policy/result 外部化阈值 | D01/D18/D21；timeout 不是 Tool 业务 arguments，不擅设强制 deadline |
| P15 | Approval Request 并发决议/恢复端口、ScopeResolver 各 Tool 的结构化范围、auto model timeout/预算、固定 Prompt版本 | D19；fail-safe 人工，审批不能改变 retry safety |
| P16 | MCP 官方 Go SDK 固定版本及 primary/legacy profile conformance、OAuth 2.1/lease/rotation/catalog/cache具体实现 | D20；connected 不是 discovery 成功，环境可用不等于集成 |
| P17 | 全 Tool 目录 schema/risk/scope/idempotency/error 映射及业务 adapter | D21；Core 不可关仍授权，无空 backend |
| P18 | Trigger provider typed identity、snapshot、checkpoint、Model/Tool retry owner、恢复/等待/取消时序 | D01/D22；全局 Agent slot 原子化，无强制总时长/max turns |
| P19 | Scheduler unknown Launch 耗尽处理、busy 补偿与 Task 人工变化竞争、quota 双计数、Sprint lock/冷却配置 | D23；不解释 Execution 终态业务结果，禁止选 assignee |
| P20 | Meeting 同轮固定上下文需冻结 references/summary/current generations 的精确边界；取消 finalizing/summary 失败/archival 清理契约 | D01/D24；busy 无新 Execution，sequential 与 parallel 可见性不同 |
| P21 | Summary 幂等键 meeting_id + summarized_through_message_id 与早期 Contribution regenerate 的关联：末条 ID 未变但当前内容变时，须证明旧结果不会被错误 replay 或补充输入 revision 契约 | D01/D24；风险待证明，不宣称确定产品 bug；首轮标题+四字段 summary 原子提交 |
| P22 | Runtime snapshot/update watermark/revision、本人跨项目 Inbox 分页/授权订阅、系统运行信息 read model、reasoning/敏感 payload 展示 | D01/D25；见 C02；Realtime best effort 不能承载正确性 |
| P23 | typed API client/URL/Session/theme persistence、真实页面和错误/loading/无权限/删除/冲突、Meeting inline editor adapter | D26/D27；不把 Debug fixture 接入生产 |
| P24 | 嵌入资源/History fallback/Compose/startup/shutdown/migration/PG与MinIO一致恢复和真实组合验收 | D28；API/缺失资源不 fallback HTML；完整 backup tooling 未定范围 |
| P25 | Dashboard 仅容器、全局搜索只有浮层；搜索对象/权限/API 未定 | D27 保留边界，另立设计工作项后方可实现搜索业务 |

已明确不做：Redis、多 Central、微服务、公开注册、项目成员权限矩阵、跨项目检索、Runner OS sandbox、MCP stdio/旧 SSE/Prompts/Elicitation/Tasks、Execution 总时长或 max-turns 强制终止、通用 steering queue、事件历史 Replay/Event Store、自动 Memory retain、Artifact 自动解析/共享对象 refcount、自动 Runner升级、durable RPC journal、断线续传、永久/raw TCP Tunnel。可选 Provider、SMTP、Runner/Desktop、reranker/image selector 缺省与调用失败分别处理；PostgreSQL/pgvector/MinIO 为 mandatory，不能降级为可选。

## 3. 文档冲突清单

| ID | 原文来源 / 差异 | 处置门槛 |
| --- | --- | --- |
| C01 | [Executor README](../../architecture/agent-executor/README.md) §12 写 Scheduler“确定 assignee”“按 Task 自己的失败/重试策略处理 failed Execution”；[Scheduler](../../architecture/scheduler/README.md) §5、[Reconciliation](../../architecture/scheduler/task-reconciliation.md) §7.6/§8/§16 明确只读现有 assignee，不选择 Agent，不解释 succeeded/failed/cancelled | 确认措辞冲突；D01 统一责任，D22/D23 验收不可由 Executor failed 推 Task 状态 |
| C02 | [Runtime View](../../architecture/agent-executor/runtime-view.md) §4 seq 表示 item 创建顺序；§19–20 snapshot max_seq 丢弃缓冲 seq≤max_seq，但既有 item 的 delta/completed 仍共享 seq；§21 item_revision 可选；[Realtime](../../architecture/platform-infrastructure/realtime.md)与[Meeting Timeline](../../architecture/meeting/meeting-timeline-realtime.md)沿用边界 | 跨文档契约缺口，可能丢同 item 新更新/终态；D01 阻断 until 更新游标/revision/watermark及去重固定，D25 race/乱序验收 |
| C03 | [Execution Context](../../architecture/agent-executor/execution-context.md) §3.2、[Domain](../../architecture/agent-executor/execution-domain-model.md) §3.2 为 meeting_turn_id；[Meeting Turn](../../architecture/meeting/meeting-turn-runtime.md) §8 为含 meeting/turn/contribution/participant 的 reference 对象 | D01 固定 typed identity/序列化/查询职责；D22/D24；不自行选择形状 |
| C04 | [Loop](../../architecture/agent-loop/README.md) §8/[Loop Runtime](../../architecture/agent-loop/loop-runtime.md) §20 由 Model System 拥有 Provider timeout/retry；[Chat Runtime](../../architecture/platform-infrastructure/model-system/chat-model-runtime.md) Error Normalization 让 Loop 据 retryable/policy retry；[Usage](../../architecture/platform-infrastructure/model-token-usage.md) §6 也示 Loop 重试 | owner/透明网络重试与上层有限重试语境不清；D01/D09/D22 固定单一责任、attempt usage、取消与 watchdog，不双重重试 |
| C05 | [项目变量](../../architecture/project-work-management/project-environment-variables.md) §15 图写 Config→credential_ref；[MCP](../../architecture/mcp-integration/README.md) §2.3/[Config Lifecycle](../../architecture/mcp-integration/mcp-server-config-lifecycle.md) §1/§4 为 Config profile-only、Connection 拥有 credential binding | 旧图/命名差异待统一；D01/D10/D20 固定引用拥有者与rotation/delete/lease，不将 Secret放Config |
| C06 | [衔接清单](../../frontend-design/architecture-follow-ups.md) §4 写“样式参数均尚未定稿”；[样式规范](../../frontend-design/styles/README.md)明示 2026-10-01 已确认，前端总览同样已定稿 | 未限定未来参数时为陈旧文档冲突；D01 清晰界定，D26/D27 继续复用确认 token，无授权改样式 |
| C07 | [Tool Runtime](../../architecture/tool-system/tool-runtime.md) §7 写 Attempt 可持久化或 Operation 子记录；[Tool Execution](../../architecture/tool-system/tool-execution.md) §4.1 明确独立实体、非 JSON 数组，计划 D18 也独立持久化 | 属于总览“子记录”措辞歧义而非确定矛盾；D01/D18 明确其为独立实体的关联子记录，详情已要求独立持久化，不据总览放宽 |
| C08 | 计划 D06“留存规则”可能被误读成清理授权；[Internal Events](../../architecture/platform-infrastructure/internal-domain-events.md) §25 明确首阶段不自动清理 Outbox/delivery 历史；SchedulerDispatch/Audit 同样不自动过期 | 属边界解释而非当前确定矛盾；D06 的规则必须保持无自动清理，不新增 retention 删除 |

以上是文档审计，不是产品缺陷复现。P21/P20 等输入 snapshot 风险须用具体契约和行为证据关闭；无需在 D00 擅改架构。

## 4. 依赖能力与工具可用性

声明来源为架构与 web/package.json；锁定来源为 web/package-lock.json。Go/PostgreSQL/pgvector/MinIO 只有技术选型，没有固定产品版本、Go module、SDK、migration 或 Compose。MCP [Protocol Runtime](../../architecture/mcp-integration/mcp-protocol-runtime.md) §2 明确 primary 2026-07-28、兼容 2025-11-25、优先官方 Go SDK；它们是已声明协议契约，SDK 实际固定版本和 conformance 尚待 D20。

| 依赖 | 已声明约束 | 锁定版本 |
| --- | --- | --- |
| Node | ^22.18.0 或 >=24.12.0 | 非 npm lock 对象；当前机器 v24.19.0 符合 engines |
| vue | ^3.5.0 | 3.5.43 |
| vue-router | ^4.6.0 | 4.6.4 |
| @floating-ui/vue | ^1.1.0 | 1.1.11 |
| vite | ^8.0.0 | 8.3.1 |
| @vitejs/plugin-vue | ^6.0.0 | 6.0.9 |
| typescript | ~5.9.0 | 5.9.3 |
| vue-tsc | ^3.1.0 | 3.3.11 |
| vitest | ^4.0.0 | 4.1.11 |
| @vue/test-utils | ^2.4.6 | 2.5.1 |
| jsdom | ^27.0.0 | 27.4.0 |
| prettier | ^3.6.0 | 3.9.9 |
| @types/node | ^24.0.0 | 24.19.0 |

实际环境只读探测：Node v24.19.0、npm 11.9.0、Git 2.52.0、Python 3.12.14、rg 15.2.0、Docker client/server 28.4.0、Compose v2.40.3。`go version` exit 1，输出 `Go: Unknown option: version`：PATH 名为 go 的程序不能据此认定为 Go compiler；显式 `/workspace/toolchains/go1.27.1/bin/go version` exit 0 返回 `go version go1.27.1 linux/amd64`，环境存在可用 Go 编译器，项目选用版本仍待 D02，不安装、不升级。psql、pg_isready、minio、mc 不在 PATH。仓库外 onboarding 仅环境准备，不是产品实现：`docker ps --format '{{.Image}}'` 显示 pgvector/pgvector:0.8.1-pg17 与本地 MinIO image；对匹配 PG container 只运行 `postgres --version` 得 17.8 (Debian 17.8-1.pgdg12+1)，显式 infra/minio --version 得 RELEASE.2025-09-07T16-13-09Z、commit 01ce918d8279a20e4706b96a64396146894adee4。pgvector 0.8.1 仅 image 标签，未查询已加载扩展；未验证 SQL/MinIO endpoint/readiness/产品集成。没有产品配置/适配器或已指定测试端点，不读取 env、connection string、docker inspect 或凭据。Docker daemon 可响应版本只证明 Docker可达，不证明数据库/对象存储集成。

后续证据门槛：D02 可构建 compiler；D03 真实隔离 PG/pgvector迁移竞争与恢复；D05 MinIO streaming/一致性；D09 Provider conformance与受控 smoke；D15–D17 真实 Runner/设备/通道；D20 两 MCP profile及 OAuth/resource/tool；D28完整组合与故障注入。纯读环境不算正式依赖集成通过。

## 5. 全部专题阅读与覆盖映射

逐文件按 `sed -n '起行,末行p' 文件` 阅读，每段最多 200 行、每命令 max_output_tokens=6000。早期聚合输出曾截断，依修订 2 改小段并补读；本表为最终无缺失的 1–N 范围，分段可重跑命令见 §6。每行映射到责任工作项；所有业务专题当前仅设计，跨模块边界共同归 D01。

12 一级专题：Agent Executor、Agent Loop、Agent Management、Knowledge/Memory、MCP、Meeting、Platform Infrastructure、Project/Work Management、Runner、Scheduler、Security/Governance、Tool System。架构索引额外计 1，共 77 文件。

| 文件 / 来源 | 已读行范围 | 责任 / 关键门槛 |
| --- | --- | --- |
| [README.md](../../architecture/README.md) | 1–214（完整） | D01；一级索引与依赖 |
| [agent-executor/README.md](../../architecture/agent-executor/README.md) | 1–521（完整） | D22/D25；slot/context/恢复/runtime cursor |
| [agent-executor/execution-context.md](../../architecture/agent-executor/execution-context.md) | 1–658（完整） | D22/D25；slot/context/恢复/runtime cursor |
| [agent-executor/execution-domain-model.md](../../architecture/agent-executor/execution-domain-model.md) | 1–688（完整） | D22/D25；slot/context/恢复/runtime cursor |
| [agent-executor/execution-lifecycle.md](../../architecture/agent-executor/execution-lifecycle.md) | 1–792（完整） | D22/D25；slot/context/恢复/runtime cursor |
| [agent-executor/runtime-view.md](../../architecture/agent-executor/runtime-view.md) | 1–653（完整） | D22/D25；slot/context/恢复/runtime cursor |
| [agent-loop/README.md](../../architecture/agent-loop/README.md) | 1–599（完整） | D22；transcript/compaction/doom-loop/retry |
| [agent-loop/context-compaction.md](../../architecture/agent-loop/context-compaction.md) | 1–983（完整） | D22；transcript/compaction/doom-loop/retry |
| [agent-loop/loop-runtime.md](../../architecture/agent-loop/loop-runtime.md) | 1–777（完整） | D22；transcript/compaction/doom-loop/retry |
| [agent-loop/transcript-context.md](../../architecture/agent-loop/transcript-context.md) | 1–910（完整） | D22；transcript/compaction/doom-loop/retry |
| [agent-management.md](../../architecture/agent-management.md) | 1–288（完整） | D10；Agent配置/能力/引用 |
| [knowledge-memory/README.md](../../architecture/knowledge-memory/README.md) | 1–336（完整） | D12/D13/D14；version/index/scope/只读 reflect |
| [knowledge-memory/agent-memory-domain.md](../../architecture/knowledge-memory/agent-memory-domain.md) | 1–722（完整） | D12/D13/D14；version/index/scope/只读 reflect |
| [knowledge-memory/agent-memory-runtime.md](../../architecture/knowledge-memory/agent-memory-runtime.md) | 1–878（完整） | D12/D13/D14；version/index/scope/只读 reflect |
| [knowledge-memory/knowledge-document-domain.md](../../architecture/knowledge-memory/knowledge-document-domain.md) | 1–751（完整） | D12/D13/D14；version/index/scope/只读 reflect |
| [knowledge-memory/knowledge-indexing.md](../../architecture/knowledge-memory/knowledge-indexing.md) | 1–903（完整） | D12/D13/D14；version/index/scope/只读 reflect |
| [knowledge-memory/retrieval-runtime.md](../../architecture/knowledge-memory/retrieval-runtime.md) | 1–873（完整） | D12/D13/D14；version/index/scope/只读 reflect |
| [mcp-integration/README.md](../../architecture/mcp-integration/README.md) | 1–558（完整） | D20；profile/discovery/credential/resource lease |
| [mcp-integration/mcp-protocol-runtime.md](../../architecture/mcp-integration/mcp-protocol-runtime.md) | 1–531（完整） | D20；profile/discovery/credential/resource lease |
| [mcp-integration/mcp-resource-adapter.md](../../architecture/mcp-integration/mcp-resource-adapter.md) | 1–599（完整） | D20；profile/discovery/credential/resource lease |
| [mcp-integration/mcp-server-config-lifecycle.md](../../architecture/mcp-integration/mcp-server-config-lifecycle.md) | 1–508（完整） | D20；profile/discovery/credential/resource lease |
| [mcp-integration/mcp-tool-default-enable.md](../../architecture/mcp-integration/mcp-tool-default-enable.md) | 1–294（完整） | D20；profile/discovery/credential/resource lease |
| [mcp-integration/mcp-tool-discovery.md](../../architecture/mcp-integration/mcp-tool-discovery.md) | 1–545（完整） | D20；profile/discovery/credential/resource lease |
| [mcp-integration/mcp-tool-execution.md](../../architecture/mcp-integration/mcp-tool-execution.md) | 1–557（完整） | D20；profile/discovery/credential/resource lease |
| [meeting/README.md](../../architecture/meeting/README.md) | 1–661（完整） | D24/D25；Contribution/snapshot/summary/Timeline |
| [meeting/meeting-context-summary.md](../../architecture/meeting/meeting-context-summary.md) | 1–656（完整） | D24/D25；Contribution/snapshot/summary/Timeline |
| [meeting/meeting-domain-model.md](../../architecture/meeting/meeting-domain-model.md) | 1–868（完整） | D24/D25；Contribution/snapshot/summary/Timeline |
| [meeting/meeting-references-inline-content.md](../../architecture/meeting/meeting-references-inline-content.md) | 1–1041（完整） | D24/D25；Contribution/snapshot/summary/Timeline |
| [meeting/meeting-timeline-realtime.md](../../architecture/meeting/meeting-timeline-realtime.md) | 1–888（完整） | D24/D25；Contribution/snapshot/summary/Timeline |
| [meeting/meeting-turn-runtime.md](../../architecture/meeting/meeting-turn-runtime.md) | 1–774（完整） | D24/D25；Contribution/snapshot/summary/Timeline |
| [platform-infrastructure/README.md](../../architecture/platform-infrastructure/README.md) | 1–548（完整） | D02–D09/D25/D28；mandatory依赖/平台事实 |
| [platform-infrastructure/authentication/README.md](../../architecture/platform-infrastructure/authentication/README.md) | 1–28（完整） | D02–D09/D25/D28；mandatory依赖/平台事实 |
| [platform-infrastructure/authentication/account-lifecycle.md](../../architecture/platform-infrastructure/authentication/account-lifecycle.md) | 1–57（完整） | D02–D09/D25/D28；mandatory依赖/平台事实 |
| [platform-infrastructure/authentication/smtp-delivery.md](../../architecture/platform-infrastructure/authentication/smtp-delivery.md) | 1–47（完整） | D02–D09/D25/D28；mandatory依赖/平台事实 |
| [platform-infrastructure/deployment-runtime.md](../../architecture/platform-infrastructure/deployment-runtime.md) | 1–640（完整） | D02–D09/D25/D28；mandatory依赖/平台事实 |
| [platform-infrastructure/human-inbox.md](../../architecture/platform-infrastructure/human-inbox.md) | 1–704（完整） | D02–D09/D25/D28；mandatory依赖/平台事实 |
| [platform-infrastructure/internal-domain-events.md](../../architecture/platform-infrastructure/internal-domain-events.md) | 1–808（完整） | D02–D09/D25/D28；mandatory依赖/平台事实 |
| [platform-infrastructure/model-system/README.md](../../architecture/platform-infrastructure/model-system/README.md) | 1–196（完整） | D02–D09/D25/D28；mandatory依赖/平台事实 |
| [platform-infrastructure/model-system/chat-model-runtime.md](../../architecture/platform-infrastructure/model-system/chat-model-runtime.md) | 1–525（完整） | D02–D09/D25/D28；mandatory依赖/平台事实 |
| [platform-infrastructure/model-system/model-configuration.md](../../architecture/platform-infrastructure/model-system/model-configuration.md) | 1–591（完整） | D02–D09/D25/D28；mandatory依赖/平台事实 |
| [platform-infrastructure/model-system/model-resolution.md](../../architecture/platform-infrastructure/model-system/model-resolution.md) | 1–253（完整） | D02–D09/D25/D28；mandatory依赖/平台事实 |
| [platform-infrastructure/model-token-usage.md](../../architecture/platform-infrastructure/model-token-usage.md) | 1–577（完整） | D02–D09/D25/D28；mandatory依赖/平台事实 |
| [platform-infrastructure/object-storage.md](../../architecture/platform-infrastructure/object-storage.md) | 1–487（完整） | D02–D09/D25/D28；mandatory依赖/平台事实 |
| [platform-infrastructure/outbound-network-policy.md](../../architecture/platform-infrastructure/outbound-network-policy.md) | 1–429（完整） | D02–D09/D25/D28；mandatory依赖/平台事实 |
| [platform-infrastructure/realtime.md](../../architecture/platform-infrastructure/realtime.md) | 1–663（完整） | D02–D09/D25/D28；mandatory依赖/平台事实 |
| [project-work-management/README.md](../../architecture/project-work-management/README.md) | 1–359（完整） | D10/D11/D23；层级/version/blocker/Sprint锁 |
| [project-work-management/project-environment-variables.md](../../architecture/project-work-management/project-environment-variables.md) | 1–684（完整） | D10/D11/D23；层级/version/blocker/Sprint锁 |
| [project-work-management/sprint-lifecycle.md](../../architecture/project-work-management/sprint-lifecycle.md) | 1–808（完整） | D10/D11/D23；层级/version/blocker/Sprint锁 |
| [project-work-management/task-blocker-dependency.md](../../architecture/project-work-management/task-blocker-dependency.md) | 1–467（完整） | D10/D11/D23；层级/version/blocker/Sprint锁 |
| [project-work-management/task-domain-model.md](../../architecture/project-work-management/task-domain-model.md) | 1–451（完整） | D10/D11/D23；层级/version/blocker/Sprint锁 |
| [project-work-management/task-event-timeline.md](../../architecture/project-work-management/task-event-timeline.md) | 1–469（完整） | D10/D11/D23；层级/version/blocker/Sprint锁 |
| [project-work-management/task-state-machine.md](../../architecture/project-work-management/task-state-machine.md) | 1–473（完整） | D10/D11/D23；层级/version/blocker/Sprint锁 |
| [runner/README.md](../../architecture/runner/README.md) | 1–305（完整） | D15/D16/D17；身份/containment/unknown/设备 |
| [runner/agent-workspace.md](../../architecture/runner/agent-workspace.md) | 1–433（完整） | D15/D16/D17；身份/containment/unknown/设备 |
| [runner/control-protocol.md](../../architecture/runner/control-protocol.md) | 1–660（完整） | D15/D16/D17；身份/containment/unknown/设备 |
| [runner/data-channel.md](../../architecture/runner/data-channel.md) | 1–498（完整） | D15/D16/D17；身份/containment/unknown/设备 |
| [runner/desktop-tunnel.md](../../architecture/runner/desktop-tunnel.md) | 1–474（完整） | D15/D16/D17；身份/containment/unknown/设备 |
| [runner/execution-runtime.md](../../architecture/runner/execution-runtime.md) | 1–692（完整） | D15/D16/D17；身份/containment/unknown/设备 |
| [runner/runner-management.md](../../architecture/runner/runner-management.md) | 1–488（完整） | D15/D16/D17；身份/containment/unknown/设备 |
| [scheduler/README.md](../../architecture/scheduler/README.md) | 1–430（完整） | D23；dispatch幂等/quota/不解释终态 |
| [scheduler/scheduler-dispatch.md](../../architecture/scheduler/scheduler-dispatch.md) | 1–854（完整） | D23；dispatch幂等/quota/不解释终态 |
| [scheduler/scheduler-loop.md](../../architecture/scheduler/scheduler-loop.md) | 1–601（完整） | D23；dispatch幂等/quota/不解释终态 |
| [scheduler/task-reconciliation.md](../../architecture/scheduler/task-reconciliation.md) | 1–679（完整） | D23；dispatch幂等/quota/不解释终态 |
| [security-governance/README.md](../../architecture/security-governance/README.md) | 1–898（完整） | D04/D19；Owner/Approval/Audit/安全retry |
| [security-governance/approval-scope.md](../../architecture/security-governance/approval-scope.md) | 1–525（完整） | D04/D19；Owner/Approval/Audit/安全retry |
| [security-governance/audit.md](../../architecture/security-governance/audit.md) | 1–671（完整） | D04/D19；Owner/Approval/Audit/安全retry |
| [security-governance/auto-approval-model/README.md](../../architecture/security-governance/auto-approval-model/README.md) | 1–93（完整） | D04/D19；Owner/Approval/Audit/安全retry |
| [security-governance/auto-approval-model/approval-model-contract.md](../../architecture/security-governance/auto-approval-model/approval-model-contract.md) | 1–751（完整） | D04/D19；Owner/Approval/Audit/安全retry |
| [security-governance/auto-approval-model/approval-model-prompt-context.md](../../architecture/security-governance/auto-approval-model/approval-model-prompt-context.md) | 1–343（完整） | D04/D19；Owner/Approval/Audit/安全retry |
| [security-governance/default-approval-policy.md](../../architecture/security-governance/default-approval-policy.md) | 1–491（完整） | D04/D19；Owner/Approval/Audit/安全retry |
| [security-governance/one-time-approval-retry-idempotency.md](../../architecture/security-governance/one-time-approval-retry-idempotency.md) | 1–488（完整） | D04/D19；Owner/Approval/Audit/安全retry |
| [tool-system/README.md](../../architecture/tool-system/README.md) | 1–623（完整） | D18/D21；immutable spec/Operation与Attempt/backend |
| [tool-system/artifact-tools.md](../../architecture/tool-system/artifact-tools.md) | 1–354（完整） | D05/D21；Artifact业务服务/模型适配 |
| [tool-system/tool-definition-registry.md](../../architecture/tool-system/tool-definition-registry.md) | 1–510（完整） | D18/D21；immutable spec/Operation与Attempt/backend |
| [tool-system/tool-execution.md](../../architecture/tool-system/tool-execution.md) | 1–447（完整） | D18/D21；immutable spec/Operation与Attempt/backend |
| [tool-system/tool-result-backend.md](../../architecture/tool-system/tool-result-backend.md) | 1–431（完整） | D18/D21；immutable spec/Operation与Attempt/backend |
| [tool-system/tool-runtime.md](../../architecture/tool-system/tool-runtime.md) | 1–359（完整） | D18/D21；immutable spec/Operation与Attempt/backend |

Knowledge 子项进一步归属：knowledge-document-domain→D12；knowledge-indexing/retrieval-runtime→D13；agent-memory-domain/agent-memory-runtime→D14；总览跨三项。Platform 子项进一步归属：authentication→D07；deployment-runtime→D02/D28；object-storage→D05；outbound-network-policy→D04；internal-domain-events→D06；model-system/model-token-usage→D09；human-inbox/realtime→D25。Executor runtime-view→D25，其余 Executor/Loop→D22。Security Audit→D04，其余授权/审批→D19。上述归属覆盖嵌套 Model/Auth/Auto Approval 专题，不把它们遗漏为一级 README。

其他输入已读：AGENTS.md、团队 README/tasks、技能、development-plan 1–413、repository-structure、frontend README/components/verification、frontend-design README/architecture-follow-ups/styles README，以及实际 package/lock/router/App/main/AppShell/SystemNav/useTheme/UI exports/styles tests。前端布局通过衔接清单和设计总览逐需求核对后端归属；不是宣称已实现业务页面。

## 6. 实际命令、结果与可重跑检查

在仓库根目录执行：`git status --short --branch`（main，保留已有台账/work-items）、`git log -5 --oneline`（§1五提交）、`git ls-files` 与 `rg --files docs/architecture web/src`（骨架与源码清单）、Python `Path.rglob('*.md')`计数 77、逐文件 sed 小段阅读。`cat web/package.json`及 Python解析 lock得§4版本；`cat`曾尝试不存在的 web/src/services/theme.ts，报不存在后用 `rg --files`定位并读取实际 composables/useTheme.ts，未作为缺失产品功能。

工具命令：`node --version`、`npm --version`、`git --version`、`python3 --version`、`rg --version`、`docker --version`、`docker compose version`均 exit 0；`docker version --format '{{.Server.Version}}'` exit 0 返回 28.4.0；`go version` exit 1 见§4。Python shutil.which 确认 psql/pg_isready/minio/mc 不可用。另执行上述显式 Go/MinIO 版本、Docker image 元数据及 PG binary 版本命令，均成功；环境目录首次递归 rg 遇 PG data Permission denied 且输出截断，改为只枚举顶层文件名与 go-selection 的字段名定位，不提升权限、不读取 data/secrets.env/连接串，未审计安装历史。未执行工具安装或第二次重复失败。

阅读段落可重跑（只读）：

```bash
python3 - <<'PY_READ'
from pathlib import Path
import subprocess
for file in sorted(Path('docs/architecture').rglob('*.md')):
    count = len(file.read_text().splitlines())
    for start in range(1, count + 1, 200):
        subprocess.run(['sed', '-n', f'{start},{start+199}p', str(file)], check=True)
PY_READ
```

工具执行时每个 sed 独立限 6000 tokens，并按文件分次查看，避免聚合截断。链接与 whitespace 的可重跑命令（检查本报告、规格、台账及全部架构的相对文件目标；fragment 不作文件路径；不宣称验证标题 anchor）：

```bash
python3 - <<'PY_CHECK'
from pathlib import Path
import re
files = list(Path('docs/architecture').rglob('*.md')) + [
    Path('docs/development/work-items/d00-baseline-audit.md'),
    Path('docs/development/work-items/d00-baseline-audit-report.md'),
    Path('docs/development/agent-team/tasks.md')]
errors = []
for file in files:
    for raw in re.findall(r'\]\(([^)]+)\)', file.read_text()):
        target = raw.split('#', 1)[0].strip().strip('<>')
        if target and not re.match(r'[a-zA-Z][a-zA-Z0-9+.-]*:', target):
            if not (file.parent / target).exists():
                errors.append((str(file), target))
report = Path('docs/development/work-items/d00-baseline-audit-report.md')
whitespace = [i for i, line in enumerate(report.read_text().splitlines(), 1)
              if line != line.rstrip()]
print('files', len(files), 'missing links', errors, 'report trailing whitespace', whitespace)
assert not errors and not whitespace
PY_CHECK
git diff --check
```

本次结果：上述 80 文件相对目标检查通过；新增报告 trailing whitespace 无；git diff --check 通过（新增未跟踪报告另行检查）。这些只验证文档，不验证产品行为。

未执行：Go/frontend产品测试、构建、浏览器、数据库/对象读写、并发/重启恢复及外部 Provider/MCP/Runner调用。原因：纯文档 D00 授权且产品模块尚不存在，不做无关测试、不接触未指定服务。D00-02 只读验收通过；主线程独立核对完整报告、77/77 覆盖与行数、源码和锁定版本及重要冲突原文，已集成台账，随本项创建本地提交（通过文件 Git 历史定位）。本轮止于 D00，不启动 D01。当前无阻止报告交付的决策阻塞；上述待定/冲突构成后续开工或验收门槛。
