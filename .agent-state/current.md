# 多 worktree 工作流规范任务

- 目标：落实已确认的多 worktree 并行、主线程只统筹与 Git、尽早隔离真实联调，以及逐分支 checkpoint 和新环境恢复规则。
- 状态：已完成；正式规范已交付 main，剩余本任务关闭 checkpoint 由 root 保存。
- 任务分支：`ai/worktree-workflow-policy-20261010`；起始基线：`6372897d`。
- 本地目录提示：`/workspace/agenteam-workflow-policy`。目录不随 push 保存，新环境从远端任务分支重建自己的 worktree。
- 会话归属：本工作流任务由当前 root 统筹并独占本任务 Git；另一 Codex 产品会话继续独占其活动分支 `origin/ai/product-continuation`，本任务不接管该分支或修改其产品状态。
- 写入分工：core 作者负责 `AGENTS.md`、`docs/development/agent-team/README.md`、`docs/development/agent-team/team-design.md`、`docs/development/development-plan.md`；skills 作者负责 root 分配的角色配置与技能；recovery 作者负责 `.agent-state/README.md`、`docs/development/agent-team/task-template.md` 和本分支 `.agent-state/current.md`。各文件单写，root 不编写文档。
- 正式交付：25 个文件（6 份工作流文档、11 个角色 TOML、8 个技能）通过作者检查和整体独立审查，正式提交为 `280a6431`。root 已收到 `git push origin HEAD:main` 成功回执，服务端显示 `6372897d..280a6431`。
- 实际检查：角色/技能作者完成相关解析与限定检查，整体独审通过；恢复说明和模板的限定 `git diff --check`、UTF-8/LF/尾换行/围栏/尾空格、9 处相对链接及 fragment、6 个 shell 块语法检查通过。旧工作树治理版本与实际角色/技能加载说明已按独审修正并获复验通过。checkpoint 脚本经只读核对，无需修改。
- 验证限制：未运行无关产品测试，未进行真实跨云端恢复演练，也未声称当前 CLI 或已运行实例自动加载新规则。
- 远端查询：正式 main push 已成功；其后 `git ls-remote` 暂遇认证失败，尚未再次精确查询 main，待 root 重试。此查询失败不表示先前 push 失败。
- 关闭保存：此前完整草稿已通过 checkpoint `14ae4621` 成功推送；关闭 checkpoint 已在本地提交，但 push 因认证缺失失败（无法读取 Username，终端提示已禁用），尚未远端保存。凭据恢复后由 root 运行同一 save 命令重试。本状态仅保留在本任务分支，不覆盖 main 的产品 current。
- 下一步：本状态完成自查后停止写入；root 保存本次状态修正并用同一 save 命令重试关闭 checkpoint，核对实际回执；认证仍缺失时如实报告尚未远端保存，并待凭据恢复后重试。工作流任务无剩余实现，其他产品会话按自身分支继续。
