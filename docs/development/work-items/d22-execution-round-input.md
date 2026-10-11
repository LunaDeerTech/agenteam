# Execution 首个后继 RoundInput

状态：独立树 `/workspace/agenteam-execution-round-input`，分支 `ai/execution-round-input-20261011`，组合起点 `2151a43f`。本片源码与必要控制在实施，尚未编译、测试或真实 PG 验收。上游 67 源码已冻结，真实 PG 仍在独立窗口验证；不能把 65/66 的交付或 67 基础通过算作本片通过。

依据：[执行编排的输入确定点](d01-contracts/execution-orchestration.md#技能变更与下一轮输入确定点)、[资源与技能](d01-contracts/resources-skills.md#分配目录与读取)。本片只把原 66 已提交结果与 67 durable pending 确定为首个后继输入；不启动 Model、Tool dispatch、waiting resume 或终态退休，不接生产默认，不解除 tools HTTP 及其它既有 STOP。

## 组合与写域

- `skill_assignment_agent`：Execution `contract/round_input.go`、`round_input*.go`、原 current-authority 分派的窄兼容；前向迁移 `00068` 唯一写者；本卡与本树 current。
- `skills_milestone_review`：Skill `contract/assignment_validation.go`、`assignment_validation.go` 与控制。提供 `NewAssignmentValidation(*skill.Authority)`，仅同原 Store/Tx SQL 事实，不创建 runtime/ref，不读其它域私表。
- root：Git 保存/集成、后续测试/native 资源窗与联调作者分配；主审检查完整事务、旧模型兼容与真实验证范围。

`NewRoundInputDeterminer(store, *execution.Authority, RoundInputDependencies)` 显式组合 Project gate、Agent ExecutionConfiguration、历史 InitialRoundBindingsReader 和 Skill RoundAssignmentValidation。它没有 Model/Tool runtime 端口、后台 worker 或全局注册。公开 DTO 不授执行权限。

## 调用与不变量

`DiscoverRoundInput(ctx,E,nextID)` 返回 private Plan；`DetermineRoundInputInTx(ctx,tx,E,nextID,plan)` 只在已持完整 union 的原活事务中构建候选，调用者负责整个事务及提交结果。普通入口 `Determine(ctx,E,nextID)` 持有原调用直到实际返回；只有已知 Committed 才发布 RoundInput。`Lookup(ctx,E,nextID)` 仅观察原已提交内容。Unknown 保原 ID/完整候选，不自动重写；未查到不清除不确定状态。Stop 只取消，Drain 等实际 Store/端口返回，不将取消当 join。

Execution 从自己的真实旧 Snapshot/Round、preparation input、66 consumption 和 seq1–3 Transcript 读原前驱。完整 read-file ToolResult 与原 ToolCall/Operation/Attempt/Request、protected argument digest、result digest/bytes 经既有 canonical reader/projection 重核；不重发 Tool，不读其它域私表，不接受 caller 自报成功。新写要求真实 slot 指向该 Execution、running 且无 cancel_requested。Project initialized/active 与 Agent active/publication 通过真实 owner 检查；短期同 Store/Tx witness 仅允许 current Agent 读取，既不借 Model call，也不把新 Agent 配置复制进旧 Snapshot。

base 锁为 Project SH、ProjectSchedule EX、Agent EX、Execution EX，合并历史 capture reader 及全部候选 Skill SH。discovery 收齐 union，final 一次预持锁并重验原来源、H 与 pending 集；漂移拒绝，不晚升级、不静默截断。私有 plan 绑定实例、原 E/nextID、原前驱/66 receipt、capture/prefix digest、H、完整候选集及锁。历史已提交重放先读原事实，不因之后 Skill 分配变化重算。

Skill 三口分别读取真实 current sequence、验证原 pending change/既有 revision-package-Object 保护、验证 grant 当前状态及固定 revision 归属。**cutoff 是该 owner 的 H**；Remove/disable 无 runtime ingress，故不能用 max(pending.sequence) 或 Agent config_version 替代。sequence≤原 capture S 的 change 追加 covered；S 之后失效者 skipped，当前有效者按 SkillID 取最高 sequence。有效新 B 替换旧 A，记录 `replaced_assignment_id`；目录每 SkillID 一项，metadata 与 revision 固定，不随库更新漂移。

removed/disabled/deleted 不主动抹旧上下文和历史保护；保留目录不授当前读取权限，失效 grant 不能由旧目录恢复。当前片不承诺资源 deleted 后仍能成功 Determine：原历史 capture reader 需要 serving 资源。真实 Remove(A)→Assign(B) 是主验收链；ReadSkill/Prepare 的后续使用、`SkillExecutionBindings.Resolve` 运行入口、Compaction 和进一步 Model rounds 未在本片实现。

## 持久边界

`00068` 新增不可变 `round_inputs`、`skill_assignment_dispositions`、`control_inputs`。目录及初始绑定、66 source、原 Transcript 前缀 digest、applied IDs、cutoff、checkpoint version 存一个 canonical RoundInput，避免另建可变目录。UNIQUE(E,predecessor_round_id) 禁止同前驱换 ID 生成第二 successor；旧 `rounds.execution_id UNIQUE`、through=2、tool_pending、原66所有行保持。

有 applied 才写一个 typed `skills_added` ControlInput（全部新增/替换）及 seq4 Transcript；无 applied 时 through=3，不造空 ControlInput。seq4 通过新 round_input_id 关联下一输入，旧 round_id 为 NULL；旧 seq1–3 保原非空 round_id/外键。互斥形状、deferred 配对、canonical digest、全候选 dispositions 与 unique change 身份保证同事务完整保存。新 guard 先分表再读取 NEW.sequence；原66 guard 的 seq≠3 跳过行为未改。

RoundInput、dispositions、ControlInput/Transcript 和 Execution version CAS 同事务提交；错误全回滚。旧 capture、Round、Transcript、66 consumption、67 ingress/pending refs 均不更新、不清理。cut 后新增仍保 pending；本片不伪造第三轮前驱来消费它。

## 验证边界与下一步

已写必要纯控制：selection/cutoff/replacement、旧目录保留、codec/digest/克隆与重复 change 拒绝、foreign Tx/plan/full锁/expired witness、Unknown 不重发和 Stop 等原 Store 返回。它们只证明控制与选择行为，不替代真实 owner、流式 SQL、DDL、回滚或 PG 数据完整性；目前尚未运行。

真实 PG 在 67 实际验证与组合输入固定后，由 root 授窗。复用 `newToolResultConsumptionFixture` 的原完整已消费 checkpoint，接真实 Owner、Agent/四引用 owner、已发布 Skill 与正式 Remove/Assign。检验 capture covered 不重复、B 替换 A、同 ID 重放、整体回滚、Unknown 原事实查询，以及 Assign/Determine 两个真实锁提交顺序。不得 SQL seed pending/receipt，不可空 pending 替代主链。

Determine 阶段新增 Model BeginChat/Tool dispatch 均应为 0；整个继承 fixture 原有一次受控首轮 Model，原 Runner WSS/I/O 真实。受控首轮不是 tools HTTP 通过证据。真实工具 HTTP、后续 Model 调用、资源删除成功边界、等待恢复、生产 default 与原 STOP 保留。
