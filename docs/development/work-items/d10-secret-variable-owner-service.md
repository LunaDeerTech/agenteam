# D10 Secret Variable Human Owner 库

状态：库级候选的既定实现、有限独审与六top/四组真实矩阵已齐，基线正式main `ce65714a`。Migration9、read6、Atomic/Concurrency10、Recovery5共30节点按各自冻结版本完整PASS，均有原Wait/两资源/私文件/runtime/desc/TCP/input全尾；原read01FAIL保留。Variables对固定生产/SQL/方法/四组入口、Runner对read测试返修有限独审无剩余must-fix。26–29为测试前缀，00030正式交付及main组合仍待有序依赖装配；HTTP/defaultroot不在本库验收中。唯一实施者为本分支 `ai/secret-variable-owner-service`；root拥有Git和共享资源调度。

本工作项落实[Secret Variables rev2](d10-secret-variables-owner.md)中的库级子结果，直接复用[已验D04 producer](d04-secret-variable-storage.md)、A纯合同和e940 Audit严格读合同。业务规则、字段/安全输出、预算与原意图定义以rev2为准；本文只固定本次实施和验收边界，不另造产品契约。

## 1. 完整结果与排除项

实现现有 `SecretCommands`、`SecretQueries` 的Create/Update/Delete、Get/List、安全Lookup；写重放必须实际D04 Match，identity-only Lookup只恢复完整安全receipt。当前Human Owner/Session/Project gate、metadata-only/no-op、显式value覆盖、两域版本分离、删除后历史、新Session恢复、分页generation、完整锁与Unknown跟踪均属于本库。

同一Project普通与Secret变量共享业务ID及名称空间；普通API底层的Get/写/List/容量精确过滤普通type，名称冲突仍跨两type。Secret另有4096活行限额和分页generation。旧ID不复活，显式value无论明文是否相同均真实覆盖；no-op不增version/generation/history/Audit/Event/Activity，但落D04原意图proof与D10 completed。

本次不实现app装配、HTTP handler/defaultroot、UI、真实Agent F1目录/引用适配、MCP/Runner材料消费或Project完整生命周期。删除必须查本域真实权威引用记录及D04真实reference/lease；存在引用拒Busy，未知owner安全失败，不能用空checker模拟未引用。F1引用变更未绑定保持unbound，资源侧完整引用适配后继实施，不借本库宣称正向通过。

## 2. 已有依赖与构造

root从b724e397精确承接D04的16 production、9 pure tests、00029和D04/D10卡；26/27/28仅连续测试依赖。正式main当前仅到25；00029以及本域预留00030均不得越过尚未正式完成的前缀交付。D04全部承接源和前序SQL只读，发现真实缺口交原域修复，不在本树复制旁路。

`projectvariable.NewAuthority(Store)`继续store-only事实提供方，不反持Project/Secret。新增immutable SecretWriteAuthority绑定本Store与真实Project gate，在Project之后、Secret Service之前构造；实现D04 Discover、无IO CheckPlan以及ReceiptRead/NewWrite当前检查。名称及共享空间由最终Owner事务核验，不假称无name输入的authority已经核新名称。当前Session由正式Project authority沿Account端口证明，不接受生产allow/stub。

库构造器一次注入同Store facts、实际D04专用写口、Project gate、Audit、Outbox、已注册Secret event、Activity和Cursor；拒缺口/typed-nil及已知异Store。不引import cycle、setter、locator、公开issuer/token或外域表直接写入。普通服务构造及已验行为保留。

## 3. 持久数据与安全恢复

00030唯一归本线；[必要DDL草案](../../../.agent-state/secret-variable-owner-service/owner-storage.draft.sql)已按root授权以相同正文落正式格式候选`db/migrations/00030_project_secret_variables.sql`，已在本树真实Migrator组执行，但尚未正式交付。扩现variables的type和互斥payload约束，活Secret一对一Credential映射，保跨type活name unique与不可变身份。Secret行只存安全metadata/内部Ref及独立Credential version，不存材料或密文。

新增completed-only secret_commands、Secret history/generation和本域引用记录；不得复用普通request.value/semantic_digest/receipt.value，也不持久Secret planned。D04受保护receipt提供原语义比较，D10只保存恢复所必需的安全身份/presence和安全结果。外部Variable expected与内部Credential version不能合并。

真实history先于D10 Audit，完整command在实际AuditID返回后才写；history若引用新command，约束须在同Tx延迟至commit检查，不为FK插入不完整completed或跨Tx计划。无跨域canonical FK/CASCADE，原D04 receipt历史独立保留。

Unknown保原CommitResult/Attempt/Cause，不自动重放callback；最多一次原3s、独立当前授权的确认，只有D10完整completed与D04原安全事实闭合才返回成功。无行/超时/撤权不能推断NotCommitted。Stop取消admission和原调用/确认，Drain等实际调用/Rows/Tx回收，不关闭共享Secret/Store。

## 4. 单最终事务与私有证明

短只读发现→事务外D04 Prepare与非no-op Outbox plan→final一次完整union AcquireAll。当前权限及两域前像/版本重验后，固定顺序为D04真实Apply及适用native Audit→D10 canonical/映射/version/generation/history→本域sameTx私有mutation witness及真实D10 Audit→含实际AuditID的completed→Outbox NewFact/Activity→commit。nonce合法预留为既定例外；其余任一步失败均同一业务事务回滚，Unknown不伪称回滚。

两种私有witness分开：prepare discovery绑定原Actor/Session/identity/前像/exact event/D04 Preparation原receiptID+Ref与完整锁；mutation witness只在真实D04 Apply和D10后像/history写入后签发，绑定sameStore/liveTx/私有issuer/Actor-Session/cause/operation/version/exact Entry-Key/该次实际D04 observation。公开安全Observation构造器、prepared或discovery不能自签mutation事实。Audit checker重读本Tx后像/history，Outbox NewFact仍重读完整completed/实际AuditID及D04事实。no-op/replay不签新mutation witness，不发新Audit/Event。

从e940只读兼容进入本次写事实阶段，Project新增精确Secret路由；既有“读形状不能授权写事实”负控随此契约演进改用实际D10 facts provider拒缺私有witness，并保当前Session/生命周期门先行。普通action/event predicates未扩大，三处分发源逆移除新增行后逐字同正式基线；本变化不授权HTTP/defaultroot装配。

并发相同key已由他方完成且原create Ref不同，退出final Tx后至多一次重新发现/准备；不可锁内补锁、重base expected或自动重放Unknown。完整锁至少原command EX/User EX/Project EX/write-key SH/Credential EX及Outbox所需registry/event/全部引用锁。

## 5. 唯一写域

- 本域新增 `internal/central/projectvariable/secret_*.go` 与pure测试、`tests/projectvariable/secret_*.go`真实组合；现projectvariable authority/repository/reader/commands/service/events仅必要兼容，同一作者持有。A公开合同保持，确需变更先报依赖。
- 仅两个Project共享例外：`audit_facts.go`、`projectvariable_event_authority.go`先分发精确新Secret action/event到新增helper/test；旧普通predicate/Knowledge/Object/lifecycle分支字节及语义保留。现`project/events.go`producer分发足够，不改该文件。
- 本域00030草案/后续正式migration、本文和本树current。D04生产/00029与26–28只读；app、HTTP、defaultroot、Outbox engine、Audit公开API/reader及其它共享源不写。

## 6. 有限验收

不重复D04已过十格/nonce/rotation全矩阵。本库以五组新增风险闭合，现有源码使用正式库装配及真实Account/Project门，Project创建仍复用明确披露的persistent Skills fixture，不冒Skills完整初始化验收：

1. 真实Account/Project gate+D04/Audit/Outbox的CRUD、metadata-only/no-op/显式覆盖、分页、删除后历史、同User新Session与库重建；无明文读、输出/普通DB/Audit/Event/诊断均无canary或可枚举摘要。
2. 当前撤权、非Owner管理员/跨Project/type与prepare后Archive先后；历史不旁路当前gate，完整锁及前像漂移按原规则拒绝。
3. 同finalTx各独立事实失败的实际回滚；witness错Store/Tx/Actor/Entry/cause/只有prepared等拒绝；Audit实际ID之后才complete，Outbox要求完整事实。测试证明故障实际到达目标，不以提前失败冒回滚。
4. ordinary与Secret同名、同key同/异义、同expected更新/删除；两个真实Tx PID/精确锁等待与确定屏障，不靠sleep。
5. Owner final COMMIT before/after/pending、原Unknown/一次3s确认及实际Stop/Drain；复用真实COMMIT代理机制，必须实际flight/连接/资源终态，不用outer kill冒join。

上述五组精确top为 `TestSecretVariableOwnerPersistence`、`TestSecretVariableOwnerCurrentAuthority`、`TestSecretVariableOwnerAtomicFacts`、`TestSecretVariableOwnerConcurrency`、`TestSecretVariableOwnerCommitRecovery`；独立迁移top为 `TestSecretVariableOwnerMigration`。全部在`tests/projectvariable/secret_*_test.go`，不改旧普通fixture与旧业务断言。callback或端口故障必须先证真实目标事实到达；并发按实际PID/本DB/排序首冲突锁/blocker判定，COMMIT只截取已到达目标完整事实的原backend帧。

迁移单独核空库、正式普通Variables存量升级/历史保留、重复启动和真实CHECK/FK；不据本树测试前缀通过称26–28正式交付。无真实资源grant时仅源码/小pure/离线编译，普通pure和list不算业务通过。高风险实现及实际方法由未参与者独审。

实际入口沿原PG-only工具按读/权限、原子/并发、COMMIT恢复、迁移四组分窗，完整节点6/10/5/9；原Go6m、105+15、123+3、TCP75和两资源不变，入口控制/固定候选/独审完成后仍须root fresh grant。Activity回滚必须先在原Tx看到真实更新；Stop格要求原writer尚未返回，取消后held-callback的Drain负向由独立受控pure覆盖，不冒该负向已经真实PG验证。

00030首真实组沿固定d52/03bd与402输入，原session66022→6226ae actualouter0/85.988s，单top9节点PASS15.50s：空库/repeat、普通00029存量和原receipt升级保留、升级后真实Secret写入、五CHECK及deferred history FK实际回滚。Go/driver Wait0、两个原ID双退役、私文件/desc/TCP双尾和输入不变均齐；精确日志及身份见current。该结果不替代其余五业务top、全Owner或26–29正式交付。

Read首真实组沿同固定产物/402输入，42579→f4ef34 actualouter1/80.220s完整FAIL：两父四子共6节点，当前Owner/跨Project、失权和撤销Session三子PASS；归档后的原材料重放报DEPENDENCY_UNAVAILABLE，Persistence历史安全Lookup报IDEMPOTENCY_KEY_REUSED。原Go/driver Wait1、PG两ID双退役、private/runtime/desc/TCP与input完整尾均齐，窗口已释放。只读确认测试no-op的expected指针后来被改为3；归档fixture双clock_timestamp可能违反正式时间顺序但本轮未采时间，仍非已证原因。尚无依据修改生产授权/digest或放宽预期；后继仅修明确测试输入并独审，原FAIL和其余两个未跑业务组保持。

返修dfc0de53仅三Secret测试：提前冻结原no-op查询，局部归档fixture使用同statement稳定时间并同Tx重新验真实Project门；旧ordinary/生产/DDL/入口未改，Runner限定静审接受。新完整包race编译95963→ae7f0a actual0，新pure1top2sub d36685 actual0，实际list恰六原top与该pure共7；不代真实SQL。root396f25校验后原子推广9a514c候选（36,744,919B，完整身份见current），退休无旧队列的d52；03bd driver和原日志未变，新403输入/`sql-owner-read-02`尚未执行，read01FAIL与MigrationPASS保持原组合边界。

Read修后同原组69141→d31012完整actualouter0/83.806s，新9a514c/03bd/403输入下六节点全PASS（CurrentAuthority7.45s、Persistence6.60s）：实际当前授权、prepare后Archive、只读生命周期原历史与新写拒绝、CRUD/type名称隔离、删除后新Session/Service恢复及安全输出检查完成。原Go/driver Wait0、PG两ID双退役、private/runtime/desc/TCP与inputs完整尾齐，窗口已释放，详情见current和原read02日志。保留read01FAIL且不回填其未采时间因果；原迁移PASS复用，AtomicFacts/Concurrency和CommitRecovery仍待各自fresh窗口，整库/正式连续迁移交付未完成。

AtomicFacts＋Concurrency原36671→5b10f7完整actualouter0/84.147s，同固定9a514c/03bd/403输入，十节点全PASS（7.61s/5.88s）。四个实际事实边界注错后的精确回滚，以及同key同/异义、同expected更新/删除、ordinary/Secret名称竞争的原backend/锁屏障与调用返回均通过；原Go/driver Wait0、PG两ID双退役、private/runtime/desc/TCP/input完整尾齐，窗口已释放。只剩CommitRecovery五节点真实组待单独授权；旧FAIL/各版本结果保持，不宣称完整Owner或正式前缀交付。

CommitRecovery原59444→190edf完整actualouter0/82.913s，同9a514c/03bd/403输入，五节点全PASS11.96s。实际COMMIT before/after/pending与Stop确认取消，原Unknown/Attempt/Cause、独立当前确认、锁屏障/调用返回、代理释放与新Session历史恢复/事实恰一次均闭合；held-callback取消后Drain负向仍属既有受控pure。原Go/driver Wait0、PG两ID双退役、private/runtime/desc/TCP/input全尾齐并已释放。至此本库既定六top矩阵按版本到齐，原FAIL保持；后继只按正式前缀/main装配差异补必要验收，不新增默认恢复矩阵或冒HTTP/root/F1完成。
