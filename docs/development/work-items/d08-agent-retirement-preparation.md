# Project 原停止轮次授权的 Agent 退役准备快照

状态：限定实现与实际验收已通过，受测版本 `0c8532d881ef8688ca9e3b711ea5e387690f10c7`，已随最终文档 `345a8771` 完整合入并推送 main `e4cf974eacd303c46e763cf9524db8ad61cec1b1`。原工作树 `/workspace/agenteam-agent-retirement-preparation`，分支 `ai/agent-retirement-preparation-20261012`，创建基线 `db80a9c4d0f2863c6246adfea8949229086aa858`。只提供真实 Project Delete 执行 owner 的同步读取能力，为后继合法 retirement phase 提供完整配置前像；不报告 Agent 已退休，不改 canonical、当前引用或历史，不新增迁移、App 默认接线、后台 worker、phase 枚举或完整 LifecycleRegistry。

## 可调用链与所有权

[原 LifecycleStopDriver](../../../internal/central/project/lifecycle_stop_round.go)仅在 `start` 的原 Store 调用实际返回 Committed 后进入固定 step。新增私有 scope 随这次 `invoke` 创建，绑定原 driver/authority 实例、Project、原 cause 以及 [running claim](../../../internal/central/project/lifecycle_stop_claim.go) 的 process/attempt/fence。公开 Service Actor、请求 DTO、可见 claim 行或另一个 driver 都不能自行制造该 scope。

真实调用链是：原 `Run` → 活动 step → 新 Agent reader → Project 同步 loan → [原 initializedAgentState](../../../internal/central/agent/current.go) 完整配置读取。step 的组合代码在创建 driver 后、运行前绑定同一真实 reader；产品无 mutable setter 或成功 fallback。结果由调用者持有作后继按版本制定 retirement plan 的输入，快照本身不是授权或持久退休进度。本片不编造尚未实现的后续 mutation 消费者。

新增 Project contract 请求 `AgentRetirementPreparationRequest` 固定 `Actor`、`ProjectID`、`Cause LifecycleCause`、`AgentID`、`ExpectedConfigVersion`。请求只描述目标，不是证明。API 固定为：

```go
project.NewAgentRetirementPreparation(driver *LifecycleStopDriver) (*AgentRetirementPreparation, error)
owner.WithAgentRetirementPreparationInTx(ctx context.Context, tx foundation.Tx,
    request contract.AgentRetirementPreparationRequest, fn func(context.Context) error) error
agent.NewRetirementPreparation(agents *Authority,
    owner *project.AgentRetirementPreparation) (*RetirementPreparation, error)
reader.ReadRetirementPreparationInTx(ctx context.Context, tx foundation.Tx,
    request projectcontract.AgentRetirementPreparationRequest) (AgentConfig, error)
```

Project owner只承认这个 driver 原活动 scope；Agent reader必须用具体 Project owner，不接受公开 fake issuer 接口。两域各自通过原 Store.InTx 核真正 live Tx，因此同 DSN 的另一个 Store 不成立；构造阶段不假称已证明两私有 Store 相同。原 `start` Tx 已终局，读取使用 step 调用者拥有的另一个原 live Tx，不混称同一个事务。

## 同 Tx 门与实际返回

1. 纯输入/取消检查后，登记当前 scope 的同步 loan，绑定精确 Tx、request、Agent/version 与独立寿命。只验证 caller 已持有 **Project SH、ProjectSchedule EX、Agent EX**；不补锁、不另开/提交事务。Project 侧同 Tx 重读真实 Project deleting、operation stopping、完整 manifest/cause 和 exact running claim；Archive、其他操作、旧 attempt/fence、未绑定或失效 scope 均拒绝。Project canonical Owner/manifest 的既有一致性校验保留。
2. 不要求全部 participant 已 stopped，也不借用只支持 Skills 的 Cleanup 授权门；当前接口位于 Delete 的原 stop round 中。既有 Project deleting gate已拒新用，本片不另改 Agent lifecycle/version。[Work 当前指派检查口](../../../internal/central/work/contract/agent_assignee_references.go)只是 Human 原事务事实，不能用于这里发行 service 权限。
3. Agent 在这个同步 loan 内用自己的真实 executor 读取已由原 completed Create receipt 发布的完整配置，包括现有 Model/Tool/Mount/Secret 等引用；允许读取已初始化的当前 canonical lifecycle，但必须匹配请求的 config version。不存在/跨 Project/未发布/陈旧版本拒绝，任何错误返回零配置。返回 Clone，无跨域引用写、无 Activity/Audit/事件副作用。
4. loan 回调只有在实际 Agent SQL和方法返回后才释放。回调后的 loan context立即失效，即使原 Tx 仍有效；保存 context、换 Tx/目标或嵌套借用不能延长权限。scope.close只由原 invoke 的退出路径执行：先拒新 loan并取消原 scope，再等已准入 loan 实际返回，最后才允许原 step/Run 退休和 claim checkpoint。不能由 loan 自己调用 close等待自己；不持 scope 锁调用外部 Store/callback，不派脱管清理 goroutine。
5. Stop/截止时间只传播取消，不把未返回 SQL算 Joined。同步 step 正常返回时已释放自身同步 loan；step错误或误留并行 reader时，scope仍持有这些原调用直到真正返回。原 Run/Drain 的实际归属和原 checkpoint 结果保留，不借 scope 关闭冒 participant 完成。

## 唯一生产写域

- `internal/central/project/contract/agent_retirement_preparation.go`：目标形状及明确的读取边界。
- `internal/central/project/agent_retirement_preparation.go`：具体 owner、私有 scope/loan 与原 claim 同 Tx 核验。
- `internal/central/project/lifecycle_stop_round.go`：原 Committed 后活动 scope 接入和实际退出次序。
- `internal/central/agent/contract/retirement_preparation.go`：完整配置读取 port，快照非 grant。
- `internal/central/agent/retirement_preparation.go`：固定两个真实 authority、原配置读取及版本核验。

纯控由 recovery 独立编写；PG 由 model 独立编写 `tests/projectvariable/agent_retirement_preparation_test.go`，复用真实 Agent/Skill/Object 图和真实 bound ProcessGuard 的 `captureProviderProcesses` 适配，不把恒 Busy 的 fixtureProcess 冒实际 guard。作者分域后可并行，限定 gofmt/static；Go/服务/Git 由 root 调度，适用 [团队流程](../agent-team/README.md)，不复制无关历史验收矩阵。

## 必要验证

- **真实正例**：非空、正式发布的 Agent 与原真实 guard；仅 accepted Delete 是明确受控的测试前置，随后由真实 StopDriver 产生 stopping/running claim，其他 participants 仍 required。在活动 step 原 Tx/full locks中读得完整配置及 refs，canonical/commands/current-ref/history 前后不变；不冒生产 BeginDelete 或完整 Registry。
- **issuer/借用拒绝**：缺 scope/伪 Actor、另 driver/Store/Tx、旧 claim、Archive、错误 Agent/版本、缺锁及保存的已失效 loan context拒绝且返回零配置。pure覆盖必要闭集，PG只补实际边界，不复刻全部旧矩阵。
- **原返回**：真实读取在同步 loan callback 内完成后，保留该原 callback 的实际返回屏障；即使 step先返、Stop或截止时间发生，原 Run/Drain仍 pending。释放并亲收这个 callback及原 owner.With方法返回后，scope才退出、原 claim attempt 才可 terminal。该 PG不声称 hold 内核SQL，也不把 reader已返回之后的上层 Store.WithinTx返回尾算 loan未返。全部实际资源退出，失败保留尚未Join的原owner；不把attempt终局扩成Agent退休或participant停止。

Agent active→deleting/version推进及各领域完整 before/after refs退休仍属于后继正式设计；本片不先写这个 mutation，也不将快照或无 Work指派解释为删除许可。


## 实际验收结果

受测版本的 basic 原58407/0d7f8c实际0：9top/41sub全部通过，Project与Agent两包race及两包vet通过，535输入六图和13项外层输入相同。原98657/6f2eff实际0完成1正/7反控制、单PG binary编译、精确list及fresh prepare；2080编译输入仅加原binary成为2081运行输入，29项外层输入相同。见[方法与原件索引](../../../.agent-state/agent-retirement-preparation/README.md#actual-validation)。

首次native原71336/d2c944实际0，supervisor1478997原Wait0/138.852s，driver1478998与Go1480740均原Wait0；1top/3sub实际35.24s（12.42/12.13/10.69s）。[原post](../../../output/ai/retired-20261012/agent-retirement-preparation/native01/post-check.json)确认2081输入/四身份、受测HEAD、7资源14次absence、desc/private/runtime双空及原75s门内TCP双空，未报告race。只证明受控accepted Delete前置后的真实StopDriver→stopping/running claim→原Tx完整配置读取，以及读取后同步loan callback的实际返回阻住Run/Drain和attempt checkpoint；不冒内核SQL持有或外层Store返回尾。配置、引用与历史未退休，其他participant仍required；本结果不授Agent删除，不包含生产BeginDelete、完整LifecycleRegistry或Project创建HTTP/完整production Skills验收，全部既有STOP保持。

原验收材料已迁至main退役输出目录，旧工作树与本地分支已普通清理，远端原空；见[交付与恢复位置](../../../.agent-state/agent-retirement-preparation/README.md#main-delivery-and-retirement)。此处“退役输出”仅指证据目录迁址，不改变本片只读快照、Agent尚未退休的产品边界。
