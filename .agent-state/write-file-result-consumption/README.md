# Write receipt consumption

当前仅源码与静态检查，尚未执行本片方法控制、Go、compile/list 或真实 PG。唯一入口为 `TestWriteFileResultConsumption`，固定两个 sub：

- `revoked-dispatch-grants-consume-and-project`：真实 512KiB Write 已持久完成、独立分块全读并停止旧 dispatch owner 后，普通 Owner 正式清除 Tool 和 Mount 授权；首次消费历史 receipt，原 66 seq3 与 Execution version 同 Tx 成功，再真实 68 Determine 和只读 Projection。
- `commit-loss-lookup-no-resend`：真实消费事务完整写入后物理 COMMIT，仅原 Store 返回被投影为 Unknown；正式撤权后原 consumer Lookup 观察原候选，再新 consumer Lookup 恢复同一 receipt，不再 Consume、Execute 或 Model。

## 真实来源和范围

唯一新 PG 源为 `tests/projectvariable/write_file_result_consumption_test.go`。它直接复用已验 `newOperationWriteFileFixture` 的普通 Owner、Agent、Registry、Launch、Preparation、唯一受控 Model JSONCaller、真实 WSS/Runner/Workspace 和原资源退出；不改旧 fixture，不 raw SQL seed、不伪造权限、receipt 或 Runner terminal。原 JSONCaller 是模型边界的唯一替身；Project test Skills receipt 的原边界保持。

实际 Write receipt 先由原生产链形成，512KiB 文件经独立 Handle.Read 分块完整校验。停止并 Drain 旧 `WriteFileAuthority` 后保留实际 ToolCallDriver；新的 `CommittedWriteFileCallAuthority` 只同步借用已提交原 call/args，`NewWriteFileResultReader` 无后台、无独立 Stop/Drain、无旧 dispatch ProcessGuard 或活动 handoff 依赖。新 consumer 与 determiner 各自实际 Stop/Drain/Joined，最后原 ToolCallDriver/Model、Runner、Manager、连接和服务器按旧顺序退出。

首次消费仍检查 Execution running/noCancel 和 Project 可写，Determine 保持原 slot、active Agent 与 Skill 当前门。Tool/Mount 当前撤权只影响再次派发，不能删改已提交 receipt 或阻止本片历史消费。已有 receipt Lookup 及已确定输入的 Projection 不续授执行权限，也不重新查询派发授权。

消费后的独立 SQL 核原 Call/Op/Attempt/RID、protected input 与各 digest、唯一 consumption/seq3、同 Tx 时间和 Execution CAS version；不复用旧 ToolFacts 的 seq2/version 固定断言。原 Snapshot、Model Round、ToolCall、arguments 和 seq1-2 全字节保持。Determine 只增一个 RoundInput 和一次版本变化，不新增 Model Round、ControlInput 或 Skill mutation；Projection 两次结果相同，原 reader 的每个实际事务返回及无写 XID 均观察，持久事实、wire 1 和原 Model 1 不变。

原 seq2 assistant ToolCall 的完整 canonical args（包括 Write data）和前缀摘要保留。新增 ToolResult 正文只含原 canonical metadata receipt：`path`、`state`、`bytes_written`、`durable` 与原可选 `completion_code`；日志和默认格式化不输出文件内容。本 PG 只记录真实健康写入得到的 durable=true / nil completion，不向数据库造 durable=false 或 late-code 报告。其他合法报告形态由生产类型纯控覆盖，known committed publication 始终保持 Tool success / IsError=false。

## 原方法复用

[profile-controls.py](profile-controls.py) 绑定新树真实 base `4ebde5a78a43f734d76e0c512fb0ec0c61170b9c`。两原 PG harness 只增加本 top/sub metadata；剥去唯一新 key 后旧 AST 与有效映射须完全等于该 base。原 1 正 / 7 反控制实际判定函数，不能将静态字符串检查记作动态通过。

[run-basic-checks.py](run-basic-checks.py) 的冻结闭集为 **12 新 + 5 个受影响旧控 = 17 top / 3 个 test 包 / 5 个 vet 包**。新增来自 Tool runtime 3、Execution 4、Loop 5；旧控仅选共享 consumer 的原持久、Unknown 和实际 Stop 返回，以及原 Read 投影顺序和借用寿命，精确名称在 `NEW_TESTS` / `COMPAT_TESTS`。vet 在三个实际包外补 Tool/contract 与 Execution/contract；不重跑原 Write/WSS/Command 全矩阵。原 race-test/vet 实际依赖发现、全 migration/embed、offline/max1、Popen/Wait 及前后输入门保持，固定 `--scope full` 不开放任意筛选。

[run-pg-checks.py](run-pg-checks.py) 保留原完整 driver 输入生产器：同包全部 Go、internal、完整 migration/embed、module、工具链和必要方法。**本片没有新 migration**；compile 仍强制实际 `00070_runner_command_operations.sql` 存在且全量迁移均在闭包，元数据另绑定真实 66/68 输入。原 `-race -tags=integration -c -p=1` 仅编译一次，然后精确 list；只有成功且输入相同才生成 prepare。runtime key 集只增加该次真实 binary。

原 [record-native.py](record-native.py) 字节保持，SHA-256 为 `56a32b0d45a633c3c85306d098730318327c02f4ee9c5267c8d712e9880d4f20`。native 仍由 `pg_only_supervisor.py --root-chain` 启动原 driver，保持七资源、540+60 s、6m test、原 Popen/实际 Wait、desc/private/runtime 双尾、75 s 自动 HOST_TCP 基线差额双空和所有输入门。prepare 不启动服务、不授权 native，不替换 driver、不改过滤、不自动重试。

首次动态检查必须等全部产品/方法源冻结和 root 独占窗口。下列为尚未运行的固定入口；每段保存原 invocation/PID/Wait 与前后实际输入，失败立即停止条件链：

```sh
python3 -B .agent-state/write-file-result-consumption/profile-controls.py
python3 -B .agent-state/write-file-result-consumption/run-basic-checks.py --output output/ai/write-file-result-consumption/basic01
python3 -B .agent-state/write-file-result-consumption/run-pg-checks.py compile --output output/ai/write-file-result-consumption/compile01
python3 -B .agent-state/write-file-result-consumption/run-pg-checks.py prepare --compile output/ai/write-file-result-consumption/compile01 --output output/ai/write-file-result-consumption/native01 --namespace /tmp/wfrc01
```

执行根只使用 `/workspace/agenteam-write-file-result-consumption`，不引用已退役活动树的绝对 binary/output 路径。热 GOCACHE 与只读 GOMODCACHE 沿原方法固定位置保护，不复制或更换锁。源码/静态通过不计真实 PG，通过范围也不包括下一 Model、再次 Tool 执行、默认绑定、终态退休或 tools HTTP 解禁。全部 Git 操作由 root 及其明确指定的 Git 执行者完成。
