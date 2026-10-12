# D15 Runner 生产入口显式启用 read-file

状态：范围与关闭次序已确认，待实现和验证。本片只把已交付的 `read-file@1` 接入真实 Runner 配置、`cmd` 与 `app.Run`；不代表完整 Agent／Model 工具循环已接通。

依据：[Runner Control](d15-runner-control.md)、[已交 read-file 适配器](d15-runner-read-file-adapter.md)、[本地 Workspace 能力](d16-runner-workspace.md)、[Workspace 契约](../../architecture/runner/agent-workspace.md)、[Control 协议](../../architecture/runner/control-protocol.md)。组合基线为 `41cea003`，不新增迁移。

## 当前缺口与结果

[真实命令入口](../../../cmd/agenteam-runner/main.go)已提供配置检查、stdin enrollment 和进程退出路径，但 [App](../../../internal/runner/app/app.go) 默认构造 `control.New`，没有装配 operation。已交付的 `control.NewReadFile`、Handler 和 Workspace Manager 只能通过显式代码构造组合；直接 control 测试不能证明生产命令已经支持读取。

本片增加 `AGENTEAM_RUNNER_OPERATIONS` 的闭集配置：未设置或空值保持无操作；显式值 `read-file` 只启用 revision `"1"`。未知值、重复项和其他组合整体拒绝。配置经真实 `config.Load`／`app.Run` 传递，不能仅增加测试构造器。`--help` 说明默认空及显式启用方式；`--check-config` 仍只作离线检查，不创建目录、打开运行时 root 或发起连接。

旧显式 `NewReadFile(options, handler)` 的外部 Manager 所有权不变；新运行时配置与外部 executor 不可混用。默认 `control.New` 的空配置不创建 Workspace owner，不公告文件能力。既有 read-file 参数、结果、Tool binding 和 revision 原样保留；本片不启用 write、command、环境变量注入或任意 registry。

## 资源装配与能力公告

1. `Client.Run` 先取得原 identity 文件锁，完成原 `Load`、显式配置与存储配置一致性检查及既有 enrollment 身份准备。显式配置省略时，只使用该锁内 identity 的配置；不以离线 `identity.ReadOnly` 结果构造第二份运行时授权。
2. 仅显式 read-file 模式，从上述同一配置取得 root；按既有 Linux 受信任目录及 openat2 边界打开已存在目录，交给 `workspace.New` 固定实际 fd。Manager 复制 fd 后，装配者关闭自己原 fd；Manager 持有副本直至实际关闭。不自动创建部署 root，不以 Metadata 拼接宿主路径，不新增公共 Root／fd getter。非支持平台或 root 校验失败必须拒绝该模式，不作路径降级。
3. 用此 Manager 构造真实 readfile Handler 与单操作 executor，完成后才允许连接并由原 `connectionHello` 公告唯一 `read-file` capability。构造中途失败必须关闭已取得的资源并保留错误，不能先公告后等待请求失败。每个 Client 只持有一份 Manager，重连沿用同一 pinned root，先等旧 session 实际返回。
4. 请求仍由原认证 WSS、闭集 payload、`Manager.Ensure`、`Handle.Read` 处理，Project／Agent／workspace name 目录隔离与范围读取门复用原实现。配置启用能力不代替 Central 的当前权限、Mount 或 Operation 授权。

## 原 owner 的实际关闭次序

[session](../../../internal/runner/control/session.go) 原 reader 实际返回后才 `workers.Wait`；[Handler](../../../internal/runner/readfile/handler.go) 保持原读取及 `Handle.Drain(context.Background())` 实际返回。本片在 `Client.Run` 的同一退出栈同步执行：session reader／workers 全部返回 → `Manager.Stop` → `Manager.Drain(context.Background())` 实际返回并保留关闭错误 → identity 文件关闭 → `Client.done`。

Manager 的 Drain 可因传入 ctx 提前返回，因此此处不能传已取消的调用 ctx 或 Force ctx，不能另起清理 goroutine 后提前释放 identity。Manager 没有公共 Joined getter；不得用收到 Stop、ctx 到期或关闭 socket 推断它已关闭。部分构造失败也遵守已有 owner 的实际关闭次序。

App 保持原 shutdown／Force 总预算。若其等待预算耗尽，报告未 join／退出失败；不能替仍未返回的 Client 清理、提前释放 identity 或写成功退出。只有原 Client 清理返回才能完成它自己的 done；真实进程终止与正常资源 join 分开记录，不通过刷新预算保证测试通过。

## 实现分域

| 文件域 | 最小职责 |
| --- | --- |
| `internal/runner/config/config.go` 及专控 | 新闭集配置、复制与离线校验；默认空兼容。 |
| `internal/runner/app/app.go` 及专控 | 从真实 Config 传递操作选择，保持原 client owner 与退出预算。 |
| `internal/runner/control/client.go`、新私有 owned-read-file 文件及专控 | identity 锁内配置来源、单 Manager／executor 装配与同步退出栈；旧显式构造互斥。 |
| 同 control 包 Linux／unsupported 小文件 | 打开受信任部署 root、原 fd 归属与平台拒绝；不暴露 Workspace 内部句柄。 |
| `cmd/agenteam-runner/main.go` 及必要入口控 | help／check-config 与真实命令启用方式一致。 |
| 一个真实 CLI／WSS 集成 fixture | 复用 [进程入口 helper](../../../tests/process/runner_control_test.go) 与既有隔离 TLS／Runner fixture，实际编译并启动原命令；不以手工 `control.NewReadFile` 替代。 |

实现者各自负责上述单写域，统一接入者承担装配及退出顺序。Workspace、filesystem、readfile、runnerprotocol 既有产品默认复用；如出现确需修改的接缝，先明确文件 owner。无需改 Central Tool／Mount、数据库、Model runtime、生产 initializer 或旧 toolsHTTP STOP。

## 五项有限验收

1. 真实 Runner 命令读取新配置，经真实 enrollment／持久 identity 与隔离认证 TLS／WSS 公告且仅公告 `read-file`。向真实私有 Workspace 发原 read-file 请求，核原 RequestID／OperationID 与准确读取字节、范围、结果；不得只验证显式 adapter 构造。
2. 重启同 identity 时，仅提供 identity 路径、信任配置及操作选择也能沿存储 root 完成读取；显式配置冲突、无效 root／不支持平台均在能力公告前拒绝。部分构造失败的 fd／Manager 关闭由必要定向控制证明，不伪造锁内 identity。
3. 未设置／空 operations 保持空 capability 和原 unsupported 行为；未知／重复配置拒绝。旧 read-file 显式适配器兼容，write／command／env 均未开放，不重复原文件操作全矩阵。
4. 必要 owner 控制持住实际 reader／close 返回，证明 Client.done 与 identity 不能抢先释放，Manager 关闭错误不被吞，原 App 预算到期不冒 Joined。受控屏障只证明这些返回关系，不冒真实文件系统或网络成功。
5. 真实 CLI／WSS 正向完成后发送 SIGTERM，亲收原进程 Wait，并证明原 session／Handle／Manager 退出和 identity 锁可重新取得；正常 PASS 必须完整收尾。失败、超时与未 join 原件保留，不能自动重试或放宽既有资源门。

基础只选新接缝与受影响旧入口控制，真实链复用已有安全 fixture 与资源方法。运行前记录真实依赖和 frozen 输入，按实际 Wait 报告结果；当前未实施、未运行任何本片产品检查。交付证明限定为生产 Runner 入口到真实 WSS 文件读取，不声称已完成 Central 默认工具执行、Agent loop、Model tools HTTP、Secret 环境解析或完整 D15/D16。
