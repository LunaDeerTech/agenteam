# D27：Project Owner 项目审计只读界面

修订：rev1，2026-10-08。状态：工程规格候选，待独立 STATIC 和 root 采纳；本稿不授产品实施、Go/Node 检查或真实资源。只写本卡与任务自有规格证据，主线程负责 Git、台账及后续授权。

前置更新：Owner 工作区 [rev2.1](d27-project-owner-workspace-ui.md) **最终24路径已独审/root正式采纳**，#22末件已提交推送 `0a939a7f12a607708e40796ac0bfae61592e7099` 并由root核远端一致，独立九轮永久档 `f9fb58d1` 已交付。本卡仍须完整SPEC独审/root采纳及共享 client/Session/router/workspace 唯一写权移交，才可实施。当前品牌已接受的 App/导航/auth 资产必须保留，不用旧 Owner snapshot 覆盖新品牌内容。

## 1. 完整结果、前置和范围

当前 Human Owner 从自己项目的“项目设置 → 安全记录 → 项目审计”读取统一 Audit 列表，明确应用结构化筛选、前后分页，并在同页打开一条安全详情。普通 Owner 与拥有自己项目的管理员遵循同一规则；管理员没有他人项目旁路。加载、空页、失败、权限变化、取消后的明确重读、双导航与窄屏构成同一完整结果。

| 正式前置 | 已接受能力与本卡消费 |
| --- | --- |
| [Project Owner Audit HTTP](d04-project-owner-audit-http.md)，产品 `4089d13128da8680955005d9507c8a7da74af1f1`，[完整16验收](../agent-team/project-owner-audit-http-verification.md) | 两条 GET/HEAD、同一事务当前 Session/Owner/Read gate、31种安全记录、分页、3s发布预算与实际尾部、默认根已绑定。本卡不改后端或 schema。 |
| [Audit final16更正](../agent-team/project-owner-audit-http-verification-evidence/originals/independent/final16/result.json) | cursor 只绑定 scope/filter/order，不绑定 Session 或 limit。每次读取仍重新授权。原 final14 误句不作为本卡依据。 |
| [Owner 工作区 UI](d27-project-owner-workspace-ui.md) | 既定 Resolve→稳定 ID Get、完整 Session identity、唯一 Cookie owner、双导航和基本信息草稿。最终24接受已满足；共享文件仍待正式移交，本卡不重做创建或生命周期。 |
| [System Audit UI](d27-system-audit-ui.md) | 复用已接受的筛选/分页/内联详情交互风格与受控取消经验；System/admin、十字段/37动作投影不能充当 Project parser。 |
| [项目设置布局](../../frontend-design/layouts/project-settings.md)、[通用设置](../../frontend-design/layouts/settings-shell.md)、[Audit架构](../../architecture/security-governance/audit.md) | 已确定设置尾部的项目审计入口、Owner权限、安全字段、双导航与展示边界，无新增待用户决定的产品含义。 |

实际 root 的 `app/project_audit.go` 从同一 `audit.Service` 与 Account boundary 构造 handler，`app/account.go` 已分派；没有需补的生产查询口。实际前端 ProjectSettingsView 目前只有“基本信息”，router 只有 `/settings/general`，client 只有 System Audit 两操作；这是本卡补齐的产品缺口。

本卡没有业务写入、幂等命令或恢复 receipt；不新增 Project/Model/Secret/Agent/Skills/成员/权限管理、Audit导出/全文搜索/删除、关联对象补读、轮询或实时订阅。Project Provider/Model UI 属于后继卡。production Skills/创建 HTTP、Resolution/Invocations/D24 与既有未绑定项保持；不借读取合法历史类型声称对应 producer/runtime 已实现。`readyz=503`、D08–D28/E01未完与 Object runtime join / OpenAI tools独立验收 / SPA并发发布三停止保持。

按[设计技能](../../../.agents/skills/agenteam-design/SKILL.md)、[Vue开发](../../../.agents/skills/agenteam-vue-development/SKILL.md)、[Vue测试](../../../.agents/skills/vue-testing-best-practices/SKILL.md)执行；后端harness按[Go技能](../../../.agents/skills/agenteam-go-development/SKILL.md)，独审按[验收技能](../../../.agents/skills/agenteam-verification/SKILL.md)。真实浏览器实施时读取可用 Playwright 技能，缺失时如实记录；本次规格未运行浏览器。只加载任务相关参考，不因示例新增依赖。

## 2. 唯一页面、项目定位和可见交互

新增精确叶子 `/:username/:project_name/settings/audit`。ProjectSettingsView 保留“项目资料 → 基本信息”，在最后新增稳定 key/组“安全记录 → 项目审计”；普通 `/settings` 仍到基本信息。System 菜单、系统审计、个人设置、品牌和 ProjectNav 的首页/设置双导航不变。未实现的设置栏目不放空按钮。

`projectRoute` 和 `safeReturnTarget` 仅在原闭集追加 `/settings/audit`。沿 Owner rev2.1 原始路径检查，先检查再 decode/clean；username、含点项目名、保留首段、大小写标准地址及 `forgot-password`/`reset-password` 双段合法 Owner 路由规则保持。query/hash/百分号/编码分隔符/反斜线/点段/多余后缀/尾斜线/数组不进入安全 return；不新增 `/audit/:id`、URL筛选或cursor参数。登录回跳后重新走正式 Owner 读取，不复用登录前授权。

直接地址仍先 Resolve 得候选 ID，再 Get 当前稳定 ID；两次完整结果且当前 identity/route generation 一致后才显示项目内容和发 Audit GET。仅 Resolve/列表行/用户名相同/旧 Project snapshot 不授本页读取。Get 失败、deleting/未初始化、未知/无权项目时零 Audit 子资源请求；旧名复用得到另一 ID 必须全新页态，不能迁移筛选、cursor、详情或旧响应。当前 Get 确认改名后的标准地址保留 `/settings/audit` 后缀，旧链接无别名。

使用既有 workspace 的稳定绑定，新增只读 `currentReadContext` 接缝：`Readonly<{ identity: PersonalIdentity; projectID: string; generation: number; readGeneration: number }> | null`。只在当前完整 Get、当前 Human/路由、既有 boundID 相符且 phase=current 时提供；两个代次来自已有 workspace 所有权，不由页面发明 grant。checking、读取失败/重读中、失去绑定或离开项目时为 null。这个接缝只暴露发布屏障，不添加 I/O，不改变 Owner 基本资料、历史命令、草稿、确认或既有读写状态机；每个 Audit HTTP 请求仍由后端重新授权。

页面只在上述上下文有效时消费它；active/archiving/archived 均可读，不能把 workspace 的普通编辑 `readOnly` 当成审计拒绝条件。归档提示沿原壳保留。审计读遭拒后显示局部不可用，清本页私密观察；不能拿旧 Get 恢复成成功、调用 System Audit 或推断不存在对象属于谁。

页面标题“项目审计”，说明只观察当前项目安全事件、时间为 UTC、分页不是全域快照。列表显示时间、历史 Actor kind及安全ID、动作code、outcome、resource kind/id、安全summary和“查看详情”。`unknown`显示“结果未知”，不是可重试业务命令；不存在的关联对象不自动跳转或补名字/邮箱。只显示本页条数与前后页可用性，不估总数。

筛选按 System Audit 的三个语义分区组织，全部十四个过滤字段有可见 label；关联分区可折叠，但未应用输入/错误不能被隐藏或丢弃。draft 与已应用 immutable filter 分开，仅“应用筛选”“重置筛选”“重新读取”或明确翻页发请求；无debounce、输入即请求或前端二次筛页。“重置筛选”明确清过滤并应用默认50条首页。limit使用canonical十进制字符串，接受1–200，默认50。

十四个字段精确为 `from,to,actor_kind,actor_id,action,outcome,resource_kind,resource_id,tool_id,execution_id,operation_id,approval_id,runner_id,agent_id`。时间按原 ParseInstant 语义校验时区、0–6位小数及日历，规范为UTC六位微秒，区间[from,to)且from<to；ID为canonical UUIDv7，service+actor_id非法。action的53个合法过滤值、resource_kind的25个合法值、三种actor与四种outcome完整保留；System-only合法action在Project返回空页，不从31种输出倒推删掉过滤项。筛选错误保留输入并定位字段，零请求。

每次应用新filter/limit从首页开始并清私有cursor历史；下一页用原next_cursor，前一页用本页期保存的先前请求cursor，返回首页显式无cursor。页面选择改变limit时回首页是UI策略，不表示HTTP cursor绑定limit。CURSOR_INVALID保留已应用条件和错误，提供明确“从第一页重新读取”，不自动丢token并续发。

详情为同叶子内联区域，隐藏列表操作并提供“返回列表”“重新读取”。始终发正式详情GET，不以列表行代替详情；audit_id必须匹配原target。返回列表取消未完详情并清目标/详情，使用本页期原完整列表及cursor历史，零自动列表GET。所有合法metadata/associations按固定标签、顺序和文本节点显示；无v-html、Markdown、自动URL链接、任意JSON dump或配置编辑器。

## 3. 两个客户端操作和Project安全投影

新增纯闭合接口，不收任意URL、scope、owner、header或原query字符串：

```ts
interface ProjectAuditAPI {
  list(projectID: string, query: AuditQuery, signal: AbortSignal): Promise<ProjectAuditPage>
  get(projectID: string, auditID: string, signal: AbortSignal): Promise<ProjectAuditRecord>
}
```

client新增 `listProjectAudit` GET `/api/v1/projects/{project_id}/audit` 和 `getProjectAudit` GET `/api/v1/projects/{project_id}/audit/{audit_id}`。projectID/auditID分别校验为canonical UUIDv7，各自替换单个路径段；列表typed query一次编码且RawQuery≤32KiB，cursor≤8192B，零参数不生成裸`?`；详情拒绝query。同步捕获/冻结实际目标与query后才await，未知字段/不合法值零fetch。不发送body、CSRF或Idempotency-Key；同源Cookie、redirect:error、cache:no-store、安全Problem/RequestID链保持。

两操作必须具有**独立Project Audit分类**：成功响应上限1,048,576B，不落入现有ProjectEndpoint成功64KiB、普通600000B或System权限分支。只改变这两口的成功读取；旧Owner list5MiB、其它Owner64KiB、SystemAudit1MiB、Provider list2MiB、RuntimeInformation16KiB与Problem600000B保持。不能接受caller任意放大cap。

只接受完整200 application/json。沿真实stream累计实际chunk字节、fatal UTF-8、完整EOF、JSON解析及全对象refinement后一次发布immutable结果；不信Content-Length、不先response.json、不发布前缀/截断字段或跳过坏行。取消、截断、错媒体、cap+1、非法UTF8、损坏DTO均零新候选。实际reader.cancel/body.cancel Promise、releaseLock及transport finally结束前保持Cookie owner；UI拒绝Promise或abort信号不能当成网络/服务终局。

`ProjectAuditPage`恰`{items,next_cursor}`；items非null数组、≤请求limit且≤200；next_cursor为null或1–8192B原安全ASCII token。非null cursor须满页、空页须null；audit_id唯一，`(created_at,audit_id)`严格倒序；坏末项/逆序/跨Project/错误filter/不满页带cursor使整页失败。客户端对有wire依据的filter关系做一致性校验，不因字段未公开或后端无匹配行而补读关联对象。

`ProjectAuditRecord`精确11个必有键为`audit_id,created_at,scope,project_id,actor,action,outcome,resource,metadata,associations,summary`。scope字面project、project_id等于请求ProjectID；详情audit_id等于原target。UUIDv7、canonical微秒UTC（0000–9999年）、Version/Progress十进制字符串与MaxInt64界沿正式schema/构造器，不经JS Number丢精度。unknown/null/额外属性/条件分支混用整条拒绝。

Project专用discriminated readonly metadata/parser/显示投影严格消费 [HTTP§3–4](d04-project-owner-audit-http.md#3-http-路由过滤分页与安全-dto)、[project-audit.json](../../../api/openapi/project-audit.json)与原Go typed构造器：

| 关键闭集 | 必须完整保留 |
| --- | --- |
| 31输出action | Secret4、Outbound1、Object6、Artifact4、Outbox1、Project8、Model6、Knowledge1；不得以当前真实fixture仅生产部分族而删其它合法历史。 |
| Actor | Human id；AgentRun id/project_id/execution_id；Service service/cause_ref/project_id。Project一致，AgentRun与execution关联一致，13合法service仍受action限制；当前读取者Human不等于历史Actor。 |
| Resource | `secret,outbound_policy,agent,stored_object,object_transfer,artifact,artifact_collection,outbox_delivery,project,project_operation,project_creation,model_provider,model_config,knowledge_document`；outbound_policy无id，其余ID及action binding严格。 |
| Associations | 九个可选UUIDv7：tool_id/execution_id/tool_call_id/operation_id/request_id/approval_id/runner_id/correlation_id/http_trace_id；缺省可省略，不补null，并核每action允许子集。 |
| 条件metadata | 完整沿原HTTP§4表：phase/reason/source/revision、initiator种类、project/creation/operation版本与过渡、changed_fields排序去重、resource/ID/outcome关系。Model只correlation/http_trace，Knowledge禁全部关联；不能套System投影的较窄五关联。 |
| 安全scalar | 原canonical MIME≤256B且仅受限charset、27 reason闭集、计数int64、sent_bytes约束；Object transfer.complete要求sent=byte，Artifact download.sent不额外发明该相等条件。 |
| Summary/metadata字节 | Secret四动作及outbound deny使用正式五条固定summary，其余为`Audit event`；metadata≤4096B按Go安全JSON转义语义计量，含HTML和U+2028/U+2029，不能拿普通JS.stringify字节冒充。 |

可复用既有纯Query/Instant/scalar函数，禁止调用System Audit API、借System actor/resource parser接受错误scope、修改旧System闭集或引入新后端字段。页面显示安全ID不意味着可读关联正文；没有Secret材料/credential值、Session Cookie/CSRF、命令key/body、SQL、Provider配置值或模型请求响应。原HTTP的1MiB容量证明沿既有接受，不重新发明页面极值/RSS承诺。

## 4. 唯一Session owner、页态和取消恢复

`createSessionController`在既有13参数末尾追加默认 `projectAuditAPI`，前13位置和旧省略调用不变。新增闭集`ProjectAuditAction = 'project-audit-list' | 'project-audit-get'`、本域独立revision及封闭`auth.projectAudit.{list,get,abandon}`，两typedAction只经原runAuthorized；不得导出任意callback或另建HTTP/CSRF/身份队列。capture输入失败零请求，abandon只退休这两个kind的实际owner/代次，不销毁Owner Update原key/body/draft、System Audit、Summary双draft或任一其它写域intent。

runAuthorized对本两action显式采用当前authenticated Human、完整userID/sessionID/epoch、operation generation和本域revision条件；不能落到现有非personal的admin/systemFailure默认分支，不把它们伪装personal或现`project-read`。现System admin/denied与Owner读写分类保持。clearIdentity/真正Session切换/全局dispose仍退休本域；普通role变化不凭admin资格改变当前Owner读权限，后端每次判断Owner。

本域current 401只有原有效`UNAUTHENTICATED`/`SESSION_REVOKED`配对按既有失效链处理；局部403/404不设System denied、不授别项目、不丢Owner未决写材料。GET不新增CSRF_FAILED身份门禁；未知code/status错配、旧identity/op/revision的迟到安全错误只作该已退休请求结果，不能清新Session。后端500/503、COMMIT_UNKNOWN/unknown和兼容retry_hint=lookup均是读取失败：只提供明确新读取，没有lookup端点、自动retry、命令追踪或“原事务未提交”断言。

本域继续同一个30s可见期限和实际finally释放；后端Audit3s、更早parent与同步I/O尾部语义不变。可见超时/取消/导航不释放owner，当前owner真实fetch/body/cancel/tail未完，Logout/恢复Session/新Audit/Owner及所有旧域Cookie操作均不能越过。释放尾部只解锁，不让旧catch/finally覆盖新状态或自动再读。

`useProjectAudit(auth, workspace)`是页期controller，持有filterDraft/applied、列表观察、详情target/观察、cursor历史、完整identity/ProjectID/workspace读代次/本页generation；没有App期审计缓存、写intent、localStorage/sessionStorage/history数据。列表至少区分waiting/loading/ready/empty/error/unavailable/inactive，详情另有not-found；empty只来自合法完整200空页。

每次请求冻结上述完整归属与目标；成功时全部仍当前、workspace context有效、页面/route仍所属本audit叶子才发布。开始读取清该目标旧候选，列表行不冒正式详情。失去currentReadContext、ProjectID/路由身份变化、checking/离页/销毁同步退休本页读代次并清私密观察/目标/cursor；不得仅watch组件unmount，因为Vue可在不同项目参数间复用同一页面。

新页/新已绑定Project generation仅有一次初始默认第一页资格，可等待旧owner真实尾部再发；等待期间失权/离开/销毁则零续发。已经发出的本页读取失败、取消、超时后，同页generation不自动重试；明确操作才新发。Owner链仍未当前时先显示其恢复路径，不为Audit强发Owner Get或抢先读取。

同完整Session checking也销毁审计页期筛选/页/详情，不能复用Owner“保留同身份编辑草稿”的策略保存审计私密页态。Session503时只显示原App恢复UI，零Audit续发；同identity恢复后新controller等当前Owner context与旧owner实际尾部，至多一次默认首页。前端清cursor是页态隔离，不改变后端cursor不绑定Session/limit的契约。真正换Session/user、旧名复用/切换项目不带入任何旧观察。

“取消读取”始终可用，不因busy禁用；同步退休代次，当前loading转可明确重读的error，保留本次合法重读目标。尾部未完仍busy，完成后只解锁。返回详情/取消/导航各自local focus回调在nextTick或await后重新核页面、identity和Project generation；不能对已卸载节点或新页面抢焦点。

离开守卫和参数更新守卫先退休本页读取，正常放行原全局认证/Project确认链；只读filter draft不注册beforeunload或新增丢写确认。后续旧域guard取消导航时，仍显示的本页保留明确错误与显式重读入口，不自动重试或永久inactive；最终unmount才不可逆dispose。不能借审计导航同意或清除基本资料/System Selection/Summary未保存内容。

## 5. 布局、可访问性与真实fixture

沿原SettingsShell/ProjectNav/Ui控件与tokens，不改shared UI或全局CSS。当前项、aria-expanded/controls、手机Drawer的Tab/Escape/遮罩/关闭焦点保持；标题可本地聚焦，inline详情进入到标题、返回优先原记录按钮，失效才到列表标题。每个筛选错误由label/aria-describedby关联，加载/失败有status播报；列表和详情长UUID/MIME/计数可换行，窄屏单列/记录堆叠，无页面级横溢出。

八个布局状态固定 light/dark × 宽390/768/1024/1440，视口高度明确固定并记录；八张实际截图覆盖有记录/筛选和安全长值/内联详情，另实际键盘、Drawer、reduced-motion与overflow断言。不能把截图文件存在等同视觉通过，也不把不可见折叠/屏外内容判通过。Debug只开发、生产资产不含Debug；不是生产SPA发布或浏览器原生tab验收。

新harness在`tests/account`的两新文件中组合真实无tag `app.Run`、同库Account/Project/Audit与本卡私有同源dist/proxy。可调用已接受Account邀请/登录/Logout与Project准备的包内helper/类型，不改旧helper或公共fixture。原`projectOwnerWebPage`只接受三个后缀，不直接复用为本audit页面fallback；新私有harness明确加入本卡唯一audit闭集，仍raw path先验、点名直链可达、真正静态资产miss404，不碰停止的生产SPA源。

测试主身份由正式bootstrap→邀请/兑换→Login建立普通Owner、另一个Owner和admin；已有初始化Project沿正式Project Service与已接受持久Skills测试隔离口准备，保持Project/Audit/Outbox同库及真实权限。测试隔离口不进入production、不声称生产Skills/创建HTTP已绑。以正式Project Update、Project Model credential/config等已有命令产生本scope多族Audit，所有浏览器观察的成功记录来自正式producer；不能直接Append/SQL造Audit行或假200。其它31类投影边界放在有来源的纯测试，不启动未绑域或谎称31种producer真跑。

archiving/archived/deleting/初始化未完等反例只沿已接受Project fixture的精确辅助事实与锁/正式服务准备，并单列“不是完整生命周期运行”；不新建生产participant、改role或任意Owner allow。实际HTTP每页/详情仍正式重新授权。SQL仅对自有项目/事件做有限事实旁证；任何辅助状态变更与审计观察用途、精确目标和时序须记录。

proxy只对精确本Project Audit集合/合法详情GET作计数、受控hold/截断/失败；原完整正式响应先到达再控制，成功主体不得修改。Session503仅沿既有私有精确一次控制，标清受控故障与真实后端结果，不能冒物理COMMIT Unknown。每个hold/callback/server/client/child创建即登记release+实际join，失败/Fatal前已可退休；取消HTTP、关闭socket、helper返回不是原handler/body/callback已完成证明。Go IPC仅任务自有0600文件/原子rename，动作闭集、sequence与返回键在Go/JS冻结时共同核对；不重犯额外`ok`与旧协议不符。

资源和代码准备阶段不执行业务。未来实际窗使用原完整Object→Outbound→PG fixture链，不缩链、不添SMTP；每轮4容器+3网络预期7个ID，结果逐ID实核。`tests/testsupport/postgres/cmd/fixture/main.go`已选择`./tests/account/...`，`tests/process/process_test.go`的原TestMain仍实际构建两cmd；本卡**无需改fixture main或任何TestMain**。新Go top自然在原包内被精确selector选中。

## 6. 候选唯一写入范围与交接（20技术＋1末件）

下表是规格接受后待root授权的产品闭集，不是本稿实施授权。当前仅本卡文档由backend_worker写；其它作者已验源保持。

| # | 路径 | 允许差量与唯一执行角色 |
| --- | --- | --- |
| 1 | `web/src/api/client.ts` | frontend：两Project Audit GET独立options/path/query/1MiB分类，保全部旧cap与操作。 |
| 2 | 新`web/src/api/project-audit.ts` | frontend：两API、typed Query/页/记录完整投影。 |
| 3 | 新`web/src/api/project-audit-metadata.ts` | frontend：Project31动作、actor/resource/关联refinement和安全显示字段。 |
| 4 | `web/src/composables/useSession.ts` | frontend：第14默认依赖、本域action/revision/facade/当前Human错误隔离及实际owner。 |
| 5 | 新`web/src/composables/useProjectAudit.ts` | frontend：页期读状态、Project/identity代次、取消/分页/内联详情。 |
| 6 | `web/src/composables/useProjectWorkspace.ts` | frontend：仅公开既有稳定绑定的只读currentReadContext；不改原草稿/命令/确认/读取行为。 |
| 7 | `web/src/router/auth.ts` | frontend：原raw path/return闭集追加唯一audit后缀。 |
| 8 | `web/src/router/index.ts` | frontend：仅新增project settings audit child。 |
| 9 | `web/src/views/projects/ProjectSettingsView.vue` | frontend：设置尾部安全记录组/项目审计叶子，基本信息默认保持。 |
| 10 | 新`web/src/views/projects/ProjectAuditView.vue` | frontend：一页筛选/列表/内联详情、local focus与两类导航守卫。 |
| 11 | 新`web/src/tests/project-audit-client.spec.ts` | frontend：路径/query、整页/1MiB/真实stream尾部。 |
| 12 | 新`web/src/tests/project-audit-metadata.spec.ts` | frontend：有正式来源的31合法动作/条件安全反例和显示投影。 |
| 13 | 新`web/src/tests/project-audit-state.spec.ts` | frontend：真实Session/workspace factory及受控transport、身份/项目/owner竞争和显式重读。 |
| 14 | 新`web/src/tests/project-audit.spec.ts` | frontend：实际App/router/View组合与用户可见筛选/详情/导航行为。 |
| 15 | `web/src/tests/authentication.spec.ts` | frontend：只扩Project合法后缀代表及其拒绝反例，保旧静态/公开token语义。 |
| 16 | `web/src/tests/project-workspace.spec.ts` | frontend：仅现基本信息单菜单期望增项目审计（当前约245行），其它Owner写/冲突断言保持。 |
| 17 | 新`tests/account/project_owner_audit_web_fixture_test.go` | backend：本卡真实root/私有dist、正式producer/状态准备、安全原body及有界控制/实际join。 |
| 18 | 新`tests/account/project_owner_audit_web_test.go` | backend：三个新真实top与必须证据键，不改原top。 |
| 19 | 新`tests/account-captcha-web/project-owner-audit.config.js` | frontend：三个精确case、单worker/retries0/45s、固定Chromium、私有输出禁trace/video。 |
| 20 | 新`tests/account-captcha-web/e2e/project-owner-audit.spec.ts` | frontend：真实页面API/权限/读取恢复、safe同body联验及八图。 |
| 21 | `docs/development/frontend/README.md` | frontend：完整20技术独审及root采纳后另授末件；从Owner24与品牌接受后的最新正文增量。 |

共享#1/#4/#6–9/#15–16须由root在前卡最终24接受后正式交回单一frontend写者；App.vue无需新provide/确认，因此保持只读。后端app/account.go/project_audit.go、业务库/handler、OpenAPI、迁移、依赖锁、脚本、全部旧Go/JS harness和shared UI均只读。若实际签名/旧断言证明需要额外路径，先给具体差量由root修订移交，不能用本表“兼容”推定权利。

frontend/backend互相交冻结API/IPC与fixed dist读权，不读活动候选下结论；独立验收只消费STOP版本。root唯一持有共享web/dist交换、数据库/Docker/端口和Git窗口；通常采用任务私有dist，如受影响旧harness实际依赖global dist，root独占安装固定资产、保原目录、最后所有reader实际退役才恢复。禁止后台互相覆盖产物或把品牌新资产复用成旧53的哈希结论。

## 7. 有意义的验证、精确预算与原件

当前规格自查只做来源/路径/链接/封闭契约核对，不运行下列实施检查。实现先固定20技术与真正消费的前置/工具/资源输入，复用已验实际Go依赖图和工具版本，仅补本卡必要源、imports/embeds/动态TestMain/两cmd/fixture变体与JS/schema/dist差量；不复制全树/cache或机械生成另一份大型索引，不能直接抄旧955/1165当新输入数。

### 7.1 离线和受控验证

| 检查 | 有意义的目标与命令形式 |
| --- | --- |
| format/types | 仅授权web/JS文件用既有Prettier `--check`；`npm --prefix web run type-check`。README不提前改。 |
| 新pure/组件 | `npm --prefix web run test:unit -- src/tests/project-audit-client.spec.ts src/tests/project-audit-metadata.spec.ts src/tests/project-audit-state.spec.ts src/tests/project-audit.spec.ts`；验证用户可见行为和实际factory，不以实现快照互证。 |
| 受影响旧pure | 一次执行既有Owner client/workspace/state、authentication/session、System Audit四文件及全部旧Session互斥覆盖；同输入已通过结果可组合复用，公共Session变更不能只跑新页。最终既有完整unit组做一次，受失败影响才再跑。 |
| build/check | `npm --prefix web run build -- --outDir <任务自有绝对dist>`；冻结实际assets、Debug排除。最终`npm --prefix web run check`只在root明确web/dist写窗运行；超过单命令预算先保原失败，再协调等价子命令分组，不暗加时间。 |
| Go闭包/compile | Go1.27.1、GOTOOLCHAIN=local、GOPROXY/GOSUMDB=off、-mod=readonly、明确既有GOMODCACHE/GOCACHE。仅必要`go list -deps -test -json -tags=integration ./tests/account`差量、`go test -race -tags=integration -c -o <私有binary> ./tests/account`、`go vet -tags=integration ./tests/account`及真实动态两cmd构建输入；不启动app。 |
| list | 对私有binary做三个新top与必要旧top精确`-test.list`；先核init/TestMain行为，compile/list/SKIP不计业务PASS。 |

每条离线外45s，若实际执行Go纯test内部40s；绝对工具路径/版本/原argv在执行freeze记录。不增加依赖/升级、改HOME、自动安装浏览器或宽泛全app测试。格式/类型/编译失败按原件保存，范围内修正后只重验受影响输入。

API/metadata用正式schema/Go构造规则来源的31动作合法向量与条件反例，覆盖三Actor、14resource、9关联、53/25合法filter闭集、日历年0000/9999与微秒、MaxInt64/排序/MIME/HTML转义、缺省与null。完整页0/200、等时ID、坏最后记录/错详情target/跨project/逆序、cap和cap+1实际chunk/EOF/非法UTF8，Problem与所有旧cap分类必须拒错。受控流扣留真实cancel Promise，核零前缀/零第二Cookie请求；合成200/Unknown或极值向量不冒正式producer/物理事务证据。

生产Session/workspace/controller配受控原生Response：覆盖新域与Owner读写/原命令lookup、个人/认证、System Audit及所有原System mutator双向排他，尤其SMTP私有材料/未决intent、Selection/Summary双draft隔离。当前合法401与局部403/404、错配/迟到安全错误、同identity checking/新Session/跨Project、响应晚到和cancel尾部均要独立断言。明确证明“同实例失败只显式重读”和“新的合法页代次可一次初读”，不能只靠flushPromises当定时器或实际tail完成。

实际App/router组合核唯一新后缀与拒绝闭集、点名直达、普通Owner/admin自有项目、双导航/设置默认/新尾部组、14字段应用、inline focus目标、无业务写入口；onBeforeRouteUpdate在复用组件跨Project时的退休，以及后续guard取消导航后可显式恢复。不能强造busy时原App pageshow监听器已执行：真实pageshow→Session503从空闲owner开始；旧tail→新页初读用另一个合法受控屏障验证。

### 7.2 三个新真实top

每个top独立新nonce，原full fixture链：`sh scripts/test-objects.sh -run '^(<精确top>)$'`，工具由冻结AGENTEAM_GO指定。每个browser case **45s**，单worker、retries=0；每个Go top **120s含全部Cleanup**，原package **6m**。注册顶层起始预算检查早于fixture cleanup，不能以main返回早于尾部算成功。不得合并超预算大组、失败重跑到绿或延长原预算。

| 精确top / selector | 必须完成的真实证据 |
| --- | --- |
| `TestAccountProjectOwnerAuditWebReadAndFilters` / read | 正式普通Owner和既有Project、Project/Secret/Model生产Audit；实际list/detail匹配同项目自有事实，至少三typed族；小limit真跨页、全部14控件实际query、组合过滤及System-only合法空，cursor错误显式首页；两GET safe原body/schema/公开client同bytes，页面行为零mutation。 |
| `TestAccountProjectOwnerAuditWebAuthorityAndRecovery` / authority | 普通Owner及admin自有项目成功、非Owner/admin他人项目拒绝；两GET当前Session/Project gate、archiving/archived可读、deleting/未初始化拒绝、详情404；正式Logout，当前与迟到响应隔离，list/detail受控截断/取消实际join→仅显式重读，零lookup/key/写命令；跨Project参数复用/旧名复用不迁移页态，旧域owner竞争另有受控真实factory证据。 |
| `TestAccountProjectOwnerAuditWebNavigationAndLayouts` / navigation | 直接及登录return含点名、raw拒绝零ProjectAPI、两设置叶/默认基本信息/双Nav；filter本地修改不新增确认，原Owner/System写域确认不被代同意；inline详情返回真实focus、Drawer键盘/Escape/遮罩、空闲pageshow503→同身份恢复新页默认读；八布局图、reduced-motion/overflow、冻结production资产无Debug。 |

read组的正式准备写和browser本人只读分开计数；setup GET、SQL旁证、控制IPC和测试客户端验证不能计作browser请求。“零mutation”指审计交互不发Project/Model/Secret写入或任何命令lookup，不将明确Login/Logout及原会话恢复误记为违规审计动作。跨权限HTTP反例若被Owner入口先挡住，另由同浏览器受控发正式GET验证API拒绝，并明确它不是页面自动越权请求。完整31动作纯覆盖与实际三族producer范围分列。

响应证据只捕获本卡两条GET的安全原始response bytes及安全Problem，旁车绑定run/top、固定源/asset hash、精确method/path/query、status、允许响应头、RequestID和body SHA/长度；query为schema允许的ID/时间/code/cursor，不收请求Cookie/Authorization/CSRF或控制私料。标准Draft2020-12+FormatChecker使用既有common/Project schema与正式0000年日历兼容扩展，同一原字节再通过**公开ProjectAuditAPI和真实accountTransport**（包含原参数、状态、媒体、X-Request-ID及chunk/EOF）解析；不重新编码或只调用私有decoder。截断/取消/受控Problem明确分类，不能把完整服务侧预截断body当浏览器已成功接收。普通logs/screenshots/trace禁止材料；trace/video关闭，截图不含私有准备文件。

### 7.3 必要旧兼容和独立代表

共享Session/Project raw路径实际受影响，作者按单top串行运行下列原精确组，保原内部case和断言；不机械重复本卡未改变的后端Audit纯/PG全集，也不以之前Owner九轮代替新版本兼容。

- `TestAccountAuthenticationWebLifecycle`、`TestAccountAuthenticationWebRevocationAndExpiry`。
- `TestAccountPersonalSettingsWebThemeAndNavigation`（个人导航/未保存确认）。
- `TestAccountSystemAuditWebReadAndFilters`、`TestAccountSystemAuditWebAuthorityAndOwnership`、`TestAccountSystemAuditWebNavigationAndLayouts`（原System/admin与metadata/menu保持）。
- `TestAccountProjectOwnerWebReadAndNavigation`、`TestAccountProjectOwnerWebIdentityAndOwnership`（原raw闭集/Owner隔离及写域聚合确认保持）。
- `TestAccountSystemMeetingSummaryWebAuthorityNavigation`、`TestAccountSystemSMTPDeliveryWebTestAndRetry`（双draft确认与独立SMTP owner兼容）。

这些是十个旧top的技术最小代表，root在实际冻结selector前核原全名/依赖；缺名先停，不扩大成模糊regex。只SMTP delivery组实际预期9资源，其余7；新三个也各7。完整unit覆其余旧mutator分类，不称全部旧browser重跑。独立验收负责人对STOP完整源审查并亲跑至少：新read+authority、旧System Audit authority、旧Owner identity；八图逐张看可见区域，必要风险差量由其计划明示。作者PASS和独立原件复核不能冒“独立亲跑”，未亲跑范围如实记。

### 7.4 资源、终局与接受

工具、两个PG固定digest、原MinIO binary/SHA及Chromium/Playwright版本复用既有已接受来源；开始前仅核该轮实际依赖，不默拉tag/网络安装。每轮fresh空间≥5GiB，独占PG/MinIO/Docker/端口、空Docker配置、实际PID+starttime/Docker/TCP全态baseline；原fixture nonce/labels/exact IDs及输入门禁不缩减。外层受控driver保builder/direct/adopted实际wait、watchdog实际join与失败前注册的finally；不要以SIGKILL已发、ctx取消或列表为空代替join。

每轮PASS并actual退役、exact7/9 ID两次absent、owned进程/runtime两空、原baseline资源不变、输入前后同，TCP含TIME_WAIT差量在原75s观察预算内两清，原件齐全后才下一轮。任何FAIL/不完整先全退役保存原raw/断言/未到达范围，停止后继，不自动retry或改预算。daemon/PID1非owned shim另列未wait，不称全机零；现Object forced-root/inner-join限制不由harness正常终局抹去。

完整交付必须同时满足：本卡20技术STATIC/适用受控与真实组、同body安全schema/client、权限/取消恢复与八图、独立代表和root审查；之后另授README末件，核最新Owner/品牌正文及相对链接。报告分清成功/失败/未执行、固定来源/工具/原argv/实际exit、作者与独立、读端原后端语义复用与新页面行为；不因本卡完成标记完整D27/系统ready/E01或三停止完成。

## 8. 本次规格来源与停止点

本稿作者只读核对上述正式卡、当前root/HTTP/Schema、System Audit与Owner Session/router/pages、既有fixture和项目锁文件；未写产品、后端、架构/计划/台账/锁或执行Git/Go/Node/browser/资源。有限43项来源指纹存于 `/workspace/scratch/project-owner-audit-ui-spec/inputs01.json`，SHA-256 `52fe9c706a21300cb35dd3a67fde2039bfe198bf243c5b770a4f2255b5a480dd`；它是规格来源，不是未来完整运行依赖图。初读时活动Owner README未作为冻结输入消费，随后root明确通知完整24与上述提交接受，历史inputs01的pending标签不回写。

STATIC自查/freeze/差量及原件放同一任务scratch。作者STOP后交未参与设计的verification_runtime独审；任何修订保旧版本。前卡最终24接受门槛已满足，本卡通过后仍须共享文件唯一移交和另授实施/资源；无未决产品含义时不新增确认流程，也不从规格批准推定真实资源授权。
