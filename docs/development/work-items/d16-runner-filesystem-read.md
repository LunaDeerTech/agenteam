# D16 Linux Workspace 文件读取库

状态：有限 SPEC 已获非作者范围接受，首实现与必要测试已冻结待保存、检查和实际源审。当前仅本地库，不提供生产 Runner operation；完整 D16、真实 Mount 和 D18 接入未完成。基线为 main `3a7a3fb5`。

## 1. 依据与交付范围

依据 [D15](d15-runner-control.md#7-rpc端口与-unknown)、[D01 Runner 边界](d01-contracts/model-tool.md#runner-与-mcp-通信边界)、[Workspace](../../architecture/runner/agent-workspace.md)、[文件运行时](../../architecture/runner/execution-runtime.md#4-filesystem-runtime)和[结果边界](../../architecture/tool-system/tool-result-backend.md#4-result-size-与-truncation)。当前 [Agent 配置](d10-agent-configuration.md#22-f1真实有限事实能力当前前置未齐)没有真实 current Mount 目录；ID 类型和 Runner 在线状态不构成目录或授权证明。

仅新增 `internal/runner/filesystem/` 的本地 Root 生命周期、read/list、固定错误与结果、Linux 实现、非 Linux 明确拒绝实现及相邻测试；另维护本卡。库不 import Central 或 runnerprotocol，不新增公开 wire/schema/operation 契约。默认 `control.Client` 的 executor 仍为 nil，hello/registry/Control/cmd/config/依赖/迁移均不改。

不做 ID→workspace name/current Mount 解析、目录创建删除、write/edit/grep/find、命令/进程、Data Channel、工具授权或结果外部化。未来真实绑定必须由事实所属模块提供当前 Mount 与固定 device root 下的合法映射，并由 D18 固定正式 schema/结果适配。此次库接受不能替代这些门槛。

## 2. 可信根与路径

`NewRoot(directory *os.File) (*Root, error)` 只接可信宿主调用方已解析并打开的**确切 workspace 根目录 fd**，不收宿主路径、业务 ID、逻辑 name 或授权标记。以 `SyscallConn.Control` 保护原 fd 的借用，经 `F_DUPFD_CLOEXEC` 取得私有副本，验证目录和 openat2 可用性；原文件仍归调用方，构造失败关闭已取得的副本。nil、已关闭、非目录均拒绝；Root 零值不拥有 fd 0。Root 不提供路径 getter，fmt/JSON/slog 仅固定安全标签。

路径必须有效 UTF-8、总长≤4096 B、单组件≤255 B；拒绝空串、NUL、反斜杠、绝对路径、空组件、`.`/`..` 组件和尾 slash。仅 List 的单独 `.` 表示根目录。不 trim、URL decode、Unicode 改写或自动 clean。Linux 文件名若不能按这些组件规则无损表示，List 返回固定类型错误和零结果，不跳过、不以 JSON replacement 伪造名字。

Linux 目标≥5.15，使用既有 `golang.org/x/sys v0.47.0` 的 `openat2`，固定 `RESOLVE_BENEATH | RESOLVE_NO_MAGICLINKS | RESOLVE_NO_XDEV`。允许根内相对 symlink，拒绝绝对/逃逸/magic link 和子 mount；ENOSYS、受限或不支持时明确不可用，不回退到 EvalSymlinks+Open。每次操作独立打开目标 fd/目录偏移，读前核实际 fd 类型，只读普通文件。打开含 `O_CLOEXEC | O_NONBLOCK`；后者避免 FIFO 等待，不保证特殊 device 的 open 无副作用或任何原系统调用可即时取消。List 对 symlink 只返回类型，不读目标；未知类型只作同目录 fd 下 NOFOLLOW 元数据核验。

三项 resolve 标志不隔离同文件系统硬链接，也不是 OS sandbox。可信根来源、宿主内容管理及不放入不受信特殊设备是调用方前提；不能用 nlink 检查声称普遍内容隔离。fd 固定原 inode，rename 后不重新按宿主路径追踪；不提供并发文件或目录快照。

## 3. 本地接口与限额

```go
NewRoot(directory *os.File) (*Root, error)
(*Root).Read(ctx context.Context, request ReadRequest) (ReadResult, error)
(*Root).List(ctx context.Context, request ListRequest) (ListResult, error)
(*Root).Stop()
(*Root).Drain(ctx context.Context) error
```

`ReadRequest{Path string, Offset int64, Length int, Encoding string}` 不设默认值：Length 1..65536，Offset≥0，Offset+Length+1 不溢出 int64，Encoding 精确 `utf8` 或 `base64`。`ReadResult{Path, Offset, BytesRead, EOF, Encoding, Data}` 的 offset/length/BytesRead 均按原字节计。最多读取 Length+1 字节，返回前 Length 字节；EOF 只根据本次读取观察，不能用旧 stat 大小推断。越 EOF 成功返回 0 B、EOF=true。base64 为标准带 padding；utf8 对选中的原始切片严格校验，切断多字节字符即类型错误，不移位、截短修补或替换。并发修改只返回同一已打开 inode 的本次 range 观察，不自动重读获取一致版本。

`ListRequest{Path string, Limit int}` 要求 Limit 1..128。`ListResult{Path, Entries, Truncated}`；Entry 为 `{Name, Kind}`，Kind 闭集 `file/directory/symlink/other`。使用有界缓冲读取至 Limit+1 项，返回前 Limit，仅实际观察到额外一项才 Truncated=true；不先读完整目录。只对返回子集按名字字节排序，不承诺全目录排序、分页、cursor、总数或跨请求快照。重复 List 从头读取。条目并发消失或无法确定类型返回固定 Conflict 和零结果。

两种结果均在返回前以本库唯一固定 JSON 投影实际编码，含字段名/路径/JSON escaping/base64/全部 entries 的总字节数≤512 KiB；超过返回固定 limit 错误和零结果。该投影仅固定库内资源预算，不成为 D15 成功 payload 或另一套 D18 公共契约。64 KiB 读取和最多129个目录项限定单调用内存；并发调用数量仍由可信宿主调用者控制，本库不引入新的工具并发策略。

任何错误、取消或关闭失败均返回零结果，不把已采部分作为成功。错误是固定本地类别：invalid request、invalid type、not found、outside root、conflict、unsupported、unavailable、limit、closed；context 取消/到期保留 `errors.Is` 语义。不返回原 PathError、host path、I/O 原文或内容，不记录业务材料。未来 D15 适配的 code 映射由正式接线单独固定；本库不返回 Runner terminal 或宣称当前 Mount 有效。

## 4. 生命周期与实际退出

Root 不可复制。准入、Stop 与成功 seal 使用同一个 mutex：操作登记后持独立派生 context；Stop 单调拒绝新调用并取消全部已登记调用。Stop 先赢则操作在原系统调用及自有目标 fd 的 close 实际结束后返回零结果和取消；操作先 seal 则该成功可以随后被调用方观察，不以墙钟 return 时间证明取消。原调用上下文的更早期限始终保留。

每个 fd 只有一个 close owner。操作完成所有 I/O、结果编码和目标 close 后，才在同一锁下检查取消/Stop、seal 并退出登记。Linux close 返回错误（包括 EINTR）后不按原 fd 数字重试，避免误关后来复用对象；关闭失败保持安全错误，不声称成功读取。

Drain 先 Stop，等全部登记的原调用完成，再实际关闭 root 副本并保存关闭结果。Drain 的 context 等待超期则返回原 context 错误，保留 root 副本和在途登记，不抢关在途 fd、不记已 join；调用真实完成后可再次 Drain。并发 Drain 只有一个 root close owner，其余等待实际 close 结果；零值/nil Root 安全拒绝且不关闭任何外来 fd。关闭已经返回的结果会缓存，不重复 close。

普通文件 pread、getdents、close 不保证可被 context 即时打断。库不新建后台 timeout goroutine 后让原 syscall 脱离，也不增加业务 timer。已经进入的原调用必须等待真实返回；未完成时始终未 join。将来接线的 Runner Force/process 退出是外层责任，此库不伪造该边界。

## 5. 必要验证与停止范围

首批检查限本包 pure/race 与任务自有临时目录：

- 参数、整数边界、UTF-8 切片、binary/base64、EOF、实际 JSON 转义后限额；非法 UTF-8 文件名不得替换/静默消失。
- 真实普通文件/目录、内部相对 symlink 正向、绝对和 `..` 逃逸拒绝、非 regular 拒绝、独立目录/读取 offset；不创建 mount、特殊 device 或外部资源。
- borrowed fd 保留、构造失败回收、Stop 拒新、原调用/目标 close 被受控 barrier 持有时 Drain 到期不得完成；释放后原调用实际结束、root close 单次、关闭失败不重试及并发 Drain 原结果一致。
- 原操作成功 seal 与 Stop 两种次序分别控制；错误/格式化/log canary 不暴露输入和材料。纯系统调用错误注入不冒真实 kernel/平台证据。

测试前使用固定 Go1.27.1、任务私有 telemetry off 并清除三个旁路变量、只读模块 cache、自有 GOCACHE 和运行时目录；同进程 fresh≥5 GiB。所有原 Go/测试命令须实际 Wait，任务临时资源及运行目录正常退役。此卡不启动 PG/socket/browser/network，不以本地结果宣称 Mount 授权、生产 RPC、macOS、全部架构或完整 D16 已验。

## 6. 当前验证记录

v1 及 Stop/seal、fd 单次关闭两项补充已落为本卡，非作者 Secret 对完整 SPEC 范围有限接受，无 must-fix；6 个本地链接与格式检查通过。首四实现文件和两测试文件已落盘并格式化，尚未编译或运行，也未获实际源独审。测试明确区分真实临时文件 I/O、受控短 pread/消失条目、受控 held 原调用及模拟关闭错误；不把 syscall 替身当 kernel 故障实证。后续结果按有限输入记录，原失败保留。
