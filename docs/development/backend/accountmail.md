# 账号邮件与恢复日志

`accountmail` 已由 Central 生产根装配，消费真实账号投递端口、Secret lease、受控出站连接与受限日志 Sink，执行邀请、密码重置和 SMTP 测试邮件。账户 System HTTP 提供配置、测试、任务安全查询和人工重试；唯一 `account.mail-enqueue` Outbox handler 负责事务入队，独立 Mail Runtime 负责实际投递，没有生产成功开关。其它产品能力仍未齐，诊断保持 `ready=false`。正式规则和组合验收状态分别见 [D07 实施规格](../work-items/d07-account-session-smtp-design.md)与[主卡](../work-items/d07-account-session-smtp.md)。

## 受信组合

先按[账号库说明](account.md)构造 Account/Audit/Secret/Outbox，共用一个 `account.Authority`。`accountmail.NewWorkRegistry(processAuthority)` 使用真实共享 ProcessGuard 的 ProcessID 与 exact-process 死亡验证器；`account.NewDeliveryPort(service, registry)` 与 `accountmail.New` 再组合该 Registry、固定 TrustStore、受控 outbound Client、同一恢复日志 Sink 和固定 PublicOrigin。`accountmail.NewRuntime(service, worker)` 统一拥有两个 worker、容量 100 的队列和维护循环。

不能只提供相同 Actor 数据却为失效操作另造 Authority：SMTP 设置、撤销/兑换邀请、重发、reset 建立/完成、普通改密和到期清理需要与发送共享可取消、writer 优先的 mailAdmission 门禁。完整锁计划在 Tx 外发现，门禁在 DB 事务前取得，事务只一次取得完整最强锁，不在 socket/文件 I/O 期间持有 DB 事务。

`Start(ctx)` 用组合根共享 30s 安全阶段的剩余预算完成 canonical 与恢复；`Check(ctx)` 只做技术检查，不尝试连接 SMTP，未配置 SMTP 不阻断账户初始化。首停先停止全部准入，再以原总期限先 Drain Mail，使协议最后回复、FinishDelivery、lease/Audit 收尾与 worker 真 join，之后才最终 Drain Account Runtime/Core。否则过早封闭 Core 会拒绝 Mail 的最后清理事务。

强停同样按 Mail→Runtime/Core 顺序使用原 force context；前项耗完预算也实际发起其余必要取消，不为每项追加时间。根须核全部 HTTP/Outbox/Account/Mail、材料使用者、Avatar reader 和整个 Sink 真实关闭后才释放共享 ProcessGuard；超时/取消/socket 已关闭均不等于 join。未 join 时 guard 保留至实际停止或 OS exit；共享 DB 最后发起关闭，整体最多额外 1s。Mail 库本身不关闭共享 DB，完整责任见[停止与退出](README.md#停止与退出)。

## 持久事实与材料

`00011_account_mail_delivery.sql` 为 SMTP 设置、mail jobs 和 attempts 增补发送事实，保留旧 `processing` 恢复语义；迁移仅 Up。历史 delivery intents 增加单跳 `origin_intent_id`，只从 exact committed 命令证明原 kind/job/link/initiator/target，坏旧事实导致整体迁移回滚，不按 ID 格式猜来源。

Claim 的第一短事务登记确切 attempt、fence、process、配置版本和不可变 token/credential Ref/LeaseID。事务外发现 Secret 计划，第二短事务重验当前绑定并 acquire 材料 lease；任何未确认提交都不返回可发送材料。即使 Claim 未返回句柄，实际本地操作也已登记：只有 DB 调用真实返回、不可再移交及同原 writer 锁下可信确认，才能证明零外发并收敛。成功返回后即使 Stop/Force 先到，worker 的 finally 仍接管句柄。

Finish 先取得实际 completion，持久关闭该 exact attempt 的材料 acquire（`io_joined=true`），再计划释放原槽并原子写终态/Audit。从未 acquire 的槽只能在这道 fence 后把正式 Secret 顶层 `SECRET_NOT_FOUND` 当零 lease；嵌套 provider NotFound、依赖错和 Apply 失败均保留 checkpoint。失效后 release 不恢复读取权。legacy 空 Ref 仅从保留的原引用/清理事实及 exact Secret lease 核实，多次换密码后也不能拿当前 SMTP Ref 代替。

同 key 的提交未知先等待原 writer 终局，未确认时保留原 Unknown 与 cause。重复恢复按持久 pass/ID 轮转，每批最多 100；活实例/未知死亡保护项不阻断独立项，硬错误仍可观察。邮件成功、未知和 lease 清理是不同事实，不能从 TTL、零行或缺少内存登记推断发送者已停止。

## SMTP 与日志

SMTP 正式支持 `none`、必须 STARTTLS 的 `starttls`、以及连接即 TLS 的 `tls`。固定系统/部署 CA、证书主机校验和受控 DNS/策略/IP pinning 始终生效，无自动降级。每次 AUTH 与 MAIL 分别走 Outbound.BeginSend 和当前 Account BeginDelivery；后者 SH 从短事务校验至底层真实首写成功/失败最多 1s，且保留更短 caller/token 期限。TLS 包装下也以底层连接的实际 Write 为界。

每 attempt 总预算 30s，连接 5s、TLS 10s、读空闲 5s；回复单行 4KiB、累计 32KiB/100 行，消息最多 64KiB，仅一个收件人。错误与 Audit 只保存闭集原因，不保存服务器回复、地址、密码、token 或链接。4xx/可重试网络故障沿持久退避，5xx/证书/配置拒绝不伪报发送；DATA 后丢回复为 unknown。首次加最多五次自动重试，每 job 真实 claim 上限六次，配置降低上限不会重置已用次数。

仅 SMTP 未配置时，邀请/reset 使用 `backend_log`；已配置但发送失败绝不降级。Sink 为受限 0600 文件，最多 32 个排队和一个实际 writer。队首才同步当前授权并消费单次 `GrantOnce`；这个不可撤回资格点不声称 syscall 或首字节已发生。授予后的 Write/Sync 不持 SH，链接仍可被撤销。`ticket.Wait` 超时可返回 unknown，而 `ticket.Done` 只有实际 I/O 终局后才关闭；完整 Write+Sync 才是 written，部分/Sync 故障为 unknown。bootstrap 的旧一次受限尝试保持不自动重印。

部署必须设置 `AGENTEAM_CENTRAL_ACCOUNT_RECOVERY_LOG`，即使已配置 SMTP 也不能省略首次管理员的受限输出渠道。路径、服务 UID、父目录 0700、文件 0600 和禁止符号链接的要求见[账号部署](account.md#部署输入与首次管理员)；`--check-config` 不打开文件，运行时失败阻断初始化。不要将该文件并入普通日志/Audit、HTTP DTO、模型上下文或测试报告；读取、备份与保留由操作者限制。公共邮件链接只基于已校验 `PUBLIC_ORIGIN`，不采信来访 Host/forwarded 头。

## 人工重试与检查

`RetryMailJob` 要求当前 Human 管理员、source job 版本和 stable command key。当前授权先于历史 receipt；首次接受在完整 root/command/User/记录锁下同时增加源 version、创建新 intent/event、写 `smtp.delivery.retry` Audit 和 receipt，不制造 mail attempt，也不与旧 active/unknown attempt 或尚未 enqueue 的同根周期并行。新周期沿 exact 单跳原始 target/password_version/token 期限；retry-of-retry 不形成无限谱系。协议、计量与迁移规则见[人工重试补遗](../work-items/d07-account-mail-retry-addendum.md)。

```sh
export AGENTEAM_GO=/path/to/go1.27.1/bin/go
sh scripts/check-go.sh
AGENTEAM_MINIO_BINARY=/task-owned/cache/minio GOFLAGS=-p=1 \
  sh scripts/test-accounts.sh mail
```

`test-accounts.sh` 不带组名会顺序运行[账号说明](account.md#检查)中的九个固定组。`mail` 沿 `test-objects.sh` 的 owned PG/MinIO/私网 fixture 与原 6m 包限制，SMTP fixture 使用精确 private IP/port 规则、自有 CA、有限真实服务器和 nonce/label/exact-ID 清理；不放宽 loopback 分类或连接外部 SMTP。integration-tag 的 recoverylog writer 接缝只进入测试构建，用真实文件 Write/Sync/Close、Account/Secret/ProcessGuard 核实际 join，不是生产可替换 writer。另有冻结 `/tmp` overlay 验证真实根中阻塞的 Sink Write/Sync/Close，正常双 binary 不含 overlay。Mail31、真实 SMTP final-reply 根组与 Sink5 按各自输入分别形成证据；不承诺 exactly-once 或外部邮箱投递，也不把未命中的旧包当兼容通过，历史 setup 失败原因未定的限制见[主卡](../work-items/d07-account-session-smtp.md#当前进度)。
