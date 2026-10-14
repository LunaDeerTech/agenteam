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

## 共同调用契约与所有权

- Knowledge 独占 `retrieval/contract` 与检索实现；Memory 复用同一检索端口及 scope 前置过滤，不复制检索引擎。ServingIdentity 固定 generation、snapshot 和维度，domain 在调用前后检查当前可服务 generation。
- Model owner 提供 `Runtime.Embed`、structured/text Chat 与既有 Secret/Usage/ProcessGuard 生命周期，Runtime 不以裸 HTTP 旁路调用。Knowledge、Memory、Meeting 各自提供真实 `mc.ConsumerAuthority`；App 静态组合 router。服务身份固定到 project 和原 operation/launch intent，构造身份不产生权限。
- 领域 owner 提供 Backend、输入校验、canonical 状态和权限；公共 Tool Runtime 保存 Operation/Attempt，持有原私有 invocation witness，核对 immutable snapshot、原输入、当前 Execution/Agent 和进程。Knowledge 用同 Tx 原参数借阅防止 document/version/query 替换；Memory namespace 来自原 Agent，lookup 只观察原 retain receipt。
- MCP 稳定身份为 `mcp:<config UUID>:<remote name>`。Project registration 固定自己的 SpecRef 与 Config/Connection/RuntimeBinding；远端 endpoint、Secret 不入 Execution snapshot。完整发现列表原子 reconcile；invalid 单项保留旧合法定义，连接/发现失败不删目录。MCP Backend 每个授权 Attempt 只调用一次，未知结果不能伪装失败重发。
- Meeting 领域校验 Session/Turn/Contribution/Participant 四项关系、当前 Agent、generation/attempt、cancel/delete gate；Execution 复用原 AssociatedExecutor/Preparation/Direct/Tool Loop，独立核对持久 Launch。领域写者不另建执行引擎。
- App、既有 Execution/Tool、identity 与必要共享 schema 由 integration 写；Meeting 另独占新增 `execution/meeting_control.go` 及其独立合同/测试，MCP 另独占新增 `tool/runtime/mcp_executor.go` 及测试，均复用原执行器。Model、Knowledge、Memory、MCP、Meeting 各自唯一写领域。全部 Git 由 root 负责。

## 迁移与验证

全局迁移号由root分配。00120 Knowledge retention、00121公共Skills下一轮输入、00122 Memory profile已与消费者组合。00123前向修复MCP typedref JSONB表达式优先级及00121变量歧义，实际PG合法/拒绝样例通过后冻结。当前共同候选新增00124 Knowledge自动profile事实及00125 Skills原ToolCall路由闭集，00125原生产约束合法/拒绝输入及完整迁移已实际PG通过，00124已随自动索引实测应用；00126领域PG已过，00127已随真实测试应用且不可回写；00128–00133 已实际应用；共同正式 1–133 空库、重复、升级与健康门通过。共同源码新增 134 的完整 ResolveSource caller 和 135 startup refresh 草稿，普通组合/Source 解析不代真实 PG。127 误删释放版本下界且残留旧 owner CHECK 的实际缺陷已用 133 前向修复，坏存量整体拒绝/回滚与原合法行保持通过；Agent assignment、Summary 固定 snapshot 和 OAuth 原进程绑定的业务接受范围另列。旧迁移不改写、不造空占位。`postgres.NewSource`要求版本从1连续无缺号，带新约束的迁移必须与实际调用方一起组合；不在main集成产品WIP。

先做能区分正常/拒绝与业务不变量的基础检查，尽早接真实提供方/调用方；随后按变更风险完成当前授权、跨项目/Agent、迁移/原子性、取消/未知结果与完整退出检查。作者自测不冒充独立验收。首条链、后继模型实际消费和完整资源退出必须同时可观察，类型编译、单库通过、连接成功或 checkpoint 不构成 M3 PASS。

资源实测可复用 `/workspace/toolchains/go1.27.1/bin/go`、共享 Go cache/module、锁定 Node 依赖和固定 SHA MinIO；PATH 的 `go` 不是该编译器，命令使用显式路径。CPU 实际配额 4 核、内存上限 16Gi，最初 overlay 可用 19Gi；重型构建最多同时两组；当前真实 PG/App fixture 单组运行且不与重编译竞争，逐轮独立目录/端口/数据库，不连接现有 dev PG/MinIO。Playwright 1.56.1/Chromium1194 已恢复，Meeting 入口正在真实 App/browser 验证。真实外部模型保留用户代理与 TLS 设置，以实际 HTTP 结果判断可用性，不凭 DNS 推断停工；凭据不进入日志或证据。

## 显式检索部署输入

`AGENTEAM_CENTRAL_RETRIEVAL_RUNTIME` 缺省不启用检索与 Memory 工具；配置时必须同时使用已合法配置的 `tool-loop-v1` Execution runtime。它是严格 JSON，当前仅接受 `backend` 与 `dimensions`：例如 `{"backend":"native_pg_fts","dimensions":4096}`。后端必须明确选择 `native_pg_fts`、`pgroonga` 或 `pg_search`，维度范围为 1–4096，不能从模型名称猜测或截短响应。空值、重复/未知字段、错误类型和越界均在配置检查时拒绝，不连接数据库或模型。

原生 PostgreSQL 只用于既有受限首链。原 hybrid 生产接线选择固定 `pg_search 0.26.1`，准确安装、显式 bootstrap、两域实际 SQL/过滤及同库就绪检查已通过隔离 PG；共同 App readiness 普通门及[显式部署配置](../../../deploy/retrieval/README.md)解析通过，镜像构建和真实 App hybrid 链仍待。实际环境须具备选定 extension，切换模型维度须新 profile/rebuild。初始候选池为 lexical/dense 各64、融合64、结果8、等权 RRF k=60、结果字节64KiB；分段 soft/hard=2048/4096 bytes、强制切分 overlap256 bytes；lexical/dense 各5秒、query embedding90秒、索引 Model120秒。这些是有界初始值，后继按冻结评测调优，不声称最佳相关性。

## 当前验证与下一步

- 公共契约、Secret/Usage、typed Audit/Project、Meeting association/source read、Registry 与正式 Object MCPFile 的普通包/拒绝门已作定向检查；连续00100–18 Source 已通过解析，Meeting首轮auto与Task伪造拒绝定向62308原Wait0；领域 PG 仅证明各自冻结的迁移组合，新增迁移仍须真实约束检查。
- MCP 官方 SDK 经受控 outbound/TLS 的 modern/legacy Tool/Resource、凭据反射拒绝、未知无重发、current policy 四项整轮 PASS，实际提供方与 PG 尾巴清理完成；尚未证明 Owner→Secret→Agent→统一执行器→媒体整链。
- Memory85537真实App受控模型首链已通过：4个Scheduler Execution、5个Tool、6个Memory Model，retain、跨Execution recall、只读reflect、跨Agent隔离、Owner删除后排除、后继Tool消费、generation引用/Audit/Usage和全部原owner退出。后继7713 profile切换通过：4E/5Tool/7Memory Model，新增profile/vector/query snapshot、旧generation退休释放v2、canonical与隔离保持、实际peer请求及App/Guard全部退出。Owner浏览器38189原Wait0，真实retain后的列表/历史/删除、丢失响应原lookup与刷新排除通过，Node/App/Guard/三fixture完整退出。旧30511退出合并断言失败不回写。后继10221真实SIGKILL/Wait后新进程只Lookup原retain receipt，下一Task退旧building/pin并重建，通过5个Memory模型、原operation/canonical/Audit不重复与完整退出；仅覆盖旧Model已全部退休的窗口。
- Model 受控 runtime 三类新顶层通过；真实外部 embedding 使用版本化 v2 usage 兼容 profile，在同一 snapshot 的64/64/8批次获得136个4096维向量，Usage/Secret/Close/Joined/Drain及独立fixture清理断言通过。旧v1严格响应合同保留。此证据不覆盖知识/Memory Tool 消费。
- App 显式检索配置与既有生命周期回归、integration-tag 组合编译已过。Memory ProcessGuard与长期serving retention已接同一静态router/具体Model retention实例；Observation只分派只读方法，不转成Apply。MCP生产装配已接统一审批/执行、资源捕获、原Execution lease退休与HTTP；79351实际Agent Tool/Resource首链已通过，不代OAuth或媒体。
- Knowledge79483真实外部模型首链已通过：canonical Object→4096维generation原子发布→正常Scheduler→query-doc→精确版本read-doc→原ToolResult由sent/succeeded后继Model消费并输出document/version来源引用。原Index/App/Guard与三fixture完整收尾；先前技术Actor及harness失败保留。后继46936受控App pin链已通过：Provider换credential后旧generation仍保留原引用、第二次Rebuild原子切换释放旧引用，两个实际Model call退休及Index/App/Guard完整退出；该固定候选未启动worker。后继34447原Wait0：同一IndexService.Run正常自动首建/更新/selector变更发布与旧pin释放，再由真实Scheduler query→精确版本read→后继ToolResult消费及引用全部通过，4Embed/3Chat和原App/Guard/三fixture完整退出。Object私有job精确目标授权已消费；四格式上传/当前版本源读取、Parser及显式PDF配置已实现；31818 四格式 HTTP/current source、PDFv2 替换、真实 Agent query/read 页码引用及正式 App 退出全部通过；Scheduler 在取消后沿原已返回 NotCommitted owner 有界完成拒绝记录，Unknown 保留不重发。6Embed/3Chat、全部 owner/Guard 和三 fixture 完整退出。浏览器已实际通过四格式上传和逐字下载。原 Chromium PDF sandbox 阻断已由真实组件对照定位并改为显式 PDF object；后继整链在 CDP POST-body 和 closed-details 的 harness 观察失败，实际 DOM/同 context GET 预检已通过，仍无浏览器整链接受。
- Meeting Owner浏览器已完成列表、新建、详情、发送、丢失已提交响应后的原lookup与刷新，限定Agent busy；另有62671两Agent首链通过OwnerStart→A→B→Summary，B实际输入含A，恰好3次受控HTTP/3个Model成功退休。Task首轮required保持，Meeting由原captured source允许auto。Decision answer/skip在79159分别通过结果配对、同E继续、Summary及退出；cancel在UI固定候选56432通过同E取消、迟到answer拒绝、无结果/Summary、Model/Secret退休与原App/Guard退出。三个分支为分轮证据，不称旧失败整top通过。21386真实三turn浏览器随后通过answer/原lookup、skip、cancel、archive/restore及同E结果/Summary，Node/App/Guard/三fixture完整退出；后继62173真实PG的targeted read与单Contribution取消/Runtime重建两门通过，使用受控Executor；实际Runner准备缺ProjectSchedule锁已由公共锁预算修复；后继86470审批浏览器整top原Wait0/PASS51.95s，真实Runner批准及丢响应原lookup、拒绝零Attempt、单Agent取消后B继续及Summaryv3，晚approve拒绝、原lease/effect和App/Guard完整退出全部通过。未派发预留Operation保持原M2 created/v1/零Attempt，旧测试误期望和timeout失败不回写。完整控制、恢复与来源入口仍待。
- Model长期serving retention的真实受控调用证明Provider换凭据/删除后旧snapshot仍使用原凭据、两个generation独立引用，填充数据升级和COMMIT未转发/ACK丢失两种真实Unknown观察有限通过；业务generation权威由该fixture提供，不替代Knowledge/Memory实际接入。
- MCP Resource文本必须在正式Object canonical保存后经原权限读取、限长消费和Close，再向当前text tools profile投影正文；当前MCPFile媒体Ref本身不能被该profile直接消费。79351原Wait0已验证actualPut/Read/Close、canonical文本进入后继模型及原runtime/Secret/owner完整退出；原生媒体消费仍未完成，不能把Ref当正文。
- 动态Skills正常首链已由`m3_model`完成，现由`m3_memory_tools`在专用树接续Agent assignment：复用原install账本，接list/read/assign/prepare及下一轮immutable输入；integration保留公共闭集/Composite/App。已有底层包不代实际入口，相关Runner按本卡边界补齐。DOCX有界实现已有限定测试，PDF有界Poppler进程组件及实际限额/取消Wait已定向通过，已绑定Index及App显式配置，四格式OwnerHTTP与后继Agent引用/退出已过，四格式浏览器尚未通过。

OAuth Secret56569原写入及两个真实Unknown分支通过，有界独审无该范围must-fix；不代OAuth协议。MCP89142真实Owner Connection、SDK发现、Registry Tool/Resource目录、lookup、启停/断开和跨Project拒绝已通过，原资源全部退出；该轮Tool调用0，不代Agent Tool/Resource消费。

共享SQL故障已由00123实际PG门修复；00125原生产ToolCall名称约束的合法/拒绝输入及完整迁移也已通过，旧失败保持。MCP原AgentRun Secret审计及Object维护路由已由79351完整Agent首链消费：远端echo一次、Resource canonical保存与读取关闭、后继正文消费和所有租约/owner退出均通过。Owner凭据真实HTTP CRUD/CAS、wrong-purpose、撤权零新写、丢失响应只读lookup及限定独审已通过；off/direct-text 控制面已实际验证且工具保持禁用；Owner 浏览器的凭据原 lookup、SDK 目录、启停/断开/删除及完整退出通过，ResourceLink 也已实际调用官方 SDK、canonical Object Read/Close 并被后继模型消费。OAuth 正常 domain 首链已通过真实 DCR/S256 授权码一次交换、原命令回查、flow Secret 退场、token 封存后的 SDK 发现及原 App Join；83507 原 Wait0/PASS7.20s，四组 owned 资源完整退出。后继82194原Wait0/PASS8.20s实际Owner HTTP从oauth配置、begin及原回查，到同Session/CSRF complete及回查、授权码第二key拒绝、官方SDK发现与App Join；原四组owned完整退出。OAuth UI 的真实 Session/router/transport 组合 98 项及类型检查已过；浏览器、刷新、冷恢复与媒体仍未完成。

动态Skills77051原Wait0：实际原Install账本安装→Owner显式分配→旧轮list不含新增→下一轮控制消息/list生效→Object正文Read/Close→第五Model消费通过，4个immutable skill input/1个control及reader leases全部释放。Agent assign 原 self 场景 83706 整 top 已通过：原 source/Audit、四引用全部 v2、旧 captured 保持、下一轮 SkillsInput/list、Object 正文/Close、模型消费与全部 owner/App/Guard 退出；旧 text/UUID 观测 SQL 错误保留。catalog/其他目标首次真实调用已到目标 canonical read、十次 Model/八次 Tool、两 E 成功和 App/peer Join，末观察 SQL UUID/text 错误使整 top 失败；修正观察后待复验，权限撤销仍待。Runner prepare 的本地受控 HTTP、安全发布与原 run-command 脚本已过，正式 Central Transfer/session/mount/App 与 Install Unknown 仍未完成。下一步继续 Knowledge 四格式用户入口、MCP OAuth/media/恢复、Meeting 完整控制与必要领域 Tool/Inbox，复用已通过的 Model/Memory 停止恢复。作者自测不代独立接受，正常首链不代本卡完整出口；风险结论见[独立报告](m3-core-independent-review.md)。

Summary snapshot 后继实际门已通过第一 generation 的格式失败保 pin、跨 selector 原 snapshot retry/lookup、发布与 pin 释放；第二 generation 因 harness 误选 ToolCalls profile 在发送前失败，整 top 未通过。后继第三个 text-only 模型候选在创建模型 HTTP400 前置失败，尚未进入本轮 Summary；作者继续核 fixture 输入，原七回复与预算保持。MCP 新装配的 source/cleanup/startup 普通组合通过；媒体首 native 在单 WireSequence 前置被拒后已修，尚未到 Model/Tool 媒体行为。OAuth refresh 初组仅普通 source/version/issuer/Unknown 门通过，真实旋转、冷恢复与浏览器仍待。

## 原 Model owner 停止后的有界恢复

原 `Model.Runtime.Initialize` 未退休 call 阻塞现已有受控恢复实现：ready之前核真实旧进程停止、原Call/Attempt/Usage/Secret及borrower精确事实，未确认仍拒绝启动。authorized/sent 两分支已分别实际 kill/restart 通过，原 Call/Attempt、诚实 Unknown、精确 borrower 关闭及零重发保持；已终态 call 的 borrower 尾巴和 Memory ModelCallOwner lease 门也分别通过。pre-admission无call borrower已96718原Wait0通过真实停止、原事实保持、零重发和新App Join。81711物理Unknown整top原Wait0/PASS38.99s，commit实际提交后仅原plan确认；rollback保留原Unknown，第三明确新App才清理，全部原owner与fixture完整退出。旧失败保持。StoppedResolution 三态在固定新 binary 上由未参与实现者实际运行通过，原三 fixture 与进程完整退出；先前双 Spool/opaque equality 的 harness 失败不回填。`ModelCallLeaseRetirement` 仍要求原 call 已退休。Memory10221 已退休窗口继续有效；新增93314 authorized 与58830 Resolve 已提交但 Begin 未入场窗口均实际通过旧 child SIGKILL/Wait、新 guard、原 Model/Usage/Secret 和 Memory 映射退休、原 retain canonical 失败、原 Tool 失败被 Agent 消费及完整 App 退出，零模型重发/新 Memory。独审发现三处后续观察失败覆盖原写 Unknown，已保留原 cause 并通过九个读失败/取消/observer Unknown 定向回归和源码复核，不将该普通门称物理 Unknown 验证。共同 App 已组合这些提供方与调用方并通过受影响普通门；Knowledge、Meeting 及其它恢复仍以各自真实业务窗口为界。

最小工程范围是提供原 call 的只读观察与停止后退休能力。请求必须绑定原 Consumer、Project/Agent、Operation、CallID、snapshot/lease owner/lease ID、完整 AttemptIdentity（InvocationID、index、ProcessID、fence）和 Input digest；Model 从自己持久记录重建并核对，领域通过原恢复私有 witness 证明该业务 owner，不能由裸 DTO 获权。观察不改变账本；退休沿原 Usage/Secret/ProjectCall 事实与完整预发现锁集合，提交未知只观察原目标，不重放外发。已有完整业务候选才允许领域重新发布，否则保持明确失败/未知，后继新逻辑重试遵循原产品操作。

停止证明使用当前真实 `ProcessGuard.ConfirmStopped(oldProcess)`：核原 claim、相同可信 host/deployment/spool 和文件 dev/inode，非阻塞取得原 claim 的精确 flock；当前进程、仍持锁、不同宿主或无法验证的 claim 均拒绝。不能以 TTL、空本地 map 或数据库 stopped 字段替代。恢复端口需能在普通 Runtime admission 之前运行，而不放宽现有 Initialize 拒绝门。验证只针对原活进程拒绝、真实旧进程 Wait 后恢复、未知提交两分支及零新外发，逐条区分平台退休与领域重新发布，不要求无依据重跑已过正常链。
