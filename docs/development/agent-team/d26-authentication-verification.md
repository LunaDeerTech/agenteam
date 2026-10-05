# D26 正式 Account 认证客户端与页面验收

**本卡通过并已采纳，完整 D26 尚未完成。** 21 个源码/测试/锁文件已提交推送 `9a710f272026b41ef69852bbeb41cb7670b500a8`，主线程确认远端同 SHA。作者为 `d08_registry_backend`，独立验收者为未参与实现的 `recovery_verification`；本次 `recovery_handoff` 只归档既有结果。工作范围沿[认证卡](../work-items/d26-account-authentication.md)及已提交的[规格/API 证据](d26-authentication-spec-verification.md)。

## 固定输入与真实能力

实际验证以 `457b1979c9d6563740543b2011eedc06cce34c71` 加作者 input05 为输入；[作者最终 21 路径清单](evidence/d26-authentication-verification/author/evidence/author-final/manifest.json) SHA-256 为 `598b10dcbc7e65c7ec197947900cf0e28248ac5a83c7fbcbfb4523d7a9fc40c0`，input05 为 `2e0d63e1fabeaf4ba1ba808f2aa5e4a795b903516f6501b08d25517f16b37966`。独立[提交对象核对](evidence/d26-authentication-verification/independent/evidence/accepted-commit.json)确认接受提交内 21 个文件逐项一致。复建须用固定 `457b197`，只从 `9a710f2` 覆盖该 21 路径，再按固定锁构建 dist；独立链另叠三份 probe，**不能以 `9a710f2` 整棵树替代实际运行基线**。

正式页面已通过真实六项 Account API 完成登录、rotate/键盘挑战、刷新及路由恢复 Session、当前 Session 注销和受保护的空首页。客户端保留严格 DTO/Problem、匿名与 Session CSRF、原请求私有材料、Cookie 单飞、代际和 transport 尾部责任；已确认主题沿现有能力应用。首页没有虚构业务数据或未实现导航。该结果没有新增生产 Go/API、共享控件或迁移。

## 作者与独立证据的分布

| 验证 | 固定版本与实际结果 | 复用限制 |
| --- | --- | --- |
| 作者 SessionLifecycle | input03，`auth-lifecycle-rotate-02` 顶层 PASS | 同一 driver 因 desktop 焦点失败 exit 1；本顶层后续逻辑未变 |
| 作者 RotateChallenge | input04，desktop/keyboard 完整顶层 PASS | driver exit 0；最终 UI05 仅改已登录页头布局及失败时几何记录 |
| 作者 RevocationAndExpiry | input04，两分支 PASS | 同一 driver 因 200% 布局失败 exit 1；不抹去该失败 |
| 作者 LayoutsAndProduction | 最终 input05，完整顶层 PASS | 原 390px/200% 断言及后续资源/History/Debug 检查均实际到达 |
| 作者 unit/构建 | 全量 check 62；后续受影响 22 替换旧 17，共 67 distinct；最终格式、类型、build、browser TS 通过 | 不是最终一次 67；Go fixture race compile/vet 未变部分复用 |
| 独立 pure/原生 DOM | 2 个受控 pure 用例；1 次本地 Chromium DOM 观察 | 分别证明尾部/身份处理和原生 disabled 失焦前提，不称真实服务器或完整 Vue 链 |
| 独立真实链 | 最终 v3：1 个 Go top/1 个 browser 链 PASS；PW 7.8s、Go top 10.99s、account 12.035s，driver/observer exit 0 | 保留首轮 CDP 取证失败及 v3 观察装饰的影响，不称无干预传输尾部证明 |

作者合计 **4 个不同 Go 顶层、6 个浏览器场景**，使用上表的版本化组合，不是一次最终全绿。精确 argv/env/exit、原日志、输入和复用解释见[作者覆盖表](evidence/d26-authentication-verification/author/evidence/author-final/coverage.json)与[作者报告原件](evidence/d26-authentication-verification/author/evidence/author-final/report.md)。

独立真实链实际到达错误 proof→400/CHALLENGE_INVALID、真实失焦及恢复、新题验证→80 字符 pass、同 key/完整原输入登录、login 与 GET Session 身份一致、旧匿名 CSRF→403/CSRF_FAILED 且当前 Session 仍有效、正式 Session CSRF 注销 204。真实 DB 两题分别 failed/consumed、精确 Session 被 logout 撤销，并有两条成功 Audit。[独立原报告](evidence/d26-authentication-verification/independent/final-report.md) SHA-256 为 `4d4257e245dbe1674c2b24a7acb68984c0e88ed546dd1b7536d97a38cf7b3fe1`；[最终真实 raw](evidence/d26-authentication-verification/independent/logs/independent-real-02.log) SHA-256 为 `b7fa8aba0508e49c1e729890c6246570d7d5c6925defc5feda5279569aa95a6b`。

## 原失败与限定修正

作者 core F1–F4、表单清理/导航/焦点、desktop drag 与 200% overflow 原红均保留；jsdom/VTU、exact-label、late-tail 入场前置及缺缓存/资产/类型错误与产品断言失败分别记录。desktop 原 raw 没有 activeElement、overflow 原 raw 没有责任元素几何，具体解释属于静态或另行 pure 证据；后续完整真实顶层按原断言通过，不追补首轮未观测事实。

独立首轮在 401 后 `response.json` 的 `Network.getResponseBody` 取证失败，未到目标链；**原因仍未知**。v3 仅在私有 probe 对精确阶段的同一 fetch response 作限长 clone，以 X-Request-ID 关联并保留原 CDP 读取及 requestfinished/failed/导航记录。最终[八阶段观察](evidence/d26-authentication-verification/independent/evidence/final-observations.json)的原 CDP 自身全部成功、copy 匹配、没有 requestfailed，没有实际执行缺体替代分支。tee 会改变背压、取消传播和微任务时序，不能据此宣布首红根因已修。详见[首轮失败原件](evidence/d26-authentication-verification/independent/first-real-failure.md)及原 v1/v2/TS03 输入和 v3 差量。

## 资源、复建与归档检查

作者五轮各 4 容器/3 网络；独立两轮也各 4/3。每轮记录实际 nonce/ID、两次 exact absent，原 2 容器/4 网络的 ID/name/labels 不变；浏览器 PID/starttime、adopted wait 终局及最终 census/runtime/tmp 清零都有原件。独立最终[资源交还](evidence/d26-authentication-verification/independent/resource-handoff-final.json) SHA-256 为 `e04c5aa1634a361074e4ba2b2377e914e9d0e42b3d452fd2f7a7f7b2b2bb4f97`。资源均在写报告前交还，没有自动重跑或扩大原预算。

[持久入口](evidence/d26-authentication-verification/README.md)列出来源映射、输入复建及检查结果。仅保留有界原始证据；接受源码使用 Git 定位，早期不同源去重保存。原报告内临时路径按 provenance 映射，不把旧绝对路径当现行入口。原 raw/patch 的实际空白例外按文件 SHA 明列，不改原字节；无凭据、node_modules、缓存、二进制或整树复制。本次仅文件/Git 对象、JSON、链接/格式与指纹检查，没有运行 Go/npm/browser/Docker 或 Git 写操作。

## 未完成边界

真实 dist 由自有测试同源服务器托管，生产 Central 未由本卡获得 SPA 托管能力；实际 Vite 开发代理未作真实浏览器验收。受控 pure Unknown/迟到响应不等于真实服务器 COMMIT 故障或 Cookie 竞争注入。完整 D26/D28、后继个人设置和其他业务页未由本卡完成；`ready=false`/503、Object 已知退出缺陷及 Artifact/Project 依赖阻断保留。

本次归档时，[个人设置规格](personal-settings-spec-verification.md)已持久归档于 `7847fb3`；rev1.2 已采纳提交 `e2ec65d4bb2bf220b67efb7a88d76bf4b2ceb901`，24 路径已授作者实施、尚未验收，§2 前置已关闭。current Model Resolution 库级 20 路径与 00017 已采纳提交推送 `4295df7`；生产 Resolution 仍 nil，没有生产 consumer/Invocation/Usage 或真实 Provider 调用，Summary 初值/Settings 产品决定仍待定。后续状态以[台账](tasks.md)和[恢复记录](recovery-2026-10-05.md)接续，不改本报告的固定验证输入。
