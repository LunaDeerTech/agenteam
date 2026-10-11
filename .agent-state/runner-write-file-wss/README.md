# Runner write-file WSS 方法

本片方法只覆盖显式构造的 write-file Handler、Control bound session 和可信测试 peer 的真实 TLS/WSS。默认 `New`/cmd 的 nil executor、生产 registry、Central 授权、enrollment 与完整 `Client.Run` 接入不在此范围。没有 PostgreSQL、Docker 或浏览器资源；不能借用旧 PG 七资源声明代替本 fixture 实际生命周期。

## 基础与真实调用分开

`run-checks.py basic` 在单独授权的 Go/cache 窗口运行三包精准 race，再对同三包 vet：

| 包 | 新入口 | 必要旧回归 |
| --- | --- | --- |
| `internal/runnerprotocol` | PayloadStrictAndCanonicalBase64、BoundsAndWholeMessage、CommittedResultAndTerminalVariants、DefaultProjectionRedacts 四个 `TestWriteFile*` | 无；旧通用 codec 未改 |
| `internal/runner/writefile` | RejectsBeforeWorkspaceAdmission、KeepsPublicationAndCompletionFacts、CancellationWaitsForActualWriteAndClose、RealWorkspaceWriteAndRead 四个 `TestHandler*` | 无；已验 filesystem/workspace Write 不重复 |
| `internal/runner/control` | `TestWriteFileExplicitClientKeepsDefaultUnbound`、`TestWriteFileControlSessionUsesRealHandler` | `TestReadFileExplicitClientKeepsDefaultUnbound`、`TestDirectoryControlExplicitClientsKeepExactCapabilities`；仅受 hello 分支影响的原入口 |

合计 10 新 top + 2 必要旧 top，不执行真实 WSS 场景或全库测试。通过依据是原实际退出、三个包精确入口 PASS 集合与输入一致；编译失败、skip、缺入口或额外 top 都不能计通过。失败保留，vet 仅在本次 race 完整成功后运行，不能自动重试。

`run-checks.py compile` 单独编译 `internal/runner/control` 的 race binary，并用其真实 `-test.list ^TestWriteFileWSS$` 核唯一 top。真实调用只选 `TestWriteFileWSS/committed-write-and-overwrite-conflict`（1 top / 1 sub），由独立 Go fixture 实际创建 TLS/WSS、bound session、Worker 和 Workspace Manager；首写 512 KiB，再以新 RequestID 验 no-overwrite 冲突，独立 Handle 分段读取核实际 bytes。精确覆盖以最终固定 fixture 及原运行结果为准，方法准备不等于已编译或验收。

## 输入与实际返回

基础用 race/plain 两次真实 `go list -deps -test -json` 的并集，编译用实际 race 闭包。记录实际 Go/C/asm/embed、同包测试、module 文件、固定 Go 以及本片方法源；不手写缩小源文件列表。生成的 test main 单列排除，其实际 package/test 源仍在闭包。每次使用新输出目录、离线只读模块和已有受保护热缓存，max1；前后再发现文件集合及逐 SHA 核对。原命令、Popen PID、`wait()` 返回、原日志和输入均独立保存，失败不覆盖。外层工具终态由执行者亲收并补存，不把子阶段返回冒成整个工具返回。

`prepare` 只读已成功 compile/list 与当前输入，添加唯一 binary SHA/size，生成新 `nativeNN` 请求、四项 recorder 身份和 `/tmp/rwfwNN` namespace 计划；不创建 namespace、不读取 TCP 基线、不启动服务。实际 native 必须另获 root 资源窗。

薄 `record-native.py` 只核冻结输入、调用原 `native.py` 并实际 Wait，不负责清理或改变门。`native.py` 对原 binary 使用 60s test timeout、70s 外层等待；超时必须记 FAIL，TERM 与 KILL 的实际 Wait 各限 3s。Go fixture 的完成标记仅在 Session/wire/worker、peer handler/连接/server、离线 client、所有独立 Handle 和 Manager 实际 Close/Drain/Joined 完成后输出；任一步失败不能输出 `WRITE_FILE_WSS owned_resources_joined=true`。方法同时要求原唯一 RUN/PASS 与 test Wait0，不能仅凭标记通过。

原监督器 `tcp`、`tcp_failure_sample`、`descendants` 的函数直接从原源码 AST 取得；不改共享门。原 test 退出后亲收后代退出，检查两次后代为空和私有 tmp 为空；自动基线为调用前真实 `/proc/net/tcp{,6}`，尾部逐字同义的 75s 自动差额门须两次 empty。任何 TIME_WAIT/其它差额都计入，不过滤、不人工预热或重设基线；无法归属也不能改为通过。没有外部服务的 profile 不伪列 PG ID。原 binary/所有 runtime 输入必须保持不变，才可形成有限 wholePASS。

## 命令与当前状态

以下命令仅说明配方，不授予 Go/native 窗口；所有结果必须来自原执行。

```sh
python3 -B .agent-state/runner-write-file-wss/run-checks.py basic --output output/ai/runner-write-file-wss/basic01
python3 -B .agent-state/runner-write-file-wss/run-checks.py compile --output output/ai/runner-write-file-wss/compile01
python3 -B .agent-state/runner-write-file-wss/run-checks.py prepare --compile output/ai/runner-write-file-wss/compile01 --output output/ai/runner-write-file-wss/native01 --namespace /tmp/rwfw01
python3 -B output/ai/runner-write-file-wss/native01/record-native.py
```

当前仅方法源码准备；产品及 WSS fixture 仍由各自作者冻结。允许的离线方法控制为 `profile-controls.py` 的 1 正 / 7 负与原 TCP while AST 对照、Python 语法和 inert `--help`。不得将这些控制计为产品 Go、真实 WSS 或退出门通过。原所有 STOP 保留。
