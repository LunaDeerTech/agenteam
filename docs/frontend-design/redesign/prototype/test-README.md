# F0 浏览器检查入口

本目录的 Playwright harness 只验证隔离样板与当前正式 Vue 页面在合成 HTTP fixture 下的表现。所有业务动作都是演示；没有真实后端、凭据、业务写入或生产验收。作者检查与独立视觉审查分别形成结论。

## Agent 独立详情与设置 header

本轮以 `c4c4c9d7` 后作者冻结的首条正常链为输入。`?page=agents` 是无默认选择的列表；`agent=agent-architecture|agent-researcher|agent-reviewer` 定位独立详情，`view=memory` 定位同一 Agent 的记忆。这三个对象是根据正式字段构造的合成样例，**没有当前正式 Vue 同三对象 fixture 的前图**；旧正式 fixture 只有一个 `reviewer`，不能混称同输入前后对照。旧 `page=memory` 只作明确的历史兼容别名。

`[agents-first]` 首轮 **4/4 通过（23.1s，exit 0）**：

- 1440×960 浅色和 390×480 深色，依次选择非首项 Reviewer / Researcher，核对名称、描述与各自记忆历史的 `execution-reviewer-1` / `execution-researcher-1`，关闭记忆回同一详情按钮，再回列表原行与滚动位置。短窗口先确认确有滚动，未改页面 CSS。
- 浏览器 Back 返回原列表；Researcher Memory 的直接地址刷新仍属于 Researcher；未知 Agent ID 明确显示不可用，不能退回 Architecture；列表不同时显示详情。
- General、Agent 列表/详情/记忆、MCP 在 1440 浅色 / 390 深色共享 header 横向起点与宽度，标题和说明左起点一致，表单/正文左对齐且表单最大宽度 760。此轮**没有断言标题纵向位置**；独审发现返回行造成的纵向漂移后已交作者修复。
- app 历史改动下，General 脏草稿遇浏览器 Back 会确认；Escape 保留输入，明确放弃后才返回 Agent 列表。

```sh
export NODE_PATH=/workspace/shared/node/account-captcha-ac60fa262f0c44ae-node24.19.0-linux-x64/node_modules
export F0_LEGACY=0 F0_PORT=5218
export F0_OUTPUT="$PWD/output/ai/frontend-f0-refinement-test/agent-first"
node "$NODE_PATH/@playwright/test/cli.js" test --config docs/frontend-design/redesign/prototype/playwright.config.cjs --grep '\[agents-first\]' --list
node "$NODE_PATH/@playwright/test/cli.js" test --config docs/frontend-design/redesign/prototype/playwright.config.cjs --grep '\[agents-first\]'
```

该轮仅正常链与既有 General 回归；Unknown、项目 scope、只读细节当时仍在作者修正队列，未记为通过。normal / reduced motion 与原生 200% 尚待最终候选定向补查，不复用旧页缩放结论。原输出在 `output/ai/frontend-f0-refinement-test/agent-first/`；自有 5218 server PID 332756 已关闭并回收，浏览器随原命令退出，没有创建 `web/node_modules` 或关闭作者 5209 服务。harness 自查不替代独立视觉接受。

## 用户反馈后的基本信息页基线

用户未接受旧 F0 的视觉结果。下文旧轮次的技术通过事实不代表用户接受，也不代替本轮单页设计与独立审查。本轮只为“项目设置 → 基本信息”准备当前正式 Vue 基线：真实路由 `/owner/atlas/settings/general` 加载 `web/src/views/projects/ProjectGeneralSettings.vue`，HTTP 数据直接复用 `tests/account-captcha-web/interface-layout.fixtures.ts`。没有改产品源码或增加 mock 接口；旧 F0 的设置页是 MCP，不能作为基本信息的同页前图。

`[general-baseline]` 在 1440×960 / 390×844、浅深主题下核对 Atlas 名称、原描述、保存按钮未启用、项目记录折叠、主题、运行时错误和横向溢出；所有请求均为 GET。初次实际运行 **4/4 通过（15.1s）**，四图位于 `output/ai/frontend-f0-refinement-test/baseline/screenshots/before-general-{1440,390}-{light,dark}.png`。正式页面内部 `main` 独立滚动，`fullPage` 图只包含其可见区域；另用真实鼠标滚轮补充 390 深色下端截图，不拉伸或重写正式 CSS。

从当前工作树根目录执行，匹配的 `web/node_modules` 临时链接仅在正式 Vue 服务有消费者时存在：

```sh
export NODE_PATH=/workspace/shared/node/account-captcha-ac60fa262f0c44ae-node24.19.0-linux-x64/node_modules
export F0_WEB_NODE_MODULES=/workspace/shared/node/web-314c5c1d204fb359-node24.19.0-linux-x64/node_modules
export F0_LEGACY=1 F0_PORT=5218 F0_LEGACY_PORT=5219
export F0_OUTPUT="$PWD/output/ai/frontend-f0-refinement-test/baseline"
node "$NODE_PATH/@playwright/test/cli.js" test --config docs/frontend-design/redesign/prototype/playwright.config.cjs --grep '\[general-baseline\]' --list
node "$NODE_PATH/@playwright/test/cli.js" test --config docs/frontend-design/redesign/prototype/playwright.config.cjs --grep '\[general-baseline\]'
```

390 深色的真实滚动补图单项 **1/1 通过（8.5s）**，原命令 exit 0，截图为 `output/ai/frontend-f0-refinement-test/baseline-bottom/screenshots/before-general-390-dark-bottom.png`。两轮 server PID 232329 / 233526 均记录 `closed: true` 且已回收；设计者确认未消费正式 Vue 依赖链接后，已移除本任务 `web/node_modules`，仓外安装保留。作者与设计者均已亲看 1440 浅色、390 深色基线；这仍是基线确认，不是独立设计接受。

新页使用 `?page=settings&fixture=baseline&scenario=normal`，与正式基线保持相同名称、描述和记录折叠状态；四张初始图为 `output/ai/frontend-f0-refinement-test/general-first/screenshots/after-general-{1440,390}-{light,dark}.png`。

首稿 `8eaaa0d3` 候选停止写入窗口内，`[general]` **7/7 通过（31.7s）**，无重试或跳过，原命令 exit 0：

- 四尺寸/主题：核对基线，描述编辑 → 取消并回焦、演示保存并递增版本、改名确认 Escape 取消与再次明确确认，核对名称和版本。
- 390 深色：只读不允许修改；读取失败 → 重读；版本冲突保留草稿 → 重读 → 取消/明确采用当前值；Unknown 阻止新保存，查证/原请求重放保留草稿，明确放弃追踪后必须重读才恢复编辑。
- 1440 浅色与 390 深色：更多菜单键盘打开、End、Escape 回到触发器；改名对话框 Tab 约束和 Escape 回焦；窄屏栏目菜单选择 MCP 后经项目导航回基本信息；项目概览“查看基本信息”也到基本信息页。
- 正常与减少动效各实际观测菜单及改名对话框连续 18 个动画帧。正常菜单记录 8 / 9 种 opacity，对话框各 12 种；减少动效 opacity 恒为 1、活动动画为 0。这证明实际有变化或已减少，不证明动效质感或视觉接受。

```sh
export F0_LEGACY=0
export F0_OUTPUT="$PWD/output/ai/frontend-f0-refinement-test/general-first"
node "$NODE_PATH/@playwright/test/cli.js" test --config docs/frontend-design/redesign/prototype/playwright.config.cjs --grep '\[general\]' --list
node "$NODE_PATH/@playwright/test/cli.js" test --config docs/frontend-design/redesign/prototype/playwright.config.cjs --grep '\[general\]'
```

这组仅使用自有静态端口 5218，不消费设计者的 5207 服务或 `web/node_modules`。server PID 240228 的关闭记录为 `closed: true`，原命令退出后 PID 已不存在；浏览器由原 Playwright 命令关闭。初稿检查后设计者针对独审视觉反馈重开样式与菜单，因此上述结果按输入/影响复用，不能写成返修后的完整结论。全部保存和恢复都是本地演示，没有真实后端与业务写入。本 harness 作者不独立验收自己编写的测试，也不据这些行为通过宣称用户接受。

第二稿 `f0138630` 冻结后，只复验变动的菜单与布局：两尺寸的上述菜单/对话框正常及减少动效、Tab 与 Escape、设置入口；四种尺寸/主题真实滚轮到展开记录最后一项，验证该项仍在视口内且未被遮挡；共享 Projects 筛选器的 Tab / Escape 与页面滚动关闭。更多菜单 Tab 现在关闭后原生前进到项目名称；窄屏栏目菜单内部真实滚动仍保持展开。关闭菜单后 `aria-controls` 被清除。

`general-second` 首次为 **3 项通过、1 项失败（19.3s，exit 1）**。失败是 harness 输入前提错误：390×844 下六行项目恰好全部容纳，鼠标滚轮没有产生页面滚动，不能证明滚动关闭行为。该原始报告、截图和 trace 保留；未当作产品缺陷，也未放宽关闭断言。将该单例设为 390×600 并先断言存在可滚动内容后，`general-second-scroll` **1/1 通过（4.7s，exit 0）**：实际 `scrollY` 增加后，菜单关闭、展开标志为 false、控制关联移除。菜单截图使用当前视口；`fullPage` 的临时视口尺寸变化可能按设计关闭菜单，不能用它观察展开态。

```sh
export F0_LEGACY=0 F0_PORT=5218
export F0_OUTPUT="$PWD/output/ai/frontend-f0-refinement-test/general-second"
node "$NODE_PATH/@playwright/test/cli.js" test --config docs/frontend-design/redesign/prototype/playwright.config.cjs --grep '(\[general\] menus|\[general-refinement\])' --list
node "$NODE_PATH/@playwright/test/cli.js" test --config docs/frontend-design/redesign/prototype/playwright.config.cjs --grep '(\[general\] menus|\[general-refinement\])'
# 实际失败修正后只复验此项，未覆盖首轮输出。
export F0_OUTPUT="$PWD/output/ai/frontend-f0-refinement-test/general-second-scroll"
node "$NODE_PATH/@playwright/test/cli.js" test --config docs/frontend-design/redesign/prototype/playwright.config.cjs --grep '\[general-refinement\] shared projects' --list
node "$NODE_PATH/@playwright/test/cli.js" test --config docs/frontend-design/redesign/prototype/playwright.config.cjs --grep '\[general-refinement\] shared projects'
```

第二稿两轮自有 server PID 247456 / 248370 均 `closed: true` 且已回收，浏览器随原命令退出；没有重建 `web/node_modules`，没有关闭设计者 5207 服务。最终行为范围由首稿 7 项中未变的业务旅程和第二稿 3 + 1 项定向结果组成；未运行旧 62 项完整矩阵、产品工程全量或真实后端。视觉结论与用户接受继续由各自负责人另行形成。

补充的 `[general-native-zoom]` 复用独审 `review/interact.mjs` 的 Chromium 原生 profile 缩放方式，在浅色基本信息页执行编辑、演示保存、更多菜单及记录底部旅程。它没有使用 CSS zoom 或 Playwright 的 viewport / DPR 模拟：实测外窗 1440×960、CSS 内窗 720×408、`visualViewport` 720×408.5、DPR 2、根 CSS zoom 1。视觉视口保留小数而 `innerHeight` 为整数；首次错误要求两者严格相等的 harness 失败保留在 `general-native-zoom`，修正为核对向下取整后的高度。

原生检查随后发现真实菜单竞争：保存后点击屏外“更多”，浏览器已将触发器滚入视图，旧 scroll 通知却在菜单打开后才交付，原 core 监听将新菜单关闭。原失败位于 `general-native-zoom-final`；提前安装事件观测后，`general-native-zoom-timing` 的 3 次有界复现为 2 通过 / 1 失败，记录了 anchor 几何不变时 `menu-added → menu-removed → scroll` 的顺序。一次增加诊断调用后的通过没有被当作修复。设计者随后仅修 core：外部 scroll 只有在触发器坐标相对打开时真实改变才关闭菜单。

在作者冻结的纯格式化结果与该最小 core 修复上，`general-native-zoom-repaired` **3/3 通过（16.4s，exit 0）**。三次均实际收到菜单打开后迟到的旧 scroll，位置不变时菜单持续可用；编辑、保存、Escape 回焦、展开记录、真实滚轮到最后时间记录且无覆盖、页面无横溢均通过。`general-scroll-repaired` 的共享项目菜单 **1/1 通过（4.0s，exit 0）**，确认 Tab / Escape 和真实页面滚动关闭仍有效。CDP 原生视口截图 `general-native-200pct-menu.png` / `general-native-200pct-records.png` 已查看；没有用普通 fullPage 的缩放裁剪差异替代真实截图。

```sh
export F0_LEGACY=0 F0_PORT=5218
export F0_OUTPUT="$PWD/output/ai/frontend-f0-refinement-test/general-native-zoom-repaired"
node "$NODE_PATH/@playwright/test/cli.js" test --config docs/frontend-design/redesign/prototype/playwright.config.cjs --grep '\[general-native-zoom\]' --repeat-each=3 --list
node "$NODE_PATH/@playwright/test/cli.js" test --config docs/frontend-design/redesign/prototype/playwright.config.cjs --grep '\[general-native-zoom\]' --repeat-each=3
export F0_OUTPUT="$PWD/output/ai/frontend-f0-refinement-test/general-scroll-repaired"
node "$NODE_PATH/@playwright/test/cli.js" test --config docs/frontend-design/redesign/prototype/playwright.config.cjs --grep '\[general-refinement\] shared projects' --list
node "$NODE_PATH/@playwright/test/cli.js" test --config docs/frontend-design/redesign/prototype/playwright.config.cjs --grep '\[general-refinement\] shared projects'
```

所有原生诊断与修复轮次的浏览器 context 已实际 close，临时原生 profile 已删除；server PID 254627、255273、256375、257343、258840、259575 均记录关闭并已回收。以上仅补当前基本信息页一个主题及共享菜单受影响行为，原通过边界和失败记录继续保留。

## 锁定依赖与启动

使用 `tests/account-captcha-web/package.json` 和完整锁文件的 `@playwright/test` **1.56.1**。正式页面对照额外使用 `web/package.json` 和完整锁文件；不能用 `npx` 隐式下载新版。两份安装在工作树外复用，安装目录按 package+lock、Node 与平台区分；`web/node_modules` 可临时链接到匹配安装，测试结束且无消费者后移除该链接，不能提交。

本轮已实际运行的环境：Node 24.19.0、Chromium 151.0.7922.173 (`/usr/bin/chromium`)。浏览器 profile、Vite cache、截图和报告均按输出目录隔离。恢复时先检查版本与资源，只在缺失时将两组 package/lock 复制到新外置目录并执行 `npm ci --prefix <外置目录>`，不修改共享安装。

```sh
export NODE_PATH=/workspace/shared/node/account-captcha-ac60fa262f0c44ae-node24.19.0-linux-x64/node_modules
export F0_WEB_NODE_MODULES=/workspace/shared/node/web-314c5c1d204fb359-node24.19.0-linux-x64/node_modules
export F0_LEGACY=1
export F0_OUTPUT=/workspace/agenteam-f0/output/ai/frontend-f0/author

# 从仓库根目录执行；每次新增/修改入口先检查可发现性。
node "$NODE_PATH/@playwright/test/cli.js" test --config docs/frontend-design/redesign/prototype/playwright.config.cjs --list
node "$NODE_PATH/@playwright/test/cli.js" test --config docs/frontend-design/redesign/prototype/playwright.config.cjs

# 有界选择器示例；不把 --list、未选择用例或跳过记作通过。
node "$NODE_PATH/@playwright/test/cli.js" test --config docs/frontend-design/redesign/prototype/playwright.config.cjs --grep '\[comparison\]'
node "$NODE_PATH/@playwright/test/cli.js" test --config docs/frontend-design/redesign/prototype/playwright.config.cjs --grep '\[(keyboard|touch|accessibility)\]'
```

`F0_PORT` / `F0_LEGACY_PORT` 默认分别为 5188 / 5189，均仅监听 `127.0.0.1`。Playwright 拒绝复用现存服务。独立审查可另用 5198 / 5199，或单独运行 `node docs/frontend-design/redesign/prototype/test-server.mjs`；原型 URL 为 `http://127.0.0.1:5188/index.html?page=projects&theme=light&scenario=normal`。仅检查新原型时不设置 `F0_LEGACY`，并以 `--grep-invert '\[(comparison|general-baseline)\]'` 排除明确需要正式页面的组；选择对照组却缺少依赖时直接失败。

## 场景与证据边界

- `[matrix]`：11 个必需页面，1440×960 / 390×844，各浅/深主题，正常、长内容、空、加载、失败、只读、Unknown；检查真实页面渲染、页面横向溢出、唯一页面标题与运行时异常。正常看板还检查首卡位置。矩阵中的几何断言不替代每种业务状态的视觉语义审查。
- `[comparison]`：当前正式 Vue 页面直接导入 `tests/account-captcha-web/interface-layout.fixtures.ts`，与原型的 `fixture=baseline` 使用相同的五项目、七状态任务及 Provider 字段，各 1440 浅色 / 390 深色；断言原型实际名称、描述及配置与该独立输入相同。Provider 旧页先打开同一对象的“查看”，两边均展示所选对象后截图。旧截图重新从当前源码生成，不冒用原审计图；旧 fixture 的模型集合为空。新协作样板没有借旧 11 路由证据作验收。
- `[interaction]` / `[keyboard]` / `[touch]`：通过实际点击、Tab / Shift+Tab、Enter、Escape 和触控输入检查创建受理→就绪阶段、失败恢复、Unknown 保留原请求、只读、模态焦点约束及返回、44px 核心触控范围。只证明样板内行为。
- `[detail]` / `[knowledge]`：实际打开 Task 详情与 Model 编辑并检查返回焦点；四格式 Knowledge 通过目录进入正文，检查 Markdown 窄屏正文位置、PDF 原文件与预览载体、DOCX 解析失败后仍可下载。浏览器实际下载后核对版本文件名与 PDF/ZIP 格式签名。允许的 `chrome-extension://mhjfbmdgcfjbbpaeojofohoefgiehjai` / `chrome://resources` 仅为 Chromium 自带 PDF 阅读器本地资源，仍阻断外部服务与业务写入。
- `[recovery]`：保留作者发现的两条 MCP/OAuth 缺陷回归：删除 Bearer 凭据后重新配置可连接；直接确认 OAuth 返回后首次进入 MCP 正确选中 GitHub 并可连接。均检查随后“刷新发现”可用；没有真实认证、凭据保存或外部连接。
- `[accessibility]`：从浏览器最终 computed token 按 sRGB 相对亮度计算对比度，正文/说明目标 4.5:1，焦点目标 3:1；覆盖最终 indigo，两主题。200% 以 720×480 CSS viewport / DPR=2 对应 1440×960 的等效重排检查，**不宣称已操作浏览器原生缩放或屏幕键盘**。减少动效检查真实媒体查询与计算样式。

默认每种模板保留 1440 浅 / 390 深正常全页截图，对照另有 before/after，其他失败保留截图与 trace。所有原始产物在忽略的 `output/ai/frontend-f0/` 下；需要保留的最小审查图由设计与独审负责人选择。结果中的失败首先区分产品、fixture、环境与观测问题；修复后只重跑相关范围，不覆盖首轮输出。

最早的服务探针曾收到 Vite/native 工具初始化后的 SIGTERM，原命令实际返回 143，未观察到预期关闭日志；随后 server 改为受本 harness 管理的 middleware HTTP listener，并在工具初始化后注册关闭处理。该探针不记作页面行为失败；`server-lifecycle.json` 记录本轮 PID、端口及是否实际完成关闭。最终仍核原命令终态。

作者首轮事实保留在忽略输出中：`projects-first` 为项目列表 6/6；`base-first` 暴露模态反向 Tab 逃逸；`matrix-first` 为 49/51，仅会议 390 浅深溢出；`targeted-second` 原焦点、会议窄屏、详情、触控与重排 8 项通过，新增 Knowledge 两项分别发现正文 y=423.53 超过 380 目标，以及 harness 误拦内置 PDF 资源。后者是测试代码问题，已按协议/固定内置扩展身份修正，未放宽外网限制。Provider 初次 before 图未选对象，经独审指出后改为先打开同一对象，早期图不作最终展开状态对照。修复后的正式结果以对应实际运行记录及独审结论为准。

2026-10-15 的最后定向结果为：`final-base` **21/21**（基础三类同字段与同选择态对照、四格式、详情/回焦/触控及受影响窄屏矩阵）；`final-pdf-token` **4/4**（四格式两尺寸、最终 token、200% 等效重排与减少动效）；`final-recovery` **2/2**（上述两条恢复链）。三组原命令均 exit 0，所属 server 关闭记录均为 `closed: true`。完整入口共 62 项，结论来自完整矩阵与定向复验的组合，未声称一次运行全部 62 项。独立验收仍由未参与实现与 harness 的审查者形成。

PDF 早期截图还暴露了观测时序问题：原文件正确、内置阅读器已出现，但绘制尚未完成。最终检查等待 Chromium 内置扩展 frame 的 `pdf-viewer` 自定义元素完成注册，随后有界轮询 `loadProgress_ === 100` 才截图；两尺寸真实页像已查看。该观测与本轮 Chromium 151 绑定，换浏览器时需重新核对，不能把空画布或仅文件签名当作预览完成。

最终 indigo 的浏览器 computed token 矩阵最小正文/说明对比度为浅色 **4.811:1**、深色 **5.936:1**；必要边界/焦点矩阵最小为浅色 **3.113:1**、深色 **3.394:1**。这是所列语义 token 组合的测量，不是所有任意叠色或禁用态的自动可访问性认证。通过结果对应最终 `--strong-border` 浅色 `#868b80` / 深色 `#737969`。

资源收尾发现 Playwright 原先经 shell 启动 Node 后，三个已关闭服务留下由容器 PID 1 持有的 Z 记录；它们无运行或监听，不能把“已关闭”改写成当时已回收。配置现用 `exec node test-server.mjs`，`final-server-reap` 的单项检查和原命令均通过，静态/Vite 均关闭，实际服务 PID 已不存在。所有本测试者浏览器已关闭；独审确认不再消费正式 Vue 后，已精确移除本任务 `web/node_modules` 软链，仓外锁定安装继续保留。独审自己的静态服务由其 owner 收尾。
