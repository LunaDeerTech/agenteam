# F0 独立视觉与交互审查

**结论：F0 独立视觉与交互审查通过。所有已知阻塞已返修并定向复验关闭。** 审查者未参与设计、实现或设计决定；产品文件只读，缺陷交原作者修复。依据 [目标设计 v2](../../target-design.md)、[页面蓝图](../../page-blueprints.md)、[F0 门槛](../../implementation-plan.md#4-f0完整页面样板与视觉系统定稿)及 Web Design Guidelines。

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
