# M3 MCP 实施

目标是 Project Owner 配置并连接 MCP 服务，安全发现具体 Tool 与 Resource，Agent 经统一执行入口使用并获得可解释的结果或失败。正式边界以 [D20](../development-plan.md#d20-mcp) 和 [MCP 架构](../../architecture/mcp-integration/README.md)为准；首条协议调用或配置库完成不代表完整 D20。

## 当前实现与接缝

MCP 本域位于 `internal/central/mcp/`，已实施的本域迁移包括 `00102`、`00111`、`00119`、`00131`、`00142`、`00143`，OAuth后继共享迁移 `00130`、`00135` 按原输入分别验收。Tool Registry、Execution binding、Operation/Attempt、Secret 共享端口与 App 装配由 M3 integration 负责；MCP 不建立第二套工具执行器。Config 与 Project Connection 分离，凭据与目录属于 Project Connection，注册目录按 Project 隔离。

无认证/static 的真实连接、发现、统一 Agent Tool/Resource、媒体基础映射/source权限、Owner UI及OAuth完整浏览器旅程已有下方限定通过证据。历史stranded、enabled连接startup成功/失败保旧、原Delete/发布token后的连接收尾与Human Refresh单窗口冷恢复均已按下方实际门闭合；后继common受影响普通、race App编译/配置/路由与完整fulltsc均已通过，共享接线获独审；warm refresh与各cold证据仍各自限定。缺失能力保持显式失败，不用 SDK 可编译替代真实绑定。

以下分期实施与验证记录保留原时点状态及失败，当前接受结论以上文及各后继实际节为准，不据旧“尚待”重复派工。

## 固定协议依赖

- 官方模块 `github.com/modelcontextprotocol/go-sdk v1.8.0`；[Go proxy 固定版本](https://proxy.golang.org/github.com/modelcontextprotocol/go-sdk/@v/v1.8.0.info)，tag commit `3f3b699b2b67e1ed033a63d6651671dab53c2d32`，2026-09-04 发布，最低 Go 1.25.0。本仓使用 Go 1.27.1。
- [该 tag 的 LICENSE](https://github.com/modelcontextprotocol/go-sdk/blob/v1.8.0/LICENSE)声明从 MIT 转 Apache-2.0：新增代码与规格贡献适用 Apache-2.0，尚未同意重许可的既有贡献仍适用 MIT；普通文档适用 CC-BY-4.0。不能将整个版本简单标成 MIT。
- 已静态核对 `client.go` 的现代 `server/discover` 与旧版初始化回退，及 Streamable HTTP 的 Tools/Resources 接口。生产仅接受 `2026-07-28` 与 `2025-11-25`；显式关闭 SDK 自动重连和独立 SSE，禁用 roots/sampling/elicitation，设置消息大小上限。
- SDK HTTP 客户端由平台 `outbound.Client.SDKClient` 提供；TLS、当前出站规则、DNS/IP、代理与凭据 origin 限制保留。OAuth discovery/token/refresh 同样必须经过受控出站。

验证：Go 1.27.1 已完成 `go test -mod=mod github.com/modelcontextprotocol/go-sdk/mcp -run '^$'` 的依赖编译（没有运行测试，不能记作协议行为通过）。实际协议、Secret、数据库、Agent 与 UI 验证尚待实施。

## 实施检查点（2026-10-14）

迁移 00102 已保存：Config、Connection、原始 pending 请求、发现目录、保留运行时影子、Execution 租约、资源物化索引与 OAuth 流记录。控制面新增配置命令与连接/刷新/启用的两阶段发现；完成发现前不持有数据库事务，返回后重新核验当前 Owner、配置版本与原始请求，注册目录和资源目录同事务发布。当前并未装配完整真实 Authority，编译通过不等于该链可用。

官方 SDK 生产适配器覆盖现代/旧版协商、分页、一次 Tool 调用、Resource 读取。`go test -mod=readonly ./internal/central/mcp/... ./tests/mcp/provider` 已通过。真实提供方 harness 位于 `tests/mcp/`，使用显式 Go 1.27.1、自有 nonce/label TLS 服务与独立 PG；当前未执行该 harness。后续验收必须分别记录协议、控制面、Secret、执行入口、Agent、UI，不用窄协议通过替代完整 D20。

真实协议后继：`GOPATH=/workspace/go sh tests/mcp/test-protocol.sh` 原 session 2216 Wait 0（schema 00100–00109），两版协商/分页、Tool 调用、Resource 读取、凭据反射拒绝、未知结果不重试、运行中策略收紧通过；实际 PG17.8/vector0.8.1，提供方和数据库 fixture 均正常退场清理。先前 37014 提供方权限启动失败、84994 协议子项通过但 SIGQUIT 退场失败仍保留，后继不回填旧结果。

控制面后继已实现 typed Audit 原事务事实校验、Registry 同事务私有 witness、当前绑定检查、无认证 capture 租约、禁用/断连保留执行影子。它们目前只编译通过；真实 Secret、Operation/Execution、StoredObject、OAuth、reconciliation 和 UI 仍在实施。

## Owner HTTP 合同（none / static 首链）

生产适配器为 `internal/central/mcp/http`：`NewHTTPHandler(*mcp.Service, *account.HTTPBoundary)` 和 `HandlesPath`，由 App 唯一装配原服务。System 路径要求当前管理员；Project 路径经当前 Session 后由原 MCP Service 在事务内核 Project Owner。浏览器写入沿原 Origin/CSRF 边界，使用唯一 `Idempotency-Key`；不接受 Actor、token 明文、runtime binding 或执行许可。

| 路径（均以 `/api/v1` 开头） | 方法与行为 |
| --- | --- |
| `/system/mcp/configs` | GET 配置集合；POST 创建 System 配置 |
| `/system/mcp/configs/{config}` | GET、PUT、DELETE |
| `/projects/{project}/mcp/configs` | GET 可见 System + 当前 Project 配置；POST 创建 Project 配置 |
| `/projects/{project}/mcp/configs/{config}` | GET；PUT、DELETE 只修改当前 Project 自有配置 |
| `/projects/{project}/mcp/connections` | GET 当前 Project 连接集合 |
| `/projects/{project}/mcp/connections/connect` | POST 连接/重连并发现 |
| `/projects/{project}/mcp/connections/{connection}` | GET 实际连接与发现状态 |
| `/projects/{project}/mcp/connections/{connection}/{refresh,enable,disable,disconnect}` | POST 对原连接操作 |
| `/projects/{project}/mcp/connections/{connection}/resources` | GET 已发布 Resource/Template 元数据 |
| `/projects/{project}/mcp/connections/{connection}/tools` | GET 正式 Registry 的当前 Project 注册目录（包含 inactive 项） |
| `/{system,projects/{project}}/mcp/commands/lookup` | POST 只读查询原命令；不重新发出网络请求 |
| `/projects/{project}/mcp/credentials` | POST 创建固定 PurposeMCP 的 Project 凭据，无公开目录 |
| `/projects/{project}/mcp/credentials/{credential}` | GET/HEAD 只读元数据；PATCH 更新原版本；DELETE 删除原版本 |
| `/projects/{project}/mcp/credential-commands/lookup` | POST 查询原 Secret 命令，不携带或重发材料 |

Config 写体为 `{ "expected_version":"0", "input":{ "name":"…", "description":"…", "endpoint":"https://…", "authentication":{ "scheme":"none" } } }`。创建版本为 `"0"`；更新为已读取版本。当前写入 `scheme` 支持 `none`、`bearer`、`basic`、`custom_header`、`oauth`；自定义头必须 `header_name` 且禁止保留认证/协议头。OAuth 配置和 Begin/Complete 已经原82194真实 Owner HTTP 门，浏览器与 refresh/cold 按下方独立范围验收。

Connect 写体为 `{ "config_id":"uuid", "credential_id":"可选当前 Project Secret UUID", "expected_version":"0" }`，首次连接版本为 `"0"`，重连为原连接版本。刷新/启用/禁用/断连与删除配置写体均为 `{ "expected_version":"正整数字符串" }`。lookup 使用同一 `Idempotency-Key` 和 `{ "command":"config.create|config.update|config.delete|connection.connect|connection.refresh|connection.enable|connection.disable|connection.disconnect" }`。

GET 集合响应为 `{ "items":[] }`。Config 字段为 `id,scope,project_id?,name,description,endpoint,authentication,version,runtime_revision`。Connection 字段为 `id,project_id,config_id,version,config_revision,state,enabled,credential_id?,protocol_revision?,tools_state,resources_state,tools_revision,resources_revision,last_error_code?`，不投影内部 runtime binding。版本/计数按平台合同使用十进制字符串，发现修订可为 `"0"`。Resource 元数据为 `id,connection_id,kind,uri,name,title,description,mime_type,byte_size?,revision`；URI 只作为 MCP namespace 元数据，不授权浏览器或服务端 HTTP 下载。远端文字由 UI 按不可信文本显示。

Mutation 响应为 `{ "config"?:…, "connection"?:…, "deleted"?:true }`；连接动作可能已完成但得到 `auth_required`/`error`，UI 必须按返回事实呈现，不能将 HTTP 200 等同远端可用。lookup 返回 `{ "state":"not_found|pending|complete", "result"?:… }`，`pending` 与 Commit Unknown 都不能触发自动重发。HTTP 失败沿原 RFC 9457 `code/commit_state/request_id/retry_hint` 闭集；网络/认证失败内容仅保留 `mcp_*` 安全码，不返回远端原始错误。

静态凭据创建体为 `{ "value":"…" }`，更新为 `{ "expected_version":"正整数", "value":"…" }`，删除仅原版本。凭据命令沿原 Secret namespace、Project/当前Human Owner、原材料比较与CAS，不另建存储。成功响应仅 `credential_id,purpose:"mcp",version,deleted`；GET/HEAD 元数据不含 deleted 或材料。lookup 沿原 key，体为 `{ "kind":"create" }` 或 `{ "kind":"update|delete", "credential_id":"…", "expected_version":"…" }`，响应 `{ "observed":false, "result":null }` 或已确认的原安全收据；未观察到不等于未提交。前端只保存原查询身份，材料仅用于一次写入；Basic 明文用户名/密码在提交前按实际协议编码为 base64 载荷，材料不会出现在连接请求、页面回显、错误或 lookup 中。该适配器及 App 路由已实现；后继76616已通过真实Owner HTTP/PG入口，具体范围见下方检查点。

Tool 集合字段固定为 `tool_id,spec_revision,definition,config_id,connection_id,remote_name,default_enabled,active`；`definition` 是原 Registry 的 `stable_key,name,description,input_schema,output_schema?,annotations?`。`tool_id` 可用于正式 Agent 的 `allowed_tool_ids` 配置；目录读取不授予执行权限。HTTP 不返回内部 runtime binding，原 Registry 负责当前 Session/Project gate、目录大小和完整定义校验，MCP 在读取前后复验当前连接版本。禁用后的条目保留 `active:false` 供 UI 显示；重连时页面重新加载连接与目录，不能复用旧调用许可。

当前 HTTP 已接原 App。固定 `08b77315`、schema00100–00119 的原89142 Wait0通过真实Owner HTTP→无认证Connection→official SDK TLS发现→Registry Tool/Resource目录、原command lookup、跨Project404与启停/断连；race编译和完整Config前置通过，原App及SDK/PG/outbound/MinIO全部退出清理。先前72356/94605的可选lease全零UUID约束失败与99762的fixture resolver装配失败保留，已分别修正。该top远端Tool调用为0，不代表Agent、static Secret凭据或UI通过。

后继Agent top已通过真实static bearer Secret/SDK发现；固定4c0ea3d6的34172由原Scheduler创建Agent Execution，Preparation与第一Model轮成功，第一统一Tool Operation/Attempt进入running后COMMIT_UNKNOWN，未取得Tool终态或Resource后继正文，不能称Agent链通过。失败诊断漏传network导致nil panic，原ProcessGuard保留直至原6分钟超时Wait2；所有任务资源已按原Wait及精确nonce收尾。该诊断已修并增加仅安全类型/提交状态/真实调用数的观测，尚待固定复验；完整原wire、Object正文、租约退场断言保持。

固定ebd5505f的54136原Wait2已取得：nilnetwork诊断panic消失，原Store多次Tool prepare Commit Unknown、SDK调用0。实际隔离PG将结果校验失败定位到00117 JSON提取与减法优先级；原表达式单独报22P02，加括号后成功，待共享owner前向00123。原进程保护锁保留至6分钟退出，PG/outbound/Object及SDK全部精确清理；没有将后置清理当作原run成功。恢复丢失39822原script Wait的事实也保留。修复包络提交后仍须定位调用前错误并完成全部Agent正文/租约断言。

前向00123由共享owner真实PG验证后，固定afdd0c7a/schema00100–00123的86091原Wait1：race/完整Config/selector通过，Agent按原Scheduler运行并终态、App/Runtime正常Drain/Joined；最终事实检查远端Toolcalls0而失败。原Secret-resolution三次NotCommitted/DEPENDENCY_UNAVAILABLE已定位Project SecretProducer未接真实AgentRun的权限委托，integration负责窄修并保留原Secret私有proof。原3fixture及SDK全部清理，未出现旧包络Unknown尾；完整Tool/Resource正文、Object与lease断言仍未通过。

前端第一组已落固定13端点和严格DTO解析，独立客户端6tests通过（17788原Wait0）；尚不代表页面可用。静态配置完整入口仍需PurposeMCP Secret凭据HTTP，当前Model凭据handler固定PurposeModel，不能把Project变量或Model credential冒作MCP引用。

首界面按上述实际入口实施：Project连接列表是主任务，显示配置来源、连接/认证与独立Tool/Resource发现状态；新建/编辑配置用对话框，选择服务后连接。选中连接后展示目录及安全错误，连接/刷新是主动作，启停/断连置于次要操作。等待时显示正在发现；提交结果未知时只提供原命令查询，禁止自动重发；明确失败后由Owner显式重试。空目录、未连接、当前权限变化与版本冲突各有实际反馈，远端文本不渲染HTML。先复用项目设置容器和既有表单/对话框/状态组件，不重设旧页面。拟新增 `web/src/api/project-mcp.ts`、`web/src/views/projects/ProjectMCPView.vue` 及必要独立组件/测试；共享router、ProjectSettings导航与Session API装配由root分配唯一写窗口后再改。

OAuth 2.1 属于本 M3。固定 redirect 为 validated PUBLIC_ORIGIN 下 `/mcp/oauth/callback`，页面先清 URL 敏感查询，再以原 Session/CSRF POST `/api/v1/mcp/oauth/complete` 交回 code/state/issuer；跨站 GET 不兑换 token。实际 Begin/Complete 与原请求/Secret 写入门已通过82194，UI共同纯门已过；80558完整真实浏览器旅程通过，68021 warm expiry refresh由root接受复用，cold生命周期仍待后继验证。Begin 为 `POST /projects/{project}/mcp/oauth/begin`，体 `{config_id,expected_version}`；Complete 不接收浏览器指定 scope。两者均保原 Idempotency-Key，原 Project command lookup 只读支持 `oauth.begin|oauth.complete`，禁止未知结果时再次兑换。

文本 Resource 保留 canonical StoredObject/MCPFile，并在原调用权限下实际 ReadObject/Close、验证SHA256/大小后投影最多64KiB UTF-8正文及正式引用，满足当前 text-only tools profile。按 [Tool Execution §6.2/§6.3/§22](../../architecture/mcp-integration/mcp-tool-execution.md) 与 [Resource Adapter §9](../../architecture/mcp-integration/mcp-resource-adapter.md)，本 M3 媒体出口须完成 image/audio 基础映射、binary StoredObject、正确引用与 media_type，以及原 Object 权限下的读取和 Close。当前唯一 text-only Model profile 对非文本输入应明确 CapabilityUnsupported，并保留 canonical 引用；本期不新增未声明的多模态支持 profile，也不声称模型已消费媒体。后继6424已通过真实 image/audio/binary 映射、后继引用持久化和能力拒绝，94153已通过实际 source Read/Close 及相应拒绝边界；两者包含原lease/Join，范围见文末。使用真实 Model 调用私有 context 的测试消费者只验证 source 权限，不能算作生产 Model 媒体消费。

### Agent 首链与页面后继检查点（2026-10-14）

- 24826 原 Wait1：062500fc 窄审计修复后的实际 static Secret/SDK连接、echo恰1次、4轮后继模型wire、工具echo及Resource目录消费、Agent terminal和原App/Runtime退出通过。read-resource 文本未解出 canonical JSON；正文/Object及最终lease事实断言未达。全部四组owned资源实际清理，未改Unknown/重试边界。
- 64119 原Wait1（ee3e4dee/30538）：read-resource安全错误为mcp_dependency_unavailable，原materialization pending=1、ready=0、content=0，四组owned资源已清理。579c0d1b 后继仅包装原Objects端口原样转发；23853 race/完整Config/selector原Wait0，35218真实top原Wait1，确证PutObject返回DEPENDENCY_UNBOUND。原上传必经的FinishWriter MaintenanceAccess在MCP内容授权中缺失；修复须保原活contentUse、当前事务原Object映射与完整tuple/locks，不能把维护DTO当新读写许可。此轮SDK/PG/outbound/Object均按原nonce精确清理，整Agent链仍未通过。
- MCP页面现消费配置/发现13端点与凭据5端点，支持none、Bearer、Basic及自定义Header配置、一次凭据保存后以正式收据连接、Tool/Resource独立目录与启停/断开、原连接/凭据Unknown查询；材料不留在Session progress或查询体，跨remount只查原key。`/settings/mcp` 接实际router与projectRoute安全上下文。OAuth和浏览器实际HTTP闭环未完成。
- 51699 原Wait0：35个API/原Session/页面/通用审批交互测试与完整vue-tsc通过。41394的31tests、70808的28tests作为前置范围保留；3621仅旧UI文案断言失败后已修，未回填。真实浏览器及生产构建未验。Meeting source-neutral审批复用原请求owner和lookup，不造Task；审批仅认可run-command或正式MCP stable key，显示真实工具名，不造DTO未提供参数。
- 静态凭据原79131窄race验证了Human/Project/PurposeMCP lookup闭集和handler内层材料/Unknown边界，不能代替ServeHTTP/PG。后继OwnerCredentials复用原App，计划验证CRUD/CAS/历史收据及Audit、一次wrong-purpose与正式session撤销拒绝、丢失真实HTTP响应后原key只读查询；数据库CommitUnknown未在该top注入。独立wire门检查真实Body deadline、Keepalive/HEAD、Write/Flush/Close失败和原service尾join。
- OAuth 6源与新增oauth_*已交integration唯一实施；本域继续Content/Resource与Owner凭据/UI。原D20 callback/exchange/refresh/lease及Unknown收尾仍为M3必需项，00119不回写。

- Maintenance 后继：66834 原Wait0通过ContentMaintenance窄race；4233原HTTP wire子包PASS及content fixture Row类型编译失败分别保留。3420b341 App同Store resolver已导入f49bd4da；16340固定b595d6bf完成race App编译、完整Config和两个exactselector。
- **79351 原Wait0，首完整文本Agent链通过**：固定b595d6bf/schema00100–00123，原Scheduler/static Secret/official SDK/Registry/3统一Operation和Attempt、远端echo恰1次、canonical Resource Object Put/Consume/Read/Close及后继模型正文消费、runtime/Secret lease退休和原App/ownersJoined全部原断言通过。PID1114353actual0，SDK与PG/outbound/Object四组自有资源均精确清理；日志 `output/ai/mcp/agent-maintenance-b595d6bf.log`。旧失败不回填，OAuth、真实取消/撤权收尾注入、多模态与浏览器仍未验。
- OwnerCredentials 37799 原Wait1：sole fixture误用默认direct-text App，首次POST404而停止；不是已构造MCP路由的业务拒绝。改用preflight同tool-loop配置并核mcp实际已绑定，后继尚未重编。PID1117213actual1、三fixture原exactcleanup且未起SDK；CRUD/CAS/Audit、wrong-purpose、撤销session与被动lookup断言仍未达，不宣凭据入口真实通过。

- **76616 原Wait0，Owner凭据实际HTTP/PG通过**：6e59c76f只修fixture实际tool-loop装配；53483 race App/完整Config/Owner selector原Wait0。CRUD、GET/HEAD、CAS、原mutation replay材料冲突、历史receipt/Audit、wrong-purpose、正式session撤销后mutation/lookup401与零新写、实际响应丢失后原key被动lookup均通过，PID1122960actual0，PG/outbound/Object三fixture原精确清理，无SDK。日志 `output/ai/mcp/owner-credentials-6e59c76f.log`；该门未注入数据库CommitUnknown，也不代浏览器UI验收。

- **60234 原Wait1，首真实OwnerWeb未通过**：952c7a35固定浏览器源码，71899 production dist原Wait0、82105 race App/Config/selector原Wait0；实际Playwright1.56.1/Chromium151.0.7922.173。安全首断点config-create/ASSERTION_FAILED，尚未到凭据保存、发现、布局检查。Node1137590原Wait失败/App1137334actual1，SDK/PG/outbound/Object四组全部原精确cleanup。日志 `output/ai/mcp/owner-web-952c7a35.log`；后继仅细分config安全子stage，不将早前API/Session纯测当实际UI通过。
- 原direct-text/off缺MCP控制面路由已由integration实现App两源解耦并导fedde51f；当前仅构造/编译证据，后继真实Owner小门待跑。此前tool-loop的79351/76616通过范围不变。

- 4768后继诊断仍FAIL69.40s：原shell会话失效不可补Wait；原日志有App1167674实际exit1/Node1167937失败，细化config-name且未发配置HTTP，四组owned资源原cleanup日志与完整SDK inspect均确认。78340/78108独立实际Chromium窄probe证实required星号使旧getByLabel exact匹配0；正式textbox accessible name可输入，UiDialog inert/aria-hidden正常。solebrowser选择器已修，生产组件未变；完整OwnerWeb仍待固定复验。
- **67007原Wait0，OwnerWeb PASS11.92s**：实际952Go/production dist与ff82d033三选择器，原Config/单selector通过；凭据一次真实写入、丢失响应后原key查询、SDK真实连接发现、Tool/Resource目录、启停/断开/删除、键盘/focus和light/dark/narrow通过，原owner全部Joined，Agent未运行。三基础fixture原exactcleanup，SDK容器/网络分别完整inspect不存在。日志`output/ai/mcp/owner-web-selectors-ff82d033.log`；实际查看`output/ai/mcp/browser.2RhYPF/TestMCPM3OwnerWeb/mcp-desktop-light.png`与`mcp-mobile-dark.png`，宽屏列表/目录层级与窄屏操作换行正确，文档无横向溢出，项目导航使用现有横向栏。旧失败保留；此门不含后继mode/ResourceLink/OAuth。
- ResourceLink规范§6.4/§11要求同Connection `resources/read` 后canonical物化；c9eb3712实现该原会话路径并在每次读前复验原Invocation，4373窄race原Wait0验证部分/总大小、原取消和不重发边界。实际Agent ResourceLink与非文本链尚未验，不能将链接元数据文本算已完成。
- 76616独审接受的拒绝门严格限定wrong-purpose GET404、注销后的create replay/lookup401；HTTP丢响应查询不代表物理DB Unknown注入。仍需mode最小实际门、非文本正式ref与当前能力错误、detached source绑定、startup reconcile及stranded cleanup。OAuth借域继续由integration推进。

- **45882 原Wait0，控制面全模式实际门通过**：固定8ccf32a4、86893 race Appbinary/Config/selector、schema00100–00123；OwnerControlModes PASS15.70s，其中off 8.27s、direct-text 7.33s。真实配置创建、MCP凭据创建/元数据读取与原receipt lookup均通过，工具执行器仍关闭，原App owners Joined。App1206059 actual0，三基础fixture原exactcleanup，无SDK。日志 `output/ai/mcp/control-modes-8ccf32a4.log`。15fa25d6只解除控制面Authority对Capture的构造依赖，实际retention/capture方法仍缺端口拒绝；96695窄race原Wait0。
- **67170 原Wait0，ResourceLink真实Agent链通过**：同8ccf32a4/86893旧SDK提供方与schema123；TestMCPM3AgentResourceLink PASS30.68s，远端Tool恰1次，原Connection/session实际resources/read，链接与builtin Resource分别canonical物化、实际Read/Close，后继模型消费正文，三Operation/Attempt终态及runtime/Secret lease退场、原owners Joined全部通过。App1216740 actual0，PG/outbound/Object原exactcleanup；SDK完整container57b83f977885f355163547b7f666b0d6a7b58374549fa78a5ce7b4b4d36d23cd和networkb725369b01b9d814a5f398a0d0c8f16e9a7aea3ac3dbd58df23f53faadd3917b分别inspect NotFound。日志 `output/ai/mcp/agent-resource-link-8ccf32a4.log`。此门不包含后继e4b79aec共享同步、媒体fixture或source桥WIP。
- 非文本后继发现明确持久化缺口：原Model Message/Execution LoopRound/transcript直接JSON编码会把opaque BusinessFileRef写成标记而失去精确引用，解码亦拒绝。需正式显式消息codec保留MCPFile并保持旧text canonical字节，不能以裸DTO或历史projection代替当前Model输入授权。Execution私有source桥正在实现；未绑定、未编译或未实际媒体验证部分不称已完成。startup reconcile/stranded cleanup、原取消/撤权收尾实际注入及integration独占OAuth仍待闭合。

- 66986原Wait0：新Execution source桥四文件与MCPcontent两文件编译；七包定向race中execution/contract、execution、identity/contract、audit/contract、audit、project选中tests通过，mcp包仅编译（无匹配tests）。纯门验证原privateModel规划、非Model阶段/未注册/已Joined拒绝、精确Ref及provenance绑定。MCPRuntime只新增Project/UUIDcause身份、refresh审计闭集及Project原锁/当前gate→固定domainproof端口；域startup claim/privateproof实现仍未落，端口failclosed。App detached reads仍未绑定，原SQL/current权限真实门和reader取消后Close未完成；131仅分配，尚无DDL/worker。

- 后继source/tail：Model显式消息codec66a与Execution持久消费84c已精确组合，本域ccbd0adc使用同codec核当前原Model输入与持久Tool来源。新增独立ReleaseReader尾权限绑定原private run、Process、Object、lease和MCP完整mapping；取消/closing只能收尾，不准新读，过期Tool witness不回退。81875因Object/Outbox typed ProcessID不等编译失败，修正确typed核验后15819原Wait0通过3包定向race；尚未实际媒体PG/source读取与撤权Close，不以pure门冒完成。
- 131草稿b99abd25保存startup与stranded materialization原process/request/回收所有权，不存秘密或正文。db124–130已精确同步；30817原Wait0仅EmbeddedSource 1–131解析通过，真实PG apply仍待。域startup claim/privateproof及worker尚未落，技术身份入口继续failclosed。
- Meeting共享retry/regenerate动作在真实MeetingsAPI→accountTransport三个闭集门接通，eff9de89已保存；9个定向Vitest及71902完整vue-tsc原Wait0。它们不改变MCP旧Browser通过范围。旧8个App binary在无live/shared引用后由integration按inode精确清理，provider/dist/日志保留；后继媒体与startup必须重新固定编译。

- 639e8a38保存startup/Project/131共13源：同原ProcessGuard先登记原请求，再逐连接发现并原Registry完整diff；Secret lease收尾与冷恢复保原request/process，另存retirement process，死亡确认不按年龄。Project AccessDeny新增封闭MCP consumer分派，仍当前Project gate+原startup私有proof，Model原分支不变。31373原Wait0通过mcp/project定向race（1.039s/1.024s）；21302原Wait0只验证连续1–131 EmbeddedSource解析0.008s。Secret四处分派与App startup尚未接通，131未实际PG，stranded materialization和实际媒体source仍未完成。
- integration的OAuthHTTP82194原Wait0/PASS8.20s通过正常配置创建、Begin与重放、同Session/CSRF Complete与重放、第二key拒绝、DCR/S256/code单次兑换、flow Secret退场、SDK发现和原App Joined；四owned资源清理。此证据不代完整refresh/cold；SPA发起授权、清URL query后同源POST及原lookup恢复仍在接入。

- **96479 原Wait0，131实际数据库门通过**：固定16503普通integration database binary，`TestEmptyRepeatUpgradeAndHealth` 在schema1–131上PASS5.07s，验证空库迁移、重复执行、健康检查和fixture升级。原test PID1310854 code0；PG、outbound、Object三个nonce均原脚本exactcleanup。日志 `output/ai/mcp/database-131-16503.log`。00131从此不可回写；此门不包含startup执行、stranded cleanup或媒体source。
- OAuth共享前端完成实际API/Session/router接线：配置支持oauth，发起授权记录同tab原flow元数据，callback先清query/hash再恢复Session并以原CSRF POST Complete；响应Unknown只查原key，材料不入持久浏览器存储。11006原Wait1发现publish尚持Session owner时普通identity入口busy导致登录回归；仅metadata restore改传已发布的精确identity并核current，原mutation门不放宽。1381原Wait0：7个定向文件98tests及完整vue-tsc通过，包含旧登录/MCP会话、实际accountTransport、回调先清URL、同Session恢复、Unknown无二次兑换和leave清除。正式OAuth浏览器旅程尚未验证；没有同tab原flow记录时明确拒绝并要求重新发起。

- **62827 原 Wait1，b2f17790 普通三包门**：Object contract/Object 窄测试分别 PASS0.004s/0.006s，涵盖原上传 tuple、原事务重读、无原 guard/私有 claim 拒绝和窄 CleanupRelease 闭集。MCP 包编译被已导 OAuth 缺失的 Secret 租约观察合同（MCPRequestLeaseObservation/MCPRequestLeaseState）阻断，ContentRecovery/ContentMedia 未执行。向 integration 精确补共享闭包，不把 Object 普通结果外推实际恢复或媒体。5ccf0c15 的 Object13、10d8b83a 的 OAuth browser3、e186/43c/af51/b2f 的媒体映射与 source 测试均已保存；实际 App/source/冷恢复仍未验。

- **38895 原 Wait0**：root 从固定 383dcabf 补齐 Secret 租约观察两文件（0eb35412）后，9a786226 上仅重跑未过的 MCP ContentRecovery/ContentMedia 普通门，PASS0.010s；62827 Object 两包通过继续复用。Object9+域5共14文件提供方类型/窄测试现已齐，交 integration 绑定 actual resolver/cleanup/source 与原 processOwner；尚未实际 App/媒体/source/stranded恢复，不以本门外推。

### 媒体首实际门与恢复草稿

普通 App7428 原Wait0固定 bc5fc912，随后完整Config、媒体/source/OAuth四exact selectors及CGO0提供方构建实际exit0；该组合包含真实App reads/StartRecovery/StartReconciliation和canonical ref等值修复。37664唯一native原Wait1/FAIL12.57s：四媒体子例的实际static Secret/SDK连接发现已到，单回复误用了要求至少两条的WireSequence，均在受控模型scenario创建处停止，未到Model/Tool媒体。后继仅改用原Wire单回复，保网络模型调用恰一次；不放宽生产能力或结果断言。四owned资源均精确退出清理，原失败不回填。

新sole `mcp_m3_recovery_integration_test.go` 保存真实Put返回屏障、原子进程SIGKILL/Wait/Guard、新App正式cleanup及MCP owner Drain的草稿；尚未编译或实际，保原未知Tool不重发，不宣称全App graceful Join。新启动接线改变了off/direct-text初始化，原45882限定旧输入，媒体/source后需当前候选两模式实际回归；OwnerWeb/ResourceLink无变输入不重跑。

后继32ddb298普通App11289原Wait0、Config与exactlist实际exit0（新恢复leaf只类型核）。89242原Wait1/FAIL27.28s，四媒体子例实际SDK/Put/consume、原Model Begin2且CapabilityUnsupported1、ToolResult和后继round正式MCPFile精确持久均已到达；对象事实查询跨域join漏Object UUID转换，后续lease和最终Join断言未到。已修solemedia为`c.object_id::uuid`，并按00102/00111/00131、00005/00003、00037/00038/00056/00091逐表核media/source/recovery的剩余参数与join类型；不改生产规则。诊断只含固定stage、安全SQLSTATE/错误类型，四owned资源原Wait与精确清理齐。整top失败保持，需固定复验后才收完整媒体出口门。

### 媒体/source 与受影响模式收束

固定 **a4be4f9a** 普通 App **18810原Wait0**、Config0.01s与exactlist实际exit0，复用7428原SDK provider，没有重建或改变生产代码。

- **6424原Wait0 / PASS26.81s**：binary Resource、image Resource、image Tool、audio Tool四类完整通过actual SDK→Object canonical存储→MCPFile/MIME/bytes/SHA→原ToolResult/后继round精确持久→text Model明确CapabilityUnsupported；每例只有一次实际模型网络请求，writer/runtime/Secret lease与原App/Runtime/Directory Joined。四owned资源精确清理，日志`output/ai/mcp/media-canonical-a4be4f9a.log`。
- **94153原Wait0 / PASS30.38s**：同四类在真实BeginChat private ctx下由测试forwarding消费者调用actual SourceResolver/Objects读取、校验并Close；裸ctx、他Project、换ref、取消后新读均拒绝，取消后原Close允许。每例reader lease1 released/无active，原其它lease/Join全过；四owned资源精确清理，日志`output/ai/mcp/model-source-a4be4f9a.log`。生产Model仍Unsupported，未新增或宣称多模态支持profile。
- **65093原Wait0 / PASS5.31s**：off/direct-text两子例在新StartRecovery/Startup/reads入口下实际控制面Config、credential create/get/原lookup可用，工具执行仍关闭，原owners Joined，三基础fixture精确清理。日志`output/ai/mcp/control-modes-a4be4f9a.log`。这才是新初始化受影响回归；旧45882保持旧输入范围。

本轮无活Go/Node/browser/native。可恢复App为`/tmp/mcp-m3-facts.PfO0OO/app.test`，provider为`/tmp/mcp-m3-current.4GqXsE/provider`；旧两App binary按原退出事实精确清理，日志与资产保留。原37664/89242失败不回填。

**M3仍未完成。** OAuthUI soleharness已编译/--list但缺新production dist与实际旅程；common135 refresh/cold后继不在本树当前candidate，需作者固定闭包后同步与真实验证。新StrandedContentColdRecovery只类型通过，历史上传清理、enabled connection startup reconciliation、失败保旧及冷收尾尚未actual。fresh空集合启动/模式Join不代这些恢复门。后继恢复路径、精确环境与借写范围见`.agent-state/current.md`，按root安排暂让席后续派，不删除剩余scope。

恢复后的OAuth UI首轮：83244原Wait0通过188生产dist、PW真实test/expect/poll断言能力及App/PW单selector；实际组合是锁定PW1.56.1与原README批准的系统Chromium151.0.7922.173，二进制SHA256 `d387400aaf740ccb75e5e996a34aa0940e6e97683a4eabd6ae34c1eed6804723`，不是配套141/build1194。86081原Wait1/FAIL16.04s在complete-uncertain，配置创建与授权按钮已越过但issuer/callback/Complete分界未确定。原Node/App实际退出、三fixture和SDK精确清理齐；日志`output/ai/mcp/oauth-owner-web-188cae64.log`。原失败没有公开截图或响应闭码，后继soleJS仅增加固定分段、HTTP状态/次数/位置分类供一次定位，不修改产品、TLS或预算，不记录query/code/token。原media/source/modes三个通过门复用。integration另已完成135实际schema85064及真实expiry refresh68021 PASS48.74s；本树当前App不含135，后继冷门精确组合，不能称OAuth浏览器或冷恢复已通过。

7806原Wait1/FAIL15.85s诊断取得Begin200、relay0、Complete0、lookup0且未到callback heading，所有原owned资源精确退出。静态对齐确定测试的HTTP redirect不满足正式PrepareOAuth的HTTPS门，合法失败receipt可以200无oauth；不是据HTTP状态断言前端产品缺陷。修订仅测试HTTPS反代与私有CA/NSS：子bwrap mount namespace隔离信任，父HOME/NSS和生产校验不变，App请求不被浏览器route截取。环境无browser能力probe exit0；新增无DB trust gate将验证自有CA成功/异CA拒绝，当前新4源尚未编译/实际。后继原OAuth UI仍需真实原key恢复与SDK/Join全链。

b68b3769的49817普通App编译、16773 Config/JS语法/锁定PW能力/两top list均原Wait0；**69176无DB trust实际FAIL2.06s**，真实browser在trusted-ca失败，尚未到异CA门。原NodeWait、两TLS server Cleanup、private runtime清理齐，无OAuth或DB启动。静态核此Chromium151的固定BoringSSL `29e593e29165df578ab778269a1f04da2055c32f` 的X.509签名闭集不含Ed25519，后继仅fixture改与原SDK一致的ECDSA P-256，并增加同namespace原CA alias比对与closed netcode；尚未增编/实际。不把原未记录的netcode补成确定运行原因，不放宽TLS或系统信任，可信/异CA双门通过之前不启动OAuth实际。

**693f2722后继90595普通编译原Wait0；10384实际无DB trust PASS2.97s / 原Wait0。**同子namespace实际CA alias DER匹配，真实Chromium151接受本CA站点并拒绝独立异CA（ERR_CERT_AUTHORITY_INVALID、应用handler0），Node1574453原Wait0、两个TLS server和私有runtime正常清理。父NSS/HOME不改，TLS验证开启；日志`output/ai/mcp/oauth-trust-693f2722.log`。仅证明App origin浏览器信任，原issuer仍NodeTLS单GET relay，不宣整个OAuth旅程已通过。下一复用`/tmp/mcp-m3-p256.UQEELE/app.test`、provider7428与83244dist，独立授窗运行OAuth实际。


**11877原Wait1 / FAIL3.64s**：693/P25690595实际OAuth在browser启动前的Session identity读取停止，尚未Begin/Complete。原App1581180actual1、三基础fixture及SDK完整container/network精确清理，private runtime删除；日志`output/ai/mcp/oauth-owner-web-693f2722.log`。静态确认原HTTP请求helper和soleJS都沿用`agenteam_local_session`，正式HTTPS Account只接受`__Host-agenteam_session`。后继只修soleApp为原CA验证的真实HTTPS session GET，并让soleJS使用正式Host cookie；shared helper/生产Cookie/CSRF及原预算不变。此修订尚未增编或实际，10384无变信任正反门复用，完整OAuth仍待复验。OAuth cold域现已移交MCP owner，common14固定刷新差异需在当前消费者结束后精确组合；135已应用不可回写，warm refresh不代cold。


**70488 ordinary App编译原Wait0，50445实际OAuth原Wait1 / FAIL7.73s。** abf729ed的HTTPS Host Cookie修订已真实通过Session/Owner配置/Begin200和issuer TLS relay1，页面已到callback；soleJS在整页导航后读取旧Begin response body的诊断阶段提前退出，Complete0/lookup0不能解释为生产Complete失败。Node1598273/App1597966原Wait与4owned精确清理齐；日志`output/ai/mcp/oauth-owner-web-abf729ed.log`，安全artifact `oauth-browser.XKnx8n/TestMCPM3OAuthOwnerWebTLSRelay/oauth-failure.json`。后继只使诊断读取失败保closed状态并继续原callback/单次Complete/原key lookup断言；原总budget与产品不变，复用cookie.K9z0j1 App/provider7428/dist83244，不重NoDB trust。新JS尚未syntax/actual。


### OAuth 完整浏览器旅程收束

**80558 原Wait0 / PASS9.29s**：固定37119049 soleJS、abf729ed/70488 Cookie App、7428原SDK provider与83244 production dist，真实HTTPS Session→Owner OAuth配置→Begin→issuer实际TLS授权GET/302→callback先清query→同Session/CSRF Complete恰1→已提交响应丢失→reload原key Lookup恰1→SDK Tool/Resource目录与原App owners Joined全部通过。原Node1622477/App1622249 actual Wait0，PG/outbound/Object三fixture exactcleanup，SDK完整container/network分别inspect absent，runtime `/tmp/mcp-ow.WwsMpF`删除，PID均无活进程。日志 `output/ai/mcp/oauth-owner-web-37119049.log`；前置JS语法与lockedPW同config list恰1 actual exit0，不重App/provider/dist或10384信任门。

亲看 `output/ai/mcp/oauth-browser.dg0MLT/TestMCPM3OAuthOwnerWebTLSRelay/oauth-connected.png`：light/1440×900实际已连接状态、配置来源与工具目录可读，主次操作清楚，无可见重叠/横向溢出。此轮只这一OAuth成功截图，不冒新增dark/narrow/键盘矩阵，原67007对应范围保留。

TLS范围分明：10384已验证子namespace私有P-256 CA的真实Chromium正反信任，父HOME/NSS未修改；80558 App请求经真实HTTPS/Cookie/CSRF边界。issuer仅精确owned授权GET经Node原CA/IP/servername/Host验证并转发真实302，**不是Chromium直接issuer信任证明**，没有拦截App exchange/lookup或关闭TLS验证。371只将导航后的可选response-body诊断失败改为闭码观察，不改产品或总预算。50445及之前所有失败保持原结果。

**M3仍未完成。** 下一为现成staticBearer stranded cold leaf、startup两连接成功/失败保旧、OAuth ready/exchange/refresh冷恢复。common14刷新后继需精确导入；135和1–140所有已应用DDL不可改，142仅为冷清理关联预留。Secret恢复须原opaque计划/完整锁/实际stopped证明，只有原canonical观察证实未提交才可继续同一Delete，不新key、不重外发；Project归档后的cleanup窄证明仍需实现。生产text Model继续明确Unsupported，80558不扩大媒体能力声明。

### 原进程退出后的恢复首门

**98692原Wait1 / StrandedContentColdRecovery FAIL20.45s**：固定abf729ed/70488 App、7428 provider与embedded schema131，真实static Secret/SDK/模型调用到达实际Put成功返回持有；materialization/upload/object/原attempt/process完整tuple及原Guard持锁通过，child1644456 SIGKILL原Wait确认signal9。新Guard child1644537在`mcp_recovery_joined`前的只读observer QueryRow/Scan返回错误并原Wait1，故对象清理结果和MCP owner Drain尚未确认，不称cold通过或App graceful Join。原Tool结果保持未知、没有为验收制造返回结果。

日志`output/ai/mcp/stranded-cold-abf729ed.log`。4container/4network精确inspect全absent，App1644090和两个child PID absent、owned外层临时目录删除，资源已归还。原通用安全日志无typedfault/SQLstate，无法区分driver扫描或Store错误；静态核131字段非空、Object UUID转换及聚合空值语义未发现可确定错误。后继sole仅增加closed postgres.Code、scan列index、错误类别并在原Wait后输出，保SQL、主流程、权限和预算；未编/复验。

ff7bdf88已精确接common OAuth14、正确wire_test与132–140；039a3e93再接141，当前1–142源码连续，旧Cookie binary不代表这些新增源。Secret恢复4源、Project SecretDelete窄分派/2叶子与142两表由ba1d1a7e保存；e9d30b0f/614eddd1已接MCP原cold私有claim、flow canonical Delete与成功exchange的原SDK connection lease尾。原process不可覆写；actual ConfirmStopped与原锁内完整tuple核验后，Secret只读canonical receipt/metadata，明确未提交才发单用continuation续写同一Delete。有效ready flow跨进程保留，不重exchange/refresh、不读token材料；Project仍需actual Secret同Tx私有cleanup proof，裸DTO沿原active门。

**31343原Wait0**：Secret contract/Secret/Project普通三包 `TestMCPOAuth(DeleteRecovery|CleanupAudit)` 分别0.003/0.006/0.006s；**28690原Wait0**：fixed614eddd1 MCP普通 `^TestOAuthRecovery` PASS0.010s，完整原tuple、原Delete绑定、裸ctx/错误purpose或版本拒绝、原token publication+1 connection版本与Release闭集通过。后继5199 Source门通过，71002已实际应用142；上述类型/纯门仍不代真实Secret continuation或cold App。

新sole `mcp_m3_oauth_recovery_integration_test.go` 已准备两个必要窗口。第一个：原真实exchange返回后，retire identity实际提交并持原Return（Delete尚未prepare），SIGKILL+Wait后新Guard恢复同一个Delete；核原binding/key、Secret receipt/Audit、零重复外发、lease与App Join。第二个在token publication实际提交、SDK尚未开始时结束原进程，验证原connection请求/lease退休及refresh指针精确解除。两窗口共用夹具/预算，仍未integration编译/actual；142连续schema实际门已通过，后继与原staticBearer诊断组合一次固定App，实际分别先第一再第二，不扩完整恢复矩阵。

独审新增两项确定恢复出口缺口，已完成最小静态修正，41747受影响普通及79885私有Web通过，实际证据仍待：原token publication后的SDK请求退休不能留下原refresh busy指针，需在原claim/Guard与完整tuple锁内、正式lease释放后精确CAS，仅原connecting转error/mcp_cancelled，保更晚Owner状态和原command Unknown；claim完成再次核原pointer已脱离。原OAuth lookup保持sameSession/currentProject/current原request读取，在完整cold claim及I/O/lease退休事实后新增独立 `{state:"stopped"}`，不带result、不修改commands.pending，不推断远端结果。私有API严格识别；Session只清本原intent，页面明确“原操作已停止、远端结果无法确认”，Owner检查连接后可显式重新发起，不自动exchange。sole首cold增加真实原Session stopped观察及原command仍pending断言；已发布token但SDK未finish的窗口仍需独立必要actual，控制性普通测试不代SQL/Guard/Secret链。

**41747原Wait0**：fixed e371 MCP普通 `^(TestOAuthRecovery|TestMCPOAuthLookup)` PASS0.011s，覆盖原connection指针收尾、pending与历史成功Begin失效后的stopped出口、原Session拒绝；不代实际SQL/Guard/Secret行为。

**71002原Wait0 / database PASS5.65s**：固定5199普通binary及source1–142，真实PG170008/vector0.8.1 `TestEmptyRepeatUpgradeAndHealth` 通过空库安装、重复应用及健康检查。early owned record的6个container/network ID、3个目录和实际test PID1711405均复核absent；日志 `output/ai/mcp/database-142-5199-actual.log`。142从此已应用不可回写，尚不代表cold worker、Secret原Delete延续或stopped实际行为通过。前置脚本位置参数错误只产生invalid fixture arguments、未创建fixture，独立日志保留，不混作schema失败。

**79885原Wait0**：私有OAuth7叶格式+三spec `mcp-oauth`、`mcp-oauth-page`、`project-mcp-oauth-session` 共21 tests PASS3.80s；actual Session/router/transport验证stopped清原intent、不重复Complete、用户显式新Begin，严格API拒stopped携result、callback显示原远端结果未知。日志 `output/ai/mcp/oauth-stopped-e371-node.log`。本轮未fulltsc/Vite/browser，原80558正常OAuth旅程复用；cold两个实际窗口仍待。

**5453原Wait0**：fixed83a8普通integration App编译包含OAuth OriginalDelete、PublishedConnection与StaticBearer新诊断；准确Config PASS0.01s，严格list三父/两个source实际child匹配。首次child selector拼写错误导致list断言失败已更正，未重不变Config/Source。仅类型与前置通过，不代三个cold实际结果；binary和恢复路径见current。

**82282原Wait1 / OAuthColdOriginalDelete FAIL18.46s**：真实一次code exchange→原retire claimed实际提交、Delete尚未prepare→完整原tuple/Guard核验→旧child1740626 SIGKILL/Wait(signal9)均已达到；新child1740674取得新Guard并ready，但原12s observer无complete claim，只有deadline闭码，尚不能归因于Secret或宣称原Delete、sameSession stopped、Join通过。parent1740375及两child实际退出；8个精确container/network、3个基础目录与全部PID已核absent。原日志 `output/ai/mcp/oauth-cold-delete-83a8.log` 保留，PublishedConnection尚未actual。

后续静态核出共同产品生命周期缺陷：`app.initialize`的security/startup ctx在Ready时取消，`accountAssembly.Start`却直接用它启动MCP Content recovery和OAuth/startup worker，`owner.begin(ctx)`派生工作因此被初始化退场取消；Object/Outbox原维护入口已使用独立取消生命周期。integration唯一App writer修精确两hook并验证原owner Stop/Drain/Join，StaticBearer旧binary同样先暂停。soleOAuth仅新增受限实际Store转发诊断（固定RecoveryCause、闭码、提交状态、阶段布尔值，最多64条），不改产品返回、权限、原12s预算或完成条件；不输出SQL/参数/token/响应正文。诊断尚未编译，不将静态缺陷反填为82282已观测的具体失败step。


**11068/70900 原Wait0**：4b95生命周期修复与12ca闭码诊断已由11068普通integration编译，准确Config PASS0.03s、三父/两child list严格通过。随后同binary原 `OAuthColdOriginalDelete` **PASS6.81s**：真实exchange一次、原retire claimed已提交/Delete尚未prepare，旧child1770259实际SIGKILL/Wait(signal9)；新Guard child1770302按原tuple/私有claim只读canonical观察后继续同Delete key，完成claim、原process保留、无Secret材料read/无重复exchange、sameSession lookup stopped且原command仍pending、新App Joined全部通过。parent1770000与两child原Wait齐，8精确Docker IDs、3dirs与3PID全部absent。日志 `output/ai/mcp/oauth-cold-delete-4b95.log` 与同名前缀owned.json。82282旧失败仍保留，不把静态缺陷回填为其实际失败step。

本轮仅闭原Delete窗口；PublishedConnection的原SDK connection lease/refresh指针出口与StaticBearer stranded仍待同11068分别实际。新 `mcp_m3_startup_integration_test.go` 已保存0efa，两个原enabled连接成功/认证失败保旧完整目录及leases/Join的用例尚未编译，明确不在11068 binary。旧5453本体已由allocator在owner确认obsolete后精确删除，日志/目录保留。


**79336 原Wait1 / PublishedConnection FAIL4.14s**：11068在seed App原退出前置的单次stopped/pg_stat_activity谓词失败；实际Wait、runtime/ownersJoined与flock均已通过，cold child未起、token publication故障点未到。8IDs/3dirs/parent1775004实际退出和absent核齐。原输出未细分两个布尔，不能据此称产品恢复失败或断言只有PG统计延迟。02817仅修本域sole：保全部原Wait/Join/flock，原fixture ctx内2s/20ms有限只读观察同stopped和connection-count、记录首末值；新startup sole复用，不改sharedhelper/产品/原cold12s或总预算。两叶尚未新编译/实际。

**90562 原Wait0 / StaticBearer StrandedContentColdRecovery PASS8.44s**：同4b95/11068与7428 provider，真实static Secret/SDK/Model/Put返回屏障、旧child1781992 SIGKILL/Wait(signal9)、新Guard child1782093正式精确materialization/upload/object清理、MCP owner Joined、Unknown Tool零重派发全部通过。保原未知Execution，**不宣称全App graceful Join**；新child最终实际SIGKILL/Wait(signal9)，parent1781695 Wait0。8精确Docker IDs、3dirs、3PID均absent，日志 `output/ai/mcp/stranded-cold-4b95.log` 与同前缀owned.json。98692旧失败保留，本限域不重复；Published返修、新startup及已暴露Human Refresh外层冷收尾仍待完成。

**89209原Wait0 / 95433原Wait1**：02817 seed观察修与新startup sole共同普通App编译通过；准确Configuration RUN/PASS0.01s与七父/child/startup list匹配。PublishedConnection实际FAIL20.26s，seed stopped=true/connections=0/observations=1已过；child1796832未到publication-return-held，清理实际SIGKILL/Wait9，parent1796581实际Wait1。全8 Docker IDs/3目录/2PID精确退场。日志`output/ai/mcp/oauth-cold-published-02817.log`；不称token publication或恢复半段通过。后继sole只补原Complete返回闭码、原transaction state/安全SQLSTATE和exact tuple gate布尔值，保原20s/12s预算、完整谓词和产品行为。Startup路径独立且现binary已ready；Human Refresh outerlease/pointer闭口仍在未验证私有实现中，无新DDL。


**89987 Startup实际失败与后继受限诊断**：fixed02817/89209、7428provider，两个真实static连接、第二凭据原CAS置无效、seed原Join均通过；child1810796 ready后原12s observer未见两请求complete，child/parent1810426原Wait1，整topFAIL17.44s。8IDs/3dirs/2PID精确全清。日志`output/ai/mcp/startup-02817.log`。截止时DATABASE_CONNECTION_FAILED不是worker根因证据；后继sole仅增加原ctx活跃期phase计数缓存、真实startup Store事务闭码/状态，截止不重新查询、不改预算或完成条件，尚未编译/actual。Published95433的原Complete返回/事务/tuple-gate诊断同下一必要App-c，不原样重复旧输入。

**Human Refresh外层冷收尾**：已暴露Owner Refresh路径由76804f24新增原command唯一outer tuple、实际ConfirmStopped/私有Guard call、正式原Secret lease观察/释放、精确旧pointer CAS和sameSession `stopped`无result出口；保原command pending/远端Unknown，不自动重发。独立审限Go7/Web6未见must-fix。73239限定普通三测试PASS0.010s/原Wait0，8262定向私有6格式+client/session/page三spec 21PASS3.75s/原Wait0；未fulltsc或实际cold。cad346仅补既有provider的SetOAuthLifetime helper，不重provider。新sole一窗口用真实授权/SDK后Owner正式Disable隔离startup，短效token自然到期、实际issuer刷新一次后在原Secret写binding已COMMIT且未prepare边界Kill；后继需核新Guard、同原lease/CAS、原token版本/Secret读Audit计数不变、同key stopped/command pending、零重复issuer/SDK与原App Join。无新DDL/手写业务行；新sole及两诊断尚未编译/actual，不将warm68021或其它三cold代证。


**34393 ready / 68335 Published诊断**：fixed1715399c普通App-c原Wait0，准确Configuration RUN/PASS0.02s及八入口严格list通过。Published原Wait1/FAIL20.25s，原Complete返回无fault，四个相关事务committed且无Fault，但exact gate均false、未达故障注入。child1836730原SIGKILL/Wait9、parent1836478Wait1，8IDs/3dirs/2PID全清；原日志`output/ai/mcp/oauth-cold-published-1715399c.log`。静态确证sole将`cursor.Digest`的JSON key重排摘要用于MCP原Canonical字节SHA256合同，故command_digest不匹配；仅修该gate为rawCanonical SHA256，保全部tuple/预算/已过Secret Delete Audit规则，产品不变。该修待必要重编及Publishedactual；Startup新诊断/Human单窗口不受该gate影响，可复用34393，不重73239/8262/142/provider。

**5311 原Wait1 / StartupReconciliation FAIL17.40s**：fixed171/34393与7428 provider，seed两个真实static连接/第二凭据原CAS失效及seed Join已到。两个startup请求均先完成原Secret lease acquire，却未到dispatch；各自`startup.publish`返回NotCommitted、DATABASE_SQL_FAILED/SQLSTATE23514，随后原retire/release均committed。缓存最终total2/abandoned2/io_started0/io_retired2/lease_released2，不是complete。child1848397与parent1848166原Wait1，8IDs/3dirs/2PID全清；原日志`output/ai/mcp/startup-1715399c.log`。静态对照确定：startup UseGrant重复填RequestID被正式Secret explicit reader拒绝（原RequestID应仅经opaque UsageRequest/proof进入Audit）；00109 MCP Audit contract仅Human，最终actor CHECK也无MCPRuntime，拒绝已合法构造的DiscoveryRefresh技术事件。现场未记录constraint名称，不能将静态两约束结论冒充现场精确名称。root已分配前向143，保1–142不变；已静态落最小grant修、新143两CHECK闭集、startup_secret窄测试及tests/database/mcp_startup_audit_migration_test.go旧142升级/历史保留/技术Actor正反门，后继8a33源码独审、55583 grant普通及70368真实PG两门已通过，143现不可回写；原Startup actual仍待新App。Human34393使用另一正确grant，后继65995已运行并仅最终Join未过。


**65995 原Wait1 / OAuthColdHumanRefresh FAIL51.71s**：fixed171/34393与7428 provider，真实授权/SDK后正式Disable隔离startup，45s原token自然过期、issuer刷新恰一次；原Secret写binding已COMMIT且未Prepare时旧child1865907实际SIGKILL/Wait9。新Guard child1866556已完成原Human外层fulltuple、同lease退休/旧pointer清空，原token版本和Secret读Audit计数保持，原command仍pending，同Session原keylookup为stopped且无result，零重复issuer/SDK。最终SIGTERM后child实际Wait1，原App graceful Join未通过；parent1865656原Wait1。8精确DockerIDs/3dirs/3PID全清。日志 `output/ai/mcp/human-refresh-cold-1715399c.log`，owned同prefix。

静态确定另一已完成exchange的恢复缺陷：原正常Release与`oauth_requests.lease_released=true`同Tx提交，随后正式flow Delete可清除released Secret lease tombstone；冷worker仍无条件DiscoverUsage(Release)，故被正式Secret先loadLease拒绝并保留原call。65995只记录重复claim/stopped_io，原root-return错误被sole过滤，不能把静态机制写成已直接观察到的最终error。最窄修在原完整锁、私有stopped proof与`w.require`核完fulltuple后，仅在原stopped_io事务明确committed且canonical released=true（或原无lease）时跳过重复Release；未released仍走actual provider，Unknown/错tuple拒绝，后续cleanup/原call退休/Join不变。新增三窄测试涵盖裸事实不授权、current released/未released、错tuple/原Unknown；sole仅转发原closed失败及MCP/accounts/producers Joined布尔，不改返回、预算或成功谓词。3Go已固定82164f37，78112三个普通RUN/PASS0.010s，core独立审接受；新App/实际仍待。

**55583/70368 原Wait0 / 143已应用**：固定8a33，startup grant/原Usage proof两普通PASS0.009s、Source143 PASS0.007s；`/tmp/mcp-db143.9BBJMT/database.test`准确两top。真实142→143升级、旧Human历史行保留、重复/health与技术Actor闭集正反（含MaxInt64允许/+1拒绝）PASS4.46s；fresh143既有EmptyRepeatUpgradeAndHealth PASS5.26s，actualPID1883363 code0。6精确DockerIDs/3dirs/PID全absent，独立review已亲核接受。日志 `output/ai/mcp/database-143-8a33-actual.log`，owned同prefix；143不可再改。仅此schema/纯grant门通过，Startup真实publish/两连接隔离仍未过。

**Published 原尚未Acquire的lease观察接缝**：静态核正式顺序：token publication事务只持久化原connection request与预分配leaseID，后续connectionAccess才调用Secret Acquire；原Published屏障正位于两者之间。旧cold尾先DiscoverUsage(Release)要求loadLease存在，导致正式Observe的absence分支不可达。最窄修新增独立MCPRequestLeaseObservationPlanner，仅Project/MCP/ReleaseLeaseUsage/MCPConnectionRequestOwner、SecretService同Project且cause=原ownerID；实际领域planner先核原私有stopped witness，再用原Secret helper包裹完整domain/P/credential/lease锁和issuer。计划不读任何Secret行、不授absence或变更；实际Observe仍在原Tx/全部锁内ValidateUsage→原claim/Guard/fulltuple→Secret精确loadLease。不存在只能由该观察结果确认，存在未released仍正式ApplyRelease；原Unknown与outer CAS不变。旧DiscoverUsage的loadLease及所有拒绝保持。仅此Published caller切换；新增Secret三窄tests核计划零读、实际absence/已有状态/foreign row拒绝、裸proof/错tuple/漏锁/撤销拒绝和旧Release NotFound。5Go已gofmt/diff检查，普通与实际均未运行；不新DDL、不改App/Web/通用Secret Release。


### Published 冷恢复完整收束

c02e672b新增Secret原connection lease的独立观察计划，保旧DiscoverUsage、原私有stopped proof/fulltuple/全部锁和正式Validate→Observe；计划本身不授absence。19117原Wait0，严格六top普通通过（Secret0.005s/MCP0.010s），core独立安全审接受。98513普通App编译与samebinary Configuration RUN/PASS0.01s、八入口严格list通过；当前可恢复binary见current，不再使用已清理的旧34393。

**70701 原Wait0 / OAuthColdPublishedConnection PASS6.47s**：真实token publication COMMIT、原SDK lease尚未Acquire时旧child1907085实际SIGKILL/Wait9；新Guard child1907143沿原完整tuple正式确认lease absence、退休原外层请求并精确清原pointer，保token/Secret材料与原command pending/Unknown。同Session原keylookup stopped无result，issuer/SDK无增量，原App graceful Wait0及accounts/MCP/producers Joined全部成立；parent1906853原Wait0。8精确Docker IDs/3dirs/3PID均absent，core独立亲读原log/断言并inspect接受。日志`output/ai/mcp/oauth-cold-published-c02e672b.log`与同prefix-owned.json。原95433/68335失败保留；此次不重schema143、旧OriginalDelete/Static或正常OAuth。

当前剩余必要实际为同98513的Startup两连接成功/认证失败保旧，以及Human Refresh原binding窗口完整graceful Join。5311/65995尚不得改判；每门按allocator独立授窗，生产text Model能力边界不变。


### Startup 与 Human Refresh 最后实际门

**53497 原Wait0 / StartupReconciliation PASS6.46s**：固定c02e672b/98513与7428 provider，两个原enabled连接分别完成真实SDK成功发现和认证失败保旧完整catalog/Registry；成功修订推进，原request完整tuple、I/O及Secret租约退休均成立。child1921618原graceful Wait0、App Joined/Guard释放，parent1921368原Wait0；8精确Docker IDs/3dirs/2PID全absent，core独立核接受。日志`output/ai/mcp/startup-c02e672b.log`与同prefix-owned.json。失败侧只沿正式closed错误词汇，不外推特定401映射或新增Audit逐row检查；5311历史失败保持。

**63771 原Wait0 / OAuthColdHumanRefresh PASS52.25s**：同固定候选，真实Owner Disable隔离startup后原令牌自然到期、issuer刷新一次；binding已COMMIT且未Prepare时old1924379实际SIGKILL/Wait9。newGuard1925058以原tuple退休同lease并精确清旧pointer，保token版本/Secret读Audit及原command pending；同Session原keylookup stopped无result、零额外issuer/SDK，最终原App graceful Wait0和accounts/MCP/producers Joined/Guard释放全部通过。parent1924129原Wait0，8IDs/3dirs/3PID全absent，core独立actual核接受；日志`output/ai/mcp/human-refresh-cold-c02e672b.log`与同prefix-owned.json。65995原完整FAIL保留；这里只验证disabled连接、原binding提交/Prepare前的已暴露窗口，不扩大到enabled竞争或任意Secret写Unknown。

原明确剩余MCP必要实际链已闭合。53专属叶与Project SecretDelete七行窄hunk已由root/integration组合至common，保共享后继；下一只做common受影响编译/完整vue-tsc，不新增MCP矩阵。Static90562仍仅MCP owner Join且保Unknown Execution，生产Model仍不支持多模态；这些边界不因本轮通过而扩大。
