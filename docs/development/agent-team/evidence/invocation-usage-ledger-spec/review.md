# D09 Invocation / Usage ledger rev1 独立规格静审

结论：**STATIC PASS**。未发现确定实施阻断或必须修改的规格条款，可由主线程采纳后另授原 20 路径实施；本结论不表示代码、迁移、性能或真实运行已经通过。

固定输入为 `4295df7d51c1f171df78ab3f0d9cef2fd241a505` 与 [rev1.md](rev1.md)，卡 SHA256 `d53835cb79618b5cea487e8d84161cdc2e4f3a9900c6d35a86a258f75f795cab`。已逐字读卡，并核 19 个原输入的 SHA256 / Git blob / 字节数，以及 13 个必要接缝的既有冻结指纹；定位见 [input-checks.json](input-checks.json)、[additional-inputs.json](additional-inputs.json) 和 [review-checks.json](review-checks.json)。未把工作区其他活动候选当作依赖。

1. **范围与可实施接口。** 卡 §2/§9 的 9 生产 + 1 迁移 + 10 测试共 20 唯一路径足以承载所述库级结果。已有 AttemptIdentity / InputIdentity.ValidateFor、Usage C0、Store 原 Tx / RequireHeldLocks、Project Read、Session 与 cursor.Keyring 可直接消费。新增 ModelRuntime 只扩闭集角色名，不获得业务权限。真 nil Facts 支持 Reader-only，Writer 必须 Unbound；typed-nil Chan、同 Store 构造和 foreign/expired Tx 边界明确。当前无证据要求扩大 C0、D03、root 或生产 Model/Secret 接口。

2. **事实与权限。** 卡 §2–§4 把 canonical call/attempt/input/snapshot/事件证明留给受信 owner：公开 DTO / issuer / caller digest 不自行授权。Discover 不泄露历史 Value；锁后在 provider 原 Store / 原 Tx 重读完整事实。reserve 与 authorized 重验当前准入；sent/usage/finalize/confirm 使用精确 Project + Invocation CauseRef 的技术权限，不能获得新发送资格。完整 union 包含真正原 writer、当前授权、本域串行及摘要锁，外层一次 AcquireAll，InTx 不 Discover / 补锁。Reader 的 User SH + Project SH、当前 Session / Owner Read 先于可区分的资源结果，归档读取和删除 gate 沿正式权限。

3. **旧序号确认可实现，无 latest 映射矛盾。** §2.1 Mapping 绑定请求所指 canonical 事件；§3 要求不可变持久观察，§5 按原 Action/Sequence 保存与读取 exact receipt。provider 必须按请求读取那一条旧事件，再独立核本次技术权限，不拿最新 attempt 投影替代旧事件。权限版本变化可以要求重规划，不能因此把旧序号的事实改成 latest。回执同义比较排除 Session 和事后 live FK 空化；返回 live 指针仅投影当前列，不修改原 receipt bytes / digest。这个要求已在原卡闭合，无需增加接口。

4. **事务、状态和 Unknown。** 同 call 原锁、完整 immutable header、连续 ordinal/sequence、旧号原回执、真实前驱终局与单活动 attempt 共同约束并发。三表同原 Tx，写前完成可判定验证，单条原子 SQL 等写法可以满足“忽略入口错误也不能半写入”，但仍需实现与真实 SQL 反例验收。canonical observation 先持久化再写账的 fixture 顺序避免 Runtime finalizer/join 构造环。外层 Committed 才确认 provisional receipt；Lookup 自身 Unknown/锁阻/权限错误不是 Observed=false。DB Unknown 与 dispatch unknown 分离；拟用实际 PG 结果装饰及普通锁，只证明后态/回执协议，不能宣称网络 ACK 故障。

5. **统计和查询。** §5/§6 的 confirmed 分母、dispatch_unknown 单列、not_sent 不计发送、六个独立 nullable 字段、0/NULL、前后贡献及 numeric/int64 溢出规则与现有 C0 相容。摘要 EX 锁使重建与写入可串行，回执重放零增量。List 的同一 SQL snapshot 上界/页面和 Aggregate 的完整分组后分页可由现有 Store 实现；真实 Keyring 能容纳所列至多 8 标量及 8 KiB token。稳定 User 而非 Session 绑定允许新合法 Session 续页，各页仍须当前授权。卡没有承诺跨页 MVCC 快照。2s 全调用预算、大数据 EXPLAIN 和零部分结果属于后续实际门槛，静审不预先认定性能通过。

6. **历史删除正例可达。** 固定 `model/references.go:59` 只对实际 Model references 做替换检查，零引用返回成功；`configuration.go:78` 要求 Provider 已无 Model，正式删除顺序可为 DeleteModel → DeleteProvider。`model/secret_authority.go:11` 为删 Provider 规划正式 ReleaseReferenceUsage；`secret/usage_plan.go:107` 只删对应 provider reference，不释放或否认已有 Secret lease。00017 的 live FK 是 SET NULL，snapshot/binding 可保留。既有 `current_resolution_selection_test.go:210` 已有正式删除后 snapshot live FK 为空且历史重入的相同接缝；本次只复核固定代码，未重跑该旧组。故新 00018 Usage live FK 的相同设计不要求绕过 Agent/summary 引用，也不要求本卡实现 lease release。§10.7 的新 Invocation 历史场景仍须作者真实执行。

7. **迁移与真实验收边界。** 00017 的 snapshots(id,project_id) 唯一键满足新复合 FK；三表的安全 DTO/loader、约束、无跨域 FK、旧数据/全约束保持、PG17 fresh / PG16 populated、同字节失败重试及只 Up 要求明确。00018 当前仍只是设计预留，无 SQL 实施或部署授权。八个新真实组要求非空数据、真实 Resolve / Secret planned read / exact Exchange Decision与Joined、当前 Human 查询、强回滚/Unknown/游标/摘要/历史删除证据；现有正式服务和新测试文件可构成严格持久 fixture。测试 owner / Secret planner 不能被称作生产 Runtime 绑定，不能复用手造 SecretMaterial 充当实际 Secret read。

未决实施验收包括原子 SQL、全部查询预算及强反例真实命中；它们是卡内已有门槛，不是新增规格阻断。生产 Facts owner、call/Provider 编排、Model read/release、Outbound 委派、Process/lease/lifecycle/HTTP/root 仍未绑定；Summary 产品决定、ready=false / 503、暂停 Object 与 Artifact 边界保持。此次未运行 Go、SQL、Docker、网络或 Provider，未写仓库、未执行 Git、未再委派。

独立审查已冻结，all-stop；后续实现与动态验收须另授。
