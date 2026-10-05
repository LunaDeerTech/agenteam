# D26 个人设置 §2 前置接缝审计预备

结论：固定认证候选具备可复用的传输、单一请求 owner、真实 Session 和 full-root 同源 fixture；**尚不具备个人设置可直接调用的队列/身份更新/主题预览接口**。建议把下述窄扩写入卡 rev1.2，将候选路径由 21 调整为 **24** 后独立审差量。认证最终独立通过并提交后，仍须重核相同 21 源与后端闭包，才能关闭 §2；本报告不是最终前置通过或实施授权。

固定输入：认证 author-final manifest `598b10dcbc7e65c7ec197947900cf0e28248ac5a83c7fbcbfb4523d7a9fc40c0`，其引用 fixture-input-05 manifest `2e0d63e1fabeaf4ba1ba808f2aa5e4a795b903516f6501b08d25517f16b37966`；逐 SHA 独立核 21 原件一致。以下认证源行号均来自 `/workspace/agenteam-d26-auth-author-jrfhr6h1/evidence/fixture-input-05/`，不消费活动树。个人设置卡固定 `ac653cfd099f959ae9bec2c3489e499351b0fdfd` 的 rev1.1；D07 基线 `ccf498d61152178c5b44994d4b9e8b8f4eb6813b`，认证候选基线 `457b1979c9d6563740543b2011eedc06cce34c71`。

## 实际导出及最小接缝

| 所有者 | 固定现状 | 必须在原所有者内完成的窄扩 |
| --- | --- | --- |
| `api/client.ts:18,35,49,150` | 导出 AccountFailure、Problem/FailureKind、shape/string/uuid7、Fetch、accountTransport；固定六 endpoint，只有 POST JSON；响应 JSON 有界读取并 await body cancellation。 | 在固定 endpoint 闭集增加八调用；支持 PATCH/PUT/DELETE JSON、raw File + If-Match 和 GET avatar bounded bytes/headers。沿同一错误解析/actual body 清理，不造任意 URL 或第二工厂。成功/失败/取消仍保真；小图合法，真实长度/声明一致。 |
| `api/account.ts:3,89,169,239` | 实际类型叫 **User**（非 AccountUser）、Session/SessionView；createAccountAPI 返回六方法，AccountAPI=ReturnType；完整 user() parser 是文件私有。 | 同文件复用 user()，增加八方法及 Version/Progress/Digest/metadata；不复制解析器。扩展 AccountAPI 后须保持真实构造和注入依赖一致；六方法旧纯 mock 的类型适配不能靠生产 optional 成功/断言绕过。 |
| `useSession.ts:73,547–566` | createSessionController、SessionController、useSession；公开只读 state 与 restore/login/logout/retryOriginal/inputChanged/createChallenge/verifyChallenge/dismissChallenge/leave/restart。**没有公开 CSRF、身份 stamp、泛用队列或资料调用能力。** | 增加闭集、带类型结果的个人调用门面及非敏感身份代际观测，由同一 controller 私有持有 token/key/原意图与唯一 owner。页面不能取得 token、直接调用第二 client 或用任意 callback/API 更新认证事实。具体新名称由 rev1.2 定义，不能把提案记作现成导出。 |
| `useSession.ts:216–254` | run 只接 restore/login/logout，返回 Promise<void>；同 kind 返回旧 visible、其它 kind 忙时 resolve；30s 可结束 visible，但 owner 只在实际 API/body 尾部结束后释放。 | 个人 DTO/Error 必须有真实返回通路，不能用这个 void/no-op 结果当成功；不按泛称 read 合并不同接口。忙时未启动须明确失败或有序等待，不能空成功。新调用的 visible 取消/超时与 actual tail 分开，原 File/body/身份责任保留至 actual 完成；密码 POST→Session GET 共占一次 owner，不嵌套 run/restore，不容 logout 插入。旧认证调用签名/单飞行为不改坏。 |
| `useSession.ts:87–98,108–145,263–297` | generation 每次 run 都增加；restore 先 clearIdentity（含 system theme）再 publish，publish 清意图并 setTheme；这些不是可直接当作“同身份稳定 epoch”的能力。 | 区分请求 generation、身份/context epoch 与临时 revalidation。只有真实身份/context 改变才作废旧设置意图；同身份重验可更新可信 saved/version，不能清 dirty 或把 GET 当 receipt。可信 profile/theme 同步只能消费 controller 自己确认的、身份及版本匹配结果，不能暴露任意 user setter。 |
| `App.vue:12–15,60` / useSession 上述 restore | visibility/pageshow 调 restore，checking 使 RouterView 卸载；故页面局部 composable 的 draft/preview 会丢失。 | 在原 App/useSession 与新 usePersonalSettings 之间明确生命周期：同身份重验期间受保护展示仍按认证规则隐藏，但该页私有 draft/preview owner 不因临时卸载被当作真实离页销毁；失败/换用户/换 Session 则清除。dirty saved/draft 与 token 身份不可混为一个 store。 |
| `useTheme.ts`（固定 Git） | 只导出 mode/resolved/setTheme，现有认证直接设 system 或服务端 theme。 | 保持共享 useTheme 只读；在 useSession 接缝内协调当前身份已确认 theme 与临时 preview，提供受身份约束的设置/撤销预览责任。迟到 Session 不覆盖 dirty preview；取消恢复同身份最新 saved，失效回 system。 |
| `router/auth.ts:5–21` | safeReturnTarget 仅返回 `/`；每认证导航 restore，拒绝后 query.return 固定 `/`。 | 扩闭集到 `/` 与三设置叶子；未认证跳登录保存净化目标，不接受任意 URL/query/hash。dirty 离页/更新应在破坏草稿或 logout 前确认；App 直接 logout 也接同一个受控确认，不另造跨域事件总线。 |

上述是本卡真实所需的新行为，不能把“有 run()”写成队列已对外支持任意业务。设置命令的字段失败/Unknown 保留在本页状态，不直接复用仅懂登录字段与身份阶段的 failure()；真正当前 401/CSRF context 失效才走认证失效路径。密码严格200的 command-confirmed 与后续 Session 确认分开记录：清密码、同 User/新 Session ID/新 CSRF 校验，失败不重发已确认写，丢 Cookie 不把原意图转移到新身份。原卡相关语义保持。

## 24 路径与旧断言冲突

原 21 路径保持，建议新增以下三条；主线程已认可窄范围方向，但须正式写卡并独立审 rev1.2 才授权：

1. **`web/src/views/auth/LoginView.vue`**：第 103 行成功硬编码 `router.replace('/')`。应窄接 `safeReturnTarget` 消费登录当前净化返回目标；不能只改 guard 函数返回类型却继续丢指定叶子。不改挑战/焦点或凭据流程。
2. **`web/src/tests/authentication.spec.ts`**：第 128 行明确要求所有 settings href 不存在，与右上真实 profile 链接和初始 password 链接矛盾。只把已过时的导航缺席断言更新成新允许目标及安全边界，保留其余认证断言。该文件及 session.spec.ts 的六成员 mock（第 50–79 行）还须与扩展 AccountAPI 的静态依赖相容；这是纯 setup 适配，不是放宽真实权限/队列测试。
3. **`tests/account/authentication_web_fixture_test.go`**：仅保留 constructor 已建立的私有旧 record 绑定/必要 fixture 引用，供同包新设置 fixture 精确读取自有 invitation recovery 记录；不暴露生产能力，不新建 fixture HTTP 接口、不改旧测试断言。

这对应 14 生产及 10 测试/fixture 路径。新增 settings 页/composable、client、useSession、App/Home、router 与既定新测试承担其余工作；共享 Ui/tokens/useTheme、package/lock、Go 产品、schema、迁移及旧 browser spec继续只读。后续如仍需范围外修改，先报实际接缝，不为数量复制框架或弱化旧断言。

## 实际 fixture 与八 API 后端差量

`newAuthenticationWebFixture(t,ctx)` 已返回同包的 db/origin/directory/webRoot/entry/log，实际启动 app.Run、随机 PUBLIC_ORIGIN、仅 `/api/v1` 反代与同源 dist、私有初始凭据和清理责任；这是真实可复用的 builder。`browser(ctx,name)` 仍硬编码 authentication.config.js、既有 selector/env/result，不能冒称能直接运行个人设置。新两 Go 文件可持有该 builder，增加专属新 config/安全 IPC/result 与相同预算/进程 wait 的 launcher；不改旧 launcher/assertion。旧 TS e2e 内 login helpers也未导出，不导入执行整份旧 spec充当 helper。

普通用户前置仍必须走随机 `f.origin` 的正式邀请/inspect/redeem。现 constructor 的局部 `old := &httpFixture{...,config:cfg}` 仅用一次 old.record 取 bootstrap，然后丢失；返回对象没有 recovery-log 读取接口。旧 `httpFixture.invite` 又绑定 `httpOrigin=localhost:8080`，不能直接调用以绕过随机 Origin。第24路径保留 private record 绑定可复用既有安全恢复日志 reader；新设置 helper 自己按当前 f.origin 发正式邀请/兑换，严禁 SQL造用户、读取别人日志或把凭据进普通输出。其实际运行、清理、邀请邮件完成均待后续验收。

独立 `git diff --name-only` 比对 ccf498d→457b197 及 ccf498d→ac653cf：`internal/central/account/`、`internal/central/app/`、`api/openapi/account.json`、`common.json`、`tests/account/`、`tests/testsupport/accountenv/` 和 `useTheme.ts` 均**零差量**；认证 21 源也没有 Go 产品/API 修改。因此八方法仍映射已验 D07：GET/PATCH `/me`，GET/PUT `/me/preferences`，GET/PUT/DELETE `/me/avatar`，POST `/me/change-password`。真实 ProfileService/AvatarAuthority/Runtime/HTTP 装配仍在 `app/account.go:324–345,382–386`。不需要后端 API、schema 或新迁移；本报告没有重验 D07 动态行为。

## 最终前置关闭条件及限制

认证最终验收提交到位后：核该提交的 21 源等上述 SHA、必要固定依赖与原 card/public exports 未漂移；再核其提交及个人设置授权基线相对 ccf498d 的上述后端范围无影响差量；核 rev1.2 已采纳24路径及 owner。相同则本静态接缝分析可复用，有差量只补受影响内容。现阶段不将“独立链正在跑”当已通过或已提交。

本次仅固定源码/ Git/manifest 只读检查和私有报告；没有 Go/npm/browser/Docker/网络运行，没有仓库/Git 写，无再委派。没有新增动态证据，不关闭 Object/Artifact 阻断、D25、生产 SPA 托管或完整 D26/D27。all-stop。
