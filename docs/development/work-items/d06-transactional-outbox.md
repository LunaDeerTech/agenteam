# D06 Transactional Outbox 与事件投递

- 修订：1；状态：设计中；唯一活动模块D06，台账AT-0013。
- 基线：`main@6b2ca24`，D05完整验收后工作区干净。D01–D05前置均通过；D05证据及未来绑定限制见[主卡](d05-object-storage-artifact.md)。31个本地提交因既有GitHub认证失效待推，不重复请求或冒称已同步。
- 目标：同业务Tx的typed事件写入、持久handler注册与投递、幂等/顺序、失败退避/dead-letter/显式重投、重启恢复与Central生命周期；事件不作为业务Source of Truth。
- 依据：[开发计划](../development-plan.md#d06-outbox-与事件投递)、[事件架构](../../architecture/platform-infrastructure/internal-domain-events.md)、[D01事实与实时](d01-contracts/runtime-events.md)、[生命周期](d01-contracts/domain-lifecycle.md)、[基础Tx/锁/幂等](d01-contracts/foundation.md)、[W40](d01-contracts/walkthroughs.md)。已确认规则不重复产品提问。

## 所有权与实施门槛

| 任务 | 角色 | 独占范围与资源 | 状态 |
| --- | --- | --- | --- |
| S01 完整工程规格 | architecture_worker | 新增d06-transactional-outbox-design.md；已验源码/架构/契约只读；不使用Docker | 已下发，设计中 |
| B01–Bn 完整结果实现 | backend_worker | 待S01确定新包/00008与最小旧文件增量后root移交；届时独占Docker | 待规格独立采纳 |
| V01 独立规格与业务验证 | verification_worker | 只读停写输入/独立副本，验证阶段独占owned PG/MinIO fixture | 待设计冻结 |

root独占本卡、计划和台账，其他文档仅按授权移交。遵循AGENTS及agenteam-design/go-development/verification/documentation技能，禁止子agent再委派及任何Git写操作。不提前D07+，不在S01实施源码。设计冻结并经独立审查/root采纳才开工。

## S01具体交付

核实际D03 Store/Tx/锁顺序、D04 identity/Audit、D05后Central启动/健康/停机和迁移/fixture约束，制定可直接实施的Go端口、私有数据/SQL约束、状态机、错误/幂等/unknown及真实验收。建议独立event/outbox契约与实现，正式包名由核对依赖后确定；不得创建万能共享包。00008为下一个全局Up-only迁移，00001–00007/go.mod/go.sum冻结；必要共享修改逐项列现有文件、增量、兼容和单作者顺序，不擅自扩大旧API。

必须落实D01注册屏障：Append分配内部sequence前取得shared直到commit；注册exclusive持久handler/已提交边界，不同Tx读取业务表、不漏晚提交。组合必须兼容D03一次完整AcquireAll与全局锁序，不能在调用者已持高阶锁后补低阶注册屏障。稳定handler ID、typed schema解码/未知schema失败、独立delivery结果、同aggregate version/sequence及同version sibling event不可误去重、handler失败不阻断他handler均有明确规则。

handler本域副作用与processed marker同Tx原子；不能宣称外部副作用exactly-once。定死执行claim/fence/lease的真实终局和重启语义、重试上限/退避/有界并发、unknown确认及取消/迟到handler防复活，运行时错误只保存稳定安全诊断不泄露payload。显式重投失败delivery需当前正式授权/稳定命令/Audit（如适用），缺provider拒绝，不新增匿名运维route。没有生产handler时可安全初始化机制但不造业务成功消费者。

晚注册投影从canonical rebuild，D06负责注册屏障/新事件delivery责任，D24/D25负责generation/dirty/rebuild原子切换；把具体接口与责任写清，不提供全历史replay框架。Outbox/delivery无自动TTL；Project显式永久删除须正式生命周期端口、相同Project gate/锁保护、活动handler真实停止/收敛、最终项目payload/delivery清零及迟到防重建；保留最小完成receipt，其他Project/System不受影响。归档仅限定Converge事实处理，不恢复业务执行。

Central真实启动、健康/维护、首停drain和共享额外1s force沿已有预算；DB最后关闭，所有本地handler/worker真实join，未知不伪drained。检查同业务事务rollback无event、已提交崩溃重投、重复投递无重复副作用、handler独立重试/失败、注册与晚commit并发、跨版本/同version事件、实际COMMIT unknown、Project删除/权限竞争及重启。复用固定Go1.27.1/local、owned PG17.8/vector0.8.1+PG16.12负例、MinIO源码固定fixture，不能skip必需依赖；普通check-go不启Docker，最终相关真实组及无过滤旧兼容按变更安排。

## 完成与交接

S01提交设计文件指纹、实际依赖输入/检查、未决工程项和停止写入声明。root确认后独立静态审查；产品决定缺失或公共契约矛盾报告root，范围内实现取舍由团队解决。当前仅工程设计，无产品能力或测试完成声明；D06–D28/E01全部仍未完成。
