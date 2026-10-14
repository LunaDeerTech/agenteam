# 当前工作：统一界面设计逻辑

- 工作树 `/workspace/agenteam-interface-design-guidelines`，分支 `ai/interface-design-guidelines-20261014`，起点 `e4cbb7bf`。用户要求先统一设计思路，再完善项目页及后续页面。
- 本轮仅文档：以 [界面设计逻辑](../docs/frontend-design/interface-design.md)统一页面类型、信息优先级、受控宽度、操作载体与状态/焦点规则，链接现有样式、布局和前端开发入口；不改产品代码、依赖或测试。
- 固定顶部导航与项目导航保留；新页面组合默认值是工程设计规则，不冒称现有界面已符合。组件实际能力、领域权限和原请求恢复契约继续有效。
- 项目页候选在 `/workspace/agenteam-project-list-polish`、`ai/project-list-polish-20261014` 的 `bb9141c0`，已远端保存，未正式集成。保留该树用于后续按新规范调整；工程验收未完成，细节见候选分支的 `.agent-state/current.md`。浏览器接口 fixture 结果仅覆盖 UI，不证明真实后端创建。
- 本轮规范已完成检查：10 个 Markdown 文件的 167 个本地链接/fragment、UTF-8/LF、末尾换行、尾空格与代码围栏自查通过，`git diff --check` 通过；独立审查无阻塞，来源、token、组件能力与业务边界相符。关闭策略按审查建议作一句措辞澄清，复核不改变领域规则。纯文档未运行产品测试，不构成产品界面通过结论。
- 本次交付以规范为主，不自动续做候选 UI；已停写，待主线程正式集成和推送。规范树无后台任务，无独有 untracked/ignored 资源；正式交付确认后可由主线程按既有规则清理本树及任务分支。项目页候选仍按上文保留，不能随规范树一起清理。
