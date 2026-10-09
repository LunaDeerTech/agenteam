# Skills 精确 Project Cleanup 当前检查点

- 工作树 `/workspace/agenteam-skills-cleanup`，分支 `ai/skills-cleanup`；正式 base `ce65714aac6eb4995a43fc427a2c77e6497470a7`。root组装76路径，2026-10-09作者只读核对 `9f5153` actual0：eaad50fd 的29个Skill Go/8集成源及4个Object初始化专用源、b37f9a49的22个Object Go/7集成源与26/27/28及D05卡、本Skills树最新两D10卡逐字一致；Project/Audit/Knowledge与main完全保留，无额外产品差异。此核对不是组合编译或动态接受。
- 本轮唯一writer为Skills agent；写域仅 `internal/central/skill` 必要dispatch/新cleanup实现与测试、`tests/skills` 新组合测试、本域D10卡/design及本文。Object/Project/SQL/App/D04只读；共享缺口交原owner，Git操作由root负责。不改00027 FK，不占00028，不恢复Object Runtime join停项。
- 目标按已独审§16 rev2：真实CleanupAuthority与Release/Object/Purge opaque plan、当前Project Cleaning gate及精确parent/cause/全锁、原子关闭serving与Release、原预算内真实物理终局/私有Audit、32历史推进及D05四anchor与Skill五核心同Tx最终删除；最终全六表空仍须当前授权/实际调用尾。禁止以unbound/stub或假proof报完整Cleanup。
- 可复用边界：原P2产品d0a16242→eaad50fd逐字未变，作者60950/58518/39205、96753、70196、61543各有限组完整PASS。P2独立02 69242/a45a4e wholePASS（2pure7sub＋真实1top4sub），01 wholeFAIL及旧4315不回填。上述没有验证本轮新Cleanup产品。
- Project当前CleanupPhase已正式ce657交付：作者3PGtop32sub＋本人独立76329/d17c95三子完整PASS，受控initializer/phase与真实权限/锁/同Tx重读边界明确；不是完整Skills/D05/root。
- D05消费基线b37（产品eda849dc）：46857/eedb1d首两top完整PASS，真实65reader/跨表32/最后四anchor与明确fixture-parent同Tx、两类COMMIT Unknown/native Audit。其65旧attempt/1001历史、PUT跨source、00028专用升级/回滚与EXPLAIN/FK成本、真实Skill最后五核心仍待验；28当前22索引候选只作连续测试依赖，不称正式交付。
- 恢复后的第一个产品WIP片段：新增 `cleanup_repository.go`（真实本域映射/关闭serving/32历史与最后五核心SQL）、`cleanup_authority.go`（exact CleanupAuthority及三种plan）、`cleanup_maintenance.go`（Claim/Checkpoint/Finalize父映射）、`cleanup_audit.go`（原ObjectDelete私有checker外层），旧 `object_authority.go/object_maintenance.go` 仅闭集dispatch。当前缺 Service.Cleanup 编排/实际call注册/原budget/完整Unknown与checkpoint及对应测试，故不能称Cleanup可用；新源尚未编译或运行。
- 环境恢复核对 `bd2318`：HEAD ba1470d6，正是2旧modified＋4新Go；本人无已提交在途session，实际/proc未见参数指向本树的进程。此事实不回填任何过去未知terminal。本次仅gofmt/差异检查，不启Go编译/PG/MinIO/socket；Object/SQL/Project不改。第一个WIP六Go＋本文freeze交root保存，继承P2/D05 PASS不外推本轮新产品。

## 完整库首实现与本域纯控

- cc68e6ab首片段基础上新增 `lifecycle_cleanup.go/lifecycle_cleanup_transaction.go/lifecycle_cleanup_checkpoint.go`，实现真正Service.Cleanup：同Project单调用准入/原2s与parent deadline、每Tx完整一次锁并集与当前CleanupPhase、已提交gate后才physical、actual返回后completed、每次最多32本域历史、D05最后purge与Skills五核心同Tx、Unknown保原attempt/cause、全六表空恢复不再调用缺父Object。没有新增Name或把单Skills当完整agent-skills-variables组合。
- `cleanup_audit.go`改私有immutable delegate并显式原样转发三个普通方法、安全日志/JSON；authority维护绑定修正正式Mapping方法和可选zero fence的primitive编码。Object/SQL/Project仍零改动。
- 新两测试 `cleanup_authority_test.go/lifecycle_cleanup_test.go`：7top/21直接子实际race0（37618/fdb3cd，1.185s），实际Service/Authority/typed plan及D05 native checker缺witness负控；Store/Project/Object事务/physical为明确doubles。覆盖当前拒绝/原Unknown、弱锁/错mapping/foreignTx、旧candidate与worker分列、closed阶段、原Audit上下文与拒绝、checkpoint闭集、gate回滚/两类受控Unknown、不重复Release、65 joined work每32、最后Object与本域整回滚/受控final Unknown、实际held physical返回前Stop不Joined。不是PG/真实D05/native Audit正向或真实COMMIT证据。
- 74993/6761ad首空-run race编译actual0；新增测试第一次8a417a actual1是测试stateID误传byte而正式helper需int，业务未运行，修后67212/a30d34首3top5子race0。该原编译失败保留，不归产品SQL。后继7top按上行真实结果。
- 全 `skill/...` race与vet 95821：测试9b286a包2.070s/contract1.288s，test actual0；vet a302b4 actual0。每次原命令同process fresh≥5GiB，最后UTC23:45:55.840359Z/5,805,240,320B；无新cache/网络/PG/socket。093dae diffcheck0，原私有go-tmp无子目录，无在途命令。
- 当前首完整库阶段可构建、作者pure通过，尚未独审。真实Skill→Project gate→D05 physical/native Audit/最后同Tx组合测试尚未写/编译/运行；28专用迁移/成本及其它D05规模缺项保持。下一优先该实际业务测试与有价值异常刺激，不扩通用harness；真实运行必须freshgrant。

实际命令均在下列固定env与本树cwd中；GOTMPDIR为本树 `output/ai/skills-cleanup/go-tmp`：

```sh
go test -race -count=1 -timeout=60s -v -run '^TestSkillCleanup' ./internal/central/skill
go test -race -count=1 -timeout=90s ./internal/central/skill/...
go vet ./internal/central/skill/...
```

## 后续固定离线环境

复用独占Variables-independent cache，不创建新GB缓存，也不写Work/VarsUI/Knowledge cache。获得编译授权时第一same-process statvfs≥5368709120B并flush实际UTC/bytes，`-p=1`；继承PATH前置Go保留Node。实际资源必须另有root fresh grant。

```sh
export PATH="/workspace/toolchains/go1.27.1/bin:$PATH"
export AGENTEAM_GO=/workspace/toolchains/go1.27.1/bin/go
export GOTOOLCHAIN=local GOENV=off GOWORK=off GOPROXY=off GOSUMDB=off GOTELEMETRY=off
export GOFLAGS='-mod=readonly -p=1' GOMAXPROCS=2
export GOMODCACHE=/workspace/agenteam/output/ai/model-ui-recovery/go-mod
export GOCACHE=/workspace/agenteam-project-variables-independent/output/ai/project-variables-independent/gocache
```
