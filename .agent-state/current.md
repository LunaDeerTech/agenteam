# 当前执行检查点

- 任务：调研公开 AI 开发团队实践，增强本项目的角色、技能和可用启动配置。
- 状态：已完成；配置、技能、工具与文档已验收，正式交付目标为 `main`，推送结果以实际远端为准。
- 任务分支：`ai/strengthen-agent-team`，保留为检查点历史，不再作为活动产品任务。

## 交付结果

- `.codex/agents/` 共 11 种角色：保留原六种，新增 `delivery_lead`、`data_worker`、`security_reviewer`、`test_worker`、`platform_worker`。全部使用 `gpt-6-astra / ultra / priority` 配置。
- `.agents/skills/` 共 10 个项目技能和原有 Vue 社区技能；新增子树交付、数据库、安全、测试工程、运行时排障方法，增强 Go/Vue/verification 的操作方法与分流。
- [团队流程](../docs/development/agent-team/README.md)、任务模板和技能来源说明已同步；11 种角色按需选择，当前运行时全树仍共 7 席，不要求全部角色或固定审批链。
- [设计依据](../docs/development/agent-team/team-design.md)保存 OpenAI、Anthropic、OpenHands、SWE-agent 一手来源与采用边界，不复制外部框架或技能包。
- `scripts/ai-team.py` 提供 `check`、`start --dry-run`、`start -- PROMPT`；测试位于 `scripts/tests/test_ai_team.py`。启动显式加载本次 CLI 配置，不修改全局 trust、权限或 sandbox。

## 实际验证与边界

- 15 项工具测试通过；真实 CLI `check` 确认 8 项显式有效设置、11 个角色映射及 11 个启用的仓库技能。
- 独立审查发现父进程退出而子进程持有管道时可能卡在收尾，已修复并独立复验 app-server 和 version 两种退出边界；相关进程和 reader 实际结束，自有临时资源已清理。
- 角色 TOML、变更技能元信息、角色/技能映射、格式及跨 831 份 Markdown 的 369 个相关链接/锚点检查通过。
- 工具要求 Python 3.11+ 与 POSIX 进程组（Linux/macOS/WSL），当前不支持原生 Windows；本轮只在 Linux 实测。
- 默认项目配置在本机仍因未信任而禁用；显式启动覆盖已验证。目录角色发现与同文件去重依据官方文档/加载器，本轮未运行真实角色实例或验证请求级 Fast 服务档位；未启动模型任务。
- 未修改产品实现、运行 Go/Vue/真实基础设施产品测试或解除历史停止项。产品状态仍见[任务台账](../docs/development/agent-team/tasks.md)。

## 后续接续

1. 本团队增强任务已完成，不重复实施。新任务先按[跨设备说明](README.md)恢复对应活动分支，或从 `main` 开始新的 `ai/<task>` 分支并更新此文件。
2. 新 CLI 会话可从仓库根运行 `python3 scripts/ai-team.py start -- '接续当前任务，先读取 .agent-state/current.md'`，启动时自动预检；仅诊断使用 `check`。已有 API 会话按工具实际支持字段及团队流程派工。
3. 修改启动工具时运行 `python3 -B -m unittest discover -s scripts/tests -p 'test_ai_team.py'`；相关输入未变时复用已有结论。原自动保存工具未修改，无需重跑其测试。
