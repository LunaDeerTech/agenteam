# Skills 持久初始化服务恢复点

- 树：`/workspace/agenteam-skills`，分支 `ai/skills-service`；基线正式 main `ca9f2d5d`。D08已正式交付，旧初始化树保持冻结，不再修改。
- 当前：D10 rev3 P2 SPEC作者片段；既有P1契约/纯包不变，尚无新增Go/DDL/编译/真实PG或对象结果。
- 本次唯一已写：`docs/development/work-items/d10-skills-initialization.md`、`docs/development/work-items/d10-skills-initialization-design.md`、本文。准备冻结交root保存与未参与者独审，未自行Git操作。
- 当前可复用：实际D05 same-Store Object Audit checker；D08 original initialization四口、收敛口与初始化Audit wrapper。真实Skill exact映射provider/root尚未绑定，constructor非nil不证明真实组合。
- 共享待协调：D05初始化Service closed shape/initiator；SkillRevision+ProjectDeleted release；Project CleanupPhase现unbound；本域active初始化与删除Audit分流；生产同participant要组合届时实际启用Variables等域。root尚未授权本实例改共享源。Object runtime join停止项不恢复。
- 迁移预留00027，仅本域schema。root集成24/25/26准确前缀前不做真实迁移。
- 下一步：SPEC独审期间先实现无歧义本域Store/状态/四口及exact authority，受控delegate与真实PG/真实Object/生产root验收分开；共享差异报root，不用stub冒成功。资源/热cache/真实PG或MinIO需freshgrant；不spawn，root协调交叉审查。
- 当前没有本实例运行进程/真实资源/缓存租约，未经运行的范围不得写PASS。必要失败和实际检查在本恢复点按发生追加。
