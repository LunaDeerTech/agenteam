# D10 Secret Variables Owner 后端

> 状态：工程 SPEC 草案，未独立审查、未实施。正式基线 main `3cea6076`。当前只写本文及分支 current，不占迁移号、不改产品。§10 的工程接缝仍须收敛，不能据此宣布实施依赖全部就绪。
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
| value写入 | 工程提案：1..65536 UTF-8 B，严格Unicode scalar、无NUL、不trim/规范化/解释；下限沿D04非空材料，空串不表示删除；限额随SPEC审查冻结 |
| version | 对外Variable version，正int64十进制字符串；真正metadata改变、每次显式覆盖、删除各+1。内部Credential version独立，不能以一个expected替代两域版本 |
| 时间 | DB微秒时刻；真实改变刷新updated_at，no-op不刷新 |
| 安全metadata | 恰 `id,project_id,type,name,description,version,created_at,updated_at`；type=secret表示受保护值存在，不返回值、长度、尾号、摘要或固定星号伪值 |

update 的 name/description/value 至少一个 presence 字段，缺席保留；null/重复/未知key拒绝。未传value的精确metadata no-op先校验expected，保存安全receipt，不增加version/generation/history/Audit/Event/Activity。显式value总是覆盖，沿D04每次覆盖新密文的规则；不解密当前值优化“相同明文”。覆盖保持VariableID/CredentialRef。

删除通过§5后，同Tx逻辑删除变量/清description与活映射，D04删除业务值；只保恢复/审计必要安全身份。旧名可供新ID，旧ID不复活。completed receipt可留安全历史metadata，不承诺备份擦除。Secret值及可离线枚举的值摘要不得进入普通command JSON。

旧 `/variables` 继续只处理type=variable；Get/Update/Delete误用Secret ID返回NOT_FOUND，List/容量SQL显式过滤type，避免把新行交给旧严格DTO。两类create/rename仍共用Project EX和名称索引。工程提案为保持原4096普通变量上限，另设4096活Secret上限，不暗改为共享总额度；分页generation区分普通与Secret视图，保留旧普通cursor语义。

## 3. Owner 命令、恢复和事务

新增独立Secret DTO，不扩旧Variable的value/type union。候选命令 `project.secret_variable.create|update|delete`。最小端口为 Create/Update/DeleteSecretVariable、Get/ListSecretVariables、LookupSecretVariableCommand；create输入业务ID/name/description/value，update用presence，delete无value。对外expected仅来自CommandMeta.ExpectedVersion，create必须无、update/delete必须有。

授权顺序复用普通变量：合法Human → 同Store活Tx/完整锁 → 当前Session/Owner Read、已初始化Project → 原command writer/历史receipt → 新写才Mutate → 目标/type/expected/名称/容量。archiving/archived可读及重放完成receipt，不能新写；pending/deleting无历史旁路。其他User含非Owner管理员不能获存在性/正文；AgentRun/Service不获管理能力。

拟采用单一final业务Tx：短只读发现当前metadata/映射/version，事务外准备D04 sealed候选及Outbox plan；final一次AcquireAll完整union，重验当前权限/版本/前像并原子提交。没有值明文或未确认nonce候选的持久计划，没有后台worker。准备失败仅消耗合法nonce；重新尝试重新准备。Secret commands仅持久completed行，故Lookup为committed/not_observed，不虚构持久in_progress，也不自动重发缺行命令。

可新增本域 `secret_commands`，保存command/key/actor/target、原metadata presence、安全receipt/history以及D04 intent receipt身份；不能另造业务目录。不得复用旧request.value、receipt.value或含明文的裸semantic_digest。safe receipt按create/update/delete闭合，含安全metadata/墓碑、changed、实际event/audit ID；no-op ID为null，不暴露CredentialRef/proof/key/digest/Session。后续更新、删除、同名新建不改历史receipt。

Unknown保原writer Attempt/Cause/CommitResult，不重跑callback。最多一次3s独立当前权限确认，由Service跟踪并可Stop/Drain；只有D10完整receipt与D04原意图事实闭合才成功。缺行/超时/撤权/坏记录都不证明原NotCommitted；HTTP不自动换key/version重发。明确NotCommitted沿原错误返回。

## 4. D04 专用组合能力

当前不是现成依赖：`WriteCommandLookupRequest.Validate`仅Model；Purpose没有ProjectVariable；现 `metadata.Purpose==consumer` 无法直接表示既定的同Secret供MCP与Agent环境使用。不得借Model/Runner purpose或复制两份值冒同一Secret。

拟在 `secret/contract/project_variable.go` 增加专有ProjectVariable purpose（`project_variable`）、opaque ProjectVariableIntent/PreparedProjectVariableWrite、ProjectVariableWrites及ProjectVariableWriteAuthority。root将D10真实同Store映射/前像证明器注入同一Secret实例，缺证明器或foreign/ended Tx、issuer/command/Project/Variable/version不符拒绝，不以任意Service actor绕过。

完整意图绑定格式、稳定User、Project、外层command、VariableID、expected presence/值、内部映射/版本、name/description presence/内容、value presence/原始字节；create候选随机Credential ID不改变原意图。Session/CSRF/RequestID排除。D04消费专用typed metadata加SecretMaterial，不能开放caller传任意裸hash的泛型加密API。

| 候选窄口 | 责任 |
| --- | --- |
| 构造ProjectVariableIntent | 纯typed校验；短寿命SecretMaterial，安全fmt/slog/JSON，显式Destroy；完整semantic digest只在受保护内存/信封中 |
| PrepareProjectVariableWrite | 在最外层Tx外准备nonce、可选sealed value和加密完整intent digest；返回同实例opaque候选、Ref、RequiredLocks，不保留明文/提交业务值 |
| MatchProjectVariableIntentInTx | caller完整锁及当前权限后，解密原receipt digest并常量时间比较；typed未观察/匹配/改义；不给D10返回裸digest；completed写重放必须用此口 |
| ApplyProjectVariableWriteInTx | 同Store/持锁/issuer/D10精确create witness或当前映射前像/write epoch；create、覆盖、delete改值，metadata-only/no-op只落intent proof；与外层同物理Tx |
| LookupProjectVariableWriteInTx | 同caller Tx只观察安全身份/结果/绑定，不需原值；D10核自己完整历史receipt。旧Model Lookup保持闭集 |

上述方法的精确Go字段/opaque工厂及D04 proof生命周期仍为§10工程收敛项，尚不能当已冻结实施API。专用intent/value沿原D04 AAD、receipt owner、rotation/canary/epoch和退休流程；不另设D10 keyring，不把加密intent降为可枚举hash。metadata-only/no-op也要完整intent proof，防止同key把“只改名”换成覆盖而命中旧成功。

低层legacy Human/Model写必须按**当前存储归属**拒绝绕过组合口修改/删除该Credential，包括purpose转换，不能只看请求purpose。组合口必须分别证明D10变量写与D04值写，generic合法DTO不能充授权。

存储归属与后继消费用途分开：本卡只开放Owner组合写，**不自动开放MCP/Runner lease**。将来对应UsagePlanner/Authority证明真实binding/执行与当前gate后，D04才按窄规则允许ProjectVariable归属用于该consumer；不全局放宽purpose比较。F1只维护白名单，无需材料解析。

D04真create/覆盖/delete保一次secret.create/update/delete Audit；metadata-only/no-op不伪造值变更。D10为真实变量语义变化另记一次project.secret_variable.create/update/delete，以VariableID为资源，二者事实来源不同。Owner metadata/Lookup无secret.resolve。

## 5. 目录、同Store引用与删除

资源侧目录仅返回同Project稳定VariableID/安全metadata和typed valid/removed/not_in_scope，一一对应请求ID，无缺项/重复；本域没有disabled状态，不补造启停。跨Project/不存在安全不可区分；普通变量不能入白名单；读取错误不能变空集合。目录不是bearer grant，不建runtime lease。

候选资源API：DiscoverSecretVariables→opaque SecretDirectoryPlan，RequireSecretVariablesInTx→SecretDirectoryFacts；DiscoverSecretReferences→opaque SecretReferencePlan，ApplySecretReferencesInTx只在caller Tx维护。plan绑定issuer、Actor/User、Project、command、完整排序ID、Before/After、owner identity/version、映射和锁，不可反序列化构造。Discover短只读；InTx先同Store/活Tx/持锁再当前权限/计划/canonical检查，不自开Tx、补锁、解密、网络或Activity。

本域引用表持Project/VariableID/owner_kind/owner_id/owner_version。本卡仅定义agent白名单类型、typed AgentID和F1最多256项集合；不准任意字符串owner。Agent F1提供SecretReferenceOwnerAuthority：事务外发现Agent gate及预期postimage；final证明同Tx真实Agent canonical、创建初始化witness或当前版本及完整postimage。创建前不存在必须由精确create plan处理，不能要求事务外已有Agent，也不能单凭caller承诺放行。

本卡交付资源侧注册/plan/验证/表维护及未绑定拒绝；生产尚无Agent owner时引用变更明确DEPENDENCY_UNBOUND，不能造空Agent provider。只有F1真实adapter交付后才声明白名单新增/移除闭环。Owner删除查询本域真实引用表；存在引用即使adapter缺失也拒绝，未知owner行安全失败。对自有权威引用表的完整查询不是“外域空checker”；真实消费者只能经已注册且实际验证canonical的写口落引用。

资源删除/覆盖/改名持Project EX；引用变化和目录final至少Project SH，并入Agent EX及所有D04/reference锁，与Agent canonical/version/receipt/events同Tx。Before/After及owner_version同步，无query-then-save窗口。完整union按Foundation锁序预取，不用跨域FK/CASCADE代替权限。

Owner Delete确定为：当前权限/version正确，且本域Agent引用为空、D04 live references/active leases均为空，才同Tx删除；否则RESOURCE_BUSY。先由配置owner正式移除引用，Owner再重试。禁止自动删白名单、解绑MCP、终止进程或释放lease；沿D04现有受引用拒绝契约。架构的删除后失效白名单清理/unavailable留其他合法生命周期，不在本卡新增级联行为。缺必需证据/坏计划/未知消费者不能当无引用。

## 6. 原子事实、Audit和完整锁

final union至少外层及必要D04 command EX、User EX、secret-write-key SH、Project EX、Credential EX、全部Outbox/reference record；一次Normalize/AcquireAll，遵正式rank，不在高序锁后补registry。不能直接在高序caller Tx套旧References便利AcquireAll。发现变化时整体拒绝或有界重新准备，不自动rebase expected。

原子提交变量canonical/映射/generation、D04值和受保护intent receipt、D10 completed receipt/history、两域适用Audit、typed Outbox及Activity；任何一步失败全部业务事实回滚。合法nonce预留消耗是既定例外。当前Session/Owner/Project、两域版本/epoch和前像在final再核。

候选event：producer=projectvariable，event=project.secret_variable_changed，aggregate=project.variable，schema1；payload仅variable_id/operation_id/change/changed_fields。Audit三action使用resource=project_variable，仅VariableID/version/字段名，因果绑定原command。name/description/value/值hash/CredentialRef不进这些metadata。ProjectVariable Authority与D04 ProjectAuditAuthority分别证明真实同Tx事实。

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

工程cap拟input/detail/receipt/Lookup≤1MiB，list≤5MiB；实测65536 B value最坏escaping。strict拒unknown/duplicate/大小写别名/null/无效UTF-8/孤立surrogate/尾随值，错误末项不得发半页。总read/Lookup 2s、mutation30s、受跟踪确认最多3s沿现预算。

分页沿C name/id keyset、默认50/最大100、cursor≤8192 B；kind=project.secret_variables/type=secret，绑定User/Project/Secret generation。换limit/同User新Session可续，真实Secret变化stale，no-op/replay不变；两类cursor不可互用。Owner目录可见不授权未白名单Agent知道Secret存在。

写raw body/decoded value尽早移入SecretMaterial并清可控byte副本，不长期保留不可擦除string DTO；诚实保留Go临时副本/GC擦除限制。日志只模板/request_id/固定code/state/计数，禁body/enclosing DTO/错误链泄露。DB普通列、Audit/Event、diagnostic、所有GET/HEAD/Lookup/receipt用canary验无明文或值摘要；无plaintext read endpoint。

root复用同Store/Account/Project Authority/Secret Service及keyring、Variable facts、Audit/Catalog/Outbox。先构造无材料事实Authority，再注入Secret组合写授权，再构造Service/HTTP；无late-fill或复制ProjectAuthority绕证明。未绑定Agent/MCP/Runner保持拒绝。构造器拒typednil/已知异Store。

Stop关闭admission并取消读写/Lookup/确认；Drain等实际函数/Rows/Tx/I/O回调结束，不关共享Secret/Store。根注册同一call owner，失败构造和晚到install实际退役；沿既有私有deadline/Read/Close/Write/Flush/join与Force总预算，不从取消或时间经过推断joined。

## 8. 文件与迁移唯一owner

当前仅本文/current可写。以下是实施提案，不授予写权；root明确整合基线/唯一owner后才扩大闭包。

| 拟文件/接缝 | 责任 |
| --- | --- |
| projectvariable/contract/secret_types.go、secret_commands.go、secret_query.go、secret_directory.go及测试；本域secret_service/commands/repository/reader/directory/references/http等源与测试 | Secret Variables作者；仍在原业务域 |
| 现projectvariable service/repository/reader/commands/authority/events的必要兼容 | 同作者获得共享写权，普通变量作者停对应路径；旧行为回归 |
| secret/contract/project_variable.go、secret/project_variable_write.go、project_variable_lookup.go、原Purpose/写入/receipt/rotation/ProjectAudit兼容 | root明确D04接缝owner，同一实施闭包可交同作者，必须独立安全审查；不可留无人负责前置 |
| 一条或必要有序前向migration，编号TBD | root按当前全局顺序分配唯一owner；统筹变量shape/intent/Purpose/Audit CHECK，不占Knowledge/Runner预留、不改旧migration |
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
| HTTP/root/退出 | 默认根Owner完整路径/实际schema/普通API回归；Body/Write/Close/Flush/确认取消与实际join；进程Wait、自有资源/runtime/desc/TCP完整尾 |

未参与实现者至少独验低熵值和metadata-only/覆盖改义与当前撤权、共享名称/删除引用竞争及final Unknown、新Purpose/legacy绕过/receipt轮换。纯probe不代替真实权限/事务验收。

## 10. 草案收敛与结果边界

尚待工程冻结：§4 opaque Go API精确字段/工厂/证明顺序及D04 receipt/rotation schema；§5 Agent authority签名及F1绑定责任；§2/§7工程限额；迁移号和共享路径唯一owner。这些是SPEC/跨域工程决定，不应以旧端口已存在跳过；当前不能把“只改D10”标为闭合实施。

已有产品规则足以限定Owner后端：无明文读、固定type、覆盖保持ID、有引用拒绝删除。本草案不新增级联清理、自动解绑、空Secret、进程热更新或Secret使用scope。若后续目标确实要求这些额外行为，再回到正式产品规则；不为用户未请求功能补问。当前未发现阻止Owner SPEC编写的新产品决定。

SPEC接受仅准冻结范围开工；Owner后端须产品/HTTP/root与独验闭合；F1 Secret前置须真实Agent adapter同Tx创建/引用实证后才完整闭合。任何一层都不等于完整D10、MCP/Runner材料使用或Project生命周期完成。
