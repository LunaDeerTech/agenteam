# 受控出站

> 当前契约已将 SMTP 拆为独立直连基础设施；实现、隔离验收与运行实例升级尚待完成。以下模块边界按新契约维护，历史结果只代表原输入，进度见[工程契约](../work-items/system-outbound-policy-proxy.md)。

`internal/central/outbound` 提供 DB 策略、地址分类观测、完整 DNS 审批与 HTTP/1.1 client。基础规则迁移为 `00004_outbound.sql`，DB 代理与凭据恢复映射为 `00144_outbound_proxy.sql`；新规则模型为 `00145_outbound_rule_modes.sql`。生产组合根已绑定真实 Account Session/SystemAdmin 授权并载入策略；D07 的 [accountmail](accountmail.md) 按当前契约使用独立 SMTP 直连传输，不依赖本模块的规则、代理或 Conn。System 出站策略管理 HTTP 已复用同一个 PolicyService 与 Account core 装配到生产根；Model Chat/Embedding、MCP 协议与 OAuth 绑定同一动态策略服务；没有匿名策略 API 或默认成功的授权实现。当前不存在单独的 Central web-fetch/URL 下载业务调用，通用 HTTP(S) 能力由本模块提供，后续消费者必须使用相同出口。

产品规则见[出站架构](../../architecture/platform-infrastructure/outbound-network-policy.md)，工程接口与验收见 [D04 实施规格](../work-items/d04-security-design.md)。此实现依赖正式部署的单 Central 前提。

## 配置、状态与停止

`AGENTEAM_CENTRAL_OUTBOUND_CA_FILE` 可选，省略时使用系统 trust roots；设置后追加文件内 PEM CA，显式空值视为配置错误。文件必须可读、仅包含有效 CA certificate PEM，最多 1 MiB；不接受私钥、非 CA 证书、无 CA 或额外正文。它与数据库 CA 独立，无 skip-verify 或业务自带 CA。配置加载后保留不可变 trust store，不保存文件路径。SMTP 继续复用这份系统/部署静态 TLS 根；配置键名不变，共享信任材料不构成对动态规则或代理的依赖。`--check-config` 读取并验证 CA，不连接网络或宣称 DB policy/canary 已初始化。

数据库启动阶段后，cursor/Audit、Secret 初始验证、维护 worker 启动与策略载入和后续 Object/Outbox/Account/Mail 初始化共用 30s 安全阶段。策略载入失败不得监听。`/diagnostics` 的 `outbound` 只包含 available 与 policy version，不含规则/IP 清单；DB 健康不可用时组件诊断也不再声称可用。Account/DB 健康时 `outbound_authorization=system_bound`，Project 仍未绑定，整体 ready=false。

首个停止信号关闭出站准入及 idle 连接，保留已在途响应。HTTP、当前 Secret batch 和出站活动都完成后才停止 DB admission；所有 drain 使用同一个首停 deadline。超时/第二信号对 DB 和出站执行 force，HTTP 关闭及 worker join 共用额外 1s 总预算。初始化期间迟到的出站资源拒收并关闭。强制关闭不代表外部请求未发送。

## 策略与实际发出

`NewPolicyService` 绑定 Store、真实 Audit 与当前 Session/SystemAdmin 端口；`GetPolicy/UpdatePolicy` 每次检查当前权限。规则为 `rule_mode=allowlist|denylist` 与 `rules[{target,ports}]`。target 支持 IP、CIDR、ASCII 域名及 `*.domain`；HTTP/HTTPS 统一按规则；SMTP 不参与该判断。版本1的默认 denylist 包含32条可见、可删除的特殊用途地址与 metadata 名称规则，唯一清单见 `internal/central/outbound/default_rules.go`，与00145初始化保持一致。没有隐藏地址类别拒绝；清空 denylist 允许所有语法有效目标，空 allowlist 拒绝所有目标。未加载、无效 mode 和 Unknown 始终 fail closed，不能回退到空 denylist。

此前00145按早期开发阶段的明确决定重置旧访问规则与其旧回执，不迁移旧规则语义，不接收旧body。仅规则单例重置为version1/default denylist，proxy/Secret/账号/SMTP及Audit历史保持。规则命令使用 `outbound-policy-v2` namespace；部署先停旧binary，禁止跨schema并发或只换旧binary回退。

管理命令在真实短 Tx 中写策略、receipt 与 Audit；旧 command 的同义重放返回原 receipt，不发布旧规则。不可用镜像须另外核实两份当前配置及未决命令后恢复，不能凭历史 receipt 恢复。

进程内可取消且 writer 优先的门禁覆盖更新提交、实际 pinned TCP dial 和 HTTP 首次 Write。DB Tx 不包含外部网络 I/O。dial 最长 5s；首写 socket deadline 最长 2s，真正返回 n>0 才记 sent。含代理凭据的首写另须等待借用和清理实际返回，阶段预算见下节。已发请求的后续 body/响应继续，后续请求和 redirect 均重新验证。COMMIT unknown 未核实前 fail closed；恢复持进程 exclusive，在短 Tx 中取得两份配置及未决命令的 shared DB lock，读取当前值并确认事务成功后才发布，避免旧未决 COMMIT 稍后覆盖已恢复镜像。

HTTP 使用包内 H1 RoundTripper 与受控 keep-alive 池，复用标准 `Request.Write/ReadResponse` 的语法处理。选择此实现是为了直接控制真实初写和重试身份：同 attempt 的 sent 跨新建/复用 socket 保持，n>0 后从不内部重发；全部写为零时最多重试一次，且重新完整 DNS/当前策略准入。GET 或 Idempotency-Key 不改变这条规则。正常 EOF、完整 trailer 且无多余预读字节后才归池；取消、错误、提前 Close 或超限关闭连接。

标准 httptrace 的 GetConn/GotConn/WroteRequest 可观测这条路径。GotConn 仅给观察 wrapper，不能 Read/Write/改 deadline 或取得底层 socket；允许关闭 socket/发送半边。callback 必须及时返回并响应其 context，不能借 callback 绕过准入。标准 Request.Write 自行产生一次 WroteRequest，不能在 adapter 重复通知。请求 Body 遵循 `net/http` 的 Read/Close 契约，Close 必须解除阻塞 Read；实际 transport 统一管理一次 Close。

## System 出站规则 HTTP

`internal/central/outbound/http.NewSystemHTTPHandler` 纯构造管理边界，接收根已创建的策略实例与 Account core；不初始化、Reload 或新建另一份策略。根的受控 client 和管理口共享该 PolicyService，规则修改无需重启。精确 wire 见 [OpenAPI](../../../api/openapi/outbound-policy.json) 与 [HTTP 实施规格](../work-items/d04-system-outbound-policy-http.md)。

| 方法与路径 | 输入与成功结果 |
| --- | --- |
| `GET /api/v1/system/outbound-policy` | 当前管理员，无 query/body；200 恰 `{version,rule_mode,rules}`，返回数据库当前完整观察。空规则为 `[]`，不是读取失败的替代值。 |
| `PUT /api/v1/system/outbound-policy` | 当前管理员、合法 Origin/CSRF、唯一 Idempotency-Key；JSON 恰 `{expected_version,rule_mode,rules}` 完整替换。200 恰 `{version,rule_count,audit_id,created_at}`，是原 UpdatePolicy 的历史命令回执，不追加 GET。 |

版本为规范正 int64 十进制字符串，rule_count 为0–512的规范十进制字符串；audit_id 为 UUIDv7，created_at 为规范 UTC 微秒时间。规则最多512条；ports 为显式 `"all"` 或1–256个互异的1–65535整数。单IP规范为/32或/128，CIDR须已掩码且规范，规则不接受 IPv4-mapped 或 zone；DNS小写并去一个末尾点，通配符只能位于 `*.` 前缀，匹配一或多级子域但不含apex。规范化重复行拒绝；旧cidr/allow_http字段及缺少rule_mode均400。无通用Profile.AllowHTTP开关；OAuth自身HTTPS与HTTPS重定向降级限制保持。

域名仅匹配原请求hostname，不由CNAME或反向DNS扩权。allowlist中的域名与该行端口命中可批准全部有效解析IP；否则全部IP分别需要IP/CIDR及同一行端口命中。denylist中任一域名行或任一解析IP命中即拒整个目标。所有DNS结果先验证合法性和完整性，IP匹配使用Unmap，凭据Origin身份保持原形式。

仅支持 GET/PUT，其他方法405且 `Allow: GET, PUT`，HEAD 无响应体；无 lookup、目标测试或 Reload HTTP；代理配置使用下述独立 `/proxy` 资源。Account 边界先检查 Host/Origin/Fetch-Metadata 和路径歧义，再认证与解析；GET不要求写CSRF，PUT要求当前合法CSRF。两口拒绝任何query，GET拒绝实际body及不合法framing；PUT只接受严格单个UTF-8 JSON，整个请求上限1 MiB，规则字段也限1 MiB（完整请求预算最终约束），拒绝重复/未知/缺失/null与尾随值。成功输出完整校验/编码后才发布，普通日志和Problem不包含规则、内网地址、Cookie、CSRF、key或底层cause。

GET最长3s、PUT最长30s，期限在HTTP预认证前建立并继承更早parent；包含实际body读取、发送门禁、事务、Unknown核实和响应准备，不为各阶段续期。局部连接读写deadline使取消能结束原生Read；body关闭与取消回调实际完成、清除连接期限后才退出，避免污染keep-alive后续请求。底层不支持所需期限时在业务调用前拒绝，不以后台goroutine提前返回。服务已调用后的取消/断响应不能改成not_started；已确认接受而不能完整送达时终止响应，不追加第二个Problem。

HTTP预认证不替代PolicyService同Tx的当前Session/SystemAdmin检查与User锁。权限撤销后不能读取旧receipt；同用户新Session仅在当前授权和合法CSRF下重放原key/body。format2语义摘要含expected_version、rule_mode和canonical target规则，端口/规则换序同义；同key异义或新key旧版本冲突都不自动生成替代命令。旧key始终返回原version/rule_count/AuditID/时间，不回退较新规则；需要恢复时另外锁定两份当前配置及未决命令，判定数据库终态后才恢复镜像。GET只是当前数据库观察，不Reload。

COMMIT_UNKNOWN保留原unknown分类和公共 `retry_hint=lookup` 兼容投影，但本域没有lookup端点；调用方只能显式用原PUT/key/body与当前合法CSRF确认，不能把GET或其版本当receipt。原服务核实receipt与当前策略并确认核实事务成功后才能发布当前镜像；核实失败仍unknown/fail closed。旧历史receipt并不证明当前镜像健康。

保存后的新请求、retry及每跳redirect继续使用同一root client的当前准入；已发出请求的body/响应可继续完成，更新不主动中断它们，也不保证撤回外部副作用。真实同实例验收包含收紧后后续请求零发送、原在途body读至EOF并实际关闭socket。Model/MCP实际接线见本页首段；管理口本身不证明外部服务可达或完整业务Runtime验收，原启动/停止门禁保持。

## DNS、地址与 TLS

直连路径每次 attempt/retry/redirect 获取完整 A/AAAA，去重后最多 64 个。任一家族错误、无效报文或任一地址不允许，全部拒绝；合法 NOERROR/NODATA 可以和另一家族的有效答案组合。仅拨号审批集合内的数值 IP，核验实际 RemoteAddr，Host/SNI 使用原规范 origin。复用连接也重新解析并检查实际 peer。

Go Resolver 的 StrictErrors 只对 temporary family error 放弃子集，因此本包提供有界 DNS wire resolver。直连目标只读取 `/etc/resolv.conf` 的 nameserver 作为受信 DNS 通道；不使用搜索后缀、NSS、hosts、mDNS 或失败后的代理回退。直连部署须提供能解析所需绝对 ASCII 域名的递归 nameserver。UDP 截断转 TCP，两个家族及 fallback 共用 5s；校验 ID/question/type/class/rcode、逐 label 边界、压缩指针、记录数量、CNAME 完整链与终局 owner。NS/SOA RDATA、长度和终局 CNAME 所属 authority zone 均校验；显式 referral 不当成 NODATA，合法 SOA mailbox 名称允许 DNS mailbox 语义。

分类常量引用 [IANA IPv4 special registry](https://www.iana.org/assignments/iana-ipv4-special-registry/iana-ipv4-special-registry.xhtml)、[IPv6 special registry](https://www.iana.org/assignments/iana-ipv6-special-registry/iana-ipv6-special-registry.xhtml) 及源码内 cloud metadata 资料。默认可见规则保守涵盖这些特殊用途段，不宣称实时跟踪在线注册表；管理员可删除。Classify仅作观测，不作为隐藏准入；IPv4-mapped IPv6 在分类/peer 时 Unmap，但保留完整 IPv6 origin 身份，不能与 IPv4 origin 合并凭据。

HTTP 固定 H1、无 HTTP/2/coalescing/cookie jar，TLS 至少 1.2，验证系统/部署 CA、有效期及原 hostname，不降级绕过错误。HTTP 和 HTTPS 使用相同规则准入。SMTP 的独立 none、TLS及STARTTLS规则见[账号邮件说明](accountmail.md)，不使用本节 DNS/规则准入。

### 数据库代理与动态受控传输

Central 的代理由 `agenteam_outbound.proxy_settings` 单例保存，默认 `direct`。System 管理员使用 `GET/PUT /api/v1/system/outbound-policy/proxy` 管理，权限、当前 Session、Origin/CSRF、版本 CAS 和幂等回执与规则管理共用正式边界；具体 DTO 见 [OpenAPI](../../../api/openapi/outbound-policy.json)。规则与代理各有版本，共享实际发送门。保存返回原命令的 `version/audit_id/created_at`，当前 GET 不替代回执。任一未知提交使本模块业务出站 fail closed；历史回执或另一资源保存不单独恢复全部可用性，原命令重放或显式 Reload 的恢复阶段必须在两个 DB 配置锁及未决命令锁下读取当前事实。新代理命令 Unknown 使用 `retry_hint=replay`；已知提交后恢复读失败保留 `commit_state=committed` 与 replay，继续原命令恢复。

产品不读取 `HTTP_PROXY`、`HTTPS_PROXY`、`NO_PROXY` 及小写项，不再接受旧 `AGENTEAM_CENTRAL_OUTBOUND_PROXY_ORIGINS` 部署字段。操作系统代理环境保留给 frp、npm 等部署工具，不会成为 Central 业务代理的后备源。App 根 Client、每次创建的 Model Client、MCP/OAuth SDK transport 读同一个动态策略服务。SMTP 独立直连，不读取该服务或代理环境值；Runner 及 PostgreSQL/对象存储继续保持原连接边界。

支持 HTTP/HTTPS CONNECT 与 SOCKS5；HTTP 目标也通过数字 IP CONNECT 隧道，SOCKS5 不委托目标 DNS。范围内每次新请求、重试和 redirect 都重新核完整目标 A/AAAA 与 rule_mode/target/ports。目标任何IP或域名被当前规则拒绝都拒绝整个集合，原域名仅保留给 Host 和 TLS 校验。网关独立完整解析并 pin 数字 IP；管理员显式保存的合法网关独立批准，包括本机地址；业务target规则不作用于网关，网关也没有额外地址类别硬禁。单标签网关可以使用部署 hosts/search 找到同一候选，最终地址必须来自完整 hosts 快照或该规范候选的严格双 family DNS。启用代理失败不直连降级。

HTTPS 网关先以网关 hostname 和系统/部署 CA 校验证书；目标 HTTPS 再保留原目标 hostname 校验。HTTP Basic 或 SOCKS5 用户名密码只发给已批准网关，业务凭据仍受原目标 origin 绑定。网关协议、规范 host、port 或 username 改变且保存了密码时，必须提供新密码或明确 remove；direct 模式保留网关也受该规则约束。keep 且密码省略保持原密钥，非空密码替换；remove 不带密码且 username 为空。DTO/Audit/日志不含密码或 Secret 引用。

代理密码使用成熟 Secret envelope、同事务映射/引用及短时 lease。代理命令语义使用账户 keyring 的独立 MAC domain，保存 kid+MAC，不保存密码的无钥摘要。获取/使用借用的子 context 为 2s，取消后的独立清理为 5s，原回调及清理真实 join 后才释放发送门；PG rollback/discard 有各自的既有限额，因此这些是阶段预算，不宣称整体墙钟固定上限。旧池连接与当前代理版本不符即拒用；业务出站规则或代理版本变化不影响独立 SMTP 连接，SMTP 仍须校验自己的当前配置与凭据。

迁移 00144 加表默认 direct，不读取或复制旧环境代理秘密。保留既有 DB、密钥与账号，公开实例更新须通过当前任务门禁后按 owner 的精确进程步骤进行。旧版本不理解已启用的 DB 代理，不能将忽略配置直连称为兼容回退；切 direct 也不足以让旧二进制使用已迁移数据库。迁移 guard 会拒绝高于自身 manifest 的已应用版本，因此回退还须协调与旧版本匹配的数据库备份和密钥，不能仅替换 binary 或绕过版本检查。

## 调用、凭据和安全投影

可信 adapter 通过 `NewCallContext` 建立 actor/scope/AccessProducer AppendKey 与固定关联 ID；每个实际访问尝试使用自己的 Audit cause。`NewProfile` 固定 consumer、是否流式、可收紧限额和可选 CredentialBinding。`Client.Do` 接受标准 Request，但禁止 caller 覆写 Host、连接控制字段或直接放入 Authorization/Cookie；凭据由 typed header/query binding 临时注入，仅匹配完整 scheme+canonical host+有效 port。

只自动跟随无 body GET/HEAD，最多 5 跳，每跳重新 URL/DNS/策略校验，拒绝 HTTPS 降级。跨 origin 丢弃全部 caller header、原 query/body 与 binding；Location 含原材料或其百分号编码形式时拒绝，不信任外部响应为授权。深层嵌套 escaping 在有绑定材料时保守拒绝。

请求头在 `Request.Write` 的实际序列化路径中有界缓冲到 CRLFCRLF，包含自动生成的 Content-Length/Connection 等字段；超过上限时底层零字节写入，不为计数提前读取 body 或调用 GetBody。

平台上限：DNS/connect 5s、TLS 10s、默认响应头 15s/64 KiB、普通 overall 120s/响应 16 MiB、stream overall 30min/响应 256 MiB/read idle 60s、请求头 64 KiB/body 16 MiB。除下述 Model 生成等待资格外，Profile 只能收紧。响应按实际字节计数，1xx 累计头与 trailer 计入限制；无自动解压，发送 Accept-Encoding: identity 并拒绝非 identity Content-Encoding。业务 CONNECT/101 不支持；受信代理内部 CONNECT 受上节约束。拒绝/超限先关闭不可复用 socket；最小 Audit 使用原 scope/cause、受原操作及最多 1s 预算约束，失败保留原拒绝，不重发网络请求。

非流式 Model 可能先生成完整 JSON 再发送响应头。`NewProfile` 仅对 `Consumer=Model && Streaming=false` 接受显式大于 15s 的 `Limits.ResponseHeaders`，且必须不超过显式原 `Overall` 与 60s。零值仍为 15s，其他 HTTP consumer 与 SSE 保持原 15s 上限；受信代理 CONNECT 等待也最多 15s，不随模型生成时间延长。当前 Model adapter 仅 JSON tools 请求在未显式指定时提供 `min(原 attempt 剩余预算,60s)`，保留显式更短值。真正读取响应头的 deadline 始终再取原 request context 的更早期限；该资格不新建 attempt、改变 retry 或延长 M1 的 300s/12-sent 观察条件。

`Response`、Profile、binding 与原始 cause 通过私有闭包保存；普通 fmt/JSON/slog 不含 URL path/query、header/body、凭据、全部 IP 或原 net/TLS 错误。显式 Body/Headers/Trailers 是可信 adapter 的读取端口。`NetworkError.Decision()` 仅投影 reason、consumer、规范 origin、policy_version、地址分类、redirect_count、sent、trace ID 与可选闭集 phase。只有 origin 的实际响应头读取失败会标记 `response_headers`；空值表示未记录具体阶段，不能推断未发送。CONNECT、TLS、请求写入及响应 body 错误不借用此标记。Model JSON tools 可将 `timeout + response_headers` 映射为安全专用码，保留原 sent/retry 含义；不输出原始响应或底层错误来诊断时间。

需要原生 `*http.Client` 的 SDK 使用 `SDKClient(profile)`，受控私有 Transport、拒绝标准库层另行 redirect、Jar=nil；Transport 内仍由 Client.Do 统一处理符合上述限制的合法 redirect。Go 的导出字段技术上可改；禁止替换属于可信 adapter 契约，不是类型强制。标准库会用包含原 URL 的 `*url.Error` 包装失败，SDK 的原 error/response 不能进入通用日志或业务 DTO；先用 `SafeNetworkError` 映射，并显式转换 response。现 Model Chat/Embedding 和 MCP/OAuth 生产接线均消费本模块；该源码接线结论不等同逐 provider 的真实外部业务端到端验证。

## SMTP 独立连接边界

SMTP 是与数据库、对象存储并列的基础设施连接，直接连接管理员保存的 host/port，按其 none/TLS/STARTTLS 模式执行。它不使用本模块的 `DialTarget`、`SMTPProfile`、`Conn` 或 `BeginSend/EndSend`，不消费动态访问规则、持久全局代理或环境代理，不提供 SMTP 专属代理开关。

移除的是统一网络策略与选路依赖。账户层 `BeginDelivery` / `SendPermit` 仍在每次 AUTH 和 MAIL 的真实首写处校验当前 SMTP 版本、凭据及授权；TLS 包装不能提前消费该许可。独立传输负责有界拨号、取消与实际连接关闭/join，Mail Registry、Secret 材料释放和账户最终排空保持原职责。协议、结果未知、重试和运行说明见[账号邮件](accountmail.md)；拆分不要求数据库迁移、重建配置或更换密钥。

## 验证

SMTP 拆分的 owned 隔离验证尚待实现 owner 与独立 reviewer 完成；以下为此前规则与代理版本的验证记录，不代证新 SMTP 传输或真实邮箱送达。

此前00145、新target/mode与可编辑默认黑名单已通过outbound与HTTP两个完整普通包单测，以及新判定、当前模式门、默认条目可删除和loopback网关的定向race。真实PostgreSQL的`TestOutboundRuleModesResetPreservesOtherDomains`已验证144→145仅重置规则和旧规则回执，账号、SMTP、Secret、proxy及Audit行与xmin保持，重复迁移不重置新规则；512条近1MiB合法规则经jsonb格式化超过1MiB后仍可写入并通过生产Reload，独审也在独占资源实际复验通过。

新Vue真实规则旅程已验证mode与通配域名同体保存、刷新、409保留草稿及显式采用、删除与原配置恢复。独立真实API/socket门通过默认本机拒绝、删除对应可见行后放行、域名/完整地址集合、发送前模式变化、redirect与加载失败关闭；其DNS答案为受控resolver，不宣称真实部署DNS验证。mode持久回执与COMMIT结果未知代表分轮通过：无法验证当前配置时仍关闭；已持原command锁证明writer结束且无receipt、重新读取当前规则后可以恢复准入，对调用方仍保留COMMIT_UNKNOWN/Unknown，原version/denylist/default32与零receipt/Audit不变。最后这一旧fixture未绑定proxy，只证明rule-only恢复，不新增双snapshot实际覆盖声明。

准备阶段的旧测试helper/迁移30s预算失败及首浏览器观测失败、独审资源清理异常均保留在恢复记录，未靠产品放宽边界处理。独占5260在测试结束后已优雅关闭，manifest/sidecar撤销且6资源/3目录清零；公开服务与真实SMTP未操作。以下其余通过结果属于此前DB代理/旧规则输入；归档web-crape旧wire harness未在本轮做业务验收，不增加legacy API。具体分轮结果与边界见本树`.agent-state/current.md`。

原管理 HTTP 的作者三新两旧与独立代表、首红归因、实际尾部及资源收尾记录见[出站规则 HTTP 验收报告](../agent-team/system-outbound-policy-http-verification.md)。原独立代表验证请求回执与更晚GET分离、当前撤权，以及同root client的在途body/后续准入；本轮数据库代理新增的真实PG恢复验证见下文，不宣称已测试公网目标。

```sh
AGENTEAM_GO=/path/to/go1.27.1/bin/go sh scripts/check-go.sh
AGENTEAM_GO=/path/to/go1.27.1/bin/go sh scripts/test-postgres.sh
AGENTEAM_GO=/path/to/go1.27.1/bin/go sh scripts/test-security.sh
# 定向真实网络/入口场景：
AGENTEAM_GO=/path/to/go1.27.1/bin/go sh scripts/test-security.sh -run '^(TestOutboundNetwork|TestRealOutbound|TestCentralOutbound)'
# 出站管理 HTTP 三新两旧定向场景：
AGENTEAM_GO=/path/to/go1.27.1/bin/go sh scripts/test-security.sh -run '^(TestSystemOutboundPolicyHTTP|TestSystemOutboundPolicyHTTPTransactionAndBudget|TestSystemOutboundPolicyHTTPRootSharedInstance|TestOutboundPolicyDurabilityReplayAndAuthorization|TestOutboundPolicyRollbackAndUnavailableReplay)$'
```

普通测试无需 Docker。test-security 组合既有固定 PG17.8/vector0.8.1 与 PG16 反例 fixture，以及同固定镜像内运行的临时网络 server binary。网络为 owned internal bridge，业务请求使用真实 private IP+精确 CIDR/端口规则+临时 CA；控制通道也只访问已验证 nonce/标签/精确 ID 的容器。DNS 测试替身只提供结果，生产分类/固定 IP/TLS/首写都真实执行，本机目标场景须经正式规则明确放行，默认规则仍会拒绝它。UDP/TCP DNS wire 测试另在本机 loopback port 0 上运行。

server 记录连接 ID、实际请求和关闭状态；barrier 用 channel、标准 httptrace、实际 DB 锁/提交协调。覆盖新建及复用连接首写前收紧、DNS 等待后禁 dial、sent 后旧响应完成、GET/Idempotency-Key 断响应只收一次、实际零字节写至多一次重试及第二次 DNS、redirect/限额/idle/cancel、SMTP 端口下 TLS 计数以及进程三路 drain。所有 fixture 只创建和清理任务 nonce 的容器、网络和临时路径，不接触现有服务或外部凭据。

代理认证借用同时持久化原进程、操作与 Secret lease 的映射。正常回收等待实际回调结束；同进程恢复持独占发送门证明所有借用已 join，跨进程恢复必须通过原 Object ProcessGuard 的停止证明。代理/规则提交结果未知时共同停止准入，原命令重放只能在锁定两份当前配置并判定未决命令后恢复；历史回执不能充当当前配置。独立真实 PostgreSQL 已通过权限/CSRF/CAS/加密持久/重启及 Unknown 原命令重放、另一资源保存两代表；旧基线相同 Unknown 输入真实失败，当前候选通过。

独立 lease 生命周期验证也已通过：注入合法持久operation/lease后，真实外进程guard仍活时恢复保留两者；原root实际join、ConfirmStopped成功后恢复释放lease并删除operation，随后正式HTTP移除凭据成功。旧基线相同输入在最终移除时返回RESOURCE_BUSY，修复候选通过。测试使用持久状态注入与真实ProcessGuard生命周期，不声称SIGKILL崩溃实测；这组真实PG binary非race，与独立transport race分开报告。所有独占资源均在原测试进程退出后精确核验清零，本轮独审无未关闭阻断。

`TestSystemOutboundProxyHTTPRouteChanges` 已通过真实根 API 保存认证代理、短时 Secret 借用、受控 CONNECT 到 owned TLS 目标、坏网关无直连回退和 direct 切换；借用 operation/active lease 在调用结束时为0，released lease至少1。独审另用独占资源复跑同一作者场景也已通过。该证据覆盖根 HTTP 出口，不能外推为逐 Model provider、MCP 或 SMTP 完整业务入口的端到端验证。所有目标、代理、SMTP sink 和数据库均为独占 fixture，不向用户真实配置或邮箱发送。

既有 `TestOutboundPolicyRollbackAndUnavailableReplay` 与 `TestSystemOutboundPolicyHTTPRootSharedInstance` 已按新恢复契约完成真实 PostgreSQL 定向验证：无效当前配置不能凭历史回执开门；恢复有效当前配置后保持原命令回执且不重复业务版本/Audit；实际请求使用最新规则而非历史规则。准备阶段只沿 Migrator 和 App 既有启动预算，业务操作预算没有延长。
