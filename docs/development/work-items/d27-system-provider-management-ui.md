# D27：System Provider 管理、凭据替换与 Models 只读子列表

状态：rev3，2026-10-06 已获 `recovery_verification`（verification_worker）独立静审通过（STATIC PASS），主线程已采纳；被审稿 SHA256 `c859524983cfb0c51f96d7cd20ee2871d0fc013498eff1cbbd109590615395c4`。rev2 已获 `recovery_documentation`（verification_worker）独立静审通过（STATIC PASS）并由主线程采纳，被审稿 SHA256 `634af3d0b2fa337cfd6a60aa568c6e8825b3f9238cc24c6f12325131847a3580`。邀请 UI 已独立最终 PASS、主线程采纳并提交推送 `1c82d888adfef0d8b58ab51c920557ca4e2f084f`（六组新真实顶层、五组旧真实回归及453项纯测通过），所属真实资源已双次清零交回；本卡前端固定消费该提交，后端仍为 `9b3201547f9b7b61fd9716a6ba6540084961496c`及迁移1–19的已接受兼容组合。主线程已确定 `directory_backend`（改任frontend_worker）为§6原二十一条路径的唯一作者；待本卡提交后再由主线程正式下发产品写入与独占真实窗口，当前不自行启动。本次仅更新页首采纳及实施安排，技术§1–7保持被审原字节；下文设计冻结时的待授叙述以本页首为准。规格通过和上游接受不代表本卡产品通过。

rev1独立静审为BLOCKED：合法Provider页经Go默认JSON转义可超过600000B；未发现其他规格阻断。rev2仅将listProviders成功JSON响应窄扩为2MiB，补完整上界、限定路径用途及相应验收；该修订已独立复审通过。rev3仅固定邀请已接受接缝，明确App期状态/Promise与View末尾确认宿主分工、checking失败恢复和旧实例焦点门禁，补已定分域/CSRF规则的固定接入注意与对应验收；API、请求/响应预算、二十一条路径、两步提交与敏感材料恢复规则不变。

## 1. 完整结果与真实依赖

当前系统管理员在“模型与 Provider → Providers”中分页查看、创建、编辑、删除 System Provider，安全替换其 Credential，并查看选中 Provider 的 Models 只读子列表。凭据替换固定为“创建新 Credential → 独立 Provider 创建/绑定”两次命令，保留部分成功及结果未确认的恢复能力；不原地轮换可能共享的旧 Credential，不自动补偿删除任何 Credential。Models 创建/编辑/删除、删除替代模型、平台 selector、Project、Provider 调用、发现与连接测试均不在本卡，也不为它们造空页或禁用入口。

| 依赖 | 本卡消费与门槛 |
| --- | --- |
| Account 与既有设置 UI | **已满足**，[认证](../agent-team/d26-authentication-verification.md)、[个人设置](../agent-team/personal-settings-verification.md)、[公开入口](../agent-team/public-account-entry-verification.md)、[系统用户目录](../agent-team/system-user-directory-ui-verification.md)均有接受证据；目录产品 `7ef3e30` 和遮罩焦点修复包含在固定前端中。复用当前 Session/admin、Cookie owner、身份草稿与导航确认。 |
| 系统邀请 UI 及多叶子设置壳 | **已满足**，[邀请 UI rev4](d27-system-invitation-ui.md)已独立最终 PASS、主线程采纳并提交 `1c82d888adfef0d8b58ab51c920557ca4e2f084f`，资源已清零。该固定输入已有受限 group key/children、useSession第三可选依赖及分域owner、parseSystemInstant导出、第六项return，以及App期状态/Promise与View末尾确认宿主；并包含已接受[剩余模态焦点恢复](d27-modal-focus-restoration.md) `79f922e`。本卡仅消费这些接缝，Provider第四依赖/新域与自身页面仍须实施验收。 |
| System 配置、HTTP 与生产根 | **已满足**，[B01-K](d09-b01-system-configuration.md) `543511c`、[HTTP 验收](../agent-team/d09-system-model-http-verification.md) `ac5b4c6`、[默认根验收](../agent-team/system-model-root-verification.md) `457b197`，均包含在 `9b32015`。消费真实 Provider CRUD、按 Provider 分页 Models、Credential create、两种命令 lookup、同事务当前权限、Secret reference、Audit 和事件事实。 |
| Credential metadata | **已满足**，[管理读口验收](../agent-team/system-model-management-reads-verification.md) `ecd7337`包含在 `9b32015`。读取当前 System/Model 安全 metadata，包含 HTTP 认证的三秒预算、同事务权限及失败零候选；不需要本卡改后端。已验 Model deletion-impact 不在本卡调用范围。 |
| 布局与协议 | [系统设置 §3](../../frontend-design/layouts/system-settings.md#3-模型与-provider)、[通用设置壳](../../frontend-design/layouts/settings-shell.md)、[样式](../../frontend-design/styles/README.md)、[组件](../frontend/components.md)、[Model Configuration](../../architecture/platform-infrastructure/model-system/model-configuration.md)、[HTTP 契约](recovery-d09-system-model-http.md)、[管理读口契约](recovery-d09-system-model-management-reads.md)、[OpenAPI](../../../api/openapi/model-system.json)。具体 wire 与支持范围以固定已接受实现/契约为准。 |

本结果不依赖待决 Summary 初值、未验 tools、Project 或实际 Runtime，未发现需要用户决定的产品含义。配置保存仅表示管理命令接受，不能显示“连接成功”“模型可调用”或协议兼容已验证。Provider.protocol 创建后不可改；当前 providerPolicy 仅接受 options={}。这些已存在的限制不能用表单或 stub 绕过。Object 原安全筛查停止任务及 tools 原独立停止任务保持停止；完整 D09/D27、ready503、生产 SPA 托管及真实 Vite 代理的原边界不变。

## 2. 页面与操作

仅新增懒加载 `/system/providers`，继承 authentication/protected/systemAdmin 元数据；登录 return 闭集从邀请 UI 的六项增至七项，只增加该精确路径，拒绝 query/hash、数组、外部 URL 及未知子路径。`/system` 仍默认 `/system/users`。SystemSettingsView 保留“用户与邀请”及其两个叶子，追加稳定 key 为 `models-providers` 的“模型与 Provider”组，children 仅有“Providers”；直接消费已接受 SettingsShell 多叶子接口，不修改公共壳。普通身份不显示入口，直链不挂载页面、不发 Provider/Model/Credential 请求。

同一叶子中提供 Provider 列表与选中项详情，详情有“返回 Providers”；选中 ID/编辑模式仅在本页私有状态，不增加动态详情路由、URL query 或 history.state。列表显示 name、protocol、base_url、enabled；详情补完整安全配置、版本/创建与更新时间、凭据状态和 Models 子列表。名称/URL 均以文本渲染，URL 不自动加载、探测或变为外部导航。Models 只显示名称、原生 provider_model_id、type、enabled 和所属 Provider，不提供任何 Model 写操作、参数编辑、selector 跳转或全域 Models 列表。

| 操作/区域 | 既定行为与成功含义 |
| --- | --- |
| 创建 Provider | UiDialog 表单含 name、protocol、base_url、enabled 和可选新凭据；协议与启用状态要求明确选择，不从 fixture 或 URL 猜默认。协议仅为 openai-chat-completions、anthropic-messages、openai-embeddings、jina-rerank、openai-images-generations。基础校验通过后才派发；无新凭据时直接以 credential_ref:null 创建，有新凭据时按 §4 两步执行。 |
| 编辑 Provider | 以独立 GET 的当前版本建立草稿，完整 PUT，不是 PATCH。protocol 只读并原样提交；options 固定 {}，显示当前无额外可配置选项，不提供任意 JSON 编辑器。空白凭据输入保留原 credential_ref，输入新材料才进入替换流程。本卡不提供输入任意旧 ref、共享原地轮换、解除绑定或 Credential 删除入口。 |
| 凭据状态 | credential_ref=null 才是“未配置”；非 null 时独立读 metadata，成功显示“已配置”及安全版本，不回显旧明文。404 显示“引用不存在或不可读取”，其他失败显示“凭据状态读取失败”；都不偷偷置空 ref、猜成功或清除编辑草稿。metadata 只观察当前 ref/purpose/version，历史 create receipt 不证明当前存在。 |
| 保存 | 明确保存/取消；未修改的编辑表单不派发无变化PUT，保存中锁定该草稿、防重复提交。严格 Provider receipt 才显示“Provider 配置已保存”；新 Credential receipt 单独只确认“新凭据已创建”。随后当前 GET/列表失败时保留写确认，显示“操作已确认，当前配置读取失败”，仅重读，不重发已确认的写。 |
| 删除 Provider | 必须确认名称与范围，取消零请求。仅当本 Provider 的 Models **首项读取**成功且为空时允许进入确认；非空说明需先完成 Models 删除/替换，读取失败说明需先重读，不能把后续空页当零 Models。实际 DELETE 仍使用捕获的 ProviderID/version，同事务裁决并发 Model 新增与权限；不级联、不调用 Model 删除。成功为200 receipt而非204，不提前移除列表项，不删除旧 Credential。 |

输入不偷偷 trim、改写协议、规范化/补全 base_url 或自动附加 `/v1`。名称沿后端1–128 Unicode字符、无NUL；base_url为1–8192 UTF-8 bytes的合法HTTP/HTTPS地址，禁止用户信息、query、fragment等既有非法项，最终校验仍由服务执行。以普通文本输入避免浏览器 URL 控件的隐式改写；现有 Provider 的 enabled=false 不等于认证/连接失败，启用也不证明运行链已绑定。

新凭据采用空白、明确标注“仅写入”的私有输入，可复用 UiTextarea 支持不经应用 trim/换行改写的材料；关闭自动补全/拼写检查，不提供复制、下载、显示旧值。输入只用于本次替换，页面不展示凭据内容摘要。清空尚未提交的新输入恢复“保留旧凭据”；一旦已派发，不能以清空字段修改原命令。临时 Session checking 后可用“已保留新凭据输入”安全状态提示，不必把私有材料重新回显到 DOM。

Provider、Models、metadata 分区分别加载并独立显示安全错误；一个读取失败不把另一个已确认事实改为空值，但当前身份失效须全部清除。成功反馈沿 UiButton 2400ms及可访问状态播报。长名称、原生模型ID、URL、时间和错误可换行，本页局部覆盖表格 nowrap；窄屏行按字段堆叠、表单单列、详情可返回列表，无页面级横向溢出。菜单760px断点、Dialog/Drawer键盘、Escape/遮罩、焦点恢复与 reduced-motion 沿已验公共组件；所有业务关闭路径统一经过 dirty 判断。

## 3. 固定 API、严格解析与材料预算

新增 `web/src/api/system-providers.ts`，提供只读 Provider/Model/Page/Metadata/Receipt 类型及 `SystemProviderAPI`。复用 `accountTransport` 的同源 Cookie、no-store、redirect:error、安全 Problem和实际 body/cancel join；仅listProviders成功200 application/json响应上限为2MiB（2097152B），所有Problem及其他endpoint响应保持600000B。沿邀请 UI 已导出的严格 Instant 解析器，不放宽 Account User/SystemUser/邀请形状。

响应预算只由transport内部按固定endpoint与成功状态选择，不接受调用方limit或任意路径放大。沿流式读取累计实际body字节，不仅信Content-Length；超过本请求上限返回invalid-response、零候选，仍等待read/cancel实际尾部结束后才能释放owner。固定后端WriteJSON使用默认json.Marshal，合法base_url路径中的&、<、>可各转义成6B。保守上界：每行URL≤6×8192=49152B、名称≤6×128=768B、其他字段和标点≤512B（含两个UUID、最长protocol、19位version、两个27B时间、false及options={}），故每行≤50432B；25行加cursor保守6×8192及逗号/页包装≤128B，总计≤1310080B，小于2MiB。单项Provider及当前policy下的Model页仍在600000B内；不因同属Model管理而扩大其他响应。

| 方法 | 唯一路径、输入及成功 |
| --- | --- |
| listProviders | GET `/api/v1/system/model-providers?limit=25[&cursor=…]`，200 ProviderPage。 |
| getProvider | GET `/api/v1/system/model-providers/{id}`，200 Provider，返回id必须等于捕获目标。 |
| createProvider | POST `/api/v1/system/model-providers`，body `{input}`，200 Provider command receipt。 |
| updateProvider | PUT `/api/v1/system/model-providers/{id}`，body `{expected_version,input}`，200对应receipt。 |
| deleteProvider | DELETE `/api/v1/system/model-providers/{id}`，body `{expected_version}`，200对应receipt。 |
| listModels | GET `/api/v1/system/models?provider_id=…&limit=25[&cursor=…]`，200 ModelPage；provider_id必填且唯一。 |
| createCredential | POST `/api/v1/system/model-credentials`，body仅 `{value}`，200 Credential create receipt。 |
| getCredentialMetadata | GET `/api/v1/system/model-credentials/{id}`，200 `{credential_id,purpose,version}`。 |
| lookupProviderCommand | POST `/api/v1/system/model-commands/lookup`，body `{command}`，command仅本卡provider.create/update/delete，200 `{found,receipt}`。 |
| lookupCredentialCreate | POST `/api/v1/system/model-credential-commands/lookup`，body仅 `{kind:"create"}`，200 `{observed,result}`。 |

以上十个固定 endpoint 使用各自封闭 options；动态段只能来自先验合法 UUIDv7，query只允许表列成员，经URLSearchParams编码；旧 endpoint不能接新参数，不开放任意URL/method/header/query map。GET不带body/key/CSRF；POST/PUT/DELETE均由同owner附原Session CSRF和稳定key，**两个POST lookup也需CSRF，key是对应原命令key**。禁止调用Credential PUT/DELETE、Model写口、selector或deletion-impact。本卡不修改后端/OpenAPI。

Provider输出恰 `id,input,version,created_at,updated_at`；input恰 `name,protocol,base_url,enabled,credential_ref,options`，credential_ref显式UUIDv7/null，options严格{}。Model输出恰 `id,provider_id,input,version,created_at,updated_at`；每项provider_id必须等于查询目标，input完整校验 `name,provider_model_id,type,enabled,parameters,request_overwrite,header_overwrite,capabilities`，不能因为仅展示四字段就忽略其余。当前已接受policy要求前三个配置对象为空、reasoning_efforts为空；类型闭集与protocol/type相容性沿现有契约。capabilities恰四bool、四唯一字符串数组、context_length/max_output显式null或正int64十进制字符串，校验既有枚举、parallel依赖tool_calls及max_output≤context_length，不把数字强转、未知字段或坏行降级成功。

两个Page都恰 `{items,next_cursor}`，**next_cursor必填且可为null**，不同于Account目录的可选字段。一页0–25项，ID不重复，完整 `(created_at,id)` 严格递减；非null cursor非空且≤8192字符，仅在满25项页合法，保留为不透明原值。版本为正int64十进制字符串、affected_references为非负int64字符串；拒绝前导零、溢出、数字和非法null，>2^53精确往返。所有时间为有效日历的27字符UTC微秒，updated_at不得早于created_at；不以Date.parse修复坏日期，也不截断微秒后排序。完整解析成功才一次发布该分区的不可变结果。

Provider receipt恰 `{kind,resource_id,version,affected_references}`，kind与原操作一致，affected_references必须为"0"；create版本为"1"，update/delete的resource_id等于捕获目标、version等于原expected_version+1，使用精确整数比较。新update/delete的expected_version必须存在合法int64后继，溢出本地拒绝。lookup false必须receipt:null，true必须合法对应历史receipt，同样核该目标/版本关系；这不代替完整body重放。Credential create receipt恰 `{credential_id,purpose:"model",version:"1",deleted:false}`；lookup false必须result:null，true必须为此合法历史create结果。metadata恰 `credential_id,purpose:"model",version`，id等于目标而版本可以大于1。lookup不返回新key、明文或当前绑定；见 §4 解释其有限含义。

value为非空、合法Unicode字符串，按实际UTF-8 bytes校验1–65536，不能用JS字符串长度替代；拒绝未配对surrogate，不 trim、截断或规范化。**仅createCredential这一固定endpoint**允许序列化JSON的UTF-8 body≤512KiB；Provider create/update这两个固定endpoint允许≤32KiB，以覆盖合法8192B URL中JSON引号/反斜杠转义、名称与完整input包装，其余原Account及本卡lookup/delete仍≤16KiB。原value上限不等于JSON大小：65536个一字节控制字符在JSON中可占393216B，再加12B包装；512KiB覆盖该合法边界且低于后端1MiB。各字符串原字段长度与完整编码后字节预算都须在创建intent/key及dispatch前分别核对，越界本地拒绝、零intent/请求；待分配新credential_ref按36字符UUID的完整编码长度预留，不能先写Credential再发现Provider包装超限。transport仍防御性复核固定endpoint预算，不全局放宽、不按调用方传入limit。编码临时副本不缓存到公共状态/错误/重试队列。

敏感材料只存在输入DOM和App期私有非响应式内存以及实际请求所需临时副本；公开reactive state/provide/props、receipt、Problem/异常、console、URL/history、local/sessionStorage和持久恢复数据不得含value、完整命令、key或CSRF。私有输入到owner只能经受限提交/清空动作传递，页面仅见是否有草稿、阶段与安全反馈；不提供材料读取接口。不得承诺JavaScript运行时所有字符串可物理擦除，须及时清掉本方可达引用、DOM值及可擦除buffer，并正确保有仍在途的实际请求尾部。

## 4. 两步提交、历史查证与恢复

`createSessionController` 在邀请 UI 已接受的前三个参数之后增加第四个可选、默认真实的SystemProviderAPI，旧调用/mocks保持。在system下增加有类型的Provider读写/lookup与私有意图域；仍只有原Cookie owner和30秒可见预算。每次开始核当前authenticated、完整UserID+SessionID+epoch、admin、system未被拒绝及本页有效代次。Provider、Credential、两个lookup、Models与Account/个人/公开/用户/邀请请求共享实际owner，不增加队列或第二Session store。

一个Provider工作流只持有一个冻结草稿；新Credential和Provider两步有**不同**稳定key、各自kind/body/receipt。派发前完成所有可本地验证的Provider字段；新Credential阶段保留精确材料、key、原CSRF与identity，Provider阶段保留原完整input/expected_version、key、目标及原上下文。创建/编辑不把当前刷新值偷偷写进原intent。无新材料时跳过Credential步骤；有新材料时同一受限工作流按下列边界顺序执行，默认不要求用户另发一条新绑定动作。

1. 先POST新Credential。只有严格成功receipt才可建立已确认的新ref；未确认、lookup未见或lookup失败时一律不得进入Provider步骤。收到严格create receipt后，尽早清空输入DOM及本方材料副本；这步实际transport/body/cancel结束后不再为Provider恢复保存value。保留新ref、create receipt/key与安全阶段；已确认的create不能再次创建新凭据或要求重新输入原材料。
2. 以新ref组装一次冻结的Provider POST/PUT，保留初始捕获的Provider版本与所有其他字段；持有同一owner顺序派发，不在两步之间放行别的Cookie请求。步骤切换前重核identity/页面有效性/取消与原上下文，取消后不能自动续发绑定。两步是两个后端事务，不声称跨HTTP原子。
3. Provider严格receipt确认后显示配置保存成功，清理已完成意图，再独立读取当前Provider/metadata/列表。create receipt和Provider receipt分开留作本次安全反馈；当前读取失败不撤回任一确认。换ref或删除Provider仅由正式服务更新引用，旧Credential本身不被删除，也不使其他Provider或已有snapshot自动失效。
4. 新Credential已确认而Provider尚未成功时，显示“新凭据已创建，Provider尚未确认保存”，保留new ref/create receipt以及Provider原key/body/version/上下文。Provider明确业务拒绝后可保留普通草稿及这个prepared ref；只有原写已知未接受且用户明确读取最新Provider、核对输入后，才可用新Provider key/version重新保存，不能重新创建Credential。若仍有历史不确定性，禁止重基/换key/改body；只能原请求恢复或明确放弃。

结果分类沿已接受Account纪律，但按本卡实际写口解释：本地未dispatch、或无更早未确认尝试且服务明确not_started/not_committed的INVALID_ARGUMENT、VERSION_CONFLICT、NOT_FOUND、INVALID_STATE、CAPABILITY_UNSUPPORTED等业务拒绝，按已知失败显示；保留草稿/部分成功，不误称未确认。已dispatch的transport、取消/超时、非法响应、COMMIT_UNKNOWN、unknown/committed却无合法receipt、5xx或RESOURCE_BUSY均保留该步骤未确认。IDEMPOTENCY_KEY_REUSED锁住原intent并提示冲突，不改材料或偷偷换key。曾未确认时，后一次非成功响应不能自动抹去前次不确定性。Model底层恢复确认后正常200仍是成功；不得因其底层曾Unknown而否认合法receipt。

“检查原请求”先沿restore重验Session；只有同完整identity、原CSRF仍匹配且当前admin才可查对应原key。Credential observed=true/Provider found=true仅证明该身份历史receipt存在，**不验证原材料/完整input语义，也不证明当前对象存在或当前绑定**；保留其安全观察并要求显式“重试原请求”通过正式Execute完成原body语义核对，不把lookup直接转成保存成功或自动进入下一步。false是这次未观察到，失败是本次查证失败，二者都不证明原请求未提交、writer已终局或可以换key。一次点击只做有界查证，不轮询；Session、Provider/Models列表、metadata和版本变化均不能充当原command receipt。

“重试原请求”仅重放当前未确认步骤的相同path/kind/key/body/expected_version和有效原上下文；Credential阶段仍使用私有原value，Provider阶段只使用已确认新ref及原input，绝不把两步key互换。合法历史重放不能被当前GET缺对象或metadata404先挡掉。确认原Credential后可继续尚未派发的Provider步骤；确认原Provider后只读取。metadata是独立当前观察；服务绑定接受稳定ref并重核当前合法性，**没有credential expected_version fence**，本卡不假造材料版本原子绑定或把metadata观察当写授权。

“放弃本次操作”明确提示此前请求仍可能生效、已创建Credential可能尚未绑定；只停止前端追踪并销毁私有意图，不撤销服务端写、不自动删除新旧Credential。普通取消编辑也遵守该部分成功提示，不能声称恢复了旧绑定。Provider未确认时旧ref只是提交前观察，可能已被本次或并发写替换；新Credential成功、Provider已知拒绝也只证明本操作未完成绑定，不保证没有别的管理员改过。明确放弃、真实身份变化、失权、注销与App.dispose清材料/草稿/receipt/ref/key；不能跨账号保留或持久化恢复。页面刷新丢失私有意图不等于服务端回滚，不提供假恢复入口。

## 5. 生命周期、分页、权限与实际取消

新增App期 `createSystemProviders` 控制器，由App创建/provide并安装导航确认；私有草稿与两步进度跨RouterView的临时checking卸载保留，同一Session复查不能当作真实离页。只有Provider叶子attach且身份确认后加载；新目标、首项/下一页/上页、详情/metadata/Models读取均捕获自身目标和代次。对应分区开始读取时隐藏旧候选，失败零新候选；各读取按同owner顺序有界执行，后续分区不能在取消或失效后继续派发。首次因busy未发出的加载最多延迟一次，无自动重试或背景轮询。

Provider列表与选中Provider的Models各有独立25项cursor历史。下一页用服务端cursor，上页重新读原输入cursor，成功才换页、后退丢弃后续历史；刷新/重新进入/已确认Provider写后列表从首项重读。切Provider清旧Models/metadata/cursor，不跨目标、User或Session复用；CURSOR_INVALID明确回首项，不偷偷换目标。空页不伪造总数，全量扫描、浏览器本地搜索/排序、跨请求原子快照均不承诺。详情GET、metadata、Models不是一事务快照，过期观察/并发变更由新读取及正式写版本规则处理。

dirty包括普通字段变化、新材料、进行中/未确认写及尚未完成的部分成功。菜单、顶部本人入口/退出、浏览器返回、选别的Provider、详情返回、分页/刷新和编辑Dialog所有关闭方式统一确认“继续编辑 / 放弃修改”，并在router触发Session复查前执行。空白未提交表单、普通只读加载不额外确认。显式“检查原请求”或已知冲突后的明确重读保留原intent/草稿，不被通用刷新误清。App顶部组合现有个人、公开与邀请确认，不能绕过或清掉其他域；安装和dispose成对移除watcher、hook、beforeunload、反馈计时器。

确认状态及待决Promise归App期controller；**确认Dialog宿主固定置于SystemProvidersView的创建/编辑（含SystemProviderEditor）与删除等业务Dialog之后**，不在App新增常驻Provider确认层。所有Provider模态属于同一页面子树，按上述顺序共同挂载/卸载；确认只由本页dirty触发，离页guard与顶部退出均在真正离开前等待，不为其他叶子保留全局宿主。App的pageshow/visibilitychange复查条件不变，不因确认打开而跳过；checking期间RouterView卸载所有Provider模态，页面detach只撤离读取与DOM，不结算确认或销毁按§3/§4仍需保留的私有草稿/材料/意图。检查失败继续隐藏Provider内容，App恢复按钮不受残留overlay/inert/滚动锁阻挡；同一完整identity且仍为admin时按固定顺序重挂原业务层与末尾确认，恢复本身不发新写、不换key、不自动选择答案。输入DOM不向公开状态转存，材料的保留与及时清理仍沿§3/§4。

继续编辑与放弃仅结算原确认一次；真实离页、失权、新Session/epoch、注销或App.dispose清旧状态并以false结束待决确认，使原导航/关闭/退出等待有终点，不能迁到新身份。checking或失败复查不视为上述失效。关闭确认后的模态内焦点恢复只消费已接受共享规则；View/Editor所有await后的焦点续体须核本实例仍有效、节点当前可操作，旧实例不触碰重挂页面。继续编辑仍保留业务Dialog时不另补焦点；确认打开时标题和业务恢复不抢焦点，即使没有底层业务Dialog也如此。正常业务关闭、提交拒绝后的可用控件恢复仍保留，不用全局querySelector、手工focus、z-index或反复nextTick重挂代偿层顺序/共享恢复。

取消、30秒可见超时或放弃只结束可见等待；owner必须保留到实际fetch、read/cancel全部join。不可在返回Promise.race、进入第二阶段、清本地材料或Dialog关闭时提前释放。每个Provider工作流/读分区仅abandon自己的scope+代次，不能取消正在restore、登录/注销、改密、邀请写或users读取；反方向清理亦不得侵入Provider。晚到成功不得启动下一步骤、重开页面或污染新身份；当前identity的迟到401可能已清Cookie，仍按既有规则失效。

固定 `1c82d88` 的接入注意：`useSession.personal.abandon()` 目前仍无kind限制地调用owner.abandon；在原第3路径内仅将该回调限制为personal owner，个人域自身清理与改密Cookie确认语义保持，不收紧App总dispose/auth.leave的全局清理。Provider读/写/lookup各分类必须绑定本域代次，不能落入runAuthorized的personalRevision默认分支；各域清理不误撤另一域操作或提前释放尾部。现systemFailure的CSRF_FAILED分支仅覆盖invitation-write，本卡须将Provider/Credential写及两个带CSRF的lookup分类接入既有完整identity和owner generation护栏，保留当前/迟到失败处理与旧GET行为，不把lookup当无CSRF的普通读。当前identity的403系统拒绝除保留既有清理，还须清Provider私有材料/草稿/intent并发布原身份绑定拒绝；旧identity/代次不得清新状态。以上落实原分域与身份规则，不扩路径或改变其他状态含义，所有owner均仍在实际finally结束后才释放。

401/SESSION_REVOKED或CSRF_FAILED使当前身份失效并清旧私有材料；当前identity的403 FORBIDDEN清所有系统页面数据/草稿/intent并设置身份绑定system拒绝，不改User.role或自动注销，成功Session重验才可恢复。旧identity/旧代次403不污染新身份；真实换账号、同User新Session/epoch、降权与注销均清Provider及两步状态。敏感材料清理不等于实际网络已结束：未join的旧请求仍由同owner托管，禁止新身份请求越过；仅保存完成尾部所需最少私有引用，不发布晚到receipt。焦点只恢复到仍连接、可操作的触发器；删除后落到创建/刷新或标题，不聚焦已删除行。

## 6. 精确候选路径与所有权

下列21路径保持候选范围，拟由directory_backend改任frontend_worker实施；邀请UI及共享焦点前置已在 `1c82d88` 接受，但本修订独立静审采纳与主线程明确移交前，仍无源码/测试写权、无真实资源窗口。以该固定输入保留共享App/router/Session/client/设置菜单/旧测试和README中的已接受能力，不覆盖邀请成果。当前仅本卡由architecture_worker写。

| # | 路径 | 限定用途 |
| --- | --- | --- |
| 1 | `web/src/api/client.ts` | 十个固定endpoint及受限options；Credential create为512KiB、Provider create/update为32KiB，其余16KiB；仅listProviders成功响应2MiB，Problem及其他响应600000B，实际join保持。 |
| 2 | `web/src/api/system-providers.ts`（新） | 专用严格DTO/receipt/lookup与输入、UTF-8/JSON预算、固定API。 |
| 3 | `web/src/composables/useSession.ts` | 第四可选依赖、同owner分域、私有两步intent/材料、恢复及当前身份清理；仅收紧personal.abandon的owner回调门禁，并将新增写/lookup接入原CSRF失效护栏。 |
| 4 | `web/src/composables/useSystemProviders.ts`（新） | App期私有草稿/进度、详情/分页、确认及身份/页面代次。 |
| 5 | `web/src/App.vue` | provide、App期Provider确认状态/Promise、导航及顶部退出组合、dispose；不新增Provider确认Dialog宿主，保留原页面流程。 |
| 6 | `web/src/router/index.ts` | 仅增加Providers叶子，系统默认用户保持。 |
| 7 | `web/src/router/auth.ts` | 第七项精确return、Provider离页前确认/完成hook。 |
| 8 | `web/src/views/system/SystemSettingsView.vue` | 保留用户/邀请组，追加仅Providers的新组与原权限拒绝行为。 |
| 9 | `web/src/views/system/SystemProvidersView.vue`（新） | 列表/详情、只读Models/metadata、业务模态与末尾统一放弃确认宿主、生命周期焦点门禁及局部响应布局。 |
| 10 | `web/src/views/system/SystemProviderEditor.vue`（新） | 创建/编辑表单、私有新凭据输入、分步进度与恢复交互；随View共同卸载/恢复，旧实例不抢焦点，不另持Cookie或key。 |
| 11 | `web/src/tests/system-providers-client.spec.ts`（新） | 封闭endpoint/DTO/receipt/lookup、时间/分页、材料及编码预算。 |
| 12 | `web/src/tests/system-providers-state.spec.ts`（新） | 实际controller+受控transport，两步/owner/身份/取消/历史观察/部分成功。 |
| 13 | `web/src/tests/system-providers.spec.ts`（新） | 真实router/controller的页面操作、草稿与App/菜单/焦点组合。 |
| 14 | `web/src/tests/system-user-directory.spec.ts` | 菜单存在项精确增加Providers；原用户默认、权限/目录断言保持。 |
| 15 | `web/src/tests/system-invitations.spec.ts` | 上游接受后仅适配菜单与共享导航组合；原邀请写/恢复/关闭断言保持。 |
| 16 | `web/src/tests/personal-settings.spec.ts` | 六项return精确增为七项及新合法项；旧正反例/草稿保持。 |
| 17 | `tests/account/system_providers_web_test.go`（新） | 下节真实顶层与正式命令/关系事实核对。 |
| 18 | `tests/account/system_providers_web_fixture_test.go`（新） | 复用authenticationWebFixture的窄准备/runner和有界HTTP响应控制，不改旧fixture。 |
| 19 | `tests/account-captcha-web/system-providers.config.js`（新） | 锁定Playwright场景/预算、独占私有输出与敏感输入证据保护。 |
| 20 | `tests/account-captcha-web/e2e/system-providers.spec.ts`（新） | 固定生产dist+真实backend的页面、恢复、权限及布局。 |
| 21 | `docs/development/frontend/README.md` | 产品接受后同步新叶子/七项return、实际命令与能力限制。 |

必读[Vue开发技能](../../../.agents/skills/agenteam-vue-development/SKILL.md)、[Vue测试技能](../../../.agents/skills/vue-testing-best-practices/SKILL.md)；独立验收读[验证技能](../../../.agents/skills/agenteam-verification/SKILL.md)，真实浏览器按环境使用Playwright技能，缺失如实记录。不新增npm依赖。SettingsShell/Ui公共组件、全局CSS、旧个人/公开/邀请controller与API、后端生产/OpenAPI、迁移1–19、旧浏览器fixture/driver、锁文件和当前归档路径只读。零DDL，不占下一迁移；若发现后端缺口、共享契约冲突或需扩大路径，保留证据交主线程修卡，不打stub或扩大旧任务。

## 7. 高风险验收与资源门槛

实施前固定前端 `1c82d888adfef0d8b58ab51c920557ca4e2f084f`、后端 `9b32015`或其已接受兼容后继、迁移1–19及本卡输入清单；冻结产品后方可交独立审查。作者执行 `npm run check --prefix web`，只格式化授权文件；Go1.27.1、`GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOFLAGS=-mod=readonly`下新增integration-tag race编译与适用vet。依赖网络准备单列，正式检查离线readonly；纯检查/编译不能冒充真实浏览器或事务恢复通过。

纯测试必须覆盖：十endpoint的合法/非法options、非三项特例的16KiB不变、合法Provider转义后>16KiB且≤32KiB、65536B多字节边界及Credential JSON转义后>64KiB仍合法、65537B/非法surrogate/超原字段或编码预算零intent/dispatch；完整闭合Provider/Models/metadata/两类lookup/receipt、错误目标/类别、必填null、日期与>2^53；协议不可改、options={}、Models只读与空首项删除门槛。材料仅用任务生成值，断言/错误/快照不打印材料或请求body。

响应预算专例须通过实际transport读取：25项均含最大合法8192B URL并按Go默认规则充分转义 `&`、`<`、`>`，同时覆盖最大名称、UUID/ref、版本/时间及cursor包装，完整页大于600000B仍合法；listProviders实际流累计超过2097152B时拒绝，不能只靠伪Content-Length触发。该endpoint的Problem以及getProvider、listModels、旧Account等其他响应超过600000B仍拒绝，不得随listProviders放大。用异步屏障覆盖超限后的read/cancel延迟或忽略abort：可见失败零候选，实际尾部未join时同owner仍阻止下一请求，不仅断言错误类别或buffer长度。

用明确异步屏障和实际生产controller+transport验证第一/第二步骤fetch、body和cancel忽略abort时的owner；30秒可见结束、放弃、身份变化仍不能放行users/邀请/登录/注销/改密/公开入口，实际join后才继续。覆盖相反方向互斥、各域abandon、步骤间取消零后续写、late receipt不续绑、sameSession checking保留、跨Session销毁、当前/迟到401/403/CSRF。其中显式覆盖personal.abandon不撤Provider读/写/lookup、Provider清理不撤personal owner，且各域自身abandon仍生效并等待真实尾部；Provider写及两种lookup的当前CSRF_FAILED清本identity，当前403清Provider私有状态并设置原系统拒绝，旧identity/其他owner代次不误清新状态；App.dispose/auth.leave全局清理与旧个人/邀请行为保持，实际尾部不被跳过。验证原材料/key/body精确重放、两步key分离、lookup true/false/失败都不跳过Execute、明确拒绝与历史未确认区别、create确认即清材料、Provider失败保留newref/receipt且无补偿删除、已知冲突显式重读后仅新Provider key、确认后读取失败仍保留成功；不能只用立即resolve mock、响应clone或内部状态镜像代替。

页面纯测复用真实App/router/controller和受控transport：先打开dirty/部分成功或未确认状态的放弃确认，再通过生产App listener的pageshow复查保持Session GET待决；checking零Provider模态且App恢复区域不被inert，失败后恢复按钮可用，同identity恢复仍保留原安全草稿状态及待决确认、末尾确认非inert/aria-hidden且无新增写。覆盖无底层业务Dialog的确认、继续/放弃仅结算一次、待决路由/退出及失权/换Session/dispose返回false；旧View/Editor异步续体不得改变新层焦点。保留原失败断言，不以直接finishConfirmation代替可交互按钮，不打印私有材料；jsdom仅证明状态/DOM，实际焦点由浏览器组合验证。

| 新真实顶层 | 必需场景 |
| --- | --- |
| `TestAccountSystemProvidersWebLifecycle` | 实际管理员经菜单创建无凭据Provider、查看/编辑/禁用/启用；协议只读/options固定；取消删除零请求，空Provider正式删除receipt与当前404。正式服务建立Models后子列表只读、非空禁删；自有并发新增Model使旧空观察的DELETE被真实后端拒绝，无级联/虚假成功。 |
| `TestAccountSystemProvidersWebCredentialReplacement` | 页面创建新Credential并创建/绑定Provider；替换产生不同ref、保留旧Credential及另一共享旧ref的Provider，确认后输入清空；删Provider不删任一Credential。实际制造Provider版本竞争，在Credential成功/Provider拒绝后保留prepared ref，明确读取核对再保存仅新Provider命令，Credential接受事实仅一次；metadata读取失败不伪装未配置。 |
| `TestAccountSystemProvidersWebOutcomeRecovery` | 自有同源服务器在真实Credential或Provider POST/PUT接受后分别有界丢失响应；查原key、保留历史观察、显式同key/body重放后核同一receipt及唯一业务事实，未确认阶段不错误进入下一步。不注入DB提交故障、不造Object探针。确认写后GET失败、明确放弃和重新进入均按真实含义显示；恢复无自动Secret DELETE。 |
| `TestAccountSystemProvidersWebReadAndPagination` | 至少26个正式Provider及选中Provider的26个正式Model，首/下/上页、目标切换、刷新、空与坏cursor；其中一完整25项Provider页使用8192B且含充分 `&`、`<`、`>` 的合法URL，由正式WriteJSON生成大于600000B的实际响应并成功显示。metadata版本推进由正式服务形成。另一管理员正式替换Provider引用、再删除已解除的旧ref，页面旧观察读metadata404时准确提示，不直改Credential表或绕过引用保护。真实请求不触发任何Provider网络连接；列表GET及子列表不冒称跨页原子快照。 |
| `TestAccountSystemProvidersWebAuthorityAndIdentity` | 普通用户无入口、直链不发管理请求；真实Session撤销与admin失权使读写/lookup收到后端401/403并清私有状态；同User新Session及明确换账号不继承材料/intent。失权准备只在自有精确fixture，不能fulfill假403冒充真实授权。 |
| `TestAccountSystemProvidersWebNavigationAndLayouts` | `/system`仍默认用户、Provider直链登录return、用户/邀请/Providers真实菜单；dirty/部分成功/未确认在所有关闭与离页路径的继续/放弃。确认已打开时触发App pageshow复查，含任务自有服务器对精确Session GET的一次有界失败、App按钮重试及真实同Session恢复；确认须真实点击/键盘可达，继续后焦点仍在剩余模态且Tab/Shift+Tab不逃出，放弃/真实离页后无旧层。合成pageshow驱动须标明，不冒称浏览器自然恢复；其后Session由真实后端响应。light/dark×1440/1024/834/390、长URL/名称、键盘、Dialog和窄屏Drawer Escape/遮罩焦点、reduced-motion、无页面溢出。 |

真实顶层用原脚本按精确名称分组运行，例如 `scripts/test-security.sh -run '^TestAccountSystemProvidersWeb(Lifecycle|CredentialReplacement|OutcomeRecovery|ReadAndPagination|AuthorityAndIdentity|NavigationAndLayouts)$'`；沿原浏览器45秒、Go顶层2分钟、workers=1/retries=0、race/count1/每包6分钟，不加时、削断言或把no-tests算通过。只用任务自有PG17.x（最低17.8）、MinIO、真实Central与同源生产dist服务器，不访问外部Provider、读取外部Credential或占现有基础设施；准备Models/共享ref等经正式服务，数据库只核授权fixture事实。

共享App/router/owner改动须真实回归 `TestAccountAuthenticationWebSessionLifecycle`、`TestAccountPersonalSettingsWebThemeAndNavigation`、`TestAccountPublicEntryWebIdentityNavigation`、`TestAccountSystemUserDirectoryWebAuthorityAndIdentity`、`TestAccountSystemUserDirectoryWebNavigationAndLayouts`，以及上游已接受的 `TestAccountSystemInvitationsWebOutcomeRecovery`、`TestAccountSystemInvitationsWebNavigationAndLayouts`。按固定未变语义复用其余证据，不重跑被停止的Object/tools任务。凭据输入期间禁止自动截图/trace/video或请求body采集；布局截图仅在清空/遮蔽输入后，私有fixture值留受限runtime，归档只保存安全状态/次数/关联结果，不能为证明重放泄露材料。

邀请UI作者/独立验收已实际停止并资源双次清零；本卡独占窗口仍由主线程另授，作者结束再向未参与实现的verification_worker移交，不能并发共用fixture。独立负责人核严格wire/原请求/材料清理/当前权限/实际join，并实际验证两步部分成功及响应丢失后的恢复；作者自测不替代独立结论。保留原失败、固定输入、实际argv/env/退出/原始安全日志；所属命令实际wait、进程结束、自有exact-ID两次absent且旧基线不变后交回。主线程核关键证据后才采纳/提交本完整结果，不把规格STATIC PASS或管理配置成功扩大为Runtime可用。
