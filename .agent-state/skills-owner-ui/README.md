# Skills Owner read UI 首链

正式范围见 [D10 UI SPEC](../../docs/development/work-items/d10-skills-owner-read-http.md#owner-读取-ui接口与首条链路)。本 recipe 只运行 `TestSkillOwnerReadWeb` / `[read] Skill Owner existing-skill read`，不启用安装、创建 Skill、版本写入或 Agent 分配。生产 Project initializer 仍 unbound；测试只经原 Account 邀请/兑换/登录得到两名普通 Human，用现有 test-only Project fixture 调用真实 Skills/Object 初始化。

## 输入与唯一入口

- Go 源：[skill_owner_web_test.go](../../internal/central/app/skill_owner_web_test.go)，integration 单 top、无 subtest、原 120s 包含清理。
- PW：[config](../../tests/account-captcha-web/skill-owner-read.config.js)、[spec](../../tests/account-captcha-web/e2e/skill-owner-read.spec.ts)、[native](../../tests/account-captcha-web/e2e/skill-owner-read.native.ts)。锁定 Playwright 1.56.1，45s/expect5s、workers1/retries0，无 trace/video/自动截图。
- 只读复用原 `knowledgeWebSetup`、`rootCompositionProjectFixture` 及 `knowledgeSessionBinding`。最后一项只定位原已加载 Session，不引用 Knowledge 的请求完成判据或 failed 消费例外。
- namespace `AGENTEAM_SKILL_OWNER_WEB_{DIST,EVIDENCE,SCHEMA_PYTHON,CASE,INPUT_HASH}`，`CASE=read`；保留 `AGENTEAM_AUTH_WEB_{ORIGIN,PRIVATE,RUNTIME,CHROMIUM}`。runtime 绝对路径长度最多45，material 只在随机0700私有目录、0600文件；Go 实际 Node Wait 日志固定 `Skill Node actual_wait pid=N success=true|false`。
- 共享 supervisor/driver/entry-controls 由 cleanup 唯一维护；其 root-chain、原7资源/Wait、private/runtime/desc/TCP双尾、540+60+3/Go6m/TCP75 门不由本文件修改。真实执行须 root 授窗，并在同 process 检查 fresh5GiB、原 owned 资源 absent、空 Docker config 和 task-private telemetry off。

## 真实链与判据

Owner 登录 → Project settings → Skills 目录 → Enter 详情 → 显式重读 → 键盘返回；原4次请求应为 list/get/get/list。同文档安装原 native 与 public 包装，所有包装返回原 Promise；原 reader EOF、字节长度/hash、cancel/release、outer cancel、Session 实际返回和忙碌尾，与唯一原 PW Request/XID 及服务端完整原响应联合。正常终态只允许一次 requestfinished 和一次原 Response.finished(null)，requestfailed 始终拒绝。双 observer 第一次 explicit 退休必须 pending0，之后实际 join；失败首 snapshot 不变，页关闭后的原尾只作追加事实。

浅色1280/深色390、减少动效、Enter/焦点及全局无横向溢出有真实 DOM/截图门。完成并退休 normal observer 后，真实 UI Logout/Login 切换第二 Human，原 Owner Project 路由应不可用且不触发 Skill GET。Go 单独用第二 Human 当前 Cookie 调用两 Skill GET，严格404/NOT_FOUND/not_started；不把浏览器 Project404混作Skill404。最终比较 Project scoped Skills/Project/Object/Audit/Outbox十类事实未变，并核原 Node Wait、proxy Serve/Shutdown、fixture/root/domain Drain/Joined 和 ProcessGuard stopped。

## 当前离线结果

- 新 Go 源仅 gofmt，未编译；第一次使用不存在的 `/toolchains/...` 路径失败后，实际路径 `/workspace/toolchains/go1.27.1/bin/gofmt` 成功。不是产品执行。
- [native-controls.cjs](native-controls.cjs) 使用实际观察源、原 Stream/crypto，transport/Session 明确受控；7项 normal/failed/duplicate/held reader/held outer/identity/void 观察控制 actual0、0unhandled（89678→7fb2f4）。不作为浏览器、真实 API 或完整 UI PASS。
- 现有产品 basic API/state38与视图3、Knowledge GET54保持其原有限证据。共享 client/auth 后继由 root 导入 Rename `22417dae`，Session 未变；新 Knowledge 两POST严格取消改动不扩入本读链。
- 下一步只做新 harness strict TS/exact list 与独立 actualdiff 审；Go candidate 与真实浏览器仍未运行。
