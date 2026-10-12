# D15/D16 Runner write-file WSS 适配器

状态：固定 `5d8cf4c4` 的基础检查、编译与隔离真实 WSS 首链已获唯一主审有限 wholePASS 接受，随完整开发历史正式交付 main `570da7a2`，原推送工具2926实际0。全部原件已无覆盖保全，说明随main `58a1b1e1`推送68131实际0；root祖先检查后普通删除旧worktree及本地分支（原`095069`全0）。远端原查询`f5f55c`实际0为空，本分支从未推送，未增加远端删除数。本片以已交付本地 `filesystem.Root.Write` / `workspace.Handle.Write` 为底座，只新增 Runner 闭集 codec、Handler 和显式 Control 绑定。

依据：[D15 §7](d15-runner-control.md#7-rpc端口与-unknown)、[本地 Write](d16-runner-write-file.md)、[Workspace](../../architecture/runner/agent-workspace.md)与[执行运行时](../../architecture/runner/execution-runtime.md)。

## 接口与边界

`runnerprotocol.WriteFileOperation="write-file"`、revision `"1"`，沿 wire v1.0。payload 精确为 `{workspace_name,arguments:{path,data_base64,overwrite}}`；workspace name 来自可信当前映射，P/A 决定目录作用域，Mount/Workspace UUID 不是目录名。该 DTO 和 Agent 项目配置均不是执行授权。本片不新增 Central grant、ticket、Tool Operation 或 dispatcher，不借 read-file 票据。

arguments 仅相对 path、严格标准 padded base64 的完整文件字节与显式 overwrite。空文件合法；原始数据最大 524,288 B（512 KiB），base64 最多 699,052 B，解码后仍核原始大小；整帧继续原 1 MiB `NewMessage` / `Encode` 门。没有流式写入或别名编码；父目录必须存在，原 no-symlink 原子写入规则保持。本地库 1 MiB 上限没有因此变为 wire 上限。拒绝 environment 与 idempotency_key，未绑定 masking。

装配为 `writefile.New(*workspace.Manager)` → `control.NewWriteFile(Options,*writefile.Handler)`。Manager 必须来自可信设备 root 的 `workspace.New`，由装配者持有至 `Client.Run` 实际退出。默认 `control.New`、cmd、生产 registry 与 ready 继续未绑定；显式构造只声明 write-file capability，不隐式加入 read-file 或 directory。

## Publication 与真实退出

Handler 顺序为校验 → 原 `Manager.Ensure` → 原 `Handle.Write` → 背景上下文的同步 `Handle.Drain` → terminal。取消不跳过真实调用或 close，不派生脱离 owner 的清理 goroutine；解码字节在这些原调用实际返回后清零。Handler 不自动重试，也不通过 Read 重新解释 Unknown。

| 原 Write 事实 | wire 结果 |
| --- | --- |
| `not_committed` 且 bytes=0、durable=false | 原安全 failure/cancelled；无 payload |
| `unknown` 或不完整/矛盾的 publication 事实 | 原 unknown，固定安全文案，无 payload；取消不能将其降为未提交 |
| `committed` 且 bytes 等于本次完整输入长度 | success，仅操作专属 payload；顶层 code/safe_message 仍禁止 |

专属结果精确 `{path,state:"committed",bytes_written,durable,completion_code?}`。`durable` 忠于原文件与父目录两次 fsync 的实际结果；它不因之后 close 错误而被清除。completion_code 仅 `INTERNAL_ERROR` / `CANCELLED` / `TIMEOUT`；durable=false 必须带固定 completion_code。文件已提交而父目录 fsync 失败仍为 committed/durable=false；已 durable 后的 close 失败仍为 committed/durable=true 并带 INTERNAL_ERROR；提交后取消/期限保留 committed 及相应告警。混合 I/O 与取消错误不得伪成单纯取消。任意原始错误、正文和系统路径不进入诊断或结果。

这里 success 表示已知完整原子发布，不等于 durable 或没有后置错误；generic failure/cancelled/unknown 禁 payload 的规则完全不变。合法终态返回 nil Go error，以免原 Control 将已确定结果改为 Unknown；原 wire、worker 与连接退休仍由既有 Control owner 实际收束。

## 已验证范围

skill_package_review 实施 Handler/Control 并亲自执行；skills_milestone_review 实施 codec；knowledge_delete_fixture 独写真实 WSS fixture；recovery_lead 实施方法；runtime_recovery 唯一独立主审已按原件接受。Git、cache 与实际执行窗口由 root 统筹。此前 peer.Close 返回证明在静审中发现漏项，于 `5d8cf4c4` 保留原 Close 结果后闭合；不是实际运行失败，不补造 FAIL。

basic01 原工具 31350 亲收 actual0：Go 810676 的 race 为 12 top / 37 sub（10 新 top、2 个受 hello 分支影响的旧入口），10.189s；Go 810921 的三包 vet 为 0/2.073s。race/plain 四次实际 go-list 全部退出0，1493 项编译输入 before / after-unit / after 完整一致。控制覆盖严格 codec/整帧、默认拒绝、publication/取消/close 分类与原调用 barrier、真实最大二进制 Write→分块 Handle.Read、禁止覆盖和隔离；没有重复旧 D16 全矩阵。

compile01 原工具 18090 亲收 actual0：Go 811848 race-c 为 0/1.228s，811878 list 为 0/1.021s，精确唯一 `TestWriteFileWSS`。1480 项实际编译输入三份一致；binary 为 17,991,296 B、SHA256 `151c6b630a9ff97eb8bd4aa20c48ec703cc86460fbb661e29560d22b4f0f4cec`。native 配方只增加该 binary，共1481项。

native01 原工具 96046 亲收 actual0：supervisor 812703 实际 Wait0/3.395s，原内部3.137s，Go 812704实际 Wait0；`TestWriteFileWSS/committed-write-and-overwrite-conflict` 精确1 top / 1 sub、1.76s PASS。可信测试 TLS/WSS peer → 原 Control bound session/wire → 显式 Handler/Manager/Root，完成512KiB原子写入及独立Handle按64KiB完整读取；同连接显式第二请求得到 no-overwrite Conflict，原正文再次完整一致。原 executor 返回、两终态及RequestID关联真实核对，没有自动重发。

唯一 `WRITE_FILE_WSS owned_resources_joined=true` 只在实际 session/worker、wire reader/writer、peer handler/socket Close、TLS server、离线显式 client、独立Handle与Manager 全部退出后产生。监督器亲收原 Go 后，desc/private 两次空、原75s自动baseline TCP差额两次empty、1481输入不变与terminal0均闭合。没有PG/Docker，不借用七资源声明。全部原输出已从旧树 `output/ai/runner-write-file-wss/` 同文件系统无覆盖迁至 main `output/ai/retired-20261011/runner-write-file-wss/`：80文件、22,975,545B与7目录的逐路径内容和身份保持；其中 `methods01/basic01/compile01/native01` 均完整。5份 `/tmp/rwfw01` 原日志/调用/PID/TCP基线逐字副本在 `retention/tmp-rwfw01/`，原tmp保留。63个仓库内输入均逐项等main可达原`5d8cf4c4`源码，1431个外部工具链/模块输入保持原路径未搬；静态修前`61e33774`及全部开发历史均main可达，无动态FAIL。方法与精确恢复映射见[恢复与执行说明](../../../.agent-state/runner-write-file-wss/README.md)。

真实链仅证明 trusted TLS/WSS peer 的 bound session，不含 Central grant/Operation/dispatcher、enrollment、完整或默认 `Client.Run`、生产接线，也不外推故障/取消真实WSS场景；后置错误/取消判据属于上述受控基础测试。已交付 local Write 的原 FAIL 与通过事实沿原卡。Object/toolsHTTP/SPA/Jina/Image 既有 STOP 与默认生产边界保持；本片测试资源与执行窗口已全部释放。
