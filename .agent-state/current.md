# D05 bounded metadata cleanup 当前检查点

- 下一有限历史单组入口已保存384551f3：`^TestObjectMetadataCleanupOldAttemptsAndStopHistory$`（1top、无子）。两tools各+1，原8f8658纯控制与预算/全尾复用。但Work静审发现成功出口未核实际deadline、proxy.finished早于原Body.Close/wg.Done；root据此撤销历史测试直接复用旧候选的准备状态，允许窄修及后继新候选。原37180277B/完整SHA保留为旧未修输入，不冒修后源码的已编或业务证据；该top从未真实运行。
- 历史返修仅 `tests/objects/metadata_cleanup_history_test.go`：首Stop、物理Delete及历史专用Stop循环在原成功返回时核原2s/总3s绝对deadline；本域helper保原40轮/5ms，不改其他已验case的共享fixture。原GET观察改等真实proxy.wg（包含原response.Body.Close及handler尾）再进入后续Stop/Release/Delete，保原2s尾并拒晚成功；既有proxy源码不改。543fdb仅gofmt/static/diffcheck通过，未Go编译或业务；当前冻结交Work返修复核，需另获编译/实际窗。

- 本组范围是65原failed attempt真实恢复、1001已关闭reader之后的第1002 held reader、原Stop/实际join、Release重放保旧cause/nativeAudit唯一。其四EXPLAIN是在方法返回后保留历史但pending已空的观测；不会补活项/五lane/全部FK或22索引成本结论。完整成本矩阵仍按卡§7.2后续准备，当前不改生产/28或其它停止项。

- **专用索引迁移 `2913` 完整 PASS**：精确 `^TestObjectMetadataCleanupIndexMigration$`，fresh／populated-27／atomic-failure-same-bytes-retry 三子分别2.38／2.60／2.87s，父7.85s；核22候选索引、journal/Goose一致、原数据及checks/FK/列/旧indexes不变、末DDL失败整体回滚与原字节重试。没有运行history/EXPLAIN或其它业务组，不据此接受索引成本。
- 本轮沿原37180277B／`46329/d8e8b8`构建来源和未变相关用例；原冻结记录**未持久保存候选fullSHA**，本次没有声称对上旧摘要。root明确接受该记录缺口后，以本轮新算并在启动时复核的 `158124c71d85bd4f1d0904754f0991dde34b4c1c0a63e68e9fe93dd1d0ac50d8` 冻结启动身份。`18246b` 首sameprocess UTC2026-10-10T00:08:43.355647Z、available5510782976B≥5GiB，fixedGo1.27.1／原完整offlineenv／继承PATH／固定MinIO，输出父`pg-migration01`原absent后才exec。
- 原session2913→`fc7cb6` actualouterexit0；`9b7f39` 按原owned.json/log核Go1369938 actualWait0、driver1368000 actualWait0、7ID×2共14absent、3private/runtime/desc双尾、TCP双delta_empty、exacttop/inputsame/terminal0，supervisor99.023s。末UTC00:10:32.952816Z、available5470879744B；本轮窗口完整释放，无命令/资源在途。日志 `output/ai/object-metadata-cleanup/pg-migration01/pg-610b615f20774e9eb93d6c415291b7ff.log`。
- 执行命令沿下方首批完整环境和root-chain入口，仅替换 `--run '^TestObjectMetadataCleanupIndexMigration$'` 与 `--output /workspace/agenteam-object-metadata-cleanup/output/ai/object-metadata-cleanup/pg-migration01`；启动额外核UTC、父目录absent、上述candidate完整SHA和固定Go版本。此输出现已使用；任何后继须fresh grant及另一个未用父目录，不自动重跑。Runner迁移入口独审54453/8403f1复用，生产/28/candidate未改。下面首批记录中的“迁移未验”保留其当时范围，以本节最新结果为准。

- **首批业务 `46857` 完整PASS**：本轮基于技术 `6c120e42`（产品 `eda849dc`、测试 `b44f46cd`＋已编migration源），复用 `metadata-cleanup-race.test` 37180277B。首同process UTC `2026-10-09T22:37:47.751662+00:00`，available6050856960B≥5GiB；Go1.27.1、继承NodePATH、固定MinIO及原absent输出父目录齐后才exec。两top分别3.77s、3.85s，Unknown两子各1.93s；Go1256650 actual_wait0、driver1254861 actualWait0、本人outer `eedb1d` actualexit0。`5c1400` 按原owned.json逐项确认7ID两轮14absent、private/runtime/desc双清、HOST_TCP双delta_empty、exact_tops/actual_test_wait=True、inputs_unchanged=True／terminal0、无STOP；supervisor98.961s。末UTC22:39:46.130854、available6044618752B，窗口完整释放。日志 `output/ai/object-metadata-cleanup/pg/pg-b7c4c3204c6f4699aa4d69553b988a81.log`，无命令在途。
- 本轮通过范围：65真实reader历史、32总额、同Tx重复拒绝整批回滚、最后4＋明确fixture父mapping一起提交或回滚；未转发和服务端已提交丢响应两种最终Unknown／原cause／新发现恢复／native Audit唯一。真实fresh fixture Migrator已执行当前嵌入SQL；专用migration3子、65旧attempt／1001历史、PUT跨source、执行计划／FK成本及Skills真实最后5核心仍未运行／未闭合，不外推root或Runtime停项。
- 本轮冻结候选来源：`output/ai/object-metadata-cleanup/metadata-cleanup-race.test`，race-c `46329/d8e8b8` actual0；`89992/1f41ef` 精确 list 恰两top。唯一入口为 `^TestObjectMetadataCleanup(BoundedHistoryAndFinalTransaction|FinalCommitUnknown)$`，不选择history/EXPLAIN/migration/pure top。Runner对固定6c输入独立有限接受（227b4b／root保存b95），两工具与两业务必要风险无mustfix；只读独审不冒本轮动态证据。
- 两既有root tools仅各新增这一闭合selector映射，逆去一行逐字 `b44f46cd`（`d9a12b` actual）：8键一致、config1正5拒、9 observer正负格各14次资源观察全部过。原6m／540+60+3／TCP75／7resources／actualWait／双尾／input门槛未改。本树supervisor保留基线TCP gate，尚未移入其它树的诊断增量。既有root_chain_test 7纯控首 `069460` 中nonroot timeout期待旧`[123,3]`而实际原实现`[123,1]`失败；原实现TERM分1s并与adopted wait共享3s。仅在内存将该旧控制期待改为`[123,1]`，`634459` 7控全过；未修改旧控制文件或监督器流程，失败不回填。
- 固定MinIO沿旧Knowledge已经冻结/验SHA的同文件硬链接到本树 `output/ai/deps-minio/bin/minio`，未下载或另复制大缓存；driver实际config再次核固定SHA。Go/cache仍下列固定环境。候选全包源码包括新 `metadata_cleanup_migration_test.go`：fresh／populated27／末DDL依赖缺失后原字节重试3子，核实际journal/Goose、22候选index状态、旧数据与checks/FK/列/旧indexes不变。它仅 `85164/c56b3c` 空selector编译 actual0/0.007s；未实际迁移或成本验收，首业务selector不包含它。

首批业务实际命令记录（输出父`pg`现已使用；**后继复现必须root另授fresh窗口与未用输出父目录，不直接重跑本命令**；首同进程空间不足5GiB则exit78不启动）：

```sh
GOMODCACHE=/workspace/agenteam/output/ai/model-ui-recovery/go-mod \
GOCACHE=/workspace/agenteam-knowledge/output/ai/knowledge/go-cache \
GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOTELEMETRY=off GOMAXPROCS=2 \
AGENTEAM_GO=/workspace/toolchains/go1.27.1/bin/go \
AGENTEAM_MINIO_BINARY=/workspace/agenteam-object-metadata-cleanup/output/ai/deps-minio/bin/minio \
PATH=/workspace/toolchains/go1.27.1/bin:$PATH \
python3 - <<'PY'
import os
root = '/workspace/agenteam-object-metadata-cleanup'
v = os.statvfs(root)
available = v.f_bavail * v.f_frsize
print('available_bytes=' + str(available), flush=True)
if available < 5368709120:
    raise SystemExit(78)
os.chdir(root)
os.execvp('python3', ['python3', '.agent-state/task-planning-recovery/pg_only_supervisor.py',
    '--root-chain', '--driver', root + '/.agent-state/work-owner-http/root_chain_driver.py',
    '--binary', root + '/output/ai/object-metadata-cleanup/metadata-cleanup-race.test',
    '--run', '^TestObjectMetadataCleanup(BoundedHistoryAndFinalTransaction|FinalCommitUnknown)$',
    '--output', root + '/output/ai/object-metadata-cleanup/pg'])
PY
```

- `e8592a4f` 已保存恢复前3 tests；恢复后实际 `8d9c06` 空 selector 编译 exit0（cached/no tests to run），没有调用 fixture。后续仅测试增量：每次 Stop 显式原2s、原3s总收敛限额不加长；1001历史后的 reader Close 仅接受 nil/原取消，核原 GET handler 返回及1002条 reader work 全退休；Unknown 原cause resume补父行 exact-delete/消失断言。
- 新 `tests/objects/metadata_cleanup_plan_test.go` 只观察真实 Service 发出的固定 bounded SELECT，保原 Store/live Tx、首调用nil cursor和数组副本；EXPLAIN 在业务方法实际返回后以独立2s执行，不占用或延长业务预算。已接入历史用例的 Stop-work/full-pending、physical-gate/full-pending，输出实际 JSON 供索引审查。当前没有真实计划；其余三Stop lane、大量其它Project、metadata各阶段、父DELETE全部FK trigger与PUT包仍是剩余矩阵，不能把查询 LIMIT 或 JSON 输出当成本接受。
- 新观察器 pure control `43787/b166ba`（启动c930ce）原offlineenv执行 `go test -mod=readonly -p=1 -tags=integration -race -count=1 -run '^TestObjectMetadataCleanupPlanCapture$' ./tests/objects` actual0/1.015s；含全包集成编译，但唯一执行是无资源的SQL捕获反例，未PG/MinIO/socket。`97251/1e42cc` 原env `go vet -mod=readonly -p=1 -tags=integration ./tests/objects` actual0；源码diffcheck `bb666b` exit0。没有旧会话缺口补认。
- Variables 对固定26df主体＋eda三产品增量独立有限接受：`46327/e70e7a` 实际Service＋显式Store 5top24sub及狭义例外控制通过，无本轮剩余mustfix。此为纯控/源码接受，非真实SQL/FK/rollback/预算证据；新28与当前活动集成测试不在其结论内，Skills最后核心消费端仍未实现。

- 环境恢复后实际核对 HEAD `eda849dc`，保留现存三个测试增量：`tests/objects/metadata_cleanup_fixture_test.go` 提取可选真实 Store/backend 构造；新 `metadata_cleanup_unknown_test.go` 准备最后四 anchors 与 fixture 父行同 Tx 的两种真实 COMMIT 丢响应；新 `metadata_cleanup_history_test.go` 准备65个真实失败候选及1001个真实已关闭 reader 后的活 reader/Stop/原 cause 重放。它们不是伪造 native joined/cleaned 的 SQL 成功夹具，仍使用明确 test-only 跨域门禁，不能代表 Skills 消费者实现。
- 该三个测试与本 current 现冻结供 root 保存。Unknown 片段在恢复前 `38461/3c598e` 仅执行原 offlineenv 的 `go test -mod=readonly -p=1 -tags=integration -run '^$' ./tests/objects`，actual0/0.008s；本次恢复未取得 history 新增后的编译或任何实际业务终态，故当前全源码组合仍是待检查 WIP。没有补跑真实资源或把缺失工具会话补为 PASS；Work08 原窗口的恢复由其 owner 负责。
- 接续先离线核这三个用例的实际生命周期/预算并编译，随后补32跨表与真实 Unknown、1001历史及22候选索引的 EXPLAIN/父 DELETE FK 计划矩阵。D05 当前没有真实 PG/MinIO、迁移28、执行计划或 Skills 最后同 Tx 组合验收；本轮无自有真实资源和在途命令。下面记录保留各历史阶段当时事实，当前范围以上述新状态为准。

- 基于9c4a2fd7新增正式路径`db/migrations/00028_cleanup_indexes.sql`，目前为22条待真实计划验证的索引候选；前序1..28连续、tx/Goose头、22索引名唯一且与前序无冲突，1e36bd静查通过，不是SQL执行/迁移验收。未编号draft已由正式候选取代并删除，旧26df655a保留其历史。
- 索引配套3产品增量：bounded pending候选加入NOT cleanup_gate，与完整终局partial谓词一致；metadata四历史查询显式按原UUID列排序，避免同名text输出排序；lease完整pending沿既有active/released闭集用active条件；ReferenceCleanup注释补已接受Skills精确分支。51423/d6d9ca受影响Bounded/Metadata/ObjectProjectAudit定向race exit0/1.043s，34645/6a4892两Object包vet exit0；均原offlineenv，无命令在途。Variables独审固定26df655a主体，以上增量另交其复核，不能混作已审。

- root已装配连续前序SQL，作者7ef673逐字核对：`00025_knowledge.sql=aaa408c8`、`00026_runner_control.sql=eea4ced0`、`00027_skills.sql=7cf7a58e`。它们只是本任务Migrator的稳定验收依赖装配，不表示Runner/Skills整模块或当前D05能力接受；正式28仍须真实计划/迁移/场景与独审。

- 新3测试源片段：`tests/objects/project_audit_fixture_test.go`仅增加可选planner/cleanup构造口，nil保持原接线；`metadata_cleanup_fixture_test.go`用明确test-only当前Owner/Project/cause/Skill/phase SQL端口与真实held锁，复用真正Object service/Stop/backend/native ProjectAuditAuthority；`metadata_cleanup_test.go`首top准备65次真实Read/EOF/Close历史、原2s物理Delete、跨表32/同Tx重复拒绝整批回滚、最后4anchor与fixture父mapping一起回滚或提交。fixture不冒Skills/D08生产Cleanup实现，Skills最后5核心联动仍待真实消费者。
- `12025/bce801` 原offlineenv实际 `go test -mod=readonly -p=1 -tags=integration -run '^$' ./tests/objects` exit0/0.008s/no tests to run，只证明集成源码编译；未启动PG/MinIO/socket或调用业务。270b06 diffcheck0。该3test+本current冻结，无命令在途；后续仍需65+旧attempt/1001历史、PUT FK包、真实COMMIT Unknown/执行计划及独立实施验收。

- 迁移归属更新：root已在Skills确认原占位无独立DDL后，将00028移交本人为共享cleanup索引唯一writer，候选`00028_cleanup_indexes.sql`。root尚待装配稳定00025/26/27，当前仅`.agent-state/object-metadata-cleanup/indexes-draft.sql`可恢复候选（未执行/未真实计划），不先写正式迁移或跳号。卡§7.1包括Skills joined work查询及initializations→work完整FK反查，首候选完整Project/id索引兼顾两者，最终按实际EXPLAIN删减。
- Stop lane1/lane4查询细化已落：reserved Upload直接给原Object主ID；pending native与active external lease两组各限32后按原transfer主ID合并。9893/a7b97a Stop定向race exit0/1.021s，限编译及既有pure，不证明SQL计划；cursor/fullpending/权限/actualWait不改。当前7路径稳定冻结，无命令在途。

- 在已保存 `6a1b4fd8` 后闭合通用 `cleanObject` 的本scope分派：只从真实 committed/revoked SkillRevision current attempt 读 canonical ProjectDeleted，缺失/损坏 anchor 不回退全历史；进入原当前 CleanupAuthority/同Tx门禁。独立子operation保原caller预算并实际结束本批，不把Recovery父operation的后续对象尾当本批join。gate历史查询按两类pending索引集合各最多31再合并，避免逐条扫描cleaned前缀。
- 新两纯top覆盖原cause/锚点损坏/其它Owner不入、unbound当前planner无history SQL，以及parent/child独立实际done、deadline相同、取消拒绝和原初始化私有admission。`36218/e7268f` 所选Bounded/StoppedSkill/Metadata/ObjectProjectAudit实际race exit0/1.042s；未PG/实际SQL计划，原env命令为 `go test -mod=readonly -p=1 -race -count=1 -run '^(TestBoundedCleanup|TestStoppedSkillCleanup|TestMetadataPurge|TestObjectProjectAudit)' ./internal/central/object`。当前无命令在途。

- 最新可恢复片段基于root已存 `c226b6dc`：`project_stop_store.go/project_lifecycle.go`与新`project_stop_batch.go`把五lane改为32个当前pending主ID、固定native指针及精确原work/command/Object/lease/transfer锁；删除原1001全历史投影和Object-wide native/grant/transfer更新。mapping摘要包含规范化后的完整原锁，新增锁会拒绝旧发现；同一个Process本轮仅ConfirmStopped一次。checkpoint仍需实际returned且origin Tx已退或精确死证；完整原projectStopPending不因cursor/诊断缺失变成allow。
- 原Prepare/native未建立、IssueDownload/grant未建立可发现原work与原锁，但没有actual join成功推断；预期已建立的native缺失或Object/process关系不匹配保持pending。Stop新3纯top和旧mapping控制实际 `74235/df8487` race0/1.021s；跨metadata/bounded/Audit选择组 `33491/7dd3ce` race0/1.041s（随后只删除Stop test无用import/占位表达式，再运行74235）。全量当前schema查询、1000+历史、原预算及Unknown仍未真实验收；全链尚待通用Recover有限分派及SQL索引/集成矩阵。
- `88804/0c653a`两包定向vet actual0（原env，`go vet -mod=readonly -p=1 ./internal/central/object ./internal/central/object/contract`）；`e3c911` diffcheck actual0，无编译或命令在途，无真实资源。此轮6路径freeze：本current、`object/{project_lifecycle.go,project_stop_store.go,project_stop_store_test.go,project_stop_batch.go,project_stop_batch_test.go}`。

- 当前实施WIP基线 `bbb7324a`（首四路径已root保存）。新增 `object/metadata_cleanup.go`／`_test.go`：private native graph discovery/fingerprint、同live Tx只能消费一次跨表32删除预算、PUT互引三行包/旧attempt两行包/最后四anchor、原Native Deleted+Audit状态不变式、本地actual-ended与原Tx结束否认。`access.go/service.go`仅配套private batch/Tx记账；真实SQL、FK顺序与执行计划未跑。
- 新 `object/bounded_cleanup.go`／`_test.go` 接入Skills精确Delete与Release：同批当前anchor＋最多31历史候选、保持原cause、有限Remaining溢出Busy、writer须io_closed、旧live cleanup claim不能覆盖。当前调用只在实际I/O返回且精确checkpoint known commit后取得私有finalize例外；`project_audit.go/project_audit_witness.go`消费同一private身份并重读终局，metadata仍要求完整returned/join，例外不可用来purge。
- `project_work.go`仅增加当前canonical ProjectDeleted＋真实CleanupAuthority同Tx门禁的stop后新claim例外，以及该新Skills调用实际退休尾继承原caller预算；不松原maintenanceAdmission、不以cancel/map空作proof、不恢复Object Runtime join停项。`reference_cleanup.go`的Skills revoked replay只核当前anchor，保旧AbandonedAttempt原因；`references.go`普通inspect原行为保持、新调用诊断总32。
- 已编译但仍未闭合：Stop五lane仍原100/1001历史fanout；通用Recover入口对本scope的有限分派仍待接；需针对有界SQL/未知提交/实际writer/两域最后同Tx的真实矩阵，索引尚无编号。此WIP不能称完整bounded provider、实际cleanup、Purge或Service验收通过。
- 作者实际 `83018/353b49` race exit0/1.059s：`go test -mod=readonly -p=1 -race -count=1 -run '^(TestMetadataPurge|TestBoundedCleanup|TestStoppedSkillCleanup|TestObjectProjectAudit)' ./internal/central/object`，沿本页完整offlineenv。新6top用受控Row/InTx核并发一次预算、foreign/ended/no-union、native pending/未返回/原Tx活、未签发plan、私有worker/fence绑定、SQL空排除集非NULL、原预算到期无新join及canonical cause/current denial；复用实际旧Object ProjectAudit控制，无PG/socket。前阶段47781/f71f35及29026/eeaf54亦actual0，后者仅当时metadata/Audit范围；最终diffcheck c41921 actual0。

- rev1 `e32b62c6`已获Variables独立有限设计接受（f4c64f实际FK控制）及Skills消费兼容核；root现授权本卡列明的Service/SQL实现，Runtime停项与迁移28不变。当前不是产品验收。
- 实施首片段：`contract/access.go`接入仅SkillRevision/ProjectDeleted的Purge operation，并逐字定向装配Skills已接受的同scope CleanupRelease分支；`contract/knowledge_cleanup_test.go`仅同步既有闭集期望，`contract/metadata_cleanup_test.go`补闭集/字段/原物理清理兼容及opaque exact operation/issuer/Tx拒例。无Service/SQL改动或真实资源。
- 实际定向race `70213/7a3267` exit0/1.032s：原env，`go test -mod=readonly -p=1 -race -count=1 -run '^(TestObjectMetadataPurge|TestKnowledgeCleanupRelease|TestAccess)' ./internal/central/object/contract`。后续实现metadata原生终局/同Tx单次预算，再接有界物理链及Stop五lane；初始四路径历史事实如下，旧“待独审”是前轮状态。

- 分支 `ai/object-metadata-cleanup`，基线正式 main `b2a7d0ab`。目标是 initialized Skills Cleanup 所需 D05 有界物理收敛/实际退休/最后同 Tx 元数据删除；不恢复 Runtime join 停项。
- 初始四路径：本文件、新卡 `docs/development/work-items/d05-bounded-metadata-cleanup.md`、`internal/central/object/contract/metadata_cleanup.go` 及 `_test.go`，已由root保存 `e61ed54d`。本次仅两docs细化为rev1；旧产品/SQL/迁移/root与纯合同源码未改。
- 实际发现：全历史 attempt 扫描；Stop LIMIT1001全历史投影循环；stop后的maintenanceAdmission拒新cleanup；main尚缺Skills分支已有Release闭集增量。另有revoked Release全cause相等拒旧AbandonedAttempt、尾部Maintenance/Inspect超出Skills许可及全量inspect。SPEC已列必要旧源与分支边界，后续先独审再接写域。
- 纯合同只新增可选typed口、Pending/Completed结果形状、总32上限、新operation常量；原AccessRequest尚不接新operation，Service未实现，没有stub或假成功。
- rev1工程方案：沿五lane改为每次32个精确主ID、固定native/work依赖及原锁；没有native的原Prepare/Issue合法准入只凭真实returned/death退休；完整projectStopPending不依赖cursor。metadata按真实00007拓扑删除至多3行PUT互引包、released leases/joined work、旧cleanup/attempt二行包，再最后四anchor；跨表32且同live Tx单次调用，不靠cascade。finalize区分本次I/O已经实际返回/已知checkpoint与外层整个operation尚未返回，metadata仍要求全部实际退休。
- 当前待独审的是完整§4 gate/原cause、§5不漏writer分页和§6最后原子性；真实SQL/索引/EXPLAIN及所有Service行为均尚未实现/验证。已列SELECT范围与外键trigger缺索引，须root另分新迁移号；00028仍属Skills预留，不先写DDL。
- 正式zero_marker永久保留，已与Skills协调措辞；最后Object/Upload/current attempt与Skills核心同Tx，Unknown保留原cause/attempt，absence不allow。
- 只离线检查获授权，无PG/socket/browser/network或自有真实资源。复用Knowledge独占cache，不建新GB缓存。

```sh
env PATH=/workspace/toolchains/go1.27.1/bin:$PATH GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOTELEMETRY=off GOMAXPROCS=2 GOMODCACHE=/workspace/agenteam/output/ai/model-ui-recovery/go-mod GOCACHE=/workspace/agenteam-knowledge/output/ai/knowledge/go-cache /workspace/toolchains/go1.27.1/bin/go test -mod=readonly -p=1 -race -count=1 -run '^TestObjectMetadataPurge' ./internal/central/object/contract
```

- 复用未变纯合同2top定向race `25403/8510e0` actual0/1.018s与包级vet `793dd1` actual0；原四文件UTF8/LF/链接 `5693d2`、diffcheck `6cd916` actual0。rev1只有文档变化，不重跑无差异Go检查；没有产品实现或真实资源证明。首次apply_patch将同一current写成delete+add，被工具整批拒绝，实际源码未变，随后按合法分步写入，不是产品编译失败。
- rev1文档检查 `55ab51` actual0（两docs UTF8/LF、两个本地链接）；`a98c68` actual0（diffcheck、限定两docs变更、Object源码相对e61ed54d无diff）。没有运行真实SQL、Object服务或ProcessGuard；全链可实现性仍待独立审查。
- 下一步：两docs稳定片段冻结供root保存，再交未参与者独审本卡＋两纯合同文件；Skills只核消费兼容。另一树Secret A已完成两mustfix返修并获Variables有限接受，技术与最终三交付docs均冻结待root正式整合，输入不混合。
