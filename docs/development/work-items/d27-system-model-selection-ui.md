# D27：System 平台模型用途配置 UI

状态：rev2，2026-10-06 已获 `recovery_documentation`（verification_worker）独立静审通过（STATIC PASS）并由主线程采纳本规格，被审稿 SHA256 `902195a51e9a5ff6f91f829ae6dc13e7feba7a7c9aaaa0c4cb77541a19b67f58`。rev1经 `recovery_documentation`（verification_worker）独立静审为STATIC BLOCKED，仅“当前引用只有Model ID，但原完整Model解析需要Provider/protocol上下文”一项阻断；被审稿 SHA256 `3166c87c86f2f66bac63301a29f237ab7c885f1cdc693f74defefc83afad1f78`。rev2只补私有两GET组合补读、实际owner/发布门禁及对应验收；本次仅同步页首实施前置，技术§1–7保持被审原字节及原采纳语义。[Model UI rev2](d27-system-model-management-ui.md)的26路径完整结果已获独立最终PASS、资源双清且README最后完成，主线程接受提交 `bc17167c42ee5d5fc1427adaac099ff888ca959b`并推送main、远端一致；Model前置已满足，本卡固定产品/前端基线采用该接受提交，沿已接受后端及迁移1–19。实际范围按原§6约定为原27候选减第2，共26路径，原表与序号不改：`web/src/api/system-models.ts`剔除，已接受的 `system-providers.ts` 已公开 `parseProviderModel` 与所需类型，可直接复用而无需重导出。原第24–26均需实施，固定基线中旧邀请browser第1528行、Provider browser第1192行、Model browser第1923及1991行，共四处全部navigation/Drawer链接计数仅作 `toHaveCount(4)`→`toHaveCount(5)`，原其余动作/强断言/预算保持。正文中的待验与原27候选表述保留原审稿状态，当前前置与实际范围以本页首为准。拟由 `directory_backend`（frontend_worker）担任上述26路径唯一作者，待本卡提交后由主线程正式下发，不得自行启动；其余后端、迁移、共享基础与锁文件不授，真实资源窗口另授。architecture_worker仅修改本卡页首，不碰其他文档、产品或资源；规格采纳和上游接受不代表本卡产品通过。

## 1. 完整结果与真实依赖

系统管理员在“模型与 Provider → 平台模型用途”读取当前四用途引用，显式分页选择合法System Model，整组保存或取消，并处理当前权限、版本冲突与结果未确认。只维护已有Model的稳定ID，不在此创建/编辑Model、Provider或Credential，不增加Project override。

| 依赖 | 证据与门槛 |
| --- | --- |
| 当前正式配置服务/HTTP/根 | **已满足**，[B01-K](d09-b01-system-configuration.md) `543511c`、[HTTP验收](../agent-team/d09-system-model-http-verification.md) `ac5b4c6`、[根验收](../agent-team/system-model-root-verification.md) `457b197`均在固定产品内。消费singleton GET/PUT、原命令lookup、Provider/Model读取与当前管理员同事务裁决，不改后端。 |
| Account、系统壳、Provider UI | **已满足**，固定 `f465f45` 已接受Provider完整结果，包含Account/邀请、SettingsShell多叶子、严格Provider/Model只读DTO、原预算、同Cookie owner及[共享模态恢复](d27-modal-focus-restoration.md)。Provider材料与两步命令保持原边界。 |
| Model管理UI | **尚未满足产品门槛**，仅消费 `611a552` 已定契约：Model GET/严格解析导出、第五API依赖与独立域、四叶子菜单/第八return、候选分页、App期controller与View末尾cohost。需最终独立接受后逐项核实际导出、签名和旧菜单断言；不能复制或先绑定活动实现。 |
| 既定产品规则 | [系统设置§3](../../frontend-design/layouts/system-settings.md#3-模型与-provider)、[通用设置](../../frontend-design/layouts/settings-shell.md)、[样式](../../frontend-design/styles/README.md)、[组件](../frontend/components.md)、[Model Configuration](../../architecture/platform-infrastructure/model-system/model-configuration.md)、[D27计划](../development-plan.md#d27-业务页面与设置)。选择器仅管理引用，参数与能力属于Model管理。 |
| 尚未绑定的消费面 | [Model Resolution](../../architecture/platform-infrastructure/model-system/model-resolution.md)、[Resolution验收](../agent-team/current-model-resolution-verification.md)、[Knowledge Indexing §3.5](../../architecture/knowledge-memory/knowledge-indexing.md#35-embedding_model_ref)。当前受限Resolution库不反向缩减合法selector配置；生产Runtime、索引重建/serving生效投影及Model事件消费者不属于本卡。 |

没有新的待决产品含义。保存只确认“平台模型用途配置已保存”，不表示模型可调用、索引已重建或检索已切换；Embedding事件持久化不等于有消费者处理。页面不造生效进度、连接测试或Runtime成功状态。这些消费面、非空Model配置、外域引用adapter及完整D09/D27仍未交付；Summary待决、Object/tools原停止任务、ready503和生产SPA/真实Vite代理边界保持，不重建停止任务。

## 2. 页面、当前引用与显式选择

仅新增懒加载 `/system/model-selection`，继承authentication/protected/systemAdmin；在既有 `models-providers` 组中置于Models之后，形成第五个系统叶子，`/system`仍默认用户。登录return闭集由Model的八项增为九项，只接收本精确路径；query/hash、数组、动态子路径和外部地址仍拒绝。普通用户无入口，直链不挂载本页面、不发管理请求。

页面显示配置版本及Embedding、Memory、Reranker、Image Generation四个用途的当前绑定、安全名称、原生模型ID、type与所属Provider/启用状态。GET的 `configured:null` 表示尚未配置；正式初始化为稳定singleton ID/version="1"，不能猜默认Model、伪造合法的四项空配置或由UI生成singleton身份。提供“配置/编辑用途”、刷新和已有Models/Providers叶子的正常导航，不为未交付功能造入口。

已配置状态只有Model ID。每轮当前引用补读按distinct Model ID去重后至多4次调用§3组合operation；每次最多1个Model GET和1个Provider GET，整轮最多4+4，不要求跨operation的Provider缓存/去重。直接复用该operation返回的Provider，不再为同项补发Provider GET；按同owner串行推进，无全域扫描、不将分次读取称为原子快照。某项补读失败/404与其保存引用分别显示，不改成未配置；Provider disabled、Model disabled或Memory能力后来撤除均保留原绑定，标明当前不可选，不自动清空/替换，不因能力失效拒绝整个合法selector DTO。身份失效则按§5清除旧数据。

使用一个业务 `UiDialog` 编辑完整四用途草稿；每个用途有明确选择入口，可选用途另有明确“不配置”。在同一编辑器内按当前用途展示Provider分页→选中Provider的Model分页，提供返回Provider列表、上一页、下一页和刷新；不新增搜索Dialog、远端搜索或嵌套通用选择框架。不同用途的已选ID独立保留，只读取当前主动浏览的用途，不预抓其余用途候选。候选字段为名称、原生ID、type、所属Provider及可选/不可选原因，不编辑参数、capabilities、Credential或任意UUID。

| 用途 | 本卡候选规则 |
| --- | --- |
| Embedding | 必填，System embedding Model，Model与Provider均enabled。 |
| Memory | 必填，System chat Model，Model与Provider均enabled，`structured_output_modes`含`json_schema`。 |
| Reranker | 可明确不配置；非null时为System reranker Model，Model与Provider均enabled。 |
| Image Generation | 可明确不配置；非null时为System image_generation Model，Model与Provider均enabled。 |

规则来自固定 `references.go:selectionCompatible/prepareSelectionModels`。Memory不附加Resolver当前OpenAI/text/无tools等更窄运行条件，不将capability声明当作实际运行验证。已知不合法候选禁用并说明原因；当前Provider/页无合法候选不等于全域不存在。无Provider或无Model时引导到已有管理叶子，离页仍经过草稿确认，不在本页嵌入创建流程。

首次保存必须同时选择合法Embedding和Memory，可选两项明确null；已配置编辑以一次完整PUT保存全部四项，不逐用途PATCH。当前引用详情尚未读取成功或已知不可选时，不能以猜测状态生成新的保存命令，用户可重新读取或明确更换该项。浏览候选不自行改变草稿；没有修改不派发无变化PUT。保存中锁定同一草稿防重复，失败保留输入；取消回到已保存观察，保存后的严格receipt与当前读取结果分开显示。

成功反馈沿按钮2400ms与可访问播报。长名称/原生ID、错误可换行，必要样式限本页与编辑器；窄屏单列、候选行堆叠，无页面级横向溢出。沿已验Dialog/Drawer、Escape/遮罩、键盘、焦点与reduced-motion规则，不修改公共Ui或设置壳。

## 3. 封闭API、DTO与预算

新增 `SystemModelSelectionAPI`，工厂及只读类型位于 `web/src/api/system-model-selection.ts`。复用Model最终接受的Provider/Model纯解析及读取能力；必要时仅从 `system-models.ts` 窄导出/重导出已有严格解析与只读类型，不扩大原Model/Provider命令闭集，不将selector receipt交给限定其他kind的解析器。正式wire见[HTTP规格](recovery-d09-system-model-http.md#4-路由严格-dto-与响应)和[OpenAPI](../../../api/openapi/model-system.json)。

| 方法 | 唯一路径、输入与200结果 |
| --- | --- |
| getSelection | GET `/api/v1/system/model-selection`，无query/body，返回SelectionState。 |
| updateSelection | PUT同路径，body恰 `{id,expected_version,embedding,memory,reranker,image}`，返回selection receipt。 |
| lookupSelectionCommand | POST `/api/v1/system/model-commands/lookup`，body仅 `{command:"model.selection.update"}`，原key与当前Session CSRF，返回 `{found,receipt}`。 |
| listProviders | GET `/api/v1/system/model-providers?limit=25[&cursor=…]`，沿已接受ProviderPage。 |
| getProvider | GET `/api/v1/system/model-providers/{id}`，目标ID精确匹配。 |
| listModels | GET `/api/v1/system/models?provider_id=…&limit=25[&cursor=…]`，provider_id必填，沿已接受完整ModelPage。 |
| getSavedModel | 一个组合operation：GET `/api/v1/system/models/{id}` 后，按私有安全ID读取 GET `/api/v1/system/model-providers/{provider_id}`；严格解析后一次返回不可变 `{model,provider}`，不公开中间Model raw。两条均为既有固定GET路径，无query/body。 |

本卡第七方法的公开签名固定为：

```ts
getSavedModel(modelID: string, signal: AbortSignal):
  Promise<Readonly<{ model: ProviderModel; provider: Provider }>>;
```

操作内部先验证输入ID并沿受限getModel传输读取有界JSON。中间raw仅保存在本次调用的私有局部：先核非null对象及Model顶层恰六字段 `id,provider_id,input,version,created_at,updated_at`，仅安全提取canonical UUIDv7的id/provider_id并核id等于捕获目标；未完整校验的其余字段不能成为Model候选。以该已核provider_id构造精确Provider GET，完整解析Provider并核其ID，再以该Provider的真实ID/protocol调用原严格Model解析器，完整校验type/protocol/capabilities、字段和时间，重核Model ID/归属，最后一起返回不可变model+provider。候选已有Provider/protocol时仍沿原listModels/getModel读取/解析契约，不增加ID-only原Model API签名或削弱其校验。内部可以复用固定getModel传输，但不得增加公开返回raw的读取方法。

同一次selection read owner覆盖整个组合operation，两GET使用同一signal与原30秒预算，不中途释放/重新获取owner、不为第二GET重开时限。相关identity/页面代次失效须取消该signal；第一段实际读取完成且signal仍有效才发第二段，第二段完成及最终完整解析后仍须核取消和发布代次。任一阶段失败/abort返回零组合结果；首段失败/取消零Provider GET，任何失效后不续发该operation。中间raw、未完整校验字段与半成品Provider均不进入reactive状态、公开响应候选、草稿/新PUT、日志或错误正文；只有完整结果通过当前identity/页面门禁才一次发布。失败/取消时放弃私有中间数据，fetch/body/read/cancel全部实际join后才归还owner，不以首段HTTP已结束当作整次完成。

client只增加必要固定GET/PUT与selection lookup分类；原getModel传输与API校验不放宽。GET不附body/key/CSRF，PUT和POST lookup使用owner中的原key、当前CSRF及signal；动态ID、query、body/options均闭合，不允许页面传任意URL、method/header或限额。无页面HEAD、Model/Provider写、deletion-impact、Secret或调用端口。

SelectionState恰 `id,version,configured`，全部required；ID为canonical UUIDv7，version为正int64 canonical十进制字符串。configured必须显式null或恰含 `embedding,memory,reranker,image` 的对象：前两项必填UUID，后两项必填UUID或null；缺字段、未知字段、错误类型全拒，不能用truthy/default折叠未配置与可选空值。Provider/Model/Page逐字段完整验证、严格Instant、Unicode、排序、25项/重复ID与nullable cursor沿上游，不只解析可见列；只有候选选择再应用§2用途约束。

PUT六字段均必填，id/expected_version来自编辑时捕获的正式selector观察；可选空值只用null。selector只有一个版本，不增加每用途版本或Model/Provider版本fence。新命令的expected_version须有合法int64后继；输入与序列化UTF-8 JSON≤16KiB在创建intent/key、派发前严校，非法输入零请求。所有本卡新响应和Problem保持600000B；仅复用listProviders成功200的2097152B例外，既有Credential512KiB/Provider32KiB请求例外不改变。实际流超限零候选，并等待read/cancel真实尾部。

selection receipt恰 `kind,resource_id,version,affected_references`：kind只为 `model.selection.update`，resource_id等于原singleton ID，version等于原expected_version+1，affected_references必须为字符串"0"；不借Model删除的可变计数规则。lookup found=false要求receipt:null，true按捕获的原命令核同一receipt，拒错kind/目标/版本/数字代字符串/前导零/溢出。历史receipt可以早于当前selector版本，不能要求它与后续GET版本相等。

## 4. 保存、版本竞态与原命令恢复

沿[Model rev2](d27-system-model-management-ui.md)既定命令结果分类和原请求恢复纪律，选择器新增独立私有intent，固定完整identity、singleton ID、expected_version、四项ID/null、序列化body与唯一key。派发后不能通过编辑某项、刷新或后读null更换原命令；不自动保存、重试、重建key或补偿回滚。未派发本地失败与可确认业务拒绝分别显示，不把所有错误都称未知；已有历史不确定性不能被后续拒绝或lookup found=false抹去。

超时、断连、截断/非法响应、取消或COMMIT_UNKNOWN保持结果未确认；“检查原请求”只显示历史观察，found=true/false及读取失败都不证明本次body接受或未接受。“重试原请求”必须显式调用原PUT，使用同key/id/expected_version/四项body，当前CSRF由同identity安全上下文提供。原请求恢复不以当前selector版本相等、旧Model/Provider仍存在/启用或Memory仍具能力为前提，404/后置失效不挡历史重放；§2候选门禁只限制新命令。只有原Execute返回严格匹配receipt才确认保存；之后GET失败仍保留该次确认，仅提供重读，不再重发该写。放弃只放弃恢复，不撤销可能已经完成的配置。

正常新保存由后端同事务重核管理员、singleton版本、全部目标Model/Provider与反向引用；页面观察不授写权。另一管理员保存、或正式Model DELETE内替代/清空平台引用会推进selector版本，旧草稿不得自动合并、换expected_version覆盖。明确冲突保留输入，用户显式读取最新值并核对后才能开始新intent/key；原不确定intent先保持原请求恢复路径，不能靠当前GET“看起来相同/仍未配置”确认旧写。候选在保存前被禁用、能力变化或删除的拒绝，不得显示部分用途成功或本地提前提交。

只保存ID，不把候选旧Model/Provider版本塞入PUT；同ID配置变化是否仍合法由正式服务当前裁决。引用更新、selector version、成功receipt、typed Audit与配置事件在原事务内成立；Embedding变化的专用事件仅为持久事实，UI不发重建请求、不推测消费者或serving状态。

## 5. 分域owner、分页与确认生命周期

在Model最终接受的五依赖之后追加第六个可选 `SystemModelSelectionAPI`（默认正式工厂）；原调用方及前五项顺序不变。useSession新增明确的selection read/write/lookup分类与独立revision/intent，不能落入personal、Provider或Model默认分支。所有七方法，包括复用的只读方法，都经同一个Cookie owner；controller/View不直接fetch或调用API绕过owner，不新增队列或Cookie通道。

四用途分页状态按purpose/Provider/identity隔离，当前主动页每次最多25项；上一页重读对应cursor，后退丢后续历史，成功后才换页。换用途/Provider后旧读不能发布到新目标；刷新/明确重进回首项，CURSOR_INVALID提示显式回首项，不循环重读。普通读取开始隐藏该分区旧候选，失败零新候选，不擦除四项草稿或已确认保存事实；初次owner忙时未派发加载最多延迟一次，取消/失效后不续发。只有至多四个已保存引用的有界补读允许自动顺序展开，不扫描候选页。

可见30秒预算、取消、放弃和身份切换不提前释放owner；fetch/body/read/cancel实际join、finally结束后才释放，超限尾部同理。各域abandon双向隔离，selector清理不能撤个人/Provider两步/Model/邀请/users owner，反方向也相同；App.dispose/auth.leave原全局清理与旧退出流程保持。selection写和POST lookup纳入当前完整identity+owner generation的CSRF_FAILED失效护栏；当前401处理沿原Cookie纪律，当前403清全部系统私有状态并设置身份绑定拒绝，成功Session重验后再恢复；旧identity/代次错误不污染新身份，未join旧尾部不能被新身份越过。

App创建/provide `createSystemModelSelection`，保存草稿、原intent、确认状态与Promise；View只承载当前DOM。未保存字段、进行中/未确认写属于dirty，单纯浏览未改变草稿的候选不算dirty。关闭编辑、显式重载、菜单/本人入口/退出与浏览器返回经过统一“继续编辑 / 放弃修改”，路由确认先于Session重验；检查/原请求重放不被通用刷新清意图。沿原导航hook与beforeunload生命周期，不改退出协议。

统一放弃确认Dialog固定在 `SystemModelSelectionView` 的编辑业务Dialog之后，App不加常驻确认层。checking卸载RouterView时同一子树全部撤离，但App期草稿/Promise保留；复查失败时原App恢复按钮可用，无残留overlay/inert/滚动锁。同完整identity/admin恢复时同序重挂，确认仍顶层、零新PUT，不自动选答案。真实离页/新Session/失权/注销/dispose清旧状态并以false结束待决确认，旧route/退出续体不能移交新身份。继续/放弃只结算一次，关闭确认使用已验共享fallback；View/Editor所有await后焦点操作须核实例仍挂载与目标当前可用，确认打开时标题不抢焦点，无业务Dialog亦同。不得以手focus、全局选择器或反复重挂修层序。

## 6. 唯一候选路径与移交

以下27路径是规格范围，不是写权。Model最终接受后核实第2的导出缺口及第24–26的真实菜单断言；不需要的候选由主线程在正式移交时剔除，不能为数量改文件。其余新增导出、后端缺口或共享规则变化先报主线程修卡。与Model当前作者共享的App/client/Session/router/菜单/测试/README不得并行实施。

| # | 路径 | 唯一用途 |
| --- | --- | --- |
| 1 | `web/src/api/client.ts` | 固定selection GET/PUT/lookup分类与受限options，原endpoint、预算和实际尾部保持。 |
| 2 | `web/src/api/system-models.ts` | 仅必要窄导出/重导出已接受Model严格纯解析和只读类型；原Model API、命令/receipt规则不变，已有导出则剔除。 |
| 3 | `web/src/api/system-model-selection.ts`（新） | 七项受限API、Selection/receipt/lookup严格解析、完整输入捕获。 |
| 4 | `web/src/composables/useSession.ts` | 第六依赖、selection独立域/intent/恢复、同owner与权限/CSRF接入。 |
| 5 | `web/src/composables/useSystemModelSelection.ts`（新） | App期当前引用/草稿、四用途候选分页、确认与页面/身份代次。 |
| 6 | `web/src/App.vue` | provide、既有导航/顶部退出hook与dispose组合，不增加确认Dialog宿主或改退出协议。 |
| 7 | `web/src/router/index.ts` | 仅新增平台模型用途叶子，原默认用户与页面保持。 |
| 8 | `web/src/router/auth.ts` | 第九项精确return、本域离页前确认/完成hook。 |
| 9 | `web/src/views/system/SystemSettingsView.vue` | Models后追加平台模型用途，五叶子与原分组/权限保持。 |
| 10 | `web/src/views/system/SystemModelSelectionView.vue`（新） | 当前引用、局部布局、编辑器及末尾统一确认宿主、生命周期门禁。 |
| 11 | `web/src/views/system/SystemModelSelectionEditor.vue`（新） | 完整四项草稿与同Dialog内用途/Provider/Model分页选择，不拥有Cookie或key。 |
| 12 | `web/src/tests/system-model-selection-client.spec.ts`（新） | 严格DTO/输入/receipt/lookup、预算与只读解析复用。 |
| 13 | `web/src/tests/system-model-selection-state.spec.ts`（新） | 实际controller+transport的分域/尾部/身份/原请求/版本及分页屏障。 |
| 14 | `web/src/tests/system-model-selection.spec.ts`（新） | 真实App/router/controller、用途规则、cohost恢复、草稿和菜单组合。 |
| 15 | `web/src/tests/system-user-directory.spec.ts` | 仅五叶子菜单兼容；默认用户/权限/读取强断言保持。 |
| 16 | `web/src/tests/system-invitations.spec.ts` | 菜单与共享导航组合兼容，原写/恢复/cohost强断言保持。 |
| 17 | `web/src/tests/system-providers.spec.ts` | 菜单与分域组合兼容，原凭据两步/材料/恢复强断言保持。 |
| 18 | `web/src/tests/system-models.spec.ts` | 菜单与分域组合兼容，原CRUD/impact/替代删除/恢复强断言保持。 |
| 19 | `web/src/tests/personal-settings.spec.ts` | 八项return增九项及新合法目标，原拒绝/草稿/退出保持。 |
| 20 | `tests/account/system_model_selection_web_test.go`（新） | 下节真实顶层、正式配置/命令/版本/引用事务旁证。 |
| 21 | `tests/account/system_model_selection_web_fixture_test.go`（新） | 复用既有正式fixture的任务自有准备、runner与有界响应控制，不改旧fixture。 |
| 22 | `tests/account-captcha-web/system-model-selection.config.js`（新） | 固定Playwright场景/预算及私有输出。 |
| 23 | `tests/account-captcha-web/e2e/system-model-selection.spec.ts`（新） | 生产dist+真实后端的四用途配置/恢复/竞态/权限/布局。 |
| 24 | `tests/account-captcha-web/e2e/system-invitations.spec.ts` | 暂候选：仅旧NavigationAndLayouts的390px Drawer全部link计数4→5；固定f465:1528为3，Model rev2约定改4，须核最终接受源；其余动作/强断言/预算保持。 |
| 25 | `tests/account-captcha-web/e2e/system-providers.spec.ts` | 暂候选：仅旧NavigationAndLayouts起始系统navigation全部link计数4→5；固定f465:1192为3，Model rev2约定改4，须核最终接受源；其余动作/强断言/预算保持。 |
| 26 | `tests/account-captcha-web/e2e/system-models.spec.ts` | 暂候选：仅Model最终接受用例中实际存在的全系统navigation/Drawer四叶子计数4→5；尚无已验产品位置，移交时核定，无此断言则剔除，不新增其他改动用途。 |
| 27 | `docs/development/frontend/README.md` | 产品接受后最后同步用途配置、九项return、实际命令与Runtime/索引生效未交付边界。 |

必读[Vue开发技能](../../../.agents/skills/agenteam-vue-development/SKILL.md)、[Vue测试技能](../../../.agents/skills/vue-testing-best-practices/SKILL.md)；独立验收读[验证技能](../../../.agents/skills/agenteam-verification/SKILL.md)，浏览器使用可用Playwright技能，缺失如实记录。SettingsShell/Ui/useLayer、全局CSS、旧业务API/controller/View、后端/OpenAPI、迁移、旧fixture/driver、脚本、锁和当前归档均只读，以上窄列用途除外。零DDL、零新依赖，不占迁移号，不触tasks/continuation或其他归档。

## 7. 高风险验收与资源门槛

开工须固定Model完整接受提交及实际API导出/五依赖/owner/菜单接缝；不得用变化中的整树替代输入清单。作者执行 `npm run check --prefix web`，只格式化授权文件；Go1.27.1/local，`GOPROXY=off GOSUMDB=off GOFLAGS=-mod=readonly`下精确integration-tag race编译和适用vet，依赖准备单列。纯测、编译与规格通过不冒充浏览器、权限或事务通过。

纯测覆盖Selection三字段及configured四字段闭集/必填null/UUID/version，完整PUT字段和int64后继、16KiB编码预算零intent/dispatch；receipt严格kind/目标/expected+1/零计数，lookup真/假/失败均不确认body。覆盖初始未配置、必填/可选清空、当前绑定后置失效仍保留、Memory缺json_schema、禁用/错type、跨Provider/用途cursor串扰及完整上游DTO/时间反例。实际transport确认只listProviders成功2MiB、其他600000B不变，超限后的read/cancel延迟仍占owner；按未变字节复用上游最大合法Provider页证据，不重造宽限额。

组合补读专例覆盖仅Model ID的正常结果、第一段错误ID/provider_id/顶层形状时零Provider请求，第二段Provider错ID/读取失败以及最终protocol/type/capabilities不兼容时零组合候选、零半成品发布/新PUT。分别保持两段fetch/body/cancel及段间屏障，验证同signal/一次30秒预算/同owner，第一段取消不发第二段、第二段迟到或identity/页面代次变化零发布，实际尾部未join不放行其他域；核每个distinct Model ID最多1+1请求、整轮4+4且不重复补读返回Provider，不依赖跨operation缓存。

以生产controller+受控transport的明确异步屏障核read/PUT/POST lookup与body/cancel忽略abort时的实际join；30秒可见结束、取消、身份变化均不能越过尾部，且无晚到发布/后续自动请求。双向核个人、邀请、Provider两步、Model、users清理不误撤selector owner，selector清理不撤它们；当前及迟到401/403/CSRF、原body/key/expected重放、确认写后GET失败、已知冲突显式核对与历史未确认分列。

页面纯测先打开dirty或未确认的放弃确认，再通过真实App listener合成pageshow并保持Session GET待决；checking零本域模态、失败App恢复按钮可操作、同identity重挂确认仍顶层且零新PUT，继续/放弃各一次。覆盖无底层业务Dialog、待决route/退出及新Session/失权/dispose的false终点，旧View/Editor续体不得抢焦点；按钮交互不以直接结算Promise替代，jsdom状态/DOM不冒充真实焦点。

| 新真实顶层 | 必须覆盖 |
| --- | --- |
| `TestAccountSystemModelSelectionWebLifecycle` | 正式初始化GET为null/v1，正式创建四类Model后经页面首次完整保存；必填不能清空、可选配置与清空、取消零PUT、无变化零PUT、刷新后引用/版本一致。只确认配置保存，不出现调用/重建成功。 |
| `TestAccountSystemModelSelectionWebReadAndPagination` | 至少26个正式Provider与主动目标下26个正式Model；四用途显式选择、上下/返回/刷新、空与坏cursor、跨目标迟到读取不串页、零自动扫页。正式四项引用经实际Model→Provider两GET完成补读，按轮计数最多4+4且无同项的额外Provider补读；自有同源服务器只对精确Provider GET有界失败时，无半成品名称/候选或新PUT，保存引用不伪装null，显式重读后恢复。正式禁用已保存Model/Provider或移除Memory json_schema后，页面保留绑定并显示不可选。 |
| `TestAccountSystemModelSelectionWebConcurrencyAndReferences` | 两个正式管理员/浏览器读同版本后不同key保存，只有合法先提交者成功，后者真实冲突且草稿保留；保存前正式禁用/改能力/删候选，核拒绝零部分用途改写。经正式Model DELETE替代必填或清空可选引用推进selector版本，旧页PUT不能覆盖；SQL仅旁证canonical/引用/receipt/Audit/事件原子事实，不直改业务引用。 |
| `TestAccountSystemModelSelectionWebOutcomeRecovery` | 自有同源服务器在真实PUT接受后有界丢失响应；lookup仅历史观察，显式原PUT重放按原命令身份核同receipt与唯一业务/Audit/event事实。其间正式Model DELETE替代旧引用可使旧Model GET404、当前selector版本推进，仍允许原PUT历史重放且不改回旧引用。确认后GET失败、放弃不回滚、未派发与可确认拒绝/未知状态分别验证；不注入DB提交故障。 |
| `TestAccountSystemModelSelectionWebAuthorityAndIdentity` | 普通用户无入口/直链零管理请求；真实Session撤销、admin失权拒绝GET/PUT/lookup并清旧系统状态，同User新Session/换账号无旧草稿/intent。权限准备只在任务自有精确identity并沿正式fixture纪律，不fulfill假403冒充授权；CSRF接入与实际Cookie尾部有证据。 |
| `TestAccountSystemModelSelectionWebNavigationAndLayouts` | 五叶子/默认用户/第九return，dirty与未确认离页的继续/放弃；确认已打开时合成pageshow，精确Session GET一次失败、App按钮恢复同Session后实际点击/键盘可达确认，关闭后焦点留剩余modal且Tab/Shift+Tab不逃出。light/dark×1440/1024/834/390、长字段、Dialog/Drawer Escape/遮罩、reduced-motion、无溢出；合成事件与自然浏览器恢复区分。 |

Provider/Model、selector与删除竞态全部经已接受正式HTTP或等价正式服务端口准备；不SQL制造平台引用、绑定Runtime或外域adapter。允许只读SQL旁证任务自有事实。固定生产dist与真实backend同源，无外部Provider请求、SMTP新依赖或既有基础设施；保存事件的零Model deliveries边界保持，不把事件存在称为重建已启动。

可按精确名称分组运行 `scripts/test-security.sh -run '^TestAccountSystemModelSelectionWeb(Lifecycle|ReadAndPagination|ConcurrencyAndReferences|OutcomeRecovery|AuthorityAndIdentity|NavigationAndLayouts)$'`；Playwright每test45秒、Go顶层2分钟、workers=1/retries=0、race/count1/每包6分钟保持。按实际耗时划分运行组，不增时、削断言或把no-tests算通过。

必要旧真实回归沿Model卡已定Account SessionLifecycle、个人ThemeAndNavigation、公开IdentityNavigation、用户AuthorityAndIdentity/NavigationAndLayouts、邀请OutcomeRecovery/NavigationAndLayouts、Provider CredentialReplacement/OutcomeRecovery/NavigationAndLayouts，再加Model最终接受的DeletionAndReplacement/OutcomeRecovery/NavigationAndLayouts。使用各自完整正式 `TestAccount…Web…` 名称，开工核固定测试清单，按差量复用未变证据；旧邀请/Provider及实际存在的Model全导航数量只精确4→5，原链接、Tab困陷、Escape/遮罩、焦点、cohost、事务和预算强断言保持。没有对应数量断言的旧browser不为凑数改写。

仅在Model作者/独立验收实际停止、资源清零且完整接受后，由主线程另授本卡独占窗口。作者冻结精确源/dist/锁/环境，保存首红、真实argv/env/退出与安全日志，再交未参与实现的verification_worker独立验证权限/恢复/竞态和浏览器。每轮命令实际wait、server/browser与所属进程join、自有exact-ID双次absent且旧基线不变后交回；不并发共用fixture、不占当前归档。产品独立通过并经主线程采纳后才写第27路径，不能据本卡接受声称Runtime、索引生效或完整D09/D27已完成。
