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
