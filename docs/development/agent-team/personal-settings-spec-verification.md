# 个人设置规格与前置验收记录

状态：**rev1.2 规格及 §2 最终前置核查均独立通过并获采纳**，规格提交 `e2ec65d4bb2bf220b67efb7a88d76bf4b2ceb901`。被审技术卡 SHA-256 `65ff66b024de34b32f6b0fbc000f5d3e77a3e40ba7bf1b334045ee2d9b031631`；采纳状态卡 SHA-256 `f78fb7f883fcac25f598ca8b220ced0ed56219dec39f02d6815be9ad5c835fa9`。这不是个人设置实现或动态验收通过。

主线程已授权 `d08_registry_backend` 独占[正式卡](../work-items/d26-personal-settings.md)的24源，固定业务基线为已验认证 `9a710f272026b41ef69852bbeb41cb7670b500a8`；`skill_verification` 负责独立验收。当前未授Docker/browser窗口。此次只同步卡页首与 §10 授权接续，§1–9 技术原文不变；不读取活动实现。

## 原审歧义与规格修订

[rev1 原审](evidence/personal-settings-spec/spec/rev1-review.md)保留唯一 F1：头像下载“1–5MiB Content-Length”可能被误读为最小1MiB。固定D07 HTTP/OpenAPI实际下界为1字节；这是规格歧义，未宣称发生前端产品故障。[rev1.1 原差量](evidence/personal-settings-spec/spec/rev1.1-delta.patch)仅改页首、明确 `1字节 ≤ Content-Length ≤ 5×1024×1024字节`，并要求真实小于1MiB头像成功读回。[独立窄复审](evidence/personal-settings-spec/spec/rev1.1-review.md)为STATIC PASS，其余21路径/产品规则保持；接受提交 `ac653cfd099f959ae9bec2c3489e499351b0fdfd`。原F1报告不倒写为通过。

[冻结认证接缝预审](evidence/personal-settings-spec/prerequisite/seams-review.md)确认：原私有run只有void/忙时no-op，不能当typed个人调用；每请求generation不是稳定身份epoch；Session重验清身份/主题并临时卸载RouterView；登录硬编码首页，旧纯测试禁止settings链接，真实fixture未保留私有record绑定。它是待实现接缝的源码依据，不是认证最终前置通过。

[rev1.2 原差量](evidence/personal-settings-spec/spec/rev1.2-delta.patch)补齐同一owner的typed门面、改密200与后续Session确认分层、稳定epoch、App草稿owner和主题preview责任、登录安全返回及正式邀请fixture；21→24仅增加 `LoginView.vue`、`authentication.spec.ts`、`authentication_web_fixture_test.go`。原产品 §1/5/7保持；新增接口不是已交付能力。[最终独立报告](evidence/personal-settings-spec/prerequisite/final-review.md) SHA-256 `9a0450e4c1f799dcbe110f511fcf7d54ab1fe04fe37bd0986339107d76aba23b` 对该技术差量判定PASS。

## 最终认证基线与后续责任

[最终提交检查原件](evidence/personal-settings-spec/prerequisite/commit-check.json)记录认证 `9a710f2` 的21源与author-final `598b10dc…`逐SHA一致，五项固定Git依赖相同。相对 `ccf498d61152178c5b44994d4b9e8b8f4eb6813b`，Account/app、两OpenAPI、accountenv、useTheme无差量；tests/account仅增加认证两fixture，旧D07 helpers未改。相对认证基线 `457b197` 的相关闭包只变化该21源。依赖指纹和接缝成立后，主线程采纳最终报告并关闭 §2；没有在此重跑认证矩阵或改变八个已验D07 HTTP端口。

[独立短计划](evidence/personal-settings-spec/plan/plan.md) SHA-256 `880de32261a4154a5912ed2395b3113217e9ad54f7451a1fba01811542e262a3` 只列风险准备：作者须完成原纯门槛、4个人设置真实顶层及4认证受影响回归；独立根据固定作者证据选择typed owner/实际body尾部、改密确认、同身份draft/preview及真实新Session/CSRF增量。尚无probe或动态结果；原30s/45s/2min/6m、无重试和资源交接门槛保持。

## 原件、重建与边界

[唯一证据索引](evidence/personal-settings-spec/index.json)绑定19份小型原报告、检查、计划和差量，保留原字节及历史状态。rev1.1可由 `ac653cf:docs/development/work-items/d26-personal-settings.md` 取得；逆向rev1.1差量还原rev1，正向rev1.2差量取得被审卡，再应用采纳状态差量得到 `e2ec65d4` 原卡。归档自查已实际按这些原差量重建并核SHA；固定设计/D07及认证源码使用精确Git locator，不复制源码树、dist、缓存或认证动态证据。原patch的上下文空白保留，精确例外列于索引。

此前报告/计划中的“认证未完成、24路径待授权”是其冻结时状态；当前认证前置已关闭且24源已授权，原件不改写。个人设置、完整D26/D27、D28生产SPA托管、D25与Project均未因本规格归档宣告完成；Object暂停修复、Artifact/领域绑定阻断及Summary待决保持。本次只有文档和文件/Git对象只读核查，无Go、npm、browser、Docker、SQL、网络或Git写操作。
