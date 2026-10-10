# Runner 身份与控制通道

迁移00026对应的 Linux/amd64 Runner identity/control 空 registry 已完成限定实现与验收，详见 [D15 规格](../work-items/d15-runner-control.md#101-linuxamd64-有限-00026-验收结果)。Central 与 Runner 默认入口已接设备管理、一次性登记、Ed25519 私有身份和出站 WSS；作者业务、独立风险补集、正式 OS 三格及新 main 组合的双 cmd/CLI 结果按明确版本复用，不能称当前 HEAD 一次全量或完整 D15 通过。

生产 operation registry 为空，hello 的 capability 列表相应为空。设备在线不代表可以执行命令、访问 Workspace 或使用 Data Channel；Agent Mount、D16 operation、D17 数据面和 D18 Tool Runtime 的真实绑定仍是独立集成门槛。整体 `ready=false` / `/readyz` 503 的既有限制不变。

## 配置与私有身份

Runner 只读取下列 `AGENTEAM_RUNNER_` 环境变量；未知字段、存在但为空的值均拒绝，不自动读取 `.env`。Token 不接受环境变量或命令行参数。

| 变量 | 语义 |
| --- | --- |
| `IDENTITY_FILE` | 必填，绝对、clean 私有身份文件路径；直接父目录必须已存在 |
| `CENTRAL_URL` | 首次登记时与下两项一同必填；HTTPS origin，无路径前缀、query、fragment 或 userinfo |
| `ID` | 系统管理员创建 Runner 时确定的 canonical UUIDv7 |
| `ROOT_PATH` | 与 Central 记录一致的绝对、clean Linux/macOS 工作根；创建后不可修改 |
| `CA_FILE` | 可选部署 PEM CA 文件，追加到系统 trust roots；不关闭证书链或 hostname 校验 |
| `LOG_LEVEL` | 默认 `info`，仅 `debug/info/warn/error` |
| `SHUTDOWN_TIMEOUT` | 默认 `10s`，100ms–5m；第一次停止后的原总预算 |

已有身份时可只提供 `IDENTITY_FILE`；若提供 `CENTRAL_URL/ID/ROOT_PATH`，三者必须齐全且与文件完全一致，不能借修改环境静默换设备。TLS 最低1.2；重定向拒绝。Runner 到 Central 的出站连接不经过浏览器 Session 或 CSRF，不把设备签名附在 URL。

身份直接父目录必须当前有效 UID 所有、0700；文件为当前 UID 所有的0600 regular file、nlink=1。祖先目录不得有符号链接或被其它用户写入。文件写入使用已验证 directory FD、同目录独占临时文件、file fsync、原子 rename 和 directory fsync；不能先截断旧身份。专用 sidecar flock 持续到实际服务退出，防两个进程同时修改或使用该身份。

首次登记先在本地原子保存 `pending` key，再向 Central 提交一次 token。只有已知成功或原 key 的后续真实 challenge/connect 被接受，才收敛为 `active`。丢失响应时保留 pending，不重新生成 key、不自动重复 token。鉴权仍失败时安全退出，由管理员显式签发新登记材料；不能覆盖一个仍持锁运行的身份。

```sh
# 只验证已配置身份和 CA，不联网、不取得运行锁、不登记。
agenteam-runner --check-config

# 从标准输入读取一个登记 token；不把 token 写进 shell 命令或环境。
agenteam-runner --enroll

# 使用私有身份进入出站控制连接及重连循环。
agenteam-runner
```

`--help` / `--version` 不加载配置。`--check-config` 输出 `scope=d15`，连接、认证与 ready 均为 false；配置通过本身不证明 TLS、Central 当前授权或操作可用。

## Central 管理与恢复

系统管理员使用 [Runner OpenAPI](../../../api/openapi/runner-control.json) 的 `/api/v1/system/runners`。设备属于 System，不属于 Project；当前 Session/admin 在每个实际事务中重新验证，管理 mutation/Lookup 仍要求原 Account HTTP 的 Cookie/CSRF 边界。无硬删除 API。

Create 同时创建离线记录和一次登记 token；Update 只修改 name/description/tags；enrollment 显式撤销旧 key、token、nonce、连接并签发新材料；revoke 只撤销。两种安全操作即使当前 key 已空，也推进 credential generation 和 metadata version。普通 heartbeat 不推进 metadata version。

所有管理写入使用唯一 `Idempotency-Key`，同 User 新 Session 可凭**首次原 DTO**执行 Lookup 或明确重放。不得从当前对象重新拼一个“等价”意图；不同字段 presence、版本或值可能改变摘要。已提交历史回执不随当前对象变化；同 key 异意图冲突。首次已知提交的 Create/enrollment 响应可以包含 token，重放和 Lookup 都没有 token；响应丢失后确认原命令，再用新 key 显式发起 enrollment 取得新材料。

登记 token 为32随机字节、base64url无padding、10分钟有效；Central 只存摘要。设备 challenge 独立30秒，每设备最多4个未过期挑战、全局16384；nonce一次消费。签名绑定 Runner ID、nonce、canonical Unix seconds 和固定协议前缀；DB时钟允许±30秒，错 key、错 Runner、旧代际或重放都拒绝。

Admin GET/HEAD 和 Lookup 原生期限2秒，写入30秒；设备 challenge 2秒、enroll 30秒，WSS认证/升级5秒。请求 body≤32KiB，单记录≤64KiB、列表≤2MiB。超时、断流、short write 或原 body Close 失败走真实 abort，不发布虚假的未提交 Problem。`COMMIT_UNKNOWN` 保留原 Attempt/Cause；取消或断开不能证明回滚。

## 在线状态与连接所有权

online 是当前数据库连接的有界视图：hello 已完成、两种 generation 均匹配、原30秒 lease 有效。已知退役、generation失效或lease到期为offline；数据库连接/借用不可用时报错，不合成offline。另一个 Central 进程持有有效连接时，本地 registry 没有记录也不能将设备判为offline。

未知进程崩溃或网络分区不具有即时死亡证明，其保守online视图最多保留到最后一次续约的原lease到期。Central 重启产生新 owner，不接管旧连接、不恢复旧 RPC。当前只支持连接 owner 所在进程 dispatch，无跨 Central 转发；调用不能因为本实例没有 socket 就修改持久状态或宣称 HA。

每次认证消费nonce并推进持久 connection generation。真正 socket write 与入站状态发布在≤5秒的当前代际门中；替换后旧连接不能发布成功、续约或清除后继。旧 socket 在≤1秒的代际观察中关闭；此期间未确认结果可以保守unknown。

协议固定 `agenteam.runner.v1`、v1.0、13种消息。只支持text，完整fragment合并消息≤1MiB，禁止压缩。单FIFO最多256条/4MiB，其中32条/256KiB留给控制消息；已租借writer仍计容量。hello≤5秒，heartbeat间隔10秒/超时30秒。重连上限依次1/2/4/8/16/30秒full jitter，稳定active60秒后重置。

## 停止与验证范围

Central 的 Runner owner 位于 Account/DB 之前停止，拥有升级后的 socket、reader/writer、连接观察器和实际事务回调。`http.Server.Shutdown` 不负责已hijack的WebSocket；close、context取消或HTTP active归零都不能替代这些尾部真实返回。Force沿root原context/deadline，不能刷新预算或把未join标为成功。

Runner 停止新请求、取消已接收请求及重连，再等待实际运行时、socket/worker退出，最后释放身份文件锁。当前D16/D17 owner为空只表示未绑定，不是其退出验证。局部状态日志仅使用固定状态和布尔值，不打印token、签名、私钥、raw public key或配置值。

首期平台目标仍为 Linux kernel≥5.15及macOS≥14、amd64/arm64；本次实际接受只到 Linux/amd64。macOS、其他CPU、跨UID与断电持久性未验，Windows不支持。有限结果不解除既有Object runtime join等停止项，也不补实际操作、Mount、D16/D17/D18绑定。

作者已完成连续迁移、当前管理权限/命令恢复、设备认证/代际、自然lease、公开Client生命周期和有限协议风险组。真实进程Crash分两原窗口覆盖pending尚未登记、后端已登记但本地仍pending；保留原key，无token重启不自动登记重放，已提交身份经同key challenge恢复。Central实际Crash后Reader保守保留原lease至自然到期，再同key重连；数据库不能读时不制造offline。独立管理风险为Concurrent三子、CommitUnknown两子和修后LogoutOrder两子，合计七子；外部降权负事实不代表尚无公开API的RoleChange生产命令已验。

Linux正式OS测试使用真实默认cmd、自有PID/starttime/exe、blocking fd0 pipe和双SYS_read见证。EOF退出Wait0；实际第二信号及原3s+1s期限退出Wait1，强退两格仍Read未join。另一次new-main默认双cmd真实验证完整1..26迁移、stdin登记、在线、锁竞争、无token同key重启、撤销/再登记、正常退出和实际数据库尾；七资源、private/runtime/desc、TCP双尾及输入不变齐全。组合六owner的未join/Force/late-install由独立纯控制补证，默认进程case不冒六owner同时held。CLI/config、held TLS challenge中正常TERM/INT与依赖方向三个原回归也在同交付候选实际完整通过。

历史失败不被后继结果改写：原DeviceAndReader的TCP四行缺原tuple/归属；原四组组合的Protocol top残留物理连接缺身份；原B首子未读POST导致handler join未完成；原OS01/02方法失败；作者Default01的同baseline tuple由ESTAB转TIME_WAIT、无PID归属，整轮仍FAIL。修后Protocol单组、A+DeviceReader、B首子、正式OS以及new-main Default分别取得后继限定合格结果；没有修改原TCP门或把事后清空补成旧轮PASS。具体原始边界与组合证据见D15卡。
