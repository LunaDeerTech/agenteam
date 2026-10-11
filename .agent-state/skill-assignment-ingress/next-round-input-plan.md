# DetermineRoundInput：首个后继输入草案

状态：仅合同与实施建议，未实现、未运行；本文件不授权开工。组合基线是已交付 main `7c874adc` 的 65/66 与待验收的 67。依据：[执行输入确定点](../../docs/development/work-items/d01-contracts/execution-orchestration.md#技能变更与下一轮输入确定点)、[Skill 分配与读取](../../docs/development/work-items/d01-contracts/resources-skills.md#分配目录与读取)、[67 范围](../../docs/development/work-items/d10-skill-assignment-ingress.md)。migration **00068 为建议号，须 root 确认全局未占用后分配单写者**。

**交付边界**：在原 66 ToolResult 已提交的 running Execution 上，确定并提交一次后继输入后停止。输入含原 Snapshot、完整已提交 Transcript 前缀、固定 Skill 目录与真实 cutoff；不启动 Model、不追加第二 ToolCall、不 resume waiting、不发布终态、不接生产默认。新增的是正式 RoundInput 持久事实，不另建执行器、队列、Model Round 或通用工作流层。

## 可分派合同

以下是拟议的窄接口；现有 `InitialRoundBindingsReader`、`AssignmentChangeAuthority`、`DispatchObservation.AgentSlotInTx` 和 Agent `ExecutionConfiguration` 继续复用。新 Skill 读口属于 Skill contract/实现；其参数只定位事实，不授运行权限。

```text
SkillAssignmentValidation（同原 Store/Tx，SQL-only）
  ReadAssignmentSequenceInTx(ctx, tx, project_id, agent_id) -> sequence
  RequirePendingAssignmentInTx(ctx, tx, execution_id, RuntimeAssignmentChange) -> error
  ValidateAssignmentInTx(ctx, tx, project_id, agent_id,
                        assignment_id, skill_id, bound_revision:{revision_id,revision})
                        -> valid|removed|disabled|deleted

Execution-owned RoundInputDeterminer（仅显式内部装配，不暴露 HTTP）
  DiscoverRoundInput(ctx, execution_id, next_round_id) -> private Plan{RequiredLocks()}
  DetermineRoundInputInTx(ctx, tx, execution_id, next_round_id, Plan) -> RoundInput
  Determine(ctx, execution_id, next_round_id) -> committed RoundInput | error
  Lookup(ctx, execution_id, next_round_id) -> {found, original RoundInput}

RoundInput = {round_id, execution_id, snapshot_id, transcript_through,
              compaction_ref:null, skill_input_binding_id, applied_change_ids[],
              assignment_cutoff, digest}
StoredInput = RoundInput + {predecessor_round_id, source_consumption_identity,
              initial_sequence, sorted_skill_bindings[], transcript_prefix_digest,
              checkpoint_version, schema_version, canonical_bytes}
```

`InTx` 返回仅是事务候选；`Determine` 必须等原 callback 实际返回且提交结果已知 Committed 才发布输入。借用现有同步 call/Stop/Drain 所有权规则，不设后台 worker；Lookup 校原完整 canonical bytes/digest，不返回“等价重算”结果。

`ReadAssignmentSequenceInTx` 读取完整 canonical current 集合/真实 live grants，兼容初始化 seq=1，不能返回默认空集合。`RequirePendingAssignmentInTx` 核原 change、Execution/assignment、固定 revision/package/Object 映射及既有保护，缺失即失败；不补建引用。`ValidateAssignmentInTx` 验当前 grant identity、同项目、enabled、资源 active 和 revision 归属，不要求 revision 为最新版，也不要求预先存在下一轮 binding。removed/disabled/deleted 是明确业务结果；身份串域、损坏或缺少保护仍报错，不一概当 skipped。

**私有证明与锁**：Plan 绑定构造实例、原 request、实际 predecessor/66 receipt、Snapshot/capture digest、current sequence、pending 身份集及完整锁集，公开 DTO 不能替代。base union 为 Project SH、ProjectSchedule EX、Agent EX、Execution EX，加初始 reader 所需锁及所有候选 Skill SH；各 owner 若有附加锁必须 discovery 前置合并。final 一次 AcquireAll/RequireHeldLocks，不嵌套 Tx、不晚升级。Execution 在真实 slot、原 capture/66 checkpoint 和当前 Project gate 验证后，签发仅本次调用、同实例/Store/live Tx/完整锁集有效的私有 current witness；经现有 Agent ExecutionConfiguration 读取 active/publication proof，返回即销毁。只在 Execution 既有 current-authority 分派处接此 witness，不开放 Agent 新权限口、不借用 Model call 或返回可逃逸的 advance 闭包。

## 一次提交的选择语义

1. 从自己的原 Snapshot/66 consumption 读取 predecessor，核原 ToolCall、seq=3 完整结果消息和对应 digest；不能凭 caller 的 receipt、Tool 摘要或成功 DTO 判断已消费。新写要求真实 slot 指向该 Execution、running 且未 cancel_requested。created/preparing 尚无该前驱，waiting 不恢复，terminal 不复活。
2. 从原 `InitialRoundBindingsReader` 取 S 和固定目录；从 Skill owner 取当前序列 H，从本域 ingress 取该 Execution 的完整已提交 change 集。**cutoff=H，不能取 max(pending.sequence)，更不能取 Agent config_version**：Remove/disable 推进 Skill sequence，却没有 runtime ingress。final 的 H/候选集若较 discovery 漂移，整次不写并重新 discovery；最终成功事务不能遗漏在它取得 gate 前已提交的有效新增。
3. `sequence<=S` 追加 covered；`S<sequence<=H` 先核原 captured/protected identity，再验当前授权。失效追加 skipped（带 removed/disabled/deleted 原因）；同 Skill 仅最高当前有效 sequence 可 applied。仅移除/禁用/删除而无有效新 grant 时，旧目录及历史保护仍保留，**保留上下文不等于保留读取权限**；后继 ReadSkill/Prepare 仍须当前授权拒绝。有效 B 替换旧 A 的目录位置，使用 B 捕获的 revision/metadata，记录 `replaced_assignment_id=A`；迟到 A、低序或失效候选不能覆盖 B，不跟随库升级换包。
4. 有 applied 时只写一个 `skills_added` ControlInput，payload 列出本次全部新增/替换；没有 applied 则不造空 ControlInput，`transcript_through` 仍为 3。有 ControlInput 时为 4。新目录、dispositions、ControlInput/Transcript、RoundInput、Execution checkpoint version 同 Tx；任何一步失败全部回滚。原 capture、旧 Round/Transcript、67 ingress/pending refs 和固定包保护不更新、不释放。
5. `(execution_id, predecessor_round_id)` 唯一 successor；同 next_round_id 返回原已提交输入，不能按新 cutoff 重算；同前驱换 ID 拒绝，不能靠多传 ID 制造第三轮。提交返回 Unknown 保原 ID/完整候选，只允许 Lookup 原事实，未查到仍 Unknown，不自动重写或发 Model。历史 Lookup 不以随后发生的 Skill 撤权/取消改写输入；它只观察原事实，不授新运行权限。cut 后的 change 保持未处置，留真正下一轮，不在本片伪造新前驱消费它。

## migration 68 最小结构建议

| Execution 自有结构 | 最小不变量 |
| --- | --- |
| `round_inputs` | 新 round ID 主键、独立 input binding ID 唯一；E/P/A、snapshot、predecessor、66 source identity、S/H、目录/canonical bytes/digest、through、checkpoint version；UNIQUE(E,predecessor)。FK 原 Execution/Snapshot/Round/consumption，并以 deferred pairing 核完整原 66 身份、输入目录/处理记录/ControlInput 一致；不可 UPDATE/DELETE。目录存同一 canonical input，不额外建平行可变目录。 |
| `skill_assignment_dispositions` | PRIMARY KEY(E,change_id)，FK 原 ingress(E,change_id) 与本次 round_input；sequence、covered/skipped/applied、原因。由同 Tx 的完整候选检查保证无漏项；不修改 67 行。 |
| `control_inputs` | 仅本片 `kind=skills_added`；ID、E、next round ID、source_id=next round ID、schema、canonical payload/digest、Transcript position；UNIQUE(E,next round,kind)，FK 本次 round_input；仅在 applied 非空时存在，双向 deferred 配对，拒绝孤儿/遗漏/重复。 |

保留旧 `rounds.execution_id UNIQUE` 与 `loadDirectText` 的原单轮语义。`transcript_entries` 最小扩展：旧 seq=1/2/3 继续原非空 `round_id` 及原 FK；新增 seq=4、kind=control，增加 nullable `round_input_id` FK 新输入，允许此分支旧 `round_id=NULL`，CHECK 严格保证两种来源互斥。增加 `(E,sequence,round_input_id)` identity 供 ControlInput deferred 配对。control 的 `message` 存 typed ControlInput canonical bytes，并单独校 schema/payload/digest；不伪造成已发送的 Model message，不弱化原三种 role/shape 检查。未来 Model 投影不属于本片。新表及 Transcript 使用既有 immutable 保护；迁移不得写回历史内容或删除旧 66 配对约束。

## 分工、首条真实验收与停止点

- **Execution 作者兼迁移单写者**：`execution/contract/round_input.go`、`execution/round_input*.go`、现有 current witness 分派的窄兼容、00068；负责 full union、前驱身份、checkpoint/Transcript 原子性、原 ID recovery。第一检查点先冻结 wire/表约束，不启动后续 Model。不为本片实现 ReadSkill、Compaction 或 `SkillExecutionBindings.Resolve` 的运行入口。
- **Skill 作者**：`skill/contract/assignment_validation.go`、`skill/assignment_validation.go` 与必要控制；复用 canonical current/capture/revision/protection 读取，只写 Skill 源码，不碰 Execution/Agent 私表。两域合同冻结后源码可并行。
- **联调作者**：`tests/projectvariable/execution_round_input_test.go`。依赖 67 真实 Assign/Remove 装配及其基础验证；复用 66 `newToolResultConsumptionFixture` 的原完整 checkpoint，使用真实 Owner/Agent/已发布 Skill 做 Remove(A)→Assign(B)，旧/新 grant identity 必须实际生成；不 SQL seed pending/receipt、不以空 pending 代替链路。实际 PG 窗由 root 另授。

最小 PG 验收：① capture 已覆盖 change 不重复；capture 后 A 撤销、B 新增产生一个替换目录/ControlInput，旧 capture 保持；② 回滚无半成品、同 ID 重放唯一、Unknown 仅原 Lookup，原 Operation/Attempt/result/Transcript 逐项不变；③ 用真实锁屏障安排 Assign 先提交/Determine 先提交，分别包含或留 pending，当前读取能力失效不能由旧目录恢复。删除/禁用路径缺真实业务 writer 时明确留未验证，不能伪造成功。新增阶段 Model BeginChat/Tool dispatch 次数均为 **0**；整个继承 fixture 仍有原先 **1 次受控首轮 Model**，原 Runner I/O 真实。既有 tools HTTP STOP 不变，受控首轮不是该 STOP 的通过证据。

草案交付后停止；不因本文件创建新树、实现源码、占用测试窗或默认宣告 67/68 通过。
