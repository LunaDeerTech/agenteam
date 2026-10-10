# Skills 持久初始化服务恢复点

- 树：`/workspace/agenteam-skills`，分支 `ai/skills-service`；基线正式 main `ca9f2d5d`。D08已正式交付，旧初始化树保持冻结，不再修改。
- 当前：已完成Variables独验并返回D10作者；rev3 SPEC已获独立有限接受。本域四口initializer、OwnerReader、持久work和有界公平Recover均可构建。原产品作者真实PG的Persistence（60950）、PublicationRollback（58518）与CommitRecovery（39205）已完整PASS，均明确消费受控Object端口；后增Project精确Stop已获有限独审，当前产品PG12子（96753）完整PASS。当前70036 binary的真实D05发布/private witness/replay/Package三子70196亦完整PASS；生产root与Cleanup/Purger仍未绑定。
- 已保存SPEC片段：`docs/development/work-items/d10-skills-initialization.md`、`docs/development/work-items/d10-skills-initialization-design.md`、本文。已由root保存/push ea13186d，设计技术段继续freeze；未自行Git操作。
- 当前可复用：实际D05 same-Store Object Audit checker；D08 original initialization四口、收敛口与初始化Audit wrapper。本域Skill exact映射provider已实现，真实Object组合测试已接线但未动态；生产root未绑定，constructor非nil不证明真实组合。
- 共享待协调：D05初始化Service closed shape/initiator及SkillRevision+ProjectDeleted release三个窄补口已完成并获有限独审，尚不证明真实清理；Project CleanupPhase现unbound，本域active初始化与删除Audit分流、生产同participant组合仍待。Project CleanupPhase/root仍未授写，Object runtime join停止项不恢复。
- 迁移00027已随上述初始化及当前Stop的真实PG fixture连续执行；原4315迁移业务PASS但whole FAIL保留。两fixture窄修后，61543当前三top组合完整PASS，含升级、约束、DDL失败回滚、AdmissionUnknown两子和Owner十二子，见下节。root已精确刷新00024到正式3cea6076，00025保持da16d95a、00026保持4174e160；前序来源与各域证据不替代本域独立迁移验收。
- 下一步：本域上述有限初始化/Stop/当前D05及三top真实组已闭合各自范围，生产root/完整participant未完成。Cleanup rev2只到规格接受，D05新口与Project当前CleanupPhase须先独立交付。独立Knowledge六组已按89530未变五子＋82746修复末子闭合，原失败不回填；当前完成新main装配有限独审，见下节。root已将原占位00028移交Knowledge作为共享cleanup索引唯一写者，Skills不写SQL、不放宽FK/列；本域joined-work查询与FK反查候选须由其真实EXPLAIN确认。无自动真实重跑，由root协调资源和交叉审查。
- 当前没有本实例运行进程/真实资源/缓存租约，未经运行的范围不得写PASS。必要失败和实际检查在本恢复点按发生追加。

## Skills Owner HTTP 独立静审与待返修方法

- 审查树 `/workspace/agenteam-skills-owner-http`：短合同已有限接受；固定 `890d632f` 的3产品、5纯测试、Schema/helper共10技术源独立静审接受，无确认产品 must-fix。本人 `eb56ef` 只读核冻结字节、I/O除package与正式Knowledge相同、安全8字段及HEAD无体；作者原JSON的11top/47sub、Schema21与18个HEAD状态均actual0，明确复用而未自行Go。vet首鲜值不足exit78未启动；不以纯控外推PG/native。
- 后继只读 `6fa6c733` 的native/PG fixture及作者冻结的两个PG矩阵源，4top/12sub与native3top/6sub均尚未编译、未运行。正式Account登录/注销、P2 List/Get、同Store Project gate、精确User advisory锁双向竞争及原Skill SELECT relation等待/取消、原Tx失活、完整帧COMMIT Unknown两向及Release/Committed/HeldJoined在源码上可行。Object仅明确受控初始化端口，Project/Creation/Owner/lifecycle为披露的合法上游fixture；不能冒D05物理或OwnerTransfer/BeginDelete API。
- 两个方法must-fix已交Runner：native回压目前只有Write进入、abort与耗时，须绑定原Write/Flush实际返回的native Timeout；PG `facts`错误排除Account Activity，须删除“合法read Activity”假设，补原Session `last_activity_at`可触发60秒throttle的合法前置与GET/HEAD前后不变事实。依据真实 `account/session.go` 的Authenticate及TouchActivity契约和本HTTP卡零Activity要求。其余静核未发现新增阻断，整方法等待上述窄修；不改作者源，不运行Go/PG/socket，不新建审查流水文件。
- 原温热GOCACHE `/workspace/agenteam-project-variables-independent/output/ai/project-variables-independent/gocache` 已交Runner独占，本人无Go在途或排队；Cleanup新history仍仅静态接受等待后继构建授权。root已精确退休正式P2旧独立候选及两个旧PG driver共63,777,606B，保源码/日志/旧FAIL/Cleanup候选，历史命令产物可按原固定源重建。

## B02 当前 main 装配独立有限接受

- 对 `/workspace/agenteam-knowledge-delivery` main `e94077eb`＋root44来源及Work冻结四共享Project Go源，未参与实现者有限接受、无mustfix。44路径逐字固定作者 `aaa408c8` / 独立 `924d5627`；共享Audit/events逆去新增路由后逐字main，原初始化wrapper/相邻权限源未改。初始化测试差异仅两普通委托子例；作者首837326整体FAIL与修后64574 Project race0分别保留，不把原EX-only fixture误报当产品缺陷。
- 本人 `python3 .agent-state/knowledge-delivery-review/run.py`，33965/3102cb actual0，race1.032s，4top/11sub：四Audit provider原ctx/Tx/key/Unknown/cause、错域/当前权限拒绝；四类event跨plan拒绝；真实Knowledge/Object checker缺private witness拒绝；Human/Agent普通Object的SH与Service初始化原EX隔离。固定Go1.27.1/offline/readonly/原独占cache完整入口在脚本与README，不PG/socket/network、不改交付产品。
- 原15生产风险审及P1/P2 15866、当前作者10top35sub＋Process50756、独立89530五未变子＋82746末子有效证据按固定输入复用。实际补读57974 Runtime、91700四通过组/14sub和其原FAIL、50756新Process原生完整尾；82746末子actualWait/outer/七资源双尾及TCP/input全齐已在独立树458b943a保存。无具体生产闭包差异要求重复11top或六独立组。有限Service不含HTTP/UI/D13、Agent destructive、完整Project生命周期、生产root，Object Runtime join停项不恢复。
- 新 `.agent-state/knowledge-delivery-review/{shared_test.go,run.py,README.md}` 与本文四路径冻结供root保存，无命令/资源在途。原89530/62452及作者旧失败和披露的上游seed/private/runtime边界不变；最终交付由root决定。

## D04 纯 Request / Intent 独立有限审查

- root授权只读 `/workspace/agenteam-secret-variable-storage` 92aca721 的 `internal/central/secret/contract/project_variable.go` 与相邻测试，依据faca8b33 rev2 SPEC；未参与其设计/实现，未纳Model活动prepared/stage新源。两冻结源及相关原契约逐字不变，作者57406/477472两包race复用。结论有限接受，无本范围mustfix，不认producer/SQL/当前授权/私有Audit通过。
- 新资产 `.agent-state/secret-storage-intent-review/{intent_test.go,run.py,README.md}`。本人35587/21c8a0 actual0，四top race1.017s：活跃借用期间alias Destroy禁止新用但原回调须实际返回才清零、panic/clone隔离；12goroutine并发材料/副本与nested日志/JSON安全面；真实D10 Update/Delete、原字节与presence适配及坏输入拒绝；原Actor/Session/identity/expected copy、跨namespace/operation/Project/Service拒及旧Purpose不扩。首27188/744b02为独验误写不存在NewProjectScope的setupFAIL，修成正式InProject后通过。
- 命令`python3 .agent-state/secret-storage-intent-review/run.py`，固定Go1.27.1/offline/readonly/独占Variables-independent GOCACHE及固定GOMODCACHE/GOTMPDIR均固化脚本；只做临时overlay，不写D04产品，不建新大cache或PG/socket/browser。SecretMaterial Destroy只清持有材料，无值Intent不是prepared退休令牌，不能外推actual callback join。四路径现冻结供root保存，无自有执行在途；Knowledge末子候选及工具仍冻结等待freshgrant。

## D04 kind3 / Plan / Prepared / Read / Prepare 有限独审

- 新冻结 `372d1e94` 的12技术源独立有限接受，无mustfix；旧Intent两源及35587结论复用。crypto3、plan2、service＋prepared/read/prepare6按实际源核用途/AAD、原issuer/Session binding、全锁/同Store liveTx/current gate、opaque native prepared与安全投影。后继Apply/newpurpose/nativeAudit/maintenance不纳范围，overlay排除新增活动Go并还原变动旧共享源到372d；未改D04产品。
- 本人7872/530ae3 actual0，race1.048s，3top/10sub：真实Prepare加密后历史Match正向（新Session需新plan）、不同语义与失读/密文/kind/receipt/Project/payload替换拒；Match实际持锁中Destroy等待，原Unknown/cancel保错且不读历史，actual返回后清sealed buffers，caller材料保留；公共Match拒伪接口/包装/foreignService且其方法/authority/SQL零调用。新增单top10894/a02a8b actual0/race1.016s只补第一kind3已seal、第二nonceUnknown的nil候选/无重试或范围复用/caller材料保留。旧前三top未变不重跑。
- 新资产 `.agent-state/secret-variable-storage-review/{stages_test.go,run.py,README.md}`；固定Go/offline/readonly/原独占cache入口与作者结果复用范围在README。controlled Store/authority/nonce不称实际D10授权/PG/提交；真实provider、正式连续DDL、Apply同Tx/privateAudit与rotation/Cleanup仍待。四路径freeze供root保存，无本人命令或真实资源在途。

## D04 Apply / native Audit / rotation / Cleanup 后继有限独审

- 前阶段4路径已root保存9071db4a；新审 `dccb6fed`＋`e0fb80df` 的11Go增量，有限离线接受、无mustfix。环境恢复后作者HEAD e9fb256f、Go逐字e0（844cbc）；先前后继仅只读，没有未知在途命令。本次不纳活动SQL/PG测试，不重复372前阶段。
- 本人7587/7e1ab1 actual0/race1.025s，3top/5sub：有效legacy identity正向后仅改newPurpose在SQL/nonce前拒、双loader用途隔离；Apply ReceiptRead之后实际再核全锁/NewWrite，Unknown原cause透传且零写；真实PrepareRewrap last-ID空页→head rescan/actual Rows.Close→混合kind1/2/3 AEAD与原historical Credential→Apply原CAS。末项CAS0按实际两项计数；末owner漂移拒，无补锁/checkpoint/公开进度。controlled前两项SQL不称实际整Tx回滚。
- 静核native witness只由实际Apply产生，同Store/Tx/原Request/完整锁/receipt与前后像重验，旧variant互斥；f2af63核旧Purpose/write/storage/error及原Project Audit保留。作者15128/21736/92922/5785未变范围复用，不新增同类全矩阵。实际D10 provider/Owner、00029/SQL/COMMIT Unknown、100/101 Cleanup、rotation/canary/Retire均仍待真实闭包；Runner另审DDL不替代本文。
- 新资产 `.agent-state/secret-variable-storage-review/{increment_test.go,run_increment.py}`，复用既有postgres Rows桥，README已追加；连同本文四路径freeze供root下个安全窗口保存。新Go/原产品均未修改，无本人命令/真实资源在途；原Work08环境中断结果不外推。

## Work recovery Project 根 GET 增量有限独审

- 只读Work `03f9228e` 后冻结9scope，限定recovery三次Project根GET/noquery/200，有限离线接受、无must-fix。原Request/XID/native EOF+CL及同原sidecar/schema/typed API，真实Session owner尾与本次Workspace generation/readGeneration/accept/canonicalize另行闭合，最后三份不同Project在真实finish/end/首explicit退休/pending0/hooks无错后才进入原complete。旧Work/Blocker/产品/Go/dist/cap4/预算未变，08原FAIL及环境中断缺尾不回填。
- 本人96791/4806f2 actual0，6差异控制/0unhandled：真实改名canonical阳性、显式adopt/directfacade不代无参Workspace刷新、两实际读取同XID拒绝、Workspace与Project facade hook冲突保替换且退休失败。首96940/48d000为自有two-read probe漏投影第二native的setupFAIL，补保真实两行后过，不是产品反例。087856核四旧AST/adapter两输入逆投影/产品零差异；首057bf0把schemaProgram常量当函数的静核setupFAIL保留。
- 作者55/旧116/41与TS未变范围复用，不全矩阵重跑。新 `.agent-state/work-project-read-review/{controls.cjs,README.md}`＋本文3路径冻结供root保存；无本人live命令、PG/browser/socket/network或作者源写入。新实际仍由root fresh grant。

## Project Skills CleanupPhase 首轮独立静审

- Variables审查三路径已root远端保存 `d2a26f41`。只读新树 `/workspace/agenteam-project-skills-cleanup` 的29dd基线后冻结 `lifecycle_authority.go`＋短卡，按Skills rev2 §16.3及原D08 source首轮未发现确定must-fix。新Skills-only/Delete/current Cleaning/domains/full stopped/required或pending/全部依赖门仍消费原Store活Tx及原Project SH；严格Project/operation/manifest/Owner/version读取沿原loader，未生成跨Tx授权或做清理。
- 本人8d6f98 actual0逆去新dispatch和两个私有函数后生产逐字29dd，旧decoder/Object Stop Inspect/Outbox/contract/SQL不变。新 `.agent-state/project-skills-cleanup-review/README.md` 记录首轮静核和待验闭包；本次无编译/Go probe/PG/socket/browser，不把静审当整块接受，作者测试仍准备中。当前root磁盘暂停编译照守，下一独立控制仅对稳定有意义场景，不重复旧Project全包。
- 后续稳定输入为生产415e0df5＋两新测试，有限离线接受，无剩余must-fix；3PGtop设计只读核真实BeginDelete与受控Cleaning/前驱事实分列、SH/EX/foreign/ended/取消及exact holder+key等待/原双callback尾，尚未编译/PG。pure fixture Outbox边已由作者运行前补齐，复用89499 race0/19sub和65938 vet0，不把较早读取的首版缺边报作未修问题。
- root单次小控制授权后，本人44702/c18c79 actual0/race1.025s，1top4sub，真实public gate/strict loader/postgres.Rows wrapper验证同原ctx/Tx/SH与两轮实际Close、漏锁零私有查询、第二次读失败不得复用首授权、完整participant流终尾错误仍拒且保cause/Close。首同process statvfs5,649,293,312B，固定Go/off/readonly/-p1/原独占Variables-independent缓存；Store/rows事实controlled不冒PG，作者产品运行前415e核定及返回后逐字未变。新authority_test.go/run.py＋README/本文4路径freeze，全部terminal，无真实资源。此前首轮2docs及OS窄审已root保存7d6f731a。

## Variables authority 首次 detail GET 增量有限独审

- root已保存D04 recovery两文档 `d80d2fb3`。新只读Variables `0c5a9e6d..2a603a3d` 的14冻结路径，限定authority首次main GET/noquery/200有限离线接受、无must-fix；原Session/transport owner尾、真实Variables live/adopt/SFC当前字段、同Request/XID/private正式decoder/Schema、旧native EOF/CL/两cancel/release/document end及采样join共同闭合。旧普通响应/其他路径门保留，生产/API/Go主验收/SQL后验与native/authority源未改，driver只加detail输入。
- 本人85150/5ca8ba actual0，5个实际lockedPW installer/helper差异控制：首错XID/晚DOM不得退休后升级、owned hook无法还原必须拒但cached调用保原、同Request failed后finished仍拒、额外参数原样委托不入候选。首c8dc37 hashbang与70378/3d174b非strict静默赋值断言误判均为本人setupFAIL，已修probe，不是作者产品缺陷。
- 原producer consumer/SFC10、作者observer116/typedGo51/旧诊断与TS复用，不重跑全矩阵；无PG/socket/browser/network或新Go编译。新增 `.agent-state/variables-detail-review/{controls.cjs,README.md}`＋本文3路径freeze供root保存，无live进程/资源。原Authority03 wholeFAIL不回填，新09候选与真实运行仍待。

## D04 SQL recovery 方法独立有限审查

- 前 Work GET 三路径已root保存/push `ece8121a`。本次只读D04 `sql-recovery-followup.md`（自述基线9ae2e0ab）与实际 Postgres/Secret/两 COMMIT-frame proxy 源，限定方法可实施；四top/十格为 final Unknown三、nonce一、maintenance三、实际锁竞争三。不启动任何PG/socket/被测child，不改作者提案/生产/现742候选，也没有用controlled Store伪造Unknown或重做旧纯控。
- 新 `.agent-state/secret-storage-recovery-review/README.md` 给出各格最小到达证据和拒例：真实Apply/nativeAudit后目标COMMIT、nonce high-water/rotation持久tuple，原Attempt/cause仅断实际暴露接口；exact backend/database/required advisory blocker；API返回、服务端COMMIT、原全锁join、测试goroutine与proxy/Store退休分列。需澄清nonce阈值只覆盖后继新payload、计数按真实producer基线；原合法历史行不作错误阈值要求。
- 结论只接受方法可实施性，不接受尚无测试/新入口或真实结果。D10 authority仍controlled，100/101旧矩阵、cross-epoch/rewrap-Cleanup并发和Owner/HTTP恢复未重复或扩认。本文及新README两路径freeze供root下安全窗口保存；无本人live进程或资源。
- 后继 `7ff214a8` 加 maintenance 的五个 recovery 测试已完成独立静审，有限源码接受、无 must-fix；4top/10格已核实际 Store/COMMIT-frame proxy、目标 native Audit/nonce/wrapping 到达、原 backend/精确锁竞争、Lookup/显式恢复及真正 callback/proxy/Store 尾。nonce 后继两 payload 与真实 Create/Delete 两 receipt 边界均落实，post-barrier StopAdmission 不会伪装 pre-gate Unknown。完整细节追加在原README，已直接交Model/root；没有Go编译或PG/socket/child实测，fixture早先43492的编译不外推新全源，plain net.Dial/wg未返必须仍记失败/缺尾。
- Project review四路径已root保存4e30ba15，P2 harness报告已dcaafa3e。随后Project `9c60199f` 后三single-top supervisor增量/controls本人91627/d3ced9 actual0（6 tests/3.224s），含冻结candidate只-list三次、坏UTF8/OSError仍完整受控尾、RUN/PASS各恰一和FAIL/SKIP拒、原tools逆投影。有限入口接受已交Work，非三PG业务通过；报告归原Project review README。本轮两个README及本文3路径freeze，无自有live/真实资源。

## D04 SQL 四 top 与 PG-only 入口有限独审

- Apply/维护四路径已root保存eaad50fd。新只读范围为作者fde3ecb5两SQL测试＋17969f85 fixture；复用已接受Go/29DDL，不重做旧矩阵。实际调用为PG Migrator/Store/Secret/Audit/native checker，D10权限与mapping仍明确controlled；无TestMain/MinIO依赖，两资源PG-only可行，耗时须实际验证。
- 首轮两测试must-fix已最小返修并核闭合：同Tx观察native写/Audit到达、精确Fault/唯一cause/hook计数，合法kind3错owner_id实际SQL修改和重读后真实checker拒绝；回滚后旧canonical/payload全封装前后像及计数保持。返修新增missing-lock错误码曾误要DependencyUnavailable，本人32793/f30506实际RequireHeldLocks/rejected控制race1.020s证明原poison优先得到InternalError+LockNotHeld；作者已只修该期望，新race候选57950/b929ba与两精确list96441/8b4a0e actual0。7bad5e核其余Replay/fixture/maintenance/产品/DDL未改，旧候选与错误预期不回填。
- 入口有限接受，无剩余本范围must-fix：core3恰17 RUN/PASS（5 rollback/9约束），maintenance1；原PG2资源、Go6m/105+15/123+3/TCP75及旧流程不变。本人55bb7e作者50控、016f03独立8 actual-main控actual0，坏UTF8/OSError/漏重PASS/driver2/input失败仍完成原受控Wait/desc/TCP/input/terminal；明确无实际子进程/资源，不冒PG尾。新candidate为27,867,046B，真实四top尚未运行，D10当前Owner/finalTx、COMMIT Unknown/并发仍不在此闭包。
- 新资产为原review目录 `sql_entry.py`、`sql_fault.py`，README与本文准确记录范围/命令；四路径停止写入供root保存。本人所有命令actualterminal、无PG/socket/network，未改作者源码。两组后继实际仍需root fresh grant。

## 三组 PG 首轮 4315：整体 FAIL，资源已退役

- root fresh grant只允许原70036业务binary（32,895,591 B）/97198 combo driver（15,394,731 B）与literal `^TestSkill(Migration|InitializationAdmissionUnknown|OwnerMetadataCurrentAuthority)$`。cwd仍 `tests/skills`，完整固定env沿下节Stop命令，仅driver换 `output/ai/skills/compile/pg-only-skills-combo-driver`、selector换该literal；另显式AGENTEAM_GO和固定MinIO变量，继承PATH。exec同process freshstatvfs=5,492,891,648 bytes≥5GiB，原Go6m/driver105+15/supervisor123+3/TCP75/PG两资源不变。
- AdmissionUnknown两个子（work/reserve）都在原 `admission_unknown_test.go:116` 聚合断言失败，top4.74s；后续物理步数、原COMMIT释放/恢复断言未到。Migration四直接子及30个约束负例业务PASS10.73s。Owner前11/12子PASS，最后deleting在 `owner_read_test.go:133` 夹具事务提交 `not_committed INTERNAL_ERROR` 失败，未到该子公开读门禁。保no-failfast顺序执行所有三top，不能把整体失败写为全矩阵接受。
- Go1094297 actualWait1，driver1093624 actualWait1/28.387966254s；outer4315 actualexit1，supervisor104.158s。nonce `00c9837e44f994fa2e3e3718668f927a` 对应container `e417cc92b910d0baa370e908f7fbf586f83331ee56c3aa8a9a7cfd0c41f10d3d`、network `7a46a29329bdecc27cdd2c2f5ea381273543b2475ede605aa927c2106f57445a` 两次clean；desc双[]，exacttop_count3/inputs_unchanged=True。**原TCP gate FAIL**：`STOP host TCP delta tail not empty: 1 rows`；旧监督器未保存baseline/delta行，不能归因或以后验空改PASS。
- actual终态后只读2d735a：两个精确Docker ID再次inspect明确不存在，原Go/driver PID双absent，run只owned.json，fixture/env/certs/runtime无路径/无symlink。现态5232ca无原PG端口33005；另有58998→8080 TIME_WAIT/inode0及59006→8080 ESTABLISHED/inode2491163，后者当前PID848/comm codex/ppid0。这不是原baseline/delta，不推断原失败行归属。自有执行与资源已全部退役并向root归还窗口；TCP原证据缺口与整体FAIL保留。
- 原件 `output/ai/skills/pg/pg-50d4ed0b91ef46a381a43b229a68f010.log` 与同名owned目录。70036原binary不覆盖。原业务输入没有变化，后续候选必须独立命名。
- 两项静态已证夹具错误：Admission将 `CauseDetails` 用reflect.DeepEqual比较，其Primary为含非nil闭包的CommandIdentity，连自身都不DeepEqual；须逐正式语义字段/Primary.Canonical与有序Related比较，保持原Unknown/attempt/原cause/空结果/原writer全部门槛，不打印私有key。原聚合断言没有逐字段记录，不能声称旧轮其它条件已过。Owner deleting写随机current_lifecycle_operation_id但未创建该operation，违反00013正式projects_operation_fk；须建立最小正式有效生命周期fixture前置，不关闭FK或冒真实BeginDelete。root授权这两个tests-only窄修；本文冻结时尚未写修复、未重编或再PG。

## 4315 后两夹具修复与安全 TCP 诊断

- 基线9e3fc022后只修 `tests/skills/admission_unknown_test.go` 与 `owner_read_test.go`。前者用验证过的Cause及全部正式语义字段比较：Primary.Canonical、有序Related、Kind以及Job/Delivery/Recovery全部标量，不用opaque JSON或DeepEqual；原Unknown state、原attempt、空result、writer PID等一律保留，聚合失败另列八个安全布尔值，不输出key/cause/ID。`repair-controls.py cause` 从实际新helper抽取并链接真实Foundation，8ceba1 actual0/14控含原DeepEqual自身恒false、同identity不同构造为true及所有改变/相关顺序/错误kind/无效cause拒绝。不是旧轮未采字段通过的证据。
- Owner deleting用 `seedReaderDeletingProject`：同UserEX+ProjectEX Tx读取现有archived/initialized/current Owner/version，写正式accepted Delete operation（project_version=current+1）、真实RequiredManifest与digest及required participant，再精确旧version更新Project到deleting并引用同operation。保留正式FK、rows1、原currentOwner公开读拒绝；仍是披露的上游生命周期fixture，不是实际BeginDelete。复用原已验Stop fixture的纯manifest构造，不改原Stop测试或产品。
- 新candidate `output/ai/skills/compile/skill-pg-admission-owner-fixed.test` 32,903,693 B：79510 race-c actual0、31755 integration vet actual0、26e8fd精确列AdmissionUnknown/Owner两top actual0，gofmt/diffcheck0；未PG。旧70036 32,895,591 B与97198 driver15,394,731 B都保留。命令沿本页固定Go1.27.1/offline/cache/GOMAXPROCS2/GOTMPDIR完整env，构建为 `go test -mod=readonly -p=1 -race -tags=integration -c -o output/ai/skills/compile/skill-pg-admission-owner-fixed.test ./tests/skills`；两tests-only修复待未参与者审，真实再运行须fresh grant，不默开或修改selector。
- 原supervisor有限移植已接受Runner57642926／Knowledge算法：只增闭集TCP样本与inode/PID稳定身份/可执行文件身份观测，原baseline/delta整集与两次空、75s尾、原Wait/资源/selector/root/default预算原样。观测扫描从原100ms pause支付≤20ms，不用owner豁免任何行；保留原baseline、最后两次循环样本与失败后再读各自时间，0600/exclusive文件，不输出argv/env/comm。549faf逐字核所有新函数和tail等于固定Runner source。
- `python3 .agent-state/skills-pg-combo/repair-controls.py tcp` b663df原18控实际0（从已保存Git blob复用原控制，仅当前树、原Skills基线8e7afde8、临时输出目录切到本树）；逆除TCP增量后整supervisor逐字原版。首780c74中17控过，1控临时parent目录不存在setupERROR；只修控制输出路径后18过，原失败保持。Work未参与者7e8dc1独立18控及7函数逐字核后有限接受TCP移植无mustfix；不评cause/fixture或Skills PG，不回填4315原TCP失败。
- 6路径全部冻结供root checkpoint：两test、supervisor、`.agent-state/skills-pg-combo/repair-controls.py`、本页、D10主卡。无执行/资源在途。Work方法整包独审3scope已root保存9e3fc022，当前授权仅离线；Knowledge独立两top候选2578a9ef仍待封闭工具映射，随后推进。

## 修后原三 top：61543 完整 PASS

- 两tests-only修复已root保存a5ebdd97。Knowledge未参与者37ed09 actual0：两文件逆投影逐字9e3fc022、真实Foundation语义41控、同Tx/UserEX/ProjectEX/Owner/version/accepted Delete/manifest/participants及FK静核有限接受；其首b73fab临时main错误cwd为自身setupFAIL，不是产品反例。未改生产；原4315失败及未采字段不回填。
- 79510候选32,903,693 B保持，8967f3只list实际发现原Migration/InitializationAdmissionUnknown/OwnerMetadataCurrentAuthority恰三top。新freshgrant后首exec b94b1b在同process核available=5,419,413,504 B≥5 GiB、原97198 driver15,394,731 B、Go1.27.1、固定MinIO及完整offline/readonly env才exec。cwd=`tests/skills`，沿下节Stop命令，仅binary换`skill-pg-admission-owner-fixed.test`、driver换`pg-only-skills-combo-driver`、selector换原literal；另显式`GOFLAGS=-mod=readonly`、`AGENTEAM_GO`和固定`AGENTEAM_MINIO_BINARY`，GOTMPDIR仍`output/ai/skills/compile/tmp`，继承PATH。原Go6m/105+15/123+3/TCP75/两资源/no-failfast不变。
- AdmissionUnknown4.44s两子、Migration9.55s四direct含30约束、Owner2.03s十二子全部PASS。Go1138755实际Wait0，driver1138198 actualWait0/24.40642665s；outer61543由517c30确认actualexit0。c8adce核两ID双clean、desc双[]、exact3tops、HOST_TCP双delta_empty、inputs_unchanged=True，supervisor84.760s terminal0；现场两PID absent、run仅owned.json，fixture.env/certs/runtime均absent且无symlink。资源窗口完整释放，没有其它自有live。
- 原件`output/ai/skills/pg/pg-b5a3e2c5a4d3407ca2d0e31187e67dba.log`与同名owned目录，nonce b31314bc453fb193a7d7c6707a9b3976；container 2dd932de142be20cd7fc93fc9316139d9561cbac402fa850b8171ca25608e0c5、network b27e1d3a7a72cac73a5da8e744180018edd2dc22c38de18f34cc465881980064。绑定当前d0a产品/a5测试修复，不重跑或外推旧产品；上游Human/Session/Project/生命周期是披露的规范seed、Object/Process受控，不称Login/Create/BeginDelete、真实D05此组、完整participant/root或独立动态验收。

## 下一独立清理 SPEC（rev2 有限接受，尚未实施）

- root授权推进精确CleanupAuthority/Project CleanupPhase工程规格并仅预留00028。原rev1三路径已保存 `e9e608f9`；Knowledge独审确认三mustfix：合法已initialized/published范围并不需要dropFK、历史work无界不能一Tx全删、删本域父表后D05 CleanupProject仍先取Maintenance plan会断链。原问题保留，未把rev1记接受。
- 修订的 [D10设计§16 rev2](../docs/development/work-items/d10-skills-initialization-design.md#16-下一独立结果skills-精确-project-cleanuprev2-spec) 撤回dropFK，保留00027全部约束；合法遗留仍同一发表Skill的旧未发布candidate/AbandonedAttempt。work与非当前attempt在已知物理完成后每批≤32，并保当前attempt和所有核心行；Unknown保原cause/attempt，按真实剩余事实续进。00028只预留joined-work索引候选，现主键id/partial-live索引不覆盖按Project历史扫描，需大历史查询计划确认，未写DDL。
- Project共享门禁仍由root指定owner补Skills-only当前CleanupPhase。另明确当前不存在的D05正式 `DeletedObjectMetadataPurger`＋唯一closed Access operation依赖：真实当前gate／opaque plan／同Store Tx／原Object与Upload anchor、私有物理完成及actualjoin前置，metadata历史每次≤32，最后Object/Upload与Skills核心同Tx清除。后序ProjectCleanup不再见此Object，避免无父映射Maintenance循环；缺口不能由root泛路由或DependencyUnbound假完成。Object既有gate/clean也须避免每次无界重扫已cleaned历史；root原则同意另建隔离D05实现，此实例不改Object/Project/App/D04产品或Runtime join。
- Knowledge TCP诊断移植临时优先窄审已完成：f8fe7d actual0，增量逐字Runner57642926，逆除分别逐字Knowledge666169b6／Runner3e7fd3bd，driver未改、11exact三表相同、预算/Wait/resource/input/gate其余字节保持。只接受已审方法的精确移植，无资源运行，不回填原56777或54818终态；已回root/作者，不另复制控制源。
- Knowledge续审确认三原问题实质闭合为明确D05前置，另要求stopWriters同样纳入有界发现／原writer终局重验。§16.6.2与验收补齐gate/stopWriters/clean/最后未完成检测全链、查询索引及历史后部尚活writer反例后，704bee独审有限接受、无本修订mustfix；root已保存三docs为1e8b5c67。仍保实际done／正式Guard证明；D05实际可执行SPEC／索引／退休证明、产品独审和真实组合是未实现前置。本次只回填限定结论，未实施Cleanup或SQL。
- D05上游作者核正式`backend/object-runtime.md`及D05设计后确认zero_marker首版永久保留；本次只校正§16.6“marker删除”措辞为payload清除、空marker核实及实际lease/work终局，不要求自动删marker，不解除Runtime停项。消费端仍exact ProjectDeleted/SkillRevision及同union Tx最后anchor；既有accepted Release分支整合与project_stops下当前cause窄许可由其上游SPEC负责，未跨改产品。

## 后续 Work 方法与 Timeline SPEC 独审

- 新Work `24ffd0ba..ff12b21a` 单Blocker Lookup五技术范围有限离线接受，无mustfix。本人ded763实际20控0/0unhandled，仅作者新增18Blocker差异及两独立真实Session/API/transport Resolve控制：held outercancel仍独占owner/第二facade busy不发HTTP，放尾后严格committed才完成；返回未resolved receipt拒绝。helper c95b1a六差异actual0，新增精确POST正例knownfailed不调用finished，GET/query/extra/非法Task/其它blockers路径仍拒并沿原finished。068d5f实核三消费AST逐字24ffd、五源冻结与产品/spec无diff。原102/80/41未变范围复用，无新browser/PG/SQL；07整体FAIL不回填。必要proof为`work-cut-review/blocker-controls.cjs`（owner/helper模式），说明在同目录README，Work树全程只读。

- 最新Work `5a49197a..0ad6e5b6` 普通方法整包独审有限接受无mustfix；仅offline技术，不回填06/05失败、不冒新浏览器/七资源结果。本人67453实际102控0/0unhandled（原98＋4真实Session held reader/stream时第二facade busy且不发HTTP，原尾释放后唯一typed成功）；helper4497/80、native13536/41实际0。6b8451 actual AST核originalBody/decodeOriginal/schemaProgram/四截断ledger逐字原版、5处originalBody调用不变、产品/Go sidecar无diff。独立proof为 `.agent-state/work-cut-review/ordinary-controls.cjs`，命令/边界见同目录README；首native8cd8b0 cwd缺TS setupFAIL保留后纠正。已回root及作者，新实际仍freshgrant；此3scope已root保存9e3fc022；Skills两个fixture修复与TCP诊断最新范围见上节。

- Work普通完成提案已由root保存5a49197a，技术仍d3322e3e；本实例只读核真实Session/API/transport、原proxy同body及固定PW1.56.1源码，接受其闭集方法可实施性，未接受新gate或实际owner动态证据。真实正常fulfillment位于actual请求／两层cancel和own-finally后；visible timeout/abandon只拒绝。必须先做真实源held-reader／outer-cancel、earlyreject／identity负控，再接受对应技术实现。当前expiry与显式finish共用retire，缺首次退休原因及退休时pending证明；晚finish/source:end不得把到期或未完成观察升级。精确正式依据与必要拒例见[方法审查](work-cut-review/README.md#普通完成方法的限定判定)。原06及05整体FAIL不变，未改Work源、未跑新browser/PG/socket。
- Timeline完整rev1（root已保存cf912e3e）后端SPEC有限接受，无mustfix，仅文档静核。四类严格原wire／独立TaskTimelineReader、每页当前Owner、原锁和(created_at,id)+Task.Version水位可由现writer/read端口闭合；真实writer共用无参SELECT clock_timestamp()，单一Store测试包装仅控制该成功Scan可产生同时间刺激，不手种正向历史。三精确PGtop、Rows/Tx/原goroutine实际取消join、未知族默认报错与无迁移边界保持。未编译／执行产品或PG，>200真实命令是否能在原105秒完成须实际验证，不能据静核扩大预算。

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
- 两工具／必要controls／本文4路径已由root保存 `c89aa120`。Variables未参与者窄审有限接受、无mustfix：8321c1原28控actual0；0a703d另以实际supervisor main验证driver exit2＋exact3top仍保2、读取OSError仍完整Wait[123]/desc双轮/TCP双空/input/terminal。两源逆投影逐字4442，无failfast；不是三业务PG通过。当前工具、controls和70036 binary保持该版本，只有本次SPEC三文档另改。新真实窗口仍需root fresh grant。

## 当前真实 D05 发布组合完整结果

- root fresh grant仅 `^TestSkillObjectInitializationPublication$`；70196 outer actualexit0，唯一top/3子Go2.85s，supervisor102.059s。exec同进程freshstatvfs=5,912,387,584 bytes≥5,368,709,120后才启动；实核Go go1.27.1、MinIO固定SHA `dc5298474f0bc87a068f0b1135c583bb1278c17c11c512212ed7644a238c89c8`、70036 binary32,895,591 B及全部cache目录。继承PATH前置固定Go，原root chain/Go6m/root540+60+3/TCP75/七资源不变；没有调用未实现Cleanup/Purger或Runtime。
- 原测试1069251和driver1067176实际Wait0且现场PID absent；nonce f533129b688f666158069193983c0276（Object）、7a80f2bc9106a12cf08d95215fb0da2d（outbound）、dcb7828ed9ff7e988b6041548f2f6921（PG）对应七精确ID双absent、三private双absent，runtime双empty、desc双[]，exacttops/actual_test_wait=True。HOST_TCP两次delta_empty、inputs_unchanged=True/terminal0齐；现场run仅owned.json/request.json/空runtime，三private无路径或symlink。原日志 `output/ai/skills/pg/pg-5c0cde23115d4d73aee1aa87a795007c.log` 及同名owned目录。窗口完整释放后才更新本文，不续跑下一组。
- 消费当前d0a16242产品及19353f4e测试组合，真实Skills、Object.Service.Initialize、MinIO和Audit/private witness全链；重建Skill service不换原Object/Upload/Attempt或重复Audit，真实Package EOF仍未Joined、Close后read lease与work结账，完整公开字段无法伪造private witness。上游Project/Creation/Human均规范种子，不是Project.Create/Login；ProcessGuard只构造资源、未绑定Runtime，不称foreign进程停止、Cleanup/metadata purge/完整participant/root或独立动态验收。旧8884 binary未执行，旧69925编译FAIL保持。
- 实际启动入口如下；命令前同一Python进程已执行上述freshstatvfs／Go／MinIO／binary核定，再exec该supervisor。任何再运行仍需root新grant。

```sh
# cwd: /workspace/agenteam-skills/tests/skills
env -u AGENTEAM_PG_FIXTURE -u AGENTEAM_PG_UNSUPPORTED_FIXTURE \
 -u AGENTEAM_OBJECT_FIXTURE -u AGENTEAM_OUTBOUND_FIXTURE \
 PATH=/workspace/toolchains/go1.27.1/bin:$PATH \
 GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOTELEMETRY=off GOMAXPROCS=2 \
 GOMODCACHE=/workspace/agenteam/output/ai/model-ui-recovery/go-mod \
 GOCACHE=/workspace/agenteam-project-variables-independent/output/ai/project-variables-independent/gocache \
 GOTMPDIR=/workspace/agenteam-skills/output/ai/skills/compile/tmp \
 AGENTEAM_GO=/workspace/toolchains/go1.27.1/bin/go \
 AGENTEAM_MINIO_BINARY=/workspace/agenteam-skills/output/ai/deps-minio/bin/minio \
 python3 /workspace/agenteam-skills/.agent-state/task-planning-recovery/pg_only_supervisor.py \
 --root-chain \
 --driver /workspace/agenteam-skills/.agent-state/work-owner-http/root_chain_driver.py \
 --binary /workspace/agenteam-skills/output/ai/skills/compile/skill-pg-stop.test \
 --run '^TestSkillObjectInitializationPublication$' \
 --output /workspace/agenteam-skills/output/ai/skills/pg
```

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
