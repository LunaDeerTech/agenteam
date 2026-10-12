# 已提交后继 RoundInput 的只读投影

状态：本片在组合源 `272c74fa` 上完成有限验收，唯一主审 `runtime_recovery` 已接受：basic01 的 8 top / 42 sub race、三包 vet；compile03/list/prepare；native03 的真实 1 top / 1 sub 与全部退出、TCP 和输入门。原 native01、native02 wholeFAIL 完整保留：前者原 errno 未知，后者记录 ENOSPC，均未到投影业务；第三轮成功不解释旧失败原因。上游 68 已随完整历史 main `bf7c882a` 有限交付。

依据：[输入确定点](d01-contracts/execution-orchestration.md#技能变更与下一轮输入确定点)、[68 RoundInput](d22-execution-round-input.md)。本片只把已提交的首个后继输入投影为数据；不发 ModelRequest、不调用下一 Model/Runner，不接生产默认或解除 tools HTTP 及其它原 STOP。

`variables_review` 独占 Execution 新 `contract/round_projection.go`、`round_projection.go` 及对应控制、本卡和本树 current；`skills_milestone_review` 独占 AgentLoop 新投影与控制。root 负责 Git、资源排期及集成。

`RoundInputDeterminer.WithCommittedRoundProjection(ctx,E,nextRoundID,consume)` 只接受原身份，沿原 Store、完整锁、`loadRoundInput` / `verifyRoundInputRows` 重核已提交 canonical 输入、原 Snapshot/前驱 Round、seq1–3、可选 Control 和固定 Tool schema。旧受保护参数按原字节短期借出，不能用 canonical 消息反推原空白/键序。当前 Registry、Agent 配置或 Skill 库不会替换历史输入；旧目录不是当前执行授权。

回调仅同步纯计算；原 Store 的实际成功返回前不能发布。回调借用原参数、三条 canonical 消息及可选 Control，结束即清除借用内存；默认格式化/JSON/日志不输出正文。取消与 Stop 仍由原 Determiner 登记，Drain 等原读取与回调实际返回；Unknown/回滚/调用方取消不能当成功。读取不改 68 写口、旧 Snapshot/Round/Transcript 或 pending 状态。

Loop 保留原 system 字节，逐项验证消息角色、CallID、参数和完整 read-file 六字段结果及其绑定；可选 control 与固定最终目录仅用白名单 metadata 作为版本 1 的 user 数据消息。仅含原单 read-file 成功链和固定 ToolSet，不承诺完整多轮模型请求格式或所有工具。

Execution 的 5 个 top 覆盖流式三条原消息的完整 canonical 前缀（含晚行/close 错误）、借用结束清除、Stop 等原 Store 返回、回调成功后 Store 尚未返回或 Unknown、默认表示脱敏。SQL 流与 Store 时序为显式受控输入，不冒完整 RoundInput SQL/PG 验收。Loop 的 3 个 top 覆盖无 Control/有效 Control、原参数与 canonical 消息的区别、完整身份/结果/目录及克隆、原 reader 实际返回前不得发布。

原 basic01 工具 22796 实际退出 0：race Go792599 / 93.361s 精确 8 top / 42 sub，三包 vet Go793827 / 73.886s 实际 0；race/plain go-list 均实际 0，657 项输入在发现、unit、vet 六份前后记录中逐项相同。原件在 `output/ai/next-round-projection/checks/basic01/`。这只验证本片受控基础，不扩大到 Model 调用或真实 SQL 全链。

compile01 原工具 97067 实际 0：Go795737 / 112.194s，精确唯一 `TestNextRoundProjection` 的 list PID797533 / 1.125s 实际 0。1893 编译输入三份前后记录相同，包含 84 个同包 Go 和全部迁移；运行闭包 1894 项仅新增真实 binary `ddf3c15e2a01de1fb12d6401f7476ea08bb1fd06fbe04de79f74d0b442817409`（87,964,260 B）。prepare 工具 25484 实际 0，仅生成 fresh `/tmp/nrp01` 原配方；原件在 `compile01/` 与 `native01/`，不算 PG 业务通过。

native01 原工具 10165 亲见退出 1，supervisor798335 / 76.456s 实际 1；Go799928 与 driver798336 实际 Wait 1。唯一 top/sub 在 0.00s 报 `owned private directory failed`，真实起点是继承 fixture 的 `runner_read_file_adapter_test.go` 中 `os.MkdirTemp(".", ".runner-read-file-")`；固定安全错误未保存原 errno，不能倒填原因。失败早于投影、上游 fixture 业务与本片后验，不记任何 Projection/PG 业务通过。七个精确资源双 absence、desc/private/runtime 双空、原自动 HOST_TCP delta 双空及 `inputs_unchanged=True` 均齐，只证明失败轮完整退出，不能升级 whole。原监督日志 `/tmp/nrp01/pg-ca39cb5c2c934124b58f00abc25176f7.log` 与 `native01/{terminal,outer-observation}.json`、`outer.log` 完整保留。

失败诊断 `77805930` 获静态接受后单独执行 compile02/list：原工具 31194 实际 0，Go803424 / 25.678s 与 list803817 / 1.060s 均实际 0；1893 输入三份前后相同，对01仅原 helper 诊断一文件改变。prepare 原81467 实际0，运行1894项仅增 binary `2cc5aad07c3ef00a44456d923fbf71967fc1e77299ef990ad7db33414653492c`（87,968,196 B）；未重跑已接受基础。

native02 原73449亲见退出1，supervisor804805 / 78.008s 实际1（原监督日志77.063s），Go806502/driver804825 Wait1。唯一 top/sub 仍在0.00s原 `MkdirTemp` 失败，新增闭集诊断 `errno=space` 精确对应 `syscall.ENOSPC`；当时 parent stat/Getwd无错误，parent为0700目录且owner_known/owned/linked为真。七资源双 absence、desc/private/runtime双空、75s内原自动 HOST_TCP delta双空与1894输入不变全部齐。原件在 `native02/`，raw log `/tmp/nrp02/pg-c442cb0e32194dd9b836b211d41ea603.log`。没有到达投影或任何本片业务后验，不记PG通过；02诊断不能补造01首因，也未证明ENOSPC来自字节、inode或其它文件系统限制。

原尾后同一 parent 的一次受控 mkdir/rmdir 均实际0、身份匹配且清理后不存在，外层46cd46实际0，记录在 main 的 [projection-parent-mkdir01.json](/workspace/agenteam/output/ai/projection-parent-mkdir01.json)。这只能说明该次当前态；无损容量处理与随后第三轮成功均不能补造01原errno，也不能解释02 ENOSPC来自字节、inode或其它限制。失败点 Statfs 诊断 `e3ed` 已纳入第三次编译；第三轮未进入该失败分支，故没有新的失败现场 Statfs 记录。


组合 `272c74fa` 包含正式 main `b6f158c3` 的已验 Runner WSS 增量与上述诊断。compile03 原66456亲收0：Go824578 / 91.883s、唯一 `TestNextRoundProjection` 的 list826190 / 1.077s 均实际0；1901输入三份相同，对02恰8新增、2变化、无删除，变化只来自已验WSS与原helper诊断。prepare原1cbe27实际0，运行1902项仅新增实物 binary `b961923cf557d7a2dfaa8cf5fcbaa48f7aee7d76250547f16c63606e4de88ec8`（87,980,429 B）。原件在 `compile03/` 和 `native03/preparation-observation.json`，未重跑已接受基础。

native03 原72578由执行者亲收 actual0（chunk `aa6095`）：supervisor827584 / 113.320s，原监督日志112.293s；Go829170、driver827585均原Wait0。`TestNextRoundProjection/committed-input-projection` 实际23.50s通过：正式Determine提交后，由真实reader投影原system、完整三消息/64KiB六字段结果、Control与固定目录；原scope/projection事务各实际返回并无write XID，重复投影和借用/访问器变异不改持久事实，正式撤去当前Skill后历史投影仍逐字相同，新增Model/WSS为0。七资源14次absence、desc/private/runtime双空、75s内原自动HOST_TCP差额双空及1902输入不变全部齐。原日志 `/tmp/nrp03/pg-f8d01279a05c414b92550c3afaea7741.log` 与 `native03/{terminal,outer-observation}.json`、`outer.log` 保留；主审已有限whole接受，不需追加运行。

本片交付止于已提交首个后继RoundInput的只读投影，不发下一ModelRequest、不授新Tool/Runner执行、不接默认生产；tools HTTP及其它历史STOP仍保持。证据位置、方法与恢复范围见[本片说明](../../../.agent-state/next-round-projection/README.md)。


原件已同FS无覆盖迁至 main `output/ai/retired-20261012/next-round-projection/`：106文件 /271,750,879 B及25目录，路径/SHA/size/dev/inode/mode/nsmtime逐项相同。本文前述output路径均为原运行位置，现按相同相对路径访问该保留根；原tmp三轮各4文件另存至旁目录 `next-round-projection-preservation/nrp01–03/`，原tmp不动。23份map的1,905仓库历史版本已匹配原三个检查点，完整main历史可达仍由root合入后核；外部依赖保持保护，可见性限制及证明映射见[恢复说明](../../../.agent-state/next-round-projection/README.md)。树/分支尚未清理，不增加全局删除计数。
