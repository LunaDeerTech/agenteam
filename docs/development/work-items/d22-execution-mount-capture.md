# D22 非空 Mount preparing 捕获

本片把已交付的 Agent `runner_mounts` 配置接入真实 `PreparationDriver`，结果是同一捕获事务中的不可变 preparation input 与 Mount 自有 material/ref。它不提交 Snapshot、running 或 Started，不创建物理目录，不开放 Runner operation、Control executor 或 agentloop 非空 Mount profile。

正式依据：[执行编排](d01-contracts/execution-orchestration.md#snapshot执行与取消)、[Workspace §9、§14](../../architecture/runner/agent-workspace.md#9-execution-context-projection)、[Agent Runner 配置](d10-agent-runner-mounts.md)。既有生产 initializer、ready、registry、Cleanup 及独立 tools 等 STOP 保持。本次是 preparing 前置能力，不是完整 MountContext 或 Agent 执行闭环。

## 数据与调用合同

- 原 `ExecutionMountCaptureRequest`、Execution 私有 discovery/capture witness、同 Store 活 Tx 和原完整锁计划仍是唯一入口。公开 P/A/E ID、Human 配置权限、Runner 在线状态均不能替代该 witness。
- `ExecutionMountSet` 保留 request、agent_version、mounts；新增 `attempt_binding` 仅在非空时存在，须等于原 preparation input 的完整 request/attempt/process/fence/Project 绑定。空集合仍编码原三个字段且零 Mount material 写入。
- 非空集合最多 128 项，按 MountID 严格升序；当前 Agent allowed IDs、Mount head/version、双向引用、同 P/A active definitions、稳定 WorkspaceID 映射必须完整相等。不得去重掩盖坏行、漏项返回部分集合或为 legacy 缺映射生成身份。
- 每项固定 MountID、WorkspaceID、RunnerID、Mount version、逻辑 name/description/workspace，以及 Runner name/description/config version。Runner 元数据由本域 `ExecutionMetadataProvider` 同 Tx 返回，不能跨域查私表或调用 Human/Admin reader；offline/incompatible 不构成捕获拒绝条件。此处不固定连接、hello/capabilities、凭据或宿主路径。
- 发现先在原 Project/Agent/Execution 门内读取 Mount 集合并结束 Tx，再以 global/per-Runner SH 与原门组成完整 union，一次按序取得并重读全部事实。最终捕获沿同一 union 重验；发生集合或版本漂移即失败重计划，不在后序锁内补锁。
- 迁移 `00061_execution_mount_capture.sql` 追加 Mount 自有不可变 capture 与逐项 reference。ExecutionID 唯一；P/A/E、attempt、Agent version、完整材料字节及摘要、每项稳定映射相互绑定。material/ref 与原 input 使用同一捕获 Tx；重复观察不分配身份、不改原材料。
- 历史 input 的正常重读、claim 竞争重读、提交 Unknown 观察必须同步核验原 material/ref 完整集合。由 Execution 原 Tx 的短期私有 witness 授权 `VerifyExecutionMountMaterialInTx`，仅读历史材料，不重抓当前 Agent/Mount/Runner。缺 provider、缺历史引用或坏材料均失败；不能仅因 input 存在就宣告恢复。
- 本片没有引用退休 API。配置移除、Stop、Cancel、超时、Unknown 均不删除不可变引用；外层真实 rollback 自然回滚本次写入。后继执行启用须另补与真实终态/实际 join 一致的 retirement 合同，不能用本片 metadata 当运行授权。

## 唯一写域与依赖

Mount 子执行者拥有 `internal/central/mount/contract/execution_capture.go`、`mount/execution_capture.go`、新 capture repository、相应测试及完整迁移 00061。Runner 子执行者拥有 `runner/contract/execution_capture.go`、`runner/service/execution_capture.go` 和两份对应测试。执行负责人拥有 `execution/preparation.go`、`preparation_mounts.go`、两个 preparation repository 的核验接线、`execution/contract/preparation_input.go`、专属定向测试、`app/execution_runtime.go` 唯一 provider 注入，以及 `tests/projectvariable/execution_mount_capture_test.go`。

旧迁移、Agent 配置写入、HTTP、web、Runner Control、agentloop 和其它执行 profile 不在写域。现有 Model/Environment/Skill/Tool、正式 Runner 管理与 Agent 命令、Claim/Launch/PreparationDriver 是首链提供方；Project fixture 的 test-only Skills 初始化边界沿原测试，不冒生产 initializer。

## 有界验收与当前状态

固定候选 `d944062b` 的 preparing 前置能力已分段验证。第一轮 `unit01` 实际退出 1，原件保留：新测试的强类型零值不能编译、锁键含函数而不能用 `reflect.DeepEqual` 比较、已取消 context 遮蔽 witness 失效判断，以及 Unknown 观察错误的预期过强。返修仅三个测试文件，未改生产代码。`unit02` 只补受影响三个包的 5 top / 19 sub，race 实际退出 0；复用 `unit01` 已通过且未受改动影响的包和子项，不把两轮写成单次全绿。两轮各自 1,679 项输入前后相等。控制覆盖旧空集合逐字编码、非空 strict codec/完整 ID 与 attempt、同 Store/held-lock/私有 witness、Runner 安全结果、陈旧 plan、历史 material 缺失与恢复。

缺 material 的 Unknown 观察返回 `DEPENDENCY_UNAVAILABLE`，保留同一原 run/claim/unresolved，未 join、未重捕；恢复原 material 后才实际收敛。该控制沿既有 `ResolveUnknown` 语义，不把读失败判成未提交或恢复成功。App `app01` 首次编译实际退出 0，明确 `[no tests to run]`，不代表默认 App 行为验证。

`compile01` race-c 与 `list01` 实际退出 0，1,746 项编译输入前后相等；运行闭包 1,747 项仅增加实际 binary（SHA-256 `fa55ed6c1df64e27cdb4c1431b0fc57c7fa7bcd765024e09d164af7aaa4e6b60`），含同包全部 72 个 Go 文件。`native01` 仅运行 `TestExecutionMountCapture` 的两个子例，真实业务 28.04s 通过：

- `nonempty-unknown-and-historical-material`：正式 Runner 管理、普通 Owner 配置、真实 Claim/Launch、完整锁并集和同 Store 的 preparing input/material/ref 原子提交。Unknown 是原事务实际 `Committed` 后测试边界丢弃回执的受控装饰，不是 PG 物理提交结果未知或网络故障；原身份恢复后，真实清空当前配置，重读仍只验证历史材料，原字节与捕获次数不变。
- `final-rollback-keeps-no-partial-capture`：末段 hook 在同 Tx 看见完整事实后返回错误，观察原事务实际 `NotCommitted`；独立 raw 查询确认既有 11 项捕获事实及 Mount material/ref 全零。

原 Go PID 498578 与 driver PID 497043 均实际 Wait 0；七个自有资源共 14 次 absence、desc/private/runtime 双尾、原 75s 自动 baseline TCP 差额双空和输入不变门齐。supervisor PID 497022 实际退出 0 / 114.343s，原外层工具 67719 的实际退出 0 由作者亲收；`native01` whole PASS。没有重试或改变原预算、门与清理方法，当前无本片后台、服务或 cache writer。

原件位于本树 `output/ai/execution-mount-capture/`：`checks/unit01-*` 和 `unit01.log` 保留原 FAIL；`checks/unit02-*`、`app01-*`、`compile01-*`、`list01-*` 保存各命令、输入和真实终态；`compile01/inputs-{before,after}.json` 与 `native01/{invocation,input-hashes,plan,recorder-identity,process,terminal,tool-terminal}.json`、`outer.log` 绑定本次真实链。原监督日志为 `/tmp/emc01/pg-883aaa419447424494fcdda919125f37.log`。

这是领域服务与自有 PG/MinIO 的 preparing 验收；Project fixture 仍使用持久 test Skills receipt。不宣称 HTTP、默认 App 运行、物理目录、Runner 执行、非空 Snapshot/agentloop、引用退休或完整 D22 已完成。既有生产 initializer/ready/registry/Cleanup 等 STOP 保留，后继不得凭本片 material 假退休或解禁执行器。
