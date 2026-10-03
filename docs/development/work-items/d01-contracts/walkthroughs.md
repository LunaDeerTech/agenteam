# D01 接口走查与后续验收输入

本文件用于核对提供方、消费方对同一个接口的理解。它是静态设计走查清单，**不是产品测试通过记录**；当前全部正式端口未实现，Go、数据库、Provider、Runner、MCP、浏览器及故障恢复行为检查均未执行。完整验收及提交由 [D01 主规格](../d01-cross-module-contracts.md)的 V01/I01 负责。

## 范围对照

| D01 必须交付 | 契约位置 | 走查编号 |
| --- | --- | --- |
| 职责、运行调用与允许代码依赖、真实绑定责任 | [索引](README.md) | W01、W02 |
| ID/时间/错误/cursor/version/request_id/幂等 | [基础](foundation.md) | W03–W08 |
| User/System/Owner/快照与当前权限 | [基础](foundation.md)、[Model/Tool](model-tool.md) | W04、W05、W20、W39 |
| 关联身份、状态枚举、原子转换、锁/删除矩阵 | [领域](domain-lifecycle.md) | W09–W16、W41–W43 |
| Agent slot、Task active、pending Dispatch | [领域](domain-lifecycle.md)、[编排](execution-orchestration.md) | W09–W12、W28–W30 |
| Model Request/Stream/Invocation 唯一重试层 | [Model/Tool](model-tool.md) | W17–W19 |
| ToolSpec/Binding/Result/Authorization/等待/unknown | [Model/Tool](model-tool.md)、[编排](execution-orchestration.md) | W20–W25 |
| Trigger/Launch/Resume/Cancel、Meeting 四项身份 | [编排](execution-orchestration.md) | W09、W23–W27、W31 |
| Project Skills 安装/分配/下一轮固定绑定 | [资源](resources-skills.md)、[编排](execution-orchestration.md) | W32–W35 |
| TaskEvent/Domain Event/Audit/Transcript/Runtime/Timeline/Inbox 事实分工 | [事实与实时](runtime-events.md) | W02、W26、W36–W40 |
| Runtime item seq/更新进度、未 flush 竞态、窗口/重启 | [事实与实时](runtime-events.md) | W36–W38 |
| Scheduler 按原 Launch key 只读核对 | [编排](execution-orchestration.md) | W28–W30 |
| Meeting 摘要实际有序输入与历史替换 | [编排](execution-orchestration.md) | W31、W43 |
| Knowledge/Memory/Object/Runner/MCP 必需边界 | [资源](resources-skills.md)、[Model/Tool](model-tool.md) | W13–W16、W21、W34、W39–W42 |

## 基础、状态与资源

下列 `P1/P2、U1/U2、A1/A2` 是测试 fixture 的不同稳定合法 UUIDv7，不是产品示例账号或生产数据。所有场景在当前 Actor/Project gate 检查后执行，除非该场景专门验证拒绝路径。并发测试使用明确 barrier 控制两事务先后，不以 sleep 碰概率。

| 编号 / 责任 | 输入和交错 | 必须观察的结果 |
| --- | --- | --- |
| W01 / D02–D28 | 组合根缺 WorkOccupancy、ToolAuthorization、Skill 初始化或 Runtime sink 任一必需 adapter | 相应能力明确 DEPENDENCY_UNBOUND；不能无占用/allow/空技能成功，不能完整 ready；导入图无实现包互引，Runner 不 import Central |
| W02 / D06/D22/D25 | 有 Runtime 文本却无合法 checkpoint；或有 Outbox payload 却 canonical 来源已删 | 不用 UI/日志/事件替代恢复事实；恢复错误可追溯；投影不会重建已删来源 |
| W03 / D02/D26 | int64 version 超过 JS safe integer；时间微秒；非法/重复 JSON key | JSON version 以十进制字符串无损；UTC 时间精度一致；非法输入在 mutation 前拒绝，request_id 每次不同 |
| W04 / D07/D08/D25 | U2/系统管理员请求 U1 的 P1/Execution/Inbox；伪造 project_id、Owner、AgentRun | Owner/System scope 分离；不可见资源不泄露；订阅与动作均拒绝，不因登录或管理员角色放行 |
| W05 / D07/各域 | U1 成功写 key K 后撤销 Session，重放 K；新有效 Session 重放同输入；再用 K 改正文 | 旧 Session 在读取幂等结果前拒绝；新合法 Session 返回原结果；同 key 不同语义冲突，不因 version 已变重新写 |
| W06 / D03/各域 | Task version=5；事务成功到6但响应丢失；同 K、expected=5 重试；另命令使用旧version | 原 K 重放成功先于 version 检查；新命令 VERSION_CONFLICT；TaskEvent/Outbox 各一次；提交未知不宣称回滚 |
| W07 / D11/D26 | cursor 用 P1/filter X 获得后改 P2/filter Y；rank rebalance；普通 title edit 同时提交 | scope/filter 错拒绝；rank cursor stale；Task.version/updated_at 不因纯 rebalance 变化；普通字段更新不回写旧 rank |
| W08 / D03/D04/D09/D18/D20/D22 | lifecycle/Launch/Complete/审批争锁；preparing 预收集后模型或 Tool/MCP 引用改变；捕获中途失败 | 按 system-config→Project→Agent→已排序 aggregate 加锁重读；变化则整体回滚重收集，不逆序补锁；Model/Tool/MCP/credential InTx 同外层 Tx 保存 metadata/ref，无嵌套 Tx/网络/SDK，提交后才外部准备；unknown 保留原 cause/幂等事实身份 |
| W09 / D11/D22/D24 | 同 A1 Task Launch 与 Meeting Launch 并发；胜者进入 waiting 再 cancel accepted | 恰一 created Execution；另一 AGENT_BUSY；waiting/cancel未完成仍占 slot；同 Launch key replay 返回原 ID不重新争槽 |
| W10 / D11/D22/D23 | Task Launch/Dispatch 创建与 Current Sprint Complete/Task Move 并发 | 共享 project-schedule lock；先创建占用则 Complete/Move拒绝，先完成/移出则新启动按当前合法性拒绝；无phantom |
| W11 / D11 | 绕过 review 请求 in_progress→done；blocked无blocker；terminal编辑；complete带unfinished但无target | 全部领域拒绝；合法review带comment/reviewer同事务；rollover保留state与相对顺序，不自动Start目标 |
| W12 / D11/D22/D23 | backlog有历史Execution/Dispatch但当前inactive；请求删除 | 按 history 查询拒绝，不因当前active列表为空删除；领域状态/历史保留不被级联破坏 |
| W13 / D05/D10 | 包/object写成功但发布事务失败；重试原 key；引用仍被Execution持有时删payload | 查询/恢复原发布，至多一revision；不暴露未发布包；活动/历史保护引用阻止物理删除；不把对象成功当安装成功 |
| W14 / D12/D13 | Knowledge正文v5→v6尚未索引；目录move；delete确认后插入子节点或移动节点 | read=v6、query不返回v5；move不改content version/索引；范围改变确认stale，全部tombstone原子且旧索引立即不serving |
| W15 / D14 | 同一合法Agent namespace 的retain与Owner删除输入Memory并发 | 整批已知未提交冲突，重读active数据并重算一次；第二冲突ToolError，无partial/静默覆盖；不使用同Agent两activeExecution非法fixture |
| W16 / D14/D09 | Memory commit成功响应丢失；或raw retain含Secret | unknown沿原retain查询，不重算第二批；敏感输入先allow/mask/reject再模型/embedding/lexical；所有新Provider attempt独立计量 |

## 模型、工具、等待与恢复

| 编号 / 责任 | 输入和交错 | 必须观察的结果 |
| --- | --- | --- |
| W17 / D09/D22 | Agent逻辑调用attempt1产生文字和半ToolCall后断流，attempt2成功 | 每attempt独立Invocation/usage；先aborted再新基线；不拼接两次文字、半call不执行；Loop无叠加同请求retry |
| W18 / D09/D19/D24 | Provider持续timeout；Agent cancel；自动审批达到有限请求期限；Summary有限retry耗尽 | Agent单请求期限渐进但不变Execution总期限；cancel禁止下次attempt；审批转持久人工等待；Summary仍finalizing，不套Agent无限策略 |
| W19 / D09/D22 | SDK有隐藏retry或Provider失败仍报usage；EOF无terminal | conformance必须发现/关闭或显式计量SDK真实attempt；失败usage保留、unknown不填0；EOF不能当成功 |
| W20 / D18/D19 | Core Tool越项目、被Policy限制的Tool已有Approval、模型伪造scope/reusable | 基础授权拒绝先于审批；Approval不能扩权；scope由真实调用Resolver产生；Backend未执行 |
| W21 / D18–D20 | 非幂等Runner/MCP操作发出后断连，原one-time已批准；Registry更新schema/Config删除 | unknown不自动重发，批准不改变副作用规则；旧ToolSpec/binding不热换；MCP旧合法lease可继续、新lease拒绝 |
| W22 / D16/D18 | 支持timeout工具等待人工审批很久、批准后执行、technical retry；另MCP有原生timeout字段 | 人工等待不耗原生执行期限；首次Backend时开始且retry不刷新；MCP字段原样保留；未指定不补全局deadline |
| W23 / D19/D22/D24 | Approval/Decision resolve先提交并通知，Execution尚未RegisterWait | 早通知可not_registered；登记Tx读已持久决议并一次消费，不永久waiting；request/Operation/Execution关联一致 |
| W24 / D19/D22/D24 | 先RegisterWait，再resolve；同通知重复/乱序；另reference误传；多工具需要审批 | matching resolve使同Execution继续、ControlInput/Transcript一次；旧/错reference不覆盖；批次保留队列、至多一个待消费WaitRef |
| W25 / D08/D19/D22/D24/D25 | 审批批准与cancel/Project归档/Meeting删除竞争；归档后迟到Model/Tool结果及投递/投影写入 | 同gate/locks重验，cancel先赢不恢复；批准先赢也不能越过后续取消启动工作；pending request按cancelled/invalidated收敛，无expired；archived拒绝业务mutation/Launch/Resume/Model/Tool，但限定Service cause可提交delivery marker、Runtime最终投影、Inbox/Timeline canonical收敛，不复活执行或改业务内容 |
| W26 / D22 | waiting后释放内存/重启；checkpoint不兼容；工具unknown位于不安全位置 | 合法checkpoint+源WaitFact继续同Execution并占slot；损坏明确recovery failure；不从RuntimeView复原Loop、不重跑未知副作用 |
| W27 / D22/D24 | Meeting四项ID分别跨Project、错Turn/Participant、Participant Agent≠目标；Preparing前删除Turn | 指定错误拒绝；Executor只调Provider不查Meeting表；已持久input恢复不漂移；取消先提交不进入running |

## 编排与技能

| 编号 / 责任 | 输入和交错 | 必须观察的结果 |
| --- | --- | --- |
| W28 / D22/D23 | Launch已提交响应丢失；随后查询DB暂不可用/暂查不到；retry计数已用尽 | 原Dispatch/key/pending/额度预占保留；仅LookupLaunch，无新Execution或替换Dispatch；known结果恢复后原子关联 |
| W29 / D11/D22/D23 | pending unknown时Task改done/cancelled/assignee、换Sprint或无CurrentSprint；暂停Scheduler | 原pending仍被核对，不能丢弃/改key；pause无新Launch/Task mutation；关联后按最新Task事实取消不再适用执行 |
| W30 / D11/D22/D23 | AgentBusy在claim后发生，同时用户改assignee/title且rank rebalance；另pending关联与waiting转换并发 | skipped无技术blocker；只补偿仍适用claim，保留用户编辑与逻辑位置；pending→launched不双计，waiting不占项目额度但占Agent槽 |
| W31 / D22/D24 | parallel发言启动间有人替换历史message/改references；sequential前序正式回复完成 | parallel共用预先持久input_ref，所有身份/summary/reference一致；sequential新input含前序正式回复；历史关联可安全展示 |
| W32 / D08/D10/D21 | 项目初始化缺Skill服务；上传同名包；安装后分配失败；显式禁用Add Skills/install工具再保存/复制 | 初始化明确失败；create冲突/update需目标版本；安装不自动分配；分别返回结果；禁用不会被初始化/模板恢复开启 |
| W33 / D10/D22 | 无既有binding时首次分配与Launch/preparing/下轮竞争；A移除后同Skill重分配B；通知丢失/乱序重复 | 同Agent gate下进入initial或带受保护captured引用的durable pending；Skill校验不要求先有正式binding；确定点按最高有效sequence形成唯一skill_id→binding，B替代A，旧/重复change不覆盖；当前轮及旧Snapshot/RoundInput/Transcript/引用不变，旧assignment不恢复权限 |
| W34 / D05/D10/D17/D21/D22 | Skill库v1更新v2；旧Execution已绑v1，新Execution启动；多个Runner准备；删除或断连 | 旧版绑定/当前授权仍合法，新Execution解析v2；读/prepare同一固定版；完整manifest后原子ready，断连不假成功，无默认Central中转 |
| W35 / D10/D19/D22 | management目录有权但自身未分配正文；waiting新增技能；ToolSet缺read/prepare；压缩/重启 | 管理目录不授正文；新增不解等待、不扩工具；缺能力ToolError；初始+applied记录可重建绑定，不能只靠Prompt摘要 |

## 实时、删除与摘要

| 编号 / 责任 | 输入和交错 | 必须观察的结果 |
| --- | --- | --- |
| W36 / D22/D25 | DB快照到38，coordinator已发布text delta到41未flush；订阅ack=41；Snapshot屏障S=45，缓冲42–46 | 返回实际内容包括≤45的全部窗口变化，丢≤45缓冲后只应用46；不能返回DB38加水位45；订阅前39–41不丢 |
| W37 / D22/D25/D26 | item_seq=2条目在update_seq=90完成；重复delta、乱序、offset错误；过滤掉其他item；tail翻页 | 低item_seq的新更新照常应用；重复不追加、缺口resync；窗口外progress_only不伪造正文同步；历史页不冒充完整live基线 |
| W38 / D22/D25 | flush乱序完成、终态主记录/最终文本并发读取、Central在未flush时崩溃、慢consumer | 旧flush不能覆盖新；terminal_complete只随完整最终snapshot；新epoch拒绝旧帧，丢失partial明确中断；buffer有界且不阻塞业务 |
| W39 / D07/D17/D25 | 登录WS后退出/改密撤销，现有Tunnel长连接；资源删除通知丢失 | 后续订阅/HTTP/upgrade拒绝，已有连接关闭；再读/重连仍正确401/404，不依赖通知送达、不留长期授权副本 |
| W40 / D06/D24/D25 | handler晚注册与生产提交交错；rebuild与来源删除竞争；v43先于v42到达、同version有多个event；单handler失败 | 注册屏障不漏事件；dirty重读不被旧baseline覆盖；同aggregate按version/sequence或canonical重读保护，v42不覆盖v43、sibling按event_id不误去重；handler独立重投不取消该顺序约束；action重验 |
| W41 / D08/各生命周期域 | DeleteProject确认后改名；202后Runner离线或清理未知；最终删除响应丢失并原key重放；名称被新项目复用 | 陈旧确认拒绝；必要stop/cleanup未知保持gate/名称；BeginDeleteProject/GetLifecycle可返回receipt分支，主记录已删仍仅原Owner当前有效Session可查原命令终态，不重放正文；旧ID清理不伤新同名项目 |
| W42 / D04/D09/D10/D20/D22 | 删除System Model与新增引用竞争；替代reasoning不兼容；Execution持Model lease期间轮换Secret；MCP删除/轮换仍有旧lease | 引用锁阻止遗漏/部分替换，不兼容全回滚；管理员不读他人正文；Model固定credential_ref并保护删除保留，后续turn取新值、已发请求不热换，不固定明文/版本；MCP按自身shadow/provider binding有效性处理并保留至合法释放 |
| W43 / D24/D09/D22 | Summary生成中早期message被替换但末条ID不变；或finalizing取消/删除；历史regenerate成功/失败 | input_digest改变拒绝旧发布；取消后迟到不复活；成功替换只标待更新等下一正常finalize，失败不盲标脏；首轮title+summary+completed原子且标题只生成一次 |

## 文档自查与证据规则

作者检查范围为本目录 Markdown；来源为基线架构、开发计划 D01 与主规格修订 1。校验应包括：所有相对链接与 fragment 存在；UTF-8/LF/末尾换行/尾空格；代码 fence 成对；上表全部 D01 条目有端口/owner/绑定责任；状态表与来源一致；冻结后记录每文件 SHA-256。`git diff --check` 不覆盖 untracked 文件，必须额外检查本目录字节。

实际可运行的文档检查使用 Python 标准库扫描本目录和本地链接目标，以及 `git diff --check`；执行命令/结果与最终指纹由 C01 交付报告、V01 独立检查记录保存。不得把 `go test`、数据库竞争/恢复、协议兼容或产品试玩写成已经运行；后续各责任模块创建真实 fixture/适配器后，将对应 W 编号转为可执行行为检查并记录输入版本、命令及结果。实现/契约变动只重跑受影响检查，未变来源和指纹可复用。
