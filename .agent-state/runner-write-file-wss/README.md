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

固定源码 `5d8cf4c4` 已完成并获唯一主审有限接受：

- `methods01` 的离线1正/7负、原TCP while AST对照、语法与 inert help 按原方法接受；这些结论仅对应方法。peer.Close 原返回证明修正来自静审，未发生本片动态 FAIL。
- `basic01`：原工具31350亲收actual0；Go810676 race 12top/37sub（10新+2旧）0/10.189s，Go810921三包vet 0/2.073s；1493项before/after-unit/after一致，四次实际race/plain go-list均0。
- `compile01`：原工具18090亲收actual0；Go811848 race-c 0/1.228s，list811878 0/1.021s，精确唯一top。1480项实际编译输入三份等；binary 17,991,296B、SHA256 `151c6b630a9ff97eb8bd4aa20c48ec703cc86460fbb661e29560d22b4f0f4cec`，native仅加此一项至1481。
- `native01`：原工具96046亲收actual0（原chunk `ea8450`）；supervisor812703原Wait0/3.395s，内部3.137s，Go812704原Wait0，精确1top/1sub 1.76s PASS。唯一真实资源marker、desc/private两次空、原自动TCP差额双empty、1481输入不变和terminal0全齐，窗口已释放。

原件原位为 `output/ai/runner-write-file-wss/{methods01,basic01,compile01,native01}`，现按下节映射位于main retired目录，各阶段原Popen/Wait日志、input maps、binary/recipe及作者亲收的tool-terminal保留；网络原件 `/tmp/rwfw01/{invocation.json,process.json,test.log,native.log,tcp-baseline.json}`。完整开发历史已随main `570da7a2`交付，root原推送2926实际0；原件已按下节保全，旧树及本地分支已由root普通清理。

结果止于本页首段的trusted peer / bound session范围，不能外推Central授权、enrollment、完整或默认Client.Run/生产接线，以及未运行的WSS故障取消。原所有STOP保持；详细业务与基础边界见[本卡](../../docs/development/work-items/d15-runner-write-file-adapter.md)。


## 已交付后的恢复映射

原树 `/workspace/agenteam-runner-write-file-wss/output/ai/runner-write-file-wss` 已通过同文件系统 `renameat2(RENAME_NOREPLACE)` 原子迁至 `/workspace/agenteam/output/ai/retired-20261011/runner-write-file-wss`，不存在覆盖或复制替代原件。原80个文件共22,975,545B、7个目录的相对路径、SHA/size（文件）、device/inode/mode/mtime（文件与目录）在迁移即刻前后逐项相等；随后仅追加保全说明和tmp副本。详细原目录清单及核对结果见该目标 `preservation-result.json`。

原 `/tmp/rwfw01/{invocation.json,process.json,test.log,native.log,tcp-baseline.json}` 的5个必需文件使用排他创建逐字副本保存到目标 `retention/tmp-rwfw01/`，原tmp及空private目录保留。原日志与manifest中的绝对路径不改写；恢复时将旧output前缀替换为上述retired前缀读取原件，binary对应 `compile01/write-file-wss.test`（原SHA/size保持）。原record不可直接从retired位置重跑；另获运行授权时先由root恢复源位置或生成新的独立配方，不能修改旧证据或复用native01输出。

所有原工具/Go/supervisor已亲收实际Wait，记录仍在各原result/tool-terminal；迁移前本任务记录的PID均已不存在，该观察不替代原Wait。methods/basic/compile/native全部仓库内输入共63个路径，逐项SHA等于main可达 `5d8cf4c4` 对应blob；方法README的旧输入亦在该提交。静态Close修前 `61e33774`、修后 `5d8cf4c4` 与最终文档 `25840576` 全历史由main保存，无需额外恢复patch、动态FAIL文件或永久归档分支。1431个外部输入属于原工具链/模块路径，未迁移或删除；SkillHTTP热cache与SkillsMilestoneResume只读modules继续保护。

本任务作者后继从已交付main取源码，无旧树source/output/binary/cache的绝对运行依赖；没有在途Go/Node/服务或网络调用。保全说明已随main `58a1b1e1`推送（原68131实际0）。root核定最终tip `25840576` 为main祖先后，普通worktree remove与普通branch -d均实际0（chunk `095069`）；此前fresh ls-remote `f5f55c`实际0为空，该分支从未推送，没有远端删除动作。清理仅增加1树/1本地分支，全局现34/35/64，146待核与keep1不变；证据、binary、原tmp和受保护依赖未删除。
