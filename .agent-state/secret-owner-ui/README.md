# Secret Owner UI 首次真实链准备

当前已有私有正式前端 build 和 Go 候选；首真实链整体 FAIL，定向方法修复后第二次尚未运行。没有整个链路 PASS；前端纯控与源码独审见 [D27](../../docs/development/work-items/d27-project-secrets-owner-ui.md)。默认 Project initializer 未绑定，生产 SPA publication STOP 不变。

## 固定输入与原链

- Go `^TestProjectSecretOwnerWeb$`，app 包，1 top/0 sub，原测试 120s 含 cleanup；未来 Go test 6m、root driver 540s + TERM60/KILL3、TCP75 和原 7 资源双退役门保持。本树共享入口已由 root 转交本作者唯一维护；基于其转入的 Installer 19a3fc80，只将既有 Knowledge 浏览器链参数化为两个固定 profile，本目录不另建资源监督器。
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

## 私有构建与共享入口

正式 `npm run build -- --config <本域output>/vite-build.config.mjs --configLoader native --outDir <本域output>/web-dist-01 --emptyOutDir`，私有配置只 import 原 web/vite.config.ts 并覆盖本域 cacheDir。build01 原88880→8bd8f4 exit1：原工作目录为 web，配置误写相对目录，Vite找不到配置；type阶段已完成。修正私有绝对路径、产品0改后 build02 原84472→dcf1b1 exit0（全vue-tsc+Vite2.55s）。69 regular/nlink1文件、981952B；相对manifest SHA256 `8d3a0b3d18028210f7ccb20b33ac51032daf66f04c6889f75f0d51af813288fd`，实际只读单例 AST 定位原 entry/import/export 成功，未修改 dist。详细 manifest/binding 保本域 ignored output。

共享 driver/sup 原 Knowledge UI helper 增可选固定selector，旧默认与 API 名保持；Secret profile 仅 namespace/case/4源/正式Schema/所选Go/已加载单例来源不同。原 metadata/schema/SkillInstallation 三数据组和 Model/Guard/旧业务门不变。所用锁定 PW/TS 借用目录只解析实际工具文件身份；本地Go/生产/dist依旧 regular，首尾重枚举全部 app 同包/前端生产/dist/PW运行包/Node/Python/Chromium/schema。短 nonce/runtime、private modeoff、原 Node+Go Wait、7资源14退役/private/runtime/desc/TCP 双尾与 sameinput AND 不变。

新 `entry-controls.py` 实际调用原 collector/observer/driver main 和 supervisor前置guard；操作系统边为 doubles，无资源。controls01 五项4PASS/1ERROR，原因逆变通用wait行在另一函数亦存在；只加 browser 相邻anchor 后该项通过。受影响旧 metadata/Installer source控各曾FAIL（新增唯一TARGET需排除；原Installer负控replace未命中）；窄修为排唯一Secret literal、先严格逆剥Secret再assert每hunk恰1，分别9940ff/8b0fb8/2ec4e8实际0。其它4新方法沿原d21031实际PASS复用，不重旧全矩阵。完整逆变恢复19a两共享源字节；未知改动、预算变更、缺/多RUN/PASS/NodeWait、失败/错mode/未知selector、缺资源/private/runtime尾，以及源/tool/dist增删改和环境变化均拒。

后继 Go 计划仅在 root 独占cache授权后：固定Go1.27.1、readonly共享GOMODCACHE、GOPROXY/GOSUMDB off、fresh≥5GiB、私有mode=off并去除三telemetry旁路；`go test -tags=integration -race -c` 仅 app 包，新 `output/ai/secret-owner-ui/candidate-01/secret-owner-ui-race.test`，再精确 `-test.list '^TestProjectSecretOwnerWeb$'`。开始前 source/compile输入冻结；原Wait退出后再按新binary枚举最终运行闭包。当前未执行，不用将计划/candidate路径当真实产物。

## 首次真实结果与定向修复

source56904deb 上 candidate01 原19161→9da97c Wait0（40.946s），exact list原Wait0恰1top；binary61,465,686B，SHA835f2f40adbc79c667eeb68655316917c3f1bd82c7d7439f02a4bdb6218bb56d。Go临时目录双空，固定MinIO普通私有副本已核。生产/dist/Go源码随后没有变化。

native01 原1840→d44246整体FAIL。outer788736/sup788795，启动fresh5,619,044,352B；1491输入hash1a65dba7b3f873cd515790703153f3d3ffb64a4199ff7d9593626416475a14ff。Go790967原Wait1/top55.50s、Node791128原Waitfalse、driver788797原Wait1。firstfailure只有create/page_closed=true，代理仅首个空list200，无POST；不得把后继诊断回填为原首次失败的具体await。原sup148.164s terminal1，7资源14absent/private/runtime/desc/TCP各双尾齐、inputs unchanged=True。外层在原sup Wait和退役以后，将set写JSON触发TypeError；外层原Wait1、缺result保持，不补造原外层观测。原log在/tmp/psu01/ui-6c2e643f587644ee.log，安全首阶段和tail在本域evidence-owner-01，supervisor原摘要在native-01-control。

后继无浏览器诊断确认一项定位器缺陷：锁定PW1.56.1实际injected selector在受控JSDOM、原UiField必填标记结构上，exact label“名称”/“新的 Secret 值”均0匹配（其elementText包含aria-hidden星号）；相同accessible textbox role各1匹配，1a445c实际0。首诊断误用生成exports构造方式失败，改用原工厂后得到上述结果。spec只把这些必填控件改为exact textbox角色，并细分create安全stage，原断言/45s/expect5s不变；产品、前端dist、Go候选均复用。

新增唯一可恢复外层入口 `python3 .agent-state/secret-owner-ui/run.py --attempt 02`，固定原Secret selector/candidate01/dist01，fresh /tmp/psu02、evidence-owner-02、native-02-control；只调用原sup/原helper，结果在原采样时sorted，不后验补采。新run自身追加原Secret collector必需输入，known逆变对应更新；metadata/Installer条目、sup预算/资源门没有变化。离线actual main的OS边明确doubles，空set经原Wait后真实写JSON，低容量零spawn；与受影响inverse/collector共3方法cc6293实际0。strictTS03原13625→c8a72b0、PWlist02原13246→b073c9恰1/0，未重复旧30组件或6native模式。第二实际尚未运行，不追认首FAIL。
