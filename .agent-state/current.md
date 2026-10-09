# Skills 持久初始化服务恢复点

- 树：`/workspace/agenteam-skills`，分支 `ai/skills-service`；基线正式 main `ca9f2d5d`。D08已正式交付，旧初始化树保持冻结，不再修改。
- 当前：已完成Variables独验并返回D10作者；rev3 SPEC已获独立有限接受。本域四口initializer、OwnerReader、持久work和有界公平Recover均可构建，正在准备真实PG持久初始化/重读确认与原COMMIT恢复。尚无本域真实PG、D05对象或生产root结果。
- 已保存SPEC片段：`docs/development/work-items/d10-skills-initialization.md`、`docs/development/work-items/d10-skills-initialization-design.md`、本文。已由root保存/push ea13186d，设计技术段继续freeze；未自行Git操作。
- 当前可复用：实际D05 same-Store Object Audit checker；D08 original initialization四口、收敛口与初始化Audit wrapper。真实Skill exact映射provider/root尚未绑定，constructor非nil不证明真实组合。
- 共享待协调：D05初始化Service closed shape/initiator；SkillRevision+ProjectDeleted release；Project CleanupPhase现unbound；本域active初始化与删除Audit分流；生产同participant要组合届时实际启用Variables等域。root现已授权本树D05三个已列窄补口及定向测试，须单独freeze独审；Project CleanupPhase/root仍未授写。Object runtime join停止项不恢复。
- 迁移00027仅本域schema，尚未SQL执行。root已精确刷新00024到正式3cea6076，00025保持da16d95a、00026保持4174e160；三前序各域SQL事实可复用，不代表本树24..27组合已验。
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

## 精确授权、Owner读取与持久工作阶段

- root已实际保存/push e6edd3c7：初始化写入、Object exact authority、初始化Audit facts及对应测试。Object授权只开放原Service初始化变体和Human当前Owner读，Agent/维护/生命周期未绑定分支仍拒；Audit provider同Tx精确映射后仅委托一次原ctx/Entry/key，constructor不能机器证明真实Skill+Object组合。作者Object authority2top/15子race99651 actual0；Audit1top/13子race56098 actual0，包括真实Object checker缺私有witness拒绝。尚无真实PG/MinIO。
- 新 `skill/{read,read_test}.go` 实现ListSkills/GetSkill：每次真实端口当前Owner/Session/Project门禁、完整父锁、已发布immutable关系；未初始化不伪装空列表。首轮95559因测试Project名称含空格setupFAIL，修fixture后96465 race actual0/1.100s，1top/10子。OpenPackage仍待实现，不声明完整OwnerReader。
- 新 `skill/{work_repository,work_repository_test,runtime_work}.go` 和service/initializer接线：physical前同Tx持久登记原process/父关系/唯一work；只有实际Discard与调用返回后才技术结账，cancel/Stop/Unknown不得提前Joined；Drain沿调用者原ctx，不借新预算。原注册Unknown用原command锁串行判定，未确认尾保留本地owner。跨进程恢复/真实生命周期仍待实现。
- work片段首轮36292错误引用不存在sc.Skill、随后534f15测试unused import均setupFAIL，修后65428 race actual0/1.072s（1top/10子）。集成81715 race actual0/1.355s：5top/23子，含writer13子、work10子和实际阻塞Discard后取消/原预算/持久join控制。精确selector `^Test(InitializationWriter|SkillInitializationWork|SkillService|SkillWork)`，Go/p1/offline/cache同前；无socket/PG。
- 本批read2、work3、service及writer2、本文/主卡共10路径freeze交root。设计技术段、D05七源保持冻结。下一继续OpenPackage、维护授权/跨进程恢复；局部controlled端口PASS不替代实际Object/PG或生产绑定。

## Owner PackageReader可构建片段

- 上一read/work10路径已root保存/push629cd46b。新 `skill/{read_package,read_package_test}.go` 实现OpenPackage及OwnerReader接口断言：当前Owner授权/完整immutable Revision和work登记在同一次已知commit后才真实Object ReadObject；由Object再查当前门禁，不移交缓存grant。
- 原P1 PackageReader保持，底层本域owner同时等实际Read/Close和取消callback返回；EOF不等Close，取消/Unknown不等work已commit，D05租约Unknown仍由D05持有。未借新的清理ctx，原错误/原预算保留；错误body、metadata/range不符都实际Close。
- 作者32726 race actual0/1.096s，2top/10子（当前gate/登记Unknown/错revision、Object、range/原body与Close错误/退休Unknown/实际阻塞Read+取消）；补取消发生在Open移交前91114 race actual0/1.053s，1top。源保持原P1内容/契约，所有Object和SQL为明确controlled端口，没有网络、PG或D05实际lease结果。
- 新2源+主卡/本文4路径freeze交root，继续维护精确映射与跨进程恢复；不把完整OwnerReader编译断言称作生产绑定或完整Skills完成。

## Object维护映射窄片段

- OpenPackage4路径已root保存/pushd74ec396。新 `skill/{object_maintenance,object_maintenance_test}.go` + `object_authority.go` 维护分派：仅Inspect/原writer完成/旧attempt join/reader释放/process释放，重验本域唯一Object→Project/Creation/Skill/Revision/upload及精确attempt/process/完整父锁。同Store活Tx重读；普通Owner/read/write门槛未变。
- 这是D05既有私有实际return/lease/process验证之外的本域映射，不制造Service授权；维护回收不以已关闭active门禁阻塞原技术结账。新physical恢复、不可逆清理仍未绑定，继续拒绝；生产组合必须由同实例D05检InstanceID和私有生命周期证据。
- 首轮28333 race actualexit1：原2top/15子PASS；新14子中8个正常准入前FAIL，实际测试executor将单列查询前缀误匹配本域完整row查询，未到目标行为。只收紧测试query匹配后30392 actualexit0/1.093s，新1top14子均过；原产品源不改，原失败保留。
- 该3Go+主卡/本文5路径freeze供checkpoint。下一持久恢复按pass推进忙项，并只用精确旧Process终局+原锁/当前gate收敛，不代D08重开发布；尚无真实PG/对象/生命周期组合。

## 持久恢复阶段与暂驻

- 维护5路径已root保存/push0638ddc3。新增 `skill/{recovery,recovery_test}.go` 和00027 work recovery_pass/index：一次最多100条按持久pass轮转，忙/当前gate拒绝项也经明确技术调度commit移至队尾；不是重发初始化，绝不调用Reserve/Upload/Publish。
- foreign实例必须ProcessAuthority精确旧ProcessID停止，随后实际取原command/Project/Skill完整锁以证明原Tx终局；同进程只信本实例私有returned记录，不按本地缺记录/TTL猜死亡。当前convergence gate/原Fault/Unknown保留，后续启动Service复用持久pass，不把已joined前仍未commit当成功。
- 首95974及27820均actual setupFAIL：误用不存在id.NewService；改正式RegisterService.Actor后85824 race actual0/1.410s，3top14子（包括100忙head后新Service达到第101条、原gate/进程/fence/Unknown、坏候选/原取消预算）。随后8057完整skill/... race actual0：skill2.441s、contract1.311s；50111vet actual0，diffcheck0。00027仍未SQL执行。
- 新3源+主卡/本文5路径freeze供root保存。当前Skills暂驻在可构建边界，转任Variables未参与产品实现的独验；本树无资源/命令在途，不改已冻源。
- 仍缺真实完整能力：Skills00027及前序整链PG、实际D05/MinIO发布与私有witness正例/COMMIT ACK丢失；D05 Recover/清理planner、Project lifecycle/删除Audit外层与participant仍须继续实现，Project CleanupPhase尚unbound且root未绑定；Object Runtime停止项不解除。技术尾授权和controlled PASS都不能证明生产完整Skills服务。

## 恢复Skills与首个PG验收片段

- 新 `tests/skills/{fixture,initialization}_test.go`：连续真实迁移、真实Store/锁/ProjectAuthority与Skill四口；覆盖持久发布、重建Service后同原ID观察/重放、私有issuer/完整锁/ended Tx确认拒绝、发布失败的原子回滚与保留attempt。Project/Creation是明确的测试规范事实，不是Project.Create/真实Human会话验收；外部Object是受控端口，但本域AccessPlanner与OwnerAuthorization仍调用真实Skill+Project实现，不冒D05/private witness正例。
- 离线Go1.27.1/p1/GOMAXPROCS2/GOPROXYoff、独占原Variables独验GOCACHE：`go test -mod=readonly -p=1 -race -tags=integration -c -o output/ai/skills/compile/skill-pg.test ./tests/skills`，26616 actualexit0；2732a4实际精确发现 `TestSkillInitializationPersistence`/`TestSkillInitializationPublicationRollback` 两top。只编译/发现，没有执行PG或网络。
- root额外精确导入正式3cea6076的 `.agent-state/project-variables-independent/commitproxy/{proxy,proxy_test}.go`，后继原完整COMMIT帧恢复直接复用它；不得修改此已验helper或另造proxy/监督框架。前序00024变更随本批保存，25/26逐字旧稳定源，不制造变更。
- 本批两新Go+主卡/本文+上述两proxy+00024共7路径可构建freeze交root；无编译/资源在途。下一另新增COMMIT恢复测试接线，真实105+15/123+3/75窗口仍须freshgrant；现有PG两top未动态。D05/生命周期/生产root未闭合与Object停止项原样保留。
