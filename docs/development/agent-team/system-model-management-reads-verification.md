# System Model 管理读口验收记录

结论：**PASS，仅限 [rev1 卡](../work-items/recovery-d09-system-model-management-reads.md)的 13 路径管理读取能力**。主线程已采纳并提交推送 `ecd733711caff5df46e423cadab52b32c34f785e`，实际远端与该提交一致。固定输入为已验业务 `c54f73f3324caa11608d84e5d207141985eb6074` 加 [input05 清单](evidence/system-model-management-reads-verification/author/input05.json)，清单 SHA-256 `0208b2dcdce8623f65f2d7c2364a3daf083994f6271aeb63c02ca6fba7915a27`；作者与独立负责人末检均确认 13 源逐 SHA 匹配。

管理员现在可通过 GET/HEAD 读取当前 System Model Credential 的安全 metadata，以及删除 Model 前的精确、有限引用聚合。读取在真实当前 Session/admin 授权、完整同事务锁与确认提交后返回；失败、Unknown 或提交后取消均不发布候选数据。Secret.Metadata 保持 Project Owner/Read gate 兼容；预览不授予删除权限，后续 Delete 仍重新核当前引用与替换事实。

## 固定输入与检查

作者 `management_reads`，独立负责人 `recovery_documentation` 未参与实现。独立审查了全部 13 路径、同 Store/Tx 权限与锁、references EX 的必要性、平台 SQL LIMIT5、目标 SQL LIMIT10001 后聚合、闭合 DTO/decimal strings、预算及错误发布。15 个关键依赖与 c54 相同；旧 OpenAPI 操作未改，139 个引用解析通过。未消费并行 ledger、00018 或 web 候选。

| 检查 | 实际结果与复用边界 |
| --- | --- |
| 作者纯检查 | input02 的 Model/Secret 包树 unit/race/vet 均 exit0；前 9 路径此后未变。input03 的 Model/Project/Security integration 编译与 Central/Runner 构建通过；后续只补受影响 Model 编译，input05 编译 exit0。 |
| 作者真实 real01 | 固定 input04，17 个确切顶层中 16 PASS / 1 FAIL；100 个命名子例 PASS、3 FAIL。四个其他新顶层及十二个旧回归通过；预算顶层的六个 Unknown/实际 NotCommitted/提交后取消子例通过。 |
| 作者真实 real02 | 固定 input05，仅重跑修正后的预算顶层，1 顶层/13 子例全 PASS、无 skip，driver exit0。卡内 5 新 + 12 旧的覆盖由 real01 的 16 个通过顶层与本轮组合，不称同输入一次 17 顶层全绿。 |
| 独立真实 A/B | 固定 input05 加私有 probe，离线 race integration 编译通过；原脚本 race/count1/6m 下 2 顶层/3 子例全 PASS，无 skip，tests/model 13.634s，driver exit0。其他包 no-tests-to-run 不计通过。 |

原始命令、环境、退出码、日志、输入与资源记录见[轻量证据入口](evidence/system-model-management-reads-verification/README.md)。真实 fixture 观察 PostgreSQL 17.8 / vector 0.8.1；PostgreSQL 16.12 仅为原不支持版本 fixture，不作正向目标。

## 原失败与修正

input01 纯测试原红保留：一处用 reflect.DeepEqual 比较包含闭包的 LockKey，一处第二次调用复用计数敏感的测试 fixture。input02 改为 canonical key/mode/长度比较，并为第二个场景新建 fixture；没有改生产或放宽权限、锁断言。

real01 的三个预算红例原样保留。固定 fixture 的 `LOCK_TIMEOUT="1s"` 早于 3 秒业务上限，原测试错误要求至少等待 2 秒。原日志仅证约 1.03 秒的 InternalError/HTTP500，不回填为当时已完成完整 SQLSTATE 断言。input05 只改新边界测试文件，保留实际 pg_locks 等待与 InternalError/NotCommitted → DATABASE_LOCK_FAILED → SQLSTATE 55P03，并增加真实授权/SQL 后的 3 秒 context 到期证明：原 caller 仍存活，最终零 DTO、实际 NotCommitted；HTTP 按 `account.session` 语义 cause 证明预算覆盖认证，100 ms 更早 caller 继续有效。生产和 fixture 预算均未改变。

## 独立关键行为与资源交还

- A：HTTP 预授权后正式 Logout 持久撤销；Secret 在同 Store/Tx、完整 User/Credential SH 内重新核真实当前身份，metadata SQL=0。另两例实际读取 version=2、底层 Committed 后分别投影 Unknown 或取消 caller，HTTP503、零 DTO、单次读取，nonce 和业务事实不变。Unknown 是正式 Store 结果装饰，不是物理网络故障。
- B：count=0 预览后正式 selector 添加 required 引用，Model.version 未变；原版本无 replacement 删除依据当前事实拒绝，业务事实未变；合法 replacement 删除后 selector/引用收敛，目标与其 impact 均 404。

作者两轮与独立一轮各 4 容器/3 网络，均记录存活 exact ID/nonce 标签并完成两次 exact-ID absent；原 2 容器/4 网络 ID/name/labels 不变，所属进程 0、runtime 空、无剩余新资源或监视错误。独立窗口 `management-independent-01` 已实际交还；验收写入与命令已停止。

本结果不涵盖管理 UI、实际 Agent/project_summary reference adapter、Provider 调用或完整 D09；未新增中央进程/浏览器验收。Summary 待决、Object 原中断任务停止与 Artifact/Project 阻塞均不变；没有恢复网络/ROLLBACK 探针。
