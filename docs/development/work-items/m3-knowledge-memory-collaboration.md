# M3 知识、Memory 与协作集成

## 目标与当前事实

本轮按用户 2026-10-14 决定继续完整原 M3：Knowledge 检索与引用、Memory、领域工具、MCP、Meeting 及其所需 Runner 扩展。M4 已撤出当前里程碑；其余缺口在 M3 完成后按实际结果重新评估。本卡维护本轮共同接缝与接受出口，长期产品规则仍见[开发计划](../development-plan.md#当前开发目标与里程碑)及各领域架构，不能把首条联调成功替代整项完成。

共同开工基线 `d9356e09`，功能分支与唯一写者见[任务台账](../agent-team/tasks.md#当前目标与工作包状态)。Knowledge真实外部Agent引用、Memory受控跨Execution工具与Meeting受控两Agent/Summary三条首链已通过，均有实际后继消费与原App/Guard/fixture完整退出。MCP连接控制面与Knowledge后继serving retention已通过；MCP Agent Tool/Resource、动态Skills安装/Owner分配/正文消费、Knowledge正常自动索引及Memory已退休窗口冷恢复现也已通过，各领域后续完整接受出口仍在实现，M3未完成。

## 用户流程与接受出口

| 功能 | 最早真实组合 | 完整 M3 接受出口 |
| --- | --- | --- |
| Knowledge | 正式 D12 内容变更→固定 profile job→canonical 内容读取与 Close→parse/chunk→Model Embed→同 generation lexical/dense→query-doc→指定版本 read-doc→后继模型引用 | 按 D13 完成候选比较、选型与可重跑数据集；Markdown/text/PDF/DOCX、版本与 generation 淘汰/切换、scope 前置过滤、失败/rebuild/删除清理和可追溯 locator。 |
| Memory | 原 Agent Tool retain→安全处理→结构化抽取/整合→revision→另一 Execution recall→后继模型消费 | retain/recall/reflect 的 namespace、provenance、冲突重读后单次重整合、未知提交查询、删除/superseded 排除、reflect 无写、Secret 在模型和索引前处理；不自动 retain/TTL。 |
| 领域 Tool / Runner | 当前真实 Execution→固定 Tool snapshot→当前授权→领域 Backend/Runner→持久原 Operation/Attempt/result→后继轮消费 | 将 M3 必需的领域能力和动态 Skills 读取/准备/安装/分配接正式服务；按 D15–D19/D21 的当前调用闭集补实际 Runner 支持、安全、等待/取消/恢复与真实结果消费。 |
| MCP | Project Connection→真实 SDK 连接→完整 discovery 原子 reconcile→项目固定 binding→统一 Tool 授权→远端调用/Resource→后继轮消费 | Tool/Resource 与凭据生命周期、项目隔离、Secret lease、受控出站、定义失效/保旧、断线与结果未知、媒体 Object 引用及必要配置/状态 UI；connected 不冒充 discovery 成功，新增定义不扩大 Agent 权限。 |
| Meeting | Human Session→持久 Turn/Contribution launch intent→Meeting Trigger provider→统一 Executor 顺序发言→完整公开结果→summary | proposed/active/archive、参与者/引用、顺序/并行/队列/打断、busy/waiting、Decision answer/skip、Approval、cancel/retry/regenerate/finalize、初次标题/四字段及滚动 summary、Timeline 与必要来源入口、删除与迟到结果边界。 |

Runner 扩展以这些真实流程实际依赖为界：复用已经接受的身份、控制通道、mount、read-file/write-file/run-command、审批与结果持久链；缺少能力时沿正式领域与 Runner 契约补齐并验证其实际平台支持。不能只把接口标成支持，也不把完整 Desktop/Tunnel、全平台平台债务或所有 D22 缺口无界并入本轮。任何本轮触及的权限、Secret、数据完整性、资源退出或恢复风险仍须在接受前通过。

## 本 M3 的 Realtime 接受范围

本阶段补 Meeting Timeline 与展开 Execution 所必需的统一 WebSocket 订阅：当前 Web Session、同源与当前 Project/Meeting/Contribution/Execution 授权；订阅 acknowledgement 后读取权威 snapshot，缓冲通知并按实际 epoch/progress 对齐，断线、慢消费者、权限丢失和对象删除沿 resync/关闭恢复。RuntimeItems 只来自已提交的真实状态、waiting、Tool 摘要/结果及公开文本，不产生 token 增量或私有 reasoning。原 Model JSONCaller、执行/重试/取消所有权保持；已有 canonical 事实作为重建来源，不为此新建逐增量账本或 snapshot flush 引擎。旧 final-text-only 投影不能冒完整 Tool 历史。

本阶段不宣完整 D24/D25 流式能力已齐：逐 token 展示、活动内容的批量持久化及其未 flush 衔接仍为后续评估项。当前订阅、真实内容、权限与退出必须实际验证，轮询或单独终态 HTTP 读取不能替代上述有限 Realtime 接受。

共同成熟后端与私有前端现已按完整 manifest 汇入，包括 Skills 安装恢复/catalog、MCP、Knowledge 文件/hybrid、Memory 和 Meeting/Inbox；共享 App/Web 逐 hunk 融合。141 Decision、RuntimeView 与有限 Realtime 的同 Hub 领域通知、原授权及 Gateway 已接共同 App；首组合编译、必要配置与精确入口检查已过。Gateway 普通/race及有限独审、Execution 公开投影和领域 known-commit 门已过；共同前端受影响五业务 spec 43例、两 codec spec 23例及完整类型检查也已过。尚未以该组合完成真实业务与 Realtime 接受，不能用 isolated 旧门外推。 共同 Reply 受影响三 spec 42例、Realtime 状态三 spec 23例及各自完整类型检查已通过；已准备固定私有 dist/harness，双 topic 真实浏览器仍待。

共同后续删除实际依次暴露独立 HTTP 路由遗漏、未派发贡献的取消标志约束及 Project Audit 要求 EX 而主删除仅持 SH 的首因；均已窄修，修后真实删除完整通过原动作/lookup、held contribution 取消与未派发贡献零启动、四次模型/lease 退休、同事务 completed Audit/Deleted/receipt、外域三 E 保持、迟到 Send 拒绝及 App/Guard/数据库退出。Decision Inbox 新实际确认 answer/skip 的 E/Turn/Inbox 均完成，cancel 的 E/Turn 已取消但 Decision 仍 pending；原因是可选 typed 零 ID 的全零 UUID 编码与 SQL 空串省略条件不符，原域参数编码已窄修，待普通与真实复验，不改 Execution 或迁移。Meeting 上 Memory 的非法 Agent 名称修正后，已完整通过同 E retain/recall/reflect、后继公开回复与 Summary、11次模型退休、canonical provenance、零 lease 及 App/Guard/数据库退出。Runner 首 cold 尚未到目标 reader 屏障，未执行目标停止/重启，新的 child 事务诊断实际仅见七个原 Skill/Mount/Object 事务提交、Transfer 仍 issued/活 lease、脚本尚未出现，未到目标 reader 屏障；继续按该事实定位，原正常整链通过保持。

Artifact 的正式五阶段 user upload 已实现；独审发现的 body/first-attempt 竞态已把拒绝条件带入原 reserve 锁内，真实 Objects 晚到 body/已有 live attempt 两门与独审现已关闭该并发 finding。默认 App 上传原先在 sole 错误要求未使用的第二项目账本时失败；测试按实际 A 账本/B 零 work 窄修后，上传与 metadata point-read 两个真实 App top 现已完整通过。正式五阶段、原 Unknown 确认、权限边界、元数据读取及 App/Artifact Join、零活跃 lease 和全部 fixture 资源退出闭合；不扩称附件浏览器接受。

MCP 原 Delete cold 旧轮未收敛后，发现共享 App 将初始化 budget 上下文传给 Content recovery 与 reconciliation 长期 worker，Ready 时会取消它们。两处调用现解除启动 budget、保留原 owner 的取消与实际 Join；定向生命周期门及独审通过后，新 OriginalDelete 实际已验证原 exchange/claimed 提交、真实 SIGKILL/Wait、新 guard、同原 Delete 收敛及 App Join，有限独审接受。stranded 随后通过真实停止、新 guard、exact materialization 清理及 MCP owner Join、Unknown Tool 零重派发，但新子进程末尾仍强制退出，不宣整 App graceful Join。Published 实际止于 seed App 合并 guard/数据库退出观察，尚未进入目标窗口，sole 已补有界事实观察。startup 隔离与已知 Human Refresh 外层 lease/pointer 冷收尾仍必须分别闭合，不能移作泛生命周期后续。

Summary 的 prepared 已提交、Resolve 前冷窗口已由原 child SIGKILL/Wait、新 guard/原 source absence 收敛、诚实失败、零新 Resolve/Model/lease/Summary、Owner 原取消及新 App Join 实际通过，有限独审接受该窗口；不外推其它 Summary 停止位置。Inline Composer 的32例与完整类型检查、有限独审已过；真实浏览器默认 Enter、跨 scope 和附件仍待。

剩余必达出口按原卡分组推进：Meeting 剩余控制、Decision 取消、有限 Realtime 和 Composer 来源/附件；MCP published connection 窗口、原 startup 隔离及 Human Refresh 外层 lease/pointer 冷收尾；Runner 必要冷退休；Artifact 附件浏览器与独审收口；最后核共同装配受影响组合。复用已接受旧链，真实失败保存后只改首因或补有区分力的诊断，不以增加等待或重复全矩阵代替定位。Agent update/comment 保持未注册，等待规则回复及 root 对完整原 M3 出口的裁定。

## 共同调用契约与所有权

- Knowledge 独占 `retrieval/contract` 与检索实现；Memory 复用同一检索端口及 scope 前置过滤，不复制检索引擎。ServingIdentity 固定 generation、snapshot 和维度，domain 在调用前后检查当前可服务 generation。
- Model owner 提供 `Runtime.Embed`、structured/text Chat 与既有 Secret/Usage/ProcessGuard 生命周期，Runtime 不以裸 HTTP 旁路调用。Knowledge、Memory、Meeting 各自提供真实 `mc.ConsumerAuthority`；App 静态组合 router。服务身份固定到 project 和原 operation/launch intent，构造身份不产生权限。
- 领域 owner 提供 Backend、输入校验、canonical 状态和权限；公共 Tool Runtime 保存 Operation/Attempt，持有原私有 invocation witness，核对 immutable snapshot、原输入、当前 Execution/Agent 和进程。Knowledge 用同 Tx 原参数借阅防止 document/version/query 替换；Memory namespace 来自原 Agent，lookup 只观察原 retain receipt。
- MCP 稳定身份为 `mcp:<config UUID>:<remote name>`。Project registration 固定自己的 SpecRef 与 Config/Connection/RuntimeBinding；远端 endpoint、Secret 不入 Execution snapshot。完整发现列表原子 reconcile；invalid 单项保留旧合法定义，连接/发现失败不删目录。MCP Backend 每个授权 Attempt 只调用一次，未知结果不能伪装失败重发。
- Meeting 领域校验 Session/Turn/Contribution/Participant 四项关系、当前 Agent、generation/attempt、cancel/delete gate；Execution 复用原 AssociatedExecutor/Preparation/Direct/Tool Loop，独立核对持久 Launch。领域写者不另建执行引擎。
- App、既有 Execution/Tool、identity 与必要共享 schema 由 integration 写；Meeting 另独占新增 `execution/meeting_control.go` 及其独立合同/测试，MCP 另独占新增 `tool/runtime/mcp_executor.go` 及测试，均复用原执行器。Model、Knowledge、Memory、MCP、Meeting 各自唯一写领域。全部 Git 由 root 负责。

## 迁移与验证

全局迁移号由root分配。00120 Knowledge retention、00121公共Skills下一轮输入、00122 Memory profile已与消费者组合。00123前向修复MCP typedref JSONB表达式优先级及00121变量歧义，实际PG合法/拒绝样例通过后冻结。当前共同候选新增00124 Knowledge自动profile事实及00125 Skills原ToolCall路由闭集，00125原生产约束合法/拒绝输入及完整迁移已实际PG通过，00124已随自动索引实测应用；00126领域PG已过，00127已随真实测试应用且不可回写；00128–00133 已实际应用；共同正式 1–135 空库、重复、升级与健康门通过，134/135 已不可回写。136 工具/Proposal/Inbox、137 删除意图、138 exact skill.prepare Unknown、139 Knowledge 本域引用环及140删除审计已组成连续源码，共同正式 1–140 空库、重复、升级与健康门现已通过；140真实139升级的审计正负约束与旧 row/xmin保持也已通过，全1–140不可回写。136/137/140 领域与 App 装配已通过普通组合门，137 schema 通过也不能替代退休/purge。后继141独立门已通过空库、真实140存量升级/重复、旧提案字段与xmin保持及Decision source/context/事件的严格SQL约束，141已实际应用不可回写；它不证明领域权限或Outbox首链。MCP 后继正式1–142空库、重复、升级与健康门也已通过，全1–142不可回写；143未分配，142 schema 通过不代OAuth cold。127 误删释放版本下界且残留旧 owner CHECK 的实际缺陷已用 133 前向修复，坏存量整体拒绝/回滚与原合法行保持通过；Agent assignment、Summary 固定 snapshot 和 OAuth 原进程绑定的业务接受范围另列。旧迁移不改写、不造空占位。`postgres.NewSource`要求版本从1连续无缺号，带新约束的迁移必须与实际调用方一起组合；不在main集成产品WIP。

先做能区分正常/拒绝与业务不变量的基础检查，尽早接真实提供方/调用方；随后按变更风险完成当前授权、跨项目/Agent、迁移/原子性、取消/未知结果与完整退出检查。作者自测不冒充独立验收。首条链、后继模型实际消费和完整资源退出必须同时可观察，类型编译、单库通过、连接成功或 checkpoint 不构成 M3 PASS。

资源实测可复用 `/workspace/toolchains/go1.27.1/bin/go`、共享 Go cache/module、锁定 Node 依赖和固定 SHA MinIO；PATH 的 `go` 不是该编译器，命令使用显式路径。CPU 实际配额 4 核、内存上限 16Gi，最初 overlay 可用 19Gi；重型构建最多同时两组；当前真实 PG/App fixture 单组运行且不与重编译竞争，逐轮独立目录/端口/数据库，不连接现有 dev PG/MinIO。Playwright 1.56.1 与原批准的 system Chromium 151（固定实件 hash）复用，Meeting 入口正在真实 App/browser 验证。真实外部模型保留用户代理与 TLS 设置，以实际 HTTP 结果判断可用性，不凭 DNS 推断停工；凭据不进入日志或证据。

## 显式检索部署输入

`AGENTEAM_CENTRAL_RETRIEVAL_RUNTIME` 缺省不启用检索与 Memory 工具；配置时必须同时使用已合法配置的 `tool-loop-v1` Execution runtime。它是严格 JSON，当前仅接受 `backend` 与 `dimensions`：例如 `{"backend":"native_pg_fts","dimensions":4096}`。后端必须明确选择 `native_pg_fts`、`pgroonga` 或 `pg_search`，维度范围为 1–4096，不能从模型名称猜测或截短响应。空值、重复/未知字段、错误类型和越界均在配置检查时拒绝，不连接数据库或模型。

原生 PostgreSQL 只用于既有受限首链。原 hybrid 生产接线选择固定 `pg_search 0.26.1`，准确安装、显式 bootstrap、两域实际 SQL/过滤及同库就绪检查已通过隔离 PG；共同 App readiness 普通门及[显式部署配置](../../../deploy/retrieval/README.md)解析通过，正式镜像已构建并核固定包/hash/许可与 immutable image ID；固定 schema125 的同 App/同库真实 hybrid 链已通过原 bootstrap/readiness、Knowledge 自动 profile 与 Agent query/read/citation、Memory 四 E retain/recall/reflect/delete/namespace、两域实际 hybrid diagnostics 和完整退出。新139清理后已另行通过限定真实门；较新共同 source 仍须按实际消费者验证。实际环境须具备选定 extension，切换模型维度须新 profile/rebuild。初始候选池为 lexical/dense 各64、融合64、结果8、等权 RRF k=60、结果字节64KiB；分段 soft/hard=2048/4096 bytes、强制切分 overlap256 bytes；lexical/dense 各5秒、query embedding90秒、索引 Model120秒。这些是有界初始值，后继按冻结评测调优，不声称最佳相关性。

## 当前验证与下一步

- 公共契约、Secret/Usage、typed Audit/Project、Meeting association/source read、Registry 与正式 Object MCPFile 的普通包/拒绝门已作定向检查；连续00100–18 Source 已通过解析，Meeting首轮auto与Task伪造拒绝定向62308原Wait0；领域 PG 仅证明各自冻结的迁移组合，新增迁移仍须真实约束检查。
- MCP 官方 SDK 经受控 outbound/TLS 的 modern/legacy Tool/Resource、凭据反射拒绝、未知无重发、current policy 四项整轮 PASS，实际提供方与 PG 尾巴清理完成；后继四类媒体 canonical/诚实 Unsupported 与原私有上下文 source Read/Close 已按下文范围通过，不外推生产媒体 profile 支持。
- Memory85537真实App受控模型首链已通过：4个Scheduler Execution、5个Tool、6个Memory Model，retain、跨Execution recall、只读reflect、跨Agent隔离、Owner删除后排除、后继Tool消费、generation引用/Audit/Usage和全部原owner退出。后继7713 profile切换通过：4E/5Tool/7Memory Model，新增profile/vector/query snapshot、旧generation退休释放v2、canonical与隔离保持、实际peer请求及App/Guard全部退出。Owner浏览器38189原Wait0，真实retain后的列表/历史/删除、丢失响应原lookup与刷新排除通过，Node/App/Guard/三fixture完整退出。旧30511退出合并断言失败不回写。后继10221真实SIGKILL/Wait后新进程只Lookup原retain receipt，下一Task退旧building/pin并重建，通过5个Memory模型、原operation/canonical/Audit不重复与完整退出；仅覆盖旧Model已全部退休的窗口。
- Model 受控 runtime 三类新顶层通过；真实外部 embedding 使用版本化 v2 usage 兼容 profile，在同一 snapshot 的64/64/8批次获得136个4096维向量，Usage/Secret/Close/Joined/Drain及独立fixture清理断言通过。旧v1严格响应合同保留。此证据不覆盖知识/Memory Tool 消费。
- App 显式检索配置与既有生命周期回归、integration-tag 组合编译已过。Memory ProcessGuard与长期serving retention已接同一静态router/具体Model retention实例；Observation只分派只读方法，不转成Apply。MCP生产装配已接统一审批/执行、资源捕获、原Execution lease退休与HTTP；79351实际Agent Tool/Resource首链已通过，不代OAuth或媒体。
- Knowledge79483真实外部模型首链已通过：canonical Object→4096维generation原子发布→正常Scheduler→query-doc→精确版本read-doc→原ToolResult由sent/succeeded后继Model消费并输出document/version来源引用。原Index/App/Guard与三fixture完整收尾；先前技术Actor及harness失败保留。后继46936受控App pin链已通过：Provider换credential后旧generation仍保留原引用、第二次Rebuild原子切换释放旧引用，两个实际Model call退休及Index/App/Guard完整退出；该固定候选未启动worker。后继34447原Wait0：同一IndexService.Run正常自动首建/更新/selector变更发布与旧pin释放，再由真实Scheduler query→精确版本read→后继ToolResult消费及引用全部通过，4Embed/3Chat和原App/Guard/三fixture完整退出。Object私有job精确目标授权已消费；四格式上传/当前版本源读取、Parser及显式PDF配置已实现；31818 四格式 HTTP/current source、PDFv2 替换、真实 Agent query/read 页码引用及正式 App 退出全部通过；Scheduler 在取消后沿原已返回 NotCommitted owner 有界完成拒绝记录，Unknown 保留不重发。6Embed/3Chat、全部 owner/Guard 和三 fixture 完整退出。四格式浏览器整链现已通过上传、逐字下载、PDF 预览与替换、窄屏深色键盘操作、后继 Agent 页码引用及 App/Guard/fixture 完整退出。原 sandbox 与 harness 失败保留。独立无 PG 的真实组件截图随后确认 PDF ready 早于首绘制，有限等待后正文可见，不另冒一轮 App 验收。
- Meeting Owner浏览器已完成列表、新建、详情、发送、丢失已提交响应后的原lookup与刷新，限定Agent busy；另有62671两Agent首链通过OwnerStart→A→B→Summary，B实际输入含A，恰好3次受控HTTP/3个Model成功退休。Task首轮required保持，Meeting由原captured source允许auto。Decision answer/skip在79159分别通过结果配对、同E继续、Summary及退出；cancel在UI固定候选56432通过同E取消、迟到answer拒绝、无结果/Summary、Model/Secret退休与原App/Guard退出。三个分支为分轮证据，不称旧失败整top通过。21386真实三turn浏览器随后通过answer/原lookup、skip、cancel、archive/restore及同E结果/Summary，Node/App/Guard/三fixture完整退出；后继62173真实PG的targeted read与单Contribution取消/Runtime重建两门通过，使用受控Executor；实际Runner准备缺ProjectSchedule锁已由公共锁预算修复；后继86470审批浏览器整top原Wait0/PASS51.95s，真实Runner批准及丢响应原lookup、拒绝零Attempt、单Agent取消后B继续及Summaryv3，晚approve拒绝、原lease/effect和App/Guard完整退出全部通过。未派发预留Operation保持原M2 created/v1/零Attempt，旧测试误期望和timeout失败不回写。完整控制、恢复与来源入口仍待。
- Model长期serving retention的真实受控调用证明Provider换凭据/删除后旧snapshot仍使用原凭据、两个generation独立引用，填充数据升级和COMMIT未转发/ACK丢失两种真实Unknown观察有限通过；业务generation权威由该fixture提供，不替代Knowledge/Memory实际接入。
- MCP Resource文本必须在正式Object canonical保存后经原权限读取、限长消费和Close，再向当前text tools profile投影正文；当前MCPFile媒体Ref本身不能被该profile直接消费。79351原Wait0已验证actualPut/Read/Close、canonical文本进入后继模型及原runtime/Secret/owner完整退出；原生媒体消费仍未完成，不能把Ref当正文。
- 动态Skills正常首链已由`m3_model`完成，现由`m3_memory_tools`在专用树接续Agent assignment：复用原install账本，接list/read/assign/prepare及下一轮immutable输入；integration保留公共闭集/Composite/App。已有底层包不代实际入口，相关Runner按本卡边界补齐。DOCX有界实现已有限定测试，PDF有界Poppler进程组件及实际限额/取消Wait已定向通过，已绑定Index及App显式配置，四格式OwnerHTTP与后继Agent引用/退出已过，四格式浏览器整链已通过，范围以本节原调用及完整退出为界。

OAuth Secret56569原写入及两个真实Unknown分支通过，有界独审无该范围must-fix；不代OAuth协议。MCP89142真实Owner Connection、SDK发现、Registry Tool/Resource目录、lookup、启停/断开和跨Project拒绝已通过，原资源全部退出；该轮Tool调用0，不代Agent Tool/Resource消费。

共享SQL故障已由00123实际PG门修复；00125原生产ToolCall名称约束的合法/拒绝输入及完整迁移也已通过，旧失败保持。MCP原AgentRun Secret审计及Object维护路由已由79351完整Agent首链消费：远端echo一次、Resource canonical保存与读取关闭、后继正文消费和所有租约/owner退出均通过。Owner凭据真实HTTP CRUD/CAS、wrong-purpose、撤权零新写、丢失响应只读lookup及限定独审已通过；off/direct-text 控制面已实际验证且工具保持禁用；Owner 浏览器的凭据原 lookup、SDK 目录、启停/断开/删除及完整退出通过，ResourceLink 也已实际调用官方 SDK、canonical Object Read/Close 并被后继模型消费。OAuth 正常 domain 首链已通过真实 DCR/S256 授权码一次交换、原命令回查、flow Secret 退场、token 封存后的 SDK 发现及原 App Join；83507 原 Wait0/PASS7.20s，四组 owned 资源完整退出。后继82194原Wait0/PASS8.20s实际Owner HTTP从oauth配置、begin及原回查，到同Session/CSRF complete及回查、授权码第二key拒绝、官方SDK发现与App Join；原四组owned完整退出。OAuth UI 的真实 Session/router/transport 组合 98 项及类型检查已过；浏览器已通过真实 HTTPS Session→Begin→issuer TLS relay→callback/CSRF Complete→丢失响应后 reload/原 Lookup→SDK 目录及完整退出，issuer 不宣 Chromium 直接访问；冷恢复仍待；135 的真实到期刷新已通过原 issuer 单次 refresh、Secret CAS、原 startup caller/外层请求退休、重复查询零刷新与完整 App/资源退出。MCP 四类媒体经实际 SDK/Object canonical 保存、精确字节/MIME 与后继轮持久后，由现 text profile 诚实返回 Unsupported，只有原一次模型 wire；所有 lease/owner 退出。独立 source consumer 在原 Model 私有上下文通过正式 Read/Close，并拒绝裸上下文、他项目、替换引用及取消后新读；该测试不替代生产模型媒体支持。新 startup/recovery 装配的 off/direct-text 控制面回归也通过。

动态Skills77051原Wait0：实际原Install账本安装→Owner显式分配→旧轮list不含新增→下一轮控制消息/list生效→Object正文Read/Close→第五Model消费通过，4个immutable skill input/1个control及reader leases全部释放。Agent assign 原 self 场景 83706 整 top 已通过：原 source/Audit、四引用全部 v2、旧 captured 保持、下一轮 SkillsInput/list、Object 正文/Close、模型消费与全部 owner/App/Guard 退出；旧 text/UUID 观测 SQL 错误保留。catalog/其他目标整 top 已通过：同项目目录与分配、caller/cross-project 拒绝、目标新 E 实际正文读取、严格引用/Audit 及完整退出。撤权后原 checkpoint 反复 Forbidden 的窄修已实际通过：仅本地确定拒绝且已知回滚转原 E 终态，零目录成功/分配，完整 App/Guard 退出。Tool 最终事务真实 COMMIT/rollback Unknown 两分支通过原 attempt 观察、backend 仅一次、commit 原 row/xmin 保持与完整退出；不外推 Skill 发布 Unknown。Runner prepare 的本地受控 HTTP、安全发布与原 run-command 脚本已过，Central 五包类型闭包与局部权限门已过，正式 Transfer/session/mount/App 已接齐，原 Project Object Transfer 审计闭集缺口已修并获独立复核；正常整链已通过 Owner 安装分配、真实 Object GET/Runner 包、原 run-command Exit0、三轮模型消费、原 Transfer/lease 退休及 App/Runner 正常 Join。此前两轮业务期间的会话断开与重连原因仍未确定，新轮成功不关闭该间歇问题；后继 Unknown/维护/冷退休继续保留原诊断。Skill 发布 Unknown 的提交分支已只读确认 publication、保原行/xmin、首次完成同 attempt 的 Tool 结果并被原 E 消费；回滚分支保留 Unknown/未 Joined 后真实停止，两轮分别接受，旧整 top 失败不回填。发布 U1 确认后新 Tool 写 U2 的错误身份覆盖已窄修、定向回归并获独立复核，不冒物理 U2 注入。下一步继续 Knowledge 新清理、MCP OAuth/恢复、Meeting 完整控制与必要领域 Tool/Inbox，复用已通过的 Model/Memory 停止恢复。作者自测不代独立接受，正常首链不代本卡完整出口；风险结论见[独立报告](m3-core-independent-review.md)。

Summary snapshot 原七回复整链现已通过：第一 generation 的格式失败保 pin、跨 selector 原 snapshot retry/lookup、发布与 pin 释放，第二 generation 使用新的合法 text-only 模型，原 sent call 取消、全部 pin 释放及 App/Guard/数据库连接退出。旧 profile 和 wire fixture 失败保留；该结果不代 134 source/cold 恢复。MCP 媒体/source 与新装配 modes 已按上述范围通过，旧失败不回填。OAuth refresh/startup/有限冷退场已过普通 source/version/issuer/Unknown 门，135 真实迁移及原 issuer 到期旋转已通过；OAuth 浏览器随后整链已通过；cold 仍待。新的冷恢复独审两处必修已固定，受影响 MCP 普通门及私有 OAuth 前端21例已过，后继 App 配置和三个 cold 入口已准备；不回填先前普通通过或宣 cold 已接受。四个已定义工具、Proposal、Human Inbox 与 App 已装配。首真实链完成五次工具调用、模型最终回复及 Summary，提案和事件各两条的旧 Inbox 零行断点已由正式 Outbox 私有 live-Tx/进程门修复；最终 Tool text-parts 断言修后，完整 HTTP 整链已通过四类工具五次调用、canonical Inbox 投递、Owner 审阅/原 lookup/rebuild/恢复/发送、精确原 receipt 后继模型消费与全部 App/Guard 退出，浏览器后继独立整轮已通过 Owner 两提案审阅、返回 pending0/handled2、后继九回复、App Join 与 guard stopped/数据库连接零的首样观察。14条 HTTP 观察均 finished/200；六资源ID/三目录与捕获的App/PW进程退出齐，Chromium各子PID未逐个捕获的限制保留。先前尾观察与响应完成观察失败均保留，不回填；update/comment 两工具定义等待用户异步澄清，其他入口继续实现。

Meeting 原并行/队列/插话整链已完整通过：双并发共享原输入、旧 queued 零启动、取消原两 E、插话优先、旧 FIFO 完成与两次 Summary，八次模型调用及全部 lease/App/Guard 退出。接受范围是固定 1–133 候选，不代共同新源或134 cold。OAuth 域已整体移交 MCP owner；新 HTTPS fixture 的私有信任隔离正反门及 OAuth 浏览器完整旅程现已分别通过。staticBearer stranded 实际已到原 Put 返回、旧进程 SIGKILL/Wait 和新 guard，后继结果观察错误仍待定位，未确认清理/Join。

Skills 安装 published 窗口 cold 已通过真实旧进程 SIGKILL/Wait、新 guard、同 E/Attempt/原进程绑定、原 canonical row/xmin保持、一次安装上传与零自动分配、后继模型消费和 newApp 正常退出；该固定候选使用 Object guard 初始化后的同实例有限 Recover。reserved 窗口未覆盖，Pending 保持原事实。独审发现的 Stop/只读 lookup 交错导致 Drain 错报已窄修，并通过确定 barrier 回归与独立复核。Knowledge populated138→139 独立升级分支与 fresh139 清理现分别通过。后者验证两项目索引、正式 pin 释放、原 source pair 原子清理、两次成功 COMMIT 后由 Store 边界返回 Unknown 的原只读确认及四域同事务写入差值为零，Model/其他项目保持与 App Join、六资源ID/三目录清退齐。旧 confirmation INVALID_STATE 已定位为 harness 误把 backend 尚未 flush 的事务统计绝对值当本次写入；仅改 before/after 差值观察，生产、原预算与 Unknown 不重发边界保持。

长期 Skill 规范明确显式更新与旧 revision 保留，但当前 M3 不另建多版本发布系统；该能力保留为未实现架构缺口。现有验证只覆盖合法 revision 的不可变绑定、分配与来源，不宣跨发布旧版本已验。

## 原 Model owner 停止后的有界恢复

原 `Model.Runtime.Initialize` 未退休 call 阻塞现已有受控恢复实现：ready之前核真实旧进程停止、原Call/Attempt/Usage/Secret及borrower精确事实，未确认仍拒绝启动。authorized/sent 两分支已分别实际 kill/restart 通过，原 Call/Attempt、诚实 Unknown、精确 borrower 关闭及零重发保持；已终态 call 的 borrower 尾巴和 Memory ModelCallOwner lease 门也分别通过。pre-admission无call borrower已96718原Wait0通过真实停止、原事实保持、零重发和新App Join。81711物理Unknown整top原Wait0/PASS38.99s，commit实际提交后仅原plan确认；rollback保留原Unknown，第三明确新App才清理，全部原owner与fixture完整退出。旧失败保持。StoppedResolution 三态在固定新 binary 上由未参与实现者实际运行通过，原三 fixture 与进程完整退出；先前双 Spool/opaque equality 的 harness 失败不回填。`ModelCallLeaseRetirement` 仍要求原 call 已退休。Memory10221 已退休窗口继续有效；新增93314 authorized 与58830 Resolve 已提交但 Begin 未入场窗口均实际通过旧 child SIGKILL/Wait、新 guard、原 Model/Usage/Secret 和 Memory 映射退休、原 retain canonical 失败、原 Tool 失败被 Agent 消费及完整 App 退出，零模型重发/新 Memory。独审发现三处后续观察失败覆盖原写 Unknown，已保留原 cause 并通过九个读失败/取消/observer Unknown 定向回归和源码复核，不将该普通门称物理 Unknown 验证。共同 App 已组合这些提供方与调用方并通过受影响普通门；Knowledge、Meeting 及其它恢复仍以各自真实业务窗口为界。

最小工程范围是提供原 call 的只读观察与停止后退休能力。请求必须绑定原 Consumer、Project/Agent、Operation、CallID、snapshot/lease owner/lease ID、完整 AttemptIdentity（InvocationID、index、ProcessID、fence）和 Input digest；Model 从自己持久记录重建并核对，领域通过原恢复私有 witness 证明该业务 owner，不能由裸 DTO 获权。观察不改变账本；退休沿原 Usage/Secret/ProjectCall 事实与完整预发现锁集合，提交未知只观察原目标，不重放外发。已有完整业务候选才允许领域重新发布，否则保持明确失败/未知，后继新逻辑重试遵循原产品操作。

停止证明使用当前真实 `ProcessGuard.ConfirmStopped(oldProcess)`：核原 claim、相同可信 host/deployment/spool 和文件 dev/inode，非阻塞取得原 claim 的精确 flock；当前进程、仍持锁、不同宿主或无法验证的 claim 均拒绝。不能以 TTL、空本地 map 或数据库 stopped 字段替代。恢复端口需能在普通 Runtime admission 之前运行，而不放宽现有 Initialize 拒绝门。验证只针对原活进程拒绝、真实旧进程 Wait 后恢复、未知提交两分支及零新外发，逐条区分平台退休与领域重新发布，不要求无依据重跑已过正常链。
