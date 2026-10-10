# Skills P2 / 00027 有限交付装配

- 树 `/workspace/agenteam-skills-p2-delivery`，分支 `ai/skills-p2-delivery`，正式 base `fb6ab7f492850bf1d3c025a59acbff381312a989`。root 已组装并暂存47技术路径；Git操作仍仅root。本人写域四个正式docs与本文，技术源除真实必要修复外保持冻结，当前零技术追加修改。
- 来源：eaad50fdb08e248b850a65c74f49d3aabf73b682 的40个新增，独立5bb671868ce5b4f9f0ff18fde0a692d432392911 的PG文件与pure源字节移位，另五个共享Object窄hunk。清单源为已存fb321664的 `.agent-state/skills-cleanup/p2-delivery.md`，不把Cleanup树产品、00028、Runtime或root接入带入本树。
- c0ff89本人实际0：42新源逐字与固定来源一致、5共享diff等于fb6→eaad既定hunks、其余技术零变、迁移1..27连续。Runner未参与者d154f6独立实际0，另核保护域／commitproxy／1..26原字节，暂无装配mustfix；待本轮验证与文档收口。
- 旧真实证据按主卡固定版本组合复用，所有旧FAIL保留，不把本次list当业务PASS；main已有B02/Audit/Project与00026不被旧整文件覆写。47技术与四docs停止写，等待独立文档收口；主卡/design不导入新Cleanup规格，README保main其余，tasks仅D10一行。

## 最小离线验证（原session 33262已终态）

cwd本树；命令同process首UTC2026-10-10T00:49:35.176886Z、available5,409,759,232B≥5,368,709,120B，固定Go1.27.1。没有启动PG、MinIO、socket或root。

```sh
export PATH=/workspace/toolchains/go1.27.1/bin:$PATH
export AGENTEAM_GO=/workspace/toolchains/go1.27.1/bin/go
export GOTOOLCHAIN=local GOENV=off GOWORK=off GOPROXY=off GOSUMDB=off GOTELEMETRY=off
export GOMAXPROCS=2 GOFLAGS='-mod=readonly -p=1'
export GOCACHE=/workspace/agenteam-project-variables-independent/output/ai/project-variables-independent/gocache
export GOMODCACHE=/workspace/agenteam-runner-control/output/ai/runner-control/go-mod
export GOTMPDIR=$PWD/output/ai/skills-p2-delivery/go-tmp
export TMPDIR=$PWD/output/ai/skills-p2-delivery/tmp
export XDG_CONFIG_HOME=$PWD/output/ai/skills-p2-delivery/go-config
```

GOCACHE是本Skills原独占热cache；只读复用Runner完整modcache（含正式websocket），不写Runner GOCACHE，不新建GBcache。私有TMP先mkdir再go version。

顺序命令与每项actualexit写 `output/ai/skills-p2-delivery/validation/results.json`，原日志同目录：

```sh
go test -json -race -count=1 -timeout=60s ./internal/central/skill/...
go test -json -race -count=1 -timeout=60s -run '^Test(SkillInitialization|SkillCleanupRelease|KnowledgeCleanupRelease)' ./internal/central/object ./internal/central/object/contract
go test -json -race -count=1 -timeout=60s -run '^Test(InitializationAuditAuthority|KnowledgeAudit|ObjectAudit|ProjectVariableAudit|SecretVariableAudit|ProjectVariableProjectGate)' ./internal/central/project
go vet ./internal/central/skill/... ./internal/central/object ./internal/central/object/contract
go test -race -tags=integration -c -o output/ai/skills-p2-delivery/skill-p2-delivery.test ./tests/skills
output/ai/skills-p2-delivery/skill-p2-delivery.test -test.run='^$' -test.list='^TestSkill(InitializationPersistence|InitializationPublicationRollback|InitializationCommitRecovery|Migration|InitializationAdmissionUnknown|OwnerMetadataCurrentAuthority|LifecycleStopPersistence|ObjectInitializationPublication|IndependentP2ConfirmationAndPackage)$'
```

本人6f5fcd已取得前三项actual0：Skills42top/183sub（包2.094s/contract1.240s）、Object5top/10sub（1.020s/1.031s）、Project13top/57sub（1.066s），均0skip；后继25555d已取得原outeractual0：vet0/integration-c0/九top精确list0。不重复旧PG或创建新资源。首次路径查找误查不存在的migrations README/tasks目录已按清单正式路径纠正，未当验证通过。

- 整串末UTC2026-10-10T00:51:32.940848Z、available5,070,692,352B；原候选 `output/ai/skills-p2-delivery/skill-p2-delivery.test` 为33,061,778B，SHA256 `76128e387cf63559379e4e5f30139becba3a868713eb66dbd891751c965f8b72`。该串只有启动前fresh门，中途未另采，不能声称integration-c单独起点≥5GiB；root随后要求下一build重新fresh时本串已终态。没有下一build、没有原在途Go或测试资源；所有新大产物暂停，待root回收协调。
- 4docs/current最终收口：主卡固定作者/独立实际版本组合与旧4315/P2-01 FAIL，design仅P2 §§10–15并校正已正式上游，未导入后继Cleanup节/00028；README只有限P2说明与原初始化provider一句校正，tasks仅D10行。此WIP恢复点不列入正式51路径，root最终交付负责移交main。新Cleanup历史另树b5bb3ae8六源已Model静态有限接受，不混入本树结果。
- 文档自查首298660因本树稀疏未展开P1已跟踪evidence而文件系统查找失败，改核正式base Git对象确认原链接存在；随后014fb8发现新增design EOF空行，已去除。未修改历史P1证据或技术源，最终链接/差异检查另列实际结果。
