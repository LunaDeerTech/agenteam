# Write receipt consumption

native04 已实际 whole PASS：原工具 83628、Go 958337、driver 956720 和 supervisor 956700 全部实际返回 0，1 top / 2 sub 共 107.10s。native01、02、03 的 whole FAIL 及其原输入、日志全部保留；本结果不覆盖失败历史。唯一入口为 `TestWriteFileResultConsumption`，固定两个 sub：

- `revoked-dispatch-grants-consume-and-project`：真实 512KiB Write 已持久完成、独立分块全读并停止旧 dispatch owner 后，普通 Owner 正式清除 Tool 和 Mount 授权；首次消费历史 receipt，原 66 seq3 与 Execution version 同 Tx 成功，再真实 68 Determine 和只读 Projection。
- `commit-loss-lookup-no-resend`：真实消费事务完整写入后物理 COMMIT，仅原 Store 返回被投影为 Unknown；正式撤权后原 consumer Lookup 观察原候选，再新 consumer Lookup 恢复同一 receipt，不再 Consume、Execute 或 Model。

## 真实来源和范围

唯一新 PG 源为 `tests/projectvariable/write_file_result_consumption_test.go`。它直接复用已验 `newOperationWriteFileFixture` 的普通 Owner、Agent、Registry、Launch、Preparation、唯一受控 Model JSONCaller、真实 WSS/Runner/Workspace 和原资源退出；不 raw SQL seed、不伪造权限、receipt 或 Runner terminal。native02 检出 fixture delegate 的运行中替换 race 后，仅给原 `fixture_test.go` 增加 nil-default、原锁保护的回执转换钩子：delegate 固定，原事务实际返回后先观察物理状态，仅精确消费 cause 的 Committed 回执可被测试投影为 Unknown。四个真实端口的透明诊断不改变参数、返回或时间预算。原 JSONCaller 是模型边界的唯一替身；Project test Skills receipt 的原边界保持。

实际 Write receipt 先由原生产链形成，512KiB 文件经独立 Handle.Read 分块完整校验。停止并 Drain 旧 `WriteFileAuthority` 后保留实际 ToolCallDriver；新的 `CommittedWriteFileCallAuthority` 只同步借用已提交原 call/args，`NewWriteFileResultReader` 无后台、无独立 Stop/Drain、无旧 dispatch ProcessGuard 或活动 handoff 依赖。新 consumer 与 determiner 各自实际 Stop/Drain/Joined，最后原 ToolCallDriver/Model、Runner、Manager、连接和服务器按旧顺序退出。

首次消费仍检查 Execution running/noCancel 和 Project 可写，Determine 保持原 slot、active Agent 与 Skill 当前门。Tool/Mount 当前撤权只影响再次派发，不能删改已提交 receipt 或阻止本片历史消费。已有 receipt Lookup 及已确定输入的 Projection 不续授执行权限，也不重新查询派发授权。

消费后的独立 SQL 核原 Call/Op/Attempt/RID、protected input 与各 digest、唯一 consumption/seq3、同 Tx 时间和 Execution CAS version；不复用旧 ToolFacts 的 seq2/version 固定断言。原 Snapshot、Model Round、ToolCall、arguments 和 seq1-2 全字节保持。Determine 只增一个 RoundInput 和一次版本变化，不新增 Model Round、ControlInput 或 Skill mutation；Projection 两次结果相同，原 reader 的每个实际事务返回及无写 XID 均观察，持久事实、wire 1 和原 Model 1 不变。

原 seq2 assistant ToolCall 的完整 canonical args（包括 Write data）和前缀摘要保留。新增 ToolResult 正文只含原 canonical metadata receipt：`path`、`state`、`bytes_written`、`durable` 与原可选 `completion_code`；日志和默认格式化不输出文件内容。本 PG 只记录真实健康写入得到的 durable=true / nil completion，不向数据库造 durable=false 或 late-code 报告。其他合法报告形态由生产类型纯控覆盖，known committed publication 始终保持 Tool success / IsError=false。

## 原方法复用

[profile-controls.py](profile-controls.py) 绑定新树真实 base `4ebde5a78a43f734d76e0c512fb0ec0c61170b9c`。两原 PG harness 只增加本 top/sub metadata；剥去唯一新 key 后旧 AST 与有效映射须完全等于该 base。原 1 正 / 7 反控制实际判定函数，不能将静态字符串检查记作动态通过。

[run-basic-checks.py](run-basic-checks.py) 默认 `--scope full` 的闭集保持 **12 新 + 5 个受影响旧控 = 17 top / 3 个 test 包 / 5 个 vet 包**。新增来自 Tool runtime 3、Execution 4、Loop 5；旧控仅选共享 consumer 的原持久、Unknown 和实际 Stop 返回，以及原 Read 投影顺序和借用寿命，精确名称在 `NEW_TESTS` / `COMPAT_TESTS`。vet 在三个实际包外补 Tool/contract 与 Execution/contract。新增封闭的 `--scope reader-followup` 仅执行 Tool/runtime 的原 3 个 reader top 加新增 deadline-scope top，以及同包 vet；原 full 选择与必选源集合不变。两模式均保留原 race-test/vet 实际依赖发现、完整实际 migration/embed、offline/max1、Popen/Wait、精确选择与前后输入门，不接受任意过滤。原 methods 的 10 项输入已含本脚本；产品和同包控由实际闭包收集，不另改共享 harness。

[run-pg-checks.py](run-pg-checks.py) 保留原完整 driver 输入生产器：同包全部 Go、internal、完整 migration/embed、module、工具链和必要方法。**本片没有新 migration**；compile 仍强制实际 `00070_runner_command_operations.sql` 存在且全量迁移均在闭包，元数据另绑定真实 66/68 输入。原 `-race -tags=integration -c -p=1` 仅编译一次，然后精确 list；只有成功且输入相同才生成 prepare。runtime key 集只增加该次真实 binary。

原 [record-native.py](record-native.py) 字节保持，SHA-256 为 `56a32b0d45a633c3c85306d098730318327c02f4ee9c5267c8d712e9880d4f20`。native 仍由 `pg_only_supervisor.py --root-chain` 启动原 driver，保持七资源、540+60 s、6m test、原 Popen/实际 Wait、desc/private/runtime 双尾、75 s 自动 HOST_TCP 基线差额双空和所有输入门。prepare 不启动服务、不授权 native，不替换 driver、不改过滤、不自动重试。

## 分段验证与原件

原件现完整保留于 main `output/ai/retired-20261012/write-file-result-consumption/`；下列相对路径均以该根为准，原输出内文件未覆盖或删除：

- `methods01`、`methods02`、`methods03`：各原 1 正 / 7 反通过，各轮 10 方法输入前后相同；轮间源码和方法的授权差额按各自记录保留。
- `basic01`：17 top / 78 sub 与 5 包 vet 实际通过，843 项输入六图相同。该结果是既有基线；后续 reader 范围单独记录，不回写此轮。
- `basic02`：reader-followup 原 4 top / 24 sub 与 Tool/runtime vet 实际通过，572 项输入六图相同。该轮只验证 deadline-scope 修复和受影响 reader，不将它累计为重复的全包通过；原 basic01 及其他 4 包 vet 保留为已接受基线。
- `compile01`、`compile02`、`compile03`：各自编译和精确 list 实际 0，1991 编译输入前后同；每份 runtime 1992 项仅增加对应 binary。compile02 相对 compile01 只变更已批准的两个 fixture 源；compile03 相对 compile02 只变更 reader 源及同包控。实际最终 binary 为 `compile03/projectvariable.test`，90,184,357 B，SHA-256 `b1771f2165ae68dc2c5ba2315e39557096ca628bf1f07519f41877ddccbfa77a`。
- `native01`：原工具 69794 / supervisor 919324 whole 1，两个 sub 在 fixture 创建目录时遇到 ENOSPC，失败时同进程 `bavail=0`，业务未入。prepare 的容量快照不代表运行峰值。后续环境回收有独立实际记录，原失败保留。
- `native02`：原工具 62998 / supervisor 933087 whole 1，两个 sub 共 67.43s；首消费失败，第二返回 DEPENDENCY_UNAVAILABLE，并检出 PG 运行中更换共享 delegate 的 race。原运行输入与完整 race 日志保留。
- `native03`：原工具 78288 / supervisor 945535 whole 1，两个 sub 共 67.09s，本轮原日志无 race warning。两条安全诊断均为最终消费事务实际返回数 0、结果借用调用数 0；历史 Discover 成功后，历史借用返回时内层 deadline 已到、callback 为 0，外层消费 context 仍 live。未到真正消费 COMMIT、Lookup、Determine 或 Projection 成功门，不能记作闭环通过。
- `native04`：受测 HEAD `98d232475c8f61a55977a917898c41b3874f0e37`，复用 compile03 与 fresh `/tmp/wfrc04`。第一 sub 55.64s，真实原 Write 及旧 owner Drain 后，正式撤销 Tool/Mount，首次消费、独立持久事实、68 Determine 和只读 Projection 均通过。第二 sub 51.46s，真实完整消费事务物理 COMMIT 后遮回执，仅原 consumer 与新 consumer Lookup 恢复同一 receipt；原 Op/Attempt/RID、唯一 consumption/seq3、wire 1 和 Model 1 保持。无再次 Consume、Execute 或 Model，原日志无 race warning。

四次 native 的原 Go / driver / outer Wait、七资源 14 次 absence、desc/private/runtime 双尾、HOST_TCP 双空、1992 项前后输入及四身份均完整保留。最终原工具 83628 亲收 exit 0 / chunk `c81c0b`，supervisor 原实际 Wait 0 / 199.212s；日志内 `SUPERVISOR inputs_unchanged=True terminal=0 elapsed=198.237s` 与外层计时分别保留。工具亲收转录在 `native04/native-tool-observation.json`，明确标为 assistant-observed，不能当作 provider 原生回执。`native04/terminal.json` 是此次真实 outer 结果，`post-native-observation.json` 与 `input-hashes-after.json` 保存后验边界。

原 native04 主日志为 `/tmp/wfrc04/pg-762a4bf901a542e08ea49693c2743c45.log`，同 nonce 目录的 `owned.json`、`request.json` 原件保留。前三轮分别位于 `/tmp/wfrc01`、`/tmp/wfrc02`、`/tmp/wfrc03`，各轮配方、输入和工具回执在对应 `native01`—`native03` 输出目录。上述 native04 限定业务与全尾已完成独立复核。完整历史已随 main `ac025026` 正式交付并推送；保全与树/ref实际清理见下节。

已执行的 sequence03 按 methods03 → reader basic02 → compile03/list → prepare04-only 串行条件推进，原工具 83881 完整返回 0；只有实际 0 且输入相同才继续。native04 在独立授权后由原 recorder 单次执行。下面保留准确方法入口供原件复核，不构成再次运行授权：

```sh
python3 -B .agent-state/write-file-result-consumption/profile-controls.py
python3 -B .agent-state/write-file-result-consumption/run-basic-checks.py --scope reader-followup --output output/ai/write-file-result-consumption/basic02
python3 -B .agent-state/write-file-result-consumption/run-pg-checks.py compile --output output/ai/write-file-result-consumption/compile03
python3 -B .agent-state/write-file-result-consumption/run-pg-checks.py prepare --compile output/ai/write-file-result-consumption/compile03 --output output/ai/write-file-result-consumption/native04 --namespace /tmp/wfrc04
```

原执行根为 `/workspace/agenteam-write-file-result-consumption`，现已普通清理；上述命令保留原调用语境，不应在已删除路径续跑。历史源码从正式 main 恢复，原 binary/output 从下述保全根取得；新任务使用自身独立树与产物。热 GOCACHE 与只读 GOMODCACHE 沿原方法固定位置保护，不复制或更换锁。源码/静态通过不计真实 PG，通过范围也不包括下一 Model、再次 Tool 执行、默认绑定、终态退休或 tools HTTP 解禁。全部 Git 操作由 root 及其明确指定的 Git 执行者完成。

## 正式交付与保全

完整开发历史与最终文档 `67780ac4f01cb66305b93de89cfdf8f79364465c` 已以正常 no-ff 合入 main `ac02502698af864c20a65fd83d35467511f014be`。main/feature atomic push PID965246实际0，fresh与显式main/ai tracking同步原20394亲Wait0；集成未修改受测技术字节，不重跑已接受检查。

保全原28079亲Wait0（47e291），Python966046实际0/4.883s：完整 output 的218文件/284,884,747B与42目录经同FS `renameat2(RENAME_NOREPLACE)` 迁至[原件目录](../../output/ai/retired-20261012/write-file-result-consumption/)，逐path/SHA/size/dev/inode/mode/uid/gid/nsmtime前后全等。全部四native、三compile、原FAIL、Wait与assistant-observed工具转录身份保持；[旁目录证明](../../output/ai/retired-20261012/write-file-result-consumption-preservation/result.json)与迁移前后清单不混入原输出。

[历史输入证明](../../output/ai/retired-20261012/write-file-result-consumption-preservation/source-history.json)覆盖56输入图、1,997个仓库path/SHA版本，逐blob匹配 `0b9a96ab`、`ff03f4ec`、`98d23247`，三者及最终文档均main祖先。三个实物binary作为原输出保留。两个外部输入仅保原Go/MinIO路径与SHA，不冒已复制或由Git恢复；原hotcache、只读modules和MinIO保护不动。

`/tmp/wfrc01`—`04` 各原log、owned/request与telemetry小文件共16份已排他复制到旁证明目录`external/`，逐字节及mode/uid/gid/nsmtime核同，原tmp未动。副本的dev/inode独立，不冒原身份。只核原已知PID当前不可见、原Wait与作者停写声明、旧树无其它ignored/普通untracked；不扩大为全局FD或未知配方保证。新Command方法owner确认后继无旧树绝对live依赖。

随后普通worktree remove PID966524、普通branch -d PID966546均0；fresh精确等最终tip后，单ref exact-lease删除PID966551实际0，fresh空及显式main/ai prune均0。原30239亲Wait0（e3f243），无force树删除、无保全副本删除。见[Git实际清理](../../output/ai/write-file-result-consumption-git/cleanup01/result.json)。原运行目录虽已移走，所有已保全失败、来源局限和STOP保持；本片不包含下一Model、再次Tool执行、默认生产装配或tools HTTP解禁。


## 历史编译二进制归档补充（archive03）

2026-10-12，本片下列裸二进制已无损收入共同 [archive03](../../output/ai/retired-compile-binaries/archive03/README.md)，随后经独立授权从原 retained 路径精确移除。完整字节现保存在 [history-binaries.tar.gz](../../output/ai/retired-compile-binaries/archive03/history-binaries.tar.gz)：298,117,374 B，SHA-256 `e72c0e3c62242e4ef0fa552bdfdfb634b97290d6c58de90026c7997a77b9922a`。表中成员名相对原 `/workspace/agenteam/output/ai`，并保留原 compile 路径关系。

| 归档成员 | 字节 | SHA-256 |
| --- | ---: | --- |
| `retired-20261012/write-file-result-consumption/compile01/projectvariable.test` | 90,149,222 | `02297942ea521b9a8bbed28c6dda1ee3a9bad0cf926db000a6a4f317413060eb` |
| `retired-20261012/write-file-result-consumption/compile02/projectvariable.test` | 90,183,565 | `659eed4a3217527b2d5f1f0ed10bee291987e511876891ef3696ca37a0e0ee52` |
| `retired-20261012/write-file-result-consumption/compile03/projectvariable.test` | 90,184,357 | `b1771f2165ae68dc2c5ba2315e39557096ca628bf1f07519f41877ddccbfa77a` |

原 phase1 工具 28190 实际 0（`662de4`），outer 988070 / child 988073 实际 Wait 0 / 50.927s；当轮完整解流验证九成员并保留全部裸件。后续 phase2 工具 59243 实际 0（`e2e194`），outer 990023 / child 990026 实际 Wait 0 / 8.168s：先重新验证整包、九成员内容与元数据及原件身份，再只 unlink 这九个获授权 ELF，目录和其它文件删除数均为 0。实际清单见 [removal03](../../output/ai/retired-compile-binaries/removal03/README.md)、[result.json](../../output/ai/retired-compile-binaries/removal03/result.json) 和 [deleted.jsonl](../../output/ai/retired-compile-binaries/removal03/deleted.jsonl)。原 139 份关联记录、FAIL、输入 maps 和归档 full stat 均未改变；当前 Command 2,007 输入与四身份保持。整批相对裸件基线净减少 394,043,392 B 已分配空间（小证明文件另计），不据此宣称 ENOSPC 已解决或新运行必定成功。

恢复时先核整个归档 SHA 和 [manifest](../../output/ai/retired-compile-binaries/archive03/manifest.json)、[before.json](../../output/ai/retired-compile-binaries/archive03/before.json)、[specs.json](../../output/ai/retired-compile-binaries/archive03/specs.json) 中的精确成员关系，再仅向新的隔离目录排他提取所需成员。例如使用 `tar --extract --gzip --file <归档绝对路径> --directory <新隔离目录> --keep-old-files --same-permissions --same-owner --numeric-owner -- <上表成员名>`；不得覆盖现有文件或写入当前 source/cache。提取后逐件核 SHA/size、mode、uid/gid 和纳秒 mtime；权限不足以恢复元数据或任一核验不符即停止，不能记为恢复完成。完整边界见共同归档恢复说明。

前文的文件数量、裸件保留和迁移 inode 相同是原保全时的历史事实。重新提取会分配新 inode 并产生新的 ctime，dev 取决于恢复目标文件系统，旧身份只保留在原 manifest/PAX 记录中，不能称新文件重建了原 inode；原日志、结果和 maps 不改写。恢复不是重跑授权或新的 PASS，热 cache、只读 modules、工具链与当前二进制不在本次删除范围。
