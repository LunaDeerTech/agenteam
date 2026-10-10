# Knowledge Owner 正常读取链

本目录的方法仅对应 `TestKnowledgeOwnerReadWeb`（integration、app 包、1 top / 0 sub）与锁定 Playwright 的一个 `[read] Knowledge Owner existing-document read` case。前两次真实读取整体 FAIL 保留；第三次 `read-author-03` 作者单正常链整体 PASS，原退出尾完整结束。生产 UI 已存 `da77c639`，当前方法源 `0f2194c8`；这不等于整 D12，默认 Project initializer 保持未绑定。现已合主线 `3a7a3fb5` 的 Account/Secret 根装配与共享入口并集（merge `26e3928d`）；新编race候选已在 `3c7958ac` 输入下完成 `read-author-04` 整体PASS，新增独立三项组件风险检查也已通过，边界见末节。

Go fixture 使用默认 `bindAccounts` 的实际 Store/Account/Knowledge/Skill/Object；普通 Owner 来自正式 invitation / inspect / redeem / login。默认 Project Create 的新 target 必须 `DependencyUnbound/NotCommitted` 且零事实；正向准备只使用已有 `rootCompositionProjectFixture` 真实 ports，实际 Stop/Drain/Joined 后才进入浏览器。没有业务 SQL 造 ready 或生产注入。静态 dist 与同源反代只供测试。

原四 GET 的每次请求同时绑定 PW 原 Request/XID、唯一原 terminal、原 reader EOF/Content-Length/正文摘要、reader cancel/release/outer cancel 实际尾、正式 Session 同 Promise 的 typed 返回与当前身份、DOM 实际正文。正常 terminal 必须 requestfinished 恰1/failed0，并实际调用 `Response.finished()` 恰1且返回 null；failed terminal 必须在原 page/context 仍 open、原预算内现场捕获 `net::ERR_ABORTED`，requestfailed 恰1/finished0，且从未调用 finished。public observer 只读定位当前 dist 已导出的唯一原 singleton，不改写资产、不建第二 Session。两个 observer 在同一显式退役点冻结第一轮 pending，实际 join 所有尾后恢复原方法；错误、过期、缺尾、void 返回、错 XID 和迟到结果都不能升级成功。重复、冲突、晚到、关闭、未知错误及首错后另取请求均拒绝；没有 Work replay 或其它接口豁免。

## 可恢复输入

- Go：`internal/central/app/knowledge_owner_web_test.go`，复用同包既有 root fixture/helpers。
- PW：`tests/account-captcha-web/knowledge-owner-read.config.js`、`e2e/knowledge-owner-read.spec.ts`、`e2e/knowledge-owner-read.native.ts`；严格使用仓库 `package-lock.json` 的 Playwright 1.56.1。
- 方法控制：[native-controls.cjs](native-controls.cjs)，使用实际客户端/Session与受控 Fetch/streams；PW 行为半边为受控输入，不代表真实浏览器。
- 新私有资产：`output/ai/knowledge-owner-ui/dist-read-01`（44971 actual0，完整 vue-tsc + Vite 8.3.1 / 294 模块）；旧 `dist` 保留，不用于本候选。
- read03旧输入候选：`output/ai/knowledge-owner-ui/knowledge-owner-web-race-01.test`（60,147,683 bytes），不能用于新主线组合结论。新组合候选为 `output/ai/knowledge-owner-ui/knowledge-owner-web-race-02.test`（60,984,862 bytes，SHA-256 `fe88b150d4a7d82476697ec96016094fad1ef291cfde83abe1449e90c9f8fa30`）；`26e3928d` 下 race-c 和精确1 top列举已通过。固定 Go1.27.1，共享只读 modules、本树私有 GOCACHE；MinIO 为 `output/ai/deps-minio/bin/minio`，由已验证共享固定产物离线复制。
- 固定 Schema Python：`/opt/codex/runtimes/codex-primary-runtime/dependencies/python/bin/python3`，通过 `AGENTEAM_KNOWLEDGE_OWNER_WEB_SCHEMA_PYTHON` 原样传给浏览器 runner；实际 safe sidecar 对照正式 common/knowledge-owner/knowledge-content Schema。

运行输入为 `AGENTEAM_KNOWLEDGE_OWNER_WEB_{DIST,EVIDENCE,INPUT_HASH,SCHEMA_PYTHON,CASE}`（CASE 仅 `read`）与既有 `AGENTEAM_AUTH_WEB_{RUNTIME,PRIVATE,ORIGIN,CHROMIUM}`。共享 driver/supervisor 由 coordination 单写，D12 独立 exact 入口已保存 `bd970b97`；每次仍需 fresh 授窗，不能借 Work selector 开跑。

预算沿原 Go top 120s（含 cleanup）、PW45s、Go test6m、root540s + TERM60s + KILL3s、host TCP75s 双空。原七资源 nonce/ID、Node/Go/driver/outer 实际 Wait、adopted child 实际 wait、private/runtime/desc/TCP/input 双尾保持；仅一个 outer cleanup owner。新私有 telemetry mode=off 并移除三旁路，空 Docker config，同 process fresh disk >=5GiB；每次真实运行仍需 root fresh grant。

## 离线检查与未验

- 首方法控制 826b5e actual1：测试 Theme 环境缺 `document`，仅补受控 `documentElement.dataset`；随后87871 actual0，58项、0 unhandled。
- 首 strict TS 20326 actual2：Reflect.apply 的原 stream/reader 返回值被推断为 unknown；仅在观察代码标注实际原 Promise/reader 类型，4320 actual0。
- AST/Promise 关联控制扩到67项时49762 actual1：原控制等待仅给100个 setImmediate 周期，未取得稳定尾；改为明确1s的离线观测期限（不改任何产品/Go/PW预算），44484 actual0，67项、0 unhandled。包含当前真实 dist singleton 唯一匹配及重复拒绝、原 fetch/read/reader cancel/outer cancel Promise 和 receiver 身份、held reader/outer busy、early retirement/错 XID/坏长度/void/身份改变拒绝。
- 双 observer 在同一个同步调用中先 seal 再分别 await 后，最终 strict TS 80628 actual0。Skills 对 `15e730da` 七源 actual diff 有限接受，无方法 must-fix；此结论不代表真实执行通过。
- 首 race-c 36818 actual1（185.531s）：旧 `project_update_test.go` 三处仍传已撤回的 initializer 参数，list 未执行。仅删除三处末尾 nil，与正式 main 该文件逐字相同。修后88190 race-c actual0（10.761s），随后同原 wrapper 的 `-test.list '^TestKnowledgeOwnerReadWeb$'` actual0，恰一个 top。编译使用已落盘 private telemetry mode=off、去除三旁路；同 process 预飞空闲9,549,406,208 bytes。
- 首锁定 PW `--list` 80873 actual1，0 tests：配置误把带文件前缀的完整 grep title 当裸 case。仅将匹配改为该精确文件与完整 case 的边界表达式；11664 actual0，恰1 test / 1 file。该 config 是编译后运行时输入变更；Go/native/spec 和生产资产未变，不重新编译。单文件 Prettier 与 diff 检查通过。
- 生产页面166/修后29项已有证据保持原范围；不重跑未变矩阵，不扩编辑/parser/来源/生产 SPA/initializer/Runtime STOP。

## 首真实失败

`read-author-01` 使用冻结 `bd970b97`、上述候选与当前 dist，session10035 / outer307373。同 process 空闲9,249,374,208 bytes、1448 inputs、私有 telemetry mode=off/去三旁路、空 Docker config 后启动。Go 单 top 59.14s FAIL，原 Node actual Wait success=false、Go code1、driver code1、outer actual1。原安全快照仅定位 `stage=actual-consumption`、page_closed=true、observer_report=false；该阶段内具体等待/断言尚不能从原材料区分，不归因为产品错误。

同次12个原反代完整 GET200 sidecar（含精确下一段 offset=65535）及两张布局截图存在，但没有最终 observer report，未执行后继逐响应联合证明/Schema与根成功断言，不能据此称消费发布或真实链通过。7个资源ID双不存在、private双不存在、runtime双空、descendant双空、4个 adopted child 原 Wait0、host TCP双空；1448输入初末重枚举/摘要一致，原 terminal1 /193.904s，窗口已释放。安全原记录见 [read-first-failure.json](https://github.com/LunaDeerTech/agenteam/blob/23f448d7c423dc9b5e872b4fb4ebf16e8b86a784/.agent-state/knowledge-owner-ui/read-first-failure.json)，不复制原凭据、headers或正文。该轮结束时下一步仅只读有界定位45s内原等待与方法覆盖缺口，保持严格 finished/reader/typed/DOM/首 explicit门；当时未授权重试或改实现。

## 第二次诊断运行

[诊断计划](https://github.com/LunaDeerTech/agenteam/blob/23f448d7c423dc9b5e872b4fb4ebf16e8b86a784/.agent-state/knowledge-owner-ui/read-second-diagnostic-plan.md) 经独立有限审后，仅 spec/native/native-controls 实施；原成功 AND、header→finished与PW tails→同调用双seal顺序不变。64664离线方法249项/0unhandled、57776 strictTS、82407精确PW list（1case）及三源格式均 actual0，实际差异由 coordination 有限接受并保存 `27a46c9e`；Go候选和dist复用。

获独占窗口后的 `read-second-diagnostic-02` /session21831 /outer345887，预飞原01七ID与三private双不存在、runtime双空；同process空闲8,365,895,680 bytes、私有modeoff/三旁路清除/空Docker config、1448输入。Go top53.36s FAIL，原Node Waitfalse、Go1、driver1、outer1。页与context仍open时，已同步记录 `phase=pw-tails/pw_pending=4`；Request seq1/4/6/9 header已返、requestfailed=true、无requestfinished、原finished仍未返回。原page-close事件后，这四个finished才reject、原尾join归零，随后page-observers evaluate拒绝。较晚的first_failure为page-observers/closedtrue，不能单凭该首快照倒推最初阻点，必须保留关闭前事件链。

最后已有browser采样发生在observer-idle：12 native行均EOF、read/read-return一致、reader-cancel/release/outer-cancel及实际join计数各1；12 public行fulfilled/settled/current/not_busy且bound1，两observer当时pending0、仍active。该安全样本未保留native摘要/CL/XID跨观察器绑定、完整typed返回对象和DTO等式，最终首次explicit退休/end/join亦未取得；这些保持UNKNOWN，不能称全消费证明齐。请求失败分类与取消来源未捕获，不推定ERR_ABORTED或产品原因。

原7ID双不存在、private双不存在、runtime双空、descendant双空、4 adopted原Wait0、TCP双空、1448输入重枚举/摘要一致；wholeFAIL terminal1/135.099s，窗口已释放。安全原结果见 [read-second-failure.json](https://github.com/LunaDeerTech/agenteam/blob/23f448d7c423dc9b5e872b4fb4ebf16e8b86a784/.agent-state/knowledge-owner-ui/read-second-failure.json)。该轮结束时只提出四固定GET原消费闭集方法方案，参考共享同一Request terminal的正常finished一次/failed零次，所有其他真实联合门保留；当时未授权实现或第三次实际运行。

## 第三次作者正常链通过

`0f2194c8` 三方法源经 coordination actual diff 有限接受。仅补原 Request 单 terminal/首 Response 占位，保完整原 native/public/typed/DOM/Schema AND 与原 PW tails→双 observer 同调用首 seal 顺序。新 children 控制首轮55684 actual1为刺激少传 parentID，按正式 `children(project, null, {limit:50})` 修正；原 FAIL 保留。最终35822方法653控/0unhandled、46653严格TS、84536精确list恰1case、30064三源format均 actual0。Go/config/dist/生产/共享预算未改。

`read-author-03` /session25505 /outer409429于UTC07:18:53.594873启动，fresh6,898,204,672 bytes，旧01+02共14ID/6private/2runtime双预飞齐；私有modeoff/去三旁路、空Docker config、原1448输入与67dist。原Node411934 Waittrue，Go411714单top32.59s PASS/Wait0，driver409741 Wait0，outer实际0，4 adopted原Wait0。新LIVE seq1/5/6为ERR_ABORTED失败终态且finished调用0，另9正常终态finished调用1/null；12原Request/native/typed的XID/CL/digest/完整DTO与正文等式、树键盘/UTF8下一段/Drawer、Schema12/status0均通过。两个observer均首次explicit且首次pending0、最终pending0、失败false，publication仍current/notbusy。默认initializer未绑定、显式真实Project fixture已join及文档2/commands2/active readers0由原Go断言通过。

七资源14次absent、private/runtime/desc双空、HOST_TCP双空、初末1448输入重枚举/摘要unchanged=true；wholePASS terminal0/150.043s，窗口已释放。安全记录：[read-third-pass.json](https://github.com/LunaDeerTech/agenteam/blob/23f448d7c423dc9b5e872b4fb4ebf16e8b86a784/.agent-state/knowledge-owner-ui/read-third-pass.json)。前两次FAIL及其UNKNOWN不回填，本次只接受作者四GET正常链；不宣称编辑、来源/parser、Project Create HTTP、完整D12或生产SPA已完成。

## 新主线组合准备与实际通过

已仅重编app integration race候选并核 `-test.list '^TestKnowledgeOwnerReadWeb$'` 恰1 top、PW config `--list` 恰1 case及新driver `--check` 当前输入。使用Go1.27.1、私有telemetry modeoff/去三旁路、只读离线modules与独立runtime；长编译前同process fresh>=5GiB。生产UI/锁/dist未改，复用最终78项及既有兼容/44971构建，不把复用记成新执行。此次42757编译/Go list实际0，原两个子进程Wait0且runtime为空；14897 PW list实际0/恰1 case，56476 driver --check实际0。当前1455输入、67dist，plan未创建runtime/evidence；这些仅是准备结果；后续read04实际结果另记如下。

真实运行仍须单次自有窗口：固定Python调用 `.agent-state/task-planning-recovery/pg_only_supervisor.py --root-chain --driver <本树root_chain_driver.py绝对路径> --binary <新候选绝对路径> --run '^TestKnowledgeOwnerReadWeb$' --output <新自有短目录>`，并设置上述D12私有环境。不得复用旧evidence/nonce；原Wait、七资源和输入初末/TCP双尾全部保持。编译/列表/plan检查本身不等于组合运行通过，必须另有原实际Wait和全部退出尾。

`read-author-04` source3c7958ac/session93990/outer448904在UTC07:53:29.989745启动；sameprocess fresh5,565,239,296B、旧01/02/03共21ID/9private/3runtime双预飞齐，privateoff/去三旁路/新emptyDocker。原collector排除pure `.spec/.test`，实际closure1455、inputSHA `6a298ef9c1c6046e0949ba09e781bbc235dd6c79f2bc5f90679c1db2447a2b4d`，不是根据新增风险测试猜1456。新candidate02与同67dist身份齐。

Node451523 Waittrue、Go451366单top27.31s PASS/0、driver449324 Wait0、4 adopted Wait0、outer实际0；12四GET/native/public/typed/DOM/Schema12全部AND，两observer首次explicit/pending0/end0齐。新LIVE seq6/7/8/12严格ERR_ABORTED/finished调用0，另8normal调用1/null。七资源14次absent/private/runtime/desc双空/HOSTTCP双空、初末1455输入一致，wholePASS130.643s，窗口释放。此安全结果留topic的 `read-fourth-pass.json`，无需随正式源码导入；原01/02FAIL和03旧输入PASS保留。

独立风险文件 `web/src/tests/knowledge-owner-risk.spec.ts` 已保存3c7958ac；coordination执行3项实际App/Session/Workspace、受控Fetch/Stream：当前401隐藏内容、reader-held跨Project与outer-held跨Session不提前释放原Cookie请求owner，释放后正控通过。unit31259 actual0（7.99s/测试3.04s）、类型66927与格式通过；首容量预飞未exec、两刺激错误FAIL保留。这里只认独立组件风险范围，不声称新真实Owner转让/Logout PG、完整D12或生产SPA验收。
