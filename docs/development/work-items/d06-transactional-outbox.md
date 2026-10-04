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
