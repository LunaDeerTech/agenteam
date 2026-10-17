# 新 Vue 模型目录与平台模型用途

状态：2026-10-17 已授权实施，起点 `97ac04e3`。本卡是本包共同合同，当前为实施中，不以旧D09或旧前端验收替代新组合结果。仅系统管理员配置系统模型；项目模型、Agent、Executor/Runner与既有STOP不扩大。允许按新页面重做接口，不新增旧UI或路由迁移；当前公网实例、用户凭据和外部服务不用于开发验收。

## 1. 页面与完整流程

系统导航仅保留“模型目录”和“平台模型用途”两个入口，系统11项改为10项，正式页面由40叶改为39叶；供应商动态详情仍属于模型目录。删除旧 `/system/providers` 入口，不保留兼容跳转。

| 路由 | 任务与载体 |
| --- | --- |
| `/system/models` | 先呈现供应商列表；页头右侧新建供应商，配置在弹窗中。 |
| `/system/models/:providerId` | 独立供应商详情，以模型列表为主体；返回目录恢复列表位置/焦点。供应商设置、凭据配置、手工添加/编辑模型、发现选择导入和测试结果使用对应弹窗，不纵向堆叠全部表单。 |
| `/system/model-selection` | 独立的平台用途页；平台四用途与Meeting Summary分区，独立草稿、版本和保存，不因共页合成事务。 |

完整出口是供应商与凭据配置→模型手工添加或发现勾选导入→显式真实测试→合法用途可选。复用SettingsShell、SystemList、UiDialog和共享页头动作；目录使用宽正文，标题无需装饰描述。页级动作在标题右侧，表单保存/取消贴近字段。宽窄、空态、读取失败、权限失效、409、Unknown、取消及返回焦点在对应任务中闭环。

## 2. 既有接口与数据

以下路径均在 `/api/v1` 下；精确字段、nullable和枚举沿[model-system OpenAPI](../../../api/openapi/model-system.json)，后端唯一维护schema。普通读取200，配置命令200返回receipt，不返回伪造的当前投影。所有写与POST lookup使用当前System Session、CSRF和原命令Idempotency-Key；业务事务再次鉴权。version及receipt计数为规范十进制字符串。

| 资源 | 方法与正文 / 返回 |
| --- | --- |
| `/system/model-providers`、`/{id}` | GET列表或对象；POST `{input}`；PUT对象 `{expected_version,input}`；DELETE对象 `{expected_version}`。对象为`{id,input,version,created_at,updated_at}`。 |
| Provider input | `{name,protocol,base_url,enabled,credential_ref:ID\|null,options:{}}`；protocol创建后不可改。新UI仅允许创建/编辑真实支持的OpenAI chat或embedding供应商；既存其他协议记录只读显示不支持状态，不扩旧库枚举能力。 |
| `/system/models`、`/{id}` | GET列表必须`provider_id`；POST `{provider_id,input}`；PUT对象 `{expected_version,input}`；DELETE对象 `{expected_version,replacement:ID\|null}`。对象为`{id,provider_id,input,version,created_at,updated_at}`。 |
| Model input | `{name,provider_model_id,type,enabled,parameters:{},request_overwrite:{},header_overwrite:{},capabilities:{…}}`；协议/类型及能力必须通过真实校验，不能由模型名字猜推理、工具、上下文或价格。capabilities各字段严格沿schema，未知容量保持null。 |
| `/system/model-credentials`、`/{id}` | POST `{value}`；GET对象只读`{credential_id,purpose:'model',version}`；PUT `{expected_version,value}`；DELETE `{expected_version}`。写结果`{credential_id,purpose,version,deleted}`，材料从不回显。 |
| 配置receipt | `{kind,resource_id,version,affected_references}`，kind使用`provider.create`、`model.update`等现有点号枚举。Provider/Model/用途各有独立版本。 |
| 配置lookup | POST `/system/model-commands/lookup`，body `{command}`、原key→`{found,receipt}`；Secret用 `/system/model-credential-commands/lookup`，create `{kind:'create'}`，update/delete另带`credential_id,expected_version`→`{observed,result}`。 |

列表使用真实cursor/limit（后端1..100、默认50；新UI默认10，可选5/10/20/50），页长或供应商变化清cursor历史。不做当前页过滤冒充全目录筛选。用途候选按供应商分页和选中供应商懒加载模型，不自动无界遍历所有供应商。

凭据弹窗空输入表示不发替换请求，非空才创建/替换；删除为显式动作。凭据写入和Provider关联是两个独立命令：已确认的凭据不能因关联失败再创建，显示实际部分完成并只恢复未完成步骤。取消不回滚已确认写入；解除关联和删除Secret不是同一事实，不能误删被其他配置引用的凭据。Provider删除不自动级联模型或独立Secret。

## 3. 用途、删除影响与恢复

`GET /system/model-selection`→`{id,version,configured:null|{embedding,memory,reranker,image}}`；PUT发送`{id,expected_version,embedding,memory,reranker,image}`。embedding与memory必填，reranker/image可null；严格匹配对应类型及当前启用/可用状态。未配置null不等于读取失败。

Meeting Summary沿独立 `/system/model-selection/meeting-summary` GET/PUT：`{id,version,model:ID|null}`与`{id,expected_version,model:ID}`。其版本和保存不与四用途互推，也不能以另一分区保存清除本区草稿；恢复方式与该资源实际receipt契约一致。

模型删除先读 `/system/models/{id}/deletion-impact`，显示真实引用组、替代要求与`reference_adapter_unbound`等阻断。影响读取不授予删除，最终命令仍重验当前引用、替代模型和版本；不因旧计数为0假定删除安全，不扩大未绑定引用适配器。Provider非空、Secret被引用等失败保留真实反馈，不自动级联或解除引用。

既有配置/Secret的lookup只观察历史身份，`found/observed=true`不能证明本次body匹配，false也不证明未提交或可换key。Unknown冻结原method/path/key/body/version；用户显式恢复时必须由原Execute严格核对原材料并返回匹配receipt后才终结，不仅凭lookup显示成功。lookup可以辅助核对，不能代原Execute。Secret原材料仅留该命令owner内存，确认、放弃或身份失效后清理；不写本地存储、日志或URL。已确认receipt后的GET失败仅重读，不再次执行成功写入。409保留非秘密草稿并显式采用新版本；权限失效清本包数据，不伪造Session注销。

## 4. 新增发现与有界真实测试

新增能力由后端真实System管理调用owner提供，不能借Project Invocation身份读取凭据。仅OpenAI chat与OpenAI embeddings支持本包发现/测试；其它既存协议只读显示实际不可用原因，不恢复Jina/Image等原STOP。保存配置、选择模型和打开页面均不自动出网。

| 接口 | 严格输入与输出 |
| --- | --- |
| GET `/system/model-capabilities` | `{protocols:[{protocol,model_types,discovery,test}]}`；未支持能力不可显示可执行按钮。 |
| POST `/system/model-providers/{id}/discovery` | `{expected_version}`→`{operation_id,provider_id,provider_version,outcome,reason,items:[{provider_model_id}],duration_ms,created_at}`；只从已保存供应商的固定`models`路径获取ID，不据ID猜能力。失败或未知时items为空。 |
| POST `/system/models/{id}/test` | `{expected_version,provider_version}`→`{operation_id,model_id,model_version,provider_version,outcome,reason,duration_ms,created_at}`；使用已保存模型/凭据和固定最小协议payload，不接自由prompt、任意URL或客户端Secret。 |

outcome为`succeeded|failed|unknown`；成功reason为null。安全reason闭集为`unsupported_protocol,configuration_changed,credential_unavailable,policy_rejected,network_failed,tls_failed,timeout,cancelled,upstream_rejected,invalid_response,response_limit,interrupted`。duration_ms为0..20000的JSON整数，created_at沿正式UTC时间。返回不含供应商原文、认证头、响应内容或Secret；一次成功不标记供应商持续在线，不替用户保证后续调用可用。

两POST均要求System/CSRF/Idempotency-Key并持久绑定原身份、body和版本。原key/body完成重放返回同一结果，**不再次出网**；未决为`COMMIT_UNKNOWN/unknown`、`retry_hint=replay`，无额外lookup。有可靠原进程退出事实时可收敛`unknown/interrupted`，不猜上游未执行。重试一次外部调用必须是用户明确发起的新操作，不能因响应丢失自动换key。

主操作20秒，真实I/O结束并join后有界5秒持久/lease收尾；全局最多4个、不排队，满额`RESOURCE_BUSY`。发现响应最多2MiB、1000个ID，超限明确失败而非假完整目录；测试响应最多1MiB。只允许已保存配置派生路径，拒绝redirect；复用统一出站DNS/当前规则/代理/TLS，不读取环境代理兜底。实际网络期间当前Session/admin与Provider/Model/Secret版本由共享锁保护；关闭/取消需实际停止并join，界面不把取消请求当上游未执行证明。

勾选导入使用现有`model.create`逐项串行，单批最多100项；每项独立key/body/receipt并显示已成功、失败、未开始或Unknown。不重复成功项，不把部分完成写成整体回滚；取消仅停止未开始项。导入前明确合法类型和能力配置，不从发现结果制造上下文、工具或推理支持。

## 5. 实施与验收边界

account_backend唯一维护后端/API/schema及00147；proxy_copy唯一维护新Vue；本卡、蓝图和当前摘要由account_contract维护，root负责Git。测试、视觉与安全由root指定owner独立分工，不把作者检查写成独审。

首链为供应商/凭据→模型→测试→用途保存与刷新；并检查部分完成、权限/CSRF、409、Unknown原请求终结、引用阻断、能力不支持与取消。发现/test使用任务拥有的模拟供应商和真实App/Secret/出站实现，不用用户现有凭据或外部供应商实发。页面核桌面/窄屏、键盘/modal回焦、返回列表与部分结果；现系统导航10项需要新检查，旧11项通过事实保留为历史。当前尚无本包实现或验收PASS，先按稳定合同并行实施和隔离联调。
