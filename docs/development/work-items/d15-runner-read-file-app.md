# D15 Runner 生产入口显式启用 read-file

状态：受测 `7c5e0784` 已完成实现、basic02 的 7 top / 20 sub 与三包 vet、双 binary 编译/list，以及 native01 的 1 top / 3 sub 有限 whole PASS；原 basic01 FAIL 保留。最终文档待保存和主线交付。本片已将 `read-file@1` 接入真实 Runner 配置、`cmd` 与 `app.Run`，不代表完整 Agent／Model 工具循环已接通。

依据：[Runner Control](d15-runner-control.md)、[已交 read-file 适配器](d15-runner-read-file-adapter.md)、[本地 Workspace 能力](d16-runner-workspace.md)、[Workspace 契约](../../architecture/runner/agent-workspace.md)、[Control 协议](../../architecture/runner/control-protocol.md)。原设计基线为 `41cea003`；实际组合包含 main `33feecbd`，受测源为 `7c5e0784ce940ab417ce8a3b9b1119d239aadbcd`，不新增迁移。

## 当前缺口与结果

本片实施前，[真实命令入口](../../../cmd/agenteam-runner/main.go)已提供配置检查、stdin enrollment 和进程退出路径，但 [App](../../../internal/runner/app/app.go) 未装配 operation，既有 `control.NewReadFile` 只能通过显式代码组合。本片现将闭集配置接入原默认构造路径，并以实际编译命令的 PG/TLS/WSS 小链验收；未配置操作仍保持空能力。

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
| [真实 CLI／WSS 集成 fixture](../../../tests/projectvariable/runner_read_file_app_linux_test.go) | 方法预先构建真实命令与 PG 两个 binary，fixture 只启动严格绑定的命令，不在 native 内 Go build，不以手工 `control.NewReadFile` 替代。 |

实现者各自负责上述单写域，统一接入者承担装配及退出顺序。Workspace、filesystem、readfile、runnerprotocol 既有产品默认复用；如出现确需修改的接缝，先明确文件 owner。无需改 Central Tool／Mount、数据库、Model runtime、生产 initializer 或旧 toolsHTTP STOP。

## 五项有限验收

1. 真实 Runner 命令读取新配置，经真实 enrollment／持久 identity 与隔离认证 TLS／WSS 公告且仅公告 `read-file`。向真实私有 Workspace 发原 read-file 请求，核原 RequestID／OperationID 与准确读取字节、范围、结果；不得只验证显式 adapter 构造。
2. 重启同 identity 时，仅提供 identity 路径、信任配置及操作选择也能沿存储 root 完成读取；显式配置冲突、无效 root／不支持平台均在能力公告前拒绝。部分构造失败的 fd／Manager 关闭由必要定向控制证明，不伪造锁内 identity。
3. 未设置／空 operations 保持空 capability 和原 unsupported 行为；未知／重复配置拒绝。旧 read-file 显式适配器兼容，write／command／env 均未开放，不重复原文件操作全矩阵。
4. 必要 owner 控制持住实际 reader／close 返回，证明 Client.done 与 identity 不能抢先释放，Manager 关闭错误不被吞，原 App 预算到期不冒 Joined。受控屏障只证明这些返回关系，不冒真实文件系统或网络成功。
5. 真实 CLI／WSS 正向完成后发送 SIGTERM，亲收原进程 Wait，并证明原 session／Handle／Manager 退出和 identity 锁可重新取得；正常 PASS 必须完整收尾。失败、超时与未 join 原件保留，不能自动重试或放宽既有资源门。

基础只选新接缝控制，真实链复用已有安全 fixture 与资源方法。原输入、实际 Wait 和全部失败材料见[方法及完整结果索引](../../../.agent-state/runner-read-file-app/README.md)。实际结果如下：

- basic01 `28081/247ad7` actual 1：6 top / 19 sub PASS，`cleanup-error` 的原返回屏障未到达，vet 未运行。日志未捕获线程调度；静态定位为缺少实际 reader-entry 同步。`7c5e0784` 只修该测试的同步门，未改产品、方法或预算。basic02 `42212/c2ea87` actual 0：7 top / 20 sub 与三包 vet，78 输入六图/11 outer 同，仅该测试文件相对旧图一 hash 变化。
- 原 profile01/02 均实际离线 PASS，无先前失败；合 main 时 `256e27ac` 遗漏 descriptor helper/两分支，被静态 `4f98bf` 拦住，`88dfa691` 恢复原已审字节。它们不属于 Go/native FAIL。profile03 实际通过 metadata 1 正/7 反、descriptor 1 正/17 反；sequence01 `25933/6d8284` actual 0 完成两个真实 go-list 闭包、双 build、exact list 和 prepare，4291 compile union → 4294 runtime 输入。
- native01 `81653/bcad54` actual 0：supervisor `1226336` Wait 0 / 118.538s，driver/Go 均 0；1 top / 3 sub 15.47s（2.50/2.44/1.35s）。九个 CLI child 的真实 Wait 为 `[0,0,0,0,0,1,2,2,2]`，非零为预期配置拒绝。七资源 14 次缺席、原 descendants/private/runtime 双尾、TCP delta 双空、4294 输入/四身份同、无 race，未重试。实际两次范围读取、identity-only restart、默认空 capability 与进入设备协议/能力公告前拒绝均有原 marker（未监测 TCP SYN，不宣称零 TCP 尝试）；详见[原 post](../../../output/ai/runner-read-file-app/native01/post-check.json)。

验收限定为生产 Runner CLI 入口到真实认证 WSS 文件读取及原资源退出。fixture 当前 Agent/Mount、session 与 Runner 真实，但 `readFileAdapterAuthority` 仍是 strict Operation-authority substitute，不是实际 Execution/ToolCall producer；不能外推 Central 默认工具执行、Agent/Model loop、tools HTTP、Secret 环境解析或完整 D15/D16。原 STOP 保持，本片尚未合入/推送、保全或清理。
