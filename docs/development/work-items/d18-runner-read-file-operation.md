# D18/D22 单 read-file Operation

本片把真实 Execution ToolCall 接到一次 Runner read-file Operation/Attempt，并持久保存可重读的完整结果。独立基础及真实 producer 首片源码已落，尚未编译或运行本片 Tool 控制，不是已交付执行链。初始基线为 main `1b10c29c`；ToolFacts 先以已提交开发依赖接入，随后通过 main `95c092bc` 正式有限交付，原 native01/02 FAIL 与修后03通过事实均保持，不替代本片验证。

正式依据：[Model/Tool 契约](d01-contracts/model-tool.md)、[Execution 契约](d01-contracts/execution-orchestration.md)、[Runner read-file](d15-runner-read-file-adapter.md)。本片止于原 Operation 的持久结果，不包含下一轮 Model、Transcript 注入或自动重试。

## 兼容存储

- 新迁移 `00065_tool_read_file_operations.sql` 增加闭集 `operation_kind`。旧行为默认 `skill.install`，保留其非空 SkillID、原 backend key、input/terminal 编码、摘要及 32KiB metadata 上限；不修改 00037。新 `runner.read-file` 明确没有 SkillID/backend key，不伪造 install 目标或后端幂等承诺。
- read-file 的独立 private input codec 只保存原 ToolCall 身份、protected input 引用及摘要、实际目标的 Mount/Workspace/Runner 和配置版本；不另存模型原始参数或文件路径。指纹使用固定 operation/revision/protocol 与逻辑目标，当前配置版本、连接、credential、request/attempt 身份不改变逻辑指纹。
- 成功 payload 单独存入 `read_file_results`，绑定原 Operation/Attempt/Execution/Project/Agent，验证原 body SHA、长度、严格协议编码及原读参数。上限沿已有 `MaxReadFileResultBytes=512KiB`，完整合法 65536B 文件结果即使 JSON 转义也不截断。原 terminal/attempt metadata 仍不承载大 body。
- payload 不可修改；延迟约束要求 success、原 succeeded+retired Attempt 与唯一 payload 在同一事务内齐全，其余状态不得出现 payload。约束不证明实际 join，真实 owner 必须在原 call 退出后才写退休。没有新增公开 HTTP 读口、默认 JSON 输出或日志材料。

## 已对齐的真实接缝

1. Tool 消费 `ReadFileExecutionAuthority.DiscoverReadFileCall/WithReadFileCallInTx`。plan 是实际 Execution producer 的私有身份；`Binding()` 仅给安全值副本以发现稳定 Operation，不授予权限。`With` 在 same Store、原 Tx、完整已持锁下重验原 ToolCall/保护参数/current Execution/Agent/取消，私有 witness 在同步回调内有效，借用参数副本在退出时清零。Tool 只可保留已校验 typed intent，不保留借用 raw bytes。
2. 完整锁集包括 Registry SH、Project SH、ProjectSchedule EX、Agent SH、ToolSpec SH、Execution EX，以及 Tool 的 Operation/适用 Command/记录锁，入 Tx 前统一排序。正确调用方向为原 Mount Resolve/Claim Tx → Tool authority → Execution live callback → Mount 传入的原 callback；不能从 Execution callback 再开 Resolve/Claim 嵌套事务。
3. Runner `Prepare(ticket)` 只生成固定 RequestID，无 Claim、队列或后台工作；原 `ReadFileCall.Dispatch` 单消费并使用该同一 RID。Tool 只在自己原 Store 的 Attempt 事务已知提交且实际返回之后调用 Dispatch，数据库 `backend_request_id` 必须等于实际 wire RID。不能用调用方自报 Committed、公开 binding 或 call 存在代替授权。旧 Dispatch、Directory 与原 frame/connection 退休尾保持。
4. Attempt 提交 Unknown 时零发送，保留原 owner/claim，不换 RID、不重新 Prepare、不自动发送。已知提交后 Claim/队列失败还须按实际是否发送区别失败与 Unknown；回包后补写 RID 不满足本片。

## 写域与当前缺口

Tool lead `skill_package_review` 负责新 contract、private runtime、迁移65及本卡/current。Runner `6f87bfff`、Execution callback `4b73635e`、Mount 安全 metadata 投影 `0eeeea0e` 已保存并有限静审，三域19top race与适用vet已实际通过，真实666项依赖前后相同且不含Tool runtime；`runtime_recovery` 为独立 principal，root 负责 Git 与资源窗口。迁移65中曾误用 uuid 局部变量的静态缺陷已在 `86c298c9` 改为实际列 `%TYPE`，不称已 PG 复现。

显式装配顺序为 `NewReadFileAuthority` → 原 Mount resolver → 原 Runner dispatcher → `NewReadFileExecutor`。`ExecuteReadFile(ctx, ExecutionID)` 是唯一执行入口，不接 caller raw/body/target/RID；在原 Execution callback 内经真实 `CurrentTargetInTx` 读取六字段安全目标并同 Tx 写完整 created，实际已知提交返回后才 Resolve。ticket.Target 只作原目标精确比对，不替代 Claim。原 Attempt Tx 中写 prepared call 的同一 RID，实际已知提交并返回之后才 Dispatch。原 frame 与 terminal/connection 实际退出之后，一次有界最终 Tx 保存原终态、小 metadata 引用、完整 payload 及 retired。

`LookupReadFile` 只经当前 Execution 权限读取原 Operation/Attempt/历史 payload，不重新读当前目标替换历史、不发送或创建新 RID。提交 Unknown 保留原 owner、实际返回事件与有界结果候选；只有精确终态观测且原调用已返回才可解除最终提交的不确定性。显式 `ResolveReadFile` 只为这个仍持有的 owner 重查/保存原事实，绝不调用 Dispatcher；Attempt 提交 Unknown 且当时零派发，只能在查见同一原 Attempt 后记已知未发送，缺行不能证明 rollback。Stop 仅取消原调用；Drain/Joined 不把 Cancel、公开结果或未知提交当退休。首次构造均显式，默认生产装配未改。

独审指出的两处静态缺陷已窄修，尚待实际控制：最终候选只由实际 instance schema mismatch 归一化，metadata/锁/取消错误不改原成功；NotCommitted保原候选，Unknown保本轮确证写意图。Lookup独立登记原调用，Stop禁止新查询，Drain等其原Store调用实际返回；Execute内部历史重读沿已登记owner，停止后只允许显式Resolve结算原保留事实。

已有三组 codec、六组 producer 控制源码，覆盖完整65536B结果、原终态/RequestID、borrowed input、实际返回/未知持久状态、候选错误恢复、公开Lookup原Store barrier及默认脱敏，尚未运行。variables_review 的唯一 PG 两子场景已冻结，含真实65536B结果、原事务返回 barrier、Lookup不重放与当前撤权；两旧 fixture 只补正式 seed handoff/透明 wire 观察接口。先做必要基础验证，尽早进入真实 PG + WSS 链，不扩完整矩阵。Model JSON 为唯一明确端口替身，Execution/Tool authority、CurrentMount、ticket、WSS 与 Runner Manager 必须真实。默认生产 nil、全 registry、OpenAI tools A/B 和其他原 STOP 不变。
