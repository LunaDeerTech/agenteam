# D15/D16 Runner write-file WSS 适配器

状态：源码阶段，尚未运行 Go 基础检查或真实 WSS。本片以已交付本地 `filesystem.Root.Write` / `workspace.Handle.Write` 为底座，只新增 Runner 闭集 codec、Handler 和显式 Control 绑定。

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

## 分域与验证计划

skill_package_review 负责 Handler、Control 显式接线、本卡/D15/current；skills_milestone_review 负责两个协议文件；knowledge_delete_fixture 独写真实 WSS 控制；recovery_lead 负责 `.agent-state/runner-write-file-wss/` 方法；runtime_recovery 唯一独立主审。Git、cache 与实际执行窗口由 root 统筹。

必要源码控制包括严格 codec/完整帧、显式构造与默认拒绝、publication/取消/close 分类、原 Ensure/Write/Drain barrier、真实最大二进制 Write→分块 Handle.Read、禁止覆盖和隔离。首条真实网络链为 `TestWriteFileWSS/committed-write-and-overwrite-conflict`：可信测试 TLS/WSS 对端 → 原 Runner Client/session → 真实 Handler/Manager/Root → 已提交结果与独立完整读取 → 显式第二请求的 no-overwrite conflict → 原客户端/连接/服务/Manager 实际退出。该测试对端不冒 Central 当前授权或生产 dispatcher。

当前仅源代码、指定 gofmt 和静态差额检查；测试通过与实际退出结果待原工具链窗口。已交付 local Write 的原 FAIL 与通过事实沿原卡，不重复整套 D16 矩阵。Object/toolsHTTP/SPA/Jina/Image 既有 STOP 和默认生产边界保持。
