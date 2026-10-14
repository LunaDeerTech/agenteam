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
