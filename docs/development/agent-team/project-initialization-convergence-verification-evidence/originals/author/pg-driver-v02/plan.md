# Project Initialization Convergence — 真实 PG 提案（仅 scratch）

当前不执行 Go、input gate、镜像命令、fixture 或网络资源，不安装工作树。UI 作者独占当前真实窗口。本提案只有 root 明确移交后才可另存 `root_authorized_resources=true` 的 authorized-freeze；原 proposal 永不覆盖。README 未授，五技术源只在 candidate03 snapshot。

## 固定输入与隔离

- baseline 4089d131；candidate03 `0c7ab9129a9757cd7f2def5bfe3375e176910b935e047bc532f328ecf8f1886b`。前三源复用已接受 unit04/pure，PG fixture 保留原 Drain 错误，取消 bounded ctx 后 Background Drain 真等终局，120s top 包含此 Cleanup。
- overlay03 5个scratch新源 + UI `tests/account/project_owner_web_fixture_test.go`、`tests/account/project_owner_web_test.go` 两条空删除；不读取/指纹 UI 内容，不消费 frontend。所有继承GOFLAGS的编译均看到此有效集合，repo没有新5源。
- 复用接受 Audit `pg-driver-v01/closure02.json`（764645e5…7805）及本轮实际full20/dynamic图。有效3378文件/440目录集合；20生成main都固定，Project main无TestMain。完整原fixture仍编译/运行20包框架，但只有4个精确top被选中，其余不计通过。
- 本轮 full20 race图 `60d89453…02e33`；dynamic普通图 `784ff175…d8205`，两者extra0。三fixture helper、TestMain动态两cmd均在实际图。PG helper main:231–239、object helper main:209–213、outbound helper main:55–56/167–171、tests/process/process_test.go:31–74均继承父GOFLAGS；tests/process TestMain在无匹配top时仍构建两cmd，因此不漏该动态依赖。
- CGO0 outbound/server原实际图 `fbafa2b7…b8a6` 全部固定文件逐项SHA一致且不导入Project；`cgo0-reuse.json`只记hash复用，无Go/server重跑。本轮普通/race/两cmd已完成离线见integration-offline-summary01.json，不重复。
- 运行时仅读取接受原fixture描述符/CA等nonce-owned临时数据、正式embed连续1–20迁移。新2top不读取工作树源/schema/JS；旧2top也不调用旧B02 claim child的文件读取分支。旧newFixture/SQL身份与project_fixture.skills测试adapter明确保留来源，不能称生产Skills。没有Object Runtime停止probe、创建HTTP或新的成功gate。

## 唯一串行四轮

| 顺序/run | group | 精确top |
|---|---|---|
| new1 | new-facts | TestProjectInitializationConvergenceFacts |
| new2 | new-transaction | TestProjectInitializationConvergenceTransactionBoundary |
| old1 | old-mapping | TestProjectB02InitializationRejectsWrongMappingPlanAndActualSkillFacts |
| old2 | old-locks | TestProjectB02InitializationMergesCompleteLockPlanAndPoisonsMissingLock |

每轮 `sh scripts/test-objects.sh -run '^(唯一top)$'`，原helper `-tags=integration -race -count=1 -timeout=6m`，GOFLAGS `-mod=readonly -buildvcs=false -p=1 -v -overlay=.../integration-overlay03/overlay.json`。实时独立watchdog从该top RUN到PASS/FAIL含Cleanup最多120s，subcase不重置。前一轮须实际exit0、精确top PASS/watchdog完成、input前后同、7资源双清、owned实际wait双空、host尾观察清楚后才下一轮。任意失败先按原所有权终局清理、停后继，不自动retry、不延top预算。

执行入口为固定绝对Python `/opt/codex/runtimes/codex-primary-runtime/dependencies/python/bin/python3` 加本目录driver.py，分别传 `--name` / `--group` / `--frozen` / `--execute-authorized`；提前source-only gate使用 `--check-input-only`，仍待root授权执行。proposal中bool=false。

## 当前 live preflight 与退役

每轮在实际窗口新采PID/starttime、Docker全部原资源/完整Mount、空私有Docker配置、fresh ≥5GiB（前置和真正spawn前各一次）；两固定本地pgvector digest需inspect精确存在，缺失/不符即停，不pull。保原MinIO/Docker可执行SHA。原7拓扑=PG2container+1network、outbound1+1、object1+1，每域唯一32hex nonce；所有退出按精确resource ID两次inspect不存在、baseline非任务资源未变、无新资源/owned PID/runtime残留。

原subreaper/direct Popen.wait、adopted wait、watchdog线程真join、TERM→KILL仅已记pid+starttime owned尾、Force导致FAIL与原7资源门禁保持AST不变。任何限时返回不是join；新fixture自身必须Drain nil，原库/fixture局部Force限制不冒全部innerjoin，由实际test/helper进程终局证据分列。daemon/PID1 shim按当轮baseline→末cleanup身份集合记非owned、未wait，不引用旧累计值或称全机零。

TCP仅新增只读补充：实际spawn前两次/proc/net/tcp与tcp6五元元数据（table/local/remote/state/inode），运行中轮询差量，终局最多75s记录含TIME_WAIT的新增host行两扫清零。它不是完整短连接trace，也不据此宣称tuple都owned；不抓载荷、不ptrace、不根据TCP给PID信号。容器网络退役以精确资源删除为主。≤75s只是已终止业务后的退役观察，不延120s/6m；若新host差量未消失，driver失败停止续轮并标明可能非owned，禁止清理非任务网络。原资源与进程门禁不能被此补充代替。

## 原件保留与结论边界

unit01/02两STATIC测试缺陷、pure初版gofmt/CGO collector extra、candidate01 IC-PG-01、integration-format01单空格diff都保留。candidate03相对02只格式，全部普通/race纯与integration离线原件有界接受，不称真实PG通过。真实新top以直接合法SQL本域授权事实+registeredService验证此单端口，不声称Account/Skills创建E2E；旧两top仅writer/持久测试adapter兼容。管理员统一MeetingSummary、生产Resolution/Invocations/D24未绑定、ready503和三停止均不改变。

## v02仅TCP轮询预算修订

v01原件保持：独审STATIC发现固定末次sleep1可能跨75s。v02设单一deadline=start+75，轮询前检查deadline，sleep夹remaining，到期不开始新轮询；原75退役预算/120s top/6m包、原7资源与owned等待不变。只改retire_tcp_observation，无Go/产品/资源执行；实际观测耗时仍原样记录，不隐藏调度或读取开销。
