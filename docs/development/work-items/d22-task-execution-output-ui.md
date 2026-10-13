# Task Execution 最终输出界面

本片在已有 Task Execution 详情下接入 `GET /api/v1/projects/{P}/tasks/{T}/executions/{E}/runtime-view`。正式来源为同轮 `d22-runtime-final-text.md` 与 `api/openapi/execution.json` 的 `durable_final_text_v1`；定向用例、类型检查、构建及真实默认 App 浏览器验收已通过。

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

API、Session 和真实 `TaskReviewDetail` 定向与兼容测试共 23 项、完整类型检查及生产构建已通过，构建共 86 个文件。覆盖大于旧摘要上限的完整文本、严格绑定和数据损坏、三种 availability 与真实空文本、纯文本渲染、显式恢复、实际取消尾及跨 Task/身份迟到响应。新 helper/spec 的目标语义检查无诊断；原传递旧源的 7 项诊断未纳本片修复，不宣称整个历史 harness 类型检查通过。锁定 Playwright 的 `--list` 已确认唯一场景可加载。

`runtime-web-native01` 使用原默认 App 的真实 Coordinator/OneRound producer 和受控 Model HTTPS 响应，完成实际列表→详情→加载最终输出→显式刷新。原加载屏障期间正文隐藏、刷新禁用；读取完成后完整 Unicode、换行及 HTML 字面内容与原快照一致，没有生成 script/img/iframe/a 节点或执行脚本。390 深色窄屏的正文内部 End/Home 键盘滚动、焦点及无页面横向溢出通过。目标 4 次 GET、页面阶段零写，22 个原响应连同辅助读取均有完整消费及 handler 返回证明；其中两个辅助读取保留实际 ERR_ABORTED 标签，以原完整消费联合判据验收，未将它们改写成 finished。Node、CDP 和自有资源实际退出。

三张本轮截图已独立查看，无视觉 must-fix：1440 浅色加载态无旧正文、刷新呈禁用外观；输出态可见字面 script/img/Markdown、Unicode 及刷新焦点；390 深色正文处于独立有界滚动区，焦点清楚、控件无横向溢出。正文下沿是内部滚动裁切，不据截图宣称全文、End/Home 动作时序或 16 MiB 边界已验证；完整内容与键盘动作沿本轮动态断言。业务、截图及资源结果位于 `output/ai/execution-runtime-view/runtime-web-native01`。原生两轮夹具出站装配失败及修复范围见[后端结果](d22-runtime-final-text.md)，没有以失败终态快照冒充成功输出。

本片只展示 schema1 DirectText 已保存的最终结果。持久 generation 不是 WebSocket epoch，当前没有活动 Runtime 事件流、token 更新、subscribe-first 或 resync，也不代表完整 D22/D25 已完成。
