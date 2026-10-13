# Task Execution 最终输出界面

本片在已有 Task Execution 详情下接入 `GET /api/v1/projects/{P}/tasks/{T}/executions/{E}/runtime-view`。正式来源为同轮 `d22-runtime-final-text.md` 与 `api/openapi/execution.json` 的 `durable_final_text_v1`；当前为源码候选，尚未运行基础检查或真实浏览器验证。

## 页面行为

沿原 Task 路由、执行记录列表和详情入口，在「最终输出」区明确点击「加载最终输出」，之后可刷新或在失败后重试。页面没有自动轮询、WebSocket 或执行命令，不从 TaskEvent、Usage 或 Task 状态合成输出。

- `pending` 显示尚未生成最终输出，可稍后明确刷新。
- `not_recorded` 显示执行已结束但未保存输出记录，不冒充空成功。
- `available` 的成功执行显示唯一 text Item 的完整正文；已保存的空字符串明确显示为空文本。失败或取消的真实空快照显示无最终文本，安全原因沿上方 canonical 执行详情。

正文仅作 Vue 文本插值，不解释 HTML、Markdown 或链接，不截断、不去除空白。采用共享主题 tokens、窄屏换行和可键盘聚焦的正文；刷新按钮保留焦点，错误或失权移除原控件后回到仍可见的执行标题。

## 请求和一致性

专属 `task-execution-output.ts` 校验封闭 DTO、Project/Task/Execution、终态与 availability、原 Snapshot generation、完整执行版本、固定 progress/seq/revision、Round source reference 和时间一致性。成功响应完整字节上限为 16 MiB + 64 KiB，正文 UTF-8 上限 16 MiB。读取达到完整 EOF 并通过全部校验后才发布；损坏、超限或部分 JSON 均为错误，不保留部分正文。

Session 的 `workReview.executionOutput` 沿原 `work-review-read` owner，使用无 body/query/key 的 GET。取消、离页及可见截止不提前释放实际请求尾。输出返回的执行摘要与正文整体采用，拒绝覆盖已知较新的执行版本；不改变 Task 状态或已有 Unknown 命令。

每次刷新先清除旧正文；刷新执行详情、返回列表、收起及 Project/Task/Session/路由代次变化同时清除输出观察。401/403/404/410 沿原 reader 清除本区列表、详情和正文。迟到结果不回填新上下文，无自动重发。

## 必要验证与限制

新增 API、Session 和真实 `TaskReviewDetail` 组件测试，覆盖大于旧摘要上限的完整文本、严格绑定和数据损坏、三种 availability 与真实空文本、纯文本渲染、显式恢复、实际取消尾及跨 Task/身份迟到响应。统一 runtime 尚待执行这些定向用例、相关原 Execution 用例、类型和构建；真实默认 App/浏览器仍需原 DirectText producer 联调，不以单元测试替身代替。

本片只展示 schema1 DirectText 已保存的最终结果。持久 generation 不是 WebSocket epoch，当前没有活动 Runtime 事件流、token 更新、subscribe-first 或 resync，也不代表完整 D22/D25 已完成。
