# D15/D16 单 read-file 可构造适配器

状态：本片已交付 `main`，有限实现、基础检查与隔离真实链已通过独立验收；不代表完整 D15/D16 或真实 Agent Tool 执行闭环。

依据：[D15 Control §7](d15-runner-control.md)、[D01 Runner 边界](d01-contracts/model-tool.md#runner-与-mcp-通信边界)、[Workspace](../../architecture/runner/agent-workspace.md)、[执行运行时](../../architecture/runner/execution-runtime.md)、[D16 本地文件能力](d16-runner-workspace.md)。

## 提供与调用

普通 Owner 正式配置 Agent Runner Mount 后，显式装配 `mount.NewReadFileResolver` 和 `runnerhttp.NewReadFileDispatcher`，通过当前 PG 映射、私有单次 ticket 和原认证 WSS 调用 Runner `control.NewReadFile`。Runner 使用真实 `workspace.Manager.Ensure`、`Handle.Read` 和实际 `Handle.Drain`，返回对应 terminal。系统 Runner 管理仍为 Admin，项目配置不增加 Admin 门。无新迁移或依赖。

真实 ToolCallBinding 当前只接受 skill.install，OperationExecutionAuthority 无生产 producer，DirectText 无 tools。本适配器必须注入可信当前 Execution/Operation 授权提供者；首链按 D15 §7 使用严格替身，不把 Actor、DTO、Snapshot、历史 material 或构造接口当授权。默认 cmd/New 的 nil executor、生产 initializer/ready/full registry 与全部旧 STOP 保留。

## 工程合同

1. 固定 operation `read-file`、revision `"1"`。arguments 仅相对 path、canonical 非负 int64 字符串 offset、1–65536 length、utf8/base64 encoding。可信闭集 payload `{workspace_name,arguments}` 的 name 由 PG 稳定映射填入，绑定 P/A/MountID/WorkspaceID/Runner/Mount version；模型不供 name，UUID 不作目录名，不接绝对宿主路径。
2. Resolver 注入同 Store 当前 Agent reader 和可信 Operation authority。完整 P/A/E 与授权方贡献的锁 union 内核原 running/Operation/参数授权，再核 Agent version/完整 allowed IDs、Mount head/refs 精确集合、active definition/唯一 tuple/稳定 workspace mapping。仅私有字段 ticket 可进入派发。Mount 契约只依赖身份与操作输入；Agent Current 请求转换位于 Mount 实现，实际 reader 和 `RequiredLocks` 沿原接口。
3. 派发在原业务 Tx 重核票据并建立单次 claim。只有原 Tx 明确 Committed 且实际返回后才进入原队列，失败或 Unknown 不发送、不重试。claim 是 operation 开始线性化点：清空配置挡新 claim，已 claim 调用不重新解析映射。业务锁不围 WSS I/O；真正 write 继续原 Runner 单连接代际门和原 5 秒有界事务，不增加全局锁。
4. OperationID 不变，每实际 wire attempt 新 RequestID。显式 `NewReadFile` 才装配单操作；Runner 全部校验后 Ensure，读取和原 Handle close 实际返回后才发 terminal。Manager 生命周期归装配 owner。取消可留已建空目录，不承诺零副作用。
5. Central 发原 Cancel 后继续持有 call，直到原 terminal 或原连接实际退役，并等原 frame 实际完成。断连未得 terminal 是 Unknown；cancel ack 不是完成。六字段结果 path/offset/bytes_read/eof/encoding/data 仅显式编码，默认格式化/JSON/log 脱敏；错误只固定 code/safe_message。

## 已验证范围

产品在 Mount resolve、Runner HTTP dispatch、runnerprotocol、Runner readfile/Control 的窄接缝实现。`runtime_recovery` 独立审阅固定产品、首真实方法和实际结果，未参与实现。

基础检查只覆盖受影响六包：`go test -race -count=1 -v -timeout=90s -run '^(TestReadFile.*|TestHandler.*)$'` 首轮 Runner 三包 12 top 通过，但 Central 三包因新增 Mount contract→Agent contract→Mount contract 循环引用 setup FAIL（原工具 10167 actual1）。移除该反向依赖并保持相同 canonical ID/AgentRun 同 P/A/E 条件后，仅补 Central 5 top/11 sub；再对六包执行 vet，原 40580 actual0。合计 17 top 分段通过，不伪称首轮全绿；取消等待原 reader/Handle close、错误与错票据、当前变更/Unknown 零发送、默认未绑定、真实本地路径隔离等范围由这些专属控证明。没有重复旧 D16 全矩阵。

首真实 fixture 为 `tests/projectvariable/runner_read_file_adapter_test.go` 的唯一 `TestRunnerReadFileAdapter`，无子测试。原 race-c/list 60749 actual0。它复用正式非 Admin Owner/P2/Agent PG fixture，另以正式 Runner 首次 Create 的有效一次性物料完成真实 enrollment/identity/WSS，验证实际 UTF-8 字节与范围；正式 clear 后旧未 claim 票和新 Resolve 被拒；正式 readd 保持 Mount/Workspace/Runner/name 后再次实读，RequestID 更新而 OperationID 保持。Project 初始化仍依赖原 persistent test Skills receipt；运行中 Execution/Operation 是严格 same-Store、完整 held locks、同 Tx 私有 live witness 替身。当前 Agent reader、Mount SQL、认证连接和文件实现均真实。native 不宣称已执行取消/网络故障或真实 Agent Tool 链。

两原 harness 只在 metadata 字典追加精确入口。离线入口控制 1 正 7 反通过，剥新增 selector 后完整 AST 和旧有效映射保持。native01 唯一 top 10.83s PASS；原工具 6867 作者亲见 actual0，supervisor 101.171s，Go/driver 原 Wait0。原七资源十四次 absence、desc/private/runtime 双尾、75 秒原自动 baseline/TCP delta 双空与输入不变全部通过，独立审阅据原件接受。没有修改尾部门、人工 baseline、过滤连接或循环试到通过。

原命令、Wait、输入和结果保存在 `output/ai/runner-read-file-adapter/{checks,compile01,native01}/`；原日志 `/tmp/rrf01/pg-2e475a01b66c495fa7e0cd4fd712b112.log`。首 unit01 FAIL 原件保留。只交付可构造有限适配器，生产授权提供者与默认执行入口仍未绑定。
