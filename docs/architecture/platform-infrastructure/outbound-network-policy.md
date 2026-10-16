# Outbound Network Policy 详细设计

> 状态：Central 持久代理已交付；访问规则正按用户确认的全网白名单/黑名单与目标匹配模型改造，见[本轮工程契约](../../development/work-items/system-outbound-policy-proxy.md)。本页定义目标契约，不代证新版本已部署或验收。
>
> 上层架构：
> - [平台基础设施与部署架构](./README.md)
>
> 相关设计：
> - [MCP Server Config / Connection Lifecycle](../mcp-integration/mcp-server-config-lifecycle.md)
> - [MCP Protocol Runtime](../mcp-integration/mcp-protocol-runtime.md)

本文定义 agenteam Central 对“目标地址由配置、用户输入或外部数据决定”的出站网络访问所使用的统一安全边界。

## 1. 定位

Outbound Network Policy 是平台级基础设施能力，不属于 MCP 私有逻辑。

典型消费者包括：

- MCP Server Endpoint；
- Model Provider Base URL；
- SMTP host / port；
- 未来 Webhook；
- 未来 HTTP / Fetch Tool；
- 未来外部 Integration / Connector；
- 其他由业务配置决定目标地址的 Central outbound request。

本轮范围为 Central 全部业务出网，包括现有 HTTP/HTTPS、Model、MCP 及其 OAuth/下载等派生调用、SMTP。Runner 与 PostgreSQL/MinIO 部署基础连接不纳入本轮；未来消费者接入时沿同一边界，不因本轮配置页面完成宣称全平台出网已统一。

核心原则：

> 业务模块不自行实现 SSRF / private-network 防护，而是统一经过平台 Outbound Network Policy。

~~~mermaid
flowchart LR
    MCP["MCP Protocol Runtime"]
    Model["Model Provider Adapter"]
    SMTP["SMTP Delivery"]
    Future["Future HTTP Integration"]

    Policy["Outbound Network Policy"]
    Client["Controlled Outbound HTTP Client"]
    MailDial["Controlled SMTP Dial"]
    Network["External / Allowed Private Network"]

    MCP --> Policy
    Model --> Policy
    SMTP --> Policy
    Future --> Policy

    Policy --> Client
    Client --> Network
    Policy --> MailDial
    MailDial --> Network
~~~

## 2. 设计目标

Outbound Network Policy 解决：

- SSRF；
- loopback / link-local / cloud metadata 访问；
- private network 边界；
- DNS resolution / rebinding；
- redirect 绕过；
- Credential 跨 origin 泄漏；
- URL scheme / TLS 约束；
- timeout / response size 等统一网络 guardrail；
- 安全 diagnostics；
- 管理员配置的直连或统一代理出口及其凭据边界。

它不负责：

- Tool Authorization；
- Project Permission；
- 外部服务自身的 OAuth / API Key 认证；
- 业务模块的 retry / idempotency；
- 把所有 Central 网络请求强制转换成同一种业务 client。

## 3. 使用边界

当运行时业务 outbound target 来自以下来源之一时，必须经过该策略：

~~~text
user configuration
project configuration
system-admin configurable remote endpoint
external response / redirect
tool / integration controlled target
~~~

用于初始化和运行平台自身的可信 Deployment 基础连接，例如由环境变量配置的 PostgreSQL、MinIO，由部署层直接管理，独立于保存在 PostgreSQL 中的管理员运行时出站策略。它们仍须校验部署参数、凭据、TLS 与可达性；这里不增加新的管理 UI 或网络模式。

边界取决于连接用途与控制来源，不取决于地址是否写在环境变量中。Model Provider、MCP、SMTP、HTTP 集成等用户或业务可配置目标，以及外部响应派生的目标，仍须执行本策略；不得借基础连接名义将其变成任意目标代理。

该划分与 [Deployment Runtime 的配置分层](./deployment-runtime.md#5-deployment-config-与-runtime-platform-config) 一致：启动先建立 PostgreSQL / MinIO 基础连接，再加载管理员运行时策略，避免策略依赖数据库、连接数据库又先依赖该策略的循环。具体基础连接适配由 D02–D05 / D28 落实，运行时业务出口仍由 D04 的受控网络能力统一约束。

## 4. 平台接口

第一阶段建议提供统一受控 HTTP Client，而不是要求每个业务模块先手工调用 validate 再自行发请求。

概念接口：

~~~text
OutboundHttpClient.Do(
    context,
    request,
    request_profile
) -> response
~~~

内部：

~~~text
request URL
-> URL validation
-> DNS resolution
-> address classification
-> current platform policy decision
-> controlled dial
-> HTTP request
-> redirect re-validation
-> response limits
~~~

业务模块可以声明自己的 timeout / response size 等需求，但最终值不能突破平台安全上限。

如果某些 SDK 必须接收原生 `http.Client`，平台应提供已经装配 Outbound Network Policy 的受控 Transport / Client，而不是允许 SDK 绕过策略。

SMTP 等非 HTTP 协议复用目标地址、DNS/IP 和安全拨号边界，由自身 adapter 执行协议握手；不把 SMTP 包装成 HTTP 请求。精确端口与错误映射由 D04/D07 规格固定。

HTTP 与 SMTP 的底层连接必须消费同一受控 TCP 选路：先批准业务目标，再按当前持久配置直连或建立代理隧道。业务 adapter 不能用独立默认 transport、环境代理或裸拨号绕过此入口。

## 5. URL Validation

第一阶段至少校验：

- scheme 是否属于调用方允许集合；
- URL 必须包含有效 host；
- 不接受通过 URL userinfo 携带 Credential；
- hostname / port 格式合法；
- 明确拒绝本地文件、unix socket 等非网络 scheme；
- 对 redirect target 重复执行同一完整流程。

业务模块可以进一步缩小允许 scheme，例如 MCP 只允许 Streamable HTTP 所需的 HTTP / HTTPS。

## 6. DNS 与 IP Classification

Policy 不能只检查 URL 字符串。

必须：

1. 解析 hostname；
2. 获取实际 A / AAAA 地址；
3. 对所有解析结果分类；
4. 根据平台策略决定是否允许；
5. 实际建立连接时确保目标地址仍符合已经批准的策略。

至少区分：

~~~text
public
private
loopback
link-local
multicast
unspecified
reserved / special-use
cloud-metadata-sensitive
~~~

第一阶段固定：

- loopback：禁止；
- link-local：禁止；
- cloud metadata / known metadata target：禁止；
- unspecified / multicast / 明显保留地址：禁止；
- public / RFC1918 与 ULA private：统一由管理员访问规则判断，没有公网隐式放行；
- 新配置默认黑名单空表，普通目标无需额外配置即可使用，固定危险地址仍禁止。

IPv4 与 IPv6 使用相同安全语义。

## 7. 全网访问规则

系统管理员通过系统 UI / 管理 API 维护统一规则，持久化 PostgreSQL，对 Central 新业务调用生效，无需重启。规则不由 Project、Agent、MCP 或其他业务模块分别维护。

~~~text
OutboundNetworkPolicy
├── version
├── rule_mode: allowlist | denylist
└── rules[]
    ├── target: IP | CIDR | domain | *.domain
    └── ports: explicitly all | selected ports
~~~

白名单仅允许命中目标与端口的请求，空表拒绝全部；黑名单拒绝命中的请求，空表允许其余目标。两种模式都先执行固定危险地址拒绝，不能通过域名、通配域名、宽网段或代理绕过。规则顺序无优先级；目标与端口须在同一行匹配。

目标支持单个IPv4/IPv6、CIDR、精确域名和最左侧 `*.` 通配域名。域名使用规范ASCII形式；`*.example.com`匹配一个或多个层级的子域名，不包含 `example.com` 本身，也不匹配 `otherexample.com`。只匹配原请求的规范hostname，不以CNAME或反向DNS名称扩展授权；IP字面目标走IP规则。字段编码、规范化、大小限制见工程契约。

每次完整解析A/AAAA后，先拒绝任何固定禁止地址。白名单中，若原域名与端口命中域名规则，其全部非硬禁解析地址可获准；否则每个解析IP都必须分别命中IP/CIDR规则和端口。黑名单中，原域名与端口命中任一域名规则，或任一解析IP与端口命中IP/CIDR规则，即拒绝整个目标。不能从混合结果中挑允许地址绕过拒绝。

HTTP、HTTPS和SMTP等业务目标复用上述地址/端口判断，移除每行 `allow_http` 及通用网络Profile的明文许可开关；协议本身的HTTPS要求、TLS验证、HTTPS重定向不得降为HTTP以及凭据作用域仍有效。

用户已明确当前处于早期开发，不保留旧访问规则或旧写接口兼容。新默认为黑名单空表，精确重置范围与版本/命令身份见工程契约；不据此清空账号、Session、SMTP、代理配置、凭据或其他业务数据。

### 7.1 更新生效边界

数据库是策略权威来源。规则修改后新请求立即使用新规则；后续 retry 和每一跳 redirect 也重新检查当前策略，复用已有连接不能跳过校验。已发出的在途请求继续执行，不因修改规则主动中断，也不承诺撤回已经发生的外部副作用。

保存操作仅限系统管理员，须校验模式/目标/端口、version 冲突并记录安全 Audit。策略缓存/版本传播、实际发出边界、DNS/dial 与连接池竞争由 D04 规格及真实并发验收确定。

代理配置沿同一生效原则。访问规则与出口设置独立编辑、各有版本，实际发出须同时使用有效的当前规则与选路；代理保存不修改访问许可，规则保存不改变代理。规则或选路不可确认时拒绝新发出，不能回退为直连。切换模式、网关或凭据后，旧连接不能被新请求复用；已发送请求按原在途边界完成。

## 8. DNS Rebinding

只在配置保存时做一次 DNS 检查不够。

攻击者可能让同一 hostname：

~~~text
第一次解析 -> public IP
随后解析 -> private / loopback IP
~~~

因此受控 Outbound Client 必须在实际连接阶段重新确认目标地址。

第一阶段原则：

- DNS validation 与实际 dial 绑定；
- 不允许经过验证的 hostname 在连接时静默落到禁止地址；
- redirect 后重新解析和校验；
- mixed public + forbidden address 的解析结果默认拒绝，而不是随机挑一个 public 地址继续。

具体实现可以通过自定义 DialContext / Resolver / approved-address pinning 完成，不要求业务模块感知。

## 9. Redirect

Redirect 是新的 outbound target，不能继承原 URL 的安全结论。

每一跳：

~~~text
redirect target
-> URL validation
-> DNS resolution
-> address policy
-> controlled dial
~~~

第一阶段统一限制最大 redirect 次数，避免循环和无限跳转。

跨 origin redirect：

- 不自动转发 Authorization；
- 不自动转发 Cookie；
- 不自动转发 API Key / Custom Credential Header；
- 由业务协议明确重新认证时再生成对应 Credential。

因此安全模型是：

> Redirect 可以继承业务请求意图，但不能继承对目标地址或 Credential 接收方的信任。

## 10. Credential Scope

Credential 必须绑定到明确的目标 origin / authentication context。

例如：

~~~text
https://mcp.example.com
-> Authorization: Bearer ...
~~~

如果返回：

~~~text
302 -> https://other.example.net
~~~

受控 Client 不把原 Credential 自动发送给新 origin。

对于 OAuth redirect、authorization endpoint 等标准认证流程，应由对应 OAuth 实现根据协议显式构造新的请求，而不是依赖通用 HTTP redirect 自动携带 Credential。

## 11. Timeout 与 Response Limits

平台提供统一网络安全上限：

- connect timeout；
- overall request timeout 上限；
- idle / read timeout；
- maximum response headers；
- maximum response body / stream guardrail；
- maximum redirects。

业务模块可以请求更严格限制，但不能绕过平台最大值。

对于真正需要 streaming 的协议，例如 MCP streaming response，应该使用 streaming-aware 限制，而不是简单要求整个响应先读入内存。

这些限制约束协议请求和网络资源，不新增统一 ToolOperation / Agent Execution 总期限，也不让人工审批因等待时间而自动过期。业务 timeout 与取消仍按各自已定契约执行。

## 12. TLS

HTTPS 使用系统 / 平台受信任 CA 与标准 hostname verification。

第一阶段不允许业务配置：

- skip TLS verification；
- 任意信任所有证书；
- 关闭 hostname verification。

私有 CA 通过平台部署级受信任证书配置支持，不通过关闭证书或 hostname 校验实现。

HTTP与HTTPS使用相同访问规则，没有每行HTTP开关或隐藏的通用网络许可。OAuth等业务协议固有的HTTPS要求仍须执行；规则许可不构成TLS降级或凭据越域授权。

SMTP 支持管理员明确选择 TLS / STARTTLS / none；加密失败不自动降级，none 不额外限定只能用于内网。该协议选择不绕过统一目标/端口授权。见 [SMTP Delivery](authentication/smtp-delivery.md)。

## 13. Policy Decision

建议内部形成结构化结果：

~~~text
OutboundPolicyDecision
├── allowed
├── reason_code?
├── target_origin
├── resolved_addresses[]
├── network_class
└── policy_revision
~~~

拒绝 reason 至少包括：

~~~text
scheme_not_allowed
invalid_url
loopback_denied
link_local_denied
metadata_denied
private_network_denied
private_cidr_not_allowed
reserved_address_denied
dns_resolution_failed
redirect_denied
tls_required
policy_violation
~~~

业务模块获得安全错误，而不是底层 socket / resolver 的任意错误文本。

## 14. 配置与作用域

Outbound Network Policy 是平台统一安全能力；全网访问规则与代理设置是管理员维护的 Runtime Platform Config。

规则模式、目标和端口存数据库，可在系统 UI 修改；TLS / trust store 与网络实现参数仍按技术部署配置管理。固定禁止地址不是可编辑放行项。配置来源与启动边界见 [Deployment Runtime](./deployment-runtime.md)。

### 14.1 持久代理与统一选路

系统管理员在“出站规则”页面维护“访问规则”和“代理设置”。配置保存在数据库，默认直连；启用代理后，Central 范围内的全部业务目标使用所选网关，不设隐式 bypass 或失败直连。支持 HTTP CONNECT、HTTPS CONNECT 与 SOCKS5 TCP 隧道，不新增通用转发服务、UDP 或代理链。

产品移除 `AGENTEAM_CENTRAL_OUTBOUND_PROXY_ORIGINS` 与 `HTTP_PROXY`/`HTTPS_PROXY`/`NO_PROXY`（含小写）的选路读取，也不增加 `ALL_PROXY` 兜底。已有环境值不自动导入数据库。此变更不修改宿主环境、frp、开发工具或部署基础连接的环境配置。

目标 DNS 仍由 Central 按完整 A/AAAA 结果执行现有分类、CIDR 与端口授权。CONNECT authority 或 SOCKS5 目标使用本次批准的数值 IP，不能把未经批准的 hostname 交网关二次解析；TLS SNI/hostname verification、HTTP Host 与业务凭据作用域仍使用原目标。代理不改变固定禁止地址或全网访问规则。无法本地完成目标 DNS 审批时明确失败，不切换为网关远端 DNS。

网关是管理员显式信任的基础设施。网关自身可为公网或 RFC1918/ULA 私网（包括显式配置的企业网关及 `proxy:8080`），与业务目标分类分开校验；不得据此放宽业务目标的私网权限。网关解析须有界、完整校验全部候选并固定实际拨号地址，拒绝 loopback、link-local、metadata、multicast、unspecified 与其他禁止地址。网关可沿部署 resolver 解析单标签名称；这一便利不扩到业务目标解析。受信网关实际如何转发不能由客户端独立证明，部署者须确保其遵守批准的数值目标。

HTTPS 网关校验网关 hostname/CA，隧道内目标 HTTPS/TLS/STARTTLS 继续校验原目标；HTTP 与 SOCKS5 网关协议本身不加密到网关的认证交换，页面保留这一必要提示。代理凭据只能发给当前明确配置的网关，不能进入目标 Authorization/Cookie、重定向、普通日志或公开错误；目标业务凭据也不能进入 CONNECT/SOCKS 握手。配置代理不扩大业务自身的认证或 Project 授权。

代理密码使用平台成熟的版本化 keyring 与认证加密保存，数据库和回执不存明文，不以新密码学替代现有安全实现。读取仅返回用户名和是否已有凭据；单密码输入留空保持、非空替换，移除认证须显式确认。改变网关身份不得把旧凭据默默转发给新网关。配置不可解密、代理连接/认证/隧道失败均拒绝，不降级、不直连。管理读写、幂等重放、版本冲突与结果未知恢复的精确接口见[工程契约](../../development/work-items/system-outbound-policy-proxy.md)。

第一阶段不提供：

- Project override；
- Agent override；
- MCP Config 自定义 allowlist / denylist；
- Tool 级网络 policy。

这样防止下层业务对象自行扩大 Central 网络权限。

未来如果确实需要 Project-specific egress isolation，应作为平台网络能力的显式扩展重新设计。

## 15. Observability 与 Audit

至少记录安全 metadata：

- consumer / subsystem；
- target origin；
- policy decision；
- deny reason；
- network class；
- redirect count；
- policy revision；
- timing。

不得因为 diagnostics 记录：

- URL 中敏感 query value；
- Authorization；
- Cookie；
- API Key；
- OAuth token。

代理地址与用户名只在当前系统管理员的配置投影中提供；普通日志、Audit 和错误仅保留选路模式、协议、配置版本及闭集失败阶段，不记录代理密码、认证报文或任意网关响应。

是否把 policy denial 写入正式 Audit，取决于调用场景；重复的普通网络失败可以保留在 operational log，敏感配置变更仍走平台 Audit。

## 16. 与 MCP 的关系

MCP Config 只保存 endpoint 和 Authentication Profile。

~~~text
MCP Config.endpoint
-> MCP Protocol Runtime
-> Outbound HTTP Client
-> Outbound Network Policy
-> MCP Server
~~~

MCP 不自行实现：

- IP/CIDR/域名的白名单或黑名单；
- loopback 检查；
- DNS rebinding 防护；
- redirect target validation。

MCP 只额外负责自己的协议语义，例如：

- MCP headers；
- MCP revision；
- MCP Credential injection；
- Tool request / response。

## 17. 与 Model Provider 的关系

Model Provider 的可配置 `base-url` 同样属于 config-driven outbound endpoint。

因此长期上 Model Adapter 也应通过同一个受控 Outbound HTTP Client / Policy 访问自定义 Provider endpoint。

Provider-specific headers 与 credentials 仍由 Model System 负责生成；Outbound Network Policy 只负责目标网络安全与 credential forwarding guardrail。

## 18. 第一阶段实现边界

第一阶段至少实现：

1. 统一 Outbound HTTP Client / Transport；
2. URL / scheme validation；
3. DNS resolution；
4. IP classification；
5. loopback / link-local / metadata / reserved address deny；
6. 公网与私网统一的allowlist/denylist目标匹配；
7. 管理员 UI / API 维护的持久化IP/CIDR/域名/端口规则与即时新请求生效；
8. DNS rebinding 防护；
9. redirect re-validation；
10. cross-origin credential stripping；
11. timeout / response / redirect guardrail；
12. TLS verification；
13. structured policy decision / safe diagnostics；
14. SMTP 等非 HTTP 消费者的正式安全拨号适配；
15. 持久直连/HTTP CONNECT/HTTPS CONNECT/SOCKS5 选路、网关凭据与动态更新；
16. Central 全部现有业务出网的组合根接线与绕过检查，范围和逐项验收以本轮工程契约为准。
