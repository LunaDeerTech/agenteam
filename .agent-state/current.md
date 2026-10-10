# Skills Human Owner 目录与详情 HTTP

## 2026-10-10 新环境接续

当前：作者修后 PG 恢复02已完整 PASS（4top/12sub），原恢复01完整 FAIL 保留；native/pure/Schema/vet 未变结果复用。未参与者独立 Session 撤销恢复01业务 1top/2sub PASS，但原 TCP75s 尾残 1row，outer actual1，整体 FAIL，尚不能正式接受。产品/测试/harness 全部冻结，只有本记录与正式卡更新及原失败日志保存。

恢复输入为 `cc0e45ac2b851912e6b10db9f212026a5b7a9956`，新唯一作者 `/root/skills_http`；采用最新 `origin/main` AGENTS/团队流程。产品、Schema、测试和 harness 未修改，现均停写供独立审查。原 pure/race/Schema/vet、native 3top/6sub 完整 PASS 和既有有限独审继续复用；PG 4top/12sub 是本 HTTP 尚未执行的真实门槛。下面旧缓存、输出路径和原命令记录属于已保存的上一环境结果，新环境没有这些产物，不凭缺失产物重跑已闭合的 native 验收。

新环境只读发现：固定 `/workspace/toolchains/go1.27.1/bin/go` 存在，当前 Python 为 `/opt/codex/runtimes/codex-primary-runtime/dependencies/python/bin/python3`，本机 jsonschema 4.26.0 可导入。旧 GOCACHE、旧 GOMODCACHE 及本树 output 均缺失。root 指定 cleanup 独占恢复锁定依赖至 `/workspace/shared/agenteam-deps/go-mod` 并统一核 pinned PG17/vector 镜像；待其完成后本树只读该 GOMODCACHE、自有 `output/ai/skills-owner-http/gocache`，沿原 Go1.27.1/off/readonly/-p1 离线重建 PG integration race candidate 与 PG-only driver。无需重建 native 或引入 MinIO；PG 的 P2 Object 明确受控，真实 D05 证据仍单列复用。

精确待运行入口为 `^TestSkillOwnerReadHTTP(Metadata|CurrentAuthority|Transactions|CommitUnknown)$`，4top/12sub。重建及 exact-list 后由 root checkpoint、另授 PG 独占窗口；每条原 fresh 5GiB 门、原 105s+15s driver/123s+3s supervisor、原 Go6m 上限、实际 Wait、两个 nonce-owned PG 资源双退役和 private/desc/TCP/input 尾保持。新执行显式传实际 Python；旧解释器路径不照搬。当前没有本域后台命令或真实资源，未连接 Docker、数据库或既有基础设施。

默认 root、真实 Project→Skills 初始化与 Project Skills UI 的后继接缝已写入正式 HTTP 卡，由 root 协调候选装配与界面写者；本树不跨写根装配，也不解除 Object Runtime join 等四停项。

coordination 本轮独审指出真实 PG 方法的一项预期错误：Account `HTTPBoundary.CheckRequest` 先拒绝 `/skills/` 非规范路径，原 PG 表却期望 404。仅修 `owner_http_test.go` 该格为 400，并加入规范但未匹配 `/skills/a/b` 的 404 控制；子例/top 数保持 12/4，产品不变，纯控制的私有 Boundary 局部路由 404 不外推真实 Account。正式卡已明确该优先次序。此为未运行 PG 方法修正，不构造原产品动态 FAIL；PG 候选须从该修订重建并交未参与者复核。

root 已将方法修正保存为 `1627278a`。cleanup 恢复共享锁模块与 pinned PG 镜像后，本人仅离线恢复作者 PG：candidate `47912→9a87d0` actual Wait 0/222.181s（首 UTC 02:27:05.591758、19,782,430,720B），driver `23658→1dc26a` actual Wait 0/44.131s（02:31:06.768487、17,071,521,792B），exact-list `65291→ee231f` actual Wait 0/1.055s（02:32:06.380334、18,732,617,728B），恰原四 top。固定 Go1.27.1、off/readonly/-p1、自有 GOCACHE 与共享只读 GOMODCACHE；命令及终态见本树 `output/ai/skills-owner-http/{pg-build,pg-driver-build,pg-list}-recovery01.{json,log}`。没有业务测试或网络/socket。

新作者 candidate 为 `output/ai/skills-owner-http/skill-owner-http-pg-race-recovery01.test`，SHA256 `f860ba54b48e63d7d2f6f4e4e010d023cd78d91a5bcec4c8708cc29ea99c85a5`；driver 为同目录 `pg-only-skill-owner-http-driver-recovery01`，SHA256 `0e6cf0bd6fbfd28b016066f6d0bec1eb06405d913c883163f6309d63a9cdbf3c`。只读预飞 `skill_http_inputs` 可读取当前完整输入，新输出 `pg-owner-read-recovery01` 未使用。原 supervisor/selector/预算/全尾不改，等待 root 当前真实窗口释放后另授本组。

coordination 新增并冻结 `tests/skills/owner_http_independent_test.go` 与 `.agent-state/skills-owner-http/independent.py`；作者 candidate 在新增前完成，不含也不宣称验证该单 top 两 sub。该独验补集由其另编 candidate、另跑精确 selector，当前运行输入集合可稳定包含该源。作者无 Go/cache writer，暂交本树 `output/ai/skills-owner-http/gocache` 给 coordination 唯一写入以复用刚恢复的热缓存，独验编译完成后交还；本域尚无真实资源。

### PG 恢复01原 FAIL 与定向修复

root 在 `08fa0432` checkpoint 后授予唯一 PG 窗口。原 outer `60256→88c74b` actual exit1；首同进程 UTC 02:34:32.357634Z、18,687,987,712B，核固定 f860/0e6 binary/driver，新输出 `pg-owner-read-recovery01`，自有空 Docker config 与原 pinned PG17。四 top 均在共同 HTTP fixture 报 `DEPENDENCY_UNAVAILABLE`，Metadata/CurrentAuthority 尚未进入子例，Transactions 三子与 Unknown 两子同构造失败。原 child49669 actual Wait1、driver49004 actual Wait1/35.222s；container `8839c2bac2881064b4031d87e895b065ab78375a2fe31cec7668154699694d92` / network `dc5765e40ef804fb595d17113ef14ff6e369b20af4c0e532df675ba8cc04006a` 双退役 clean=true，私目录仅 owned.json、desc 双空、HOST_TCP 两次 delta_empty，inputs_unchanged=true、supervisor terminal1/95.790s。原全尾日志脱敏检查后按原字节保存至 [失败原件](skills-owner-http/pg-recovery01-failure.log)，不回填成功；窗口已释放。

coordination 未参与者与作者只读定位：Knowledge 真登录 fixture 在 Authority 构造后调用 `accounts.Initialize`，Skills P2 seed fixture 没有此需求，新 HTTP fixture 漏了这一步。`account/browser.go` 的 NewAnonymousContext 要求 account_key_registry UPDATE 恰一行，因此真实 Login 的首匿名上下文确定需要正式启动事实。仅修改本域 `owner_http_fixture_test.go`：NewAuthority 后调用正式 `accounts.Initialize(testContext(t))`，并给 Authority/Initialize/匿名上下文失败加安全阶段名；不改共享 P2、HTTP 产品或 Account，不手种凭据/允许授权。已 gofmt/diff-check，技术修订待独立复核/新候选，动态因果待新窗口验证。

coordination 独验 candidate/list 已离线 actual0、一 top 两 sub 仍未执行；其原补集 wrapper 曾将新 selector 误入 native observer，已限定 namespace 改为 PG 并通过11离线控制，保留为准备方法修正。热 GOCACHE 已归还作者，目前无后台 Go/cache writer或自有真实资源。作者/独验均须从修后 fixture 重新编译，复用原 driver、pure/Schema/vet/native 的未变证据，不重跑无影响通过项。

修后 fixture 经 coordination 窄复核接受，root 保存为 `3b97fe69`。仅一次新 integration race-c `54270→cf4d39` actual0/9.476s（首 UTC02:40:01.009994Z、18,687,721,472B）生成同一 `output/ai/skills-owner-http/skill-owner-http-pg-race-recovery02.test`，SHA256 `86fd92431a3c928946445873c3b574b05bc55734aecef7d59e8fd7f3abb8a05f`。随后 `19561→245265` 按每条 fresh 门分别 exact-list 作者四 top 及独验一 top，两个子进程 actual0/1.048s、0/1.199s（17,726,709,760B / 17,726,697,472B）；不执行测试体。命令与终态见同目录 `pg-build-recovery02`、`author-list-recovery02`、`independent-list-recovery02` 的 json/log。原 driver 0e6 保持，作者/独验将分两个实际窗口使用同一新候选；原候选仅保原失败复现，不再当作修后证据。当前无 Go/cache writer，等待 root 另授真实 PG 窗口。

### PG 恢复02原完整 PASS

root 在 `a5b385b5` 记录保存及前一实际窗口完整释放后授予本组。原 outer `30222→fc38c6` actual exit0；首 UTC02:51:44.144001Z、17,883,529,216B，固定86fd candidate/0e6 driver、原四 top selector，新输出 `output/ai/skills-owner-http/pg-owner-read-recovery02`、独有空 Docker config。Metadata6.76s、CurrentAuthority6.98s、Transactions19.30s、CommitUnknown14.78s，4top/12sub全PASS、0skip，实际Schema2控/HEAD18无体也通过；原子进程67853 actualWait0，driver67244 actualWait0/55.293s。

原日志 `pg-owner-read-recovery02/pg-dd0620f7eafa44249fd7153ddff19e20.log`：container `a975eb70ddc2379cce573b440177b596468345ebb1510cb5b1fa60707820759e` / network `d9ec675431143a04247d5766e85e3cb3fb5d3ed408a3fa7604b5f394062fc1aa` 两次精确退役clean=true；私目录仅 owned.json，desc双空、SKILL_HTTP exact_cases_wait_private=True、HOST_TCP两次delta_empty、inputs_unchanged=True，supervisor terminal0/114.362s。该原尾齐后才接受本组完整PASS、向root释放窗口；无追加采样补原尾或自动重试。当前无真实资源、Go/cache writer。

结果支持原失败夹具的正式 Initialize 修复；原恢复01仍为FAIL，作者业务/P2受控Object/上游Project seed边界不变。作者4top结果不能代替后继独验。默认 Object 授权分派及私有 Audit witness 的必要根装配链已补进正式卡，默认root/UI不在本HTTP接受范围。

### 未参与者独验恢复01原整体 FAIL

coordination 在 root 另授唯一窗口使用同一86fd候选/0e6 driver、其冻结独立入口，执行 `^TestSkillOwnerReadHTTPIndependentCurrentSession$`。真实 HTTP 身份认证后，在原 P2 读取 Tx 继续前由正式 Account.Logout 撤销同 Session，再放行并核 GET/HEAD 详情返回 SESSION_REVOKED；两 sub 均通过（11.73s/11.79s），原1top/2sub业务PASS/23.52s。该场景及实际执行属于未参与者独验；本人只 peer 方法核与保存结果，没有将作者测试冒充独验。

原 outer `50063→96a6c8` actual1；child71642 actualWait0，driver70983 actualWait0/38.527s。container `9ea95caaf6b0667b10b7b35b63839963d714e1f3739786a85648f1f783368a2e` / network `31102a03c1d1b68b95dff439b303d58402eaee1030d113b9829c570c3fa556f5` 两次精确退役clean=true，private/exact_cases_wait=true、desc双空、inputs_unchanged=true；但原hostTCP75s尾只记录 `STOP host TCP delta tail not empty: 1 rows`，没有两次delta_empty，supervisor terminal1/113.891s，故整体FAIL，不能升级正式独验接受。原日志只存残留数量，没有该行的身份或状态，不能据此归因于产品、PG或外部连接。

原1988B日志在安全字段检查后逐字保存至 [独验失败原件](skills-owner-http/independent-recovery01-failure.log)，与原 `output/ai/skills-owner-http/independent-current-session-recovery01/pg-2dfcd4360b6e437293453b006c2335ad.log` 相同；不后采样、不补原终态、不自动重试。窗口调度已交回root，作者4top/12sub完整PASS继续有效。后继只围绕未齐资源尾由root安排诊断或新窗口，当前没有本域Go/cache writer或真实命令。

## 已保存的原环境证据

工作树 `/workspace/agenteam-skills-owner-http`，分支 `ai/skills-owner-http`，基线正式 main `3b7ed9da`。唯一作者 Runner；Git 保存与真实资源窗口由 root 负责。

当前可恢复片段：新 [短规格](../docs/development/work-items/d10-skills-owner-read-http.md) 已固定两 GET/HEAD、空 query/body、安全八字段、真实 P2 ListSkills/GetSkill 与当前身份/Project Read gate、原 2s/I/O 尾。Skills 对短合同只读核真实P2/Project/Account、字段上限与3链接（45bf1c）有限接受，无must-fix；不是产品实现验收。现独立产品3源 handler/wire/io、5纯测试源、Schema及其本包testdata/schema.py已形成，固定gofmt/JSON解析完成；本包初次pure/race实际40059→a88f0b exit0：11top/47sub/0skip，标准Schema21控及18HEAD无体状态通过，原日志 `output/ai/skills-owner-http/pure-race-01.jsonl`。没有 PG/native/HTTP 资源。作者在授权新包内实现，不改 P2/Project/Cleanup/root/迁移。四停项不解除。

允许新 `internal/central/skill/http/**`、专用 `api/openapi/skill-owner.json`、`tests/skills/owner_http*`、短卡/current；不含包流/安装/分配/UI。原完整 P2/00027 的适用实际结果可复用，不把它提升为本 HTTP 已通过。

root 已交接唯一可写热 GOCACHE：`/workspace/agenteam-project-variables-independent/output/ai/project-variables-independent/gocache`；Skills 确认无在途或排队 writer。只读 GOMODCACHE：`/workspace/agenteam-runner-control/output/ai/runner-control/go-mod`。后继离线 Go 固定 `/workspace/toolchains/go1.27.1/bin/go`，PATH 前置并保原 Node，GOTOOLCHAIN=local、GOPROXY=off、GOSUMDB=off、GOTELEMETRY=off、`-mod=readonly -p=1`；同进程先打印 UTC/available 并要求至少 5,368,709,120B，否则不启动。不得新建大 cache；PG/socket/native 仍须 fresh grant。

下一步：已过 pure/race/Schema/vet 复用；native 3top/6sub 已在唯一真实窗口完整通过（24202→c0f5e4），PG 候选/driver/精确发现已就绪，等待 root 另授 PG 窗口。源码98bb09f0保持冻结，尚无实际 PG 权限/事务结果。原I/O实现仅从正式Knowledge HTTP复制改package，传输控制保实际 callback/领域调用尾；native 局部领域控制不替代真实 Account/P2/SQL 组合。

离线验证：首次同process门采 2026-10-10T01:21:41.992595Z / 5,409,042,432B，通过后固定Go运行 `go test -mod=readonly -p=1 -race -count=1 -timeout=60s -json ./internal/central/skill/http`，显式 `AGENTEAM_SKILL_HTTP_SCHEMA_PYTHON` 指向启动包装的实际 `sys.executable`，实际终态0。随后vet预飞 e0a7ad 于01:22:24.529801Z采5,287,940,096B，exit78，未启动Go/vet；不据前一次门冒后一次通过。当前无在途命令/cache writer。产品3源与短卡未因纯控制改变，PG/native验收仍待。

后继稳定片段：新增 `internal/central/skill/http/native_test.go`（Linux TCP 3top/6sub）及 `tests/skills/owner_http_fixture_test.go`（PG2 组合 fixture）已 gofmt、未实际运行；后者尚未编译。native 只消费私有局部控制，包含原 2s/更早期限、真实同连接、Close错误和原领域调用尾；合法最大单Metadata背压通过SYN前设置接收buffer建立，未伪造大响应。PG fixture复用真实P2数据库初始化及既有受控Object，正式Account Bootstrap/Invitation/Redeem/Login/HTTPBoundary、Project gate；Project/Creation/ready是明确上游seed，不冒Create/D05物理发布。root已认可该最小边界，metadata HTTP零Object消费，原P2真实D05结果单列复用。

Skills对固定890d632f产品实现独立静审 eb56ef 有限接受，无must-fix：10技术源与该点相等、真实实例/参数、严格输入、完整安全投影/Unknown和原I/O尾。仅只读检查并复用作者pure，不冒本人Go或PG/native验收。后继native/PG不在该结论。

vet后继单次获root恢复授权：35671→45b0c1 actual0，首同process采2026-10-10T01:25:49.002528Z / 5,388,660,736B，固定同cache/env `go vet -mod=readonly -p=1 ./internal/central/skill/http`，日志 `output/ai/skills-owner-http/vet-02.log`。涵盖包内native源码类型检查，不执行socket；原vet首次门78保留。当前无在途命令、无自有真实资源。current与上述两新源冻结给root；另两PG业务matrix仍在新文件实施，未纳本片段。

后继两业务矩阵现冻结：`tests/skills/owner_http_test.go`、`owner_http_transactions_test.go`，4top/12sub 源码，gofmt 051635、静查 0894dd；尚未编译/执行。Metadata 和 CurrentAuthority 消费原 fixture 的真实 Account/P2/HTTP Boundary，核 GET/HEAD/Schema、严格请求、当前 Session/Owner、合法未初始化与 missing-publication 差异、Archived/Deleting；原八业务表快照未观察 Session Activity，后续独审指出缺口并按下项返修。Transactions 以原真实 Tx/PID 与精确 User SH/EX 两序，以及 Skill relation AccessShare/AccessExclusive 原 SQL 等待观测取消与 Tx 实际退役。CommitUnknown 复用现有完整帧代理，按原 `skill-read` JobCause 选择同物理只读 Tx，not-forwarded 与 committed-ack-lost 两方向保实际结果/上游 join，不发布候选；后续 GET 不确认旧 Unknown。没有改变产品或原 P2 fixture/代理。2026-10-10 当前只读盘采 5,324,996,608B 低于 5GiB，未启动新 Go 编译，不把这次采样当已运行的 exit78。两 matrix + current/card 给 root 保存；原 native/PG fixture 已存6fa6c733。

Skills 对新测试方法审发现两处判据缺口（非产品实际失败），本轮两源窄修已 freeze：native 原 Write/Flush 委派保留原返回，handler 原终态后必须从原输出 error `errors.As(net.Error)` 且 `Timeout()` 绑定实际截止；不只用 elapsed+abort。PG facts 新增仅 Session ID/last_activity_at 的第九安全快照，Metadata 在真实 User EX 下先将本 Session 合法 issued/activity 置 120s/90s 前，实际核已过原60s节流且仍有效，再由真实 GET/HEAD 证明 Activity 不动。956b03 gofmt/diffcheck0；未跑 Go/native/PG；Skills e43905/909441 限定只读复核接受，无剩余确认 must-fix，4top/12sub 与 native3top/6sub 仅方法接受。产品3源不变。原热 cache 已明确停写，root 临时交 Vars；归还前本域不运行新Go。

两精确入口最小源码现冻结（尚无新 driver/candidate）：原 `.agent-state/task-planning-recovery/{pg_only_driver.go,pg_only_supervisor.py}` 与 `.agent-state/work-owner-http/native_driver.go` 仅新增 `^TestSkillOwnerReadHTTP(Metadata|CurrentAuthority|Transactions|CommitUnknown)$` PG 和 `^TestSkillOwnerHTTPNative(Deadlines|KeepAliveAndClose|BackpressureAndDisconnect)$` native。PG 保 Go6m/105+15、sup123+3；native 保 Go90/driver105/sup123+3，原TCP75/Wait/reap/资源尾不改。新 exact 观测核 RUN/PASS 恰父+全部12或6子、原child PID/Wait与driver成功、PG两精确ID双退休/private空或native原owned/私目录空；读取异常固定FAIL仍走原尾。输入只冻结固定binary/driver与本域test源码、runtime Schema helper/两JSON/实际Python（不全仓hash），末尾重取集合捕获新增/删除，PG同解释器显式传入并固定tests/skills cwd。

新 `.agent-state/skills-owner-http/entry-controls.py` 实际644f75 exit0/107控：两工具去唯一增量字节等784ecf02，完整supervisor AST逆投影等原；相同selector/budget、实际spawn参数/env、闭集合/坏UTF8/每子缺失/重复/错Wait/私目录、真实临时文件输入变化，以及actualmain完整尾8格（明确OS/process/TCP替身）。没有Go/proc/PG/socket。首2ad595系控制向已打开日志外追加非法字节却被原文件offset后续覆盖，探针修seek后保该setup失败；014b96是逆投影漏移else-if闭括号，按实际限定差异修正后通过，不冒产品红。入口三源+control/current停止写，Skills 2e5de4 实际107控0与静核有限接受，无must-fix；等待root窗口尾checkpoint。本域无实际资源。

卡要求的 Problem 标准Schema小缺口已补：仅 `internal/central/skill/http/schema_test.go` 与 `testdata/schema.py` 增量，原真实Account Boundary生成七类 Problem 交正式common.json，另private cause/非法requestID/带query instance/非法commit_state四负；不手造producer正结果，不改产品/正式Schema。root将原热cache交还本域，并只授权 StandardSchema 单top；60512/892ced→0cd0c0 actual0/5.353s，首UTC2026-10-10T01:40:11.310063Z/5,512,081,408B，固定Go/off/readonly/-p1/race，日志 `output/ai/skills-owner-http/standard-schema-02.jsonl`。7fd432原JSON确认1top/0skip，32Schema/18HEAD无体，未重跑其它纯组、不执行native/PG；无在途cache writer。两schema源+current现freeze交Skills仅复核小差异。candidate/driver仍未构建，须后续root明确compile授权。

必要候选离线分步构建已有两条actual0（源码98bb09f0未变），每条独立outer Wait后才启动下条：native 86279/752ddf→bf099f actual0/2.512s，首UTC2026-10-10T01:54:20.416158Z/5,450,342,400B；`output/ai/skills-owner-http/skill-owner-http-native-race.test` 20,672,971B、SHA256 `e60c8355faefadb81fa2653b4485bf8c0123871ffc0cf87dd90944a0bd7cd3c8`。PG 81480/58e1d4→b49722 actual0/10.6s，首01:54:45.295913Z/5,429,440,512B；`skill-owner-http-pg-race.test` 37,956,311B、SHA256 `5b329ded76f6bb399215c9f87eba01e3e21e5666cedd866577ee92fc853cddfb`。原fixed Go1.27.1/off/readonly/-p1、native race-c / PG integration race-c，均无测试资源；同目录 `native-build-01.{json,log}` / `pg-build-01.{json,log}` 保存actual记录。

第三条PG driver构建首门16cc49于01:55:07.028058Z采5,367,181,312B，actualexit78；未启动Go，也未开输出log/生成driver。依root指令立即停止后续，native driver和两精确list均未执行；不能把源码3/4top声明冒实际发现。保留两新candidate，不重编。后继空间协调与root授权后只补 `.agent-state/task-planning-recovery/pg_only_driver.go`→`output/ai/skills-owner-http/pg-only-skill-owner-http-driver`、`.agent-state/work-owner-http/native_driver.go`→`skill-owner-http-native-driver` 两build，以及各candidate原exact-list。两包无TestMain；list仍待本人actualWait，不能提前计PASS。当前无session/Go/cache writer，无native/PG/socket执行。current/card冻结给root。

root 保存上述阶段为892046dd并回收明确已退休产物后，授权只补缺项。本轮四条仍每条单独 outer actualWait 后才下一 fresh，同原 fixed Go/off/readonly/-p1/cache，未重编两个 candidate：

| 补项 | 首 UTC / 可用 B | 本人原实际终态 | 产物或发现 |
| --- | --- | --- | --- |
| PG driver build | 2026-10-10T01:57:57.084277Z / 5,432,700,928 | 34104/7c61db→6839c1，0 / 1.905s | `pg-only-skill-owner-http-driver`，15,400,524B，SHA256 `3c9e6bf9f5ef8f7bdb4bd1dbe9068b989202611936ce6a0bc4d97a0ad5911f1b` |
| native driver build | 2026-10-10T01:58:11.179006Z / 5,416,235,008 | ba9efe，0 / 0.47s | `skill-owner-http-native-driver`，4,857,015B，SHA256 `c11ba3a9c7d626d7cbd9b1ba964ccb02e00827737ebabb7dcaa687ad07111767` |
| native exact list | 2026-10-10T01:58:30.905286Z / 5,411,487,744 | 63489/0b0a02→422f18，0 / 1.06s | 恰 `TestSkillOwnerHTTPNativeDeadlines`、`TestSkillOwnerHTTPNativeKeepAliveAndClose`、`TestSkillOwnerHTTPNativeBackpressureAndDisconnect` |
| PG exact list | 2026-10-10T01:59:02.268652Z / 5,411,430,400 | 30701/4dbe63→bcd478，0 / 1.07s | 恰 `TestSkillOwnerReadHTTPMetadata`、`TestSkillOwnerReadHTTPCurrentAuthority`、`TestSkillOwnerReadHTTPTransactions`、`TestSkillOwnerReadHTTPCommitUnknown` |

同目录 `pg-driver-build-02.{json,log}`、`native-driver-build-01.{json,log}`、`native-list-01.{json,log}`、`pg-list-01.{json,log}` 保存实际记录。两次list分别核原candidate完整SHA；PG在`tests/skills` cwd，仅实际发现，不执行TestMain或测试体。16cc49门78和全部先前方法局限保留。

f5ec1a只读执行冻结supervisor的`skill_http_inputs`（不调用main/driver）：native15、PG34输入均存在可读。新输出 `output/ai/skills-owner-http/native-owner-read-01`、`output/ai/skills-owner-http/pg-owner-read-01` 均未使用且无symlink；当时git status clean。未据该预飞声称实际资源尾。后继仅获fresh grant后，在本树cwd沿上文固定env/同进程fresh5GiB，分别执行原supervisor：

```text
python3 .agent-state/task-planning-recovery/pg_only_supervisor.py --driver /workspace/agenteam-skills-owner-http/output/ai/skills-owner-http/skill-owner-http-native-driver --binary /workspace/agenteam-skills-owner-http/output/ai/skills-owner-http/skill-owner-http-native-race.test --run '^TestSkillOwnerHTTPNative(Deadlines|KeepAliveAndClose|BackpressureAndDisconnect)$' --output /workspace/agenteam-skills-owner-http/output/ai/skills-owner-http/native-owner-read-01
python3 .agent-state/task-planning-recovery/pg_only_supervisor.py --driver /workspace/agenteam-skills-owner-http/output/ai/skills-owner-http/pg-only-skill-owner-http-driver --binary /workspace/agenteam-skills-owner-http/output/ai/skills-owner-http/skill-owner-http-pg-race.test --run '^TestSkillOwnerReadHTTP(Metadata|CurrentAuthority|Transactions|CommitUnknown)$' --output /workspace/agenteam-skills-owner-http/output/ai/skills-owner-http/pg-owner-read-01
```

PG必须显式传原实际解释器 `AGENTEAM_SKILL_HTTP_SCHEMA_PYTHON=/opt/codex/runtimes/codex-primary-runtime/dependencies/python/bin/python3.12`；两组各自原预算、Go/driver/outer实际Wait、private/desc/TCP双采/input尾均不变，PG另核精确两ID双退役。当前没有session/Go/cache writer或本域真实资源，两组均未执行。仅current/card更新后冻结给root；全部技术源与已成产物不动。

## Native01 原窗口完整结果

root 在dae224f2结果保存后明确授唯一native窗口，本人原session24202/chunk339cf6于2026-10-10T02:02:54.242191Z同进程采5,411,155,968B，核原e60候选/c11 driver完整SHA与新输出absent，再exec原supervisor。未改任何输入，未启动PG。原日志 `output/ai/skills-owner-http/native-owner-read-01/pg-e66dba39c10645669d9d390897e508c4.log`：3top/6sub全PASS，原read 2.01s/更早parent0.15s、清deadline同连接2.15s、原Close错误、实际写Timeout2.00s及断连原领域尾均通过；这里仍是真实TCP加明确局部领域控制，不据此接受PG权限/SQL。

child1506218实际Wait0；driver1506211 terminal0/7.384s且原supervisor actual_driver_wait=true/actual_exit0。`runtime_empty=true`、`actual_child_wait=true`、`private_removed=true`，双descendants=[]，`SKILL_HTTP exact_cases_wait_private=True`；HOST_TCP两次delta_empty，`inputs_unchanged=True terminal=0 elapsed=68.173s`。本人原outer由c0f5e4取得actualexit0，7456a2只读原日志/owned目录确认仅剩owned.json、manifest同child PID；未用后采样补原尾。窗口完整PASS后已向root释放，无在途命令/Go/cache writer或本域真实资源。尾后2026-10-10T02:04:14.723492Z可用7,345,946,624B仅记录现状，不作后继启动门。

PG输出`pg-owner-read-01`仍未使用，原4top/12sub尚未实际；需独立fresh grant、fresh门与原PG2全部尾。没有自动重跑native或附带PG。current/card本结果更新后再次freeze；技术源及候选/driver保持原字节。
