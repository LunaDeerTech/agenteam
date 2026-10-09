# D05 有界收敛与单对象元数据清理

状态：rev0 可恢复设计草案与纯合同，尚未独审或实现 Service/SQL。正式基线 main `b2a7d0ab`。本结果解开 initialized Skills Cleanup 的具体上游缺口，不恢复 Object Runtime join 停项，不代表完整 D05/Skills participant。

## 1. 来源与范围

消费权威是 `ai/skills-initialization` 的 `docs/development/work-items/d10-skills-initialization-design.md` §16.6/16.6.2，root 已保存 `1e8b5c67`；本基线若尚无该文档，直接读权威分支，不复制一个不同版本。D05 以[原设计](d05-object-storage-design.md)、[Runtime 说明](../backend/object-runtime.md)、真实 `cleanup.go/reference_cleanup.go/access.go/project_work.go/project_stop_store.go` 和迁移00005/7/14/16为准。

第一 provider 只支持 initialized Project 的已发表 SkillRevision＋ProjectDeleted、原 Creation/Skill/Revision/Object/Upload 和同命令的旧 candidate。不加 Creation取消、其它Owner新purge权限、Runner退休、HTTP/生产root或自动删除永久marker。

初始写域仅本卡、current、新 `object/contract/metadata_cleanup.go` 及测试。下面列的旧源需设计独审后接写权；没有 SQL 迁移号，00028仍仅为Skills预留。无PG/socket/browser/network授权。

## 2. 实际缺口与必须成立的结果

- `gateObject/stopWriters/cleanObject` 的 `attemptIDs` 全历史扫描。原活候选2个配额排除已cleaned历史，不能约束历史规模；stopWriters还逐项load，不只改claim查询就能解决。
- `finalizeCleanup` 未完成count与最早cleanup cause查询缺exact Object访问路径；native Audit又重复同类检查。
- `CleanupProject` 先取Owner Maintenance再看Deleted。先删Skills映射会永远失去授权；原最后多表DELETE也无界。
- 已有 `project_stops` 会令 `maintenanceAdmission` 拒绝新增cleanup工作，故必须给正式Cleaning的exact cause一个窄许可，保留旧stop gate。
- `projectStopFacts` 对每个Object全历史投影LIMIT1001，超过即overflow；不能先等Stop完成再靠metadata删历史解开它，这是实际依赖循环。
- 本main的CleanupRelease闭集尚缺SkillRevision+ProjectDeleted；Skills分支已有接受增量，由root定向整合，不再发明另一个版本。

正式zero_marker首版永久保留：完成指payload消除＋空marker核实＋独立writer/lease/work退休，**不是删除marker key**。Skills“marker删除”措辞已经发给作者校正，不改变D05物理规则。

## 3. 独立typed口

`DeletedObjectMetadataPurger.PurgeDeletedObjectMetadataInTx(ctx,tx,cause,object,plan,locked)` 返回 `ObjectMetadataPurgeResult{State,OperationID,ObjectID}`。State仅Pending/Completed，Validate/MatchesOperation只检查数据形状和IDs。结果可以由任何人重建，不能作为physical/join/commit proof。可选接口未绑定则DependencyUnbound，不扩原Objects/ReferenceCleanup迫使假实现。

唯一新operation `PurgeDeletedObjectMetadataAccess` 仅属于ObjectCleanupAccess。纯合同阶段只声明常量，原AccessRequest暂不接受它；构造、Validate/fingerprint、Service行为必须在后续实现阶段一起闭合。旧CleanupObjectAccess/普通Owner plan不通用。

请求绑定完整cause（Owner/Project/reason/operation）与exact Object；D05 discovery绑定真实Upload/current attempt/原command与当前批依赖。一次完整union包含Project EX、真实Skill EX、Object EX、原command/Owner reference以及本批原work/关联记录锁。真实issuer/live Tx/同Store/actual Acquire及当前Skills CleanupAuthority均重新校验；公开字段一致不构成授权，foreign issuer/Store/Tx、漏锁/弱锁、ended Tx、映射漂移均拒。

该InTx口不取消、不做外部I/O/ProcessGuard调用、不开goroutine、不加锁或嵌套事务。调用方必须回滚任何error；Pending只有外层Committed才算进度。缺anchor不是成功重放；最终Unknown由Skills在当前Project EX和正式gate下确认唯一共同最后事务的六表全空。

## 4. 原子gate与32项物理推进

沿现有同Tx原子关闭：exact Upload revoked、Object cleaning、删除exact reserved/canonical引用，与Skills唯一gated记录一起提交。保留原published current attempt的ProjectDeleted cleanup行至最后；旧AbandonedAttempt继续保原operation/reason，不为重放改写原因。不引入公开parentgate布尔。

本scope初始化已发表current唯一、原私有未终局candidate受配额限制。gate只发现原Upload尚未cleaned的候选，至多33条，最多32条实际处理；已真实cleaned历史不重复gate。合法集合若出现超配额/错kind/混杂事实，不能部分成功并称全gate，须Pending/安全错误。新Reserve/Send/Publish/Consume仍在原锁下受revoked/cleaning禁止，不把分批candidate标记变成可以复活的中间态。

**phase=cleaned不单独证明writer结束**。另以未io_closed、active lease、未joined work/未完成cleanup的索引谓词发现旧marker背后的活writer；本轮stopWriters的load/cancel/actual wait/join、claim、I/O和checkpoint复用同一至多32个原候选集合，不每阶段再挑32个不同ID，不先走全历史前缀。未选项仍由最终完整EXISTS阻止Completed。

实际writer只能凭原本地done/returned及原Tx尾、原持久终局，或正式ProcessGuard对exact ProcessID的死亡证明推进。ctx.Err/取消已发/map为空/TTL/marker成功不是join。原claim worker/fence/operation保持，不能用新fence覆盖仍活旧worker。每次物理操作实际返回后才能checkpoint；Unknown保留原cause/attempt、不发推测性下一次I/O。

所有Tx/I/O/实际Wait受调用者原剩余预算及force cap，沿现WithinBudget，不扩D08的2s或补一轮新2s。最终用固定数量可索引EXISTS确认无ref/active lease/未cleaned候选/未完成cleanup/未退休原work；不用全量count或LIMIT空诊断推终局。原native ObjectDelete Audit＋Deleted事务只一次，沿最早原cleanup cause摘要和私有witness，metadata不重写Audit。

## 5. stop后精确cleanup准入与Stop历史循环

ClaimCleanupAccess的窄许可在**原同Tx**读取保留的current cleanup ProjectDeleted cause，核SkillRevision/原Object/Upload，并实调本Store CleanupAuthority；Skills重验D08当前Cleaning、Owner/cause/version、Stop齐备与gated/pending。成功才注册原worker/fence的cleanup work；普通preparation/verification/其它maintenance仍走原deny。Checkpoint保持原admitted work/witness；Finalize当前授权保持。Skills转completed后不再发新物理claim，只允许独立metadata operation。

物理调用返回后的operation-work实际join仍必须闭合；失败/Unknown保留未joined记录，由原技术join恢复，不要求重新获得已经关闭的业务物理许可。metadata不能先删这些记录再声称退休，也不在持Tx时等外部过程。

进入Cleaning前，Stop投影必须去掉“一个Object扩展全部终局历史”这一循环。方案：现持久lane/after分页选主ID，仅投影本轮实际修改/取消/退休的native/work及固定Object/Upload/command父映射。joined work和真正终局attempt/lease/cleanup不作为全历史row_to_json fanout。未退休work在自己的主lane按原ID推进，本轮未选项不取消、不改joined；Object lane不间接批改未纳入完整plan的行。每个选中原writer/callback仍携带所需实际身份、worker/fence和完整原锁；闭包过大则缩小主集合，不截断关联。

最终projectStopPending继续在当前Project EX下查全部当前未退休事实，与cursor/本批诊断独立。把活writer放在历史末尾也必须拒Stopped；不以phase过滤漏掉cleaned但未io_closed的旧writer。**尚需实施前细化**：各主lane/native依赖包的精确分页SQL及总额，确认无需改变现scan_kind/after；如必须新增内部状态，先提出真实合法反例和迁移需求。此项为Service/SQL前置，不赋予Runtime/Guard Close/Drain修改权。

## 6. Metadata与最后anchors

Skills只有在D05物理调用实际返回、持久确认本域completed并完成自己的历史压缩后进入此口。D05同union Tx重验当前CleanupAuthority与完整Object/Upload/current attempt/current cleanup、Scope/Owner/initiator/Creation。Deleted必须是D05真实finalize/native Audit的持久结果，不消费public result字段；还必须核所有原work/reader/writer/cleanup/transfer实际退休及当前无新工作。没有真实证明则Pending/原错。

只有这套终局前提齐全才开始单调删历史。每批重新检查当前gate及剩余private facts；Deleted/revoked gate保证旧请求不能再生成工作，删除过的只是此前真实确认的终局历史。metadata不会自己append/delete ObjectDelete Audit或新增Event/Activity。

每Tx跨表合计最多32条DELETE，最后anchors也计数；不每表各32、不级联隐藏无界工作。按FK依赖选择有限exact ID：终局transfer关联包、released lease、joined work、旧completed cleanup/cleaned attempt。当前attempt及其cleanup、Upload、Object一直保留。来自其它Object的source引用不在本口越权清除，仍有引用则Pending由真实Owner退休。

00007 transfer/staging互引必须同Tx删除完整依赖包，不能将transfer_id清NULL伪造kind。每个包有正式retirement、external/source lease释放和原本地join；包及其它DELETE总数不得超过32。**尚需实施前细化**：精确包的FK拓扑/锁集合及跨对象source依赖查询，不因当前Skills夹具没有transfer跳过正式检查。

历史确空时，最后最多4行current cleanup/current attempt/Upload/Object同Tx清除（先置current_attempt_id=NULL解除FK），Skills立即同Tx删其核心cleanup/revision/skills/current attempt/initialization。任一步失败一起回滚；外层Known Committed才报告Completed。最终Unknown保留原Tx cause/attempt，Skills重取原Project EX/current gate核六表全空及原本地实际尾；部分核心缺失属矛盾，完整核心则续metadata，不再次对已失父映射Object调用维护。

## 7. 索引与DDL边界

可复用：uploads.object_id unique、attempts(upload_id,ordinal) unique、cleanup.attempt_id unique、references Object前缀PK、leases(object_id,owner_kind,owner_id) unique和active Object partial索引。

实际缺口：本Upload未cleaned/本Object未io_closed、cleanup Object phase及最早created_at/id、本Object work未joined和历史分页、transfer Object与跨source_lease依赖。全局attempts_recovery/cleanup_recovery、work(project_id,id)、transfer(project_id,recovery_pass,id)不能宣称覆盖。候选索引须跟实现SQL一起收敛：原Upload非cleaned范围、Object未io_closed、cleanup/work Object pending与历史、transfer Object/source lease。先核EXPLAIN/BUFFERS和数据量，不机械全建，不将LIMIT当扫描成本上界。

需要由root分配**新的唯一迁移号**后才可写SQL；不碰Skills00028和历史迁移。本轮尚无PG/EXPLAIN，以上是源码查询缺口证据，不是性能PASS。

## 8. 旧源最小预计写域与验收

设计独审后：`contract/access.go`闭集；`access.go`真实binding；`cleanup.go`＋新bounded SQL helper；`project_work.go`精确cleanup准入；`project_stop_store.go`及必要`project_lifecycle.go`投影分页；新`object/metadata_cleanup.go`；必要`project_audit.go`仅终局查询。各相邻tests及最小`tests/objects`组合，均先获明确写权。Skills独占其planner/CleanupAuthority/六表/participant，Project独占CleanupPhase，root负责immutable routes/迁移/组合，Object不读Skills私表。

有限验收：纯result/Access闭集与同issuer/Store/liveTx/锁反例；原子gate两种真实Unknown和旧cause；65以上历史及超过1000投影、末尾活writer、32总额/原2s/服务重建；实际reader/callback阻塞尾及foreign guard；FK关联包/跨source保护、分批cancel/Unknown、最后两域一起保留或消失；最终Skills父映射已清后真实CleanupProject无旧维护，Audit恰一次且fake witness拒。旧受影响Avatar/Knowledge/transfer/Stop有限回归。

纯合同、受控Store、真实PG、真实MinIO/ProcessGuard、生产root分别报告；当前仅形成草案，没有冒称后三层通过。正式可实施SPEC接受须先闭合§5/§6标注的算法细节。

当前作者仅纯合同2top race25403/8510e0、contract包vet793dd1、本卡链接与四文件UTF8/LF5693d2通过；旧Service/SQL尚未修改，不以类型可编译声称清理能力可用。
