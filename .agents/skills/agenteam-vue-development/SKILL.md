---
name: agenteam-vue-development
description: 在 agenteam 接到前端实现任务卡时使用，按批准接口修改 Vue 页面、共享组件或 Debug 展示并执行指定验证。
---

# agenteam 前端开发

## 适用任务

用于 `frontend_worker` 执行主线程批准的前端任务卡。
只处理卡片指定的页面、组件、样式或测试文件，不自行重新设计组件边界。
完整任务卡应明确目标、文件范围、接口、步骤、必用技能、验收命令与预期、停止条件和交付格式。

## 按需读取来源

- 开始前读 [仓库规范](../../../AGENTS.md) 和 [团队流程](../../../docs/development/agent-team/README.md)。
- 读 [前端开发基础](../../../docs/development/frontend/README.md) 与 [公共组件接口](../../../docs/development/frontend/components.md)。
- 从 [前端设计入口](../../../docs/frontend-design/README.md) 选择任务对应的 `layouts/` 文档。
- 从 [样式规范](../../../docs/frontend-design/styles/README.md) 选择任务对应的主题、密度和交互规范。
- 写 Vue 测试时读取相邻 [Vue 测试最佳实践](../vue-testing-best-practices/SKILL.md)，仅加载当前测试需要的参考。
- 浏览器任务使用环境中可用的 `playwright` 技能；不可用时报告限制。

## 执行检查

1. 检查当前 `main` 基线、实际源码、依赖版本和用户改动，保留他人修改。
2. 按批准的 props/emits 实现；核对源码类型与组件文档，发现冲突先返回主线程。
3. 优先复用现有 Ui 控件、composables 与共享 tokens。
4. 共享组件不得引入 Debug 演示代码、数据或业务 API。
5. Debug 仅供开发，确保生产路由和构建继续排除其展示代码。
6. Vue 3 使用周边一致的 `<script setup lang="ts">`、模板及样式写法。
7. 除任务卡明确批准，不新增 Tailwind、Pinia、Testing Library 等依赖。
8. 只格式化授权文件，不运行会重写整个仓库的格式命令。
9. 执行任务卡指定工程检查，记录命令、环境和实际结果。

测试应验证用户可见行为和异常处理，不机械复制参考示例。
对 MSW 等示例先核对项目依赖与 API 版本，不能直接引入不匹配方案。
涉及定时器时按实际 Vitest API 推进 fake timers，并在测试结束恢复。
`flushPromises` 不保证推进定时器，异步等待与时间推进应分别处理。
视觉或交互任务按卡片检查主题、窄屏、键盘、焦点恢复、减少动效和溢出。

## 阻塞与交付

未决接口、未授权的新依赖、范围扩展或同一失败出现两次时，停止受影响步骤并报告主线程。
不得创建下级 agent；允许只读 Git 检查（如 status、diff、log）；不执行 Git 写操作（暂存、提交、推送、reset、clean），不改变分支或 worktree。
产品决策不能从 Debug 默认状态或测试 fixture 推导。
交付实际修改文件、行为变化、验证命令及证据，以及失败、未执行项和阻塞。
未经实际执行的检查不得报告为通过；需要返修时保留现有改动供主线程安排。
