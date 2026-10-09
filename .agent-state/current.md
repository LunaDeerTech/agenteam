# Project Skills CleanupPhase 当前检查点

- 树 `/workspace/agenteam-project-skills-cleanup`，分支 `ai/project-skills-cleanup-authority`，基线正式 main `29dd4298`；作者 `/root/work_ui`，Git 由 root 负责。
- [工作项](../docs/development/work-items/d08-project-skills-cleanup-authority.md)固定仅 SkillsParticipant/CleanupPhase 的真实当前 Project 门禁。生产唯一写域 lifecycle_authority.go；新相邻/PG测试、本卡/current 属本任务，无迁移、无 D05/contract/lifecycle_store/旧 Stop Inspect 写权。
- 首产品片段已落实既有 rev2 §16.3，复用原 cause/manifest 核验后在同 Tx/Project 锁重读严格 operation/participants，精确检查 domains/当前 required或pending/全部依赖 completed。尚无独立或 PG 接受；首 apply_patch 重复 current target 导致校验拒绝未落盘，已修编辑方式。
- 未执行真实 PG/浏览器/socket，无在途命令。共享磁盘暂停已由 root 仅解除本包小检查，大 integration 候选仍不编。
- 后继离线 Go1.27.1/local/off；GOMODCACHE=`/workspace/agenteam/output/ai/model-ui-recovery/go-mod`，GOCACHE=`/workspace/agenteam-work-ui/output/ai/work-owner-planning-ui/implementation/gocache`，复用本人无其它 writer 缓存，不创建 GB 级新缓存。
- 新相邻测试两 top 与 `tests/project/skills_cleanup_authority_test.go` 三个 PG top 已落盘并 gofmt/diffcheck；PG 源仍未编译/执行。PG 源复用正式 BeginDelete，明确 controlled Cleaning/manifest 前驱事实，实际 SH/EX/foreign/ended/取消与 Owner writer 被 SH 阻塞均有矩阵；不把受控 initializer/participant 当实际 Skills/D05工作。首静核发现测试 manifest 的 Outbox 必须显式依赖所有新增参与者，运行前已补齐。
- 作者小范围 race 89499→0cff7b actual0，3top/19sub（两新 pure＋原 LifecycleAuthorityConstructionAndCompatibility），1.024s；同包 vet65938→750b69 actual0。首次同进程 fresh statvfs=5,895,520,256B、固定Go/off/-p1/原缓存后启动，无新cache或PG资源；后继可用空间5,792,038,912B。命令：`go test -race -count=1 -timeout=60s -v -run '^(TestSkillsCleanup|TestLifecycleAuthority)' ./internal/central/project`；`go vet ./internal/central/project`。
- 产品与两新测试冻结交 Skills 未参与者接续独审。下一大 integration 编译/精确发现与 fresh PG 另排，3入口为 TestProjectSkillsCleanupCurrentFacts、TestProjectSkillsCleanupTransactions、TestProjectSkillsCleanupCurrentFactsRemainLocked。Work09/Timeline 原冻结候选保持。
