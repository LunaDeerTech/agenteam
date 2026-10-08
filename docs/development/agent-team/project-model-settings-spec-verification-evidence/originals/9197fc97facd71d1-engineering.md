# Project Owner 模型设置 — T1–T3 静态工程补充 rev1

只补原 rev0 `8b78072a…` 与独审 `463cd09a…` 的工程项；原件不改。root已采纳29路径/17操作、无用户产品待决。本目录是建议规格和协议，未成为正式卡、未安装、未运行。Audit当前17产品源已停止，但整卡及共享源正式handover仍pending；本次后继仅Audit JS测试修订，不把它当新产品接受。

## T1：17 typed API/action、当前Session与后缀

两新API文件保持原写域#2/#3。下列签名为新的精确建议；ID/版本仍string以兼容既有风格，但捕获时严格UUIDv7/精确十进制校验，不代表任意string合法。`WriteOptions`原样取`api/account.ts:81`，仅Session私有owner注入CSRF/key/signal。读取query恰cursor?/limit?，limit验证1..100；UI可选25/50/100不缩窄HTTP typed API。无provider_id、任意path/header/scope/actor或caller cap。

```ts
type ProjectChatProtocol = 'openai-chat-completions' | 'anthropic-messages';
type ProjectModelPageQuery = Readonly<{ cursor?: string; limit?: number }>;
type ProjectModelPage<T> = Readonly<{ items: readonly T[]; next_cursor: string | null }>;
type EmptyObject = Readonly<Record<string, never>>;
type ProjectProviderWriteInput = Readonly<{
  name: string; protocol: ProjectChatProtocol; base_url: string;
  enabled: boolean; credential_ref: string | null; options: EmptyObject;
}>;
type ProjectModelWriteInput = Readonly<{
  name: string; provider_model_id: string; type: 'chat'; enabled: boolean;
  parameters: EmptyObject; request_overwrite: EmptyObject; header_overwrite: EmptyObject;
  capabilities: ProjectChatWriteCapabilities;
}>;
type ProjectModelWriteContext = Readonly<{ provider_id: string; protocol: ProjectChatProtocol }>;
type ProjectModelWriteTarget = ProjectModelWriteContext & Readonly<{ id: string }>;
type ProjectConfigurationKind = 'provider.create' | 'provider.update' | 'provider.delete'
  | 'model.create' | 'model.update' | 'model.delete';
type ProjectConfigurationReceipt<K extends ProjectConfigurationKind = ProjectConfigurationKind> =
  Readonly<{ kind: K; resource_id: string; version: string; affected_references: '0' }>;
type ProjectConfigurationCommand =
  | Readonly<{ kind: 'provider.create'; input: ProjectProviderWriteInput }>
  | Readonly<{ kind: 'provider.update'; id: string; expected_version: string; input: ProjectProviderWriteInput }>
  | Readonly<{ kind: 'provider.delete'; id: string; expected_version: string }>
  | (ProjectModelWriteContext & Readonly<{ kind: 'model.create'; input: ProjectModelWriteInput }>)
  | (ProjectModelWriteTarget & Readonly<{ kind: 'model.update'; expected_version: string; input: ProjectModelWriteInput }>)
  | Readonly<{ kind: 'model.delete'; id: string; expected_version: string; replacement: string | null }>;
type ProjectConfigurationObservation = Readonly<{ found: false; receipt: null }>
  | Readonly<{ found: true; receipt: ProjectConfigurationReceipt }>;
type ProjectCredentialLookupTarget = Readonly<{ kind: 'create' }>
  | Readonly<{ kind: 'update' | 'delete'; credential_id: string; expected_version: string }>;
type ProjectCredentialMetadata = Readonly<{ credential_id: string; purpose: 'model'; version: string }>;
type ProjectCredentialCreated = ProjectCredentialMetadata & Readonly<{ version: '1'; deleted: false }>;
type ProjectCredentialUpdated = ProjectCredentialMetadata & Readonly<{ deleted: false }>;
type ProjectCredentialDeleted = ProjectCredentialMetadata & Readonly<{ deleted: true }>;
type ProjectCredentialObservation = Readonly<{ observed: false; result: null }>
  | Readonly<{ observed: true; result: ProjectCredentialCreated | ProjectCredentialUpdated | ProjectCredentialDeleted }>;
interface ProjectModelsAPI {
  listProviders(projectID: string, query: ProjectModelPageQuery, signal: AbortSignal): Promise<ProjectModelPage<ProjectProvider>>;
  getProvider(projectID: string, providerID: string, signal: AbortSignal): Promise<ProjectProvider>;
  listModels(projectID: string, query: ProjectModelPageQuery, signal: AbortSignal): Promise<ProjectModelPage<ProjectModel>>;
  getModel(projectID: string, modelID: string, signal: AbortSignal): Promise<ProjectModel>;
  listAvailableChatModels(projectID: string, query: ProjectModelPageQuery, signal: AbortSignal): Promise<ProjectModelPage<ProjectAvailableChatModel>>;
  createProvider(projectID: string, input: ProjectProviderWriteInput, options: WriteOptions): Promise<ProjectConfigurationReceipt<'provider.create'>>;
  updateProvider(projectID: string, providerID: string, expectedVersion: string, input: ProjectProviderWriteInput, options: WriteOptions): Promise<ProjectConfigurationReceipt<'provider.update'>>;
  deleteProvider(projectID: string, providerID: string, expectedVersion: string, options: WriteOptions): Promise<ProjectConfigurationReceipt<'provider.delete'>>;
  createModel(projectID: string, context: ProjectModelWriteContext, input: ProjectModelWriteInput, options: WriteOptions): Promise<ProjectConfigurationReceipt<'model.create'>>;
  updateModel(projectID: string, target: ProjectModelWriteTarget, expectedVersion: string, input: ProjectModelWriteInput, options: WriteOptions): Promise<ProjectConfigurationReceipt<'model.update'>>;
  deleteModel(projectID: string, modelID: string, expectedVersion: string, replacement: string | null, options: WriteOptions): Promise<ProjectConfigurationReceipt<'model.delete'>>;
  lookupConfiguration(projectID: string, command: ProjectConfigurationCommand, options: WriteOptions): Promise<ProjectConfigurationObservation>;
}
interface ProjectModelCredentialsAPI {
  getCredentialMetadata(projectID: string, credentialID: string, signal: AbortSignal): Promise<ProjectCredentialMetadata>;
  createCredential(projectID: string, value: string, options: WriteOptions): Promise<ProjectCredentialCreated>;
  updateCredential(projectID: string, credentialID: string, expectedVersion: string, value: string, options: WriteOptions): Promise<ProjectCredentialUpdated>;
  deleteCredential(projectID: string, credentialID: string, expectedVersion: string, options: WriteOptions): Promise<ProjectCredentialDeleted>;
  lookupCredential(projectID: string, command: ProjectCredentialLookupTarget, options: WriteOptions): Promise<ProjectCredentialObservation>;
}
type ProjectModelSettingsAPI = ProjectModelsAPI & ProjectModelCredentialsAPI;
```

`ProjectProvider`、`ProjectModel`、七字段`ProjectAvailableChatModel`和只读Capabilities使用rev0§3全DTO；不得alias成System DTO或以上较窄WriteInput。`ProjectChatWriteCapabilities`为原十字段、四bool、modalities/structured闭集、reasoning_efforts恰readonly []、两容量原nullable string；协议相关policy在捕获的`context.protocol`检查。读取的合法非空options/overwrite/efforts继续完整接受并只读。此补充不改变rev0§3/4任何字节/policy边界。

Context/Target中的protocol/provider_id是从原正式Provider/Model观察捕获的本地校验背景；create HTTP仅发provider_id+input，update HTTP仅发expected_version+input。protocol从不额外上网，update不改变provider_id/type；原重放不用新Get查现Provider。Provider编辑保原protocol，正式后端仍判不可变；无新增Provider Get前置。delete不用Context强迫无关Provider读取，replacement必有null或明确候选；有reference仍DependencyUnbound。

lookupConfiguration接收已捕获六kind完整Command以核receipt目标/版本，但HTTP体只发`{command: command.kind}`；Credential lookup只发上述闭集target且绝不发value。found/observed结果都是历史观察，不能严格确认输入或材料。API不会借lookup做额外Get、自动Execute或新key。

所有路径以下列相对`/api/v1/projects/{project_id}`定义，均预期200 application/json；每行唯一transport分类和Session Action，不折叠进System或Owner64KiB分支。

| # | typed方法 | Session action | client endpoint key | HTTP与相对path |
| --- | --- | --- | --- | --- |
| 1 | `listProviders` | `project-model-provider-list` | `listProjectModelProviders` | GET `/model-providers` |
| 2 | `getProvider` | `project-model-provider-get` | `getProjectModelProvider` | GET `/model-providers/{provider_id}` |
| 3 | `listModels` | `project-model-list` | `listProjectModels` | GET `/models` |
| 4 | `getModel` | `project-model-get` | `getProjectModel` | GET `/models/{model_id}` |
| 5 | `listAvailableChatModels` | `project-model-available-list` | `listProjectAvailableChatModels` | GET `/available-chat-models` |
| 6 | `createProvider` | `project-model-provider-create` | `createProjectModelProvider` | POST `/model-providers` |
| 7 | `updateProvider` | `project-model-provider-update` | `updateProjectModelProvider` | PUT `/model-providers/{provider_id}` |
| 8 | `deleteProvider` | `project-model-provider-delete` | `deleteProjectModelProvider` | DELETE `/model-providers/{provider_id}` |
| 9 | `createModel` | `project-model-create` | `createProjectModel` | POST `/models` |
| 10 | `updateModel` | `project-model-update` | `updateProjectModel` | PUT `/models/{model_id}` |
| 11 | `deleteModel` | `project-model-delete` | `deleteProjectModel` | DELETE `/models/{model_id}` |
| 12 | `lookupConfiguration` | `project-model-configuration-lookup` | `lookupProjectModelConfiguration` | POST `/model-commands/lookup` |
| 13 | `getCredentialMetadata` | `project-model-credential-get` | `getProjectModelCredentialMetadata` | GET `/model-credentials/{credential_id}` |
| 14 | `createCredential` | `project-model-credential-create` | `createProjectModelCredential` | POST `/model-credentials` |
| 15 | `updateCredential` | `project-model-credential-update` | `updateProjectModelCredential` | PUT `/model-credentials/{credential_id}` |
| 16 | `deleteCredential` | `project-model-credential-delete` | `deleteProjectModelCredential` | DELETE `/model-credentials/{credential_id}` |
| 17 | `lookupCredential` | `project-model-credential-lookup` | `lookupProjectModelCredential` | POST `/model-credential-commands/lookup` |

前5成功cap8MiB，余12成功cap1KiB；Problem统一原600000B。6配置写+配置lookup请求1MiB；Credential create/update400KiB，delete/lookup1KiB，decoded65536B；六GET不发key/CSRF。9mutation+2POST lookup共11调用使用原WriteOptions。DTO、capture与body一次冻结，所有HTTP前无await；发布要真实EOF、严格UTF8/JSON/全对象验证。cap只管准入，不当RSS保证。

当前`createSessionController`（useSession:475–489）有14个默认依赖，位置按次序为：1 api、2 systemAPI、3 invitationAPI、4 providerAPI、5 modelAPI、6 selectionAPI、7 accountSecurityAPI、8 smtpAPI、9 smtpDeliveryAPI、10 outboundAPI、11 auditAPI、12 runtimeInformationAPI、13 projectAPI、14 projectAuditAPI。建议仅追加：

```ts
projectModelSettingsAPI: ProjectModelSettingsAPI = createProjectModelSettingsAPI(), // 15
```

组合factory放#2 `project-models.ts`，仅组合自己的12方法factory与#3凭据5方法factory；默认同一个可选Fetch注入用于受控测试，不增加第三API路径，不导出System callback。旧0..14参数、省略调用、System Selection第6参数内的Summary组合保持。Session facade建议`auth.projectModelSettings`：相同17方法名，但读方法去除signal，9写去除WriteOptions，两个lookup改无参、只消费本域原intent；显式retryOriginal复用原9写之一的action，不新增第18HTTP。`abandonReads`/`abandonPending`为本域封闭控制，不接受callback/URL/key。

Action union为表内17个literal。建议按action持17个revision计数，以6读/6配置写+lookup/3凭据写+lookup分组；普通读取消只退休六读，不清三类写intent或旧域。runAuthorized:1288–1338的revision/current分派及:1397失败分派均显式接本17种，当前authenticated Human+完整user/session/epoch/op/revision，绝不落旧admin默认。6GET的CSRF_FAILED不触发身份门禁；9写和2POST lookup的有效current CSRF拒绝按私有CSRF失效语义；其余局部权限错误不设System denied。same-identity checking只隐藏观察和暂停新提交，保原写材料；真正identity/CSRF变化清本域原材料。实际finally才释放owner。

App唯一创建/provide/dispose新controller；:109现有9个confirmLeave及selection所持双intent不替换。新confirm接同一聚合流程，逐域保留明确选择，不能顺带丢Owner/System/Summary。router/auth只在其现有私有导航注册域添加新controller的闭合confirm；workspace只读currentReadContext/detail，绝不新增workspace写方法。

两个后缀是`/settings/model-providers`、`/settings/available-models`。`projectRoute`返回suffix union（auth.ts:41与:54）及同一个raw regexp（:46）同时加两literal；原`[%\\?#]`拒绝、用户名/保留根/大小写规范不变，不开任意settings子树。router/index现Project Settings children仅新增`model-providers`（name project-model-providers）和`available-models`（name project-available-models），settings默认redirect仍general。ProjectNav仅把同username/同project两suffix加入设置current条件，href仍general；ProjectSettings新增“模型与Provider”组两个leaf，general与Audit原组不删。authentication.spec增加两合法后缀及`/`、child、query、hash、percent、backslash反例；project-workspace菜单期待最小兼容，原Owner导航/恢复不改。

## T2：精确旧回归候选

固定14个受影响旧top，不以旧输入PASS代替。各一轮精确`^名称$`；不合并大regex。列表是源码已存在的static selector，不是当前binary discovery或运行许可。其准确命令/asset分类见selectors.json。

| 组 | 精确selector | tests/account来源 | 资产 | 必要理由 |
| --- | --- | --- | --- | --- |
| auth-lifecycle | `^TestAccountAuthenticationWebSessionLifecycle$` | `authentication_web_test.go:11` | global | 原bootstrap/Login/Session/Logout及两条正式审计事实；第15依赖不能改变省略调用。 |
| auth-revocation | `^TestAccountAuthenticationWebRevocationAndExpiry$` | `authentication_web_test.go:51` | global | 原revocation与expiry两subcase；失效后隐藏受保护内容，两个browser各45s仍共用原120s top。 |
| owner-edit | `^TestAccountProjectOwnerWebEditAndRename$` | `project_owner_web_test.go:46` | owner-private | 原更新、版本冲突、rename与dirty-navigation；两后缀和App新确认不能损坏基本资料。 |
| owner-recovery | `^TestAccountProjectOwnerWebOriginalRecovery$` | `project_owner_web_test.go:50` | owner-private | Owner三态lookup/原PATCH与新Model两类历史观察区分，保原key/body与local-abandon。 |
| owner-identity | `^TestAccountProjectOwnerWebIdentityAndOwnership$` | `project_owner_web_test.go:54` | owner-private | 同Session checking、换Session/Owner、late-read、aggregate与正式Logout。 |
| audit-authority | `^TestAccountProjectOwnerAuditWebAuthorityAndRecovery$` | `project_owner_audit_web_test.go:151` | audit-private | 旧独立Audit action/revision及actual body/cancel尾部与新域单owner互不混淆；依Audit最终accepted版本重绑。 |
| audit-navigation | `^TestAccountProjectOwnerAuditWebNavigationAndLayouts$` | `project_owner_audit_web_test.go:154` | audit-private | 原settings-current/default-general/raw-return/focus与既有写草稿guard；两新leaf不能改Audit与ProjectNav。 |
| provider-recovery | `^TestAccountSystemProvidersWebOutcomeRecovery$` | `system_providers_web_test.go:37` | global | 现top同时有credential_replayed/provider_replayed/historical_only/confirmed_read_failure，足够作旧System凭据＋Provider恢复代表。 |
| model-recovery | `^TestAccountSystemModelsWebOutcomeRecovery$` | `system_models_web_test.go:41` | global | 原create/update/delete重放、历史lookup与删后原请求，保System DTO及容量分类。 |
| selection-recovery | `^TestAccountSystemModelSelectionWebOutcomeRecovery$` | `system_model_selection_web_test.go:40` | global | accepted-cut、lookup-only、old404、same-original；新的Project配置不得误用selector command。 |
| summary-recovery | `^TestAccountSystemMeetingSummaryWebRecovery$` | `system_meeting_summary_web_test.go:30` | summary-private | 真实双intent/cross-key/local-abandon保持，防新域清Summary或旧Selection未决材料。 |
| summary-authority-navigation | `^TestAccountSystemMeetingSummaryWebAuthorityNavigation$` | `system_meeting_summary_web_test.go:33` | summary-private | 现有aggregate/checking/new-session/current403/logout/owner/focus，覆盖App和旧两个intent的聚合确认。 |
| personal-theme | `^TestAccountPersonalSettingsWebThemeAndNavigation$` | `personal_settings_web_test.go:43` | global-via-auth | App theme preview/cancel、dirty-menu/back/logout与同Session checking的最小旧personal代表。 |
| system-audit-authority | `^TestAccountSystemAuditWebAuthorityAndOwnership$` | `system_audit_web_test.go:41` | global | 旧admin只读分支代表：ordinary-forbidden、两GET、cancel-join/cross-domain/logout/late-isolated；防新Project分支吞旧System默认分类。 |

旧System Provider Outcome已含credential与Provider原请求恢复，不默认再跑其整6组；Model/Selection同取恢复代表。Summary Recovery保双intent、AuthorityNavigation保aggregate，不能只留一个。Owner新增OriginalRecovery是为新恢复分类不能吞旧committed三态。SystemAudit取一个只读admin/actual-tail代表；Runtime专用分支、leaf和导航若最终未改，不另加其两top；发生实际相关diff时，selectors.json已有精确候选及触发理由，须回报后重绑定。全旧pure check仍按rev0执行。

Owner三轮用`AGENTEAM_PROJECT_OWNER_WEB_DIST`；Audit两轮用`AGENTEAM_PROJECT_AUDIT_WEB_DIST`；Summary两轮用`AGENTEAM_MEETING_SUMMARY_WEB_DIST`。其余7轮读旧global dist（personal经authentication fixture）。新6轮只私有dist。future root须让这些asset reader使用同一个已审新build；global唯一备份/交换/最后reader退休恢复另授，不能从ENV存在推断私有支持。独立driver若仍逐组校验global/private相等，也算global reader。

## T3：私有IPC与轮次、预算

`protocol.draft.json`是有版本的最小协议建议，不是控制实现。17业务端点之外仅对exact当前Session GET做private hold，Session body不得留证。IPC闭集counts/arm/control-state/release/snapshot/logout/archive-recovery-project/reference-fact/rename-reuse；无任意method/path/header/SQL/eval。arm必须精确P/合法known target/operation/query，恰一次下一匹配；每个真实handler另有request_token，release必须arm+request双token，响应body完成/释放信号均不能冒充实际handler join。采用此前D1接受的outer-handler终局界，不重开任何join停工。

writes的丢/截控制只能在完整正式安全receipt已接收后作用到browser，真实Tx事实另从same-root snapshot验，原body/key等值仅在私有内存比较、公开布尔/长度。configuration与Credential各自lookup观察后，仍必须显式原Execute；反向archived两分支保留。reference-fact及aux active→archived只用于精确私有事实，带fixture_only，不称Agent adapter、生产归档或Skills绑定。协议未把401后静态App路径算实际DOM成功。

安全取证始终区分：已分发HTTP、完整上游body、browser完整EOF、typed client成功、schema成功、Tx提交、outer-handler实际join。原始safe响应先闭集合格再存body与SHA；Credential材料、请求body/key及其digest、Cookie/CSRF、Session响应永不留证，异常echo不先落盘。44上游sidecar不自动等于44浏览器EOF的既有边界必须延续。新6组每组finish都要求该组实际相关response的schema及public-client检查，未到finish保持未观察。

schema绑定两既有JSON及其实际本地refs；Node链只导入最终新spec→实际新增API→client/纯标量依赖和实际config/runtime；Go链沿已接受实际graph加本两新源和真实import差量。Python/jsonschema按实际导入闭包，browser/dist用最终私有构建manifest。source17基线、schema哈希和工具包均在执行前原位固定，绝不复制Audit/System全图或使用历史输入数。旧helpers/TestMain/scripts只读，不授重构。

六新top每轮1case、workers1/retries0、browser45s含同body检查；Go top120s含cleanup，建议准备35+browser45+终局事实10+实际cleanup30秒。总包原6m，TCP尾75s，fresh5GiB；每轮4容器/3网络共7ID，direct/adopted实际wait、watchdog实际join，两次exact absent/owned与runtime/browser-runtime空。14旧轮原budget/内部subcase保持；AuthRevocation两case共用同120s，不能倍增。IPC单ack8s计入45s，不加额外case时间。预算耗尽只能FAIL/完整退休后STOP，不能先pass后清理或自动续轮。

每轮唯一root资源/Go-cache/asset窗口，阶段不能并行占用；候选总顺序为6新top→14必要旧top（每轮单独冻结/退役），失败停止，无自动重跑。独立至少再2轮：不同构造的配置＋Credential Unknown/原Execute组合，以及current权限＋归档两恢复路径组合；其selector/source尚需独立作者冻结，不借作者结果预填。外层可执行driver/watchdog/完整input gate与每轮grant是执行前固定件，当前没有可执行true模板或授权。

## 仍待交接的边界

已静态补齐17建议签名/action、第15依赖、两个闭集后缀、14实际旧selector、6新top逐轮预算及private IPC设计。保留T1的Audit整卡/共享17源正式handover、T3实施前私有material/snapshot精确DTO和双作者protocol agreement/真实闭包、T4正式卡路径与基线/唯一writer/README末件。以上均为工程冻结事项，无新用户产品待决。29原路径不变；本稿不许可其它源/协议写入。三硬停与Jina边界、生产调用/平台selector/Summary override/Invocation/生命周期/E01保持。**STATIC DRAFT STOP。**
