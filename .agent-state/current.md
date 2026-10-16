# 当前状态：接续新 Vue 工程，框架与空路由验证中

2026-10-16，用户要求从已暂停并保存的 Vue 重建继续。当前任务仍未验收；WIP、旧 HTML 和归档 Vue 的检查不证明新工程通过。

## 目标与边界

- 旧 `web/` 已完整归档到 `web-crape/`，仅作历史参照。新 `web/` 从零实现，不复用旧 API 客户端、Session、权限或业务代码；后端接口按具体页面需求另行设计，本轮不做。
- 先共享 UI、AppShell、**唯一 SettingsShell** 与全部已定路由骨架，系统设置优先。系统/个人/项目设置为 11/3/11 个叶子；未讨论正文只保留标题与空区域，不搬完整 Agent 或项目列表正文，不填虚构数据。
- `ai/frontend-f0-refinement-20261015@746ddcb3` 的可运行 HTML 原型是本轮设计参考，目录为 `docs/frontend-design/redesign/prototype/`。实现者与独审须实际运行并观察整页；原型检查不代证新工程。设计入口见[重设计说明](../docs/frontend-design/redesign/README.md)。

## 分支、会话与文件所有权

- 当前工作树：`/workspace/agenteam`；任务分支：`ai/frontend-vue-rebuild-20261016`。用户所指暂停点为 `635c90dc`；root 已保留远端后续 `d05bc8ab`、`ac23d6a9`，本轮从 `ac23d6a9` 接续，不回退覆盖。恢复以任务分支最新提交和实际差异为准，本状态不回填自身提交号。
- `/root` 是当前唯一协调写会话，负责全部 Git、分支、worktree、checkpoint/push 与集成。已核对 `main@b6db5297` 治理与任务分支相同，本轮不合 main。
- 骨架、共享组件与 harness 紧耦合，本轮共用工作树并按文件保持唯一写者；跨 owner 接口变更先沟通。Git 保存持续获用户授权；WIP 不等于正式完成或用户设计接受。

| Owner | 唯一写入范围与职责 |
| --- | --- |
| `/root/vue_integration` | `web/` 工程配置、锁、入口、路由、页面与布局；依赖恢复、工程检查、壳集成、作者浏览器与预览 |
| `/root/vue_shared_ui` | `web/src/components/ui/`、`web/src/styles/`、`web/src/composables/useTheme.ts`；组件合同及受影响检查 |
| `/root/vue_tests` | `web/tests/` 与 `tests/frontend-rebuild/`（含该目录 README）；组件测试与浏览器 harness |
| `/root/vue_visual_review` | 独立只读浏览器审查；可再生材料放忽略的 `output/ai/` |
| `/root/vue_docs` | 本状态、根 AGENTS/README、前端开发文档、`web/README.md` 和授权设计入口状态；只读核对新旧隔离，不代跑产品检查 |

## 当前结果与第一个断点

独立 Vue 工程、共享控件、AppShell/AccountShell/单一 SettingsShell 和 40 个空正文叶子已建立。本轮工程格式、类型、构建和全量 14 项 unit 通过；浏览器 12 项以 1 首链 + 4 控件 + 7 路由分轮有效结果组合覆盖，不能称为最终一次全量运行。

用户最新指出自绘 Logo 错误，要求使用 `docs/logos/` 已有正式资源。独审另发现 390px 长项目 slug 下短 owner 被拆行、返回箭头收缩的局部可读性问题，已交集成 owner 定向修复。当前断点是正式 Logo、项目长名可读性修复及独审的受影响复验；含错误 Logo 的旧截图不作为最终展示。其余源码未受影响的检查按对应 owner 结论复用。本轮仍未合 main，也未宣称用户已接受设计。

前端开发入口与组件合同已按新源码归位，旧业务验收保留为归档记录；UI owner 已只读确认八控件合同无实际 API 误述。

## 本轮已报告检查与资源

- 集成 owner 报告：Node `24.19.0`、npm `11.9.0`；新 `web` 与锁定浏览器 harness 的 `npm ci` 均 exit 0；Playwright `1.56.1`、Chromium `151.0.7922.173` 可用。当前源码 `npm run format:check --prefix web` 与 `npm run build --prefix web`（含 `vue-tsc --noEmit`）均 exit 0。首轮三个既有 unit 文件的格式失败，以及新增 unit 两处 strict 可能 undefined，已由测试 owner 修复；最终通过属于修复后的输入，不改写早期失败。
- 文档 owner 只读核对：`746ddcb3:web` 与当前 `HEAD:web-crape` 的 569 个文件 blob/执行位完全相同，工作树归档无受跟踪改动。新 lock 为独立 `@agenteam/web-rebuild`、179 个 package；当前新 `web/src` 未检出旧 client、Session、权限/API 或归档导入。路由导出核对为 40 正式叶子、11/3/11 设置、3 个重定向及 404，URL 形状与蓝图一致，全部正文为 SkeletonPage；三类设置只使用一份 SettingsShell，无 debug 路由。默认 npm、`deploy/dev/start.sh` 与 Compose 挂载均指新 `web/`。这是有限静态边界结论，本轮未运行 Compose，不是业务或浏览器验收。
- 集成 owner 已实际启动原型 5241 与新 Vue dev 5244，并报告各自 HTTP 200；原型为 Python PID 7451，新 Vue 为 Vite PID 7464（npm 父进程 7452），日志和 PID 文件位于忽略的 `output/ai/vue-integration/`。frp 助手 PID 8418 保持，公网 `http://mcpc-terra.lunadeer.cn:18081/system/users` 与本地 index SHA-256 一致，真实浏览器导航与主题切换已通过，root 已向用户提供该入口并保留 dev/frp。HMR 本地同 token/Upgrade 返回 101，但本环境经继承 HTTP 代理访问公网 WebSocket 被 envoy 403 拒绝；该环境链路未验成 HMR，可手动刷新获取更新，不据此断言用户直连也失败。
- 测试 owner 报告全量 unit 14/14（theme 4、menu 3、dialog 3、Input 1、Select 2、Tabs 1）；生产浏览器 12 项分轮有效通过：首链 1、控件 4、路由 7。覆盖主题键盘/刷新/跟随系统、控件焦点/选择、40 叶子、3 个 optional URL、默认跳转/保留路径与窄屏目录，命令及精确范围见 [harness](../tests/frontend-rebuild/README.md)。
- 路由组首轮 6/7，最后一项误设 390px 项目导航必横溢；实际导航 342px 已容纳约 326px 标签，document 无横溢。测试 owner 改用 320px 真实触发局部横滚后定向 1/1 通过，保留原失败，不归为产品缺陷。最终 5242 preview PID 18822 已退出，报告 `closed=true / exitCode=143` 且无监听；原生 200% 缩放由独审另证，不算 harness 条目。
- UI owner 根据实际打开的原型与新 Vue 校准 `tokens.css/useTheme.ts` 的中性暖灰、字体、圆角和 theme-color，无组件 API 或存储语义变更；报告作者浏览器主题、回焦、reduced motion、overflow 与对比度检查通过。两文件已随 checkpoint 保存，无接口变更。
- 独审 owner 以 `85677d28` 实际观察原型 general、Agent 列表/四 Tab、浅深/390 与菜单/Select/草稿 dialog，以及新 Vue 系统/个人/项目壳和 UI fixture。低高 1440×600 侧栏滚动、390×600 抽屉末项、导航后 H1 焦点、原生 200% 后 720×408 无横溢、普通/减少动效均报告通过，未发现框架结构性返修；后续独审发现上述长项目名局部问题，待同输入复验。Logo 变化使壳截图输入改变，最终结论待受影响复验。
- 旧输入独审 5243 production+fixture 服务已退出：wrapper PID 16600 终端 exit 0，preview PID 16650 报告 `closed=true / exitCode=143`。Logo/长名返修后由独审启动新隔离 preview 并复验，实际资源事实待其报告。原型 5241、新 Vue dev 5244 和 frp 18081 按用户授权保留。环境快照不保留进程，恢复时必须重新核实。
- 工具链要求 Node `>=24.12.0`；依赖按新 `web/package-lock.json` 安装，不能链接归档依赖。浏览器命令以[新工程 harness](../tests/frontend-rebuild/README.md)为准；普通工程运行及 Host/HMR 配置见 [web 运行说明](../web/README.md)。
- 日志、截图、dist 和缓存放忽略目录；源码、锁和必要 harness 随 checkpoint 保存。用户图片展示先按项目约定上传图床并使用其公网链接；凭据不入文档。用户已授权通过 frp 保留 dev 供体验，只有公网响应及页面功能验证后才提供地址。

## 下一步与保存边界

1. 集成 owner 使用 `docs/logos/` 正式品牌资源，并修复 390px 长项目名下 owner/返回箭头的可读性；补受影响检查，独审按同输入复验后给出当前范围最终结论。
2. 保留已验证公网 dev 供用户体验；如环境重启，按运行说明恢复并重核服务，不将本机 WebSocket 代理限制外推为用户直连结果。
3. 文档 owner 按 Logo 返修与最终独审更新最小状态和资源事实；保留历史验收，不宣称用户已接受设计。
4. 有意义结果或持续修改约 5 分钟的安全边界，各 owner 报告并停写本批路径，由 root checkpoint/push 后续写；本轮正式范围验收前不合 main。

## 保留的既有恢复入口

M3 明确范围已正式交付 main；接受范围、原失败和未覆盖边界继续见 [M3 卡](../docs/development/work-items/m3-knowledge-memory-collaboration.md)、[独审报告](../docs/development/work-items/m3-core-independent-review.md)及[任务台账](../docs/development/agent-team/tasks.md)。本轮设计工作不扩大其接受范围，也不解除原 STOP。

- Model 原测试和 helper 的恢复说明见 [Model harness](m3-model-runtime/reproduction/README.md)与[原工作卡](../docs/development/work-items/m3-model-runtime.md)。既有 compile-only 结果不等于本轮执行测试。
- 固定检索选型、排名、来源和 bootstrap 见[检索恢复入口](m3-retrieval-selection/README.md)；公开向量沿用 [runtime-embeddings-v2](m3-model-runtime/runtime-embeddings-v2/manifest.json) 的 gzip。Meeting SQL 测试和已审截图的位置沿上述正式工作卡恢复。
- 远端 `origin/ai/task-comments-20261013` 继续保留，保存尚未合入 main 的“不改变任务状态的独立追加评论”API、迁移、服务、UI 与测试，完整任务未交付；本轮不接管、不清理，也不改写其旧失败或验收事实。恢复该任务须另核分支实际代码、差异与会话归属，不能把其旧 `current.md` 当作本轮状态。

M3 后继缺口、完整 Lifecycle/全平台 Runner、生产 SPA/部署恢复和完整 D24/D25 token 流仍按原卡评估；`update-meeting`、`comment-meeting` 的可写语义仍未定义、未注册、未接受。不自动进入 M4、E01 或生产发布。
