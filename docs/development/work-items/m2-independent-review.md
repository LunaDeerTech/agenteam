# M2 组合独立审查

2026-10-14 用户已恢复 M2；M1 真实场景已完成并归入 `main 20aded69`，不再以旧暂停或 DNS 未验作为当前状态。本角色继续独立验证，仅写本报告；其他协调记录由其 owner 更新。当前结论：M2 本轮约定范围已独立接受，见[最终结论](#最终-m2-约定范围接受)；root 已确认产品与正式文档推送 main，任务 Git 收尾已完成。

审查角色未参与产品或 harness 实现。主组合结论绑定 `14252bc8..aa9128d07eb963bd722f5b496b8b62131ba53027`，使用冻结提交读取；复用此前 `45fe6e11` Artifact 领域静审。`da380` 冷恢复、`eec19674` 分表断言和 `5b821bff..d877bd2b35fa2a2790d067246713943d936043cf` 修复差量另行限定范围，不代表最终组合已验证；后续活动源码不在结论内。前述为历史阶段结论，按各冻结输入保留；本轮最终接受范围见文末，不代表未覆盖能力；实际 Git 交付状态见 [current](../../../.agent-state/current.md)。

## 发现与处置

1. **P1 原合法拒绝后继阻断已修；合法提交及最小非法配对反例均通过真实 PG。** 原 `internal/central/execution/multi_round_rejection.go:97` 起写入拒绝 transcript、`loop_approval_results` 和后继轮；但 `00091_execution_multi_round.sql:102` 的后继轮检查及 `:126` 的 transcript 检查只读取 `loop_tool_results`，`00096` 未扩展这两个分支。因此原合法零 Attempt 拒绝会在提交时触发配对错误，不能继续下一 Model 轮；这是静态可确定的原产品阻断，没有冒称已实际 PG 复现。`d877bd2b` 的新 `00098` 已覆盖两个分支，独立静核未发现新确定问题，详见修复差量；后续 `cdd15280` 真实双场景已通过合法拒绝后继提交，关闭该正向阻断；随后 `22112` 的错 approval ID 反例在原 deferred guard 被拒并完整回滚，最小防误配门已验证；不泛称全部非法字段组合或恢复已覆盖。
2. **P2 原拒绝场景错误结果表断言已修，并获真实双场景验证。** 原 `internal/central/app/m2_approval_workflow_test.go:177` 统计 `loop_tool_results`，`:187` 对批准、拒绝均要求一行；实际拒绝契约使用 `loop_approval_results` 且零 Attempt。限定复核 `eec19674` 该文件差量，现分别要求批准 1 条 tool result / 0 条 approval result，拒绝 0 条 tool result / 1 条 approval result，并配对原审批、Operation、fingerprint 与 denial；零 Attempt、零 report 和实际文件无副作用断言保留。该 P2 源码修复随后在 `cdd15280` 真实批准/拒绝场景通过；同提交其他新增文件不因该复核获得接受。

P1 修后必要真实门通过前不作正式接受；可以继续冻结组合首链联调。首 App native 在 Start 原事务发生 `FORBIDDEN/not_committed` 的信息来自 W2/root，独立审查尚未消费最终日志、原 Wait 与资源退出事实，故不将该轮计为已通过，也不以静态修复回填该轮结果。

## 已核对的安全与事务边界

- `tool/runtime/builtin_authority.go`、`builtin_resolution.go`、`execution/tool_call_authority.go`：私有 witness 限定原 executor/ProcessGuard、原持久 Operation/Attempt、完整 ToolCall binding 和原输入字节；真实执行在原事务内重验 Project、当前 Agent/ToolSet/Policy。Object 内部部分 subject 的非空来源仍须精确相等，缺省值来自该原调用，不由模型 DTO 补造。
- 历史结果查询使用独立 committed-input loan；`checkBuiltinTask` 拒绝其 Task 写意图，Artifact gate 拒绝其非 Read 意图。`Lookup` 不调用 Execute；只在原 domain 已实际返回且观察到精确完整 terminal 后退休保留 owner。该结论不包含尚未纳入的冷恢复增量。
- `execution/cancellation*.go`、`work/task_transition_cancellation.go`：Human 取消必须消费原取消调用或 Work 同事务取消 postimage；Task 保护、取消请求和原 receipt 同事务写入，只有已确认提交后通知原 driver。取消请求不是终态，原 Model/Tool 的 join 门继续适用。
- `governance/approval` 与 `tool/runtime/approval_operation.go`：原 Operation/fingerprint 预留；等待、决定要求 created/version 1、无 active Attempt 且历史 Attempt 为零；批准的最终 Claim 仍经过当前工具授权。命令重放返回原 receipt，查询不触发调度；拒绝终态不伪造 Runner report。
- `00092..00097` 为新增前向迁移。`00095` 把完整结果、原 Attempt 退休与 Operation 终态约束在同事务；`00097` 保留 pending Unknown 的原身份/结果，只查证原 Launch，并禁止增加发送次数。没有运行迁移或据此宣称真实 PG 通过。
- Artifact 投影复用此前已审的业务引用/正文边界，不把 StoredObject ID、storage key 或下载 URL交给模型。Human 现有审核入口可以承担首个交付出口，无须把自主 reviewer 全链作为其前置。

## 最小后续验收

1. 保存修复组合后定向审 `00098` 与拒绝断言，要求普通结果和零 Attempt 拒绝结果互斥、身份/序列/正文摘要/原 Operation 及已拒绝审批精确配对；真实 approve/reject App 两场景分别证明原 E/Operation、批准只一次实际副作用、拒绝零副作用、receipt 重放/查询及原资源退出。
2. 用真实 App/PG/MinIO 的 Artifact 发布、列举、读取与 Human 审核通过/退回证明用户交接结果；针对真实 provider 补最小来源错配/当前撤权反例，不能用受控 authority 单测替代。
3. 取消至少消费一条原运行中 Model/Tool 的真实取消与 join 证据，以及 Task pending Unknown 查证、不重发的真实持久证据；核取消请求/保护/receipt 同事务、原资源退出前无 Execution 终态、无新增副作用。
4. 相同输入的作者基础检查可复用。冷恢复、原生迁移、权限竞争、浏览器完整行为、旧 profile 回归尚无本审查的实际通过结论；按最终受影响范围选择必要补验，不重跑无关全矩阵。

## 冷恢复增量静审

额外读取冻结提交 `da380841f2675ee13ccf699e0c327eda341ef61d` 的恢复增量，没有对活动源码作结论，也未运行恢复测试。未发现新的可确定 P0/P1；这不是冷恢复正式接受。

- `execution_recovery.go:405` 起只恢复已持久化的 ToolCall checkpoint，拒绝当前进程丢失指针；要求原 snapshot Process 停止，并检查同一 direct invocation 的完整 ProjectCall ledger。当前进程旧记录还须 `business_closed` 且本地 owner 已实际退出，单凭数据库阶段不构成 join。
- `execution_recovery_repository.go:203` 的读取限定原 project/lane/identity/digest 全集；`:221` 起在完整锁内重读 ledger、Execution、最新 round、原 preparation input 和 waiting 关联。ledger 变化、输入或 checkpoint 不一致均拒绝恢复。
- `execution_recovery.go:502` 起恢复 running 只 Lookup 原 ToolCall，不重发 Execute；waiting 沿原审批门继续。私有冷恢复证明只代替已完成原 Model session 的丢失指针，取消仍经精确 ToolCall 的 Cancel/Join，不能把取消信号当作退休证明。
- controls 已报告的 late-cancel 缓存问题在此原输入中尚未修：原 associated job 返回并缓存错误后，新持久取消可能不再唤醒。后续 `d877bd2b` 修复的静态结论单列如下；真实旧 Process 停止后的恢复与当前 owner 竞争证据仍待补。不得仅凭上述静审或原普通流程通过宣称恢复已完成。

## d877bd2b 修复差量静审

限定检查 `5b821bff..d877bd2b35fa2a2790d067246713943d936043cf` 的 8 个变更路径及必要既有依赖，未运行 Go 或原生栈。未发现新增确定 P0/P1，可继续组合联调；作者定向 `70722` Wait 0 仅作为其报告，不替代本角色独验或真实 PG。

- `00098_execution_approval_provenance.sql:7` 新函数要求原 Execution/Round/ToolCall/invocation/spec/Operation/fingerprint、已拒绝 Governance request 与 denial 全配对；Operation 必须 failed/version 2、无 active 或历史 Attempt，错误正文必须与正式拒绝结果一致。拒绝记录、transcript、后继 round 和 running Execution checkpoint 同事务，普通结果与拒绝结果互斥。`:52` 起扩展原两个分支并给拒绝表增加 deferred trigger，普通结果其余检查保持。与真实 writer 的字段、序列、版本、时间及 JSON 形状核对未见不可达约束；迁移和实际提交尚未验证。
- `tool_call.go:186` 与 `tool_call_authority.go:203` 将 Core 与 ordinary allow list 分开，同时两类仍受 Denied 限制；调用入口先从原持久捕获集合精确匹配 tool/spec/model name/binding，再经原 Registry 比较当前 ref/class/binding/scope/risk 及原捕获引用。模型或公共 Actor 不能仅自称 Core 获得权限，ordinary 撤权仍拒绝。这与首 Start 阶段的 Core 装配冲突相符，但修后真实 Start 仍待新一轮证明。
- `associated_executor.go:180`、`:210`、`:514` 与 `cancellation_owner.go:191` 把已确认持久取消通知转为私有重访提示；无 retained 子 owner 的旧缓存 job 只能重新核 canonical pre-start cancellation，直接返回，不进入普通 inspect/Prepare/Start。证明暂不可得时保留提示，Unknown 仍保留原恢复 owner。新增 5 个定向子例的断言覆盖 created/preparing、无持久请求、阻塞后再观察及原调用期间通知；受控 fixture 不证明真实 ProjectCall/PG 生命周期。
- 最小实际门仍为修后同组合 approve/reject：批准只一次原副作用；拒绝零 Attempt/report/文件副作用且真实后继轮提交；再选一个错 request/call 或已有 Attempt 的拒绝反例验证新 guard。取消需补真实原调用返回后的 canonical 重访，以及旧进程退场/竞争和 Unknown 不重发证据。无需扩大为无关全矩阵。

以上前轮仅只读静审，未执行 Go/native 或旧 STOP；其范围和未验事实保留。用户恢复后的独立差量如下。


## 恢复后的 00099 与 waiting/retained 差量

冻结 Approval `f1c9a78b`、Controls `f6c4c342`。静审未发现新增确定 P0/P1，当前两修片可进入组合联调；不把这一结论写成真实 PG 或完整 M2 接受。

- `00099_governance_outbox_id_pairing.sql` 与原 `00094` 的 `check_publication` 正文程序比较仅增加六处 `::uuid`，从受 canonical UUIDv7 text 域约束的 Governance ID 转为 Outbox UUID 比较。原事件类型/producer/aggregate/version/project、grant 及拒绝零 Attempt 配对不变；原迁移未回写。该静审当时尚缺 Ensure/resolve deferred 提交及 `00098` 拒绝后继配对的真实 PG；下述双场景现已补齐合法路径。
- `awaitApprovalDecision` 在原 callback 内仅轮询 canonical Pending；每次 Resolve 自己的原事务返回后才等待，循环不持跨轮 SQL/业务锁。原 request ID、形状验证、真错误及取消仍返回；批准/拒绝沿原 E/Operation 继续，未加重发入口。临时恢复 caller 取消不直接取消 retained run，原 run 的取消仍进入正式未派发终止流程。
- ProjectCall 只为真实 retained direct/preparation/parent owner 保留原取消句柄；后续借用释放临时 context。父 context、lane/global/project Stop 均可触达原句柄；Stop 在原锁下收集当前及 retained cancel，锁外调用，重复取消幂等。退休仍受原 closed/absent/NotCommitted、实际返回及 child outcome/Join 门约束，才移除 owner、释放句柄；未把 callback returned 当作业务退休。

独立原 session `90524` **Wait/exit 0**：在冻结 controls 树，Go 1.27.1、`GOMAXPROCS=2`、`-race -p=2 -mod=readonly -count=1`，仅 `./internal/central/execution -run '^(TestExecutionProjectCallRetained.*|TestMultiRoundApprovalWait.*)$'`，5 顶层/10 子例通过、0 fail/skip，包耗时 1.554s。证据 `output/ai/m2-independent/waiting-retained-race.jsonl` 与同目录命令记录。覆盖 pending 保持原 callback 到批准/拒绝、原读错误/错引用/原或临时取消、重复恢复借用/退休、父/lane/project/global 取消；使用受控记录，**不证明真实 PG 冷恢复或实际进程 Join**。本次未启 native，未读 KEY/DNS 或发外部模型请求。

作者完整 Execution `10688` Wait 1 的旧 fixture 两项失败（AssociatedRunLifetime nil-store recovery 读、RetirementDeleteFailures 表序）保留，待原 owner 定向修复；新组通过不回填其全包结果，作者首 fixture digest 前缀错误 FAIL 同样保留。前两次 App `79719` Start FORBIDDEN/零 Model/Wait 1，以及 `91225` Ensure 回滚、retained COMMIT_UNKNOWN/timeout Wait 2、拒绝未运行继续未通过；超时后资源退出不是正常 App Join。

## 当前最小组合出口

沿 [M2 工作卡](m2-task-collaboration.md#首条真实链与验收路径)完成 Artifact 原成果→Human 实际读取→退回→同 Task 新 Execution 修订→再读接受；配适用的当前撤权/来源错配零写反例。Approval 实际 waiting、批准一次副作用、拒绝零 Attempt/副作用且后继提交、等待取消；Execution/Task 分别持久取消、pending Unknown 原 Launch 查证不重发；原进程已实际 Wait 退出后的原 Operation 恢复/竞争与副作用不重发，仍属待验门。M1 真实模型成功可复用其未变能力，但 controlled M2 只证明自身真实领域组合；最终外部 M2 场景由 delivery 组织，不将其预写成通过，也不要求自主 reviewer 决策作为 Human 审核前置。

至少一条 Task 协作须让 Scheduler 持续正常运行。当前 `task_transition_authority.go:190` 允许同 Task 唯一 active owner 的 Human review edge，但 transfer 自身不自动取消该 review；不能把 Task 到 todo/done 当作旧 Execution 已退休。需观察原 review 实际退出/Join 与占用释放后再启动修订 worker，严格区分 work/review 归属并核所有原 owner；不得靠关闭 Scheduler、提前清空占用或忽略旧审核者的后续写入规避竞争。成功 receipt/版本与评论、合法 assignee、原成果版本保持等正式门继续有效。


## 真实 Approval 双场景与退休适配差量

固定 `cdd15280` 的已有 app.test，实际入口精确选择 `TestM2ApprovalWorkflow`；证据在 review 树 `output/ai/m2-app-integration/approval.CgLQmg/`。原 session `90958` Wait 0，`native.log` 记录原测试 `588620` actual_wait code 0，approve 7.06s、reject 6.99s，总 14.05s。独立读取冻结 harness、原日志及 owned/cleanup 记录，不重复起栈；比对该组合 `00099`/`00098` 与已审输入相同。

两场景均从真实 App/Owner/Scheduler/PG/Runner 进入原 waiting E，只有一个原 created/version 1 Operation、零 Attempt 和零实际文件副作用。HTTP 决定绑定同 request/E/Operation/fingerprint/Owner；原命令重放和 lookup 返回同 receipt，换命令旧版本与同 key 换意图均拒绝。批准后原 Operation 只有 1 Attempt/1 command report/1 tool result/1 grant，真实文件内容严格一次；拒绝后 0 Attempt/report/tool result/文件副作用，1 denial/1 approval result，精确 pairing 且原 E 的后继 Model 轮和终态实际提交。决定、command receipt、Outbox resolved event 的 xmin 相同，原 pending Model/ToolCall 参数字节保持。**00099 合法发布提交、00098 合法零 Attempt 拒绝后继及 P2 分表验收门据此关闭。**

原 Runtime/App owners 与 peer 连接的 Join 断言到达；Runner `588780`、`589017` 原 Wait 0，原测试 Wait 0，三层 nonce cleanup 齐。独立逐个 inspect 3 containers/3 networks 均明确不存在，3 临时目录与上述 3 原 PID 均不存在。此处使用自有受控 Provider，证明真实领域/事务/Runner 组合，不是新增外部模型实证；不覆盖等待中取消、冷恢复、非法配对反例、Artifact/Human 首链或 M2 整体。

另静核 `f329d227` 三路径差量：退休 before-image 由过时 17 字段适配实际 20 字段，三项 waiting 字段全部存在且必须满足既有 `projectTaskWaiting` 闭集；非 waiting 必须全 null，waiting 必须 approval/合法 request ID/不早于 started_at。完整原始行仍参与 digest/再次比较，unsupported Tool 历史、原 ownership/Join 和退休写入范围未放宽。测试补真实 Store fixture 与既定删除表序，保每表 short-delete/缺字段/多字段/旧 waiting 事实拒绝。静审无新增确定 must-fix；复用作者 `28089` 完整 Execution 回归 Wait 0，不冒称独立重跑；`10688` 原 FAIL 保留。Task 首次 `82370` 的 op=0 新断点仍由 delivery 定位，本结论不推断其根因。


## 运行中持久取消与非法审批配对：限定接受

固定 `8d0c7cc7` binary，review 证据 `output/ai/m2-app-integration/cancellation.KID4fc/`，原 session `22112` Wait 0、测试 `596821` actual_wait code 0。独立读取该冻结 `m2_execution_controls_workflow_test.go`、实际日志与 owned 记录；未读取活动新增 waiting/cold 源码作为结论，未重跑 native。

真实 Execution/Task 取消两子例共 12.74s：原 command 已运行且属于 running 未退休 Attempt，经真实 Human HTTP 后保持原 E/Operation、ToolCall/参数字节及单次 Model 请求。Execution cancel 原 receipt、重放/lookup一致，Task 进入 blocked，精确 user_cancelled_execution blocker 保留 Owner/reason/原 E；cancel command、Task、blocker 的 xmin 相同。整个 Task transfer 取消则进入 cancelled、不制造上述 blocker，原 transfer receipt 重放/lookup不变，并持久记录原 E cancel_requested。两例都在 Runtime.Stop 清理之前观察到 cancelled 终态、原 child pidfd 返回且 child/group 不存在、lease 退休、无后继调用，再执行原 App/Runner/peer Join。这关闭运行中命令的两种持久取消语义，不代替 waiting、重启不复活或 pending Unknown。

`TestM2ApprovalRejectsUnpairedResult` 6.93s：向原真实 pending round 插入一条错 approval ID 的候选结果，确认 INSERT 到达一行，提交精确返回 `23514 / invalid loop approval result pair`，回滚后结果行数为零；随后原 Owner 合法 reject 仍按既有同 E/Operation 路径完成。该最小真实 PG 反例与此前合法拒绝后继组合，关闭原 P1 修复所需的最小正/反配对门。

Runner `596962`、`597175`、`597466` 原 Wait 0、原测试 Wait 0及三层 nonce cleanup 齐。独立逐个 inspect 3 containers/3 networks 均不存在，3 临时目录、4 原 PID 均不存在。采用受控 Provider，不产生外部模型实证；后继新增 waiting/cold 稿和整体 M2 保持未验。

## 五项 Core Builtin 当前注册门：限定接受

固定三文件已保存于 review `e4a0026b`：`current_builtin.go`、`current_builtin_test.go`、`builtin_executor.go`。新 `RequireCurrentWorkArtifactBuiltinInTx` 仅接受既有五项 binding/revision 1、实际 Core class及原 spec/binding/scope/risk 全 tuple；复用 currentCaptureTool 的当前数据库注册、actual Source/Active、canonical definition 与真实 backend binding 检查，原 Store/Tx 和 Registry/Tool SH 锁要求不变。原 Ordinary API 保持拒 Core，未把 Agent allowlist 或任意 public Actor 当作执行许可。

Runtime 仅替换原受保护输入事务中的 metadata gate；当前 Execution 私有输入 loan、ProcessGuard、精确 binding、lookup 的 committed-only loan 及后续领域当前授权均保留。静审无新增确定 must-fix，可继续冻结组合联调。独立原 session `4061` Wait 0：Go 1.27.1、`-race -json -p=2 -mod=readonly -count=1`，仅 Registry/Runtime 的 `TestRegistryCurrent(Builtin|WorkArtifact).*` 与 `TestBuiltin.*`，10 顶层/18 子例通过、零 fail/skip，两包分别 1.027s/1.051s。覆盖五项真实 Core 正例、旧 Ordinary 仍拒 Core、13 项当前注册/来源/tuple/Tx/锁反例，以及原私有 loan、lookup 禁止 mutation、原 Join/结果身份等受影响 Runtime 门；日志为 review `output/ai/m2-independent/core-builtin-race.jsonl` 与命令记录。未编译 App、未起 native 或读取外部凭据。


## Task Artifact 与 Human HTTP：受控组合接受

固定 `30844bcd` / `build.o76Hwg` 的既有 binary，review 证据 `output/ai/m2-app-integration/task-human.tEUTa0/`；原 session `75611` Wait 0，accept 8.46s、return-same-task-and-accept 13.73s，总 22.19s。独立核冻结断言、实际日志和精确资源，不重跑 native。真实 Core Builtin 经过原权限、Tool Operation、Artifact/Object 发布；Owner HTTP 按 source Execution/Operation/Agent 与 ArtifactRef 查询并读取实际完整内容。原 E 成功仍为 Task in_progress，显式送审和接受才到 done；退回保持同 Task，Scheduler 启动新 E，产生新 Artifact，旧成果仍可读，版本/同 key receipt 重放与 Human 事件历史均验证。

原 Runtime/App/peer Join 断言到达；Runner `604166`、`604462` 原 Wait 0、test `604026` actual_wait code 0，三层 nonce cleanup 齐。独立逐个 inspect 3 containers/3 networks 明确不存在，3 临时目录、3 原 PID 均不存在。该场景使用预设受控 Provider，且审核阶段显式暂停 Scheduler；只关闭领域/事务/Owner HTTP 业务门，**不等于浏览器、持续 Scheduler 协作、真实外部 M2 或整体 M2 出口**。原 `82370`/`54436` 失败保持原结论，新通过不回填。

## M2 live 入口：最小静态准备门

限定 `30844bcd` 新 live/shared factory/browser/run.sh 及必要未变依赖，未运行 live 或读取 KEY。未见新增确定 must-fix，可按既定顺序先验证受控组合和新增高风险场景，再由 delivery 启动显式授权的真实场景；这里不是外部验收通过。

M2 case 与 opt-in 双门先于外部 metadata/资格读取，复用原 exact-origin proxy 路由、SystemResolver/direct policy、CA/TLS 和 qualified formal Secret 写入；缺配置不回退预设响应或 synthetic Secret。Runner 环境为独立白名单，浏览器删除 DEV_CHAT；真实 KEY 不进入其 material 或日志。原 300s 绝对总期限、60s request、4096 output 及 project sent12 软观察预算沿原已验 owner，含 work/review/retry；触发后原 Stop/Drain/Join，不能把软阈值称硬计费上限。浏览器和后续材料保持同一绝对 deadline，不按步骤续期。

模型输出与 Artifact 正文不预设：独立从真实 Runner 输入计算统计、核原 read-file 结果和外部 Model 选定的 create-artifact 参数/原 Operation/ToolResult/后继历史；修订须在同 Task 的新 work E 捕获原 Human 评论并生成新成果，旧成果不可覆盖。live 分支不关闭 Scheduler，最终分别归属 work/review、核无 active/unknown/pending/unretired 原 owner，再确认真实 Join；review 仅接收通知，不能称自主审核决策。实际连续调度、浏览器退回/读取/接受及所有原资源退场仍待本轮 live 事实，未替代后继 waiting/cold/pending Unknown 门。


## 冷恢复与浏览器首轮：分场景接受

固定 `30844bcd` binary，review `recovery.yqtgeM` 原 session `54107` Wait 1，整轮保持 FAIL。独立核冻结源码和原日志：`running-without-result-never-redispatches` 9.98s 通过；原审批已提交而尚无 Attempt 的真实 child 经 SIGKILL/原 Wait 后，新 ProcessGuard 至少完成四次原 Tool 读取，仍同 E/Operation/fingerprint、0 Attempt/report/文件副作用、原 pending 字节不变，保持未退休。该故意未解 owner 最后也以真实 kill/Wait 退出，不能称优雅退休。

`TestM2AppExecutionCompletedToolColdRecovery` 7.66s 通过；原真实 command 一次完成且报告已提交、尚未消费为后继时 kill/Wait，重启只 Lookup 原结果，Operation/Attempt/report 全行字节与原 pending 不变，真实独占创建文件内容严格一次；原 E 后继与终态成功、lease 退休，新 App 和 Runner 原 Join/Wait 门到达。两成功子例可复用；waiting-cancel 的 child ready 前退出、waiting-cold 的批准后 failed/retired Attempt 无 report 继续 FAIL，不能推为整个恢复出口通过。

独立复查该轮 6 个 owned 容器/网络、3 目录及记录内 5 PID 全部不存在，三层 nonce cleanup 齐；旧 child 的 kill/Wait 由实际通过源码断言支持，但精确 child PID 未写入现有清单，**不宣称已逐 PID 复查所有 child**。后续只补可恢复的原事实，不猜测 PID，也不为这项重复已通过业务场景。

同输入 `browser.tfTqp2` 原 `68867` Wait 1：accept 17.82s 限定接受，真实原成果读取、Owner 一次确认 done、刷新仍可读，Node `610747`、Runner `610647` 原 Wait 0及 App/peer Join；12 条同请求证明全部为 requestfinished，native EOF/零 cancel、公有 consumer fulfilled、Go EOF/Close/handler 返回与正文长度/摘要一致。return-and-accept 的 Node `611228` 本身 Wait 0、完成读/退回/新成果/接受，18 条联合完整证明中 3 条 ERR_ABORTED 同样满足上述完整组合；但 Go 最后的旧 projection 对比失败，**该子例和整轮仍 FAIL**，不能仅按浏览器成功接受。

该失败与冻结代码 `firstView := w.view` 共享指针、随后向 `w.view` Unmarshal 复用指针相符。新 `e7be2e82` 仅将首次 projection JSON 深拷贝，保留原完整 DeepEqual 与旧成果检查；静审无 must-fix，待只重验 return 新事实。该轮 6 资源/3 目录/记录内 5 PID/2 临时依赖链接已独立确认不存在。以上仍属受控 Provider，未补持续 Scheduler 或真实 M2 live 门。


## 浏览器 return 受影响复验：限定接受

`d41f708f` / `build.D3ZAZQ` 的既有 binary，review `browser-return.Vm5r3N`，原 session `81046` Wait 0、18.93s；独立核原日志、18 条同 RequestID 联合完整响应证明、冻结断言与精确资源。全部证明为 requestfinished，原生 EOF/零 cancel、公有 consumer fulfilled、Go EOF/Close/handler 返回及长度/摘要均一致；6 阶段、2 次 Owner 写入、3 次真实 Artifact 读取完成，退回同 Task 后生成新 E/新 Artifact，刷新后 done 与成果保持。深拷贝后的首 E、次 E 原完整 projection DeepEqual 均实际通过。与前轮 accept 成功证据组合，受控浏览器 Human 接受及退回修订门限定接受；旧 `68867` return/整轮 FAIL 不回填。

原 Node `616360`、Runner `616265`、test `616122` Wait 0与原 App/peer Join 门齐，三层 cleanup 齐；独立逐个确认 6 resources、3 目录、3 PID 不存在。前轮 cleanup 记录两临时依赖链接已移除；独验时下一 live 已在运行、同路径链接重新出现，故仅引用原轮清理记录，不将下一轮链接当作旧残留或删除。该受控场景仍在审核期间暂停 Scheduler，不冒充正运行的真实外部 M2 场景。


## 55ad17de 最小修片与首轮 M2 live 边界

只读核 `55ad17de` 冻结输入，未跑 Go/native。pending Unknown 的刺激来自原 Launch writer backend PID 上的完整 COMMIT 帧：代理关闭原 client、原 Store 自行返回 Unknown，wrapper 原样返回该 CommitResult；随后只释放原 COMMIT 至真实 PG，核 COMMIT/Ready 及原连接 Join，并以独立连接读取原 Created Execution。Owner Task 取消、pending reservation 与原 E 取消须同事务，原 Visitor 仅 Lookup Found、一次 Launch/Attempt/Claim、再次访问无额外查询/发送，零 preparation/Model/Tool。未构造 Unknown 或 Launch receipt；该测试只覆盖 Found，实际 PG 结果仍待本轮。

waiting-cancel 新刺激通过同一真实 App 已装配 executor 的第二次 `Advance`，从真实 Scheduler dispatch 读取完整关联，验证 Active/非 Joined 及原 associated/direct 两条 ProjectCall ID 不变；称同进程原 owner 复用，不称跨进程竞争。原失败第二 App 因共享 spool 独占锁未进入业务范围，不能拿其退出证明竞争恢复。取消后的 cold 路径仍要求原 child 实际 SIGKILL/Wait、guard 释放、新 ProcessGuard 与旧 cancelled checkpoint 不复活；新日志只输出已闭合状态/错误码、数字 PID/Wait，原 Store CommitResult 不被观察器改写。当前未新增确定 must-fix，待真实 waiting 与 pending 三场景。

真实外部首轮 `29440 / live.YFB7ig` 原 Wait 1、61.95s，**整轮 FAIL**。两个 work 各 3 Model 轮/sent 3/retry 0、真实 Runner 输入与动态 Artifact/原 Operation 及独立统计 oracle 通过；浏览器 6 阶段、2 次 Human 写、3 次成果读取通过，Node `619079` Wait 0。18 条同请求完整证明含 2 条 ERR_ABORTED，均满足原 native EOF/零 cancel、公有 consumer fulfilled、Go 完整返回/长度/摘要联合门；原首末 work projection 保真及 Task done/history 已通过。最后严格 review 终态门未过，尚无本轮 closed Model code，不把它猜成正常撤权或外部故障，也不降低 succeeded/cancelled 门。Runtime Drain 已到，正常最终 App/Runner Join 顺序未全到，后续 cleanup 不冒充正常成功。

原 test `618581` Wait 1；Runner `618689` 原 cleanup Wait 与三层 cleanup 齐。独立确认本轮 6 resources/3 目录/3 原 PID 不存在，原清理记录两链接已移除。已确定的静态输入冲突是 reviewer prompt 禁 Tool，而正式 JSONtools 首轮要求 required；获准的新输入应沿原真实精确 ArtifactRef 只读后 ack、Human 仍唯一决定。该下一输入尚未因此通过，预算、持续 Scheduler、原权限和全部 Join 门不放宽。

剩余功能门收敛为：pending Unknown 真实 Found 查证不重发；waiting 中取消及重启不复活；waiting 冷恢复批准后同原 Operation 继续；完整外部 M2 两 work 与 Human 浏览器审核、持续 Scheduler/review 及所有原 owner Join。此前成功范围按未变输入复用。原报告要求的新 Core/Artifact 当前撤权或来源错配零写最小安全反例正向 delivery 核已有证据；若边界相同则复用，确缺时只补一条领域组合拒绝，不增全矩阵。


## waiting 取消与重启：限定接受

固定 `55ad17de / build.Zmp98D`，review `pending-waiting.iaPxiA`，原 `91741` Wait 1，整轮仍 FAIL。其中 `TestM2ExecutionWaitingCancellationRecovery` 6.17s PASS 经独立冻结源码/原日志/资源核对可接受：原实际 App executor 第二次 Advance 保留同 associated/direct 两条 owner 和 active/非 Joined 状态、旧 guard 仍持有；真实 waiting 精确 request/E/Operation/原 pending 字节，Human E 取消后 Task 保护与 receipt、lease 退休成立，0 Attempt/grant/report/文件副作用、仅原 1 Model。

旧 child `628165` 原实际 Wait 为 SIGKILL/signal9；新 child `628267` 使用不同 ProcessGuard，实际读取已 cancelled checkpoint，不复活；晚 approve 返回精确 ConfirmationStale，原 pending 字节、零副作用保持，新 App SIGTERM 后原 Join/Wait 0，Runner 原 Join 断言通过。该门关闭 waiting 中持久取消和该取消终态的真实重启不复活；不声称跨进程竞争成功或未获结果调用已退休。

同轮 waiting-cold 6.51s FAIL，闭集诊断 `not_dispatched / attempted=false / report_available=false`，Runner 最后 disconnected、认证连接数1；不能据此判生产缺陷或宣布修复。pending Unknown 3.78s 已走到最后事实，但 launches=0、origins=1、live=1 与断言不符仍 FAIL，实际 owner 生命周期待原作者核，不能仅按前置 Lookup 成功关闭门。原 test `628022` Wait 1，四 child 的真实 start/Wait 均记录；独立检查 6 resources/3 目录/7 已记录 PID 全不存在，三层 cleanup 齐。原轮成功冷恢复证据不重跑，不用本轮新增 PID 反填旧缺记录。

剩余为 pending Unknown 完整门、waiting 冷恢复批准继续、完整真实 M2 live 与最终 Join，以及一条已约定的真实来源拒绝安全反例。该反例限定真实 A Task→Model→原 Core 私有输入/Adapter 携真实 B ArtifactRef，被原 ProjectRef/权限门拒绝且不给 B 正文/地址、B 内容和 metadata 不变，无 Artifact/Object 业务写或读取成功/下载授权；允许正式失败 Operation/Attempt/拒绝诊断记录。按实际拒绝层报告，不绕前置 gate 强迫进入领域内部、不称撤权或全部授权组合。


## pending、waiting 冷恢复与来源拒绝：三门接受

固定 `8f8128ea / build.vHytjS`，review `remaining.B8leZr` 原 session `93835` Wait 0，原 test `636173` actual_wait code 0。独立核原日志、冻结最终源、owned/cleanup；相对已审 `66368252` 只有三处 observer 参数取地址的编译修正，以下业务断言未减弱。

- 来源错配 6.62s PASS：真实 A Task/AgentRun 的原 Core ToolCall 携真实 B ArtifactRef，原 Adapter ProjectRef gate 返回精确 FORBIDDEN；失败 Operation/Attempt 退休及拒绝 ToolResult/后继消费完整，A/B 的 Artifact/Object/commands/uploads/attempts/references/leases/Artifact audit 全快照不变，合法 Owner 随后仍读到 B 原完整内容，原 Runtime/App/Runner/peer Join。该最小真实来源拒绝门关闭，拒绝发生在领域读取之前，不宣称撤权或领域内所有授权分支。
- waiting 冷恢复 8.12s PASS：旧 child `636570` 实际 SIGKILL/Wait，先观察新的断连事件，再启动不同 ProcessGuard 的 child `636647` 并观察新的 authenticated connected，才批准原 pending request；同 E/Operation/fingerprint 继续、一次 Attempt/report/独占文件副作用、原 pending 字节不变，后继及终态提交、lease 退休，新 child SIGTERM 原 Join/Wait 0。35s 屏障只等原 Runner 重连，不增加工具重发或放宽权限，原 Runner `636522` Join 门通过。
- pending Unknown 3.58s PASS：完整原 COMMIT 客户端响应物理丢失，原 Store Unknown 与 PG 独立已提交 Created 事实成立；Owner Task 取消/原 E 取消/pending reservation 同事务，原 Visitor 仅 Lookup Found，1 Launch/1 E/1 Claim、Dispatch attempt1，第二次访问不再查发。Lookup 后精确原 launch_once 技术托管仍为唯一 live1；原 App/全 owners Join 后同 ID business_closed、全部 live0且关联 Dispatch 原字节不变，preparation/Model/Tool 为0。Found 最小门关闭，NotFound/cold Unknown 不由此例代证。

原 Runner `636315` Wait 0、cold 原 child 两次 Wait、原 test Wait 0及三层 nonce cleanup 齐；独立核 owned 与 cleanup 精确一致，6 resources/3 目录/5 已记录 PID 全不存在。`54107`、`91741` 和更早失败均保留，已通过场景不重跑。至此既定受控业务、取消/审批、冷恢复、Unknown 与最小来源安全门已按限定范围满足；**完整真实 M2 live 与原最终 Join 仍待后续实际结果，当前不作 M2/main 正式接受。**


## 最终 live 前的诊断与失败保留

`8836 / live.Y0PFsa`（8f）Wait 1、55.67s：首 work3/review2 的真实 Model 与精确 Artifact 消费通过，浏览器在 Human return 前的 Execution 详情 GET 遇 502，只有3条成功响应 proof、closed=false；该次具体502根因仍未知。`bc957f3e` 仅添加固定 stage/status/错误类型与布尔诊断，原完整响应门未放宽，并修正不存在的诊断列。

`63389 / live-diagnostic.o3gaO6`（bc957）Wait 1、300.06s：两个 work 与首 review 成功，8 Model/5 Tool，第二 review 尚未创建而触发原 deadline。持久安全观察为 task in_review、relaunch remaining48/version204；正式配置按 eligible Task visit 递减而非秒。`ac52d083` 仅为 M2 live 显式配置40次，旧 factory 保持240，正常 Scheduler、300s/60s/4096/project sent12软预算均不变；不把聊天中的更早92观测当作已保存证据。

`11992 / live-review40.eDsq8e`（ac52）Wait 1、143.06s：两 work/两 review、10 sent、6 Tool、Human 全行为与原投影和原 owners/root Join 均到达，18浏览器联合完整证明通过；最后旧即时退出查询的 stopped/gone 至少一项为 false，但旧日志未区分，不能倒推哪一项。`e110faec` 仅在所有原 Join 与 Flock 取释后，为 M2 增加最多2s/20ms的精确 process/同DB只读观察，仍须 stopped=true且app_connections=0，原共享/M1 helper 不动。以上三轮 owned/cleanup 对应的6资源、3目录、3原PID均已独立确认不存在，原链接清理记录保留；后轮成功不回填任何一轮 FAIL，也不把该退出修片认作旧502根因。

## 最终 M2 约定范围接受

**独立结论：本轮约定 M2 产品与适用验证范围可正式接受，无剩余确定 must-fix。** 固定 `e110faeca0ef96d89d0f30c154c19ed582d89a46`，review `output/ai/m2-app-integration/live-final.FZ9v5n/`，原 session `3646` Wait 0、143.46s；独立复核冻结断言、原 native/browser 证据、binary provenance 与 owned/cleanup，未重跑模型或原生栈。

真实外部 Model 沿原 Secret、explicit-origin trusted proxy/CA/TLS，在始终启用的 Scheduler 下完成同 Task 两个 work E 与两个只读 review E；work 共6 sent、review共4 sent，10轮全成功/provider usage、无 retry，6个真实 Tool 结果。两次 worker 从真实 Runner 输入独立计算并发布动态 Artifact；reviewer 按原 Human 提交的精确业务引用读取同内容/source Operation，原 Model/ToolResult/后继消费完整；Human 浏览器退回同 Task、新 E 修订、新成果读取后明确接受 done，旧成果可读、Task历史与两个原 work 的完整 projection 保持。reviewer只读后 acknowledgement，**正式通过/退回决定仍由 Human 作出**。

浏览器6阶段、2次Human写入、3次真实Artifact读取，Node `659356` 原 Wait成功。18条同RequestID联合完整响应证明为17条requestfinished、1条ERR_ABORTED；该异常条亦具原native EOF/零cancel、公有consumer fulfilled、Go原EOF/Close/handler返回、相同正文长度/摘要与身份，故按既定完整证明门接受，**不写零浏览器异常**。

原 Runtime Drain、Runner `658917` Wait 0、App root实际返回err=nil及全部原 execution/scheduler/planning/assembly/producers Joined 门齐；原 guard Flock成功取释。最终退出初次观察即 stopped=true/app_connections=0，一次查询8ms完成，无须轮询；原 test `658791` actual_wait code0。独立比对 owned与cleanup清单一致并逐个确认6容器/网络、3临时目录、3原PID、2临时依赖链接均不存在，三层nonce cleanup齐。App binary字节/SHA与e110编译记录一致；复用Runner binary字节/SHA与原30844记录一致，Runner/protocol/platform/go.mod/go.sum至e110无差异。

组合此前已接受的真实批准/拒绝同E与Operation、00098合法/非法配对及00099提交、运行中Execution与Task两种持久取消、waiting取消重启不复活、waiting/已完成结果冷恢复与无结果不重发、pending Unknown Found实际COMMIT/Lookup与原托管Join、真实来源错配拒绝，满足本轮适用门。未覆盖的NotFound/cold Unknown、跨进程同时业务竞争、所有撤权组合及自主review决策不借此宣称通过；M3/M4未启动。本轮保持原STOP的具体范围，未运行旧Tools driver。

此前 Git 认证失败曾阻断交付；root 已恢复认证并确认 M2 产品与正式文档推送 main，相关任务工作树与引用已清理。此处仅更新 Git 交付状态，上述实际测试和独立接受结论保持；收尾结果及必要原件保存见 [current](../../../.agent-state/current.md)。


证据保全以主树 `output/ai/m2-delivery-evidence/manifest.json` 为现行清单。此前独核186个白名单文件（921476字节），本次仅追加两轮最早失败 `79719/run.vYLNvO` 与 `91225/run.oDBoMy` 的6份原件（13571字节）；现有192个原件共935047字节，含manifest共193文件。已独立逐项核对全部原件的源文件、staging与主树副本字节数/SHA256及完整文件集合，新增历史来源引用也与Git记录/工作卡原文哈希一致。首轮未补造缺失的精确cleanup文件，第二轮保留Wait2超时与未证明正常App Join的限制；两轮均仍为FAIL，不改变本轮接受结论。最终live仍映射为e110/3646/Wait0，并包含原native、browser完整证明、owned/cleanup与binary provenance；不复制binary、凭据或活动配置。manifest中元数据来源哈希仅表示读取时刻，不冒称之后的工作流文档未变。
