---
name: agenteam-verification
description: 在 agenteam 接到验收、测试或文档任务卡时使用，按授权范围核对实际 diff、行为证据和文档链接并报告真实结果。
---

# agenteam 测试与文档验证

## 适用任务

用于 `verification_worker` 执行主线程批准的验收、测试或文档任务卡。
先确认任务是只读审查，还是允许写入指定测试、文档文件。
完整任务卡应明确目标、文件范围、接口、步骤、必用技能、验收命令与预期、停止条件和交付格式。

## 按需读取来源

- 读 [仓库规范](../../../AGENTS.md)、[团队流程](../../../docs/development/agent-team/README.md)、任务卡和实际 diff。
- 前端影响读 [前端开发基础](../../../docs/development/frontend/README.md) 与 [验证记录](../../../docs/development/frontend/verification.md)。
- 组件验证读 [公共组件接口](../../../docs/development/frontend/components.md) 及任务对应设计文档。
- 写 Vue 测试时读取 [Vue 测试最佳实践](../vue-testing-best-practices/SKILL.md) 的对应参考。
- 后端验证从 [仓库结构](../../../docs/development/repository-structure.md) 和 [系统架构](../../../docs/architecture/README.md) 选择相关来源。
- 浏览器任务使用环境已有的 `playwright` 技能；不可用时报告无法执行。

## 执行检查

1. 检查 `main` 基线、用户改动和任务卡授权范围，保留其他人的工作。
2. 审查实际 diff 与批准接口，确认实现覆盖任务目标及停止条件。
3. 用正常和异常行为证据验收，不用快照代替行为验证。
4. 只写任务卡允许的测试和文档文件，不私改产品代码。
5. 前端影响按任务卡执行 `npm run check --prefix web` 并记录结果。
6. 仅改 docs/config 时，不运行不相关的前端或 Go 测试。
7. 文档变更检查相对链接目标、受影响引用和 Markdown 结构。
8. 执行任务卡要求的 whitespace 与其他检查，记录实际命令和环境。

角色 TOML 以 `name` 字段识别角色；从实际目录枚举配置文件，不假定文件名与角色名完全一致。

视觉或交互变更按任务卡检查浅深色、桌面与窄屏、键盘导航、焦点恢复。
同时检查 reduced motion 和 overflow，等待必要的动画或异步状态结束。
浏览器截图放在 `output/playwright/`，遵循仓库忽略规则，不自动纳入提交。
没有运行浏览器时，不能声称已验证视觉或交互。
不为简单文档或可逆改动新增无意义测试。
Vue 测试示例应匹配实际版本；定时器按实际 Vitest API 推进。
`flushPromises` 不保证推进定时器，不以等待 Promise 替代时间推进。
数据库测试只使用任务卡授权的隔离测试库，不接触其他数据库。

## 阻塞与交付

发现实现 bug 时返回主线程，由原执行角色修复，不私改产品代码。
未决接口、未授权的新依赖、范围扩展或同一失败出现两次时，停止受影响步骤并报告主线程。
不得创建下级 agent；允许只读 Git 检查（如 status、diff、log）；不执行 Git 写操作（暂存、提交、推送、reset、clean），不改变分支或 worktree。
交付审查文件与实际写入文件，分别列出通过、失败、未执行检查。
每项结果附实际命令、环境、证据或未执行原因，以及仍需主线程处理的阻塞。
