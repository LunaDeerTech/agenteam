# M3 Core 独立局部审查

2026-10-14；审查者未参与所验实现。D13 输入冻结为 selection `e1964240`，Model 输入冻结为 `784c6468`，Model 差异基线 `d9356e09`。仅补读 Knowledge `5d65c0a3` 的三个 Lookup bridge 文件；当前开发树中这三个文件相对此修订无差异。本文接受固定比较证据，不代表整个 Model、产品领域权限或 M3 接受。M4 不在本次范围。

**结论：D13 数值与范围判断无 must-fix；Model 本次所读新增 Runtime、v2 adapter、共享 Secret/Usage 和 pre-admission retirement 未发现新增已证缺陷。活跃 serving generation 的长期凭据 retention 仍是相应持续服务行为的 must-fix；取消后的 Unknown 恢复尚未完成独立行为验证。**

## 固定 D13 复核

独立脚本未导入作者的 cosine/RRF 实现，使用原始向量的 `dot(q,d)/(norm(q)*norm(d))`，分别生成 top20；RRF 对两路 rank 按精确分数 `1/(60+rank)` 等权求和，缺失路为零，按 ASCII source ID 打平。288 份 query 排名、5,760 个 hits 全部匹配归档，最大 score 绝对误差 `1.1102230246251565e-16`。四次原 `scripts/search-benchmark.py score` 均退出 0，全部 groups 和 no_answer 与归档逐项相等。

| 路径 | nDCG@10 | Recall@20 | MRR@20 | 无答案有返回 |
| --- | --- | --- | --- | --- |
| Dense | 0.9868422792946057 | 1 | 1 | 8/8，各 20 |
| Native FTS + dense RRF | 0.9868422792946057 | 1 | 1 | 8/8，各 20 |
| PGroonga + dense RRF | 0.9868422792946057 | 1 | 1 | 8/8，各 20 |
| pg_search + dense RRF | 0.8936382638653616 | 1 | 0.9583333333333334 | 8/8，各 20 |

pg_search 融合相对 dense 的 64 道有答案题为 34 退步、2 提升、28 相同；另外两个空 lexical leg 的全部 72 条排名与 dense 一致。总体指标的分母是 64，无答案 8 条独立统计。

已核同一 snapshot `01a13b76-ce43-7dcc-890e-af5f5129f0e0`、64/72 唯一且顺序一致的 ID、4096 维有限非零向量、gzip/解压正文/原 result 与两份 export 哈希、两份 lexical run 哈希及其 export 来源。三批 64/64/8 的 Runtime input digest 亦独立按 Go JSON 字节协议重算，绑定原文本顺序和维数。源 manifest 与 selection 副本一致。原 native run 缺失，空结果仍仅复用 D13 §1.1 已接受历史事实；没有把派生输出写成找回的原件。

执行：`python3 /workspace/m3-core-independent-review/check_fusion.py`，成功输出在 `/workspace/m3-core-independent-review/fusion-result03.txt`；脚本通过原 CLI 导出固定数据、读取冻结向量和 lexical 排名，并对归档解压运行文件调用原 scorer。前两次独立 harness 序列化被原 scorer 的文本 framing/行长约束拒绝，纠正为有结尾换行的缩进 JSON 后完成，无产品或作者产物修改。另一次短 Python 检查完成三批 input digest 对照。临时脚本/输出可由上述冻结输入再生，不作为新的长期证据包。

selection 冻结修订中的 `docs/development/work-items/d13-lexical-benchmark.md` §1.4 对实验限制及范围的判断成立：当前结果不证明拒答、引用正确性、生产 scope/current/serving 过滤、延迟、容量或 ANN。4096 维 exact cosine 结果不能当作已验证 pgvector ANN。原架构 [§3、§22、§28](../../architecture/knowledge-memory/retrieval-runtime.md)仍约束首期 hybrid、真正 lexical unavailable 时的降级和等权参数；dense 优势不自动授权改首期范围。用户选择未到，本审查不替用户决定，也不在已见 test 上调参。

## Model 边界与门禁

| 检查范围 | 所读实现与可复用证据 | 本次判断 |
| --- | --- | --- |
| Nonchat、结构化输入、Runtime 生命周期 | `consumer_input.go`、`runtime_embedding*.go`、`runtime_attempt.go`、`runtime_stream.go`；正文顺序/维数和 schema 内容纳入 digest，text 摘要保持原投影；BeginEmbed 保留原 admitted handle，结果在 Finalize/retirement 确认后发布，Close/Joined 沿原 owner | 未发现新的授权绕过或兼容回退；未以受控 consumer 代替领域权限验证 |
| Serving snapshot 与迁移 | `resolution_serving.go` 按 project/snapshot 取不可变事实并验摘要；consumer 在锁内提供 generation 权限；`00106` 仅取消 snapshot 单列唯一限制，保留每 call preparation/lease | selector drift 后复用 snapshot、新建每 call lease 的既有受控证据可复用；不证明长期 retention |
| Pre-admission retirement | `call_retirement.go` 绑定 actor/model/终态版本和私有 plan；核 consumer、锁、当地 live call、未 retired 持久 call 与精确 preparation/snapshot/lease；Secret 观察仍先走相同授权 | 现有无 admission 清理正常链可复用；公共 DTO/旧 plan 不自行产生终态权限 |
| v2 embeddings 与 Usage | `embedding_json.go`、`embedding_transport.go`、`openai_embeddings.go`；只有 v2 接受额外 `completion_tokens` literal `0`，v1 仍严格拒绝；非零/null/负数/小数/指数/重复/其他未知字段的作者反控可复用。`ValidateInvocation` 仅放行严格 embedding 关联投影，真实输入仍在 Runtime 单独校验 | 未发现 v1 放宽或伪造缺失 usage 为 0；共享 ProjectInvocation owner 和 Secret witness 未被旁路 |

复用 Model 冻结修订中 `docs/development/work-items/m3-model-runtime.md` 的分轮事实：39743 包级通过；10276 前两个 top 通过但整轮第三个失败；80228 pre-admission top 单独通过；67632 定向纯检查、31270 v2 受控 top 通过；2079 原 Wait0、43.24 秒、三批 HTTP200/4096、原 Usage/Secret/Close/Joined/Drain 与 D03/D04/D05 清理。已直接核对保存的 `result.txt` 和 manifest，并对照真实 harness 的终态断言。未重放任何外部付费请求，也未将旧失败改成通过。

**必须补齐的已知存续缺口。** `internal/central/model/secret_authority.go:11` 的 reference 随 Provider 配置持有和替换；`resolution_serving.go:17` 只读取 snapshot，每次解析只取得 call lease。`internal/central/secret/write.go:319` 的删除门只看 live references 和未 released leases。最小场景：generation G 正在 serving → 所有当前 call 已结束、lease 已释放 → Provider 从凭据 A 换为 B（或删 Provider）→ A 无其他引用时被合法删除 → G 下一次仍按 snapshot A 解析，无法再取得原凭据。snapshot FK 不能阻止这一情况。此为代码路径确认，未在本轮新启动 PG 复现；它阻止 M3 声称支持该配置变更后的连续 serving，不阻止无配置删除风险的隔离首链。相同 CredentialRef 仅更新 value 属于原轮换机制，不应与替换 ref 混淆。修复范围应限于 generation-to-snapshot retention/retirement 和同事务领域证明，无需扩大全产品 GC。

**Lookup 的结论与下一门禁。** `resolution_lookup.go:39` 仍用 `resolutionConsumerRequest`，后者在 `resolution_plan.go:240` 固定 `ResolveConsumer`。Model 自身在原 command 锁下只读确认 committed 解析，不 prepare、acquire 或 dispatch；但 consumer callback 的权限及副作用必须单独核验。当前 benchmark 仅证明活跃原解析的正常 Lookup，不证明取消后的恢复。

Knowledge `5d65c0a3` 的 `index_resolution_lookup.go`、`index_model.go`、`index_model_authority.go` 私有 bridge 静态边界合理：只在原 `op.mu` 下，将 same-op、resolveUnknown、terminal=failed 的私有 context 交给 Lookup；原 actor/call/digest/process/持久 operation/snapshot 仍核验，该 context 未传给 ResolveModel/Embed。共享 action 本身不是已证越权，独立 Lookup action 可使契约更明确；尚不能把私有 bridge 写成已完成行为接受。Memory 私有恢复和 Meeting 恢复本轮亦不形成产品接受结论。

下一项有界检查应只覆盖：原 Resolve commit Unknown 后取消/撤权或 serving 变化，原 owner 通过 Lookup 确认原 snapshot/lease 并退休；prepared/不存在分支不新增 lease；wrong operation/project/process/terminal 不获恢复权；观察无新 preparation/acquire/provider dispatch。未完成前，阻塞相应 Unknown 清理行为的正式接受，不扩大为所有 M3 工作停项。

本轮未启动 Go、PG/native 或模型请求，未使用 dev 数据库，未修改实现/作者测试/current，未执行 Git 写操作。唯一仓库写入为本文；离线命令均已退出，无后台资源。报告冻结后由 root 保存。

## Serving credential retention 追加静态审查

2026-10-14；固定 Model `0156924c`，仅审实现组 `b27b1961`、Observation 修正 `65c13796` 及对应 contract、Secret observer、`00115` 和三份 `tests/model/serving_retention*_test.go`。**本次有界无 must-fix。前文截至 `784c6468` 的存续缺口在 Model/Secret 受控层已有实现与适用证据；这不完成 Knowledge/Memory 实际 App generation 发布/退休链的接受，也不改变原 Lookup 未验结论。**

核对结果：

- 私有 `servingRetentionPlan` 以所属 service 实例和完整 request binding 拒绝替换；consumer 原 issuer/mapping 由真实 authority 再验，Secret provider plan 绑定 Model 私有 issuer、request/mapping。每次 Apply/Applied 都先验证本 Store 的事务、完整 consumer/project/generation/snapshot/credential/reference 锁并调用领域当前授权，公开 owner DTO 或 snapshot FK 不产生权限。Secret witness 限原 plan、精确 Usage binding、活跃 callback 和同一 Tx。
- Model 行固定 project/kind/generation、agent、snapshot 摘要和 credential tuple；retain 必须等于初始 owner_version，release 不得早于初始版本，已 released 只接受相同 released_version，不能重新 retain 或改绑。Secret 长期 reference 使用独立 owner UUID，与 Provider reference、每 call lease 独立；Model 状态和 Secret 引用在同一外层事务更新。释放后保留的 Model 行可在凭据删除后继续作精确只读观察。
- `serving_retention.go:249` 分开 Observation 和 Apply 的 consumer 方法，Applied 的私有 witness 只交给只读 Secret observer；Apply 始终走目标状态的写授权。原计划能够观察尚 active 的 release 返回 false，并不因此得到释放 active generation 的权限。Model/Secret observer 本体无写入、lease acquisition 或 provider 调用；领域 Observation callback 也必须保持只读。
- `00115` 只新建账本和索引，无存量 generation 自动授权回填；复合 snapshot/project FK、owner 形状、credential scope、retained/released 版本与时间约束符合当前契约。既有服务仍须在首 call lease 存活期间完成 retain，并在真实 generation 状态转移的同一事务发布或释放；核心接口不会替领域保证调用时点。

复用并直接核对原 `output/m3-model-checks/retention-native-01.log` 与 `retention-unknown-native-01.log` 的 PASS、actual_wait=0 和三类 exact cleanup：75483 的 retention top 10.79 秒，91453 的 upgrade top 8.37 秒、release Unknown top 12.07 秒。已对照 harness：A→B、删除 live Model/Provider 后原 snapshot 三次 Runtime、两个 generation reference 最后释放才允许删 A；裸 DTO/错误 tuple/缺锁 poison/外层 rollback/logout 拒绝。Unknown 在最终 COMMIT 才启用原代理，分别不转发 COMMIT 或丢弃已确认 COMMIT+idle ACK，并经独立连接取得原锁确认 writer 已退出；同一原计划观察 false/true，Model/generation xmin、reference count、解析/call/lease 数和 dispatch 不变。先观察 false 再 Apply active generation 仍拒绝，直接覆盖此次权限分离。

证据限制：上述 generation authority 是受控 fixture；未接受真实 Knowledge/Memory 当前权限、ProcessGuard 停止竞争或 App 装配，也未把 release 的物理 CommitUnknown 扩称 retain 的物理 CommitUnknown 已测。升级测试六个 SQL 负例实际通过，但 `released missing version` 同时缺 `released_at`，不能单独归因到版本约束；生产 SQL 明确要求非空 released_version，属非阻断的测试归因限制，已通知作者。本次没有发现需要扩大全 Model、全 M3 或 GC 的理由。

本次只追加本文，未运行 Go、PG/native、模型 API 或修改作者文件；检查了报告链接目标、末尾换行及空白。所有只读命令已退出，无后台资源，停止写入交 root 保存。

## MCP OAuth Secret 与 media 追加静态审查

2026-10-14；固定输入 `/workspace/agenteam-m3-model` 的 `359c4ff6`，仅审 OAuth Secret 组 `ee649cc1`、`41221049`、`468b0e69`，Project `7b34f95a` / Model `3ffa`，Audit `529f9c13` / Model `9b7f9648`，以及 media `66640c00`。**本次有界无 must-fix；结论只覆盖 Secret 技术写口、Project/Audit 窄路由和文本 tool profile 的媒体前置拒绝，不接受实际 MCP OAuth 交换、刷新或整个 M3。**

- `secret/contract/mcp_oauth_write.go:47` 将 SecretService、Project、connection/config revision、flow/token owner、command、CAS 与材料摘要绑定；Account owner 闭集未扩大。`secret/mcp_oauth_write.go:123` 拒绝外来或另一 writer 的 sealed handle，Prepare 后只保留封存值和无材料 request。Apply 在同 Store 事务中先核完整 Secret command/control/Project/credential 锁及领域依赖锁，再调用领域写授权，之后才读 receipt 或更新值；更新/删除仍检查 MCP purpose、精确版本，删除仍检查引用与活跃 lease。公开 DTO 和 Service actor 本身不授写权。
- `audit/service.go:243` 仅把项目内 SecretService 的 create/update/delete 委托给必需的 Project checker，generic `serviceOwns` 保持原义。`project/audit_facts.go` 仍核 actor/project、原 Project 共享锁和当前 initialized/active 状态，原 context/Tx/Entry/Key 透传到 Secret。`secret/project_audit.go:74` 与 `mcp_oauth_write.go:197` 再核同 Store/Tx 的私有写证明、command/receipt/版本与 canonical postimage，错误 MCP marker 不回退到 Human 路径。对应纯测试覆盖裸技术 actor、错 Tx/store/receipt/command、缺锁以及 archiving/archived/deleting/uninitialized Project 的拒绝。
- `secret/mcp_oauth_write.go:148` 的 Observe 只接受原 sealed handle，先核完整锁，再调用独立 Observation authority，并解封比对原 receipt；没有 Prepare、Apply、ledger mutation 或 HTTP 路径。取消后只读原结果不等于写授权；真实领域仍必须实现私有原 owner 与 before/after 持久事实核验，不能只返回成功。

直接核对 `.agent-state/m3-model-runtime/oauth-secret/result.txt`、原 `output/m3-model-checks/oauth-native-02.log` 与 `tests/model/mcp_oauth_write_test.go`：56569 原 actual_wait=0，SealedWrite 5.47 秒、RealCommitUnknown 10.01 秒，D03/D04/D05 三项 exact cleanup。前者真实调用 Account 当前 session、Project owner gate、Secret/Audit，覆盖 create/update/delete 的 3 receipt / 3 Audit / 0 Model call，以及裸 DTO、缺锁 poison、外层 rollback、stale CAS、取消后 Observe 与 Apply 拒绝。领域私有意图、process/config revision/owner 映射来自受控 fixture，未冒领真实 MCP authority。

Unknown 两分支均在原外层事务的 Secret/receipt/领域 postimage 已写入后才 arm：一支不转发最终 COMMIT，另一支只丢弃已收到的 COMMIT+idle ACK；独立连接取得原完整锁确认 writer 已退出，再用同一 handle 观察 false/true。前后检查的是领域行 xmin/state/credential/version、Secret/receipt 数量和 Model call 数，不是全部账本逐行摘要。核心 Observe 的只读实现与这些检查共同支持本端口无再写/再 dispatch；物理 Unknown 仅覆盖 create，未扩称 update/delete、实际 token HTTP 计数或冷启动恢复已测。36560 的原 `oauth-native-01.log` 明确 Wait1，create 审计失败且两子例未到 arm，未算入成功证据。

`model/runtime_tools.go:25` 对文本 tool profile 的 File/Image result 返回 CapabilityUnsupported；`runtime_attempt.go:131` 的调用顺序在 consumer discovery、Usage 获取与 wire 准备之前，原 canonical parts/business refs 不被改写。`runtime_tools_test.go:159` 覆盖 File/Image 的 BeginChat nil handle、零 consumer/IO、引用保留和 Drain/Joined；复用 10458 原 race Wait0 及旧 tools/auto 结果，不重跑。这里不提供媒体投影、对象读取或多模态模型消费能力。

本次仅追加本文，未执行 Git、Go、PG/native 或外部 API，未改实现、测试或其他文档。检查报告链接、空白和末尾换行后冻结；所有只读命令已退出，无后台资源，交 root 保存。

## Skills routing、MCP audit 与 Owner credential 追加审查

2026-10-14；只读 `git show` / 限定 diff 审查 `0992936b`、`d3216b59`、`a89f4a4c`、`82d30a98`、`f6dfb2e6`；上述目标路径在组合 `c7951f6b` 相同。未读取活跃 App/Object WIP 作为固定验证输入。**五组源码有界无已证 must-fix；新 MCP credential HTTP 的完整行为与 Skills 全链仍未接受。**

- `00123` 的 successor guard 与 `00121` 原函数逐字比较，函数体仅将两处 JSONB disposition 集合别名改为 `dispositions_row.value`；原 cutoff、ingress、binding、control transcript 校验未改变。原 `TestSkillRoundSQLPair` 实际取迁移后的生产函数，只将 schema 指向临时账本，覆盖 unchanged/applied/skipped 与缺 disposition、外来 binding、错 control 三个反例。这证明函数分支，不能替代生产 FK、deferred trigger 和完整 App 事务链。
- `00125` 相比 `00105` 原 operation_name 集合恰增 `skill.install/list/read/assign/prepare`，无删除。Execution 的 candidate、load、storage 与 acceptsToolBinding 共用原捕获 binding 的闭集判断，拒绝混合 MCP/Builtin/Runner 及未知 revision；install 仍走其独立账本，不进入 DomainBuiltinBinding。SQL probe 复用真实迁移后的 CHECK，实际有 5 个 Skill 和 6 个既有名称合法、4 个未知名称拒绝；不是 5 个完整 Skill 操作已测。`d3216b59` 同带的 Knowledge EmbeddingProfiles 注入不计作本次 Skills 行为接受。
- `a89f4a4c` 只允许同 Project 的 AgentRun SecretResolve/MCP 元数据及完整 execution/operation/tool/request association 进入原 Secret provider；当前 Project 共享锁、initialized/active 门和私有同 Store/Tx resolution/Usage/lease/AEAD 事实仍必须通过，公开 tuple 不授读取权。纯测试保留裸 tuple、闭 Project、缺锁及错 consumer/缺 association 的拒绝。`82d30a98` 按真实 backend registration 的 Ordinary/Core 类别选择既有 Registry 当前门；Ordinary 仍核 canonical definition、当前 Source 与绑定，Core 仍核原 class，之后仍走 schema 与原 Execution 权限，不形成通用 bypass。
- `f6dfb2e6` 仅将 Human passive lookup 扩至 ProjectScope/PurposeMCP，仍按原 command/user/project 锁重验当前 Session 与 Project Read，查询历史安全 receipt 列，并检查 kind/ref/purpose/版本；无 Secret 材料准备或新写入。HTTP 固定 Project、Human、MCP 与严格有界 DTO；update/delete 的 metadata/late-receipt 检查不能代替最终 ExecuteWrite 的同事务 Mutate、CAS 和完整材料重放比较。原 Unknown 不由 handler 自动重发，返回投影检查固定用途和版本，输入材料在原调用退出时销毁。

已直接核原日志：`/tmp/agenteam-m3-integration-sql-check/native.log` 的 7340，MCP envelope top 5.08 秒、Skill pair top 4.86 秒，actual_wait=0；`/tmp/agenteam-m3-skill-routing.4ajqY5/native.log` / `exit-status` 的 77655，routing top 4.31 秒、actual_wait=0。两轮均三 fixture exact cleanup，复用通过输入而未重跑。79131 仅复用作者的定向 race 结论；已读新增测试，其 HTTP 部分调用内部 execute，未覆盖 ServeHTTP 或真实 PG。

另核 `/workspace/agenteam-m3-mcp/output/ai/mcp/agent-audit-062500fc.log` 与其固定 harness：24826 整 top **FAIL、actual_wait=1**，停在 `mcp-successor-canonical-resource`。此前已断言实际 SDK echo 恰 1 次、Model 请求 4 次且 handler/socket 退出、原 Runtime/App owners Joined，以及后继输入保留 echo/catalog 原字节；仅将这些前置事实用于 audit 窄路由证据，不把 Resource 正文、后续持久退休断言或整链改写成 PASS。三类 fixture 已清理。

适用后继门已交 MCP/integration，待 root 冻结实际 App 装配后安排资源：复用已有 harness 完成真实 HTTP/Account/Project/Secret/Audit/PG 的 create→GET/HEAD→update→delete 与原 passive lookup；定向核 wrong-purpose、撤权后 mutation/lookup 拒绝、原 Unknown 的只读确认且无再写；用既有 Model wire harness 补新 ServeHTTP 的取消/deadline/body close/flush 与实际退出。现 inner/pure 证据不能替代这些门；不扩 OAuth、冷恢复、Runner 或重跑已过 SQL 矩阵。Skills 剩余正式链沿原 owner 计划验证。

本轮结束前追加固定 Object `84627e76` 四源及 Knowledge `44089ad7` 两源的最窄静态审查，亦无已证 must-fix。Object contract 仅增加 KnowledgeRuntime、ExistingOwner/Knowledge、Read、精确 ReadObjectID、无 CreationCause/ProtectedLease 的授权形状；原 ProjectInitialization 条件保持。`object/read.go` 在 canonical reference 捷径之前匹配指定 ObjectID，不能借同文档另一个 canonical ref 放大授权。Knowledge 两个授权入口先走原 ownerScope 的同 Store/Tx、完整锁、当前 Process/Project 和持久 job/profile/document 资格，再由私有 live admission 精确核 document/version/object；公开 Service actor 及已退出 admission 均不足。新增测试能区分错 target、错 project/document/version 和返回后 witness；Object exact target 仍过当前 gate。复用作者 19636/89573 纯检查结果而未重跑，首次 helper 错误仍保留；未将其外推真实 PG 对象读取或自动索引首链，后者交 Knowledge 下一轮固定验证。另读 Registry `cf800224` 定向测试，确认其覆盖 Ordinary skill.list/read 的实际 Source 检查、Core 门拒绝及 class 漂移拒绝；不把该测试源当作 Skills 全链证据。

本次只读 Git，无 Git 写操作；未启动 Go、PG/native、API 或后台资源，只追加本文。原实现与测试均未修改，原失败保留；报告完成格式和链接检查后停写交 root 保存。

## Builtin cold、Memory 用户边界与维护授权追加审查

2026-10-14；固定 Memory `/workspace/agenteam-m3-memory` 的 `ef17def7`（工作卡事实保存于 `bceddd4b`），只审新增 Builtin cold 六源、Memory `8f9680a6` 恢复接线及对应实际 App harness，并补查下述 Memory 用户边界。另只读 Skills `ae4778ab` 两源、MCP `c6d9c0e1` content 三源与 integration `3420b341` App 两源；`b595d6bf` 仅复核 content fixture 的 Row 类型纠正。**上述固定源码有界无已证 must-fix；本轮未新增 Go/PG/native 实测，不把作者报告写成亲核原日志，也不接受未 retired Model、MCP/Install 冷恢复或整个 M3。**前文的 Model/Secret 受控审查不能代替真实 Memory 权限；本节补充这一范围。

- `tool/runtime/builtin_cold.go:53` 仅接受 `memory.retain` revision 1 的原 Lookup；私有 proof 绑定所属 executor、当前登记 lookup、原 call/binding/record 和旧 process，拒绝本地仍存活调用、当前 process、其他 backend 或已停止 executor。第一次同完整锁事务读取原 committed input 和 attempt；Tx 外调用原 `ConfirmStopped`，随后同锁重核完整 binding、输入原字节、digest/fingerprint、attempt ordinal/fence/request/process。活进程拒绝依赖原已接受 ProcessGuard 的真实 flock 机制，没有替换该机制。后端只调用 Lookup，私有 Memory witness 仅允许 `lookup-retain`；没有 Execute 或新模型调用入口。终态发布保留原 operation/attempt/request，`builtin_repository.go:132` 不改 attempt 的 process 归属。结果提交 Unknown 后，下一次仍观察原持久 record；未提交时也只重新读取原 receipt，不重发 retain。
- `memory/generation_recovery.go:24` / `:79` 只选择同 Project/Agent、旧 process、源 Model call 已 retired、非 serving、building/version 1/retained 的 generation。Tx 外确认旧进程停止后，在包含 namespace 与原 retention plan 的完整锁内再次核当前 Process/Tool 权限、旧停止事实和持久 tuple，才同事务退到 retired/version 2/released 并释放原 snapshot reference；不接管旧 Model call，不发布旧 vectors。`runtime.go:370` 在空 namespace 返回前也清理上述旧 build。release Unknown 保留原私有 generation/plan；cleanup 先用 Applied 只读观察，false 时才重试原目标 release，true 时只完成 owner 退场；不能把此流程描述为所有恢复事务均只读。
- Memory 用户路径仍通过原 private Tool 与当前 Project/Agent 授权；Owner list/history/delete/lookup 每次在同事务验证当前 Owner 和 Agent。查询、dense/lexical 候选与最终 canonical 回投都绑定 Project/Agent、当前 revision/status 和 serving tuple；跨 Agent 或已删除正文不能因旧索引命中返回。Reflect 在模型等待后再验引用 current/revision，只返回回答；其 readonly 指 canonical Memory，不是没有 Model/Usage 或索引维护。Delete 将版本检查、软删除、namespace version、Audit 和 receipt 同事务提交，LookupDelete 按原 user/input/key 只读原 receipt；LookupRetain 亦核原 operation/input/Execution，未调用 ensureIndex 或 Model。
- schema 2 profile 包含 selector version、Model/Provider/adapter，校验保存摘要，并与配置维数一同比较；新 profile 先完整构建，再原子切换 serving 与原 reference 释放。实际 harness 只覆盖同维数 `[1,0]` → `[0,1]` 的 selector/model 替换，不能外推维数变化、带存量迁移或超首批上限。旧 reference、query snapshot、canonical 不变的断言与该有限结论相符。

证据分层：已读保存的工作卡/current、恢复与 runtime/web harness、相应私有 proof/拒绝测试源码；未取得 10221、85537、7713、38189 的完整原 native 输出，也未独立重跑。以下仅复用作者已报告的实际结果：

| 作者轮次 | 固定 harness 能支持的范围 | 本轮限制 |
| --- | --- | --- |
| 10221；race 95190 / Config 85489 | reported PASS 48.84 秒、Wait0。原 child 在 canonical retain 已提交、源 Model 已 retired、building retained 且 publish 事务尚未开始时 SIGKILL/Wait9；新 child 恢复同原 E 的 receipt，第二 Task 退旧 build/ref v2、建新 ready/pin；5 次 Model 已发送/成功/退休、1 retain receipt/Audit、正常 Join 与三 fixture 清理均有实际 harness 断言 | 这是精确 kill 点的作者实际证据，不是任意崩溃点；30073 原失败保留。原输出/资源消失仅为作者报告，本轮没有冒称亲核 |
| 85537 / 7713 | reported PASS 52.46 / 60.46 秒、Wait0。跨 E retain→recall/reflect、Agent B 空结果、Owner 删除后空结果；7713 另验证 schema 2 profile 切换及旧引用释放 | 读到真实 App、Tool response、持久事实、peer/Join 断言；不外推外部模型或完整 D14 |
| 38189 | reported PASS 27.80 秒、Wait0。实际 Owner 页面 list/history、取消/确认删除、已提交响应丢失后只 lookup；harness 要求 delete 1 / lookup 1 / Audit 1、原 App/peer Join | 是 HTTP 响应投递丢失，不是数据库物理 COMMIT Unknown；不声称全键盘可用性 |

未覆盖的 cold 行为门保持明确：物理 COMMIT 未转发 / 已提交 ACK 丢失的两分支、另一个真实活进程持锁时经新 cold 入口拒绝、取消/撤权竞争及未 retired Model 启动恢复。本次没有发现需要重放已过正常链或整个 ProcessGuard 矩阵的具体缺陷；上述分支若要纳入接受范围，应只补原目标账本/引用零重复、零业务 redispatch、原 owner Join 的定向门。现有旧 Model 未 retired 时 fail closed 的缺口仍在，不用本次成功掩盖。

维护授权的窄结论：`skill/object_authority.go` 仅让 MaintenanceAccess 跳过 ambient runtime-read context 分支，后续原 mapping、current gate、同 Store/Tx 与锁要求保留；active/expired runtime context 均不应截获原 reader Close，新 read 仍走原 runtime 权限。复用作者 7545 pure/race 通过报告。MCP `content_maintenance.go` 只接受原私有 active contentUse 的 FinishWriter/ReleaseReader，重核完整 materialization/project/E/operation/attempt/request/connection/runtime 事实；reader 还核 object membership。App resolver 仅从同 Store、原 Tx 映射 Object upload owner，不自行授权；原 Object 私有维护证明和完整锁仍必需。reader Close 在 contentUse 失活前完成；新读和 publish 仍执行当前 invocation 权限，cleanup 不获得新业务权。

本轮结束前取得并直接核对 Skills `ae4778ab` / App06 的原 `/workspace/agenteam-m3-skills/output/ai/m3-dynamic-skills/first07/native.log`：77051 的 `TestSkillsM3AppDynamicInstallRead` PASS 39.04 秒、actual_wait=0，PG/peer/Object 三项 exact cleanup。原 log 与 `/tmp/agenteam-m3-skills-app06.test` 的 SHA256 均与作者固定 manifest 一致。对照固定 `skills_m3_integration_test.go`：真实 install 得随机 Skill ID，显式 Owner HTTP assign 在已捕获 round 后发生，旧 list 为空、后继 list 含新 Skill，实际 Object 正文进入第 5 次 Model 请求；4 个 immutable successor inputs、1 个 skills_added control、所有 reader leases released、Skill work joined、原 App/Runtime/producers/ProcessGuard 退出均有断言。本次接受该正常首链及维护 read/Close 的原证据，不重跑；仍不外推 Agent assign、Runner prepare、Install Unknown 或撤权竞争。

MCP 原 4233 content test 因 fixture 返回 `pgx.Row` 而接口要求 `postgres.Row` 编译失败，`b595d6bf` 已仅纠正该类型；作者后报 66834 定向 race PASS 1.031 秒 / Wait0，本轮未亲核其日志。4233 HTTP 子包 race PASS 1.423 秒不算 content 或实际 Resource 通过。结束前又直接核原 `/workspace/agenteam-m3-mcp/output/ai/mcp/agent-maintenance-b595d6bf.log`：79351 的 `TestMCPM3AgentToolsAndResources` PASS 25.44 秒、actual_wait=0，三 fixture exact cleanup。作者标明使用 16340 编译的 `/tmp/agenteam-m3-mcp-maintenance.uDo43Q/app.test`；`b595d6bf` 中 App 两路径与 `3420b341`、content 产品路径与 `c6d9c0e1` 精确无差异。固定 harness 验证真实 Put/Consume/Read/Close、canonical 正文/ref/hash 进入后继第 4 个 Model 请求、SDK echo 1 次、3 个 operation 成功、runtime/Secret lease 退休和原 App/Runtime owners Joined；本次接受该正常 Resource 维护链的原证据。没有将此轮描述为实际取消 tail 注入或逐条 Object lease 查询已测。已交 MCP/integration 的后继门仍限撤权/取消只能完成原 cleanup、新 read/publish 必须拒绝；OAuth WIP、媒体和冷恢复均不外推。

本轮仅追加本文；未改源码、测试、其他文档或执行 Git 写操作。所有只读命令已退出，无 Go/native/fixture/API 或后台资源；完成格式检查后冻结本文，交 root 保存。

## MCP Owner 凭据与 Model stopped 恢复短轮审查

2026-10-14；固定 MCP `6e59c76f`、Model `bd9f076f`、Usage 主体 `5da01c71` / 尾巴 `0a92da04`、合同 `dfe979f4` 和 `00127` 组 `b9816154`，只读限定提交，不采用后继工作区漂移；结束前另核修复 `e847e7eb` 的三个固定文件。**MCP Owner HTTP 有界无 must-fix；Model `bd9f076f` 的已确认 Drain 回退在 `e847e7eb` 源码中已修。本次后继固定源码无剩余已证 must-fix，但尚未完成真实 PG 恢复验收。**

MCP 已直接核 `/workspace/agenteam-m3-mcp/output/ai/mcp/owner-credentials-6e59c76f.log`：76616 的 `TestMCPM3OwnerCredentials` PASS 7.37 秒、actual_wait=0、PG/peer/Object 三 fixture exact cleanup。对照固定 harness 与既有 HTTP/Secret 窄口：真实 App/Account 下 create→GET/HEAD→update→delete，原值重放成功、同 key 改材料及 stale CAS 冲突，原三个 receipt/Audit 和历史 lookup 版本固定；metadata 不含材料、HEAD 无正文。wrong-purpose 实际负例为 Model credential 的 GET 返回 404；logout 后实际验证原 create replay 与 lookup 均 401、原目标 receipt/Audit 计数不变，不能扩称每个 verb 或 Project 降权竞争已测。原响应丢失 fixture 在真实成功 HTTP body 读尽关闭后返回错误，随后原 key 只 lookup，未注入数据库 COMMIT Unknown。原 App/Runtime/producers Joined 断言成立。`60234` OwnerWeb 失败仍按作者报告保留；`d91b1879` / `fedde51f` mode 控制面仅构造检查、无 PG，不在此正常 HTTP 接受范围。

**原 `bd9f076f` 的 must-fix：** `runtime_stopped_lookup.go:108` 调用 `prepareStoppedCall` 后，活跃 Lookup 的私有 witness 登记在 `s.recoveries`，但 `w.original` 为 nil。`runtime.go:194` 的 Drain 先调用 `confirmStoppedRecoveries`，后者 `runtime_stopped.go:371` 因该 nil 直接返回 ResourceBusy，尚未进入原 admission.cancel / done 等待。最小交错：ready Runtime 的合法 Lookup 已登记 witness，第二只读事务阻塞；StopAdmission→Drain 提前失败，而原查询尚未退出。这是固定源码的明确路径，发现后 Model owner 独立确认并返修；本审查未改实现，原缺陷与下述后继修复分开记录。

**后继修复复核：** `e847e7eb` 的 Drain 先取消并等待已登记 admission.done，再确认 retained Unknown；Lookup 原 defer 顺序为先释放 witness、后关闭 admission.done。因此上述活观察不会再进入 Unknown 扫描。新 barrier 测试登记 original=nil 的原 witness，以第二次 cancel 明确观察 Drain 已进入 admission 路径，原 callback release/done 后检查成功及 Joined；旧实现会在进入该路径前返回，能被测试拒绝。复用作者 81756 五门 race PASS 1.032 秒 / Wait0 报告，本轮未亲核原日志或重跑。该提交同时补 Model owner-tail 消费：私有 startup 仅 initializing、未 ready、未 stopped 且当前 guard 有效；原 call 锁核无 admission 或精确 retired record，Model 与 Usage 各自在 Tx 外确认旧 process 停止，活 owner 注册与完整 before/rowset/锁在写入时再验；Unknown 仅拿原 plan 观察，不从 confirm 调用 close 重放。新增三源静态无另一已证缺陷，实际两种 tail 窗口与物理 Unknown 仍待真实 PG。

其余静态核对结果：

- Model 在 ready 前读取原 Call/Attempt；原 ProcessGuard 的 ConfirmStopped 位于 SQL 外，private runtime proof 登记后，同完整 call/invocation/resolution/snapshot/Usage/Secret 锁重验当前 guard 和原 before/after record。binding 摘要、call/attempt/process/fence/sequence/version、Input/Snapshot 均不改绑；known Final 与已有 Usage 原文保持。无 Final 时仅把 reserved→not_sent/cancelled、authorized→dispatch_unknown/unknown，已 sent 保持 sent 并记 unknown；没有重发或伪造 Provider 输出。
- Secret 仅对 ModelCallOwner 的原 committed preparation/snapshot/ref/lease 发精确 Release；写 grant 还要求同 Tx 的 staged 与 validated 私有标记。ExecutionOwner 不走该释放分支，不能借单个 Agent call 退休释放整 Execution lease。Usage 复用原持久 header/Initiator，plan 绑定私有 issuer、完整 request、原 invocation-lane borrower rowset 的 ID/project/process/instance/call/registered_at；闭合只允许 phase 单调变化，新增/移除/替换原行使原 plan 失效。最多 64 个 owner 的有界限制保持，summary lane 不进入此闭合。
- Usage Applied、OwnerClosed 与 Model confirm/Lookup 本体只读原目标；confirm 不调用 Apply/dispatch，已证未提交后只退出本次 mutation，后续显式 Initialize 才能发起新清理。Lookup 先后重验领域独立 StoppedCallAuthority 和完整输入，只在原进程 stopped、Model retired、Usage closed、适用 Secret lease released 后返回原 attempt/status；Found=false 仅表示原锁下没有 Call，不是消费者或 pre-admission borrower 已停止。`0a92da04` 的 owner-tail 端口还要求 Model 私有 startup/原 owner facts；`bd9f076f` 明确尚无该完整接线，不把接口存在当作 pre-admission/已退休尾巴已恢复。
- `00127` 仅把原 retention consumer 闭集加入无 Agent 的 Meeting，旧 Knowledge/Memory 形状、版本、credential 和 snapshot FK 保留；serving source purpose 限两个 embedding 与 Meeting summary initial/update，Summary 要求 chat adapter 和原 SelectionVersion。领域 generation 私有权限仍必需；未因此接受 Meeting 实际 snapshot 重试或带存量升级。

复用作者 47330、89302、17258 的纯检查报告并读对应测试源码：规范化、known Final、裸 proof/当前 process 拒绝、完整 rowset/mapping 及私有 startup 要求有覆盖；本轮未亲核这些原日志，纯检查不证明真实 stopped flock、SQL/事务或 COMMIT Unknown。已向 Model/integration 交付最小后继门：真实旧进程退出后精确原 attempt/known Final 与 lease/borrower 退休、Execution 整 lease 不动；rollback 与物理 Unknown 两分支的确认零写/零 dispatch；tail 验证无 admission/已退休两种窗口。`47472d54` 新 App harness 尚未编译运行，不计作恢复行为证据。无需重放已过 MCP 或旧 Memory 矩阵，Memory 原日志缺失限制保持。

本轮未启动 Go、PG/native、浏览器或 API；只写本报告，无 Git 写操作和后台资源。固定缺陷及未验证范围交原 owner 继续，报告停写后由 root 保存。

## Model 停止恢复、Scheduler 返回尾巴与 Agent Skill assignment 增量审查

2026-10-14；仅审固定 Model `3a063e44`（产品修订至 `63a6710a` / `e847e7eb`）、absence 两源 `b1ea8be4`，复用前节 Usage `5da01c71` / `0a92da04` 与共同输入 `c117b512`；Scheduler `9c51fd82` 三源及共同输入 `25df1419`；Skills `41a0e3c3`、`00128` 的 `9d27f00d` 和 Project Event 窄路由 `75357539`。源码使用限定 `git show`，未采用活跃工作区后继改动。**本次固定增量有界无新的已证 must-fix；以下实际证据只接受注明的子门，不是整组恢复、Agent assignment 或 M3 验收通过。**

Model 复核了原停止证明、完整锁及 Usage rowset 的既有结论。`runtime_stopped_owner.go` 的 `63a6710a` 在确认事务未成功或 ctx 已取消时返回原 Unknown，保留原私有 owner/plan；只读确认不 close、不 dispatch、不换原目标。`b1ea8be4` 将无 Model admission 的 before mapping 编码为 `Present=false`、Version/Digest 均 nil；存在记录仍编码实际非零值，不借“缺席”放宽存在记录校验。其测试区分稳定 absence、present 及非法零 version；复用作者 65439 race 通过报告，未亲核该原日志。完整锁下仍重读原 call 是否缺席或精确 retired，并核原 Usage owner rowset、当前 process 和 Tx 外 stopped 证明；新修订没有释放整 Execution lease 或覆盖 known Final。

已直接核 Model 树 `output/m3-model-cold/app02/native.log`、`app03/native.log`、`app04/native.log`，并对照 `3a063e44:internal/central/app/model_cold_m3_integration_test.go` 的真实 App/PG harness。wrapper 只在原正式事务物理返回后设屏障，非伪造业务行；恢复前后原 tuple、账本、lease 和请求数的断言按场景不同，不混用：

| 原轮次 / 日志 | 亲核实际结果 | 接受范围及限制 |
| --- | --- | --- |
| 61912 / app02 | authorized 子门 PASS 18.52 秒；sent 子门失败，整 top FAIL、actual_wait=1 | 原 child SIGKILL/Wait9、新 child 正常 Wait0；authorized 的原 Call/Attempt/Input/Snapshot/Execution lease 不改绑，final unknown、Usage/borrower 退休，模型零发送。不能称 61912 整轮通过 |
| 34445 / app03 | sent 定向门 PASS 15.53 秒、actual_wait=0 | 原发送恰一次，停止后保留 sent、同原 attempt 记 unknown；恢复不重发，整 Execution lease 原文不变，原 App/owners 正常退出 |
| 70086 / app04 | retired-before-returned 子门 PASS 16.66 秒，Memory ModelCallOwner 子门 PASS 18.45 秒；borrowed 子门失败，整轮 actual_wait=1 | retired 子门仅闭合原 Usage borrower，Model/Attempt/Usage observations/Secret leases 原文保持，并正常 Join。Memory 子门通过真实 retain 的 extraction ModelCallOwner 精确 release，原 Agent 请求 1、Memory/embedding 请求 0；新 child 在 Model Initialize 屏障仍以 SIGKILL/Wait9 结束，不能据此接受 Memory 业务恢复或完整 App graceful Join |

上述日志均记录三类 fixture exact cleanup。borrowed 原失败是新 child Initialize 的 absence Version/Digest 编码失败；`b1ea8be4` 源码已修，但本节没有后继真实 borrowed PASS。reserved 崩溃点、物理 COMMIT 未转发/ACK 丢失两支及真实 LookupStoppedCall 消费链未由这三份日志覆盖；沿前节的原目标零重发/只读观察门继续，勿重跑已过且未变的正常子门。`66a36728` codec 及后继 harness/WIP 不在本次审查。

Scheduler `launch.go` / `launch_association.go` 仅在同步 Launch 原返回已证明 NotCommitted 后，为原 rejection checkpoint 使用最长 3 秒的 `WithoutCancel` context。原 typed outcome 在错误包装之前分类；原或嵌套 Unknown/Committed 不会被转成 known-not-created。checkpoint 前及同事务内要求原私有 call 指针、登记 owner、running、send 已退休和原 record；同 Store/Tx 下持有 command/dispatch/Project/Schedule 与原 intent 完整锁，校验原 ProjectCall 的 running tuple/current process，再对原 attempt/version 的 Pending/Unknown 作 CAS。checkpoint 自己 Unknown 或失败仍保留原 cause 与 owner，观察不重 Launch。`launch_rejection_tail_test.go` 的受控返回屏障能区分 callback 已返与事务实际尚未返，并覆盖取消、停止、嵌套 Unknown 和错 owner/tuple；这些是纯测试，不伪称物理 PG Unknown。

Scheduler 27546 race、31818 `TestKnowledgeM3OwnerFiles` PASS 35.30 秒 / Wait0、原 App/producers/21 owners Joined 及三 fixture 清理，**仅复用作者原会话回执**。作者确认没有另存原 native logfile；本审未将会话报告改称亲核日志。其固定 binary 为 `/tmp/agenteam-knowledge-files-9c51fd82.test`，diagnostic harness 仍为 `6476adbc`，事实记录于 `01891f03`。修前 28417 的 App shutdown failure / Wait255 保留。未发现需要重放该未变完整链的源码问题。

Skills 静态检查限定新 AgentRun 管理入口：

- `tool/runtime/skill_assignment.go` 与 `skill/runtime_assignment.go` 的公开 Run DTO 不授予写权。私有 active witness/issuer/原 operation、attempt、request、ToolCall、spec 和完整原始参数摘要均需匹配；Execute 经原 Execution current 检查，Ordinary `assign-skill` 必须仍在当前允许列表且不被原 policy 拒绝。Lookup 只贷原 committed input，不能取得 Mutate 权限或重发。目标可以是同 Project 的另一 Agent；目标 ID、动作与 expected version 必须来自该次原输入，不能从私有 context 换目标。
- `agent/skill_assignment.go` 在同完整锁下再次核当前 Run/Project/Agent 与 canonical preimage，才 CAS 推进目标 Agent 和四类 reference version。过渡中的同 Tx 私有 witness 仅供这四个原 reference owner 读取精确 postimage，回调结束即失效，未贷给 Skill finalize。canonical assignment、runtime ingress、Audit、Outbox 和 completed receipt 仍在原事务提交；同 key 的 input/run/candidate 不可替换。它只授予分配管理，未因此授予调用者读取目标 Skill 正文。
- `75357539` 的 Project Event AgentRun 分支限定 Skill assignment producer/闭合 header/同 Project/原 target，要求当前 Project gate、同 Store/Tx、完整锁与原 RuntimeAssignment 私有 source；NewFact 还需原 Applied witness，不能仅凭合法 summary、当前 AgentRun 或另一次相同 Agent/Execution 的调用发事件。Audit 同样绑定原 canonical command 和 caller/target 各自身份。`00128` 扩展 actor/run 形状并保持原 Human 无 run 的输入表示；原 command/run/input/candidate 仍不可变，裸 Agent actor 或 JSON 不能绕过应用私有 authority。

复用作者 18030 权限负例 race 报告并读相应测试源码；本轮未亲核其原输出、未启动 PG。Agent self-assignment 真实 successor/消费链尚待正在进行的固定联调，不由旧 Owner assignment 首链替代；跨目标实际 PG、带既有 Human 行的 128 升级和 assignment 的物理 Unknown 未在本节接受。没有要求重复无变化的旧 Owner、MCP、Memory 或 ProcessGuard 矩阵，旧 Memory raw log 缺失限制保持。

本次仅追加本文，未修改产品、测试或其他文档，未执行 Git 写操作。无 Go/native/API/fixture 或后台命令；格式检查后停写交 root 保存。本节不覆盖后继 codec、OAuth、MCP 媒体、Meeting 或 Runner。

## StoppedResolution 首次独立 PG 门：前置 fixture 失败

2026-10-14；本轮复用 Model 固定 `13c92736`（`0249a738` 仅交接文档）的 integration/race binary `/tmp/agenteam-m3-model-stopped-resolution01.test`，直接核 SHA256 为 `823cbc7a8f9738689ba251f4b4fc6cc2f449282f3bd65a72e1f60bb4287c1f4f`，未重编该 binary。integration 授唯一 native 窗口；起前 overlay 可用约 2.6 GiB、tmp 约 4.3 GiB，无活动 Go/native，显式 current-only，未访问现有 dev PG/MinIO。只运行 `^TestModelM3StoppedResolution$`，未重复原 81711/96718、迁移或其他通过矩阵。

命令在 `/workspace/agenteam-m3-model` 执行 `/tmp/m3web.ZVAtEu/object-fixture -run '^TestModelM3StoppedResolution$'`，保默认代理/TLS；环境为 `AGENTEAM_GO=/workspace/toolchains/go1.27.1/bin/go`、`GOTOOLCHAIN=local`、`GOMAXPROCS=2`、`GOFLAGS=-p=1`、原共享 GOCACHE/GOMODCACHE、原 source-built MinIO；`AGENTEAM_FIXTURE_TEST_BINARY` 为上述 binary，`AGENTEAM_FIXTURE_TEST_CWD=/workspace/agenteam-m3-model/tests/model`、`AGENTEAM_FIXTURE_PG_CURRENT_ONLY=1`，owned record 指下述本轮独占目录。helper 原有小型 Go 构建保留。

**独立 session 91350 实际 FAIL 5.68 秒 / Wait1，尚未进入 StoppedResolution 行为断言，不能接受此门。**原日志位于 `/workspace/agenteam-m3-integration/output/ai/m3-core-review/stopped-resolution01/native.log`，同目录保留 `owned.json`、`exit-status` 和 `wrapper-pid`。测试 PID 1336775 原 `actual_wait code=1`，wrapper PID 1335699 原 Wait1；错误定位显示 `stopped_resolution_test.go:223` 的 `RESOURCE_BUSY`。D03/D04/D05 原日志分别记录 exact cleanup；结束后按 owned record 的六个精确 container/network ID 复查均不存在，三个目录与上述两个 PID 亦均不存在。native 窗口已归还。

**已证 harness 必修，非已证产品缺陷：** `13c92736:tests/model/runtime_native_test.go:137` 用 `objectfixture.Environment(db.Name)` 获得固定路径，`:153` 的第一个 `OpenSpool` 持锁至 fixture 收尾；`tests/model/stopped_resolution_test.go:134` 再次用同一 `db.Name`，`:147` 再开同一 spool。`tests/testsupport/objectstore/environment.go:37` 按 identity 固定 suffix，`internal/central/object/spool.go:102` 对原 `.object.lock` 使用 `LOCK_EX|LOCK_NB`，第二次打开必然返回 ResourceBusy。helper 标有 `t.Helper`，故原输出归到调用行 223；此处尚未创建第二个 ProcessGuard、调用其 Runtime.Initialize、seed 三种原 resolution 或执行 LookupStoppedResolution。已将决定性位置交 root 安排 Model 作者修 fixture；本审查未改产品或测试，不放宽生产 flock，不对相同输入盲重试。

本轮已读 `9868ee1c` contract、`9bcfe7ae` provider 的固定代码及实际 harness：观察保持完整原 ResolveRequest（包括 nil Selection.Version）、私有域 plan、原 call/input/prepared process；在两次完整锁读取之间调用真实 ConfirmStopped，不准备解析、不 acquire/release lease、不 dispatch；CallFound 仍需另走 StoppedCall 观察，返回原 Model 仍需正式 retirement。此有限源码阅读未另发现已证产品问题，但实际前置失败使旧 guard 拒绝、停止后三状态、原 lease 退休及零 dispatch 均仍未验证。计划中的同 OS 双 guard 也不能称新增 OS kill 门。

133 及完整 1–133 的作者已过结果本轮未重跑或独立静态复核；Memory 132/后继恢复、MCP 131/startup/Object cleanup、Skills InstallUnknown/finalRaw 与 App router 后继完整审查均留待续派，不以读取 commit 摘要或旧报告代替已审。按 root 指示在已确定前置失败处收敛，让原作者返修；仅本报告新增正文，全部命令已退出，无在用资源或 Git 写操作。

## StoppedResolution 复验、132/133 与 Unknown 身份修复

2026-10-14；本次独立实测固定 Model `7188f34d`（`f321c822` 修复顺序 guard fixture，后者修正 opaque identity 比较；产品 provider `9bcfe7ae` 未改），静态复核 Memory 组合 `7786610b` / App `985e0198`、132 原迁移与来源，以及 133 `4993b368`。发现的 Memory 三处 Unknown 身份丢失已由原作者在 `93b1c534` 修复并经下述限定复核关闭。**本节已审范围无剩余已证 must-fix；真实通过仅覆盖注明的门，不接受整个 M3 或未运行的后继链。**

**Model 独立 session 96148：PASS 6.55 秒、原 Wait0。**复用 `/tmp/agenteam-m3-model-stopped-resolution03.test`，亲核 SHA256 `19d205ad7e2a1a2fc4f93128d6b0fb808fe29755eac6aa452805cbc5be855460`；未重编 binary。沿前节 cwd/helper/精确 selector/Go1.27.1/默认代理 TLS，`GOFLAGS='-mod=readonly -p=1'`，显式 `AGENTEAM_FIXTURE_PG_CURRENT_ONLY=1`，独占 native 窗口起前 overlay/tmp 均约 4.0 GiB、无活动 Go/native。原输出和 `owned.json` 在 `/workspace/agenteam-m3-integration/output/ai/m3-core-review/stopped-resolution03/`；test PID 1366371 `actual_wait code=0`、wrapper PID 1365191 Wait0。原 D03/D04/D05 exact cleanup 后按六个精确资源 ID、三个目录和两个 PID 复查，全部无残留，窗口已归还。

固定 harness 现在先完整 Join 原 Model/Usage/Object owners 并关闭 spool，才在同数据库/同 spool 启动新 ProcessGuard。三个原请求分别未准备、仅 prepared、已 committed resolution：新 observer 的 Settled/Model/CallFound 形状正确，返回 Model 的 Consumer/LeaseOwner/CredentialRef/LeaseID 使用正式 Equal，其余完整 snapshot 严格比较；原 preparation/call/invocation/Secret lease 的行与 xmin 不变，受控模型请求为零，随后正式 retirement 释放并确认原 lease。裸 DTO 和把原 nil Selection.Version 改成具体版本被拒绝。live 拒绝使用的是当时当前 guard 的 ProcessID，命中 `ConfirmStopped` 的 current-process 拒绝；新 guard 再真实检查旧 claim/flock。**不把此门外推另一个活跃 OS 进程争锁、新 OS kill、CallFound=true 分支或真实 Memory/Meeting 域权限**；域权属仍为严格私有 fixture。原 91350 失败与作者报告的 42231 opaque DeepEqual 失败保留，不回填为通过。

**133 有界复核通过。**对照原 115/127、实际 CHECK catalog 和 `4993b368`：127 曾误删 `released_version>=owner_version` 的自动命名约束，并留下旧 Knowledge/Memory owner CHECK；133 单个 ALTER 删除旧 `check1`，恢复显式命名 version floor，127 的扩展 owner/kind、原 credential/state CHECK 与 FK 保留，不改写存量行。亲核 Model `output/m3-model-cold/retention-catalog02/native.log`：99703 `TestServingRetentionForwardRepair` PASS 0.11 秒及 10 子例、test PID 1312803 Wait0、D03 cleanup。对照 harness，非法存量释放版本使 ALTER 原子失败且行/xmin/旧约束保持，合法 Knowledge/Memory 存量不变，Meeting 合法形状接受，错误 agent/kind/version/time 拒绝。此门使用最小前置表；完整 1–133 的 62067 fresh/repeat/upgrade/health PASS 仅复用共同作者报告，本轮未亲核其原日志或重跑。已应用的 115/127/131–133 均未改写。

Memory 132 与恢复静态核对：新 operation 保存原 prepared process；model_calls 在原 admission 事务保存完整 selectable source 与 ResolveBinding，包括原 nil/具体 Selection.Version 或原 serving snapshot/generation。immutable trigger 保护原 Project/Agent/Execution/operation/input/process/source；legacy NULL 不猜测回填，缺证据保持未决。启动按 Model→Memory 顺序，先确认旧 process 停止，私有 active witness/issuer 绑定原 row mapping；同 Store/Tx、完整 namespace/command/Project 锁与当前 guard 下再次核原 tuple。无 snapshot 或 StoppedCall 未找到时，走正式 StoppedResolution；已解析 lease 使用原 ModelCallRetirement，CallFound=true 仍须另验 StoppedCall，未把 row 缺席当作 lease 已空或允许重发。

原 canonical retain failure 仅由原 cold `memory.retain` Lookup 的私有 proof 触发，检查原 operation/input/Execution/prepared process、所有相关 Model owner 均 retired 且归属一致，才在同事务将 prepared 改为持久 `DEPENDENCY_UNAVAILABLE`；已 committed receipt 不覆盖。公开 DTO、任意错误、缺行或只读观察 false 均不能制造失败。Builtin typed outcome 闭集只接受该代码，保留原 operation/attempt/request/process/ordinal/fence，把持久失败作为同一 Execution 的后继 Tool response，不重 Execute/Model，也不写 canonical Memory。

已亲核 Memory 原两份日志并对照 `memory_m3_stopped_integration_test.go` 与实际子进程 harness，复用固定 `66603dc3` / 普通 App02 的结果，不重跑：

| 原轮次 / Model 窗口 | 亲核结果与范围 |
| --- | --- |
| 93314 / 已 authorized、未发送 | `output/ai/m3-agent-memory/stopped-authorized-first02/native.log`：PASS 7.21 秒 / Wait0；原 child 1339350 SIGKILL/Wait9、新 child 1339415 正常 Wait0。原 Model/Usage/Secret 与本域 call 退休，原 retain 持久失败，同原 Execution 消费失败 |
| 58830 / Resolve committed、尚未 admit | `output/ai/m3-agent-memory/stopped-resolved-first01/native.log`：PASS 7.29 秒 / Wait0；原 child 1359326 SIGKILL/Wait9、新 child 1359390 正常 Wait0。正式 StoppedResolution 与原 lease retirement 后完成相同失败语义 |

两门均断言完整 source/input/process/Tool tuple 保持，受控 peer Agent 2 次、Memory/embedding 0 次且 Joined，无新 Memory/retain Audit，原 Secret lease released，App/owners/ProcessGuard 正常 Join；日志均有三 fixture exact cleanup。普通 binary 结果不称 race；这些正常窗口不证明 Memory 新增写事务的物理 COMMIT Unknown，旧 Memory 10221 等 raw log 缺失限制不被覆盖。

**本轮原 must-fix 与修复：** `7786610b` 的 `memory/model_stopped_recovery.go:214`、`model_stopped_resolution_recovery.go:192`、`stopped_operation.go:78` 在原 mutation 已 Unknown 后，只读确认若取消、读取失败或自身另一个 Unknown，直接返回 observation error，丢失原 mutation 的 Attempt/Cause。最小交错为原 UPDATE 的 COMMIT 响应丢失，随后观察失败；新已知失败不能证明原写未提交。发现后作者逐处确认；本审未据此宣称已存在权限绕过、模型重发或错误 canonical 终态。

`93b1c534` 只将三处失败返回改为原 mutation error，SQL/权限/dispatch 不变。已读三处固定 diff 和 `stopped_unknown_test.go`：三个真实方法入口各覆盖 read-failed、实际 ctx cancelled、observer-unknown，核原 Attempt、完整 RecoveryCause owner/run/checkpoint、lookup 提示、仅两次事务尝试以及内存 owner/receipt 不推进；原实现会失败。已核 `/tmp/m3-memory-stopped-unknown.log` 的 ordinary 单包 `ok` / 0.010 秒输出；84156 原 Wait0 和精确 selector 复用作者回执，该非 verbose 日志不逐列子例。测试控制 Store 返回边界，不执行 SQL/guard 回调，不冒物理 Unknown。该明确缺陷在固定修订中关闭，不重复两条未变正常 PG 链。

顺带完成的额外**窄源码阅读**：MCP `20ae6fe0` 的 Execution content Source、MCP source 与 Project runtime route，及 `5ccf0c15` 的 content recovery authority/driver、Object upload resolver/join。新 detached read 要求原 running Model input 中的 canonical Tool File/Image、原 transcript/result/digest/round/operation/attempt/request 与当前 Agent；字符串提及或裸 reference 不授予读权。ReleaseReader tail 只贷原 closing owner 与精确 lease，不授予新 read。Project 技术 route 仍需当前 lifecycle/初始化、原 private startup call 和完整 tuple。cleanup 要求旧原 process（及旧 recovery owner）真实 stopped，原私有 active owner、pending materialization、原 claim/upload/object/attempt/current process 与完整锁重验，只能撤销原 reference、join 原 writer 和清理无引用对象；resolver 仅给元数据，不自行授予 cleanup。已读这些路径暂未另证 must-fix，**未完成 131 全 startup/Secret/OAuth 状态机审查，也未接受该新 Object/媒体 actual 链**；原文本 Resource 正常通过不替代它们。

Skills `791c3d70` + `7769fec8` + `653adcc5` 仅复核 install finalization 核心：原 handoff 必须仍由同 dispatcher/issuer 保留、原同步 executor 已实际返回且 backend 结果已知；同完整锁下，只接受精确 v2 running tuple 或一致 v3 终态。已提交只观察，证实原 finalization 回滚才重写原已验证 finalRaw；不重 backend、不重新解释 schema。缺失/不一致 finalRaw 被拒且 retained owner 不清除。此为有限静态结论，76944 ordinary 仅作者报告，4206 actual harness 未由本轮执行；不接受 Install Unknown 真实链或冷恢复。

本次仅运行上述一个独立 native 门并追加本文，其他通过结果复用；未改产品、测试、共享 current 或执行 Git 写操作。全部命令已退出，窗口和资源已释放；完成格式检查后冻结报告，交 root 保存并释放席位。

## Execution 当前 policy 收尾与同进程 Install Unknown

2026-10-15；只读固定 common `c4ef74cb` 的 Execution 三文件（产品改动 `eb151bce`），以及 Skills `41c074a3` 的 Install FinalTx / publication 恢复（`791c3d70`、`7769fec8`、`effd58d9`、`76018ac8`、`1a83cf8e`、`4b8f2e59`）。Skills 固定树中的 Execution 三文件与 `c4ef74cb` 一致。本轮未运行 Go/native，按实际源码和原日志复用未变通过门。**Execution 此窄组无新已证 must-fix；Install 发现一处新的 Unknown 身份交接 must-fix，以下注明，尚未关闭。**既有 Memory/Model/133 结论不重开，不扩 MCP135/Runner Observer。

Execution 的私有 `capturedToolPolicyRejection` 只由本地 captured-tool policy 检查产生。原 Model session Joined、完整锁、current process、原 running Execution/round/snapshot 且无 pending checkpoint 后，current Agent/Project 检查先行；该 marker 在 `insertToolCallFacts` 前返回，且只有 `NotCommitted` 才交回原 `finish` terminal owner。没有调用 Tool、创建 Tool operation 或放宽当前权限；`Unknown`、外域 Forbidden、DB 故障仍保留原 checkpoint 观察。原 terminal 事务若失败仍留在 closing/terminal 恢复路径，不伪造 durable completion。已读四模式 controlled 测试，终态 Store 故意拒绝写以证明只交接原 owner；68687 ordinary 通过复用作者回执，未亲核其原输出或另跑。

亲核 Skills `output/ai/m3-dynamic-skills/catalog-revoked-first02/native.log`：76050 PASS 5.96 秒、test PID1448985 actual Wait0，只有一次原 checkpoint `FORBIDDEN/not_committed`。对照固定 harness，原模型确已捕获工具，Owner 正式撤销 allowlist 后原 Execution 到达终态，零成功 catalog operation、零 AgentRun assignment command，原 App/peer/guard 退场。该实际 SQL 并非全部 Tool 表零行计数；“checkpoint 前零 Tool 写/调用”另由本次源码顺序与 controlled 门支持。旧 11828 重试至超时仍保留。

Install 的两条恢复路径已分别核对。原 dispatcher/issuer 的 active handoff、同 binding/operation/attempt/request、原 backend `h.done` 实际关闭及 `live=false` 才可恢复；`recoveryMu` 串行同一 owner。FinalTx 观察在同 Store/Tx、完整原锁和当前 ProcessGuard 下，仅精确 v2 running tuple 可证明原最终写已回滚；一致 v3 只读。复写只用原 callback 已校验的 `finalRaw`，不再解释 schema，不重 Install/Upload。观察前置失败、取消、只读 COMMIT Unknown 保留原写错误；新实际 final 写 Unknown 在内部改存自身 Attempt/Cause。

publication Lookup 是独立私有 Read witness：原同进程 owner、原 command 和 Unknown 身份、已返回 backend、完整调用参数及原 physical request 均绑定；无 active handoff、裸 DTO、旧 cancelled dispatch context 或 expired witness 不能授予新权。发现计划与同事务 Require 都核 issuer/原 witness/tuple，检查完整锁、当前 guard、原 Execution/Registry permission 与原 running Tool；Skill 层仅绑定 read plan，Mutation 拒绝。canonical lookup 核原 actor/semantic/key 和 installed receipt，不执行 Object I/O 或安装写。只有 exact published receipt 才首次写 Tool final；missing/reserved/读取失败均保原 publication Unknown，不能把缺席当回滚或制造完成。

已亲核以下原日志，并对照固定 App harness 的 complete-frame proxy、原 writer settle、严格 tuple/row/xmin、同 E 消费及 Join 断言；均为复用作者执行结果，非本轮独立重新运行。路径均相对 `/workspace/agenteam-m3-skills/output/ai/m3-dynamic-skills/`：

| 原门 / 输入 | 亲核结果与准确边界 |
| --- | --- |
| 67124 / `1a83cf8e` App02；`install-final-unknown-first01/native.log` | 整 top PASS 17.16 秒、PID1426381 Wait0；Tool FinalTx commit 8.75 秒 / rollback 8.42 秒。原完整 COMMIT frame 响应断开；commit 原帧提交后终态行/xmin 不再改，rollback 确切终止原 writer 后复写原 final bytes。同 Attempt、一次安装/上传、零自动分配、同 E 后继模型消费、正常 App Join |
| 94608 / App03；`install-publication-unknown-first01/native.log` | **整 top FAIL 17.58 秒、PID1450999 Wait1**；commit 在旧业务到达 barrier 前失败，没有故障注入结果。rollback 子例 PASS 8.80 秒：发布事务实际回滚后 reserved 行/xmin 与 Tool running tuple 不变、无重发；Drain 仍原 Unknown/CauseID、Joined=false。父进程精确 SIGKILL/实际 Wait，检查持久 pending 与零连接后清理自有 DB；**非正常 Join、非 cold** |
| 64265 / `41c074a3` App04；`install-publication-unknown-commit02/native.log` | 仅 commit 子例 PASS 8.68 秒、PID1465370 Wait0；4556 ms 到原发布 COMMIT frame。真实 Unknown 后只读原 published receipt，publication 行/xmin 不变；首次 Tool final、同 Attempt/E、一次安装/上传、零自动分配及后继模型消费、正常 App Join。App04 只将业务 barrier 绑定原 90 秒总预算，产品与独立 COMMIT/Join 门不变 |

以上四份日志（含 76050）均显示 D03/D04/D05 原 exact cleanup；六资源/目录/PID 无残留复用作者原检查，本轮未再次 inspect。普通 App 结果不称 race；新的复合 Unknown 交错未由这些门注入。

**未关闭 must-fix：** `41c074a3:internal/central/tool/runtime/skill_install_dispatch.go:180` 无条件 `errors.Join(err, observedErr)`。最小可达交错是 Skill 发布 U1 Unknown → 同次 `Lookup` 已只读确认 published → 首次 Tool final 写产生新 U2 Unknown。`install_publish_recovery.go:227–241` 正确将 U2 存入 `h.retirement` 并返回，但外层 `Execute` 又把已解决的 U1 放在 error tree 首位并存为 `call.err`：标准 `errors.As(*foundation.Fault)` 仍首先得到 U1 CauseID，`skill.UnknownAttempt` / `installUnknown` 也能优先找回旧 Skill U1。原 Tool FinalTx U1 确定回滚 → 复写 final U2 Unknown 同样受到该交接影响。

该缺陷限于对外/dispatch-call 错误的当前物理 Attempt/Cause 被旧身份遮蔽；内部 handoff 已保 U2，**未据此证明重发、权限绕过或错误 canonical 终态**。原作者须窄修 dispatcher 对新 final 写 Unknown 的交接；单纯只读失败/取消/只读 Unknown 仍须保 U1，不能一律改返 observer error。需要针对这两种 U1→U2 的实际交接方法做 controlled 回归，核正式 Fault/Attempt/Cause、保留 owner 和无 backend 重发；未变的正常 PG 门无需重复。本审已将精确位置与交错发 root、原 owner 和 integration，未改产品或测试。

本节尚不接受整个 Install Unknown 闭包；Skill Install cold、进程重启恢复、新写再次 Unknown 的物理 PG 注入均未验证。完成唯一报告格式检查后停写交 root 保存；无自有活动命令或 fixture。

## Install U1/U2 错误交接返修复核

2026-10-15；独立只读复核 Skills `d6459fae` 的五个 runtime 源码/测试文件，并亲核 `output/ai/m3-dynamic-skills/install-unknown-handoff/ordinary.log`。**前节 `41c074a3` 的 U1 遮蔽 U2 must-fix 在此固定修订关闭；本次窄改无剩余已证 must-fix。**原缺陷记录与既有失败不删改。

`skill_install_dispatch.go` 不再拼接原错误与 observer error；`recordInstallRecoveryError` 从私有原 owner 选取当前写错误并保存为 `call.err`。选择过程核原 dispatcher 中同一 call、active handoff 的 operation、同 executor/issuer/完整 binding；持 `recoveryMu` 后要求原 `done` 已关闭、`live=false`、known terminal 有效且编码与原 `finalRaw` 精确相同。只有 handoff 中正式 Tool `CommitFailure` 的有效 Unknown Attempt、与原 `inputCause` 相同的 command 且无 Related，才返回其 `retirement`；否则仍返回原 U1。该方法不接收 `observedErr`，不调用 Store、adapter 或新增写权限。已复核相关 retirement 写点：只读失败、取消和只读 COMMIT Unknown 不改私有写结果；真正的新 final 写 Unknown 才替换它。原 U1 不再保留于 U2 的错误树，标准 Fault/CommitFailure/installUnknown 提取与内部 owner 一致。

78960 原 ordinary 日志明确三个 top、十个子例全部 PASS、包耗时 0.020 秒；原 Wait0 复用作者回执。新两个子例用 controlled post-write 私有状态分别模拟 publication→first final U2、final rollback→refinal U2，经生产交接方法检查返回 error 与保存 `call.err` 的 Fault CauseID/CommitFailure/installUnknown、旧 U1 不残留、原 owner 保留；缺 final bytes 不提升错误。另八个子例从原两个恢复入口实际执行只读失败/取消/只读 Unknown/已取消分支，再经 dispatcher 交接，仍保 U1。新 publication 子例使用合成错误状态，未调用真实 Skill writer；两新子例均不执行恢复写 callback、SQL/guard 或物理 COMMIT，不称新的复合 U2 PG 注入。

此次变更只影响错误选择/保存，未变的恢复写入、原 bytes、权限和生命周期路径已按固定源码复核；结合上述针对原缺陷的回归，足以关闭此错误交接问题，不要求重复 67124/64265 正常 native 门。此前亲核的同进程 FinalTx/publication 限定行为继续有效；publication rollback 仍为 Unknown/Joined=false 后精确进程退出，Skill Install cold 和复合 U2 的物理注入仍未验，不外推整个 Skills/M3。仅追加本报告，未启动 Go/native、改产品/测试/current 或执行 Git 写操作；格式检查后停写，无活动命令和资源。

## Object Transfer Project Audit 分派窄复核

2026-10-15；由未参与该分支实现或决策的 MCP owner 独立只读复核 `992dfa5f` 相对 `cf28c32d` 的 `project/object_audit_facts.go` 和对应测试，并追读原 Object 私有证明调用。**此两文件改动接受，无已证 must-fix；仅为授权分派源码结论，不接受 Runner 实际链。**

新增动作限定 TransferIssue/Complete/Revoke、ObjectTransferResource、Success 与 ordinal 0/1/2。当前同 Store/Tx、Project SH、存在且 initialized 仍必验；Issue 再过 Active/Mutate，Complete/Revoke 仅允许向原 Object checker 证明历史收尾，不授新 I/O。已追 `TransferService.within` 的实际 authorization/完整 locked issuer、私有 stage/witness 构造及 `checkProjectTransferAudit`：原 Store/Tx/entry/key、持久 transfer/owner Project、operation/runner/execution/lease 与 postimage 均匹配。公开 metadata 或注册 ObjectService 不能生成私有 witness，替换 Project/entry 被拒，其他动作没有旁路。原 `portError` 未改，正式 Fault 的同指针、Cause 与 Unknown 保持；新增测试覆盖原错误、阶段 ordinal 和真实 checker 拒绝裸 DTO。

本次没有运行 Go/native，未重跑作者已过的 74441，也未亲核其原日志；没有要求增加无变化矩阵。未修改产品、测试或其他报告，只追加本节并检查格式，全部 Git 由 root 完成。MCP 本域仍等待 P-256 fixture 增编与独立信任门，与本结论无关。

## Skills published 安装冷恢复与 Drain 交错复核

2026-10-15；由未参与该实现的 Model owner 独立只读审查，integration 按 root 接受的结论归档。固定闭包为 runtime `2435a097`、同实例启动 Recover `59b2cad9`、真实冷恢复 sole `01726b2b` 及返修 `8902`。已亲核实际实现 diff 与作者定向日志；此 published/running 窗口无剩余已证 must-fix。

独审先发现 Stop 与原只读 lookup 返回交错时，join 等待旧 done 后无条件重进 recovery 会在已移除原 owner 的情况下返回 ShuttingDown，使 Drain 错报失败。R1 重新观察原 retained owner 和实际返回状态；无 retained write 时正常退出，原未决写 Unknown 继续保留。14081 普通回归通过 join-call、Drain 和 retained-write-unknown 三个确定 barrier 交错，并保留原 owner 实际等待、只读失败 Unknown 与 U1→U2 接缝反控。该窄退出修复不要求重复不变的正常 PG 链。

结合 64479 实际 published cold 整 top PASS10.25s：原 child 真实 SIGKILL/Wait，新进程 guard 下恢复同 E/Attempt/原进程绑定，canonical row/xmin 保持、一次安装与上传、零自动分配、后继模型消费及 newApp 正常 Join；原父/子 Wait、六资源 ID、三目录和三 PID 全清退。接受范围不含 reserved/missing cold、cold 新 U2 的物理注入、外部第三方安装或整个 Skills/M3。原失败和先前 must-fix 记录保留。

## MCP 142 与 Secret 原 Delete 恢复独审

2026-10-15；由未参与实现的 Meeting owner 独立只读核固定 MCP `614eddd1`：`ba1d1a7e` 的 Secret 恢复四源、Project SecretDelete 分派三源与 142，`e9d30b0f` 的原 claim/cleanup consumer，以及 `614eddd1` 的外层 connection lease 收尾。沿原 OAuth publication、Secret mutation/lease、ProcessGuard 与 Project Audit 最终 gate 核对调用关系；不覆盖新 sole App 草稿、全 MCP 或其他 OAuth 模式。**发现两处关联的恢复可用性 must-fix，尚未关闭；未发现本次已读 Secret 权限链的额外 must-fix。**

1. `oauth_recovery_connection.go:193–199/250–259` 只将原 connection request 退休并释放 lease，没有收敛当前连接的原 `refresh_id` 与 `connecting` 投影。最小交错为 `publishOAuthToken` 已提交 token/connection request，但原 SDK `finishConnection` 尚未提交时进程停止；新 claim 可 complete，连接仍显示正在发现。`startup.go:124` 拒绝非空 refresh_id；现 Owner 页只对 unconnected/auth_required/error/needs_reconnect 显示连接按钮，connecting 没有该入口。正式 Connection 状态规范将 connecting 表达为进行中、error 允许 retry。修复应在真实旧进程停止、原 claim/full tuple/锁和正式原 lease 收尾后，CAS 收敛仍指向该请求的当前投影；已 released 分支也须处理，较新版本或请求不可覆盖。不得因此重发 OAuth/SDK 请求或假定原远端失败。
2. 仅修当前连接投影仍不足以恢复原浏览器。`oauth_lookup.go:43` 只观察原 commands.pending；`mcp-oauth-session.ts:190/252` 保留原 oauth.complete 的 uncertain intent，`ProjectMCPView.vue:66–84` 因而禁用后续写操作，包括断开与重新授权。后台新的 oauth.begin 确能以当前版本覆写连接（`oauth_flow.go:161–175`），故不能声称全部 Owner API 被锁死；缺的是同原 Session/key 经正式 lookup 观察安全收尾、解除原 intent、再由用户显式发起新操作的可见出口。须使用已证停止/退休事实保留远端 outcome unknown，不能把旧请求改成虚构成功或失败，也不能用清本地状态或自动重发代替恢复协议。

其他已读边界保持：真实 Guard 确认旧 writer 与前驱 claimant 停止；新私有 active owner、same Store/Tx、完整锁重读原 tuple；142 的不可变前驱和单 flow Delete 身份；原 Delete 的 key/binding/version/process 不因 Unknown 替换；Secret opaque 同实例计划仅观察原 receipt/binding 与 metadata，未打开凭据材料，正式未提交观察才给单用 continuation；外层 lease 只授原 Release，不能读或 Acquire。Project 的私有 cleanup 分派后仍经过 `CheckAppendInTx` 的最终 Audit lifecycle fence，未旁路其删除阶段屏障。

复用作者 31343（Secret/contract/Project）及 28690（MCP）普通通过回执，并读对应测试；本次未亲核原日志、未运行 Go/PG、未改产品或测试。142 Source/真实迁移、Secret continuation、归档 Project cleanup 与完整冷恢复 App 均未实际验收，不由上述源码结论替代。两处缺口已交 MCP 原作者；只待其固定返修复核这两个出口，不增加无关矩阵。

### 固定返修复核

继续只读核 `d2a60361` 及 `e371ad67` 的两个原出口。外层 connection 收尾现在在原 Guard/claim/完整锁与正式 lease 退休后，仅 CAS 仍匹配原 P/config/revision/connection/version/credential/request 的投影；已 released 分支同样处理，并在 claim 完成前验证原 busy 指针已经脱离。较新 Owner 发布不被覆写，原 SDK/远端结果不被改判。

原 Session lookup 新增无 result 的 `stopped` 观察，要求同原 user/P/session/command/request/process、完整 recovery claim 与 I/O/lease 退休事实；原 command receipt 不改。该观察同时覆盖 pending complete，以及成功 begin 的旧 URL 已不可用且冷清理已完成的分支；合法 ready URL 仍按原成功回执返回，没有完整清理事实时仍保留 ConfirmationStale。后者是同一恢复出口的遗漏：只修 pending 会让原 begin intent 在页面继续禁用新授权，不能仅凭过期时间清本地状态。

前端严格解码 `stopped` 且拒绝携带 result，核当前身份后清原 intent，明确显示远端结果无法确认；不重新兑换授权码或自动导航。Project 页重新读取当前连接后，用户才可显式发起新 key 的授权。源码与新增窄测试覆盖了这两个原 must-fix 的修复形状，**静态复核与受控测试未见剩余同范围 must-fix；冷恢复实际验收尚未关闭**。复用作者 41747 原 Wait0：`TestOAuthRecovery|TestMCPOAuthLookup` 普通通过 0.011秒。亲核 `output/ai/mcp/oauth-stopped-e371-node.log`：79885 的三个 spec、21 tests 通过，包含实际 Session/router/account transport 原 key lookup、清原 intent、零自动 Complete、显式新 key begin，以及 strict stopped 与回调文案；两叶格式化差异已读，仅换行。另亲核 `output/ai/mcp/database-142-5199-actual.log`：71002 的 `TestEmptyRepeatUpgradeAndHealth` PASS5.65秒、PG17 actual_wait=0 与三组官方清理可见；作者另报六资源 ID、三目录与 PID absent，142 已实际应用、不可回写。该迁移门不验证恢复业务；原 exchange Unknown 的 cold 首链及 token 已发布、SDK 未完成的外层 lease/pointer 分支仍未实际验收，受控 helper 的次序断言不替代 SQL CAS 或真实冷恢复证据。

## Knowledge 139 引用释放与成对清理独审

2026-10-15；由未参与该清理实现的 Model owner 独立只读核 Knowledge `97dd9db2` 的 cleanup 十一个领域文件、139、sole App 清理 harness 及原生命周期 helper 的可选配置/写计数增量。此前仅解释过既有 Model retention 合同，未参与该清理或迁移实现。**本限定闭包接受，无新增已证 must-fix。**

私有 cleanup 控制绑定原 Delete、Project、profile/version/snapshot、source operation/原 process，同 Store、当前 guard、Project EX 与完整 Model plan 锁均重验。只有本域 Model operation 已 retired/version3 才继续；没有从本地 owner 缺席推断租约消失。原 profile 状态更新与正式 Model reference release/Applied 同事务，Unknown 保留原 request/plan/cause；后续确认只观察原 before/after，失败仍保原 Unknown，不顺带清理下一批。物理删除按 FK 顺序、有界且比较完整 preimage；source operation 与 profile 成对删除，只临时延迟 139 指定的那一条 FK，随后恢复 IMMEDIATE。确认要求原两集合全在或全无，partial/drift 拒绝；Project 过滤、原 Model 行及其他 Project 保留。139 改为 NO ACTION、DEFERRABLE INITIALLY IMMEDIATE，未取消最终引用完整性或其他约束。

亲核 `/tmp/agenteam-k-cleanup-5afd3dee/cleanup-native.log`：99337 的 populated-schema-upgrade 子例 PASS7.84s，合法 138 存量经正式 139 后原循环 pair/其他约束不变；其整个 top 后续失败，仍保留 FAIL。另亲核 `/tmp/agenteam-k-cleanup-97dd9db2/native.log` 与 `cleanup-check.json`：50168 fresh139 父选择器 PASS13.58s/原 Wait0，真实两 Project、两 embedding、零 Chat；原 release 与 pair 删除各一次 Unknown 确认的四 schema 写差值均为零，Knowledge 清完、Model/另一 Project 保留、App 正常 Join，六资源 ID、三目录与 test PID 全 absent。该轮未重跑 upgrade，末日志的组合摘要不作为本轮升级证据。

上述 Unknown 是实际 PG COMMIT 成功后在 Store 返回边界掩蔽为 Unknown，随后经原正式只读确认；不是网络 COMMIT 丢包或 rollback 双态注入。接受范围不含旧 version2/未退休 operation 的冷收敛、整个 Project 删除及其他参与者。此次未运行 Go/native、未改产品或测试，只追加本节；输入未变的作者实际证据按上述范围复用。

## Meeting 137 删除与原 owner 退休有限独审

2026-10-15；由未参与删除实现的 Realtime/Artifact owner 独立只读核固定 `ea13e1c8`。范围限定 Meeting deletion contract、接受/查询/cleanup/purge/authority/audit/HTTP、137，相关 Runtime/control/Summary 收尾接点，以及 Project 私有删除 cleanup gate、Execution Meeting retirement 与对应控制门测试。此前 Realtime 跨接口阅读不涉及删除实现；本轮不扩全 Meeting、D25 或 Project 删除。**此限定源码闭包未见剩余已证 must-fix；真实 App purge 尚待实际验证。**`ea13e1c8` 修正原 App Delete 路由登记，前轮 HTTP 404 不能据此推断领域删除失败。

接受入口使用原 Account Human 与当前 Owner；完整 User/Project/Meeting/command 和 Decision 锁下重读版本、原 command 和 deleting gate，首次接受与取消标记、删除 receipt、Audit 同事务提交。同 key 的查询与重放仍需当前 Owner 和原语义摘要；保留 receipt 不等于裸 ID 可授权。后台 cleanup 使用同实例原删除意图的私有 active witness，绑定 Store/Tx、完整 P/M/command、原 initiator、当前 ProcessGuard 与 Project EX。已接受的清理可在归档后收敛，但不能借此新发 Execution，也不越过 Project 最终 Audit 阶段屏障。

原 launch Unknown 只观察原 key/tuple；缺 Execution 指针或空本地 map 不构成已退休。曾核查的旧 generation/attempt 漏清疑点由正式 restart 路径排除：同 Meeting EX、原完整 association 和同 Tx ResultReader 下，retry 只接受原 Failed，regenerate 只接受原 Succeeded，才 CAS 当前 generation/attempt。已完成 Turn 中的当前 replacement 仍由删除 cleanup 扫描；purge 另读取全部 immutable launch intents，不用当前 Contribution 指针替代历史范围。

最终 purge 在完整历史 Execution/command 锁集合下再次核原 deleting version、原意图全集和 settled 条件，再借同 Tx 私有 witness 调原 Execution 四 owner 的退休证明。终态必须与原 capture/direct terminal 事实一致；当前 process 的本地登记、实际返回尾和持久 owner 均须收敛，旧 process 需正式 ConfirmStopped。Summary 的 returned/retired、原 retention release 及终态版本也必须成立；已有 Unknown terminal transition 先观察原请求，不能改目标重发 Model。固定 purge 语句只删 Meeting 自有记录，保留独立 Artifact/Object、Execution/Tool/Approval 等资源及原删除 command/receipt；删除完成、精确 Audit postimage 与 lifecycle event 同事务，Unknown 不报告已删除。

本轮只读固定源码和控制门测试，未执行 Go/PG/native，未亲核作者普通门原日志；作者既有普通通过仅作为交接事实，不冒独立运行。尚未取得真实默认 App 删除到 purge、冷旧 process 证明或物理 COMMIT Unknown 的新结果；控制测试不替代这些行为证据。仅追加本节，产品与测试未改，全部 Git 由 root 保存；Artifact 上传的并行作者验证不计入本删除独审结论。

## Meeting Realtime Web 有限独审

2026-10-15；由未参与该状态机或 codec 实现的 MCP owner 独立只读核 `f6dc8f00` 的八个 Web 状态机/消费/测试叶，以及 `1034feb7` 的 Realtime 与 Runtime codec 四叶，并追读既有 Session 身份、重复 JSON 成员检查和 Go canonical-v1 合同。此前仅参与无关 MCP/Meeting transport 端点闭集；本轮不重审 Model owner 已核的 Gateway，也不扩 token stream。**该限定源码闭包接受，未见已证 must-fix；尚不代表实际 WebSocket 浏览器验收。**

一个 socket 归原 Session 身份及 epoch；登出/身份替换会清除订阅与内容，异步 digest 解码完成后再次核当前 connection/identity。Topic 精确匹配 Project、Meeting，以及 Execution 订阅的 Contribution/generation/attempt/Execution 全 tuple；页面上下文或当前执行变化会同步 dispose 原订阅，旧 socket 回调、退订中的 queued frame 不能写新页。公开投影仅触发原授权 HTTP 的审批事实重读，没有从实时 DTO 取得命令或审批权限。

只有 subscribe ack 后才能请求 snapshot；原 watermark 与 durable version 分开处理，读取期间更晚的 invalidation 会触发补读。补读连续八次、十六订阅、六十四 queued frame 及字节总量均有界；超限、epoch 变化、错误帧和断线转显式 stale/关闭，由用户重新连接并取得新 subscription ID 和授权 snapshot，不静默丢弃更新或无限重试。暂时不可用保留最近确认内容并显示暂停状态，身份失效则擦除内容。

Codec 复用 decoded-key 重复成员拒绝，逐层检查固定 shape、UUIDv7、十进制 64 位进度、原 scope、UTF-8 与有界文本/条目。Snapshot 对实际 content 重算 SHA-256；固定 typed object 的键为 ASCII，数值字段为安全整数或十进制字符串，排序、数组顺序、Go HTML 与 U+2028/U+2029 转义和 canonical-v1 一致。没有把 transport sequence 当 Execution 版本，或将任意 JSON/外来 tuple 当正式投影。

本轮未启动 Go、Node、browser 或 fixture，未修改产品/测试。作者既有状态机 23 tests、codec 23 tests 与 full vue-tsc 通过只作为已报告证据复用，本轮未亲核其原日志；实际 WS 握手、服务端身份撤销推送、浏览器刷新/断线及视觉表现仍由对应正式组合门验证。只追加本节，其他 MCP、Knowledge、137 及历史结论保持原样，全部 Git 由 root 保存。

## Runner skill.prepare maintenance / cold 有限独审

2026-10-15；本 reviewer 未参与所验实现。只读 `/workspace/agenteam-m3-runner-skills` 固定 `214fcbbf`：`426c0821` Object maintenance 与 Recover 私有入口、`8ff441f3` 合同、`63a4c923` Object/Skill/Runtime cold 叶、`151b7b71` Builtin cold 分派、`bb6339e9` App 固定路由及对应测试；`4e4fd504`/`b1d0f50f` 是 fixture 修正，`fbb40f5b`/`214fcbbf` 是 sole harness。**该限定源码闭包未发现新的已证 must-fix，但 cold 实际门仍失败且尚未到目标窗口，不能接受其运行闭包或整个 Runner/Skills。**未扩 Skill Publisher、全 Object 或其他迁移域。

maintenance 仅由真实 `recoverProgress` 枚举的原 row 发出有界私有 witness，结束即失效；ObjectMaintenance actor 必须指向原 transfer/project，且同一个实际 Object Store。只准 GET / SkillRevision 的 Cleanup、ConfirmTerminal 和原 TransferOwner Release；裸技术 DTO、Issue/Materialize/Capture/Inspect、新 Acquire 均无权。已有持久 validated retirement evidence、kind、32-byte digest 才进入；pending reference 或旧进程死亡不能替代它。同 Tx 重核当前 guard、完整依赖锁、原 transfer request fingerprint、initiator/command/manifest/version/retirement 状态及精确 lease tuple。原 `retireInTx` 仍在真实 source/reader/writer lease 存活时保持 transfer lease；maintenance 不制造已 Join，也不赋予正文读取、签名或执行权限。

cold 沿原 Builtin registered Lookup：限定 `skill.prepare` rev1，排除本地 Execute owner；事务外对原 Attempt.process 调用 `ConfirmStopped`，随后同完整锁重核原 operation/attempt/request/ordinal/fence/input/fingerprint 和当前 guard，原 process 不重写。同进程和活 claim 仍拒绝；新 Skill 叶只能借此原私有 proof 和 committed arguments/captured binding，重新核 immutable metadata。Object observer 在同 Store/Tx 与完整锁内重新确认原 Issue actor/key/request/version/Operation/Owner/Object/manifest/semantic，必须原 durable retirement 加精确 released lease；缺行、pending-only、active lease、错误 evidence/tuple 均不构成成功或无效应证明。

只有上述观察完成，Skill 才编码闭集 `PrepareUnknownResult`（原 Skill/revision/mount、`may_have_prepared=true`、`DEPENDENCY_UNAVAILABLE`，无 path）；Builtin 复用严格 classifier 将原 Attempt 记为 unknown，既不重发 Prepare/Issue/Sign，也不建新 Attempt 或声称 package 成功。App 构造时固定 preparation/maintenance 两提供方，技术分支只选上述 Cleanup/Confirm/Release；原 context/Tx 直传，拒绝不 fallback，readonly retirement/command observation 仍走 preparation 私有证明。138 仅作为该 typed terminal 的共同前置阅读，本轮未独立验证其 SQL 行为；134–137 不在审查范围。

12175 Object maintenance ordinary、55474 Object/contract/Skill/Runtime cold 拒绝门及既有 Builtin cold 回归、80962 App router ordinary 的 Wait0 复用作者原回执；本轮已读对应测试，但无原普通日志可亲核，未重跑。这些测试分别为 controlled SQL/私有 witness/路由验证，不代替真实 PG、reader 或 OS 停止证明。

已亲核 `output/ai/m3-runner-skills/app07/native.log` 并对照 `c8a1a649` 正常 harness：59470 PASS 38.52 秒，test PID1648229 actual Wait0。正式 Owner install/assignment、真实 GET/Runner 只读包、显式 run-command Exit0/完整输出与进程组终结、三轮受控模型消费及原 App/Runner Join 均有断言；原 transfer lease released。该输入早于新增 maintenance/cold，正常通过不外推后者。日志有三 fixture exact cleanup；`app07/cleanup.json` 记录六资源 inspect 退出1、三目录与两个 PID 不存在，本轮未重新 inspect。原 63865、93239/21628 失败及间歇关闭原因未定的限制继续保留。

已亲核 `output/ai/m3-runner-skills/cold01/{native.log,wait.json,cleanup.json}`：2236 固定 `b1d0f50f`（sole `fbb40f5b`、连续1–138）**FAIL 27.20 秒 / Wait1**，wrapper 总40.869秒。真实 provision Join、first child 原 Reader 和 Runner generation2 重新认证已到，但原15秒未到 `m3_prepare_terminal_reader_held`；末原 Tool running、GET1/complete0/retired0/activelease1/revoked0。因此目标 SIGKILL、第二 child/new guard、maintenance retirement、strict Unknown 消费及恢复后 Join 都未执行。PID1722660 Wait9 是失败 cleanup，不是通过目标屏障后的故障注入；日志另有原 Runner cleanup Wait 和三 fixture cleanup，保存的检查记录为六资源/三目录/四 PID absent。父 provision trace 不能代表 first child 内部 Skill/Object 行为，不能据此认定 maintenance provider 已失败。

`214fcbbf` 仅追加 child 最多64条闭集事务诊断与脚本存在/只读模式布尔，未改原预算或产品，尚未编译/实际。下一必要门仍由作者沿原 `cold-terminal-reader` 定位首断点、修正后复验；不得以 normal/ordinary 关闭 cold，也无需重复未变正常链。IssueUnknown 物理分支和其他未完成窗口保留待验。本轮只追加本节，无 Go/native/fixture、生产或测试修改、Git 写；格式检查后停写交 root 保存并释放席位。

## Meeting Reply 与历史消息引用有限独审

2026-10-15；由未参与该 Reply 实现的 MCP owner 独立只读核 `0b6a3a02`、`fb340c46` 与 `85096c55` 的八个受影响 Web 叶：client/Meetings API、ProjectMeetings controller/View、历史版本组件及三份相关测试。此前仅参与无关 Meeting transport 端点闭集；本轮不重审 Realtime、不扩 Meeting 执行或审批。**此增量源码范围接受，未见已证 must-fix；真实 Reply 用户旅程仍待对应浏览器门。**

`reply_to_message_id` 只允许出现在 Send 和其原 lookup；必须是 canonical UUIDv7，Start、显式 null/空值/非法 ID 均拒绝。capture 复制并冻结原 Reply ID 与正文，Session 保存原 body/key，Unknown 后的 lookup 不从可编辑草稿重建意图。未决状态禁止新发、切换或取消 Reply；not_found 保留原草稿，只有确认已提交才清除正文与 Reply，版本冲突也不丢草稿。身份/页面上下文变化沿原 owner 退休路径清除，未把本地 Message ID 当服务端授权。

消息替换后，引用仍以历史不可变 Message ID 为准；预览从当前已授权 detail 的当前/历史消息集合读取。历史组件只展示原消息，不编辑或重写历史；Reply 事件同时要求 canReply 和精确历史成员，disabled 状态也在 handler 拒绝。发送权限、原预期版本和原命令入口保持不变，历史回复按钮不复用重新生成/重试 Execution 的权限。

已只读核 actual API→transport 对同 Reply body/key 的 Send/lookup 断言，以及 Session capture、Unknown、显式确认、版本冲突与历史禁用测试。作者组合 42 tests 与 full vue-tsc 通过按回执复用，本轮未亲核原日志、未启动 Node/Go/browser、未改产品或测试；不将这些控制门当真实 HTTP/浏览器发送通过。仅追加本节，既有 Runner、Realtime、MCP 等结论保持原样，Git 由 root 保存。


## Artifact Human 上传的首次写入并发独审

2026-10-15；由未参与该上传实现的 Model/Runner owner 独立只读核合同 `ebf8ea21`、实现 `1e324969`、真实测试 `d4378e2e` 及修复 `4df0794d`。范围是原 Human Session/Project Owner、完整 upload intent 与原两命令锁、payload 预留、数据库 receipt 完成发布、Unknown 查询及真实 body Close/owner 退出；不扩全 Object 或 Artifact 生命周期。入口沿原 Account HTTPBoundary，完整 intent 和当前权限在原事务锁内重核；Lookup 不做 Object I/O，complete 从原持久 receipt 发布，未知结果不投影成功或公开未完成 Artifact。

发现并已关闭一个首次写入的 TOCTOU must-fix：A/B 都在较早查询中见 pending 且无 attempt，A 随后阻塞于原 spool，B 的实际 PUT/验证失败并关闭 attempt 后，旧实现允许 A 借原 Object 内部恢复语义再预留一次。`4df0794d` 将 Human 首次写入限定带入原 reserve 事务，在完整锁内、调用 ReserveUploadInTx 之前重核 pending 且 object/upload/attempt 均空；旧内部 UploadPayload 的恢复语义和 uploaded 的只读验证保持。该修复不是仅增加较早预检。

亲核作者 `8222` 原日志 `output/ai/m3-artifact-user-upload/objects-first02/native.log` 及同目录 `cleanup-check.json`：真实 late-body 门 PASS2.66s、live-attempt 门 PASS2.27s，原测试 PID1744185 actual_wait code0。late-body 用真实 spool barrier 和失败 PUT 后的 durable closed attempt 区分竞争时点，随后 A 被原事务拒绝，保持一次 PUT、一个 attempt；live 门拒绝仍活跃写入的第二次发送。6 个资源 ID、3 个目录及 wrapper/helper/test 3 个 PID 全部 absent。结合固定源码修复，首次写入并发 finding 已关闭，限定闭包未见剩余已证 must-fix。此轮复用作者实际运行并独立核证，未重跑测试；fixture 使用原受控领域 Authority，不能据此宣称真实默认 App/Account Session 上传、网络 COMMIT Unknown 或 rollback 分支通过，这些范围仍由后继实际门分别证明。


## Meeting 同一 Execution 的 Memory 工具实际链有限核证

2026-10-15；Model/Runner owner 独立只读核共同候选 `4ecdb` 的 `internal/central/app/meeting_m3_memory_integration_test.go` 与原 `58034` 日志 `/tmp/m3-meeting-memory-native.dp6uryot/native.log`。本轮只核该 App 编排、原输入消费与退出，不重审已有 Memory 核心。Owner HTTP 创建 Meeting 后，原 Meeting 来源的同一个 Execution 依次 retain、recall、reflect、final，再发布 Summary；后续三个 Agent 请求逐一检查原 Tool 结果。持久断言为1个 Execution、0个 Task、3个 Tool 结果、1个已提交 Memory operation、6个 Memory model calls，全部11个 Model calls 已退休且原 Secret leases released。没有另造 Task、Tool witness 或 Memory mutation 路径。

原日志 PASS10.90s，测试 PID1750063 actual_wait code0；原 Drain、App Wait0、各 owner Joined、全部受控 peer handler/连接关闭、ProcessGuard stopped 与数据库连接归零均有断言。对原 `owned.json` 的6个精确资源 ID 再次只读 inspect 均退出1，3个目录及原测试 PID 均不存在。该正常同 Execution 组合的有限核证接受，未见已证缺陷；使用受控 Model peers，不能外推外部 Provider、cold、Unknown 或完整 Memory 生命周期。本轮未新增测试或修改产品。

## Meeting 137 删除返修与默认 App 实际闭合

2026-10-15；原 Realtime/Artifact reviewer 继续独立核对前述 137 限定范围。初次静态结论后，实际 `23253` 的领域事务报 SQLSTATE `23514`，暴露无 Execution 的 Contribution 被写入不合法取消标记；`61897` 又暴露接受删除时原 Project Audit 所需 EX 锁未持有。这两次失败和此前 HTTP 404 均保留，不由普通门或旧静态结论覆盖。已逐一核固定 `87dcc98f` 和 `d0f95aee`：接受 UPDATE 只对有当前 Execution 的项置取消请求；确定未尝试且没有 Execution 的 cleanup 分支写 cancelled 并清除取消请求，Unknown 分支仍仅观察。接受命令在完整初始锁集合中取得 Project EX，原 Lookup SH 与 purge EX 不变，没有事务中升级锁、DDL 或绕过原 Audit authority。

亲核作者 `50965` 原日志 `/tmp/m3-meeting-delete-lock-native.POySrSeU/native.log`，并对照 `internal/central/app/meeting_m3_deletion_integration_test.go` 的实际断言。共同候选 `61828`（App binary SHA256 `f38b932587c05525d3da0ca984d0eb883f825fa85927ec5b75267b33d896005e`）中 `TestMeetingM3AppDeletionRetiresOriginalOwners` **PASS7.81s，原测试 PID1765402 actual_wait code0**。Owner HTTP 接受原删除意图后只用原 key 查询完成；当前 A 的原 Execution 被取消、尚无 Execution 的 B 未派发，4 个原 Model calls 均退休且 lease released。随后 Meeting 自有记录清除，3 个外域 Execution 保留；completed receipt、精确 service/initiator Audit、lifecycle event 与 Outbox 由实际 xmin 断言同事务，晚到 Send 为404、原 receipt 可重放。

该测试最后核原 runtime、Meeting runtime、account assembly 与 producers Joined，受控 peer 的4次调用全部退出，原 ProcessGuard stopped、数据库退出及连接归零。日志显示三组官方 cleanup；本 reviewer 又按原 `owned.json` 只读 inspect 6 个精确资源 ID，复核3个目录及测试 PID 均不存在。**137 当前默认 App 删除到 purge 的限定闭包接受，未见剩余已证 must-fix。**本轮复用作者实际结果，未另跑删除门；不外推旧 process 冷恢复、物理 COMMIT Unknown、浏览器、整个 Project 删除或 D25。Artifact 作者测试不计作本删除独立执行。

## MCP 启动恢复上下文的有限独审

2026-10-15；由未参与修复的 Realtime/Artifact owner 独立只读核 `df505512` 的 `app/account.go`、`app/m3_mcp.go`、`app/m3_mcp_lifecycle_test.go`，并追读原 `initialize/run`、MCP `processOwner`、Content recovery、reconciliation 及 Account/resources 退出链。原缺陷由源码确认：两 worker 使用短期 security/startup ctx，`initialize` 返回及 `run` 收到启动结果即取消该 ctx。旧 MCP `82282` 的恢复未完成日志不能独立证明具体在哪个取消阶段停止，仍保留原失败。

固定修复只在私有启动 helper 去除该 ctx 的取消与 deadline，保留 context values；调用前、两个入口之间及返回前仍核原启动取消，实际入口错误原样传播。两个真实 worker 仍经同一 Service 的 `processOwner.begin` 登记原 call，StopAdmission 逐个取消，Drain 等原 goroutine returned 并处理保留的 Unknown observer，Joined 不以 cancel 代替返回。原 Account Stop/Drain/Force、未 Joined 时的 provider 保留及部分启动失败清退路径未改。

已亲核 `/tmp/m3-mcp-startup-lifecycle-m6sp_3b1/ordinary.log`：作者 `83438` 原 Wait0，精确 `^TestMCPRecovery(Survives|Partial|Rejects)` 命中3个顶层测试和 partial 的2个子例，全部 PASS，App 包0.011s。测试以受控 owner 验启动取消后两个原 call 仍活、原 Account assembly Drain 等待真正 return barrier、部分启动失败仍由原 owner 退出、已取消启动不再 admission。**此启动上下文修复的有限范围接受，未见剩余已证 must-fix。**本 reviewer 未重跑；受控 owner 不代替真实 PostgreSQL、ProcessGuard 或新 MCP cold 恢复实际链，后者仍由对应原测试验证。本轮只追加这两节，产品及测试未改，全部 Git 由 root 保存。

## Artifact 默认 App 上传与稳定引用点查实际核证

2026-10-15；原 Model/Runner reviewer 在前述上传与首次写入并发独审基础上，独立核共同 `61828` 的原实际日志及 `f3907708` / `ed149247` sole 增量。`f3907708` 只修测试作用域：上传只在 Project A 调用 Artifact，故保留 A 的原 ledger 全退休检查、要求 B 总调用数为零，再执行原 App/ArtifactPair/ProcessGuard/数据库退出及零 active Object lease；没有删除退出门或为 B 伪造调用。旧 `62261` 末尾 helper 失败保持，不回填为通过。

亲核 `output/ai/m3-artifact-user-upload/app-second02/{native.log,exit-status,owned.json,cleanup-check.json}`：作者 `37618` 中 `TestArtifactUserUploadHTTPDefaultApp` PASS3.97s、`TestArtifactMetadataHTTPDefaultApp` PASS4.07s，原测试 PID1767609 actual_wait code0，wrapper exit0；binary SHA256 `f38b932587c05525d3da0ca984d0eb883f825fa85927ec5b75267b33d896005e`。上传经默认 App、真实 Human Session/CSRF/current Owner 和 Object 存储，验证二进制精确读取、complete 前无公开 Artifact、原发布 COMMIT 后 typed ACK Unknown→原 Lookup、单次 Artifact/Audit/attempt、两种取消以及越权拒绝。点查以稳定 ArtifactID 取得原完整 ArtifactRef，GET/HEAD 不改变 Object/lease 事实且 read audit 为零正文 bytes，随后仅用返回的正式 FileID 读取原正文；跨项目、非 Owner、缺失 ID 和额外 query 均被拒绝。两个 top 均走到原 owner/App/guard/DB Joined。

官方三 fixture cleanup 与保存的精确检查相符：6个资源 ID、3个目录及 wrapper/helper/test 3个 PID 全部 absent。本轮只读源、原日志与清理记录，未重跑测试；**默认 App 上传与点查的上述限定闭包接受，未见剩余已证 must-fix。**发布 Unknown 是原真实 COMMIT 后的受控返回 mask，不是网络故障或 rollback；8222 的并发门独立复用，浏览器上传/打开及其他生命周期仍不在本次实际结论内。

## Meeting prepared Summary 冷窗口的有限核证

2026-10-15；由未参与该实现的 `m3_core_review` 独立只读核共同候选的 Summary cold sole、原 child/helper 与作者 `2805` 原日志 `/tmp/m3-meeting-summary-cold-native.0ubWfzyF/native.log`。原 prepared source 已提交、selection.version 仍为空且旧 process 绑定存在；旧 child 实际 SIGKILL/Wait9 后，新 guard 沿原 source 的 absence 事实收敛为诚实失败，没有重新 Resolve、派发 Model、新 lease 或 Summary 输出。随后 Owner 以正式原 Turn 取消入口结束，replacement App 实际 Wait0/Joined。

原 top PASS9.76s，parent 与双 child 的实际退出有原日志；reviewer 又独立核原 owned record 的6个精确资源 ID、3个目录与3个 PID 全部不存在。**接受 prepared 已提交、Resolve 前这个唯一窗口，未见该范围剩余已证 must-fix。**不外推已 Resolve、模型 I/O 中断或全部 Summary cold。原观察 SQL 类型错误失败保留；本轮复用作者运行并核证，没有新测试或产品修改。

## MCP 原 Delete 冷恢复实际的有限核证

2026-10-15；由未参与 MCP 实现的 Meeting owner 独立只读核 `70900` 原日志、对应冻结 sole 与原 lookup 出口。该轮使用已修复长期 worker 启动上下文的候选：原 exchange 只发生一次，retire claimed 真实提交而 Delete 尚未 prepare；旧 child 实际 SIGKILL/Wait9 后，新 guard 沿原 request/flow/credential tuple 完成同一个 Delete 的 canonical 观察和收敛，未换 key 或重新 exchange。原 command 仍 pending，原 sameSession lookup 明确返回 stopped 且无成功结果；新 App 实际 Joined。

原 top PASS6.81s、parent Wait0；作者保存的8个精确 Docker ID、3个目录及 parent/双 child 清理记录均为 absent，reviewer 亲核原日志与固定断言，未另起 fixture。**OriginalDelete 的上述限定恢复范围接受，未见剩余已证 must-fix。**此前 `82282` 超时不回填；本结论不代 PublishedConnection 的外层 lease/pointer、stranded 的整 App graceful Join、startup 隔离或 Human Refresh 外层冷收尾。本节按实际 reviewer 结论归位，integration 仅整理记录，未冒充独立执行。

## MCP stranded materialization 冷清理的有限核证

2026-10-15；由未参与该实现的 `m3_core_review` 独立只读核固定 `4b95` sole/child 与作者 `90562` 原日志 `output/ai/mcp/stranded-cold-4b95.log`。原 Put 已返回并由旧 child 持有屏障，真实 SIGKILL/Wait 后的新 guard 对精确 materialization 完成清理，原 MCP owner Drain/Joined；原 Tool 保持1个 attempt、0个 result，SDK与Model各1次，无重派发。原 top PASS8.44s、parent Wait0，reviewer 又独立 inspect 原8个精确 Docker ID、3个目录及3个 PID 均不存在。

**接受这一 MCP materialization 冷收尾范围，未见剩余已证 must-fix。**新 child 最后仍由 SIGKILL/Wait9 退出，原记录明确不声称整 App graceful Join；Unknown Execution 未假造终态。本结论不代 PublishedConnection、startup 隔离或 Human Refresh 外层 lease/pointer 冷收尾，旧结果观察失败保留。integration 按实际 reviewer 结论归位，未另跑 fixture。

## Meeting Decision Inbox 取消返修与默认 App 实际核证

2026-10-15；由未参与本次取消修复的 Model/Runner owner 独立核作者 `58814` 原实际、固定 sole 与退出记录。此前 `50907` 中 answer/skip 已完成，cancel 的 Execution/Turn 已取消但 Decision 仍 pending；可选 typed 零 ID 被 `.String()` 编码为全零 UUID，而原 SQL 用空串表示省略，导致原发现及同事务重读漏掉目标。修复只规范未指定的可选参数，必需 Project/Meeting 与已指定的 T/C/E/D 保持原值，原权限、锁、Execution 与迁移不变。

原七回复 top 在共同 `40974` 候选中 PASS13.38s、原 Wait0。answer/skip 沿原同一 Execution 继续并完成 Turn，cancel 使原 Decision cancelled/version2、Execution/Turn cancelled、Inbox resolved；重建不重开。实际 sole 计数为6条 canonical 事件（3 requested、3 closed），6条 Outbox 与对应事件同 xmin，3条 terminal 事件与对应 Decision 同 xmin；不是四条事件，四指 requested/answered/skipped/cancelled 四类。7个 Model calls 与 Secret leases 全退休，原 App/Guard/数据库退出及官方 fixture 清退齐。

Model owner 接受上述同 E、Inbox 与原退出的有限闭包，未见剩余已证 must-fix；integration 已核 sole 的6/6/3计数后整理本节。原 successor stale 观察未被当作根因或要求修改 Execution，旧超时不回填。本轮未重跑，真实浏览器、其它控制及完整 Meeting 仍按各自出口验证。

## Meeting 双 topic Realtime 默认 App 实际核证

2026-10-15；由未参与所验实现的 `m3_core_review` 独立只读核共同 App `6ae58476`、最终 Playwright `ba2efba5`、作者 `22854` 原日志 `/tmp/m3-meeting-realtime-mode-native.KnmbkTx2/native.log` 及对应冻结断言。原 Go Wait0/PASS9.43s、Playwright 单例 PASS4.8s；两 topic 的精确 acknowledgement/snapshot/invalidation、原 Decision 同一 Execution 继续、已提交 Tool 结果/公开文本与 Timeline Summary、Start/Answer 原 lookup 均实际到达。原3个 Model calls、3个 lease、1个 Decision result 及 App/runtime/owners/Guard/数据库连接归零断言通过。

前两轮关闭事件观察和默认 mode 与原 intent 不符的失败保留。修后显式选择原 sequential，业务结束时复制完整帧窗口，随后实际导航离开 Meeting 并观察 socket close；没有按类型筛掉业务失败帧。45是包含 teardown 的总观察帧数，Go 严格核验导航前完整业务窗口，退出尾保留独立诊断。reviewer 亲看两张截图，又独立 inspect 6个精确 Docker ID、3个目录以及 App/list-Node/PW-Node 3个已记录 PID 全部 absent；没有逐个捕获 Chromium 子 PID，不扩大为每个浏览器后代的独立退出证明。原私有 result JSON 已由 Go 读取后删除，reviewer 未声称亲读该已删除文件。

**接受这一有限双 topic 正常用户链与原退出闭包，未见范围内剩余已证 must-fix。**不外推 token 增量、新的 resync/撤权异常旅程、窄屏或完整 D24/D25；Gateway 旧普通/race和异常边界证据按原范围复用。Composer 的后段失败独立保留。integration 仅按实际 reviewer 回执整理本节，未冒充独立执行或重跑 fixture。

## Meeting Retry/Regenerate/history 浏览器实际核证

2026-10-15；由未参与所验实现的 `m3_core_review` 独立核作者 `90473` 原实际与固定断言。输入为旧共同 UI App `96226`、前端 `15624` 和 `f3a4b222` CJS；原日志 `/tmp/m3-replacement-final.rcRPDIh6/native.log` 显示 Wait0/PASS35.77s。真实 Retry、原 lookup 与输入 lineage、新 attempt、Regenerate 新 generation、history/deferred Summary、后续正常 Turn、八次 Model/lease 退休及原 App/Guard/数据库退出均到达。原6个资源 ID、3个目录及记录的 App/Node PID 已清退，Chromium 后代未逐个捕获的限制保留。

**接受上述限定用户链，未见范围内剩余已证 must-fix。**本 top 未新增 Retry/Lookup key/body 或完整 context bytes 的逐字比较，原客户端与领域合同证据按原范围复用；不因此扩大到 RegenerateUnknown 或额外矩阵。Metadata 与 Composer 的失败和后继验证独立保留。integration 按实际 reviewer 回执整理，未冒充独立执行。

## Runner prepare immutable input 私有借阅有限核证

2026-10-15；由未参与实现的 `m3_core_review` 独立只读核 Runner `8c0d40a6` 的四个 Execution 叶及 `d7a1f29d` 测试修正，并亲核作者 `3752` 原 `ordinary-fixed.log`。原完整 current/guard/锁检查先于借阅创建；借阅绑定同 driver、Store、Tx、原 plan/binding 与活期，只返回原已验证 immutable bindings 的副本，单次消费，callback 返回/error/panic 后清空。错配、过期或已取消的借阅不退回另一条授权路径；无借阅调用仍走旧完整 SQL 校验。

三个顶层测试、14个子例及 fallback 检查实际通过。前轮测试构造缺少必填 Description 的失败保留，未被当作生产失败。**接受此四叶的有限安全闭包，未见新 must-fix；不证明原15秒 reader 屏障、性能改善或真实冷恢复已经通过。**原正常链复用，后继仍须由原 cold actual 回答；integration 参与接缝约束，只整理独立 reviewer 结论。

## Composer 来源、附件与删除完整浏览器核证

2026-10-15；由未参与所验实现的 `m3_core_review` 独立核作者 `17997` 原日志、固定 Go/Playwright 断言及三张截图。输入为共同 `40974` App、`73szlbsa` 固定前端和 `7753966d` Playwright 叶；原 Go Wait0/PASS19.56s、Playwright PASS13.5s。原选择范围与 Request 终止观察的两轮失败保留，修后没有扩大原预算。

正式来源选择、历史 Reply、上传/正式 Artifact、独立 Pin、原发送与 lookup、Delete 持久事务和后像全部执行；原 Go 精确核对 key/body、单次命令及各持久事实，五个 Model/lease 退休与 App/owners/Guard/数据库连接归零通过。13条响应中11个200均实际 finished；仅原计划丢失 complete/turns 两个 POST 的502允许 failed，其他成功请求没有放宽。三张截图包括390宽深色下读取原文本，未把该单图扩大为全站窄屏验收。

reviewer 独立重核6个精确资源 ID与3个目录均 absent；捕获36个 PID 中34个 absent，两个 Chromium 后代保持原 start_ticks 且为 PPID1/Z，不宣全部 PID 不存在。**接受上述完整用户旅程与原退出闭包，未见范围内剩余已证 must-fix。**不外推数据库物理 CommitUnknown 或全 scope 竞争；旧 `44126`/`53366` 失败不回填。integration 只归位实际 reviewer 结论，未冒充独立执行。

## Meeting Reply 与 Inbox 定向 PostgreSQL 核证

2026-10-15；`m3_core_review` 复用已完成的有限源码审查，独立核固定 `6c1f674a` 的 `message_reply_test.go`、`20641` binary及作者 `53653` 原日志。精确 top PASS2.14s/Wait0；原 Send/replay/Lookup、同 key 更换 ReplyTo 拒绝、跨 Meeting 新目标拒绝且 Turn/version 不变、历史 MessageID 保持和 reply_to_id 精确两条无 replay 重复均通过。独立核6个资源 ID、3个目录及 test PID1868688均不存在。该门明确使用 Project/Agent doubles，不外推 Executor/Model、Session或默认 App 的权限链。

同一 reviewer 随后独立核 `cf7d3bf7` 的 Inbox SQL 叶与作者 `53397` 原实际日志：精确 top PASS2.50s/Wait0。四条 Proposal/Decision 投影逐条 Limit1 分页无漏重序，partial-provider 重建不误关其他来源，完整重建保 canonical Version2，迟到旧 requested 不重开。独立核6个资源 ID、3个目录及25个捕获 PID 全部 absent；此门限定真实 PostgreSQL 与 Project/Source doubles，不扩大为 Meeting producer/App。

**分别接受上述两个原定向 SQL 补集，未见范围内剩余已证 must-fix。**没有增加或重跑已接受的 producer 与浏览器矩阵。integration 仅归位 reviewer 结论，未另跑该门。
