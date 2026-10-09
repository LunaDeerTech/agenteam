# D05 bounded metadata cleanup 当前检查点

- 分支 `ai/object-metadata-cleanup`，基线正式 main `b2a7d0ab`。目标是 initialized Skills Cleanup 所需 D05 有界物理收敛/实际退休/最后同 Tx 元数据删除；不恢复 Runtime join 停项。
- 初始四路径：本文件、新卡 `docs/development/work-items/d05-bounded-metadata-cleanup.md`、`internal/central/object/contract/metadata_cleanup.go` 及 `_test.go`。SPEC rev0 草案，旧产品/SQL/迁移/root 未改。
- 实际发现：全历史 attempt 扫描；Stop LIMIT1001全历史投影循环；stop后的maintenanceAdmission拒新cleanup；main尚缺Skills分支已有Release闭集增量。SPEC已列必要旧源、真实索引缺口与当前语义，后续先独审再接写域。
- 纯合同只新增可选typed口、Pending/Completed结果形状、总32上限、新operation常量；原AccessRequest尚不接新operation，Service未实现，没有stub或假成功。
- 必须继续细化：Stop主lane/native依赖包精确分页与不漏writer；transfer/staging互引包的FK/锁/总额；最小索引与真实EXPLAIN。没有分配新迁移号，00028仍属Skills预留。当前草案不冒充已独审可实施SPEC。
- 正式zero_marker永久保留，已与Skills协调措辞；最后Object/Upload/current attempt与Skills核心同Tx，Unknown保留原cause/attempt，absence不allow。
- 只离线检查获授权，无PG/socket/browser/network或自有真实资源。复用Knowledge独占cache，不建新GB缓存。

```sh
env PATH=/workspace/toolchains/go1.27.1/bin:$PATH GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOTELEMETRY=off GOMAXPROCS=2 GOMODCACHE=/workspace/agenteam/output/ai/model-ui-recovery/go-mod GOCACHE=/workspace/agenteam-knowledge/output/ai/knowledge/go-cache /workspace/toolchains/go1.27.1/bin/go test -mod=readonly -p=1 -race -count=1 -run '^TestObjectMetadataPurge' ./internal/central/object/contract
```

- 当前实际检查：纯合同2top定向race `25403/8510e0` actual0/1.018s；包级vet `793dd1` actual0；本卡链接/四文件UTF8/LF `5693d2` actual0，diffcheck `6cd916` actual0。四路径已冻结供root保存；没有产品实现或真实资源证明。首次apply_patch将同一current写成delete+add，被工具整批拒绝，实际源码未变，随后按合法分步写入，不是产品编译失败。
- 下一步：保存后继续Stop及互引包具体算法细化，再交未参与者独审。Secret A 两独审mustfix在另一树已按root授权窄修并冻结交Variables续审，源码输入互不混合。
