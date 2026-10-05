# D09 Secret Model usage 独立验收

**本卡 20 源独立验收 PASS，已由主线程采纳并提交推送 `8ad6759dbb499ae1cfec1d47bcab75f1abb2f56d`，远端一致已由主线程确认。** 范围为 Secret planned read、同 Tx RequestID witness/checker 和实际 Model legacy 入口边界，含 6 生产 + 14 测试；生产 Model consumer/Invocation/Runtime、发送与 actual join、HTTP/root 尚未因本卡完成。

本报告按冻结原件归档，不新增动态通过声明。业务基线为 `4cc4726b5707127f4b50c7da7023a3cd1f0b7f2d`，规格 [rev1.1](../work-items/recovery-d09-secret-model-usage.md)提交 `9110686`；[最终清单](evidence/d09-secret-model-usage-verification/author/candidate-freeze-02/manifest.json) SHA 为 `ccc8044f7ac0c9ba2ac13ee00edbbd9c65ef042bdef9c43c712f223a32d006f0`，6 生产 review03 清单 SHA 为 `62f592c62b14fe62feeabf9389bfa3bbe0e43419075d2fdf53a35fec5b941338`。实施者 `recovery_handoff` 与独立验收者 `restore_test_dependencies` 分离；[独立原报告](evidence/d09-secret-model-usage-verification/verification/review.md.txt) SHA 为 `9b184a0177fcda1ddc16b7d7981be0a477c70bc9740a30a9dcc991e7907a4087`。

## 实际结果与原失败

| 固定检查 | 实际结果 |
| --- | --- |
| 作者最终 Secret unit/race、integration compile/vet、两 cmd build | 最终 20 输入，原 argv/env/exit 明确为 0；compile-only 不计动态覆盖 |
| 作者新真实组 | 6 顶层/43 子例 PASS，Security 38.126s |
| 作者旧真实组 | 10 顶层/35 子例 PASS：9 Security 31.238s + 1 Project 19.367s |
| 作者 Account 只读回归 | 4 顶层/7 子例 PASS，Account 9.312s |
| 独立 race integration 编译 | exit 0，31.958s，仅编译 |
| 独立唯一真实一轮 | 2 顶层/7 子例 PASS，Security 11.345s；driver/runner exit 0，101.873s |

作者三组共 **20 顶层/85 子例**，独立为 **2 顶层/7 子例**；无 FAIL/SKIP/race，其它包 no-tests 不计覆盖。原 Model/Project 纯检查及补齐基线 API 后的 Account/Secret 定向 race 按适用语义复用，不声称所有历史检查都在最终输入重跑。逐日志和输入核对见[独立检查映射](evidence/d09-secret-model-usage-verification/verification/author-evidence-verified.json)，实际命令/env/exit 保留于[作者证据](evidence/d09-secret-model-usage-verification/author/delivery-index.json)及[独立证据](evidence/d09-secret-model-usage-verification/verification/evidence-index.json)。空日志不单独作为通过依据。

原失败链完整保留：

- nonce 首次 integration 编译因测试私有结构新增字段而发生位置字面量错误，33.489s、exit 1；卡 rev1.1 唯一新增测试路径将该行改为 keyed literal，原强断言不变。
- 作者选择性 snapshot 漏 `api/openapi/account.json`，导致 Account OpenAPI 对照纯测试失败；补固定基线原字节后定向通过。此为输入准备失败，不归因产品。
- S1 首轮 Prep 早于 leaseGrant，实际四个授权/错误优先级反例红；review02 恢复授权顺序后，Prep 仍早于 writable，又有 uninitialized/unavailable 两个真实单元反例红。review03 将 Prep 移到原 writable 后、ID/INSERT 前；最终纯及真实组通过。
- S2 是修前静态确定问题：确认 Committed 后 caller 已取消仍可能交材料。修后在确认提交后及创建材料后检查原 caller context，必要销毁材料，保留 Committed 事实与 context cause，Unknown 优先且原标识不被取消覆盖；新真实 `committed-then-cancelled` 通过。**没有 S2 修前动态红记录。**

[原静审](evidence/d09-secret-model-usage-verification/static/production-review01.md.txt)、[最终差量静审](evidence/d09-secret-model-usage-verification/static/review03-delta.md.txt)、原日志/输入与窄 patch 均保留。四份关键失败输入可从已提交源加小型差量恢复，未复制作者整树。

## 独立增量与真实边界

独立实际命令为 `sh scripts/test-objects.sh -run '^(TestIndependentSecretModelRequestAndWitness|TestIndependentSecretModelRevalidationAndOutcome)$'`。原 driver 的 `-race -count=1 -timeout=6m` 保持，verbose 沿原 GOFLAGS；Go 1.27.1、GOTOOLCHAIN=local、GOENV/GOWORK=off、GOPROXY=off、modreadonly、私有 cache/runtime 和空 Docker config 见[原命令](evidence/d09-secret-model-usage-verification/verification/fixture-runs/independent-01/command.json)。固定 PG17/PG16、vector 与 MinIO 由原 fixture 实际核验。probe 源 SHA 为 `5805c6d04b344fadf7a297e867dde0b85e3f6b26105f63d34061c25a0a1f0ebb`，运行前后 20 源及 probe 不变。

七个子例分别证明：另一个真实成功 Invocation 换绑原 exact lease 被拒；同映射外来 issuer 精确 ResourceBusy；AEAD 后篡改 typed-valid RequestID 确实进入 Secret checker 并回滚；成功 witness 换新 Tx、完整 union 后仍精确拒绝；实际 PG blocker/holder PID 证明等待期间零 Audit，锁后合法 process tuple 变化导致旧 mapping ResourceBusy，新 Discover 仍成功；真实提交和真实回滚两种后态中，Store 装饰 Unknown 再取消 caller 均保留原 AttemptID/Cause、零材料，并分别核 Audit 有行/无行。跨 Tx 探针使用非取消 context 和新 root Tx，未止于 Nested/已取消前置。

Unknown 是标准 Store **结果装饰**，不证明物理 backend attempt、丢 ACK 或原 writer 未终局；本轮没有新增网络故障方法。B03 Human 缺 User SH 的反例可能先在 Audit→Account Session gate poison，保留精确 tap/outer 错误和真实回滚，但不宣称越过该 gate 到 Secret checker；独立合法 Service 路径另证真实 Secret checker，二者不混写。

## 资源、归档与后继

作者三轮累计 12 容器/9 网络 exact ID 二次不存在，原 2 容器/4 网络 ID/name/labels 不变，320 条跟踪进程记录及所属组无存活、私有 runtime/gotmp 空。首轮 `new-01` 原 result 的 network_count=0 是采集遗漏，原记录不改；实际 3 网络的 live ID/name/fixture labels 和两轮 absence 已补证并由 V 核对，未为补统计重跑测试。

独立轮 4 容器/3 网络两次 exact ID 不存在，三网存活标签实际 inspect；原 2 容器/4 网络两轮完全相同，335 个跟踪进程及所属组无存活，额外自有进程为 0，runtime/gotmp/config 空。作者和 V 已退出全部 Go/Docker/runner 命令并交回窗口。[资源交接](evidence/d09-secret-model-usage-verification/verification/final-handoff.json)与原漏统计/补证并存，不把零计数写成未创建网络。

[持久证据入口](evidence/d09-secret-model-usage-verification/README.md)提供 20 源的 accepted Git 对象核对、原独立 probe、四份失败输入小型差量和全文件索引。原日志/patch 不改字节；[空白例外](evidence/d09-secret-model-usage-verification/whitespace-exceptions.json)只列实际检查发现警告的精确原文件。归档阶段只运行 SHA/源码恢复、链接、空白检查与只读 Git；未运行 Go/Docker/网络或产品测试，未执行 Git 写操作，详见[归档检查](evidence/d09-secret-model-usage-verification/archive-checks.json)。

生产 ConsumerAuthority、真实 Model lease/Invocation/Runtime、Resolver/Usage/发送与 actual join、HTTP/root 仍未因本卡绑定；严格事实 fixture 不是生产消费者。未运行真实 Provider 账号测试，未恢复暂停的 Object 任务。Object/Artifact 阻断、旧 Outbox 未归因红、Summary 初值/Settings 待决及完整 D08/D09/D28/E01 未完成状态均保留。System Model HTTP 另沿已采纳卡 `b4dd1e6` 的 17 路径实施、尚未验收；本卡通过不替代其门槛。
