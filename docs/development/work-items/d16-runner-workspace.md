# D16 Runner Workspace manager

2026-10-11，基线 `7e0a123e`；variables_review 实施，skill_package_review 最终独审，recovery_lead 仅前置契约咨询，root 独占 Git。固定 `20672baa` 已有限独立接受，本地库交付完成，待 root 主线集成。

## 范围与来源

依据 [Workspace §§4–8、10–11、14–15](../../architecture/runner/agent-workspace.md)、[执行运行时](../../architecture/runner/execution-runtime.md)、[Read/List](d16-runner-filesystem-read.md) 和 [Find](d16-workspace-find.md)。唯一新增产品域 `internal/runner/workspace/`，用可信设备根 fd 安全创建或打开 `<root>/<project-id>/<agent-id>/<name>`，返回受 manager 管理的只读 Handle。既有 `filesystem.Root` 承担实际 Read/List/Find；不修改它或引入新依赖。

这是本地库，不解析当前 Mount、不授权、不注册 operation、不接 Control/executor/hello/生产 initializer。协议 `workspace_id` 不是目录名，也不自动等同于 Mount ID 或逻辑 name。默认 Runner executor 仍拒绝，旧 STOP 不变。不实现写文件、命令、Git、依赖安装、技能准备或目录删除。

## 接口与目录规则

```go
type Identity struct { ProjectID, AgentID runnerprotocol.ID; Name string }
New(directory *os.File) (*Manager, error)
(*Manager).Ensure(context.Context, Identity) (*Handle, error)
(*Manager).Stop()
(*Manager).Drain(context.Context) error
(*Handle).Metadata() Identity
(*Handle).Read(context.Context, filesystem.ReadRequest) (filesystem.ReadResult, error)
(*Handle).List(context.Context, filesystem.ListRequest) (filesystem.ListResult, error)
(*Handle).Find(context.Context, filesystem.FindRequest) (filesystem.FindResult, error)
(*Handle).Stop()
(*Handle).Drain(context.Context) error
```

New 仅在构造期间借用 root fd，以 `SyscallConn.Control` 和 `F_DUPFD_CLOEXEC` 获得副本；原 fd 始终归调用方。Manager/Handle 不可复制、不暴露 raw fd、Root 或 host path；Metadata 仅含已验证逻辑 identity。通用 fmt/JSON/slog 固定安全标签，错误为固定类别，context 错误保留 `errors.Is`。

Project/Agent 使用 runnerprotocol 的 canonical UUIDv7 校验。Name 精确沿用 `mount/types.go` 的 safeWorkspace：ASCII `[A-Za-z0-9_-][A-Za-z0-9_.-]{0,254}`，拒尾点及大小写无关的 CON/PRN/AUX/NUL/COM1–9/LPT1–9（包含扩展名）；不 trim、clean 或改写。Runner 不 import Central。

Linux 以 root fd 为锚，三层目录全部拒绝 symlink（包括根内链接）、非目录及子 mount。固定 `openat2` 的 `BENEATH | NO_MAGICLINKS | NO_SYMLINKS | NO_XDEV` 与 `O_NOFOLLOW`，无较弱 fallback；非 Linux 明确 unsupported。新目录申请 0700（仍受 umask 约束）；既有和新建目录都须当前 euid 所有、精确 0700，拒 setuid/setgid/sticky 或多余权限，不 chmod/chown。可信 root 须当前 euid 所有且无 group/other 写权限；可信宿主负责其来源与内容。

逐层从 root 重开完整相对路径；创建使用已验证的父 fd，开后校验实际类型/owner/mode。成功前再从 root 重开三层比较 dev/ino，观察到置换即 conflict。并发 Ensure 同 identity 各返回独立 Handle，允许 mkdir 的 EEXIST 后正式重开校验。成功 Handle 固定原 inode，不按 host path 跟随之后的 rename，也不承诺文件系统快照或抵御拥有 root 写权的恶意宿主。此库不是 OS sandbox。

## 所有权、取消与停止

Manager 同一 admission mutex 登记在途 Ensure 及 Handle Read/List/Find。Stop 单调拒新、取消全部已登记调用并停止各 Root；成功 seal 与 Stop 在同一锁上定序。每个 Handle 可独立 Stop/Drain，不影响同 identity 的其他 Handle。各 native 临时 fd 在原调用退出前单次 close，失败返回零结果。

Ensure 创建 Root 后，发布 Handle 与 Manager 封口在同一锁上定序。Stop 或取消先赢时，原 Ensure 必须实际 Drain 尚未发布的 Root，再退出登记；不能产生脱管 Root。Handle 仅在实际 Root 关闭完成后从 Manager 移除，关闭失败保留为 Manager 最终错误。

Drain 先 Stop，等待原调用、取消后的失败清理及临时 close 全部实际返回。随后每个 Handle 的唯一 owner 同步执行 Root.Drain，最后唯一 owner 关闭 Manager root 副本；关闭结果保留，不按 fd 数字重试。等待者各自遵守 ctx；close owner 进入不可取消的实际 close 后必须等原返回，不另启后台 timeout/cleanup。等待超时不抢关在途 fd、不虚报已 join，后续 Drain 可继续。Stop 本身不等于已关闭。

取消、置换或后续失败可能留下已经创建的目录；失败不代表零文件系统副作用。第一阶段不自动删除目录、递归清理或回滚用户内容。Root 的既有读限额、路径规则、结果脱敏及 trusted-host 边界保持。

## 最小检查

仅本包 race/vet 和任务自己的真实临时目录：Ensure→实际文件 Read/List/Find、Project/Agent 隔离、同 identity 并发、借用 fd 保留、0700 与既有 owner/mode、非法 identity/name、三层 symlink/普通文件/逃逸与置换、错误脱敏、取消残留、Stop 与原 I/O/close 实际 join、并发 Drain 单次关闭。受控 syscall/Root 端口 barrier 只证明对应实际返回所有权，不冒 kernel 故障。无网络、PG、native 服务或新依赖；共享 Go cache 按 root 单 writer 排期，实际结果随后追加。

## 作者验证

产品四文件由 root 保存为首技术检查点；后续仅新增本包三个测试文件。Linux/amd64、固定 Go 1.27.1，离线 `GOMAXPROCS=2`、`GOFLAGS=-mod=readonly -p=2`，复用经 Outbox 明确交接的 SkillHTTP Go 热缓存与既有只读模块缓存，无依赖下载或服务。实际命令：

```sh
go test -race -count=1 -timeout=90s -v ./internal/runner/workspace
go vet ./internal/runner/workspace
```

原 `unit01` 工具 61018 实际 exit 1（Go378768，2.914s）：三个 mode 反例的 `Mkdir(0755)` 被测试进程 umask 收成 0700，原前置并未建立不安全权限。仅 fixture 改为显式 Chmod(0755) 后 Stat 确认；产品和断言未放宽，原 FAIL 保留。

修后 `unit02` 与 `vet01` 原工具 51273 亲收 exit 0。Go379240 race 实际 0/1.971s，10 top、11 sub 全部通过（包内 1.190s）；Go379314 vet 实际 0/0.651s。精确命令、env、原 PID/Wait/日志在 `output/ai/runner-workspace/{unit01,unit02,vet01}/`，源码在各运行期间停写，以检查点及限定差额标识，没有另造全树 manifest。

通过范围含真实临时目录的 Ensure→Read/List/Find、跨 Project/Agent、16 个同 identity 独立 Handle、rename 后原 fd 与新 Ensure 的不同 inode、三层 symlink/file/mode 拒绝、非法 name 零目录副作用、escaping read、借用 fd 保留、受控 owner/平台错误、安全格式化与错误投影、取消后实际残留、置换冲突，以及 Stop 后实际 read/未发布 Root/Handle close/Manager close 返回和单 close owner。受控 `linuxCalls`/Root 装饰均在包内，生产仍直连真实 syscall/Root；不以此声称真实 kernel 故障。非 Linux 拒绝源和专用 build-tag 测试已落，但本轮未在非 Linux 执行。

所有作者 Go 原进程已退出，热缓存已明确交回 Outbox；无后台或 native 窗口占用。最终独审已有限接受，不称完整 D16、当前 Mount 权限或生产 Runner 已可执行。

### 独审关闭失败返修

独审 skill_package_review 在 `d7024b14` 指出：native Ensure 的失败 defer 曾自行 Drain 未发布 Root 并丢弃返回错误，导致 Manager 无从保留关闭失败。原定向反例 `TestUnpublishedEnsureRetainsCloseFailure` 先以真实 Root 实际关闭后投影 ErrUnavailable，控制取消发生在 Root 已创建而尚未返回时；`close-failure-repro01` 原工具92351亲收 exit1（Go385590，1.267s），实际报 Manager.Drain 错误丢失为 nil。原反例 FAIL 保留；它不是 kernel 关闭故障实证。

唯一产品修复是 native Ensure 只关闭自己取得的临时 fd，将已经创建的 Root 连同错误交给仍登记在途的 Manager.Ensure；后者既有唯一路径实际 Drain、保留关闭错误后才退出登记。未改公共 filesystem、预算、路径规则或原成功断言。

修后 `unit03` 与 `vet02` 原工具21505亲收 exit0：Go385770 race 实际0/2.262s，11 top、11 sub 全过（包内1.210s），含原 read/close/未发布 Root 真实返回屏障及新增关闭失败控；Go385832 vet 实际0/0.216s。两源码固定 `20672baa` 后由 skill_package_review 独立核对差额、原 FAIL 与修后原件，唯一 must-fix 已闭合，无新增阻断；没有额外 native、网络或平台矩阵。热缓存再次明确释放。
