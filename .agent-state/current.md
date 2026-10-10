# Knowledge 文档树管理命令 HTTP 接续

- 工作树 `/workspace/agenteam-knowledge-tree-http`，分支 `ai/knowledge-owner-tree-http`，正式基线 `ce65714aac6eb4995a43fc427a2c77e6497470a7`。唯一负责人 `/root/work_ui`；Git/真实资源调度归 root。
- [SPEC rev1](../docs/development/work-items/d12-knowledge-owner-tree-http.md)已获 Runner 未参与者有限接受。四个 commandhttp 产品源（handler/input/wire/io）、四个相邻纯测试与独立 OpenAPI 已实现；作者 pure/race 10 top、86 sub 与标准 Schema 33 向量通过，同包 vet 通过。实现已获Runner有限独立接受（2top/9sub实际race0），作者PG首轮整体FAIL保留；修后Mutations＋Unknown定向整轮PASS，与未变Authority/Transactions原实际结果组成4top/13sub限定通过。本域native三top六sub也已完整PASS；最终未参与者正按实际结果和已接受的独立风险控制收口。范围为 title-only Update、Move、PrepareDeleteSubtree、DeleteSubtree 与原意图 Lookup 的独立 HTTP adapter。
- 已与 Knowledge read HTTP 负责人确认路由无交叠；新 `internal/central/knowledge/commandhttp` 独立构造、IO/DTO/Schema，不依赖或修改其未验 read 实现。只消费正式 B02/Account；不改领域产品/迁移/共享 root，不含上传、正文或生产生命周期绑定。
- 唯一新增写域见卡 §6；四个产品源已由 `3a063234` 保存；pure/Schema束已 `f1a1bd45` 保存且技术冻结；后继只新增本域PG/native测试。root已批准原Work缓存上的定向pure/race/vet，首次同进程fresh磁盘≥5GiB才启动；不新增GB cache，不启动 PG/browser/socket。
- 有限实际结果：pure 第二次实际启动 `19023` / `29d4f9` exit 0（3.543s）；首次 marker 类型名 `oc.Object` 编译失败 `63e42e` exit 1 已以正式 `oc.StoredObject` 修复，后续磁盘不足 `ee2f6f` exit 78 未启动 Go，均不回填。标准 Schema `2cb198` 33 向量无失败；同包 vet `75741` / `85896b` actual 0。没有本域在途命令。
- 可复现纯输入：Go `/workspace/toolchains/go1.27.1/bin/go`，保留继承 PATH；`GOTOOLCHAIN=local GOENV=off GOWORK=off GOPROXY=off GOSUMDB=off GOTELEMETRY=off GOFLAGS='-mod=readonly -p=1' GOMAXPROCS=2`；`GOCACHE=/workspace/agenteam-work-ui/output/ai/work-owner-planning-ui/implementation/gocache`，`GOMODCACHE=/workspace/agenteam/output/ai/model-ui-recovery/go-mod`，本树 `output/ai/knowledge-tree-http/tmp` 同设 TMPDIR/GOTMPDIR；`AGENTEAM_TREE_HTTP_SCHEMA_VECTORS` 为本树 `output/ai/knowledge-tree-http/schema-vectors.json`。原命令 `go test -race -count=1 -timeout=60s -v ./internal/central/knowledge/commandhttp`、`go vet ./internal/central/knowledge/commandhttp`；Schema `python3 .agent-state/knowledge-tree-http/check-schema.py output/ai/knowledge-tree-http/schema-vectors.json`。日志仍 ignored，不复制材料进 Git。
- 后续顺序：有限 pure/标准 Schema验证及返修、作者 native/真实 PG 与独立风险补集。全部真实命令须原 Wait 与资源尾齐，旧 B02 与本实例 Work/Timeline 的结果不能代本任务验收。
- 原 Work UI/Timeline 技术输入继续冻结；Work09 未获本次运行授权，不能因新树存在而改其输入或重开旧失败轮。

## 作者 PG 矩阵源码准备

- 新 `tests/knowledge/owner_tree_commands_{fixture,authority,transactions,unknown}_test.go` 与 `owner_tree_commands_test.go` 共5源已gofmt/差异检查；四top拟13sub，已离线race编译及精确发现，尚未运行。Account构造由固定 `1ee8b7e3` 的测试fixture独立复制私有命名，生产只消费正式B02/Account，不import read HTTP。正式Bootstrap/Invitation/Redeem/Login/Logout；Project初始化与Owner变更为明确上游fixture，权限仍真实原锁/Store，不冒Project.Create/Skills初始化/Transfer API。文档与Object/Audit/Outbox均经原B02真实发布产生，没有手种command/receipt。
- 矩阵限定五POST的真实改名/移动/删除与Lookup、当前Owner/归档/Session、两个Owner锁顺序与同key两Session、实际取消退出，以及原完整COMMIT帧代理的未转发/已提交丢响应和已plan最终回滚的in_progress。SQL效果和安全HTTP投影分别核对；native deadline/connection不由这些capability recorder证明。未知/失败原事实不改写。
- 该5源及本文已 `7fee6d3c` 保存。首同process UTC23:52:46.748858Z/5,560,705,024B 后原cache实际race-c `6269`→`75e170` actual0，candidate01=40,648,125B/SHA256 `6f9e06baf4437b899fdb373361e222b7793cca90f881830a1da9d0a69c88a226`；同命令实际-list恰四top、actual0。主机后采5,341,958,144B，低于门槛；TMP零子目录，无在途命令。后续native候选仍等待fresh恢复。不执行PG/native/socket，不改已独审产品/纯测试/Schema。

- `internal/central/knowledge/commandhttp/native_test.go` 使用integration tag＋显式native env门，3top/6sub拟验原生慢body/更早父deadline、正常keepalive、原Close失败、响应背压及断开取消原call。两处零响应检查仅接受原Read的EOF或ECONNRESET，timeout不能冒连接关闭。独占监听/连接/Serve/handler均要求原实际尾；Account/domain明确controlled，不能代真实身份/PG/defaultroot。native原实际结果见末节，PG首轮结果见下节。
- native候选与driver已串行离线构建：`79908`→`93cb7f` 原owner实际exit0，race-c、精确list、driver build各actual0。native01 `output/ai/knowledge-tree-http/knowledge-tree-commands-native-race-01.test` 为19,123,996B/SHA256 `91d1db8f3ee3eeba4f4a433504275f35a65060a92fa90970496d565571f77b92`；driver `output/ai/knowledge-tree-http/tree-command-native-driver` 为4,857,367B/SHA256 `359384f465d26acb48ed72a3b878e7553e7b8d96a29ea8b227eedc5bb08fb9a1`。原固定env/cache下，命令为 `go test -c -race -tags=integration -o output/ai/knowledge-tree-http/knowledge-tree-commands-native-race-01.test ./internal/central/knowledge/commandhttp`，该binary `-test.list '^TestTreeCommandsHTTPNative(ReadDeadlines|KeepAliveAndClose|WriteAndDisconnect)$'` 恰3top，继以 `go build -o output/ai/knowledge-tree-http/tree-command-native-driver .agent-state/work-owner-http/native_driver.go`。每次启动Go前同process fresh≥5GiB：UTC00:01:25.001357Z/5,737,144,320B，driver前5,713,465,344B，尾5,708,484,608B；主机净降28,659,712B不全归本构建，TMP零子目录、无在途命令。
- PG与native的三个精确入口工具及selector-controls已保存并冻结，作者132离线控制及Vars独立37负控均通过、有限接受；旧入口逆投影与原预算不变：PG原Go6m/root540+60+3/7资源/TCP75，native原Go90/driver105/outer123+3/TCP75。候选和list只是准备，须root另授真实窗口，不自动启动。

## 作者 PG 首轮实际结果

- 唯一四top首轮 `29190`→`fa6ad6` 已完整 **FAIL**，138.481s；启动UTC2026-10-10T00:16:39.475204Z，同process available5,460,840,448B≥5GiB、固定候选/MinIO全SHA及新 `/tmp/ktc-pg-01` 核齐。Authority三子、Mutations三子、Transactions四子和两种原COMMIT Unknown共12子PASS；唯一失败为 `Unknown/original_final_rollback_exposes_in_progress_not_a_receipt` 在原131行“same original pending command retry effects”。其真实rollback、in_progress无receipt/零读副作用、原重试HTTP成功、committed Lookup与当前Document比对均已到达；末门各计数/Activity未打印，不事后补数或归因。
- Go1381039/driver1379174原actualWait均1、原outer1；七exact IDs各两轮absent、private双absent/runtime双empty/desc双[]、TCP双delta_empty与inputs_unchanged/actual_test_wait齐，窗口已释放。原expected exact门false忠实保留失败，不自动重跑/native。必要安全原件见 `knowledge-tree-http/pg-01-failure.json`，普通日志 `/tmp/ktc-pg-01/pg-477a2456070142ecb5f6ade631696bee.log` 不复制；候选与产品保持原输入，下一步只按正式业务语义定位末门，其他12子按不变范围复用。
- 退役后静源确定本测试时间前置不符：正式Account `TouchActivityInTx` 将60s内Activity更新合并；新真实Login仅5.04s子时长，却沿用了旧B02“两分钟前SQL Session”用例的无条件After断言。原复合门没有分项值，不能据此重建原值或回填PASS。仅新 `owner_tree_commands_unknown_test.go` 的末子现补明确上游时钟前置：原User EX/真实RequireCurrentSession、同活Tx将原Session issued_at与last_activity_at同时平移2分钟，核due与原Session仍当前，known committed后继续；原凭据/身份/expiry/业务事实不改、不手种receipt。回滚Activity原值与retry实际前进仍要求，末门拆calls/安全计数/Activity三段诊断。其余两Unknown/其他12子及产品无修改；该修正已由Knowledge独立静核有限接受；尚未真实执行，旧candidate01不代表新输入。

## 定向重验准备

- 仅新增精确 `^TestKnowledgeTreeCommandHTTPUnknown$`，复用原1父3子名称、root链/七资源/input闭包；原四top入口与native不变。入口两工具和原selector-controls增量冻结，`python3 .agent-state/knowledge-tree-http/selector-controls.py --unknown-only` 实际 `56e61d` 39控0，包含缺/重RUN或PASS、FAIL/SKIP/错误top/UTF8、精确selector、失败仍走原Wait/reap/资源/desc/TCP/input尾；逆删仍逐字原7fee三个工具。Knowledge已独立复跑39控并逆差异有限接受；没有据此提升业务结果。
- 修后PG候选已编译并精确发现：`41927`→`a965d4` actual0；原固定Go/env/cache、同process UTC2026-10-10T00:27:34.864604Z/available5,654,011,904B≥5GiB，`go test -c -race -tags=integration -o output/ai/knowledge-tree-http/knowledge-tree-commands-race-02.test ./tests/knowledge` 5.652s，随后该binary `-test.list '^TestKnowledgeTreeCommandHTTPUnknown$'` 恰1父、actual0。candidate02=40,654,205B/SHA256 `bfd5f4aa49a5fdd84a1d808d505596357c5d2c92fffe2cdd9ed9aeea181db7ea`；尾available5,605,830,656B、TMP0、无在途命令，主机净降48,181,248B不全归本编译。candidate01/原FAIL保留；native01与driver原件不重编。
- Skills对六个PG/native测试源的方法独审指出并已确认收口两处判据：delete原输出须严格升序精确等于真实root/child两ID；native背压原Write错误须net.Error.Timeout且elapsed不早于原2s预算的3/4，仍保原上界。生产、原权限/事务/Unknown链及各自实际资源尾不变，六源方法已获有限接受；native断开是原TCPConn全Close，不冒half-close。旧PG01各结果与native01未运行事实不回填。
- root授权仅将受影响Mutations与Unknown合并为唯一下一实际selector `^TestKnowledgeTreeCommandHTTP(Mutations|Unknown)$`（2父6子），复用未变Authority3与Transactions4原实际结果，避免全四组重复。旧四组/Unknown/native入口仍原样保留；新增两工具分派与原selector-controls `--recheck-only` 在 `40520e` 61控0/旧三工具逆7fee0，Knowledge已实际复跑61控并逆差异有限接受，预算全不变。
- 两受影响候选串行离线 `45374`→`53487f` actual0：PG03前UTC00:38:23.958123Z/5,493,108,736B，原cache race-c6.461s及精确2top-list0，`output/ai/knowledge-tree-http/knowledge-tree-commands-race-03.test`=40,655,205B/SHA256 `56baeef055a0639193f65d163d7ad2c997b17372281ed03873d1aca22be40613`；native02前UTC00:38:31.477769Z/5,444,931,584B，race-c2.896s及精确3top-list0，`knowledge-tree-commands-native-race-02.test`=19,125,516B/SHA256 `2420da6c1d9ee768abd304a7192528fbec13a1cd1a36b3a114409cc62060ce56`。两次sameprocess fresh均≥5GiB，原fixedGo/off/Workcache/私有TMP；尾5,421,301,760B、TMP0、无命令在途，主机净降71,806,976B不全归本构建。原candidate01/02与native01均保留，native driver原件未重编。
- 已授权并完成的定向入口：沿上述完整env和同process fresh≥5GiB，先核原新 `/tmp/ktc-pg-02` 不存在，再运行 `python3 .agent-state/task-planning-recovery/pg_only_supervisor.py --root-chain --driver /workspace/agenteam-knowledge-tree-http/.agent-state/work-owner-http/root_chain_driver.py --binary /workspace/agenteam-knowledge-tree-http/output/ai/knowledge-tree-http/knowledge-tree-commands-race-03.test --run '^TestKnowledgeTreeCommandHTTP(Mutations|Unknown)$' --output /tmp/ktc-pg-02`。原Go6m/root540+60+3/TCP75、七exact资源各双退役与完整原Wait/input均保持；native另授窗口使用native02和原driver，不自动续跑。

## 修后 PG 定向实际结果

- `9527`→`b8dcb4` 原outer actual0，117.055s，唯一 `^TestKnowledgeTreeCommandHTTP(Mutations|Unknown)$` 2top/6sub完整PASS；Mutations7.33s、Unknown17.42s。原启动UTC2026-10-10T00:44:40.210078Z，同process available5,450,321,920B≥5GiB，candidate03与固定MinIO尺寸/SHA、继承Node PATH及新输出absent均核齐；全部输入冻结，没有自动重跑或native续跑。
- Go1417828/driver1416054原actualWait均0；七exact IDs各两轮absent、private双absent/runtime双empty/desc双[]、HOST_TCP两次delta_empty、inputs_unchanged=True，以及exact_tops/tree_commands_exact/actual_test_wait均齐。日志 `/tmp/ktc-pg-02/pg-2cd352f2538145f7bbcd89f675642d9a.log`；本人原terminal与尾核对完成，窗口已释放，无自有在途命令。
- 原PG01整体FAIL及其末门缺分项值永久保留；本次不回填原值。仅组合未变Authority三子、Transactions四子与本次修后Mutations/Unknown六子，形成作者4top/13sub的固定输入有限通过，不称当前HEAD单次全量。该次PG结束时native02尚待后继授权，其实际结果见下节；产品、预算、原B02/Account边界不变。

## 作者 native 实际结果

- native02原 `52400`→`0b3bce` outer actual0，67.850s，精确 `^TestTreeCommandsHTTPNative(ReadDeadlines|KeepAliveAndClose|WriteAndDisconnect)$` 3top/6sub完整PASS；Read2.16s、KeepAlive2.11s、Write2.13s，其中新增原Write Timeout与至少3/4预算门实际2.12s通过。原启动UTC2026-10-10T00:49:17.131702Z，同process available5,409,759,232B≥5GiB，native02与原driver尺寸/SHA、继承Node PATH和新 `/tmp/ktc-native-01` absent均核齐，没有重试或PG并跑。
- child1422975原Wait0、driver1422968原Wait0/7.457s，runtime_empty/private_removed、desc两轮[]、tree_commands_exact、HOST_TCP两次delta_empty及inputs_unchanged=True均齐；本人原terminal与尾 `519485` 已核，窗口释放、无自有在途命令。日志 `/tmp/ktc-native-01/pg-87731d1f665a4a1cbb33325eb9548141.log`。native仅原TCP全Close，Account/domain端口controlled，不能冒half-close或真实PG权限。
- Skills已收到两轮原日志与PG01不变七子入口，按其六源方法独审、Runner独立2top/9sub实际风险控制与当前作者结果作最终有限收口；不把原PG01整体FAIL改为PASS。交付目标现为正式Skills P2 main `3b7ed9da35844e3a367cc5e9da0cf424ab36499a`；清单加Skills独占、尚待验的唯一Owner/旧receipt独立PG源为19路径，不扩root/SQL；仍须root装配与同包编译，未宣布正式交付。

- 后继分工：Skills已有限接受两实际原结果的方法，独占新增independent test与两入口的唯一新literal/专属controls；作者不参与其预期设计，原4产品/5PG/native及当前候选保持冻结。旧PG01/PG02/native01 ignored binary已由root按精确路径回收，可按已保存固定源重建；原FAIL、源码、SHA与日志不删不改。P2共享Object增量仅精确Skills分支，旧Knowledge/Human路径保持；装配只需必要同包integration编译，未授权新的真实运行。

## Skills 独立补集恢复点（未运行 PG）

本节由未参与 command HTTP 产品实现的 Skills 编写；Work 已移交本节写权，不改前述作者事实。唯一新业务文件 `tests/knowledge/owner_tree_commands_independent_test.go`，精确 `^TestKnowledgeTreeCommandHTTPIndependentReceiptOwner$`，一 top、无子测试/新并发框架。

场景沿原真实 Account 登录、HTTP、B02：A 原 rename/Lookup 正向、改变原意图的 Lookup 拒绝；同一原 Store/Tx 先取得规范 User A EX、User B EX、Project EX，重验 A 当前 Owner 和 B 当前 Session，再精确改变已初始化 active Project 的 Owner 并重验 B。该 UPDATE 仅上游 fixture，不称生产 OwnerTransfer API。B 新 key 真 rename/Lookup 正向后，B 查询 A 旧 key/原意图须 IdempotencyKeyReused、A 旧 scope 须 NotFound；原持久 A/B completed command 整行只读快照、当前正文引用、公开 receipt、领域计数与两 Session Activity 分开核，不插入或修改 command/receipt/witness。

写权仅上述新 Go、两个既有入口工具及新 `.agent-state/knowledge-tree-http/independent-selector-controls.py`。driver 增唯一 literal，supervisor 增常量/1父0子集合/expected/原严格 RUN+PASS 支路成员；原 `tree_command_inputs()` 已 glob 全部 tests/knowledge，自动冻结新 Go。旧 selector、输入函数、7资源、Go6m/root540+60+3/TCP75 和原 Wait/双尾不改；旧 selector-controls 源不改。四技术已冻结，入口交 Variables 未参与者窄审，不把入口审当业务独验。

- 首 compile `72413/4fbdf3→e5c523` actual1：UTC2026-10-10T00:59:47.973933Z、fresh5,568,876,544B 合门，固定 Go1.27.1；新测试误将 SQL `CommandTag` 直接与1比较，编译失败且未生成候选。已仅改 `RowsAffected()!=1`。原日志 `output/ai/knowledge-tree-http/independent/compile-01.log` 保留。
- 首入口控 `440388` 是本控制误用别的 supervisor 的 descendants 日志标记，setup FAIL；改正式标记后 `f42620/fbc2da` 实际负控发现新增 literal 未进入严格 RUN/PASS 分支，缺 PASS 被误接受。已只补原 main 的 tuple 成员；`201deb` 实际26控/0，其中5格执行 actual main、仅 OS/进程/资源边界替身，核原 Wait540、14次资源观察与两次 private/runtime/desc/TCP/input 尾。UTF8、OSError 与 child exit2 均安全保失败，两工具逆去新增逐字等于 `fa7fcc6f`。没有 PG/socket/业务运行。
- 返修重编 `7b18da` actual78：UTC2026-10-10T01:01:22.628203Z，fresh4,523,102,208B 低于5GiB，未启动 Go/未建新日志或候选。当前 Model 独占实际 PG 临时资源，等其完整退役恢复空间后重新 fresh；不删除 cache、旧候选或跳过门。没有本域在途命令。

后继必要一次 race-c/list 使用本 Skills 既有独占 cache，**不写 Work GOCACHE**：

```sh
export PATH=/workspace/toolchains/go1.27.1/bin:$PATH
export AGENTEAM_GO=/workspace/toolchains/go1.27.1/bin/go
export GOTOOLCHAIN=local GOENV=off GOWORK=off GOPROXY=off GOSUMDB=off GOTELEMETRY=off
export GOFLAGS='-mod=readonly -p=1' GOMAXPROCS=2
export GOCACHE=/workspace/agenteam-project-variables-independent/output/ai/project-variables-independent/gocache
export GOMODCACHE=/workspace/agenteam-runner-control/output/ai/runner-control/go-mod
export TMPDIR=$PWD/output/ai/knowledge-tree-http/independent/tmp
export GOTMPDIR=$TMPDIR
export XDG_CONFIG_HOME=$PWD/output/ai/knowledge-tree-http/independent/config
go test -race -tags=integration -c -o output/ai/knowledge-tree-http/independent/knowledge-tree-commands-independent-race-01.test ./tests/knowledge
output/ai/knowledge-tree-http/independent/knowledge-tree-commands-independent-race-01.test -test.list '^TestKnowledgeTreeCommandHTTPIndependentReceiptOwner$'
PYTHONDONTWRITEBYTECODE=1 python3 -B .agent-state/knowledge-tree-http/independent-selector-controls.py --artifacts
```

首次每个新 build invocation 同 process statvfs≥5,368,709,120B/UTC/bytes 后才启动；私有目录先 mkdir 再 Go version，保继承 Node PATH。编译与 list 不执行 top；目前 `--artifacts` 尚不能通过，因为候选未生成。真实 PG 须 root 另授唯一窗口，不重跑旧四作者 PG 或三 native top；新独立补集尚未通过。
