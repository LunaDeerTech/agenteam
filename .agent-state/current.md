# D08 Project 初始化 Audit 授权库恢复点

2026-10-09；本文件只记 `/workspace/agenteam-project-initialization` 的当前模块，不作为原 Model 或全团队台账。

- 作者：`/root/model_delivery/independent_acceptance`，已由旧 Model 独验角色转为本卡 SPEC 与后续实现作者；不能独立验收自己的实现。
- 交接树/分支/基线：`/workspace/agenteam-project-initialization` / `ai/project-initialization` / 正式 main `f1c94ee5`（父/root 交接事实；本人未执行 Git）。
- 当前阶段：本卡库范围实现/验收完成；作者离线pure/race/vet/编译和三真实PG top（42子例）完整PASS，model_delivery本人产品独审及独立PG四子例完整PASS。README末件已完成，精确正式8路径及本恢复文件冻结交root，尚不代root声明正式main提交。
- 核心范围：`NewInitializationAuditAuthority(*Authority, audit.ProjectFactAuthority)`；初始化三个 Object Audit action 的同 Store 活 Tx/Project EX/Project-Creation-owner-状态事实，随后原 ctx/Tx/Entry/Key 委托完整上游 facts。普通四口保持原行为。
- 关键区分：ObjectService Audit Actor cause 是 UploadID/AttemptID/delete digest；CreationID 在 typed Service initiator metadata。Entry 没有 initialization key 参数；真实 Skill provider 必须用自己的原请求/key 调原 Project gate，再组合既有 Object 私有 witness checker。当前真实 Skill mapping provider 不存在，非 nil 接口/受控 delegate 不能证明生产组合；本库不接根或创建 HTTP，不称 Skills/Object 发布可用。
- 已读：本树 AGENTS、团队 README、architecture/backend/documentation profiles；design、Go、test-engineering、documentation、database 技能；D10 初始化设计、D08 Owner/收敛卡及验收，实际 Project/Audit/Object 接口与真实 PG fixture。
- 唯一后继写域：新 `internal/central/project/initialization_audit.go`、`initialization_audit_test.go`；新 `tests/project/initialization_audit_fixture_test.go`、`initialization_audit_test.go`；本卡/current；完整验收后的 backend README 必要事实。不改旧 audit_facts、共享契约、Skill/app/Object 停止源或迁移；00024 已留 ProjectVariables。
- 当前禁止 Git、新子席、network/socket/PG/browser/真实资源；离线 Go 编译/作者纯测已授权，Git 与资源由 `/root` 协调。SPEC/阶段冻结同时报 `/root` 和 `/root/model_delivery`。模型 astra ultra；priority 不可证。
- 验证边界：三作者PG top与独立四子例已完成，包括真实Object缺witness负控；受控delegate不是Skill exact-mapping provider，没有真实Skill/Object正向发布、生产root接线或实际COMMIT ACK丢失验收。Object Runtime join停止项保持，完整D08未完成。
- 下一步：root将精确8路径导入已创建的正式delivery树并完成原子交付；本作者无Git/资源。后继D10等待根单独新树/授权，不在本树实现。

## 首个实现片段（2026-10-09）

- 冻结可保存：`internal/central/project/initialization_audit.go`、`internal/central/project/initialization_audit_test.go` 与本卡/current。原源未改；尚无 integration 两新源。
- 实际离线命令：`GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOMODCACHE=/workspace/agenteam/output/ai/model-ui-recovery/go-mod GOMAXPROCS=2 timeout 45s /workspace/toolchains/go1.27.1/bin/go test -mod=readonly -p=1 ./internal/central/project -run '^TestInitializationAuditAuthority' -count=1 -timeout=40s`，session 20939 实际 exit0 / 包 0.015s，三个新 pure top 完成。只读取现有第三方依赖缓存，未读取或改旧 Model 源。
- 原失败：默认 GOMODCACHE 缺 pgx/goose，GOPROXY=off 直接 exit1；改用已有依赖缓存的冷 `go build` session 67939 触45s上限 exit124。新测试第一次编译 session75254 exit1（三个测试符号名误用），修正后 session47037 exit1（非匹配 producer 原行为为 Forbidden，测试误期 Unbound），已按原 Authority 对照纠正；产品未为测试改 gate。
- 当前结果仅作者 pure，未 full project/race/vet/真实 PG/独立实现接受；不得宣称本卡完成。

## 四新源可构建阶段（2026-10-09）

- 已补 `tests/project/initialization_audit_fixture_test.go` 与 `initialization_audit_test.go`。普通 integration 编译 session37494 exit0；按真实 schema 修正测试构造后 session56940 exit0；精确 list 实际3top，未执行body。
- 真实 fixture 只复用正式 Project/Creation seed、两个 postgres.Store/原 migrator；受控 delegate 明示不是 Skills provider，原key通过原Project gate检查；真实Object checker只做无private witness的拒绝。包括真实持锁/poison、caller rollback、同Tx provider查询错误与三action四状态。
- 自查已在运行PG前修正两测试构造：反向Creation矛盾在原deferred FK的caller Tx内到达gate并回滚，不冒 schema错误为gate拒绝；archiving/archived/deleting用已完成Project与真实本域lifecycle输入，保持DDL原约束。不改产品或schema。
- 当前四Go源/SPEC/current阶段freeze供root保存；full project/contract pure session83500 actualexit0（0.114s/0.023s）；下一补 race/vet、integration race构建及最小PG资源命令，真实PG未授权未运行。

## 离线验证与PG监督接缝（2026-10-09）

- root已保存首批46116076、四源完整阶段5945abf0并实际push（根交接事实，本人无Git）。四新Go产品/测试源继续保持该输入。
- full project/contract vet66720 exit0；默认冷cache race5087/24901均45s exit124，无race body结果。根授热cache短窗和仅编译120s上限后，pure race-c13272 exit0；两个pure-race binary6000均PASS/actual0（各原40s body/45s外界）；integration race-c79680 exit0，`project-race.test`精确list新3top实际0。pg-only-driver build99427 actual0。热Go cache已向root释放。
- 真实候选只1 PG17固定镜像容器+1nonce network，不需要MinIO/PG16/Object Runtime。复用原 `.agent-state/task-planning-recovery/pg_only_driver.go` 与其 supervisor；命令传本树output的driver/project-race.test，每次只一个精确top，原driver105+cleanup15、supervisor123+3/TCP75/5GiB。
- 根唯一新增写权：本树原 `pg_only_supervisor.py`，仅把non-root异常direct/adopted收尾置原3s绝对deadline；TERM最多1s，SIGKILL directWait与adopted共享余量。direct尚未wait不得generic waitpid抢reap，pending显式FAIL/actual=false，不能写joined。旧root-chain/正常两ID/原TCP尾不变。
- 本地进程控制源在ignored `output/ai/project-initialization-audit/supervisor-controls/check.py`；v1六控34175 exit0/实际owned desc空，原日志保留，其中exited-adopted parent被过短测试启动预算截到，故v2明确让其自然exit0、忽略TERM正控实际-9。v2结果另见result-v2.json及*-v2.log，不覆盖原件；无socket/network/PG。
- 下一：监督器单源freeze交未参与者窄审；root freshgrant后先实际新Facts top，完整资源尾后再按调度其余两top；产品独立静/PG补集仍待root分配。本卡仍不是产品接受，未来真Skill+Object组合/HTTP/root均未绑定。

## 作者首个真实PG完整终态（2026-10-09）

- supervisor已4b82af57保存并获recover_harness本人独立6控接受；本人v2控制83492 actual0，真实child全Wait且最终desc空。
- 首Facts精确命令：`python3 /workspace/agenteam-project-initialization/.agent-state/task-planning-recovery/pg_only_supervisor.py --driver /workspace/agenteam-project-initialization/output/ai/project-initialization-audit/pg-only-driver --binary /workspace/agenteam-project-initialization/output/ai/project-initialization-audit/project-race.test --run '^TestProjectInitializationAuditFacts$' --output /workspace/agenteam-project-initialization/output/ai/project-initialization-audit/pg`，cwd本树；session64065 actualouter0。
- Go2.57s/26子例PASS，Go538098 Wait0/driver536678 Wait0，未有adopted；完整72.323s，1PG+1network双absent、desc2空、私有凭据与CA私钥退休（只留owned.json）、HOST_TCP2空、inputs unchanged。安全原件`pg/pg-bee9d7b19c6b4f80b22ac79400bbc0a5.log`，全局窗已交root。
- 下一精确selector `^TestProjectInitializationAuditTransactionBoundary$`，随后 `^TestProjectInitializationAuditDelegation$`，同以上命令仅换run参数；四Go源/race binary/已独审supervisor均未变，无需重编或重复pure，freshgrant前不运行。
- 剩余两top、产品独立静审/真实PG补集、最终README未完成；当前只是作者首top，不称真Skills/Object初始化或整卡通过。

## 作者第二个真实PG完整终态（2026-10-09）

- root已将首Facts记录eac8ac15实际push；随后freshgrant本人执行同一命令，仅`--run '^TestProjectInitializationAuditTransactionBoundary$'`，session45958 actualouter0。
- Go2.63s/9子例PASS；Go545940 Wait0、driver544736 Wait0/13.985747582s，无adopted。完整73.381s；container4890f13543e16171cd8eae30504955b54c4aee3ee9c09705a6dd526526de49a3与network0dd6acb6a4fd7fa23bb7768f538361eedfa91560c8f8bca60c9d06258e8c85d9两次exact absent，desc2空、私密退休（仅owned.json）、TCP2空、inputs unchanged。安全原件`pg/pg-1353338f9d774bc4bb30861b685d7424.log`；全局窗口已先交root。
- 实际门槛包括无锁/SH/错Project锁、foreign/ended Tx、取消/Store关闭拒绝；同Project另连接SH/EX实际阻塞，wrapper返回不释放caller EX，caller释放后waiter完成。没有新源/输入变化，无需重编或重复pure。
- 下一精确selector只剩`^TestProjectInitializationAuditDelegation$`，其余driver/binary/supervisor/原105+15、123+3、75TCP、5GiB、1PG+1network保持；等root freshgrant才启动。产品独立静审/真实PG补集、最终README仍未完成，作者两top不等于本卡或生产发布接受。

## 作者三top完成与产品静审（2026-10-09）

- root已将第二top记录3779b7b2实际push；freshgrant后同一命令仅换`--run '^TestProjectInitializationAuditDelegation$'`，session76352 actualouter0。
- Go1.84s/7子例PASS；Go551929 Wait0、driver551311 Wait0/12.050801142s，无adopted；完整72.290s。两个owned ID双absent、desc2空、私密退休（仅owned.json）、TCP2空、inputs unchanged；安全原件`pg/pg-3fa9d16ab8ab4476bf120ca4d0c94b92.log`。已先向root释放全局窗。
- 三作者top合计42子例，含同Tx provider实际SQL错误/错误传播、真实Object缺private witness拒绝、caller rollback、普通Unbound；不是真实Skill组合发布或独立验收。
- model_delivery本人产品四Go源只读独审已有限接受，无mustfix。其自主不同输入真实PG补集由root另授新独立源与资源窗，尚未运行，不能用三作者结果代替；四Go源持续freeze，不需重编现有作者binary。
- 本次仅card/current更新并freeze交root保存。独立PG补集接受后再写backend README，未完成前不标整卡完成。

## 独立真实验收与最终冻结（2026-10-09）

- 作者三top记录已5f00a87a实际push；root授权model_delivery唯一新增`tests/project/initialization_audit_independent_test.go`。其原三项在入窗前对齐SPEC§6.3并自行补ended Tx，独立源已7145b676保存；本人未写该源、未代跑、未参与具体实现。
- 独验者41838 actualouter0：`^TestIndependentProjectInitializationAuditBoundaries$`，Go2.41s/4子例PASS；driver12.333152569s/总70.252s，Go574109与driver573305实际Wait0；两ID双absent、desc/TCP2空、runtime仅owned.json、inputs unchanged。安全原件`pg/pg-ec3ecdc1636e47b8b258986b0c760a9d.log`；独验者先交root释放，root已接受。
- 独验覆盖同Tx owner/状态变化重验、真实物理Tx与原context、Unknown Fault引用/cause不改、ended Tx拒绝，所有临时事实rollback。与三作者42子例组合满足本库门槛；生产composite身份不可由constructor证明，缺真实Skill映射/provider的边界保留。
- 正式8路径：`internal/central/project/initialization_audit.go`、`initialization_audit_test.go`；`tests/project/initialization_audit_fixture_test.go`、`initialization_audit_test.go`、`initialization_audit_independent_test.go`；`.agent-state/task-planning-recovery/pg_only_supervisor.py`；本卡`docs/development/work-items/d08-project-initialization-audit.md`；`docs/development/backend/README.md`。已有pg_only_driver.go原样复用，不复制AI流水到README；current仅恢复，不另当产品文档。
- 三文档已更新最终限定事实与重建/4精确selector命令，全部范围停止写入供root正式导入。没有新资源或cache租约，不因文档改动重跑未变产品检查。
