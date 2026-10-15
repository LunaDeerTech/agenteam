# 技能来源与更新

## 项目技能

| 技能 | 适用角色 | 来源与规则依据 |
| --- | --- | --- |
| [agenteam-subtree-delivery](../../../.agents/skills/agenteam-subtree-delivery/SKILL.md) | `delivery_lead`，各级负责人按需使用 | 本项目编写；从完整结果拆依赖、所有权和预算，局部集成与返修，交付可恢复片段 |
| [agenteam-design](../../../.agents/skills/agenteam-design/SKILL.md) | `architecture_worker`、`research_worker` | 本项目编写；共享设计与调研入口，引用已确认规则、开发计划和实际工程证据 |
| [agenteam-go-development](../../../.agents/skills/agenteam-go-development/SKILL.md) | `backend_worker`；data / test / platform 按需使用 | 本项目编写；引用仓库结构及对应架构文档，运行前核对实际工程 |
| [agenteam-vue-development](../../../.agents/skills/agenteam-vue-development/SKILL.md) | `frontend_worker` | 本项目编写；引用现有前端开发、组件与设计文档 |
| [agenteam-frontend-redesign](../../../.agents/skills/agenteam-frontend-redesign/SKILL.md) | `frontend_designer`、`frontend_worker`、`frontend_visual_reviewer`；verification / test 按阶段使用 | 本项目编写；以 Linear 官方产品参考及设计规范 v2 串联完整页面样板、Vue 迁移与独立浏览器视觉验收，不复制设计参数 |
| [agenteam-database](../../../.agents/skills/agenteam-database/SKILL.md) | `data_worker`；backend / verification 按需使用 | 本项目编写；前向迁移、同事务、完整锁集合、Unknown 确认与真实 PostgreSQL 验证 |
| [agenteam-security](../../../.agents/skills/agenteam-security/SKILL.md) | `security_reviewer`；实现者 / verification 按需使用 | 本项目编写；当前授权与跨 scope 负例、秘密输出、出站和审计边界 |
| [agenteam-test-engineering](../../../.agents/skills/agenteam-test-engineering/SKILL.md) | `test_worker`；frontend / backend / verification 按需使用 | 本项目编写；可恢复 harness、确定性故障与并发、真实浏览器及最终事实判定 |
| [agenteam-runtime-debugging](../../../.agents/skills/agenteam-runtime-debugging/SKILL.md) | `platform_worker`；实现者 / verification 按需使用 | 本项目编写；跨设备环境检查、最小复现、进程结束与按需性能测量 |
| [agenteam-documentation](../../../.agents/skills/agenteam-documentation/SKILL.md) | `documentation_worker`，实现者按需使用 | 本项目编写；将已确认决定归位，维护正式来源、引用和历史事实 |
| [agenteam-verification](../../../.agents/skills/agenteam-verification/SKILL.md) | `verification_worker`、`frontend_visual_reviewer`，作者自查按需使用 | 本项目编写；引用实际差异及验证规范，自查不冒充独立验收 |

十个原有工程技能与新增的前端重设计编排服务十三种按需角色；技能按当前问题组合，不要求全量读取，也不授予额外写入权或独立验收身份。公共调度、所有权和风险验收集中在[团队流程](README.md)，技能保留触发条件、专业方法、交付结果和失败边界。业务规则与样式参数仍在正式文档维护，不复制成另一份规则。外部实践的采用与取舍见[团队设计依据](team-design.md)。

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

## 前端设计与 UX 审查技能

2026-10-14 按用户要求补充两项可随仓库携带的技能。现有 `agenteam-design` 负责工程规格，`agenteam-vue-development` 负责前端实现，`vue-testing-best-practices` 负责测试方法；新增入口补充视觉层级、界面文案、交互与无障碍审查。两项 `SKILL.md` 均由本项目编写适配，外部原文作为本地固定参考完整保留；不将适配入口称作上游原版。

| 项目入口 | 固定参考与许可证 | 固定提交与原文 SHA-256 |
| --- | --- | --- |
| [frontend-design](../../../.agents/skills/frontend-design/SKILL.md) | Anthropic 官方 [anthropics/skills](https://github.com/anthropics/skills) 的 `skills/frontend-design/SKILL.md`，本地[原文](../../../.agents/skills/frontend-design/reference/frontend-design.md)及同目录 [Apache-2.0 许可](../../../.agents/skills/frontend-design/LICENSE.txt) | 提交 `dbd4588f9e1033efb41dad4bef2f7947c8993d44`；原文 `d91970639e9f5c37682ac7ab60094d35f1c7c1f38d731bd56396563aee10c1d3` |
| [web-design-guidelines](../../../.agents/skills/web-design-guidelines/SKILL.md) | Vercel Labs 官方组织下 [web-interface-guidelines](https://github.com/vercel-labs/web-interface-guidelines) 的 `command.md`，本地[原文](../../../.agents/skills/web-design-guidelines/reference/web-interface-guidelines.md)及 [MIT 许可](../../../.agents/skills/web-design-guidelines/LICENSE)，保留 `Copyright (c) 2025 Vercel Labs` | 提交 `434b7f91364665f2f733b310ec54809bf8f37937`；原文 `d246b026f4f29b5823a9cc857f9edf3d2507002e055e32040cadeaf3b38e0234` |

来源通过 GitHub HTTPS API 核对组织仓库、提交、目录与许可，并通过固定提交的 `raw.githubusercontent.com` 地址下载必要文本；使用环境默认代理与 TLS 校验。核查时两个来源均未归档或禁用，仓库最近推送分别为 2026-10-09 与 2026-10-05；这是仓库维护信号，不代表每条设计建议已经适用于本项目。

候选 [vercel-labs/agent-skills](https://github.com/vercel-labs/agent-skills) 的 `skills/web-design-guidelines/SKILL.md` 在提交 `063bee94c3f4df8453406c830b0a7df0f2860278` 要求每次获取远端 `main/command.md`，且该提交文件树未发现许可文件。未复制此 wrapper；采用其指向的、有明确 MIT 许可的规则来源并编写本地适配入口。该候选核查时未归档或禁用，仓库最近推送为 2026-08-28。

用户选择的 Linear 官方产品参考和[设计规范 v2](../../frontend-design/redesign/target-design.md)是重设计依据；旧布局、样式、token / Ui 组件用于迁移核对，不锁定新设计。公开 Linear 页面是产品设计参考，不是本项目获得了一套完整官方设计系统或官方 token。Anthropic 的品牌探索、字体/调色板重选及 hero 建议不覆盖已确认风格；Vercel 的 React/Tailwind 示例、英文大小写、统一关闭自动填充、全量 URL 同步和固定虚拟化阈值按 Vue 与业务契约解释。两个入口明确技术信息渐进展开、状态反馈和真实证据边界。

2026-10-14 的安装仅包含上述六个技能文本/许可文件；逐文审读未发现收集数据、上传源码或 Secret、更改全局权限、运行远端脚本的要求。没有执行 `npx`、远端安装器、依赖升级或全局配置修改。原文中的产品/库示例不授权安装依赖，参考更新须作为单独工作项固定新提交、重读内容与许可后再替换，不在日常审查中动态拉取规则。

入口位于 `.agents/skills/<name>/SKILL.md`，并由 [Vue 开发技能](../../../.agents/skills/agenteam-vue-development/SKILL.md)按任务引用。静态检查使用 PyYAML 解析两项入口的 frontmatter，核对名称与目录、必要字段、UTF-8/LF、相对链接与固定参考字节校验值；目录扫描能列出两项技能。该检查证明仓库入口和引用存在，不证明当前会话或 CLI 自动加载，也不代表产品 UI 已验收。实际使用按具体任务记录。

## 前端重设计编排

2026-10-15 根据用户“对照 Linear 重新制定设计规范”的要求，新增本项目编写的 [agenteam-frontend-redesign](../../../.agents/skills/agenteam-frontend-redesign/SKILL.md)。它将[设计规范 v2](../../frontend-design/redesign/target-design.md)、当前[页面蓝图](../../frontend-design/redesign/page-blueprints.md)和[执行阶段](../../frontend-design/redesign/implementation-plan.md)连接到已有技能；不引入外部脚本、依赖或新的设计参数副本。

| 阶段 | 已有能力与本次补齐 |
| --- | --- |
| 规范 / 完整页面样板 | 复用 `frontend-design`、`web-design-guidelines`；新编排要求先验证项目列表、任务工作台、复杂设置三种完整页面，再提炼共享层 |
| Vue / 组件 / 页面迁移 | 复用 `agenteam-vue-development`；写测试时复用 `vue-testing-best-practices`；现有业务状态与接口继续按正式契约执行 |
| 浏览器与独立验收 | 复用 `agenteam-test-engineering`、`agenteam-verification`、`web-design-guidelines`；新编排补充按模板的版式门槛、同数据/状态/viewport/theme 对照和失败返修 |

上述六项已有技能本次没有重装，上游固定参考与许可保持原样；仅最小调整四个项目入口及前端/验证角色的按需引用，让旧视觉规则退为迁移输入。`vue-best-practices` 和 `skill-creator` 未在本轮检查的仓库及本机技能目录中找到，没有将其写为已安装能力，也不将其作为阶段前置条件；本轮使用已有文档技能核对来源、frontmatter、链接与配置。

角色接入使用 [frontend_designer](../../../.codex/agents/frontend-designer.toml) 承担完整页面设计与授权样板，使用 [frontend_visual_reviewer](../../../.codex/agents/frontend-visual-reviewer.toml) 承担独立浏览器视觉/交互审查；已有开发、测试和通用验证角色继续按需组合。两角色均复用本节技能，没有安装另一套技能或增加阶段审批。

新技能位于仓库标准发现目录 `.agents/skills/`。目录存在、静态解析、运行时发现及实例实际调用分别举证；这些事实都不能保证视觉品质，真实页面效果与交互仍须按任务独立验收。

## Playwright

仓库内的 [agenteam-test-engineering](../../../.agents/skills/agenteam-test-engineering/SKILL.md) 提供跨设备可携带的浏览器方法，复用 [tests/account-captcha-web](../../../tests/account-captcha-web/) 的现存配置和 Go fixture；[package.json](../../../tests/account-captcha-web/package.json) 与锁文件固定 `@playwright/test` 为 1.56.1。执行前检查本机 Node、锁定依赖、浏览器可执行文件和隔离资源，用本地锁定 CLI，不能用 `npx latest` 替换版本。

环境已经提供的 `playwright` 技能和 wrapper 可补充使用；它们不复制入本项目，也不写入某台机器的绝对安装路径。缺少该外部技能时仍可按仓库技能使用锁定 CLI。缺依赖或浏览器时由任务所有者按当前授权恢复必要项，确实超出范围或环境受阻才逐级报告；已明确受阻的下载不反复重试。

团队初始化只验证技能发现和工具前提；真实页面、交互与持久事实在具体任务中验收。浏览器条件未满足、只列出测试、无测试命中或仅收到 HTTP 响应都不能冒充场景通过；未运行范围照实记录。新增团队能力不代表原来缺失的 Task 源码或 Model harness 已恢复。

## Codex 依据

- [子 agent 配置](https://learn.chatgpt.com/docs/agent-configuration/subagents)：项目自定义角色、默认模型、思考强度与并发上限。
- [技能发现和调用](https://learn.chatgpt.com/docs/build-skills)：仓库 `.agents/skills/`、明确触发描述与显式技能调用。
