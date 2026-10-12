# D15/D18/D22 单 write-file Operation

状态：2026-10-12 源码实施，尚未编译或真实运行。基线 main `105ff95f`，独立树 `/tmp/agenteam-runner-write-file-operation`。已交 Runner 本地 Write 与 trusted TLS/WSS 只提供下游能力，不代本片 Central 当前授权、Operation/Attempt 或默认装配。

正式依据：[Model/Tool](d01-contracts/model-tool.md)、[Runner execution-runtime §6](../../architecture/runner/execution-runtime.md#6-write-file)、[原 read-file Operation](d18-runner-read-file-operation.md)及[已交 write-file WSS](d15-runner-write-file-adapter.md)。本片只把持久 ToolCall 接到一次原 write-file 操作及原结果持久化，不消费 Transcript，不启动后继 Model。

## 原事务与发送边界

- 独立 `WriteFileExecutionAuthority` 借出真实原保护参数；same Store、live Tx、完整有序锁与当前 Execution/Agent/Tool witness 必须在同步 callback 内重验。Tool 配置、公开 DTO、Snapshot 及目录信息均不是 grant，普通写不增加 Admin 审批。
- 私有 input 只持久保存原 Call/Spec/payload reference 与摘要、可信 Mount/Workspace/Runner 及版本。实际写字节仍由 Execution 原参数拥有，Tool 仅持有自己复制的有界 typed intent；默认 JSON/日志脱敏。
- 已知 created 提交实际返回后才 Resolve 当前 Mount；Prepare 只固定 RID。Attempt 将此 RID 写入原 Tx，只有真实 Committed 且实际返回才打开 running admission。Claim 必须携同一 RID并再次通过当前授权，网络写位于数据库事务之外；read/write 票据、binding、authority 均为独立闭集。
- 原 Dispatch 真正等待原 frame/terminal 或连接退出后，才在同一最终 Tx 写 Operation/Attempt 终态、retired 与唯一严格结果。成功结果保留 `state=committed`、原 bytes_written、durable 和 completion_code，不把成功发布扩大成 durable 或无 completion error。
- 原响应失联或不合法终态保持 unknown，无 readback 推成功。数据库 Unknown 保原 owner/身份/候选，仅由 `LookupWriteFile` 观察原持久记录，不换 RID、不重发、缺行不推 rollback；仅精确最终候选与原调用实际返回共同成立才释放最终 Unknown owner。created/running 未解决的 Unknown 继续持有，不提供会重新写最终事务的 Resolve 入口。
- 新迁移69只增独立 `runner.write-file` 及 `write_file_results`，旧 install/read 编码和上限不变。模型原参数独立上限1MiB/decoded512KiB，终态 metadata/result 分别≤32KiB。原 schema engine 可验证1MiB实例；仍使用真实 Registry历史schema及同Tx权限，不绕过输入/输出校验。

## 写域与验证

`recovery_lead` 负责 Tool runtime 的独立 write_file authority/executor/attempt/lookup/profile/repository/schema、新必要控制与本卡/current；`skills_milestone_review` 负责 Tool contract/Registry/capture/迁移69；`variables_review` 负责 Execution/Loop；`skill_package_review` 负责 Mount/Central Runner。唯一 principal 为 runtime_recovery，root负责所有 Git及资源窗口。

先验证新增闭集、实际Message codec、publication/durable/completion保存、原参数借用/副本/清理、RID/阶段拒绝、Unknown与Stop/实际返回，再由独立方法作者接真实PG+认证WSS+Manager首链。当前仅源码/gofmt，未运行Go、PG、服务或网络；既有通过证据不冒充本片通过。生产 initializer/ready、完整registry/default executor、真实tools HTTP、OpenAI独立A/B及原STOP均保持。
