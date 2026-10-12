# D22 RoundModelCall 预留与提交回执恢复

状态：2026-10-12，设计准备；基于已交付的共同主线，尚未实现或运行本卡。当前迁移最大号为 72，主线程为本卡独占预留前向迁移 **73**；此处只规定约束，不落 DDL。Model 后继纯契约与 Agent 本地停止已交付，不代表实际后继 Model 调用可用。

依据：[持久下一请求](d22-next-round-model-request.md)、[纯后继请求与响应契约](d22-model-successor-contract.md)、[Model 输入与重试所有权](d01-contracts/model-tool.md#model-解析与调用)、[恢复事实与持久边界](d01-contracts/execution-orchestration.md#恢复事实与持久边界)。实施遵循[团队流程](../agent-team/README.md)；已接受的同输入证据复用，不重做完整 D22。

## 目标与边界

从原已提交 `(ExecutionID, RoundInputID)` 和 72 的稳定 CallID，建立唯一的 **reservation 身份事实**，只完成 Claim、Lookup 与原事务提交回执恢复。RoundInputID 仍是 68 `round_inputs.id`；不是 56 的第二个 Round，也不是 Model Invocation。返回回执不提供 Invoke 或凭据读取权限。

本片新增 Model Begin/Result/Close、CredentialRead、Provider/HTTP、Runner、Invocation/Usage 写入均为零；不新增 response ledger、Transcript seq5、Tool Operation、默认装配或后台恢复 worker。不重建 72 请求，不改变首轮 session、Model Runtime/capture-only revision、旧 Snapshot/lease、56 唯一 Round、68 输入与现有 tools HTTP STOP。实际调用仍须另行解决可执行 profile、当前 Invoke/CredentialRead、唯一原 handle 及真实 Finalize 边界。

## 接口与共享前置

拟落 `RoundModelCallOwner.Claim(ctx,E,next)`、`Lookup(ctx,E,next)` 和 `Stop/Drain/Joined`；Lookup 返回原回执及 found，入口不收 caller request、CallID、ProcessID、lease、committed 布尔或可替换的 Model Caller。`NewRoundModelCallOwner(store, authority, projects, processGuard)` 核 Execution authority 与 Store 相同，固定真实 Project gate、具体 guard 及从其当前身份取得的 expectedProcess；拒绝缺 provider、外来 authority 或无效 guard，但构造成功不宣称已证 guard 同 Store。

回执是私有构造的 owned 值，显式字段仅含 ClaimID、P/A/E、RoundInputID、原 CallID、原 reservation ProcessID、SnapshotID、原请求/输入/来源摘要及创建时间；默认格式化/JSON/日志脱敏。回执与数据库行均不是本地 owner 能力；本地调用登记保留自己的私有实例/候选身份，不能由回执、反序列化或同一个 ProcessID 重建。

**尚需统一补充的 Object 前置：** [现有 ProcessGuard](../../../internal/central/object/process.go) 的 `CurrentProcess()` 只检查 bound/仍持有状态，没有公开 Store 绑定证明。主线程已选择单一只读 `RequireCurrentProcessInTx(ctx context.Context, tx foundation.Tx, expected objectcontract.ProcessID) error`：在原 guard 短锁下核 bound/未关闭、真实 Service 与 expected 身份，再由该 Service 原 Store 的 `InTx(tx)` 验证 caller 原 live Tx 归属，首次写之前必须通过；不在 guard 锁内执行外部 Store 回调，不暴露 Store、不开新 Tx、不靠接口可比较性、不查 foreign death或新增 pin。此共享口尚未实现，由唯一 Object writer 提供后两任务共同消费，不另造构造式 API。

真实 composition 必须从构造、Claim 到全部借用实际返回始终持有原 ProcessGuard。`CurrentProcess` 或上述新口的成功只是当次检查，既不延长 guard 生命周期，也不证明进程死亡；PID、时间、查询失败、取消信号和 LocalJoined 都不能替代真实持有或死亡证明。本片不调用 `ConfirmStopped`、不释放他方 guard、不实现 foreign takeover。

## 同事务历史与唯一预留

1. Claim/Lookup 先登记原 `(E,next)` 本地调用；从 Execution canonical 发现实际 P/A，一次规范取得既有 `roundInputLocks` 的 **Project SH / Schedule EX / Agent EX / Execution EX**，在同 Store/live Tx 中核完整锁集，不能事后补锁或凭 caller 列表缩减。
2. 读取新 reservation 与原 72，复用或窄提取 [requestInTx/loadRoundModelRequest](../../../internal/central/execution/round_model_request_repository.go) 的同 Tx 历史验证：真实 68 输入、Snapshot、`loadCommittedRoundProjection`、原完整 Actor/Consumer/model/credential-ref/lease 与纯请求重建逐字相同。旧 canonical bytea 与全部 digest 是事实来源；不另嵌公开 Prepare/Lookup/Projection Tx，不读取外域私表，不以当前 Registry/Tool/Mount/Skill 配置替换历史。
3. 首次不存在 reservation 才检查 Running、无 canonical cancel、原 Project 可写和原 guard 仍有效，然后插入本次固定 ClaimID/原 CallID 候选。历史观察先于新写门；随后撤权或取消不能抹去已提交 reservation。Invoke 时的当前 Agent/Model 权限另属后继，不由本片历史 loan 暗授。
4. 同一本地 owner 的重复 Claim 只复用自己的已确认原 reservation；本地 Unknown 阻止再次 Claim。已有但不属该本地实例的 reservation 只允许 Lookup 观察，不能因同 ProcessID、同 key 或新实例初始化取得原本地能力；唯一约束与原 Execution gate 收敛并发。

拟新增 `agenteam_execution.round_model_claims`：ClaimID 主键、唯一 `(execution_id,round_input_id)`、唯一原 CallID；保存上述安全关联和原 reservation ProcessID，不复制 72 大正文。FK 与 deferred 完整配对约束绑定同一条 72、68、Execution/Snapshot，逐项核原 CallID、P/A/E、来源/请求/输入摘要与创建顺序；行不可改删，原 56/66/68/72、Execution version、Transcript、lease/ref 均不因本片预留而改写。迁移仅 Up，沿真实 EmbeddedSource 格式，不引入 Down 标记；不得解析消息内层结果 JSON 或损坏合法 NUL 的原字符串编码。

**未来承接：** 行内 ProcessID 永久说明“谁提交了这次 reservation”，不是永久可执行 owner。未来实际调用必须用独立、可演进的执行 lease/fence/attempt 事实引用 ClaimID 与稳定 CallID，并关联真实 Model Invocation；不得靠改写原 reservation 或新建 logical CallID 接管。原唯一键只约束一次逻辑预留，不阻止将来按正式死亡、当前准入和 fence 规则建立新的执行归属。这里的执行 lease/fence 不等于 72 已保留的 Credential lease；本片不实现任何接管或调用阶段，也不制造已具备恢复能力的占位记录。

## 原返回、Unknown 与退出

- 回调成功、插入可见或 context 取消均不等于原事务返回。只有原 Store **实际返回 Committed**、原候选完整吻合且调用仍可发布时才返回成功回执；已知 Committed 后遇到本地取消，保留已知提交事实供 Lookup，不能改写成 NotCommitted。
- 原 Store **实际 NotCommitted** 可以确认该尝试未提交；明确 rollback 零部分。Unknown 则保存原 ClaimID、原 CallID、完整候选与原提交错误，只按相同 `(E,next)` Lookup 比较；不重新 Claim、不换 key/CallID、不自动重发。**Lookup 缺行单独不证明 NotCommitted**，查询失败或未观察到都保持原 Unknown。
- 新实例 Lookup 可以观察原提交事实，不能重建旧进程 handle、继承本地 owner 或擅自恢复执行。历史读取不要求原 reservation 进程仍存活；当前查询实例自身资源/Store/guard 仍须有效。
- Stop 禁止新 Claim 并取消在途调用；精确原 Unknown 的观察通道仍保留。Drain 受调用方 context 约束，必须等原 Store、历史 loan、回调实际返回，借用数据清除后才能退役本地登记；不得 detached 确认、丢 pending 或把 cancel 当 join。[原请求 owner](../../../internal/central/execution/round_model_request_owner.go) 的真实返回模式可复用，不能依轮询缺行消除责任。
- `Joined` 只描述本实例已停止、原调用实际退出且原 Unknown 已解决；未解决 Unknown 由 Drain 如实报告并保持未 joined。它不证明 Model 已结束、foreign process 已死或未来可接管，也不能把 Unknown 变成业务 known。

## 文件分域与最小验收

| 唯一所有者 | 拟文件域 | 本片责任 |
| --- | --- | --- |
| Execution owner/存储 | `internal/central/execution/contract/round_model_call.go`、`execution/round_model_call{,_owner,_repository}.go` 及专控 | Claim/Lookup、私有实例、完整同 Tx 历史、原返回与退出；按需窄提取既有 72 私有接缝 |
| 数据 owner（同 Execution 交付） | `db/migrations/00073_execution_round_model_claims.sql` | 仅新 reservation 及原来源完整约束，不改旧表含义 |
| 共享 Object owner（主线程另行单派） | ProcessGuard 窄只读 Store 绑定口及必要专控 | Agent 与本片共同前置；无死亡算法/运行时 pin 扩展 |
| 独立 PG/方法 | 新 `tests/projectvariable/round_model_call_reservation_test.go` 与必要专属方法 | 复用真实 Command→68→72，验证 reservation；不替换真实 authority/process/Store |

1. **构造与拒绝边界：** 必要纯控覆盖错 Store/guard/Tx、缺完整锁、改变来源或候选、首次 current gate 拒绝；foreign 实例不能从历史回执获得本地 Claim 能力，默认表示不泄漏原请求。
2. **真实唯一历史：** 计划 `TestRoundModelCallReservation` 的 `committed-request-reservation-and-history` sub，复用实际 Command 消费→Determine→72 Prepare，真实绑定 guard 后 Claim；同实例重复与新实例 Lookup 得原 ClaimID/CallID，独立 SQL 完整核行与 72/68 来源，并核旧 56/66/68/72、Transcript/E version 没有变化。
3. **真实提交恢复：** 同 top 的 `reservation-rollback-and-commit-loss-lookup` sub，在原同一 Store 末 Tx 实际 rollback 证明零部分；另一次物理 COMMIT 后遮回执，固定原候选只由原/新 Lookup 恢复，零重复 Claim。原返回 barrier 前不发布回执，不把回调或新查询可见当原返回。
4. **真实返回与资源归属：** 必要受控 Store barrier 覆盖 Stop/取消与原返回两序、短 Drain ctx、Unknown 尚未确认；原调用/loan/guard 的实际生命周期闭合，foreign 查询缺行不被用作死亡或未提交证明，不为测这些边界重建完整旧矩阵。
5. **零调用与兼容：** PG 前置 Model 受控端口/真实 Runner 的实际次数单独记录，不声称整条 setup 零调用；reservation 阶段 Begin/Credential/Provider/Runner/Invocation/Usage 新增均零。原 NUL、双流、schema3 完整请求字节继续保留，原 HTTP STOP、Runtime/default 及响应写口不变。

设计检查仅核相关链接、格式与实际接口边界；产品实现、迁移 apply、基础 race/vet 和真实 PG 必须等主线程分派与运行窗，当前没有本卡动态通过结论。
