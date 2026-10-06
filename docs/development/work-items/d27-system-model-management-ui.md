# D27：System Model 管理、删除影响与替代删除

状态：rev1，2026-10-06 已获 `recovery_documentation`（verification_worker）独立静审通过（STATIC PASS），主线程已采纳本规格；被审稿 SHA256 `d56ba2ab3ecca56058fa614d15d716228e8294ed51fd74ab02308f4064aed9f6`。本次仅更新页首采纳及前置等待状态，技术§1–7保持被审原字节；§6仍为24条候选路径，业务实施与真实资源均未授权。固定已接受前端为 `1c82d888adfef0d8b58ab51c920557ca4e2f084f`，后端为 `9b3201547f9b7b61fd9716a6ba6540084961496c`及迁移1–19。正在实施的Provider UI只按提交 `a8f0135` 中[Provider rev3规格](d27-system-provider-management-ui.md)作为待验接缝，未读取其活动源码；必须等待该完整结果最终接受、固定产品提交与资源清零后，复核实际接口并由主线程另授实施。本卡不写其他规格、后端、迁移或当前归档。

## 1. 完整结果、依赖与未交付边界

管理员在系统设置“模型与 Provider → Models”中显式选择一个Provider，分页管理其System Models，查看安全详情，创建/编辑/启用/禁用，并基于真实删除影响选择替代或明确清空允许的用途后确认删除。四种type为chat、embedding、reranker、image_generation；本卡管理其当前已接受配置形状，不发模型调用、不声称配置已经通过连接或运行验证。原Providers叶子的Models子列表仍只读，不跨卡改为另一套管理流程。

| 依赖 | 真实证据与消费边界 |
| --- | --- |
| Account、系统设置与邀请 | **已满足**，前端 `1c82d88` 已接受真实系统壳、用户/邀请、同Cookie owner、身份草稿和第六项return；见[邀请卡](d27-system-invitation-ui.md)、[用户目录报告](../agent-team/system-user-directory-ui-verification.md)。App期状态/Promise、页面末尾确认宿主及共享[模态焦点恢复](d27-modal-focus-restoration.md) `79f922e`均已接受。 |
| Provider UI与共享客户端接缝 | **尚未满足产品门槛**，[Provider rev3](d27-system-provider-management-ui.md)仅规格已采纳、正在实施。需其第四API依赖、Provider/Model严格DTO与解析、固定endpoint及预算、分域owner/个人abandon门禁/CSRF与403清理、模型菜单组、第七项return、cohost实际产品接受。不得把规格或活动源码当通过；开工前核最终差量与测试接缝，缺口交主线程，不提前复制未验实现。 |
| System配置、HTTP与正式根 | **已满足**，[B01-K](d09-b01-system-configuration.md) `543511c`、[HTTP](../agent-team/d09-system-model-http-verification.md) `ac5b4c6`、[正式根](../agent-team/system-model-root-verification.md) `457b197`均包含于 `9b32015`。消费Model CRUD、按Provider分页、原Model command lookup、平台引用替代的真实事务、当前Session/admin、Audit与事件。 |
| 删除影响读口 | **已满足**，[管理读口报告](../agent-team/system-model-management-reads-verification.md) `ecd7337`及[读口契约](recovery-d09-system-model-management-reads.md)包含于 `9b32015`；当前权限在同事务核对，最多7组/10000条登记引用、三秒总预算、失败零候选。它只观察，不产生删除计划或权限。 |
| 配置与Resolver边界 | [Model Configuration](../../architecture/platform-infrastructure/model-system/model-configuration.md)、[Model Resolution](../../architecture/platform-infrastructure/model-system/model-resolution.md)、[Resolution已验报告](../agent-team/current-model-resolution-verification.md)。`4295df7`仅接受direct/platform.memory的受限库，生产Resolution仍未绑定；其更窄OpenAI/text/无tools限制不反向改写已接受管理CRUD的合法范围。 |
| 前端布局与wire | [D27计划](../development-plan.md#d27-业务页面与设置)、[系统设置§3](../../frontend-design/layouts/system-settings.md#3-模型与-provider)、[设置壳](../../frontend-design/layouts/settings-shell.md)、[公共组件](../frontend/components.md)、[Model HTTP](recovery-d09-system-model-http.md#4-路由严格-dto-与响应)、[OpenAPI](../../../api/openapi/model-system.json)。以固定正式实现及已接受细化契约决定可交付字段，不从设计表格补造后端能力。 |

完整设计中的非空parameters/request_overwrite/header_overwrite、reasoning_efforts/native profile参数、全域Model列表/检索、平台selector管理UI、外域Agent/Project引用adapter、Resolver生产绑定、Provider调用/发现/连接验证均**仍未交付**。本卡不会提供空页、无效按钮或成功stub代替它们，也不修改待决Summary初值。平台引用替代只消费已接受DELETE内部事务，不等于交付selector页面。窄卡接受不代表完整Model、D09或D27完成；ready503、生产SPA/真实Vite代理的原边界保持。Object与tools的原停止任务保持停止，不重建。

## 2. 页面、四type配置与安全详情

仅新增懒加载 `/system/models`，继承authentication/protected/systemAdmin。SystemSettingsView在原 `models-providers` 组的Providers之后增加Models，不改SettingsShell公共接口；`/system`仍默认用户。登录return从Provider的七项增加为八项，只接收该精确路径，拒绝query/hash、数组、动态子路径与外部URL。普通用户无入口，直链不挂载Model页面、不发管理请求。

页面先分页列Provider供明确选择，选中后分页列该Provider的Models；提供返回Providers、当前页刷新、上一页、下一页。没有全域Model列表API，不预抓全部Provider或循环扫全部Models。选中Provider/Model、编辑及替代目标仅在App期本页私有状态，不放URL/history或持久存储。Provider列表空时说明先配置Provider并提供已有Providers叶子的正常入口；当前Provider无Model时显示真实创建入口。

Model列表显示name、原生provider_model_id、type、enabled、所属Provider；详情由独立GET建立当前观察，展示ID、版本、创建/更新时间（UTC与完整27字符datetime）、完整安全配置及所属Provider的名称/protocol/enabled。字段作为文本，不外链、不探测、不执行JSON。credential、Secret metadata/材料、其他项目/Agent内容均不在本页；本卡不调用Credential接口。Provider disabled不等于配置丢失，不禁止原服务允许的Model管理CRUD；替代选择的enabled条件单独执行。

创建/编辑采用显式保存的UiDialog。创建时捕获明确选择的Provider，type仅允许其已确定protocol对应类型；编辑时Provider与type只读，完整PUT并保留原值，不能迁移归属或变type。输入不trim、补全或改写原生ID；name为1–128 Unicode字符且无NUL，provider_model_id为1–256 UTF-8 bytes且无NUL，两者拒绝未配对surrogate。enabled明确选择；没有修改的编辑不派发无变化PUT。名称/原生ID重复不由前端自造唯一约束，正式服务仍裁决所有输入。

capabilities是后端独立字段；界面按以下正式配置白名单渲染容量/能力声明，不把它们写入仍只接受空对象的parameters。context_length/max_output为可空容量声明，未配置显式null；有值必须为正int64十进制字符串，两者都有值时max_output≤context_length，不用JS Number损失精度。数组允许空、不可重复，仅允许表列集合；不把未填写能力解释为已验证外部支持。

| type与Provider协议 | 本卡已接受的能力配置 |
| --- | --- |
| chat：openai-chat-completions / anthropic-messages | tool_calls、parallel_tool_calls、streaming、reasoning四bool；parallel=true要求tool_calls=true。input_modalities为text/image/file/vector的子集，output_modalities仅text子集。OpenAI的structured_output_modes为text/json_schema子集；Anthropic仅text子集，不能保存json_schema。 |
| embedding：openai-embeddings | 四bool固定false，structured_output_modes固定[]；input_modalities仅text子集，output_modalities仅vector子集。 |
| reranker：jina-rerank | 四bool固定false，structured_output_modes固定[]；input_modalities仅text子集，output_modalities固定[]，不借用chat文本输出字段。 |
| image_generation：openai-images-generations | 四bool固定false，structured_output_modes固定[]；input_modalities仅text子集，output_modalities仅image子集。 |

四type的parameters/request_overwrite/header_overwrite均固定 `{}`，reasoning_efforts固定 `[]`。显示当前仅支持这些空形状，不提供任意JSON/Header/effort编辑器；reasoning=true本身也不承诺effort或Runtime可用。非chat按各自上述声明渲染，不出现聊天工具/推理/结构化输出开关或伪造其原生参数表。非空类型参数/覆盖的完整设计待正式profile能力后另交付。

保存只有严格Model receipt才提示“Model配置已保存”；随后列表/详情读取失败显示“操作已确认，当前配置读取失败”，仅重读，不恢复为未确认写。成功反馈沿2400ms与可访问播报。长名称/原生ID、时间、错误可换行，本页局部覆盖表格nowrap；窄屏按字段堆叠、表单单列、候选可分页，无页面级横向溢出。沿已接受Dialog/Drawer、键盘、Escape/遮罩、焦点与reduced-motion规则，不改公共组件。

## 3. 固定API、严格DTO与预算

新增 `web/src/api/system-models.ts`，提供封闭 `SystemModelAPI` 和不可变Model/Input/Page/Impact/Receipt类型。Provider/Model既有只读类型与纯解析复用Provider最终接受实现，必要时仅在 `system-providers.ts` 窄导出原解析器，不改变该API的Provider命令闭集/receipt规则。现有三项只读方法可在新工厂内委托已验Provider API；全部调用仍进入本卡同一Session owner，不能从页面直接调用绕开owner。

| 方法 | 唯一路径、输入与成功 |
| --- | --- |
| listProviders | GET `/api/v1/system/model-providers?limit=25[&cursor=…]`，200 ProviderPage；复用原严格读与2MiB成功预算。 |
| getProvider | GET `/api/v1/system/model-providers/{id}`，200 Provider，id等于捕获目标。 |
| listModels | GET `/api/v1/system/models?provider_id=…&limit=25[&cursor=…]`，200 ModelPage；provider_id必填、单一。 |
| getModel | GET `/api/v1/system/models/{id}`，200 Model；id及provider_id须匹配本地捕获目标/选中Provider，不把本地核对上下文塞入query。 |
| createModel | POST `/api/v1/system/models`，body恰 `{provider_id,input}`，200 model.create receipt。 |
| updateModel | PUT `/api/v1/system/models/{id}`，body恰 `{expected_version,input}`，200 model.update receipt。 |
| deleteModel | DELETE `/api/v1/system/models/{id}`，body恰 `{expected_version,replacement}`，replacement必填canonical UUIDv7或null，200 model.delete receipt，不是204。 |
| getDeletionImpact | GET `/api/v1/system/models/{id}/deletion-impact`，无query/body，200下述六字段Impact。 |
| lookupModelCommand | POST `/api/v1/system/model-commands/lookup`，body仅 `{command}`，command仅model.create/update/delete；使用原key及Session CSRF，200 `{found,receipt}`。 |

动态ID先校验canonical UUIDv7，所有输入对象拒绝未知成员；只列表允许固定表列query，经URLSearchParams编码。GET不带body/key/CSRF；三写与POST lookup由owner附稳定原key、CSRF和signal。client.ts仅增加必要固定方法/Model command分类，不开放任意URL、header、method或caller limit；Provider旧lookup仍只处理原provider三kind。无HEAD页面调用、selector GET/PUT或Credential/Provider写入口。

Model输出恰 `id,provider_id,input,version,created_at,updated_at`；input恰 `name,provider_model_id,type,enabled,parameters,request_overwrite,header_overwrite,capabilities`。capabilities恰四bool、四字符串数组与context_length/max_output两个必填nullable字符串，逐项按§2校验，不能只解析可见列。Provider/Page形状沿上游；ModelPage恰 `{items,next_cursor}`，next_cursor必填、null或非空≤8192字符，仅满25项页允许非null。一页0–25项、ID不重复、provider_id等于捕获Provider、完整 `(created_at,id)` 严格递减。时间沿已导出严格Instant解析器，合法27字符UTC微秒与日历，updated_at≥created_at；不截断微秒排序。完整解析后一次发布不可变候选，坏行或未知字段全拒。

Model receipt恰 `{kind,resource_id,version,affected_references}`。kind匹配原操作；create版本为1、update/delete版本为原expected_version+1且resource_id等于目标，全部精确字符串整数；新update/delete的expected_version必须有合法int64后继。create/update的affected_references必须为0；**delete为任意合法非负int64十进制字符串，可与旧impact数量不同，也不受读取10000上限约束**，不得沿Provider解析器硬限0。lookup found=false时receipt必须null，true按原命令同样核receipt；不接受错目标/错kind/数字代字符串、前导零或溢出。

Impact恰下列六字段，全部required：

| 字段 | 精确约束 |
| --- | --- |
| model_id | canonical UUIDv7，等于本次捕获目标。 |
| version | 正int64 canonical十进制字符串；这是被读取Model版本，不是引用集合版本或删除许可。 |
| reference_count | canonical十进制字符串0–10000，等于所有group count的精确和。 |
| reference_groups | 非null数组，0–7项；每项恰owner_kind/role/count，count为1–10000 canonical字符串。无重复组合，按owner_kind再role字典序严格递增；只允许下表七项。 |
| replacement_requirement | 闭集none/optional/required，须与groups贡献一致：任一required则required，否则非空optional，空则none。 |
| delete_blocker | 显式null或reference_adapter_unbound；有任一外域group必须为该值，仅平台/空集合必须null。null仅表示未发现这项已知阻断，不表示可删。 |

| owner_kind / role | requirement贡献 | 当前adapter |
| --- | --- | --- |
| agent / agent_model | required | 未绑定 |
| agent / approval_model | required | 未绑定 |
| platform_selector / embedding | required | 已绑定 |
| platform_selector / image | optional | 已绑定 |
| platform_selector / memory | required | 已绑定 |
| platform_selector / reranker | optional | 已绑定 |
| project_summary / meeting_summary | required | 未绑定 |

total=0必须groups=[]、requirement=none、blocker=null；其他组合或额外owner/name/link/effort字段均拒绝。显示“登记引用”及静态中文类型标签，计的是边数，同一owner两role计两条，不称去重项目数或外域当前活体事实。无Project/Agent ID、名称、内容或跳转。后端10001哨兵为RESOURCE_BUSY/409且零Impact；失败/Unknown/取消/非法响应一律零候选，不将已有值保作当前许可、不截断、不分页、不自动重读。三秒预算涵盖该GET认证/锁/事务，客户端仍遵守原owner实际结束规则，500/503等真实错误不硬改为超限。

所有本卡新body保持序列化UTF-8 JSON≤16KiB，字段原长度与编码预算在创建intent/key和dispatch前分别校验。当前空配置与有限能力枚举、128字符名称/256B原生ID足以落入该预算；不为未来非空配置扩限。transport实际流累计响应限额沿上游：仅listProviders成功200为2097152B，所有Problem、Model单项/页/Impact/lookup等均600000B；超限零候选，等待read/cancel真实尾部。保留上游Credential512KiB/Provider32KiB窄请求例外，不全局改限。

## 4. 删除影响、替代选择与命令恢复

删除操作先捕获选中Model，再读取当前详情及Impact；只有完整同目标且version一致的观察才进入可确认状态。版本不一致显示配置已变化，要求明确重读、核对并再次确认，不能悄悄替换原expected_version。读取失败/超限只提供重读或取消，不显示零引用或提交按钮。delete_blocker非null时说明相关引用替换能力尚未绑定，保持零DELETE；不让选择替代绕过阻断。

| 影响requirement | 删除确认中的明确动作 |
| --- | --- |
| none | 显示当前未登记引用，replacement显式null；仍由正式DELETE重新检查。 |
| required | 必须明确选择合法替代，缺候选/未选时不能确认；不提供清空。 |
| optional | 明确选择“清空相应用途引用”或合法替代，再确认；不暗中把未选择映射为null。 |

替代浏览器在删除Dialog内独立分页选Provider再选Model，沿真实25项GET，不自动扫全库、不声称当前页之外没有可用模型。可展示不可选原因，但只接受非自身、System、同type、Model.enabled=true且所属Provider.enabled=true的当前观察；跨Provider替代允许。含memory引用时还须structured_output_modes含json_schema；不能用text或协议名称代替该声明。旧候选变化或失效时须重新选择/核对，不能自动改用第一项。平台其他用途能力沿其type规则；外域仍blocked，不伪造Agent reasoning或项目可见性判定。

确认捕获原ModelID/expected_version及明确replacement，DELETE只发一次命令。Impact不产生token/plan/grant、替代版本fence或引用快照；Model.version不随所有引用变化，不能凭版本相等保证引用未变。正式后端重新发现现有引用、候选与当前权限，在同事务更新合法平台selector及引用后删除；UI不先PUT selector，不级联Provider/其他Model，不修改外域。读后新引用、候选禁用/删除、版本竞争或adapter缺失按真实服务失败显示，输入/原观察保留为旧事实；不静默改version/替代后再发。严格receipt后才移出页面并刷新，显示其实际affected_references；刷新失败不撤回确认。

`createSessionController` 在Provider最终接受的四个参数后增加第五个可选、默认真实SystemModelAPI，旧调用/mocks保持。Model读/写/lookup各自绑定本域scope与代次，共享唯一Cookie owner与原30秒可见预算；不复用Provider域intent，不建立队列或第二Session store。每次开始检查authenticated、完整UserID+SessionID+epoch、当前admin、未system拒绝与页面代次；同一时刻只保留一个本页不可变Model写意图，kind/path/body/provider/expected_version/replacement/key/原CSRF及identity全部冻结。

本地未派发或无历史未确认且服务明确not_started/not_committed的字段/版本/不存在/业务相容性拒绝，显示已知失败；允许用户明确重读核对后以新意图保存。已派发transport、非法响应、超时/取消、COMMIT_UNKNOWN、unknown/committed却无严格receipt、5xx及RESOURCE_BUSY保留原请求未确认。曾未确认时后续非成功不抹去历史不确定性；IDEMPOTENCY_KEY_REUSED锁住原意图，不换key/body。规则沿Provider已接受纪律，不将所有业务拒绝误称未知。

“检查原请求”先恢复当前Session；仅同完整identity、原CSRF且当前admin才用原key作一次lookup。found/receipt只证明该身份历史观察，不验证当前body、不证明对象还在、也不证明writer终局；false/失败不证明原写未提交。必须显式“重试原请求”以原path/kind/key/body重放正式Execute，严格receipt才确认；DELETE之后当前GET404、Impact失败/变化或替代已消失不能先挡住合法历史重放。当前列表/Impact/版本不是receipt，不重复读取至成功、不自动换key。明确放弃只停止前端追踪并清原意图，说明此前请求仍可能生效；不撤销服务端删除/替代、不补偿新建。

## 5. 分页、权限、实际取消与确认生命周期

App创建/provide `createSystemModels`，保存页面草稿、选择、未确认意图与确认Promise。主Provider页、当前Provider的Model页、替代Provider页及其Model页分别保留各自25项cursor历史，不跨目标或identity复用；成功后才换页，上一页重读原cursor、后退丢后续历史，刷新/重新进入/确认写后回首项。CURSOR_INVALID明确回首项，不偷偷重读；普通GET开始隐藏该分区旧候选，失败零新候选，无自动重试。初次因其他owner忙未派发的加载最多延迟一次；多分区顺序读取且失效/取消后不续发。

普通表单变化、替代选择、进行中或未确认写属于dirty；空白未提交表单/只读Impact不额外确认。页内换Provider/Model、返回/分页/刷新、菜单/顶部本人入口/退出、浏览器返回、Dialog关闭均先统一“继续编辑 / 放弃修改”，路由确认先于Session重验。明确查原请求、已知冲突的显式核对不被通用刷新清意图。导航hook与beforeunload/计时器成对清理，不绕过其他个人/公开/邀请/Provider域。

确认状态/Promise留App期controller，**统一放弃确认Dialog固定置于SystemModelsView的Editor、删除/替代等业务Dialog之后**；App不增加常驻Model确认层。所有Model模态同一子树共同卸载/同序重挂。App真实pageshow/visibilitychange复查规则保持；checking卸载RouterView只撤离读候选/DOM，不结算确认或丢草稿。复查失败时App恢复按钮可用、无残留overlay/inert/滚动锁；同完整identity且admin恢复原确认顶层，重挂不发新写、不选答案。真实离页/失权/新Session/epoch/注销/App.dispose清旧状态，以false结束待决确认，旧导航/关闭/退出不能迁到新身份。

继续编辑与放弃只结算一次；关闭确认的模态内焦点由已接受共享层恢复，View/Editor/DeleteDialog的await后续体须核本实例仍有效、目标当前连接且可操作。继续编辑仍有业务Dialog时不补焦点，确认打开时标题不抢焦点，即使无底层业务Dialog；正常业务关闭/提交拒绝后可用控件恢复仍保留，删除后只落到现存列表控制/标题。不得以全局选择器、z-index、业务手focus或反复nextTick重挂代偿。

可见超时、取消或放弃不释放实际owner；fetch/body/read/cancel全部实际join、finally结束后才释放。各域abandon双向隔离，包括个人、Provider两步、邀请、users及Model；Model分类不能落入personal或Provider的revision默认分支。正常业务清理不干扰restore/登录/注销/改密，App.dispose/auth.leave全局清理语义保留。当前Model写与POST lookup的CSRF_FAILED按完整identity+owner generation失效；当前401仍处理迟到Cookie后果，当前403 FORBIDDEN清所有系统域私有状态并设置身份绑定拒绝，不改role、不自动注销，成功Session重验后才恢复。旧identity/代次错误不污染新身份；旧请求尾部未结束时新身份请求仍不能越过，无晚到发布或自动后续写。

## 6. 二十四条候选路径与所有权

本卡只授规格写入。以下路径须Provider最终独立接受、固定实际产品与资源移交且本规格静审采纳后，由主线程唯一指派frontend_worker；与Provider活动共享文件不得并行实施。若上游最终已导出所需纯解析器，可由主线程在移交时删去第2候选，不为达到数量而改它；缺口或其他路径扩张必须先报告。

| # | 路径 | 唯一用途 |
| --- | --- | --- |
| 1 | `web/src/api/client.ts` | 固定Model GET/写/Impact/lookup分类与封闭options；原预算/Provider/Account形状不放宽。 |
| 2 | `web/src/api/system-providers.ts` | 仅必要窄导出现有Provider/Model纯解析及类型，原API、读/receipt语义保持；最终已导出则不改。 |
| 3 | `web/src/api/system-models.ts`（新） | 九项受限API、Model/Impact/receipt严格解析与输入预算。 |
| 4 | `web/src/composables/useSession.ts` | 第五可选依赖、Model独立scope/intent、原请求恢复、实际owner及当前权限/CSRF清理接入。 |
| 5 | `web/src/composables/useSystemModels.ts`（新） | App期草稿/分页/选择/Impact/确认、身份和页面代次。 |
| 6 | `web/src/App.vue` | provide/导航/顶部退出组合及dispose，仅状态/hook，不增加Model确认UI宿主。 |
| 7 | `web/src/router/index.ts` | 仅新Models叶子，系统默认用户保持。 |
| 8 | `web/src/router/auth.ts` | 第八项精确return及Model离页前确认/完成hook。 |
| 9 | `web/src/views/system/SystemSettingsView.vue` | 原模型组追加Models，原用户/邀请/Providers与权限保持。 |
| 10 | `web/src/views/system/SystemModelsView.vue`（新） | 按Provider分页/详情、业务Dialog及末尾放弃确认、局部布局/焦点门禁。 |
| 11 | `web/src/views/system/SystemModelEditor.vue`（新） | 四type当前合法字段、完整输入与安全错误、保存/dirty及实例生命周期。 |
| 12 | `web/src/views/system/SystemModelDeleteDialog.vue`（新） | 精确Impact、独立分页替代选择/清空确认，不拥有Cookie/key或写授权。 |
| 13 | `web/src/tests/system-models-client.spec.ts`（新） | 闭合DTO/Impact/receipt/输入/预算/只读复用与反例。 |
| 14 | `web/src/tests/system-models-state.spec.ts`（新） | 实际controller+transport，owner/分域/身份/原命令/取消/Impact及分页。 |
| 15 | `web/src/tests/system-models.spec.ts`（新） | 真实router/controller、四type表单、删除确认、App恢复及焦点/菜单。 |
| 16 | `web/src/tests/system-user-directory.spec.ts` | 菜单精确增Models，原用户默认/权限/分页断言保持。 |
| 17 | `web/src/tests/system-invitations.spec.ts` | 菜单/共享导航组合适配，原写入/恢复/cohost强断言保持。 |
| 18 | `web/src/tests/system-providers.spec.ts` | 菜单/共享owner组合适配，原两步材料/恢复/Models只读断言保持。 |
| 19 | `web/src/tests/personal-settings.spec.ts` | 七项return增八项与新合法目标，旧拒绝/草稿保持。 |
| 20 | `tests/account/system_models_web_test.go`（新） | 下节真实顶层及正式命令/引用/receipt事实核对。 |
| 21 | `tests/account/system_models_web_fixture_test.go`（新） | 自有正式服务准备、runner、受限响应控制及下述唯一外域索引负例。 |
| 22 | `tests/account-captcha-web/system-models.config.js`（新） | 固定Playwright、精确场景/预算、私有输出。 |
| 23 | `tests/account-captcha-web/e2e/system-models.spec.ts`（新） | 生产dist+真实backend的管理/删除/恢复/权限/布局。 |
| 24 | `docs/development/frontend/README.md` | 接受后同步窄Models能力、第八return、实际命令与全部未交付边界。 |

必读[Vue开发技能](../../../.agents/skills/agenteam-vue-development/SKILL.md)、[Vue测试技能](../../../.agents/skills/vue-testing-best-practices/SKILL.md)；独立验收读[验证技能](../../../.agents/skills/agenteam-verification/SKILL.md)，浏览器使用环境可用Playwright技能，缺失如实记录。SettingsShell/Ui/useLayer、全局CSS、旧个人/公开/邀请/Providercontroller与页面、后端/OpenAPI、迁移、旧fixture/driver、锁及当前归档均只读。零DDL，无新依赖；停止任务不解冻。

## 7. 高风险验收与资源门槛

开工先固定Provider实际接受提交及其严格解析导出、第五参数插入点、菜单/return、owner分类；本卡input以接受组合加精确候选构成，不能用正在变化的整树。作者执行 `npm run check --prefix web`，只格式化授权文件；Go1.27.1/local、`GOPROXY=off GOSUMDB=off GOFLAGS=-mod=readonly`下integration-tag race编译与vet，依赖准备单列，正式检查离线只读。纯测/编译不冒充真实权限、浏览器或事务通过。

纯测覆盖四type/protocol组合、type/Provider不可变、空配置边界、所有capability限制、原字段与16KiB编码限制、非法Unicode/完整DTO/时间/cursor/>2^53。Impact逐字段缺失/未知/null/错目标、七组闭集/顺序/重复/总和、none/optional/required/blocker相互约束、10000合法/10001非法及失败零候选；delete receipt允许非零及与旧观察不同，而create/update零。复用Provider2MiB成功与其他600000B实际流预算，不因新API导出放宽旧解析。

用生产controller+受控transport的异步屏障覆盖Model读/写/lookup及body/cancel忽略abort时的实际owner：30秒可见结束/放弃/身份变化后仍阻止其他域，实际join才释放；反方向与各域自身abandon、迟到401/403/CSRF、两步Provider不被Model清理干扰均验证。覆盖原key/body/version/replacement重放、lookup true/false/失败不确认写、明确拒绝与历史不确定区分、删除后404不阻断历史重放、确认写后GET失败保留receipt、候选切换/分页失败不串目标。

页面组合先打开dirty或未确认的放弃确认，再合成pageshow经真实App listener保持Session GET待决；checking零Model模态，失败App恢复按钮可操作，同identity恢复原顶层/草稿、零新写；继续/放弃只结算一次，待决导航/退出及真实失权/换Session/dispose有终点。覆盖无底层业务Dialog、旧View/Editor/DeleteDialog续体不得抢焦点。不能直接finishConfirmation代替按钮、删强断言或用DOM顺序冒充浏览器命中/焦点。

| 新真实顶层 | 必需场景 |
| --- | --- |
| `TestAccountSystemModelsWebLifecycle` | 正式Provider准备后，经页面创建四type、GET安全详情、名称/原生ID/合法能力编辑、禁用启用；type/Provider只读、非空配置无编辑入口、普通禁用Provider仍可管理配置。无引用删除取消零DELETE，严格receipt后真实GET404；不发Provider网络调用。 |
| `TestAccountSystemModelsWebDeletionAndReplacement` | 正式平台选择使embedding/memory为required、reranker/image为optional；页面明确选择替代或可选清空，经唯一DELETE核Model删除与平台引用同事务结果。memory缺json_schema、disabled Model/Provider、自身/错type不得选。读取后以正式服务增加/切换引用、禁用候选或更改目标版本，再确认原删除须重新裁决且无部分改写；不能以旧version推断引用未变。含下述真实adapter-unbound页面阻断与正式HTTP拒绝两层证据。 |
| `TestAccountSystemModelsWebOutcomeRecovery` | 任务自有同源服务器分别在真实create/update/delete接受后有界丢失响应；lookup仅历史观察，显式原Execute重放后核同一receipt及唯一业务/Audit/event事实。删除已生效后的GET404、Impact失败不妨碍历史恢复；成功后读取失败仍保留确认，放弃不回滚、不补偿。 |
| `TestAccountSystemModelsWebReadAndPagination` | 至少26个正式Provider及目标下26个正式Model，主选择和替代浏览分页独立；空/失败/坏cursor/返回/刷新、跨Provider切换、当前页无合法候选不冒称全域无候选。所有正向数据由正式服务产生；记录无自动扫页/查询放大，保留上游大Provider页兼容。 |
| `TestAccountSystemModelsWebAuthorityAndIdentity` | 普通用户无入口/直链零请求；真实Session撤销/admin失权拒绝CRUD/Impact/lookup并清旧系统状态，当前与迟到错误不污染新身份；同User新Session/明确换账号不保留旧意图。受控权限准备仅限任务自有精确identity，不fulfill假403冒充正式授权。 |
| `TestAccountSystemModelsWebNavigationAndLayouts` | 系统默认用户、Models直链登录return、四叶子导航；dirty/替代选择/未确认的继续与放弃。确认已打开时触发App pageshow，含自有服务器精确Session GET一次失败、App按钮重试/真实同Session恢复；实际点击/键盘可达确认，关闭后焦点留剩余modal且Tab/Shift+Tab不逃出，真实离页无旧层。light/dark×1440/1024/834/390、长字段、Dialog/Drawer Escape/遮罩、reduced-motion及无溢出；合成事件驱动与自然浏览器恢复区分。 |

平台selector引用及其并发变化必须经已接受正式GET/PUT或等价正式服务端口建立：先正式创建所需Provider/Models，再以singleton真实ID/version提交完整embedding/memory及可选项；不直接SQL修改selector或平台引用索引，不为测试绑定Runtime/外域adapter。

主线程仅允许一个明确标注的外域索引负例：在任务自有隔离库，对正式创建且核属本fixture的精确System Model，只向 `agenteam_model.references` 插入一条 `owner_kind='agent', role='agent_model', owner_id=<本例新UUIDv7>, project_id=<本例新模拟UUIDv7>, model_id=<精确目标>, owner_version=1, reasoning_effort=''`。完整闭集、FK/唯一键及有效UUID约束保持；不写Agent/Project表，不声称模拟owner/project正式存在或producer已实现。页面经真实Impact显示reference_adapter_unbound并禁确认，计数证明零DELETE；测试随后独立以正式HTTP对同目标发合法DELETE，核真实503/DEPENDENCY_UNBOUND及目标、selector、引用、成功receipt/Audit/event业务事实不变。两层证据分列，不把测试发出的DELETE归给页面。清理只按本例完整owner/kind/role/project/model/version/effort谓词删这一行并核恰一条，不宽删索引；其后按原自有fixture清理。该例仅复用已验management-impact的反向索引负例模式验证UI组合，不证明外域真实绑定、替代或可删；不得新增其他直写业务引用、生产stub或放宽adapter。

真实执行可按精确名称分组：`scripts/test-security.sh -run '^TestAccountSystemModelsWeb(Lifecycle|DeletionAndReplacement|OutcomeRecovery|ReadAndPagination|AuthorityAndIdentity|NavigationAndLayouts)$'`。保持Playwright每test45秒、Go顶层2分钟、workers=1/retries=0、race/count1/每包6分钟；分组依据实际耗时，不加时/削断言/把no-tests算通过。仅任务自有PG17.x（最低17.8）、MinIO、Central与冻结生产dist同源服务器，禁止外部Provider请求或既有基础设施；不引SMTP/新的外部依赖。

共享App/router/owner的必要旧真实回归：`TestAccountAuthenticationWebSessionLifecycle`、`TestAccountPersonalSettingsWebThemeAndNavigation`、`TestAccountPublicEntryWebIdentityNavigation`、`TestAccountSystemUserDirectoryWebAuthorityAndIdentity`、`TestAccountSystemUserDirectoryWebNavigationAndLayouts`、`TestAccountSystemInvitationsWebOutcomeRecovery`、`TestAccountSystemInvitationsWebNavigationAndLayouts`，以及Provider最终接受的 `TestAccountSystemProvidersWebCredentialReplacement`、`TestAccountSystemProvidersWebOutcomeRecovery`、`TestAccountSystemProvidersWebNavigationAndLayouts`。按最终固定差量复用未变证据；原敏感材料/请求body不进入日志、截图、trace或归档，Provider回归继续沿其保护纪律。

Provider作者/独立验收实际停止、资源清零并接受后，由主线程另授本卡独占窗口。作者冻结输入/dist/锁/环境、保留原失败、实际argv/env/退出与安全日志，再向未参与实现的verification_worker移交独立权限/恢复/引用竞态及真实浏览器验收。命令实际wait、server/browser/所属进程join、自有exact-ID两次absent且旧基线不变后交回；不并发共用fixture、不占当前归档。主线程核证据后才接受/同步文档/提交窄完整结果，未交付边界始终保留。
