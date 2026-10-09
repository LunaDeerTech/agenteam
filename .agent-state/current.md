# D08 Project 初始化 Audit 授权库恢复点

2026-10-09；本文件只记 `/workspace/agenteam-project-initialization` 的当前模块，不作为原 Model 或全团队台账。

- 作者：`/root/model_delivery/independent_acceptance`，已由旧 Model 独验角色转为本卡 SPEC 与后续实现作者；不能独立验收自己的实现。
- 交接树/分支/基线：`/workspace/agenteam-project-initialization` / `ai/project-initialization` / 正式 main `f1c94ee5`（父/root 交接事实；本人未执行 Git）。
- 当前阶段：新 `docs/development/work-items/d08-project-initialization-audit.md` rev0 已完整写出，待独立审查；尚未编译、实现或运行真实资源。只 SPEC/current 可写；四新 Go 源须 SPEC 接受后再实施。
- 核心范围：`NewInitializationAuditAuthority(*Authority, audit.ProjectFactAuthority)`；初始化三个 Object Audit action 的同 Store 活 Tx/Project EX/Project-Creation-owner-状态事实，随后原 ctx/Tx/Entry/Key 委托完整上游 facts。普通四口保持原行为。
- 关键区分：ObjectService Audit Actor cause 是 UploadID/AttemptID/delete digest；CreationID 在 typed Service initiator metadata。Entry 没有 initialization key 参数；真实 Skill provider 必须用自己的原请求/key 调原 Project gate，再组合既有 Object 私有 witness checker。当前真实 Skill mapping provider 不存在，非 nil 接口/受控 delegate 不能证明生产组合；本库不接根或创建 HTTP，不称 Skills/Object 发布可用。
- 已读：本树 AGENTS、团队 README、architecture/backend/documentation profiles；design、Go、test-engineering、documentation、database 技能；D10 初始化设计、D08 Owner/收敛卡及验收，实际 Project/Audit/Object 接口与真实 PG fixture。
- 唯一后继写域：新 `internal/central/project/initialization_audit.go`、`initialization_audit_test.go`；新 `tests/project/initialization_audit_fixture_test.go`、`initialization_audit_test.go`；本卡/current；完整验收后的 backend README 必要事实。不改旧 audit_facts、共享契约、Skill/app/Object 停止源或迁移；00024 已留 ProjectVariables。
- 当前禁止 Git、新子席、network/socket/PG/browser/真实资源；Git 与资源由 `/root` 协调。SPEC/阶段冻结同时报 `/root` 和 `/root/model_delivery`。模型 astra ultra；priority 不可证。
- 验证边界：planned pure/race/真实 PG 与独立补集均未执行；真实 PG Project facts + controlled delegate 与真实 Object 缺 witness 负控分别计证，不造真 Skill 正向。Object Runtime join 停止项保持。
- 下一步：冻结 SPEC/current 后交未参与者审查；若规格获接受，再按四源完成可构建实现和必要离线验证，真实窗另候明确授权。不回旧 Model 工作或修改其源/卡/诊断。
