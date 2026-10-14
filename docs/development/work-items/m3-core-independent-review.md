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
