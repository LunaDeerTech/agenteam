# 多 worktree 工作流规范任务

- 目标：落实已确认的多 worktree 并行、主线程只统筹与 Git、尽早隔离真实联调，以及逐分支 checkpoint 和新环境恢复规则。
- 状态：进行中；规范草稿已完成及作者自查通过，待整体独立检查与正式交付，不代表产品验收。
- 任务分支：`ai/worktree-workflow-policy-20261010`；起始基线：`6372897d`。
- 本地目录提示：`/workspace/agenteam-workflow-policy`。目录不随 push 保存，新环境须从远端任务分支重建自己的 worktree。
- 会话归属：本工作流任务由当前 root 统筹并独占 Git；不接管另一 Codex 会话的活动产品分支 `origin/ai/product-continuation`，产品进度以该分支及正式任务台账为准。
- 写入分工：core 作者负责 `AGENTS.md`、`docs/development/agent-team/README.md`、`docs/development/agent-team/team-design.md`、`docs/development/development-plan.md`；skills 作者负责 root 分配的角色配置与技能；recovery 作者负责 `.agent-state/README.md`、`docs/development/agent-team/task-template.md` 和本分支 `.agent-state/current.md`。同一文件保持单写；root 不编写文档。
- 已完成：正式交付范围的 25 个文件均已完成作者检查；checkpoint 脚本经只读核对支持逐 worktree 调用，无需修改。整体独立审查已完成，唯一 must-fix 为旧工作树治理版本与实际角色/技能加载之间的恢复说明缺口。
- 保存状态：root 已确认 checkpoint `427d2802` 正常推送；当前恢复说明补充与本状态等待下一次保存，不将尚未推送编辑视为远端已保存。
- 未完成与未验证：恢复说明正修正上述独审问题，尚待审查者复验确认，未作最终 PASS 声明，未正式合入 main。只做与文档/配置相关的检查，未运行产品测试；实际新环境恢复、CLI 刷新或线上多会话运行没有通过声明。
- 下一步：补充既有会话治理核对、明确范围同步和实际加载限制后自查并停写，交原审查者复验；root 保存和完成正式交付。本状态仅保留在本任务分支，不覆盖 main 的产品 current。
- 恢复入口：先显式 fetch `main` 和全部 `ai/*`，读取最新 `origin/main` 的仓库规范；确认本任务与产品会话归属后，以本分支创建或接续独立 worktree。实际命令与限制见 `.agent-state/README.md`。
