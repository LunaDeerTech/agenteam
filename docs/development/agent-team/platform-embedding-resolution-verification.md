# 平台 Embedding current-selection Resolver 技术验证

最终验收更新（2026-10-08）：[九路径末件组合独审](platform-embedding-resolution-verification-evidence/originals/39890564b4c9abf5-review.md) `39890564` 为有限 PASS / STOP，root 已正式接受本卡完整九路径的固定版本组合，README `8dfab520` 补足第 9 路径。policy/pure 沿 `fa4bc123`，六 PG 沿 `28dc84cf`；七轮作者成功与独立 A/B 等沿既有版本组合，不表示当前 HEAD 单次全测。本结论限本卡库范围，不代表生产或完整 D09/D13/D14，生产 consumer、serving、Invocation 与默认 root 仍未绑定。下文保留八技术封存时点正文；[原报告 `430ce1a1`](platform-embedding-resolution-verification-evidence/originals/430ce1a16be61c13-prior-eight-technical-report.md)和[原 manifest `316e4a05`](platform-embedding-resolution-verification-evidence/originals/316e4a0540f1212e-prior-eight-technical-manifest.json)按原 bytes 保留。

[最终八技术组合独审](platform-embedding-resolution-verification-evidence/originals/ab14ab6b5832c2c9-review.md) `ab14ab6b` 为有限 PASS / STOP，root 已正式接受当前八条技术路径的固定版本组合；[独立 A/B 正式结果](platform-embedding-resolution-verification-evidence/originals/c26b50951e19fbb6-review.md) `c26b5095` 亦已接受。README 第 9 路径及完整九路径／整卡尚待末件组合确认。本报告不将分阶段、不同固定输入的结果描述为当前 HEAD 一次 fresh 全套。

[rev2 规格永久档](platform-embedding-resolution-spec-verification.md)原位复用。[rev3 差量独审](platform-embedding-resolution-verification-evidence/originals/0095e4ce8c387343-review.md) `0095e4ce` 只澄清第 3、71、99 行：结构合法同单位重入仍先核当前事实、原 writer 与 canonical 身份；有已提交相容性冲突且无更早错误时先 Forbidden，其余前置通过后的持久 semantic 改义才按 KeyReused。路径、预算及停止边界未扩大，原 rev1 D1/D2 和 rev2 原件不回写；rev3 卡固定于 `afc700b9`。

当前 `knowledge_embedding`、`memory_embedding` 使用 `platform.embedding`，选择已验 float profile；原合法 direct agent/tool（含 ApprovalAuto）、platform.memory 和 Summary 分支保留。Select 只是安全选择读；受信 consumer 事实、同 Store/完整锁/调用者 Tx、planned Acquire、不可变 snapshot/binding/canonical lease 与原 writer 确认仍是接受前提。

| # | 技术路径 | 固定 SHA-256 前缀 |
| --- | --- | --- |
| 1 | `internal/central/model/resolution_policy.go` | `c8ccd0950f39015e` |
| 2 | `internal/central/model/platform_embedding_resolution_test.go` | `dd1e60b71975fd83` |
| 3 | `tests/model/platform_embedding_resolution_fixture_test.go` | `c43365bf6d92480a` |
| 4 | `tests/model/platform_embedding_resolution_selection_test.go` | `115d60519161b31d` |
| 5 | `tests/model/platform_embedding_resolution_atomicity_test.go` | `64471260c1a514d0` |
| 6 | `tests/model/platform_embedding_resolution_authorization_test.go` | `9fce2b7f26d6f4ac` |
| 7 | `tests/model/platform_embedding_resolution_replay_test.go` | `b796560e0348ec1f` |
| 8 | `tests/model/platform_embedding_resolution_unknown_test.go` | `13fb6988c1337978` |

#1/#2 沿已接受 `fa4bc123`；六 PG 源已由 root 提交 `28dc84cf0cbec0188bbc4b9577d16bcabf0de2b7`，按[八行原始指纹](platform-embedding-resolution-verification-evidence/originals/bfbecbd1360af4a2-source-rows.json)定位；完整 commit:path Gitref 与源 SHA 保存在来源映射，归档者未另读 Git 或重验源码。作者 pure 为最新 ProfileMatrix 24＋未受影响原 50＝74 唯一 RUN，独立三补集由两实际轮组成；原整体 Marshal 忽略 error 与可变 ContextLength 期待缺口、独立 probe 原前提 FAIL 均保留。只修 #2 断言，未改产品 Clone。compile02 在 c433 helper 下 race-c／vet 实际通过；原 14 名枚举只因 generated TestMain 同字节复用发现能力，不替代新 helper 行为。原编译大 action graph、二进制与缓存不重复归档。

| 实际运行 | top / RUN ID | top 秒 | fixture / outer 秒 | 原终局 |
| --- | --- | --- | --- | --- |
| [原 Selection FAIL](platform-embedding-resolution-verification-evidence/originals/9e918dc5b18ace77-result.json) | 1 / 11 | 5.50 | 52.033 / 112.547633 | FAIL 保留 |
| [Selection](platform-embedding-resolution-verification-evidence/originals/9e1f9c1ba1aff995-result.json) | 1 / 11 | 5.99 | 53.455 / 108.939894 | PASS / full STOP |
| [Atomicity](platform-embedding-resolution-verification-evidence/originals/d6e4ececc519d446-result.json) | 1 / 15 | 7.23 | 58.818 / 114.394861 | PASS / full STOP |
| [Authorization](platform-embedding-resolution-verification-evidence/originals/bf3261bd7b364935-result.json) | 1 / 55 | 8.99 | 57.120 / 112.730188 | PASS / full STOP |
| [Replay](platform-embedding-resolution-verification-evidence/originals/f8793ba7c7dfc080-result.json) | 1 / 45 | 12.57 | 65.477 / 119.967490 | PASS / full STOP |
| [Unknown](platform-embedding-resolution-verification-evidence/originals/1767ccc6401bc634-result.json) | 1 / 17 | 6.93 | 62.474 / 118.145440 | PASS / full STOP |
| [原 Resolver 六组](platform-embedding-resolution-verification-evidence/originals/d40814a1d71a06e8-result.json) | 6 / 41 | 2.06/1.79/1.43/2.05/1.78/1.99 | 62.784 / 118.255019 | PASS / full STOP |
| [原 S3 三组](platform-embedding-resolution-verification-evidence/originals/b692e88bfb7175d8-result.json) | 3 / 25 | 5.92/5.38/4.69 | 66.882 / 123.345860 | PASS / full STOP |
| [独立 A](platform-embedding-resolution-verification-evidence/originals/8587c0acba32bc38-result.json) | 1 / 9 | 6.43 | 63.889 / 118.580069 | PASS / full STOP |
| [独立 B](platform-embedding-resolution-verification-evidence/originals/c32876a0bc817001-result.json) | 1 / 7 | 6.28 | 57.358 / 112.947926 | PASS / full STOP |

七个作者成功轮共 14 top、209 RUN/PASS；独立 A/B 另为 2 top、16 RUN/PASS（10 叶子），原失败 11 RUN 单列。作者成功轮固定 996 输入，A/B 为已审 overlay 的 999 输入，各轮前后相同。A 覆盖两 purpose 归属／输入撤权和 selector 锁竞争；B 覆盖真实提交后 Unknown 修饰、取消后历史确认和 provisional 后取消的实际回滚。旧 Resolver6／S3三的[组合独核](platform-embedding-resolution-verification-evidence/originals/d5f3b19965646c9a-review.md)保留既有私有 consumer 与历史边界。

原 `embedselect01` 首个业务断言为 Selection:229 的全库 `secret.resolve` 计数 5，十个 nested 已 PASS；五条实际记录未单独保存，不补称其均来自 Account。正式账户身份链可能写 system consumer；[唯一 +34B SQL 修正](platform-embedding-resolution-verification-evidence/originals/2e01911140c61a46-delta.patch)改为 `action='secret.resolve' AND metadata->>'consumer'='model'` 的零计数，仍覆盖 System/Project 全部持久 Model 行，不以 project、actor、当前 lease、时间或 baseline 缩窄。后七成功轮及 A/B 使用 c433；该断言不证明回滚内没有尝试。

原失败 fixture exit255、outer exit1 和 `double_cleanup=false` 永久保留；原 task runtime 的 `go-build907913867` 在直接／收养 wait 终局后才由另授权步骤精确删除，[恢复原结果](platform-embedding-resolution-verification-evidence/originals/5ffbf09a2e23ca3e-result.json)不改原失败。旧 v02 历史 gate 仍阻断，root disposition 只允许 exact-byte 新 generation v03；没有宽容旧失败分支。driver v01 的两项 gate 缺陷、纯自查／Python 导入前提失败均以原件保留。

十轮均各有 fixture 和 driver 的实际 direct wait；成功九轮 adopted=0，原失败 adopted=1。observed 进程数仅为观察，不等于 wait 数量。每轮七个精确资源 ID 两扫 absent，owned 两空；成功轮 runtime 两空，原失败 runtime 由上述独立恢复补足。作者 v03 七轮只声明其内部 49 ID 不重复，A/B 只声明其内部 14 ID 不重复，不新增跨 generation 全局去重结论。TCP 在原 75 秒内双清，仅补充 host 轮询；Summary 整组 outer 123.345860 秒包含三个 top、准备和退休，原约束为每 top 120 秒及 package 六分钟。原非 owned PID1 shim 不认领 wait/kill；Summary 另有 sh／pg_isready 两项原观察，未推断归属或当前存活，不称全主机清零。

Unknown 仅为真实 commit/rollback 后的受控结果修饰，非物理 COMMIT ACK 丢失；新 Authority/Session 非进程重启。私有 Knowledge/Memory 严格事实、released SQL 不等于生产 consumer/release API。生产 consumer、serving generation、ExpectedDimensions、Nonchat.Embed、Provider 发送、Invocation/Usage 和 root 未因此绑定；三停止、Jina、ready503 与完整 D09/D13/D14 边界不变。

[来源映射](platform-embedding-resolution-verification-evidence/source-map.json)保留 583 个逻辑原件；十次 run/launch 完整为 330 件、4256052 bytes。按 SHA 去重后本档新增 445 个实体、4618687 bytes，并原位复用 5 个既有实体。没有复制源码树、900 条闭包、Go binary、runtime 或 cache。原件内部路径和 Markdown 链接保留历史语境；本页生成链接定位永久实体。

本次只做原件 source/archive bytes 与 SHA、JSON、生成链接及格式核对；[检查记录](platform-embedding-resolution-verification-evidence/checks.json)、[逐行原格式例外](platform-embedding-resolution-verification-evidence/format.json)和[manifest](platform-embedding-resolution-verification-evidence/manifest.json)记录实际范围。原 raw/diff 空白和缺末 LF 不改写。未运行 Git、Go/Node、业务、资源、网络或新验证；root 后续 Git 检查与 README #9 接受单独办理。
