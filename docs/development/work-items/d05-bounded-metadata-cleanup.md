# D05 有界收敛与单对象元数据清理

状态：rev1设计、Service/SQL主体及三个配套增量已获Variables独立有限接受（f4c64f、46327/e70e7a），Skills已核消费契约兼容。首批metadata／最终Unknown两top于46857真实PG/MinIO完整PASS：65真实reader历史、跨表32与同Tx重复回滚、最后4＋fixture父mapping原子性、两种真实COMMIT Unknown恢复/native Audit唯一；原Wait/7资源/TCP/input完整尾通过，详见当前检查点。专用IndexMigration一top三子于2913完整PASS（fresh／populated27／末DDL失败整体回滚后原字节重试），原Wait/7资源/TCP/input尾齐。历史规模／索引成本／PUT跨source／真实Skills最后5核心仍未闭合。正式基线 main `b2a7d0ab`；本结果不恢复 Object Runtime join 停项，不代表完整 D05/Skills participant或生产root接入。

## 1. 来源与范围

消费权威是 `ai/skills-initialization` 的 `docs/development/work-items/d10-skills-initialization-design.md` §16.6/16.6.2，root 已保存 `1e8b5c67`；本基线若尚无该文档，直接读权威分支，不复制一个不同版本。D05 以[原设计](d05-object-storage-design.md)、[Runtime 说明](../backend/object-runtime.md)、真实 `cleanup.go/reference_cleanup.go/access.go/project_work.go/project_stop_store.go` 和迁移00005/7/14/16为准。

第一 provider 只支持 initialized Project 的已发表 SkillRevision＋ProjectDeleted、原 Creation/Skill/Revision/Object/Upload 和同命令的旧 candidate。不加 Creation取消、其它Owner新purge权限、Runner退休、HTTP/生产root或自动删除永久marker。

初始四路径及后继§8实现域均已获root授权。2026-10-09 root在Skills确认原占位无独立DDL后，将`00028_cleanup_indexes.sql`移交本任务为共享cleanup索引迁移唯一writer；不改FK/约束/列。root已将稳定00025/26/27精确装配到本树（分别aaa408c8/eea4ced0/7cf7a58e）；28为待成本计划核验的22索引候选；专用迁移矩阵已实际验证fresh、旧库27升级、末DDL失败整体回滚及原字节重试，核journal/Goose及旧schema/数据不变，不据此接受查询或FK trigger成本。本次及首批独占窗口已完整释放，后继真实资源仍需root另授。

## 2. 实际缺口与必须成立的结果

- `gateObject/stopWriters/cleanObject` 的 `attemptIDs` 全历史扫描。原活候选2个配额排除已cleaned历史，不能约束历史规模；stopWriters还逐项load，不只改claim查询就能解决。
- `finalizeCleanup` 未完成count与最早cleanup cause查询缺exact Object访问路径；native Audit又重复同类检查。
- `CleanupProject` 先取Owner Maintenance再看Deleted。先删Skills映射会永远失去授权；原最后多表DELETE也无界。
- 已有 `project_stops` 会令 `maintenanceAdmission` 拒绝新增cleanup工作，故必须给正式Cleaning的exact cause一个窄许可，保留旧stop gate。
- `projectStopFacts` 对每个Object全历史投影LIMIT1001，超过即overflow；不能先等Stop完成再靠metadata删历史解开它，这是实际依赖循环。
- 本main的CleanupRelease闭集尚缺SkillRevision+ProjectDeleted；Skills分支已有接受增量，由root定向整合，不再发明另一个版本。
- `ReleaseForCleanupInTx` 已revoked重放要求全部历史cleanup同一cause，与合法旧AbandonedAttempt冲突；`DeleteUnreferenced` 尾部的Maintenance/InspectAccess又不在Skills三个物理维护许可内。`inspect()`全量加载引用/活lease，也不能留在有界路径里冒充常数工作。

正式zero_marker首版永久保留：完成指payload消除＋空marker核实＋独立writer/lease/work退休，**不是删除marker key**。Skills“marker删除”措辞已经发给作者校正，不改变D05物理规则。

## 3. 独立typed口

`DeletedObjectMetadataPurger.PurgeDeletedObjectMetadataInTx(ctx,tx,cause,object,plan,locked)` 返回 `ObjectMetadataPurgeResult{State,OperationID,ObjectID}`。State仅Pending/Completed，Validate/MatchesOperation只检查数据形状和IDs。结果可以由任何人重建，不能作为physical/join/commit proof。可选接口未绑定则DependencyUnbound，不扩原Objects/ReferenceCleanup迫使假实现。

唯一新operation `PurgeDeletedObjectMetadataAccess` 仅属于ObjectCleanupAccess。纯合同阶段只声明常量，原AccessRequest暂不接受它；构造、Validate/fingerprint、Service行为必须在后续实现阶段一起闭合。旧CleanupObjectAccess/普通Owner plan不通用。

请求绑定完整cause（Owner/Project/reason/operation）与exact Object；D05 discovery绑定真实Upload/current attempt/原command与当前批依赖。一次完整union包含Project EX、真实Skill EX、Object EX、原command/Owner reference以及本批原work/关联记录锁。真实issuer/live Tx/同Store/actual Acquire及当前Skills CleanupAuthority均重新校验；公开字段一致不构成授权，foreign issuer/Store/Tx、漏锁/弱锁、ended Tx、映射漂移均拒。

该InTx口不取消、不做外部I/O/ProcessGuard调用、不开goroutine、不加锁或嵌套事务。调用方必须回滚任何error；Pending只有外层Committed才算进度。缺anchor不是成功重放；最终Unknown由Skills在当前Project EX和正式gate下确认唯一共同最后事务的六表全空。

## 4. 原子gate与32项物理推进

沿现有同Tx原子关闭：exact Upload revoked、Object cleaning、删除exact reserved/canonical引用，与Skills唯一gated记录一起提交。保留原published current attempt的ProjectDeleted cleanup行至最后；旧AbandonedAttempt继续保原operation/reason，不为重放改写原因。不引入公开parentgate布尔。

初始化已发表范围的Release重放以真实Upload/current attempt的ProjectDeleted cleanup作为唯一canonical anchor，核exact cause、Object/Upload、当前Skills门禁及cleaning/Deleted；旧attempt属于原Upload/Object，其既有AbandonedAttempt不改写也不要求等于新cause。其它Owner的原分支不因本例外得到新权限。首次原子gate Unknown后必须重取完整锁并读原事实；current原因缺失/不符或部分anchor矛盾拒绝，不能用任意一条旧cleanup存在代替。

本scope初始化已发表current唯一、原私有未终局candidate受配额限制。gate只发现原Upload尚未cleaned的候选，至多33条，最多32条实际处理；已真实cleaned历史不重复gate。合法集合若出现超配额/错kind/混杂事实，不能部分成功并称全gate，须Pending/安全错误。新Reserve/Send/Publish/Consume仍在原锁下受revoked/cleaning禁止，不把分批candidate标记变成可以复活的中间态。

**phase=cleaned不单独证明writer结束**。另以未io_closed、active lease、未joined work/未完成cleanup的索引谓词发现旧marker背后的活writer；本轮stopWriters的load/cancel/actual wait/join、claim、I/O和checkpoint复用同一至多32个原候选集合，不每阶段再挑32个不同ID，不先走全历史前缀。未选项仍由最终完整EXISTS阻止Completed。

实际writer只能凭原本地done/returned及原Tx尾、原持久终局，或正式ProcessGuard对exact ProcessID的死亡证明推进。ctx.Err/取消已发/map为空/TTL/marker成功不是join。原claim worker/fence/operation保持，不能用新fence覆盖仍活旧worker。每次物理操作实际返回后才能checkpoint；Unknown保留原cause/attempt、不发推测性下一次I/O。

所有Tx/I/O/实际Wait受调用者原剩余预算及force cap，沿现WithinBudget，不扩D08的2s或补一轮新2s。最终用固定数量可索引EXISTS确认无ref/active lease/未cleaned候选/未完成cleanup；不用全量count或LIMIT空诊断推终局。尚活旧cleanup worker阻断claim及终局，但本次调用自己的末尾SQL不能等待自己先return：其私有call关联只允许已经实际I/O返回、原checkpoint明确提交的本次worker进入finalize；其它worker须实际退休。**metadata仍要求包括本次worker在内全部实际尾已返回/退休**，不消费这项finalize内部例外。原native ObjectDelete Audit＋Deleted事务只一次，沿最早原cleanup cause摘要和私有witness，metadata不重写Audit。

Skills这个WithinBudget分支的保护判断用完整EXISTS，不全量调用inspect；需要返回Remaining时，按原cause的ObjectCleanupAccess/CleanupObjectAccess在当前门禁下读取，不能新开Maintenance/Inspect许可。诊断最多发现33条、返回合计不超过32条**完整**引用/活lease；超过32则Pending＋ResourceBusy，不把截断数组伪装成完整Remaining。Completed必须在同授权事务确认全部为空，列表为空不独立授终局。普通InspectReferences端口语义不改，当前其它Owner路径如未迁移到此算法，不能借本卡声称也已获得全链有界性。

## 5. stop后精确cleanup准入与Stop历史循环

ClaimCleanupAccess的窄许可在**原同Tx**读取保留的current cleanup ProjectDeleted cause，核SkillRevision/原Object/Upload，并实调本Store CleanupAuthority；Skills重验D08当前Cleaning、Owner/cause/version、Stop齐备与gated/pending。成功才注册原worker/fence的cleanup work；普通preparation/verification/其它maintenance仍走原deny。Checkpoint保持原admitted work/witness；Finalize当前授权保持。Skills转completed后不再发新物理claim，只允许独立metadata operation。

物理调用返回后的operation-work实际join仍必须闭合；失败/Unknown保留未joined记录，由原技术join恢复，不要求重新获得已经关闭的业务物理许可。metadata不能先删这些记录再声称退休，也不在持Tx时等外部过程。

进入Cleaning前，Stop投影必须去掉“一个Object扩展全部终局历史”这一循环。保留现5个lane与单一持久after，选中项仍按ID升序；新一轮最多32个主ID，按本轮原主ID而非展开后的依赖ID推进after。停止条件没有放宽，只减小单轮工作量；原2s/取消100ms上限均不增加。

| lane | 当前候选与本轮动作 | 必须发现的固定关联 |
| --- | --- | --- |
| 0 work | relevant kind且joined_at IS NULL，逐项gate、取消已捕获原handle、真实retire后checkpoint | 有Object的preparation及verification的原attempt；reader/source的原lease；cleanup的原cleanup ID＋worker/fence；transfer_get/put的原transfer；download由work.object是否存在区分grant issue与stream attempt，并取得原grant。每项仍保private handle捕获锁与正式work mutex。 |
| 1 objects | 本Project有reserved Upload的Object，关闭原reserved gate/ref | 唯一Upload、Object、原command；不展开该Object全部attempt/work/lease。已attached canonical不在Stop中物理清理。 |
| 2 leases | 本Project active且当前action相关的lease | writer精确attempt/process；reader/source精确process与能直接匹配的原work；transfer精确lease_owner的transfer。只凭原returned/death＋原Tx锁checkpoint选中native行。 |
| 3 grants | Delete时本Project尚未revoked的grant，锁原grant并revoke | 固定grant绑定/Object/command。不展开全部download attempts；实际流由lane0原work控制，started但无真实终态的原最终谓词继续阻断。 |
| 4 transfers | 当前action相关且尚未revoked、retirement缺失或external lease未终局的原transfer | 单个transferAccessFacts的真实锁和固定staging/candidate/external/source lease；revoke/Audit仅本批精确ID。retirement必须沿正式提供方，Stop不制造证据。 |

终局历史不参与全历史row_to_json fanout。发现的是本批将读改的exact原行及上述固定关联，Validate在完整一次Acquire后重建同一投影；相关映射变化整Tx回滚后重发现。每项关联经主键/唯一键读取，禁止用object_id展开全部work/attempt。lane0未选项不被lane1间接取消或改joined；选中writer的原事务锁不因没有全量投影而省略。lease与关联work只有经过其真实returned/death、相同身份和原锁才能checkpoint，不能凭另一行已released批量确认同Object的其它work。

原主lane取满32则after=第32主ID，否则进入下一lane并清after；每个已提交checkpoint才推进，Unknown按原持久cursor恢复。首个Project stop gate关闭旧准入，取消只能在已知gate提交后发生。未终局项可在lane循环后重新看到，不靠UUID时间单调或内存cursor；新低ID也不会永久被跳过。不存在新的scan_kind或游标字段迁移。

保留真实的“已准入但尚未产生native行”状态：PreparePayload最初的preparation work使用自身resource且object为空；IssueDownload的原work同样可以先提交、随后Resolve失败而没有grant。这些行只凭原handle实际结束或exact ProcessGuard死亡、原work mutex及原事务尾退休，不要求伪造attempt/grant，也不把缺native本身当退休。native一旦存在则其固定身份/原锁必须纳入；有Object的candidate preparation与其attempt、lease是同Tx注册，缺原行不能按上述预准入分支豁免。

native没有work的旧writer仍由active writer lease lane发现，必须核原attempt.io_closed；不能因phase=cleaned就过滤。缺失本该存在的native/work关系属矛盾或未证明，保持Pending，不制造ProcessID或删除阻塞事实。最后projectStopPending仍在当前Project EX下检查完整当前未退休集合，与cursor/本批诊断独立，特别保留未io_closed native、applying cleanup无正式joined worker、未终局download/transfer。该改动不修复其它原退休证据缺失或Runtime行为；源码若发现合法原记录无法通过上述lane到达，应作为实际缺口保留而非放宽最终谓词。

## 6. Metadata与最后anchors

Skills只有在D05物理调用实际返回、持久确认本域completed并完成自己的历史压缩后进入此口。D05同union Tx重验当前CleanupAuthority与完整Object/Upload/current attempt/current cleanup、Scope/Owner/initiator/Creation。Deleted必须是D05真实finalize/native Audit的持久结果，不消费public result字段；还必须核所有原work/reader/writer/cleanup/transfer实际退休及当前无新工作。没有真实证明则Pending/原错。

只有这套终局前提齐全才开始单调删历史。每批重新检查当前gate及剩余private facts；Deleted/revoked gate保证旧请求不能再生成工作，删除过的只是此前真实确认的终局历史。metadata不会自己append/delete ObjectDelete Audit或新增Event/Activity。

每Tx跨表合计最多32条DELETE，最后anchors也计数；不每表各32、不级联隐藏无界工作。按FK依赖选择有限exact ID：终局transfer关联包、released lease、joined work、旧completed cleanup/cleaned attempt。当前attempt及其cleanup、Upload、Object一直保留。来自其它Object的source引用不在本口越权清除，仍有引用则Pending由真实Owner退休。

本首版每个live Tx只允许调用该provider一次purge；同一个完整union不能连续调用它绕过32上限。沿现私有accessTransactions记录本Tx已消费的metadata调用，在首次写入前占用，第二次拒绝并要求回滚；仅原Tx实际结束后回收。它是本实例的资源限额，不是权限/死亡/物理proof，不增加持久墓碑、后台协程或新预算。Skills固定消费同Store同Object实例，不在一个最后事务拼接多个provider。

00007互引包按真实拓扑处理，不清transfer_id伪造kind：GET transfer删除一行；PUT transfer的最小互引包是其原completed cleanup（如已被前批合法删除则0）、cleaned runner_staging attempt、transfer本行，至多3行。先删cleanup，再删staging，再删transfer，利用已有两个DEFERRABLE FK直到同Tx最后一致；candidate是独立private candidate，不随包提前删除。Upload/current anchor不能是待删staging，当前已发表Skills不允许该矛盾。真实源码只有private candidate writer lease填写attempt_id，staging不生成这种writer lease；若发现其它对staging的依赖，按真实关系阻断/保留，不用级联逃过。

每个包先核retirement_evidence完整、external/source lease真实released以及全对象work退休，计划包含原transfer命令及其固定关联锁。`transfer_complete.go`创建的source lease归同一r.object且无attempt_id，原transfer引用的lease均先保留；删transfer解除它们的入边后再按32上限删released leases。因此不需要把无界lease历史装入互引包。另一Object仍引用本Object lease/candidate时，本方法不删其transfer，返回Pending/关系矛盾，保留原数据交实际owner收敛。

扫描原Object transfer索引最多33主行，依次纳入完整包直到总DELETE数将超过32；不纳入第一个超额包，本次已选包提交为Pending，下一轮从真实剩余头部继续。由于合法单包最多3，不会出现“一个包永远装不下”；失败/Unknown不推进内存offset。transfer全空后依次删除released leases、joined work；再按cleanup＋非current attempt两行一包删除，单批最多16包。最后只剩current四行才走最终事务。所有跨表行数按实际受影响行计数并核exact ID，不靠估计省略FK关联。

历史确空时，最后最多4行current cleanup/current attempt/Upload/Object同Tx清除（先置current_attempt_id=NULL解除FK），Skills立即同Tx删其核心cleanup/revision/skills/current attempt/initialization。任一步失败一起回滚；外层Known Committed才报告Completed。最终Unknown保留原Tx cause/attempt，Skills重取原Project EX/current gate核六表全空及原本地实际尾；部分核心缺失属矛盾，完整核心则续metadata，不再次对已失父映射Object调用维护。

## 7. 索引与DDL边界

可复用：uploads.object_id unique、attempts(upload_id,ordinal) unique、cleanup.attempt_id unique、references Object前缀PK、leases(object_id,owner_kind,owner_id) unique和active Object partial索引。

实际缺口：本Upload未cleaned/本Object未io_closed、cleanup Object phase及最早created_at/id、本Object work未joined和历史分页、transfer Object与跨source_lease依赖。全局attempts_recovery/cleanup_recovery、work(project_id,id)、transfer(project_id,recovery_pass,id)不能宣称覆盖。候选索引须跟实现SQL一起收敛：原Upload非cleaned范围、Object未io_closed、cleanup/work Object pending与历史、transfer Object/source lease。Stop选择应使用Project主范围及各未退休谓词，真实EXPLAIN还要覆盖空集合/只有其它Project大量数据，不能仅验LIMIT有返回时的计划。

还必须核DELETE的真实外键检查成本：目前references.upload_id、leases.attempt_id、transfers.upload_id/candidate_id/source_lease_id和attempts.object_id等子表查找没有对应首列索引。父行按ID删除也可能在FK trigger扫描全库；实现前将这些实际入边与已有unique/Object前缀索引对照，补必要最小子索引并实际EXPLAIN/BUFFERS触发成本。不能只验证SELECT再宣称整Tx有界，也不机械全建重复索引。

root已明确移交**00028**给本任务，候选名`00028_cleanup_indexes.sql`，Skills不再独占该号。稳定00025/26/27已逐字装配，正式路径28已连续落盘，不以跳号29或空迁移绕连续前缀。尚无PG/EXPLAIN，以上是源码查询缺口证据，不是性能PASS。

### 7.1 实现访问路径与待编号索引

下表是针对实际SQL的候选 `CREATE INDEX … ON …` 主体，已写入`00028_cleanup_indexes.sql`并通过专用迁移三子；索引必要性、查询及FK trigger成本仍待真实计划验收。唯一编号现为root移交的00028，索引名仍随实际计划收敛；不修改历史约束、不增加级联/公开proof或填充业务假数据。同一个完整索引兼顾历史分页和FK入边，不因有另一partial索引就重复建同形完整索引。

| 表（`agenteam_object`，另注除外） | 索引列及谓词 | 实际用途 |
| --- | --- | --- |
| upload_attempts | `(object_id,id)` | 每批16个历史attempt及Object删除入边 |
| upload_attempts | `(object_id,id) WHERE phase<>'cleaned' OR NOT cleanup_gate OR (kind='private_candidate' AND NOT io_closed)` | 当前32物理候选、完整终局；cleaned不等于writer关闭 |
| upload_attempts | `(transfer_id) WHERE transfer_id IS NOT NULL` | 删除transfer时的staging反向FK检查 |
| uploads | `(current_attempt_id) WHERE current_attempt_id IS NOT NULL` | 删除非current attempt时的反向FK检查；current最后先置NULL |
| uploads | `(project_id,object_id) WHERE disposition='reserved'` | Stop lane1直接取本Project reserved Object ID和完整pending |
| cleanup_operations | `(object_id,created_at,id)` | 原最早cause不变的native Audit、Object删除入边 |
| cleanup_operations | `(object_id,attempt_id) WHERE phase<>'completed'` | gate的第二个有限候选集合与完整pending |
| object_references | `(upload_id) WHERE upload_id IS NOT NULL` | 删除Upload的反向FK检查；Object方向已有PK |
| object_leases | `(object_id,id)` | 每批32个released历史；Object方向虽已有unique但不能按id有序取小批 |
| object_leases | `(attempt_id) WHERE attempt_id IS NOT NULL` | 删除attempt的反向FK检查 |
| project_work | `(object_id,id)` | 每批32个已joined历史 |
| project_work | `(object_id,id) WHERE joined_at IS NULL` | 本Object真实未退休检测与旧cleanup claim否认 |
| project_work | `(project_id,id) WHERE joined_at IS NULL` | Stop lane0跳过已joined历史 |
| object_transfers | `(object_id,id)` | 每批32个历史transfer及Object删除入边 |
| object_transfers | `(object_id,id) WHERE revoked_at IS NULL OR retirement_evidence IS NULL` | metadata/物理完整pending |
| object_transfers | `(project_id,id) WHERE revoked_at IS NULL OR retirement_evidence IS NULL` | Stop lane4原native未终局分支 |
| object_transfers | `(upload_id) WHERE upload_id IS NOT NULL`、`(candidate_id) WHERE candidate_id IS NOT NULL`、`(source_lease_id) WHERE source_lease_id IS NOT NULL` | 三条原FK反向检查；staging_id/lease_id已有unique，不重复建 |
| objects | `(project_id,id)` | Stop本Project native join；原state/cleanup_pass前缀不能提供此id顺序 |
| agenteam_download.grants | `(project_id,id) WHERE NOT revoked` | Stop lane3跳过已revoked历史 |
| agenteam_skill.work | `(project_id,id)`（完整索引候选） | §16.5 joined历史按Project/id取33，以及initializations删除时`(project_id,skill_id)`入边反查的Project前缀；现两个partial索引均排除joined |

Skills候选基于消费SPEC而非已实现查询：`WHERE project_id=$1 AND phase='joined' ORDER BY id LIMIT 33`，原FK为`(project_id,skill_id)→initializations(project_id,skill_id)`。先以一个完整`(project_id,id)`候选同时承担两访问路径，保留现live/recovery partial。若真实计划表明同Project大量live work使joined查询过滤成本不能满足预算，再评估增加joined partial；不机械同时建两条重复用途索引，也不宣称完整Project前缀已经性能通过。最后删除initialization前本Project work应全空，FK probe仍须实际证明不会扫描其他Project历史。

gate从两个各最多31个pending集合合并后再取31，current anchor另占1；不能使用“所有attempt逐行相关查cleanup”的旧查询。Stop lane1用reserved Upload直接发现原Object主ID；lane4按原native pending与真实active external lease两支各最多32再合并原transfer主ID。后者active支从本Project Object/active lease开始，沿已有transfer.lease_id unique取原transfer，避免扫描已终局transfer历史；没有把lease缺失当inactive。其余lane/原after/完整pending不放松。Project native join仍须在真实计划中证明使用Project范围与现active/Object索引，SQL的LIMIT本身不证明索引/预算合格。

真实验收除业务矩阵，还必须在同fixture执行 `EXPLAIN (ANALYZE, BUFFERS)`：本Object空集、65及1001+已cleaned/joined/retired历史带末尾活项、只有其它Project大量数据、本Project大量终局Object而无active lease；分别核gate两支、5lane、metadata四历史阶段、完整pending及最早cause。父DELETE须实际在回滚事务执行并观测FK trigger时间，包含Upload、attempt、transfer、lease、Object的全部原入边（含已由unique/PK覆盖的其它领域入边）；不能只EXPLAIN SELECT。首次合法PUT三行包与最后四anchors的原DEFERRABLE约束须真实提交验证。原2s剩余budget内不能完成时保留Pending/原错误，不临时扩时或把查询超时作完成。若真实计划指出某索引冗余或仍有全局扫，再按实际计划调整，不能以本候选表宣称优化已验收。

### 7.2 有限成本矩阵与首业务窗口的分离

首业务候选只运行完整metadata/最后Unknown两top，不等全部成本场景。其通过也不关闭本节。已编的history观察器记录**实际Service发出的SQL和首次参数**，原方法实际返回后才独立EXPLAIN；保留首轮nil游标，不能用最后空页替换。其现有停止后观察只能证明仍保留1001终局历史时的空pending范围，不能冒称取消前的活reader计划；物理返回后的gate计划同样不能冒称未清理候选的计划。新增状态须在变化前留实测，且不得在原业务2s内部插入额外观察查询改变时序。

| 有限组 | 计划执行时必须仍在场的代表性事实 | 查询与边界 |
| --- | --- | --- |
| 本Object历史 | 原65旧attempt、1001已join work/已release lease仍在；另独立保留末尾pending候选或native活项 | 两pending候选集合、完整physical/metadata pending；真实退休行为另测，SQL状态不替代实际I/O |
| Project范围 | 本Project1025终局Object且无活lease；另一Project1025 Object及10001条历史；缺失Project/Object的范围查询时其它数据不删除 | 五lane＋完整Stop pending，Archive/Delete各自相关分支；first/after页分别保参数 |
| metadata各阶段 | 每一类真实待删历史尚未删到空；PUT包含原staging/cleanup/transfer完整互引包 | transfer→lease→work→旧attempt阶段的首批；每批实际32总额及FK顺序仍由业务测试判定 |
| FK入边 | 外Project/外Object子表历史保留；被删目标本身合法无引用 | 在独立回滚事务实际DELETE并SET CONSTRAINTS检查deferred边；逐父表记录trigger耗时和buffers对应查询，不能只看父PK扫描 |
| Skills共享索引 | 本Project1001 joined＋少量live，以及只有其它Project历史；最后initialization前本Project work全空 | 正式SPEC joined扫描与完整FK反查分别测，Project前缀是否足够由计划决定，不先增加重复partial |

`.agent-state/object-metadata-cleanup/scale-fixture.sql` 是隔离的新成本数据库草稿，当前仅准备Object/Upload/attempt/cleanup/reader/work的代表性基数，**未执行／未SQL验证**。为合法CHECK/FK形状而插入的终局和标记Audit行只能用于SQL成本，禁止传入Service充作物理完成证明；实际业务组仍由真正调用产生native/Audit/退休事实。计划查询继续取实际Service观察器，草稿不另抄一套实现SELECT。transfer、download、Skills及所有FK完整入边仍需各自合法fixture，空表不抵充其成本接受。此草稿和本节不改变已冻结首业务二进制的输入来源。


后继首个可执行成本来源为 `tests/objects/metadata_cleanup_cost_test.go`＋内嵌 `testdata/metadata_cleanup_project_cost.sql`（尚未编译/PG）。它用两个真实Service的未关闭reader保持原Stop五lane均可到达，捕获原SQL；随后只在另一新数据库执行成本seed与原SQL参数重绑。先测1025+1025终局Object和1001+10001退休reader仍在场的目标/缺失范围，再在独立available Object上增33活reader形状，分别测Archive/Delete与first/after-32的32+1精确结果。活状态是合法SQL成本刺激，**不是实际native活锁/退休验收**；真实查询/Rows关闭与EXPLAIN逐次沿2s绝对时限，完整节点/过滤/loops/buffers保留供判定，不强制planner或仅凭LIMIT/测试返回接受扫描成本。两现候选不含新top，已验history不受影响。

该单例覆盖及剩余22候选所需合法数据如下；不是增加新的业务门槛，也不将空表计划算成全量覆盖：

| 候选用途组 | 此片段及后继合法数据要求 |
| --- | --- |
| `objects_project_id`、`object_work_project_pending`、`uploads_project_reserved`；Stop完整pending涉及的attempt/cleanup范围 | 当前片段有Project历史/缺失范围、reader活尾和保留历史；reserved Upload、非closed attempt、applying cleanup仍需合法原Upload/current/worker-fence数据，不能用本轮空集合替代其有项计划。 |
| `attempts_object_history/pending`、`cleanup_object_cause/pending`、`leases_object_history`、`object_work_history/pending` | seed有65旧attempt与1001历史，但本top不执行gate/metadata历史SELECT；后继复用原观察器记录的这些Service查询，在未删空及末pending状态测。原最早cause与native Audit不靠seed证明。 |
| transfer三访问索引与`transfer_upload_fk/candidate_fk/source_lease_fk`、`attempts_transfer_fk` | 需实际00007合法PUT staging↔transfer循环（原DEFERRABLE）、candidate/Upload/external lease/source lease全套父行；GET/PUT与retired/末pending分别在场，不能让缺父行或缺lease暗示退休。当前未覆盖。 |
| `uploads_current_attempt_fk`、`references_upload_fk`、`leases_attempt_fk`及其它已由unique/PK覆盖的入边 | 需在隔离回滚Tx删除合法无引用目标，保留其它Object/Project子行；`SET CONSTRAINTS`实际触发延期边，记录原FK trigger成本。SELECT父PK计划不能代替此项。当前未覆盖。 |
| `download_grants_project_active` | 需正式grant/attempt形状：大量revoked历史、末active及started attempt＋原download work关联；Archive/Delete差异和完整pending分别测。当前表空只作分支结果，不接受索引成本。 |
| `skill_work_project_history` | 需00027合法Project/Skill initialization与work外键，同Project1001 joined＋少量live、其它Project历史；原joined扫描和最后initialization父DELETE的全FK反查分别测。当前未覆盖，沿已独审Skills消费契约继续准备。 |


共享Skills成本来源另见 `metadata_cleanup_skill_cost_test.go`／`testdata/metadata_cleanup_skill_cost.sql`（仅源码）。其joined SELECT逐字取已验消费源598bc02e的`compressCleanupHistory`，不是本旧基线中尚未存在的Service实现；只测该SQL及00027真实约束成本。成本库保完整五核心互引、1001joined＋低ID running、外域10001joined，最后父DELETE在回滚Tx实际执行并**SET CONSTRAINTS ALL IMMEDIATE**冲刷原deferred队列；记录总耗时／队列耗时，不把DELETE plan未列出的延期trigger编造成逐项耗时，Rollback后核原数据恢复。transfer补充seed `testdata/metadata_cleanup_transfer_cost.sql` 目前尚未接测试：保65目标＋1001外域 retired PUT原staging↔transfer双向FK、candidate和external/source lease；真实SQL、活GET/PUT及FK成本观察仍待接入，不能算成本接受。


第三来源 `metadata_cleanup_transfer_cost_test.go` 已将上述transfer补充seed接入独立成本数据库，实际Service小对象只用于取得原gate/metadata/physical SQL。退休PUT形状执行原查询的32/16首批、empty范围、两个full-pending；原外部lease仍被引用的坏序删除必须实际23503/rollback，合法cleanup→staging→transfer→旧candidate→两lease的父DELETE与原延期队列flush单独观测，保其它Project包且回滚恢复。此项不调用Purge消费seed，不据它关闭正式PUT能力或32批次业务。三成本top已 `91195/536ec3` race-c＋精确list actual0（候选见current），尚无真实SQL/计划；第三方法待窄审，两个先前方法仅静态接受。活GET/PUT、download、其它父入边及实际计划后的索引减裁仍须闭合；不得以此组可编或一格通过接受全部22索引。


三成本top已按闭集组合映射到原两root工具（控制b3273f actual0），当前已编候选不变；仅新组合额外冻结本test包Go源与3个实际embed SQL，旧10入口/原7资源及所有预算/Wait/TCP/input尾保持。组合方法与业务源均未PG，入口待独立窄审；不以prepared关闭前述剩余成本项。


错误遵循现有Fault/CommitResult：输入/结果形状错误InvalidArgument；缺正式provider为DependencyUnbound；当前authority/owner/cause不符Forbidden或原Project gate错误；plan/native映射变化ResourceBusy且整Tx NotCommitted；合法仍活关系为Pending，超过有限完整诊断上限为Pending＋ResourceBusy。已持久的矛盾关系保持安全DependencyUnavailable/InvalidState，不暴露原Locator/SQL/正文。任何Unknown保留原error、cause和attempt；InTx返回Completed本身仍不是CommitResult，不能据它提前删其它事务中的父表。

### 7.3 成本来源的有限闭合范围（未实测）

当前以两个精确三top组合准备成本观察，复用既有root7资源、Go6m／root540+60+3／TCP75及全尾。原组`ProjectHistoryPlans|SkillsIndexPlans|TransferAndForeignKeyPlans`已编候选只涵盖其原三源；新组`LiveTransferAndDownloadPlans|FinalAnchorForeignKeyPlans|PendingHistoryAndCausePlans`现已与原三top/history编入同一新整包候选（82986/99d949 compile0＋七exact list0），执行仍分两个精确组，不将新源码当旧候选输入。后组只增唯一闭集入口与六个实际embed SQL的输入观察，旧组及其他11配置的预算/输入不变。

- live源补实际原五lane/完整Stop与Object gate/physical查询的有项成本：16GET+16PUT遵真实全Store32 outstanding门，17非终局attempt/每command最多2；原33+33草案超独立transfer配额的问题保留，不能复用该草案为合法状态。33active grants保分页，1001+10001 revoked历史在场；started-only虽joined仍pending，checkpoint+joined只影响原SQL谓词，不产生真实下载结果或Audit。
- final-anchor源保1001外域available Artifact/Object/Upload/current/reference/writer→attempt及原retired PUT全部入边背景；三坏序父DELETE拒绝与原current NULL→最后四anchor DELETE、原延期FK queue、同2s实际rollback分别观察。此格测SQL/FK成本，不替Purge权限、32业务批次或Skills最后五行。
- pending/cause源保65cleaned旧attempt、一条closed abandoned applying、原current已gated/abandoned，两个非终局符合配额。原current published却已有gated cleanup的草案不可达形状已按`gateAttempt`原子更新修正。原union两个候选集合去重/物理pending、已joined但错fence仍Stop pending及原`ORDER BY created_at,id LIMIT 1`真实捕获SQL均独立观测；较小ID较晚cause使错误排序可辨，不删除旧cause重算native Audit。

这些成本种子只进入另一个真实Migrator建立的独立SQL数据库，任何标记Audit/binding/退休状态均不交给Service或权限提供方。六成本来源仅有限静审接受，pending唯一原子shape返修Runner e0e2fb/b194bd接受，新后三top入口Work fd74fc接受；整包race-c和七exact list完成后，原三cost于83167首次真实整轮FAIL：ProjectHistoryPlans在真实Stop查询捕获前置失败，未到该top成本库；SkillsIndexPlans与TransferAndForeignKeyPlans业务PASS，原Go/driver/outer actualWait、七资源双尾/TCP/input均齐。两通过top仅接受各自实际SQL/延期FK/回滚范围，仍须据完整计划核索引成本；其它四成本top及history尚未通过实际组。两个捕获helper的真实小对象预读前置已tests-only修正，Work有限静态接受；修后de54整包82925编译actual0，新3b55候选37459554B；原七top list前空间门5061918720B不足而exit78保留，后单独8505e1在fresh5457285120B下仅补七exact发现actual0。新候选未执行业务，全部后继成本/history改用它、另候实际窗口；旧563全部排队用途取消，可由root精确退休，失败/源码/日志保留（身份及界限见current）；旧08 history和5c7c成本二进制由root精确退休，原编译/失败/未验记录不改。原三cost首次预飞67f7c4因fresh5334228992B不足5GiB而exit78，监督器/业务/资源均未启动，输出未触碰；只保留空间前置失败，后续另候fresh grant。剩余门是原history真实单组、Project前置修复后实际计划及后三成本组/全部FK检查与原2s返回，然后根据全计划中的扫描/过滤/loops/buffers和真实trigger成本判断22索引必要性或缺项；测试body返回不自动接受成本，也不扩大Runtime join或其它停项。

## 8. 旧源最小预计写域与验收

已获rev1独审及root授权的实现域：`contract/access.go`闭集；`access.go`真实binding与同Tx一次purge消费、`service.go`仅相应私有access事务记录类型；`cleanup.go`＋新bounded SQL helper；`reference_cleanup.go`仅Skills canonical cause重放；必要`references.go`仅新私有有限诊断helper；`project_work.go`精确cleanup准入；`project_stop_store.go`及必要`project_lifecycle.go`投影分页；新`object/metadata_cleanup.go`；必要`project_audit.go`仅终局查询。各相邻tests及最小`tests/objects`组合，均先获明确写权。Skills独占其planner/CleanupAuthority/六表/participant，Project独占CleanupPhase，root负责immutable routes/迁移/组合，Object不读Skills私表。

有限验收：纯result/Access闭集与同issuer/Store/liveTx/锁反例；原子gate两种真实Unknown和旧cause；65以上历史及超过1000投影、末尾活writer、32总额/原2s/服务重建；实际reader/callback阻塞尾及foreign guard；FK关联包/跨source保护、分批cancel/Unknown、最后两域一起保留或消失；最终Skills父映射已清后真实CleanupProject无旧维护，Audit恰一次且fake witness拒。旧受影响Avatar/Knowledge/transfer/Stop有限回归。

纯合同、受控Store、真实PG、真实MinIO/ProcessGuard、生产root分别报告；当前rev1只完成工程规格与纯类型结果，待未参与者核§4原子gate/终局、§5分页不漏writer和§6互引包/最后原子性，没有冒称真实三层通过。实现中发现本规格未覆盖的真实合法拓扑或原退休缺口时，暂停受影响路径并更新SPEC，不把未定义关系投影成成功。

当前作者仅纯合同2top race25403/8510e0、contract包vet793dd1、本卡链接与四文件UTF8/LF5693d2通过；旧Service/SQL尚未修改，不以类型可编译声称清理能力可用。
