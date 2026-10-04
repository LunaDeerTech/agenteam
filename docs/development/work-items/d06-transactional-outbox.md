# D06 Transactional Outbox 与事件投递

- 修订：1；状态：实现中；唯一活动模块D06，台账AT-0013。
- 基线：`main@6b2ca24`，D05完整验收后工作区干净。D01–D05前置均通过；D05证据及未来绑定限制见[主卡](d05-object-storage-artifact.md)。31个本地提交因既有GitHub认证失效待推，不重复请求或冒称已同步。
- 目标：同业务Tx的typed事件写入、持久handler注册与投递、幂等/顺序、失败退避/dead-letter/显式重投、重启恢复与Central生命周期；事件不作为业务Source of Truth。
- 依据：[开发计划](../development-plan.md#d06-outbox-与事件投递)、[事件架构](../../architecture/platform-infrastructure/internal-domain-events.md)、[D01事实与实时](d01-contracts/runtime-events.md)、[生命周期](d01-contracts/domain-lifecycle.md)、[基础Tx/锁/幂等](d01-contracts/foundation.md)、[W40](d01-contracts/walkthroughs.md)。已确认规则不重复产品提问。

## 所有权与实施门槛

| 任务 | 角色 | 独占范围与资源 | 状态 |
| --- | --- | --- | --- |
| S01 完整工程规格 | architecture_worker | 新增d06-transactional-outbox-design.md；已验源码/架构/契约只读；不使用Docker | rev2独立静态通过，冻结 |
| B01–Bn 完整结果实现 | backend_worker | 待S01确定新包/00008与最小旧文件增量后root移交；届时独占Docker | B01已授权，B02待B01验收 |
| V01 独立规格与业务验证 | verification_worker | 只读停写输入/独立副本，验证阶段独占owned PG/MinIO fixture | S01复验通过，B01待实现冻结 |

root独占本卡、计划和台账，其他文档仅按授权移交。遵循AGENTS及agenteam-design/go-development/verification/documentation技能，禁止子agent再委派及任何Git写操作。不提前D07+，不在S01实施源码。设计冻结并经独立审查/root采纳才开工。

## S01具体交付

核实际D03 Store/Tx/锁顺序、D04 identity/Audit、D05后Central启动/健康/停机和迁移/fixture约束，制定可直接实施的Go端口、私有数据/SQL约束、状态机、错误/幂等/unknown及真实验收。建议独立event/outbox契约与实现，正式包名由核对依赖后确定；不得创建万能共享包。00008为下一个全局Up-only迁移，00001–00007/go.mod/go.sum冻结；必要共享修改逐项列现有文件、增量、兼容和单作者顺序，不擅自扩大旧API。

必须落实D01注册屏障：Append分配内部sequence前取得shared直到commit；注册exclusive持久handler/已提交边界，不在注册事务读取业务表、不漏晚提交。组合必须兼容D03一次完整AcquireAll与全局锁序，不能在调用者已持高阶锁后补低阶注册屏障。稳定handler ID、typed schema解码/未知schema失败、独立delivery结果、同aggregate version/sequence及同version sibling event不可误去重、handler失败不阻断他handler均有明确规则。

handler本域副作用与processed marker同Tx原子；不能宣称外部副作用exactly-once。定死执行claim/fence/lease的真实终局和重启语义、重试上限/退避/有界并发、unknown确认及取消/迟到handler防复活，运行时错误只保存稳定安全诊断不泄露payload。显式重投失败delivery需当前正式授权/稳定命令/Audit（如适用），缺provider拒绝，不新增匿名运维route。没有生产handler时可安全初始化机制但不造业务成功消费者。

晚注册投影从canonical rebuild，D06负责注册屏障/新事件delivery责任，D24/D25负责generation/dirty/rebuild原子切换；把具体接口与责任写清，不提供全历史replay框架。Outbox/delivery无自动TTL；Project显式永久删除须正式生命周期端口、相同Project gate/锁保护、活动handler真实停止/收敛、最终项目payload/delivery清零及迟到防重建；保留最小完成receipt，其他Project/System不受影响。归档仅限定Converge事实处理，不恢复业务执行。

Central真实启动、健康/维护、首停drain和共享额外1s force沿已有预算；DB最后关闭，所有本地handler/worker真实join，未知不伪drained。检查同业务事务rollback无event、已提交崩溃重投、重复投递无重复副作用、handler独立重试/失败、注册与晚commit并发、跨版本/同version事件、实际COMMIT unknown、Project删除/权限竞争及重启。复用固定Go1.27.1/local、owned PG17.8/vector0.8.1+PG16.12负例、MinIO源码固定fixture，不能skip必需依赖；普通check-go不启Docker，最终相关真实组及无过滤旧兼容按变更安排。

## 完成与交接

S01提交设计文件指纹、实际依赖输入/检查、未决工程项和停止写入声明。root确认后独立静态审查；产品决定缺失或公共契约矛盾报告root，范围内实现取舍由团队解决。当前仅工程设计，无产品能力或测试完成声明；D06–D28/E01全部仍未完成。

## S01有界可行性核对

backend已完成只读核对并停止，未写仓库、未运行Docker/产品测试。报告`/tmp/agenteam-d06-readonly-feasibility-eaa6gsk5/findings.md` SHA256 `e7e2f9db6f5be069efdcd808e0e11e26f51d02755c6ae33eacd0822209ae0bf7`；28输入manifest `inputs.sha256` SHA256 `9ba48fbe57bed604b17a212fe35b0609a64c032c801dbd3f164bcd0e83a2831d`，root逐项复核全部匹配。结论已交架构纳入规格，属于静态可行性，不是实现授权或验收。

注册SH须纳入producer首次完整锁union；拟增加D03仅检查实际held、不取锁的RequireHeldLocks窄口。handler副作用和processed marker同Tx，失败先回滚再独立记录；COMMIT unknown须取得原writer串行化锁确认终局，不能裸SELECT未命中便重做。拟由app私有assembly复用D05真实ProcessGuard；正常与强制关闭均须保护未join的Outbox handler，不能提前释放共享guard。具体端口、旧文件增量与完整验收以冻结设计和独立审查为准。

S01 rev1设计冻结SHA256 `626cd2c95cf1b0cac42b5855a5c58056766ce584bf41052c1cf12f1f21b7c0aa`，作者52输入manifest `f0d3443a32257b267338c910f3270f5cd8ba8e6ee3b12722e980727838d9de40`经root全部核对一致。独立静态审查`/tmp/agenteam-d06-s01-verify-fh_21nlv/verification-report.json`要求5项窄修：中立event/contract分层、Sequence正int64编码、cursor允许可变limit、删除gate在事件幂等前、archive checkpoint不永久阻断Restore后新准入。56输入manifest `7e3c7e671b9bfa549b4161d41776b523c2bc007a7e1decf8f03310523e965d2e`，证据索引SHA256 `620dff76132d9374d1de569925c5ddb730b9496d937af5f87aca4b3d882477d7`；格式链接通过，其余核心组合未见阻塞，未运行产品测试。V全停且无资源后root采纳修订方向，解冻设计及D01契约README的最小上层包定位给architecture，源码仍冻结。rev2冻结后独立复验才授权B01；无需新增产品决定。

## S01采纳与B01实施授权

设计rev2 SHA256 `4703840c9216a7e9949549478bebfdcc266eab343302aec36e98069401e42f28`与D01索引增量SHA256 `97a8c9fec33fdd28222d5c2c3ee5d44bd1bcc6d2a8517dc44e8aab3ee6cfc093`经root审查采纳。独立复验五项全部闭环，56输入manifest `f6ed906d60fbf0976f30b04064b53ea7c9f8b8c9ce3cd403715e2da996bdca93`全部匹配，52技术依赖未变；报告`/tmp/agenteam-d06-s01-rev2-verify-m2yln6gw/verification-report.json`，证据索引SHA256 `98172d8bdfa3cbc54004bee2fc305406c70ef258e3dee8aec6f2b1ccc733c58e`。所有设计/验证执行者已停，无运行资源；这是规格验收，尚无D06产品测试结果。

root正式采纳设计§1 B01全部新包与精确旧文件增量：backend独占event/contract、outbox/contract及B01 outbox实现/测试、00008 Up-only迁移、tests/outbox、postgres持锁验证/error及测试、identity注册职责及测试、Audit typed闭集/必要服务校验及测试、PG fixture新增包纳入和outbox迁移测试。允许新包内按职责调整拆分，不增加依赖，不更改旧迁移/既有Tx语义；额外旧文件需求先报root。D05核心、app/Runtime、AGENTS和backend操作文档暂冻结，B02再授权。根主卡/计划/台账与设计仍由原所有者持有。

B01目标是可独立构建与验证的持久typed事件、注册屏障、原子Append/delivery/marker组合；不装配半成品dispatcher。backend独占owned Docker/PG/MinIO fixture，自测含普通/race/vet及真实Tx回滚、注册晚提交/重启边界、一次完整锁与D05组合、幂等/当前权限/删除gate、Sequence/schema与Audit迁移兼容。使用固定Go1.27.1/local、nonce/labels/exact ID，只清本次资源。源码与实际依赖冻结、交付SHA/命令/日志/失败修复/清理和停止声明后独立V接管；root不读活动实现。B01独立验收前不进入B02，D06整体仍未完成。

### B01中立事件契约首轮独立验证

backend冻结event/contract四源manifest `210646136cf49fa074a5e8084bf534e9dc376010675bb849bf939213ed7b4565`及18稳定依赖`9969f6d3bdce8e196faa0c133fc98536c3d3b01f8128158b3dfaf62935ccb017`，继续非重叠core。独立副本`/tmp/agenteam-d06-b01-event-verify-qz8d50nt`确认3缺陷：DecodeEvent泄露customcodec原error、Header/Scope大小写别名重复可覆盖、缺required occurred_at静默补零。原probe SHA256 `a7d7a91e83a678fb43617278e72602d92abf8375a3fc33abb738bbca12e06ae0`，失败日志`27783e935a702a5268f7c487d57f2e769820b6e5b8b07860f58f9798bc6dae60`保留；其他6边界race1.106s/vet/格式及foundation-only依赖通过。报告SHA `e678ed272ec1bb284bfdd0ad589d6aa90fed09e5d0ad85c731eda434630b6c48`、证据索引`7739140203f6b535a831b3715ff600a096118faf9e5d80325c5ba756d399bdd4`。V全停且无Docker/socket资源后root仅解冻此4文件返修与测试，18依赖不变，不更改foundation合法年份。原probe须逐字复验再独立确认；该块未验收，活动Outbox/Audit/SQL不在此结论内。

B01中立契约四源窄修独立通过：source manifest `f358d3ae72639bd65be1e7ee3332afbf31dea0b635bb417e7041b06fd1c595f7`，18依赖不变；原a7d7探针逐字通过，新增安全Fault/零结果/嵌套日志、合法year0001/时区/转义精确键/required-null/转义重复均通过。独立完整包12项race1.119s/vet/格式通过，22并集指纹`ff66ba7fb2bde595849c5caba499cddddde5873e87fbcbf07cdcc601d43f128c`，副本`/tmp/agenteam-d06-b01-event-repair-verify-kkwr64tp`，报告SHA `1f46ee85c6ebccc97759faeeb1dbe6ea3b5ab104bd642c85fcf1027ddbbec051`、证据索引`5905f7806b1f2a405d03a98410bd86892e4aecb698a34b7be1b90c32df5dface`、race日志`93db3ffcffd7176f20e81854eb76e8330f87d5b125a59b3d462043d8d8430a2d`。V全部停读/命令且资源0，root审查采纳并精确提交四源；活动Outbox/Audit/D03/迁移仍由backend实施，未纳入验收，不读取或暂存。整个B01/D06仍未完成。

### B01完整冻结与独立验收移交

已验中立契约提交`ea334f9`后，backend完整32源码冻结，manifest `/tmp/agenteam-d06-b01-source.sha256` SHA `88c796b67b1b9be8d3c73b6e40dfaffeb8f7e6606bcae1624813ab304db73152`，314依赖`9cb34d6d7dd8108112ef881fa5152361ec261d3fc71eda2857f3a480b82275a5`，root与V逐项匹配；作者报告SHA `3a48177b310fab00128e8a920c15901ee3302740bd6a52df8521c875e3bca9cb`、证据索引`cc870f30cbcfac78e6c7c05597dc9cc493987a4662182933dd1bb00f670268ae`。实际TestOutbox真实race完整通过(database4.338s/outbox19.880s，日志cf4adb348543495d86e299cffb121536c181f285077e299c48c81f0057118ef8)，含注册SH/EX晚提交、原子事务/marker、真正截获COMMIT响应unknown、D05一次完整union、Audit升级/正式权限和catalog边界。最后CheckStorage关系检查增量在最终check-go之前；check-go日志e0bb795092cc4e9ab0263df7dcb9904c1948919f4eedc5731b13e1cceea568f6通过，最后定向真实race1.778s日志c82575eddbb5af756147b05a52cfdd8a9ee18e7bace310d4cd89028ff956cb3e通过。

保留首轮新增D03测试错误预期日志12594c139a30efc53e893100cb5f4f5ed983ef3bc278699edc122b12f7628036：回调结束已移除token按既有Store返回INVALID_TRANSACTION，测试原误期TRANSACTION_EXPIRED；仅修测试，生产旧语义不变，之后定向及完整组通过。15nonce容器/network全0、5runtime目录不存在，cleanup证据4eb2af959e6d44f12e2555279c4006ae3e01b8df8ad133762d12fc9a22254b18。作者全部停读写/命令后Docker交V，独立副本`/tmp/agenteam-d06-b01-verify-i3w7vi21`的346并集39dc0f9960bfbdca6bf93bcb684412e9ac00a7b6304fb522d84b70ad567cb2c4，运行无过滤test-objects兼容和风险探针。root只读审查，requeue_commands指向Audit的FK可能阻塞既定Project清理顺序候选交V确认，尚不宣布缺陷或验收。B02未开始。

### B01独立确认缺陷与窄修授权

V首轮无过滤test-objects exit1，438.56s，日志SHA `9081435af2074726a8663b52f923bebebba8cbac88a9582986776ab8b3b48191`：新Outbox49.150s、database106.999s/process123.228s、objects315.361s及object契约通过；旧app blocked_io在进程退出后观察PG后台连接消失超时，旧SMTP取消返回InternalError。原两个用例保持字节/断言/时限单次定向过（app2.914s/security2.392s，日志2a46bf6bcc92b7156c55c3fdaf18f6d4fdc7511adac65dbf1bf6f73e79b8fe00）。app首轮原因未确证、失败时server状态未采，不能称负载或以定向通过代替最终全量。

独立实际探针确认两项阻塞。其一00008 requeue_commands.audit_id跨域FK使正式Audit.CleanupProject返回SQLSTATE23503/outbox_requeue_audit_fk，Project Audit无法先清、System隔离正确；原probe `/tmp/agenteam-d06-b01-probes-gliu9ui5/tests/security/verify_outbox_audit_cleanup_test.go` SHA `a20bb70e78ac92839577cbdca2886eeb112a1e67f0d252bcadbb7bd3038496e8`，真实日志2f3802326550e49517b74c14a7b754a8c9749fdf7e1678ec9f4562e30d9a94fe。同轮handler内后继事件完整union/漏锁poison和pg_blocking_pids并发Apply单callback/marker均过，outbox5.620s。其二SMTP操作ctx取消先经production stopOnCancel关闭真实TCP，BeginSend的AfterFunc取消传播尚未到发送ctx时，Conn.Read误报InternalError；受控实际Read探针SHA `f9c72b45b912967690b2722c9935911a6cc6a42fa7f7914f3fc94d5d62312460`，日志ff86572d7588c169ec2e5f4b32ffc45675cc478a36d55b13292e5e6d856e9ccd。这是实际TCP+受控传播窗口，不冒称完整Dial/BeginSend集成；原真实SMTP取消失败与之相符。

V报告`/tmp/agenteam-d06-b01-verify-i3w7vi21/verification-report.md` SHA `be720df55e7b1ac2355124a984c0e08046d24c23054a37739ba10c13a812e75e`，31项证据索引`1d1adbdf751fd53731ef168cca2cb3194277a608b62270c55ec80f46914e2508`；346输入repo/full/probe三份末次一致，9nonce容器/network、3runtime、PID全0（资源证据93d88639d67c6619a70b5bb2e9e16ba782754af8bab6f6202715331ac4ab9527）。V全部停读/命令后，root正式解冻00008仅去跨域删除依赖、保AuditID和同Tx权限事实，以及旧outbound/smtp.go仅修取消错误映射；允许相应新增/最小回归测试。其他生产源、D03/app预算均冻结，不为未确证旧app失败改实现/断言。Docker交backend窄修，必须原两probe逐字复验、check-go及受影响真实Audit/迁移/SMTP通过；最终V无过滤兼容和原probe复验再决定验收，B02未开始。

两项窄修最终35源冻结，source `/tmp/agenteam-d06-b01-repaired-source.sha256` SHA `1f2bc54d5edb05b397466a043e2f0e7670033b3213c6c1344a94aad2c1a4de60`，313依赖`b03197a624d9825244021437ccad71fd22156e39beeff9c064de779ab2d41fc4`。4增量manifest `1c838fc2364c753102917f56f54a0438c2f7ecbacd0c8eae97172ae39b322443`仅00008、smtp.go和两新回归；其余原输入字节不变，root核348全部匹配。作者两原probe逐字通过、真实database3.157s/security20.361s/outbox3.722s（日志5bfdd7f0d576a3554728df4b91f70ff3ca5c61f63aed4828f5e8e12c61afaea4）、check-go（25d5bc5ba4e85cbcd79f1f510ff3839549a85d96d707190d7b7a87de27083461）过。报告849e81bc0f41c1e3418715f7565326fb00f53e14662ded0dc3bedc4655aa4e64、索引ff888f2d39f517f773894acd4ec87ecacc3ad1d8c93038fab2e33a10b78f1503，3nonce资源0并全部停后交V。

最终V副本`/tmp/agenteam-d06-b01-repair-verify-8enfytwx`核348输入；原SMTP探针与受影响race5.575s通过（ba124d184ae39e35142ebae1e0bf385474454d8370f7d0a5d6e60133589faf60），无过滤总组运行于`/tmp/agenteam-d06-b01-repair-run-a60s25md`，原Audit探针逐字纳入。仅/tmp原app测试失败路径加只读server诊断（diff781e74a340982b652acb7d40fee2aa97a05b015d6d5629ff7b31e5a60cd03771），正常路径无诊断SQL/等待，原断言/时限不变；不冒称整份app测试源逐字相同。源码冻结，最终兼容尚待完成。

### 剩余阻塞：既有PostgreSQL强停取消目标竞态

最终独立总组391.541s exit1，唯一失败仍为原app blocked_io（app104.801s）；database77.226/process111.280/security117.194/outbox36.938/objects285.541s等全过，原Audit探针0.99s、SMTP cancel1.02s及局部race通过，前两返修已闭环。全日志SHA `5ffe1a662956f854922202eef82a7a294741cf1792e2a4464716d9ee9739f5c0`；V报告`/tmp/agenteam-d06-b01-repair-verify-8enfytwx/revalidation-report.md`及30项索引`9a1ed8b79652ef95e8d07cbd13f8406b0516f479024aff25ec99dbdfd57af219`。3nonce资源全0、348输入匹配、V全部停。失败诊断确认进程已按原强停预算退出、未宣称COMMIT、表事实0，但PG exact owned backend仍active/PgSleep20.249s、无blocker；片段SHA `f1089e38552a06a8925b98fcc206b68b93685709cf3958de0a0a0002c77f9f06`，不再作为仅未采样偶发记录。

backend有界只读诊断已停，报告`/tmp/agenteam-d06-force-cancel-readonly-diagnosis.md` SHA `a9bb4ca81cc96e94eb74f7e1483d7a5756691cf4629823d6b6eea93060629b02`，源码/pgx/失败片段索引`4071e3824ac468846836c6f7125f8f9def15f4212417dfe0d30da99312822afc`。源码证明可达窗口：HTTP先取消、默认watcher使SQL先返回而asyncClose后台取消尚未完成、operation.release先移除、Store ForceClose仅快照operations并封闭同DialFunc，可能遗漏/截断取消。pgx没有closed状态早退，已排除该猜测；CleanupDone/CancelRequest nil均非服务端终局证明。没有PID263具体取消调用/返回或DBforce剩余预算记录，不能声称已确定该次单支路径。

root保持源码冻结，授权architecture仅新增`d06-postgres-cancellation-repair.md`，比较最小取消所有权协调方案和既有migration joined-cancellation先例，明确有界容量/退役/真实join/原预算耗尽限制及确定性真实红测试；不使用Docker、不实现代码。原app断言/时限、shared额外1s/取消100ms、DB最后实际发起和Tx提交语义保持；网络不可达或预算已尽不能冒称server停止。修复规格冻结并独立采纳后才解冻最小源码给backend。当前是D06/B01兼容门槛中的上游必要返修，B01整体仍阻塞，B02未开始，无新增产品待定。

### PostgreSQL取消修复规格采纳与实施授权

[取消修复规格](d06-postgres-cancellation-repair.md)修订2独立静态通过，SHA `ae1761dd12b5bca976dd1128351cb917be21875528349eeab9f639ff8e47a31a`。rev1唯一必修为Rows正常cleanup已先取消op.ctx，不能据此判用户取消；rev2保存原始caller来源，独立识别force/work，并补正常Query/QueryRow/Tx复用验收。原27依赖未变；独立报告`/tmp/agenteam-d06-cancel-design-rev2-verify-ku1h_yga/static-recheck.md`，索引SHA `884958a4feb7f442586faa384eebb072ee0cfe0b84c4ac36cfc4fb7b0929722d`。作者/V全停且资源0，root审查差异并采纳；仅规格通过，原PgSleep残留未关闭。

root授权backend独占`internal/central/postgres/store.go`及新增`pool_cancellation.go`、`pool_cancellation_test.go`、`tests/database/cancellation_force_test.go`和必要同目录专用测试helper；Docker顺序移交backend。先在冻结旧实现独立副本跑真实确定性红probe，再同断言验证修复；完成定向真实取消/复用/预算/原app与check-go后冻结。其他B01源码、sql.go/transaction.go/migrate.go、app及原测试、迁移和依赖继续冻结，超范围先报告。最终由V执行原probe及无过滤test-objects；B01整体通过前B02不开始。本地提交继续，既有GitHub认证阻塞未解除，不声称推送。
