# D16 list-directory / find-files 有限适配器

## 当前CLI接线

原目录 Handler 已接到真实 `cmd/agenteam-runner` → Config → App → `Client.Run`。`AGENTEAM_RUNNER_OPERATIONS` 是仅含 `read-file`、`list-directory`、`find-files` 的显式集合，允许任意子集与顺序，默认空；未知、重复、空项与空白拒绝，不把旧单项阶段限制解释为长期互斥 profile。Config 与 Control 使用同一有限 parser，Hello 按固定顺序仅公告选中能力；目录 Handler 内两种能力仍各自经过选择门。原显式构造器的调用方资源归属不变，不与配置装配混用。

所有所选 Handler 共用一个由原 identity 锁定配置打开的 Workspace Manager；保持 session/worker 实际返回 → Manager Stop/Drain 原返回 → identity Close → Client.done 的顺序。原默认空与单独 `read-file` 兼容，不新增 Grant、模型工具 HTTP、write/run、Data Channel 或 Agent 执行循环。Runner 是系统级资源，项目 Owner 给 Agent 配置 Runner 与资源目录名称；挂载配置不替代 Execution/Operation 授权。

新增 `TestRunnerDirectoryApp`（[真实 CLI 验收源](../../../tests/projectvariable/runner_directory_app_linux_test.go)）要求方法预构建的真实 `AGENTEAM_RUNNER_BINARY`，不在测试里构建/跳过。复用正式系统 Runner、Owner Agent/Mount、TLS/WSS 与原进程回收夹具：目录组合登记并读 List/Find，原身份重启后组合 read-file；单 List 拒绝未选能力，默认空且不打开缺失 root；离线检查、坏 root、显式身份冲突、未知与重复项在设备协议/能力公告前拒绝。四个实际在线子进程及五个离线/拒绝子进程均须取得原 Wait；正常 SIGTERM 还核真实 handler/连接退役、原身份锁释放。上游 Operation 继续明确为 D15 §7 严格替身，不声称生产 ToolCall/Agent loop 或零 TCP SYN。

当前接线已完成有限非作者源码审与真实验收，技术候选为 `c5c3f22dba09bfcc3bf3319cb9773de850139b48`。[定向基础结果](../../../output/ai/runner-directory-cli-delivery/basic01/result.json)原 `13989/517f19` 实际退出 0：race 为 12 top / 46 sub（含原两项 CLI），4 包 vet 全部通过；[双二进制编译结果](../../../output/ai/runner-directory-cli-delivery/compile01/result.json)原 `93168/c724d9` 实际退出 0，分别构建真实 Runner 命令和唯一 PG selector，没有在测试内另建命令。

[首次真实结果](../../../output/ai/runner-directory-cli-delivery/native01/result.json)及[原日志](../../../output/ai/runner-directory-cli-delivery/native01/browser.log)原 `41374/7c8f18` 实际退出 0：`TestRunnerDirectoryApp` 1 top / 2 sub 共 19.94s（两子例 2.82s / 4.32s），[supervisor 原 Wait](../../../output/ai/runner-directory-cli-delivery/native01-outer.json)为 0 / 77.903s，Go 原 Wait 0。9 个 CLI 原 Wait 精确为 `0,0,0,0,0,1,2,2,2`；正式登记、身份重启、真实 List/Find/组合 read-file、单项能力拒绝、默认空及设备协议前拒绝各原成功标记均到达，六个自有资源、私目录与剩余子进程全部清退，后验 `aaace2` 确认输入与技术候选不变。默认空未打开缺失 root，负例只证明设备协议/能力公告前拒绝，不宣称零 TCP SYN。Project setup 仍用 Project Service 与持久测试 Skills receipt，Operation 仍是上述 D15 §7 严格替身；本次不证明完整 Execution authority、生产 Skills 初始化或 Agent 工具循环。下方原显式适配器交付历史不改写。

## 原显式适配器交付


状态：本片有限交付，基线 main65fc8687；源码/方法、基础 race/vet 与首次真实 PG/WSS 整轮已由 runtime_recovery 独立验收，无 must-fix。不代表整个 D16 或生产 Agent 工具链完成。

依据：[执行运行时 §4/8](../../architecture/runner/execution-runtime.md)、[Agent Workspace](../../architecture/runner/agent-workspace.md)、[本地 List](d16-runner-filesystem-read.md)、[本地 Find](d16-workspace-find.md)、[已交付 read-file 接线](d15-runner-read-file-adapter.md)。

## 合同与边界

复用已验当前 Mount→明确 Committed 后派发→原认证 WSS→真实 Workspace.Manager 的调用方向，新增显式可构造的 list-directory 与 find-files revision `"1"`。默认 New/cmd nil executor、生产 initializer/ready/full registry、DirectText no-tools 与全部旧 STOP 不变；Execution/Operation 原授权提供方仍未生产绑定，首真实链明确沿 D15 §7 严格替身，不将 Snapshot/material 当执行许可。

- payload 为可信 `{workspace_name,arguments}`；name 仅来自同事务核验的稳定 PG 映射，模型只提供 arguments。`list-directory` 的 arguments 精确 `{path,limit}`，`find-files` 精确 `{path,limit,pattern,match}`。不接受额外字段、重复键、null、绝对路径或宽路径 fallback。
- path 沿 Workspace 相对路径，`.` 代表根；limit 为 1–128。Find 沿原 Go path.Match、name/path 两模式、最多 256 字节、无 globstar、点文件参与；原 4096 visited、32 depth、路径/component 限额和不追踪遍历 symlink 全部不变。
- List 返回 `{path,entries:[{name,kind}],truncated}`，kind 为 file/directory/symlink/other；Find 返回 `{path,paths,truncated}`。数组可空但不为 null，按原 UTF-8 字节排序且无重复；Find 返回真实 Workspace 相对普通文件路径并符合原 pattern 与起点。完整编码 JSON 上限 512KiB。
- Truncated 只保留 Root 实际观察到额外命中的含义；预算耗尽、非法名称、IO、取消或关闭失败不能降为部分成功。返回 truncated 时已给足本次 limit。每个请求持有原 Ensure/Handle/操作直至 List/Find 与实际 Drain 返回。
- 独立 DirectoryIntent/Authority 和私有单次 ticket 绑定完整 operation/参数/P/A/E/Operation/Mount；当前 Agent 版本、完整 refs、active definition、稳定 mapping 在同 Store/完整 lock union 内核验。只把已验 read-file 的当前事实读取提取成同包私有 helper，保其原身份、顺序和错误语义，不把目录操作伪装为 read-file。
- 原事务明确 Committed 且实际返回后才进入原队列。clear 拒绝新 claim，已 claim 调用不重新解析；实际 write 保原当前连接代际门。Central 在取消后等原 terminal 或真实连接退役，且原 frame.done 实际返回，不将 Cancel 入队当 join。

## 所有权与必要验证

recovery_lead 独占 Central Mount/HTTP、两原 harness 必要 selector、卡/current 并负责组合执行；runner_read_file 子作者独占共享 directory wire、新 Runner directory handler 与 Control 显式构造窄接线、专属控和新真实 PG/WSS fixture。Git 仅 root，独审 runtime_recovery。无新迁移或依赖。

先检查两 operation 严格 codec、实际文件目录/匹配/空与截断、拒绝/越界、当前授权/票据操作绑定和真实取消/关闭等待。随后首链复用正式普通 Owner、Runner 注册/认证 WSS 与受管目录，比较独立预设内容、原响应关联、clear 阻新 claim/readd 稳定映射；上游授权替身与真实域分别记录。不重复已验全 D16 矩阵，不把编译、方法控制或旧 read-file 通过冒成本片行为。

当前 Central、Runner 和唯一 PG fixture 已有限独立静审，无 must-fix。两原 harness 仅扩 `TestRunnerDirectoryAdapter` 元数据，离线控制已实际 1 正、7 反通过，剥新增键后旧整 AST 与有效映射保留。原件为 `output/ai/runner-directory-adapter/checks/profile-controls01*`。唯一 PG top 无子测试；它复用原注册/WSS/关闭 helper，预设两种目录读取、空结果、截断、symlink 不追和 clear/readd，未绑定的上游与生产入口边界不变。

基础检查原工具 `22934` 已实际退出 0。六包 `go test -race -count=1 -timeout=120s -p=1 -v -run '^(TestDirectory.*|TestReadFile(Ticket|Claim|Dispatch|Explicit|Control|Client).*)$'` 实际运行 20 个新 top 与 8 个受影响旧 top，全 PASS，Go 原 Wait 0/63.939s；`mount/contract` 只编译、无匹配测试。随后同六包 `go vet -p=1` 原 Wait 0/49.344s。范围为 `central/mount/contract`、`central/mount`、`central/runner/http`、`runnerprotocol`、`runner/directory`、`runner/control`（均在 `internal/`）；精确 argv、max1/offline 环境、77 个相关输入前后相等与原日志在 `checks/unit01-*`、`unit01.log`、`vet01-*`、`vet01.log`。没有重复原 Filesystem 全矩阵，也没有将基础控冒作真实 PG/WSS 验收。

首场景编译原工具 `58087` 实际退出 0：`go test -race -tags=integration -c -p=1 -o <candidate> ./tests/projectvariable` 原 Wait 0/119.550s，随后 `<candidate> -test.list=^TestRunnerDirectoryAdapter$` 原 Wait 0/1.273s，仅一个精确 top。1787 原编译输入前后相等，原件在同任务 `compile01/`；`native01/` 沿原监督器配方，运行闭包仅追加真实 binary。

首次 `native01` 原工具 `21942` 已实际退出 0：唯一 `TestRunnerDirectoryAdapter` 11.12s 通过，原 Go/driver Wait 0，supervisor 实际 0/100.470s；原七资源双 absence、descendants/private/runtime 双尾、HOST_TCP 自动基线差额双空及1788输入不变齐全，未重试。原件为同任务 `native01/{invocation,plan,input-hashes,terminal,tool-terminal}.json`、`outer.log` 和 `/tmp/rda01/pg-c1d4d85caef14807b4bbcfe1664b0643.log`。本片没有失败检查，不另造恢复 patch。

实际首链从正式普通 Owner 配置与当前 PG 映射开始，经过 Runner 首次材料/登记/认证 WSS，调用真实 Manager.List/Find。8 个独立预设读取覆盖 kind/sort、name/path glob、空结果、实额外命中截断、恰限不截断及 symlink 不追；formal clear 同时拒绝两种未 claim 旧票及新 Resolve，readd 保持完整稳定映射后两种操作再次实读且原响应关联独立核对。退出沿真实 Client.Run/Drain、Manager、Runner service/HTTP 和原物理关闭。Project 创建仍使用持久化 test Skills receipt；上游 Execution/Operation 是 D15 §7 严格提供者替身。取消/Unknown/能力拒绝按上述包级控范围接受，不将本次 PG 正常链外推成故障全矩阵、默认 App 或真实 Agent Tool 执行。
