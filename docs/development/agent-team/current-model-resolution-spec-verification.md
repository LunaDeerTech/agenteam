# current Model Resolution 规格验收与实施接续

状态：**rev1.1 规格独立静审通过并获主线程采纳**，接受提交为 `e81b029cd65fb26cc8598955e32b961dac35cb85`；被审卡 SHA-256 `9413b70e0d524cc3223bf6aec9d1f761237ec22e96f988a99c9fa5e6827052b6`。固定业务基线仍为 `be0bd07b1dc1fcd91ad217c9bdbfe5a14003ce74`。这不是 Resolver 代码、迁移或动态验收通过。

主线程已授权 `recovery_handoff` 按[正式卡](../work-items/recovery-d09-current-model-resolution.md)实施精确 **20 路径**（9 生产、4 纯测试、6 集成测试、00017 迁移）；`restore_test_dependencies` 负责独立计划、后续固定生产/SQL 静审及验收。当前没有 Docker 窗口，真实资源另授。此次文档归位只改卡页首及授权追加段，§1–11 技术原文不变，不读取活动实现。

## 规格原审与窄修

[rev1 独立原报告](evidence/current-model-resolution-spec-verification/spec/review-rev1.md) SHA-256 `6d7b4ee89dbe6ff6e27f77e19bf209f9f51d1d3f962827c57e16e077a26427a2` 记录唯一阻断 R1：两处同单位异义错误使用了不在 Foundation 闭集中的 `IDEMPOTENCY_CONFLICT`。既有码是 `IDEMPOTENCY_KEY_REUSED`；未知码会安全转为 INTERNAL_ERROR，20 路径不含新增公共错误契约的权限。

[原差量](evidence/current-model-resolution-spec-verification/spec/rev1.1-delta.patch)只替换这两处字面量和页首修订/待复审状态。[rev1.1 独立复审](evidence/current-model-resolution-spec-verification/spec/review-rev1.1.md) SHA-256 `1f1946e347d10e3d91937fd165068220168469d6a0c9ee512adcc2f6b4b636a7` 为 **STATIC PASS**：行为、API、20 路径、锁/当前权限/事务/Unknown/清理边界及验收要求未变；不是通过改规则避开缺陷。原 rev1 指纹及可重建来源保留，不倒写首审结论。

独立审查确认的设计可行性限于当前选择的完整库结果：direct/platform.memory 配置解析、持久不可变 snapshot/binding、正式 Secret planned Acquire 和确认后交付。当前授权须先于存在性错误；持久稳定主体与完整 Actor/Session plan 分层；完整锁 union、exact-Tx witness、canonical lease 并发、原 writer 确认与 Unknown 零交付均须在实现后真实验证。严格 consumer fixture 隔离尚无实现的业务事实，不能当成生产 allow 或生产 consumer 已绑定。

[独立验收短计划](evidence/current-model-resolution-spec-verification/spec/acceptance-plan.md) SHA-256 `0a716da0233b295169b68212d3428af27d02fae81fcda2f6775986437ccc05c8` 已冻结，仅是计划：先审 9 生产及 00017 的固定输入，再按作者证据选择最小独立增量。7 个新真实顶层、旧 Model/Secret/Account/MCP 适用回归和迁移 fresh/populated/失败回滚仍是后续门槛，本次没有执行或预先宣称通过。

## 00017 迁移排期的原遗漏与修正

三文档排期变更已在 `6a9c04ee741edcf8722a5d828a05eb6e3bc55891` 接受：撤回 R4 仅文档预留的 00017，转留 current Resolution；R4 待实际后继编号，不预占 00018。该提交只改文档，不表示 SQL 已实现。

[首轮独立报告](evidence/current-model-resolution-spec-verification/migration/review-freeze01.md)保留两项遗漏：R4 Initialize 仍固定写“schema 17”，旧 schema 测试条款仍写“prefix17/受保护回滚”。它们是活动技术条款，不能将 Resolver 的 17 当作 R4 completion_plan 已具备；历史采纳段的原编号则保留。

[两行修正原差量](evidence/current-model-resolution-spec-verification/migration/final-delta.patch)改为 R4 届时获分配迁移的实际 schema、连续前缀和前向/隔离恢复。[freeze02 独立报告](evidence/current-model-resolution-spec-verification/migration/review-freeze02.md) SHA-256 `9a94da8ba692ae1e12110eeeb210da430bcd81b5012ff26cbd3ba3cf9f389973` 为 PASS，另两状态页原字节未动；API、34 路径/13 目标和既有阻断保持。现在主线程另已授权 Resolver 卡内 `00017_model_current_resolution.sql` 实施；尚无该迁移的独立验收结论。

## 轻量证据与边界

[唯一证据索引](evidence/current-model-resolution-spec-verification/index.json)列出 17 份原报告/检查/计划/差量及精确 Git locator。保留原字节；不复制规格或源码树。38 项原设计输入全部按原 commit:path 核 SHA；已接受规格可由 `e81b029` 读取，结合原差量可重建 rev1。迁移三文档从固定 `3e5d882` 应用两段原差量后，与 `6a9c04e` 的三个 Git blob 逐字节相同。此处只有静态文件与差量核查，不是 Go/SQL 执行。

structured wire 的 10 源已验 `be0bd07`，115 路径归档已接受推送 `9da1e2ba8f6a2b4d44d9b9f8213509e990956e23`，沿[原报告](d09-openai-chat-structured-wire-verification.md)复用，不重复归档。Resolver 实施授权不交付 serving_snapshot、生产 consumer、材料读取/退休、Invocation/Usage、Project cleanup 或 root 装配；Summary 初值/Settings 待决，Object 原任务停止、Artifact/领域绑定阻断与 ready503 保持，完整 D09 未完成。

本次仅归档已接受决定并接续三状态页，没有 Go、npm、Docker、SQL、网络或 Git 写操作。原 patch 上下文空白按索引中的精确例外保留；新文档正常检查链接、格式及限定 diff。
