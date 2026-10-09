# 技能来源与更新

## 项目技能

| 技能 | 适用角色 | 来源与规则依据 |
| --- | --- | --- |
| [agenteam-subtree-delivery](../../../.agents/skills/agenteam-subtree-delivery/SKILL.md) | `delivery_lead`，各级负责人按需使用 | 本项目编写；从完整结果拆依赖、所有权和预算，局部集成与返修，交付可恢复片段 |
| [agenteam-design](../../../.agents/skills/agenteam-design/SKILL.md) | `architecture_worker`、`research_worker` | 本项目编写；共享设计与调研入口，引用已确认规则、开发计划和实际工程证据 |
| [agenteam-go-development](../../../.agents/skills/agenteam-go-development/SKILL.md) | `backend_worker`；data / test / platform 按需使用 | 本项目编写；引用仓库结构及对应架构文档，运行前核对实际工程 |
| [agenteam-vue-development](../../../.agents/skills/agenteam-vue-development/SKILL.md) | `frontend_worker` | 本项目编写；引用现有前端开发、组件与设计文档 |
| [agenteam-database](../../../.agents/skills/agenteam-database/SKILL.md) | `data_worker`；backend / verification 按需使用 | 本项目编写；前向迁移、同事务、完整锁集合、Unknown 确认与真实 PostgreSQL 验证 |
| [agenteam-security](../../../.agents/skills/agenteam-security/SKILL.md) | `security_reviewer`；实现者 / verification 按需使用 | 本项目编写；当前授权与跨 scope 负例、秘密输出、出站和审计边界 |
| [agenteam-test-engineering](../../../.agents/skills/agenteam-test-engineering/SKILL.md) | `test_worker`；frontend / backend / verification 按需使用 | 本项目编写；可恢复 harness、确定性故障与并发、真实浏览器及最终事实判定 |
| [agenteam-runtime-debugging](../../../.agents/skills/agenteam-runtime-debugging/SKILL.md) | `platform_worker`；实现者 / verification 按需使用 | 本项目编写；跨设备环境检查、最小复现、进程结束与按需性能测量 |
| [agenteam-documentation](../../../.agents/skills/agenteam-documentation/SKILL.md) | `documentation_worker`，实现者按需使用 | 本项目编写；将已确认决定归位，维护正式来源、引用和历史事实 |
| [agenteam-verification](../../../.agents/skills/agenteam-verification/SKILL.md) | `verification_worker`，作者自查按需使用 | 本项目编写；引用实际差异及验证规范，自查不冒充独立验收 |

十个项目技能服务十一种按需角色；技能按当前问题组合，不要求全量读取，也不授予额外写入权或独立验收身份。公共调度、所有权和风险验收集中在[团队流程](README.md)，技能保留触发条件、专业方法、交付结果和失败边界。业务规则与样式参数仍在正式文档维护，不复制成另一份规则。外部实践的采用与取舍见[团队设计依据](team-design.md)。

检查项目技能时核对 frontmatter 的 `name`、`description`、目录对应关系及引用，记录实际使用的校验方法、稳定输入和结果；当前环境支持时再核对运行时发现。静态格式通过、技能可发现、显式实例运行与 CLI 自动加载分别说明，不能互相代替。

## Vue 测试技能

- 名称：[vue-testing-best-practices](../../../.agents/skills/vue-testing-best-practices/SKILL.md)。
- 来源：[vuejs-ai/skills](https://github.com/vuejs-ai/skills)。这是社区项目。
- 固定提交：`c9d355ff23f654309dd02006be671859df0a134c`。
- 源目录：`skills/vue-testing-best-practices`。
- 安装内容：完整技能目录，保留所有 `reference/` 文件；额外保留同一提交仓库根目录的 [MIT LICENSE](../../../.agents/skills/vue-testing-best-practices/LICENSE)。
- `SKILL.md` SHA-256：`376fd21d6adfd37f7d102fa17ca099abddef16943389f37588405b013cef23b8`。
- 上游技能正文与参考文件保持原样，版本、作者等上游 frontmatter 保留。第三方技能的来源完整性和实际发现结果单独记录，不因项目技能调整而重写或自动更新上游内容。

AT-0001 通过 `skill-installer/scripts/install-skill-from-github.py` 使用 `--repo vuejs-ai/skills --path skills/vue-testing-best-practices --ref c9d355ff23f654309dd02006be671859df0a134c --dest .agents/skills --method download` 安装；当时的校验工具与实际结果保留在[初始化验证记录](verification.md)。

参考中的 Testing Library、MSW、Pinia 等是示例，不代表本项目已安装这些依赖。只选择适用于当前版本与任务的参考，不由技能示例引入新依赖或替换现有 Vitest / Vue Test Utils。

更新时作为单独工作项：先检查目标提交的入口、引用文件、许可证和新增执行内容，再更新目录、固定提交与校验值，并重新验证技能发现与受影响任务。技能不自动拉取 `main` 最新版本。

## Playwright

仓库内的 [agenteam-test-engineering](../../../.agents/skills/agenteam-test-engineering/SKILL.md) 提供跨设备可携带的浏览器方法，复用 [tests/account-captcha-web](../../../tests/account-captcha-web/) 的现存配置和 Go fixture；[package.json](../../../tests/account-captcha-web/package.json) 与锁文件固定 `@playwright/test` 为 1.56.1。执行前检查本机 Node、锁定依赖、浏览器可执行文件和隔离资源，用本地锁定 CLI，不能用 `npx latest` 替换版本。

环境已经提供的 `playwright` 技能和 wrapper 可补充使用；它们不复制入本项目，也不写入某台机器的绝对安装路径。缺少该外部技能时仍可按仓库技能使用锁定 CLI。缺依赖或浏览器时由任务所有者按当前授权恢复必要项，确实超出范围或环境受阻才逐级报告；已明确受阻的下载不反复重试。

团队初始化只验证技能发现和工具前提；真实页面、交互与持久事实在具体任务中验收。浏览器条件未满足、只列出测试、无测试命中或仅收到 HTTP 响应都不能冒充场景通过；未运行范围照实记录。新增团队能力不代表原来缺失的 Task 源码或 Model harness 已恢复。

## Codex 依据

- [子 agent 配置](https://learn.chatgpt.com/docs/agent-configuration/subagents)：项目自定义角色、默认模型、思考强度与并发上限。
- [技能发现和调用](https://learn.chatgpt.com/docs/build-skills)：仓库 `.agents/skills/`、明确触发描述与显式技能调用。
