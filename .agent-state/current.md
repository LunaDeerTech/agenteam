# Skills 持久初始化服务恢复点

- 树：`/workspace/agenteam-skills`，分支 `ai/skills-service`；基线正式 main `ca9f2d5d`。D08已正式交付，旧初始化树保持冻结，不再修改。
- 当前：D10 rev3 SPEC经Variables作者有限独审/root接受；首新4源码片段已编译及pure通过，既有P1契约/纯包不变，尚无服务/真实PG或对象结果。
- 已保存SPEC片段：`docs/development/work-items/d10-skills-initialization.md`、`docs/development/work-items/d10-skills-initialization-design.md`、本文。已由root保存/push ea13186d，设计技术段继续freeze；未自行Git操作。
- 当前可复用：实际D05 same-Store Object Audit checker；D08 original initialization四口、收敛口与初始化Audit wrapper。真实Skill exact映射provider/root尚未绑定，constructor非nil不证明真实组合。
- 共享待协调：D05初始化Service closed shape/initiator；SkillRevision+ProjectDeleted release；Project CleanupPhase现unbound；本域active初始化与删除Audit分流；生产同participant要组合届时实际启用Variables等域。root现已授权本树D05三个已列窄补口及定向测试，须单独freeze独审；Project CleanupPhase/root仍未授写。Object runtime join停止项不恢复。
- 迁移预留00027，仅本域schema。root集成24/25/26准确前缀前不做真实迁移。
- 下一步：SPEC独审期间先实现无歧义本域Store/状态/四口及exact authority，受控delegate与真实PG/真实Object/生产root验收分开；共享差异报root，不用stub冒成功。资源/热cache/真实PG或MinIO需freshgrant；不spawn，root协调交叉审查。
- 当前没有本实例运行进程/真实资源/缓存租约，未经运行的范围不得写PASS。必要失败和实际检查在本恢复点按发生追加。

## 首个持久实现片段

- 新 `internal/central/skill/{store,initialization_state,initialization_state_test}.go`：同Store接口、原Fault/UnknownAttempt及cause保留、原命令摘要、冻结builtin持久状态、精确Revision owner与真实Skill父锁；不是已可调用服务或成功initializer。
- 新 `db/migrations/00027_skills.sql`：6本域表及精确composite FK/closed状态草案，无跨域FK/查询/共享Audit CHECK；24/25/26未齐，未执行DDL。
- 首次pure session7624 actualexit1：构建误用不存在的`object.NormalizeLocks`，0行为测试；随后改为已验证且按Foundation顺序的4锁集合。
- 修后session56471 actualexit0：Go1.27.1 `go test -mod=readonly -p=1 ./internal/central/skill/... -run '^TestInitialization' -count=1 -timeout=45s -v`，3top/9子，skill0.008s；contract无匹配不算新增PASS。GOPROXY/GOSUMDBoff、GOMAXPROCS2、自有复制cache位于Variables树output/ai/project-variables/independent-http-review，未共写作者cache，无网络/真实资源。
- 该4源+主卡/本文6路径当前freeze交root WIP；SPEC技术段不变。下一新增repository/Authority与四口实现，同时按已授范围做独立D05闭集补口片段。原SQL/PG/实际对象/Unknown ACK/生产root均未验，不能据pure写完整结果通过。
