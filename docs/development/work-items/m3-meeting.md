# M3 Meeting

目标沿 [D24 Meeting 架构](../../architecture/meeting/README.md)：真实 Session、Turn、Contribution、Decision、Summary 与用户入口，Agent 沿原 Executor/Model/Tool 协作。M4 暂撤为当前里程碑；M3 后评估完善。本卡尚未完成。

## 完整范围差额（当前）

| 原 M3 范围 | 已实现/实际证据 | 仍需闭合 |
| --- | --- | --- |
| proposed / active / archive | Owner Start/归档恢复已验；47694真实Agent提议→Inbox→Owner HTTP接受/拒绝与原lookup、重建及恢复后发言完整通过 | 同一Inbox→Proposal→处理→返回浏览器；后继普通archive/restore canonical事件组合 |
| 参与者与引用 | 会中roster/typed references原事务、HTTP/UI与资源选择器已落；原普通门及前端定向检查通过 | 新PG/三Turn浏览器与真实跨域composite；File按D24纠正为稳定ArtifactID，内部FileID只属正文链，新形状待窄测/实际门 |
| 编排 | sequential 首真实 App；7298 新parallel/queue/interrupt真实App通过共同input、原取消屏障后插话优先、旧FIFO继续和完整退出；busy/skip已有领域受控验证 | busy/skip受影响的用户可见组合；不重跑不变通过门 |
| Decision / Approval | Decision与Approval原真实App/浏览器已过；58814另验证Decision四类事件/Inbox、answer/skip/cancel与原退出完整链；Approval含批准/原lookup、拒绝零派发、单A取消后B继续及晚批准拒绝 | 保持原权限/来源边界；22854另通过真实Realtime等待/回答同E链 |
| Contribution cancel / retry / regenerate | 单C取消真实Runner/浏览器已过；原lineage领域PG通过；90473真实Retry响应丢失查询、运行中反馈、成功后Regenerate、历史回复与下一Turn更新Summary完整浏览器通过 | 保留本次input_id/lineage判据边界，不外推逐字重比全部context或Regenerate Unknown |
| Summary / finalize | 首次标题+四字段与滚动摘要；82796固定snapshot/retry/取消退休；2805 prepared-beforeResolve实际崩溃后保原source、不重发、诚实失败并由Owner取消原Turn | 本冷门不覆盖已Resolve/dispatch窗口；受控Provider通过不代外部模型服务 |
| Timeline / 领域工具 | 稳定贡献项与当前来源；request-decision已验；四常规工具47694通过；58034真实同Meeting E的retain/recall/reflect、公开回复与Summary完整通过；90473已验历史generation网页 | update/comment语义待用户；22854已通过有限Realtime展开执行与已提交文本 |
| 生命周期与通知 | 原owners与队列重建；50965真实Owner删除、原调用退休、同Tx审计/Deleted、外域历史保留及迟到命令拒绝通过 | 删除浏览器；其它Unknown/冷恢复窗口不由当前链外推 |

以上属于原 M3 完整交付差额，不另缩减为普通文本 Summary 或单个正常链。Approval、固定 Summary snapshot/取消与134 prepared冷窗口实际链已闭合；Decision141四类事件/Inbox完整七回复、Realtime双topic22854及Replacement90473均完成各自持久事实与退出门；Reply53653原限定PG判据也已通过。下一参与者/引用及其它完整差额。

## 本轮剩余出口（有限清单）

以下按 D24 正式第一阶段边界一次列清，后继只关闭这些缺口，不扩展完整 D25 或另造执行、审批、文件与通知平台。

1. **已有用户链的实际验收**：90473固定App96226/dist15624/f3 CJS原Wait0/PASS35.77秒，已完成Retry响应丢失后的查询、可见运行状态、新attempt/Execution、成功后Regenerate新generation/input_id/lineage、历史回复与延迟到下一普通Turn更新Summary，八次Model及lease退休、App Joined和三fixture清理通过。四截图已view并经独立复核；本top核controls=2/lookups=1，不逐字比较Retry/Lookup key/body，也不重比完整context字节，原client/domain证据单独复用。40066/64308失败保留，Chrome子PID未逐捕。会中成员及Task/Knowledge/ArtifactID/Link引用原metadata旅程仍待；复用固定候选，不把deadline后completed当通过。
2. **原调用冷恢复**：2805固定61828已原Wait0/PASS9.76秒，prepared-beforeResolve真实SIGKILL/Wait→新Guard按原source/binding/process及正式absence观察收尾，selector Version=nil保持，零新Resolve/Model/lease或Summary输出，原generation诚实failed后Owner HTTP取消原Turn，新App正常Joined。6资源/3目录/父子3PID全部不存在。原8438观察SQL失败保留；本门不覆盖已Resolve/dispatch等其它窗口。缺历史source仍Unknown，不按当前selector补历史或自动重发。
3. **删除用户闭环**：50965固定61828的原四回复真实App门已Wait0/PASS7.81秒：Owner HTTP原命令/lookup、held A取消与未发B零派发、全部原调用与lease退休后本域purge、completed Audit/Deleted/receipt同Tx，保留三条外域Execution历史，迟到Send拒绝、原receipt replay与App/guard退出通过。原23514取消flag与LOCK_NOT_HELD锁预算失败保留，窄修已实际覆盖；不改已应用DDL。Owner确认/清理中及删除完成的浏览器仍待，不扩大Project全生命周期或外推旧Process恢复。
4. **正式工具与提议**：四常规工具/Proposal/Inbox原HTTP九回复链47694已完整通过，原App/peer/guard和夹具实际退出；同一Inbox→Proposal→处理→返回浏览器按原owner另记。update/comment两项只有用户尚未答复的产品语义缺口，不自行定义。58034已验证原Memory retain/recall/reflect在同Meeting Trigger/E下的完整调用、原provenance、Summary及退出，不新增三种Memory提议工具。
5. **本Meeting的canonical事件与人工待办**：Proposal与Deleted已有真实源；普通archive/unarchive同Tx Archived/Restored受控普通已过，实际组合另验。141 Decision四类事件/私有source/Inbox已组合，独立SQL10976通过。50907原失败定位第三cancel的optional typed零ID编码漏选Decision；最小修保显式scope和全部权限门，9414六scope/原live transition普通通过。修后58814固定40974原七回复90秒门Wait0/PASS13.38秒：answer/skip各原E与Turn完成，cancel原D/E/Turn取消且Inbox resolved；四类事件共6条（3requested+3closed），6条各与Outbox同xmin、3条终态各与原Decision同xmin，strict source_context、重建不重开与原App Joined通过，6IDs/3dirs/PID完整清退。原50907及22091失败保留，不把successor日志单独当根因，也不外推Realtime浏览器。本项只处理Meeting Decision，不复制Governance或聚合其它新业务域。
6. **内联内容第一阶段入口**：领域已保存并序列化text/mention/reference节点；新composer旅程由Tools作者另记实际范围，不能用Meeting Info picker替代。Reply53653原Wait0/PASS2.14秒已验证同key变更目标拒绝、跨Meeting目标零副写、历史immutable目标与重放保持；真实PG+显式Project/Agent doubles，不外推App权限。原6IDs/3dirs/PID清理并经独立复核。@参与者、#现有Task/Knowledge/Artifact资源、URL、上传插入及pin的网页证据按其原旅程记录；资源不可用仍沿正式资源权限查询。不做完整富文本、自动读取引用正文、自动抓链接或独立文件存储。
7. **实时与展开Execution视图**：22854固定40974 App、f6dc原dist和ba2 PW的三回复真实链已Wait0/PASS9.43秒：原Cookie/Origin、两topic ack/snapshot、同E Decision waiting→HTTP answer、已提交Tool/公开text及Timeline Summary1、原receipt/current tuple/3Model与lease退休、真实socket关闭和App Joined全部通过，6IDs/3dirs/进程完整清退；两桌面截图已view。5649关闭后观察与77567未选串行的harness失败保留；修正没有放宽Go或预算。仅正常连接的canonical状态与已提交内容，不含token增量，不外推实际断连/resync或完整D25流式能力。

## 已实现与接缝

Meeting 持有领域表、原子 Owner Start/Send、稳定 contribution generation/attempt、固定输入、launch intent 与 receipt；00103 初始 schema 和 00110 Summary call ownership 已保存。Trigger/Capture/ContextBuilder 与 AgentLoop Meeting 分支保留来源边界，顺序后继输入读取前 Agent 已发布消息，并行使用共同不可变输入。

Runtime 将真实运行委托给原 AssociatedExecutor.AdvanceMeeting；结果通过 Execution 原事务 reader 获取完整公开 terminal snapshot。Summary 读取全部当前消息，首轮标题加四字段、后续四字段；系统 selector、持久 consumer mapping、原 JSON call handle 与 lease retirement 已接。失败保持 finalizing，不发布 fallback。

Summary采用普通text模型响应并严格解析JSON，不要求json_schema capability；原技术清理actor绑定当前ProcessGuard、同Invocation和原snapshot/lease。历史intent Read只核不变源绑定，Launch仍需原command与当前来源权限。

List/GetDetail/Start/Send 是首个 HTTP/UI 合同。Execution control 新增源限定 slot/cancel 端口，Meeting 私有 witness、同 Store/Tx 和完整绑定校验；取消复用原 Execution owner。公共 App/identity/Model/Execution 装配由 integration，HTTP/UI 由 root 分配唯一写者。

## 常规工具与提议的独立实施边界

后继工具作者负责 `list-meetings`、`request-meeting`、`add-agent-to-meeting`、`remove-agent-from-meeting` 的真实 backend、领域命令与原操作查询；这四个 backend 和领域源已保存，现有 `request-decision` 保留原作者与权限链。`update-meeting`、`comment-meeting` 的可写对象在现设计中缺少定义，已向用户确认，未获答复前保持待定，不自行创造主题或 Agent MeetingMessage。`DomainBuiltinBinding` 的闭集声明及四 backend 普通测试不等于真实 App 已注册运行；`request-execution-approval` 仍沿 Governance，不另建审批状态机。

可隔离的新增文件为 `meeting/{agent_service,agent_commands,proposal,proposal_events}.go`、`meeting/contract/{agent_tools,proposal,events}.go`、`meeting/tools/agent_backends.go` 及对应测试。现有 `service.go`、Decision 文件及 HTTP/UI 不交叉写。工具作者负责 proposed 的创建与 Human 接受/拒绝领域方法；现有 Owner HTTP/UI 作者消费合同，继续 Summary、Retry、metadata 和其余生命周期。active/archive 控制已有实现和实际浏览器证据，按原行为复用。

普通工具使用独立 `AgentToolCall`（原 `tc.ToolCallBinding`、OperationID、AttemptID、RequestID）与 `MeetingAgentInvocationAuthority`，不扩展 Decision-only authority。最小端口为原 Store/Tx 绑定检查、`DiscoverMeetingTool(ctx, call, lookup)` 返回原调用锁集，以及 `WithMeetingToolInputInTx(ctx, tx, call, lookup, consume)` 同步借出原 protected input。原 Builtin owner 必须核实际私有 witness、actor/project/agent/execution、operation/attempt、handler/revision、当前 ProcessGuard 与 lookup 模式；DTO 本身不授权。Meeting 在完整锁联合下再核目标会话、参与者、当前版本及领域范围。写工具按原 operation/digest/receipt 幂等，查询只能观察原操作结果，不通过查询重发。

公共接线由 integration 协调：新增 `tool/runtime/builtin_meeting_authority.go` 沿 `BuiltinAuthority.witness/withCurrent`，不借 Decision 的等待入口；App 在 `account.go` 的现 `constructMeetingDecisionBackend`/`executionBuiltins.addBackends` 附近加入固定普通 backend，构造放 `m3_meeting.go`。typed Event 在原 `account.go` Catalog 与 producer map 注册，仍由同一 `outbox.New` 管理。新作者先给精确 App hunks，不能自行并写装配。

前向迁移 00136、immutable source Execution/最多一个 origin、原 Agent operation/digest/receipt 映射及 ProposalService 已由工具作者实现，并随47694真实App链实际使用。00103 已有 `proposed/active/archive` 和 `proposal_content`，不再创建 MeetingProposal 对象；现 Human `commands` 使用 UserID，不能伪造 Human Actor 复用。proposed 不创建正常 Turn；Owner 接受为 active、拒绝为 archive，两支均保证当前 Owner 的 Human participant 存在但不产生 Turn/Message，原创建 Agent 和来源不变。Owner Proposal HTTP handler及App挂载已实际完成；私有界面定向测试/类型检查通过，但完整浏览器旅程仍待验。

现 Owner Task/Knowledge/Artifact reference adapters 均仅限 Human/current Owner、同 Project identity；Agent 工具不能伪造 Owner 或直接读外域私表绕过。提议的原 source Execution/Task 从原执行调用证明派生；显式 typed references 若对 Agent 开放，须另有对应域正式 Agent 授权端口。Human picker 接通不能替代 Agent 权限。

本卡的 Memory proposal 指 [D14 §18–19](../../architecture/knowledge-memory/agent-memory-runtime.md#18-consolidation-安全约束) 中 retain 内部模型 mutation proposal，经原 namespace/input targets/revision 校验后原子 apply、Unknown 查原事实；[§31](../../architecture/knowledge-memory/agent-memory-runtime.md#31-approval--destructive) 不将内部 action 变成独立 Agent Tool。D24 未定义额外 Human Memory 提案决议，也不自动 recall Memory，不新增 propose/accept/reject 三工具。58034固定12030的真实App场景已原Wait0/PASS10.90秒：Owner HTTP创建的单一Meeting Agent沿原E依次retain→recall→reflect→final及Summary，4 Agent回复、3 Memory chat、3 Embed与1 Summary共11次Model调用全部退休和lease关闭；核原Trigger、零Task、canonical revision/provenance与完整App/guard退出。三official cleanup和6资源/3目录/原PID逐项不存在。原22398非法fixture Name前置失败保留；本门使用受控Provider，不代外部模型服务。

Human Inbox 的 [正式设计](../../architecture/platform-infrastructure/human-inbox.md) 已有最小 source provider、projector、projection schema 与 HTTP 源码检查点。canonical Proposed/Activated/Archived 事件沿原 typed Catalog/Outbox 私有 same-Tx producer，projection 以 `source_type=meeting/source_id=meeting_id/action_type=meeting.review_proposal` 单向引用，Meeting 不保存反向 Inbox ID。47694原Wait0/PASS9.13秒完整实际通过四backend的五次调用、两份提议原source/creator、canonical Inbox、Owner真实HTTP批准/拒绝及原lookup/replay、重建后handled不重开、拒绝→恢复→Send/Summary、后继Model消费精确五个原Tool receipts。原App/九次受控peer/guard均实际退出，六资源ID、三目录和App PID精确清理；前两次失败保留。Inbox Web组合49项定向检查和fulltsc已过，Vite/浏览器仍待；普通ControlEvents、137删除、Decision/Approval新source和RuntimeView不属于该固定输入，不外推。

## 验证及剩余

Go 1.27.1 的 Meeting/contract/AgentLoop 指定基础测试通过，最近 session16555 Wait0；这覆盖严格 Summary 形状、对话投影和输入校验，以及当前详情编译。control组后继 session41682 Wait0，取消原绑定/终态不写窄测通过。领域 PostgreSQL51677及14214 Wait0：完整111迁移、Start/Send/receipt、不可变消息、顺序/并行输入、两个parallel分支真实同时进入受控端口、queued取消、archive恢复、summary失败保持finalizing及显式retry。原fixture cleanup均完成。这些领域测试的 Execution/Summary 使用明确受控双，不代表真实 App 链。

真实顺序 A→B→Summary 已通过，下一步优先闭合用户可见 Decision 等待、回答、跳过和取消，再覆盖 parallel/queue/interrupt/busy/skip、Decision/Approval 原 E 恢复、retry/regenerate、summary finalizing 失败与重试、取消/重启、Memory proposal、工具与用户入口。Runtime.Run已加入有界后台visit与Stop/Join；Summary补原handle恢复与retirement Unknown只lookup。冷running无原handle仍保留Unknown，真实Model故障矩阵及事件尚未闭合。完整 M3 Meeting 门槛不以类型、单库或 WIP checkpoint 替代。

真实 App 顺序链已通过：`TestMeetingM3AppSequentialCollaboration`（62671 原 Wait0，12.95 秒）使用原 owned PG/MinIO/TLS、Owner Start、后台 Runtime、Executor、Model 和 Secret；仅 Provider 响应由确定性 peer 控制。B 的实际请求包含 A 的已发布贡献，Summary 的实际请求包含 A+B；2 个独立成功 Execution、3 条消息和首版标题/Summary 持久化。3 个 Model 调用及 Secret 租约全部终结，原 App/ProcessGuard/owners 正常 joined，原 fixture 精确 cleanup 完成。普通 text Summary 模型未要求 json_schema；Meeting 首轮 auto 沿原真实来源门生效，Task 保留 required。该结果不外推外部模型服务、故障恢复矩阵或完整 M3。

领域结果发布以正式私有 same-Tx ResultReader 的持久终态为准，保留原 Advance 错误；相关 4 分支 PostgreSQL 测试通过并完成 cleanup。HTTP/UI 另树 Owner browser 已通过 list/start/detail/send/lookup/reload，范围仅入口与 busy fixture。冷 Summary running 缺正式旧 Process 停止和原 Model 终态证明，仍保留 Unknown，不能重发；同 generation retry 的固定 snapshot 保留也未闭合。

Decision 已沿原 Builtin invocation 接通创建/等待/人工回答或跳过与原 Tool Result 消费接缝，HTTP/UI 控件已实现待组合。正式结果 schema 与固定 App 编译通过；首次真实场景暴露贡献误写 waiting，修复按原架构保持 Contribution running、只改变 Execution waiting。该轮 answer 失败后原 guard 未 joined，测试按 6 分钟总预算退出并完成夹具精确清理；skip/cancel 尚未运行，不能标记 Decision 链通过。

Decision 回答、跳过已分别通过真实 App：原 E waiting → 人工响应与原 lookup → 同 E successor，原 Tool/Decision 结果持久配对、最终贡献和 Summary 发布，原 App/owners/peer 正常退出。取消在固定 UI 候选中独立通过：原 E/Decision 取消，迟到回答拒绝，零结果消费和零 Summary；Model/Secret、原 App/owners/peer/flock 与数据库退出事实完整，原 fixture 精确清理。模型 Provider 回复为受控 peer，实际 Executor/Model/Secret 为原 App。复用未变的回答/跳过证据，没有重跑旧正常链。

原测试失败保留：贡献状态与结果 SQL 类型问题已修正；错误的 text[] 观测、单响应夹具和取消诊断策略已修正。旧退出合并断言无法区分停止事实与数据库连接哪项未满足；后继分项观测首样即全部满足，不据此倒推旧根因。一次环境恢复丢失的原 Wait 仍按缺失保留，精确恢复清理不代替原退出证据。

用户可见三 Turn Decision/controls 真实浏览器通过（21386 原 Wait0）：回答、提交响应丢失后的原查询、跳过、取消、归档恢复/reload；实际原 Execution 与结果消费、Summary、Model/Secret、App/owners/peer 退出均闭合，三 fixture 精确清理。桌面浅色、390px 深色、键盘焦点与无横向溢出通过并审查截图。固定 UI/schema119，Provider 为受控 peer；后继单 Contribution 取消意图与 Approval 入口仍在实施。

targeted Contribution Execution 读取、HTTP/API/session 与 Meeting 中立审批组件已实现：原完整锁集、Owner 和 current association 同事务授权，显示 Execution waiting/approval reference，沿统一 Governance API 决定，不复制审批状态机或虚构 Task。HTTP 定向测试、前端审批确认/Unknown 查询/来源变化检查及类型检查已通过。

00126 单贡献取消已真实 apply：新 Human 意图持久绑定 current g/a/E，后台仅取消原 Execution，其他 Agent 继续；旧回执重放不再取消。62173 原 Wait0 的两个真实 PostgreSQL 场景通过当前 Execution 精确读取、Owner/来源拒绝、领域 Runtime 重建后恢复单 A 取消、B 完成和 Summary，原 test 与三 helper 精确退出清理。其 Executor/Summary 为明确受控端口，尚不代表真实 Runner/Approval。

当前 UI 已接126取消标志与单 Agent 确认/禁审批，24个定向前端测试及类型检查通过。新真实浏览器限定同Meeting、两Agent、三Turn：批准及丢失响应的原查询、拒绝零Attempt、仅取消A后B继续；原Runner/Executor/Model与11个受控Provider回复。固定App/Runner/完整dist及Config均已通过；首轮11365原Wait1在模型前遇preparation LOCK_NOT_HELD，未进入Approval，原进程/三个helper已精确退出清理。已定位共享准备事务缺Mount要求的Schedule锁，由integration修复，不外推审批链通过。原Approval仅pending/approved/rejected；取消后的pending历史请求必须失去操作资格，不新增状态。整M3未完成。

贡献 Retry/Regenerate 首域组通过轻包测试和33446真实PostgreSQL场景（Execution/Summary明确替身）：同Contribution的新generation/attempt与新Execution lineage、原控制回执、Runtime重建、历史消息保留、失败不替换旧消息、成功后只标记旧Summary待更新，以及下一普通Turn finalize更新均已验证。无新增DDL；历史Turn保持completed、标题不改、其他Agent不级联。后继仍需历史版本详情/控件和实际App验证，不以此代完整Retry/Regenerate验收。

Summary固定generation snapshot及pin、有限重试、发布/终失败/取消释放运行组已实现，取消仅收尾原调用；warm Unknown只读原resolution，cold必须取得原停止/退休事实，无可确认事实不重发。普通包、领域integration-tag编译及固定App race/Config通过。连续100–129首实际生命周期门15885原Wait1（10.95秒）：两Agent完成后，首Summary pin事务触发Model表 `serving_snapshot_retentions_check1` SQL23514，尚未派发Summary；由Model owner核原匿名约束与127扩展并前向修复。原test实际退出及三helper exactcleanup齐。固定snapshot重试、pin取消释放和冷恢复仍未接受，不能以此失败轮外推通过。

Approval浏览器此前23788整top仍FAIL，但真实三Turn批准+原lookup、拒绝、单A取消→B继续及Summaryv3已到达，11个Model请求完成、原Node成功。末Go门误要求尚未派发的Tool reservation为cancelled；原M2明确保created/version1/noactive及pending历史Approval，依此仅修harness并加强零Attempt/grant/result检查，生产不改。原test/Runner及三fixture已退出清理；lateapprove/lease/effect/AppJoined后续门未跑，须修正候选完整复验。

Approval完整浏览器现已通过：86470原Wait0，`TestMeetingM3ApprovalOwnerWeb` PASS51.95秒。固定诊断App63871、普通tracked CJS、原Runner93337/dist35565，同Meeting两Agent三Turn覆盖批准+已提交响应丢失原lookup、拒绝零派发、单A取消后B继续和Summaryv3。原E/Operation/Approval同源、M2未派发created/v1/noactive与零Attempt/command/grant/result、晚approve拒绝、11次Model/lease/Runner副作用及App/owners Joined全门通过。Node/Runner/test原Wait和三helper exactcleanup齐，退出首样stopped/database_gone/0连接；桌面浅色、390px深色/键盘/无溢出截图已审。Provider受控，不外推外部模型。4928首pending超时未复现、89170未知根因及其它原FAIL仍保留，本次不倒推旧结论。

Summary新1–133候选79385原Wait1、FAIL17.78秒：首generation固定snapshot retry与发布释放实际通过，第二Turn的两Agent完成后Summary尚未发第7请求即failed，因此取消/末退出门未接受。三helper精确清理齐。静态证据显示fixture将selector改为ToolCalls=true的Agent模型，当前正式Summary profile只支持受控text-only形状；当轮未记录具体error_code。仅修soleharness选第三个合法text-only Model，保持模型/selector改变与新旧generation边界，并补安全closed counts诊断；不改生产能力、原失败不重记成功。

第三text模型fixture候选77829编译/Config通过，61523原Wait1在模型创建HTTP400 INVALID_ARGUMENT退出（6.90秒、Provider0请求），原进程和三helper均精确清理。根因是fixture把HTTP input转成领域ModelInput再编码，丢失HTTP要求显式null的context_length。后继只保原wire并替换name，独立检查同实际POST的构造/编码入口；selector更新与snapshot观测字段同步静态核对。后继77711固定race编译和12810同binary Config/实际POST Wire检查通过；不改变生产模型profile、七次回复或总预算。

Summary完整后继实际门82796原Wait0：`TestMeetingM3AppSummarySnapshotLifecycle` PASS18.27秒，固定a5候选/连续1–133/原7reply。首g非法摘要失败后显式retry和原lookup保持同snapshot及原selector版本，新g采用新合法text-only模型；原sent call取消后退休、generation pin全部释放，原App/owners正常Wait。退出首样stopped/database_gone/0连接，actual test1436625 code0，PG/outbound/Object三helper exactcleanup齐。原79385/61523失败如上保留；本次受控Provider实际链不代外部模型服务、134冷恢复或137删除。

原D24§7.2插话优先的后继两源已落，未编译/实际验证：原Runtime取消后只按queued创建顺序会让旧队首先行，现仅优先原cancelled Turn的interrupted_by_id指向且仍queued的目标，其余FIFO，仍等待原取消流程完成。sole App新增八回复parallel/queue/interrupt场景，验证并发原Model请求、共同不可变input、旧queued未派发、插话先行后继续队列、原Execution/Model/租约及Join；不改已通过七回复Summary场景，不以静态修正宣称原完整编排已验收。

提案可见闭环的下一最小组合：现有Meeting详情按原owned read获取Proposal，并核同Project/Meeting/version；独立“会议提案”区展示理由、发起Agent和来源，接受/拒绝归档确认调用原ProposalService HTTP，Unknown仅用原Meeting session保存的key/payload lookup。返回入口使用本项目规范`/inbox`路径，不增加query或外部redirect。已有MeetingView/composable与私有API/session由Meeting作者接入，sharedclient三endpoint及Inbox页由tools作者提供；同一真实Agent创建提案→Inbox→详情处理→返回列表状态的浏览器旅程由tools作者实施。当前只有隔离私有四叶静态源和HTTP来源，未组合/未验网页，不把Inbox链接当完整Owner审阅。

新parallel/queue/interrupt实际门7298原Wait0：`TestMeetingM3AppParallelQueueInterrupt` PASS19.28秒，固定a728/34205及1–133。两真实Agent请求同时held且共同input；旧queued不派发，原2Execution取消后优先插话，随后旧FIFO继续。最终2取消+4成功Execution、8Model及lease全部退休、2Summary；App/owners Joined、guard首样stopped/database_gone/0。原test1569834 code0及PG/outbound/Object三helper exactcleanup齐，证据`/tmp/m3-meeting-parallel.NoIUYi`。Provider受控，不代134冷恢复、137删除或网页新旅程。
