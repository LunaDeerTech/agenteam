# Knowledge Owner 已有文档改名

- 树 `/workspace/agenteam-knowledge-owner-rename`，分支 `ai/knowledge-owner-rename`，main 基线 `04455194`。规格见 [D12 主卡末段](../docs/development/work-items/d12-knowledge-documents.md#owner-已有文档改名-ui实施规格尚未验收)。Git 全由 root 负责；没有真实 socket/browser/PG 授权。
- 当前生产实现为已有文档 rename/原 Lookup/显式原请求重放→当前 metadata/body/root+三已知父层有界重读；不含 create/replace/move/delete，不改 backend/锁/D13/STOP。八生产源及必要测试窄返修已保存 `22417dae`；main `33903460` 已合入为 `f408715d`，生产冻结。新真实 fixture 六源初稿与本状态已保存 `f88a1824`，首次退役方法窄返修待下一保存。
- root 已导入 Skills `abdb43b4` 的最新 `client.ts`、`useSession.ts` 与 readonly `api/skill-owner.ts`。named capabilities 同时保 knowledgeCommands/skills，Skills 两 GET/current Human/原尾不变。当前 client/session 交回 content 唯一写，Session 与该来源零差异；下一转交由 root 做。auth 保 Skills canonical suffix 与原 Project 导航顺序。
- 三个独审 must-fix 已窄修并获 skills 实际差异有限接受：冲突捕获旧 metadata，未取得新完整 GET 不能采用；same-session 恢复/重新 mount 由 Session 公开 target/identity 重建 pending 归属，原文档不可用也可查证/显式放弃，导航确认后清原私料；只有两个新 POST 严格处理 reader/outer cancel reject，原 actual join/release 保留，安全失败使原命令 uncertain，旧端点取消分支保持。

## 已完成与失败边界

- 首 pure `86317` actual1，29 项 24 PASS/5 FAIL：三个 Instant fixture 少正式微秒；旧 controller 错清 same-session unknown；同组件 guard 晚于 auth.restore。早期 Project 上下文猜测未作为修复依据。修后 `89673` actual0/30 项及类型 `63528` actual0；它们不代表后来独审缺口已接受。
- 原兼容 `13568` actual1：40 selected 39 PASS/1 FAIL，旧 Project 取消后确认离页断言仍为旧 URL，build 未执行。返修 `31318` actual1：新 35 核心全 PASS，仍仅旧 Project case FAIL；类型 `13051` actual0。保旧取消分支后该 case `55453` 仍 actual1。
- 有界诊断 `54674` 原进程退出；current 与 `04455194` 原三 shared 内存覆盖均同点 FAIL，实际 Project 确认 true/Session not busy，但原测试未等待 Router 导航完成。临时诊断首稿 `34763` 因插入变量未命中 ReferenceError 失败；修正后 `39370` actual0 仅证明等待原导航的诊断，不升级原测试。正式修该 case 用原 click 前的首个 afterEach 事件、严格目标/来源/无 failure、finally/onTestFinished 解绑，不加 sleep/retry 或预算；修后 `58520` actual0/恰一 case，原取消/草稿/URL/零 PATCH 断言保留。
- 最终三新文件 `46075` actual0：35 项（client19/state13/page3）；`16585` 类型 actual0/27.497s + Vite 私有 build actual0/2.488s，dist 为 `output/ai/knowledge-owner-rename/dist`。旧受影响兼容 39 PASS 复用，仅失败项按以上修后验证；未重跑全 126。prod/source 后续不变则复用此次构建。原 Project test 修后最后 noEmit `67029` actual0/22.323s；owned format/diffcheck 实际0。
- 原日志/result 保在 ignored `output/ai/knowledge-owner-rename/{pure-01,pure-02,compat-build-01,repair-03,project-compat-02,project-diagnostic,pure-04,project-compat-03,build-01,type-final}`。这些恢复事实不代表真实浏览器、实际 Owner 写权限或 Unknown 事务路径已验。

## 写域与下一步

- 生产：新 api/knowledge-commands、composables/useKnowledgeRename、components/knowledge/KnowledgeRenameDialog；旧 api/client、composables/useSession、composables/useKnowledgeOwner、views/projects/ProjectKnowledgeView、router/auth（均在 web/src）。新增 command-client、command-state、rename 三测试；另 root 授权只修 project-workspace.spec.ts 原单 case 的实际导航 join。Schema/API/Session 公共契约不变。
- 新真实源码已落盘：`internal/central/app/knowledge_owner_rename_web_test.go`，`tests/account-captcha-web/knowledge-owner-rename.config.js`，`tests/account-captcha-web/e2e/knowledge-owner-rename.{spec,native}.ts`，`.agent-state/knowledge-owner-rename/{README.md,native-controls.cjs}`。exact `TestKnowledgeOwnerRenameWeb` 1top0sub/PW1case，首正常 rename 与八 GET；unknown/conflict 先保当前受控验证界限。Go 尚未编译、真实链未运行。
- 初稿原 native 格式解析因选择表达式少右括号失败，机械修正；原离线 native `35402` actual0（10 模式/57 控/0 unhandled）、fixture strict TS `72800` actual0、锁定 PW list `19235` actual0/恰一 case。独审随后发现 first finish 先等待 PW 尾、后记录 browser 首退休的缺口；这些旧 PASS 不冒方法完整接受。
- 仅新 native/controls 窄返修：首次 finish 同步冻结 Node pending/terminal/完整 tail 资格，立即调用同一次 page 双 seal，再 allSettled join 全部原 PW/page 尾；缓存原 finish Promise，首时未完成或之后新 Request 均不能迟到升级。新增一个 actual observeRename held-finished 负控；修后 `25870` actual0（11 模式/71 控/0 unhandled，3.587s），fixture strict TS `49712` actual0（1.821s）。POST normal-only 与旧四 GET live ERR_ABORTED 全联合门保持；旧 read04、Go/config/spec/dist、预算未改。最后实际 diff 独审待结论。
- 上述四路径已保存 `e80c1d07`。复审确认首快照/双 seal/实际 join 主路径，补出退休后已登记 Request 的首 response 仍可新建尾；本轮仅在 response 准入增加 `retiring` 拒绝，既有尾继续 join。71 控/类型证据属于该一行补门前，不冒修后重跑；补门只做有限静态复核与格式检查。没有新增业务或矩阵。
- default initializer 仍 unbound/零事实；复用 same-package 正式 Account/Knowledge/Object 与 test-only 真实 Project fixture Stop/Drain。POST 必须原 Request finished、原响应完整消费/取消尾、Session typed 同 Promise 与 DOM 全门；不借旧四 GET failed 例外。原七资源、Go120/PW45/6m/root540+60+3/TCP 全尾不变。
- shared supervisor/driver 新 Rename+Skills 闭集由 cleanup 唯一 writer；content 只发确切 env/闭包，不改 shared。long Go/native 前 fresh≥5GiB/私有 telemetryoff，并等 root 实际窗口；短 Node pure 不要求该磁盘门。没有在途本树进程。
