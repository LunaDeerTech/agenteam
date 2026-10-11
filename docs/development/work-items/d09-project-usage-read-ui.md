# D09 Project 模型调用记录只读 UI

2026-10-11。工作树 `/workspace/agenteam-project-usage-read-ui`，分支 `ai/project-usage-read-ui-20261011`；已由 root 快进至 Secrets 已交主线 `7e0a123e`。作者 agent_display_settings，独审 recovery_lead，root 独占 Git。产品、真实正常读取链和退出尾已获 recovery_lead 有限独立接受；等待 root 保存并集成，完整 D09 未完成。

## 范围与真实来源

复用 [Owner Usage HTTP](d09-project-usage-read-http.md) §§1–5、[OpenAPI](../../../api/openapi/project-usage.json) 与 `usage/contract`、`usage/query.go`。入口为项目设置“模型与 Provider → 调用记录”，路径 `/{username}/{project_name}/settings/model-usage`。仅调用稳定 Project ID 的 `GET /api/v1/projects/{id}/model-usage`；不加入 summary、总量推算、估价、Provider 请求或 Dashboard。

Invocation 为 **24 个顶层必需字段**；列表外壳另有 `items`、`next_cursor`，后者无下一页时必须显式 null。派工中的“26 字段”已按真实 DTO 纠正，不新增 wire 字段。usage 为六个 nullable 十进制计数和 source，final 为 null 或三个字段；保留安全历史名称、nullable 当前引用、全部合法历史 consumer/purpose/protocol，不把未知当零或用 Number 转换 int64。

每次首读/刷新捕获一次显式 UTC 近 30×24 小时 `[from,to)` 窗口；页长 50，下一页与返回页沿同一窗口及目标 cursor。返回是重新读取：返回首无 cursor 时后端会取得新的 upper boundary，因此不承诺固定快照。成功返回后丢弃旧后续页链，下一页只采用本次返回的新 cursor；刷新另捕获新窗口。逐页完整验证字段、Project、时间、`started_at DESC,id DESC`、ID 唯一性及游标，不发布半页。当前页及返回所需游标属于页面观察；路由、项目、身份、checking 或当前读取上下文改变清理旧观察，迟到结果不发布。

独立 `usage-read` 复用唯一 Session 的原实际 I/O owner。取消 public Promise 不等于 transport 退休；原尾未结束仍 busy。当前拒绝清本页，失败/游标失效明确重读，不自动重试，不清 Secrets 或其他领域的 Unknown 原意图。页面支持加载、空态、安全错误、停止读取、下一页/上一页/刷新和本页已取得记录的详情，不新增详情 HTTP。

## 写域与最小验证

专属产品：`web/src/api/project-usage.ts`、`web/src/composables/useProjectUsage.ts`、`web/src/views/projects/ProjectUsageView.vue`；专属控在 `web/src/tests/project-usage*`。共享增量仅 `api/client.ts`、`composables/useSession.ts` 的独立只读接线，`router/index.ts`、`router/auth.ts`、`components/layout/ProjectNav.vue`、`views/projects/ProjectSettingsView.vue` 的路由与菜单；布局仅 [项目设置](../../frontend-design/layouts/project-settings.md) 调用记录条目。本卡与 current 保存当前状态。

必要基础控：精确 GET/options/1 MiB 完整 JSON 与闭集；null、零、>2^53、MaxInt64；关联/时间/末行非法整页拒；两页/返回/刷新窗口、循环游标与跨页错序；真实 Session/Workspace 的 checking、拒绝/迟到及持有取消尾；页面可见状态、键盘与焦点。前端纯测单 worker；真实链沿既有默认 App、正式 Writer 和原七资源 fixture，不重建账本框架。

生产读取只展示既有真实记录，空数据库正常为空；本片不提供 Runtime/consumer/Facts 写口，不通过测试 seed 冒充生产绑定。Secrets 已交增量、Move 固定候选及其旧 FAIL 均不改；ready=false/生产 initializer 未绑定和既有 STOP 保持。

## 已接受结果与原失败

产品固定 `cc594e84`、Go fixture `98d7a02e`，最终方法 `dab84c8c`；实现者与独立审查者分离。原件在 `output/ai/project-usage-read-ui/`，各段保存精确命令、原 PID/退出与前后输入。`frontend02` 为客户端6项与真实 Session/Workspace8项，共2files14PASS；`view02` 为控制器接视图4PASS；`regression01` 为既有 Secrets、Scheduler Session、认证5files93PASS；最终 `type03` 与 `build01` 的类型/生产构建均0，不把分段结果叠加为当前 HEAD 单次全量通过。82个物理私有 dist 资产及唯一 Session 绑定贯穿真实验收。

`compile01` 原工具35864 actual0，Go390304 race-c0/70.834秒，1716保守输入前后和运行前当前值相同；其中1713项属于运行闭包，额外3个 cmd 测试仅保守冻结。`compile-list01` 原391889 actual0，唯一 `TestProjectUsageWeb`。最终运行闭包2555项、1939项 tracked 源码与 `dab84c8c` 对应；方法返修复用原 binary/dist，没有重编或冒新产品回归。

真实 fixture 为 `tests/model/project_usage_web_test.go`，复用同包 `newProjectUsageHTTPFixture` 的 strict Facts、实际 wire/Writer 与 `startProjectUsageRoot` 的 public app.Run。51条记录经过正式写口准备；浏览器只执行首50条、下一页1条、返回首50条、再次下一页1条、显式刷新首50条共五次 List，详情使用当前页安全 DTO。原后验确认 ledger/receipts/summaries 未变。受控 Facts 仅用于测试准备，不证明生产 Runtime producer 已绑定。

浏览器采用 HTTPS、自有安全 Cookie 和原完整响应/typed/public/current 联证。`native01` 的 allHeaders 受原入口拦截影响而缺 Host，原严格门拒绝：4请求/1次 List、verified0、joint0，wholeFAIL保留。窄修为 Usage 默认关闭的只读 CDP 四事件联证：真实 wire Host、requestId、method/完整URL摘要、canonical response RID/status 与原 PW、Go 同一请求一一核对；原缺 Host 投影仍记 missing，不推导或设置 Host，不以 Go Host 替代浏览器证据。重复、缺失、重定向、缓存/SW、冲突及未关联 API 均拒绝，实际 detach 后再次检查完整性。`wire-retire-repro01` 的晚到新 API/孤立 Extra 两个原FAIL保留；同脚本 `repro02` 两项通过。最终 `profile-controls03` 的真实 guard 4正33反、CDP状态/生命周期4正36反均0；原 launcher 控1正6反及旧有效 AST不变复用，最终 `browser-list04` 唯一case0。

`native02` 已验证五次 List、8份联合证明和 CDP，但390px下未先打开栏目 Drawer，末尾链接断言失败，wholeFAIL保留；当轮8请求为7 finished＋seq6第三次 List 200 ERR_ABORTED，57,644B完整消费已联证。后继仅给原 spec 补真实 Enter 打开“项目设置栏目”、可见 dialog 内原 href/aria-current 断言、Escape 关闭和按钮回焦，没有使用 includeHidden、改变产品或放宽门。

`native03` 原工具18108由作者亲收 actual0，supervisor435378 actualWait0/177.934秒；原日志 `/tmp/puw03/ui-b9d3a679c1d94025.log` terminal0/176.444秒。Go437287的1top/1sub PASS64.61秒，Node438738、driver435383与四个 Chrome 原 Wait均0。8个GET为 **6 finished＋seq6/7两次 Usage GET 200 ERR_ABORTED**，分别完整消费57,644B/1,178B，8份原响应/EOF/close/typed/public/current联合证明齐；五次List、零写/Lookup及112个CDP事件的实际detach均通过。七资源14次absence、private/runtime/desc与原75秒HOST_TCP baseline差额双空、2555输入前后相同，业务/资源均有限独立接受。

三张原图为1440浅色详情、390深色详情与返回列表，独审接受实际可见区域的主题、品牌、布局和焦点；长整数可换行且未截断，不据截图宣称所有长详情字段入图。完整字段、null/0/>2^53/MaxInt64、详情与分页焦点和窄屏菜单键盘末链由同轮 DOM/用户操作证明。`client01`导入失败（0 tests）、`type01`导入路径FAIL、`view01`错选设置壳h2的3PASS/1FAIL也保留；修复后只复验相应范围，未回填旧结果。

本片不包含汇总图、总量估算、费用、Provider调用、真实撤权/取消全矩阵或生产 Runtime producer/Project initializer/SPA部署；原 STOP 和完整 D09/D27/E01 未完成边界不变。当前无本片后台、native或Go cache writer；后续扩展由 root 另派。
