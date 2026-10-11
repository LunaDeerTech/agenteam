# 已提交后继 RoundInput 的只读投影

状态：独立树 `agenteam-next-round-projection`，起点 `71b13d5c`，运行输入固定 `c30623db`。产品、基础和 PG 方法已获有限静态接受；basic01 的 8 top / 42 sub race、三包 vet，以及 compile01/list/prepare 已实际通过并获独立接受。首次 native01 原工具 10165 **wholeFAIL**：fixture 最前置创建私有目录失败，未到投影业务。上游 68 已随完整历史 main `bf7c882a` 有限交付，不代替本片真实 PG 验收。

依据：[输入确定点](d01-contracts/execution-orchestration.md#技能变更与下一轮输入确定点)、[68 RoundInput](d22-execution-round-input.md)。本片只把已提交的首个后继输入投影为数据；不发 ModelRequest、不调用下一 Model/Runner，不接生产默认或解除 tools HTTP 及其它原 STOP。

`variables_review` 独占 Execution 新 `contract/round_projection.go`、`round_projection.go` 及对应控制、本卡和本树 current；`skills_milestone_review` 独占 AgentLoop 新投影与控制。root 负责 Git、资源排期及集成。

`RoundInputDeterminer.WithCommittedRoundProjection(ctx,E,nextRoundID,consume)` 只接受原身份，沿原 Store、完整锁、`loadRoundInput` / `verifyRoundInputRows` 重核已提交 canonical 输入、原 Snapshot/前驱 Round、seq1–3、可选 Control 和固定 Tool schema。旧受保护参数按原字节短期借出，不能用 canonical 消息反推原空白/键序。当前 Registry、Agent 配置或 Skill 库不会替换历史输入；旧目录不是当前执行授权。

回调仅同步纯计算；原 Store 的实际成功返回前不能发布。回调借用原参数、三条 canonical 消息及可选 Control，结束即清除借用内存；默认格式化/JSON/日志不输出正文。取消与 Stop 仍由原 Determiner 登记，Drain 等原读取与回调实际返回；Unknown/回滚/调用方取消不能当成功。读取不改 68 写口、旧 Snapshot/Round/Transcript 或 pending 状态。

Loop 保留原 system 字节，逐项验证消息角色、CallID、参数和完整 read-file 六字段结果及其绑定；可选 control 与固定最终目录仅用白名单 metadata 作为版本 1 的 user 数据消息。仅含原单 read-file 成功链和固定 ToolSet，不承诺完整多轮模型请求格式或所有工具。

Execution 的 5 个 top 覆盖流式三条原消息的完整 canonical 前缀（含晚行/close 错误）、借用结束清除、Stop 等原 Store 返回、回调成功后 Store 尚未返回或 Unknown、默认表示脱敏。SQL 流与 Store 时序为显式受控输入，不冒完整 RoundInput SQL/PG 验收。Loop 的 3 个 top 覆盖无 Control/有效 Control、原参数与 canonical 消息的区别、完整身份/结果/目录及克隆、原 reader 实际返回前不得发布。

原 basic01 工具 22796 实际退出 0：race Go792599 / 93.361s 精确 8 top / 42 sub，三包 vet Go793827 / 73.886s 实际 0；race/plain go-list 均实际 0，657 项输入在发现、unit、vet 六份前后记录中逐项相同。原件在 `output/ai/next-round-projection/checks/basic01/`。这只验证本片受控基础，不扩大到 Model 调用或真实 SQL 全链。

compile01 原工具 97067 实际 0：Go795737 / 112.194s，精确唯一 `TestNextRoundProjection` 的 list PID797533 / 1.125s 实际 0。1893 编译输入三份前后记录相同，包含 84 个同包 Go 和全部迁移；运行闭包 1894 项仅新增真实 binary `ddf3c15e2a01de1fb12d6401f7476ea08bb1fd06fbe04de79f74d0b442817409`（87,964,260 B）。prepare 工具 25484 实际 0，仅生成 fresh `/tmp/nrp01` 原配方；原件在 `compile01/` 与 `native01/`，不算 PG 业务通过。

native01 原工具 10165 亲见退出 1，supervisor798335 / 76.456s 实际 1；Go799928 与 driver798336 实际 Wait 1。唯一 top/sub 在 0.00s 报 `owned private directory failed`，真实起点是继承 fixture 的 `runner_read_file_adapter_test.go` 中 `os.MkdirTemp(".", ".runner-read-file-")`；固定安全错误未保存原 errno，不能倒填原因。失败早于投影、上游 fixture 业务与本片后验，不记任何 Projection/PG 业务通过。七个精确资源双 absence、desc/private/runtime 双空、原自动 HOST_TCP delta 双空及 `inputs_unchanged=True` 均齐，只证明失败轮完整退出，不能升级 whole。原监督日志 `/tmp/nrp01/pg-ca39cb5c2c934124b58f00abc25176f7.log` 与 `native01/{terminal,outer-observation}.json`、`outer.log` 完整保留。

原 PG 作者已提供仅 helper 失败分支的有界安全诊断候选，尚未重编或运行；新增诊断不能补造原01 errno。当前未归因具体 errno、未改产品、未重试。真实联调须沿原 68 已提交输入调用 `NewRoundProjector(d).Project(ctx,E,next)`，不以自造 DTO 冒充 committed 输入。后继编译或 native 均由 root 另授窗口，原预算与所有退出门、tools HTTP 等 STOP 不变。
