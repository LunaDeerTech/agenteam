# Owner 模型设置 T3 精确 DTO 建议 — rev2（草案）

仅补 rev0 8b78072a、rev1-static 9197fc97/83e3e652 及独审 691ab122 的 material/snapshot/私有 IPC 字段缺口。17 HTTP 操作、9 IPC、29路径和原预算不变；协议标识仍为尚未实施的 `project-owner-models.v1`。没有产品/正式卡/资源授权，原稿不改。frontend 已通过消息确认本建议足够消费（含origin仅作样本配对的限定）；这不是 frontend/backend 双作者 agreement。

所有下述对象均恰含所列字段，必需 nullable 用显式 null，不用缺字段；重复/未知键、第二个JSON值、坏UTF8及非法 union 拒绝。`ID`=正式小写 UUIDv7；`Version`=1..MaxInt64 的 canonical decimal string；`DBCount`=0..MaxInt64 decimal string；`Count`=非负安全整数。配置 expected 可到 MaxInt64，凭据 expected 只到 MaxInt64−1，沿正式写 DTO。Token 是本 case Go 登记器按序产生的 `r000001`/`a0001`；与 input_hash 一起解释，重启/另一case无效，不取自 key、body 或任何digest。

## 1. material：安全 ID 期望与原登录 bootstrap

material 顶层仍恰 `protocol,input_hash,mode,actors,projects,expected,system`。actors 固定 `owner,other_owner,other_admin`，每个仍恰 `email,password,user_id,username`。这只保留原私有0600登录bootstrap文件及原退休删除规则，不能复制进证据；**Credential value、原写body/key及其digest从不进入material或任何其它文件**。原 fixture 登录密码和产品待写 Credential 是两种不同材料，不能用“私有文件”给后者新增落盘许可。

Node 每 case 在内存生成 Credential value→实际受保护输入→由产品正式HTTP提交；代理只在受控内存捕获/比较原字节。不得通过新IPC送值、绕过UI填充、存入trace/video/截图/console/error、`expect(value)`差异输出或digest。填充失败也必须把可能含 fill 参数的 Playwright 错误净化为固定安全消息；只在受保护值已清空、材料对话框关闭后拍图。不承诺JS string/Go decoder所有内部副本可可靠擦除。

```ts
type Mode = 'configuration'|'credential'|'recovery'|'read'|'authority'|'navigation';
type ProjectKey = 'main'|'second'|'other'|'admin_owned'|'archiving'|'archived'
 |'deleting'|'pending'|'config_recovery'|'credential_recovery'|'referenced';
type ProjectLocator = { id:ID; username:string; name:string; normalized_name:string;
 owner_user_id:ID; initialized:boolean; lifecycle:'active'|'archiving'|'archived'|'deleting' };
type ProviderRef = { id:ID; project_id:ID; name:string;
 protocol:'openai-chat-completions'|'anthropic-messages'; version:Version; credential_ref:ID|null };
type ModelRef = { id:ID; project_id:ID; provider_id:ID; name:string; version:Version };
type CredentialRef = { project_id:ID; credential_id:ID; purpose:'model'; version:Version };
type SeedRefs = { providers:ProviderRef[]; models:ModelRef[]; credentials:CredentialRef[] };
type DirectoryRef = { id:ID; provider_id:ID; scope:{kind:'system'}|{kind:'project';project_id:ID};
 name:string; provider_name:string; version:Version };
```

`ProjectLocator` 保原7字段，不添猜测版本；archive 控制的 expected_version 唯一取自下一节 `snapshot.project.version`。Provider/Model Ref 只作 fixture 期望，不取代原HTTP完整安全DTO；DirectoryRef 是期望用安全子集，不是七字段目录响应的新版本，正式 `capabilities` 仍由实际HTTP＋公开client/schema验证。禁止从System Get补目录字段。

projects 与 expected.projects 的键集按mode精确相同：configuration/credential只有main；recovery为main/config_recovery/credential_recovery；read/navigation为main/second；authority为全部11个ProjectKey。值分别为ProjectLocator/SeedRefs。所有Ref所属Project、Model.provider_id与相应Provider、Provider.credential_ref与相应Credential须一致；ID不得重复，数组按ID排序。空数组为[]，不是省略。登记集只包含该case正式准备得到的safe ID/版本及后述正式create结果；不允许任意新ID/SQL/URL。

expected 是以下闭合联合：除read外恰 `{projects:<上述精确键表>}`；read恰 `{projects,pagination}`，pagination恰 `{provider_ids,model_ids,available}`，前两数组分别恰26个不同本main ID，available恰26个DirectoryRef且含本main和system两scope。Models的26条来自两Provider；完整顺序/分页取实际GET结果核验，ID数组只作覆盖集合，不能编造排序列。所有正式DTO、版本及名字仍由同root准备实值填入，不复制大配置/options/overwrite/efforts/材料。

system 在非navigation模式必须null；navigation恰：

```ts
type SystemDraftFacts = {
 selection:{ initial:{id:ID;version:Version;configured:
   {embedding:ID;memory:ID;reranker:ID|null;image:ID|null}|null};
   draft:{purpose:'memory';model:{id:ID;name:string}} };
 summary:{ initial:{id:ID;version:Version;model:ID|null}; draft:{model:{id:ID;name:string}} };
};
```

只给现有System Selection与统一Meeting Summary两表单的安全初值和一个不同的可选项，测试草稿/确认隔离，不新增Project selector、独立initial/update字段或Summary override。navigation的owner登录身份必须经正式账户准备就是admin且确为main/second Owner；实际Session响应再核role/user，禁止改role SQL或在组件里豁免权限。非navigation原ordinary Owner代表保持。

## 2. snapshot：当前资源、历史提交、原请求比较分开

`snapshot` args仍恰 `{project:ProjectKey}`，必须是本mode已登记key。只读同root Store、同一有界read Tx内的本Project安全列；结果只能在该Tx成功提交且ctx有效后发布。无额外HTTP、自动lookup/Execute、授权替代、后台读取或SQL输入。私有fixture事实不作为产品授权。不得复制旧fixture的 `to_jsonb(row)` 全行快照到ack。

```ts
type ProviderFact = {id:ID;present:true;version:Version;credential_ref:ID|null}
 |{id:ID;present:false;version:null;credential_ref:null};
type ModelFact = {id:ID;present:true;version:Version;provider_id:ID}
 |{id:ID;present:false;version:null;provider_id:null};
type CredentialFact = {credential_id:ID;metadata:{credential_id:ID;purpose:'model';version:Version}|null};
type ConfigReceipt = {kind:'provider.create'|'provider.update'|'provider.delete'|
 'model.create'|'model.update'|'model.delete';resource_id:ID;version:Version;affected_references:'0'};
type CredentialResult = {credential_id:ID;purpose:'model';version:Version;deleted:boolean};
type ReplayComparison = {request_token:RequestToken;
 body_equal:boolean;key_equal:boolean;target_equal:boolean;identity_equal:boolean;method_equal:boolean;
 original_body_bytes:Count;replay_body_bytes:Count};
type OriginFact = {origin_token:RequestToken;original_request_token:RequestToken;
 operation:MutationOperation;project_id:ID;target_id:ID|null;original_body_bytes:Count;
 history:{family:'configuration';committed_rows:'0';receipt:null}
   |{family:'configuration';committed_rows:'1';receipt:ConfigReceipt}
   |{family:'credential';committed_rows:'0';result:null}
   |{family:'credential';committed_rows:'1';result:CredentialResult};
 comparison_count:Count;comparison:ReplayComparison|null};
type SnapshotResult = {
 project:{project_id:ID;version:Version;initialized:boolean;lifecycle:ProjectLocator['lifecycle']};
 current:{providers:ProviderFact[];models:ModelFact[];credentials:CredentialFact[]};
 history:{configuration:{committed_commands:DBCount;audit_records:DBCount;events:DBCount};
   credential:{committed_commands:DBCount;audit_records:DBCount}};
 reference_presence:{models:{id:ID;present:boolean}[];credentials:{credential_id:ID;present:boolean}[]};
 origins:OriginFact[];
 fixture_only:{archive_recovery_applied:boolean;reference_fact_state:'present'|'absent'|null;
   rename_reuse_applied:boolean};
};
```

current只返回登记ID；不存在时null/false不能推成历史删除成功。Credential没有canonical行就metadata=null，不能补造deleted Mutation；deleted只来自正式安全历史result。history中的配置计数严格本Project `agenteam_model.commands.phase='committed'`、Audit/Outbox `producer='model'`；Credential计数严格本Project Secret receipt及 `producer='secret'` Audit。Nonce/payload/prepared计划不属于这些提交计数，拒绝时不能要求整个DB零变化；也不把Credential说成产生Model/Project事件。两族当前/历史不可混淆，HTTP尝试和POST lookup次数不进这些计数。

每origin历史仅查其私有内存内正式identity：配置namespace model.project/owners=[Project,Human]，Credential secret/相同owners及正式digest计算；只取安全receipt列。0行不证明原Unknown回滚，committed_rows=1须有同族完整receipt/result，否则fixture_failed/无候选。各Project的Audit/Event总数是明确的同scope总数，不能冒充逐origin Audit关联；测试比较串行步骤前后差量，并另核该origin唯一receipt。引用查询只本Project/登记Model或Credential，不能读全库ref；fixture_only三项单列，不宣称真实归档或引用owner adapter绑定。

origin_token **等于首个由arm选中的mutation真实original_request_token**，最多4个。每个 `(ProjectKey, operation, target_id)` 只允许一个selected origin；collection create的target_id=null仍只一项，避免根据key“匹配”而漏掉换key。后续同精确端点请求只成为比较候选，不依赖key相等来决定配对；这不是同command identity的证明，更不能把正常新意图写冒称原重放。受控case须用既有不同recovery Project/operation隔离同端点其它合法新意图，并结合key/body布尔及明确UI原Execute动作判断。只有两份完整有界body、header key、登记身份均实际捕获后才产生comparison，尚未比较为null。latest comparison＋comparison_count足够逐步骤检查；不存重放body历史。若同tuple需要第二个独立origin则该草案场景不能猜关联/覆盖，必须在实施agreement前精确调整已有case安排，不增加IPC动作或产品能力。

原body通过正式请求被实际消费时的有界tap收集，不预读后重构请求；原/重放UTF8 bytes与header key原样比较，不trim/排序/remarshal。identity_equal指原命令的稳定Human+Project+namespace+kind，Session不是命令identity组成部分；仅来自已登记正式Login/Session的私有关联，不接受caller声明。未知身份/不完整捕获的comparison必须null并让要求该证据的case失败，不能false冒已比较。该布尔不是当前授权判定；同User新Session可原同义重放，当前Session有效性仍由真实请求验证。Secret材料和request/key/digest仅内存，不进snapshot/日志/sidecar。

历史found/observed、current GET相等/404、snapshot已提交都不能令UI确认/清材料；仅显式原Execute严格成功才可。archiving/archived上配置可原同义Execute，Credential被动lookup可见但原Execute仍Mutate拒绝。失败/Unknown不自动重key、补GET或后台重放。

## 3. 精确IPC与请求token、终局

request仍恰 `{protocol,input_hash,sequence,action,args}`，严格next sequence1..128；ack仍恰 `{protocol,input_hash,sequence,action,ok,result,error}`。ok=true时error=null、result是该action精确DTO；ok=false时result=null，error仅固定 `invalid_envelope|invalid_sequence|invalid_action|invalid_arguments|unknown_target|arm_busy|token_mismatch|not_ready|budget_exhausted|fixture_failed`，无message/stack/SQL。未知/错误控制置安全失败标记并触发原已注册退休，不能继续装作PASS。8KiB request/64KiB ack/8s ack界原样保留。

17 operation literal及方法/路径/target/query条件见 endpoints.json；只再允许既定私有 `getCurrentSession` GET。Project target只能material已登记ID或**完整正式create安全结果中产生的新ID**；后者在施加cut/disconnect之前原子登记，即使browser没收到也能供后续原请求/metadata精确控制。必须同时核scope/operation/receipt类型和resource ID，不能据任意JSON `id`登记。分页cursor同样只取本case完整正式safe页，仍通过原cursor校验。

arm args精确 `{operation,project,target_id,query,effect}`，所有字段必有；Session为project/target_id/query=null；17端点中的collection无target，detail必登记target；非list query=null。list query是`CanonicalPageQuery`字符串：只含0/1个cursor、0/1个limit，limit1..100，cursor取上述登记值，按正式编码、无重复/额外key/ForceQuery；记住此原字符串并与下一真实请求RawQuery逐字比较，不能放宽为任意query字典。所有canonical path由操作表＋登记ID生成，无URL/method/header参数。

```ts
type Effect = 'before_dispatch_hold'|'after_complete_hold'|
 'after_complete_cut'|'after_complete_disconnect';
type ArmResult = {arm_id:ArmToken;state:'armed'};
type ControlState = {arm_id:ArmToken;request_token:RequestToken|null;
 origin_token:RequestToken|null;
 state:'armed'|'claimed'|'upstream_complete'|'released'|'joined';
 held:boolean;release_requested:boolean;upstream_complete:boolean;safe_admitted:boolean;
 effect_applied:boolean;joined:boolean};
type ReleaseResult = {arm_id:ArmToken;request_token:RequestToken;release_requested:true};
```

一次只有一个未实际终局的arm，且只claim下一匹配browser请求一次。before_dispatch_hold仅六read/Session；after_complete_hold仅六read/九mutation；cut/disconnect仅九mutation且必须先收到200完整、严格绑定的safe final receipt/result，不控制lookup或伪造COMMIT Unknown。此处read精确指六GET；两个POST lookup不在故障effect域，仍正常正式执行/计数/安全取证。Session只before_dispatch_hold、不留其body。arm不命中须仍有原界内取消/退休；不能给handler/body另加预算。

control-state args恰 `{arm_id}`；token由该真实outer handler入场时登记，arm无请求时null。state是派生快照，不能假设统一顺序：joined优先，其次release_requested，其次upstream_complete，再claimed/armed；例如取消可令joined=true而upstream_complete=false。held表示该token实际进入hold，safe_admitted只表示完整上游通过安全证据准入（Session固定false），effect_applied单列。任何字段都不声称浏览器收到、Tx提交或Cookie owner已释放。

release args恰 `{arm_id,request_token}`，两者必须匹配且已进入hold；重复相同release可返回同一收取事实，但不能释放下一arm、null或wildcard。ack只表示释放许可；随后必须从同一control-state观察joined=true，并对实际hold核held恰增1、held_joined==held，最后server_finished==server_started。只有D1已接受的outer handler返回/unwind（ReverseProxy body/write/ErrorHandler实际退出后）可以记joined；失败/Fatal之前注册所有release+join，不在ctx.Done/Close/release处提前加数。

`counts` args={}，result精确 `{operations,session,server,controls,browser_eof,schema_bodies,client_bodies}`：operations是按endpoints表顺序恰17行 `{operation,setup,browser,control,upstream_complete,handler_joined}`（Count）；session是 `{setup,browser,control}`；server是 `{started,finished}`；controls是 `{armed,claimed,held,held_joined,cut,disconnected}`。后三个browser_eof/schema_bodies/client_bodies **固定null**，Go控制器没有其观察权。总数分source、mutation和lookup由精确operation表归类，不与提交计数混用。

Node独立保持native EOF/typed client/schema验证记录，final_result的counts精确 `{server:<最后counts结果>,browser:{attempts,complete_eof,typed_client_ok,schema_ok,incomplete}}`（均Count）；旧顶层schema_bodies/client_bodies须与此一致。无第10个“browser上报”IPC。Go最终只验证Node原子result文件与已取证字节/实际child终局，不能在counts IPC伪造这些数。server.joined、native body/cancel/finally的Cookie owner、snapshot读Tx提交是三个独立事实，禁止建立“owner必持至server.joined”的假因果。

其余五action保持原精确形状：

| action | args | result及界 |
| --- | --- | --- |
| snapshot | `{project}` | 上述SnapshotResult；同Tx、完整输出后发布 |
| logout | `{session_id}` | `{session_id,revoked:true}`；只能已登记正式browser Session，调用正式Logout，不写revoked_at |
| archive-recovery-project | `{project:'config_recovery'|'credential_recovery',expected_version}` | `{project_id,initialized:true,lifecycle:'archived',fixture_only:true}`；snapshot版本作CAS、预登记active→archived辅助事实；不承诺参与者运行或推断version增量 |
| reference-fact | `{project:'referenced',state:'present'|'absent'}` | `{project_id,model_id,reference_present,fixture_only:true}`；唯一预分配slot，真实delete仍DEPENDENCY_UNBOUND，不改Agent |
| rename-reuse | `{project:'main'}` | `{renamed:ProjectLocator,replacement:ProjectLocator}`；正式Update＋既有私有创建fixture，旧稳定ID不变，新ID与旧名注册给本case，不冒完整创建/生命周期绑定 |

所有snapshot/ack先完整有界编码，超过64KiB失败不截断；ID数组只本case登记集，未知形状或额外材料不先落盘。原sidecar仅已闭集合格的安全response；Login/Session、原body/key及其digest、Credential材料不留证。原45s/120s含cleanup/6m/75s/5GiB/7ID预算和串行root资源窗原样，未给任何执行许可。

## 4. 冻结前置

本稿可供两作者以同SHA确认：精确operation/args/result/模式键集、新create登记、origin关联与身份来源、handler actual join以及Node独立取证。frontend消息只作需求输入，backend尚忙旧回归，本轮未要求其读取；**双作者agreement仍pending**。Audit整卡与T1最终共享源handover、T4正式卡/唯一writer/README末件及实际闭包/driver/grant仍是实施前置。未读活动产品、未做Go/Node/资源/网络/Git；三硬停与生产未绑定范围保持。建议草案 STOP。
