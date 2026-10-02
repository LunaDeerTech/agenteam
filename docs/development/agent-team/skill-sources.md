# 技能来源与更新

## 项目技能

| 技能 | 来源与规则依据 |
| --- | --- |
| [agenteam-vue-development](../../../.agents/skills/agenteam-vue-development/SKILL.md) | 本项目编写；引用现有前端开发、组件与设计文档 |
| [agenteam-go-development](../../../.agents/skills/agenteam-go-development/SKILL.md) | 本项目编写；引用仓库结构及对应架构文档，运行前核对实际工程 |
| [agenteam-verification](../../../.agents/skills/agenteam-verification/SKILL.md) | 本项目编写；引用团队流程、实际差异及项目验证规范 |

项目技能入口只保留触发条件、必要边界、按需读取来源、执行检查和交付要求。业务规则与样式参数仍在原文档维护，不复制成另一份规则。

## Vue 测试技能

- 名称：[vue-testing-best-practices](../../../.agents/skills/vue-testing-best-practices/SKILL.md)。
- 来源：[vuejs-ai/skills](https://github.com/vuejs-ai/skills)。这是社区项目。
- 固定提交：`c9d355ff23f654309dd02006be671859df0a134c`。
- 源目录：`skills/vue-testing-best-practices`。
- 安装内容：完整技能目录，保留所有 `reference/` 文件；额外保留同一提交仓库根目录的 [MIT LICENSE](../../../.agents/skills/vue-testing-best-practices/LICENSE)。
- `SKILL.md` SHA-256：`376fd21d6adfd37f7d102fa17ca099abddef16943389f37588405b013cef23b8`。
- 上游技能正文与参考文件保持原样。版本、作者等上游 frontmatter 保留；项目新增技能使用本机 skill-creator 校验器检查，第三方技能另外核对来源完整性及 Codex 实际发现结果。

本次通过 `skill-installer/scripts/install-skill-from-github.py` 使用 `--repo vuejs-ai/skills --path skills/vue-testing-best-practices --ref c9d355ff23f654309dd02006be671859df0a134c --dest .agents/skills --method download` 安装。

参考中的 Testing Library、MSW、Pinia 等是示例，不代表本项目已安装这些依赖。只选择适用于当前版本与任务的参考，不由技能示例引入新依赖或替换现有 Vitest / Vue Test Utils。

更新时作为单独工作项：先检查目标提交的入口、引用文件、许可证和新增执行内容，再更新目录、固定提交与校验值，并重新验证技能发现与受影响任务。技能不自动拉取 `main` 最新版本。

## Playwright

使用当前开发环境已经安装的 `playwright` 技能，通过技能目录定位其 `SKILL.md` 与 wrapper。它不复制入本项目，也不在仓库写入某台机器的绝对安装路径。

团队初始化只验证可发现性和工具前提；真实页面与交互验收应在具体前端任务中执行。其他环境没有该技能或浏览器能力时，执行者报告缺失项，由主线程决定如何补齐，不能把缺失的检查标记为通过。

## Codex 依据

- [子 agent 配置](https://learn.chatgpt.com/docs/agent-configuration/subagents)：项目自定义角色、默认模型、思考强度与并发上限。
- [技能发现和调用](https://learn.chatgpt.com/docs/build-skills)：仓库 `.agents/skills/`、明确触发描述与显式技能调用。
