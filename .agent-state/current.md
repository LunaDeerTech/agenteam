# D05 bounded metadata cleanup 当前检查点

- rev1 `e32b62c6`已获Variables独立有限设计接受（f4c64f实际FK控制）及Skills消费兼容核；root现授权本卡列明的Service/SQL实现，Runtime停项与迁移28不变。当前不是产品验收。
- 实施首片段：`contract/access.go`接入仅SkillRevision/ProjectDeleted的Purge operation，并逐字定向装配Skills已接受的同scope CleanupRelease分支；`contract/knowledge_cleanup_test.go`仅同步既有闭集期望，`contract/metadata_cleanup_test.go`补闭集/字段/原物理清理兼容及opaque exact operation/issuer/Tx拒例。无Service/SQL改动或真实资源。
- 实际定向race `70213/7a3267` exit0/1.032s：原env，`go test -mod=readonly -p=1 -race -count=1 -run '^(TestObjectMetadataPurge|TestKnowledgeCleanupRelease|TestAccess)' ./internal/central/object/contract`。后续实现metadata原生终局/同Tx单次预算，再接有界物理链及Stop五lane；初始四路径历史事实如下，旧“待独审”是前轮状态。

- 分支 `ai/object-metadata-cleanup`，基线正式 main `b2a7d0ab`。目标是 initialized Skills Cleanup 所需 D05 有界物理收敛/实际退休/最后同 Tx 元数据删除；不恢复 Runtime join 停项。
- 初始四路径：本文件、新卡 `docs/development/work-items/d05-bounded-metadata-cleanup.md`、`internal/central/object/contract/metadata_cleanup.go` 及 `_test.go`，已由root保存 `e61ed54d`。本次仅两docs细化为rev1；旧产品/SQL/迁移/root与纯合同源码未改。
- 实际发现：全历史 attempt 扫描；Stop LIMIT1001全历史投影循环；stop后的maintenanceAdmission拒新cleanup；main尚缺Skills分支已有Release闭集增量。另有revoked Release全cause相等拒旧AbandonedAttempt、尾部Maintenance/Inspect超出Skills许可及全量inspect。SPEC已列必要旧源与分支边界，后续先独审再接写域。
- 纯合同只新增可选typed口、Pending/Completed结果形状、总32上限、新operation常量；原AccessRequest尚不接新operation，Service未实现，没有stub或假成功。
- rev1工程方案：沿五lane改为每次32个精确主ID、固定native/work依赖及原锁；没有native的原Prepare/Issue合法准入只凭真实returned/death退休；完整projectStopPending不依赖cursor。metadata按真实00007拓扑删除至多3行PUT互引包、released leases/joined work、旧cleanup/attempt二行包，再最后四anchor；跨表32且同live Tx单次调用，不靠cascade。finalize区分本次I/O已经实际返回/已知checkpoint与外层整个operation尚未返回，metadata仍要求全部实际退休。
- 当前待独审的是完整§4 gate/原cause、§5不漏writer分页和§6最后原子性；真实SQL/索引/EXPLAIN及所有Service行为均尚未实现/验证。已列SELECT范围与外键trigger缺索引，须root另分新迁移号；00028仍属Skills预留，不先写DDL。
- 正式zero_marker永久保留，已与Skills协调措辞；最后Object/Upload/current attempt与Skills核心同Tx，Unknown保留原cause/attempt，absence不allow。
- 只离线检查获授权，无PG/socket/browser/network或自有真实资源。复用Knowledge独占cache，不建新GB缓存。

```sh
env PATH=/workspace/toolchains/go1.27.1/bin:$PATH GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOTELEMETRY=off GOMAXPROCS=2 GOMODCACHE=/workspace/agenteam/output/ai/model-ui-recovery/go-mod GOCACHE=/workspace/agenteam-knowledge/output/ai/knowledge/go-cache /workspace/toolchains/go1.27.1/bin/go test -mod=readonly -p=1 -race -count=1 -run '^TestObjectMetadataPurge' ./internal/central/object/contract
```

- 复用未变纯合同2top定向race `25403/8510e0` actual0/1.018s与包级vet `793dd1` actual0；原四文件UTF8/LF/链接 `5693d2`、diffcheck `6cd916` actual0。rev1只有文档变化，不重跑无差异Go检查；没有产品实现或真实资源证明。首次apply_patch将同一current写成delete+add，被工具整批拒绝，实际源码未变，随后按合法分步写入，不是产品编译失败。
- rev1文档检查 `55ab51` actual0（两docs UTF8/LF、两个本地链接）；`a98c68` actual0（diffcheck、限定两docs变更、Object源码相对e61ed54d无diff）。没有运行真实SQL、Object服务或ProcessGuard；全链可实现性仍待独立审查。
- 下一步：两docs稳定片段冻结供root保存，再交未参与者独审本卡＋两纯合同文件；Skills只核消费兼容。另一树Secret A已完成两mustfix返修并获Variables有限接受，技术与最终三交付docs均冻结待root正式整合，输入不混合。
