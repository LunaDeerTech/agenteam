# D27 Project Owner 模型设置 UI — scratch 规格草案 rev0

状态：仅工程草案，未成为正式卡，未授实现或执行。root 已接受的三 HTTP 能力与 Owner24 是功能前置；Audit UI 仍在真实验收，相关共享代码须待整卡接受、停止及唯一 writer 移交后才可实施。本稿不将“已读当前源码”当作该能力已验收。

## 1. 完整结果与开工门槛

推荐单张完整结果卡：当前 Human Owner 在 Project 设置中管理本 Project chat Providers/Models、Model-purpose Credential，并浏览安全的 System/Project 可用 chat 目录；所有写入具有明确结果、版本冲突和原请求恢复。两个菜单叶子遵守正式 project-settings §1/4：Providers 与可用模型。本 Project Models 的管理放在 Providers 叶子的内部“项目 Models”面板，不新增第三个设置叶子。

| 直接依赖 | 正式接受与本稿消费 | 仍不据此完成 |
| --- | --- | --- |
| Project Model 五读 HTTP | a0b012ce，`project-model-owner-read-http-verification.md`；Provider/Model list/get、七字段 available、当前逐页 Owner/Session、8 MiB完整表示 | 配置可调用、外部Provider网络或生产Resolver |
| Project Model Credential HTTP | e4b1b891，`project-model-credentials-http-verification.md`；Create/Update/Delete、metadata、被动lookup、默认根 | 材料GET、Credential列表、跨命令原子绑定 |
| Project Model 配置写 HTTP | cc850b22，`project-model-configuration-write-http-verification.md`；六写、found/receipt lookup、同默认根读写 | Agent rewrite、删除preview、Project selector |
| Owner UI 最终24 | `project-owner-workspace-ui-final24-verification.md`，14d9d717/末件0a939a7f；稳定ID、Resolve→Get、Session、导航确认、当前Owner与只读生命周期 | 创建/归档/恢复/删除UI或生命周期链完整绑定 |
| 既有 System/账号前端基础 | frontend README 对应完整验收；复用唯一Cookie owner、Ui/Dialog/SettingsShell、严格标量/Instant等纯工具 | 不复用System端点、admin谓词、按Provider分页或Impact状态机 |
| 后继 Audit UI 完整接受 | **待满足**。client/useSession/router/ProjectSettings/ProjectNav/相关旧测试及只读currentReadContext须在接受后冻结移交 | 当前审计离线PASS、单轮通过或只读源码不代替整卡结果 |

后端三卡已提供此UI所需全部公开协议，无新增迁移、公共contract、SQL或生产root修改理由。29个候选路径（28技术＋README末件）是同一设置结果的有限范围；不先做只有列表的界面卡。Provider与Credential是显式独立命令，Model共享这些稳定引用和当前Owner，不引入自动创建/补偿工作流。若实现确需额外共享路径，先报精确理由并修订正式卡，不凭本稿扩大。

开工前 root 须：接受正式SPEC；接受Audit整卡并给共享源停止快照；记录当时main与所需最小源码/工具输入、唯一写者；移交客户端/API→Session/state→UI→私有harness的可审冻结阶段。运行资源、Go/cache、资产窗另授。当前 ModelSelection 的资源窗口不属于本任务。

## 2. 路由、布局与用户行为

只追加以下两个合法Project设置后缀，其他Owner/Audit路由与默认基本信息保持：

- `/:username/:project_name/settings/model-providers`：Providers列表/详情/表单，以及内部“项目 Models”面板/表单。
- `/:username/:project_name/settings/available-models`：只读安全可用chat目录。

安全return仍按既有raw path闭集验证；query/hash、编码绕过、未知尾部不能成为返回目标。ProjectNav仅将这两个同Project合法后缀认作“设置”当前项，默认href仍general。只在当前Owner Get完成后显示页面与入口，不从路由username、admin角色、Resolve候选或旧缓存推导权限。三个列表均独立页态，默认25，可选50/100；不是HTTP缺省50的变更。

Providers面板显示name/protocol/enabled/稳定ID，正式详情显示本Project配置、version、时间与credential_ref；协议创建后只读。Models面板标题明确“项目 Models（全部 Providers）”，不要求先选一个Provider才能列表，不在请求附provider_id、不静默筛掉本页其他Provider的行。模型归属用稳定ProviderID；已加载同Project Provider名称可作附加显示，未知时显示ID，不自动扫全库或按行发N+1查询。创建Model从一个正式读取的本Project Provider选择（独立分页）或其详情进入；编辑Model的Provider/type只读，协议依据同Project Provider正式Get，不从名字猜测。

可用模型仅显示七字段与capabilities的安全子信息，标明System/Project来源。System行没有编辑按钮、System管理链接或补读完整System配置；本Project行也不能把目录七字段当作编辑详情，明确编辑时另走Project Get。Provider/Model配置enabled与可用目录是不同观察，禁用Provider仍可管理合法配置；保存不声称已连通、能调用或已用于Agent。

所有列表/详情/表单独立loading/empty/error；empty只来自完整合法200空页。已读页可保留为明确旧观察，不冒当前状态；cursor只在内存，不进URL/storage，翻前页重新读原cursor、刷新回首页、limit变更回首页。CURSOR_INVALID保条件与错误，用户明确从第一页重读，无自动扫页/重试。三列表cursor互不共用，首水位不解释为配置快照；无total或本地计算的全库总数。

复用双导航、SettingsShell和Ui组件。桌面表单正常换行，390窄屏单列/行堆叠，菜单Drawer与对话框键盘/Tab/Escape/焦点遵原框架。长ID/URL文本不成为自动可点击外链，不使用v-html。页面标题、字段错误与反馈可访问；动作禁用说明当前原因，不能只靠颜色。

## 3. 闭合数据、API与容量

新增Project专用read/write类型，不能把System Provider/Model强转为Project DTO。合法读取与当前写policy分开：HTTP可读取的非空配置对象或合法非空reasoning_efforts不能被客户端静默删掉/压缩。当前编辑器只支持§4的写集合，遇合法但超出该集合的既有值可完整安全只读并说明编辑限制，不能清空后提交。

| DTO | 必有闭集 |
| --- | --- |
| ProjectProvider | id,scope,input,version,created_at,updated_at；scope恰kind=project/project_id=本请求ID；input恰name/protocol/base_url/enabled/credential_ref/options |
| ProjectModel | id,provider_id,scope,input,version,created_at,updated_at；scope同上；input恰name/provider_model_id/type/enabled/parameters/request_overwrite/header_overwrite/capabilities，type=chat |
| AvailableChatModel | 恰id/provider_id/scope/name/provider_name/version/capabilities；scope为system或本Project，无第八字段，无endpoint/protocol/credential/原生型号/参数/时间 |
| 三种Page | 恰items非null数组、next_cursor显式null或合法token；≤请求limit≤100，ID不重复；空页cursor=null，非null cursor要求满页；Provider/Model按公开created_at/id严格倒序，目录不伪验不存在的created_at |
| ConfigurationReceipt | kind/resource_id/version/affected_references四字段；六kind；本已绑Project能力的成功affected_references必须"0" |
| ConfigurationLookup | found=false/receipt=null，或found=true/合法receipt；不是Secret observation |
| CredentialMetadata | credential_id/purpose=model/version三字段 |
| CredentialMutation | Metadata＋deleted；create版本1且false，update=expected+1且false，delete=expected+1且true |
| CredentialLookup | observed=false/result=null，或observed=true/合法Mutation；无in_progress，不是Owner Update三态 |

UUID用canonical v7；version/计数/容量用精确十进制字符串，不经JS Number。配置expected_version接受1..MaxInt64；首次递增溢出交原InvalidState，不能把配置界改成凭据的Max−1。Credential update/delete/lookup expected_version为1..9223372036854775806，metadata/result仍完整正int64。Instant沿既有UTC微秒及日历/created≤updated校验。严格Unicode/字节/rune规则，拒lone surrogate，不trim名称、base_url、型号或材料。

Capabilities十字段沿正式schema：四bool、四非null数组、context_length/max_output必有null或正int64字符串；parallel→tools，非reasoning时efforts空，modalities四值与structured两值闭集去重，effort safeToken≤32B、**无新业务数组总数上限**，两容量有值时max_output≤context_length。读投影保原顺序，不把写policy当读取损坏判据。

两新API模块共17个封闭方法，参数只收ProjectID、typed ID/input/query、AbortSignal或既有私有WriteOptions；不收任意URL、scope/actor/user/header/cap。所有输入在await前同步捕获/验证/冻结，调用者后改数组/对象不改变请求：

| 方法组 | 精确资源与请求 |
| --- | --- |
| 5读 | GET `/projects/{P}/model-providers`、其`/{provider}`、`/models`、其`/{model}`、`/available-chat-models`；均位于`/api/v1`。列表只cursor/limit，详情无query/body；不发key/CSRF |
| Provider三写 | POST collection `{input}`；PUT detail `{expected_version,input}`；DELETE detail `{expected_version}` |
| Model三写 | POST collection `{provider_id,input}`；PUT detail `{expected_version,input}`；DELETE detail `{expected_version,replacement}`，replacement必有null或非自身ModelID |
| 配置lookup | POST `/projects/{P}/model-commands/lookup`，body恰`{command:<六kind>}`；原key，不能发target/input/expected或selector kind |
| Credential metadata | GET `/projects/{P}/model-credentials/{C}`，无query/body/key/CSRF |
| Credential三写 | POST collection `{value}`；PUT detail `{expected_version,value}`；DELETE detail `{expected_version}` |
| Credential lookup | POST `/projects/{P}/model-credential-commands/lookup`；create恰`{kind:"create"}`，update/delete恰kind/credential_id/expected_version；不含value |

所有写及两个POST lookup均同源Cookie、CSRF、恰一原Idempotency-Key；无PATCH、无collection Credential GET、无HEAD客户端功能扩张。ID逐段校验后编码，ProjectID始终来自稳定当前绑定；query一次编码、无裸?、原32KiB/8192B cursor界。新动作须独立Project分类，不能进入System/admin或现Owner64KiB分支。

五读成功cap统一8,388,608B；配置receipt/lookup、Credential metadata/mutation/lookup成功cap1,024B；Problem保原600000B。配置七POST/PUT/DELETE请求cap1,048,576B；Credential create/update 409600B、delete/lookup 1024B，decoded value1..65536B，不能复用System 512KiB材料请求界。原其他端点cap不变，不给caller可调cap。

transport必须实际chunk累计/fatal UTF8/EOF/完整JSON/全对象refinement后一次发布不可变结果；不信Content-Length，不截断、丢坏末行、减limit或额外查详情救坏页。完整200 application/json/no-store及既有RequestID/Problem校验保持。body/reader.cancel、releaseLock与真实transport finally结束前不释放Cookie owner。cap是响应准入界，不宣称Go/浏览器全RSS或后端事务硬期限。

## 4. 表单与独立凭据命令

Provider支持name、两chat协议openai-chat-completions/anthropic-messages、base_url、enabled、nullable credential_ref，options固定{}。协议创建后只读；scope不由用户编辑。名称1..128 rune且≤512B，URL沿原http/https、有host、无userinfo/query/fragment/空port等endpoint规则，≤8192B且不规范化成另一串。

Model支持name、provider_model_id、enabled及capabilities；Provider创建时选择、之后只读；type始终chat。parameters/request_overwrite/header_overwrite固定{}，reasoning_efforts固定[]，无任意JSON/header/effort编辑器。chat输入允许text/image/file/vector子集，输出仅text子集；四bool沿正式约束；OpenAI structured可text/json_schema，Anthropic仅text。context_length/max_output可空且精确字符串。保原CAPABILITY_UNSUPPORTED和typed InvalidArgument边界，不修改后台policy或暗示这些声明经过实际模型调用验证。

Credential没有目录。入口仅为：Provider已绑定ref的安全metadata；用户明确输入同Project CredentialID并读取metadata；在本页创建新Credential后保留其安全ID。不得列出所有Credential、Get材料、用System credential或Project Variable/Secret替代。metadata与材料分区：已有材料不回显；新值以password输入，空输入表示不旋转，删除为独立确认动作。

**显式分步，禁止自动链式写：** 用户单独确认“创建凭据”后生成Credential intent并执行。只有严格Execute Mutation确认才清材料并把安全ref作为“已创建、尚未绑定”的本地候选；用户再明确选择/使用该ref到Provider草稿，再明确保存Provider。不会在Credential callback自动发Provider写，不自动创建默认Model，不做失败补偿删除。Provider失败保已确认Credential事实与ref，重读冲突后新Provider保存不能再次创建Credential。显示可复制的安全CredentialID以便随后手动metadata管理；key/材料/digest不可复制或展示。

rotate/delete仅针对当前明确读取的metadata ID/version，分别冻结新value或expected_version。Secret删除有引用或lease时保RESOURCE_BUSY；用户可另行编辑Provider解绑并确认保存后，再明确重读metadata、重新发新删除，不自动解绑/删Provider/释放lease。删除Provider只释放其引用，不删除Credential；历史ref已删除不应阻断已捕获原写重放。

Provider/Model配置与已读baseline无变化时禁保存；Credential无法从metadata判断新旧材料是否相同，仅空输入不旋转，不能把相同长度或旧version当作材料相同证明。原字段值保留且验证错误零网络。409版本/当前状态冲突保输入，由用户显式重读、核对、选择采用当前值后形成新意图；不换版本自动提交，不自动合并。已确认写后GET失败保写确认，只提供读重试，不重复写或以当前对象缺失取消历史确认。

## 5. 删除与引用边界

Provider删除前显示对象及影响说明并捕获当前version；**全Project Models某页为空、首项不属于该Provider或本地未见其Model都不能证明该Provider无Model**。没有按Provider查询或删除preview，因此不虚构影响数、不自动扫全部页；最终DeleteProvider由正式库判断，有任何Model按原INVALID_STATE拒绝。

Model删除确认捕获正式Get的目标ID/version。replacement必由用户明确选择“无替代”(null)或从独立分页的安全available目录选非自身System/本Project chat；不从目录补System Get，不用当前Models页证明目录资格。候选启用和引用竞态最终由DELETE重验；UI预选不冻结它们。

当前已接受Project能力：有任何Model reference返回DEPENDENCY_UNBOUND，无论候选是否提供；不能把没有preview、索引没有公开计数或目录可见说成“无引用”。用户说明为“仍被使用的模型暂不能在此删除”，保原记录、选择、输入和错误。无引用的合法删除实际affected_references=0；成功也不得宣称更新Agent/approval_model/Project Summary。不增加引用迁移、Agent编辑、selector/Summary override或后端替代adapter。

## 6. 唯一Session owner与App期状态

createSessionController在Audit接受后的实际参数末尾追加一个带默认值的ProjectModelSettings API依赖（内部可组合两新API），旧参数位置/省略调用不变。新增17个明确action及本域revision/封闭facade，使用原runAuthorized与私有Cookie/CSRF持有者；不导出任意callback，不重建fetch队列，不伪装personal/System/旧project-read。

当前predicate要求authenticated Human、完整user/session/identity epoch、operation generation与本域revision；普通Owner和admin Owner同样处理，每次正式API仍由后端当前Owner授权。System denied/admin变化不能授予或撤销本Project；局部403/404不能污染System。当前合法401会话错误、当前写CSRF失败沿既有全局失效链；旧identity/generation的晚401/403/finally不得清新Session。

App创建/provide唯一`useProjectModelSettings(auth,workspace)`，生命周期跨Session checking临时卸载。只读消费workspace已公开currentReadContext来绑定稳定ProjectID/完整identity/读代次，**它仅是UI读归属，不是Mutate grant**；当前detail同ID/phase=current时的lifecycle用于新编辑提示，后端重验Mutate。不要拿workspace.canSave/readOnly中的Owner基本资料冲突或旧草稿状态代替Model权限，也不改workspace自己的draft/intent。

状态区分读页/详情观察、用户draft、已确认write receipt、历史lookup observation、pending原intent。Provider配置、Model配置、Credential分别持有准确kind/目标；共同遵唯一实际owner。同一确认流内只允许一个待决提交，若另一类待决需先明确保留/查证/放弃，不能覆盖其原材料。已确认但未绑定的Credential事实独立于Provider draft，回退不撤销它。

同完整identity checking或Owner当前读取暂不可用：隐藏受保护数据和模态、停止新请求；保内存dirty/未决/已准备ref以便同identity恢复，旧只读页失效，重新获得同stable Project当前Get后才可显示编辑和发请求。真正user/session/CSRF epoch变化、Logout、失去本Project当前Owner归属、切换稳定Project或确认离开，清本域旧材料/观察/代次；无localStorage/history/URL恢复旧key。名字只用于路由定位，旧名复用到新Project不可带入任何草稿。不能以read generation刷新自动覆盖dirty草稿或原expected。

导航到另一个本组叶子、其他Project、System、history返回及Logout接入App既有聚合确认；取消保持URL/输入/原焦点，确认才放弃相应本地追踪。分别保System四用途和Summary的现有独立草稿，不默认同意其确认。不确定/dirty注册既有beforeunload约束；关闭或放弃不撤销服务端命令。焦点/await/nextTick回调须再核identity/Project/页面实例，不能抢新层焦点。

30秒可见截止与实际owner分开。已发请求即使取消/超时/卸载仍持owner直到fetch/body/cancel/finally真实结束，Logout/Session恢复/旧域操作均不能越过。只读取消提供显式重读，尾部结束只解锁；不自动发写或查证。后端五读/lookup2s、写30s保持，Model原ctx私有Unknown确认与Secret不自动确认的差异不能在UI抹平。

## 7. 写结果、Unknown与原请求恢复

明确保存才生成一次随机合法key，私有冻结完整identity/原CSRF/ProjectID/kind/target/provider/replacement/expected_version/完整typed输入及最终body bytes。表单后改、双击、query变化不能改变它；输入验证未派发为零网络。Credential材料只在password输入/必要私有内存中持有，发出后不在反馈/历史详情重新显示；严格确认、身份清理或明确放弃时清输入和引用，不承诺JS字符串物理擦除。

首次明确拒绝必须是完整合法Problem、此前无不确定，code/status及not_started/not_committed同时匹配下表；unknown无条件优先。这里的闭集是保守UI分类，未列情况仍不确定，不修改服务端错误语义。

| 首次响应来源 | 可解释为明确拒绝的原配对与UI行为 |
| --- | --- |
| 任一新配置/Credential写的严格输入边界 | 400 INVALID_ARGUMENT、413 PAYLOAD_TOO_LARGE、415 UNSUPPORTED_MEDIA_TYPE；保草稿，用户明确修正后再保存，不自动换key |
| 六配置写业务拒绝 | 409 VERSION_CONFLICT/INVALID_STATE/RESOURCE_BUSY/PROJECT_NOT_ACTIVE，422 CAPABILITY_UNSUPPORTED，503 DEPENDENCY_UNBOUND；保原错误及输入；版本/状态冲突须明确重读，引用未绑不能解释成已替换 |
| Credential三写业务拒绝 | 409 VERSION_CONFLICT/INVALID_STATE/RESOURCE_BUSY/PROJECT_NOT_ACTIVE；用途或目标404按下一行，不能用metadata证明材料同义 |
| 当前权限/目标拒绝 | 401 UNAUTHENTICATED/SESSION_REVOKED；403 FORBIDDEN/CSRF_FAILED/ORIGIN_DENIED；404 NOT_FOUND。仅合法完整Problem与上述明确commit态才说明本次被拒；显示/材料清理按§6与真实identity/current-Owner变化，404不可武断区分“目标删除”与“Project失权”，隐藏不可确认内容并经Owner显式重读再决定 |
| 原key发生异义 | 409 IDEMPOTENCY_KEY_REUSED，单独冲突态：禁原重放/自动新key/把别的receipt当本意图确认；既往未知不得降为未执行 |

503 DEPENDENCY_UNAVAILABLE、500 INTERNAL_ERROR、未知code/status或其它未列配对即便携not_started，也不在本稿首次拒绝闭集；保守待决并提供原恢复。只读和lookup的失败不产生新mutation intent；对既有intent只更新观察错误。任何一次之后的拒绝都不能抹掉此前不确定。

Unknown、超时、断线、body截断、错200/DTO/媒体、写后响应不能完整验证均保原intent不确定。此前不确定有粘性：后一次拒绝/未观察/新GET相等/资源404都不能改成“从未执行”。IDEMPOTENCY_KEY_REUSED禁止自动换key与原重放，保说明并只许明确放弃追踪；不拿另一body历史当成功。普通读Unknown只属读取失败，无写intent或自动lookup。

| 恢复请求 | 页面精确解释 |
| --- | --- |
| 配置lookup found=false | 本次未观察到，非回滚证明；保原intent |
| 配置lookup found=true | 六kind、receipt闭合并与已知原目标/版本可比字段校验；是该命令identity的历史观察，不验证原input semantic；**不作为严格Execute确认，不自动推进后续操作** |
| Credential lookup observed=false | 同样非回滚证明，不代表可安全换key |
| Credential lookup observed=true | 历史Mutation按kind/ref/version/deleted校验；不验证完整value，不清材料、不自动发Provider或宣布材料同义确认 |
| 用户显式原重放 | 同完整当前identity/原CSRF、原P/path/kind/key/body/版本/候选/材料完全一致，走原Execute；不先用当前GET/metadata/候选存在性阻断历史，仍由服务器授权/semantic判断 |

没有任意key查证控制台、自动轮询、自动retry、新key修复、查证后自动重放。只有原Execute的严格成功DTO才能确认对应写；create版本1，update/delete正确target及expected+1、kind/deleted/affected严格。损坏lookup只是本次观察失败，原写仍待决；不能造in_progress或借Model锁等待超时输出not found。

| 当前Project事实 | 配置六写 | Credential三写 | 两类lookup/metadata/配置读 |
| --- | --- | --- | --- |
| active且当前Owner/Session有效 | 允许新写/合法原重放，库终局判定 | 允许新写/合法原重放，Purpose/材料由库检查 | 正常只读当前授权 |
| archiving/archived且当前Owner有效 | 禁新草稿提交；**保已捕获原Execute重放**，库Read→历史可返回旧receipt | 禁新写且**禁Execute重放，连已有receipt也先Mutate拒绝**；保待决与显式lookup，不伪称已撤销 | Read允许；Credential observation仍不是材料同义确认 |
| deleting/未初始化/不存在/失Owner/会话撤销 | 不越权恢复 | 不越权恢复 | 保原gate错误，不从历史绕过；清/隐藏按§6 |

UI内存跨checking恢复不扩大为跨新Session恢复；真正新Session/CSRF后旧材料清除。后端同User新Session可能接受历史语义是后端能力，本卡不因此持久保存原请求。

## 8. 候选唯一写域

下表是**提请正式卡采纳的候选**，当前全只读；README须全部28技术接受后另授。frontend独占#1–23/#26–28；backend独占新Go #24–25；root独占Git/协调/资产窗，其他旧Go/JS helper及共享Ui/样式/HTTP/schema/生产后端不授写。

| # | 路径 | 最小作用 |
| --- | --- | --- |
| 1 | web/src/api/client.ts | 17封闭Project model操作、路径/请求/成功cap；旧分类不变 |
| 2 | 新web/src/api/project-models.ts | 五读＋六写＋lookup typed输入/完整Project投影 |
| 3 | 新web/src/api/project-model-credentials.ts | metadata/三写/lookup及Project材料界 |
| 4 | web/src/composables/useSession.ts | 追加默认依赖/17 action/本域revision/私有原intent/单owner |
| 5 | 新web/src/composables/useProjectModelSettings.ts | App期Coordinator、三类草稿/恢复、独立页态与确认 |
| 6 | web/src/App.vue | 唯一controller provide/dispose及既有聚合离开确认 |
| 7 | web/src/router/auth.ts | 仅两新Project合法return后缀 |
| 8 | web/src/router/index.ts | 两受保护Project child，无新全局navigation |
| 9 | web/src/views/projects/ProjectSettingsView.vue | 模型与Provider组/两叶子，general默认与audit位置保持 |
| 10 | web/src/components/layout/ProjectNav.vue | 同Project两后缀设置选中；原href/品牌保持 |
| 11 | 新web/src/views/projects/ProjectModelProvidersView.vue | Providers主面板、详情、内部Models面板入口与Provider删除 |
| 12 | 新web/src/views/projects/ProjectModelsPanel.vue | 全Project Model列表、Provider归属、创建/编辑/删除入口 |
| 13 | 新web/src/views/projects/ProjectAvailableModelsView.vue | 七字段安全只读目录 |
| 14 | 新web/src/views/projects/ProjectProviderEditor.vue | 受控Provider表单/凭据ref选择，无自动跨命令写 |
| 15 | 新web/src/views/projects/ProjectModelEditor.vue | chat及原policy支持的能力表单 |
| 16 | 新web/src/views/projects/ProjectModelCredentialEditor.vue | metadata、独立create/rotate/delete、write-only输入 |
| 17 | 新web/src/views/projects/ProjectModelDeleteDialog.vue | 捕获目标/版本、nullable候选、安全删除说明 |
| 18 | 新web/src/tests/project-models-client.spec.ts | 五读/配置请求、Project scope/目录/8MiB/lookup |
| 19 | 新web/src/tests/project-model-credentials-client.spec.ts | 400KiB/65536B/Max−1/Mutation/Observation |
| 20 | 新web/src/tests/project-model-settings-state.spec.ts | 真实Session/workspace factory、原请求/身份/尾部/归档差异 |
| 21 | 新web/src/tests/project-model-settings.spec.ts | App/router/组件与原聚合确认/焦点/分页 |
| 22 | web/src/tests/authentication.spec.ts | 两后缀合法及绕过拒绝代表 |
| 23 | web/src/tests/project-workspace.spec.ts | 原设置菜单期待的精确兼容，不改Owner写契约 |
| 24 | 新tests/account/project_owner_models_web_fixture_test.go | 真实root/私有dist/正式事实、安全body/有界私有控制与actualjoin |
| 25 | 新tests/account/project_owner_models_web_test.go | 六完整新top及证据键，旧top原样 |
| 26 | 新tests/account-captcha-web/project-owner-models.config.js | 六case/单worker/零retry/45s/固定工具与私有输出 |
| 27 | 新tests/account-captcha-web/e2e/project-owner-models.spec.ts | 六真实UI场景、同body/schema/client及8图 |
| 28 | web/src/tests/session.spec.ts | 本域与旧mutator单owner/安全错误隔离兼容 |
| 29 | docs/development/frontend/README.md | 技术接受后能力/边界/真实命令末件 |

workspace仅消费公开currentReadContext/detail，不新增workspace写域。System API/composables只读，纯类型/标量工具能原样复用才引用，不能导出System callback或调用其HTTP。默认不改ProjectGeneral/ProjectWorkspaceView、原account TestMain、test-objects/security/postgres脚本、旧browser spec、公共fixture、schema/contract/store/root/迁移/锁文件。若实际旧测试只有菜单期待变化，限#23；其它新的必要修改交root精确评估。

## 9. 必要验收与执行前提

正式实施者先读design/vue-development/vue-testing-best-practices；Go harness读Go技能，独立负责人读verification，真实browser再读可用Playwright技能。此稿不执行下面命令，也不发安装/网络研究；工具和锁沿当时接受环境，正式执行前冻结实际必要输入及原件。

离线：新增两个API、state、App/router组合和#22/#23/#28；完整`npm run check --prefix web`及私有outDir生产build。Go两新源只作精确integration/race compile/vet与精确six-top discovery，再由root另授真实窗；编译/list不是业务通过。不要机械复制System/Audit大图；只导入既有受接受实际图与真正新增文件/运行时浏览器imports，图/filename sets变化须明确处理。

pure/controlled必须覆盖：17个request分类；全Project models query拒provider_id；三个Page和详情正确scope/ID/最后项/完整EOF；七字段目录敏感字段和值canary拒绝且无System请求；五读合法>600000B、8MiB边界/cap+1/实际reader取消join，合法大reasoning数组与非法最后项；1KiB写/lookup cap；Credential最大decoded65536B含末字节/合法最坏转义/raw cap、不得泄漏；MaxInt64配置与CredentialMax−1不同界；typed输入已冻结后caller改对象无效；两协议及所有当前写policy反例；两个lookup union不可互用；Model/Secret/Owner三种恢复差别；首次拒绝与未知粘性、key-reused、坏写receipt、lookup异常、已确认后GET失败；same-identity checking、变Session/CSRF、换Project/旧名复用、晚401/403/ finally、真实transport/body/cancel hold使所有旧操作不能抢owner；App旧Selection/Summary两个未决域与新域聚合确认互不默认清除。

拟六个新top，每轮一个精确锚定名称：

| top | 真实代表与最小证据 |
| --- | --- |
| TestAccountProjectOwnerModelsWebConfigurationLifecycle | 正常Owner经默认root创建/改Provider与chat Model、两协议/type/provider不可变、enabled与目录重读、空Provider删除及有Model拒绝；配置事实/receipt/引用与Audit按正式生产者 |
| TestAccountProjectOwnerModelsWebCredentialLifecycle | create→仅ref准备→显式Provider绑定、metadata/rotate、被引用delete拒绝、显式解绑后delete、Credential已确认但Provider失败的保留与后续Provider单步重试 |
| TestAccountProjectOwnerModelsWebOriginalRecovery | 严格目标私有proxy丢/截已完成安全响应，配置与Credential各至少一个真实原请求恢复；lookup仅观察且零隐式write；至少一次资源已删后的原DELETE重放，不以新GET修复；原请求bytes等值只报布尔/长度，不公开body/key/material或其digest |
| TestAccountProjectOwnerModelsWebReadAndPagination | ≥26Providers、跨≥2Providers的≥26Project Models、混System/Project的≥26available；逐页当前权限、无provider_id、无System补读、详情、disabled配置与目录区别；坏页/大页的受控证据不冒真实PG大页 |
| TestAccountProjectOwnerModelsWebAuthorityAndIdentity | 非admin Owner成功、另一admin/另一Owner拒绝；会话checking/恢复、当前revocation与项目切换、辅助archived读/新写拒绝；配置原重放与Credential仅lookup差别；带reference原DEPENDENCY_UNBOUND无Agent rewrite |
| TestAccountProjectOwnerModelsWebNavigationAndLayouts | Providers/Models面板/available实际导航、dirty/待决/部分成功离开确认、键盘/焦点；浅深×1440x900/390x844×正常/减少动效共8图（凭据输入已清空且模态安全退出后） |

root/fixtures须使用正式Account/Project/Secret/Model API和当前同一Authority。Project持久创建可沿已接受私有Skills fixture；辅助归档状态与私有隔离reference准备均准确标明，不证明生产Skills、完整归档执行链或Agent adapter。Model引用负例按原完整scope/target谓词设置及恢复，不能改生产consumer或让假adapter成功。控制代理限本轮独立Project、精确method/path/请求token和已注册请求；outerhandler实际终局后才记join，body读完、浏览器EOF、pending请求与事务提交分别观察。所有server/handler/child/控制goroutine/finally必须注册并actual join；无宽泛截获、网络I/O绕过或重复强制终局。

真实schema对安全原response bytes及实际method/path/status/Content-Type/Content-Length/X-Request-ID/目标/run/input绑定，以已固定标准Python/jsonschema本地refs核；同原body还经原生browser内的新client解析，不用手造重编码样本。日志/截图/trace/video/证据不得采集Credential材料、原key/body、Cookie/CSRF、登录材料、DB协议帧；合成配置/capability仅在可公开fixture安全集合内。禁止在凭据输入/原材料未清时截图。

共享client/useSession/App改变要求全旧纯测试及针对性真实兼容。新域之外至少覆盖实际受影响的旧auth lifecycle/revocation、Owner identity/edit、Audit authority/navigation、System Provider credential recovery、System Model recovery、System Selection recovery、System Summary authority/navigation；个人主题与旧只读域按实际App/owner差量取最小代表。**正式SPEC前补精确现存selector与复用理由**；不机械重跑System整套，也不以旧PASS覆盖已变shared输入。每轮仍分开冻结/实际退出/复核，旧body/预算不改。

新case45s、workers1/retries0；每Go top120s含cleanup、包6m、TCP尾75s、fresh≥5GiB。每轮实际4容器＋3网络7个ID，direct/adopted actualwait、watchdogjoin、两次精确资源不存在/owned runtime/进程与TCP观察按旧正式driver验；现有root/innerjoin限制不声称修复。资源串行，失败保存、完整退休后STOP不自动重跑。新Go私有dist；旧组若读global dist，root独占交换/备份/最后reader退休后恢复，禁止并行改资产。单独独验至少配置/凭据恢复一个组合与当前权限/归档一个不同构造；审查者未参与实现，限定复用已验HTTP语义，不为UI重跑全后端套件。

## 10. 交付范围与待root决定的工程项

本草案已明确两叶子/29候选路径/17闭合操作与各事实来源，正常、异常、权限、幂等、取消、版本、恢复与当前未绑定边界。没有新增业务事件；UI不发布Audit/Event，六配置命令及Secret事务事实仍由既有后端负责。

正式SPEC前尚需工程补齐而非用户产品确认：Audit整卡停止交接；当前shared签名/新增依赖位置与具体最小旧回归selector；私有harness控制协议及可审预算分配；正式卡路径/基线和固定来源。这些不影响本稿单卡可行结论，也不预先授实现或资源。

本卡不包括Project创建/生命周期/Owner转移、Agent配置或引用替代、非chat Project模型、生产调用/Resolver root/Invocations、平台selector、Project Summary override/默认复制、凭据目录或明文读取、连接测试、Provider discovery、生产SPA发布与E01。三硬停止与Jina停止保持，完整D09/D26/D27/D28与E01不因该卡或此草案完成。
