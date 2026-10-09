# D10 Secret Variable Human Owner 库

状态：实施已授权，基线正式main `ce65714a`；当前只完成承接和范围冻结，尚无本库实现/动态验收。唯一实施者为本分支 `ai/secret-variable-owner-service`；root拥有Git和共享资源调度。

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

00030唯一归本线；先在 `.agent-state/secret-variable-owner-service/` 保存必要DDL草案，待root确认连续输入后落正式migration。扩现variables的type和互斥payload约束，活Secret一对一Credential映射，保跨type活name unique与不可变身份。Secret行只存安全metadata/内部Ref及独立Credential version，不存材料或密文。

新增completed-only secret_commands、Secret history/generation和本域引用记录；不得复用普通request.value/semantic_digest/receipt.value，也不持久Secret planned。D04受保护receipt提供原语义比较，D10只保存恢复所必需的安全身份/presence和安全结果。外部Variable expected与内部Credential version不能合并。

真实history先于D10 Audit，完整command在实际AuditID返回后才写；history若引用新command，约束须在同Tx延迟至commit检查，不为FK插入不完整completed或跨Tx计划。无跨域canonical FK/CASCADE，原D04 receipt历史独立保留。

Unknown保原CommitResult/Attempt/Cause，不自动重放callback；最多一次原3s、独立当前授权的确认，只有D10完整completed与D04原安全事实闭合才返回成功。无行/超时/撤权不能推断NotCommitted。Stop取消admission和原调用/确认，Drain等实际调用/Rows/Tx回收，不关闭共享Secret/Store。

## 4. 单最终事务与私有证明

短只读发现→事务外D04 Prepare与非no-op Outbox plan→final一次完整union AcquireAll。当前权限及两域前像/版本重验后，固定顺序为D04真实Apply及适用native Audit→D10 canonical/映射/version/generation/history→本域sameTx私有mutation witness及真实D10 Audit→含实际AuditID的completed→Outbox NewFact/Activity→commit。nonce合法预留为既定例外；其余任一步失败均同一业务事务回滚，Unknown不伪称回滚。

两种私有witness分开：prepare discovery绑定原Actor/Session/identity/前像/exact event/D04 Preparation原receiptID+Ref与完整锁；mutation witness只在真实D04 Apply和D10后像/history写入后签发，绑定sameStore/liveTx/私有issuer/Actor-Session/cause/operation/version/exact Entry-Key/该次实际D04 observation。公开安全Observation构造器、prepared或discovery不能自签mutation事实。Audit checker重读本Tx后像/history，Outbox NewFact仍重读完整completed/实际AuditID及D04事实。no-op/replay不签新mutation witness，不发新Audit/Event。

并发相同key已由他方完成且原create Ref不同，退出final Tx后至多一次重新发现/准备；不可锁内补锁、重base expected或自动重放Unknown。完整锁至少原command EX/User EX/Project EX/write-key SH/Credential EX及Outbox所需registry/event/全部引用锁。

## 5. 唯一写域

- 本域新增 `internal/central/projectvariable/secret_*.go` 与pure测试、`tests/projectvariable/secret_*.go`真实组合；现projectvariable authority/repository/reader/commands/service/events仅必要兼容，同一作者持有。A公开合同保持，确需变更先报依赖。
- 仅两个Project共享例外：`audit_facts.go`、`projectvariable_event_authority.go`先分发精确新Secret action/event到新增helper/test；旧普通predicate/Knowledge/Object/lifecycle分支字节及语义保留。现`project/events.go`producer分发足够，不改该文件。
- 本域00030草案/后续正式migration、本文和本树current。D04生产/00029与26–28只读；app、HTTP、defaultroot、Outbox engine、Audit公开API/reader及其它共享源不写。

## 6. 有限验收

不重复D04已过十格/nonce/rotation全矩阵。本库以五组新增风险闭合，具体top在实际fixture依赖核定后固定：

1. 真实Account/Project gate+D04/Audit/Outbox的CRUD、metadata-only/no-op/显式覆盖、分页、删除后历史、同User新Session与库重建；无明文读、输出/普通DB/Audit/Event/诊断均无canary或可枚举摘要。
2. 当前撤权、非Owner管理员/跨Project/type与prepare后Archive先后；历史不旁路当前gate，完整锁及前像漂移按原规则拒绝。
3. 同finalTx各独立事实失败的实际回滚；witness错Store/Tx/Actor/Entry/cause/只有prepared等拒绝；Audit实际ID之后才complete，Outbox要求完整事实。测试证明故障实际到达目标，不以提前失败冒回滚。
4. ordinary与Secret同名、同key同/异义、同expected更新/删除；两个真实Tx PID/精确锁等待与确定屏障，不靠sleep。
5. Owner final COMMIT before/after/pending、原Unknown/一次3s确认及实际Stop/Drain；复用真实COMMIT代理机制，必须实际flight/连接/资源终态，不用outer kill冒join。

迁移单独核空库、正式普通Variables存量升级/历史保留、重复启动和真实CHECK/FK；不据本树测试前缀通过称26–28正式交付。无真实资源grant时仅源码/小pure/离线编译，普通pure和list不算业务通过。高风险实现及实际方法由未参与者独审。
