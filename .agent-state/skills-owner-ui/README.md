# Skills Owner read UI 首链

正式范围见 [D10 UI SPEC](../../docs/development/work-items/d10-skills-owner-read-http.md#owner-读取-ui接口与首条链路)。本 recipe 只运行 `TestSkillOwnerReadWeb` / `[read] Skill Owner existing-skill read`，不启用安装、创建 Skill、版本写入或 Agent 分配。生产 Project initializer 仍 unbound；测试只经原 Account 邀请/兑换/登录得到两名普通 Human，用现有 test-only Project fixture 调用真实 Skills/Object 初始化。

## 输入与唯一入口

- Go 源：[skill_owner_web_test.go](../../internal/central/app/skill_owner_web_test.go)，integration 单 top、无 subtest、原 120s 包含清理。
- PW：[config](../../tests/account-captcha-web/skill-owner-read.config.js)、[spec](../../tests/account-captcha-web/e2e/skill-owner-read.spec.ts)、[native](../../tests/account-captcha-web/e2e/skill-owner-read.native.ts)。锁定 Playwright 1.56.1，45s/expect5s、workers1/retries0，无 trace/video/自动截图。
- 只读复用原 `knowledgeWebSetup`、`rootCompositionProjectFixture` 及 `knowledgeSessionBinding`。最后一项只定位原已加载 Session，不引用 Knowledge 的请求完成判据或 failed 消费例外。
- namespace `AGENTEAM_SKILL_OWNER_WEB_{DIST,EVIDENCE,SCHEMA_PYTHON,CASE,INPUT_HASH}`，`CASE=read`；保留 `AGENTEAM_AUTH_WEB_{ORIGIN,PRIVATE,RUNTIME,CHROMIUM}`。runtime 绝对路径长度最多45，material 只在随机0700私有目录、0600文件；Go 实际 Node Wait 日志固定 `Skill Node actual_wait pid=N success=true|false`。
- 共享 supervisor/driver/entry-controls 由 cleanup 唯一维护；其 root-chain、原7资源/Wait、private/runtime/desc/TCP双尾、540+60+3/Go6m/TCP75 门不由本文件修改。真实执行须 root 授窗，并在同 process 检查 fresh5GiB、原 owned 资源 absent、空 Docker config 和 task-private telemetry off。

## 真实链与判据

Owner 登录 → Project settings → Skills 目录 → Enter 详情 → 显式重读 → 键盘返回；原4次请求应为 list/get/get/list。同文档安装原 native 与 public 包装，所有包装返回原 Promise；原 reader EOF、字节长度/hash、cancel/release、outer cancel、Session 实际返回和忙碌尾，与唯一原 PW Request/XID 及服务端完整原响应联合。正常终态只允许一次 requestfinished 和一次原 Response.finished(null)，requestfailed 始终拒绝。第一次 finish 同步封存 Node pending/每原Request终态资格，立即在同一 page task 发起双 observer 首次 explicit 退休；之后才实际 join 原PW/page尾。首资格与pending0不可被迟到完成补成成功，sealed之后不新启header观察。失败首 snapshot 不变，页关闭后的原尾只作追加事实。

浅色1280/深色390、减少动效、Enter/焦点及全局无横向溢出有真实 DOM/截图门。完成并退休 normal observer 后，真实 UI Logout/Login 切换第二 Human，原 Owner Project 路由应不可用且不触发 Skill GET。Go 单独用第二 Human 当前 Cookie 调用两 Skill GET，严格404/NOT_FOUND/not_started；不把浏览器 Project404混作Skill404。最终比较 Project scoped Skills/Project/Object/Audit/Outbox十类事实未变，并核原 Node Wait、proxy Serve/Shutdown、fixture/root/domain Drain/Joined 和 ProcessGuard stopped。

## 当前离线结果

- 本线程对新 Go 源仅执行 gofmt；组合候选编译由 content 另按 root 授权执行。第一次使用不存在的 `/toolchains/...` 路径失败后，实际路径 `/workspace/toolchains/go1.27.1/bin/gofmt` 成功。不是产品执行。
- [native-controls.cjs](native-controls.cjs) 使用实际观察源、原 Stream/crypto，transport/Session 明确受控；原7项 normal/failed/duplicate/held reader/held outer/identity/void 观察控制 actual0、0unhandled（89678→7fb2f4）。独审发现先join PW再首seal可后移首次资格；新增held-PW header/finished与晚reader两负控修前ed17d3 actual1，窄修后最终9项f29cfb actual0/0unhandled，strictTS49414→aa7f96/格式8daeea actual0。两源已保存1f67ddd5，skills_http精准复核接受；这些不作为浏览器、真实 API 或完整 UI PASS。
- 现有产品 basic API/state38与视图3、Knowledge GET54保持其原有限证据。共享 client/auth 后继由 root 导入 Rename `22417dae`，Session 未变；新 Knowledge 两POST严格取消改动不扩入本读链。
- harness 同锁 `cb6dfd30b1fa05013b617e2dfb1c5d063115b4a7d231e368a24ffd14df03f534` 私有离线复制19392→bff603 actual0，实际 Playwright1.56.1/TS5.9.3。strict TS 首15074→1a6b7f actual2仅命令 typeRoots 指向无 Node types 的harness；改为 web/node_modules/@types 后7670→2afe91 actual0，无源变更。精确 list98644→460a9e actual0恰一用例；格式与 gofmt-l 5041e1 actual0。
- cleanup shared方法09f7eb19已离线ready（其自有6控与旧31控通过）；两作者方法均获有限独审接受。当前最终source闭包由cleanup枚举，Go组合候选交content按root授权一次编译，各场景单独真实窗口；本人未执行Go/浏览器，不冒真实联调。


## 两 UI 组合资产

root 将 Rename `40e1e4ea` 的19路径精确导入 Skills `1f67ddd5` 当前树，保留本域入口与current。共享 client/useSession/auth/knowledge-commands 同源；本次没有编辑产品或这些共享源，也未覆盖 Rename 自身current。

只在组合树执行一次：`npm run build -- --outDir ../output/ai/skills-owner-ui/web-dist-combined-01`（cwd `web`，脚本依次vue-tsc/Vite），原session `57900→d9fdc8` actual0，304modules。首次预飞650101因源hash清单误含不存在的tsconfig.app.json退出，当时npm/type/build尚未启动；核实际单tsconfig.json后才执行此唯一构建。

产物从首目录通过普通文件copy到第二目录，旧web-dist保留：

- `output/ai/skills-owner-ui/web-dist-combined-01`
- `output/ai/knowledge-owner-rename/web-dist-combined-01`

两目录各69个regular文件、978051 bytes，无symlink、所有文件nlink1，逐相对路径对应inode互异。对排序的 `relative_path + NUL + file_sha256 + LF` 计算的共同manifest SHA256为 `a83ba680012d00ff1cd9dab62932cb7782f58926906e01ed0b6485eec5ae57ca`。构建前后web/src与实际入口/锁/config源清单hash一致 `e0169c99fbc4f80b09c78f8bd0bf1679777f776db93c360a262ec13633605d7e`；该清单只证明构建期间输入未变，不能替代driver完整候选闭包。ignored输出 `output/ai/skills-owner-ui/combined-frontend-01.json` 保存上述本机产物摘要。cleanup与content已收到精确DIST供后继闭包/候选使用。


## 首次真实结果与停止点

Skills01使用已冻结组合source `6f3f1faf`、同一61,196,757-byte候选（SHA `adaed67fef0d3571709f671d1448061d9730a4d088fc6e73dd4f5c6259f74825`）、上述69文件dist，以及1474项Skills闭包 `d47c109e0698b4b23afd07364ce2023142b7689e40775f8a02cfaec1a1e98ef0`。固定Python运行原supervisor `--root-chain`，driver原入口、selector `^TestSkillOwnerReadWeb$`、fresh `/tmp/sui01` 与 `output/ai/skills-owner-ui/evidence-read-01`；CASE=read，预算未改。原同启动free6,232,592,384 bytes，私有telemetry off/空Docker配置与原清理全部保留。

整轮FAIL：session61629→e96f98 exit1，Node实际Wait失败、Go top36.06s。原driver/supervisor/outer与4 adopted实际Wait齐，七资源14次absent、private/runtime/desc/HOST_TCP双尾齐、inputs unchanged、STOP0，总236.476s后释放。安全原件投影与精确身份在 [first-read-result.json](first-read-result.json)；本地原输出为 `/tmp/sui01/ui-8a3f37e4514b43c1.log`，不可把body前置通过升级整轮。

最早持久阶段为directory：原PW/native/public均无Skill请求，pending0、未failed，public current/not_busy为true；没有响应sidecar。原安全Go错误只说明Node未通过，精确PW断言未持久化；关闭后的pw_failed=true不回填首snapshot。源码可确定Vue Router缺省optional参数解析为空字符串，View把它保留为详情ID，控制器在Session调用前校验失败；这是确定源码缺口而非已采原route/DOM的动态归因。下一最小修建议仅将缺省空字符串归为目录null，并补真实View/controller路由组合控；本次只读诊断后停写技术源，未修改产品/方法/预算，未重试。目录后续、完整只读事实与真实第二Human页面均未验收；生产初始化unbound不变。


## 可选路由空值修复与组合02

根保存原FAIL至521b124b后，仅 `web/src/views/projects/ProjectSkillsView.vue` 将缺省optional空字符串归为目录null，非空ID仍沿原校验。既有 `web/src/tests/skill-owner-state.spec.ts` 增加一条真实MemoryRouter/View/controller/Session/Workspace/API组合控；只有网络和singleton取得方式受控，先退役旧state fixture，目录→UUID详情→返回目录严格请求序列list/get/list，并验证原owner busyfalse。修前25840→4a5cce actual1、修后39115→ab4296 actual0（恰1 passed/8 skipped）；skills_http两技术限定实际差异审接受。旧方法、Go fixture、权限/UUID门、预算不变，没有新浏览器结果。

在web cwd仅执行一次 `npm run build -- --outDir ../output/ai/skills-owner-ui/web-dist-combined-02`，npm PID570647实际Wait0，vue-tsc通过，Vite304modules/1.09s；同产物普通文件复制到 `output/ai/knowledge-owner-rename/web-dist-combined-02`。各69文件、978075 bytes，sorted relative-path/NUL/file-SHA/LF共同manifest `13b13dff90ee55806b2f588709340458885a31e2109ba4009f4c0813f7419fe4`，nlink1且对应inode互异。源构建前后hash同为 `bf5bc945dcb1daa04a2a987e3e65a43d8af6d81ac3df14a3b44823de47d420be`。

原outer18799→91f60b actual1发生在build/copy成功后写summary：循环Path变量覆盖Popen变量，取pid时报错；其原失败保留。63b10d actual0仅复核既有两目录并写 `output/ai/skills-owner-ui/combined-frontend-02.json`，未重build/copy。两个01目录及Skills01原FAIL不变。Go源码未变，继续复用同候选，driver最终closure须针对02重新枚举；Rename首轮优先，Skills02无实际授权。
