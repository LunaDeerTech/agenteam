# D16 Workspace Root.Find

2026-10-11。基线 `1d41132a`，分支 `ai/workspace-find-20261011`；作者 recovery_lead，独审 agent_display_settings，root 独占 Git。契约与源码 `3fe35261` 已获限定独立接受，无遗留 must-fix；真实临时目录 Find→Read 和原件核验已完成，等待 root 主线集成。

## 来源与范围

[Runner 执行运行时](../../architecture/runner/execution-runtime.md) 的 find-files 要求按路径、文件名或 glob 查找，返回 Workspace 相对路径，禁止越界。[既有 Read/List 卡](d16-runner-filesystem-read.md) 尚未交付 Find。本片仅补 filesystem 包的真实目录查找，再用既有 Read 读取命中；不绑定生产 Tool、Mount、RPC、Authority 或 initializer，不改变已有 STOP。

唯一源码域为 `internal/runner/filesystem/root.go` 的私有接口增量，以及新 `find.go`、`find_linux.go`、`find_test.go`、`find_linux_test.go`。文档域为本卡与本树 `.agent-state/current.md`。不新增依赖、shell 或宽路径 fallback。

## 冻结接口与匹配

```go
type FindRequest struct {
    Path string
    Pattern string
    Match string
    Limit int
}
type FindResult struct {
    Path string
    Paths []string
    Truncated bool
}
func (*Root) Find(context.Context, FindRequest) (FindResult, error)
```

- `Path` 沿用既有严格 Workspace 相对路径规则，`.` 代表根，必须指向目录；不 trim、clean 或接受绝对路径。
- `Match` 必须是 `name` 或 `path`。前者对每个普通文件的 basename 匹配；后者对完整 Workspace 相对路径匹配。例如 `Path=src`、`Match=path`、`Pattern=src/*.go`。返回路径没有 `./` 前缀。
- `Pattern` 为非空、合法 UTF-8、最多 256 字节的 Go `path.Match` glob，支持 `*`、`?`、字符组与反斜杠转义；拒绝 NUL、连续 `**`、非法语法。`name` 模式拒绝 `/`；`path` 模式拒绝起始 `/`。不支持 globstar，不将模式作为打开路径。点文件参与匹配。
- 只返回普通文件，目录、symlink、设备和其他类型不作为命中。递归按实际目录枚举顺序选择命中，再按路径 UTF-8 字节排序返回集合；不提供跨调用稳定的前 N 项、游标或快照。

## 安全、预算与结束语义

- 起始 `Path` 沿用 Read/List 的 contained symlink 解析；遍历得到的子项一律先 `Fstatat(AT_SYMLINK_NOFOLLOW)`，symlink 不追踪（包括内部链接及循环）。
- 子目录从同一 Workspace root fd 以完整相对路径、原 `RESOLVE_BENEATH | NO_MAGICLINKS | NO_XDEV` 及叶节点 `O_NOFOLLOW` 打开，再核实际 fd 的目录类型与刚观察的 dev/ino。消失或替换为冲突；不从已打开子目录 fd 重新解析路径，不引入 `EvalSymlinks`。
- 保留已打开 fd 的原对象语义；不声称并发重命名下路径始终指向同一对象，也不声称这是 OS sandbox 或硬链接隔离。后续 Read 仍重新按 Root 边界解析。
- `Limit` 为 1–128。只有实际观察到第 `Limit+1` 个匹配普通文件才成功返回 `Truncated=true`；完整扫描且未见额外命中为 false。
- `MaxFindVisited=4096`：累计每个非 `.`/`..` 目录项，包含不匹配文件、目录、symlink 和其他类型；观察到第 4097 项即 `ErrLimit`，不发布部分结果。
- `MaxFindDepth=32`：起始目录深度为 0；需要进入深度 33 的子目录即 `ErrLimit`。每目录固定 4 KiB 读取缓冲与当前批次名称，深度优先，最多同时持有 33 个目标目录 fd；不预载完整目录或树。
- 每个观察名称沿用既有 UTF-8/component 校验；非法名称为 `ErrType`。完整拼接路径超过既有 4096 字节限制为 `ErrLimit`。结果完整私有 JSON 投影沿用 `MaxResultBytes=512KiB`，超过即 `ErrLimit`。预算错误与任何 I/O 错误均返回零结果，不能以 Truncated 掩盖未完成扫描。
- 整次遍历使用原 Root `begin/finish` 的一个 operation。Stop 取消，Drain 等待原 syscall 和每个实际 close 返回后才关闭根；不拆 goroutine 或伪造超时终态。所有目标 fd 在退出前实际关闭，close 不重试，失败为 `ErrUnavailable`；取消仍服从原 finish 的线性化语义。
- request/result 的通用格式化、日志及公开 JSON 编码沿用包内固定安全标签；真实数据只由调用者读取字段，预算使用内部投影。

## 最小验收

冻结差额后，按资源窗口运行精确 Go 1.27.1 的包级测试、race 与 vet；无数据库、浏览器、网络或生产服务。必要控制：真实临时树的 name/path/dot/Unicode 查找与命中后 Read；实际 Limit+1 截断、完整扫描、空结果；深度/visited/完整 JSON 预算；内部/外部/循环 symlink 不追踪及起始 contained symlink；目录替换/异常名称/I/O 与 close 错误归零；持有原 dirents 或 close 的 Stop/Drain 不提前释放、并发调用不共享目录偏移。匹配语法与安全标签单测覆盖接口边界。

## 作者验证与恢复

源码 `3fe35261` 未修改后顺序执行以下两个命令。精确工具为 `/workspace/toolchains/go1.27.1/bin/go`（`go version go1.27.1 linux/amd64`）；`GOMAXPROCS=2`、`GOFLAGS=-mod=readonly`、`GOTOOLCHAIN=local`、`GOPROXY=off`、`GOSUMDB=off`。只读模块缓存为 `/workspace/agenteam-skills-milestone-resume/output/ai/skills-milestone-resume/go-mod`，私有构建缓存为本树 `output/ai/workspace-find/go-build`。本片没有依赖下载或共享缓存 writer。

```sh
/workspace/toolchains/go1.27.1/bin/go test -race -count=1 -timeout=90s -p=2 -v ./internal/runner/filesystem
/workspace/toolchains/go1.27.1/bin/go vet -p=2 ./internal/runner/filesystem
```

- race：原工具 `12782` 实际 exit 0；Go PID `271784` 原 `p.wait()` 为 0，22.103 秒。20 个 top（新增 Find 9 个、既有 11 个）全部通过，包内报告 1.625 秒；不另跑同断言的普通 test。真实 tempdir Find→Read、根内/外/循环链接、目录置换、4096/4097 visited、32/33 depth、真实等待 dirents/close 的 Stop/Drain 及并发目录偏移均在本轮运行，不只是组件或纯匹配器测试。
- vet：原工具 `81217` 实际 exit 0；Go PID `273680` 原 `p.wait()` 为 0，11.096 秒。
- 两轮原 `invocation.json`、日志、`result.json`、`inputs-before.json` 与 `inputs-after.json` 分别在 `output/ai/workspace-find/race01/`、`vet01/`；所有 filesystem Go 源码与 `go.mod/go.sum` 的前后 SHA 逐项相等。没有重写原退出码或新增验证框架。

独审 agent_display_settings 已逐段核实现，并亲读两轮日志/result、比对当前 11 个 Go 文件与 `go.mod/go.sum` 同四份前后清单逐项相等；接受上述有限成果，无遗留 must-fix。原工具 Wait0 来自作者亲见，独审没有冒称重新运行。无需重复已通过输入。

当前全部自有 Go/cache 任务已实际退出，没有服务、浏览器或 host TCP 观测窗口占用。结果只证明本地 Linux Root 的 Find/Read 与所列生命周期/边界；不代表生产 Mount/Tool/权限装配、完整 D16 或非 Linux 动态验收。
