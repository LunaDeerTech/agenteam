# 新 Vue 模型接入与平台模型用途

状态：2026-10-17 原包起点 `97ac04e3`；当前从 `0c182e08` 实施统一总结模型。记忆与会议共用真实单配置，四用途整组保存；此前五用途、两资源的页面调整及验证保留为历史，不替代本次验证。本卡是本包共同合同。仅系统管理员配置系统模型；项目模型、Agent、Executor/Runner与既有STOP不扩大。允许按新页面重做接口，不新增旧UI或路由迁移；当前公网实例、用户凭据和外部服务不用于开发验收。用户明确本包完成后不部署公网，交付新增页面功能截图，先上传图床再提供公网图片URL。

## 1. 页面与完整流程

系统“模型配置”分类下保留“模型接入”和“平台模型用途”两个入口，系统11项改为10项，正式页面由40叶改为39叶；供应商动态详情仍属于模型接入。系统与项目设置中的共享分类名均为“模型配置”；名称调整不改变既有URL或项目功能范围。删除旧 `/system/providers` 入口，不保留兼容跳转。

| 路由 | 任务与载体 |
| --- | --- |
| `/system/models` | “模型接入”先呈现供应商列表；页头右侧新建供应商，配置在弹窗中。 |
| `/system/models/:providerId` | 独立供应商详情，以模型列表为主体；返回目录恢复列表位置/焦点。供应商设置、凭据配置、手工添加/编辑模型、发现选择导入和测试结果使用对应弹窗，不纵向堆叠全部表单。 |
| `/system/model-selection` | 独立的平台用途页；向量、总结、重排、图像四用途平级，共用一个配置、版本与保存动作。“重新获取”位于页面标题行右侧，保存仍贴近表单；候选与已选模型显示为“供应商名称/模型ID”。 |

完整出口是供应商与凭据配置→模型手工添加或发现勾选导入→显式真实测试→合法用途可选。复用SettingsShell、SystemList、UiDialog和共享页头动作；目录使用宽正文，标题无需装饰描述。页级动作在标题右侧，表单保存/取消贴近字段。宽窄、空态、读取失败、权限失效、409、Unknown、取消及返回焦点在对应任务中闭环。

## 2. 既有接口与数据

以下路径均在 `/api/v1` 下；精确字段、nullable和枚举沿[model-system OpenAPI](../../../api/openapi/model-system.json)，后端唯一维护schema。普通读取200，配置命令200返回receipt，不返回伪造的当前投影。所有写与POST lookup使用当前System Session、CSRF和原命令Idempotency-Key；业务事务再次鉴权。version及receipt计数为规范十进制字符串。

| 资源 | 方法与正文 / 返回 |
| --- | --- |
| `/system/model-providers`、`/{id}` | GET列表或对象；POST `{input}`；PUT对象 `{expected_version,input}`；DELETE对象 `{expected_version}`。对象为`{id,input,version,created_at,updated_at}`。 |
| Provider input | `{name,protocol,base_url,enabled,credential_ref:ID\|null,options:{}}`；protocol创建后不可改。新UI仅创建/编辑`openai-compatible`供应商family，同一供应商共享base_url与credential，允许同时添加chat和embedding模型。旧五种协议记录不迁移，在新UI只读；不再要求同一服务按模型类型重复配置供应商。 |
| `/system/models`、`/{id}` | GET列表必须`provider_id`；POST `{provider_id,input}`；PUT对象 `{expected_version,input}`；DELETE对象 `{expected_version,replacement:ID\|null}`。对象为`{id,provider_id,input,version,created_at,updated_at}`。 |
| Model input | `{name,provider_model_id,type,enabled,parameters:{},request_overwrite:{},header_overwrite:{},capabilities:{…}}`；新family按每个模型的type选择既有chat或embedding adapter，能力必须通过真实校验，不能由模型名字猜推理、工具、上下文或价格。capabilities各字段严格沿schema，未知容量保持null。 |
| `/system/model-credentials`、`/{id}` | POST `{value}`；GET对象只读`{credential_id,purpose:'model',version}`；PUT `{expected_version,value}`；DELETE `{expected_version}`。写结果`{credential_id,purpose,version,deleted}`，材料从不回显。 |
| 配置receipt | `{kind,resource_id,version,affected_references}`，kind使用`provider.create`、`model.update`等现有点号枚举。Provider/Model/用途各有独立版本。 |
| 配置lookup | POST `/system/model-commands/lookup`，body `{command}`、原key→`{found,receipt}`；Secret用 `/system/model-credential-commands/lookup`，create `{kind:'create'}`，update/delete另带`credential_id,expected_version`→`{observed,result}`。 |

列表使用真实cursor/limit（后端1..100、默认50；新UI默认10，可选5/10/20/50），页长或供应商变化清cursor历史。不做当前页过滤冒充全目录筛选。用途候选按供应商分页和选中供应商懒加载模型，不自动无界遍历所有供应商。

凭据弹窗空输入表示不发替换请求，非空才创建/替换；删除为显式动作。凭据写入和Provider关联是两个独立命令：已确认的凭据不能因关联失败再创建，显示实际部分完成并只恢复未完成步骤。取消不回滚已确认写入；解除关联和删除Secret不是同一事实，不能误删被其他配置引用的凭据。Provider删除不自动级联模型或独立Secret。

## 3. 用途、删除影响与恢复

唯一 `GET /system/model-selection`→`{id,version,configured:null|{embedding,summary,reranker,image}}`；PUT发送`{id,expected_version,embedding,summary,reranker,image}`。四字段均须显式提供：embedding与summary必填，reranker/image可null。embedding须为当前启用的System embedding模型；summary须为当前启用的System chat模型、供应商亦启用，并具备`json_schema`能力。候选按模型type、能力及当前启用状态判断，同一openai-compatible供应商可分别提供chat与embedding模型；未配置null不等于读取失败。

“总结模型”同时供Memory提取、整理、reflect与Meeting摘要/首轮标题使用。两类消费者统一解析`SummarySelector='summary'`，平台引用role为`summary`，各自的业务Purpose、提示词、用量归属和生命周期保持。系统只存一个用途配置、一个id/version和`model.selection.update`回执；删除旧独立meeting-summary HTTP/command/store/owner/锁，旧路由返回404，旧memory字段严格拒绝。Project既有`project_summary`未绑定预留契约不属第二个System配置，本轮不删除或启用。

`json_schema`是共享模型的能力门；Memory沿既有结构化输出调用，Meeting本轮只切selector，仍以`ResponseFormat=text`、空Tools生成并解析固定JSON envelope，不额外重做会议生成流程。

页面维护一份四字段草稿，单次PUT原子保存整组，取消或无改动不写入；不再串行写两个资源，也不存在用途保存的部分成功。409保留草稿，显式重读并采用最新版本；Unknown冻结原key/body/version，lookup仅观察，须由原Execute返回匹配receipt才确认。已确认成功后的GET失败仅重读，不再次PUT；放弃未确认草稿不宣称服务端已回滚。模型名称统一显示为“供应商名称/模型ID”。

00149仅重置System用途配置及其平台引用、移除旧独立配置表，再初始化version1的未配置单例；不自动选择任一旧模型、不保留旧接口兼容。账号、SMTP、供应商/模型/凭据及业务Purpose不因此重置。本轮仅在新建隔离库验证，现有公开实例保持。

模型删除先读 `/system/models/{id}/deletion-impact`，显示真实引用组、替代要求与`reference_adapter_unbound`等阻断。总结模型仅有共享平台引用，替代仍须为启用的System chat且支持`json_schema`，同事务替换该引用并推进整组版本；新调用使用新选择，已接受调用保持原snapshot。影响读取不授予删除，最终命令仍重验当前引用、替代模型和版本；不因旧计数为0假定删除安全，不扩大未绑定引用适配器。Provider非空、Secret被引用等失败保留真实反馈，不自动级联或解除引用。

既有配置/Secret的lookup只观察历史身份，`found/observed=true`不能证明本次body匹配，false也不证明未提交或可换key。Unknown冻结原method/path/key/body/version；用户显式恢复时必须由原Execute严格核对原材料并返回匹配receipt后才终结，不仅凭lookup显示成功。lookup可以辅助核对，不能代原Execute。Secret原材料仅留该命令owner内存，确认、放弃或身份失效后清理；不写本地存储、日志或URL。已确认receipt后的GET失败仅重读，不再次执行成功写入。409保留非秘密草稿并显式采用新版本；Secret已知未提交（not_started或not_committed）的409须清秘密输入，显式读取新凭据版本后重新填写，不能沿用Unknown的原材料恢复；权限失效清本包数据，不伪造Session注销。

## 4. 新增发现与有界真实测试

新增能力由后端真实System管理调用owner提供，不能借Project Invocation身份读取凭据。本包只开放`openai-compatible`供应商family，以Model.type派发既有OpenAI chat与embeddings两个运行adapter；family不是第三种运行协议，配置校验、snapshot/Profile与管理测试须一致解析。旧协议记录只读，不恢复Jina/Image等原STOP。保存配置、选择模型和打开页面均不自动出网。

| 接口 | 严格输入与输出 |
| --- | --- |
| GET `/system/model-capabilities` | `{protocols:[{protocol:"openai-compatible",model_types:["chat","embedding"],discovery:true,test:true}]}`；新建入口只有这一个family，不显示两套供应商协议选择。 |
| POST `/system/model-providers/{id}/discovery` | `{expected_version}`→`{operation_id,provider_id,provider_version,outcome,reason,items:[{provider_model_id}],duration_ms,created_at}`；只从已保存供应商的固定`models`路径获取ID，不据ID猜能力。失败或未知时items为空。 |
| POST `/system/models/{id}/test` | `{expected_version,provider_version}`→`{operation_id,provider_id,model_id,model_version,provider_version,outcome,reason,duration_ms,created_at}`；使用已保存模型/凭据和固定最小协议payload，不接自由prompt、任意URL或客户端Secret。 |

outcome为`succeeded|failed|unknown`；成功reason为null。安全reason闭集为`unsupported_protocol,configuration_changed,credential_unavailable,policy_rejected,network_failed,tls_failed,timeout,cancelled,upstream_rejected,invalid_response,response_limit,interrupted`。duration_ms为0..20000的JSON整数，created_at沿正式UTC时间。返回不含供应商原文、认证头、响应内容或Secret；一次成功不标记供应商持续在线，不替用户保证后续调用可用。

两POST均要求System/CSRF/Idempotency-Key并持久绑定原身份、body和版本。原key/body完成重放返回同一结果，**不再次出网**；未决为`COMMIT_UNKNOWN/unknown`、`retry_hint=replay`，无额外lookup。有可靠原进程退出事实时可收敛`unknown/interrupted`，不猜上游未执行。重试一次外部调用必须是用户明确发起的新操作，不能因响应丢失自动换key。

主操作20秒，真实I/O结束并join后有界5秒持久/lease收尾；全局最多4个、不排队，满额`RESOURCE_BUSY`。发现响应最多2MiB、1000个ID，超限明确失败而非假完整目录；测试响应最多1MiB。只允许已保存配置派生路径，拒绝redirect；复用统一出站DNS/当前规则/代理/TLS，不读取环境代理兜底。实际网络期间当前Session/admin与Provider/Model/Secret版本由共享锁保护；关闭/取消需实际停止并join，界面不把取消请求当上游未执行证明。

同一供应商发现一次，勾选导入时逐模型明确chat或embedding类型及合法能力，再使用现有`model.create`逐项串行，单批最多100项；每项独立key/body/receipt并显示已成功、失败、未开始或Unknown。不重复成功项，不把部分完成写成整体回滚；取消仅停止未开始项。导入前明确合法类型和能力配置，不从发现结果制造上下文、工具或推理支持。

## 5. 实施与验收边界

account_backend唯一维护后端/API/schema及00147、00148、本轮00149；proxy_copy唯一维护新Vue；本卡、蓝图和当前摘要由account_contract维护，root负责Git。测试、视觉与安全由root指定owner独立分工，不把作者检查写成独审。

统一总结模型当前集成`846fe701`（backend `b87ed09b`、Vue `0852da63`、harness `5edd81b1`），仍待动态接受：

- 作者contract、Model分轮及Memory/Meeting单测通过，tests/model integration编译通过；源码静审缺口与00149 summary CHECK独立复核已关闭。原route测试、schema环境与既有media fixture失配保留，修后仅按受影响结果记录，不称首次全绿。
- Vue受影响18项单测分轮通过、type/build通过；导入结果裸模型ID的P2已改用共享斜杠名称，局部type及原受控case复验通过。有限独审覆盖四用途单GET/PUT、Unknown、390长ID及焦点；这些使用受控输入，不代真实API或数据库验证。
- 真实API/UI、作者PG与Memory→Meeting实际共用选择、独立CAS探针尚待；runtime唯一App编译/运行与后端自有测试fixture分别推进，资源未报告清退前不写已完成。

两张当前示例图已实看、上传200并匿名下载核对字节：[四用途页面](https://img.lunadeer.cn/i/2026/10/17/6ad2ed1e4c216.png)、[模型选择器](https://img.lunadeer.cn/i/2026/10/17/6ad2ed1e40ef9.png)。仅用于页面交付，不代表公网部署；用户公开实例保持。

### 此前模型接入与页面调整结果

以下结果均属此前冻结输入，不能据此宣称新selector已完成或已部署。

首链为供应商/凭据→模型→测试→用途保存与刷新；并检查部分完成、权限/CSRF、409、Unknown原请求终结、引用阻断、能力不支持与取消。发现/test使用任务拥有的模拟供应商和真实App/Secret/出站实现，不用用户现有凭据或外部供应商实发。页面核桌面/窄屏、键盘/modal回焦、返回列表与部分结果；现系统导航10项需要新检查，旧11项通过事实保留为历史。新family已完成下述真实链；先前冻结输入的权限/生命周期等未变结果仅限定复用，不把旧协议整页或旧配置链当作新family验收。

最终主树集成`65e61bd4`，生产输入backend `e48dceaf`与Vue `2464e966`，运行harness为`c798f9b4`；结果按各自冻结输入和分轮范围记录：

- r3严格CA管理API通过发现、chat测试、同key终态重放零重复、429安全失败及成功后丢浏览器响应的原key恢复；独立权限probe通过普通403、无CSRF403、异body409、撤销Session401，对供应商调用增量均为0，自有对象和Session已清理。
- 真实Vue两case分轮通过：同供应商chat/embedding、凭据与配置/手动编辑、混合发现导入、首项受理后停止未开始项并继续、两adapter测试、429和丢成功响应后同key恢复；用途保存刷新、两处引用的取消及替换删除均有持久状态核对。各case finally logout204→401、浏览器退出。用途有效通过所在外层因目录定位器失败exit1，目录限定修复后单跑通过，不写成初次整轮全绿。
- 最后作者组合门采用生产`e48dceaf`/取消测试`271b68`、PG17.8，实际PASS11.06s/外链exit0：同family两type与SQL约束、引用删除409、带凭据认证代理真实调用及原key零重复、规则拒绝零hit；真实HTTP取消后caller/handler/socket join，持久cancelled可原key恢复，Model/Proxy租约归零，删除模型/供应商/凭据后历史receipt仍返回。此为作者组合检查，非新增race或独立全矩阵；其中embedding创建不单独代表调用，实际embedding调用由前述API/UI链证明。
- Vue最后13项受影响单测、格式/type/build通过；之前17文件86unit及字典定向仅按原输入复用。秘密材料closure与known Secret 409两合法状态处理已修并独审关闭；有限视觉复核包括窄屏横溢、列表滚动330→330/回焦、409与Unknown。

原失败保留：r2启动前依赖失败尚未归因；管理harness初次缺正确CA，补CA后未降TLS；UI关闭按钮定位器歧义经限定修改后通过；旧PG16 setup失败未进入业务。没有使用用户凭据或外部供应商，不宣称所有供应商协议、全部项目/Agent执行或公网已验。

r3原链actual exit0，App/TLS/供应商join，4容器+4网络及4专属目录清零；风险fixture另6资源/3目录亦清零。Vue5281 TERM后原wait143且无进程/监听，5282/5283无监听；5288仅确认进程/监听已无，旧launcher wait不可恢复。两个测试二进制按原SHA删除，必要私有证据保持0700/0600、不入Git；既有公开服务和用户数据未操作。

前轮四张示例图均由视觉owner上传并匿名下载核对；旧分区、五用途单保存及名称调整图仅保留对应历史输入，不作为本轮四用途页面证明。截图证明页面呈现，不替代真实API验收，也不代表公网已更新：

| 页面 | 原图 |
| --- | --- |
| 供应商目录 | [查看原图](https://img.lunadeer.cn/i/2026/10/17/6ad2d9385b204.png) |
| 同供应商聊天与嵌入模型 | [查看原图](https://img.lunadeer.cn/i/2026/10/17/6ad2d9385c771.png) |
| 发现与导入 | [查看原图](https://img.lunadeer.cn/i/2026/10/17/6ad2d938aa5e8.png) |
| 前轮深色平台用途（旧分区） | [查看原图](https://img.lunadeer.cn/i/2026/10/17/6ad2d938569be.png) |

五用途单保存增量以`09efa870`为起点，仅修改页面和对应单测，后端不变。定向15/15、vue-tsc、Vite build及限定格式通过；既有正式spec适配通过语法/格式与列表加载，未重跑真实PG。独审4个受控实际Vue场景通过：双dirty单保存串行、第二阶段400后仅续写会议摘要、Unknown同key/body恢复且不重发成功四用途、四用途未配置时仅保存会议摘要。1440/390浅深、焦点与reduced-motion实看无返修；这是受控UI验证，不新增真实API或公网结论。

临时5314经精确进程身份核对后TERM，原launcher actual143，进程消失且端口无监听，浏览器已关闭。新[五用途单保存深色原图](https://img.lunadeer.cn/i/2026/10/17/6ad2e10dcc7bb.png)为1440×960示例数据，已实看、上传200、匿名下载200且字节一致；本轮仅交截图，不部署公网。

后续名称与页头调整起点`e36992d8`：系统/项目共享分类“模型配置”、页面及导航“模型接入”，用途“重新获取”置于标题右侧，URL/API与保存逻辑未改。作者15项受影响单测、type/build和限定格式通过；独审1440深色/390浅色两场景通过页头同行、名称/返回、键盘与重读两个GET各一次/零写入，未重跑保存、Unknown或真实PG。当前[用途页头与命名示例原图](https://img.lunadeer.cn/i/2026/10/17/6ad2e47c1752d.png)已实看、上传及匿名下载200且字节一致；此前用途截图仅保留对应输入，不作为当前按钮位置依据。临时5314精确TERM后原launcher actual143、进程消失且无监听/子进程；本轮不部署公网。

精确恢复状态与待办见[当前状态](../../../.agent-state/current.md)。
