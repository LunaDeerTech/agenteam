# Secret Owner UI 首次真实链准备

当前仅源码和离线方法准备。没有 Go 编译、浏览器、PG 或整个链路 PASS；前端纯控与源码独审见 [D27](../../docs/development/work-items/d27-project-secrets-owner-ui.md)。默认 Project initializer 未绑定，生产 SPA publication STOP 不变。

## 固定输入与原链

- Go `^TestProjectSecretOwnerWeb$`，app 包，1 top/0 sub，原测试 120s 含 cleanup；未来 Go test 6m、root driver 540s + TERM60/KILL3、TCP75 和原 7 资源双退役门保持。共享入口由 content 唯一维护，本目录不另建资源监督器。
- PW `project-secret-owner.spec.ts [owner] Project Secret lifecycle`，锁定 1.56.1，1 worker、retries0、45s、原 expect5s；只 `--list` 不执行浏览器。Node 原单次 Wait 标记 `SecretOwner Node actual_wait pid=N success=true`，不得根据报告文件替代实际 Wait。
- `AGENTEAM_SECRET_OWNER_WEB_{DIST,EVIDENCE,INPUT_HASH,SCHEMA_PYTHON}` 都需真实绝对 owned 路径，INPUT_HASH 是本轮闭包 SHA256；`CASE=owner`。Go 原入口还需 `AGENTEAM_AUTH_WEB_RUNTIME`（绝对路径、长度≤45）。Go 为实际 Node 构造同源 ORIGIN/private/Chromium 环境。
- 新 4 源：`internal/central/app/project_secret_owner_web_test.go`，`tests/account-captcha-web/project-secret-owner.config.js`，同 `e2e/project-secret-owner.{spec,native}.ts`；另本域 `native-controls.cjs` 是离线方法控。
- 运行闭包应含原 root 全集、app 所有 Go/同包 helper、这 4 源、当前前端生产文件和全部私有 dist、实际 PW/Node/Chromium/TS/Python、`api/openapi/{secret-variables,common}.json`；readonly `knowledge-owner-read.native.ts` 只提供现有 Session singleton export 的 AST 定位，不继承其 GET/failed 豁免或判定。

本树 web 私有依赖集合只读链接已锁主树；PW node_modules 只读借 `/workspace/agenteam-skills-owner-ui/tests/account-captcha-web/node_modules`，package.json/lock 逐字同本树，实际1.56.1。私有 cache/output 不写 donor；无需 npm ci 或新依赖。

## 实际方法边界

正式 bootstrap/invitation/redeem/登录准备普通 Owner，浏览器自己登录取得真实 Cookie。默认 Project Create 原 Unbound/NotCommitted 零事实；正向仅显式 test-only 同 Store/真实 ports 创建已有 Project，实际 Stop+Drain/Joined 在浏览器开始前完成。Secret 使用默认根原服务与 HTTP，代理只提供本轮私有 dist/同源入口。

唯一正常链：create→当前 list/detail→PATCH 实际后端成功。代理完整读取原200回执、核正式安全 DTO、实际 Close、保存安全 body/XID 证据后，只对这一次 PATCH 返回固定502；没有领域回滚或虚构 Problem。浏览器必须进入 uncertain，值输入清空、不能重发；人工 Lookup 仅 command/target/expected_version，原 key 相同，无 value；历史回执确认后重读 current GET v2，再 delete v2→3。原3次写入的 history/Audit/event/commands/D04 receipt 各恰3，重复 PATCH 不接受。

新 observer 对所有 Secret 请求坚持 normal requestfinished 恰1、failed0、原 Response.finished(null)。502 是明确失败执行响应，只接受外层 body.cancel 的实际退役与 Session uncertain；绝不作为安全 metadata 完成。200 额外要求同一原 Request/XID、原 fetch Promise、同 reader EOF/Content-Length/bytes/hash、reader.cancel 实际 Promise→release→outer.cancel join、原 Session 公有方法原 Promise 的完整 typed 值/current identity/busy=false，以及页面对应展示。无第二 fetch/clone/tee/模拟 Session。首次 finish 先同步固定 Node pending/ready，再立即派发 browser 同步 seal，之后才 allSettled join；迟到完成不能修复首失败。

合成 Secret canary 的 raw/JSON 转义/base64/SHA256 形态不得出现在响应、响应头、日志或提交后的 DOM/输入。原登录凭据和原写 key 仅私有内存/受控文件，不进入安全证据。失败只写固定阶段和计数，先保存首次失败再等原尾。截屏仅在值已清空后。标准 Draft202012 schema 实际验证原 backend 安全响应，PATCH 原200与线上502分别标明。

## 离线方法与原失败

- `harness-type-01.log` 原 session58788→85c02c exit2：Reflect.apply 的返回静态推断 unknown。仅加 `Promise<any>` 类型断言，不改运行门。type02 原88841→f52e7c exit0。
- PW list01 原90403→7d86cf exit0，恰1 case；无浏览器进程。
- native controls01 原67254→98b21e exit1：normal、held-reader、held-PW、requestfailed 已通过；identity-change 控误写正式 readonly identity，被 Vue 拒绝。修为正式 `auth.leave()`，不改产品。
- controls02 原41172→a5f1f0 exit0，6模式/30显式检查/0 unhandled。使用真实 API+Session+本 observer；Fetch/PW/单例 export 解析为明确 doubles，不冒真实 Cookie/PG/浏览器或 AST/dist 接入 PASS。正向、两种原尾持有后首次退休硬失败、requestfailed、正式身份清除、泄漏响应均覆盖；相同 predicate 还拒错 XID/typed/current/取消或release缺尾/错误Lookup/将Unknown当成功。

可复跑命令：`node .agent-state/secret-owner-ui/native-controls.cjs`。严格 TS 用现 web TypeScript、ES2022/ESNext/bundler/strict/skipLibCheck、DOM/DOM.Iterable/ES2023、`--types node --typeRoots <本树>/web/node_modules/@types`，只选新两个 TS（传递 readonly binding）。日志在 ignored `output/ai/secret-owner-ui/`，原 FAIL 保留。后继 actual 必须 root fresh grant。

work_ui 已实际只读审四方法源（3e406c0e 加单类型行）有限接受，无确认 must-fix；未复跑作者方法控/TS或资源，不升级真实结果。
