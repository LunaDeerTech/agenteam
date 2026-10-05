# D09 最终独立验收准备

已启动 verification_worker，未参与实现。本轮固定 `81fe742` + candidate-rev3 21 源（manifest SHA `6d122a4a34b9eb31421c0b795edfb22155dc26d5a50e93b02a3a1f964dee68d6`）。复用生产静审、typed-nil 窄修静审、首红归因与 rev3 测试差量静审，指纹见 reused-evidence.json；不重复全文审查。作者最终源/日志仍待交付，变化只做相关 delta。

## 最小独立探针

1. `TestModelProjectIndependentSixHistoricalReceiptsRequireCurrentRead`，archiving/archived 两子例。真实执行全部六 CRUD，删除后确认 canonical Provider/Model 为零、6 commands/6 Model Audits/6 Model events、Secret 引用归零；再建立合法 lifecycle fixture。原 Session 撤销、同 User 新 Session 仍可重放及查证六个原精确 receipt；同样 payload 的新 key 全部 ProjectNotActive；旧 Session 及随后失去 Owner 的新 Session 均无回执、零 Model 副作用。原 Update 的 stale expected version 不改变。archive completed checkpoint 与 Owner 改变仅为受锁保护的 authority fixture 输入，不声称验了生产 Archive/Restore/所有权转移实现。
2. `TestModelProjectIndependentLateGateBindingRollsBackAllFacts`，same-user-session / same-owner-other-project / summary-header-version 三子例。先实际 ProviderUpdate+新 Secret retain+旧 Secret release+typed Model Audit+Outbox Append；在同一实际 Tx 精确查询这些事实与 prepared command，才能进入 negative check。将合法 ProjectRequest 的完整 Actor.Session、两个真实同 Owner Project 的匹配 scope/ID、合法完整 Header version 分别改动，原 deps 应由真实 Project Authority 拒绝。断言必须通过 typed constructor、到达实际 verifier、最终 NotCommitted/Forbidden/零回执，完整旧 Provider JSON、两方向精确 refs、两 Secret 和全部 effects 均回滚。原正常请求随后以同 key 成功一次。没有假 Project allow、替换 CommitResult 或放宽生产契约。

只新增自有 `probes/d09_independent_test.go`；固定 snapshot 加同一字节探针用于编译/后续动态。预期 2 顶层、5 子例。作者 13 新顶层的其它覆盖与既有回归证据按固定输入复用，不机械再跑全部 13。

## 执行与资源边界

证据/probe 固定在本目录；仅必要 backend Go、migrations、测试/fixture/script/cmd 来自固定 commit 的 773 文件，位于 `/workspace/agenteam-d09-final-build-k1jf1jcx/snapshot`，覆盖固定21源；没有复制活动主树、docs 或 web。基线指纹见 base-go-manifest.json。私有 GOCACHE/GOTMPDIR/TMPDIR 均在同一 workspace 私有根；固定 dependency modcache + GOPROXY=off/GOSUMDB=off/GOTOOLCHAIN=local，Go1.27.1，不联网。编译 `go test -c -race -count=1 -timeout=6m -tags=integration ... ./tests/model` 不执行测试或 fixture；记录 compile-input/result 和原 log。

目前无 Docker/PG/MinIO 权限，不连接现有基础设施。动态窗口由 root 另行授予；届时先记录现有资源基线、只使用 nonce/私有 socket 自有资源，沿原 script/race/count1/6m 预算执行两 probe，随后 exact-ID/名称/labels 二次 absent、基线不变、私有 process/runtime 清零。Go 编译成功不代表行为通过。

作者下一轮 43 顶层/最终完整 argv/source manifest/log 到达后按原始输出核对实际测试数、skip、每一失败及新 SQLSTATE 证据；首轮缺 SQLSTATE 的历史限制保持，不用新轮结果改写原红。若探针失败，先保留真实输入/日志并定位，只有自有 fixture/probe 的明确错误可范围内修；生产缺陷报 root 返作者，不私改生产。
