# Runner 身份与控制通道

本分支按 [D15 规格](../work-items/d15-runner-control.md)实现 Runner 管理、一次性登记、Ed25519 设备身份和出站 WSS。当前源码已接 Central 与 Runner 默认入口；连续迁移、管理服务、TLS/WSS 代际与公开 Client 生命周期已有作者限定真实结果；默认双进程退出、独立风险验证和完整平台矩阵仍待验收。共享 wire 与 Linux 身份文件已有局部验证，不能将这些结果视为完整 D15 或可部署结论，验收状态以规格卡为准。

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

首期支持目标是Linux kernel≥5.15及macOS≥14，amd64/arm64；Windows明确不支持。目前仅有Linux局部文件/协议、限定PG和TLS/WSS结果，macOS、跨UID、真实crash和其它CPU保持未验。业务PG测试使用实际Account Bootstrap/Login、同Store/Audit与隔离Postgres；native/真实双入口须分别取得资源窗口。开发中只运行已核实无网络的精确pure selector，禁止递归测试误纳native组。

作者真实 `TestRunnerControlMigration` 与 `TestRunnerControlManagement` 已通过各自断言及实际 Wait、精确资源退役、runtime/descendants、TCP 和输入不变检查；前者限定连续迁移/约束，后者限定管理原意图、同 User 新 Session 与 Audit 原子性。`TestRunnerControlDeviceAndReader` 的设备/代际/自然 lease/闭池断言通过，但原 host TCP 尾有4行未清、外层退出1，整轮仍为失败；原 tuple 未保存，事后资源清空不能补写原归属或 PASS。

作者本人 `TestRunnerControlNativeGeneration` 已在独占窗口实际通过坏 CA/错 hostname、原设备登记/token重放、Origin/nonce拒绝、hello/heartbeat、双 Service 替代旧连接及撤销退役，并完成实际 Wait、资源/runtime/TCP双尾及输入不变检查。`TestRunnerControlNativeClientLifecycle` 后继完整通过真实登记、原10s heartbeat、无token/config重启原key与held回调下Force/身份锁/原Run返回边界，Go/driver/outer实际退出及双资源/runtime/TCP尾齐全。`TestRunnerControlDeviceCompetition`、`TestRunnerControlNativeProtocolRejection`、`TestRunnerControlNativeDeadlines`、`TestRunnerControlNativeIdentityRecovery` 与默认双cmd组仅编译并精确发现，分别待真实验证。当前结果不覆盖全部 COMMIT Unknown、双 cmd、RPC/Runtime 或平台矩阵。
