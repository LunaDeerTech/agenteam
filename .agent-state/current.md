# Skills 精确 Project Cleanup 当前检查点

- 工作树 `/workspace/agenteam-skills-cleanup`，分支 `ai/skills-cleanup`；正式 base `ce65714aac6eb4995a43fc427a2c77e6497470a7`。root组装76路径，2026-10-09作者只读核对 `9f5153` actual0：eaad50fd 的29个Skill Go/8集成源及4个Object初始化专用源、b37f9a49的22个Object Go/7集成源与26/27/28及D05卡、本Skills树最新两D10卡逐字一致；Project/Audit/Knowledge与main完全保留，无额外产品差异。此核对不是组合编译或动态接受。
- 本轮唯一writer为Skills agent；写域仅 `internal/central/skill` 必要dispatch/新cleanup实现与测试、`tests/skills` 新组合测试、本域D10卡/design及本文。Object/Project/SQL/App/D04只读；共享缺口交原owner，Git操作由root负责。不改00027 FK，不占00028，不恢复Object Runtime join停项。
- 目标按已独审§16 rev2：真实CleanupAuthority与Release/Object/Purge opaque plan、当前Project Cleaning gate及精确parent/cause/全锁、原子关闭serving与Release、原预算内真实物理终局/私有Audit、32历史推进及D05四anchor与Skill五核心同Tx最终删除；最终全六表空仍须当前授权/实际调用尾。禁止以unbound/stub或假proof报完整Cleanup。
- 可复用边界：原P2产品d0a16242→eaad50fd逐字未变，作者60950/58518/39205、96753、70196、61543各有限组完整PASS。P2独立02 69242/a45a4e wholePASS（2pure7sub＋真实1top4sub），01 wholeFAIL及旧4315不回填。上述没有验证本轮新Cleanup产品。
- Project当前CleanupPhase已正式ce657交付：作者3PGtop32sub＋本人独立76329/d17c95三子完整PASS，受控initializer/phase与真实权限/锁/同Tx重读边界明确；不是完整Skills/D05/root。
- D05消费基线b37（产品eda849dc）：46857/eedb1d首两top完整PASS，真实65reader/跨表32/最后四anchor与明确fixture-parent同Tx、两类COMMIT Unknown/native Audit。其65旧attempt/1001历史、PUT跨source、00028专用升级/回滚与EXPLAIN/FK成本、真实Skill最后五核心仍待验；28当前22索引候选只作连续测试依赖，不称正式交付。
- 恢复后的第一个产品WIP片段：新增 `cleanup_repository.go`（真实本域映射/关闭serving/32历史与最后五核心SQL）、`cleanup_authority.go`（exact CleanupAuthority及三种plan）、`cleanup_maintenance.go`（Claim/Checkpoint/Finalize父映射）、`cleanup_audit.go`（原ObjectDelete私有checker外层），旧 `object_authority.go/object_maintenance.go` 仅闭集dispatch。当前缺 Service.Cleanup 编排/实际call注册/原budget/完整Unknown与checkpoint及对应测试，故不能称Cleanup可用；新源尚未编译或运行。
- 环境恢复核对 `bd2318`：HEAD ba1470d6，正是2旧modified＋4新Go；本人无已提交在途session，实际/proc未见参数指向本树的进程。此事实不回填任何过去未知terminal。本次仅gofmt/差异检查，不启Go编译/PG/MinIO/socket；Object/SQL/Project不改。第一个WIP六Go＋本文freeze交root保存，继承P2/D05 PASS不外推本轮新产品。

## 完整库首实现与本域纯控

- cc68e6ab首片段基础上新增 `lifecycle_cleanup.go/lifecycle_cleanup_transaction.go/lifecycle_cleanup_checkpoint.go`，实现真正Service.Cleanup：同Project单调用准入/原2s与parent deadline、每Tx完整一次锁并集与当前CleanupPhase、已提交gate后才physical、actual返回后completed、每次最多32本域历史、D05最后purge与Skills五核心同Tx、Unknown保原attempt/cause、全六表空恢复不再调用缺父Object。没有新增Name或把单Skills当完整agent-skills-variables组合。
- `cleanup_audit.go`改私有immutable delegate并显式原样转发三个普通方法、安全日志/JSON；authority维护绑定修正正式Mapping方法和可选zero fence的primitive编码。Object/SQL/Project仍零改动。
- 新两测试 `cleanup_authority_test.go/lifecycle_cleanup_test.go`：7top/21直接子实际race0（37618/fdb3cd，1.185s），实际Service/Authority/typed plan及D05 native checker缺witness负控；Store/Project/Object事务/physical为明确doubles。覆盖当前拒绝/原Unknown、弱锁/错mapping/foreignTx、旧candidate与worker分列、closed阶段、原Audit上下文与拒绝、checkpoint闭集、gate回滚/两类受控Unknown、不重复Release、65 joined work每32、最后Object与本域整回滚/受控final Unknown、实际held physical返回前Stop不Joined。不是PG/真实D05/native Audit正向或真实COMMIT证据。
- 74993/6761ad首空-run race编译actual0；新增测试第一次8a417a actual1是测试stateID误传byte而正式helper需int，业务未运行，修后67212/a30d34首3top5子race0。该原编译失败保留，不归产品SQL。后继7top按上行真实结果。
- 全 `skill/...` race与vet 95821：测试9b286a包2.070s/contract1.288s，test actual0；vet a302b4 actual0。每次原命令同process fresh≥5GiB，最后UTC23:45:55.840359Z/5,805,240,320B；无新cache/网络/PG/socket。093dae diffcheck0，原私有go-tmp无子目录，无在途命令。
- 首完整库11路径已由root保存远端 `598bc02e`。Knowledge消费层独审有限接受、无本轮已确认mustfix，11239/194d92实际race0/1.127s、4top17sub：异常physical/Purge结果、原Unknown、早parent deadline、全空仍当前gate/实际held或cancelled reader/returned-Unknown work/partial core、1work+65attempt每32及rollbackUnknown恢复。实际Skills配明确Store/Object doubles；该审不包含审查者参与实现的D05，也不是PG/FK/最后真实双域提交。原作者7top/fullrace/vet复用。

## 真实组合测试源码准备

- 新 `tests/skills/lifecycle_cleanup_fixture_test.go/lifecycle_cleanup_test.go/lifecycle_cleanup_unknown_test.go` 已形成2top5直接子源码。fixture真正绑定同Store Project LifecycleAuthority、Skill Authority/Service、Object Service/MinIO及Audit；沿原初始化私有Audit再包本域Cleanup Audit路由，Runtime仍nil。Project创建/ready、accepted Delete与Cleaning阶段/其它participant stop是明确上游事实；本域Skill及Object Stop必须真实返回Stopped后才进入Cleaning，不冒完整root或BeginDelete。
- `TestSkillLifecycleCleanupPersistence` 三子：当前phase/version拒绝不写；实际关闭serving+Release后的原Tx失败整回滚；65次实际OpenPackage原EOF/Close产生历史，物理完成/native Audit一次、原payload删除或永久空marker、Skill work每32、D05 metadata每32；最后Object四anchor＋Skill五核心同原活Tx删除后注错整回滚，再真实提交/全六表空重建replay。不SQL伪造canonical/claim/witness/reader join，hook只在原真实callback后查看或拒绝该Tx。
- `TestSkillLifecycleCleanupCommitRecovery` 两子拟复用原未改完整COMMIT-frame proxy：gate及最后双域anchor分别只arm真实原backend，原Store返回Unknown且候选为空；独立连接看原基线、精确Project key的真实pg_locks/pg_blocking_pids证原writer锁仍持有，取消竞争者后实际Tx返回；释放原COMMIT、实际proxy writer join后重建Service恢复原cleanup ID/最终全空，原Unknown不回填。仅此两种晚提交边界，不宣称所有COMMIT失败模式均已覆盖。
- 三新源与本文已由root保存 `58073b67`；静核修正测试误写不存在的ObjectLock为正式AggregateLock(ObjectAggregate)，并在新Unknown测试优先注册Release/实际HeldJoined，使提前断言失败也先解除原代理writer再执行Object/Skill/Store Drain，未执行错误候选。Knowledge对该3源静态有限接受、无确认mustfix，核真实组合输入/32与65/最后4+5原Tx/COMMIT代理原身份与失败join；未Go/PG，也不独审其本人D05实现。
- 初磁盘低于5GiB时暂停编译；root回收已确认废弃旧候选并协调Work编译终态后单独授权。55305/27e659同process UTC2026-10-10T00:02:24.902016Z、fresh5,675,003,904B，固定Go1.27.1及下节env/-p1，实际 `go test -race -tags=integration -c -o output/ai/skills-cleanup/compile/skill-cleanup-integration-01.test ./tests/skills`。b0d352原outeractual0，compile0；随后原binary `-test.run=^$ -test.list='^TestSkillLifecycleCleanup(Persistence|CommitRecovery)$'` actual0仅两top。候选34,139,265B、SHA256 `c04c9f12c0ab02bfe6a3cf329d42c66f843c8e7437b9a1288c3f921d9f91aff8`；末UTC00:02:46.006526Z available5,587,689,472B、go-tmp为空。没有业务test主体执行，没有PG/MinIO/proxy/socket或在途命令。

## 唯一七资源入口准备

- root授权只在原Python adapter/supervisor新增literal `^TestSkillLifecycleCleanup(Persistence|CommitRecovery)$`，cwd=`tests/skills`；原三个fixture脚本原样exec，Go6m/root540+60+3/TCP75及七精确资源/actualWait/双private/runtime/desc/TCP/input尾不改，无failfast或新Go driver。新日志门要求两父五子RUN/PASS各恰一次，任何FAIL/SKIP、额外/缺失/重复项及缺原test Wait0拒绝。仅新selector的结果日志OSError/UTF8安全FAIL，原资源观察已完成，随后仍执行原TCP/input/outer尾；旧七targets全文逆投影不变。
- 原input闭包追加九个实际源码依赖：本三新tests，既有fixture_test/object_publication_test/lifecycle_stop_test/owner_read_test/commit_recovery_test五helpers，以及未改 `.agent-state/project-variables-independent/commitproxy/proxy.go`。原生产/embed/SQL/Go/MinIO/binary/三shell及旧input均保留；没有重写通用harness。
- 持久控制 `.agent-state/skills-cleanup/entry-controls.py`；命令 `PYTHONDONTWRITEBYTECODE=1 python3 .agent-state/skills-cleanup/entry-controls.py`，661814 actual0、4个control methods。核两工具逆去精确增量全文=58073、actual configuration闭集/旧7targets/九输入、完整及反序正例与缺失/重复/错子/失败/跳过/Wait反例；actual supervisor.main八格在明确process/OS/Docker/TCP doubles下验证原Wait540、14资源观察、双private/runtime/desc/TCP/input/terminal，包含实际非法UTF8、OSError、driver2、残资源/runtime及input变化。无真实子进程或资源，不冒真实尾。
- f9cc95 diffcheck0。入口增量尚待未参与者独审；本树固定 `output/ai/deps-minio/bin/minio` 当前不存在，真实预飞前由root准备原已验固定binary并核身份，不下载新版本。真正运行必须freshgrant，00028专用迁移/成本、D05规模及完整participant/root缺项保持。

实际命令均在下列固定env与本树cwd中；GOTMPDIR为本树 `output/ai/skills-cleanup/go-tmp`：

```sh
go test -race -count=1 -timeout=60s -v -run '^TestSkillCleanup' ./internal/central/skill
go test -race -count=1 -timeout=90s ./internal/central/skill/...
go vet ./internal/central/skill/...
```

## 后续固定离线环境

复用独占Variables-independent cache，不创建新GB缓存，也不写Work/VarsUI/Knowledge cache。获得编译授权时第一same-process statvfs≥5368709120B并flush实际UTC/bytes，`-p=1`；继承PATH前置Go保留Node。实际资源必须另有root fresh grant。

```sh
export PATH="/workspace/toolchains/go1.27.1/bin:$PATH"
export AGENTEAM_GO=/workspace/toolchains/go1.27.1/bin/go
export GOTOOLCHAIN=local GOENV=off GOWORK=off GOPROXY=off GOSUMDB=off GOTELEMETRY=off
export GOFLAGS='-mod=readonly -p=1' GOMAXPROCS=2
export GOMODCACHE=/workspace/agenteam/output/ai/model-ui-recovery/go-mod
export GOCACHE=/workspace/agenteam-project-variables-independent/output/ai/project-variables-independent/gocache
```

## 首次真实启动的前置失败（业务未启动）

- root已保存入口4scope为 `6a1c03b3`，Knowledge入口独审 `b43f66` 有限接受（记录 `e7ee272c`）；root已把固定MinIO硬链接到本树原路径，size109,289,632B/fullSHA与原身份一致。新Cleanup库/候选未改变。
- 获freshgrant后的首次执行 `b6fd24` actualexit1，无running session。same-process UTC2026-10-10T00:26:58.761607Z、available5,751,590,912B满足5GiB；候选34,139,265B/fullSHA `c04c9f12c0ab02bfe6a3cf329d42c66f843c8e7437b9a1288c3f921d9f91aff8`、MinIO/fullSHA `dc5298474f0bc87a068f0b1135c583bb1278c17c11c512212ed7644a238c89c8` 及实际Go1.27.1均通过身份核对。启动包装先检查新private不存在，却在创建它之前以该XDG环境运行 `go version`；Go建立本地计数目录后，包装的 `private.mkdir` 报FileExistsError。失败发生在exec supervisor之前，不是业务、driver或七资源运行。
- `451a01`原终态后只读核：`output/ai/skills-cleanup/pg/cleanup-01` 不存在；`runtime-env-01` 只有 `config/go/telemetry` 目录及2/16,384/0B本地计数文件；`/proc` 无argv精确指向本树路径的进程。没有PG/Object/outbound资源ID或业务actualWait可报告，也不冒完整七资源尾。原前置失败保留；未自动重试，已向root报告无live/可释放。
- root已授权本域包装次序返修，无须为这种常规修正逐次确认；后继 `cleanup-02/runtime-env-02` 先创建新private四子目录，再用原固定env做Go身份核验与exec。不改已冻结产品、测试、driver/supervisor或任何预算。当前root有Git网络保存窗，故尚未创建新目录或启动；保存结束的单条通知后，按原唯一2top5sub授权再次same-process fresh磁盘/身份核验再exec。
- Runner新main Default准备的独立窄审记录已冻结于 `.agent-state/runner-default-delivery-review/report.md`：四技术相对85832bd9，实际e8a67e原controls exit0，有限接受，无mustfix；不是新Default动态结果，不回填14016。只写本人记录，未改Runner源或启真实资源。
