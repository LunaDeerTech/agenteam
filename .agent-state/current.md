# 多 worktree 工作流规范任务

- 目标：落实已确认的多 worktree 并行、主线程只统筹与 Git、尽早隔离真实联调，以及逐分支 checkpoint 和新环境恢复规则。
- 状态：进行中；当前文档修改尚未完成或验证，不代表产品验收。
- 任务分支：`ai/worktree-workflow-policy-20261010`；起始基线：`6372897d`。
- 本地目录提示：`/workspace/agenteam-workflow-policy`。目录不随 push 保存，新环境须从远端任务分支重建自己的 worktree。
- 会话归属：本工作流任务由当前 root 统筹并独占 Git；不接管另一 Codex 会话的活动产品分支 `origin/ai/product-continuation`，产品进度以该分支及正式任务台账为准。
- 写入分工：core 作者负责 `AGENTS.md`、`docs/development/agent-team/README.md`、`docs/development/development-plan.md`；skills 作者负责 root 分配的角色配置与技能；recovery 作者负责 `.agent-state/README.md`、`docs/development/agent-team/task-template.md` 和本分支 `.agent-state/current.md`。同一文件保持单写；root 不编写文档。
- 已完成：独立任务状态与 AGENTS 首稿已由 root 保存至远端 checkpoint `d04e8a8f`；11 个角色 TOML 草稿已停止写入，作者完成解析及限定 diff 格式自查。读取 checkpoint 脚本并完成只读核对，支持逐 worktree 调用，无需新增机制。
- 未完成与未验证：核心流程、恢复文档/模板和技能修订仍在进行；11 个角色草稿尚待本轮远端保存。总体一致性和完整文档检查未完成，整体未验；未运行产品测试。Git 保存结果由 root 执行并逐次核对。
- 下一步：暂停本状态文件写入，交 root 保存 11 个角色草稿及本状态；其余作者继续明确范围，完成后汇总实际检查和未完成项，再保存并交付。本状态仅保留在本任务分支，不覆盖 main 的产品 current。
- 恢复入口：先显式 fetch `main` 和全部 `ai/*`，读取最新 `origin/main` 的仓库规范；确认本任务与产品会话归属后，以本分支创建或接续独立 worktree。实际命令与限制见 `.agent-state/README.md`。
