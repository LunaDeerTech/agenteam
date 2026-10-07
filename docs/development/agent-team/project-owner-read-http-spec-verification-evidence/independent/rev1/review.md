# D08 Owner 读取 HTTP rev1 独立 STATIC

**完整静态审查已完成；需闭合一处协议表述后才能给完整 STATIC PASS。** 输入为冻结卡 `0ac1ab4813ea56fcdb18bff95d122e383af55d2953dc01f626578faa94e59aff`、作者 manifest `a5d2d2867dfca0956db7266dc06d3da248eb36e02f08fcde6c776b0ba4c1e162`。本验收者未参与编卡；已读 design/verification 技能和必要治理增量，仅读 baa6ffac 固定依赖及 c210 的四份 app 来源，未把活动 S2 实现当已接受输入。

唯一需明确项 **D08-STATIC-01，卡第80行**：既有 `HTTPBoundary.WriteProblem → httpapi.Problem` 和 `common.json` 没有 CauseID/cause_id 输出字段或头；Project `commitError/UnknownAttempt` 的 CauseID/attempt 是内部原事务证据。“保留 CommitState/CauseID/RequestID/RetryHint”将内部与wire信息并列，容易迫使后续作者扩大公共Problem接口，与只读公共映射/精确白名单冲突。建议仅修卡说明：内部 Fault/UnknownAttempt 保留原 CauseID/attempt，HTTP 沿既有 code/commit_state/request_id/retry_hint，绝不新增 cause_id 字段/头或改公共Problem。该问题已报root与原作者；验收者未改卡。

其他完整边界核对通过：

| 边界 | 固定实际依据与结论 |
| --- | --- |
| 窄 Reader | Service目前构造要求Activity/Audit/Events/Processes并注册初始化actor；新Reader只需现有Store、同实例Authority和有效cursor，能在新reader.go共用私有helper，不需假写依赖。原Get/List的begin/done和旧命令、Resolve保持。 |
| 权限和状态 | Authority.current在RequireHeld(UserSH/ProjectSH)后查当前Session，ownerProject隐匿另一Owner/admin为NotFound；Read仅允许已初始化active/archiving/archived，deleting/未初始化ProjectNotActive。列表仅UserSH+当前Session+Owner SQL，明确不是整页Project锁快照；不Touch、不写receipt/Audit/Event。 |
| 分页与哨兵 | 原owned-projects-v1/System scope/Owner+规范filter+created_at-desc,id-desc digest，两个instant/UUID scalar且无generation，不绑Session/limit，均与卡一致。原scanProject已查完整Ref及creation/operation等ID，卡新增哨兵的item操作字段、Owner/filter/顺序/数量完整校验和Committed后取消，避免先裁哨兵漏验；Rows.Close/Err和游标签发须终局前完成。 |
| DTO与schema | 四lifecycle准确条件字段、详情11字段、两个显式null、deleting隐藏正文、正int64字符串和原UUID/Instant一致。默认转义容量上界5026048B低于5MiB，216832B余量；这只是算术/字段有界核对，真正最大合法页和标准schema仍是实施验收项。 |
| HTTP与IO | 静态resolve显式排除，nested Usage不抢占；非法ID单段安全400，其他方法405/Allow。Account原CheckRequest确实拒RawPath/不净路径；正式Human与事务当前授权分层、strict query/实体EOF/HEAD同查询编码、Problem固定instance可沿已验Usage模式。总2s从预认证起，同步终局、短写/Flush/Close/callback join与deadline清除、abort不补第二Problem已写清。 |
| Unknown可实现性 | 原project.UnknownAttempt保留真实CommitResult；同tests/model内已有projectModelPGTrace，能在实际COMMIT+idle响应时断开并记录安全阶段，可由自有胶水在目标Reader回调完成后arm，原helper不需修改。卡正确区分真实协议ACK丢失与受控CommitResult装饰，读重试不是原提交确认。 |
| 默认根和交接 | c210的projectUsageAssembly已有唯一Project.Authority，可由新app文件与account.go局部调用保留同Store/Account/Cursor；不加Initialize/worker/readiness。S2必须先完整接受、源停写和唯一移交account.go，其他三共享源只读；固定旧覆盖不能冒最终S2共同根验收。 |
| 可执行验收 | 原六个回归名全部在baa6ffac中静态存在；三native/三PG/两独立组合分别约束真实身份、正式Create+持久Skills测试边界、当前撤销双顺序、锁/坏哨兵、真实Unknown、默认app.Run/路由/schema/HEAD。45s离线/native、120s PG顶层含Cleanup、包6m、单资源窗/actualwait/七资源双清/daemon差量均明确。 |

精确16技术路径+末件README共17路径，无重复；当时14个拟新增技术路径均不存在，不与S2产品路径争写，既有service.go与account.go职责受限。26本地链接全部存在且无fragment，空白与围栏检查通过。必要来源与命令hash见 [source-input.json](source-input.json)、[additional-input.json](additional-input.json)，文档/容量/名单检查见 [document-check.json](document-check.json)。只读git show/grep实际退出和来源保留；一次辅助git读从scratch错误cwd失败后已改为固定repo读取，不是业务测试失败。

未运行Go、schema执行器、浏览器、网络、数据库或容器；没有业务/卡片/Git修改。修订应由root协调原作者在新freeze中完成；此原rev1报告保持不改，后续仅核该措辞差量。规格采纳仍不授权实施或证明新HTTP已交付，不改变创建/lifecycle/UI/D24/完整D08–D28/E01和三停止边界。
