# D08 Project 初始化 Audit 授权库恢复点

2026-10-09；本文件只记 `/workspace/agenteam-project-initialization` 的当前模块，不作为原 Model 或全团队台账。

- 作者：`/root/model_delivery/independent_acceptance`，已由旧 Model 独验角色转为本卡 SPEC 与后续实现作者；不能独立验收自己的实现。
- 交接树/分支/基线：`/workspace/agenteam-project-initialization` / `ai/project-initialization` / 正式 main `f1c94ee5`（父/root 交接事实；本人未执行 Git）。
- 当前阶段：rev0 SPEC 已由 model_delivery 独立有限审查接受、root 已授四新 Go 源实施。wrapper + pure 测试形成首个可构建片段；两个 integration 新源待补，PG 未运行。
- 核心范围：`NewInitializationAuditAuthority(*Authority, audit.ProjectFactAuthority)`；初始化三个 Object Audit action 的同 Store 活 Tx/Project EX/Project-Creation-owner-状态事实，随后原 ctx/Tx/Entry/Key 委托完整上游 facts。普通四口保持原行为。
- 关键区分：ObjectService Audit Actor cause 是 UploadID/AttemptID/delete digest；CreationID 在 typed Service initiator metadata。Entry 没有 initialization key 参数；真实 Skill provider 必须用自己的原请求/key 调原 Project gate，再组合既有 Object 私有 witness checker。当前真实 Skill mapping provider 不存在，非 nil 接口/受控 delegate 不能证明生产组合；本库不接根或创建 HTTP，不称 Skills/Object 发布可用。
- 已读：本树 AGENTS、团队 README、architecture/backend/documentation profiles；design、Go、test-engineering、documentation、database 技能；D10 初始化设计、D08 Owner/收敛卡及验收，实际 Project/Audit/Object 接口与真实 PG fixture。
- 唯一后继写域：新 `internal/central/project/initialization_audit.go`、`initialization_audit_test.go`；新 `tests/project/initialization_audit_fixture_test.go`、`initialization_audit_test.go`；本卡/current；完整验收后的 backend README 必要事实。不改旧 audit_facts、共享契约、Skill/app/Object 停止源或迁移；00024 已留 ProjectVariables。
- 当前禁止 Git、新子席、network/socket/PG/browser/真实资源；离线 Go 编译/作者纯测已授权，Git 与资源由 `/root` 协调。SPEC/阶段冻结同时报 `/root` 和 `/root/model_delivery`。模型 astra ultra；priority 不可证。
- 验证边界：planned pure/race/真实 PG 与独立补集均未执行；真实 PG Project facts + controlled delegate 与真实 Object 缺 witness 负控分别计证，不造真 Skill 正向。Object Runtime join 停止项保持。
- 下一步：冻结首个可构建片段交 root 保存，继续两个新 integration 源和必要离线验证；真实窗另候明确授权。不回旧 Model 工作或修改其源/卡/诊断。

## 首个实现片段（2026-10-09）

- 冻结可保存：`internal/central/project/initialization_audit.go`、`internal/central/project/initialization_audit_test.go` 与本卡/current。原源未改；尚无 integration 两新源。
- 实际离线命令：`GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOMODCACHE=/workspace/agenteam/output/ai/model-ui-recovery/go-mod GOMAXPROCS=2 timeout 45s /workspace/toolchains/go1.27.1/bin/go test -mod=readonly -p=1 ./internal/central/project -run '^TestInitializationAuditAuthority' -count=1 -timeout=40s`，session 20939 实际 exit0 / 包 0.015s，三个新 pure top 完成。只读取现有第三方依赖缓存，未读取或改旧 Model 源。
- 原失败：默认 GOMODCACHE 缺 pgx/goose，GOPROXY=off 直接 exit1；改用已有依赖缓存的冷 `go build` session 67939 触45s上限 exit124。新测试第一次编译 session75254 exit1（三个测试符号名误用），修正后 session47037 exit1（非匹配 producer 原行为为 Forbidden，测试误期 Unbound），已按原 Authority 对照纠正；产品未为测试改 gate。
- 当前结果仅作者 pure，未 full project/race/vet/真实 PG/独立实现接受；不得宣称本卡完成。

## 四新源可构建阶段（2026-10-09）

- 已补 `tests/project/initialization_audit_fixture_test.go` 与 `initialization_audit_test.go`。普通 integration 编译 session37494 exit0；按真实 schema 修正测试构造后 session56940 exit0；精确 list 实际3top，未执行body。
- 真实 fixture 只复用正式 Project/Creation seed、两个 postgres.Store/原 migrator；受控 delegate 明示不是 Skills provider，原key通过原Project gate检查；真实Object checker只做无private witness的拒绝。包括真实持锁/poison、caller rollback、同Tx provider查询错误与三action四状态。
- 自查已在运行PG前修正两测试构造：反向Creation矛盾在原deferred FK的caller Tx内到达gate并回滚，不冒 schema错误为gate拒绝；archiving/archived/deleting用已完成Project与真实本域lifecycle输入，保持DDL原约束。不改产品或schema。
- 当前四Go源/SPEC/current阶段freeze供root保存；full project/contract pure session83500 actualexit0（0.114s/0.023s）；下一补 race/vet、integration race构建及最小PG资源命令，真实PG未授权未运行。
