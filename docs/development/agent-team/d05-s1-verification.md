# D05 S1 维护 work schema 独立验收

日期：2026-10-05。结论：**S1 两文件独立验收通过，无未决阻断，可供 S2 消费固定 schema**。验收者未参与实现。主线程已采纳相同两源，提交并推送 `49c6589c3919cad62a4bae2c993b5fd953d36b1e`，且确认远端一致；本报告不宣称 Object 停止能力或 D05/D08 整体完成。

## 固定输入

规格为[恢复卡](../work-items/recovery-d05-object-stop.md) rev2，SHA-256 `962915706bfa49e46aba5c2399360dfdc96fdff96df3e1408da1d9e6ad712137`。独立执行的初检和末检 HEAD 均为 `1f9f2671d0cbf5ad9308ba11fcc4d0945f7d749b`，另加下列冻结候选；后续 S2 与 R3 写入不属于本次输入。

| 文件 | SHA-256 |
| --- | --- |
| `db/migrations/00016_object_project_work_maintenance.sql` | `176ffc2d6bbaa9dbd1d1fb5fe7ea53692cb4d16d066aa82650baa2d49ebaa034` |
| `tests/objects/project_stop_work_migration_test.go` | `8030e403f4d221bcd1527cf17acac2a09ce08fd177fb3c57e630dcf1b622b06b` |

[输入记录](evidence/d05-s1-verification/inputs.json)保留 39 项迁移、卡、helper、fixture 与 module 锁文件指纹，11:52:22 UTC 末检全部一致，已跟踪代码相对独立初检 HEAD 无变化。作者记录基线为 `70006762bd2b24f30ae075b335c17e644b623f3f`；该基线到独立初检 HEAD 已有 15 项提交的 R2 Project Go 改动，不能宣称整个代码树仍等于作者基线。初次过宽的等同检查因此拒绝，尚未启动测试；随后明确采用当前固定 HEAD。原迁移、迁移器和本次 fixture 输入仍逐项匹配。活动 R3 卡已排除。

## 审查与行为证据

静审确认 00016 是连续、仅 Up、事务迁移，只扩 Object work 的 kind CHECK，并增加一个可空、无默认值的 bigint 列、两个 CHECK 和一个部分唯一索引。没有改旧 SQL、embed、Artifact、公共 enum 或运行实现。cleanup CHECK 显式拒绝 NULL；索引覆盖所有 cleanup claim，包括已 joined 行。

新测试的 catalog helper 包含 Object/Artifact work、stops、domain、全部约束、索引和列；正常升级只剔除五项预期变更，失败回滚不剔除任何项。其范围没有沿用旧 helper 对 work/stops 的整体排除。

| 风险 | 独立执行的实际观察 |
| --- | --- |
| fresh 与 populated 15→16 | 两路径通过；原六类 work、stop receipt、Artifact source/target 与 native cleanup 的旧字段保持，新 fence 全 NULL，不补造 work；重复迁移不改变数据或 catalog。 |
| NULL 与维护对象约束 | cleanup 的 NULL、0、负 fence，以及两维护 kind 各自单独缺 object 均拒绝；独立探针还逐项核 SQLSTATE `23514` 与准确 CHECK 名称，排除另一约束掩盖失败。原六类与 verification 非 NULL fence 也逐项拒绝。 |
| claim 身份与唯一性 | 同 worker 的冲突插入、同 resource/fence 的不同 work 拒绝；新 fence 保留旧未 joined 行。独立探针核索引恰有两个指定键，最大正 bigint 可用，已 joined 且不同 project/process 的重复 claim 仍以准确索引名报 `23505`，两个 verification 可与该 cleanup 共存。 |
| 并发唯一 claim | 两真实连接中，后者确实被前者事务阻塞；以 `pg_blocking_pids` 观察阻塞关系后才释放。首事务提交时后者精确报唯一键错误；首事务回滚时后者成功，两种结果均仅保留正确 work 身份。 |
| 原约束与隔离 | safe_id、NOT NULL、admission 对偶、stop 状态/游标、跨 Project revoked FK、Artifact kind/source FK 及独立 source/target join 的正反例通过；旧 catalog 保持。 |
| 同字节失败回滚与重试 | 在全部真实 00016 DDL 后追加缺失函数，精确观察 `42883`；全部 DDL、旧 catalog 和数据回滚，journal 为原 checksum 的 pending，Goose 最大 applied 仍为 15。只补测试函数，复用同一 Source、checksum、Migrator 后成功；第三次仍只应用一次。正常生产 SQL 升级另行通过。 |

## 实际命令与结果

工作目录 `/workspace/agenteam`。Go 1.27.1，`GOTOOLCHAIN=local GOENV=off GOWORK=off GOPROXY=off`，恢复后的 `GOMODCACHE=/workspace/agenteam-dependency-cache/modcache`；自有空 GOCACHE、TMPDIR 和 DOCKER_CONFIG，显式本地 Docker socket。`GOFLAGS` 为 `-mod=readonly -v -p=2 -overlay=<owned-overlay>`。两个 PG 镜像均按固定 digest 校验；真实输出 PG17 `170008`、PG16 `160012`、vector `0.8.1`。MinIO SHA 为 `dc5298474f0bc87a068f0b1135c583bb1278c17c11c512212ed7644a238c89c8`，详见[环境记录](evidence/d05-s1-verification/environment.json)。

独立只运行一次原脚本：

```sh
sh scripts/test-objects.sh -run '^(TestObjectProjectStopWorkMigration(FreshAndPopulatedFifteen|MaintenanceConstraints|PreservesPriorConstraints|AtomicFailureAndRetry)|TestD05S1Independent(ConstraintDiagnostics|ConcurrentClaim))$'
```

原 driver 的 `-tags=integration -race -count=1 -timeout=6m` 保持。四个卡定顶层与两个独立顶层、合计 16 个子例全部通过；objects 包 9.126s，总墙钟 120.593s，exit 0，无 skip、测试失败或 race 报告。其余 18 个包显示 `no tests to run`，不作为这些包的行为通过证据。[完整命令](evidence/d05-s1-verification/command.json)、[原日志](evidence/d05-s1-verification/fixture.log)、[结果](evidence/d05-s1-verification/result.json)已保留；原日志 SHA 为 `e81b7836a9374edb2b35fb4affd6aac56bee313730c449aa4e174f5382b6a3ce`。

作者证据单列[复用记录](evidence/d05-s1-verification/author-reused.json)：integration compile、`go vet -mod=readonly -tags=integration ./tests/objects` 与 `go test -mod=readonly -count=1 ./internal/central/postgres` 均 exit 0；[postgres 原日志](evidence/d05-s1-verification/author-postgres-unit.log)为 0.073s。作者原脚本执行四个新顶层加旧 `TestProjectLifecycleMigrationClosedStatesAndProjectIsolation`，race 7.247s，exit 0，见[作者原日志](evidence/d05-s1-verification/author-fixture.log)。四新测试本次已独立再执行；旧回归、unit、完整 vet/compile 复用匹配的相关输入证据，不改称独立执行。作者资源审计助手曾有两次假设错误（network create 不带 label、网络不存在错误文案不同），修正记录保留；这两次不是产品测试失败，本轮独立资源审计首轮通过。

[独立探针](evidence/d05-s1-verification/s1_independent_probe.go.txt) SHA 为 `cf0f4db8a747e6b6a7bc244f096acf939a1aee9ae31882fac37e3fa30b4fafa0`，通过 Go overlay 注入虚拟测试路径，没有修改仓库测试。取得新的独占 fixture 窗口后，可在固定 S1 代码的隔离检出运行 `python3 docs/development/agent-team/evidence/d05-s1-verification/replay-probes.py`；[重放助手](evidence/d05-s1-verification/replay-probes.py)只做了语法检查，正式通过证据来自上述原命令，后续运行仍须独立审计资源。

## 清理、限制与交接

[资源审计](evidence/d05-s1-verification/resource-cleanup.json)按本轮 create 事件的 exact ID 与 nonce/name 关联，确认 4 容器、3 网络均不存在；跟踪到的 159 个进程、4 个进程组无残留，自有运行 TMP 为空。既有 2 个容器及网络 ID 集合与执行前完全一致。未操作既有外部服务、读取其凭据或全局清理 Docker。11:52:22 UTC 完成输入与资源末检后已停止 Go/Docker 活动并交回窗口，主线程随后交予 R3；当前只归档本报告及证据。

S1 仅证明数据库迁移、CHECK、插入 PK/唯一性及事务行为。没有验证 UPDATE 身份不可变、真实 work 登记/Unknown 确认、Object 停止、join/进程死亡、ProjectStopAuthority、真实 Object I/O 或最终删除；MinIO 由既有 wrapper 准备不代表这些行为已执行。S2 必须消费已验 schema 并另行完成运行闭环。

验收者没有修改两源或其他产品文件。实际新增仅本报告与 `evidence/d05-s1-verification/`，原始临时证据保留于 `/tmp/agenteam-d05-s1-independent-3t_e4rql`；缓存、fixture 数据与大依赖索引不纳入提交。产品测试失败 0，剩余阻断 0。
