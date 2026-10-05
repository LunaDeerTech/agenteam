# 受控出站

`internal/central/outbound` 提供 DB 策略、固定地址分类、完整 DNS 审批、HTTP/1.1 client 和 SMTP 受控连接端口。全局迁移为 `00004_outbound.sql`。生产组合根已绑定真实 Account Session/SystemAdmin 授权并载入策略；D07 的 [accountmail](accountmail.md) 已用该端口执行 SMTP 协议。出站策略管理 HTTP、Provider/MCP SDK 与 Project 授权仍待后续绑定；当前没有匿名策略 API 或默认成功的授权实现。

产品规则见[出站架构](../../architecture/platform-infrastructure/outbound-network-policy.md)，工程接口与验收见 [D04 实施规格](../work-items/d04-security-design.md)。此实现依赖正式部署的单 Central 前提。

## 配置、状态与停止

`AGENTEAM_CENTRAL_OUTBOUND_CA_FILE` 可选，省略时使用系统 trust roots；设置后追加文件内 PEM CA，显式空值视为配置错误。文件必须可读、仅包含有效 CA certificate PEM，最多 1 MiB；不接受私钥、非 CA 证书、无 CA 或额外正文。它与数据库 CA 独立，无 skip-verify、业务自带 CA 或代理开关。配置加载后保留不可变 trust store，不保存文件路径。`--check-config` 读取并验证 CA，不连接网络或宣称 DB policy/canary 已初始化。

数据库启动阶段后，cursor/Audit、Secret 初始验证、维护 worker 启动与策略载入和后续 Object/Outbox/Account/Mail 初始化共用 30s 安全阶段。策略载入失败不得监听。`/diagnostics` 的 `outbound` 只包含 available 与 policy version，不含规则/IP 清单；DB 健康不可用时组件诊断也不再声称可用。Account/DB 健康时 `outbound_authorization=system_bound`，Project 仍未绑定，整体 ready=false。

首个停止信号关闭出站准入及 idle 连接，保留已在途响应。HTTP、当前 Secret batch 和出站活动都完成后才停止 DB admission；所有 drain 使用同一个首停 deadline。超时/第二信号对 DB 和出站执行 force，HTTP 关闭及 worker join 共用额外 1s 总预算。初始化期间迟到的出站资源拒收并关闭。强制关闭不代表外部请求未发送。

## 策略与实际发出

`NewPolicyService` 绑定 Store、真实 Audit 与当前 Session/SystemAdmin 端口；`GetPolicy/UpdatePolicy` 每次检查当前权限。规则为规范 RFC1918/ULA CIDR、all 或明确端口集合、allow_http；固定禁止地址不可由规则覆盖。默认版本 1 的规则集为空。管理命令在真实短 Tx 中写策略、receipt 与 Audit；旧 command 的同义重放返回原 receipt，但不发布旧规则，也不恢复 unavailable 镜像。

进程内可取消且 writer 优先的门禁覆盖更新提交、实际 pinned TCP dial 和 HTTP 首次 Write。DB Tx 不包含外部网络 I/O。dial 最长 5s；首写持 shared gate 最长 2s，真正返回 n>0 才记 sent。已发请求的后续 body/响应继续，后续请求和 redirect 均重新验证。COMMIT unknown 未核实前 fail closed；Reload 持进程 exclusive，在短 Tx 中取得同一 policy shared DB lock，读取当前值并确认事务成功后才发布，避免旧未决 COMMIT 稍后覆盖已恢复镜像。

HTTP 使用包内 H1 RoundTripper 与受控 keep-alive 池，复用标准 `Request.Write/ReadResponse` 的语法处理。选择此实现是为了直接控制真实初写和重试身份：同 attempt 的 sent 跨新建/复用 socket 保持，n>0 后从不内部重发；全部写为零时最多重试一次，且重新完整 DNS/当前策略准入。GET 或 Idempotency-Key 不改变这条规则。正常 EOF、完整 trailer 且无多余预读字节后才归池；取消、错误、提前 Close 或超限关闭连接。

标准 httptrace 的 GetConn/GotConn/WroteRequest 可观测这条路径。GotConn 仅给观察 wrapper，不能 Read/Write/改 deadline 或取得底层 socket；允许关闭 socket/发送半边。callback 必须及时返回并响应其 context，不能借 callback 绕过准入。标准 Request.Write 自行产生一次 WroteRequest，不能在 adapter 重复通知。请求 Body 遵循 `net/http` 的 Read/Close 契约，Close 必须解除阻塞 Read；实际 transport 统一管理一次 Close。

## DNS、地址与 TLS

每次 attempt/retry/redirect 获取完整 A/AAAA，去重后最多 64 个。任一家族错误、无效报文或任一地址不允许，全部拒绝；合法 NOERROR/NODATA 可以和另一家族的有效答案组合。仅拨号审批集合内的数值 IP，核验实际 RemoteAddr，Host/SNI 使用原规范 origin。复用连接也重新解析并检查实际 peer。

Go Resolver 的 StrictErrors 只对 temporary family error 放弃子集，因此本包提供有界 DNS wire resolver。生产只读取 `/etc/resolv.conf` 的 nameserver 作为受信 DNS 通道；不使用搜索后缀、NSS、hosts、mDNS 或环境代理回退。部署须提供能解析所需绝对 ASCII 域名的递归 nameserver。UDP 截断转 TCP，两个家族及 fallback 共用 5s；校验 ID/question/type/class/rcode、逐 label 边界、压缩指针、记录数量、CNAME 完整链与终局 owner。NS/SOA RDATA、长度和终局 CNAME 所属 authority zone 均校验；显式 referral 不当成 NODATA，合法 SOA mailbox 名称允许 DNS mailbox 语义。

分类常量引用 [IANA IPv4 special registry](https://www.iana.org/assignments/iana-ipv4-special-registry/iana-ipv4-special-registry.xhtml)、[IPv6 special registry](https://www.iana.org/assignments/iana-ipv6-special-registry/iana-ipv6-special-registry.xhtml) 及源码内 cloud metadata 资料。清单保守拒绝特殊用途段，不宣称实时跟踪在线注册表。metadata 优先于 private allow；IPv4-mapped IPv6 在分类/peer 时 Unmap，但保留完整 IPv6 origin 身份，不能与 IPv4 origin 合并凭据。

HTTP 固定 H1、无代理、无 HTTP/2/coalescing/cookie jar，TLS 至少 1.2，验证系统/部署 CA、有效期及原 hostname，不降级绕过错误。private HTTP 同时要求 Profile.AllowHTTP 与同一命中规则 allow_http；public HTTP 也要求 Profile.AllowHTTP。SMTP 的显式 none 与此开关无关。

## 调用、凭据和安全投影

可信 adapter 通过 `NewCallContext` 建立 actor/scope/AccessProducer AppendKey 与固定关联 ID；每个实际访问尝试使用自己的 Audit cause。`NewProfile` 固定 consumer、是否流式、可收紧限额和可选 CredentialBinding。`Client.Do` 接受标准 Request，但禁止 caller 覆写 Host、连接控制字段或直接放入 Authorization/Cookie；凭据由 typed header/query binding 临时注入，仅匹配完整 scheme+canonical host+有效 port。

只自动跟随无 body GET/HEAD，最多 5 跳，每跳重新 URL/DNS/策略校验，拒绝 HTTPS 降级。跨 origin 丢弃全部 caller header、原 query/body 与 binding；Location 含原材料或其百分号编码形式时拒绝，不信任外部响应为授权。深层嵌套 escaping 在有绑定材料时保守拒绝。

请求头在 `Request.Write` 的实际序列化路径中有界缓冲到 CRLFCRLF，包含自动生成的 Content-Length/Connection 等字段；超过上限时底层零字节写入，不为计数提前读取 body 或调用 GetBody。

平台上限：DNS/connect 5s、TLS 10s、响应头 15s/64 KiB、普通 overall 120s/响应 16 MiB、stream overall 30min/响应 256 MiB/read idle 60s、请求头 64 KiB/body 16 MiB。Profile 只能收紧。响应按实际字节计数，1xx 累计头与 trailer 计入限制；无自动解压，发送 Accept-Encoding: identity 并拒绝非 identity Content-Encoding。CONNECT/101 不支持。拒绝/超限先关闭不可复用 socket；最小 Audit 使用原 scope/cause、受原操作及最多 1s 预算约束，失败保留原拒绝，不重发网络请求。

`Response`、Profile、binding 与原始 cause 通过私有闭包保存；普通 fmt/JSON/slog 不含 URL path/query、header/body、凭据、全部 IP 或原 net/TLS 错误。显式 Body/Headers/Trailers 是可信 adapter 的读取端口。`NetworkError.Decision()` 仅投影 reason、consumer、规范 origin、policy_version、地址分类、redirect_count、sent、trace ID。

需要原生 `*http.Client` 的 SDK 使用 `SDKClient(profile)`，受控私有 Transport、拒绝标准库层另行 redirect、Jar=nil；Transport 内仍由 Client.Do 统一处理符合上述限制的合法 redirect。Go 的导出字段技术上可改；禁止替换属于可信 adapter 契约，不是类型强制。标准库会用包含原 URL 的 `*url.Error` 包装失败，SDK 的原 error/response 不能进入通用日志或业务 DTO；先用 `SafeNetworkError` 映射，并显式转换 response。当前没有 Provider/MCP SDK 集成完成声明，D09/D20 绑定时验证各 adapter。

## SMTP 正式连接端口

`DialTarget(ctx,host,port,SMTPProfile)` 只做同一完整 DNS/分类/pinning/当前策略控制下的连接。返回 Conn 兼容 net.Conn，由 Client 统一拥有取消和停机。已装配的 D07 accountmail 负责 TLS/STARTTLS/none、协议、认证和持久邮件结果；配置后 SMTP 失败不回退到恢复日志。

可信 D07 adapter 初始写只能执行不带凭据的建连/TLS 协商，不能把初始握手计作 AUTH/邮件业务 sent。每次 AUTH 或每封邮件前调用 BeginSend(ctx)，重新完整 DNS 并核验固定 peer；真正底层首 Write 再持当前策略门禁并记录 sent。TLS 包装不会绕过该底层 Write。完成本次协议结果后 EndSend，之后禁止写到下一次 BeginSend。BeginSend/Write 必须串行，并发明确拒绝；BeginSend 失败关闭连接，不能回退沿用旧 attempt。StopAdmission 与新 attempt 发布共用同一准入锁；停止期间尚在 DNS 的 BeginSend 不能发布新 attempt，已准入发送可按在途规则完成。caller 设置 deadline 不能放宽持门禁首写的 2s 上限，caller 更短的期限在首写后仍保留。D07 不得省略 BeginSend 或在初始协商阶段泄露凭据。

## 验证

```sh
AGENTEAM_GO=/path/to/go1.27.1/bin/go sh scripts/check-go.sh
AGENTEAM_GO=/path/to/go1.27.1/bin/go sh scripts/test-postgres.sh
AGENTEAM_GO=/path/to/go1.27.1/bin/go sh scripts/test-security.sh
# 定向真实网络/入口场景：
AGENTEAM_GO=/path/to/go1.27.1/bin/go sh scripts/test-security.sh -run '^(TestOutboundNetwork|TestRealOutbound|TestCentralOutbound)'
```

普通测试无需 Docker。test-security 组合既有固定 PG17.8/vector0.8.1 与 PG16 反例 fixture，以及同固定镜像内运行的临时网络 server binary。网络为 owned internal bridge，业务请求使用真实 private IP+精确 CIDR/端口规则+临时 CA；控制通道也只访问已验证 nonce/标签/精确 ID 的容器。DNS 测试替身只提供结果，生产分类/固定 IP/TLS/首写都真实执行，不将 loopback 分类改为 allow。UDP/TCP DNS wire 测试另在本机 loopback port 0 上运行。

server 记录连接 ID、实际请求和关闭状态；barrier 用 channel、标准 httptrace、实际 DB 锁/提交协调。覆盖新建及复用连接首写前收紧、DNS 等待后禁 dial、sent 后旧响应完成、GET/Idempotency-Key 断响应只收一次、实际零字节写至多一次重试及第二次 DNS、redirect/限额/idle/cancel、SMTP 端口下 TLS 计数以及进程三路 drain。所有 fixture 只创建和清理任务 nonce 的容器、网络和临时路径，不接触现有服务或外部凭据。
