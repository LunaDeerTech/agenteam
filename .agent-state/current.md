# D04 Secret Variable Storage 当前检查点

- 树：`/workspace/agenteam-secret-variable-storage`，分支 `ai/secret-variable-storage`，正式 main 基线 `8cb0a95338dc417ff34be06c00086fbbc7159efa`；root 独占 Git/迁移编号/实际资源授权。
- 任务：落实已接受 D10 rev2 的 D04 专用 producer，保持旧 kind1/2、Model/Account/消费者边界。[工作项](../docs/development/work-items/d04-secret-variable-storage.md) rev2/CheckPlan 已有限独审接受；typed合同、crypto、Prepare/Match/Lookup/Apply、native Secret Audit及维护增量已落代码，纯控制的实际范围见下。真实D10 provider/Owner、正式连续迁移与PG矩阵未完成；root已将00029预留本域，00028由Knowledge统一cleanup索引协调。
- 已读 AGENTS、团队 README、D10 卡、实际 Secret/ProjectVariable/迁移源码及 Go、database、security、verification、test-engineering、design skills。D10 A 的正式契约不等于专用 authority/provider 或 Owner 实现。
- 与 Knowledge 核对：现 store-only 事实 Authority 不反持 Project/Secret；独立专用 authority 在 Project 后、Secret Service 前构造。Knowledge 的新 Audit 树只做严格合同/读端兼容，不写 D04/private witness/Owner facts。
- 端口已固定：contract窄prepared＋本Service私有concrete issuer、无IO CheckPlan和两个当前stage、四producer方法。真实D10 provider必须后继正式绑定；没有late setter/默认允许/生产stub，当前pure中的authority与SQL明确controlled。
- 缓存：复用 `/workspace/agenteam/output/ai/model-ui-recovery/go-build` 的既有独占编译 cache；只读 `/workspace/agenteam/output/ai/model-ui-recovery/go-mod`。本树不另建 GB cache。实际 compile 前与 root 确认无本线 cache 在途，Go1.27.1、离线 env、独有 GOTMPDIR；本轮仅复用该 cache 执行下面两 contract 包的小范围离线 race，已结束。
- 独立 Model AuditAuthority 原 session62616 已实际 outer exit0（ee71a4），完整原尾齐并已释放唯一真实窗口；D04 未占 PG/browser/socket/网络。本树 SPEC/current 是新 recoverable 两路径，未执行动态产品验证。

## 无依赖纯合同片段

- 新增 `internal/central/secret/contract/project_variable.go` 与 `project_variable_test.go`：原 Human/Project/Variable/完整 D10 CommandIdentity/expected 的不可变无材料请求，create/update/delete 字段 presence，独立 owned SecretMaterial、metadata copy、safe fmt/JSON/slog 与拒反序列化。使用实际 D10 A identity factory/create material contract 作兼容控制；不提供 authority/basis/producer，不扩旧 Purpose.Valid。
- Go1.27.1 原离线 env、既有独占 GOCACHE/只读 GoMod、本树独有 GOTMPDIR，`go test -race -count=1 ./internal/central/secret/contract ./internal/central/projectvariable/contract`：session57406 → chunk477472 actual exit0，两包1.029s/1.189s。gofmt27414c、diffcheck415a62实际0；没有真实 PG/browser/socket/网络。
- Runner SPEC 初审提出 prepared contract 载体/producer signature、无 name 输入不能声称 authority 校验新名称、正式表名对齐 D10 `agenteam_secret.project_variable_receipts`。作者接受：name syntax/shared space 归同 final Tx 的 D10 Owner，专用 authority 只验其有输入的当前权限/前像；prepared 拟采用安全非空 interface，由真实 Secret 私有 concrete pointer＋同 Service issuer 严格验真，不导出 any/unwrap/digest。该修订仍待独审收口，卡暂保 rev1 冻结，不把方案算实现。
- 本轮四路径统一 freeze：本文、工作项卡、上述两 Go 源。root checkpoint 后再改必要 SPEC；现有 Service/SQL、旧 kind1/2、Model 验收输入与产物全部未改。

## SPEC rev2 接口闭合待复核

- root已保存四路径92aca721；两Go纯源保持原freeze。本文与卡只修Runner指出的三处SPEC缺口：prepared contract非空接口/四producer与两authority签名及生命周期；新name规则归同final Tx的D10 Owner；表名对齐正式agenteam_secret.project_variable_receipts。
- Prepared必须先精确私有concrete/typed-nil/同Service issuer/共用Destroy状态检查，再调用自有方法；拒外部实现/包装器/跨Service，安全Preparation给原Request/typed receiptID/ref，复制完整locks供D10 Outbox准备；无any/unwrap/digest/回调能力，不误销毁调用者材料。最小锁明确command EX/User EX/Project EX/write-key SH/CredentialRef EX，不增开Tx或补锁。
- rev2仅接口方案修订，待Runner有限delta复核；Service/SQL/provider均仍未实现，无新增动态控制，不重跑原纯组合。卡与本文重新freeze供review/root保存。

## CheckPlan 接缝与 kind3 纯加密片段

- rev2两docs已root保存faca8b33；Runner5bac1f/e52a29有限接受三缺口方案，无remaining must-fix，不含两Intent源代码独审。实施发现Prepare不能二次Discover验issuer（合法create候选可能随机不同），经root备案在唯一authority接口新增CheckPlan(request,plan)：绑定provider私有issuer/原binding无IO验真、不改候选、不授当前权限；最终ReceiptRead/NewWrite原CheckInTx仍必需。该signature窄delta再次交Runner。
- 新加密片段仅 `internal/central/secret/envelope.go`、`storage.go`、`project_variable_envelope_test.go`：kind3仅Project、摘要32/密文48、AAD绑定kind/Project/receipt/payload；scan在int16→byte前闭集拒绝溢出。kind1/2格式保持，尚无新receipt SQL/producer/rotation/Cleanup接线，绝不称完整kind3存储已可用。
- 原6 top受影响pure（3新kind3＋3旧envelope）race42255→f25143 actual0/1.058s；随后实际独立Python cryptography AESGCM固定公开fixture生成kind2/3两向量fe1e6f，已加入同测试文件。新增向量exact top原session68539→b24eff actual0/1.037s；只重跑新向量，不重复已过无变化矩阵。
- 两Intent Go源仍92aca721冻结；Skills已反馈35587/21c8a0本人四top race实际0无mustfix（初744b02 probe误写API属独验setupFAIL，后改正式InProject）；该接受仅92aca721两纯源，不包含本新crypto/plan/producer。没有PG/socket/browser/网络，Go cache保持既有独占，拟DDL尚未落地，不占migration号。

## Plan/Observation 合同与拟 DDL 可恢复片段

- root已保存92aca721（Intent）、faca8b33（rev2）、683bbf1（CheckPlan＋crypto）；前5路径不再列待保存。CheckPlan单签名delta已获Runner46808f有限SPEC接受；Skills的Intent独审probe已在其树c04fbd96保存，不外推producer/权限/PG。
- 新增 `internal/central/secret/contract/project_variable_plan.go`、`project_variable_plan_test.go`：两闭集stage/四producer/Prepared接口、安全observation/preparation、opaque issuer与精确原request（含Session）绑定、最低command/User/Project EX＋write-key SH＋CredentialRef EX锁。历史receipt basis不依赖仍活canonical，返回副本；纯Matches只证明签发和原绑定，不证明当前DB/权限/持锁。两contract包race97245→afbe81 actual0（1.048s/1.178s），f6d75d diffcheck0，尚无真实authority/Service实现。
- `.agent-state/secret-variable-storage/project-variable-storage.draft.sql` 为明确非正式、未编号、未执行/未经Migrator的拟DDL：kind3 Project/48B、新用途Project-only、新D04 safe_id与receipt表/闭集结果/两个唯一约束/Project-id清理索引，无canonical/当前Credential FK及cascade，不扩旧consumer/legacyreceipt。2c1c9b diffcheck0。现树迁移≤24，必须等root整合真实连续前缀；不补空号、不借他域号、不把draft执行冒正式迁移。
- 上述plan两源＋draft＋本文四路径freeze待root保存；下一片段可另新文件推进真实Service代码。正式main后继已交付SecretAudit纯读兼容e94077eb，本树仍8cb起点，尚未包含/验证该新main组合，按后继实际依赖装配，不声称重基已验。

## 私有 prepared、读取阶段与真实加密准备（有限纯结果）

- plan两源/draft/本文已root实际保存1b415158；此前92aca721/faca8b33/683bbf1均已保存，不再列待存。当前待保存技术恰七源：`project_variable_prepared{,_test}.go`、`service.go`、`project_variable_read{,_test}.go`、`project_variable_prepare{,_test}.go`，均在 `internal/central/secret/`。
- 私有prepared：长度/presence版原意图内部摘要，不公开hash；同Service私有concrete/typed-nil/未知interface在任何方法前拒绝，别名共用mutex退休并清自有sealed缓冲。独立Python语义golden10f824；三top race8949→28c88f actual0，后加强活canary安全输出的唯一受影响top46230→60d6fb actual0。该初段直接构造包私有state，只证明守卫/编码/生命周期，未声称native签发。
- 新读取阶段：构造时不可变ProjectVariables authority字段；原caller Tx内CheckPlan→Store.InTx→RequireHeld全锁→当前ReceiptRead→专用receipt与kind3 exact owner查询。Lookup不解密；异Ref需离锁重准备，已观察receipt丢失不可当absence。Match代码在D04内部打开两个kind3 digest常量时间比较，正向Match尚待专门控制。Lookup三top controlled race8082→cdbf1b actual0；末只删测试unused局部，ff3da9 diffcheck0。
- 真实Prepare方法：绑定authority.CheckPlan后才control/nonce/seal，不二次Discover/不换candidate；新值真实kind1 AEAD、意图真实kind3 AEAD；历史已完成重放仅准备摘要比较，metadata-only不封新业务值；失败销毁D04自有候选，不销毁调用者Intent。三top race10260→734ad6 actual0，测试失败也释放mutex的强化仅重跑正向top13922→fa22d0 actual0。nonce Unknown不发布candidate、不复用range或重试。
- Store/authority与已提交nonce-range在这些测试中均明确controlled；实际密码原语/私有Service方法不等于真实D10授权、PG SQL、初始化或Migrator验证。公开Apply、nativeAudit、rotation/Cleanup仍未实现/未接，不称producer完整。所有Go命令已actualWait，无PG/browser/socket/网络或在途编译。
- 本文与上述七技术源共8路径统一freeze供root保存；后继新文件可独立推进，但不混入本片段验收。

## 专用 Apply 与 native Secret Audit（有限纯结果）

- 前8路径已root保存372d1e94；Skills正对此前固定crypto/plan/prepared/read/Prepare独审，本新Apply不混入其范围。root已明确D04唯一预留正式00029，当前仍只保未执行draft，等root装配实际连续25–28前缀，不添加正式迁移、不创建D10未来Owner表。
- 新七技术路径：`secret/contract/project_variable_purpose.go`，以及`secret/`中的`project_variable_metadata.go`、`project_variable_apply{,_test}.go`、`project_variable_audit{,_test}.go`、`project_audit.go`（仅3行私有variant分派）。旧Purpose.Valid/旧loader闭集不变，专用loader反向拒Model等用途。Apply在同caller Tx先ReceiptRead和真实kind3摘要Match，原意图已完成则返回历史结果；未观察才NewWrite、完整原锁、epoch/purpose/内部version/删除引用约束，再实际值/receipt/nativeAudit。none仅专用receipt，无值更新/值Audit；零行、SQL或Audit失败不发布安全结果，没有新增commit/补锁/回调重跑。
- 新私有Audit witness在native写后生成，只含同Store/Tx/原Request/Entry/Key/安全前后像/receipt与payload ID/复制锁，无Service/keyring/prepared/sealed/材料；与旧mutation/resolution互斥。checker重新查专用receipt、kind3 exact owner及实际canonical，外部expected与内部Credential version保持独立。
- 实际Prepare/AEAD/Match/Apply/native Secret checker纯控制：91996→423e6b actual0（首4top，1.046s）；补退休后新Session历史重放与22个native witness负控后15128→4c748a actual0（6top，1.097s）。后一个命令里的ProjectAudit分支未匹配旧名字，未据此声称旧组跑过；随后按实际`^TestSecretProjectAudit`补6旧top，21736→111c8b actual0（1.036s）。Store/authority/Append transport均明确controlled，SQL未由PG解析，事务原子回滚与真实D10权限仍未验；不把内存模型写入失败当真实rollback证据。
- 本文与七技术路径共8freeze供root保存；rotation/Cleanup接线、正式迁移/PG、真实D10 provider和Owner整组仍未完成。Model交付输入继续冻结；本线所有编译/控制均离线，无真实资源占用。

## kind3 轮换归属与有界项目清理（有限纯结果）

- Apply七技术/本文已root保存dccb6fed并保持freeze，交Skills在前段独审后续审。本新增四技术源为`secret/rotation.go`、`cleanup.go`、`project_variable_maintenance{,_test}.go`。kind3按原receipt ID＋exact payload＋Project反查原Credential，保删除后聚合锁；receipt缺失但payload仍在则失败，二者已退役才跳过。实际ApplyRewrap在原一次完整锁后再核反查和payload owner，映射变动不补锁、不CAS，要求离Tx重新准备。原kind1/2分支、100批次/head rescan、canary/最终全payload栅栏均未扩大或旁路。
- Cleanup在原persisted lifecycle gate/Project EX/引用与lease检查之后追加100条专用receipt批次：先查缺失或wrong scope/project/kind/owner payload，异常失败；同Tx exact回执与kind3 payload成对删除；完成判据加新表。原Unknown仍返回Unknown错误/不发布completed，不重跑callback。D10删除后历史归属无需current canonical。
- 新3top/20sub controlled race92922→82e3af actual0（1.028s）；其中调用真实reverse helper/ApplyPreparedRewrap/Cleanup方法并检查SQL闭集与次数，明确不能证明PG执行100/101、Rows扫描或实际commit/rollback。旧PrepareRewrap的真实PG扫描、启动canary/Retire及并发/Unknown完整数据库验证仍待正式00029与真实资源。19e978 diffcheck0。
- 本轮所有纯生产增量到齐后，实际`go test -race -count=1 ./internal/central/secret ./internal/central/secret/contract`全两包5785→713106 actual0（1.219s/1.032s）；没有TestMain/真实资源，本线无在途命令。本文＋四维护技术共5路径freeze供root保存，原已保存片段不再列待存。真实SQL/完整集成未验，不称D04或D27正式完成。

## 连续迁移输入装配（仅测试依赖）

- 维护5路径已root保存e0fb80df，Apply/维护全部Go源freeze给Skills后继独审。此前372d1e94十二技术源已获Skills有限接受，无mustfix；其实际Prepare→Match/退休/错误与第二nonceUnknown四独立top已保存其树9071db4a，不外推本后继Apply/PG/真实D10权限。
- root db4b48实际装入四原源：`00025_knowledge.sql`来自main29dd4298，`00026_runner_control.sql`来自eea4ced0，`00027_skills.sql`来自7cf7a58e，`00028_cleanup_indexes.sql`来自eda849dc；本人d1a577只读核四文件和连续prefix均在。本线不改这四源。28仅测试候选，未PG/EXPLAIN；不存在空占位或绕过Migrator的验证。
- 本文/卡/draft注释已与该实际状态对齐；加上述4 SQL共7路径freeze供root保存。draft DDL语句未改、正式29尚未创建或执行。root授权随后落本域`00029_project_variable_receipts.sql`及有限真实SQL/事务测试准备，真实资源另需fresh grant；真实D10权限与Owner结果仍独立缺口。

## 00029 与 SQL fixture 恢复检查点（WIP，未编译/未实跑）

- 连续依赖/文档7路径已root保存e9fb256f。环境恢复后07440a实际核HEAD与仅两未保存源码：`db/migrations/00029_project_variable_receipts.sql`（66行）、`tests/security/secret_variable_storage_fixture_test.go`（217行）；原Go实现/独审输入保持冻结。保留现有片段继续，不从旧视图覆盖重写。
- 正式29按root授权落为连续迁移输入，实际逐语句比较与原冻结draft一致，仅加事务/Up标头；未执行Migrator/PG，25–28原字节未改。fixture复用实际Migrator/Store/Secret/Audit/native Secret事实checker，D10映射与stage仍明确test-only controlled端口、当前Session/Project取audit_fixture；Discover显式unbound，无生产D10权限或Owner完成结论。
- 新增fixture尚未伴随实际test top，未获得编译/发现/真实测试终态。恢复段只读/文档，无PG/socket/network；没有借旧资源或未知session补成功。上述两源码与本文共3路径WIP freeze供root保存，后继实际test文件可另行推进。
- 只读核实际Foundation入口后发现fixture拼写`NewCommandCause`，按root允许在保存前窄修为正式`NewCommandsCause`；该处已改，仍未编译WIP，不据静态修字称通过。更新后相同3路径重新freeze，无生产Go差异。

## SQL 四 top 离线候选就绪（业务未执行）

- 正式29/fixture/本文已root保存17969f85，前序SQL与生产Go继续冻结给Runner/Skills独审。新增两源`tests/security/secret_variable_storage_test.go`、`secret_variable_storage_maintenance_test.go`：实际D04四effect/删除后历史与新Session重放/KeyReused/current deny；实际Secret Audit拒绝与后置Owner错误的数据库原子回滚；101个kind3、原Credential已删的轮换/无旧key启动/100+1清理；真实safe_id/用途/kind3/结果/两个唯一约束SQLSTATE负控。fixture权限/映射仍明确受控，未实现正式D10 Owner；测试中的后置Owner错误不冒真实Owner调用。
- 本轮只做离线编译/发现：Go1.27.1原离线env、既有独占Model GOCACHE/只读GoMod/private tmp，`go test -race -tags integration -c -o output/ai/secret-variable-storage/secret-variable-storage-sql.test ./tests/security`原session5200→d4760a actual0；开始available6444412928。随后固定二进制`-test.list '^TestSecretVariableStorageSQL(ReplayAndEffects|AtomicAuditAndOwnerRollback|RotationDeletedOwnerAndCleanup|ClosedConstraints)$'` f503e7 actual0，恰4top；没有TestMain/业务执行/PG/socket/网络。
- 候选27,842,774 bytes，SHA256 `a587afca936a5d1b6fa246f039482eaecc89cc61d9cee131a2d1f90a455b6b2e`（e63322，diffcheck0）。实际SQL、正式Migrator、PG Unknown/并发/原子性、四top业务及所有资源尾均未验，不把编译发现当通过；启动仍需root fresh grant和完整owned监督。
- 本文＋两新测试共3路径freeze供root下一安全checkpoint。当前无在途命令；不覆盖原Model产物或授予D04正式交付。

## SQL 负例补强与 PG-only 两窗口入口（待独审/未实跑）

- SQL两测试/本文已root保存fde3ecb5。Skills对后继Apply/维护eaad50fd、Runner对29DDL72115c44均有限接受无mustfix，不外推PG。Skills四SQL top审查发现负控可能在目标之前失败假绿及缺合法kind3错owner_id刺激；已只修`secret_variable_storage_test.go`：5目标均核hook真正到达、精确Fault/失败来源；owner-tail先在原Tx看到canonical+receipt+nativeAudit；Audit before先看到native写，再确认实际SQL篡改成功和真实checker调用；回滚后对照旧payload ID/版本/用途/密文/nonce/wrapped DEK/master/revision逐项保持。未改变生产、29或业务断言预算。
- 真实调用依赖仅PG：fixture→newAuditFixture→NewDatabase/Migrator/Store；Secret/nonce/Audit没有MinIO、outbound或TestMain。采用两个原PG-only窗口：core selector `^TestSecretVariableStorageSQL(ReplayAndEffects|AtomicAuditAndOwnerRollback|ClosedConstraints)$`，维护 `^TestSecretVariableStorageSQLRotationDeletedOwnerAndCleanup$`；分别隔离7个fixture初始化与101 receipt循环。没有实测时长保证，不加预算：Go6m、driver105+15、supervisor123+3、TCP75均保留。
- 入口增量只`.agent-state/task-planning-recovery/pg_only_driver.go`两闭集selector、`pg_only_supervisor.py`对应精确RUN/PASS（core3top＋5 rollback＋9约束=17，maintenance1）、固定二进制及相关fixture/Secret/Audit/PG/全部正式migration source输入前后快照/成员核。旧generic/root流程保持，不新增资源/cleanup/等待阶段；命令不带`--root-chain`。新增可复跑`.agent-state/task-planning-recovery/secret-storage-entry-controls.py`仅offline/no main/proc/socket/Docker。
- 新编译原session76875→8de5f7 actual0：race integration test与普通PG driver均真实Wait0，旧a587候选保留未实跑。`secret-variable-storage-sql-reviewed.test`=27,866,742B SHA256 `b8231f34329d50491f8d35390e6c426fed598b7f393f7e678ca9aab1e43146a6`；`pg-only-driver`=15,395,195B SHA256 `83129026d52195daa057c8e11a9c9dd771b880795029dd9e30313b1931ce2e70`，均在本树`output/ai/secret-variable-storage/`。
- 入口controls首48项9473→a35f46 actual0；补完整逆差异控制时95562→5581ab因控制脚本替换顺序自错FAIL（非driver/runtime失败），修顺序后87772→865b52 actual0共50断言，含两组实际binary精确发现、5错误selector由原driver在stat/mkdir前拒、RUN/PASS漏/重/skip/fail负控及原supervisor AST/driver字节逆差异。368583 diffcheck0。未执行四top业务或PG，COMMIT Unknown/并发等不在此四组闭包。
- Skills后续32793/f30506 actual0/race1.020s实际证明：缺锁会先poison原pg.LockNotHeld，WithinTx优先拒该原错误为InternalError，而非callback包装的DependencyUnavailable。本轮只修missing-lock这一预期并加来源注释；原LockNotHeld cause、零hook/checker均保留。新race编译57950→b929ba actual0（fresh available6057283584），两组精确发现96441→8b4a0e actual0与diffcheck0；当前同路径候选已更新为27,867,046B、SHA256 `742fb20b2309d8948171f37fca18769ce280370b8841da6247f36daf4457380b`，取代上段b823候选，pg-only-driver未变。入口50控制和独审实际poison控制复用，未重复无关矩阵；业务/PG仍未执行。
- 本文＋以上test/driver/supervisor/control共5路径freeze供Skills复核/root保存；原生产/DDL继续freeze。待fresh grant才可原supervisor `--driver <本树>/output/ai/secret-variable-storage/pg-only-driver --binary <本树>/output/ai/secret-variable-storage/secret-variable-storage-sql-reviewed.test --run <上述单组> --output <本树>/output/ai/secret-variable-storage/sql-owned`。当前全部actualWait齐，无自有资源/在途命令，Vars当前独占真实窗。
