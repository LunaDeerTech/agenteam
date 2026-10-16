# Central 出站规则与持久代理

状态：持久代理基线已交付；本轮按用户新决定实现全网白名单/黑名单、IP/域名目标与紧凑规则表格，尚待新模型验证与部署。本卡仍为唯一工程合同，架构见[出站网络策略](../../architecture/platform-infrastructure/outbound-network-policy.md)。用户明确不保留旧访问规则或旧写接口；精确规则重置不扩大为清空账号、SMTP或代理。公网身份未验证，真实SMTP暂停，Runner及部署基础连接继续排除。

## 1. 目标与分工

管理员在新 Vue `/system/outbound-policy` 配置访问规则与出口代理，保存后 Central 新业务调用立即使用，无需重启。现有全部 Central 业务 HTTP/HTTPS、Model、MCP（含 OAuth、发现、下载等派生调用）与 SMTP 接入同一受控出网模块；业务权限、目标认证、重试和业务结果仍由各自模块负责。

Runner、数据库、MinIO 等部署基础连接不纳入本轮。移除产品 `OUTBOUND_PROXY_ORIGINS` 和 HTTP(S)_PROXY/NO_PROXY（含小写）选路读取，没有环境兜底或自动迁入；不修改宿主环境、frp、开发工具。真实 SMTP 发送仍暂停，只能用任务所有的隔离 SMTP/代理验证。

| Owner / 工作树 | 唯一责任 |
| --- | --- |
| account_contract / `/workspace/agenteam` | 本卡、出站架构、main current |
| account_backend / `/workspace/agenteam-rule-modes-backend` | 存储/迁移、API/OpenAPI、安全选路、Central 消费者装配及后端说明 |
| proxy_copy / `/workspace/agenteam-rule-modes-vue` | 新 Vue 出站页、严格 DTO、命令和共享组件接线 |
| vue_tests / root 指定测试路径 | 本轮正式 UI/API harness 与必要回归 |
| 独立安全/视觉 reviewer | 未参与实现的安全反例与有限浏览器审查 |
| root | Git、组合、共享文件/迁移编号及实际运行资源协调 |

实施与验证使用独立配置、密钥、数据库和网络资源，不因新源码出现替换运行中实例。用户本轮明确允许舍弃旧访问规则；只重置该资源及其规则回执，不转存旧语义、不做旧接口兼容。18443现有账号/Session、用户SMTP、代理配置及凭据、对象数据和keyring必须保留；不据此重建整个实例或清空数据库。

## 2. API 最小合同

所有路径以 `/api/v1` 为前缀，仅当前 SystemAdmin 可读写。每次命令及历史回执恢复重验当前 Session/System 权限；写操作保留合法 Origin、CSRF、Idempotency-Key。401与403沿现有账号/系统权限代际处理，403不伪登出个人会话。

| 方法/路径 | 语义 |
| --- | --- |
| `GET /system/outbound-policy` | 200恰为 `{version,rule_mode,rules}`，数据库当前完整观察，无分页。 |
| `PUT /system/outbound-policy` | JSON恰为 `{expected_version,rule_mode,rules}`，模式与整表原子替换；200历史回执仍为 `{version,rule_count,audit_id,created_at}`，不混入当前配置。 |
| `GET /system/outbound-policy/proxy` | 200恰为 `{version,mode,gateway}`；gateway为null或 `{protocol,host,port,username,credential_present}`，不返回密码。 |
| `PUT /system/outbound-policy/proxy` | JSON恰为 `{expected_version,mode,gateway,credential_action,password?}`；gateway为null或 `{protocol,host,port,username}`。200历史回执恰为 `{version,audit_id,created_at}`，当前配置另GET。 |

规则 `rule_mode`必填且仅为 `allowlist`（白名单）或 `denylist`（黑名单），覆盖全部公网与私网。`rules`最多512条，每项恰为 `{target,ports}`，不接受旧 `cidr`、`allow_http`或缺少模式的body。ports为显式 `"all"`或1–256个互异端口1–65535。模式、目标、端口共同形成完整命令；顺序无优先级，规范化后重复项拒绝。版本为规范正int64十进制字符串，回执rule_count也为规范十进制字符串且最多512，不用JS number比较版本。无新lookup、任意URL测试、公开代理或Reload API。

`target`统一接受单IP、CIDR、精确域名或 `*.domain`。单IPv4/IPv6规范输出为 `/32`或 `/128`，拒绝IPv4-mapped及zone；CIDR须规范书写并位于网络边界，可覆盖公网/私网及宽网段。域名沿唯一host校验：ASCII、小写、去一个合法末尾点、合法label，拒绝模糊数字主机；不接受scheme、userinfo、path、query、fragment或附带端口。通配只允许最左侧一次 `*.`，后接合格DNS名称；匹配一个或多级子域，不含根域，不作子串匹配。只匹配原请求的规范hostname，不使用CNAME或反向DNS扩权；IP字面目标按IP/CIDR匹配。

先检查metadata hostname及完整A/AAAA的固定危险地址，任一命中即拒绝，域名规则和宽网段不能绕过。白名单中，原域名与端口命中同一域名行则允许其全部非硬禁解析IP；否则每个解析IP均须分别命中某条IP/CIDR与端口。黑名单中，任一域名行或任一解析IP与端口命中即拒绝整个目标。白名单空表拒绝全部；黑名单空表允许其余目标，固定危险地址始终拒绝。HTTP与HTTPS不再有独立网络许可，均按上述规则；OAuth等协议固有的HTTPS要求、HTTPS→HTTP重定向禁止、TLS验证和凭据作用域保持。

用户明确不要求旧规则迁移或兼容。数据库结构升级 `00145`只将访问policy重置为version `"1"`、`denylist`、空rules，并删除该资源的旧规则receipts；新装使用相同默认，普通目标无需预先配置，固定危险地址仍拒绝；未加载、读取失败或Unknown不能冒充默认空黑名单。不存在38条补集、四条默认黑名单或隐藏legacy模式。旧格式请求400，不重放旧format1回执；新规则命令使用 `outbound-policy-v2` namespace及含mode/规范target规则的format2语义摘要，避免旧key身份别名。proxy独立version/配置/凭据及其receipts/operations不重置，其他业务数据和Audit不清除。命令pending为进程内状态，结构升级须在旧Central实际停止后进行，不直接清其他恢复记录代替join。

代理配置默认version `"1"`、mode=`direct`、gateway=null。mode仅`direct`/`proxy`，协议仅`http`/`https`/`socks5`；port是1–65535整数。host与port独立输入，沿后端规范ASCII域名/IP解析，不能带URL userinfo/path/query/fragment。GET/PUT拒query；GET拒body，PUT拒未知/重复/null必填字段、尾随值和非法UTF-8。规则JSON与整PUT均设1MiB上限，以完整body上限作为最终约束；代理请求有界，不复用规则大体积预算。版本为规范正int64字符串，audit_id为UUIDv7，created_at为规范UTC微秒时间。

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
| 业务目标 | 保留原URL/协议、完整A/AAAA与固定禁止地址；执行全网模式/目标/端口规则，混合允许/禁止DNS结果拒绝，HTTP无独立网络开关。 |
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

访问规则先显示“规则模式”下拉白名单/黑名单，随后使用紧凑表格编辑“目标 / 端口 / 操作”，删除位于每行末尾并显式确认；移除允许HTTP开关。支持新增与完整集合保存，窄屏表格在自身区域处理宽度，不让页面横溢。切模式保留未保存行，不隐式保存；空表明确当前模式的全拒或允许其余目标含义。保留真实加载/失败/空态，不把读取失败当默认空黑名单。代理页先选择直连/代理，再配置代理服务器类型、host、port及可选认证；读取只显示是否已保存密码。只保必要提示：新调用生效而在途不撤回、代理失败不自动直连、相应网关协议的明文认证边界。失败不伪造空配置。

保存正常、字段错误、409、权限丢失、Unknown与原请求恢复必须可观察且有下一步；长表单错误聚焦到可见反馈或首错误字段，模态Esc/关闭回原触发器，滚动与Tab状态保持。未增加真实外网探测按钮或SMTP测试入口。

## 6. 本轮完成出口

1. 精确结构升级：仅访问规则及其回执重置为version1/黑名单空表，proxy与账号/Session/SMTP/密钥等原样；旧body拒绝，重启保持新模式/目标，产品仍不读代理env。
2. 新模型真实管理员API与Vue首链：读取→选择模式/增改单IP、CIDR、域名、通配目标→保存→真实受控调用→新读取；两模式空表、同域多级/apex边界、混合DNS及固定危险拒绝，HTTP统一与协议自身HTTPS边界；权限/CSRF、409、新命令同key回执与未知恢复仍有效。
3. 隔离真实代理/目标：HTTP CONNECT、HTTPS CONNECT、SOCKS5，匿名/账号认证，目标TLS、SMTP受控握手/发送门；拒绝场景证明零目标业务发出及无直连回退。真实外部邮箱不在本轮验证内。
4. 当前策略竞争：已发请求继续，新请求/redirect/池复用/SMTP BeginSend采用新版本；Unknown镜像关闭、取消/drain无遗留连接，凭据更新不复用旧隧道。
5. Central各既有业务消费者接线核对及必要代表真实调用；DB/Object/Runner明确排除。不以仅新代理库通过宣称全部Central已完成。
6. 有限浏览器浅深/宽窄、键盘/焦点/缩放，受影响格式、类型、build与必要unit；独立安全审查本轮新增凭据/网关/未知提交边界。旧业务证据只按原输入复用，不抄全量历史。

## 7. 当前检查点

本轮规则模式、target与紧凑表格契约已定，前后端隔离实施中，尚无新模型通过或公开部署结论。下列为上一轮代理基线的已有证据，只按原输入复用，不代本轮新增验收。

- 前端候选已实现双Tab、严格DTO、规则/代理命令及恢复状态。作者完整check为55项unit及格式/类型/build通过；后续两项P2分别修复回焦与“严格回执成功、后读失败”时误显直连/空规则，增量格式/类型/build及作者/独立受控复验通过，未重跑55项。已确认回执与读取失败分开显示，恢复只GET，不猜当前配置。
- 独立受控UI已查看四视口、浅深、两Tab、原生200%、焦点与减少动效；两次初期加载失败原因未定，保留原材料，后续通过不回填根因。这些拦截响应与合成身份结果不代真实API、代理通道或公网验证。
- 新Vue与owned TLS5260 r3的规则、代理、权限三个真实旅程已分轮通过：规则增改删/刷新/409，密码空保持/替换/移除、安全投影，真实提交丢响应的同key/body恢复、严格回执后读失败仅GET，以及普通身份403/缺CSRF拒绝。首死Vite502与准备/导航/回读失败均留在harness；其中未证原因不由后续通过倒推。
- 后端生产源码6ea的root HTTP API→认证代理→Secret借还→owned HTTPS、坏网关不直连→direct代表经作者及独立运行通过；18项transport定向race与环境毒值门通过。Model/MCP/OAuth/SMTP沿同root装配；没有独立web-fetch消费者，不虚构该链，也不称所有Provider逐一端到端或PG race通过。
- 独立真实PG权限/Secret、Unknown两代表及lease活性门均通过；旧a37同输入反例失败后，6ea确认held COMMIT实际settle/writer join、原key恢复双snapshot及另一资源成功不提前解禁。lease使用合法durable-state注入及真实ProcessGuard生命周期：guard仍活时恢复保留lease/operation，旧process实际join后才释放并使HTTP remove成功，不称SIGKILL。原双root共spool准备失败不算产品缺陷；两个既有受影响selector已实际通过。PG非race，原准备/输入错误保留，产品deadline未放宽。
- r3已优雅退出0，专属3容器/3网络清零，5260无监听且runtime/sidecar撤；任务专用frontend5246也已精确清退，主树用户5245保留。最终普通build与VCS clean通过，实际生产输入与集成主树一致；`api/openapi/outbound-policy.json`不在该cmd的Go/Embed依赖中，此前认为需要重做embed构建的待办已纠正。
- 保数据manager登记失败的同.next故障已独审关闭。实际持久实例已stage、私有一致备份并切换到新binary，schema144；停前后用户/Session、完整SMTP及凭据、旧规则、keyregistry相等，原immutable文件/数据目录与基础容器保持，未发邮件。首次备份验证因buffered FD offset失败，修复读取方式后同一dump经PG17.8 `pg_restore --list`通过，原失败保留。严格本机TLS/nonce、live/login与匿名管理拒绝通过；ready503仍为D05 DEPENDENCY_UNBOUND，首断言未保存详情不追认原因。最终只读浏览器在严格本机TLS/实际证书与nonce门后确认登录、新Vue模块、规则/代理读取与显示，退出Session/浏览器/relay并保留原服务；未执行业务写入或SMTP。本机CONNECT保持public origin，不代证公网链路，公网严格读取仍未取得nonce。

恢复与143→144边界见[持久实例stage与恢复](../backend/account.md#既有持久实例的stage与恢复)。nginx保留原基础设施身份，新backend另核active-release、实际进程/exe SHA，前端按实际集成源核对，不套用旧probe的source相等假设。

真实SMTP继续暂停，保留现服务与用户数据供预览。精确运行身份、预览检查及限制见[当前状态](../../../.agent-state/current.md)，隔离UI/API命令和结果由[出站harness](../../../tests/frontend-outbound-policy/README.md)维护。
