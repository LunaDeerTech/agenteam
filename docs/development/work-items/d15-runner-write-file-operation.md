# D15/D18/D22 单 write-file Operation

状态：2026-10-12 基础检查及真实 PG/认证 TLS/WSS 的 1 top / 2 sub 首链已获独立验收，完整历史已合入 main `fff1f829`；包含该历史的 main `6a27bc3d` 已实际推送成功，原件保全及工作树、本地和远端分支清理完成。初始基线 main `105ff95f`，技术组合 `c1530e95` 已包含已交 Projection/WSS；原独立树为 `/workspace/agenteam-runner-write-file-operation`，后继从 main 与保全目录恢复。本片已闭合显式 Central 当前授权、Operation/Attempt 与原单次 Runner 写入，不代表生产默认装配。

正式依据：[Model/Tool](d01-contracts/model-tool.md)、[Runner execution-runtime §6](../../architecture/runner/execution-runtime.md#6-write-file)、[原 read-file Operation](d18-runner-read-file-operation.md)及[已交 write-file WSS](d15-runner-write-file-adapter.md)。本片只把持久 ToolCall 接到一次原 write-file 操作及原结果持久化，不消费 Transcript，不启动后继 Model。

## 原事务与发送边界

- 独立 `WriteFileExecutionAuthority` 借出真实原保护参数；same Store、live Tx、完整有序锁与当前 Execution/Agent/Tool witness 必须在同步 callback 内重验。Tool 配置、公开 DTO、Snapshot 及目录信息均不是 grant，普通写不增加 Admin 审批。
- 私有 input 只持久保存原 Call/Spec/payload reference 与摘要、可信 Mount/Workspace/Runner 及版本。实际写字节仍由 Execution 原参数拥有，Tool 仅持有自己复制的有界 typed intent；默认 JSON/日志脱敏。
- 已知 created 提交实际返回后才 Resolve 当前 Mount；Prepare 只固定 RID。Attempt 将此 RID 写入原 Tx，只有真实 Committed 且实际返回才打开 running admission。Claim 必须携同一 RID并再次通过当前授权，网络写位于数据库事务之外；read/write 票据、binding、authority 均为独立闭集。
- 原 Dispatch 真正等待原 frame/terminal 或连接退出后，才在同一最终 Tx 写 Operation/Attempt 终态、retired 与唯一严格结果。成功结果保留 `state=committed`、原 bytes_written、durable 和 completion_code，不把成功发布扩大成 durable 或无 completion error。
- 原响应失联或不合法终态保持 unknown，无 readback 推成功。数据库 Unknown 保原 owner/身份/候选，仅由 `LookupWriteFile` 观察原持久记录，不换 RID、不重发、缺行不推 rollback；仅精确最终候选与原调用实际返回共同成立才释放最终 Unknown owner。created/running 未解决的 Unknown 继续持有，不提供会重新写最终事务的 Resolve 入口。
- 新迁移69只增独立 `runner.write-file` 及 `write_file_results`，旧 install/read 编码和上限不变。模型原参数独立上限1MiB/decoded512KiB，终态 metadata/result 分别≤32KiB。原 schema engine 可验证1MiB实例；仍使用真实 Registry历史schema及同Tx权限，不绕过输入/输出校验。

## 写域与验证

`recovery_lead` 负责 Tool runtime 的独立 write_file authority/executor/attempt/lookup/profile/repository/schema；`skills_milestone_review` 负责 Tool contract/Registry/capture/迁移69；`variables_review` 负责 Execution/Loop；`skill_package_review` 负责 Mount/Central Runner；`knowledge_delete_fixture` 负责真实 PG、方法与最终三文档。唯一 principal 为 runtime_recovery，root 负责所有 Git 及资源窗口。

基础检查覆盖新增闭集、实际 Message codec、publication/durable/completion 保存、参数借用/副本/清理、RID/阶段拒绝、Unknown 与 Stop/实际返回。`basic01` 因测试的未使用 import 编译失败，整轮 FAIL 原样保留；其中六包 34 top / 109 sub PASS。窄修后 `basic02` 仅补 runtime 10 top / 36 sub 及原八包 vet，全部 PASS；分阶段合计 44 top / 145 sub 和八包 vet，两轮各自 829 输入前后同。profile 1 正例 / 7 反例与旧 AST/有效映射保留通过。

`compile01` 原 race/integration-c 与精确 list 均实际 0，1,928 编译输入前后同，runtime 1,929 仅增加实际 binary。`native01` 的 `TestWriteFileOperation/committed-attempt-write-and-lookup` 验证真实 Attempt 提交但 hold 原返回时零 wire，原返回后同持久 RID 唯一 write-file WSS，512 KiB 独立完整读取，最终 DB COMMIT 后丢回执仅原 Lookup 恢复、无重发；`revoke-before-claim-no-dispatch` 验证 Attempt 已返回、Claim 前正式 Owner 撤销 Mount 后零 wire、原 bytes 不变及 `not_dispatched` 退休。原工具 34580 / supervisor 857048 实际退出 0，Go / driver 原 Wait 0；七资源双 absence、全部 private/runtime/descendants 尾、HOST_TCP 自动差额双空和输入不变通过。方法与完整原件定位见[恢复说明](../../../.agent-state/runner-write-file-operation/README.md)。

本链使用受控 Model JSONCaller 和原 Project 持久 test Skills receipt；真实 Owner/Agent/Registry/Preparation/Execution、PG 事务、认证 WSS、Handler/Manager/Root 与独立读取已执行。它只完成一次 write Operation 与结果持久化：没有后继 Model、Transcript 消费、终态 Execution 退休或生产默认接线。生产 initializer/ready、完整 registry/default executor、真实 tools HTTP、OpenAI 独立 A/B 及原 STOP 均保持，未将测试后验 readback 用作产品成功推断。

完整 output 的 95 个文件、97,809,643 B 已同文件系统无覆盖保全至 main 工作树的 `output/ai/retired-20261012/runner-write-file-operation/`，四份 tmp 原件副本及逐项证明在旁目录 `runner-write-file-operation-preservation/`，原 tmp 保留。正式 main 历史补证已核 `79d88efc`、`c1530e95` 和最终任务提交 `98083527` 均为 `fff1f829` 祖先，全部 1,931 个历史 blob 与原输入 map 相同，原 `basic01` FAIL 保留。root 推送原 1815 实际 0（`0d33eb`）后，普通移除树/本地分支实际 0（`bfcaa3`）；fresh 核定原远端完整 tip 后，11874 exact-lease 删除实际 0，16888 fresh 空值与 prune/tracking 复查实际 0。完整恢复映射、工具来源及可见性限制见上述恢复说明；外部保护工具链、热 cache 和只读 modules 不随清理变化。
