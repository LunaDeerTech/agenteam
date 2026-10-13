# D09 同一次 Execution 的调用筛选

2026-10-13。基线 `e46a4b6ecc9d53e144afbdff0cd0e145aaf1bd3f`；工作树 `/workspace/agenteam-usage-execution-filter-ui`，分支 `ai/usage-execution-filter-ui-20261013`。本片沿[调用记录与日汇总](d09-project-usage-read-ui.md)补齐同 Execution 的只读观察，首版源码待独立审查和统一运行验证；不把静态实现记为通过。

## 用户操作与正式契约

当前页调用详情有非空 `execution_id` 时提供“查看同一次执行的调用”。选择后关闭详情、从明细第一页读取该执行的调用，页顶显示当前执行与“清除执行筛选”。已筛选同一执行时不重复提交。无关联执行的记录保留正常详情，不提供该入口，不要求用户填写或猜测 ID。

明细、续页、返回、按日汇总与刷新均携带同一 `execution_id`。应用、清除和切换视图保留当前 UTC `[from,to)` 窗口并丢弃旧分页链；清除保留当前视图。只有显式刷新重新取得近 30 天窗口，同时保留执行筛选。应用／清除后的焦点回到当前视图标题，普通详情关闭仍回到原记录按钮；现有主题、精确整数和有界汇总滚动区复用。

正式来源为 [Owner Usage HTTP](d09-project-usage-read-http.md) 与 [OpenAPI](../../../api/openapi/project-usage.json)。两个既有 GET 的可选 `execution_id` 只接受 canonical UUIDv7，省略表示当前项目全部调用；不接受 null、空字符串或额外筛选键。明细固定 `limit=50`，日汇总固定 `group_by=day`、`limit=100`。合法但无匹配的执行沿正式接口返回空记录／空分组，不伪造 Execution 存在性检查。明细整页校验每一条的项目、执行、时间与排序；日汇总使用服务端全量结果，不从当前 50 条相加。

原 Session `usage-read` owner 与捕获透传保持。项目、路由或身份代际变化清除执行筛选、详情和分页；取消后不发布迟到结果，原 I/O 未退出时不可启动另一读取。一般读取失败清除观察结果并保留筛选／窗口供显式重试或清除；当前无权／不可访问响应清除执行与窗口。任何新执行筛选均建立新读取代际，不复用前一执行的 cursor。

## 文件与必要验证

产品仅 `web/src/api/client.ts`、`web/src/api/project-usage.ts`、`web/src/composables/useProjectUsage.ts`、`web/src/views/projects/ProjectUsageView.vue`。三个新 `web/src/tests/project-usage-execution-{client,state,view}.spec.ts` 针对双 GET 捕获与严格参数、整页跨执行拒绝、空结果、分页／日汇总／清除、取消迟到、跨项目／身份及可见按钮和焦点；复用原 Usage fixture，不修改 App、Session、后端或共享浏览器 helper。

独立浏览器作者另域维护专属测试文件，沿 `tests/model` 的真实 Usage Writer／自有 wire 和默认 App 准备同执行超过 50 条、另一执行、无执行与另一项目对照。计划证明同窗筛选续页、全量日汇总、清除与原账本不变，并在宽浅／窄深实际操作检查键盘、焦点和溢出。统一运行由 runtime 负责人调度，静态作者不启动 npm、Go 或服务；共享锁定依赖只读复用，输出与可写缓存归本树独有。

本片不提供 Execution 运行状态、外部模型执行证明、费用估算或完整 D09。真实 DEV_CHAT 验收独立进行，既有 STOP 保持。测试、构建、真实浏览器与截图的实际结果在原运行完成后补记，原失败证据不覆盖。
