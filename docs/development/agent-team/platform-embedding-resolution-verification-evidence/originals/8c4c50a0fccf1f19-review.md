# Model 八技术路径版本组合映射

STOP，仅整理已接受阶段与缺项，**不宣布八技术或完整九路径最终通过**。依据rev3卡 `f6535ef6`。#1/#2沿root已接受提交 `fa4bc123`；六PG当前版本来自固定compile02安装记录。本次未重新核工作树全源、闭包或执行任何检查/资源。完整SHA与原位报告引用见 [mapping.json](mapping.json)。

| # | 技术路径 | 固定SHA前缀 | 可复用接受来源 |
| --- | --- | --- | --- |
| 1 | `internal/central/model/resolution_policy.go` | `c8ccd0950f39015e` | 纯阶段＋独立补集 |
| 2 | `internal/central/model/platform_embedding_resolution_test.go` | `dd1e60b71975fd83` | 纯74项版本组合＋独立三补集 |
| 3 | `tests/model/platform_embedding_resolution_fixture_test.go` | `c43365bf6d92480a` | 原六源STATIC＋c433窄修＋compile02＋五新实际 |
| 4 | `tests/model/platform_embedding_resolution_selection_test.go` | `115d60519161b31d` | STATIC／compile02＋Selection实际独核 |
| 5 | `tests/model/platform_embedding_resolution_atomicity_test.go` | `64471260c1a514d0` | STATIC／compile02＋Atomicity实际独核 |
| 6 | `tests/model/platform_embedding_resolution_authorization_test.go` | `9fce2b7f26d6f4ac` | STATIC／compile02＋Authorization实际独核 |
| 7 | `tests/model/platform_embedding_resolution_replay_test.go` | `b796560e0348ec1f` | STATIC／compile02＋Replay实际独核 |
| 8 | `tests/model/platform_embedding_resolution_unknown_test.go` | `13fb6988c1337978` | STATIC／compile02＋Unknown实际独核 |

纯阶段 [0c31f9d6](/workspace/scratch/owner-ui-runtime-verification/platform-embedding-pure-final-review/review.md) 为最新ProfileMatrix24＋原未受影响50＝74唯一RUN，vet按产品/import不变复用；独立三补集 [9f3768ef](/workspace/scratch/owner-ui-runtime-verification/platform-embedding-independent-pure-result-review/review.md) 是两轮组合，不是最终源一次fresh全套。原作者空对空缺口、独立probe前提FAIL及后继修正保留。

六PG [da648d4f](/workspace/scratch/owner-ui-runtime-verification/platform-embedding-pg-static01/review.md) 原STATIC中的fixture为cd738；现c433由[首FAIL/退休恢复/查询窄修](/workspace/scratch/owner-ui-runtime-verification/platform-embedding-embedselect01-review01/review.md)及[compile02末件](/workspace/scratch/owner-ui-runtime-verification/platform-embedding-compile02-review01/review.md)连接。仅加consumer=model查询谓词，持久材料审计0不外推回滚内尝试。compile02 race-c/vet实际通过，486输入只换一个hash；[14名list](/workspace/scratch/platform-embedding-resolution-author-v1/pg-list-proposal01/run01/result.json)按新旧TestMain逐字同有限复用，它不执行测试正文或证明新SQL行为。

| 作者实际轮 | RUN项（含父组） | top／package秒 | 已接受独核报告 |
| --- | --- | --- | --- |
| embedselect02 | 11 | 5.99／7.024 | [1adbd48b](/workspace/scratch/architecture-model-embedselect02-review/review.md) |
| embedatomicity01 | 15 | 7.23／8.268 | [462a545e](/workspace/scratch/owner-audit-ui-verification/model-embedatomicity01-review/review.md) |
| embedauth01 | 55 | 8.99／10.029 | [2b93e811](/workspace/scratch/owner-audit-ui-verification/model-embedauth01-review/review.md) |
| embedreplay01 | 45 | 12.57／13.606 | [de379ff4](/workspace/scratch/owner-ui-runtime-verification/platform-embedding-embedreplay01-review01/review.md) |
| embedunknown01 | 17 | 6.93／7.959 | [1f02d923](/workspace/scratch/owner-audit-ui-verification/model-embedunknown01-review/review.md) |

五新为作者真实PG＋独立原件复核，不等于独立A/B亲跑。复用996固定输入、各轮实际wait/owned/runtime/TCP退役原件；五轮35ID仅本v03 generation去重。83 observed不等于83wait，adopted0，非owned PID1 shims单列。原Selection FAIL／cleanup=false及后续精确恢复不回填。Unknown限真实commit/rollback之后受控装饰/取消；新Authority不等于进程重启，private/released SQL不等于生产consumer/release API。

| 最终缺项 | 当前映射边界 |
| --- | --- |
| 旧Current Resolver六top | 待对应实际轮及独核接受；14list不能替代 |
| 旧Meeting Summary三top | 待另一实际轮及独核接受 |
| 独立实际A/B | 仍pending；已有STATIC/offline/prepared/filegate不算test body |
| 第9路径 `docs/development/backend/README.md` | 仅候选、未安装；技术接受后root另授末件 |
| 最终八技术／完整九路径 | 待以上结果与最后有界组合，不是当前HEAD一次全套fresh |

本映射未等待后继运行、不读取活动轮；没有Go、安装、业务写入或资源操作。生产Knowledge/Memory接入、serving/维度/Invocation/root与原三停止/Jina边界保持。写入停止，可供后继验收原位引用。
