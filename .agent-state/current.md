# 新 Agent Skills 初始化

- 工作树 `/workspace/agenteam-agent-skill-initialization`，分支 `ai/agent-skill-initialization`；coordination 唯一写 Skills 实现、相邻清理门、迁移 `00034_agent_skill_assignments.sql`。Agent 核心/00032 与 Tool Registry/00033 分属 content/cleanup；Git 由 root 执行。
- 已有首片段：Skills 自有初始 assignment/receipt 值、两表 repository 与迁移；基础契约/行校验测试已写。借用 Agent consumer-owned 四契约文件，本文域不修改这些接口。
- 新 `NewAgentInitializer` 独立绑定原 Skills Authority 与真实 Agent Creation Authority；Discover 冻结私有 issuer/原请求/完整锁/已发布 protected AddSkills 映射；final 只在同 Store 的原活 Tx 中重验当前 Owner Read/Mutate、未发布新建 Agent witness 和 Skill 映射，写同事务初始集合。默认启用写一条 assignment；显式 false 仍走全部 provider/权限门，仅保 sequence=1 的空初始 head。
- 新表事实使现 Cleanup 在 gate/Release/物理删除之前 fail closed，空检查同时纳入两表。尚无 Agent 删除提供方，任何未退役 head（包括 disabled）都不能当空集清掉。
- 必要纯测试已形成：同事务重入身份、false、当前权限/外来计划/外来或已结束 Tx/缺锁/未发布 witness/stale Skill、原 callback 回滚及 Unknown discovery；均明确使用 Store/Agent contract doubles。仅 gofmt 与 whitespace 检查已完成，尚未启动 Go、执行迁移或真实 PG。
- 下一步：首片段后继续本轮 core/cleanup 有限静审，待 root 授共享 Go 窗口后定向 race/vet。真实成功链须组合 canonical Agent writer、完整迁移前缀与真实 publication，不用假 authority 或 SQL 手种替代。普通运行 Assign/Remove、RuntimeSink、生产 Agent/F1、install-skill Registry 及完整 participant/initializer 仍未绑定；E01 未开始。
