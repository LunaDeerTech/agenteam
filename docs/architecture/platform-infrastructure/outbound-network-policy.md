# Outbound Network Policy 详细设计

> 状态：初版设计稿
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
- 安全 diagnostics。

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
- public：允许；
- private：默认拒绝，须命中管理员维护的系统内网放行规则。

IPv4 与 IPv6 使用相同安全语义。

## 7. Private Network Policy

private network 不能在各业务模块中单独配置。

系统管理员通过系统 UI / 管理 API 维护统一内网规则，持久化 PostgreSQL，不要求重启 Central。概念结构如下，正式字段/编码由 D04 规格确定：

~~~text
OutboundNetworkPolicy
├── version
└── private_allow_rules[]
    ├── CIDR
    ├── selected ports / explicitly all ports
    └── allow HTTP (default false)
~~~

默认不放行内网：

~~~text
private_allow_rules = []
~~~

管理员可以显式允许内网服务，例如：

~~~text
10.20.0.0/16, ports = [443, 8443], allow HTTP = false
192.168.50.0/24, ports = [8080], allow HTTP = true
~~~

规则支持网段及可选端口限制；不限制端口时须管理员显式选择全部端口。命中网段/端口只表示地址授权，内网 HTTP 仍须独立开启允许项。loopback、link-local、metadata、unspecified/multicast 等固定禁止地址不能通过规则放行。Project、Agent 或 MCP Config 不能自行修改系统策略。

这样：

- 未配置规则时禁止访问 Central 内网；
- 管理员可按网段和端口允许内部 MCP / Provider / SMTP；
- 业务配置不能借由 endpoint 自行扩大 Central 的网络可达范围。

### 7.1 更新生效边界

数据库是策略权威来源。规则修改后新请求立即使用新规则；后续 retry 和每一跳 redirect 也重新检查当前策略，复用已有连接不能跳过校验。已发出的在途请求继续执行，不因修改规则主动中断，也不承诺撤回已经发生的外部副作用。

保存操作仅限系统管理员，须校验网段/端口/HTTP 选项、version 冲突并记录安全 Audit。策略缓存/版本传播、实际发出边界、DNS/dial 与连接池竞争由 D04 规格及真实并发验收确定。

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

HTTP 是否允许由具体消费者和系统策略共同决定。

例如：

- 公网 endpoint 可以要求 HTTPS；
- 内网规则放行网段/端口后默认仍要求 HTTPS，管理员须在相应规则中显式允许 HTTP。

SMTP 支持管理员明确选择 TLS / STARTTLS / none；加密失败不自动降级，none 不额外限定只能用于内网。该协议选择不绕过地址/端口授权，也不直接使用 HTTP 规则的 allow_http 作为 SMTP 开关。见 [SMTP Delivery](authentication/smtp-delivery.md)。

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

Outbound Network Policy 是平台统一安全能力；内网放行部分是管理员维护的 Runtime Platform Config。

内网网段、端口和显式 HTTP 放行规则存数据库，可在系统 UI 修改；TLS / trust store 与网络实现参数仍按技术部署配置管理。固定禁止地址不是可编辑放行项。配置来源与启动边界见 [Deployment Runtime](./deployment-runtime.md)。

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

- private CIDR allowlist；
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
6. public network allow；
7. 管理员 UI / API 维护的持久化 private CIDR / port / HTTP 放行规则与即时新请求生效；
8. DNS rebinding 防护；
9. redirect re-validation；
10. cross-origin credential stripping；
11. timeout / response / redirect guardrail；
12. TLS verification；
13. structured policy decision / safe diagnostics；
14. SMTP 等非 HTTP 消费者的正式安全拨号适配。
