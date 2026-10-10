# Knowledge Owner 正常读取链

本目录的方法仅对应 `TestKnowledgeOwnerReadWeb`（integration、app 包、1 top / 0 sub）与锁定 Playwright 的一个 `[read] Knowledge Owner existing-document read` case。离线方法、类型、构建及两个精确列表检查已完成，真实链未运行。生产 UI 已存 `da77c639`，首链测试源已存 `15e730da`；默认 Project initializer 保持未绑定。

Go fixture 使用默认 `bindAccounts` 的实际 Store/Account/Knowledge/Skill/Object；普通 Owner 来自正式 invitation / inspect / redeem / login。默认 Project Create 的新 target 必须 `DependencyUnbound/NotCommitted` 且零事实；正向准备只使用已有 `rootCompositionProjectFixture` 真实 ports，实际 Stop/Drain/Joined 后才进入浏览器。没有业务 SQL 造 ready 或生产注入。静态 dist 与同源反代只供测试。

原四 GET 的每次请求同时绑定 PW 原 Request/XID、`Response.finished()==null`、原 reader EOF/Content-Length/正文摘要、reader cancel/release/outer cancel 实际尾、正式 Session 同 Promise 的 typed 返回与当前身份、DOM 实际正文。public observer 只读定位当前 dist 已导出的唯一原 singleton，不改写资产、不建第二 Session。两个 observer 在同一显式退役点冻结第一轮 pending，实际 join 所有尾后恢复原方法；错误、过期、缺尾、void 返回、错 XID 和迟到结果都不能升级成功。只接受正常 finished 请求，没有 Work replay/aborted 例外。

## 可恢复输入

- Go：`internal/central/app/knowledge_owner_web_test.go`，复用同包既有 root fixture/helpers。
- PW：`tests/account-captcha-web/knowledge-owner-read.config.js`、`e2e/knowledge-owner-read.spec.ts`、`e2e/knowledge-owner-read.native.ts`；严格使用仓库 `package-lock.json` 的 Playwright 1.56.1。
- 方法控制：[native-controls.cjs](native-controls.cjs)，使用实际客户端/Session与受控 Fetch/streams；PW 行为半边为受控输入，不代表真实浏览器。
- 新私有资产：`output/ai/knowledge-owner-ui/dist-read-01`（44971 actual0，完整 vue-tsc + Vite 8.3.1 / 294 模块）；旧 `dist` 保留，不用于本候选。
- 已编译候选：`output/ai/knowledge-owner-ui/knowledge-owner-web-race-01.test`（60,147,683 bytes）。固定 Go1.27.1，共享只读 modules、本树私有 GOCACHE；MinIO 为 `output/ai/deps-minio/bin/minio`，由已验证共享固定产物离线复制。
- 固定 Schema Python：`/opt/codex/runtimes/codex-primary-runtime/dependencies/python/bin/python3`，通过 `AGENTEAM_KNOWLEDGE_OWNER_WEB_SCHEMA_PYTHON` 原样传给浏览器 runner；实际 safe sidecar 对照正式 common/knowledge-owner/knowledge-content Schema。

运行输入为 `AGENTEAM_KNOWLEDGE_OWNER_WEB_{DIST,EVIDENCE,INPUT_HASH,SCHEMA_PYTHON,CASE}`（CASE 仅 `read`）与既有 `AGENTEAM_AUTH_WEB_{RUNTIME,PRIVATE,ORIGIN,CHROMIUM}`。共享 driver/supervisor 由 coordination 单写；新 D12 入口尚待组装，不能借 Work selector 开跑。

预算沿原 Go top 120s（含 cleanup）、PW45s、Go test6m、root540s + TERM60s + KILL3s、host TCP75s 双空。原七资源 nonce/ID、Node/Go/driver/outer 实际 Wait、adopted child 实际 wait、private/runtime/desc/TCP/input 双尾保持；仅一个 outer cleanup owner。新私有 telemetry mode=off 并移除三旁路，空 Docker config，同 process fresh disk >=5GiB；每次真实运行仍需 root fresh grant。

## 离线检查与未验

- 首方法控制 826b5e actual1：测试 Theme 环境缺 `document`，仅补受控 `documentElement.dataset`；随后87871 actual0，58项、0 unhandled。
- 首 strict TS 20326 actual2：Reflect.apply 的原 stream/reader 返回值被推断为 unknown；仅在观察代码标注实际原 Promise/reader 类型，4320 actual0。
- AST/Promise 关联控制扩到67项时49762 actual1：原控制等待仅给100个 setImmediate 周期，未取得稳定尾；改为明确1s的离线观测期限（不改任何产品/Go/PW预算），44484 actual0，67项、0 unhandled。包含当前真实 dist singleton 唯一匹配及重复拒绝、原 fetch/read/reader cancel/outer cancel Promise 和 receiver 身份、held reader/outer busy、early retirement/错 XID/坏长度/void/身份改变拒绝。
- 双 observer 在同一个同步调用中先 seal 再分别 await 后，最终 strict TS 80628 actual0。Skills 对 `15e730da` 七源 actual diff 有限接受，无方法 must-fix；此结论不代表真实执行通过。
- 首 race-c 36818 actual1（185.531s）：旧 `project_update_test.go` 三处仍传已撤回的 initializer 参数，list 未执行。仅删除三处末尾 nil，与正式 main 该文件逐字相同。修后88190 race-c actual0（10.761s），随后同原 wrapper 的 `-test.list '^TestKnowledgeOwnerReadWeb$'` actual0，恰一个 top。编译使用已落盘 private telemetry mode=off、去除三旁路；同 process 预飞空闲9,549,406,208 bytes。
- 首锁定 PW `--list` 80873 actual1，0 tests：配置误把带文件前缀的完整 grep title 当裸 case。仅将匹配改为该精确文件与完整 case 的边界表达式；11664 actual0，恰1 test / 1 file。该 config 是编译后运行时输入变更；Go/native/spec 和生产资产未变，不重新编译。单文件 Prettier 与 diff 检查通过。
- 当前真实 Go/PW 用例未运行，编译/list不算实际链通过。生产页面166/修后29项已有证据保持原范围；不重跑未变矩阵，不扩编辑/parser/来源/生产 SPA/initializer/Runtime STOP。
