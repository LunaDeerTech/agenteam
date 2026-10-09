# Skills 持久初始化服务恢复点

- 树：`/workspace/agenteam-skills`，分支 `ai/skills-service`；基线正式 main `ca9f2d5d`。D08已正式交付，旧初始化树保持冻结，不再修改。
- 当前：已完成Variables独验并返回D10作者；rev3 SPEC已获独立有限接受。本域四口initializer、OwnerReader、持久work和有界公平Recover均可构建。原产品作者真实PG的Persistence（60950）、PublicationRollback（58518）与CommitRecovery（39205）已完整PASS，均明确消费受控Object端口；后增Project精确Stop已获有限独审，当前产品PG12子（96753）完整PASS。真实D05对象组合仅完成编译准备，生产root仍未绑定。
- 已保存SPEC片段：`docs/development/work-items/d10-skills-initialization.md`、`docs/development/work-items/d10-skills-initialization-design.md`、本文。已由root保存/push ea13186d，设计技术段继续freeze；未自行Git操作。
- 当前可复用：实际D05 same-Store Object Audit checker；D08 original initialization四口、收敛口与初始化Audit wrapper。本域Skill exact映射provider已实现，真实Object组合测试已接线但未动态；生产root未绑定，constructor非nil不证明真实组合。
- 共享待协调：D05初始化Service closed shape/initiator及SkillRevision+ProjectDeleted release三个窄补口已完成并获有限独审，尚不证明真实清理；Project CleanupPhase现unbound，本域active初始化与删除Audit分流、生产同participant组合仍待。Project CleanupPhase/root仍未授写，Object runtime join停止项不恢复。
- 迁移00027已随上述初始化及当前Stop的真实PG fixture连续执行；单独升级、约束和DDL失败回滚矩阵仍未动态。root已精确刷新00024到正式3cea6076，00025保持da16d95a、00026保持4174e160；前序来源与各域证据不替代本域独立迁移验收。
- 下一步：原CommitRecovery已实际闭合；Migration、AdmissionUnknown、OwnerMetadataCurrentAuthority和真实D05三子组合仍各自等待root单top fresh grant后实际执行。前三项用原两资源PG窗口，D05用原七资源窗口；资源、缓存继续唯一所有。root授权的Project精确Stop子能力已获下述有限独审；旧binary及对应产品基线单独保留，不将旧PG结果外推新产品。不spawn，root协调交叉审查。
- 当前没有本实例运行进程/真实资源/缓存租约，未经运行的范围不得写PASS。必要失败和实际检查在本恢复点按发生追加。

## 下一独立清理 SPEC（待未参与者审）

- root授权推进精确CleanupAuthority/Project CleanupPhase工程规格，并仅预留全局00028给Skills后续必要升级。本轮在 [D10设计§16](../docs/development/work-items/d10-skills-initialization-design.md#16-下一独立结果skills-精确-project-cleanuprev1-spec) 与主卡写rev1可审片段，未写SQL/产品，未跨改Project/App/D04。00027不重写。
- 最小数据变化去除cleanup到已发表skills的FK，保留原initialization exact Project/Skill/Revision/Object/Upload约束；当前D08 !initialized不得BeginDelete，reserved可存绝不等取消/清理授权。已initialized Project的合法遗留是原candidate实际closed后同Object/Upload重试、留下旧AbandonedAttempt payload/marker；原Creation与每个attempt映射须同Tx核实。
- 明确root负责Project lifecycle_authority.go及测试的Skills-only CleanupPhase：同Store/liveTx/Project锁、当前Cleaning/domains、原Owner/action/operation/ProjectVersion、冻结manifest与依赖、全stop终局。Skills消费并补Cleanup/Release opaque plan、Claim/Checkpoint/Finalize exact父锁provider；同Tx gate+Release后实际预算内D05清理，原Unknown确认、已gated不重复Release、不重写旧D05 cause；ObjectDelete原Creation metadata＋真实Object witness。最终六表原子清空与当前gate下单调终态复核，不另留永久本域receipt；全participant/root/Object runtime join仍后继。
- Knowledge TCP诊断移植临时优先窄审已完成：f8fe7d actual0，增量逐字Runner57642926，逆除分别逐字Knowledge666169b6／Runner3e7fd3bd，driver未改、11exact三表相同、预算/Wait/resource/input/gate其余字节保持。只接受已审方法的精确移植，无资源运行，不回填原56777或54818终态；已回root/作者，不另复制控制源。
- 本节三路径（主卡/设计/current）已由root保存 `e9e608f9`，Knowledge正作未参与设计者独审，技术段保持冻结。本域无执行／资源，不因等待而继续改已冻结SPEC。

## 本轮恢复保存与 Knowledge 独立风险审查

- root 已保存当前 Stop PG 测试／卡／本文三路径到 `19353f4e766783cdaabd3068833c424f3a631985`；此前 `70036` race-c、`f8911f` 单 top discovery 仅编译准备，随后12子已由96753实际验证。原 `39205` CommitRecovery 完整 PASS 只对应旧 frozen binary 产品组合，不外推到新增 Stop。本轮Stop使用原两资源 driver，无 MinIO/harness新依赖，结果见下。
- 已完成 Knowledge B02 15 生产源、00025 和四个共享 Project adapter 的独立风险审，详见 [完整报告](knowledge-b02-review/README.md)。发现真实 D05 reader 取消后 Close 返回原错误导致 Knowledge call 不退役（P1）、RecoverCleanup 首 Pending 全局阻塞后续 Project（P2）；作者修复后，root 保存 `2287eca0` 的五源已获本实例独立有限接受。完整 B02 仍待当前真实补验，不改他域产品。
- 原离线实际源 overlay `61981` actual0/race1.042s，1 top/2 子；补调度控制 `99769` actual0/race1.023s，2 top/2 子。原复现测试断言缺陷、文件保留；另增修复独验 `15866` actual0/race1.044s，4 top/7 子：真实 D05 Close／cancel／阻塞实际 join 与保错，65旧项32/32/1及新低/高项下轮公平、原 hard/Unknown cause/attempt、并发 Busy／取消后 actual callback 退出。初 `15786` probeFAIL 为 DeepEqual 比 opaque closure 的测试误判，改正式 Equal/Details 后通过，产品未改。完整 env、必要探针与边界在报告；没有 PG/socket/网络。
- Knowledge 原 Runtime56777真实第一子FAIL、后两子PASS，hostTCP尾FAIL保留；修复不追改原结果。新分页SQL及当前新 Runtime/Cleanup 尚需真实窗口，新增 Cleanup 两真 reader 按 durable ID 判序的子例仅静核。作者59862 UUID顺序假设FAIL与修后49004 full race事实独立保留，不替代本实例结论。此次仅新增探针及报告／本文3路径冻结，旧桥和缺陷探针未改，无自有命令或资源在途。
- 新 review 目录四文件加本文共五路径可恢复冻结。源检查未发现其他授权／删除原子性 mustfix；原作者六组有限 PG 和后续五组／最小独立真实补集边界不变。当前本实例无测试／真实资源在途。

## Work 四条预声明截断方法独审

- 环境恢复后本域 status clean；root 已远端保存 Stop 结果 `0a20321a` 与 Knowledge 修复独验 `0d9cf6b1`。没有重做本域实现或重跑真实资源；Knowledge 新 Runtime54818 由作者收原 session 全尾，在结果到达前不外推。
- 对 Work 固定 `152eb964` 相对 `ae101b00` 的四条截断 helper 方法有限接受，无 mustfix。实际 `53260` exit0／63 控制／0 unhandled（原57＋本人6）：四类 failed 尾完成后再 success 仍拒且不启动 finished；普通 failure 无截断豁免、held-read 原 finished 晚拒仍失败，均调用原 finished 一次。依据与边界见 [独审记录](work-cut-review/README.md)，必要探针只读取固定 Git source、写 Skills 自有忽略 output；没有 Work 产品写入或 PG/browser/socket。
- 该次检查时 native 仍为作者 WIP，后续稳定整包独审见下节；原05整体FAIL和五条普通 aborted 的缺失证据保持，不以方法接受代替真实 UI。

## Work native 整包独立复核

- Work `d3322e3e` 相对方法基线 `152eb964` 的 helper/spec/native/publication/作者controls 五技术范围有限接受，无mustfix。AST `dd8eb0` actual0确认原三消费函数及198个验收调用／顺序不变；原 Request唯一XID、public候选二次唯一门禁、私密闭集投影、原Promise／body／reader、导航与单flight有界观察均核过。详见 [后续独审记录](work-cut-review/README.md#后续-native-整包独审)，没有改Work产品。
- 最终独立 `16351` actual0／41控／0unhandled（作者33+本人8）：cancel原Promise／一次调用／保错、sync read throw、退休后旧wrapper不升级、end拒绝不变end witness、实际locked PW async evaluate同步内错转拒绝、finish已stopped但listener尚在时late原PW事件不绑定／不成EOF，以及实际不可扩展ReadableStream reader仍原成功且诊断失败显式。中间71581／38与86592／40 actual0；本人96237注入未定义变量FAIL保留，修后16351通过，不是产品反例。
- `retired` 不单独证明所有hooks还原，须看observer_failed；sample_joined不证明原native/public pending join。flush/finish为先已有pending≤250ms、再end≤250ms两个串行上界，原PW45／expect5／Go120／Go6m／root540+60+3+75未改；原普通finished/schema/client全部mandatory，无fallback。没有browser／PG／socket／网络，原05FAIL不变。
- 新 `.agent-state/work-cut-review/native-controls.cjs`、该目录README、本文三路径freeze供root下个安全间隙保存；Cleanup SPEC两技术文档仍 `e9e608f9` 未改，无自有命令／真实资源在途。

## 剩余三组 PG 的固定组合入口

- root已保存上一节native独审3路径为 `4442a356`。随后只读核当前 `tests/skills`、Skills/Object/Project产品及迁移相对 `19353f4e` 无diff，Skills产品相对 `d0a16242` 无diff。现成70036 `skill-pg-stop.test`（32,895,591 B，Go1.27.1/race/integration）本就是完整测试包；`7321c7` 实际list发现尚未执行的Migration／AdmissionUnknown／OwnerMetadataCurrentAuthority／ObjectInitializationPublication四top，不需要新业务compile。旧migration/admission/owner/object四binary实际list分别4/5/6/7tops、均无Stop（5306 actual0）；其旧产品边界保持。
- root授权的两工具增量仅允许新 literal `^TestSkill(Migration|InitializationAdmissionUnknown|OwnerMetadataCurrentAuthority)$`，原单top格式照旧；监督器对该唯一PG组合要求实际顶层集合正好3且无重复。无效UTF8／读取失败返回明确FAIL，仍继续driver已Wait后的desc双轮／TCP双空／input与terminal尾。未增failfast，Go6m／driver105+15／supervisor123+3／TCP75、两精确资源与原单top/root流程全不变。
- 新小driver `output/ai/skills/compile/pg-only-skills-combo-driver`（15,394,731 B）97198 actual0；旧driver与全部测试binary未覆盖。完整离线命令：

```sh
# cwd: /workspace/agenteam-skills
env GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOTELEMETRY=off GOMAXPROCS=2 \
 GOMODCACHE=/workspace/agenteam/output/ai/model-ui-recovery/go-mod \
 GOCACHE=/workspace/agenteam-project-variables-independent/output/ai/project-variables-independent/gocache \
 GOTMPDIR=/workspace/agenteam-skills/output/ai/skills/compile/tmp \
 /workspace/toolchains/go1.27.1/bin/go build -mod=readonly -p=1 \
 -o output/ai/skills/compile/pg-only-skills-combo-driver \
 .agent-state/task-planning-recovery/pg_only_driver.go
python3 .agent-state/skills-pg-combo/controls.py
```

- `b09e76` actual0／28离线控制：运行实际新driver验证3个允许selector（组合＋两个旧单top）与6个宽／错序／缺项／多项拒例；允许项故意使用不存在parent，恰在Statfs失败退出，无资源创建。实际helper核正向／缺项／多项／重复／仅sub／无效UTF8／失读；实际supervisor main在受控已Wait child、空desc/tcp下跑正向、缺项、无效UTF8与旧单top四例，保实际Wait观察及完整双尾，未把控制当真实资源回收。两工具逆去唯一增量后全文逐字 `4442a356`，264020 syntax/gofmt/diffcheck actual0。
- 未验范围仍是Migration 4直接子（其中30约束负例）、Admission work/reserve 2子、Owner 12子，7个独立DB顺序清理、两个proxy顺序实际join；只计划原105秒内一次有限组合尝试，未实跑、不保证静态耗时、不增预算。真实D05另走原七资源root-chain／同70036 binary／单独ObjectPublication三子，Cleanup unbound/noRuntime限制不变。已PASS的四top不重跑。
- 两工具、必要 `.agent-state/skills-pg-combo/controls.py` 与本文4路径freeze，交Variables未参与者窄审；本实例无真实资源或命令在途。新的真实窗口仍需root fresh grant，当前Model19唯一窗不受影响。

## 当前 Stop PG 完整结果

- fresh grant 后第一工具同完整env采样可用5,707,370,496 bytes≥5,368,709,120，才exec原supervisor。`96753` 只跑 `^TestSkillLifecycleStopPersistence$`，Go5.32s、12子全PASS；Go986629与driver986046实际Wait0，driver15.375099071s、supervisor74.585s、outer actualexit0。
- PG container `abd75e868fca3a9b85455b45fd5d5b9979a29cea834b25a6c020df4f21c65c2b`、network `c47b4eb492932b0d1f0b663b81b4979ffd5bc81e40e8d531a0cc9d2f9abaea69`、nonce `b1e88e7e604635298362a94a485645bb` 原两资源双clean；desc双[]、HOST_TCP双delta_empty、inputs_unchanged=True/terminal0齐。现场两PID absent，run仅owned.json，fixture/env/证书/runtime皆absent无symlink。原件 `output/ai/skills/pg/pg-93953cff7f174f49937d089f7b5c6c1c.log` 与同名目录；已向root归还唯一窗口，不自动续跑。
- 消费70036独立binary（32,895,591 bytes），产品=d0a16242六源＋19353f4e新测试；当前真实same-Store Project LifecycleAuthority、phase/cause/participant/Owner门禁、archive/delete原held Discard实际call退出、父锁竞争、原Stop COMMIT Unknown与不取消/后继明确取消均验证。Object/Process仍受控，Project/Creation/lifecycle是披露的规范种子；不证明真实D05/foreign进程死亡、Create/BeginArchive/Delete、完整participant/root。
- cwd固定 `/workspace/agenteam-skills/tests/skills`；沿原Go6m/driver105+15/supervisor123+3/TCP75，不变更预算。实际env/argv如下（已运行，不自动重跑；任何新真实top仍须root新grant）：

```sh
env -u AGENTEAM_PG_FIXTURE -u AGENTEAM_PG_UNSUPPORTED_FIXTURE \
 -u AGENTEAM_OBJECT_FIXTURE -u AGENTEAM_OUTBOUND_FIXTURE \
 PATH=/workspace/toolchains/go1.27.1/bin:$PATH \
 GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOTELEMETRY=off GOMAXPROCS=2 \
 GOMODCACHE=/workspace/agenteam/output/ai/model-ui-recovery/go-mod \
 GOCACHE=/workspace/agenteam-project-variables-independent/output/ai/project-variables-independent/gocache \
 GOTMPDIR=/workspace/agenteam-skills/output/ai/skills/compile/tmp \
 python3 /workspace/agenteam-skills/.agent-state/task-planning-recovery/pg_only_supervisor.py \
 --driver /workspace/agenteam-skills/output/ai/skills/compile/pg-only-driver \
 --binary /workspace/agenteam-skills/output/ai/skills/compile/skill-pg-stop.test \
 --run '^TestSkillLifecycleStopPersistence$' \
 --output /workspace/agenteam-skills/output/ai/skills/pg
```

## 首个持久实现片段

- 新 `internal/central/skill/{store,initialization_state,initialization_state_test}.go`：同Store接口、原Fault/UnknownAttempt及cause保留、原命令摘要、冻结builtin持久状态、精确Revision owner与真实Skill父锁；不是已可调用服务或成功initializer。
- 新 `db/migrations/00027_skills.sql`：6本域表及精确composite FK/closed状态草案，无跨域FK/查询/共享Audit CHECK；24/25/26未齐，未执行DDL。
- 首次pure session7624 actualexit1：构建误用不存在的`object.NormalizeLocks`，0行为测试；随后改为已验证且按Foundation顺序的4锁集合。
- 修后session56471 actualexit0：Go1.27.1 `go test -mod=readonly -p=1 ./internal/central/skill/... -run '^TestInitialization' -count=1 -timeout=45s -v`，3top/9子，skill0.008s；contract无匹配不算新增PASS。GOPROXY/GOSUMDBoff、GOMAXPROCS2、自有复制cache位于Variables树output/ai/project-variables/independent-http-review，未共写作者cache，无网络/真实资源。
- 该4源+主卡/本文6路径当前freeze交root WIP；SPEC技术段不变。下一新增repository/Authority与四口实现，同时按已授范围做独立D05闭集补口片段。原SQL/PG/实际对象/Unknown ACK/生产root均未验，不能据pure写完整结果通过。

## D05 精确兼容片段（已有限独审）

- root授权的7路径已freeze：`internal/central/object/contract/{authority,access,reference_cleanup,knowledge_cleanup_test,skill_initialization_test}.go`、`internal/central/object/{transfer_upload,skill_initialization_test}.go`。
- 只增加原ProjectInitialization+SkillRevision+同Project/UUIDv7 CreationCause的Read/Mutate形状；ReadObjectID/ProtectedLease拒绝。原fullplan还必须由本域当前Authority校验，不是通用Service许可。唯一Service initiator持久原CreationID且在任何SQL前核精确grant。Release shape只增SkillRevision+ProjectDeleted，原Knowledge矩阵对应一格更新，具体Release和Runtime没改。
- 作者离线Go1.27.1/p1/GOMAXPROCS2，18349 pure actualexit0：新3top+旧Avatar/Knowledge相关兼容全部通过；96242 object/contract完整race actual0/1.048s；86475唯一真实reservation函数SQL边界控制race actual0/1.023s。这些没有PG/对象网络，仍需未参与者窄审和后续真实组合。
- root已精确导入00024=334f86d0、00025=da16d95a、00026=4174e160的迁移字节；没有运行SQL。24/25各域作者已验、26当时仅独审，不能据此称24..27整链已PG通过；前序由root按各域正式交付集成。00027仍本实例唯一写域，本轮未改。
- 当前无本实例编译或真实资源在途。本7代码+主卡/本文9路径freeze供root保存/派独审；后续新repository/Authority与4口继续，不等待Object停止项或生产root。

## 当前服务片段

- root 已保存/push c59189ad：此前 D05七源+主卡/current、`skill/{repository,repository_test,authority}.go`、`skill/{service,service_test}.go`及前序三迁移。D05由未参与者recover_harness有限独审接受：真实函数overlay 2top/6子actual0，仍非PG/MinIO或Runtime停止组合。
- repository/Authority作者26587 pure actual0：累计5top/20子，扫描损坏与当前Project gate/held/live顺序；service owner作者67258 race actual0/1.056s：累计7top，缺绑定与取消后容量/实际join；不是完整initializer。
- 新 `skill/{initialization_read,initialization_read_test}.go` 实现Inspect/DiscoverConfirmation/ConfirmInitializedInTx：仅原command当前门禁，同Store活Tx/完整真实父锁、完整发布关系；读事务未明确commit不外送结果，Confirm不新Tx/补锁/I/O，原issuer/ctx/Tx/actor不替换。当前写入四口的Initialize尚未实现，不声明已满足完整接口。
- 首轮57171 race actualexit1：确认12子通过、观察7子中denied错误身份控制失败，原因Foundation CommitResult复制/改写callback Fault；已修为确认NotCommitted时返回原callback error，Unknown外层仍保留。原失败不删除。
- 修后71990 race actual0/1.757s（2top/19子）；再加原delegate Unknown负控96767 actual0/1.057s（1子）。当前2top共20子有效，均受控SQL/Project端口、无PG/网络。精确命令 `go test -race -mod=readonly -p=1 ./internal/central/skill -run '^TestInitialization(Inspection|Confirmation)' -count=1 -timeout=45s -v`，工具链/隔离cache同上。
- 本新2源+主卡/current共4路径freeze交root checkpoint；下一继续初始化Plan/Reserve/Publish与exact Object/Audit authority，不等待生产未绑定项，不改已审D05源。当前无本实例运行资源/命令。

## 初始化写入可构建片段

- 新 `internal/central/skill/{initialization_write,initialization_write_test}.go` 已实现正式 Initialize 与四口编译断言：原冻结命令规划 → 实际builtin payload准备 → 同Tx Reserve+精确attempt映射 → 已知commit后UploadPrepared → 同Tx不可见Skill/真实ObjectPublish/Revision/初始化完成。Known重放不再physical；无scope/完整metadata匹配或仍prospective receipt则整个Publish Tx失败。
- 作者60171 `go test -race -mod=readonly -p=1 ./internal/central/skill -run '^TestInitializationWriterCommitBeforePhysicalAndAtomicPublication$' -count=1 -timeout=45s -v` actualexit0/1.174s，1top/11子：正常与重放、Plan/Reserve/Publish各Unknown、physical/revoke/publish失败、foreign Object/receipt、真实Discard后才释放本地call。端口用明确controlled事务/Object替身，不是真实PG/D05。
- 新源编译22189 actual0，原观察/确认相关纯控0。当前四口编译闭合不代表完整服务：持久work/recovery/生命周期和真实Skill Object/Audit授权、OwnerReader尚需接入；无Production root或真实对象声明。新2源+主卡/current4路径freeze供checkpoint后继续authority/runtime，不改此前D05七源。

## 精确授权、Owner读取与持久工作阶段

- root已实际保存/push e6edd3c7：初始化写入、Object exact authority、初始化Audit facts及对应测试。Object授权只开放原Service初始化变体和Human当前Owner读，Agent/维护/生命周期未绑定分支仍拒；Audit provider同Tx精确映射后仅委托一次原ctx/Entry/key，constructor不能机器证明真实Skill+Object组合。作者Object authority2top/15子race99651 actual0；Audit1top/13子race56098 actual0，包括真实Object checker缺私有witness拒绝。尚无真实PG/MinIO。
- 新 `skill/{read,read_test}.go` 实现ListSkills/GetSkill：每次真实端口当前Owner/Session/Project门禁、完整父锁、已发布immutable关系；未初始化不伪装空列表。首轮95559因测试Project名称含空格setupFAIL，修fixture后96465 race actual0/1.100s，1top/10子。OpenPackage仍待实现，不声明完整OwnerReader。
- 新 `skill/{work_repository,work_repository_test,runtime_work}.go` 和service/initializer接线：physical前同Tx持久登记原process/父关系/唯一work；只有实际Discard与调用返回后才技术结账，cancel/Stop/Unknown不得提前Joined；Drain沿调用者原ctx，不借新预算。原注册Unknown用原command锁串行判定，未确认尾保留本地owner。跨进程恢复/真实生命周期仍待实现。
- work片段首轮36292错误引用不存在sc.Skill、随后534f15测试unused import均setupFAIL，修后65428 race actual0/1.072s（1top/10子）。集成81715 race actual0/1.355s：5top/23子，含writer13子、work10子和实际阻塞Discard后取消/原预算/持久join控制。精确selector `^Test(InitializationWriter|SkillInitializationWork|SkillService|SkillWork)`，Go/p1/offline/cache同前；无socket/PG。
- 本批read2、work3、service及writer2、本文/主卡共10路径freeze交root。设计技术段、D05七源保持冻结。下一继续OpenPackage、维护授权/跨进程恢复；局部controlled端口PASS不替代实际Object/PG或生产绑定。

## Owner PackageReader可构建片段

- 上一read/work10路径已root保存/push629cd46b。新 `skill/{read_package,read_package_test}.go` 实现OpenPackage及OwnerReader接口断言：当前Owner授权/完整immutable Revision和work登记在同一次已知commit后才真实Object ReadObject；由Object再查当前门禁，不移交缓存grant。
- 原P1 PackageReader保持，底层本域owner同时等实际Read/Close和取消callback返回；EOF不等Close，取消/Unknown不等work已commit，D05租约Unknown仍由D05持有。未借新的清理ctx，原错误/原预算保留；错误body、metadata/range不符都实际Close。
- 作者32726 race actual0/1.096s，2top/10子（当前gate/登记Unknown/错revision、Object、range/原body与Close错误/退休Unknown/实际阻塞Read+取消）；补取消发生在Open移交前91114 race actual0/1.053s，1top。源保持原P1内容/契约，所有Object和SQL为明确controlled端口，没有网络、PG或D05实际lease结果。
- 新2源+主卡/本文4路径freeze交root，继续维护精确映射与跨进程恢复；不把完整OwnerReader编译断言称作生产绑定或完整Skills完成。

## Object维护映射窄片段

- OpenPackage4路径已root保存/pushd74ec396。新 `skill/{object_maintenance,object_maintenance_test}.go` + `object_authority.go` 维护分派：仅Inspect/原writer完成/旧attempt join/reader释放/process释放，重验本域唯一Object→Project/Creation/Skill/Revision/upload及精确attempt/process/完整父锁。同Store活Tx重读；普通Owner/read/write门槛未变。
- 这是D05既有私有实际return/lease/process验证之外的本域映射，不制造Service授权；维护回收不以已关闭active门禁阻塞原技术结账。新physical恢复、不可逆清理仍未绑定，继续拒绝；生产组合必须由同实例D05检InstanceID和私有生命周期证据。
- 首轮28333 race actualexit1：原2top/15子PASS；新14子中8个正常准入前FAIL，实际测试executor将单列查询前缀误匹配本域完整row查询，未到目标行为。只收紧测试query匹配后30392 actualexit0/1.093s，新1top14子均过；原产品源不改，原失败保留。
- 该3Go+主卡/本文5路径freeze供checkpoint。下一持久恢复按pass推进忙项，并只用精确旧Process终局+原锁/当前gate收敛，不代D08重开发布；尚无真实PG/对象/生命周期组合。

## 持久恢复阶段与暂驻

- 维护5路径已root保存/push0638ddc3。新增 `skill/{recovery,recovery_test}.go` 和00027 work recovery_pass/index：一次最多100条按持久pass轮转，忙/当前gate拒绝项也经明确技术调度commit移至队尾；不是重发初始化，绝不调用Reserve/Upload/Publish。
- foreign实例必须ProcessAuthority精确旧ProcessID停止，随后实际取原command/Project/Skill完整锁以证明原Tx终局；同进程只信本实例私有returned记录，不按本地缺记录/TTL猜死亡。当前convergence gate/原Fault/Unknown保留，后续启动Service复用持久pass，不把已joined前仍未commit当成功。
- 首95974及27820均actual setupFAIL：误用不存在id.NewService；改正式RegisterService.Actor后85824 race actual0/1.410s，3top14子（包括100忙head后新Service达到第101条、原gate/进程/fence/Unknown、坏候选/原取消预算）。随后8057完整skill/... race actual0：skill2.441s、contract1.311s；50111vet actual0，diffcheck0。00027仍未SQL执行。
- 新3源+主卡/本文5路径freeze供root保存。当前Skills暂驻在可构建边界，转任Variables未参与产品实现的独验；本树无资源/命令在途，不改已冻源。
- 仍缺真实完整能力：Skills00027及前序整链PG、实际D05/MinIO发布与私有witness正例/COMMIT ACK丢失；D05 Recover/清理planner、Project lifecycle/删除Audit外层与participant仍须继续实现，Project CleanupPhase尚unbound且root未绑定；Object Runtime停止项不解除。技术尾授权和controlled PASS都不能证明生产完整Skills服务。

## 恢复Skills与首个PG验收片段

- 新 `tests/skills/{fixture,initialization}_test.go`：连续真实迁移、真实Store/锁/ProjectAuthority与Skill四口；覆盖持久发布、重建Service后同原ID观察/重放、私有issuer/完整锁/ended Tx确认拒绝、发布失败的原子回滚与保留attempt。Project/Creation是明确的测试规范事实，不是Project.Create/真实Human会话验收；外部Object是受控端口，但本域AccessPlanner与OwnerAuthorization仍调用真实Skill+Project实现，不冒D05/private witness正例。
- 离线Go1.27.1/p1/GOMAXPROCS2/GOPROXYoff、独占原Variables独验GOCACHE：`go test -mod=readonly -p=1 -race -tags=integration -c -o output/ai/skills/compile/skill-pg.test ./tests/skills`，26616 actualexit0；2732a4实际精确发现 `TestSkillInitializationPersistence`/`TestSkillInitializationPublicationRollback` 两top。只编译/发现，没有执行PG或网络。
- root额外精确导入正式3cea6076的 `.agent-state/project-variables-independent/commitproxy/{proxy,proxy_test}.go`，后继原完整COMMIT帧恢复直接复用它；不得修改此已验helper或另造proxy/监督框架。前序00024变更随本批保存，25/26逐字旧稳定源，不制造变更。
- 本批两新Go+主卡/本文+上述两proxy+00024共7路径可构建freeze交root；无编译/资源在途。下一另新增COMMIT恢复测试接线，真实105+15/123+3/75窗口仍须freshgrant；现有PG两top未动态。D05/生命周期/生产root未闭合与Object停止项原样保留。

## 原 COMMIT 帧恢复验收准备

- root 已保存前段 `7b2c6753` 与新 `tests/skills/commit_recovery_test.go` 的 `492c08ea`，本实例现已恢复 Skills 作者角色。新的恢复测试复用正式 commitproxy：精确原发布事务 PID、完整 COMMIT 帧先 hold、原调用 Unknown、释放后实际 COMMIT/代理 join、原 key Inspect 与重建 Service 重放；不替换 CommitResult，不把 Unknown 后的观察改写成原调用已确认。
- 16497 离线 race-c actualexit0，产物 `output/ai/skills/compile/skill-pg-recovery.test`；858c2c 精确发现 `TestSkillInitializationCommitRecovery` actualexit0。旧 `skill-pg.test` 未覆盖新 top，保留但不用于该恢复场景。三个已编 top 均未执行 PG；每个 fixture 调用连续迁移，只说明将实际消费 00027，不冒独立升级/DDL 失败回滚验收。
- 当前仅准备既有两资源 PG driver：单个精确锚定 selector，105s 主体＋15s 清理、supervisor 123＋3s、TCP 尾 75s、fresh 5GiB；测试 cwd 固定 `tests/skills`。COMMIT top 额外 loopback proxy 为该测试自有并要求实际 Close/join，不启 MinIO/七资源 root。Project/Creation 仍是披露的规范测试事实，外部 Object controlled；真实 Skill/Project/PG 与受控 D05 边界分开，尚无本轮真实资源或业务 PASS。
- 82290 既有 PG driver 离线构建 actualexit0，产物 `output/ai/skills/compile/pg-only-driver`。原 driver 接受一个 canonical 精确锚定 top，不需新增 domain 映射或改监督器。主卡/本文更新完成后冻结；首个 Persistence top 已具备编译输入，单独 00027 升级/约束/DDL 失败回滚测试另行实施，不阻塞这个限定初始化子能力。
- 第四个独立 top `TestSkillMigration` 已新增于 `tests/skills/migration_test.go`：fresh/repeat、已有 Account/Audit 数据的 00026→27 升级、六表合法延后外键循环和 30 项 CHECK/FK 拒例、末尾 DDL 故障整 schema 回滚/拒换 checksum/同源恢复。35384 离线 race-c actualexit0 至独立 `skill-pg-migration.test`，a4373e 精确发现 actual0，gofmt/diffcheck0；未执行 SQL。原三 top 的 `skill-pg-recovery.test` 和 driver 未改；新迁移源码与主卡/本文三路径冻结供保存，真实首窗仍仅 Persistence，不合并 selector 或扩大资源预算。

## 首轮真实初始化持久性

- `60950` 在根 fresh grant 的唯一窗口实际执行 `^TestSkillInitializationPersistence$`，cwd `tests/skills`、原 `skill-pg-recovery.test`/PG driver/supervisor；开始可用 6,400,135,168 bytes。Go 2.51s：真实本域发布/原 ID 持久观察、重建 Service 同原命令零新增 physical 重放、同 Tx 确认，以及缺真实父锁/已结束 Tx/另一 Service 私有 issuer 三负例均 PASS。
- 实际 outer exit0/72.659s、driver 13.708s；Go PID865195 与 driver PID864527 实际 Wait0、两精确 PG/nonce network ID 双 clean、desc 两次空、HOST_TCP 两次 delta_empty、inputs_unchanged=True、terminal0。自有 runtime 仅 `owned.json`，私有 fixture/证书/临时清除，完整退役后已交还窗口。原件 `output/ai/skills/pg/pg-7bfba875db50465983258b7c2075eb56.log` 及同名目录。
- 此为作者有限 PG 结果，root 已接受；真实 Skill/Project Authority/同 Store 锁与四口被消费，Project/Creation 为规范测试种子、D05 Object 为受控端口。不是独立验收、Project.Create/Human 正向、D05/private witness/MinIO 或生产 root 通过。PublicationRollback、CommitRecovery、独立 Migration 三 top 均仍未运行；未自动续轮。本轮使用的连续 00027 迁移已执行成功，但不替代尚未跑的升级/约束/DDL 回滚矩阵。

## work／Reserve 未确认时的 physical 边界准备

- 新 `tests/skills/admission_unknown_test.go` 定义 `TestSkillInitializationAdmissionUnknown` 的 work／reserve 两子项。测试 Store 转发器仅在原 callback 成功后的同一真实 Tx 读取精确本域阶段与 work，按原 backend PID arm 正式完整帧代理；真实 `WithinTx` 的 ctx/Tx/cause/CommitResult 原样保留，不注入假的 Unknown。
- 要求 work 登记 Unknown 时没有 prepare，Reserve Unknown 时仅 prepare/reserve/discard、没有 Upload/Publish；原帧释放前后的真实可见行分别核对。释放后只 Inspect 原 command 的 Pending 及实际 Drain 记账，不自动续写；原 physical Attempt/Cause/Unknown 值必须保持。Object 端口受控，不冒真实 D05。原 fixture/产品/先前 binaries 均未改。
- 97930 独立 race-c actualexit0 至 `output/ai/skills/compile/skill-pg-admission.test`，c293f4 精确发现唯一 top actual0，gofmt/diffcheck0；没有执行该 PG/socket 场景。新源码与主卡/本文三路径冻结供恢复保存；资源仍按单 top fresh grant，原两资源/预算不变。

## Owner 元数据当前权限准备

- 前段 admission Unknown 三路径已 root 保存并远端确认 `0fb43308`。现在新增 `tests/skills/owner_read_test.go`，唯一 top `TestSkillOwnerMetadataCurrentAuthority`；真实 Account.Initialize 注册与原 fixture 相同的测试 keyring，原 Account/Project Authority/Skill Service 验证每次 List/Get 的当前 Session、Owner 与 Project 状态。
- 12 子项覆盖已发布但 Project 尚未初始化、active Owner 正向、跨 Owner/admin、Session 错配/缺失/撤销/过期后重验、未知 Skill/Project、archived 正向及 deleting 拒绝。正向按真实 Skill 表值精确比对 metadata；全部读取要求零额外 Object 操作。User/Session、completed Project/Creation 与生命周期状态是披露的测试规范种子，不声称 Login、Project.Create/归档/删除命令或真实 D05 流式读取通过。
- 首编译 `7162c9` 因自有 `output/ai/skills/compile/tmp` 不存在而 setupFAIL，未启动 Go 编译；补齐该目录后 `98760` 离线 race-c actualexit0，产物 `output/ai/skills/compile/skill-pg-owner-read.test`，`33518` 精确唯一 top 发现 actual0。gofmt/diffcheck0，没有运行 SQL/socket；原 fixture、产品、PG driver/supervisor 和先前固定 binaries 均未改。
- 该新源与主卡/本文三路径在可构建边界冻结供 checkpoint。下一实际仍由 root 单 top fresh grant，OwnerReader 准备不改变已有 Rollback/CommitRecovery/Migration/Admission 的未验事实或既定队列。

## 发布回滚实际终态与真实对象组合准备

- OwnerReader 三路径已保存并远端确认 `5e27c32d`。根随后 fresh grant 的 `^TestSkillInitializationPublicationRollback$` 仍消费固定原 `skill-pg-recovery.test`/PG driver，cwd `tests/skills`，开始实采可用 5,907,701,760 bytes，原两资源/105+15/123+3/TCP75。
- `58518` 实际 outer exit0/69.153s，Go 1.86s；Go PID896030、driver PID895431 实际 Wait0，driver 10.442s。两精确 PG/network ID 两次 clean、desc 两次空、HOST_TCP 两次 delta_empty、inputs_unchanged=True、terminal0；自有目录仅 `owned.json`，private/runtime 不存在。原件 `output/ai/skills/pg/pg-f219db39a0504addb2c06c423a402ff5.log` 及同名目录。完整尾后已向根释放，没有续跑。此组证明受控 Object 发布失败后的真实本域 SQL 原子回滚/attempt 保留，不冒真实 D05 故障或独立验收。
- 新 `tests/skills/object_publication_test.go` 将实际同 Store 的 Skill Authority/精确 mapping、D08 初始化 Audit wrapper、Object 私有 checker/Audit 和独立 Object Service 组合；三子要求真实 publication/reference/Audit 与原 ID 重放、真实 immutable package EOF/Close/reader lease/work、公开字段正确但缺私有 witness 时确实到达真实 checker 并拒绝。上游 Project/Creation/Human 仍是披露的测试事实，不冒 Login/Create/生命周期命令。
- 初 69925 race-c actualexit1：fixture 错将 `*skill.Authority` 当成尚未实现的 `CleanupAuthority`。只移除这条无效绑定，Cleanup 继续原 DependencyUnbound，不补 allow/stub或产品；ProcessGuard 只持构造资源，未绑定 Runtime、不能证明旧进程停止。随后 8884 race-c actualexit0 至 `output/ai/skills/compile/skill-object-publication.test`，2c8c10 精确发现唯一 `TestSkillObjectInitializationPublication` actual0，gofmt/diffcheck0；该组合尚未实际运行。
- 本域缓存 MinIO 从 Variables 已验缓存本地精确复制，SHA 为原固定 `dc5298474f0bc87a068f0b1135c583bb1278c17c11c512212ed7644a238c89c8`，没有执行它。既有 `root_chain_driver.py` 和 `pg_only_supervisor.py` 各只新增 `^TestSkillObjectInitializationPublication$` → `tests/skills`/expected singleton。作者 d47937 actual0（反删两行全文原样、实际配置 1正4负、observer 尾5控）；未参与实现的 service_delivery 窄审 685fef actual0，无 mustfix，原 6m/七资源/Wait/预算均未变，不是业务独验。
- 新测试、两 harness、主卡/本文共五路径再次 freeze 供恢复保存；无资源/编译在途。此前 CommitRecovery/Migration/Admission/OwnerReader 均仍未动态。生产 service/runtime_work 暂不变：后续精确 lifecycle participant 需要工作与原调用的 Project/取消关联及真实 CleanupPhase，不能拿当前技术尾替代业务停止授权，Object Runtime 停止项继续保留。

## Project 精确 Stop 子能力

- 前五路径已由root保存并远端确认 `5291515fd6190da335a3eea8c9153a19321f49ac`。root随后明确授权本域Stop片段；原 `skill-pg-recovery.test`、`skill-pg-migration.test`、`skill-pg-admission.test`、`skill-pg-owner-read.test`、`skill-object-publication.test` 及测试/harness均未改，继续对应该checkpoint前的产品源。下述新产品不能复用旧binary的业务PASS。
- `service.go`、`runtime_work.go`、`initialization_write.go`、`read_package.go` 让初始化/reader从准入即取得Project及稳定work ID，持久work绑定原call。新 `lifecycle_stop.go` 仅提供RequestStop/InspectStop两口：同Store完整原锁+当前StopPhase确认后捕获refs，已知commit后才取消；Archive只停初始化、Delete包含reader。Inspect本身不取消；本地必须私有实际returned，foreign必须精确ProcessAuthority后再取原锁/重验gate，原登记或退休Unknown保留原owner，取消不等于join。100项之外仍返回Pending，缺本域初始化不能冒空域完成。
- 新 `lifecycle_stop_test.go` 最终4top/21子作者race通过，包括当前门禁/原Unknown/跨Project/Meeting拒绝、实际合法archive reader、阻塞Discard/Read在Close/cancel后仍Pending、原登记无行/持久退休Unknown、精确foreign终局和fence重验、101项尾。初8f96b2是错误GOMODCACHE导致setupFAIL，GOPROXYoff没有网络；修正后86040定向race1.234s actual0。随后71500全 `skill/...` race actual0（skill2.317s、contract1.317s）；再补缺本域事实拒例后18336最终Stop定向race1.213s actual0，27243 `go vet -mod=readonly -p=1 ./internal/central/skill/...` actual0，gofmt通过。均为受控Store/Project/Process的作者本地证据，没有真实PG/生命周期/根停止或独立验收。
- 下次可复制环境与定向命令（目录必须存在；复用现cache，不删除或复制）：

```sh
GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOTELEMETRY=off GOMAXPROCS=2 \
GOMODCACHE=/workspace/agenteam/output/ai/model-ui-recovery/go-mod \
GOCACHE=/workspace/agenteam-project-variables-independent/output/ai/project-variables-independent/gocache \
GOTMPDIR=/workspace/agenteam-skills/output/ai/skills/compile/tmp \
/workspace/toolchains/go1.27.1/bin/go test -mod=readonly -p=1 -race ./internal/central/skill -run '^TestSkillLifecycleStop' -count=1 -timeout=45s -v
```

- 此6源+主卡/本文8路径freeze供checkpoint和未参与者独审，无资源/编译在途。不声明完整ProjectLifecycleParticipant，不新建成功Cleanup。后续root负责D08真实CleanupPhase准入和多域`agent-skills-variables`组合/manifest/root guard；本域负责精确CleanupAuthority、Release、预算物理删除与删除Audit外层。00027 cleanup的skills FK无法表达尚未发表的reserved attempt清理，后段需root分配新全局迁移；本轮不重写已执行00027、不改Cleanup/Project/app，Object Runtime停止项保留。

## 原 COMMIT 恢复闭合与当前 Stop 的 PG 准备

- Stop八路径已远端保存 `d0a16242dd406fdaa4964e896f162b7087f35ad2`。未参与实现的Knowledge作者独审限定六源有限接受、无mustfix；独立overlay62067实际race0/1.082s，4top/2子覆盖锁前置失败不得gate/取消/求death、本地work已joined但原call未end仍Pending且Inspect不取消、foreign proof后process/kind漂移拒绝、原gate COMMIT Unknown禁止death并保留attempt。它不证明PG、真实ProcessAuthority、完整participant或root；六产品源此后未改。
- root独占grant下39205只跑原 `^TestSkillInitializationCommitRecovery$`。同一env首次statvfs实采6,485,872,640 bytes后exec原driver/原 `skill-pg-recovery.test`，cwd `tests/skills`，无MinIO依赖。Go3.88s、outer73.143s actualexit0；Go PID942672与driver PID942097实际Wait0，driver13.764590355s；PG/network两精确ID双clean、desc双空、HOST_TCP双delta_empty、inputs_unchanged=True/terminal0均闭合。目录仅owned.json，fixture/env/证书/private/runtime清除，随后已向root释放，无续跑。原件 `output/ai/skills/pg/pg-fe2cc62ede664d729d82763fa1c7ac65.log` 与同名目录。
- 该组证明原完整COMMIT帧hold→原Unknown→释放实际提交及代理join→原key只读观察/重建Service同ID零physical重放。它仍绑定旧binary的 `5291515f` 前产品源，不证明新Stop、D05、Project.Create或生产root。实际入口（下次必须先获对应fresh grant，并在同env按原5GiB门槛采样；本组已通过，不自动重跑）：

```sh
# cwd: /workspace/agenteam-skills/tests/skills
env -u AGENTEAM_PG_FIXTURE -u AGENTEAM_PG_UNSUPPORTED_FIXTURE \
  -u AGENTEAM_OBJECT_FIXTURE -u AGENTEAM_OUTBOUND_FIXTURE \
  GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOTELEMETRY=off GOMAXPROCS=2 \
  python3 /workspace/agenteam-skills/.agent-state/task-planning-recovery/pg_only_supervisor.py \
  --driver /workspace/agenteam-skills/output/ai/skills/compile/pg-only-driver \
  --binary /workspace/agenteam-skills/output/ai/skills/compile/skill-pg-recovery.test \
  --run '^TestSkillInitializationCommitRecovery$' \
  --output /workspace/agenteam-skills/output/ai/skills/pg
```

- 新 `tests/skills/lifecycle_stop_test.go` 单top `TestSkillLifecycleStopPersistence` 共12子，仅准备：真实同Store Project LifecycleAuthority验证archive/delete、accepted phase、错version/action、缺participant、错Owner、unbound；真实Project锁竞争不取消；真实原initializer卡在controlled Object的Discard，取消后仍Pending、实际return后真实work才joined；复用原完整帧proxy核Stop COMMIT Unknown不取消且Inspect不另发取消，原attempt/cause始终保持。Project/Creation/lifecycle是明确一致规范种子，不冒Create/BeginArchive/Delete命令；外部Object/Process仍受控，不代表真实D05或foreign死亡。
- 70036沿上一节完整Go/cache/offline env执行 `go test -mod=readonly -p=1 -race -tags=integration -c -o output/ai/skills/compile/skill-pg-stop.test ./tests/skills` actual0；f8911f执行该binary `-test.list '^TestSkillLifecycleStopPersistence$'` 精确唯一top actual0；gofmt通过。新binary消费已独审Stop产品 `d0a16242` 加本新测试，旧五个binaries/fixture/harness全部未改。未启动这组PG/socket，不能记行为PASS。
- 新测试+主卡/本文三路径freeze供恢复保存，无资源/编译在途。下一可按原两资源单top窗口切换新binary/selector；真实Stop准备完成后本实例按root派工转Knowledge B02未参与者整体产品风险审。D10 Cleanup/新迁移/root接入继续只记录前置，不越权实现。
