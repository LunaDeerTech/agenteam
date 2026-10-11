# D11 Sprint 标题与描述编辑 UI

2026-10-11，有限独立接受。已有 initialized Project 的当前 Human Owner 可在任务页面选择 planned/current Sprint，编辑标题、描述并明确保存。产品基础 `86b18626` 与 Scheduler main 组合为 `df554807`，最终浏览器方法固定 `9651ec91`；非作者已独审源码、方法、原始业务与资源证据及五张截图，未将作者执行写成非作者本人重跑。

## 用户行为与恢复边界

沿 [Work Structure §2/4](d11-work-structure.md)、[Owner HTTP §2.1/2.2](d11-work-owner-http.md)及[已交 Milestone 编辑](d11-milestone-edit-ui.md)，不新增后端、迁移或通用框架。未触达字段省略，空描述明确清空，null/空 patch 拒绝；标题和描述保留原字节，不 trim/NFC。planned/current 可新写，completed 连同值 no-op 也拒绝；不能修改父 Milestone、rank、生命周期、Project Current 或隐式启动 Sprint。

复用 `PATCH /api/v1/projects/{P}/sprints/{S}` 的 `{expected_version,request:{title?,description?}}`。唯一 Session 在发送前捕获原 key、Project/target、字符串版本和完整字段 presence；原 `POST /structure-commands/lookup` 精确发送 `{command:"work.sprint.update",target_id,expected_version,request}`，只接受 Structure 的 `state/result`，不混用 Start 的 `status/receipt`。回执 command/target/触达字段及完整 Sprint lifecycle/actor 均校验：changed 版本 +1/event 非空，no-op 原版本/event null。service Actor 必须绑定同 Project，沿正式 identity registry；已有 schema 枚举漂移不在本片修改。

明确拒绝保草稿；冲突需显式重读并明确采用读取版本后再保存。重读发现 completed 保留说明和草稿，禁止采用并继续提交。Unknown 仅查原 key/原完整意图，不自动重发或换版本；当前 completed 或 Project 归档不否定历史已提交 Lookup。保存回执与当前事实分开，确认后通过真实 Get/List 更新显示，读取失败只重试读取。

与 Milestone 编辑/创建、Sprint 创建/启动和 Task 命令双向互斥；明确放弃只释放 rejected，Unknown/在途原意图不释放。同 Session checking、503 与无关目标拒绝保留本片材料；绑定 Sprint 或同 Project 的真实拒绝、真实身份/CSRF 改变清理私有材料，跨目标迟到不发布。表单使用既有标签控件，键盘取消回编辑按钮；completed 后按钮不可用时回同目标标题，保存读取成功后聚焦当前标题。

## 已执行检查与原失败

作者受控检查原件在 `output/ai/sprint-edit-ui/checks/`，逐轮范围不相加为唯一总数：`client-session01` 4 文件 18 PASS（本片 client/真实 Session 各5、原 Milestone 8）；独审 service Actor 同 Project 修正后 `client02` 5 PASS。`page01` 4 文件 20 PASS，其中新 Sprint 8 使用 typed ports；最终 Unicode White_Space/FEFF 原字节和 completed 取消焦点补差后 `page02` 新8 PASS。

`recovery01` 真实 Session + Workspace + Tasks、受控 fetch 原20 PASS/2 FAIL 保留：两 Sprint 导航控仅等待路由、未等异步 pump。专属 fixture 加 flushPromises 后 `recovery02` 新12 PASS，未因此改产品或放宽断言；覆盖 same-Session restore、新身份、绑定/同项目拒绝、503/无关目标保留、切 Sprint 迟到不发布，以及两种 rejected 放弃后的下一真实创建。`regression01` 4旧状态文件15 PASS；`type03` 为当时全产品/控，最终 `build01` 含 vue-tsc 和生产构建 actual0，生成77文件。client/Session 合并双方反补丁后逐字相等证明在 `merge-scheduler01/`，不重跑未变输入。

无 browser/page 的 `browser-controls01` actual0：实际 shared guard 的 strict state/status、所有混合 flag 拒绝及原 Milestone/Scheduler/Start 正反控。`browser-list01` normal/lost 各恰1项。原 launcher 闭集 `browser-launcher01` 作者控误将非 UI selector 传 UI profile 而 ValueError/actual1；02 仅控制正确比较原拒绝后 actual0，旧组保持、新组及6反控通过，未放宽正式 launcher。

`compile-sprint01` 原 Go PID220674 的 `p.wait()==0` 后，作者记录器在 SHA footer 错取 argv[7] 而非二进制参数，外层工具45078实际 exit1；原异常与执行分支说明保留，不能写成 wrapper 成功，也没有伪造精确编译耗时或重编。补充记录核1692输入前后完全一致、二进制 SHA `25fcb4c875f4618fc446fdd78a38ae217893cdf5ed2e2dcee9f349ec13cd5fc6`；原工具39291的实际 selector list0且唯一 `TestSprintEditWeb`。该区分已独核接受。

## 真实浏览器限定交付

沿原真实 Account、显式初始化 fixture、默认 App/PG/正式 HTTP 准备父 Milestone 和目标 Sprint；normal 为 planned v1，lost 另经正式 Start 成为 current v2，均无 Task。浏览器不创建或启动目标。normal 精确3 PATCH：标题/描述原字节变更到v2、仅触达同值标题 no-op仍v2、仅空描述清空到v3；每步真实 Get/List与用户显式刷新均核对当前字段。lost 仅1 PATCH，在完整原 TLS EOF/Close、handler 返回及 SQL 提交确认后物理截断，唯一同 key/target/version/full presence Lookup，再读取当前内容，不再 PATCH。

数据库后验核原 command、semantic digest、request、plan 前后像、receipt、no-op、Outbox与canonical；父 Milestone、Project/Current、生命周期、rank/排序 generation、既有 Audit、Execution及Scheduler事实保持。初始化与静态托管为隔离测试前提，不冒生产 initializer/SPA 发布。

`native-sprint01` 原 `TestSprintEditWeb` 1 top/2 sub PASS45.00秒（normal25.04、lost19.96）。作者亲收外层工具31455 actual0；supervisor224310实际 Wait0/144.162秒，原日志142.476秒。原Go226343、两Node、driver及8Chrome Wait均0；7个精确资源14次absence、private/runtime/descendants与HOST_TCP双尾闭合，2509输入前后完全一致。1689项编译共享输入相同，另3个cmd测试仅额外冻结，77个物理dist文件与build01相等。

normal 60请求为52 finished与8个实际 ERR_ABORTED（包含seq46 PATCH），58份原请求联合证明；lost 31为29 finished、seq16声明PATCH截断和seq25 Tasks GET ERR_ABORTED，28份联合证明。所有9个 aborted 都核原字节/EOF/closed、同RID、typed/public/current和Go原正文；normal该PATCH原569B及body/key hash一致，lost原696B提交后cut再同keyLookup。保实际终态，不写成全部finished。

五图已逐张独立查看：1440浅色完整编辑表单/Unknown提示、390深色当前清空结果/恢复可见范围正常；390-dark-clear只展示到描述框上缘，底部按钮未入图，同轮DOM/键盘保存断言单独成立。真实链另核取消回焦、保存焦点、减少动效和无页面横向溢出。原件在 `output/ai/sprint-edit-ui/native-sprint01/`，原日志 `/tmp/sew01/ui-f19db55b37e340bb.log`。重建沿专属 `sprint-edit.config.js`、原 driver/supervisor 的 `^TestSprintEditWeb$` 与 `AGENTEAM_SPRINT_EDIT_WEB_DIST/EVIDENCE`，固定输入和资源窗口后运行。

本片不含 Sprint 完成/rollover、排序/删除、完整权限/撤权浏览器矩阵或完整 Work 功能。生产 initializer、完整 F1/participant、ready=false/503 及 Object Runtime join、OpenAI tools、Central SPA concurrent publication、Jina/Image STOP 保持；完整 D11/D27 与 E01 未完成。
