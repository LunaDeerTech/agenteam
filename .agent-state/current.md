# 当前执行检查点

- 目标：从环境中断处恢复产品开发，完成 D01–D28 全部能力及 E01 平台内游戏复刻与真实试玩验收。
- 状态：进行中；产品恢复实施尚未验收，不能视为 D11/D27 或全平台完成。
- 当前分支：`ai/task-planning-recovery`，恢复基线为远端 `main` 的 `1b38f470`。
- 恢复核对：初始本地 `work` 为 `3add174d`、工作区干净；fetch 后保留并 fast-forward 远端三个协作流程提交。没有发现 `origin/ai/*` 活动任务分支，也无本地未推送独有提交。

## 当前工作与所有权

1. D11 Task Planning：恢复实施已形成可构建检查点；按[正式规格](../docs/development/work-items/d11-task-planning.md)恢复缺失实施。负责人负责卡中产品/测试路径、迁移 `00022` 和局部 README，安排唯一写者及自测；未参与实现者独立验证后才正式交付。两 ID PG-only harness 旧源同样缺失，必要重建输入保存在 `.agent-state/task-planning-recovery/`。
2. D27 Model Settings：并行按[正式卡](../docs/development/work-items/d27-project-owner-model-settings-ui.md)重建四个缺失 Go/browser harness；此执行者仅写本卡四测试路径及 `.agent-state/model-ui-recovery/`，保留旧 FAIL，首个 recovery 浏览器业务已准备待实际运行，整卡尚未验收。
3. root 独占当前检查点、全局台账与 Git。构建、迁移和真实测试资源按唯一写者与隔离 fixture 协调；必要源码随检查点保存，可再生日志在忽略的 `output/ai/`。

## 环境实际核对

- Go：`/workspace/toolchains/go1.27.1/bin/go`，实际版本 `go1.27.1 linux/amd64`。
- Docker server：`28.4.0`；工作盘可用约 30 GiB。
- 已成功取得测试固定 PG 镜像 `pgvector/pgvector@sha256:99a149d3c84cfb0f32d8da7d72737e4643468787220af2223418730f8e9e9cdc`；尚未启动任务 fixture，镜像存在不代表业务验证。
- 现有 `scripts/test-postgres.sh` 默认会转 Object 套件，公共 PG fixture 会含 PG16 且未列 tests/work，不能直接当 Task 两 ID 测试 driver。
- MinIO：已按正式固定来源与 Go/CGO0/trimpath/版本 flags 构建 `output/ai/deps-minio/bin/minio`，version 与 commit 正确，SHA256 `dc5298474f0bc87a068f0b1135c583bb1278c17c11c512212ed7644a238c89c8` 与测试契约一致；二进制可重建，不纳入 Git。
- web/harness 均已按各自锁恢复依赖；本域4单元文件354测试实际PASS（8.25s）。浏览器 executable `/usr/bin/chromium` 存在，首个 data: 页面探针被 policy 拒绝（原exit1保留），后续 owned loopback HTTP 探针实际exit0，Playwright1.56.1/Chromium151.0.7922.173，browser/server Close均返回；不归因为业务失败。

## 未完成与后续

- Task DTO/typed event/六Fault/Project闭集4源已形成可构建片段：作者离线 contract+Foundation 旧pure实际exit0；Task新pure测试待补。Project追加离线检查首因固定依赖缺失FAIL，准确 go mod download 后三包实际PASS；Task runtime/reader/迁移/PG测试源已恢复到正式路径：work pure实际PASS、integration race-c及私有driver build实际exit0，七新top矩阵仍需补齐且尚未运行PG，不是整卡验收。
- Model harness首段3源已形成：同fd私有读取/严格JSON/IPC闭合和case配置；作者限定两个Go源race测试exit0，独立复核已完成，绑定 `02e3daaf`：四race测试、private/JSON边界及13个Node配置检查PASS；8个非法typed/union IPC参数仍获空错误真实复现，独立probe保留预期exit1；decode当前仅envelope/action键闭合，后续原语已补值类型/union与登记Project/target/cursor/effect校验，作者5场景race PASS；独立旧FAIL仍绑定02e3daaf，不回填。浏览器spec类型检查/private Vite build PASS，但尚无六业务case。完整后端fixture与业务仍未实现。
- account/app间接嵌入全部 migrations 的依赖已核实：原工作树含未验00022的account race-c仅记非正式编译，不执行该binary。D27后续验收在交付worktree的旧已验00021基线上构建两个同份测试源；运行前逐源核一致，不称当前wholeHEAD验收。
- 独立A/B输入已恢复为 `independent-runtime-{ab_test.go,b_test.go,build.py}`；overlay独立race-c与精确两top list实际exit0，仅编译/发现，没有PG body通过。可再生build/binary已迁至忽略的 `output/ai/task-planning-recovery/`，不纳入Git。
- 当前尚无新整卡产品测试通过结论。Contract7路径新pure与八组独立检查已PASS；Go fmt 未导出enclosing及slog嵌套JSON回退两组原FAIL保留，正式卡已纠正可保证边界及实际日志出口约束，公共DTO/wire不变。
- Task两IDdriver/监督器源码已恢复：`.agent-state/task-planning-recovery/pg_only_driver.go` 和 `pg_only_supervisor.py`；具体启动/选择器依该driver说明。初版Migration4.84s/Persistence28.48s/Atomicity1.75s实际body与全部driver/外层terminal0、两ID/runtime/TCP双空；仅作早反馈，后续测试矩阵扩展须重编译和重跑。Atomicity原编译binary/driver始终不变，但TCPtail期间planning测试源曾变，不能声称该轮整个源码窗口冻结，不用其代作最新完整输入。阶段源码完成后先作者自测、冻结限定输入，再按风险安排独立验证；每个完整结果及时交付 main 并普通 push、核实远端。
- Model UI 原恢复 FAIL、Work Structure 原 Unknown01 FAIL 与独立 B 外部工具终态缺口保留，不回填历史。
- Object runtime join、OpenAI tools 独立验收、SPA concurrent-publication、Jina/Image 来源沿台账停止边界保持；局部停止不妨碍 Task 规划与验收输入恢复。
- E01 未开始。游戏参考版本、完整内容分母、权重与可复现覆盖率须在平台前置完成后、游戏实施前冻结；最终需要平台内任务/协作/执行/审核/产物和真实试玩证据。
- root交付worktree `/workspace/agenteam-delivery` 的 `main` 仅带基线与4个Task contract源；三包race、vet及Central/Runner两入口build实际exit0。契约七路径与必要卡片/独立probe已正式commit `cbd0edc1` 并push main；git ls-remote已核远端main精确包含该提交。这仅是契约子结果，不是Task整卡或D11完成。
- Model独立可复跑probe：`.agent-state/model-ui-recovery/independent-probe.py`；审查输入固定02e3daaf，未来新输入须另验。
- 当前保存必须遵守用户“不提交无法构建的中间状态”：仅保存独立闭合、可构建的明确片段，未闭合源码保留工作区待依赖完成。
- 工具全树当前 7 席位；子实例显式 Astra/Ultra，priority 实际生效未确认。不因配置 100 推断当前容量。

## 当前可恢复片段

- Model第三段：真实app.Run私有root/handler join、17端点登记、single-arm hold/cut/disconnect、安全响应/消费tap/原请求比较/Session登记已写入 fixture 源；delivery最新account integrationrace-c与5pure原语top实际exit0。constructor/9IPC dispatcher/同Txsnapshot及六浏览器业务尚未闭合，不宣称这些通过。
- Task四个必要旧Project/Outbox selector全部实际body/监督器/外层terminal/资源终态PASS。七新top矩阵作者已扩完整源码，expanded race-c到新独占二进制exit0，最新Work pure由root race运行PASS1.234s；扩展版七新与四旧Structure尚待实际运行，不把早反馈沿用为最新输入通过。PG期间只在每top完整终态后做短Git保存窗，避免辅助hostTCP delta受推送连接污染。

## 本轮独立发现与返修

- Task私有计划闭集原FAIL已真实复现：placement.state改STATE、rank向量同时含Rank/rank仍被接受，见 `independent-runtime-private_test.go`；原offline race命令exit1、actualWait。没有冒称授权绕过。新PG已在启动前暂停，原owner仅在Task repository/events补完整嵌套strict解码及caps，20+pure负例PASS；独立原输入复验及224结构/10Unicode-EOF负例与正例实际race PASS、作者pure/vet PASS；新作者expanded-codec及新独立A-B binary已重编/精确发现exit0。旧binary不能当修后语义证据，原exit1不回填。
- Model第四段：constructor、9IPC dispatcher、只读snapshot与浏览器子进程受控入口/六Go selectors已补入两Go源，delivery race-c与6个pure实际exit0；六浏览器business仍未通过。

- 私有修复/新独立probe已冻结待恢复：Task repository/events/test三源，private-matrix及private-run。最新独立A-B binary为忽略output中的 `independent-runtime-build-7teshm0n/independent-runtime.test`；七新四旧Structure与独立A-B PG尚待运行。

- expanded Concurrency首轮FAIL：same-key final hook未到，原Go/driver/outer均exit1且资源完整退役。原owner定位为测试观察器在Update发现阶段尚无command row时把ErrNoRows注成写错误；仅修并发测试观察为EXISTS/coalesce，真SQL错仍保留，4个handshake新增提前writer结果观察。新hooks binary race-c/精确11selector exit0，尚未实际重跑，不称产品并发缺陷或原PASS。

## 最新运行边界

- Task观察器修复已保存推送285797c4；最新作者11selector binary和独立j78 A/B均race-c/list exit0，独立20技术路径全文静审闭合，无已证实未修must-fix。新Migration在codec修后实际全终态PASS；Concurrency首轮FAIL不回填；修后body5.01s、完整outer17189 exit0/70.125s，Go80762/driver80273 Wait0，两ID/runtime/TCP双空且input不变（pg-32a87df43…）。剩余9作者top、独立A/B待执行。四旧Project/Outbox有效结果保持。
- Model首个 recovery 完整case已写入，含cut/disconnect→lookup→original replay、deletedProvider replay、第四origin effect hold、同响应body schema/native client/EOF及最终counter比较；delivery account race-c/vet、6pure、TS strict与recovery discovery实际exit0。最终URL及当前Project真实GET EOF同步已补。另5browser case仍未实现；六Go selector不等于六browser通过。可恢复自有输入含native-client-probe.ts、build-native-client-probe.mjs、validate-same-body.py、fixture-go.py、run-owned-top.py。固定PG16镜像16e62164…已真实拉取。
- 首Model recovery基于5534607b实际启动，owned-recovery-769015dc…在fixture-go adapter reject处exit1，尚未进入business/browser。实际terminal65.6s、directWait/watchdogjoin/input不变、hostTCP两空；helper cleanup均返回，但driver resources.json未登记（resources0），不能称7ID独立退役验收。原setup FAIL保留，由原owner修adapter参数/失败资源记录后重跑；其它5browser未实现。Task待资源安全交还重跑Concurrency，期间不做额外host网络/Git推送污染TCP tail。

- Model独立553静审发现两处harness must-fix：delete/credential断连仅joined不足以证明after_complete效果；final严格计数允许complete_eof0而schema/client1的矛盾观测。原owner补三旗/逐token观测/精确effect计数及结果admission，setup adapter另修真实descriptor字段和失败资源采集；不把上述证据缺口称产品故障。

- 独立Task A首PG原FAIL：基线Project create INVALID_ARGUMENT，尚未进入Task authority/membership场景；session98105实际exit1，driver/Go Wait1，两ID/runtime/TCP双空、input不变（pg-31ed606…）。独立作者核输入规范后最小修probe，B暂不启动，不能当产品故障或独立PASS。
- 后继规格纯文档：d11-task-transitions.md前三节草案已可恢复，§4–13待续，未接受/未实施；d10-agent-configuration.md首三节已冻结保存；同样仅rev0草案，端口/锁/Model引用/验收待续，未接受或实施。Model返修进行中尚未冻结。

- 独立A/B原作者已按现NormalizeName最小修3处Project Name空格→连字符，2probe源冻结保存，rq9 offline race-c/list exit0。但作者Membership测试随后发现同因非法Project Name（task_concurrency_test.go966），原body1.69s/full terminal1/66.786s、两ID/runtime/TCP双空/input不变；原FAIL保留。作者将只修新测试中的非法Project创建输入，之后作者及独立binary重编/list再冻结，不以rq9冒最终闭包通过。

- Model返修稳定片段：descriptor按真实Object/Outbound UpperCamel与PG snake_case分别严格解析，新owned_resources.py在正常/失败路径记录实际资源ID；两断连逐token complete/safe/applied/native未EOF与精确cut/disconnected计数已补，Go final严格EOF/client/schema和native token/body witness一致性已补。delivery race-c/vet、8pure、TS strict/list、collector闭集/文件18checks及纯4container+3network去重发布实际PASS；两Go源root/delivery逐字节一致。原setup FAIL仍保留，新business尚未运行，独立复核进行中。

- 作者Membership唯一非法Project名称已修为membership-foreign，work-expanded-names.test race-c/11list actualexit0；独立最终7teshm0n A/B重新race-c/list exit0/Wait/inputsame，仍尚无修后独立PG。仅修测试输入，生产runtime不变。
- Model第二轮0dad9真实recovery RUN，browser configuration-loss未完成（仅Providers GET200，无mutation），Go body53.27s FAIL/工具26607 exit1，root/handler实际join；driver123.934s含TCPtail，4adoptedWait0/directWait1/watchdog+observerjoin、真实7IDs两次absent/runtime/TCP两空/inputsame。原FAIL保留，不能未定位就称产品原因。原owner补安全failure location/细step再定位。独立两mustfix静态闭合、resource35pure实际PASS，必要可复跑source保存在independent-resource-probe.py。下一资源窗交Task最新7tes独立A/B。

- 最新独立A第二次7tes原FAIL是revocation首writer User锁等待未到，归档与membership双向子项PASS；outer14779 actualexit1/72.732s、child/driver Wait1、两ID/runtime/TCP双空/inputsame，B未启动。静态可确认Execute与Lookup同CommandEX不可能同时到User门槛；原轮未采Command holder PID，不回填谁先取得。独立owner仅自有A添加第二个真实完成Create身份用于历史Execute，Lookup/Get仍首身份，以同一User撤权对3真实waiter独立观察，修后重编待验。
- D10 Agent完整rev0草案239行已冻结保存，C1为AgentCore/Ref六纯路径，可消费现identity/Model/Foundation；F1真实事实依Model引用/目录初始化等未绑定，不称完整Agent实施或Task指派可用。Task transitions规格仍待完整闭合/独审。
- Model selector纯探针确认UiField真实markup在锁定Playwright算法中exact label=false而role accessible name=true；spec已改role/闭合password label regex，补安全细step和封闭failure类别/数字本spec定位，未保存敏感原message/stack，不回填第二次真实FAIL根因。TS strict/list与纯probe实际exit0，Go不变复用编译。

- 独立A确定性返修仅自有A增加真实第二completedCreate身份用于历史Execute（Lookup/Get仍原对象），保留3个真实User门槛waiter/撤权零结果；rromoyii A/B新race-c/list actualexit0/Wait/hash一致，PG待重跑，旧7tes FAIL不回填。

- Task修后Membership完整outer59200 exit0，body2.16s/driver7.97s，Go99177/driver98668 Wait0，两ID/runtime/TCP双空/input不变（pg-a620aba…）。作者剩4新+4旧Structure，独立rromoyii A/B待实际完成；原FAIL不回填。
- root隔离候选树 /workspace/agenteam-task-delivery / ai/task-planning-delivery 从main cbd0edc1只复制13未交付Task技术源；offline -p2 Central/Runner build实际exit0，两产物真实存在，不commit/接受/runtime装配，不改Model所在delivery00021树。正式交付仍待完整PG/AB/README真实范围。
- Task transitions完整rev0草案352行已冻结保存，尚待独立SPEC；D10 C1六纯contract范围已独立SPEC接受，F1的ToolID不得从层5向上导入Agent层4，owner补低层接缝责任/阻断。已派C1六路径纯实现，无服务/迁移或指派正向能力。

- 独立Task A/B最终rromoyii组合完整PASS：A outer40886 exit0/69.414s（pg-c8c57e06…），B outer42492 exit0/84.273s（pg-63444268…）；bodyA2.64s/B18.24s，所有child/driver Wait0、两ID/runtime/TCP双空，22源末次changed=[]。20技术全文静审+privatecodec独立矩阵+A/B均通过，无已知未修产品阻断；作者余4新+4旧实际PG未齐，整卡不能接受。
- D10 C1首3源core/reference/codec已gofmt并offline包编译exit0（no test files，只编译），稳定保存；3测试正在补齐，尚无实现行为/独立验收。D10原doc已补F1 ToolID层级BLOCKED/C1状态。Task transition最新草案把T0a无Blocker纯闭包与待B0-C的T0b明确拆开，358行peer修订冻结保存，独立SPEC读者须采用此最新输入。

- Model第三轮32809 actualexit1：Go9.97s FAIL，完整terminal81.05s、directWait/4adoptedWait0/watchdog+resourceobserverjoin、7IDs/runtime/TCP双空/inputsame（owned-recovery-2920bc…）。更正此前仅检查driver顶层遗漏证据子目录而误报未入body：实际case configuration-loss-armed，closed error CONTROLLED_NATIVE_LOSS_NOT_OBSERVED；r000032已真实create Provider、formal200/119B完整安全上游、transfer cut。native具体失败分支尚未保存，原owner将补仅安全native事实，不直接放宽断言。Node hook前诊断增强仍独立有用，但不是该轮已证根因修复。
- Agent C1六source与doc已全freeze保存，pure0.023s/准确4依赖pure及race（Agent1.217s）/vet实际exit0，gofmt/diff/10链接均通过；3产品source未为tests修改。待独立实现验收，不提供真实Agent或生产服务。Task transition时间列歧义已单行消除，全文分段SPEC独立接受仅解锁T0a登记纯闭包，后继真实前置保留。下一窗作者连续剩4新top，完成后安全保存/排程再4旧Structure。

- 作者新CommitUnknown完整PASS：body15.42s/driver22.085s，outer79229 exit0/81.798s，Go114010/driver113474 Wait0、两ID/runtime/TCP双空/inputsame（pg-13b99538…）。连续后Persistence扩展paging fixture在planning253事务not_committed INTERNAL_ERROR，body40.85s/Go116123/driver115607 Wait1、outer63817 exit1/106.529s、两ID/runtime/TCP双空/inputsame（pg-4810a4d8…）。Authority/Atomicity未启动；原owner核安全SQLstate/constraint原因，不先归因产品。
- Model Node早失败诊断3源已freeze：Go只投影封闭failure类别/本spec数值位置，spec补beforeEach probe/material读取细step，不落原message/stack/log/body。delivery race-c、failureprojection racepure、accountvet、2Go cmp、TSstrict/list/diff实际exit0；原第三FAIL未定位，诊断更改不回填根因。

- Agent C1独立实现验收已通过（8819779f六source＋卡固定）：5自有pure/race actualexit0，147JSON/Unicode负例、1282字符/保留字边界、组合247344B与rawcap、Clone/log/typedRef责任均通过；依赖不含Work/Task/App/Postgres或上层Tool。可复跑independent-core_test.go/independent-run.py已freeze；root在main隔离树复制6源+2probe，候选race1.205s/vet/Central与Runner build全部实际exit0。C1卡/README仅C1独立hunk已freeze准备正式main，不复制未交付Task段。
- Paging FAIL静态定位是test matrix将future medium首项移到backlog critical保留midpoint，撞既有deferred tasks_group_rank_key；仍需安全23505/constraint实证和修后全top，不修改DDL/reader。Task源码保持PG停窗，仅原testowner最小合法rank/oracle返修。
- Model新增独立read-pagination-contract.ts/probe.mjs已strictTS与1正16负/immutable纯probe实际exit0，保持当前recovery spec不import；只验证26目录种子/跨Provider/闭集关系，不当真实pagination。必要源码冻结保存后待实际readcase消费。

- C1已正式交付main 7f2bb21176a0218286e82369b7efc75c3dab9017，正常push actual0/ls-remote精确确认；12路径仅6Agent纯source、2独立probe与卡/README/台账/current，没有00022或Model未验harness。main合入活动任务分支时保留所有Task/Model进度，仅整合root current与C1卡接受状态文档冲突。

- Task paging单测试修复100增5删已gofmt/author race-c与11list实际exit0，产物work-paging-rankfix-author-v2.test；诊断先在真实回滚Tx复现原冲突并严格验23505/tasks_group_rank_key，合法seed刷新205项真实rank后给28项唯一1..28并同步oracle，原分页/过滤/游标/cap矩阵全部保留。仅测试源改变，DDL/reader及运行实现不变；真实诊断与修后Persistence待执行。

- Task rankfix Persistence已完整PASS：body43.92s/driver48.486s、outer34134 exit0/108.495s，两ID/runtime/TCP双空/inputsame（pg-12f39fa…）；原23505/tasks_group_rank_key由真实回滚Tx确证，合法rank矩阵全过。新Authority随后body6.05s FAIL，两个archived历史分支DependencyUnavailable（planning354/1041），Go130242/driver129700 Wait1、outer35090 exit1/72.347s及资源/input全闭合（pg-dbfea1fa…）；候选是shared seedLifecycle多clock_timestamp导致archived_at>updated_at，需原owner严格实证，不先归因产品。Atomicity/旧4未启动。
- Model spec仅诊断增强冻结：actualLoss严格断言之前0600 wx写control/current-native fixed13字段/expected_bytes，路径与空query/数值/size闭合，不含请求或header值、不放宽token/ended/非EOF/status/bytes门槛。TSstrict/list/diff实际exit0；第四run仅为取得第三真实controlled-loss分项，尚无已证transport修复。

- Model第四轮99747 actualexit1，全终态83.44s/Go8.69s，7ID双absent、全部Wait/join、descendants/TCP双空/input同一。原controlled-loss-a0001实际native唯一r000032/status200/ended/非EOF/cancelled/released，三控制旗均真；bytes=0/chunks=[]是旧exact1唯一失败，不能把server cut字节等同reader交付。spec冻结仅cut改max1（disconnect仍0），新增cancel/release，保留其余严格门槛；TS/list/diff实际0，待独立复核及第五真实run，原FAIL保留。
- Task Authority仅2测试源冻结并编译race-c/list实际0，新增私有归档helper：观察旧双clock关系、不回填原FAIL；真实确定性+1µs非法输入须Read DependencyUnavailable/NotCommitted/整行回滚，合法同一materialized时刻再Read/Ref.Validate校验。产品/DDL/shared旧helper不变，Authority与Atomicity待真实运行。D01三资源低层身份草案首57行冻结，仅SPEC未接受未实施，后半验收/责任章节待续。

- 09755383已正常push保存两fixture修正与R1首SPEC；独立只读复核Model native边界/Task私有helper实际exit0接受，业务断言及旧fixture定义逐字保留。原独立A/B不调用新helper可复用已实际PASS，不能称旧binary含新helper；独立A自身双clock未来重跑稳定性未修。root隔离task-delivery同步最新3test，13技术源逐字同一，准确Go带race integration -c实际exit0。
- Task新Authority最终PASS body7.58s，Go142262/driver141749 Wait0、outer39953 exit0/71.273s、两ID/runtime/TCP双空/input同一（pg-1caa930…）。本轮旧双clock两次关系false；确定性+1µs坏输入真实拒绝且整行回滚，合法singlemoment真实读取通过，不回填原FAIL时间。新Atomicity最终PASS body4.21s，Go144049/driver143546 Wait0、outer28188 exit0、两ID/runtime/TCP双空/input同一（pg-2278a46…）；七新top按限定版本组合均有实际完整PASS，不冒当前HEAD单轮全套。下一四旧Structure正在顺序验证，尚未正式runtime交付。
- 旧StructureMigration最终PASS：body5.16s、Go145714/driver145192 Wait0、outer12737 exit0/70.880s，两ID/runtime/TCP双空/input同一（pg-df34498…）；已在该top全退后暂停保存，余旧Paging/Atomicity/Unknown待继续。R1完整128行仅SPEC冻结待独审，未产品实施；Model第四原636B安全闭集诊断输入已原样保存在controlled-loss-zero-byte.json，保留expected_bytes=1与实际bytes=0，仅原FAIL证据，非新成功。
