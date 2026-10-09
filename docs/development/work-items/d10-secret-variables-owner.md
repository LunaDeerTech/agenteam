# D10 Secret Variables Owner 后端

> 状态：工程 SPEC rev2 已获 Model 独立有限接受；A 纯合同、Schema及独立复验资产已正式交付 main `8cb0a953`。Owner Service/HTTP、D04提供方、SQL及生产组合尚未交付，不能将A视为整卡通过。原SPEC基线main `3cea6076`；§4/§6专用D04端口、回执轮换/清理和真实事实证明仍是后继前置。实施授权及共享所有权见§10。
>
> 拟完整结果：已初始化 Project 的当前 Human Owner，经默认 Central HTTP 创建 Secret Variable、读取安全元数据、分页、修改 name/description、覆盖 value、删除及恢复响应丢失。与普通变量共享业务 ID 和名称空间；明文不进入读取响应或持久命令。另落实 Agent F1 的资源侧目录/引用协议；真实 Agent canonical/引用适配由 F1 完成，不以测试 owner 代替。

## 1. 正式来源与真实依赖

对照 [变量架构](../../architecture/project-work-management/project-environment-variables.md) §2–5、§15–18，[普通 Variables](d10-project-variables-owner-http.md) §2–7，[Agent F1](d10-agent-configuration.md) §6.2，[D04 Secret](d04-security-design.md) §5–6 及 [Secret 后端](../backend/secret.md)。按工作采用 design、Go、database、security、test-engineering 和 verification 技能。

| 依赖 | 已有能力与仍缺责任 |
| --- | --- |
| Account/Project/Postgres | 正式 Session、当前 Owner、Project gate、同 Store caller Tx、完整锁、CommitResult、Activity 可复用；须实际组合验收 |
| 普通 Variables | main 已有 `identity.ProjectVariableID`、共享目录/名称规则、Owner HTTP/root；本卡前向扩展，保留原普通值语义 |
| D04 | 已有真正信封加密、nonce/epoch、Prepare/Apply、加密 receipt digest、Audit、引用/lease及轮换；§4 专用组合口仍待实现 |
| Audit/Outbox/Cursor | 复用同 Tx 事实与严格闭集/签名游标；新事实须接来源证明和现读取端 decoder |
| Agent F1 | 尚无真实 Agent 配置 owner；本卡资源侧端口不等于真实 Agent 创建/引用通过 |
| MCP/Runner | 已定同一 Project Secret 可供 MCP backend 和获白名单 Agent 使用；本卡不创建连接、执行 lease、resolver 或材料下发 |

UI、AgentConfig/Prompt、执行环境注入/masking、MCP revalidate、Project 完整生命周期及生产 AgentRun 排除。无新增 Object/OpenAI 前置；原 Object runtime join、OpenAI tools 等停项保持，默认 root 原 Object/MinIO 启动要求不删减。

## 2. 共享目录、字段和兼容

继续扩展 `internal/central/projectvariable` 和现 `agenteam_projectvariable.variables`，不另造 Secret 名称表。VariableID 与 CredentialID 不互转，Owner 不可将任意已有 Credential 挂成变量。Secret 行只存同 Project 稳定 CredentialRef 的内部映射和 Credential version，不存 value/ciphertext/DEK；普通行仍存普通值。新 migration 前向增加互斥 payload CHECK、一对一映射约束，保留原存活 `(project_id,name)` partial unique；不改 `00024`。

| 字段 | 规则 |
| --- | --- |
| id/project_id/type | 调用者提供 canonical UUIDv7业务 ID，全局不重用；type固定secret，不接任意type或variable/secret互转；跨Project不可见 |
| name/description | 完全复用普通变量的大小写、保留名、UTF-8、长度/控制字符规则；两type共享名称空间 |
| value写入 | 1..65536 UTF-8 B，沿D04 MaxValueBytes；严格Unicode scalar、无NUL、不trim/规范化/解释；下限沿D04非空材料，空串不表示删除 |
| version | 对外Variable version，正int64十进制字符串；真正metadata改变、每次显式覆盖、删除各+1。内部Credential version独立，不能以一个expected替代两域版本 |
| 时间 | DB微秒时刻；真实改变刷新updated_at，no-op不刷新 |
| 安全metadata | 恰 `id,project_id,type,name,description,version,created_at,updated_at`；type=secret表示受保护值存在，不返回值、长度、尾号、摘要或固定星号伪值 |

update 的 name/description/value 至少一个 presence 字段，缺席保留；null/重复/未知key拒绝。未传value的精确metadata no-op先校验expected，保存安全receipt，不增加version/generation/history/Audit/Event/Activity。显式value总是覆盖，沿D04每次覆盖新密文的规则；不解密当前值优化“相同明文”。覆盖保持VariableID/CredentialRef。

删除通过§5后，同Tx逻辑删除变量/清description与活映射，D04删除业务值；只保恢复/审计必要安全身份。旧名可供新ID，旧ID不复活。completed receipt可留安全历史metadata，不承诺备份擦除。Secret值及可离线枚举的值摘要不得进入普通command JSON。

旧 `/variables` 继续只处理type=variable；Get/Update/Delete误用Secret ID返回NOT_FOUND，List/容量SQL显式过滤type，避免把新行交给旧严格DTO。两类create/rename仍共用Project EX和名称索引。本SPEC工程限额保持原4096普通变量上限，另设4096活Secret上限；分页generation区分普通与Secret视图，保留旧普通cursor语义。满Secret容量只拒新增，不阻止修改/删除；受限错误只含固定code/field。

## 3. Owner 命令、恢复和事务

新增独立Secret DTO，不扩旧Variable的value/type union。候选命令 `project.secret_variable.create|update|delete`。最小端口为 Create/Update/DeleteSecretVariable、Get/ListSecretVariables、LookupSecretVariableCommand；create输入业务ID/name/description/value，update用presence，delete无value。对外expected仅来自CommandMeta.ExpectedVersion，create必须无、update/delete必须有。

授权顺序复用普通变量：合法Human → 同Store活Tx/完整锁 → 当前Session/Owner Read、已初始化Project → 原command writer/历史receipt → 新写才Mutate → 目标/type/expected/名称/容量。archiving/archived可读及重放完成receipt，不能新写；pending/deleting无历史旁路。其他User含非Owner管理员不能获存在性/正文；AgentRun/Service不获管理能力。

拟采用单一final业务Tx：短只读发现当前metadata/映射/version，事务外准备D04 sealed候选及Outbox plan；final一次AcquireAll完整union，重验当前权限/版本/前像并原子提交。没有值明文或未确认nonce候选的持久计划，没有后台worker。准备失败仅消耗合法nonce；重新尝试重新准备。Secret commands仅持久completed行，故Lookup为committed/not_observed，不虚构持久in_progress，也不自动重发缺行命令。

可新增本域 `secret_commands`，保存command/key/actor/target、原metadata presence、安全receipt/history以及D04 intent receipt身份；不能另造业务目录。不得复用旧request.value、receipt.value或含明文的裸semantic_digest。safe receipt按create/update/delete闭合，含安全metadata/墓碑、changed、实际event/audit ID；no-op ID为null，不暴露CredentialRef/proof/key/digest/Session。后续更新、删除、同名新建不改历史receipt。

Unknown保原writer Attempt/Cause/CommitResult，不重跑callback。最多一次3s独立当前权限确认，由Service跟踪并可Stop/Drain；只有D10完整receipt与D04原意图事实闭合才成功。缺行/超时/撤权/坏记录都不证明原NotCommitted；HTTP不自动换key/version重发。明确NotCommitted沿原错误返回。

## 4. D04 专用组合能力

### 4.1 归属、端口和原始意图

当前不是现成依赖：`WriteCommandLookupRequest.Validate`仅Model；Purpose没有ProjectVariable；现 `metadata.Purpose==consumer` 无法直接表示既定的同Secret供MCP与Agent环境使用。新增归属 `ProjectVariable = "project_variable"`，只允许Project scope和本专用组合口；不借Model/Runner purpose，不复制两份值，不扩旧Model Lookup的适用范围。Secret的contract定义新typed端口，D04实现加密和受保护回执，D10提供真实映射证明器；root注入同一Store/Secret实例，缺口返回DEPENDENCY_UNBOUND。

`secret/contract/project_variable.go`的类型边界如下；名字可在编码时依Go习惯微调，字段含义、权限和生命周期不能弱化。只导入identity/foundation及既有Secret类型，D04不反向导入projectvariable产品包。

| 类型/工厂 | 精确内容与有效性 |
| --- | --- |
| ProjectVariableCommand | 闭集create/update/delete；外层CommandIdentity namespace=`projectvariable`，scope parts恰ProjectID，command=`project.secret_variable.create/update/delete`，key沿原CommandMeta。D04专用回执与Audit直接绑定这个原identity，不造第二个Model或legacy写命令 |
| NewProjectVariableIntent(fields) → opaque ProjectVariableIntent | fields为当前Human Actor、ProjectID、VariableID、原CommandIdentity、外部expected的presence/值、name/description/value各自presence和原始内容；value为SecretMaterial。create三字段全有且无expected，update至少一字段且有expected，delete只有expected。工厂仅校验输入，不产生权限 |
| ProjectVariableWriteBasis / ProjectVariableWritePlan | authority私有issuer签发；绑定同Store、精确Actor（含当前Session）、原identity、Project/Variable、操作、只读发现的变量version/映射CredentialRef及Credential version，create为精确未存在事实、候选Ref和ID不重用；RequiredLocks与安全binding不可变。不得含明文或低熵值的裸摘要 |
| PreparedProjectVariableWrite | D04同实例opaque候选，绑定intent和authority plan、key epoch、随机receipt ID、可选sealed value、sealed intent digest及完整锁；不序列化恢复、不跨实例/进程使用。显式Destroy清理可控临时摘要/材料，fmt/slog/JSON只输出固定安全标签 |
| ProjectVariableWriteObservation | typed未观察或完整安全结果：原receipt ID、Project/Variable/稳定User/command、原external expected、CredentialRef、值效果及结果Credential version、deleted；无value/digest/ciphertext。只供本域内部，HTTP另投影 |

请求的受保护semantic为带版本和字段长度的无歧义编码，恰绑定稳定User、Project、原command、VariableID、external expected presence/值、name/description/value presence及原字节。**当前canonical、内部Credential version、随机候选Ref/receipt/event ID不属于原请求语义**；它们绑定在不可伪造的basis/实际结果中。否则后续修改或删除后无法重放原请求。Session/CSRF/RequestID也不属于原semantic；每次调用仍重新核当前Session和Owner。D04在内部计算32字节摘要并加密，不开放接收caller裸hash的通用保护口，也不给D10返回该摘要。

| 专用窄口 | 行为和同Tx证明 |
| --- | --- |
| ProjectVariableWriteAuthority.Discover(ctx, request) → plan | D10只读发现；request含Actor/Project/Variable/原identity、操作和external expected presence/值，不含材料。新写检查当前Owner/初始化及canonical前像；历史观察按当前Owner Read和自己原completed receipt绑定，不要求旧canonical仍存在。返回本authority的opaque plan和完整锁 |
| ProjectVariableWriteAuthority.CheckInTx(ctx, tx, request, plan, stage) | 同Store活Tx、原issuer/Actor/identity、完整持锁、当前Session/Owner；stage闭集ReceiptRead或NewWrite。ReceiptRead绑定D10原writer及安全receipt或确实未观察；NewWrite另核Mutate、external expected、映射前像/create未存在。不得自开Tx/补锁；缺表行不是任意create许可 |
| PrepareProjectVariableWrite(ctx, intent, plan) → prepared | 最外层Tx之外；验证专用typed语义/同实例plan，准备合法nonce和sealed候选。仅nonce预留可持久；不提交值或业务planned行。发现已完成原receipt时只准备比较所需内容，不再生成业务值 |
| MatchProjectVariableIntentInTx(ctx, tx, prepared) → observation | 完整锁、当前ReceiptRead授权后，读取本专用receipt并在D04内部解密原intent digest、常量时间比较；未观察、匹配、IDEMPOTENCY_KEY_REUSED分明。completed写重放必须调用；不同Project/Variable/writer不能命中 |
| ApplyProjectVariableWriteInTx(ctx, tx, prepared) → observation | 再核NewWrite证明和D04自身scope/purpose/版本/epoch/ref/lease；同物理Tx写值和加密intent回执。只接受已完整预取锁的候选，拒foreign/ended Tx或漂移。metadata-only/no-op也落完整intent proof，Credential version保持 |
| LookupProjectVariableWriteInTx(ctx, tx, request, plan) → observation | identity-only、当前ReceiptRead授权及同Tx安全观察；不需原value，不等于验证caller原semantic。只能连同D10完整receipt恢复安全结果；不使用generic Model Lookup |

D04 Apply与D10 canonical顺序固定：完整union及两域当前前像校验 → D04值/intent回执及适用Audit → D10 canonical/映射/版本/generation、history与completed receipt → D10 Audit/Outbox/Activity → 同一commit。D04证明器查自己所属D10表的原前像，不以尚未存在的postimage循环授权；最后两域结果精确相合。所有失败回滚同一业务Tx。并发同key在准备期间获胜时，先匹配真实原receipt；新create候选Ref与原Ref不同需要另锁时退出Tx后重新发现，最多一次准备重试，不能锁内补锁、重base expected或重放Unknown callback。

### 4.2 持久回执、信封和轮换

新增D04自有 `agenteam_secret.project_variable_receipts`，与D10 `secret_commands` 一一对应；只在最终业务Tx完成时插入。最小列为receipt ID、ProjectID、VariableID、稳定UserID、原command identity摘要、closed command kind、external expected presence/值、原Credential ID、effect（create/replace/delete/none）、结果Credential version、deleted、digest_payload_id。唯一约束为(ProjectID,command identity摘要)及digest_payload_id；安全字段不含name/description/value/semantic摘要。D10记录原receipt ID和安全结果身份，不复制密文；跨域不建CASCADE或依赖canonical尚存的FK，历史delete/replay须可用。

扩D04信封owner闭集为 `projectVariableReceiptOwner=3`，owner ID为新receipt ID；owner1值、owner2旧回执的格式和AAD字节保持不变。新owner3仍使用现AES-256-GCM/DEK/nonce/keyring/格式1，AAD显式包括kind3、Project scope、receipt ID和payload ID；plaintext固定32 B，ciphertext固定48 B，SQL CHECK和seal/open/scan/AAD验证同时扩精确分支。不得仅重用owner2却让rotation仍只查旧表，也不将新purpose加入无关lease consumer的合法闭集。

现 `rotation.go` 的receiptOwner解析只查询 `secret_command_receipts`。本结果必须增加kind3 → 新receipt的精确owner/payload/Project/Credential反查，再沿原Project SH、Credential EX、write-key SH、epoch和payload CAS执行；delete后的历史receipt仍保原Credential ID用于锁，不要求当前Credential还存在。owner映射缺失但payload还在必须安全失败；并发已整体清除的payload可沿旧规则跳过。每批仍100，末尾重扫和写key EX退休检查覆盖全部三种owner；canary/启动校验不能漏kind3。新表不是D10自有第二套密钥设施。

低层legacy Human/Model写必须按**当前存储归属**拒绝绕过组合口修改/删除该Credential，包括purpose转换，不能只看请求purpose。新的Purpose.Valid不自动使所有旧写口、Account/Model adapter、reference或lease consumer放行。矩阵覆盖请求旧purpose但目标已属ProjectVariable、伪造create、新归属System scope和合法专用写对照。

### 4.3 CleanupProject及后继材料边界

新回执纳入现D04 `CleanupProject`：仍由真实ProjectLifecycle actor、原operation/cause和D08当前删除/停机gate授权，Project EX内先核全部live references/active leases；有任一则pending。原值及旧回执清理不变，新表额外每批最多100行，删除所返回的exact digest_payload_id并核kind3/owner/Project一致；同Tx删除回执和payload，Unknown保原结果，不根据checkpoint跳过未确认行。最终Completed必须同时确认secrets、旧回执、新回执和全部Project payload为空。删除后的历史回执、metadata-only/no-op回执也必须被扫描；不能只随活Credential级联。Owner删除不提前清这些历史回执。

这只是D04已有清理端口对新存储的必要覆盖，不交付D10 Project完整Cleanup participant。D08后继必须先让真实Agent/MCP等owner移除引用、确认停止，再清D10目录/引用/命令历史和D04材料；各域只删自有表、原cause不替换，不借本SPEC宣称该后继编排已绑定。缺正式D08 gate时D04仍拒绝，不能以全部表空跳过当前gate。

存储归属与后继消费用途分开：本卡只开放Owner组合写，**不自动开放MCP/Runner lease**。将来UsagePlanner/Authority证明真实binding/执行与当前gate后，D04才按窄规则允许ProjectVariable归属用于该consumer；不全局放宽purpose比较。F1只维护白名单，无需材料解析。

D04真create/覆盖/delete保一次secret.create/update/delete Audit；metadata-only/no-op不伪造值变更。D10为真实变量语义变化另记一次project.secret_variable.create/update/delete，以VariableID为资源。Owner metadata/Lookup无secret.resolve；具体native Audit证明见§6。

## 5. 目录、同Store引用与删除

资源侧目录仅返回同Project稳定VariableID/安全metadata和typed valid/removed/not_in_scope，一一对应请求ID，无缺项/重复；本域没有disabled状态，不补造启停。跨Project/不存在安全不可区分；普通变量不能入白名单；读取错误不能变空集合。目录不是bearer grant，不建runtime lease。

候选资源API：DiscoverSecretVariables→opaque SecretDirectoryPlan，RequireSecretVariablesInTx→SecretDirectoryFacts；DiscoverSecretReferences→opaque SecretReferencePlan，ApplySecretReferencesInTx只在caller Tx维护。plan绑定issuer、Actor/User、Project、command、完整排序ID、Before/After、owner identity/version、映射和锁，不可反序列化构造。Discover短只读；InTx先同Store/活Tx/持锁再当前权限/计划/canonical检查，不自开Tx、补锁、解密、网络或Activity。

本域引用表持Project/VariableID/owner_kind/owner_id/owner_version。本卡仅定义agent白名单类型、typed AgentID和F1最多256项集合；不准任意字符串owner。Agent F1提供SecretReferenceOwnerAuthority：事务外发现Agent gate及预期postimage；final证明同Tx真实Agent canonical、创建初始化witness或当前版本及完整postimage。创建前不存在必须由精确create plan处理，不能要求事务外已有Agent，也不能单凭caller承诺放行。

资源contract的 `SecretReferenceChange` 固定为Actor、ProjectID、typed AgentID、原Agent CommandIdentity、create/update操作、expected owner version的presence/值、结果owner version、按ID排序去重的Before/After集合（各≤256）。`SecretReferenceOwnerAuthority.Discover(ctx, change)` 返回绑定同Store/issuer及完整锁的opaque OwnerPlan；`CheckAppliedInTx(ctx, tx, change, ownerPlan)` 必须在真实Agent canonical写入后验证精确postimage。旧Before、原expected及create初始化事实由Agent实际写入点的同Store/同Tx私有witness证明，不能以已覆盖的canonical反推，也不能仅以public DTO或历史receipt放行。F1是该端口唯一实际提供方；本卡不实现Agent SQL或成功返回的假authority。

资源侧Discover组合OwnerPlan和每个Variable映射/版本，返回本域issuer的SecretReferencePlan。Apply在同caller Tx一次验证上述两类plan、当前Owner/gate和真实postimage，再精确比较本域原引用集=Before及owner_version、原子写After/新owner_version；Before为空也要验证完整查询和create/update语义，不能误当无权限路径。Directory Facts和Reference plan均不得作为可序列化bearer。F1创建/更新的canonical、引用集、Audit/Event/receipt同Tx回滚；资源端不得替Agent生成自己的写事务。删除/生命周期释放需要后继正式操作类型和原cause，不能借Owner update接口伪造Agent删除。

本卡交付资源侧注册/plan/验证/表维护及未绑定拒绝；生产尚无Agent owner时引用变更明确DEPENDENCY_UNBOUND，不能造空Agent provider。只有F1真实adapter交付后才声明白名单新增/移除闭环。Owner删除查询本域真实引用表；存在引用即使adapter缺失也拒绝，未知owner行安全失败。对自有权威引用表的完整查询不是“外域空checker”；真实消费者只能经已注册且实际验证canonical的写口落引用。

资源删除/覆盖/改名持Project EX；引用变化和目录final至少Project SH，并入Agent EX及所有D04/reference锁，与Agent canonical/version/receipt/events同Tx。Before/After及owner_version同步，无query-then-save窗口。完整union按Foundation锁序预取，不用跨域FK/CASCADE代替权限。

Owner Delete确定为：当前权限/version正确，且本域Agent引用为空、D04 live references/active leases均为空，才同Tx删除；否则RESOURCE_BUSY。先由配置owner正式移除引用，Owner再重试。禁止自动删白名单、解绑MCP、终止进程或释放lease；沿D04现有受引用拒绝契约。架构的删除后失效白名单清理/unavailable留其他合法生命周期，不在本卡新增级联行为。缺必需证据/坏计划/未知消费者不能当无引用。

## 6. 原子事实、Audit和完整锁

final union至少外层及必要D04 command EX、User EX、secret-write-key SH、Project EX、Credential EX、全部Outbox/reference record；一次Normalize/AcquireAll，遵正式rank，不在高序锁后补registry。不能直接在高序caller Tx套旧References便利AcquireAll。发现变化时整体拒绝或有界重新准备，不自动rebase expected。

原子提交变量canonical/映射/generation、D04值和受保护intent receipt、D10 completed receipt/history、两域适用Audit、typed Outbox及Activity；任何一步失败全部业务事实回滚。合法nonce预留消耗是既定例外。当前Session/Owner/Project、两域版本/epoch和前像在final再核。

候选event：producer=projectvariable，event=project.secret_variable_changed，aggregate=project.variable，schema1；payload仅variable_id/operation_id/change/changed_fields。Audit三action使用resource=project_variable，仅VariableID/version/字段名，因果绑定原command。name/description/value/值hash/CredentialRef不进这些metadata。ProjectVariable Authority与D04 ProjectAuditAuthority分别证明真实同Tx事实。

### 6.1 单final Tx的Outbox事实来源

现 `projectvariable/authority.go` 的 `appendBinding` 强制原普通command.Plan/EventID，DiscoverAppend又先读取其持久command。它不能直接证明本卡尚未持久的Secret准备阶段。原普通分支逐义保留，新增只处理 `project.secret_variable_changed` 的专用分支；不能取消旧plan检查或让全部caller只凭Event DTO通过。

D10实际命令准备在读权限/前像和D04准备完成后，为非no-op生成安全event及**包私有call-local discovery witness**，再调用现Outbox.PrepareAppend。witness绑定同Store、Authority私有issuer、当前Actor、原CommandIdentity、Project/Variable、operation ID、目标version、原安全前像、exact Event Summary及D04候选receipt ID/Ref、完整command/Project/Credential锁。只在本域真实准备函数签发，私有context key或同等不可伪造的内部能力传递；不导出任意DTO→witness工厂，不含材料/值摘要，不持久化。DiscoverAppend校验本witness后用本authority自己的PlanIssuer生成Dependencies；binding只含上述安全身份、Summary及锁。copy/重放其他Project、Session、event或command均不匹配。缺witness的公开Outbox调用拒绝。

最终AppendEventInTx仍走真实Outbox两stage及Project事实gate。CurrentAccess核本issuer/Actor/summary/完整锁和当前Session/Owner；NewFact读取**当前caller Tx中**的Secret canonical或墓碑、history、secret_commands completed row与其安全结果，核原operation/event/目标version/变化字段及exact D04 receipt observation相合。D10已完成行只在同一未提交Tx内暂可见，后续任何Audit/Outbox失败整笔回滚；不能只凭prepared witness充作已提交事实。无跨事务planned command，也无以allow替代CurrentAccess。no-op/replay不准备或Append新event；重启后的响应恢复读正式completed事实，不试图恢复call-local plan。

Project的producer事实路由必须把新event/action导向同一个真实ProjectVariable Authority，Outbox Catalog、Project allowlist同步扩闭集。此私有准备分支是D10 Authority新增责任，现普通adapter与Outbox本身不能冒称已具备。

### 6.2 两域Audit的opaque证明

现D04 `project_audit_witness.go` 只在native applyWriteInTx的canonical/旧receipt成功后签发私有context witness，`project_audit.go`又验证旧receipt及legacy命令。这些事实不能由新typed DTO冒充。新增native ProjectVariable Apply的私有mutation variant：只在本专用值/新receipt实际写入后、紧邻Audit.Append签发，绑定同Store/同Tx/当前完整Actor/exact Entry与AppendKey、原外层CommandIdentity、kind3 receipt/payload、原CredentialRef/前版本及实际result/value payload。witness不携材料、sealed candidate、Service或Keyring；固定安全fmt/log。

D04 ProjectAuditAuthority精确区分legacy/new/resolution三种互斥分支。新分支验证闭集外层identity、原producer=secret/ordinal0及command因果、所需持锁、专用receipt的原Project/Variable/稳定User/effect/result、kind3信封owner和payload关系，以及当下Credential canonical/value payload或delete缺行。create version=1，replace/delete恰前Credential version+1，purpose固定ProjectVariable；safe Audit metadata仍是原secret.create/update/delete的真实值变化语义。receipt effect=none绝不签发值Mutation witness，不伪造secret.update。任何public构造器、伪receipt ID、移植context到foreign Tx、错Session、旧witness改Entry均拒绝。

D10自己的Audit通过本域secret_commands/history和canonical证明精确Variable变化及原命令，使用同一外层identity但producer=projectvariable，故与D04的Audit去重键区分。两域Audit均由当前Project事实路由先重验Owner/gate，D04存储证明器只负责自己的事实；不新增“Service拥有任意Project写权”。identity-only Lookup、metadata-only D04路径和no-op不产生Secret Resolve/Mutation审计。已归档历史写重放只返回原receipt，不补发任何Audit/Event。

SQL CHECK、Audit contract、Project事实路由、HTTP/OpenAPI及现客户端decoder必须同步严格扩闭集，旧Audit仍可读；不宽松接受未知action。Activity仅真实变化，无no-op/replay增量。

## 7. HTTP、输出、分页及默认根

候选P=`/api/v1/projects/{project_id}/secret-variables`，保留原 `/variables` 的普通值可读协议，不新增混合类型UI。沿现精确rawpath/canonical ID、Cookie Human、Origin/CSRF、method/Allow、strict JSON、媒体和安全Fault规范。

| 路径 | 语义 |
| --- | --- |
| GET/HEAD P、P/{id} | 摘要页/安全metadata，不读/解密/返回value |
| POST P | request含variable_id/name/description/value，返回安全create receipt |
| PATCH P/{id} | expected_version及presence request，返回安全update receipt |
| DELETE P/{id} | expected_version，返回安全delete receipt，不用204丢回执 |
| POST P/commands/lookup | 原Idempotency-Key、command/target_id及原expected（create无），返回安全历史观察；不接受value或ordinary semantic_digest |

Lookup不声称证明caller仍持原明文；显式写重放才重新提交完整原意图并由D04比较。同key改value/metadata/presence/version/target拒绝，不能以identity-only Lookup替代写比较。同User新Session可恢复，旧Owner失权不可恢复。

工程cap为input/detail/receipt/Lookup≤1MiB，list≤5MiB；验收实测65536 B value最坏escaping。strict拒unknown/duplicate/大小写别名/null/无效UTF-8/孤立surrogate/尾随值，错误末项不得发半页。总read/Lookup 2s、mutation30s、受跟踪确认最多3s沿现预算。

分页沿C name/id keyset、默认50/最大100、cursor≤8192 B；kind=project.secret_variables/type=secret，绑定User/Project/Secret generation。换limit/同User新Session可续，真实Secret变化stale，no-op/replay不变；两类cursor不可互用。Owner目录可见不授权未白名单Agent知道Secret存在。

写raw body/decoded value尽早移入SecretMaterial并清可控byte副本，不长期保留不可擦除string DTO；诚实保留Go临时副本/GC擦除限制。日志只模板/request_id/固定code/state/计数，禁body/enclosing DTO/错误链泄露。DB普通列、Audit/Event、diagnostic、所有GET/HEAD/Lookup/receipt用canary验无明文或值摘要；无plaintext read endpoint。

root复用同Store/Account/Project Authority/Secret Service及keyring、Variable facts、Audit/Catalog/Outbox。先构造无材料事实Authority，再注入Secret组合写授权，再构造Service/HTTP；无late-fill或复制ProjectAuthority绕证明。未绑定Agent/MCP/Runner保持拒绝。构造器拒typednil/已知异Store。

Stop关闭admission并取消读写/Lookup/确认；Drain等实际函数/Rows/Tx/I/O回调结束，不关共享Secret/Store。根注册同一call owner，失败构造和晚到install实际退役；沿既有私有deadline/Read/Close/Write/Flush/join与Force总预算，不从取消或时间经过推断joined。

## 8. 文件与迁移唯一owner

root 已授权并分配 A 纯合同结果给 `/root/knowledge`：五个新的 `projectvariable/contract/secret_{types,commands,query,events,directory}.go`、其测试、独立 `api/openapi/secret-variables.json` 及本文/current。其余下表仍是实施提案，未授予共享源、D04、生产装配或 SQL 写权。

| 拟文件/接缝 | 责任 |
| --- | --- |
| projectvariable/contract/secret_types.go、secret_commands.go、secret_query.go、secret_events.go、secret_directory.go及测试；本域secret_service/commands/repository/reader/directory/references/http等源与测试 | Secret Variables作者；仍在原业务域 |
| 现projectvariable service/repository/reader/commands/authority/events的必要兼容 | 同作者获得共享写权，普通变量作者停对应路径；旧行为回归 |
| secret/contract/project_variable.go、secret/project_variable_write.go、project_variable_lookup.go、原Purpose/legacy写入/envelope/receipt/rotation/canary/cleanup/ProjectAudit兼容 | root明确D04接缝唯一owner，同一实施闭包可交同作者，必须独立安全审查；kind3及新回执全生命周期属于同一个必要前置，不留无人负责部分 |
| 一条或必要有序前向migration，编号TBD | root按当前全局顺序分配唯一owner；统筹变量shape/intent receipt/kind3/Purpose/Audit CHECK，不占Knowledge/Runner预留，00028现由root给Skills后继需求预留，本卡不占用；不改旧migration |
| Project/Audit/Outbox精确allowlist、api/openapi/secret-variables.json、现Audit schema/client decoder | 单一共享文件owner，保留旧完整闭集；不占无关UI/client全局文件 |
| app/project_variables.go、project_usage.go、account.go及Secret装配/测试 | 唯一装配owner，保留main真实普通变量和现Secret用途 |
| tests/projectvariable/secret_*、native/defaultroot/process | 作者/独验各自拥有自己的测试文件，真实资源统一排窗 |
| Agent F1 owner adapter、MCP/Runner材料适配 | 后继真实owner；本卡不伪造canonical/成功端口 |

Knowledge B02另一树的未合入源、迁移和binary不混入本基线。main后继合并后按正式提交核必要差异，不跨树全文件覆盖共享Authority。

## 9. 有限验收矩阵

SPEC接受后实施，作者完整矩阵一次确定；独验补真正风险，不无限新增top。上游未变加密/nonce基础可复用，新增归属/intent receipt/rotation必须有本组合证据。SPEC阶段无PG/socket/browser/network。

| 风险 | 必验结果 |
| --- | --- |
| 协议/schema | type/ID不混用、presence/边界/最坏escaping、opaque安全输出与clone、历史Lookup和完整写重放区别 |
| 迁移 | fresh、main含普通变量/历史command/旧Secret/Audit升级、重跑/DDL回滚、共享名称/payload互斥/ID不可复活 |
| Owner闭环 | 真实Account/Project/D04 create→metadata/page→改名/覆盖/no-op→delete→历史Lookup/replay；重启同映射/receipt；值仅在测试授权D04材料口核对 |
| 原子/安全 | 每个独立事实边界回滚；普通DB列/日志/HTTP无canary；坏intent proof不成功；legacy写不能绕变量归属 |
| 权限 | 跨Owner/admin/Project/type、撤销/过期、新Session历史；prepare后撤权/Owner变化/Archive两种真实提交顺序 |
| 竞争 | 普通vsSecret同名create/rename、同ID跨Project、同key同/异义、同expected覆盖/update/delete；真实PID/key/mode/waiter屏障，无sleep猜测 |
| 引用 | 目录同Store/活Tx/锁/issuer/跨scope、缺Agent owner拒绝、资源真实引用表与Owner删除保护；Agent创建retain/release正向待F1真adapter，不能用假owner记通过 |
| D04/Unknown | 实际ref/lease保护、final COMMIT未转发/提交丢响应、新SessionLookup/原replay、原Attempt/Cause；新intent payload实际rotation/退休、旧epoch拒绝 |
| D04新增存储闭包 | kind3 AAD错owner/Project/payload拒绝、owner1/2历史解密兼容；metadata-only/no-op与已删除Credential的receipt轮换；超过100新回执多批Cleanup、真实当前gate/引用保护、Unknown不跳行、最终所有payload空 |
| 真实证明接缝 | 无持久plan的专用Outbox prepare→final真实事实通过；缺/伪witness、错Actor/Tx/Store、prepared无canonical、跨event/receipt复用均拒绝；D04新native Audit variant与effect=none无值Audit；旧普通Outbox/旧Secret Audit回归 |
| HTTP/root/退出 | 默认根Owner完整路径/实际schema/普通API回归；Body/Write/Close/Flush/确认取消与实际join；进程Wait、自有资源/runtime/desc/TCP完整尾 |

未参与实现者至少独验低熵值和metadata-only/覆盖改义与当前撤权、共享名称/删除引用竞争及final Unknown、新Purpose/legacy绕过/receipt轮换。纯probe不代替真实权限/事务验收。

## 10. 草案收敛与结果边界

rev2在现产品规则内提出闭合工程范围：专用purpose/typed intent与sameStore authority、新kind3加密回执和rotation/CleanupProject、单final Tx的私有Outbox准备及两域native Audit证明、资源侧Agent引用端口。§2/§7限额是本SPEC工程选择，随本修订审查，不宣称已经产品实测。没有新增明文权限、级联删除或消费用途决定。

rev2 SPEC 已获 Model 未参与者有限接受（ee4095）；A 纯合同已获单独授权。Owner 服务实施仍须 root 在当时正式 main 上分配 D04、D10、Project/Audit/root 共享路径的唯一 owner 和前向迁移号；确认本结果会实际实现并验收全部§4/§6新增端口，不能把“只改D10”当闭合交付。D04旧API的存在不证明这些前置就绪。Agent F1 adapter是另一真实后继，由F1作者在已定端口上实现/验收；本卡的未绑定拒绝不阻止Owner CRUD开发，也不构成F1引用正向通过。

已有产品规则足以限定Owner后端：无明文读、固定type、覆盖保持ID、有引用拒绝删除。本草案不新增级联清理、自动解绑、空Secret、进程热更新或Secret使用scope。若后续目标确实要求这些额外行为，再回到正式产品规则；不为用户未请求功能补问。当前未发现阻止Owner SPEC编写的新产品决定。

SPEC接受仅准冻结范围开工；Owner后端须产品/HTTP/root与独验闭合；F1 Secret前置须真实Agent adapter同Tx创建/引用实证后才完整闭合。任何一层都不等于完整D10、MCP/Runner材料使用或Project生命周期完成。

## 11. A 纯合同作者结果

五个新 Go 合同源定义独立 Secret 命令闭集、安全 metadata/receipt/两态 Lookup、专用事件及 F1 目录/引用端口。请求构造器克隆 `SecretMaterial`；`UseValue` 只借同步私有副本，`Destroy` 清可控材料，JSON/fmt/log 对请求只给固定标记。strict JSON 解码保 presence，拒空值/NUL/坏 UTF-8、孤立 surrogate、重复/未知字段；无 plaintext semantic digest API。metadata 不含 value、摘要、长度、mask 或 CredentialRef。

目录/引用计划按独立 issuer、完整 Actor（含当前 Session）、Project、原 Agent command、AgentID、expected/result version 和排序集合绑定；计划不可反序列化，锁集克隆归一且至少包含原 command/User/Project gate，引用包含 Agent EX 及完整 owner plan 锁。空集合 nil/empty 语义一致。`SecretReferenceOwnerAuthority` 只是正式接口，F1 实际 preimage/create witness、postimage、同 Store/活 Tx/实际持锁及当前授权仍由后继真实 provider 验证，构造器形状检查不是授权证明。

独立 OpenAPI 保存三个 Secret 路径的闭合 schema，原普通 API/DTO/CommandName、Purpose.Valid、D04、Project、Catalog/default root 与 SQL 均未改。新 Catalog 注册函数尚无生产调用。Owner Service/HTTP 和前向迁移没有实现，不存在成功 stub。

作者离线验证：新 7 top 定向 race `90452/c20c4b` actual0；旧普通合同定向 race `28070/cfbedb` actual0；包级 vet `bc2800` actual0。OpenAPI 全部172本地引用解析；Draft202012 本地 registry 38 正负控制 `3ae145` actual0，涵盖安全输出、65536边界、null/空值、三种Lookup identity与receipt分支；未解析网络引用。首次仅编译 `56776/324761` 因新event重复旧Change enum失败，已删除重复声明后通过；首 schema 控制 `fc1edf` 使用无六位小数的非canonical Instant失败，修正夹具后通过，不修改共享Instant规则。

以上是作者纯合同结果；最终独立结论见本节末，不代表§9真实权限、数据库、引用、D04加密存储/rotation、Unknown、HTTP或退出验收。

Variables 独审在 A 的7c2fb954输入实际复现两 must-fix：未知 JSON 成员名经共用 fields 错误路径泄入公开 Fault（28456/6bc29f actual1），Name schema 的 `$` 接受末尾换行（3738f6）。修复只给新 Secret decoder 加固定 schema path 投影，含 deleted DTO 和事件共八入口；旧普通 helper 不改。未知成员统一空路径/INVALID_FIELD，已声明字段的 REQUIRED 等保留；Name 使用真正 EOF 的 negative lookahead。作者修后最终8top race20400/7e3ef9（补事件前97569/3743f9）、最终vet f5a918、Name14边界f25bfd均actual0；事件夹具首编译7143bd误用i.Operation失败，改本包Operation后通过，旧失败保留；原独立审者复核见下段；这些纯合同检查不增加任何生产能力结论。

返修6111f6f0对应技术输入获未参与者Variables有限接受，无剩余must-fix。独立四组race35659/9cf442 actual0覆盖材料副本与寿命/presence/字节、metadata/receipt/Lookup、F1完整绑定与锁负例、opaque反序列化/事件隔离；复审仅重跑受影响项：六原红例24897/dbdbc0、deleted/event及nested receipt/known path/失败原对象不变79379/cb8b88均actual0，正式Schema83控ce6a22 actual0。独立probe首轮用reflect.DeepEqual比较opaque LockKey闭包的错误已改为CompareLockKeys，不算产品缺陷。

A纯合同与Schema现可作为独立完整子结果交付；全卡Owner后端仍待真实D04、F1引用提供方、同Store/Tx授权、迁移、Service/HTTP与root。不能把端口声明或构造器的锁/形状校验解释为已有真实授权与Secret使用能力。
