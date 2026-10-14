# M2 工具审批与原 Operation 继续

本片实现 [W3](../development-plan.md#当前五个工作包) 的真实一次性工具审批。正式含义沿用[治理](../../architecture/security-governance/README.md#5-approval-与用户审批)、[默认策略](../../architecture/security-governance/default-approval-policy.md)、[一次性批准与幂等](../../architecture/security-governance/one-time-approval-retry-idempotency.md)、[Execution 生命周期](../../architecture/agent-executor/execution-lifecycle.md)及[D01 命令/Tx](d01-contracts/foundation.md)。本页只固定本次实现接缝，不宣布完整 D19、Human Inbox、Reusable Approval 或 Approval Model 已完成。

## 事实与边界

- Governance 持久维护 `approval_requests`（pending/approved/rejected）、一次性批准和命令结果；请求与批准/拒绝分别在原事务内写入统一 Outbox requested/resolved 事件。前端读取同一对象，不复制决议状态机。
- Tool 先通过原 Execution 当前权限、原始已提交 arguments、Registry schema 和当前 Mount 预留 created operation。预留不创建 Attempt、不发送 Runner；原 operation ID、Tool、输入和目标指纹不可变。后续 Execute 复用这条 created operation，final Resolve/Claim 再校验当前权限与批准。
- Execution 负责持久 waiting/reference/slot、取消、恢复和拒绝结果注入；Governance 不调用新 Execute，不创建 follow-up Execution。取消或终态与审批通过同一 Execution 锁串行，迟到新决议不得恢复执行。
- 拒绝在同事务将原 Tool operation 终结为 failed/approval_rejected，真实 Attempt 数为零。Tool denial 约束禁止复活和新增 Attempt；Governance 的同步拒绝结果 loan 只来自该事实，Execution 消费一次并继续原 conversation。
- 常规 read/write-file 按默认规则直接执行；run-command 的 arbitrary_execution 风险进入审批。Allow 跳过请求；Auto 无可用审批模型的可靠决议时按正式规则转人工。批准不扩大 Capability、Execution policy、scope 或 Mount。

## 接口与装配

`governance/approval/contract.Gate` 提供 Ensure、Resolve、RequireApprovedInTx。Ensure 一定返回真实请求 ID 与 pending/approved/rejected，不以空 ID 表示许可。Resolve 仅用于 canonical waiting、尚未 dispatch 的原操作：pending/approved 同 Tx 要求 Tool 的 created/version1/零 Attempt 证明，rejected 要求零 Attempt denial；已批准转 running 后的实际结果观察仍走原 Tool Lookup。`ExecutionAuthority` 的 ApprovalLocks 和 RequireApprovalExecutionInTx 由原 ToolCallDriver 提供：空 reference 为 running 创建，非空为 waiting/相同 reference 决策；均重验取消与当前 scope。`RejectedResults` 提供原计划和同 Tx 同步 ToolResultPart loan；不伪造 Attempt。

`tool/contract.ApprovalOperations` 的 Reserve/Require/RequireUnattempted/Reject/RequireRejectedOperationInTx 由原 command executor/统一 dispatcher 提供。App 必须在 catalog seal 前注册 Approval typed events，在统一 Outbox 注册 Governance producer，并使用同 Store、原 Execution/Tool 实例构造 Service。Project 只证明当前项目/Owner 门，Governance producer 证明私有 applied witness 和原事务 postimage。

## HTTP 与并发恢复

正式字段见 [approval.json](../../../api/openapi/approval.json)。当前 UI 由 Execution waiting_reference 进入 GET approval，不新增历史列表。

- `GET/HEAD /api/v1/projects/{project_id}/approvals/{approval_id}` 返回当前审批；pending version=1，resolved version=2。
- `POST .../decision` 使用独立 `Idempotency-Key`，JSON `{decision,expected_version}`。同项目 approval.decide 命令身份、稳定 Human user、目标请求、决议和 expected_version 纳入 canonical-v1 摘要；session/request trace 不纳入。
- `POST .../decision/lookup` 保留原 key 与 body，返回 committed+原 receipt 或 not_observed+null。后者不证明所有旧调用已终止；网络断开/Commit Unknown 不自动替换请求。当前 GET 不能代替原命令 lookup。
- 当前 Session/Owner/read scope 先于命令重放；同 key 不同语义拒绝；原成功同语义回放不再检查旧 expected_version，也不再次恢复/执行。不同 key 对已 resolved 请求返回 VERSION_CONFLICT，即使决议相同，不冒认本次命令成功。
- 新决议在同 Tx、完整有序锁集合中重验 Owner mutate、当前 Execution waiting/reference/取消、原 Tool operation 与指纹，再 CAS pending→终局。并发批准/拒绝只提交一个；无过期或自动放行。

迁移 00094 与前向配对修复00099属本片；00092/00093为前置并行片，Execution waiting/result-consumption迁移由controls独写，root整合全局顺序。

## 验证边界

实现与测试进度保存于本工作树 current；有独立路径的可运行测试源码随实现保存。领域编译、单元、受控 HTTP 或 Tool reservation slice 不等于当前 Owner、完整审批等待、重启/取消恢复组合通过；最终验收须观察同 Execution/operation、零拒绝副作用、批准最多一次、当前撤权、并发决议、Unknown lookup 和原进程退出。

### 当前已取得的限定证据

- 原实现片 `bff9b99a` 的 Governance/HTTP/Tool runtime 定向测试通过。真实 `TestApprovalOperationReservation` 使用完整迁移 00092/00093/00094、原 Owner/Execution/Tool/Mount/Registry 与实际 Runner：重复预留保持同 operation/fingerprint，等待前零 Attempt/零 RPC；后续 Execute 复用原操作且仅一次实际 RPC。新增冷等待门在 created/零 Attempt 时通过，在原 Attempt 已持久但 RPC 尚未发送时拒绝。官方 fixture 原 session96270 exit0，测试 PASS 4.30s，子进程 actual_wait code0，所属六资源和三目录已精确确认退出。该测试沿原显式 Tool profile，不能证明完整 Governance 审批授权。
- 真实 App/Owner/Scheduler/PG/Runner 的 approve/reject 双场景已获[独立接受](m2-independent-review.md#真实-approval-双场景与退休适配差量)：原 session90958 Wait0，保持同 request/E/Operation/fingerprint，批准仅一次 Attempt/report/文件副作用，拒绝零 Attempt/report/副作用且原 E 的合法后继提交。同 key receipt/lookup、旧版本/改变意图拒绝、决定/receipt/Outbox同事务及原 Runtime/App/Runner Join和资源退出已核；00099合法发布、00098拒绝后继及分表断言门已关闭。受控 Provider 只证明该真实领域组合。
- 真实 PG 的最小非法配对反例已获[限定接受](m2-independent-review.md#运行中持久取消与非法审批配对限定接受)：原 session22112 Wait0，错 approval ID 的候选结果在 deferred guard 提交时返回 `23514 / invalid loop approval result pair` 并完整回滚，随后原 Owner 合法拒绝仍可完成；结合上述合法后继，关闭原 P1 的最小正反配对门，不泛称全部非法字段组合。
- waiting 中持久取消及该取消终态的新进程重启不复活另获[限定接受](m2-independent-review.md#waiting-取消与重启限定接受)，原混合轮仍有失败。后续 waiting 冷恢复批准继续已获[独立接受](m2-independent-review.md)：旧 child 实际 kill/Wait、新 ProcessGuard 与原 Runner 新认证连接后，批准原 request，保持同 E/Operation/fingerprint、一次 Attempt/report/文件副作用及原 pending 字节，原后继、退休与新进程 Join 完成。pending Launch 的真实响应丢失/Unknown Found 也已限定接受，不代替 Approval 决议响应丢失、NotFound/cold Unknown、完整并发决议或当前撤权组合；旧失败保留，M2约定范围已获[最终独立接受](m2-independent-review.md)，该结论不扩大本片的审批验证范围。

### 真实 App 断点与前向修复

修复前 review 组合 `cae53ce0` 的第二次真实 App 审批链（原 session91225，`output/ai/m2-app-integration/run.oDBoMy/`）未通过：首 Model sent/succeeded、原 Tool created/version1/零 Attempt，Approval request 表为零。原 owned PG 的有界日志确认唯一错误是 `operator does not exist: agenteam_outbox.safe_id = agenteam_governance.safe_id`，只提取了类型和运算符，未输出语句或参数。

Outbox ID 域基于 UUID，Governance ID 域基于 text；00094 `check_publication()` 的 requested/resolved 两条匹配中，event ID、aggregate ID、project ID 共六处直接比较导致 deferred COMMIT 失败，审批请求回滚。预留 operation 是此前独立已提交事实，仍为 created；不能据此宣称已进入审批 waiting。此前通过的 reservation slice 没有触发该审批 publication 约束。

用户恢复 M2 后，新增前向迁移 [00099](../../../db/migrations/00099_governance_outbox_id_pairing.sql)，只替换原 publication 函数：六处 Governance ID 显式转成 UUID，保持 Outbox 列及其索引类型、原事件身份/版本/项目、批准 grant 和拒绝零 Attempt 约束。原00094不改写；无数据回填或表重写，原 deferred trigger 绑定继续有效。依正式数据库规则仅提供事务型 Up，禁止 Down。

静态核对确认，撤销六处 cast 及 CREATE OR REPLACE 后函数体与原体完全一致。完整迁移 source 与含00095–00099的真实 approve/reject COMMIT已在后继 review 组合验证，00099合法发布的实际接受范围见上节。App `m2CheckApprovalTerminal` 同类 JOIN已由m2_delivery修为 `ev.id=r.resolved_event_id::uuid`。原 session91225 的 cancelled/retained COMMIT_UNKNOWN、超时退出及失败清理仍保留历史结论，后继通过不回填该轮正常 Join或整轮成功。
