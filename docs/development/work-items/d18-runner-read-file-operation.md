# D18/D22 单 read-file Operation

本片把真实 Execution ToolCall 接到一次 Runner read-file Operation/Attempt，并持久保存可重读的完整结果。当前仅独立存储、codec 与消费接口源码已落；尚未编译、运行测试或接通真实 producer，不是已交付执行链。基线为 main `1b10c29c`，后继依赖 ToolFacts 正式交付的 00063/00064 与真实保护参数 producer，不复制其在制实现。

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

Tool lead `skill_package_review` 负责新 contract、private runtime codec/repository、迁移65及本卡/current。Runner 作者仅负责原 dispatcher 两文件，已由 root 保存为 `6f87bfff`，只完成静态审查与格式化，新增控制尚未运行。Execution owner `skills_milestone_review` 已完成接口设计，待 ToolFacts 正式集成才实现；`runtime_recovery` 为独立 principal，root 负责 Git 与资源窗口。

当前没有公开 Tool read-file producer、Attempt owner/终态写入入口或默认装配。另一个明确后继接缝是：现 Mount ticket 只公开 RunnerID，真实目标的 Mount/Workspace/name/版本需由原 resolver 给安全只读投影，不能由模型参数或调用者自报；本次未改 Mount。完整结果引用还需由真实 owner 写入原 terminal metadata 并用于历史重读。这些均须在首链前接通，不把 private repository 函数当运行授权。

已有三组 codec 控制源码覆盖闭集输入、指纹/原记录绑定及 utf8/base64 两类完整 65536B 结果和错误引用；尚未执行。后续先做必要编译与兼容控制，再在原窗口验证最小真实 PG + WSS 链、事务实际返回前/Unknown 零发送、同 RID 和结果重读。Model 可复用已明确端口替身，Execution/Tool authority、CurrentMount、ticket、WSS 与 Runner Manager 必须真实。默认生产 nil、全 registry、OpenAI tools A/B 和其他原 STOP 不变。
