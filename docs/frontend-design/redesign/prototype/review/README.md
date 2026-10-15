# F0 独立视觉与交互审查

## 2026-10-15 用户实看拒绝后的独立诊断

**当前结论：`b6db5297` 的基础整页视觉 FAIL，F0 未获用户接受。** 用户指出下拉、信息层次、动画和整体精致度明显不足，并确认项目内“待办”可以保留；项目“设置”直接进入 MCP、遗漏基本信息和 Agent 等栏目是结构缺陷。以下旧报告保留技术事实，但其中的视觉 PASS 不能继续作为当前接受依据。本轮审查者未参与被审设计、实现或定稿决定；未修改样板实现，也不替设计者选定布局或 token。

已在真实 Chromium 中亲看 Projects、Tasks、系统 Provider 三页完整页面及筛选展开、hover、键盘焦点、新建 Dialog 过程。独立诊断脚本从 Git 只读服务冻结的 `b6db5297`，避免作者并行工作改变输入；主输入为 1440×960、浅色、正常合成数据、100% 缩放。下列尺寸是本原型实测，用于定位问题，不是 Linear 的数值或新设计决定。

| 位置 / 状态                    | 观察与影响                                                                                                                                                                                              | 参考与返修方向                                                                                                                                                 |
| ------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Projects 正常列表，y=254—745   | 六个两行对象每行 **81.80px**，总高约491px；名称与短说明的内容量远小于占用空间。全宽分隔线和空白成为视觉主体，名称、状态与进入动作横向分离，像松散管理表而非聚焦的工作集合。                             | L1 的弱化边界、L4 的连续扫描。重新校准真实内容、行节奏和对象/状态/动作距离，不能只把字号缩小。                                                                 |
| Tasks 正常看板，顶部三层与卡片 | 系统层48px、项目层52px、视图层57px，用同色长横线连续划分；项目层、视图、画布、白卡片的表面区别很弱。卡片264×121px，标题13px，主要靠同色描边表达可操作对象。整体平且机械，当前对象缺少视觉中心。         | L1 2026 的 chrome/content 区分、较柔和分隔；L2 整页的内容与辅助栏权重。需要共同审查表面、排版与局部控件，保留真实七状态及项目上下文。                          |
| 系统 Provider 详情，x=508—1268 | 字段左缘x=508、输入左缘x=948，相隔440px；五行配置每行约86.80px。页面标题、对象标题、Tab、配置标题和重复说明累积，三栏之间的距离感大于组内关联；完整页面高1100px，少量配置显得冗长。                     | L8 官方设置图中的同组表面、字段/说明/控件关系。校准分区节奏与表单凝聚程度，不能把“分了三栏”和“有分隔线”当作成熟设置页。                                        |
| Projects 的“全部状态”实际展开  | 原生下拉是约108px宽的直角白框、系统蓝选中条；与鸢尾蓝、圆角按钮和页面表面不是同一视觉体系。闭合态通过对比度检查掩盖了展开态差距。                                                                       | L6 官方 priority menu 的浮起表面、内部选中区域、边缘与间距。完整检验关闭、展开、hover、focus及选择后的状态；原生语义本身不是缺陷，当前呈现与用户明确目标不符。 |
| 新建项目 Dialog，正常动效      | 实际点击后按浏览器帧观察约300ms：正常模式18帧均为 opacity=1、transform=none、transition/animation=0s，动画实例为0；早期与稳定截图均已直接处于最终画面。当前打开过程是瞬时切换，没有用户要求的过渡质感。 | 动效应服务层级/状态切换，并能被输入打断、响应减少动效。未取得 Linear 官方精确动画曲线，不据此捏造数值；不能通过“没有持续动画”证明正常动效质量。                |

本轮有限通过仅为：行 hover 确有浅底变化；Tab 后“刷新”呈现2px可见焦点环；减少动效媒体条件实际生效，Dialog 没有持续运动。减少动效规则还产生短至0.01ms的过渡记录，不能把它写成正常动画已实现。旧 `capture.mjs` 默认 `reducedMotion: "reduce"`，因此旧静态矩阵不能证明正常动画。原技术测试的权限、恢复等证据按原限定范围保留；本轮没有重新认证完整功能或可访问性。结构正确性另由实际路由审计负责，完整项目设置仍须补齐，系统与项目 Provider 不能混用。待办保留不构成失败项。

**官方参考边界：** 已重新下载并亲看 L1 的2026导航/边界配图、L2 的2024 After整页、L6 的展开菜单及L8的设置截图，另在1440×960浏览器画布中完整展示原图作整体比例对照。L1只有局部示意，L2明确为历史版本，Docs截图版本未声明；这些不是可登录的2026完整产品视口，也不与本项目共享fixture。只比较层级、表面、信息关系和控件完整度，不量取官方精确token，不复制营销背景。来源及固定资源URL在脚本中可恢复，原图与同画布参考图仅放忽略输出。

复现使用 [refinement-diagnostic.mjs](refinement-diagnostic.mjs)；唯一新增保留图为[实际展开的项目筛选](screenshots/refinement-project-filter-open.png)，原有项目整页图继续可用。当前诊断输出在 `output/ai/frontend-f0-refinement-review/diagnostic/`，含三页viewport/完整图、hover/focus、动效早期/稳定图、逐帧记录及五份官方参考；截图仅支持其实际观察范围。

```sh
node docs/frontend-design/redesign/prototype/review/refinement-diagnostic.mjs --list
node --use-env-proxy docs/frontend-design/redesign/prototype/review/refinement-diagnostic.mjs --references
```

实际环境为 Node24.19.0、锁定Playwright1.56.1、Chromium151.0.7922.173；最终命令exit 0，浏览器、context与随机loopback服务均关闭。首次直接Node fetch未采用环境代理，官方素材获取失败、原命令exit 1；产品观察已经完成且资源已关闭，失败输出保留在 `diagnostic-initial-reference-fetch-fail/`。使用环境代理后取得五份官方素材；这次工具成功不改变视觉FAIL。按用户最新要求，当前只完成失败定位，未铺开新稿矩阵；下一稿需先实看三基础整页与控件/动效，再决定扩大检查，最终用户实看仍待发生。

## 上轮审查记录（历史事实，非当前视觉接受结论）

**上轮记录的结论：F0 独立视觉与交互审查通过，已知阻塞已返修并定向复验关闭。该视觉结论已被用户实看拒绝，不再代表当前接受状态。** 审查者未参与设计、实现或设计决定；产品文件只读，缺陷交原作者修复。依据 [目标设计 v2](../../target-design.md)、[页面蓝图](../../page-blueprints.md)、[F0 门槛](../../implementation-plan.md#4-f0完整页面样板与视觉系统定稿)及 Web Design Guidelines。

本轮从当前 main M3 基线 `d55bf044` 开始，审查作者停写的 F0 页面；MCP/OAuth 最后恢复修复输入为 `558b246a`，Knowledge 低高度阅读最终冻结输入为 `dca7633f`。结论仅限合成样板的视觉与交互契约。页面底栏、文件输入和提交反馈明确本地演示；没有验证真实 API、权限、持久化、上传解析或 OAuth 服务，也不代表 F1—F8 产品迁移完成。

## 实际观察

| 范围             | 独立观察与操作                                                                                                                                                                                                                                                                                                            |
| ---------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 11 必需页        | 亲看 Projects、Task 看板/详情、Provider/Model、系统/项目 Dashboard、Knowledge、Meeting、Inbox、MCP、OAuth、Memory 的整页。主输入为 1440×960 浅色与 390×844 深色，另捕 1440 深色/390 浅色并检查主题及布局。连续行、正文、上下文与就近动作成立；没有把旧11路由审计图当作新增页面。                                          |
| 三基础同输入对照 | 亲看当前 Vue 与 `fixture=baseline` 的同尺寸、主题和对象状态：五项目、七状态任务、Research Provider。Provider 对照已改为旧页同样展开所选对象；旧 fixture 的模型集合为空，Model 编辑和新 Task 详情仅属于新样板验证。                                                                                                        |
| Knowledge        | TXT/Markdown 原文、PDF 原生 viewer 的实际页像、DOCX 解析中及失败/原版本下载边界。390 目录为单面板，关闭回焦；草稿按文档隔离、恢复、保存明确未提交服务端；分段未读完不能编辑。PDF 实际页像已亲看，不以 iframe 或 PDF 文件签名代替。                                                                                        |
| 协作五页         | Meeting 附件停传→原结果查询→插入引用，保留消息草稿；审批 Unknown 的首次 pending/后续确认。Inbox 只回当前项目来源会议处理。MCP 系统配置只读与项目连接分开、发现错误就近显示、Unknown→stopped→重新读取。OAuth 原兑换查询及返回原 GitHub。Memory 当前/已替代修订、删除确认、Unknown 保留原记录、查询后移除并回到同一 Agent。 |
| 状态与可达性     | 实际浏览器覆盖正常、长内容、空、加载、读取失败、只读、Unknown 的代表输入；其余组合几何矩阵引用[作者记录](../test-README.md)，不声称逐个视觉穷举。项目/Memory 对话正反 Tab、Escape 回焦，Provider 方向键标签，Task 草稿关闭及跨对象隔离通过。Artifacts 仅检查配套导航、文本原文、返回、合成边界与窄屏无横溢。              |
| 缩放与对比度     | 真实 Chromium 原生 200%：外宽 1440、CSS 内宽 720、DPR 2。11页未见全页横溢，reduced motion 下没有持续动画；Knowledge 的后继可读性缺陷已返修：实际正文、wheel 与继续读取通过，不能只据几何通过。最终必要输入边界实测浅/深 3.489/3.690:1，2px 键盘焦点 5.928/7.097:1；渲染后文本候选检查无未解释低对比度项。                 |

[L1—L8 结构及动作依赖映射](../README.md#页面与参考结构)已对照实际页面核查：框架层级、列表/工作台、概览、看板、属性、输入关闭、分组设置均有对应；样板没有引入新的业务命令。完整 API/composable 接续仍在该入口维护。

## 缺陷与定向复验

| 页面/输入与位置                  | 初次观察、影响与规范依据                                                                             | 当前结果                                                                                                                                |
| -------------------------------- | ---------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------- |
| Task / Unknown / 列头加号        | 顶部禁用但列头仍可创建；违反原意图查询与统一写门禁。                                                 | 所有等价创建入口及函数守门，原步骤通过。                                                                                                |
| Task 1 草稿→保留关闭→Task 2      | 第二个任务显示第一个任务草稿；违反稳定对象身份。                                                     | 按任务 id 隔离，1440/390 原步骤通过。                                                                                                   |
| Provider / 配置标签 ArrowRight   | 焦点不移动，`tablist` 语义不完整。                                                                   | 方向键、roving focus 与对应面板通过。                                                                                                   |
| 390 Projects/Provider / 键盘焦点 | 固定样板栏覆盖末行入口或保存按钮。                                                                   | 样板栏改文档流，受影响焦点路径通过。                                                                                                    |
| 390 Task 详情 / 标题下           | 当前状态/负责人落到正文与历史之后，打开时不能判断。                                                  | 标题下就近摘要已亲看确认。                                                                                                              |
| MCP Bearer / OAuth 首次返回      | 作者报告删除凭据重配后仍锁定；直接 OAuth 返回未初始化 MCP 仍锁定。                                   | 独立完成删→配→连接及 OAuth 直达→确认→GitHub→连接，原失败路径通过。                                                                      |
| Knowledge / 长名 / 原生 200%     | CSS 720×408 下正文滚动区只有32px，全部为底部 padding；文字可视区近零。违反缩放后正文可达与阅读优先。 | 低高度改为页面滚动；原生200%正文区域1309.59px、有效内容1278px，实际wheel与继续读取可达，亲看正文多行；390/1440及目录/草稿旅程复验通过。 |

作者侧先前对话焦点逃逸、Meeting 390 横溢及必要控件边界修复也在最终输入上观察通过；原 FAIL 留在作者输出。独审最初五项失败留在 `output/ai/frontend-f0-review/interaction-base-initial-failures.json`。200% 的最初“无横溢”不能代表可读性通过，上表记录后继发现。

## 最小截图与复现

保留未改动的浏览器原图：[项目](screenshots/projects-1440-light.png)、[任务详情](screenshots/task-detail-390-dark.png)、[Provider](screenshots/provider-1440-dark.png)、[窄屏 PDF](screenshots/knowledge-pdf-390-dark.png)、[Meeting](screenshots/meeting-1440-light.png)、[Inbox 来源](screenshots/inbox-source-390-dark.png)、[Memory Unknown](screenshots/memory-unknown-390-dark.png)。另保留[Knowledge 原生200%阅读](screenshots/knowledge-native-200pct.png)，共8张。其余截图/测量/失败输出留忽略目录 `output/ai/frontend-f0-review/`，可再生，不另建永久验收档案。

[截图与渲染色检查](capture.mjs)、[键盘/基础页/原生缩放](interact.mjs)、[知识与协作旅程](journeys.mjs)先用 `--list` 发现入口。`capture.mjs` 自启随机 loopback 服务；另两脚本连接自有 `F0_REVIEW_ORIGIN`（默认5198）。Playwright 1.56.1，Chromium 151.0.7922.173，隔离容器使用 `--no-sandbox`；锁定依赖配置同作者入口。

```sh
F0_PORT=5198 node docs/frontend-design/redesign/prototype/test-server.mjs
node docs/frontend-design/redesign/prototype/review/capture.mjs projects,tasks,providers normal
node docs/frontend-design/redesign/prototype/review/interact.mjs base
node docs/frontend-design/redesign/prototype/review/journeys.mjs content
node docs/frontend-design/redesign/prototype/review/journeys.mjs collaboration
F0_REVIEW_PAGES=knowledge node docs/frontend-design/redesign/prototype/review/interact.mjs zoom
```

观测限制：完整页截图在 Chromium 原生 zoom 下存在 Playwright clip 坐标差异，最终缩放图改用 CDP 实际 viewport，未改产品布局规避。PDF 守卫需允许 Chromium 内建 viewer 的扩展源与 `chrome://resources/`；先前灰图是观察器拦截，已纠正并亲看真实页像。两次旅程初测误把空密码 input 留在关闭 dialog 当作未清空、未展开 DOCX 演示 details；观测修正后的定向旅程通过，初次输出保留为 `journeys-*-observer-initial.json`。

资源终态：独审静态服务5198已收 `F0_SERVERS_CLOSED`、进程 exit 0；各浏览器 context、原生缩放临时 profile 与自启随机服务均在 finally 中关闭。没有遗留本任务浏览器或服务。未执行 Git 写操作，由 root 集成。
