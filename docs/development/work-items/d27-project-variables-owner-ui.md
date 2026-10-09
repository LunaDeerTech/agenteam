# D27 普通 Project Variables Owner 管理界面

状态：rev1 规格已独立接受；API、Session、页面片段已实施并通过限定纯控，真实浏览器验收准备中。正式依赖 main `3cea6076` 的[普通变量服务与 HTTP](d10-project-variables-owner-http.md)已接受；本卡不代表完整 D10/D27 或平台 ready。

## 1. 完整结果

已有 initialized Project 的当前 Human Owner 可从项目设置进入 `/:username/:project_name/settings/variables`，分页浏览普通变量、读取完整值、创建、编辑、删除，以及在响应丢失后查询和显式重放原操作。管理员没有 Owner 旁路。

不含 Secret、Agent 白名单、运行环境注入、模板解释、Project 创建/生命周期、自动重试、批量导入导出、名称搜索和公开历史浏览。没有跨 Session 的 UI 恢复承诺；草稿、值、原 key/body/CSRF 不写 local/session storage、URL、日志或公开浏览器证据。普通值不渲染 HTML、不执行 shell/URL，不提供 Secret 安全承诺。生产 SPA 停止项保持。

依据：[项目设置布局](../../frontend-design/layouts/project-settings.md)、[工作台](../../frontend-design/layouts/project-workspace.md)、[变量架构](../../architecture/project-work-management/project-environment-variables.md)、[前端工程](../frontend/README.md)、[正式 Schema](../../../api/openapi/project-variables.json)。

## 2. 文件与精确接缝

本分支 `ai/project-variables-owner-ui`，工作树 `/workspace/agenteam-project-variables-ui`。新增独占：

- `web/src/api/project-variables.ts`：严格 DTO、输入捕获、六 HTTP 调用及原意图/回执一致性。
- `web/src/composables/useProjectVariables.ts`：分页、详情、草稿、历史命令/当前观察分离及离开确认。
- `web/src/views/projects/ProjectVariablesView.vue`、`ProjectVariableEditor.vue`：目录、完整值表单及删除/离开确认。
- `web/src/tests/project-variables-client.spec.ts`、`project-variables-session.spec.ts`、`project-variables-state.spec.ts`、`project-variables.spec.ts`。
- 本卡及本树 `.agent-state/current.md`。全局台账由 root 统筹。

root 已授本树以下七共享路径的本域最小增量：

| 路径 | 必要变化与边界 |
| --- | --- |
| `web/src/api/client.ts` | 六精确端点/query、本域1MiB/5MiB与重复JSON检查；旧域默认上限/行为不变，不开放任意URL/调用者上限 |
| `web/src/composables/useSession.ts` | 末位Variables API依赖、独立action/revision/私有intent facade；唯一runAuthorized，不导出CSRF、不新增Cookie队列、不改变旧Session默认 |
| `web/src/router/auth.ts` | 精确suffix/safeReturn与本域navigation owner；原Owner→Model保持，后续合Work时为Owner→Model→Work→Variables |
| `web/src/router/index.ts` | 单一settings/variables懒加载leaf，不改Project默认入口 |
| `web/src/App.vue` | App级provide/路由通知/dispose/logout和本域确认；constructor新依赖追加末位，不破坏原调用 |
| `web/src/views/projects/ProjectSettingsView.vue` | 既定“变量与 Secret”组仅提供已实现Variables子项，不放Secret空入口；保留现菜单 |
| `docs/development/frontend/README.md` | 限定能力、路由、恢复及未包含范围 |

`useProjectWorkspace.ts`、`ProjectWorkspaceView.vue`、`ProjectNav.vue` 只读复用。root最终逐差异整合，不用本树旧基线覆盖WorkUI/Model增量。

浏览器拟新增 `tests/account/project_variables_web_test.go`、`project_variables_web_fixture_test.go`，`tests/account-captcha-web/project-variables.config.js`、`e2e/project-variables.spec.ts`、`e2e/project-variables.helpers.ts`。既有 `tests/account/project_owner_web_fixture_test.go` 的Variables-only可选观察/config/IPC seam已获root授予，nil默认Owner行为原样，不依赖Work WIP类型。runner精确selector扩展另报工具写权，不复制监督器。独立probe由未参与者持有。

## 3. API/Session 接口

API导出 `ProjectVariable`、`ProjectVariableSummary`、`ProjectVariablePage`、`ProjectVariableQuery`、`ProjectVariableCreate`、`ProjectVariableUpdate`、`ProjectVariableCommand`、`ProjectVariableReceipt`、`ProjectVariableLookup`、`ProjectVariablesAPI`；对象/摘要字段完全沿正式Schema，摘要不得伪造value。`newProjectVariableID()` 用当前毫秒及安全随机生成canonical UUIDv7，不能用randomUUID的v4替代。版本只canonical十进制字符串，比较用BigInt，不转浮点；时间严格校验正式Instant和先后关系。

命令闭集：create `{kind:'create',projectID,request}`；update `{kind:'update',projectID,targetID,expectedVersion,request}`；delete `{kind:'delete',projectID,targetID,expectedVersion}`。create request自带提前生成variable_id；update只含确实修改的presence字段且至少一项；delete不带request。捕获深拷贝/冻结，拒未知/null/非法Unicode，不trim/归一用户文本。

低层API：`list(projectID,query,signal)`、`get(projectID,targetID,signal)`、`create(projectID,request,csrf,key,signal)`、`update(projectID,targetID,expectedVersion,request,csrf,key,signal)`、`delete(projectID,targetID,expectedVersion,csrf,key,signal)`、`lookup(command,csrf,key,signal)`。signal必传；Lookup组装正式三variant，不直接序列化内部kind/projectID。

页面仅消费 `auth.projectVariables`：`list`、`get`、`start(command)`、`lookupOriginal()`、`replayOriginal()`、`abandonReads()`、`abandonPending()`、`editRejected()`和只读progress；无signal/CSRF/key参数。progress含projectID/kind/targetID、phase(submitting/uncertain/rejected/confirmed)、receipt、observation(none/committed/in_progress/not_observed/failed)、lookingUp/contextValid/keyConflict/canReplayOriginal，不暴露key/CSRF/原body。

所有Cookie调用仍唯一runAuthorized。30s逻辑期限/页面取消可结束可见等待，实际fetch/body read/cancel全结束前仍占owner，不允许下一Cookie请求重叠。私有CSRF只在Session内部取得。POST Lookup用原key及原当前Session材料。完整EOF、UTF-8、Content-Type、严格DTO、Project/target/command/原version和字段匹配缺一不可确认。响应JSON重复member（含转义后同名）拒绝；坏末项不得发布半页。

严格限定本域：request/detail/mutation/Lookup 1MiB，summary list 5MiB；错误体沿原小额上限。合法最坏JSON escaping按真实完整表示验，不把已有600k默认套在新域，也不扩大其他域。

## 4. 页面与字段

保留双导航/SettingsShell，标题Variables，普通变量说明、新建按钮、摘要表。列表列名称、描述、版本/更新时间、“查看”；仅选中后Get完整值，不批量加载value。默认固定50项，保持服务端 `(name COLLATE C ASC,id ASC)` 排序，不按locale重排。上一页保存opaque cursor栈，下一页仅在next_cursor存在时可用；显示当前页，不伪造总页数，不使用需要total的UiPagination。

完整页须同Project/type、ID唯一、严格排序、next_cursor存在时满页。CURSOR_INVALID/STALE保留旧观察并标旧，提供明确“从第一页重新读取”，不自动跳页。mutation后旧cursor可能失效，明确重读；当前Get或名称相同不确认原命令。同名新UUID不继承旧详情/草稿。

选摘要后Get成功才显示完整值和编辑原版本；摘要不充当前像。页外对象不新增路由或猜页位置：命令结果可以按原稳定ID重新读取当前对象；删除后显示当前不存在，与历史receipt分开。切换对象先处理dirty/未决intent。读失败保留草稿，不以旧详情解锁新保存。

| 输入 | 正式规则 |
| --- | --- |
| name | ASCII `[A-Za-z_][A-Za-z0-9_]{0,127}`，不trim/折叠；ASCII不区分大小写等于AGENTEAM或以AGENTEAM_开头拒绝 |
| description | 必须字符串，可空，≤4096 UTF-8 B；禁NUL与除TAB/LF/CR外Cc；逐字保存 |
| value | 必须字符串，可空，≤32768 UTF-8 B；禁NUL及非法Unicode，其余逐字保存，不解释内容 |
| version | 原Get版本，canonical正int64十进制字符串；创建不带版本，更新/删除必带 |

字节数不能用HTML maxlength/UTF-16长度替代。type固定variable，不提供Secret选择。新建预生成UUIDv7；提交前固化key/request presence/body/target/version，发送后材料不可变。未变化禁保存；版本冲突保草稿，必须显式重读并选择采用当前值或保留输入重新审阅，不自动合并/换版本/换key。

删除确认展示名称、稳定ID、原版本及“从当前目录移除；历史操作回执仍可能保留旧值”。不承诺擦除、Agent引用清理或运行环境变化。只有合法delete receipt成功；404/列表消失/Get不存在不能代替原成功。新key删除已删目标仍拒绝，同key历史成功原样返回。

采用既有UiInput/UiTextarea/UiTable/UiButton/UiState/UiDialog及tokens。成功在触发按钮，不加底部toast；错误留字段/区域。窄屏单列、长值局部滚动。对话框可取消、标题/说明关联，键盘焦点限制与恢复；删除后原触发消失时使用既有fallbackFocus到仍存标题，不抢其他浮层焦点。取消/关闭策略显式控制，不丢dirty。

## 5. 当前身份、权限与离开

仅当Owner `currentReadContext` 非空、Project稳定ID与Session/Owner/generation/readGeneration匹配才请求子资源；getter非空不等于active，另核当前detail.project.lifecycle。initialized active允许新写；archiving/archived只读、Lookup及完成原意图重放；pending/deleting拒绝。每HTTP仍后端重新授权，管理员无旁路。

结果发布同时检查本域revision、完整identity(userID/sessionID/CSRF epoch)、Project稳定ID和上下文代次。同identity checking隐藏内容/取消读但保草稿和intent；真实Session/CSRF epoch或User改变立即销毁私有材料。同身份恢复须重新取得当前Owner Get才能操作。迟到旧结果不能复活视图/确认命令。改名canonicalize复用稳定ID并保合法suffix，旧名被另一个Project占用不继承草稿。

只接受精确 `/settings/variables`，raw route拒query/hash/编码绕过/额外suffix；safeReturn同闭集，合法点号Project名可直达。直接GET、浏览器导航、刷新和菜单点击均走正式Resolve→Get门禁。

controller放App生命周期，临时页面卸载不丢同身份内存。导航/切Project/logout先确认本域dirty/未决intent，取消留原页并恢复焦点。原Owner→Model（后续Work）→Variables顺序保持；各域只清自身，不承诺后域取消可复原前域已明确放弃的草稿。放弃提示只清本地材料、不撤销服务器操作；实际I/O尾未结束仍占唯一owner。

## 6. 原意图恢复

私有intent保存完整User/Session/CSRF epoch、Project/command/target、expected_version presence和值、request presence和字节、原key。无自动重放/轮询。transport/缺EOF/响应校验失败、COMMIT_UNKNOWN或unknown commit_state均保uncertain。

“查询原操作”只有Lookup committed且历史receipt严格匹配原intent才confirmed；not_observed/in_progress/查询失败/当前拒绝均不证明原未提交。GET同值或已删不确认。用户明确“重放原操作”才发原key/body/target/version，服务端receipt-first决定历史成功或当前拒绝。unknown粘性；已confirmed历史receipt不能因后继Lookup/replay拒绝或断流倒退/丢失。

首次明确not_started/not_committed且正式闭集业务拒绝可标rejected，不能仅凭HTTP4xx/409。IDEMPOTENCY_KEY_REUSED禁止重放，不清旧unknown或历史成功。字段错误只展示已知path/code，不回显任意服务端文本。只有从未unknown的明确拒绝才可editRejected修改后新提交；否则必须先明确放弃本地材料。

仅内存恢复：真正Session变化或刷新/关闭页面会失去原材料，明确提示此限制；不改变后端同User新Session可恢复的契约，不增浏览器持久化或可泄漏日志。

## 7. 验收矩阵

| 层 | 必须区分正确与错误行为 |
| --- | --- |
| API | 六精确method/path/query/key/CSRF；正式Schema正反；UUIDv7/int64/时间/UTF-8边界；重复member/坏末项/乱序/非满next/错scope/伪receipt；合法最大表示；EOF及cancel实际join、旧域不变 |
| Session | 唯一owner与逻辑超时后实际尾；捕获后源输入突变无效；三命令原Lookup/replay；明确拒绝/unknown粘性/confirmed后失败；checking及真实身份/CSRF变化、迟到结果 |
| 状态/组件 | Owner上下文与lifecycle分离；cursor前后/显式过期重置；摘要无值/按需详情；CRUD/空值/presence/冲突保稿；离开/logout取消/焦点；同名不同ID隔离 |
| 真实read/layouts | 正式no-tag根/Account登录/Project前置；点号Project名直接GET/导航/刷新；真实分页/只读/错误；浅深色、宽屏/390px、键盘/缩放/减少动效/长值 |
| 真实CRUD | 六HTTP、实际schema/client解码；真实变化/no-op/同名新UUID/delete及历史receipt；独立连接核command/history/Audit/Outbox，不只数200 |
| 真实recovery | 三域命令各一次已完成后真实截断；uncertain→原Lookup/显式replay；后继修改不改历史；not_observed/in_progress/改义/错版本不确认、无第二事实 |
| 真实identity/authority | 同身份checking保稿；真实logout/revoke/newSession清内存；非Owner管理员/跨Project拒绝；prepare后归档拒绝及历史重放；held读取消/原ctx/handler实际退出 |
| 独立 | 未参与者自行验证权限/历史意图/断流取消和持久事实，不用作者success布尔作oracle |

作者 case 闭集及精确入口如下；独立补集由验收者选。

- `TestAccountProjectVariablesWebReadAndPagination`（read）
- `TestAccountProjectVariablesWebCRUDAndHistory`（crud）
- `TestAccountProjectVariablesWebOriginalRecovery`（recovery）
- `TestAccountProjectVariablesWebIdentityAndCancellation`（identity）
- `TestAccountProjectVariablesWebAuthorityAndLifecycle`（authority）
- `TestAccountProjectVariablesWebLayouts`（layouts）

两既有 harness 仅登记这六个 exact selector。仅命中这六项时采用同 owned 目录的 `ui-<16hex>` 短 stem，排他创建冲突即失败；driver 将已登记 `plan.runtime` 显式传入 `AGENTEAM_AUTH_WEB_RUNTIME` 并硬拒长度超过45。非 UI stem、env 与默认输入列表不变；UI 的可选 selector 输入绑定须包含实际配置、浏览器源和冻结资产。没有外置 TempDir、第二监督器或额外清理者，仍由原外层观察同一个 runtime/private/7资源并实际 Wait。须先 Go/browser 编译及精确发现，再实施工具窄增量并独审。

复用Owner fixture进程内默认no-tag `app.Run(...)`、真实Account/Project、私有生产资产托管和原Node actualWait/7资源链，不宣称独立cmd。持久test Skills仅作已有Project前置，不能证明真实Skills/创建HTTP；归档fixture明确是门禁输入，不冒完整生命周期。

故障预先绑定原Request/method/domain/project/target/key/body，先有后端完整Body+Close与同keycompleted，再真实截断。浏览器预期不完整仅接受该精确已登记请求故障，普通成功仍要求finished/完整EOF/schema/client。held GET须有真实started/release/ctx取消/handler返回，不用sleep或buffer完成冒join。不复制Model私有harness。非法route零变量读取，API/缺失asset不得回退SPA。

预算沿既有Owner每Go top120s、Playwright45s、expect5s、workers1/retries0及原有界清理；root freshgrant独占真实窗口。dist构建/私有资产另协调，不能改他树dist。首CRUD已真实执行但whole FAIL，其他场景和完整动态接受仍未完成。原FAIL和缺失测量保留，修后只验影响范围，编译/发现不是动态PASS。

## 8. 当前实施状态

SPEC rev1已获独立窄审接受，无mustfix；这不是产品或动态验收。API/client首片段strictTS及新旧客户端102纯控已通过，Session首片段32纯控及strictTS通过；controller/view/路由已接实际Session facade，77项API/schema/状态/页面纯控通过；真实fixture与独立动态验收尚未完成。每片段可构建保存，WIP不当整卡完成。Vue与测试技能已读；两套本树私有node_modules已按同package/lock离线恢复，无下载，现已构建私有dist，首真实CRUD的FAIL与返修见下。

真实 harness 源已补齐六个 Go 顶层和对应 Playwright 场景。Variables-only optional/nil 接缝保留 Owner 默认路径；普通变量 seed 与 browser 响应分开，私有台账核原 method/path/query/key/body/CSRF，SQL 按原 User/command/request/receipt 及去重操作核 command/history/Audit/Outbox；held GET 的原 context 取消和 handler 返回分别观察。browser strictTS、Go race compile03、六 Go 与六 browser 精确发现已通过，私有 dist01 已构建；最终 SQL 加强后 compile04 和六入口 discovery02 也已 actual0。两个既有工具仅六 selector 的 closed mapping/短 runtime/可选 input_paths(binary, selector=None) 增量已写，parse和精确正负控已过；非 UI 默认输入列表、stem和env不变。真实 PG/socket/browser仍零执行，无独立技术接受；完整卡仍进行中。

首真实 CRUD01 whole FAIL，安全标记只到crud step2，PW具体失败行/DOM未保留，不回填根因。所有资源/进程/TCP实际尾已结束，旧adopted STOP保留。后续组件红控另确证了Variables replay getter的busy响应式依赖缺失：原owner持有期间的短路可让页面在当前GET完成后继续禁用重放；已先捕获reactive busy，原授权/串行/实际I/O尾不变。修后相关67纯控/strictTS通过，binary05与dist02已构建；CRUD脚本明确断言删除重放关闭详情，安全失败投影只含源码位置与布尔DOM。按root授权复用已有UI有限WNOHANG预reap，先采PID State并实际Wait0才通过，live/new/notwaitable/nonzero仍保留原失败gate；离线正负控通过，产品与harness增量分别获有限独审接受。整卡未接受，后续真实重跑仍需fresh窗口；恢复负例的真实补集与最终共享接缝组合验收保留。

CRUD02前的离线实际helper控制发现合成CSRF占位不满足正式43字符要求，以及Schema包装复用文档$id导致局部引用错误。两项仅修测试helper，使用合法合成token与绝对正式Schema引用，原请求六项身份绑定不变。实际helper/API/Schema红绿与原请求绑定、非法Schema负控已通过，browser strictTS通过，Runner窄独审有限接受；新增Problem反例最初fixture code错误的FAIL保留。binary05/dist02及产品未改。

CRUD02随后获新独占窗口并执行，whole FAIL：Go20.86s/browser exit1，闭集投影定位crud step6、helpers:372的网络事件复合error gate；原body/schema与SQL后验尚未到达，不能据末段页面状态声称CRUD通过。原布尔合并了非预期失败、重复事件和finished尾异常，具体原因仍缺测量；拟仅补闭集诊断，不放宽gate。所有层actualWait、本次owned Z先采State再wait0、7资源/private/runtime/desc及TCP双尾齐，监督器122.100s terminal1和输入不变已确认，窗口释放。首两次原FAIL保留，下一真实重跑仍需fresh授权。

诊断首源码已接入：闭集网络事件分类及原请求native消费计数，旁观原fetch/read/cancel Promise，不clone/tee/额外read/HTTP；按唯一XID+method/原path/query/status/同document关联，EOF须早于取消/abort/read拒绝，CL仅identity编码且合法时可比。单flight采样与有界退休只提供诊断，不以native EOF证明UI发布，不替代普通finished/body/schema/client/SQL gate。生产/dist未改，Go只新增typed安全输出，driver只追加确切native源输入；strictTS已过，行为控制/新测试binary及窄独审仍待完成。

诊断离线行为控制已通过：实际helper事件正反、锁定PW变换/序列化后的native原Promise语义、唯一原请求绑定、EOF/取消顺序与CL、单flight/导航/退休/迟到及Go安全解码正反。Node同步evaluate throw和未能恢复owned hook均标不可用，不冒join或退休。最终strictTS、binary06 race编译和六入口发现通过；六UI输入只新增确切native源且非UI默认不变。原dist02复用，本次尚待独立窄审；普通网络gate没有放宽，CRUD03尚未获授权或执行。

独审阶段又确证诊断缺陷：late PW事件能升级退休观察，冻结reader上的诊断安装能让原getReader新增throw。已分别加退休门禁及非侵入安装异常处理，并以observer-error阻止不完整观测推EOF/CL；原红及修后JS定向反控保留。生产/dist02/普通gate不动；Go安全派生条件受影响，须待磁盘恢复后新binary07及Go控制，本次尚未整体验收或真实重跑。

Runner补尾发现缓存旧wrapper在退休后可重装hook，已为全部缓存入口增加退休后原方法纯委托，保this/args/原Promise/throw且不更新观察；实际红绿和最终strictTS通过。更新的Go投影正反已由Runner实际独立通过，此次cached返修不改Go；Runner最终退休控制复审接受且无剩余must-fix，仅接受本次离线诊断准备。磁盘恢复后新binary07 race编译与六精确入口发现均actualWait0，原dist02复用；新CRUD03仍须另获fresh真实窗口，没有新动态PASS，原CRUD02 wholeFAIL不变。

CRUD03 随后以已审诊断源、binary07与原dist02获独占fresh窗口，作者真实CRUD/历史范围whole PASS：Go19.32s，外部工具实际terminal0，监督器113.178s terminal0。原普通finished、原响应body/schema/client和SQL后验门槛保持；Node/handler/service/root/Go/driver实际尾、四owned adopted actualWait0、7资源/private/runtime/desc与TCP双尾及输入不变全部闭合，窗口已释放。CRUD01/02的原FAIL、未确证归因和未执行范围保持；本次通过不接受完整D27或独立动态验收。下一现有AuthorityAndLifecycle可复用同一候选，仍须fresh资源窗口；ReadAndPagination、OriginalRecovery、IdentityAndCancellation、Layouts及额外恢复负例仍待实际，归档fixture不冒完整生命周期。

Authority01 后续使用同一已审源/binary07/dist02执行，whole FAIL：Go22.74s、监督器116.904s及外部actual terminal1。主断言位于spec:510：第二个Project归档并刷新Session后，新建按钮未按预期disabled；后续非Owner/admin及归档历史查询/重放尚未执行。另采得一次GET/list收到200后unexpected-failed，native关联ambiguous，不能推EOF或把它确定为主失败根因。全部进程actualWait、四owned Z后wait0、七资源/private/runtime/desc、TCP双尾和输入不变已闭合，窗口释放。下一步仅离线定因及定向返修，仍需独立审查和fresh真实窗口；原CRUD03通过及其余未验范围不变。

Authority01 离线定位确认了§5的实际产品缺口：同身份检查恢复时，Workspace 原行为只恢复旧 Project 观察；Variables 未要求新的 Owner Get，就再次接受旧 active 上下文。新增真实 Session/Workspace/controller 组合反例首先实测 Owner Get 次数0（836eec），对应归档后仍可新建的主断言；并存 GET/list aborted 仍不据此归因。本域 controller 现以身份及 readGeneration 阻止旧观察恢复，Cookie owner 实际空闲后仅自动重读一次；等待或失败保持内容隐藏和操作禁用，失败可显式重读，成功新 archived 只读并保留历史原 intent。未改 shared Workspace/Session 或 browser/Go/网络门槛。新增 pending、500/403、迟到响应与新身份、实际组件按钮控制；21状态及51组件/Session控制通过，旧原红与首次返修漏掉 state.phase 顺序的红保留。Work 已有限独审接受，并另以真实 Session/OwnerGet 的 reader/stream cancel 挂起四控证实原实际尾前仍串行禁用、其后一次新 Owner Get 发布 archived。源审后唯一新 dist03 已构建通过（binary07 复用）；旧 dist02 不含此修正。下一候选 Authority02 仍需 fresh 真实窗口，不把纯控或构建当 Authority PASS。

Authority02 使用上述binary07/dist03获fresh窗口执行，whole FAIL：Go21.36s，监督器115.804s及外部actualWait1。实际失败移至helpers:463普通网络完成性gate，原PATCH/update返回409但PW报告failed且没有finished；同原请求native诊断唯一绑定，29采样均结算，EOF早于取消且262字节匹配合法identity Content-Length，原reader/stream取消与release完成。诊断不替代typed consumer、正式响应与SQL后验，不以旧spec510未再报证明整组权限已过；hooks_retired=false也原样保留。Node/所有handler/service/root/Go/driver实际尾、四owned先Z后wait0、七资源/private/runtime/desc与TCP双尾及输入不变已齐，窗口释放。必要四条闭集失败原件保留在本树.agent-state；下一步只离线核原mutation、当前权限/恢复和Session实际owner释放，再确定窄观测修正，不放宽普通finished或预算。

Authority02 后续限定离线归因：实际生产Session/Workspace/Variables UPDATE client/controller的十个控制通过，reader/outer取消尾各覆盖精确PROJECT_NOT_ACTIVE409两种已知未提交状态及wrong code/unknown/wrongXID；原Promise、busy与Cookie串行在实际尾前保持，尾后已知拒绝进入rejected，其余保持uncertain，草稿/原版本保留且没有receipt。未出现该路径产品红，不能回填真实409的未保留body。诊断新片段只旁观归档拒绝stage的实际dist单例原start Promise，绑定同document/request/XID/method/path/body/key/CSRF，在reload前实际采样并恢复hook；所有不完整请求另作有界闭集投影。Go后验在browser Fatal后也只检查实际已发的该拒绝key、目标原像及受影响项目seed事实，不接受未执行的后续矩阵。36个锁定PW变换/序列化与实际helper适配控制、30个实际Go方法/显式SQL替身控制和新安全投影控制均通过，相关旧事件/native/六API及Schema回归与strictTS通过；这些替身不证明真实PG结果。产品/dist03与普通finished/响应校验gate未改；独立窄审和对应新binary08尚待完成，没有新真实运行，原Authority02 FAIL保持。

诊断独审另发现两项真实反例：正式Account HTTPBoundary将Problem.instance投影为`/api/v1`，新诊断却比较完整endpoint；endDocument最后evaluate微任务在原250ms期限后完成时仍可能报退休。现只对齐该正式投影（原请求完整path仍独立核验），并在返回成功前重验同一个deadline和停止状态。两持久控制消费真实Boundary原JSON/XID，补未投影endpoint拒绝、249/250/251ms与停止中退休反例；原红保留，修后31项Go方法/安全投影、41项实际变换观察器及strictTS通过。尚待独立返修复验及新binary08，未启动真实资源；普通finished门、产品/dist03和Authority02原wholeFAIL不变。
