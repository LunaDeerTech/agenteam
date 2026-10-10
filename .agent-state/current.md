# 多 worktree 工作流规范任务

- 目标：落实已确认的多 worktree 并行、主线程只统筹与 Git、尽早隔离真实联调，以及逐分支 checkpoint 和新环境恢复规则。
- 状态：进行中；当前文档修改尚未完成或验证，不代表产品验收。
- 任务分支：`ai/worktree-workflow-policy-20261010`；起始基线：`6372897d`。
- 本地目录提示：`/workspace/agenteam-workflow-policy`。目录不随 push 保存，新环境须从远端任务分支重建自己的 worktree。
- 会话归属：本工作流任务由当前 root 统筹并独占 Git；不接管另一 Codex 会话的活动产品分支 `origin/ai/product-continuation`，产品进度以该分支及正式任务台账为准。
- 写入分工：core 作者负责 `AGENTS.md`、`docs/development/agent-team/README.md`、`docs/development/development-plan.md`；skills 作者负责 root 分配的角色配置与技能；recovery 作者负责 `.agent-state/README.md`、`docs/development/agent-team/task-template.md` 和本分支 `.agent-state/current.md`。同一文件保持单写；root 不编写文档。
- 已完成：独立任务状态已建立；读取现行仓库规则、团队流程、文档技能及 checkpoint 脚本，脚本支持逐 worktree 调用，无需新增机制。
- 未完成与未验证：各作者规范修订、限定链接/格式与一致性检查、正式文档交付。未运行产品测试，也未取得本次远端保存结果；Git 保存由 root 执行并核对。
- 下一步：暂停本状态文件写入并交 root 先保存 checkpoint；其余作者继续各自明确范围，完成后汇总实际检查和未完成项，由 root 再逐次保存及交付。
- 恢复入口：先显式 fetch `main` 和全部 `ai/*`，读取最新 `origin/main` 的仓库规范；确认本任务与产品会话归属后，以本分支创建或接续独立 worktree。实际命令与限制见 `.agent-state/README.md`。
