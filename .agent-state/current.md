# 多 worktree 工作流规范任务

- 目标：落实已确认的多 worktree 并行、主线程只统筹与 Git、尽早隔离真实联调，以及逐分支 checkpoint 和新环境恢复规则。
- 状态：进行中；规范草稿已完成及作者自查通过，待整体独立检查与正式交付，不代表产品验收。
- 任务分支：`ai/worktree-workflow-policy-20261010`；起始基线：`6372897d`。
- 本地目录提示：`/workspace/agenteam-workflow-policy`。目录不随 push 保存，新环境须从远端任务分支重建自己的 worktree。
- 会话归属：本工作流任务由当前 root 统筹并独占 Git；不接管另一 Codex 会话的活动产品分支 `origin/ai/product-continuation`，产品进度以该分支及正式任务台账为准。
- 写入分工：core 作者负责 `AGENTS.md`、`docs/development/agent-team/README.md`、`docs/development/agent-team/team-design.md`、`docs/development/development-plan.md`；skills 作者负责 root 分配的角色配置与技能；recovery 作者负责 `.agent-state/README.md`、`docs/development/agent-team/task-template.md` 和本分支 `.agent-state/current.md`。同一文件保持单写；root 不编写文档。
- 已完成：core 的 4 个规范文件、11 个角色 TOML 和 8 个技能草稿已停止写入，作者报告相应自查通过；恢复说明与任务模板已完成，限定 `git diff --check`、UTF-8/LF/尾换行/代码围栏/尾空格、9 处相对链接及 fragment、6 个 shell 块语法检查通过。checkpoint 脚本经只读核对支持逐 worktree 调用，无需修改脚本。
- 保存状态：首个规范 checkpoint `d04e8a8f` 已由 root 确认远端保存；最新本地 checkpoint `8adae261` 保存了其他作者草稿，写本状态时其最后 push 结果待 root 核对。恢复说明、任务模板和本状态等待下一 checkpoint，不能视作全部已远端保存。
- 未完成与未验证：整体独立一致性检查与正式文档交付尚未完成。只做与文档/配置相关的检查，未运行产品测试；新环境实际恢复和线上多会话运行没有通过声明。
- 下一步：本状态和恢复两文档均停止写入，交 root 逐次保存并确认远端，再由指定审查者整体核定；缺陷交原文件 owner 修复。本状态仅保留在本任务分支，不覆盖 main 的产品 current。
- 恢复入口：先显式 fetch `main` 和全部 `ai/*`，读取最新 `origin/main` 的仓库规范；确认本任务与产品会话归属后，以本分支创建或接续独立 worktree。实际命令与限制见 `.agent-state/README.md`。
