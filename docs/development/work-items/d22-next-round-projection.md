# 已提交后继 RoundInput 的只读投影

状态：独立树 `agenteam-next-round-projection`，基线 `71b13d5c`。合同首片 `31e8a34f` 已保存；Execution reader、Loop 投影和必要控制已落，尚未编译或运行。上游 68 的真实 PG 验收尚未交付，本片不据其静态合同宣称组合通过。

依据：[输入确定点](d01-contracts/execution-orchestration.md#技能变更与下一轮输入确定点)、[68 RoundInput](d22-execution-round-input.md)。本片只把已提交的首个后继输入投影为数据；不发 ModelRequest、不调用下一 Model/Runner，不接生产默认或解除 tools HTTP 及其它原 STOP。

`variables_review` 独占 Execution 新 `contract/round_projection.go`、`round_projection.go` 及对应控制、本卡和本树 current；`skills_milestone_review` 独占 AgentLoop 新投影与控制。root 负责 Git、资源排期及集成。

`RoundInputDeterminer.WithCommittedRoundProjection(ctx,E,nextRoundID,consume)` 只接受原身份，沿原 Store、完整锁、`loadRoundInput` / `verifyRoundInputRows` 重核已提交 canonical 输入、原 Snapshot/前驱 Round、seq1–3、可选 Control 和固定 Tool schema。旧受保护参数按原字节短期借出，不能用 canonical 消息反推原空白/键序。当前 Registry、Agent 配置或 Skill 库不会替换历史输入；旧目录不是当前执行授权。

回调仅同步纯计算；原 Store 的实际成功返回前不能发布。回调借用原参数、三条 canonical 消息及可选 Control，结束即清除借用内存；默认格式化/JSON/日志不输出正文。取消与 Stop 仍由原 Determiner 登记，Drain 等原读取与回调实际返回；Unknown/回滚/调用方取消不能当成功。读取不改 68 写口、旧 Snapshot/Round/Transcript 或 pending 状态。

Loop 保留原 system 字节，逐项验证消息角色、CallID、参数和完整 read-file 六字段结果及其绑定；可选 control 与固定最终目录仅用白名单 metadata 作为版本 1 的 user 数据消息。仅含原单 read-file 成功链和固定 ToolSet，不承诺完整多轮模型请求格式或所有工具。

Execution 已写 5 个 top：流式三条原消息的完整 canonical 前缀（含晚行/close 错误）、借用结束清除、Stop 等原 Store 返回、回调成功后 Store 尚未返回或 Unknown、默认表示脱敏。SQL 流与 Store 时序为显式受控输入，不冒完整 RoundInput SQL/PG 验收。Loop 的 3 个 top 覆盖无 Control/有效 Control、原参数与 canonical 消息的区别、完整身份/结果/目录及克隆、原 reader 实际返回前不得发布。以上仅源码数量，尚无实际通过结果。

真实联调需先有上游 68 真实提交，并用 `NewRoundProjector(d).Project(ctx,E,next)` 读取该提交；不以自造 DTO 冒充 committed 输入。仅新增本片控制和相应包 vet 由 root 单独排期，不重跑旧基础矩阵。当前不运行 Go、服务或网络。
