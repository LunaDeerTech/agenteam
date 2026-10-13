# D09 同一次 Execution 的调用筛选

2026-10-13。基线 `e46a4b6ecc9d53e144afbdff0cd0e145aaf1bd3f`；工作树 `/workspace/agenteam-usage-execution-filter-ui`，分支 `ai/usage-execution-filter-ui-20261013`。本片沿[调用记录与日汇总](d09-project-usage-read-ui.md)补齐同 Execution 的只读观察。源码经非作者审查接受，前端检查与默认 App 真实读取闭环已通过；受测源码为 `3be0dcce50ca923f1b9ed55be8c33db04ae7ef54`，最终结果文档交独立事实核后由 Git 负责人集成。

## 用户操作与正式契约

当前页调用详情有非空 `execution_id` 时提供“查看同一次执行的调用”。选择后关闭详情、从明细第一页读取该执行的调用，页顶显示当前执行与“清除执行筛选”。已筛选同一执行时不重复提交。无关联执行的记录保留正常详情，不提供该入口，不要求用户填写或猜测 ID。

明细、续页、返回、按日汇总与刷新均携带同一 `execution_id`。应用、清除和切换视图保留当前 UTC `[from,to)` 窗口并丢弃旧分页链；清除保留当前视图。只有显式刷新重新取得近 30 天窗口，同时保留执行筛选。应用／清除后的焦点回到当前视图标题，普通详情关闭仍回到原记录按钮；现有主题、精确整数和有界汇总滚动区复用。

正式来源为 [Owner Usage HTTP](d09-project-usage-read-http.md) 与 [OpenAPI](../../../api/openapi/project-usage.json)。两个既有 GET 的可选 `execution_id` 只接受 canonical UUIDv7，省略表示当前项目全部调用；不接受 null、空字符串或额外筛选键。明细固定 `limit=50`，日汇总固定 `group_by=day`、`limit=100`。合法但无匹配的执行沿正式接口返回空记录／空分组，不伪造 Execution 存在性检查。明细整页校验每一条的项目、执行、时间与排序；日汇总使用服务端全量结果，不从当前 50 条相加。

原 Session `usage-read` owner 与捕获透传保持。项目、路由或身份代际变化清除执行筛选、详情和分页；取消后不发布迟到结果，原 I/O 未退出时不可启动另一读取。一般读取失败清除观察结果并保留筛选／窗口供显式重试或清除；当前无权／不可访问响应清除执行与窗口。任何新执行筛选均建立新读取代际，不复用前一执行的 cursor。

## 文件与必要验证

产品仅 `web/src/api/client.ts`、`web/src/api/project-usage.ts`、`web/src/composables/useProjectUsage.ts`、`web/src/views/projects/ProjectUsageView.vue`。三个新 `web/src/tests/project-usage-execution-{client,state,view}.spec.ts` 针对双 GET 捕获与严格参数、整页跨执行拒绝、空结果、分页／日汇总／清除、取消迟到、跨项目／身份及可见按钮和焦点；复用原 Usage fixture，不修改 App、Session、后端或共享浏览器 helper。

独立浏览器作者另域维护四个新测试文件，沿 `tests/model` 的正式 Usage Writer／自有 wire 和默认 App 准备同执行 51 条、同项目另一执行 1 条、无执行 Tool 调用 1 条及另一项目 1 条对照。同执行跨两个 UTC 日期，分别 25／26 条，项目全部记录对应 27／26 条；时间和 Execution ID 是写入前提供的受控 Runtime Facts，不是生产 Execution 启动事实。共享锁定依赖只读复用，输出与可写缓存归本树独有，统一运行由 runtime 负责人调度。

## 实际结果与边界

[前端基础检查](../../../output/ai/usage-execution-filter-ui-delivery/basic01/result.json)为 8 个 spec、38 项唯一测试全部通过，完整 `vue-tsc` 退出 0；[首次构建](../../../output/ai/usage-execution-filter-ui-delivery/build01/result.json)退出 0，84 个 dist 文件共 1,513,069 B。浏览器定位器的后续单行修正不改变产品、Go 或构建资产，因此复用这些结果与原 Go binary，没有重复基础矩阵或编译。

首轮 [native01 原失败](../../../output/ai/usage-execution-filter-ui-delivery/native01/chain.log)保留：检查完整执行 ID 的 `span` 定位器同时命中目标和 UiButton 内部五个 span，严格定位失败。修正仅收窄到原直接 `p > span`，完整 ID 等值及其余断言不变；不追认首轮通过。

修后 [native02](../../../output/ai/usage-execution-filter-ui-delivery/native02/result.json) 原 `6420/fb4c3a` whole Wait 0／147.589s，Go `TestUsageExecutionFilterWeb` 为 1 top／1 sub、45.15s，原 Node 1 case／14.0s 且 actual Wait 成功。16 个原请求中有 10 次调用明细、3 次按日汇总，浏览器写入 0；同执行筛选首 50 条／续页 1 条、全量日汇总、返回／刷新与清除均核同窗和对应执行条件，无执行详情无入口，已筛同执行按钮禁用，详情返回和操作标题焦点接受。原完整响应、公开 facade、EOF／Close／handler 返回联证齐，账本前后不变且另一项目记录未混入。

[原 post](../../../output/ai/usage-execution-filter-ui-delivery/native02/post.json)全门通过：9 个原进程已退出、6 个自有资源两轮 absence、3 个私有目录已移除，runtime、子进程与 Host TCP 差额双空，输入哈希不变。[唯一 binary 处置](../../../output/ai/usage-execution-filter-ui-delivery/compile01/binary-disposition.json)核实原编译及两轮运行的 21 个进程全部退出后，精确回收 84,091,916 B binary；原失败、运行日志与截图保留，不复制依赖、dist 或 binary。

runtime 已逐张查看四张原截图，并记录[有限视觉结论](../../../output/ai/usage-execution-filter-ui-delivery/native02/visual-review.json)：[1440×960 浅色筛选明细](../../../output/ai/usage-execution-filter-ui-delivery/native02/browser/usage-execution-filter-1440-light-filtered.png)、[390×844 深色筛选汇总](../../../output/ai/usage-execution-filter-ui-delivery/native02/browser/usage-execution-filter-390-dark-filtered-summary.png)、[深色汇总横滚后](../../../output/ai/usage-execution-filter-ui-delivery/native02/browser/usage-execution-filter-390-dark-summary-scrolled.png)、[深色清除后控件](../../../output/ai/usage-execution-filter-ui-delivery/native02/browser/usage-execution-filter-390-dark-cleared-controls.png)。完整 UUID 正常换行、控件与可见数字无明确重叠；横滚截图左侧标签部分移出滚动区属于该位置的实际状态，不冒称全列同时可见或完整设备／主题矩阵。

完整数字与范围见[最终汇总](../../../output/ai/usage-execution-filter-ui-delivery/summary.json)。本片证明默认 App 正式只读 HTTP 消费受控 Facts 经原 Writer 持久化的账本，不提供生产 Execution 运行状态、外部模型执行证明、费用估算或完整 D09。权限、取消和身份竞争仍按定向单元范围，不由正常浏览器链扩称已验全矩阵；真实 DEV_CHAT 验收独立进行，既有 STOP 保持。
