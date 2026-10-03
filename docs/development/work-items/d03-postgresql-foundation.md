# D03 PostgreSQL 与全局迁移基础

- 修订：1；状态：B01 已验收，B02 待开始；唯一活动模块 D03；基线 `main@c4e0320`，已推送 origin/main，开工工作区干净。
- 前置：D02 B01/B02 全部验收，见 [D02](d02-engineering-foundation.md)；D01 [基础契约](d01-contracts/foundation.md)与[责任目录](d01-contracts/README.md)已固定。
- 目标：按[计划 D03](../development-plan.md#d03-postgresql-与全局迁移)实现真实 PG/pgvector 连接、全局 Goose 迁移、事务/有序锁基础及可复现隔离数据库验收。只建当前必要 schema，不提前实现领域表或 Outbox/Secret。

## 任务与所有权

| 卡 | 依赖 | 角色 | 独占范围 | 状态 |
| --- | --- | --- | --- | --- |
| S01 实施规格 | D02 完成 | architecture_worker | 新增 `d03-database-design.md`；代码与根规格只读 | 已确认 |
| B01 存储、事务、迁移与集成验证 | S01 主线程确认 | backend_worker | 按实施规格 §2：go.mod/sum、foundation tx/cause/lock 新文件、postgres、db/migrations、tests/database 与专属testsupport、测试脚本/数据库说明 | 已验收 |
| B02 启动集成与诊断/开发工具 | B01 验收提交 | backend_worker | 按实施规格 §2 登记 Central/config/app/logging/脚本/文档必要适配 | 待开始 |
| V01/V02 独立验证 | 对应冻结范围 | verification_worker | 只读代码，使用本任务专属临时数据库/容器和探针；主线程维护规格/台账/计划 | 待开始 |

S01 读 AGENTS、团队流程、agenteam-design 技能、D01 foundation 与 deployment-runtime 的相关章节；实施读 agenteam-go-development，独立验证读 agenteam-verification。子 agent 不再委派，不执行 Git 写操作。不提前修改 B01 已验收类型/HTTP，除非本规格明确移交必要数据库适配文件。

## 已授权工程范围与设计要求

允许引入精确固定的 pgx v5 与 Goose v3 必要依赖，根 go.mod/go.sum 由实施者独占；Go仍1.27.1/local。固定 PostgreSQL 与 pgvector 的实际镜像/版本，禁止靠latest漂移；先核验可取得、兼容、实际运行版本，再记录digest与可复现命令。环境Docker client/server 28.4.0、linux/amd64已读验证可用。只创建本任务具名/标记容器、临时volume或目录及随机loopback端口，禁止访问/清理其他已有服务、容器、数据库或凭据；测试口令仅限临时实例，不能借已有环境DSN。

规格先明确连接/配置/TLS与安全日志、版本/extension要求、读写诊断、migration嵌入/全局编号/锁/事务与非事务失败恢复、上下文取消/关闭和required dependency状态。D03绑定的数据库必须真实校验，其他未实现mandatory能力保持unbound且整体ready=false；数据库配置缺失或连接/迁移失败不能继续伪报已绑定。

按D01固定Tx不透明句柄、WithinTx的committed/not_committed/unknown、TransactionCause与attempt identity、有序Acquire共享/排他和deadlock/rollback语义。明确pgx如何区分已知回滚与提交结果未知，不用进程mutex冒充DB锁或因cancel推断提交未发生。数据库驱动只在adapter暴露；公共Tx与领域层不依赖pgx。

确定小块边界，B01可独立在真实隔离PG验证后提交，B02再接入口配置/诊断/资源关闭。覆盖空库、重复启动、旧库升级、坏迁移、并发迁移、非事务中断、断连/取消/停机与实际schema/read-write验证；确有无法自动安全恢复的情况显式拒绝并给可执行恢复流程，不伪造回滚。数据库/迁移版本对不上须失败且安全输出。

开工时未选依赖/镜像版本，现已在下文及实施规格固定；尚无D03实现验收结论。无用户待定产品问题，工程细节由主线程确认后实施。通过小块即按已有授权提交推送，D03完整验收后才进入D04。

## 环境核验

主线程实际 `docker manifest inspect pgvector/pgvector:0.8.1-pg17-bookworm`、pull固定linux/amd64子manifest `sha256:99a149d3c84cfb0f32d8da7d72737e4643468787220af2223418730f8e9e9cdc` 成功。用本任务随机具名/标记容器、`--network none`、tmpfs数据目录运行，docker exec psql（ON_ERROR_STOP=1）查询实际 PostgreSQL17.8 / server_version_num170008，CREATE EXTENSION vector后为0.8.1；创建vector(3)探针表、写入[1,2,3]并计算同向量距离0通过。finally仅删除该owned容器，未暴露端口、未接触现有服务、无容器/volume残留。此证明镜像与基础扩展可运行，不代替D03 pgx/迁移/事务验收。

S01候选库为pgx/v5 v5.11.0与Goose/v3 v3.28.0，已只读取精确Go proxy metadata；实际API兼容/编译/数据库集成尚待确认。

额外网络探针：第二个任务专属临时容器仅发布随机127.0.0.1端口，宿主socket成功连接并完成PG SSLRequest（响应N，默认未启用TLS）；容器已删除。后续真实pgx/故障代理可通过loopback连接；TLS需单独配证书/服务器并验证，不能复用此结果。

## S01 确认与 B01 开工

主线程已完整审查并确认[实施规格](d03-database-design.md)修订1。S01原始203行文件SHA-256 `ffefabd7efeca79d05b3cdd5ddd2b7c458e721aa682bd87cb70d569257a80cc0`；作者20来源manifest `bce14000accf5a30183f29488b69d27f48553a946e00875de411ef0b7df20003`，4本地链接/2fragment/结构格式检查通过。B01独占实施规格§2范围，按D01–D08场景完成真实DB验收后冻结；B02仍待B01通过提交。

生产版本门禁PG17 minor≥8/vector精确0.8.1，实际只验证镜像17.8；其他patch部署前复测。修复命令只执行编译登记计划，当前正式migration仅事务型，测试fixture可验证真实non_tx恢复，不造生产修复成功stub。事务callback/SQLadapter为受信任代码，不将SQLExecutor当可接受外部SQL的安全sandbox；句柄归属/活跃/并发/Rows/poison边界必须实际实现。当前无用户产品待定或工程环境阻塞。

## V01a 公共类型分段审查（进行中）

B01作者已冻结foundation新增tx/cause/lock各实现与测试共6文件，manifest `/tmp/agenteam-d03-foundation.sha256` SHA-256 `c23dc7fc0bb8f7ffbff388ecb7a1baf7597c4d111396e6273ce89bc1187ae54b`；该包test/vet/race通过，原D02scalar/HTTP未改。独立验证仅此范围，在临时最小module中复核以避开仍在变化的go.mod；postgres/migration/真实DBtests继续作者独占写入，尚未整体验收。

B01反例环境授权：为实际验证错误PG major拒绝，主线程核验并固定 `pgvector/pgvector:0.8.1-pg16-bookworm` 的amd64 digest `sha256:16e62164a405447dca191079a924ee5b8a9dbf04fe53128701ffbea857b37782`，允许测试脚本按同样隔离/nonce/精确清理规则创建第二种反例容器；仅用于拒绝测试，不扩大生产支持。实际PG16 patch版本由运行后记录。

V01a已完成且停读，原6文件manifest末次仍一致；隔离race验证8,192并发Tx、Cause边界/身份/复制/安全投影、59锁key两两排序与7独立hash向量通过。两项实际失败已采纳并移交作者：LockKey私有canonical含业务key，嵌套fmt/slog.TextHandler可绕过安全投影；NotCommittedResult未在构造时复制FieldErrors导致输入别名可改结果。已解冻lock/tx及对应tests集中修复，cause保持不变，修改后重验。真实DB fixture首跑在容器启动阶段失败，未进入迁移验收；作者确认该次owned容器/网络已按nonce清理，正在定位，无通过声明。

V01a返修复核通过：LockKey闭包安全存储、NotCommittedResult构造拷贝已关闭两项阻塞；复用原缺陷探针及排序/hash检查race通过，原D02依赖哈希未变。6文件最终manifest `6d65eb622d0e7d90286382011ab54e3dd2bb3ee9aa435fd75d45e293ee0b8948`，独立临时副本 `/tmp/agenteam-d03-v01a-review-ee4wo44o`；仅公共类型范围通过，真实DB事务仍待验收。fixture启动问题已由作者定位为容器postgres用户读取TLS证书权限，以及internal网络不发布端口，改为任务专属bridge+127.0.0.1端口并修权限后重跑；此前每次失败owned容器/网络清理通过。

真实PG第二轮观察：COMMIT已尝试后收到FATAL应保留unknown，作者修正了错误的测试预期；仅关闭TCP会使客户端返回但不立即唤醒服务端pg_sleep。主线程确认force加入同owned连接有界CancelRequest，取消/close/join共享额外1s总预算，禁止每连接重置或终止其他backend；已同步实施规格。non_tx实际索引中断/Repair再中断及迁移双进程场景作者报告通过，测试helper Wait结果二次消费导致的假残留已定位修复，完整冻结与独立验证仍待后续。

D07真实COMMIT代理揭示固定pgx5.11.0行为：截断提交响应后，驱动NextResult忽略peekMessage EOF，再返回connLockError（SafeToRetry=true）；另一连接已证实数据提交，原SafeToRetry规则会误判。主线程改为COMMIT已尝试后的传输/closed/connLockError全部unknown，仅明确ErrTxCommitRollback或确认idle的服务器回滚证据为not_committed；删除单凭SafeToRetry确定未发送的分支，不引入字节计数/Tracer猜测，也不改第三方。COMMIT调用前明确取消仍not_committed。作者将保留两个真实代理场景及源码定位，修改后重验。

## B01 全量冻结与独立 V01b

作者31文件已正式冻结，manifest `/tmp/agenteam-d03-b01.sha256` SHA-256 `ac1aa25a95b405e76cc65fa080ec5d788f46907a0e865ff01e48e76b570972a0`，另删除db/migrations占位；无运行命令，任务label容器/网络已清理，foundation6文件独立指纹未变。精确Go1.27.1/local、pgx5.11.0/Goose3.28.0。check-go.sh的普通test/vet/race/两bin build通过；最终test-postgres.sh exit0（postgres race1.023s、database16.411s），输出 `/tmp/agenteam-d03-b01-integration.log`，nonce fc0fee6c2391f5f0de172004104acfb7清理通过。PG17.8/170008支持实例、PG16.12/160012拒绝实例，均vector0.8.1可用。

作者实际覆盖全部D01–D08：migration空库/重复/升级/历史漂移/并发持锁进程kill；真实non_tx无效索引/repair再次中断/重跑/严谨只读bool verifier；Tx误用/poison/Rows/序列化/死锁/延迟约束/锁timeout与cancel；两方向COMMIT代理及独立连接事实；TLS/认证/PG来源/权限安全与drain/force。vector0.8.0反例仅隔离catalog版本不匹配，不宣称旧扩展二进制实测。缺fixture的integration明确失败，Runner无Central/pgx/Goose依赖。

脚本中断实测：SIGTERM1.521s、SIGINT1.072s内清理owned资源；修复过早Unix readiness误判，改为最终TCP readiness；修复子Go工具TMPDIR泄漏，临时构建目录归owned fixture清理。V01b现独立复核整批，主线程已读核心生产和脚本/文档，5文档108链接/格式通过；上述仍属作者证据，未宣称B01整体验收或D03完成。

## B01 完成与提交

V01b独立验收通过，无新增阻塞。真实PG/race选定高风险集exit0（database11.225s），覆盖COMMIT unknown、锁释放、Rows并发poison、Serializable/死锁/延迟提交约束、双进程同会话guard及kill、non_tx repair再次中断、journal收敛、TLS/PG来源拒绝。新增独立探针证明：6个owned pg_sleep连接共同force约36.16ms且另一Store仍可用；AcquireAll重复key两种顺序均真正exclusive；服务端已提交且响应被拦时caller取消仍unknown、cause/attempt保留且callback只一次；verifier多结果/尾写/事务控制候选均被固定驱动以42601拒绝，repairing保留且零写入。

独立日志 `/tmp/agenteam-d03-v01b-independent.log`，fixture nonce2b890221af2952af7d2b07356a9911ba的两个容器/网络/临时目录均不存在，当前测试label资源数0。go mod verify、Runner依赖方向通过；末次31文件manifest仍 `ac1aa25a95b405e76cc65fa080ec5d788f46907a0e865ff01e48e76b570972a0`，26Go文件格式及31文件空白通过，db/migrations占位删除正确。作者普通test/vet/build和脚本信号清理证据复用，未重复未变检查。

所有相关作者/验证者均停读写和命令；主线程审查核心代码/脚本/文档与证据通过。B01验收完成，按授权精确提交推送，通过本节Git历史定位。收尾同步两处现状文字，说明已引入pgx/Goose库但Central入口尚未绑定；此类文档不扩大实现范围。B02未开始，D03未整体验收；下一步按D09/D10接入配置/启动/健康/repair CLI与同预算HTTP+DB关闭。
