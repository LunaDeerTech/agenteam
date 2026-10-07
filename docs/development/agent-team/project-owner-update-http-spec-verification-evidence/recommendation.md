# D08 Owner 读口之后的有界前沿建议

2026-10-07，仅只读核对与 scratch 建议，没有 Git、Go、网络/资源执行或新产品实现。本建议不是正式规格或独立接受。

固定前置：原已接受 S2 产品基线 `4e615c7da2875418e1fbd925a49f134f14feae6d`；本次 Owner read 完整17路径已独立 PASS，并由 root 确认产品提交推送 `901eb54605d293d4308caadd278c2c3a7ae1b824`、远端一致。技术 candidate04 manifest SHA `097d47b4f4cdb5c92a102f62f6398c0d691bc918111c76b95a7972cd54004b38`，技术 final01/review.md SHA `e10e890b2d91698c7b290b2f31014d4d0ec00ac9ce5148ab9183b0f58fa58d9c`，完整 final17/result.json SHA `380f0b0cc68e0ee59e88450d00e0795e213aa319355f033d6a20fae77a128b7a`。下一卡以 `901eb546` 为已接受产品基线；核对源码/文档的逐文件 hash 见同目录 inputs.json。

## 推荐：D08 Owner 名称/描述更新、原 update 命令查证与默认根

这是首选完整结果：Owner 能通过正式 HTTP 修改已有项目的 name/description，收到原命令结果；丢失响应后可以在当前授权下查证或同 key 重放。后续 GET/list/resolve 能读到同一稳定 ProjectID 的新配置，旧可读名称立即失效，不引入别名。复用已实现的业务规则，不扩大为创建、归档/恢复/删除或 UI。

真实前置已足够：

- D08 B01 `199554b`、B02 `769ec8c`/`6319d03` 已接受 Update、Lookup、名称唯一、expected_version、幂等/Unknown、Project Audit/Event 原子性。主卡 `docs/development/work-items/d08-project-owner.md:60–76` 记录范围、20 真实主例和首红/组合限制；不是只有接口声明。
- 实际 `project/commands.go:60` 的 UpdateProject 仍有当前 Session/Owner → 可见性 → 同 key receipt → 首次 Mutate/version/名称顺序，`:306` 的 LookupCommand 已存在。`contract/commands.go:104` 的请求只允许 name/description presence patch。`project/audit_authority.go:91` 验真实 ProjectUpdate，`project/events.go` 与 `contract/events.go:132` 已有正式 typed 更新事件。
- D07、D04/D06 及默认根已有真实 Account Activity/Audit/Outbox/Process authority；本次 Owner Reader/HTTP 和已接受 Usage resolve 可直接回读与验证修改结果。架构 `docs/architecture/project-work-management/README.md:68–74` 已确定改名、稳定 ID、旧链接失效规则，无需另问产品。

必须同一卡补齐的端口，不可只加 handler：

1. 严格 Owner HTTP mutation + **仅 update** 的原命令查证投影；复用正式 CommandMeta/UpdateDigest/Lookup 语义，当前 Session/CSRF/Owner、不可逆 gate、历史 receipt 与当前 GET 分开。查询并不授权自动重发；Unknown 保原 key/body/version，HTTP 不猜提交结果。不顺带开放 Create/Archive/Delete/Restore/Retry lookup。
2. 默认根目前 `app/security.go:15` 仅给 Audit Accounts/System/Models，`app/account.go:314` 的 Outbox 无 Projects gate/Project producer，且没有 Project Service。须把现有同 Store/Account 的 Project Authority 提前构造，给真实 Audit Projects、Outbox Projects/producer 和 typed catalog 接线，再构造只向该 HTTP 暴露 Update/Lookup 的真实 Service；同一 Authority 继续供原 Usage/Reader。不得用可变 locator/allow 或另开私表授权。
3. `project.New` 的实际必需 deps 在 `project/service.go:26–79`；Initializer 和 LifecycleRegistry 可空，Update/Lookup 不依赖它们，不调用 RecoverCreations/创建后台工作。ProcessAuthority 使用已有真实 root 固定 process/guard，不造 process，也不改变 Object runtime。Service 已有 Stop/Drain/Force（`:117–152`），但没有 root `Joined` 接口；私有 root adapter 必须以实际 Drain 终局记账，不能把 Force/cancel 等同 join。纳入原关闭预算与 DB 最后关闭，完整证明新 HTTP work 返回。

建议工程卡边界：Project HTTP 新命令处理/DTO/schema/测试；app 新窄写装配与 account/security 的必要真实路由/授权注入及测试；必要的既有 Project 命令差量仅在发现具体公共口问题后授权。准确白名单留正式卡核定。无需新迁移、不改 D08 contract 语义，不注册空 Outbox consumer，不启动生产 Skills/lifecycle/Resolver，也不修或重启三停止。

最小验收：正式账户身份与 Owner；当前权限撤销先于旧 receipt；同 key 跨当前 Session、异义/版本/无变化；两项目抢名与旧路径失效/可复用；真实 Audit+Event+canonical+receipt 原子性；真实 COMMIT ACK Unknown/原 writer 查证、失权不泄露；自然 HTTP 预算与 Body/Write/Flush/Close/cancel 尾部；默认 app.Run 新写→GET/list/resolve 回读、原 Usage/Summary 路由及关停。Project fixture 可沿已接受持久 Skills 测试口，但不能宣称 fresh 生产创建已绑定。

## 可执行备选：D09 Project 配置和安全可用目录的 Owner 只读 HTTP

已接受 `de00c610` 的五查询位于 `model/project_query.go`：本 Project Provider/Model list/detail + System/本 Project enabled chat 安全目录；验收来源 `docs/development/agent-team/d09-project-configuration-verification.md:1–37`。它已证明当前 Owner/Session、scope/cursor、七字段安全目录、真实 Secret/Model 事实，不能再把它列成未实现业务库。当前 HTTP 只有 System routes（`model/http.go:41,97`），默认 `model.Authorizations` 未注入 Projects（`app/account.go:247`），这是尚未绑定的真实缺口。

完整备选卡可只交付这五个 GET/HEAD、安全 schema、同一 Project Authority 的 root 只读接线和真实权限/分页/敏感 canary/Unknown/native/root 验收；不开放 credential/config mutation、Agent 引用替换或 Resolver。它只依赖已验读能力，也不需要 Object/Skills。但相比首选，Owner 元数据写入口仍空，因此排第二；若首选 root shutdown 规格揭示新的未接受依赖，可转此完整只读结果，无须强拼大卡。

## 不能据当前增量提前启动的链

- **Project 初始化/完整生命周期**：D10 P1 builtin/package 已在 `8872110` 接受（`d10-skills-initialization.md:3,28`），不是旧 development-plan 底部的“待独审”。D05 Object checker `a716ae2` 也已接受，不再是“checker 不存在”。但 `project/audit_facts.go:18–34` 仍拒 ObjectProducer，初始化专用事实/失败收敛、Skill 持久服务与 revision cleanup/真正 initializer 未交付；`internal/central/skill` 仍只有纯载体/builtin/contract。不能用测试 Skills 或纯 ZIP 当生产初始化。
- **R4 / D12 Knowledge**：Artifact 16 源只有本域有限验收、未提交，Object join 缺陷使共享 guard 和领域绑定仍 BLOCKED，见 `artifact-project-stop-verification.md:3,38`。`recovery-project-domain-bindings.md` 的 STATIC 不是产品绑定。Knowledge 仍只有12 contract，旧 B02 的通用 Project Human Outbox gate 也不是现存 Model-only `project/external_event_authority.go`。不缩成 SQL seed 只读服务来宣布 B02，亦不以这些卡重启 Object 停止。
- **Meeting/Resolver 生产消费**：Summary S1/S2/S3 已接受，不能重复做 selector/设置/解析库。S3 报告 `system-meeting-summary-resolution-verification.md:9,39–41` 明示严格 Meeting canonical 仅测试事实；生产 ConsumerAuthority、Meeting/Operation/Turn/Call 状态、D24 finalizing/标题与四字段提交、InvocationFacts/发送恢复均未绑定。`development-plan.md:83–99,105–117` 的 D24←D23←D22←D21 真实链仍缺；只给 root 填 Resolution 或拼 wire+库不能成为 Meeting 完整结果。D10 普通变量可以另作新 canonical 服务方向，但目前无该服务/工程卡与生产通用事实绑定，不优先扩成新计划。

本次无新增产品未决问题。用户已定系统统一会议 Summary，不扩大到其他 Purpose；三停止、生产 Resolution/Invocations nil、ready503、完整 D08–D28/E01 未完成及 E01 未选定/未启动保持。建议 root 以已接受 `901eb546` 只授首选的一张有界工程卡。
