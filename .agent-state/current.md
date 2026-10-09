# Skills 持久初始化服务恢复点

- 树：`/workspace/agenteam-skills`，分支 `ai/skills-service`；基线正式 main `ca9f2d5d`。D08已正式交付，旧初始化树保持冻结，不再修改。
- 当前：D10 rev3 SPEC经Variables作者有限独审/root接受；本域Store/repository/Authority/服务owner与观察/确认三口已形成可构建片段，既有P1不变；完整初始化写入/OwnerReader/恢复仍在实现，尚无真实PG或对象结果。
- 已保存SPEC片段：`docs/development/work-items/d10-skills-initialization.md`、`docs/development/work-items/d10-skills-initialization-design.md`、本文。已由root保存/push ea13186d，设计技术段继续freeze；未自行Git操作。
- 当前可复用：实际D05 same-Store Object Audit checker；D08 original initialization四口、收敛口与初始化Audit wrapper。真实Skill exact映射provider/root尚未绑定，constructor非nil不证明真实组合。
- 共享待协调：D05初始化Service closed shape/initiator；SkillRevision+ProjectDeleted release；Project CleanupPhase现unbound；本域active初始化与删除Audit分流；生产同participant要组合届时实际启用Variables等域。root现已授权本树D05三个已列窄补口及定向测试，须单独freeze独审；Project CleanupPhase/root仍未授写。Object runtime join停止项不恢复。
- 迁移预留00027，仅本域schema。root集成24/25/26准确前缀前不做真实迁移。
- 下一步：SPEC独审期间先实现无歧义本域Store/状态/四口及exact authority，受控delegate与真实PG/真实Object/生产root验收分开；共享差异报root，不用stub冒成功。资源/热cache/真实PG或MinIO需freshgrant；不spawn，root协调交叉审查。
- 当前没有本实例运行进程/真实资源/缓存租约，未经运行的范围不得写PASS。必要失败和实际检查在本恢复点按发生追加。

## 首个持久实现片段

- 新 `internal/central/skill/{store,initialization_state,initialization_state_test}.go`：同Store接口、原Fault/UnknownAttempt及cause保留、原命令摘要、冻结builtin持久状态、精确Revision owner与真实Skill父锁；不是已可调用服务或成功initializer。
- 新 `db/migrations/00027_skills.sql`：6本域表及精确composite FK/closed状态草案，无跨域FK/查询/共享Audit CHECK；24/25/26未齐，未执行DDL。
- 首次pure session7624 actualexit1：构建误用不存在的`object.NormalizeLocks`，0行为测试；随后改为已验证且按Foundation顺序的4锁集合。
- 修后session56471 actualexit0：Go1.27.1 `go test -mod=readonly -p=1 ./internal/central/skill/... -run '^TestInitialization' -count=1 -timeout=45s -v`，3top/9子，skill0.008s；contract无匹配不算新增PASS。GOPROXY/GOSUMDBoff、GOMAXPROCS2、自有复制cache位于Variables树output/ai/project-variables/independent-http-review，未共写作者cache，无网络/真实资源。
- 该4源+主卡/本文6路径当前freeze交root WIP；SPEC技术段不变。下一新增repository/Authority与四口实现，同时按已授范围做独立D05闭集补口片段。原SQL/PG/实际对象/Unknown ACK/生产root均未验，不能据pure写完整结果通过。

## D05 精确兼容片段（已有限独审）

- root授权的7路径已freeze：`internal/central/object/contract/{authority,access,reference_cleanup,knowledge_cleanup_test,skill_initialization_test}.go`、`internal/central/object/{transfer_upload,skill_initialization_test}.go`。
- 只增加原ProjectInitialization+SkillRevision+同Project/UUIDv7 CreationCause的Read/Mutate形状；ReadObjectID/ProtectedLease拒绝。原fullplan还必须由本域当前Authority校验，不是通用Service许可。唯一Service initiator持久原CreationID且在任何SQL前核精确grant。Release shape只增SkillRevision+ProjectDeleted，原Knowledge矩阵对应一格更新，具体Release和Runtime没改。
- 作者离线Go1.27.1/p1/GOMAXPROCS2，18349 pure actualexit0：新3top+旧Avatar/Knowledge相关兼容全部通过；96242 object/contract完整race actual0/1.048s；86475唯一真实reservation函数SQL边界控制race actual0/1.023s。这些没有PG/对象网络，仍需未参与者窄审和后续真实组合。
- root已精确导入00024=334f86d0、00025=da16d95a、00026=4174e160的迁移字节；没有运行SQL。24/25各域作者已验、26当时仅独审，不能据此称24..27整链已PG通过；前序由root按各域正式交付集成。00027仍本实例唯一写域，本轮未改。
- 当前无本实例编译或真实资源在途。本7代码+主卡/本文9路径freeze供root保存/派独审；后续新repository/Authority与4口继续，不等待Object停止项或生产root。

## 当前服务片段

- root 已保存/push c59189ad：此前 D05七源+主卡/current、`skill/{repository,repository_test,authority}.go`、`skill/{service,service_test}.go`及前序三迁移。D05由未参与者recover_harness有限独审接受：真实函数overlay 2top/6子actual0，仍非PG/MinIO或Runtime停止组合。
- repository/Authority作者26587 pure actual0：累计5top/20子，扫描损坏与当前Project gate/held/live顺序；service owner作者67258 race actual0/1.056s：累计7top，缺绑定与取消后容量/实际join；不是完整initializer。
- 新 `skill/{initialization_read,initialization_read_test}.go` 实现Inspect/DiscoverConfirmation/ConfirmInitializedInTx：仅原command当前门禁，同Store活Tx/完整真实父锁、完整发布关系；读事务未明确commit不外送结果，Confirm不新Tx/补锁/I/O，原issuer/ctx/Tx/actor不替换。当前写入四口的Initialize尚未实现，不声明已满足完整接口。
- 首轮57171 race actualexit1：确认12子通过、观察7子中denied错误身份控制失败，原因Foundation CommitResult复制/改写callback Fault；已修为确认NotCommitted时返回原callback error，Unknown外层仍保留。原失败不删除。
- 修后71990 race actual0/1.757s（2top/19子）；再加原delegate Unknown负控96767 actual0/1.057s（1子）。当前2top共20子有效，均受控SQL/Project端口、无PG/网络。精确命令 `go test -race -mod=readonly -p=1 ./internal/central/skill -run '^TestInitialization(Inspection|Confirmation)' -count=1 -timeout=45s -v`，工具链/隔离cache同上。
- 本新2源+主卡/current共4路径freeze交root checkpoint；下一继续初始化Plan/Reserve/Publish与exact Object/Audit authority，不等待生产未绑定项，不改已审D05源。当前无本实例运行资源/命令。

## 初始化写入可构建片段

- 新 `internal/central/skill/{initialization_write,initialization_write_test}.go` 已实现正式 Initialize 与四口编译断言：原冻结命令规划 → 实际builtin payload准备 → 同Tx Reserve+精确attempt映射 → 已知commit后UploadPrepared → 同Tx不可见Skill/真实ObjectPublish/Revision/初始化完成。Known重放不再physical；无scope/完整metadata匹配或仍prospective receipt则整个Publish Tx失败。
- 作者60171 `go test -race -mod=readonly -p=1 ./internal/central/skill -run '^TestInitializationWriterCommitBeforePhysicalAndAtomicPublication$' -count=1 -timeout=45s -v` actualexit0/1.174s，1top/11子：正常与重放、Plan/Reserve/Publish各Unknown、physical/revoke/publish失败、foreign Object/receipt、真实Discard后才释放本地call。端口用明确controlled事务/Object替身，不是真实PG/D05。
- 新源编译22189 actual0，原观察/确认相关纯控0。当前四口编译闭合不代表完整服务：持久work/recovery/生命周期和真实Skill Object/Audit授权、OwnerReader尚需接入；无Production root或真实对象声明。新2源+主卡/current4路径freeze供checkpoint后继续authority/runtime，不改此前D05七源。
