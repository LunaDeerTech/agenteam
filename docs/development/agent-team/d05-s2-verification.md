# D05 S2 Object 项目停止独立验收

日期：2026-10-05。结论：**卡内 28 文件独立验收通过，无未决阻断，可供后继 Artifact、Object Audit 与 Project 组合消费**。验收者未参与产品实现；独立静审发现的完整 writer 假 join，以及作者真实回归发现的旧 Source Force 预算与 startup admission 缺陷均保留原红，并在最终输入独立验证修复。主线程已采纳相同 28 文件，提交并推送 `6658a6cb1f29299521773bc8dc86b2f607b8c809`，确认远端一致。本报告只证明 Object 库的 S2 范围，不宣称 Project/root 生产装配或 D05/D08 整体完成。

## 固定输入与执行

规格为[恢复卡](../work-items/recovery-d05-object-stop.md) rev2，SHA-256 `962915706bfa49e46aba5c2399360dfdc96fdff96df3e1408da1d9e6ad712137`。[S1 schema](d05-s1-verification.md) 已验，未重复执行迁移组。

实际隔离树由 `49c6589c3919cad62a4bae2c993b5fd953d36b1e` 的 Git archive 加最终 28 文件及一个独立探针组成。没有把当前共享 HEAD、后续 Project 绑定 `81fe742` 或完整验收提交树混入执行输入。28 项逐 SHA 等于作者[最终 manifest](evidence/d05-s2-verification/author-final-input.json)，其 SHA 为 `c3dc0800bb7b4c2c7368186b42954f2ba8ac23d2cefd2d2d0ff6990f6c6320bd`；执行末检没有变化，归档时也逐项核对上述已提交代码。

独立[探针源码](evidence/d05-s2-verification/independent_gate_unknown.go.txt) SHA 为 `1dfc3b7140e9a6f022f0f725d6338f8fa9851c591c950a0d1764edbd0b45ec16`，只放入自有隔离树的 `tests/objects/project_stop_independent_gate_unknown_test.go`。先执行 `go test -c -tags=integration -race -o <owned>/bin/objects.test ./tests/objects`，exit 0，13.252s；没有运行该二进制替代正式 fixture。

随后仅运行一轮原 `sh scripts/test-objects.sh -run <25 个精确顶层的正则>`，完整参数、cwd、环境和指纹在[命令记录](evidence/d05-s2-verification/fixture-command.json)。固定 Go 1.27.1，`GOTOOLCHAIN=local GOENV=off GOWORK=off GOPROXY=off`，离线锁定 GOMODCACHE，复用验收者自有 S1 GOCACHE；使用自有 TMPDIR、空 DOCKER_CONFIG 与本地 Docker socket。`GOFLAGS=-mod=readonly -v -p=2` 仅增加实际测试输出和限定构建并发，原 driver 的 `-tags=integration -race -count=1 -timeout=6m` 保持。

实际环境为 PG17 `170008`、PG16 `160012`、vector `0.8.1`，两镜像精确 digest 和平台通过 inspect。MinIO `RELEASE.2025-10-15T17-29-55Z` 的二进制 SHA 为 `dc5298474f0bc87a068f0b1135c583bb1278c17c11c512212ed7644a238c89c8`，fixture 验证后使用真实服务；见[环境记录](evidence/d05-s2-verification/environment.json)。

## 独立结果与覆盖

**25 个顶层、43 个子测试，共 68 个实际命名 case 全部 PASS；无 skip、FAIL 或 race 报告。** objects 包 94.897s，整轮墙钟 148.506s，exit 0。顶层集合是卡定 21 新、原 Source unopened shutdown、两个 Runtime 回归及 1 个新增 gate Unknown 探针。SR1 的 writer 子例已包含在卡定测试中，没有重复单独运行。其它包的 `no tests to run` 不计行为通过。见[实际 case 清单](evidence/d05-s2-verification/independent-result.json)与[完整原日志](evidence/d05-s2-verification/independent-fixture.log)，日志 SHA 为 `081fa42b94c45d887184a62ad2cbd93bc036a26d5f17a1aebc15067a06aaa396`。

| 风险 | 最终独立观察 |
| --- | --- |
| 当前 cause/权限、Archive 与 Delete | 严格真实 SQL fixture 校验 actor/cause/manifest、同 Tx 与已持锁；错 cause/Restore/只读分支不取消。Archive 保留已发布 canonical、正常读取/下载 SHA；Delete 等待真实 reader、source、未 open lease 与 transfer 退休。 |
| 实际 I/O 与多关系 | resolver 前 work 已持久；活 reader 用大 body、真实 GET hold、active native lease/unjoined work/exact process 前置证明。prepare/body/cleanup zero/remove/回写阻塞期间保持 pending；源与目标关系不覆盖，精确停止不取消其它 Project 的维护轮次。 |
| native、进程、claim 与扫描 | 真实 kill/flock 才能证明精确旧进程；缺 provider 或原 writer 未终局保持 pending。旧 cleanup worker 被新 native claim 覆写仍保留独立 work/fence/process；身份篡改拒绝。无 work native、仅技术行、多页及低 ID busy 公平推进均覆盖。 |
| admission 与维护 Unknown | 原 PG COMMIT 协议代理分别命中 admission、preflight、gate/join/stopped、verification/cleanup；未确认 admission 零外部 I/O，原 writer 持锁时不能猜终局，晚提交/回滚保留原身份。 |
| 新增活 work gate Unknown 探针 | 以真实已登记 preparation 阻塞 Read/Close，覆盖已提交丢 ACK、延迟提交、回滚。观察零提前 Close、未 join、原 work/process/epoch 不变；延迟/回滚期间 Project EX 仍被原 writer 占用，后续 stop 不能报 Stopped。串行确认原事务后，gate/revocation 精确对应其结局；释放 body 后同 cause 收敛。原空项目 gate 用例不能独自证明此项，因此单独补测。 |

## 三项真实缺陷及原始失败

| 缺陷 | 原红与修复 | 最终独立门槛 |
| --- | --- | --- |
| 完整 standalone writer 被 spool 结束误判 join | [writer-red-01](evidence/d05-s2-verification/author/logs/writer-red-01.log)：真实 300000-byte PUT+verify 后，FinishWriterAccess 在 DB acquire 前阻塞，Archive/Delete 提前 Stopped，Discard 误成功。修复以同 mutex 的 preparation writer 引用覆盖完整调用，禁止活 writer 被 discard；实返后自动继续收尾。 | archive/delete/discard 三子例通过，阻塞中 native/work 不假 join、Drain 不成功；释放后无需人工补 Discard 即 Stopped/Drain 成功。 |
| Source unopened Force 另开 cleanup 预算 | [compat-core-01](evidence/d05-s2-verification/author/logs/compat-core-01.log)：原 release 已被 Force 取消，work join 却另开活 context 重争 held Object 锁。修复沿用原 cleanup ctx，取消后留下可恢复 checkpoint。 | 原完整顶层的 drain/force/late_cancel 通过；Force 仍为 500ms、原 elapsed 上限 650ms，锁未释放时不能抹掉 active checkpoint，零 GET。 |
| startup 子工作丢失初始化准入 | [compat-runtime-01](evidence/d05-s2-verification/author/logs/compat-runtime-01.log)：FORBIDDEN 被 DEPENDENCY_UNAVAILABLE 掩盖，startup 未到真实 cleanup。review03 仅让同 Service、active 且未取消的真实父 operation 传递不可变 initializing 位。三个实际 child 调用都先归一为父 op.ctx；cleanup 的 WithoutCancel 不回流该入口，stop/force/quota/普通 readiness 保留。 | 两个原 Runtime 顶层通过：真实后续 Forbidden 可见，独立对象继续清理；startup/worker 都到实际 cleanup，取消后仍以原 1 秒 join，保留 applying checkpoint，未延长原触达或退出预算。 |

原静审与两轮修复差量分别保存在[初审](evidence/d05-s2-verification/static-review01.md)、[writer/Source 修复复核](evidence/d05-s2-verification/static-review02.md)、[startup 差量复核](evidence/d05-s2-verification/static-review03.md)、[review02 patch](evidence/d05-s2-verification/fix-review02/delta.patch)和[review03 patch](evidence/d05-s2-verification/fix-review03/delta.patch)。静态发现与作者动态原红分开记录，不把静审当作复现。

另外保留[首轮 reader 前置失败](evidence/d05-s2-verification/author/logs/fixture-new-01.log)及其[原输入](evidence/d05-s2-verification/author/input-fixture-01.json)：4-byte body 已在 ReadObject 返回前同步 EOF/Close，无法作为仍活 reader 的前置；该轮库已清理，不能补取原 SQL 现场。作者随后增加小 body 已 join 对照，大 body 用真实 hold 和 native/work/process 断言证明仍活；原 stop 断言及预算未弱化。早期编译两项错误未另存原终端输出、私有 plan 单测前置错误、误传 driver `-v`、共享 cwd 的作废 compile 也按[作者交付](evidence/d05-s2-verification/author/author-final.md)原样保留限制，不纳入独立通过证据。

## 复用结果、重建与资源交接

[作者证据核对](evidence/d05-s2-verification/author-evidence-review.json)核了 44 个索引条目的实际 SHA、各轮 28 文件、精确命令和 objects PASS 行。作者共覆盖 **95 个不同顶层＝21 新＋74 旧**；review03 直接执行 61，余 34 按已审差量复用 review02。没有宣称 95 项在最终输入全部重跑。最终 object unit/race/vet、integration compile、两 cmd build 均复用匹配输入证据；旧 Source、Runtime、Transfer、Download、Object recovery 的实际集合和分轮输入见[作者覆盖记录](evidence/d05-s2-verification/author/coverage-final.json)。本轮额外 25 项是独立运行，不以作者自测替代。

[输入索引](evidence/d05-s2-verification/inputs.json)与 9 个最小历史字节片段可离线重建最终、三轮真实原红及 review02 绿输入。Source Force 原红的 pre-writer 测试没有原样备份，本次从保留的 writer-red 源去掉新增入口/helper 后，SHA 精确等于原 manifest 的 `356209c4155532c168ad3811d0d7b2f0b0b3b34ffdf87e9a937ae94c11bd1125`；[还原记录](evidence/d05-s2-verification/history/reconstruction-core.json)明确标为哈希确认的重建。没有把猜测还原冒充原现场。

`python3 docs/development/agent-team/evidence/d05-s2-verification/rebuild-inputs.py --repo /workspace/agenteam --verify-only` 已实际通过，核对五组各 28 文件及探针字节。去掉 `--verify-only` 可生成自有隔离树；默认 final，`--revision` 可选原红。助手只读 Git 对象、重建源码，不执行 Go/Docker。后续实际重放须另获独占 fixture 窗口、使用原命令与[资源审计源码](evidence/d05-s2-verification/run-fixture.py.txt)；该源码使用当次自有目录，依赖的历史准备计划一并保留，其待运行状态不替代最终结果。原红不是当前生产缺陷。

[资源末检](evidence/d05-s2-verification/resource-cleanup.json)确认本轮 **4 个容器、3 个网络逐 exact ID absent**，原 2 容器/4 网络的 ID/name/labels 不变；跟踪 216 个进程、4 个进程组无残留，event watcher 已 Wait，自有 runtime 目录为空。没有操作既有外部基础设施、读取其凭据或全局清理 Docker。13:49:17 UTC fixture 完成，末检后已交回唯一窗口并停止 Go/Docker，归档期间没有再次占用资源。

验收者实际新增仅本报告与同名 evidence 目录；没有修改产品、作者测试、卡或状态文档，没有 Git 写操作。只读输入、精确命令、原日志、probe 和资源结果在[证据 SHA 索引](evidence/d05-s2-verification/SHA256SUMS)中。缓存、依赖、fixture 数据和大二进制未归档。
