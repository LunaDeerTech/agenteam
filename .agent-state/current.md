# 当前状态：新 Vue 工程已暂停，保存未验收 WIP

2026-10-16，用户要求暂停、记录全部当前内容并 commit/push，随后在新对话接续。全部实现、测试与独审 owner 已停写；本记录描述暂停时的实际断点，不是完成或通过结论。

## 用户已确认的方向

- 旧 `web/` 归档为 `web-crape/`，新 `web/` 从零建立，不复用旧 API 客户端、Session、权限或业务实现。必要后端接口随具体页面需求重新设计；当前没有重建后端或实现认证授权。
- 先共享 UI、AppShell、**单一 SettingsShell** 和全部已定路由骨架，系统设置优先；未讨论正文只保留标题与空内容区域，不填假数据、旧表单或业务动作。系统/个人/项目设置分别为 11/3/11 个叶子，共用设置壳。
- 不再等待全站 HTML F0 完成。Agent HTML 保留对照、不继续扩功能；本轮不迁入完整 Agent 或项目列表正文。旧样板和旧 Vue 的结果均不代证新工程。

## 分支、保存与唯一写者

- 工作树：`/workspace/agenteam-vue-rebuild`；分支：`ai/frontend-vue-rebuild-20261016`；起点 `746ddcb3`。`web/` → `web-crape/` 的 `7a4fc14a` 已推远端并设置 upstream；暂停前其余 WIP 尚待 root 本次保存。恢复以远端该分支最新提交及实际差异为准，不依赖本状态的自提交哈希。
- `main` 仍为 `b6db5297`；旧 `ai/frontend-f0-refinement-20261015` 保留在 `746ddcb3` 供参照，未合 main。不要回旧树修改其 `current.md`。
- `/root` 是本轮唯一协调会话，执行全部 Git、分支、工作树和集成。新工程紧耦合，共用本树但按文件唯一写者划分；所有 owner 现已停写，接续先确认旧会话无写入活动。

| Owner                             | 已保存到工作区、待本次 checkpoint 的范围                                                                                                                                                                                                                                                                                                                  | 暂停时断点与检查事实                                                                                                                                                                                                                                                                |
| --------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `/root/agent_completion_designer` | `web/package.json`、独立 `package-lock.json`、`index.html`、`tsconfig.json`、`vite.config.ts`、`.prettierignore`、`.prettierrc.json`、`public/.gitkeep`；`src/main.ts`、`App.vue`、`router/{index,routes}.ts`、`views/SkeletonPage.vue`、`layouts/{PageHeading,SettingsNavigation,SettingsShell,ProjectNavigation,AppShell,AccountShell}.vue`，共 19 文件 | 已写路由、单一设置壳与空正文；`/system` → `/system/users` 只是源码首链。仅 `npm install --package-lock-only --ignore-scripts --prefix web` 实际 exit 0；未安装依赖或运行工程检查/浏览器。                                                                                           |
| `/root/vue_foundation_ui`         | `web/src/components/ui/{UiButton,UiIcon,UiIconButton,UiMenu,UiDialog,UiInput,UiTabs,UiSelect}.vue`、`index.ts`、`internal/{useAnchoredLayer,scrollLock}.ts`；`styles/{index,tokens,controls}.css`；`composables/useTheme.ts`，共 15 文件                                                                                                                  | 前五组件已导出并有 CSS；Input/Tabs/Select 已落盘但**尚未导出、缺专用 CSS**。主题、菜单、模态行为只是实现，未 build/unit/browser 验证。                                                                                                                                              |
| `/root/f0_structure_audit`        | 根 `.gitignore`；`tests/account-captcha-web/` 的 2 个旧 fixture server 与 8 个 e2e/helper；`tests/meeting/metadata-focus-{app.ts,browser.cjs}`；`tests/{m1-app-workflow,m2-app-workflow}/run.sh`；`tests/account/` 的 11 个历史 web fixture，共 26 路径                                                                                                   | 仅将历史 fixture 显式指向 `web-crape/` 并忽略归档产物，默认 deploy/Compose 仍用新 `web/`，未改后端/API 或归档内部。归档 569 文件 blob/执行位与原版本一致；限定 shell/Node/TS 解析、引用、gofmt、diff/忽略检查已过，未跑旧业务或新 Vue。新 lock 和最终 archive-import 隔离尚待复核。 |
| `/root/agent_completion_test`     | `web/tests/{theme,menu,dialog}.spec.ts`；`tests/frontend-rebuild/{README.md,browser-helpers.cjs,playwright.config.cjs,preview-server.mjs}`，共 7 文件                                                                                                                                                                                                     | 已写 10 个组件用例（4/3/3），尚未格式或运行。仅 config/server 的 `node --check` 通过及既有 Playwright 缓存/版本核对；浏览器配置指向尚不存在的 `foundation.spec.cjs`，harness 未完成。                                                                                               |
| `/root/f0_refinement_review`      | 无正式文件写入，未创建新 review 目录，未修改旧 review                                                                                                                                                                                                                                                                                                     | 只看参考图与旧 HTML，未检查新 Vue 的整页、路由、交互、动效或原生缩放。可再生截图/脚本在忽略的 `output/ai/vue-rebuild-review/`，不新增永久档案。                                                                                                                                     |
| `/root/f0_docs`                   | 本文件、`docs/development/agent-team/tasks.md` 顶部、`docs/development/development-plan.md` 当前目标、`docs/frontend-design/redesign/{README,implementation-plan,page-blueprints,target-design}.md`，共 7 文件                                                                                                                                            | 已将现行路线改为 fresh Vue 并记录暂停；旧 F0/M3 事实保留。仅文档格式/链接/限定 diff 检查，不是产品验证。其余开发说明与新工程 README 尚未归位。                                                                                                                                      |

## 当前首断点与接续顺序

1. 从本任务远端分支恢复，读本状态、限定 diff 和各唯一 writer 的 WIP；先确认最新保存与会话归属。新 `web/` 尚未启动，不把源码/路由存在当作可运行。
2. 按新独立锁安装依赖：仓库根执行 `npm ci --prefix web`。Node 要求 `>=24.12.0`；锁定 Vue 3.5.43、Router 4.6.4、Vite 8.3.1、TypeScript 5.9.3、vue-tsc 3.3.11、Vitest 4.1.11。暂停时未生成 `web/node_modules`、没有依赖软链。
3. 核对 UI 已导出接口、所需图标与壳消费；处理 Input/Tabs/Select 未接线状态。`router/routes.ts` 集中路由 metadata，核对系统标题“模型”/“Models”、“Runner”/“Runner 设备”的正式命名。未讨论叶页继续保持空 `data-page-content`，不要趁接线填业务。
4. 先运行必要格式、类型、构建和组件检查，再完成浏览器 harness 的 `foundation.spec.cjs`。工程已定义 `dev`、`type-check`、`build`、`preview`、`test:unit`、`format`、`format:check`、`check`，**均未运行**；`build` 含类型检查，`check` 含格式/单测/构建。首个可浏览目标为 `/system/users`：`npm run dev --prefix web` 使用 127.0.0.1:5173 strict；`preview` 使用 4173 strict。这是待执行命令，不是已验证启动说明。
5. 跑通新 Vue 系统设置首链及全部既定空路由，再由独审检查整体、菜单/焦点、浅深/窄屏、动效及缩放。测试自有服务拟用 5242，独审拟用 5243；先核端口与资源归属。旧 HTML 与归档业务通过不能补证。
6. 按实际工程合同补 `AGENTS.md` 长期目录/命令、根前端入口、`docs/development/frontend/{README,components}.md` 和新 `web/README.md`（由文档 owner 独写，当前不存在）；不要将这些尚未更新的旧说明当成新工程合同。旧 fixture 复现须显式构建 `web-crape/`，`*_WEB_MODULES`、`*_WEB_DIST`、`AGENTEAM_DIALOG_WEB_ROOT` 对应归档或其外部构建，不能链接新工程依赖。

## 暂停时资源与材料

lead 唯一生成 lock 的 npm 命令已实际退出；未启动 npm ci/dev/build/browser。UI 与工具兼容 owner 无自有运行进程。测试未启动本轮 install/server/browser，无 profile 或新软链，5242 无监听。独审的旧 HTML 参考截图服务 5243 及 browser/context 已关闭，原命令 join exit 0。上述为各 owner 的实际退出报告，不代表未来新命令自动清理。

本次保存源码、独立锁、harness、必要文档和恢复状态，不保存依赖、缓存、构建产物或可重建 `output/ai/` 日志。新工程尚无 type-check/build/unit/browser/视觉通过结论；用户未接受新设计，未合 main，也未启动系统设置业务正文或后端重设计。

现行路线见[重设计入口](../docs/frontend-design/redesign/README.md)、[开发计划](../docs/development/development-plan.md#当前开发目标与里程碑)与[任务台账](../docs/development/agent-team/tasks.md)。历史样板入口保留在[原型说明](../docs/frontend-design/redesign/prototype/README.md)、[测试记录](../docs/frontend-design/redesign/prototype/test-README.md)和[独审记录](../docs/frontend-design/redesign/prototype/review/README.md)，不改写其原结果。

## 保留的既有恢复入口

M3 明确范围已正式交付 main；接受范围、原失败和未覆盖边界继续见 [M3 卡](../docs/development/work-items/m3-knowledge-memory-collaboration.md)、[独审报告](../docs/development/work-items/m3-core-independent-review.md)及[任务台账](../docs/development/agent-team/tasks.md)。本轮设计工作不扩大其接受范围，也不解除原 STOP。

- Model 原测试和 helper 的恢复说明见 [Model harness](m3-model-runtime/reproduction/README.md)与[原工作卡](../docs/development/work-items/m3-model-runtime.md)。既有 compile-only 结果不等于本轮执行测试。
- 固定检索选型、排名、来源和 bootstrap 见[检索恢复入口](m3-retrieval-selection/README.md)；公开向量沿用 [runtime-embeddings-v2](m3-model-runtime/runtime-embeddings-v2/manifest.json) 的 gzip。Meeting SQL 测试和已审截图的位置沿上述正式工作卡恢复。
- 远端 `origin/ai/task-comments-20261013` 继续保留，保存尚未合入 main 的“不改变任务状态的独立追加评论”API、迁移、服务、UI 与测试，完整任务未交付；本轮不接管、不清理，也不改写其旧失败或验收事实。恢复该任务须另核分支实际代码、差异与会话归属，不能把其旧 `current.md` 当作本轮状态。

M3 后继缺口、完整 Lifecycle/全平台 Runner、生产 SPA/部署恢复和完整 D24/D25 token 流仍按原卡评估；`update-meeting`、`comment-meeting` 的可写语义仍未定义、未注册、未接受。不自动进入 M4、E01 或生产发布。
