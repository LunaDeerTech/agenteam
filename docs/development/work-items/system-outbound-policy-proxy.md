# Central 出站规则与持久代理

状态：交付前WIP；基线 `89e4e901`。本卡是本轮工程接口、体验和验收的唯一合同，架构规则见[出站网络策略](../../architecture/platform-infrastructure/outbound-network-policy.md)。三条真实UI/API旅程、代理实际选路和Unknown恢复代表已有通过证据；最后lease活性门与最终组合核对尚未完成，生产未切换，不代表本卡正式交付。

## 1. 目标与分工

管理员在新 Vue `/system/outbound-policy` 配置访问规则与出口代理，保存后 Central 新业务调用立即使用，无需重启。现有全部 Central 业务 HTTP/HTTPS、Model、MCP（含 OAuth、发现、下载等派生调用）与 SMTP 接入同一受控出网模块；业务权限、目标认证、重试和业务结果仍由各自模块负责。

Runner、数据库、MinIO 等部署基础连接不纳入本轮。移除产品 `OUTBOUND_PROXY_ORIGINS` 和 HTTP(S)_PROXY/NO_PROXY（含小写）选路读取，没有环境兜底或自动迁入；不修改宿主环境、frp、开发工具。真实 SMTP 发送仍暂停，只能用任务所有的隔离 SMTP/代理验证。

| Owner / 工作树 | 唯一责任 |
| --- | --- |
| account_contract / `/workspace/agenteam` | 本卡、出站架构、main current |
| account_backend / `/workspace/agenteam-outbound-backend` | 存储/迁移、API/OpenAPI、安全选路、Central 消费者装配及后端说明 |
| vue_integration / `/workspace/agenteam-outbound-vue` | 新 Vue 出站页、严格 DTO、命令和共享组件接线 |
| vue_tests / root 指定测试路径 | 本轮正式 UI/API harness 与必要回归 |
| 独立安全/视觉 reviewer | 未参与实现的安全反例与有限浏览器审查 |
| root | Git、组合、共享文件/迁移编号及实际运行资源协调 |

不接管现有18443 production或用户配置，不因新源码出现替换运行中实例。现生产数据库包含用户新填SMTP，必须保留，旧测试数据重建授权不适用于这份数据。实施与验证使用独立配置、密钥、数据库和网络资源。

## 2. API 最小合同

所有路径以 `/api/v1` 为前缀，仅当前 SystemAdmin 可读写。每次命令及历史回执恢复重验当前 Session/System 权限；写操作保留合法 Origin、CSRF、Idempotency-Key。401与403沿现有账号/系统权限代际处理，403不伪登出个人会话。

| 方法/路径 | 语义 |
| --- | --- |
| `GET /system/outbound-policy` | 保留现有 `{version,rules}`，数据库当前完整观察，无分页。 |
| `PUT /system/outbound-policy` | 保留 `{expected_version,rules}` 完整替换；200历史回执 `{version,rule_count,audit_id,created_at}`，不混入当前配置。 |
| `GET /system/outbound-policy/proxy` | 200恰为 `{version,mode,gateway}`；gateway为null或 `{protocol,host,port,username,credential_present}`，不返回密码。 |
| `PUT /system/outbound-policy/proxy` | JSON恰为 `{expected_version,mode,gateway,credential_action,password?}`；gateway为null或 `{protocol,host,port,username}`。200历史回执恰为 `{version,audit_id,created_at}`，当前配置另GET。 |

现有规则继续复用唯一 DecodeRules：最多256条规范 RFC1918/ULA CIDR，ports为显式`"all"`或1–256个互异端口1–65535；allow_http默认false，响应显式给出。空集合真实表示不放行私网；顺序不表达优先级，规范重复项拒绝。版本为规范正int64十进制字符串；回执rule_count也是规范十进制字符串，不用JS number比较版本。无新lookup、任意URL测试、公开代理或Reload API。

代理配置默认version `"1"`、mode=`direct`、gateway=null。mode仅`direct`/`proxy`，协议仅`http`/`https`/`socks5`；port是1–65535整数。host与port独立输入，沿后端规范ASCII域名/IP解析，不能带URL userinfo/path/query/fragment。GET/PUT拒query；GET拒body，PUT拒未知/重复/null必填字段、尾随值和非法UTF-8。规则保留现有1MiB请求/rules512KiB上限；代理请求有界，不复用规则大体积预算。版本为规范正int64字符串，audit_id为UUIDv7，created_at为规范UTC微秒时间。

`direct`可保留完整gateway与已存凭据但不使用；正常切回直连携带完整gateway，不能隐含清除设置。`proxy`必须有gateway。gateway=null仅允许direct且credential_action=`remove`，表示明确清除网关/认证。

credential_action仅`keep`/`remove`：空密码输入省略password，keep保留；keep加非空password替换；remove禁止携password且username必须空。用户名与已有/新密码成对校验，username最多255 UTF-8字节、无控制字符，HTTP/HTTPS不接受冒号；password为1–255 UTF-8字节，不含NUL/CR/LF，不trim。网关身份为protocol/规范host/port/username；已存凭据时更改任一身份字段，拒绝keep且无新password，须明确提供新密码或remove，不能将旧密码带往新网关，direct模式保留gateway时同样适用。密码不回显，移除认证为次级确认动作；不把清空输入当删除。模式切换不隐含编辑访问规则。

保存配置只验证合法性与持久化，不连接外网、不测试认证、不表示网络或SMTP可用。成功保存对新调用生效，不能显示为“连接成功”。凭据由成熟版本化keyring与认证加密保存；幂等摘要沿受保护的keyed MAC，receipt/Audit/错误/日志均不含明文或可离线枚举的裸密码摘要。

## 3. 命令、并发与恢复

- 规则与代理独立version/CAS，分别有命令身份；同一用户、原key与同义body返回原历史回执，不增加版本或Audit，也不发布旧配置。不能用新key绕过未决结果。
- 400只保字段错误；409保非秘密草稿并显式加载/采用当前版本，不自动覆盖；401清身份数据，403清系统数据/草稿并冻结管理命令，重验权限后才能恢复读取。密码不写浏览器存储、路由、错误、截图或日志。
- 合法commit_state为not_started、not_committed、committed、unknown，原样解释。断响应、缺失/非法投影和未知提交保留原命令；已知committed但回执未完整取得保留“已提交、确认信息未完整”含义，不能转为未执行。
- 未决命令冻结可改字段；显式恢复使用原PUT/key/body和当前CSRF。GET只能观察当前配置，不证明原命令接受/未接受，不自动解除冻结。密码仅在当前命令的私有内存意图内保留，完成/放弃/身份失效后释放；重新输入不能被偷偷替换为原请求。刷新页面若无法恢复原敏感body必须说明限制，不自动生成新保存。
- 同事务保存配置、原命令回执与安全Audit；规则或代理任一提交Unknown时，共享发出门fail closed。另一资源保存成功或旧receipt重放均不能误恢复整体可用；只有核实两部分当前持久快照及未决提交结果后才能恢复镜像。数据库不可用/解密失败不能转为默认direct。
- 代理与规则共用发出准入门。每次新请求、重试、redirect、池连接复用及SMTP BeginSend重验当前规则/代理版本；新代理版本不复用旧模式/网关/凭据连接。已真正发出的在途调用继续，保存不撤回外部副作用。实现保持单Central正式前提，不虚构跨实例广播保证。

## 4. 目标与代理的安全边界

| 检查点 | 必须行为 |
| --- | --- |
| 业务目标 | 保留原URL/协议、完整A/AAAA、固定禁止地址、CIDR/端口与HTTP许可；混合允许/禁止DNS结果拒绝。 |
| 隧道目标 | HTTP(S) CONNECT及SOCKS5只传已批准数值IP；原域名仍用于目标TLS/HTTP Host/凭据作用域，不提供remote-DNS绕过。 |
| 网关自身 | 显式配置的公网或RFC1918/ULA私网可用，支持企业单标签网关；完整解析并固定拨号IP，仍拒loopback/link-local/metadata/其他禁止地址。网关许可不放行同网段业务目标。 |
| TLS | HTTPS网关单独验证CA/hostname；隧道内目标TLS/STARTTLS保留原校验与部署CA，无skip verify或TLS失败降级。 |
| 凭据 | 代理认证只发当前网关，业务凭据只发授权目标；CONNECT/SOCKS不带业务headers/body。HTTP/SOCKS认证到网关无协议加密，页面作必要提示。 |
| 失败 | 网关DNS/TCP/认证/CONNECT/SOCKS/TLS失败均不直连、不读env、不以另一条路重新发业务。 |
| 发送事实 | CONNECT/SOCKS/TLS握手不算业务sent；Model/HTTP首实际业务写、SMTP AUTH/MAIL前门禁与原sent定义保持。 |
| 资源与输出 | 有界DNS/拨号/握手/响应，取消和drain覆盖建立中隧道/原socket；错误仅闭集阶段，不回显代理原响应、敏感URI/用户名/密码。 |

代理是显式受信基础设施。客户端校验与发送数值目标，不声称能独立证明代理内部的实际远端连接；网关部署仍须遵守此信任边界。无法本地解析/批准目标时失败，不临时委托网关DNS。

backend须盘点实际Central发出入口与组合根：受控HTTP client、Provider/SDK transport、MCP/OAuth及其派生HTTP、账号SMTP、其余已存在业务HTTP/下载。逐项记录绑定或受同端口覆盖的依据；既有文档的“长期应接入”不代实际源码完成。不得因本任务擅自恢复其他业务STOP或新增不存在的消费者。

## 5. 页面与状态

沿已确认1200px设置整框、左齐开放表单和共享控件。在唯一页头下用“访问规则 / 代理设置”Tabs；不重复装饰description。页面级刷新在标题右侧，表单保存/取消就近；有草稿或未决命令时不因切Tab丢输入或暗中保存。

访问规则展示真实完整集合、空态和加载/失败状态；新增/编辑CIDR、端口范围及HTTP许可，删除显式确认，保存完整集合。代理页先选择直连/代理，再配置协议、host、port及可选认证；读取只显示是否已保存密码。只保必要提示：新调用生效而在途不撤回、代理失败不自动直连、相应网关协议的明文认证边界。失败不伪造空配置。

保存正常、字段错误、409、权限丢失、Unknown与原请求恢复必须可观察且有下一步；长表单错误聚焦到可见反馈或首错误字段，模态Esc/关闭回原触发器，滚动与Tab状态保持。未增加真实外网探测按钮或SMTP测试入口。

## 6. 本轮完成出口

1. 数据库迁移和原配置兼容：默认direct、旧规则保留、重启保持配置/凭据；产品不读代理env，宿主/工具环境不变。
2. 真实管理员API与新Vue首链：读取→保存规则/代理→后继受控调用实际换路→新读取；当前权限/CSRF、409、同key历史receipt与未知恢复、秘密不泄露。
3. 隔离真实代理/目标：HTTP CONNECT、HTTPS CONNECT、SOCKS5，匿名/账号认证，目标TLS、SMTP受控握手/发送门；拒绝场景证明零目标业务发出及无直连回退。真实外部邮箱不在本轮验证内。
4. 当前策略竞争：已发请求继续，新请求/redirect/池复用/SMTP BeginSend采用新版本；Unknown镜像关闭、取消/drain无遗留连接，凭据更新不复用旧隧道。
5. Central各既有业务消费者接线核对及必要代表真实调用；DB/Object/Runner明确排除。不以仅新代理库通过宣称全部Central已完成。
6. 有限浏览器浅深/宽窄、键盘/焦点/缩放，受影响格式、类型、build与必要unit；独立安全审查本轮新增凭据/网关/未知提交边界。旧业务证据只按原输入复用，不抄全量历史。

## 7. 当前检查点

- 前端候选已实现双Tab、严格DTO、规则/代理命令及恢复状态。作者完整check为55项unit及格式/类型/build通过；后续两项P2分别修复回焦与“严格回执成功、后读失败”时误显直连/空规则，增量格式/类型/build及作者/独立受控复验通过，未重跑55项。已确认回执与读取失败分开显示，恢复只GET，不猜当前配置。
- 独立受控UI已查看四视口、浅深、两Tab、原生200%、焦点与减少动效；两次初期加载失败原因未定，保留原材料，后续通过不回填根因。这些拦截响应与合成身份结果不代真实API、代理通道或公网验证。
- 新Vue与owned TLS5260 r3的规则、代理、权限三个真实旅程已分轮通过：规则增改删/刷新/409，密码空保持/替换/移除、安全投影，真实提交丢响应的同key/body恢复、严格回执后读失败仅GET，以及普通身份403/缺CSRF拒绝。首死Vite502与准备/导航/回读失败均留在harness；其中未证原因不由后续通过倒推。
- 后端生产源码6ea的root HTTP API→认证代理→Secret借还→owned HTTPS、坏网关不直连→direct代表经作者及独立运行通过；18项transport定向race与环境毒值门通过。Model/MCP/OAuth/SMTP沿同root装配；没有独立web-fetch消费者，不虚构该链，也不称所有Provider逐一端到端或PG race通过。
- 独立真实PG权限/Secret与Unknown两代表已通过；旧a37同输入反例失败后，6ea确认held COMMIT实际settle/writer join、原key恢复双snapshot及另一资源成功不提前解禁。最后live lease活性门仍在跑：先以正式StopAdmission/Drain释放spool、保实际ProcessGuard活，再验证保护与可靠停止后的回收。原双root共spool的fixture准备失败不算产品缺陷。两个既有受影响selector已实际通过；原等待预算/selector/输入错误保留，产品deadline未放宽。
- r3已优雅退出0，专属3容器/3网络清零，5260无监听且runtime/sidecar撤；frontend5246保留，用户18443及SMTP未动。候选普通build已通过，最终OpenAPI embed与组合构建仍待核对。
- 保数据manager的Popen后登记失败问题经同.next故障独立复验关闭：候选实际回收、保留selected release且拒旧版回退，同版可恢复，合成配置/数据/密钥未变。实际production stage、备份、停机均未执行；运行顺序与143→144恢复边界见[持久实例stage与恢复](../backend/account.md#既有持久实例的stage与恢复)。nginx保持原基础设施身份，候选另核实际进程与binary，不套用旧probe的source相等假设。

不操作现production或真实SMTP。精确输入、失败与下一步见[当前状态](../../../.agent-state/current.md)，命令和行为结果由[出站harness](../../../tests/frontend-outbound-policy/README.md)维护。
