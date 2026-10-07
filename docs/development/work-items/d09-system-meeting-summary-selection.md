# D09：系统会议 Summary 模型选择

修订：rev1，2026-10-07，规格已独立完整 STATIC 接受并获 root 采纳；尚未实施、未动态验收。规格接受不授予业务、SQL、测试运行或共享资源权限。文档基线 `175694bf1157b77ea13268bcd8a47787e1a28acd`，产品输入固定 `6fa2ee721a75ea34a1ccd6523b8b25d68328c5b9`。root 已核已接受迁移连续前缀 00001–00019，**00020 唯一预留给本卡**；本阶段不创建迁移源。

依据：[开发计划](../development-plan.md) D09、[团队流程](../agent-team/README.md)、[Model 配置](../../architecture/platform-infrastructure/model-system/model-configuration.md)、[Model Resolution](../../architecture/platform-infrastructure/model-system/model-resolution.md)、[Meeting Summary](../../architecture/meeting/meeting-context-summary.md)。相关11份架构、布局与设计文档已同步用户决定；会议 Summary 的用户决定以本卡 §1 及[当前恢复记录的保留边界](../agent-team/recovery-2026-10-07-environment.md#保留边界)为准，历史验收报告不改写。

## 1. 已确认结果与分块

用户已决定“系统管理员统一配置用于 summary 的模型”。root 已限定本次为 **Meeting Rolling Summary initial/update，含首轮标题**：管理员显式选择一个 enabled System chat Model，所有项目的新逻辑 Summary 生成消费系统当前选择，不复制创建默认、不提供 Project override。

逻辑选择名为 `platform.meeting_summary`。它不改变 Memory 三用途、Agent generation/approval、`agent_compaction` 的原 Execution 固化 snapshot，或 Execution Summary read model。Meeting 调用仍是 `MeetingConsumer`，带原 Project/Meeting/Operation 和两个原 Purpose；无 Agent Execution，首轮标题与四字段 Summary 沿原同次普通 text 调用/同事务提交，`tools=[]`，不因这次选择改为必须 structured output、Tool Calling 或 reasoning。

| 块 | 完整结果 | 前置与后续责任 |
| --- | --- | --- |
| **S1，本卡实施范围** | 系统独立 selector 的真实持久化库、当前管理员 get/update/原命令查证、未配置状态、引用/删除替换、旧 receipt/旧四用途兼容，以及现有 deletion-impact HTTP/schema/client 的必要兼容 | 只消费下表已接受能力；没有新 Summary 配置 HTTP 写口，没有生成或 Resolver/root 绑定 |
| S2，另卡 | 同页独立 Summary 设置区、正式 GET/PUT、安全恢复、默认 root 技术初始化 | 消费已接受 S1；复用原 Cookie owner，不改旧四项 PUT 语义；包括显式启动初始化接线，届时协调 Usage HTTP 文件所有权 |
| S3，另卡 | `platform.meeting_summary` current-resolution 库 | 消费已接受 S1 和 accepted current-resolution；新 Purpose 映射、锁内当前选择、plain text profile、不可变 snapshot/lease；生产消费者仍由 D24 绑定 |

S1 的最小结果不是只有 schema 或模型 ID 字段：更新、查证、原子替换、真实旧 HTTP 投影和既有前端 strict decoder 必须一起通过。S2/S3 仅记录责任，本卡不预授权实施。完整 D09/D24、Project 初始化/生命周期、Meeting Runtime、Provider 请求、Usage 写入、ready503、E01 和 Object/tools/SPA publication 三停止均不改变。

### 1.1 精确依赖

| 已接受能力 | 接受证据与本卡消费范围 |
| --- | --- |
| 当前 Account Session/System 授权与 Model 根装配 | [System Model root](../agent-team/system-model-root-verification.md)、`model.Authority.current/currentScope`；同实际 Store/Tx、当前 Session/admin，不授管理员 Project 正文旁路 |
| System Model 命令、Secret引用、Audit/Event、四用途选择及删除 | `26622bc`/`543511c` 的 System 库及后续已提交根；[Project 配置验收](../agent-team/d09-project-configuration-verification.md) `de00c610` 保留 scope/legacy System receipt 与未绑定外域拒绝；本卡仅读写 Model 自有表 |
| 安全删除影响管理读 | [管理读验收](../agent-team/system-model-management-reads-verification.md)；当前库 3s、同Tx授权、完整 reference 闭集、原 HTTP GET/HEAD 与零候选终局 |
| 既有 Model UI / 四用途 UI | [Model UI](../agent-team/system-models-ui-verification.md) `bc17167c`、[Selection UI](../agent-team/system-model-selection-ui-verification.md) `870ebbb9`；只扩现有删除影响 group/label，保存/恢复/Owner流程不重写 |
| 迁移与 PG | 已接受 00001–00019；00019 来源[邀请最近投递读](../agent-team/system-invitation-delivery-read-verification.md) `9b320154`。保留真实 PG17.x（至少17.8）、pgvector、原 fixture 规则 |
| 后继 Resolution 的稳定输入 | [current-resolution](../agent-team/current-model-resolution-verification.md) `4295df7d`；仅作本卡兼容依赖与S3起点，S1不放开当前拒绝分支、不新增 consumer或租约 |

本卡不消费未接受 Artifact/Object guard、D10 Skill 服务、D12、活动 Usage HTTP 源或停止任务的私有候选。以上模块级未完成不妨碍此完整库结果，但不能将本卡通过外推为模块完成。

## 2. 正式 S1 API 与安全状态

新增 `internal/central/model/contract/meeting_summary.go`，不修改旧 `PlatformSelection` 序列化形状。具体类型：

```go
type MeetingSummarySelection struct {
    ID      string          `json:"id"`       // canonical UUIDv7
    Version foundation.Version `json:"version"`
    Model   *ModelID        `json:"model"`    // 显式 null 表示从未配置
}
func (MeetingSummarySelection) Validate() error
func (MeetingSummarySelection) Clone() MeetingSummarySelection

type UpdateMeetingSummarySelectionRequest struct {
    CommandMeta
    SelectionID     string             `json:"selection_id"`
    ExpectedVersion foundation.Version `json:"expected_version"`
    Model           ModelID            `json:"model"` // 必填，不能清空
}
func (UpdateMeetingSummarySelectionRequest) Validate() error

// model.Service 的新具体方法；构造签名不变。
func (*Service) InitializeMeetingSummarySelection(context.Context) error
func (*Service) GetMeetingSummarySelection(context.Context, identity.Actor) (contract.MeetingSummarySelection, error)
func (*Service) UpdateMeetingSummarySelection(context.Context, contract.UpdateMeetingSummarySelectionRequest) (contract.CommandReceipt, error)
```

新 request 只允许 Human、System scope、正版本、合法稳定 ID 和 key。不是 ProjectModelSettings，也不把 Project ID 存在新 selector。新增 DTO 保持 string version/null/ID 的无损协议；Format/LogValue 对写请求不输出 key/body，沿既有 model 契约的安全格式。

`InitializeMeetingSummarySelection` 是内部技术初始化：先要求旧 `Service.Initialize` 已建立原四用途技术singleton（其configured仍可false），同实际Store取得旧singleton SH与新singleton EX，一笔事务检查本卡schema并创建version=1/model=NULL的技术行；重入保持原ID/版本/值。旧技术行不存在返回DEPENDENCY_UNBOUND，不隐式代调；新ID须与锁内读取的旧ID不同，避免跨owner碰撞被概率假设代替。无Model默认、无Audit/Event/command、无后台任务。new constructor无I/O。**S1保持旧 `Service.Initialize` 原行为，不自动从其中调用本方法**；原1..15/16/17的历史迁移场景不因新表强制访问失效。S2再在原startup context/deadline内接一次显式方法；不重开预算或新增资源owner。

应用完00020但尚未运行此技术初始化的状态合法：不存在新 singleton且零新 role 引用代表“尚未初始化”。新 `Get/UpdateMeetingSummarySelection` 返回 `DEPENDENCY_UNBOUND`，不临时创建行；旧四用途/Model读删继续工作。技术初始化后 Get 可返回 `model:null`；这不是“生成已就绪”。坏行、多个行、缺表或无singleton却出现新引用是 `DEPENDENCY_UNAVAILABLE`，不能按未配置掩盖。

新 Get 先校验 actor，使用 min(parent,now+3s)，同Tx User SH、summary singleton SH、model-references SH；真实 current Session/System Read后完整核 canonical和该owner精确引用。只有 Committed且context仍有效才返回完整安全 DTO；Unknown/取消/坏事实零候选。DTO仅ID/version/model，不带Provider endpoint、credentials、Project或内部command材料。所选Model后来停用仍返回已保存ID，不把“当前可用性”伪装成配置不存在；S2自行按原安全Model/Provider读取显示可用性。

完成结果复用原 `CommandReceipt{kind:"model.selection.update",resource_id:<summary ID>,version,affected_references:"0"}`。原 `LookupCommand` 以 `Command="model.selection.update"`、当前管理员和原 key 查证，返回同形 receipt；lookup 不证明 caller 的新body同义。只有 `UpdateMeetingSummarySelection` 的原请求重放比较新 semantic digest并确认同义。

**旧 HTTP lookup 的显式兼容决定：** `/system/model-commands/lookup` 的已有 kind 不新增、不扩 `validCommand`；它可在当前原身份授权下观察以同一 action 接受的 Summary 安全 receipt。这是 S1 明确接受的原 generic lookup 行为，不是新 Summary 设置读/写口。保留资源ID与历史版本，不返回Summary配置、请求body或新权限。原四用途 PUT 对新 singleton ID 不得变成Summary写入口，任何未列字段仍拒绝。S2只增加其真实GET/PUT和专门client恢复，不能再把generic lookup当新的授权来源。

## 3. 持久化、命令与事务

### 3.1 00020 与完整事实

新 `db/migrations/00020_model_meeting_summary_selection.sql` 只新增 `agenteam_model.meeting_summary_selection` 并扩已有 references 的闭合role约束：

- `id safe_id PRIMARY KEY`、`singleton boolean NOT NULL DEFAULT true UNIQUE CHECK(singleton)`、`version bigint NOT NULL CHECK(version>0)`、`model_id safe_id NULL REFERENCES agenteam_model.models(id) ON DELETE RESTRICT`、`updated_at timestamptz(6) NOT NULL`；不造默认行/Model，不加Project FK。
- 新引用只允许 `owner_kind='platform_selector', project_id IS NULL, role='meeting_summary', reasoning_effort=''`；canonical对应关系由正式同Tx service校验。旧三类 owner/各role约束逐字语义保留，不能顺手删除 `project_summary` 分支或将其映射成新系统配置。
- 配置为空时该新owner零引用；非空时精确一条引用，owner_id=新singleton ID、model_id=canonical Model、owner_version=canonical version。稳定ID不得等于原四用途singleton ID，初始化须真实检查冲突，不能因UUID随机而省略；不存在合法“第三个平台owner”。
- 旧 00001–00019、Audit闭集、commands表/command_name、原四用途表不重写；本卡沿同 action/metadata，无需扩大 Audit `selector_kind`。

迁移是事务内DDL，fresh及populated升级失败不得留下半表/半约束；原checksum与migration journal规则保持。旧schema前缀仍是历史证据，不冒充新二进制生产可在未迁移20的库上运行。

### 3.2 新请求身份与历史原字节

新 Summary 继续使用正式 `model.system / [UserID] / model.selection.update / key` 的 CommandIdentity；**同一管理员对旧四用途与新Summary使用同一个key就是同一命令身份，异义明确 `IDEMPOTENCY_KEY_REUSED`**，不另造未定义命名空间。旧identity、key digest和lookup完全保持。

新 request 才使用 `model-meeting-summary-command-v1` 语义体：固定字段 `format,user,kind,resource,expected_version,model`，kind=`model.selection.update`，resource=summary ID，expected_version为无损十进制字符串，model为canonical ID；沿 `cursor.Digest` canonical JSON。Session/RequestID不进入semantic；当前Session仍每次复核。

旧 `commandSemantic` 的 `model-command-v1` / `model-project-command-v1` 分支、JSON外形、原字段、null与默认、版本、canonical算法必须保持。不能向 `PlatformSelection` 加可序列化Summary字段，也不能给旧semantic外壳加空新字段。新内部 `commandRequest`可有私有Summary分支，但先显式分派，新旧互斥。

mutationPlan新增 `BeforeMeetingSummary/AfterMeetingSummary` 可选字段，零值使用 `omitempty`，原无此字段的plan照原scope/身份规则加载；不回写已持久旧plan/receipt/Audit/Event。新Summary计划要求精确新before/after、合法version+1和同ID、System scope、原SelectionID、没有旧BeforeSelection/AfterSelection、没有Provider/Model修改分支。Model删除的计划可同时包含两个selector owner，须另以删除规则验证，不把混合形状当普通Summary更新。新字段存在而不符合闭集即坏事实，不能悄悄忽略后继续提交/重放。

### 3.3 Update、原子作用与Unknown

沿原runCommand次序：输入形状→当前System权限→原command receipt/semantic→首次变更权限/version/新依赖。合法完成重放优先于旧expected_version、后来停用/删除的原Model；返回原receipt，不回退canonical、不补发Audit/Event。准备出错仍用原command锁复查并发同义receipt，不能让陈旧候选盖过已接受结果。

首次写只选 enabled System Provider下的enabled `ChatModel`；只要求已有chat文本配置合法，不要求memory的json_schema、tools、reasoning或某一wire已运行；配置能力与实际Resolver支持范围分开。Project Model、非chat、禁用Model/Provider、缺失target分别沿现有typed错误并零副作用。选择与当前相同的Model仍沿原显式保存规则推进新版本并形成一份receipt；不能把重放误记为新保存。

准备读取不授写权。Tx外收集Provider/Model、originalcommand、summary、reference、Audit/Event计划后，单次Normalize/Acquire完整union：command EX、User SH、`model-references` SH、`model-meeting-summary-selection` EX、必要Provider/Model SH、reference record、command record与Outbox要求锁；不在InTx补早序锁。Tx内重核当前Session/admin、receipt优先、canonical ID/version与整个旧引用映射、目标完整配置。漂移全回滚/受控冲突，不自动换模型。

同一实际Tx提交：新canonical、精确reference、prepared command→committed安全receipt、一份`model.selection.update` Audit、一份 `model.configuration_changed` Event。Audit沿旧 `{selection_id,version,changed_fields:["selection"],selector_kind:"platform"}`，由独立SelectionID区分；不改Go/SQL/OpenAPI/Audit UI的literal闭集。Model本域private prepared事实checker必须重读新canonical+reference，校验同Store/Tx/held locks/actor/command，不能仅凭旧action通用形状通过。普通Summary更新不发embedding-selection-changed。

Unknown沿既有原physical cause/attempt和command EX序列化确认，不换key/模型/版本，不把未取到锁或查不到行当原writer已回滚。确认失败保留原Unknown；查询无候选不授权自动重写。必要原Session失效时公开回执仍按当前身份拒绝；不得借内部恢复泄露配置。没有新异步worker或租约，S1不改变ProcessGuard/Object退出语义。

## 4. 删除替换与管理读兼容

### 4.1 两个真实owner，不只扩role

`prepareReplacement` 必须按exact ownerID把平台引用分为原四用途singleton和新Summary singleton，分别重读canonical并核该owner**全部**引用；未知第三owner、错误role所属、版本错、额外边/漏边均失败。新Summary下只能meeting_summary；旧singleton下只能原四role。外域agent/project_summary依然 `DEPENDENCY_UNBOUND`，不能为本卡放宽。

删除拿原 `model-references` EX；需要改变的每个singleton拿各自EX，目标Model EX及替代Provider/Model SH，所有reference和事件锁完整union。先前发现集合/target/替代/owner版本在Tx内重核。Summary required：无replacement拒绝；replacement必须不同且为enabled System chat。若同一待删模型还承担memory，替代同时满足json_schema；两个owner用同一个请求replacement，各自版本各推进一次，不能只更新反向索引或只改一个owner。

原四用途canonical由其现有算法负责，新Summary由新本域helper负责；先同步两个canonical/references，再物理删Model。任何一处失败全Tx回滚。receipt/Audit的affected_references为真实边数，不是owner或Project数。Model删除生成原Model配置Event，加每个实际改变selector各一份配置Event；embedding变化另沿旧专用Event规则。所有ID/header/payload首次准备固定，重放不新增。

Provider/Model停用仍沿原规则保留selector和引用，后继新Resolution明确失败，无自动清空/替换。删除真正完成也不改历史snapshot/Usage身份；S1不修改00017/00018或触发新的lease处理。

### 4.2 现有HTTP/client必须在S1相容

现有 `managementSelectionConsistent` 不能继续把全部platform引用限定为旧四边。它在当前管理员同Tx下持两个singleton SH与model-references EX，分别读取旧singleton与可不存在的新singleton，建立**最多五条**canonical reference的完整并集，按 `(owner_id,role)`确定性排序；查询所有平台引用，LIMIT **6** 为多一条哨兵，完整校验行后比较精确并集。新singleton不存在仅在新role零边时合法；它不能使未知owner或坏引用被忽略。无新singleton时旧四用途判定与错误保持。

`managementReferenceKinds` 与实际聚合加入 `platform_selector/meeting_summary`（required=true、unbound=false），按kind/role字典序放在image之后、memory之前。最多8个正数group，原10000边预算与10001哨兵、sum/count精度、全部未知组合拒绝不变；不把新组折入project_summary或漏计以兼容旧client。旧project_summary组仍required/unbound，使用历史标签，不冒认为已迁移。

S1 **同一交付**更新 `api/openapi/model-system.json` 的精确role配对、maxItems=8及required描述，`web/src/api/system-models.ts`的strict group闭集，以及`SystemModelDeleteDialog.vue`的新标签“系统会议 Summary 模型”。原client的required/clear禁止与memory额外限制继续生效；只含summary时不得被memory规则错误要求json_schema。原七组响应字节和行为不变，新组存在才返回第八种组合；不额外增加零count group。

因此S1库实际写入新引用后，**现有** GET/HEAD deletion-impact、DELETE replacement、generic lookup、当前构建的既有Model页面均可处理该事实；不以“新写HTTP没开放”作为兼容证明。已部署旧二进制/旧静态资产不是混合版本兼容承诺，生产SPA发布仍停止；本交付前后端兼容源必须共同冻结接受，不能先合入仅后端新group。新Summary GET/PUT与设置编辑器保持不存在，旧四用途HTTP body/schema严格原样。

## 5. S1 精确候选路径与所有权

本表为规格接受后可下发的完整候选白名单，不是本轮实施授权。新增文件落笔前核不存在；必要变化超出表格先报精确差量，不能自行写共享源。新卡与技术正文现由architecture唯一负责，root转交后才由作者更新状态。

| 路径 | 职责 |
| --- | --- |
| 新 `internal/central/model/contract/meeting_summary.go`、`meeting_summary_test.go` | DTO/request/validation/clone/safe formatting |
| `internal/central/model/contract/references.go`、`references_test.go` | 仅新增平台meeting_summary required配对，旧分支保持 |
| 新 `internal/central/model/meeting_summary.go`、`meeting_summary_store.go`、`meeting_summary_commands.go` | 三个服务方法、独立初始化、严格loader/精确引用、私有semantic与计划helper |
| 新 `internal/central/model/meeting_summary_test.go`、`meeting_summary_commands_test.go` | 授权/安全投影/计划/终局纯边界 |
| `internal/central/model/commands.go`、`commands_test.go` | 新私有请求/计划分派、旧semantic与持久旧形状字节保持 |
| `internal/central/model/configuration.go`、`configuration_test.go` | 完整lock union、两个owner映射验证/原子apply |
| `internal/central/model/references.go`、`references_test.go` | 两真实owner删除替换和完整集合 |
| `internal/central/model/audit_authority.go`、`audit_authority_test.go` | exact新canonical/reference同Tx事实，无Audit闭集变化 |
| `internal/central/model/events.go`、`events_test.go` | Summary配置及双owner删除事件，旧事件身份保持 |
| `internal/central/model/management_read.go`、`management_read_test.go` | 五边完整并集/六哨兵、八组投影/当前权限/3s |
| `internal/central/model/http_management_test.go` | 现有路由新group和旧严格契约回归；生产HTTP源不改 |
| 新 `db/migrations/00020_model_meeting_summary_selection.sql` | 新表和精确references约束，root独占排号 |
| `api/openapi/model-system.json` | 仅删除影响schema兼容 |
| `web/src/api/system-models.ts`、`web/src/views/system/SystemModelDeleteDialog.vue` | 新group strict解析及label，无新工作流/owner域 |
| `web/src/tests/system-models-client.spec.ts`、`web/src/tests/system-models.spec.ts` | 旧七组/新组/required/标签/Memory组合兼容 |
| 新 `tests/model/meeting_summary_fixture_test.go` | 正式Account/bootstrap/login、Model/PG/HTTP测试胶水，复用固定helper |
| 新 `tests/model/meeting_summary_selection_test.go`、`meeting_summary_replacement_test.go`、`meeting_summary_unknown_test.go` | library、完整引用/事务、真实Unknown |
| 新 `tests/model/meeting_summary_schema_test.go`、`meeting_summary_upgrade_test.go`、`meeting_summary_http_test.go` | fresh/populated、前二进制旧receipt升级、现有HTTP/客户端投影 |

表中 **35技术路径**（按实际展开逐项核验，不以数量限制正确范围）。不包含任何当前Usage HTTP十五路径：不写account boundary、usage/http、app/account.go、app/project_usage.go或其测试/fixture。`Service.Initialize`、query.go、Audit公共contract、旧迁移、锁文件、脚本/shared fixture、Resolver/adapter/根都保持只读。后端README与现有Usage任务冲突，最终说明由root在该路径交还后另行授权串行归位；不把说明延迟记成业务已全部关闭。

一个backend作者拥有上述生产/迁移/测试；前端三类窄兼容文件可由root转交唯一frontend作者，只有精确文件移交才可并行。S1至少一名未参与实现的验证者独立验权限/事务/恢复/迁移/HTTP兼容。Docker/PG/MinIO仅root交接的唯一窗口，离线工具和真实browser/native也须按实际授权执行；迁移预留不是运行许可。

## 6. 验收与证据

正式实施前冻结完整技术路径、相关Git/import/embed/TestMain动态build图和实际测试集合；不复制全仓/缓存，不因-run过滤漏掉TestMain构建的cmd。沿现有Go1.27.1、锁定依赖与原fixture，离线执行/真实资源分别由root授权。纯检查、build/list不是产品或资源PASS。

### 6.1 纯与前端兼容

- 新DTO空/坏ID/version/System scope、nullable只读/写禁止清空；deep clone；旧PlatformSelection原字节；旧command-v1固定golden与project wrapper；新旧同key异义；坏混合plan/错owner/错session/同Store/锁缺失。
- query Committed/context有效才出DTO，Unknown/取消/Rows错误/损坏行零候选；3s或更早parent实际完成，受控尾部放行前不返回，不靠超时select冒join。
- 新singleton零/一行、0/1边，两个owner联合0–5边和第6哨兵；错误owner/role/version/effort、额外平台owner、分组漏计拒绝。旧七组集合/10000上界保持，新Summary为required且bound。
- 现strict client解析新group，unknown/重复/乱序/总数不符仍拒绝；dialog显示新标签并禁止clear；summary-only plain-text替代可选，summary+memory继续要求json_schema。沿既有Model页面pure组件测试，不新写Summary编辑器。
- 后端unit/race/vet、两cmd兼容build、集成compile/list；前端受影响pure/type/format/build。只因role/标签新增不强制新browser流程，但真实HTTP响应必须与候选strict client/schema做同输入解析核对；这不冒称S2浏览器完成。

### 6.2 真实顶层与必要回归

| 新顶层名称 | 完整场景 |
| --- | --- |
| `TestModelMeetingSummarySchema` | fresh 1..20；populated1..19升级；失败DDL原子回滚/同checksum恢复；旧表/receipt/Audit/Event/约束除指定references差量保持；无默认行/模型，Initialize幂等及并发；NULL/坏ID/版本/错role SQL负例 |
| `TestModelMeetingSummarySelection` | 正式bootstrap/login当前admin、非admin与正式Logout撤权；未初始化/初始化未配置、独立于原四用途；首次/同值更新、expectedVersion竞争；零副作用非法候选；读取安全DTO/完整锁与终局 |
| `TestModelMeetingSummaryReplacement` | summary-only删除无替代拒绝/合法跨Provider替代；sameModel承担memory+summary的跨两个owner原子替代；plain-text候选对memory拒绝；第三owner/坏映射拒绝；计划后新增引用/版本/停用竞争；Audit/Event/receipt一体回滚 |
| `TestModelMeetingSummaryUnknown` | 真实原COMMIT回包丢失、原writer持锁下仍Unknown；释放后COMMIT/ROLLBACK分别确认，原cause/attempt保持；并发异义不能被错误采纳；同key重放不回退当前selector、不重发副作用 |
| `TestModelMeetingSummaryUpgradeReceipts` | 固定6fa2ee72前版本真实接受四用途/Provider/Model删除命令，实际停机后迁移20并由候选重新读取/lookup/原请求重放；旧plan/receipt/semantic/Audit/Event原字节；新Summary操作及同模型双owner替代后旧回执仍是历史版本；新旧同key异义 |
| `TestModelMeetingSummaryExistingHTTP` | 正式服务库写入非空Summary后，经实际旧System HTTP GET/HEAD deletion-impact/DELETE/lookup读取替代；新group安全投影/schema/client一致，旧四用途GET/PUT不带新字段；新Summary资源GET/PUT仍404；另一当前admin不能用不同UserID的key观察原receipt |

新真实每顶层2m、整包沿原6m/race/count1/p=1，不自动加预算。前版本升级用固定6fa产品的最小实际构建闭包与任务自有二进制，不能用候选自造“旧receipt”代替跨二进制；实际命令、旧/新原字节、数据库状态切换与writer终局必须保存。前版本仅服务自有fixture；禁止触碰既有基础设施。若现fixture不支持这条准备方式，作者先报精确接缝，不改旧公开fixture或把静态golden冒成真实升级。

必要已接受旧顶层按影响运行：`TestModelB01PlatformSelectionAndAtomicReplacement`、`TestModelB01PreparedReferencesAndCurrentAdminAreRechecked`、`TestModelB01SecretReferencesAndAllEffectsRollback`、`TestModelB01ConcurrentSameKeyPreparationFailureStillReplaysReceipt`、`TestModelProjectReceiptReadGateAndLegacySystemPlan`、`TestModelProjectUnboundReferencesAndDeleteBarrier`、`TestSystemModelManagementDeletionImpact`、`TestModelSystemHTTPSelectionAndReferenceDeletion`、`TestModelSystemHTTPCurrentAdministrator`、`TestModelSystemHTTPBoundaryAndStrictWire`、`TestModelCurrentResolutionSelection`、`TestModelCurrentResolutionReplay`。名称先精确list，no-tests不能计通过；不为本卡重跑停止任务。

独立至少两组：A真实PG授权/两owner完整映射/同Tx原子性与Unknown；B跨前二进制升级/旧receipt/实际旧HTTP新group及严格客户端。独立不能只复跑作者断言。旧层已接受证据可按未变输入复用，返修只重跑受影响范围，首红和原输入不可删除。

每轮资源执行另获唯一窗口；真实完整PG/MinIO fixture与nonce-owned环境、固定工具、原资源完整Mount/labels基线、exact容器/网络ID、PID/starttime、direct/adopted实际wait、源前后hash、两次清零与交窗均沿团队规则。不存在的证据不得重构补造；一轮失败先实际收尾，不能重跑到绿后擦除原红。

## 7. 后继边界与完成

S2正式规格消费本卡接受的独立state/request/receipt，可能用`/system/model-selection/meeting-summary`和同页独立form；先核默认dispatcher前缀与已有lookup，再绑定初始化/HTTP。原四项仍独立保存，Summary不因embedding/memory未配置而被阻塞。S2负责真正浏览器使用、新请求不确定性恢复和默认root；S1两文件client兼容不是S2完成。

S3必须改Purpose→platform映射、读取Summary而非Memory、独立SH锁/selector version、plain text profile与原完整consumer授权/Secret witness。初始未配置/停用导致新生成失败、无fallback；已accepted逻辑调用/重放使用原snapshot，新generation才读当前选择；旧project_summary请求不得静默改义。S1没有修改这些契约或模拟生产消费。D24负责真实Meeting事实、finalizing、首轮标题和Summary同事务、usage归属与业务重试；它不能从系统配置成功推断生成已通过。

本卡S1通过须技术范围全部实现、适用独立证据通过、前后端旧路由兼容完整、无stub/临时默认/静默成功，相关写入/命令/资源实际停止，由root采纳并只提交已验完整结果。最终指南待精确文件交还后单独核准，台账按真实阶段更新。此rev1已独立完整STATIC接受并获root采纳，相关11份架构、布局与设计文档已同步用户决定；尚未实施、未动态验收，00020仅为唯一预留，不授予迁移源或业务开写权限。
