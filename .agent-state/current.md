# 当前工作：核心开发规划已固定，产品工作包待派发

用户已确认以真实任务执行与评审闭环优先推进完整平台；目标、四个里程碑、五个工作包和依赖顺序统一见[开发计划](../docs/development/development-plan.md#当前开发目标与里程碑)，真实状态见[台账](../docs/development/agent-team/tasks.md#当前目标与工作包状态)，派工方式见[团队流程](../docs/development/agent-team/README.md#按当前目标派工与联调)。

- 本次文档归位已完成：开发计划、台账、团队流程、任务模板、README/AGENTS 入口与本恢复记录；没有修改产品代码，也没有新增产品 PASS。
- 本次规划归位的输入基线为 `6d16bdae`；本文作为后续核心开发的协调接续入口。文档提交、推送、主线集成与临时分支清理由 root 在实际 Git 操作后报告，本记录不预先宣称远端保存成功，也不把本次临时文档分支列为长期活跃任务。
- W1–W5 产品工作包均待派发，尚无继承的 owner/资源窗口。下一步由 root 指定工作包负责人、集成负责人及独立验证者，明确真实接缝与唯一写入权后启动。
- 首先核 tools 原独验停止原因/恢复条件，组织真实 Model → read-file → ToolResult → 下一 Model 的候选组合；复用已存在的 Agent/Task/Runner/DirectText 能力，同时并行准备审批/取消/详情与检索基础。全部原 STOP、FAIL 和未验边界保留。
- 本次只验证文档的一致性、链接/fragment、UTF-8/LF 与差异空白，不运行产品测试；规划完成不意味着 M1 或全平台完成。

## 已完成交接：统一页面布局

以下保留 `6d16bdae` 的前端范围交付及恢复材料，作为已完成交接；不再作为当前开发主任务，原结果和限制不由本次规划改写。

用户限定本轮只调整前端样式与页面布局。按[统一界面规范](../docs/frontend-design/interface-design.md)整理共享壳、现有正式页面、创建/编辑载体与响应式；不改后端、API、请求生命周期或既有业务恢复合同，不补造会议、Inbox、Explore、全局搜索。

本轮 UI 已完成：共享 App/导航/页面布局、首页与项目列表、账号入口、项目/系统/个人设置、任务工作台、知识库与产物。正式源码位于 `web/src/`，布局说明位于 `docs/frontend-design/`，验证源码位于 `tests/account-captcha-web/interface-layout.*`。恢复以 main 内这些文件、锁文件和下列命令为准；没有尚未解决的本轮 UI 布局阻塞。

### 可重复验证与实际结果

浏览器入口为 [interface-layout.config.js](../tests/account-captcha-web/interface-layout.config.js)，同前缀 server、fixtures、spec 为完整可恢复源码。锁定 Node 24.19.0、Playwright 1.56.1，使用本机 `/usr/bin/chromium` 151；依赖由 package-lock 恢复，不使用 latest 下载。命令在仓库根执行：

```sh
node tests/account-captcha-web/node_modules/@playwright/test/cli.js test --config=tests/account-captcha-web/interface-layout.config.js
npm run test:unit --prefix web -- src/tests/project-audit.spec.ts --no-cache --maxWorkers=1
npm run type-check --prefix web
node web/node_modules/vite/bin/vite.js build web --outDir="$PWD/output/ai/interface-layout/dist"
```

- 真实 Chromium 共55项：39个正式路由实例与16项代表交互/预览。最后整轮54通过，唯一 Agent 最终回焦断言误期望后备标题；实际按产品规则回到“创建 Agent”。修正测试定位后该项独立复验1/1通过，未改产品、未放宽锁焦与草稿断言。原整轮 exit1 与最终复验 exit0 如实保留。
- 覆盖核心入口8、个人设置3、系统设置11、项目设置11（含技能深链）、任务3（含 Sprint/Task 深链）、知识2、产物1；具体合法路径与标题见 fixtures 的 routes。每条检查真实路由挂载、唯一页面标题、错误状态、控制台与页面横向溢出。
- 代表模板覆盖浅深色、390/834/1024/1440/1920、长内容、减少动效、限宽与侧栏、目录/正文滚动及模态键盘。项目列表1920时外容器1120；设置最大1200、菜单216。200%用720×480 CSS像素与2倍像素密度验证等效重排，未声明实际操作系统缩放或移动屏幕键盘证据。
- 实际交互包括项目加载/空/失败重试与单次创建；Task、Milestone、Sprint 三个 footer 各一次提交；Task/Agent/普通变量草稿确认和返回编辑；设置与知识 Drawer 关闭/选择回焦；产物834/390详情替换及返回原按钮；Secret Unknown 模态内查证且不重发；Provider初焦与有效邀请/重置表单。
- 类型检查与生产构建在冻结产品上实际 exit0；后续只撤回基线纯格式，产品语义不变，复用该证据。导航相关 project-audit 单文件在最终范围上独立49/49通过。相对 df20d9ea 的110个实际变更前端文件限定 Prettier 检查通过；新增浏览器源格式与 diff-check 通过。

浏览器使用明确的合成 HTTP fixture，经过真实生产解码器、控制器、Vue 与 Chromium，只证明前端行为；未运行真实后端联调、数据库或 Go 测试。所有代表截图已实际查看。六张最终预览保存在原主树 `/workspace/agenteam/output/ai/interface-layout/preview/`（project-list、project-list-mobile-dark、settings、tasks、knowledge、artifacts PNG），可在临时工作树清理后继续查看。它们仍是接口 fixture 预览；知识短正文与压力用长正文分开，任务最终正常图无读取失败。可重复生成产物不纳入 Git。

### 本轮范围外的既有检查限制

首次全量 unit 为202文件、3686项：189文件通过，3654项通过、32项失败。1项为本轮导航旧期望，已修并独立49/49复验；其余31项为基线请求生命周期相关测试失败，部分已在 df20d9ea精准复现，其余共享同一旧fixture机制。用户明确只做前端布局，12份生命周期测试修复没有纳入，不修改生产行为、放宽预算或跳过这些测试。全局格式检查的17个基线文件也保持原样，撤回了曾临时进行的纯格式处理。因此不宣称全局 npm check 全绿。

既有31项及17文件的事实保留在首轮日志；原主树 `output/ai/interface-layout/` 已复制 unit.log、browser-final.log、browser-agent-recheck.log、audit-recheck.log 作为当前机器的简短证据。正式恢复材料是上述跟踪源码、锁文件、命令与本摘要，不依赖临时树输出。后续仅在另行授权涉及这些模块时处理，不扩大本轮范围。

### 恢复边界

所有验证进程均已退出；可写缓存、截图和构建位于忽略的 `output/ai/interface-layout/`。依赖按锁文件恢复，包含 go-captcha-vue 2.0.7。本轮没有待继续的生命周期测试修复；既有未完业务以[任务台账](../docs/development/agent-team/tasks.md)为准。
