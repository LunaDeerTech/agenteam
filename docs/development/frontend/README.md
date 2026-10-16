# 前端开发基础

当前 `web/` 是从零建立的 Vue 3、TypeScript、Vite 和 Vue Router 工程。旧前端完整保存在 `web-crape/`，新工程不导入其 API 客户端、Session、权限或业务实现。共享 UI、AppShell、唯一 SettingsShell 和已定路由骨架已有有限检查；当前实施[账号入口与个人设置](../work-items/frontend-account-rebuild.md)，包含真实会话与后端接入。其他未讨论的页面正文继续保留标题与空区域，不迁回完整 Agent/项目列表或虚构数据。

设计依据为[设计 v2](../../frontend-design/redesign/README.md)、[页面蓝图](../../frontend-design/redesign/page-blueprints.md)和[执行计划](../../frontend-design/redesign/implementation-plan.md)。可运行参考为 `ai/frontend-f0-refinement-20261015@746ddcb3` 的 [HTML 原型](../../frontend-design/redesign/prototype/README.md)，实现与审查均须在浏览器实际观察。原型的合成数据和历史检查不证明新 Vue 业务能力或设计已被用户接受。

## 启动与检查

使用 Node `>=24.12.0`，在仓库根目录执行：

```sh
npm ci --prefix web
npm run dev --prefix web
```

打开 [系统设置骨架](http://127.0.0.1:5173/system/users)。dev 默认仅监听 `127.0.0.1:5173`，端口冲突时退出；指定独立端口使用 `npm run dev --prefix web -- --port 5244`。本工程没有 `/debug` 展示页。默认dev保持 `VITE_ACCOUNT_ENABLED=false` 的静态预览；本轮账户联调在5245显式开启该变量，浏览器经真实同源TLS5250访问页面和Account API；提供方是长驻Go测试进程内的真实App与独立依赖，不能表述为独立cmd二进制部署。独立dev命令不启动后端；精确运行与部署边界见[web运行说明](../../../web/README.md)，实际结果见当前状态。

```sh
npm run check --prefix web
npm run preview --prefix web
```

`check` 依次执行 Prettier 只读格式检查、Vitest 组件测试、严格类型检查和 Vite 生产构建，产物位于 `web/dist/`。`preview` 仅用于本地检查生产产物，默认监听 `127.0.0.1:4173`；它不是生产部署。分项命令、远程 dev 的 Host/HMR 配置见 [web 运行说明](../../../web/README.md)。

账号真实旅程使用[账号harness](../../../tests/frontend-account-rebuild/README.md)，框架浏览器使用[骨架harness](../../../tests/frontend-rebuild/README.md)，两者按各自锁定Playwright说明运行；组件和受控状态测试位于 `web/tests/`。当前检查、失败与未验事项只在[当前状态](../../../.agent-state/current.md)维护，不从下方历史记录推定本轮通过。文档中的命令说明不等于已执行。

## 代码边界

| 路径 | 职责 |
| --- | --- |
| `web/src/components/ui/` | 八个共享控件与内部浮层/滚动工具，公开导出见 `index.ts`；[合同](components.md)依据实际源码维护 |
| `web/src/styles/` | 新工程独立的 token、基础样式和控件样式 |
| `web/src/account/` | 独立实现的同源传输、会话/命令、链接捕获、离页与字段校验 |
| `web/src/composables/useTheme.ts` | 共享主题呈现；账号页需按本轮合同区分服务端保存与临时预览 |
| `web/src/layouts/` | AppShell、AccountShell、唯一 SettingsShell、页头与两类导航 |
| `web/src/router/` | 路由、页面标题、设置 scope/栏目数据及项目导航 |
| `web/src/views/SkeletonPage.vue` | 尚未实现业务的叶子空正文 |
| `web/tests/` | 新工程 Vitest + Vue Test Utils 测试 |
| `tests/frontend-rebuild/` | 新工程框架生产产物浏览器 harness |
| `tests/frontend-account-rebuild/` | 当前真实TLS/账号提供方的七页旅程与边界harness |
| `web-crape/` | 完整历史前端，仅作参照；旧 fixture 显式消费该归档，不能作为新工程证据 |

共享组件不包含业务数据、API、权限或页面状态。后续页面须先明确内容、接口、状态和权限合同，再在新工程实现；不从归档自动复制客户端或业务 composable。依赖按新 `web/package-lock.json` 安装，不链接归档的 `node_modules`。默认 npm 和开发 Compose 的构建来源仍是新 `web/`；本轮骨架检查不代表 Compose、后端或生产 SPA 组合已验收。

## 骨架与新页面

当前保留 40 个正式叶子定义：系统设置 11、个人设置 3、项目设置 11，其余 15 个覆盖首页、项目集合/概览、四个账号入口、任务三个入口、知识库、产物、会议、待办及 MCP OAuth 回调。动态详情仍属于各自叶子；三个目录重定向和 404 不计入 40 叶。URL 形状沿[页面蓝图](../../frontend-design/redesign/page-blueprints.md)，新代码参数名为 `owner/project`、`taskId/sprintId/documentId/meetingId/skillId`。

路由与设置栏目集中在 `src/router/routes.ts`；骨架建立时全部叶子使用 `content: "unimplemented"`，本轮七页依合同接入实际内容，其余保持空正文。`/system`、`/settings`、`/:owner/:project/settings` 分别重定向到用户、个人资料和基本信息。HTML5 History 支持直达与返回；保留命名空间避免未知系统路径落入项目路由。这些路径可访问不表示认证、权限或业务已经实现。

AppShell组织系统导航、项目上下文和内容区；“个人设置”只保留在右上用户菜单内，菜单触发器显示当前身份已保存头像或默认占位。顶栏与资料页共享受控头像读取状态，页面候选预览独立，全部请求仍经过同一账号owner。AccountShell承载账号与OAuth回调。login与forgot-password复用同一居中轻边框面板模板，包含正式Logo/标题/当前表单并用克制阴影；个人设置继续开放表单。系统、个人和项目设置均由同一 SettingsShell 消费对应栏目数组，窄屏目录由共享 UiDialog 承载。布局职责与控件接口见[组件说明](components.md#页面组合与滚动)。

骨架阶段的主题使用本地 `agenteam.vue.appearance.v1` 缓存。本轮账号主题改为服务端保存与临时预览分离；匿名/失效使用system，本地缓存不作为用户偏好事实，不得跨账号继承。实际迁移实现与检查以本轮工作项及当前状态为准。

## 归档前端能力与验收记录（历史）

以下内容保留旧前端的实现、失败和限定验收事实，源代码现位于 `web-crape/`。其中“当前”“本轮”、历史 `web/` 路径、账号/API/权限及路由能力均指各记录当时的版本，不属于上方新工程的完成范围；原后端接受边界与 STOP 不因前端重建改变。旧组件/Debug 和账号基础验证另见[历史验证记录](verification.md)。后续页面以已确认产品目标和重新设计的接口实施，不从这里直接推导新源码已具备的能力。

## 普通 Project Variables

`/:username/:project_name/settings/variables` 提供已有 initialized Project 的普通变量目录、详情、创建和编辑。页面先等待当前 Human Session 与 Project Resolve→Get 授权；普通 Owner 与管理员使用相同的 Owner 边界，正在归档和已归档项目只读。接口沿[普通变量后端契约](../work-items/d10-project-variables-owner-http.md)与[正式 OpenAPI](../../../api/openapi/project-variables.json)，不新增后端写入者或迁移。

目录按每页 50 项显式继续，摘要不含 value；详情显示完整原文。创建、保存都需要明确确认，字段不会自动修剪；编辑仅提交用户明确修改的字段，description/value 的空字符串表示清空。已确认命令的历史 receipt 不直接作为当前详情，必须重新取得当前 Get/List。版本冲突保留输入、字段 presence、目标和旧版本，用户先重读当前值，再明确采用当前版本并重新确认保存；不会自动合并、换版本或重发。

结果不确定时只提供原 key、原完整意图的 Lookup，不自动重发或轮询。同 Session checking 期间隐藏页面内容，Session 内原 Unknown 追踪继续保留；只有当前身份与项目重新成立后才可查询。真实 Session/Project 切换和当前 401/403/404 拒绝清除旧材料。输入与请求材料不进入 URL、storage 或诊断日志。

限定实现已有前端定向检查及类型/构建证据；版本冲突保留输入、同 Session 恢复不丢 Unknown 两项修复已独立复核。[真实 fixture](../../../tests/projectvariable/project_variables_web_test.go) 的 `TestProjectVariablesWeb/create-update-and-clear` 已完成一次 Create、两次 PATCH（含显式空值）及各次完整 Get/List，13 个已观察 API 请求全部 finished，10 份 Variables 证明覆盖初始目录、三次写与三对当前 Get/List；浏览器零 Lookup，服务后验以原 key Lookup 核三条历史 receipt、canonical/Audit/Outbox 与其他域事实不变。原 supervisor 收齐 Node/driver 实际 Wait、七资源及 private/runtime/descendants/TCP 双尾；浏览器覆盖键盘确认、焦点、减少动效和横向溢出，1440 浅色与 390 深色图已独立查看。控制检查首次环境遗漏失败与前端原失败保留，不能写成当前 HEAD 单次全量通过。

本片不含删除、Secret、UI 原命令重放、Agent/Runner 环境注入或完整 Variables 生命周期；正常浏览器链不代真实权限/Unknown 全矩阵。生产 Project initializer 和 Central SPA 发布仍未由此完成；上述有限成果已随 main `3e28f10a` 交付，开发历史与原件的保全及早期失败输入限制见[恢复说明](../../../.agent-state/project-variables-resume/README.md)。

## Project Secrets 安全目录

已有 initialized Project 的当前 Human Owner 可在 `/:username/:project_name/settings/secrets` 分页查看安全目录，选择条目后独立读取详情，并明确刷新目录或当前详情。八字段 DTO 仅包含身份、名称、描述、版本和时间，拒所有额外材料字段；不复用普通变量 value DTO，不显示接口未提供的有效性或授权 Agent 信息。

页面实例绑定当前 Session、Project 与路由；checking、失权、换身份或离页清观察，迟到响应不能发布，原 body/cancel 实际退出前不释放唯一请求 owner。完整页校验后才追加，失效游标回第一页。打开详情立即聚焦稳定标题，loading 使用通用标题，网络返回不会抢走其他焦点；返回目录恢复选中按钮焦点。

[限定交付卡](../work-items/d10-project-secrets-read-ui.md)记录分段前端控、133项共享回归和最终类型/构建结果。默认 App 真实链经正式服务准备51项，验证50+1目录、末项独立Get和显式刷新Get；7个请求为6 finished与一个实际Session GET aborted，7份完整联合证明，四个Secrets GET均finished，零浏览器写入/Lookup。服务端随机值、受保护字段和SQL快照留在Go进程；真实键盘焦点、1440浅色/390深色两图、持久事实不变及原Wait/七资源/所有退出尾已独立接受。首次真实焦点FAIL及修复前的控制/构建FAIL保留，不外推创建、改值、删除、Agent授权、真实撤权全矩阵、完整D10、生产Project initializer或SPA发布。

## Project 模型调用记录

当前 Human Owner 可从项目设置“模型与 Provider → 调用记录”进入 `/:username/:project_name/settings/model-usage`，读取近30天调用明细。每页50条，提供下一页、上一页、刷新及本页详情；详情不另发请求。Invocation 的24字段安全 DTO 与分页外壳的2字段分开校验，历史名称按调用时记录展示，nullable计数显示“未知”，零和大int64保持原字符串。

首读/刷新建立一次明确 UTC 时间窗口，翻页沿原窗口和游标；返回上一页会重新读取，成功后清除旧后续页链，后继只取新响应游标，不承诺固定快照。完整页面通过字段、顺序和关联校验后发布。独立 usage-read 绑定当前 Session/Project/路由，checking、失权或换身份清观察；原body/cancel实际退出前仍持有唯一请求owner，迟到结果不发布。

[限定交付卡](../work-items/d09-project-usage-read-ui.md)记录前端定向控、类型/构建及默认 App 真实五次List验收。最终8GET为6 finished和两次已证明完整消费的Usage GET aborted；8份联合证明、只读CDP真实Host/实际detach、账本不变与全部原Wait/七资源/退出尾已独立接受。真实键盘包含详情返回、分页焦点及390px栏目Drawer打开/关闭回焦；三图只证明实际可见范围，完整字段断言由同轮DOM支持。

首轮Host投影缺失、第二轮未打开窄屏菜单的浏览器FAIL和退役晚到事件反例FAIL保留。该页面只读取既有记录，不提供summary、总量估算、费用或Provider请求；测试准备使用正式Writer及受控Facts，不代表生产Runtime producer、生产Project initializer或完整D09已交付。

## Agent 显示设置

已有 initialized Project 的当前 Human Owner 可在 `/:username/:project_name/settings/agents` 编辑 Agent 的显示名称与标签颜色。表单回填当前配置，仅保存用户编辑的字段；删除输入后保存明确清空，显示名称清空后目录回退标识名。显示名称保留原字节，颜色须为小写 `#rrggbb`；仅空字符串映射清空，空白输入仍按非法值拒绝。

原版本、原 key/完整 patch、Unknown 只查询及冲突保草稿规则保持。保存先显示已确认回执，用户“刷新 Agent 目录”再取得当前 Get/List；字段不会因模型目录读取失败而被误禁用。产品、真实三次写/清空/回读及原旧字段丢回执链已有有限独立接受，精确请求终态、截图可见范围、原 FAIL 与重建方法见 [显示设置卡](../work-items/d10-agent-display-settings-ui.md)。该结果不代表所有配置字段编辑、完整 F1/D10 或生产 initializer 完成。

## Task 历史筛选与时间顺序

当前 Owner 可在任务详情的“任务历史”选择全部或七种事件之一，并切换由新到旧（默认）或由旧到新。类型和顺序传给正式历史读口；更换条件从第一页读取，相同条件不重复请求。默认仍取最近 50 条，继续读取分别显示“加载较早历史”或“加载较新历史”；筛选无结果时可明确清除。两种方向均按服务端时间和 ID 顺序追加，整页验证失败保留此前完整页与原 cursor。

历史读取复用原 Session 串行请求和当前 Project/Task 身份。忙时不可改条件，旧请求实际退出后才开始新读取；离开任务或真实身份变化清空历史与条件，同任务刷新保留条件。读取不会确认、重发或清除 Unknown 写意图，评论仍以完整纯文本展示。

[限定交付卡](../work-items/d11-task-history-filters-ui.md)记录受控 5 文件 27 项及类型/构建通过，真实八次历史查询、只读持久事实后验与全部资源退出已独立接受。25 请求为 22 finished 和 3 个原 GET aborted，10 份原请求联合证明；浏览器零写入、零 Lookup。1440 浅色新到旧与 390 深色旧到新两图已独立查看，不外推七类型全部视觉/真实权限矩阵、评论写入或完整 Task 功能。此前历史页反转为正序的行为已由本片明确的双向顺序取代，旧验收记录保留。

## Milestone 标题与描述编辑

当前 Human Owner 可在已有 initialized Project 的任务页面选择 Milestone，编辑标题和描述；仅保存触达字段，删除描述后保存明确清空。标题保留原字节；版本冲突保留输入，用户须重新读取并明确采用当前版本后再保存。取消恢复按钮焦点，保存确认后从真实 Get/List 更新当前内容，读回失败可单独重读。

结果未知时仅查询原保存，不自动重发或改写原 key/版本。编辑与创建在唯一 Session 内互斥，确认放弃只释放明确拒绝；同 Session checking/503 保留草稿与 Unknown，绑定目标或同 Project 的真实失权清理私有材料。[限定交付卡](../work-items/d11-milestone-edit-ui.md)记录正常编辑/no-op/清空、真实丢回执恢复、五图与完整资源尾的独立接受，保留首轮观察方法失败和 lost 的实际 GET aborted。该结果不包含 Sprint 编辑、排序/删除、完整权限浏览器矩阵或生产 initializer。

## Sprint 标题与描述编辑

已有 initialized、active Project 的当前 Human Owner 可在任务页面选择 planned/current Sprint，编辑标题和描述；只保存触达字段，空描述明确清空，原字节不 trim。completed 连同值保存也禁用，不改变父 Milestone、排序、生命周期、Project Current 或隐式启动状态。

版本冲突保草稿，须显式读取并采用当前版本后再保存；重读已 completed 时不可继续提交。结果未知只查原 key、版本及完整 patch，不自动重发；历史确认与当前 Get/List 分开。与已有规划/Task 操作互斥，同 Session checking/503 保留，真实绑定拒绝或身份变化清理，取消和保存有键盘焦点恢复。

[限定交付卡](../work-items/d11-sprint-edit-ui.md)记录受控恢复与真实两场景：planned 三次修改/no-op/清空及显式 Get/List，正式 current 丢回执后唯一原 Lookup；业务和完整退出尾已独立接受。normal60请求中52 finished，lost31中29 finished，其余原 aborted/cut均保实际终态与消费联证。五图仅接受可见范围，窄屏清空图底部按钮未入图，相关DOM/键盘断言分列；原编译记录器及控制FAIL保留。不完成 Sprint完成/rollover、排序/删除、完整权限矩阵、生产初始化或完整D11/D27。

## Project Scheduler 调度配置

当前 Human Owner 可从项目设置进入 `/:username/:project_name/settings/scheduler`，暂停/启用自动调度，并明确选择正整数并发上限或“不限制”。暂停保留所设上限，不结束已有执行；启用不自动启动 planned Sprint。表单保留完整 int64 字符串与原版本，冲突后明确读取和采用当前版本；归档后只读，原未决意图仍可查询，但不能重放写入。

保存确认和当前配置分开：PATCH 两字段 ack 或旧 Project Lookup 仅确认历史记录，随后独立 GET 才发布当前值。Unknown 保原 key/body/version，不自动重发；Lookup 请求只含原 key 与 `command:update`，不证明调用者传入的 Scheduler body。同 Session checking 保留草稿与原命令，真实身份变化或当前权限拒绝清理私有材料。

[本片卡](../work-items/d08-project-scheduler-ui.md)记录42项专项、129项共享回归及类型/构建分段证据。真实默认 App 两场景已有限独验：正常暂停大整数上限→恢复不限；提交后物理截断→用户查询原 key→历史 ack→独立 GET，未重发 PATCH。三张浅色1440/深色390图与完整资源尾接受；原 browser01 导航 fixture FAIL、02 原 GET aborted 与 PATCH 截断均保留。不代完整权限/故障矩阵、Scheduler Runtime、生产初始化或平台 ready。

## Skill 固定修订包下载

已有 initialized Project 的当前 Human Owner 可在 `/:username/:project_name/settings/skills` 进入详情，下载当前所示 revision 的原包。revision 保持规范正 int64 字符串；一次明确点击发起单次 GET，无 HEAD 预检、自动重试或命令 Lookup。进度和取消反馈沿当前详情；身份、项目、技能或修订变化退役旧请求并清理临时字节与 Object URL，迟到结果不保存。用户可在安全失败反馈后明确重新下载。

前端有界消费完整响应，核成功头、长度上限、实际长度、EOF 与 ZIP 魔数后才交浏览器保存为 `skill-package.zip`；canonical ZIP、manifest 与原摘要完整性由[后端](../work-items/d10-skill-owner-package-http.md)负责。实际下载文件的 SHA/长度已在真实浏览器中与原 HTTP 字节和后端 canonical material 三方核对，正常链七 GET 全 finished、浅色 1440/深色 390 两图及原资源尾均获独立接受。定向 39 测与共享回归 60 测、类型和构建已通过；原 browser01 观察失败、共享测试既有两项 strict TypeScript 诊断及未覆盖范围见[本片任务卡](../work-items/d10-skill-package-ui.md)。该结果不提供安装 UI、历史版本选择器或生产 Project 初始化，也不解除原 Object/SPA STOP。

## Knowledge Owner 文档 UI

`/:username/:project_name/knowledge` 读取当前 Owner 的已有文档，`/knowledge/:document_id` 使用规范 UUIDv7 深链；仅在 Project Resolve→Get 和当前 Human Session 均成立后读子层、metadata、祖先及正文。树展开与选择分开，每层按 cursor 显式继续；祖先是当前路径，不是完整目录快照。正文按正式默认 65536 UTF-8 字节分段，只显示当前段，版本变化清空旧段；Markdown 安全显示源文本，PDF/DOCX 明确不可读，不调用解析、预览或来源获取。

实现复用唯一 Cookie 请求 owner、现有 Project 工作区与 UiTree/Drawer。取消/切换不提前释放原 fetch/body/cancel 尾；新的页面观察与原请求完成分别管理。普通 Owner 无管理员门禁，局部拒绝不污染 System 状态。定向客户端/会话/页面检查及旧后端输入下的作者单正常链 `read03` 已通过，涵盖真实四 GET、树键盘、UTF-8 下一段和窄屏 Drawer；新主线Account/Secret根装配下 `read04` 同范围整体通过，另有三项独立App/Session/Workspace受控网络风险验证，具体边界见 [D12 UI 片段](../work-items/d12-knowledge-documents.md#owner-文档树与正文读取-ui限定读取范围)。默认生产 Project initializer 仍未绑定；真实测试只用隔离正式服务准备已有 Project，并实际 Stop/Drain 后供默认读取服务使用；测试静态托管不代表 SPA 生产发布。

已有文档改名之外，当前提供根层/子文档的纯文本与 Markdown 创建、完整正文编辑及保存后当前 metadata/ancestors/content/目录回读。创建固定目标与原 key；替换只提交原版本和来源，不覆盖并发标题或父文档变化。完整写入 JSON 上限 1 MiB，原 Lookup 包装允许额外 84 B；不把分页片段作为完整编辑基线。版本冲突保留草稿，明确重读和采用当前版本后才能再次保存；Unknown 只显式查询与按原请求重放。同 Session checking/页面卸载保留草稿与原命令，真实身份变化或当前拒绝清除私有材料，已确认写的事实不被普通回读失败推翻。

[文本 UI 正式卡与 recipe](../work-items/d12-knowledge-text-ui.md)记录定向状态/页面控制及真实普通 Owner 链：根 plain 与 Markdown child 各创建、编辑、回读，共四次写入，最终两文档均 v2；真实浅色 1440/深色 390、窄屏 Drawer 与焦点已有限独验。28 请求为 23 finished 与 5 个原 GET aborted（联合消费证明闭合），四 POST 全部 200/finished；浏览器 Lookup 为 0，不把正常链外推真实 Unknown/撤权矩阵。原 native01 旧构建资产残留、native02 窄屏 fixture FAIL 保留，native03 业务、七资源及原退出尾全部接受。完整 D12、上传/移动/删除 UI、生产 initializer 和 SPA 发布仍未由本片完成。


全项目标题查找已接入桌面目录和窄屏文档树：用户输入后明确查找，大小写、空格及 `%/_/!` 按原文字匹配，不限已加载分支、不检索正文。命中显示标题与完整祖先路径，完整页校验后发布，可显式继续加载或清空。选择命中复用改名/正文未保存确认，确认后仍核当前查询与身份，再读取当前文档。查询属于页面只读状态，离页或 checking 清除；取消保持唯一 Session 原请求退出规则。

[标题查找正式卡](../work-items/d12-knowledge-title-search-ui.md)记录20项专属控制、冻结接线10控、相关76回归及type/build。真实默认App正常链使用正式文本创建准备父子文档，未加载子标题 `%_!` 命中并读取当前正文，再查询空结果；9GET全部200/finished、无写入/Lookup、1440浅色与390深色两图及原资源全尾获有限独验。该首链没有实际触发failed GET补证，也不代真实分页、撤权或命令Unknown矩阵；原fixture与路径预检FAIL保留，既有STOP不变。

## Project Owner 工作区

[Owner 工作区卡 rev2.1](../work-items/d27-project-owner-workspace-ui.md)连接正式 Owner List/Get、Resolve、Update 和 Update lookup 五个端点。普通 Human 与管理员使用同一 Owner 校验，管理员身份不提供 Project 访问豁免。

| 路由 | 当前能力 |
| --- | --- |
| `/projects` | 当前 Owner 的项目列表，位于系统“项目”入口 |
| `/:username/:project_name` | 经重新授权的项目概要，保留系统导航和项目导航 |
| `/:username/:project_name/settings` | 重定向到同一项目的基本信息 |
| `/:username/:project_name/settings/general` | 查看项目身份字段，编辑名称和描述，处理原命令恢复 |
| `/:username/:project_name/settings/audit` | 只读项目审计列表、结构化筛选与内联详情 |

列表默认每页25项，可选择50/100项、按生命周期筛选、上一页/下一页或从首页重读。失败不发布半页；保留的上次完整结果明确标为旧观察。删除中行不显示描述或内容入口，正在归档和已归档详情只读。

名称路径只用于定位：Resolve 给出候选稳定 ID，随后 Owner Get 独立授权并完整校验，才显示详情和项目导航。大小写归一到合法小写地址，点名项目可进入；无权、不存在、删除中及读取失败不放宽权限。旧名失效不猜测新名，旧名被另一个项目复用也不继承原草稿或命令。安全登录返回保留原静态闭集，并增加上述严格 Project 目标；query/hash、编码绕过和未知后缀不作为 Project 返回目标。

基本信息只编辑 name/description，未变化禁用保存；空描述表示清空。名称冲突保留输入，可修改名称后再次明确保存；版本或当前状态冲突需显式重读，是否采用当前值由用户决定，不自动合并或换版本重发。确认保存后还要按原稳定 ID 读取当前值，才能更新标准地址；当前读取失败保留“命令已确认”的事实，只提供重读，不诱导重复 PATCH。

写结果区分已确认、首次明确拒绝和结果不确定。不确定时只由用户明确查证原命令、按原请求重放或放弃本地追踪：committed 为已确认历史回执，in_progress 保留待决并禁重放，not_observed 不证明未提交。原 key/body/expected_version 与原身份保存在内存，查证不顺带写入，不自动轮询或换 key；放弃和离页不撤销服务端命令。只读项目仍可在原身份有效时恢复先前意图，不能借此开始新编辑。

Project 与既有账号/System 操作共用唯一 Cookie 请求 owner；可见超时、取消和离页不提前释放真实 fetch/body/cancel 尾部。同 Session checking 隐藏内容并保留草稿，真正 Session/身份或 CSRF 变化清理旧材料。项目切换、系统导航、history 返回和退出接入原聚合确认，取消保留草稿与焦点；Project 本地确认不擅自清理 System 四用途/会议 Summary 的独立草稿，各域仍由原聚合流程处理。私有请求材料不写入 URL、history、storage 或普通日志。

该 Owner 工作区卡的初始验收不包括创建、归档、恢复、删除、Owner 转移、会议/任务与 Project Model 配置；后续能力按对应工作项交付，当前页面覆盖见[骨架与新页面](#骨架与新页面)。系统管理员统一会议 Summary initial/update（含首轮标题），没有 Project override 或复制默认值。

独立前端启动沿本页“启动与检查”的 `npm run dev --prefix web` 与可选 `AGENTEAM_DEV_API_TARGET` 同源代理；登录后可访问 `/projects`。没有后端连接时不提供假登录或假项目。受影响纯检查及任务自有静态构建入口如下，构建目录由调用方先设置为独占绝对路径：

```sh
npm --prefix web run type-check
npm --prefix web run test:unit -- src/tests/project-owner-client.spec.ts src/tests/project-workspace-state.spec.ts src/tests/project-workspace.spec.ts src/tests/authentication.spec.ts src/tests/session.spec.ts src/tests/system-user-directory.spec.ts
npm --prefix web run build -- --outDir "${AGENTEAM_PROJECT_OWNER_WEB_DIST:?set an owned absolute dist directory}"
```

真实浏览器只通过私有 Go fixture 启动。除 `web` 依赖外，先按锁执行 `npm ci --prefix tests/account-captcha-web`；沿[后端验证准备](../backend/README.md)使用 Go1.27.1、PG17.x（最低17.8）、MinIO、Node/Playwright、`/usr/bin/chromium` 和固定 Python3 jsonschema/referencing。Go fixture 生成私有登录材料与同源地址，不把 Playwright 当作可指向任意在线服务的独立入口。每轮先冻结实际输入、工具和完整静态资产，并设置：

| 环境变量 | 要求 |
| --- | --- |
| `AGENTEAM_PROJECT_OWNER_WEB_DIST` | 已冻结、含 index.html 的任务自有绝对构建目录 |
| `AGENTEAM_AUTH_WEB_RUNTIME` | 已准备的任务私有绝对短目录，路径不超过45字节 |
| `AGENTEAM_PROJECT_OWNER_WEB_EVIDENCE` | 任务自有绝对证据父目录；本轮 Go top 子目录必须未使用 |
| `AGENTEAM_PROJECT_OWNER_WEB_INPUT_HASH` | 本轮实际冻结输入的64位十六进制 SHA-256 |
| `AGENTEAM_AUTH_WEB_IMAGES` | layouts 必填的任务自有绝对截图目录 |

在明确的自有资源窗口逐组执行以下原完整入口；上一轮实际退出、完成原 cleanup/owned 双清并核输入未变后才执行下一组，失败保留原件并停止，不自动重试：

```sh
sh scripts/test-objects.sh -run '^(TestAccountProjectOwnerWebReadAndNavigation)$'
sh scripts/test-objects.sh -run '^(TestAccountProjectOwnerWebEditAndRename)$'
sh scripts/test-objects.sh -run '^(TestAccountProjectOwnerWebOriginalRecovery)$'
sh scripts/test-objects.sh -run '^(TestAccountProjectOwnerWebIdentityAndOwnership)$'
sh scripts/test-objects.sh -run '^(TestAccountProjectOwnerWebLayouts)$'
```

新浏览器 case 为45秒、单 worker/零 retries；每个新 Go top 的120秒包含 Cleanup，包6分钟。该 fixture 用私有 loopback 静态入口和反向代理连接真实 no-tag Central API；旧组需使用 web/dist 时仍须独占测试资产窗口，全部 reader 实际退休后恢复原字节，不是生产发布。

目前可引用的作者新五场景为[read02](../agent-team/project-owner-ui-recovered-read02-verification.md)、[edit03](../agent-team/project-owner-ui-recovered-edit03-verification.md)及[nextnew01 三轮](../agent-team/project-owner-ui-recovered-nextnew01-verification.md)；旧回归按[前两轮 auth](../agent-team/project-owner-ui-recovered-old16batch01-verification.md)与[后续14轮](../agent-team/project-owner-ui-old14-success-verification.md)组合接受，不称在一个最终输入上重跑全部16组。UI19、Go04/browser-v5 和额外 personal 错误正文取证的版本差量、所有原失败及退役记录按各档保留；[静态/受控结果](../agent-team/project-owner-workspace-ui-controlled-verification.md)不替代真实 A/B。独立负责人已亲跑并完成 A 三轮与 B 六轮的[九个真实代表](../agent-team/project-owner-ui-independent-ab-verification.md)：五个 Project 场景及四个旧域代表逐轮通过并退役，自产八张布局图按可见区域限定接受。此结果不代表完整 D27 或生产托管完成，也不复用为后续品牌验收。

真实恢复轮的 lookup 只验证 committed；in_progress/not_observed 是受控验证，不冒称真实 PG 三态或数据库 COMMIT ACK 丢失。浅深主题、1440/390宽度与常规/减少动效的八格图只接受截图可见区域，窄屏内部滚动下部、原生缩放和动画过程不由静态图证明。辅助归档/删除状态准备不验收生命周期停止链。

该前端与私有 harness 结果不等于 Central 生产 SPA 托管、生产直链 fallback、安装部署或发布接受；production Resolution/Invocations、D24 仍未绑定，ready503 及 Object runtime join、OpenAI tools 独立验证、SPA 并发发布三个停止项保持。

## Project Owner 审计

[项目审计工作项 rev2](../work-items/d27-project-owner-audit-ui.md)在项目设置尾部增加“安全记录 → 项目审计”，精确路径为 `/:username/:project_name/settings/audit`。普通设置入口与项目导航的“项目设置”链接仍进入基本信息；基本信息和审计两叶都保持正确的两级导航当前项。安全登录返回只追加该后缀，含点项目名和大小写归一沿 Owner 路径规则，query/hash、编码绕过和额外详情后缀仍拒绝；筛选、cursor 与详情 ID 不写入页面 URL。

页面先由 Resolve 定位，再等待当前稳定 ID 的完整 Owner Get，随后才调用 GET `/api/v1/projects/{project_id}/audit` 或 GET `/api/v1/projects/{project_id}/audit/{id}`，正式表示见[Project Audit API](../../../api/openapi/project-audit.json)。每次请求仍独立授权，管理员没有跨 Owner 豁免；active、archiving、archived 可读，deleting、未初始化、无权或未确认当前项目时不发 Audit 子请求。不用 System Audit 或系统配置接口补读，也不以旧项目观察恢复授权。

十四个结构化筛选字段按时间与事件、操作者与资源、关联 ID 分区。只有明确应用、重置、重读或翻页才请求；每页1–200项，默认50，应用新条件或数量从首页开始。时间输入含完整时区并规范为 UTC，区间为 `[from,to)`；非法日期、UUID 或 `service` 与操作者 ID 的冲突保留输入、标记字段并定位错误，零请求。过滤目录的53个 action 与25个 resource kind，与 Project 输出闭集的31个 action、14个 resource kind 分开校验；合法 System-only action 可返回空页。分页仅沿已取得的 cursor，不猜总数或全域快照；`CURSOR_INVALID` 保留条件，由用户明确从第一页重读。

列表和独立 GET 的内联详情只显示安全字段、固定标签与文本，不把列表行冒作详情，不渲染任意 HTML/Markdown/JSON 或自动链接。返回列表复用本页完整观察并恢复合法焦点，不自动发列表 GET。完整 EOF、UTF-8、1MiB成功表示上限与全部 typed DTO 校验通过后才一次发布；截断、损坏或读取失败不发布半页。没有写入、命令查证、导出、全文搜索、关联业务正文补读、轮询或实时订阅；支持合法历史 action 不证明其对应 producer/runtime 已绑定。

审计复用唯一 Cookie 请求协调者。同一 Project 的合法审计地址仅做大小写规范化时保留当前读取；其他地址更新、离页、取消读取或可见超时先退休本次观察，实际 fetch/body/cancel 尾部结束前仍保持互斥。已退休请求不因后续路由守卫取消导航而恢复；留在原页时由用户显式重读，尾部完成只解锁。checking、真正身份变化、切换项目或旧名复用均清理审计筛选、cursor 和详情；同 Session 恢复也建立新页面状态，等当前 Owner 上下文和旧尾部后最多一次默认首页读取。只读筛选不新增丢写确认，也不代替 Owner 基本信息或 System 双草稿的既有离页确认。

当前有效的 `401 UNAUTHENTICATED/SESSION_REVOKED` 清除旧身份与受保护审计内容，沿已有“会话尚未确认”恢复页处理；用户明确点击“检查当前会话”，再依据真实 Session 结果恢复，失效会话进入登录流程。局部403/404不设置 System 权限拒绝状态；旧身份或已退休请求的迟到错误不能清理新身份。500/503、`COMMIT_UNKNOWN` 和读取取消只表示这次读取失败，不证明事务未提交，也不产生原命令重放入口。

纯检查与独占绝对目录构建沿本页通用命令；本卡目标单元入口如下。命令列为复验入口，不表示已在当前输入完成全部验收：

```sh
npm --prefix web run test:unit -- src/tests/project-audit-client.spec.ts src/tests/project-audit-metadata.spec.ts src/tests/project-audit-state.spec.ts src/tests/project-audit.spec.ts
npm --prefix web run build -- --outDir "${AGENTEAM_PROJECT_AUDIT_WEB_DIST:?set an owned absolute dist directory}"
```

私有真实检查沿上节锁依赖、Go1.27.1、PG17.8+、MinIO、Node/Playwright、Chromium 与 Python schema 准备。Go fixture 持有私有登录材料、同源地址和正式 Project/Secret/Model producer；不把 Node 脚本当作可指向任意服务的入口。每轮绑定冻结输入、工具和静态资产，并设置以下变量；`read/authority/navigation` 由对应 Go top 向 Node 显式传入：

| 环境变量 | 要求 |
| --- | --- |
| `AGENTEAM_PROJECT_AUDIT_WEB_DIST` | 已冻结且含 index.html 的任务自有绝对构建目录 |
| `AGENTEAM_AUTH_WEB_RUNTIME` | 已准备的任务私有绝对短目录，路径不超过45字节 |
| `AGENTEAM_PROJECT_AUDIT_WEB_EVIDENCE` | 任务自有绝对证据父目录；本轮 top 子目录未使用 |
| `AGENTEAM_PROJECT_AUDIT_WEB_INPUT_HASH` | 本轮实际冻结输入的64位十六进制 SHA-256 |
| `AGENTEAM_AUTH_WEB_IMAGES` | navigation 必填的任务自有绝对截图目录 |

```sh
sh scripts/test-objects.sh -run '^(TestAccountProjectOwnerAuditWebReadAndFilters)$'
sh scripts/test-objects.sh -run '^(TestAccountProjectOwnerAuditWebAuthorityAndRecovery)$'
sh scripts/test-objects.sh -run '^(TestAccountProjectOwnerAuditWebNavigationAndLayouts)$'
```

每次只在明确的自有资源窗口运行一组，前轮实际退出和 owned 双清后再开始下一组，失败保原件并停止；浏览器45秒、单 worker/零 retries，Go top 120秒含 Cleanup、包6分钟。完整浏览器 EOF 的原响应字节需同时通过正式 schema 与公开客户端；服务侧预截断 body、SQL旁证、准备请求和控制 IPC 不计作浏览器完整读取。辅助归档状态不验收 Project 生命周期停止链。

固定版本、已覆盖场景、原始失败及未完成项见[项目审计 UI 验证记录](../agent-team/project-owner-audit-ui-verification.md)。单元检查、作者真实浏览器、旧域兼容回归和独立验证分别记录，不以某一阶段通过替代完整验收；原全量 unit 2265/2266与窄修后旧文件20项通过属于版本组合，不是最新源单次全绿。读取焦点、401恢复步骤及大写直达的原浏览器失败和修复保留，未到达检查不追补为通过。浅深主题×390/768/1024/1440八图只接受各自实际可见的900px视口，不证明完整页面、所有内容长度或动态键盘与原生缩放行为。

该页面和私有测试服务器不等于 Central 生产 SPA 托管、发布或生产直链回退接受；独立原body校验不等于完整生产调用链可用。生产 Resolution/Invocations、D24、ready503与三项停止边界沿上节保持，完整 D27/E01不因本卡交付而完成。

## 本人设置

`/settings` 进入资料页；三个叶子均要求当前本人 Session。普通用户和管理员使用相同本人页面，不提供管理员代改入口。

| 路由 | 实际操作 |
| --- | --- |
| `/settings/profile` | 修改用户名、显示名；独立选择、预览、上传或移除 JPG/PNG/WebP 头像，读取服务端确认的头像 |
| `/settings/appearance` | 预览浅色、深色或跟随系统；明确保存后持久化，取消恢复已保存偏好 |
| `/settings/password` | 输入当前密码、新密码和确认；成功后清空密码，并确认新 Session/CSRF 后再允许后续身份操作 |

资料、头像、主题和密码分别提交，不存在跨分区保存。版本冲突保留草稿并提示重新检查；未保存内容在菜单、返回和退出时有离页确认。同一 Session 重验保留草稿和主题预览，真实身份失效清除旧草稿、候选头像及其 Blob URL。

写结果未确认时保留原 key、完整输入及必要的 File 引用，供用户检查当前会话后明确重试或放弃，不自动更换 key。GET Session 不作为原写命令的成功回执。改密严格成功的反馈与随后 Session/资料读取失败分别保留，避免提示用户重复修改；密码与候选材料只在内存中持有。

个人设置源码已提交为 `c54f73f3324caa11608d84e5d207141985eb6074`，验证范围及原失败见[个人设置验收记录](../agent-team/personal-settings-verification.md)。本节只覆盖本人设置；管理员用户目录见下节，不提供代改他人资料的操作。

## 公开账号入口

[公开入口验收记录](../agent-team/public-account-entry-verification.md)对应25源提交 `787a5c7eeadf5e5f37bab97cbf030b4a745b72af`；三个页面使用独立居中布局，不展示已登录导航。

| 路由 | 实际操作 |
| --- | --- |
| `/invite` | 校验真实邀请链接、填写账号并兑换；成功后由用户明确登录，不自动创建Session |
| `/forgot-password` | 首屏显示邮箱表单；点击提交才准备所需上下文，真实202后显示统一渠道说明；底部返回登录；202不证明账号存在或已经投递 |
| `/reset-password` | 校验链接、设置密码；确认后清输入并核对当前Session，不猜目标身份或自动改绑账号 |

链接材料在history建立前移除，仅由私有页面owner持有；刷新后须重新打开原链接。有效Session可与匿名CSRF共存，三个公开写入仍使用匿名authority。找回的 Session/匿名 CSRF 准备在业务 POST 前完成，准备或提交失败在主按钮附近显示一条短错误、保留邮箱，字段错误就近展示；不会先展示 SMTP/日志渠道说明。`/login?switch=1`只提供显式身份选择，必须由用户点击注销；不额外扩展上述登录返回闭集。原写结果未知时保持原key/context/body供明确重试，不自动重发，必要的离开确认保持。

本卡151项pure/格式/类型/构建通过，四个新顶层和原认证四个、设置四个顶层按固定输入及未变语义分轮闭合；首红、CDP根因未知与观察修订保留。独立真实跨账号及已验management/ledger00018组合通过。测试clone只证明实际响应JSON形状，不证明原stream尾部；CSS `zoom=2`不是原生浏览器缩放。实际投递渠道为受限backend_log，没有新增真实SMTP、生产SPA托管或Vite开发代理浏览器验收，完整D26/D27仍未完成。

## 系统设置与用户目录

[目录工作项 rev1](../work-items/d27-system-user-directory-ui.md)的 `input04` 基于已接受的[注册时间读口](../agent-team/system-user-directory-read-verification.md) `3affc01` 与[共享遮罩焦点修复](../agent-team/dialog-outside-focus-repair-verification.md) `b53895f`。目录 UI 已获独立最终 PASS 并由主线程采纳：作者完整检查为 16 文件、244 项测试及格式/类型/构建通过；三个新顶层、三个必要旧顶层和一个独立补充组的真实浏览器检查通过。验收限于本节只读目录与设置壳，不代表完整 D27。

| 路由 | 实际行为 |
| --- | --- |
| `/system` | 固定重定向 `/system/users`，不记忆上次系统栏目 |
| `/system/users` | “用户与邀请 → 用户”叶子，读取当前管理员可见的真实用户目录 |

非管理员直接链接保留原 URL，显示“无权访问系统设置”、返回首页与重新检查权限入口，不挂载目录或发列表请求。系统实例复用 `SettingsShell`，按已注册叶子分组显示菜单，不显示侧栏退出；顶部本人名称和退出仍沿 App 的原流程，个人草稿须先确认继续编辑或放弃再离页。760px 以下复用 `UiDrawer`，保留焦点限制、Escape/遮罩关闭和触发焦点恢复。

目录分开显示邮箱、用户名、显示名、角色和 UTC 注册时间；显示名为空时回落邮箱。桌面五列可换行，窄屏逐字段堆叠；长邮箱和名称正常换行，五个字段均可阅读。`time` 保留完整六位微秒 canonical 值，可见值到秒。只有刷新、上一页和下一页；固定每页 25 项，上页重新读取，成功后才改变页位置，刷新、离页及身份变化清除旧分页材料。加载与失败隐藏旧行，空结果、读取错误和无权限分别显示；失效 cursor 由用户明确返回首页重新加载。

`system-account.ts` 只使用同源 `GET /api/v1/system/users?limit=25[&cursor=...]`，不携带 body、CSRF 或命令 key；沿原 transport 的 600,000B JSON 上限和实际 body/cancel 结束规则。专用 DTO 恰为原 User 八字段加 `created_at`，完整校验日历、微秒排序、UUID/版本、重复 ID 和 cursor 字符串边界，成功才发布冻结页面。目录行不进入当前 Session、主题或个人草稿，目录/cursor 不写入页面 URL、日志或持久存储。

目录 GET 共用 `useSession` 的唯一 Cookie 请求 owner：30 秒可见等待结束不提前释放仍在进行的 fetch/body/cancel，离页只放弃本页 system 操作。真实 401 清除失效身份；当前身份的 403 `FORBIDDEN` 清目录、隐藏入口并显示无权限，不自动注销或改写 User.role，后续成功 Session 检查或真实身份变化才清除拒绝。迟到结果不能回写新账号、新 Session 或已离开的页面。

纯检查与定点目录测试：

```sh
npm run check --prefix web
npm run test:unit --prefix web -- src/tests/system-account-client.spec.ts src/tests/system-user-directory-state.spec.ts src/tests/system-user-directory.spec.ts
```

真实检查需先按锁安装 `tests/account-captcha-web` 依赖、构建当前 `web/dist`，并取得任务自有独占资源窗口。浏览器使用 `/usr/bin/chromium`；`AGENTEAM_AUTH_WEB_RUNTIME` 必须是预建、绝对路径且至多 45 字符的私有目录。Go1.27.1、MinIO、PG17.x（最低17.8）等准备沿[后端指南](../backend/README.md)，只选择本卡新三组及共享 owner/router/设置壳涉及的旧三组：

```sh
AGENTEAM_GO=/path/to/go1.27.1/bin/go \
AGENTEAM_MINIO_BINARY=/task-owned/cache/minio \
AGENTEAM_AUTH_WEB_RUNTIME=/task-owned/private \
sh scripts/test-objects.sh -run '^TestAccount(SystemUserDirectoryWeb(ReadAndPagination|AuthorityAndIdentity|NavigationAndLayouts)|AuthenticationWebSessionLifecycle|PersonalSettingsWebThemeAndNavigation|PublicEntryWebIdentityNavigation)$'
```

单浏览器 45 秒、Go 顶层 2 分钟、workers=1/retries=0 及脚本原 race/count1/每包 6 分钟预算不变。管理员和普通账号经正式流程准备，其余 28 行仅是明确标记的分页/长文本样本，不作为邀请注册证据。观察器的响应 JSON 或原 reader 旁路不能证明 Cookie owner 尾部结束，该边界由受控原 transport 延迟屏障单独验证。本节目录不提供用户详情/修改或 Model 管理；邀请管理见下节。目录卡未扩大生产 SPA 托管、真实 Vite 代理、SMTP、Provider/tools 等既有边界，也不代表完整 D27。

## 系统邀请管理

[邀请管理工作项 rev4](../work-items/d27-system-invitation-ui.md)的最终 `input07` 已通过独立验收，基于已接受的[邀请投递读口](../agent-team/system-invitation-delivery-read-verification.md) `9b32015` 与[共享 modal 焦点恢复](../agent-team/modal-focus-restoration-verification.md) `79f922e`。作者完整 `npm run check` 为 20 文件、453 项测试及格式/类型/构建通过；新增六组、旧五组和独立补充组的真实浏览器检查按固定输入与未变语义分轮闭合。

`/system/invitations` 是“用户与邀请 → 待注册邀请”叶子，与用户目录共用系统壳；`/system` 仍固定进入用户目录。页面只列当前有效的待注册邀请，显示邮箱、UTC 创建/到期时间和最近一次投递任务的接受时间、阶段、尝试次数、渠道、尝试结果及固定原因说明，不展示邀请链接或 token。列表固定 25 项，cursor 只在内存中持有，上页重新读取、刷新返回首页；加载、错误、空结果和失权分别显示。完整 DTO、微秒日历/排序和 cursor 边界经校验后才发布，桌面五列换行、窄屏逐字段堆叠。

创建严格确认 201，重发和人工重试投递严格确认 202，撤销严格确认 204 及原 body 实际空结束。前端不 trim 或规范化输入邮箱；重发沿用原有效链接，不续期。人工重试使用原观察的 job/version，先提示可能重复投递；终态与渠道不代表收件箱送达，也不提供后台日志中的私有链接。版本冲突保留原观察并提示重新加载；重新读取后可打开新预览，不静默替换 version 重发。

邀请命令沿同一 `useSession` Cookie owner 私有持有原 key、完整 body 与 CSRF；30 秒可见截止不提前释放实际 fetch/body/cancel 尾部。结果未确认时，检查 Session/列表不能代替原写回执，只有显式重试原请求或放弃；放弃后须成功重读再开始新操作。严格写确认与随后列表 GET 失败分别显示，新表单不会继承上一笔操作的完成反馈。App 保留草稿及离页确认 Promise，同 Session checking/失败临时卸载 Dialog，恢复后按固定顺序重挂；真正离页、身份/Session/CSRF 变化、401 或当前管理员 403 清除旧材料。未保存输入与未确认写入在导航、返回和注销前均需确认，选择继续编辑后焦点仍留在可操作的业务层。

纯检查与定点邀请测试：

```sh
npm run check --prefix web
npm run test:unit --prefix web -- src/tests/system-invitations-client.spec.ts src/tests/system-invitations-state.spec.ts src/tests/system-invitations.spec.ts src/tests/settings-shell.spec.ts
```

真实检查沿上节锁依赖、生产 dist、Go1.27.1、MinIO、PG17.8+ 与独占短私有 runtime 准备；以下选择覆盖新六组和旧五组，可按精确名称分轮执行：

```sh
AGENTEAM_GO=/path/to/go1.27.1/bin/go \
AGENTEAM_MINIO_BINARY=/task-owned/cache/minio \
AGENTEAM_AUTH_WEB_RUNTIME=/task-owned/private \
sh scripts/test-security.sh -run '^TestAccount(SystemInvitationsWeb(Lifecycle|DeliveryRetry|OutcomeRecovery|ReadAndPagination|AuthorityAndIdentity|NavigationAndLayouts)|AuthenticationWebSessionLifecycle|PersonalSettingsWebThemeAndNavigation|PublicEntryWebIdentityNavigation|SystemUserDirectoryWeb(AuthorityAndIdentity|NavigationAndLayouts))$'
```

原 45 秒浏览器、2 分钟顶层、workers=1/retries=0、race/count1/每包 6 分钟预算不变。实际创建/兑换、受限日志与自有私网 SMTP 失败后人工重试、真实 401/403、原请求重放和投递版本竞争均已验收；响应截断、一次 GET 503、Session 503 和限流时钟准备均是明确的自有 fixture 控制，合成 `pageshow` 不称真实浏览器恢复。reader 旁路只观察同一原请求的闭合响应，不作为 owner 尾部结束证明。浅深两主题、1440/1024/834/390 四宽度及抽屉/模态焦点通过；截图等待遮罩物理离场。未增加生产 SPA 托管、真实 Vite 代理或原生浏览器缩放验收，不交付用户代改、Model 管理或完整 D27。

## 系统 Provider 配置

[Provider 工作项 rev4](../work-items/d27-system-provider-management-ui.md)的最终业务候选 `input07` 已通过作者及独立验收。完整检查为 23 文件、603 项测试及格式/类型/构建通过；新六组和共享 App/router/owner 涉及的旧七组真实浏览器按固定输入与未变语义分轮通过。独立验证覆盖实际响应丢失后的原请求重放及 Credential 成功、Provider 失败后的恢复。

`/system/providers` 是“模型与提供商 → Providers”叶子，与 Models、平台模型用途、用户、待注册邀请及账号安全组成三组、六个菜单项；`/system` 仍固定进入用户目录。管理员可分页查看、创建、编辑、启停及删除 Provider。名称与 Base URL 保留原文，不自动 trim、补全或规范化；协议创建后只读，options 固定为空对象。详情中的 Models 仅供分页查看，Credential metadata 只显示引用及版本状态，不读取凭据内容。删除前要求当前 Models 首项确认为空，最终仍由服务执行当前约束检查；删除 Provider 不删除 Credential 或 Models。

填写新凭据时依次执行 Credential create → Provider create/update，两个步骤使用不同命令 key。新凭据仅在私有内存中保存，Credential 严格确认后清除原材料。若 Credential 已成功而 Provider 尚未确认，保留已准备的引用和首步回执，明确显示部分成功；版本冲突后由用户重新读取核对，再只提交新的 Provider 命令，不重复创建 Credential，也不发起补偿删除。

结果未确认时，“检查原请求”只查询原 key 的历史观察。查到成功或未查到结果都不替代当前步骤的执行回执，用户仍需明确以原 key、原 body 执行原请求，严格确认后才推进下一步；检查 Session、刷新列表或读取当前详情同样不能确认原写。明确放弃只停止本地追踪，不撤销已被服务接受的事实。写入已确认而随后 GET 失败时保留成功反馈，避免重复保存。

Provider、Credential、lookup 和只读请求沿同一 Cookie owner；30 秒可见截止不提前释放实际 fetch/body/cancel 尾部。App 持有草稿与离页确认，同 Session checking/失败恢复保留私有进度，旧 View/Editor 续体不能抢新层焦点；当前身份失效、CSRF 变化、真实 401/403、换 Session 或真正离页清除对应追踪。列表固定 25 项，仅 Provider 列表的成功 JSON 响应允许 2MiB；Problem 与其他响应仍限 600000B。请求和输入预算见工作项及[正式 Model/System API](../../../api/openapi/model-system.json)。

纯检查与定点测试：

```sh
npm run check --prefix web
npm run test:unit --prefix web -- src/tests/system-providers-client.spec.ts src/tests/system-providers-state.spec.ts src/tests/system-providers.spec.ts
```

真实检查沿上节锁依赖、生产 dist、Go1.27.1、MinIO、PG17.8+ 与任务自有独占短 runtime 准备；以下选择覆盖新六组及旧七组，可按精确名称分轮执行：

```sh
AGENTEAM_GO=/path/to/go1.27.1/bin/go \
AGENTEAM_MINIO_BINARY=/task-owned/cache/minio \
AGENTEAM_AUTH_WEB_RUNTIME=/task-owned/private \
sh scripts/test-security.sh -run '^TestAccount(SystemProvidersWeb(Lifecycle|CredentialReplacement|OutcomeRecovery|ReadAndPagination|AuthorityAndIdentity|NavigationAndLayouts)|AuthenticationWebSessionLifecycle|PersonalSettingsWebThemeAndNavigation|PublicEntryWebIdentityNavigation|SystemUserDirectoryWeb(AuthorityAndIdentity|NavigationAndLayouts)|SystemInvitationsWeb(OutcomeRecovery|NavigationAndLayouts))$'
```

原 45 秒浏览器、2 分钟顶层、workers=1/retries=0、race/count1/每包 6 分钟预算不变。正式 26 个 Provider 与 26 个 Model、合法转义后大于600000B的列表页、当前授权、两步部分成功与原请求重放已验证。合成 `pageshow` 只驱动 App 复查，其后的 Session 响应来自真实服务；reader 旁路只记录闭合安全响应，实际 owner 尾部由受控 transport 屏障另证。浅深两主题、1440/1024/834/390、键盘与模态/抽屉焦点通过。凭据输入期间禁止自动截图、trace、video 或请求 body 收集；布局图只在私有输入清空、遮罩物理离场后采集。

保存成功只表示管理配置命令被接受，未调用外部 Provider，也未验证连通性、协议兼容或模型可调用性。Model 写管理见下节；Provider 页面不提供连接测试，不扩大生产 SPA 托管、真实 Vite 代理浏览器、Runtime、ready503 或完整 D09/D27 的验收范围。

## 系统 Model 配置

[Model 工作项 rev2](../work-items/d27-system-model-management-ui.md)的最终业务候选 `input03` 已通过完整检查：26 文件、829 项测试及格式/类型/构建通过，Go1.27.1 integration-tag race 编译与 vet 通过。新增六组、共享 App/router/owner 涉及的旧十组按固定输入与未变语义分轮通过；独立两个真实代表覆盖原请求恢复、删除替代及 Session/确认层组合。

`/system/models` 是“模型与提供商 → Models”叶子，也是第八个精确登录返回目标。普通用户直链不挂载页面、不发管理请求。管理员先在每页 25 项的 Provider 列表中明确选择，再分页查看该 Provider 的 Models；主列表与删除替代的 Provider/Model 四处分页各自独立，上一页重新读取，刷新回首页，不自动扫全库。选择和 cursor 仅在内存中保留。详情显示安全配置、所属 Provider、版本及 UTC 微秒时间；禁用 Provider 仍可管理服务允许的 Model 配置，替代资格另行检查。

创建时按 Provider 协议确定 type，编辑时 Provider 和 type 只读。名称与原生 Model ID 保留原文，不 trim 或自动补全；分别限制为 1–128 个 Unicode 字符和 1–256 个 UTF-8 字节，并拒绝 NUL 与未配对 surrogate。四种 type 的当前配置边界如下，完整能力白名单沿工作项：

| type / Provider 协议 | 当前能力配置 |
| --- | --- |
| `chat` / `openai-chat-completions`、`anthropic-messages` | 工具、并行工具、流式及推理四个布尔声明；并行工具要求工具开启。输入允许 text/image/file/vector 子集，输出仅 text；结构化输出 OpenAI 允许 text/json_schema，Anthropic 仅 text。 |
| `embedding` / `openai-embeddings` | 输入仅 text 子集，输出仅 vector 子集。 |
| `reranker` / `jina-rerank` | 输入仅 text 子集，输出固定空数组。 |
| `image_generation` / `openai-images-generations` | 输入仅 text 子集，输出仅 image 子集。 |

非 chat 的四个布尔声明固定 false，结构化输出固定空数组；所有数组不允许重复。四种 type 的 `parameters`、`request_overwrite`、`header_overwrite` 固定 `{}`，`reasoning_efforts` 固定 `[]`，没有任意 JSON/Header/effort 编辑入口。容量 `context_length`、`max_output` 可空；有值时为精确正 int64 十进制字符串，两者都有值时要求 `max_output ≤ context_length`。能力声明与配置保存均不证明外部模型支持这些能力。

删除先捕获目标及版本，再读取完整详情和 Impact。Impact 只说明当前登记引用：`none` 明确提交空替代，`optional` 要求明确选择清空相应用途引用或替代，`required` 必须选合法替代。替代可跨 Provider，但须非自身、同 type、Model 与 Provider 均启用；含 memory 引用时还须声明 `json_schema`。未绑定的外域引用 adapter 阻止页面提交。Impact 不授予写权，也不冻结引用集合；读取发现目标版本变化，或提交因版本、候选不相容被拒绝时，须显式重读核对。Model 版本不随所有引用变化；正式 DELETE 重新发现并检查当前引用集，同时核对权限与候选，在同一事务处理合法平台引用及删除。合法新增引用可能体现在实际 `affected_references` 中。页面不预先修改 selector、不级联删除其他配置；只有严格 receipt 才确认，并显示其实际 `affected_references`。

Model 操作沿唯一 Cookie owner 私有保留原 key、path、完整 body、版本、替代和 Session CSRF。检查原请求的 lookup 只提供历史观察，不能作为本次写入的确认回执；必须显式以原材料重放 Execute，不能更换 key 或自动重发。删除已生效后的 GET 404、Impact 失败或旧候选消失不阻挡合法原请求恢复；放弃只停止前端追踪，不回滚已经接受的操作。严格写确认后若 GET 失败，只提供读取重试，不重新提交写入。

草稿、替代选择、进行中或未确认写入均受导航/返回/注销确认保护。同一 Session checking/失败临时卸载全部 Model 模态，恢复后保留草稿与原顶层确认；身份或权限失效清除旧状态。30 秒可见截止不提前释放实际 fetch/body/cancel 尾部。页面使用同一套 Dialog/Drawer 与焦点规则，桌面六列正常换行，窄屏逐字段堆叠。

纯检查与定点 Model 测试：

```sh
npm run check --prefix web
npm run test:unit --prefix web -- src/tests/system-models-client.spec.ts src/tests/system-models-state.spec.ts src/tests/system-models.spec.ts
```

真实检查沿前述锁依赖、生产 dist、Go1.27.1、MinIO、PG17.8+ 与任务自有独占短 runtime 准备。以下选择器覆盖新六组与旧十组；实际验收按精确顶层和固定输入分轮执行：

```sh
AGENTEAM_GO=/path/to/go1.27.1/bin/go \
AGENTEAM_MINIO_BINARY=/task-owned/cache/minio \
AGENTEAM_AUTH_WEB_RUNTIME=/task-owned/private \
sh scripts/test-security.sh -run '^TestAccount(SystemModelsWeb(Lifecycle|DeletionAndReplacement|OutcomeRecovery|ReadAndPagination|AuthorityAndIdentity|NavigationAndLayouts)|AuthenticationWebSessionLifecycle|PersonalSettingsWebThemeAndNavigation|PublicEntryWebIdentityNavigation|SystemUserDirectoryWeb(AuthorityAndIdentity|NavigationAndLayouts)|SystemInvitationsWeb(OutcomeRecovery|NavigationAndLayouts)|SystemProvidersWeb(CredentialReplacement|OutcomeRecovery|NavigationAndLayouts))$'
```

原 45 秒浏览器、2 分钟顶层、workers=1/retries=0、race/count1/每包 6 分钟预算不变；每轮等待实际退出并双查自有资源、进程及 runtime 清零。浅深两主题与 1440/1024/834/390 四宽度、真实权限拒绝、原请求恢复及模态/抽屉焦点已验。响应截断、一次 Session/读取 503 和合成 `pageshow` 属于明确的自有 fixture 控制；reader 旁路不代替 owner 实际结束证明。唯一外域负例仅使用完整谓词隔离的自有引用索引行，不代表已实现 Agent/Project adapter。

本页只管理配置，不读取 Credential 内容，不发起外部模型调用、Runtime 调用或连接测试，不改变既有 ready503 门禁。生产 SPA 托管、真实 Vite 代理浏览器、原生浏览器缩放、非空类型参数/覆盖、外域 adapter 及完整 D27 仍未交付。

## 平台模型用途

[平台用途工作项 rev2](../work-items/d27-system-model-selection-ui.md)的最终业务候选 `input02` 已获独立验收。`/system/model-selection` 是“模型与提供商 → 平台模型用途”第五个系统叶子，也是第九个精确登录返回目标；`/system` 仍进入用户目录。普通用户没有入口，直链不挂载本页、不发管理请求。

既有四项区域读取单一平台配置的版本与四项 Model 引用。正式初始状态为 `configured:null`、`version:"1"`，显示尚未配置，不猜默认 Model。管理员在一个业务 Dialog 中显式分页选择 Provider，再选择其 Model；每页 25 项，只读取当前浏览的用途，不自动扫全库，不提供任意 UUID 输入或本页创建配置入口。

| 用途 | 新保存的候选条件 |
| --- | --- |
| Embedding | 必填，System `embedding` Model，Model 与 Provider 均启用。 |
| Memory | 必填，System `chat` Model，Model 与 Provider 均启用，`structured_output_modes` 声明含 `json_schema`。 |
| Reranker | 可明确不配置；非空时为 System `reranker` Model，Model 与 Provider 均启用。 |
| Image Generation | 可明确不配置；非空时为 System `image_generation` Model，Model 与 Provider 均启用。 |

保存以一次完整 PUT 提交配置 ID、捕获的 `expected_version` 与四项 ID/null；取消及无变化不发 PUT。当前引用补读失败、404 或已知不可选时保留原绑定，须重读或明确更换后才能开始新保存。后端在事务中重核权限、版本与全部候选，拒绝时无部分用途成功。冲突保留草稿，须显式读取最新配置并核对，不静默换版本覆盖。

七项受限 API 中的 `getSavedModel` 在单次操作内先读取 Model，再读取其 Provider，按真实协议完整解析后一次返回不可变组合，不发布半成品。四项区域每轮当前引用最多 4 次 Model GET 加 4 次 Provider GET，分次读取不是原子快照。每个组合共用一次 owner、signal 和 30 秒预算，实际 fetch/body/read/cancel 全部结束后才释放唯一 Cookie owner；平台用途状态域与其余业务隔离。

结果未确认时保留原 key、完整 body、版本与身份。lookup、当前 GET 或 Session 恢复只提供观察，须明确重试原 PUT 并取得严格回执才确认；当前版本推进或旧 Model 删除不阻止合法历史重放。严格确认后 GET 失败仅重试读取，放弃不回滚已接受的配置。未保存或未确认操作受离页/注销确认保护，同 Session checking/失败恢复保留草稿与待决确认；真实身份或权限失效清除旧状态。

完整前端检查为 29 文件、993 项测试及格式/类型/生产构建通过；Go1.27.1 integration-tag race 编译与 vet、浏览器类型及选择器检查通过。定点纯测试：

```sh
npm run check --prefix web
npm run test:unit --prefix web -- src/tests/system-model-selection-client.spec.ts src/tests/system-model-selection-state.spec.ts src/tests/system-model-selection.spec.ts
```

真实检查沿前述锁依赖、生产 dist、Go1.27.1、MinIO、PG17.8+ 与任务自有独占短 runtime 准备。以下四组分别执行；每组实际退出并双查自有 exact-ID 资源、进程及 runtime 清零后再开始下一组，原 45 秒浏览器、2 分钟顶层、workers=1/retries=0、race/count1/每包 6 分钟预算保持：

```sh
export AGENTEAM_GO=/path/to/go1.27.1/bin/go
export AGENTEAM_MINIO_BINARY=/task-owned/cache/minio
export AGENTEAM_AUTH_WEB_RUNTIME=/task-owned/private

sh scripts/test-security.sh -run '^TestAccountSystemModelSelectionWeb(Lifecycle|ReadAndPagination|ConcurrencyAndReferences|OutcomeRecovery|AuthorityAndIdentity|NavigationAndLayouts)$'
sh scripts/test-security.sh -run '^TestAccount(AuthenticationWebSessionLifecycle|PersonalSettingsWebThemeAndNavigation|PublicEntryWebIdentityNavigation|SystemUserDirectoryWeb(AuthorityAndIdentity|NavigationAndLayouts)|SystemInvitationsWeb(OutcomeRecovery|NavigationAndLayouts))$'
sh scripts/test-security.sh -run '^TestAccountSystemProvidersWeb(CredentialReplacement|OutcomeRecovery|NavigationAndLayouts)$'
sh scripts/test-security.sh -run '^TestAccountSystemModelsWeb(DeletionAndReplacement|OutcomeRecovery|NavigationAndLayouts)$'
```

实际六新组由 `new01` 前五组与 `new02` 单独 Navigation 组合通过；原超长 Provider 名称 seed 失败保留，修正仅为夹具输入。旧十三组分 `old-core01` 七组、`old-provider01` 三组、`old-model01` 三组通过。浅深主题、1440/1024/834/390、Dialog/Drawer 与键盘焦点已验，八张图为 900px 高视口，未作独立截图或全部折叠以下内容的人工审阅。

独立 A 验证丢失响应、正式删除/替代后的原请求重放与当前配置不倒退，并核原命令/Audit/配置事件唯一；B 验证候选失效 400 零部分写、版本冲突 409、明确核对及 Session 503 恢复后的确认层/原生键盘焦点。接受组合为 `independent02` 的 B 与 `independent03` 的 A；02 整轮 exit 1，03 exit 0，并非同轮全绿。

以下独立实际命令须沿固定工作区的私有 Go overlay 与 `command.json` 完整环境；补充测试不属于仓库常规测试集：

| 原命令记录 | 实际选择与 overlay | 复用结果 |
| --- | --- | --- |
| `runs/independent02/command.json` | `sh scripts/test-security.sh -run '^(TestAccountSystemModelSelectionIndependentReplay\|TestAccountSystemModelSelectionIndependentReview)$'`；`bound04/overlay.json` | 仅 Review（B）完整通过；整轮 exit 1，77.600 秒。 |
| `runs/independent03/command.json` | `sh scripts/test-security.sh -run '^(TestAccountSystemModelSelectionIndependentReplay)$'`；`bound05/overlay.json` | Replay（A）完整通过；整轮 exit 0，61.415 秒。 |

工作区作者原件根为 `/workspace/scratch/agenteam-model-selection-author-sglwpvis`，见 `input02/manifest.json`、`author-index.json`、`author-report.md` 与 `runs/new02/images/`；独立根为 `/workspace/scratch/agenteam-selection-real-preparation-6plowa4i`，见 `verification-report.md`、同名 JSON 及表中原命令。原页面产品失败、夹具/观察前提失败与历史源码缺件限制永久保留。响应截断、精确读取/Session 503 和合成 `pageshow` 为自有 fixture 控制，原 reader 旁路不代替 owner 实际结束证据。

保存只确认平台模型用途配置持久化；Memory 能力是声明，Embedding 事件持久化也不证明已被消费。页面不调用外部模型、不触发索引重建、不验证 Runtime、检索切换或模型可调用性；生产 SPA 托管、真实 Vite 代理浏览器、原生缩放、ready503 与完整 D09/D27 的边界保持。

### 会议 Summary 设置（S2）

[S2 工作项 rev3](../work-items/d09-system-meeting-summary-settings.md)的技术结果已获独立验收。同一 `/system/model-selection` 页新增内联会议 Summary 区域，保留原四项 Dialog、菜单和路由。系统管理员统一选择所有 Project 的 Meeting initial/update 模型，包含首轮标题；Project 没有覆盖项或创建时复制的默认值。独立 GET/PUT `/api/v1/system/model-selection/meeting-summary` 使用自己的配置 ID、版本和草稿，初始为 `model:null`、`version:"1"`，不猜默认 Model。新保存只接受 Model 与 Provider 均启用的 System `chat` Model，不要求 `json_schema`，也不提供清空操作；原 Memory 候选条件不变。

`auth.system.selection.meetingSummary` 与原四项方法共用 selection 域及唯一 Cookie owner，内部保留两个独立的不可变 intent。各自捕获 key、body、版本、身份与 CSRF；局部放弃只清本槽，不撤销已经提交的后端事实。lookup、当前 GET 和 Session 恢复只作观察，仍须显式重试原 PUT 并取得严格回执才确认；已确认后的补读失败只重读。可见超时、取消或页面卸载均不提前释放实际 fetch/body/cancel 尾部的 owner。

应用内离页和注销只作一次聚合确认，同时保护两份草稿与未确认操作；同身份 checking/恢复保留状态，调度避免自动初始读取抢占原请求查证。真实身份或当前权限失效使旧状态整体失效。`beforeunload` 仅使用浏览器原生保护，不把事件或假定的用户选择当成清槽回调；真实文档离开后的 App 销毁照常处理。

本轮纯测试按未变输入复用及受影响重跑组合为 client 98、state 96、component 27，共 221 项，另复用共享 owner/security 57 项；格式、类型、生产构建及相关 integration-tag race 编译、vet 通过，不声称最终全套重新运行。六个作者真实轮次通过四个新顶层与六个旧平台用途顶层，独立 B02 通过双 intent、恢复、导航及原生离页保护检查。B01 的私有 `page.reload` 等待在取消原生离页后触及 45 秒预算，原失败保留；B02 仅修私有等待方式，产品未改。每轮沿用浏览器 45 秒、顶层 2 分钟（含 Cleanup）、包 6 分钟预算，实际等待自有进程退出并双查七项精确资源；daemon 所属 PID1 zombie 单列，不声称任务已 wait 或全机清零。

作者实际查看九张图，root 另查看其中 390px 深色、1440px 浅色两张。八张主题图覆盖 390/768/1280/1440px、均为 900px 高的局部视口；CSS `zoom=2` 一张显示前面的四项区域，不能作为 Summary 区域、原生缩放或全页审阅证明。真实浏览器共用的 42 文件测试构建已在全部八个已启动轮次（含 B01 失败）实际结束后撤下，原 `web/dist` 三文件字节、身份及约定元数据恢复；目录 size 与 ctime 的实际差量保留。作者轮次、截图及恢复原件位于工作区 `/workspace/scratch/meeting-summary-settings-frontend-author/runtime-prep02/` 下的 `browser-author-review01.md`、`screenshot-review01.json`、`asset-restoration-final01.json`；首次 preflight/apply 与测试失败均保留。

保存仅确认共享配置持久化，不触发模型调用或 D24 Meeting 生成。S3 当前解析库与生产消费者绑定是不同交付；生产 Resolution/Invocations 仍未绑定，ready503、既有 compaction snapshot 与 Execution Summary 边界不变。

## 账号安全配置

[账号安全工作项 rev2](../work-items/d27-system-account-security-ui.md)已获独立验收。`/system/account-security` 是“平台配置 → 账号安全”叶子，也是第十个精确登录返回目标；系统菜单现共三组、八叶子，`/system` 仍进入用户目录。普通用户没有入口，直链不挂载本页、不发管理请求。

页面通过 GET/PUT `/api/v1/system/account-settings` 读取和显式保存配置。内联表单保留当前版本与四项字符串输入：`session_idle_seconds` 为 900–2592000 秒，`session_absolute_seconds` 为 3600–7776000 秒且不小于 idle，`password_reset_seconds` 为 300–7200 秒，`challenge_after_failures` 为 1–20 次。输入须为无前导零的十进制整数；初次读取成功前不填默认值，无变化、非法输入与取消均不发 PUT。

当前观察、草稿和原命令确认分别保存。冲突保留输入，须显式读取、核对并采用当前值后再编辑，不自动改版本覆盖。结果未确认时保留私有原 key/body/version/CSRF，只显式重试原 PUT；没有公开 command lookup，检查 Session 或 GET 相同值都不能确认写入。PUT 仅在 singleton、原版本加一及四项原值严格匹配后确认历史 Settings，历史结果不会倒退较新的当前观察；确认后的刷新失败只重试读取，不重发保存。

账号安全复用同一 Cookie 请求 owner 并使用独立状态域；30 秒可见截止、取消或放弃不提前释放实际 fetch/body/read/cancel 尾部。同 Session 检查及失败恢复保留草稿和待决离页确认；确认关闭后恢复当前可用触发器，旧触发器已卸载时落在当前页标题，随后可用 Tab 访问控件。真实身份或权限失效清除旧草稿与原命令。

期限修改只影响随后签发的 Session/token，已签发对象保留原期限；挑战阈值在下一次尝试读取当前值，邀请期限仍固定 24 小时。保存不撤销既有登录，本页不提供 Session 管理；SMTP 配置见下节。

组合检查为 32 文件、1115 项测试及格式/类型/生产构建通过。五个新真实组与必要十六个旧组按固定输入及未变语义组合通过；已接受的 [Selection 取消读取修复](../work-items/d27-model-selection-cancelled-read-recovery.md)与账号安全另经两个 Navigation 组合验证。独立 API、owner、页面、真实 A/B 及最终组合复核通过，原失败和分版本证据保留。真实命令沿前述 Go1.27.1、MinIO、锁依赖及独占 runtime 准备，原 45 秒浏览器、2 分钟顶层、workers=1/retries=0、race/count1/每包 6 分钟预算保持：

```sh
npm run check --prefix web
npm run test:unit --prefix web -- src/tests/system-account-security-client.spec.ts src/tests/system-account-security-state.spec.ts src/tests/system-account-security.spec.ts
sh scripts/test-security.sh -run '^TestAccountSystemAccountSecurityWeb(Lifecycle|Concurrency|OutcomeRecovery|AuthorityAndIdentity|NavigationAndLayouts)$'
sh scripts/test-security.sh -run '^(TestAccountSystemAccountSecurityWebNavigationAndLayouts|TestAccountSystemModelSelectionWebNavigationAndLayouts)$'
```

最终组合的两页 light/dark × 1440/1024/834/390 共 16 张视口图已由作者逐张审阅；窄屏下方内容仍需主区域滚动。响应截断、精确 Session/GET 503 与合成 `pageshow` 仅为自有测试控制，不冒充真实 BFCache。生产 SPA 托管、真实 Vite 代理浏览器、原生缩放、Runtime、ready503 和完整 D26/D27 的边界保持。

## SMTP 配置与私有凭据

[SMTP 配置工作项 rev2](../work-items/d27-system-smtp-settings-ui.md)提供 `/system/smtp`，位于“平台配置 → SMTP”，是第七个系统叶子和第十一个精确登录返回目标；系统菜单仍分三组，`/system` 默认进入用户目录。普通用户没有入口，直链不挂载本页、不发管理请求。

页面只调用 GET/PUT `/api/v1/system/smtp` 与 POST `/api/v1/system/smtp/unconfigure`。已配置时显式编辑主机、端口、`tls`/`starttls`/`none`、发件地址/名称、认证与重试策略；未配置时可单独保存策略，或明确进入完整配置。自动重试次数为 0–5，间隔为 10–3600 秒，输入使用无前导零的十进制字符串。无变化、非法输入和取消不派发写入；版本冲突须明确重读核对，不自动采用新版本覆盖。停用另行确认，清除传输配置并保留确认时捕获的已保存策略；未保存的策略须先处理草稿。

密码留空表示保持，输入新密码表示替换；明确移除认证时用户名须为空。材料只在输入 DOM、私有非响应式 owner 及实际请求所需临时副本中持有，不回显、不进入公开状态、日志、URL 或持久存储。输入 DOM 清空或同 Session 重挂不抹去仍合法的原请求材料；明确放弃或身份失效立即取消恢复资格，实际尾部结束后清理可达引用，不承诺凭据已被物理擦除或在途副作用已撤销。

PUT 与停用返回的 `applied_version` 确认原命令的历史版本，`settings` 是重新授权后的当前观察；两者不能混为同一次配置快照。原 PUT 重放可读到当前已停用，原停用重放也可读到当前重新配置，页面分别显示原操作确认和当前状态，较旧观察不倒退较新版本。没有公开 command lookup；Session 检查、GET 相同值或版本推进均不能确认原写。已派发写的后读失败即使标为 `not_started`，也保留原 method/path/key/body/version 及精确密码；仅由用户明确使用当前合法 CSRF 重试原请求，严格匹配结果后才确认。确认后刷新失败只重试读取，不重新保存。

SMTP 是 Session controller 的第八项依赖，使用独立状态域并共用唯一 Cookie 请求 owner。30 秒可见截止、取消、放弃或身份变化不提前释放实际 fetch/body/read/cancel 尾部；读取取消同步结束失效的 loading，须显式重读，不自动补发。草稿及未确认写受离页/注销确认保护，停用与放弃使用各自常驻 Dialog 和 Promise；同 Session checking/失败恢复保留待决状态，真正身份或权限失效清除旧恢复资格。关闭确认恢复当前可用触发器，旧触发器已卸载时使用当前页标题后备目标；原生焦点与 Tab 顺序已验。

最终完整前端检查通过 35 文件、1214 项测试及格式/类型/生产构建，Go1.27.1 integration-tag race 编译与 vet 通过。作者六个新增组由 `input04` 的 Lifecycle、CredentialLifecycle、OutcomeRecovery、NavigationAndLayouts 与 `input02/new02` 的 Concurrency、AuthorityAndIdentity 按未变语义组合通过；旧十九组在 `input04` 分 core7、Provider3、Model3、Selection3、账号安全3 五批通过。独立真实 A/B 与最终差量复核通过，原失败和复用边界保留于[SMTP 配置验收记录](../agent-team/system-smtp-settings-ui-verification.md)。`input04` 真实基线为 `f670cb1`；后续 `a942779` 组合仅按 Outbound 接缝静态复核，最终主树 Go 编译由整合执行，不作为 `a942779` 的浏览器实测。

定点检查沿前述锁依赖、生产 dist、Go1.27.1、MinIO 与独占短 runtime 准备；真实验收按固定输入分批执行，保持浏览器 45 秒、顶层 2 分钟、每包 6 分钟、race/count1、workers=1/retries=0，并等待实际退出与自有资源双查清零：

```sh
npm run check --prefix web
npm run test:unit --prefix web -- src/tests/system-smtp-settings-client.spec.ts src/tests/system-smtp-settings-state.spec.ts src/tests/system-smtp-settings.spec.ts
sh scripts/test-security.sh -run '^TestAccountSystemSMTPSettingsWeb(Lifecycle|CredentialLifecycle|Concurrency|OutcomeRecovery|AuthorityAndIdentity|NavigationAndLayouts)$'
```

`navigation04` 的 light/dark × 1440/1024/834/390 八张 900px 高视口图由作者逐张查看，主线程未作图审；窄屏下方表单仍需滚动。截图仅在材料输入清空后采集，禁用 trace/video 与请求 body 收集。响应截断、精确读取/Session 503 和合成 `pageshow` 是自有 fixture 控制，不代表真实 BFCache；reader 旁路不代替实际 owner 尾部证明。

保存只确认配置持久化，不证明 SMTP 连接、认证、发送或外部邮箱收件成功。配置区不提供连接检查；同页测试发送与投递任务管理见下节，不调用 Runtime；生产 SPA 托管、真实 Vite 代理浏览器、原生缩放、ready503 和完整 D27 的边界保持。

## SMTP 测试与投递任务

[SMTP 投递工作项](../work-items/d27-system-smtp-delivery-ui.md)在 `/system/smtp` 增加“测试与投递任务”区，与配置区显式切换；系统菜单现共八叶子、三组、十二个精确登录返回目标。本区调用四口：POST `/api/v1/system/smtp/test`、GET `/api/v1/system/mail-jobs/management`、GET `/api/v1/system/mail-jobs/{job_id}/management` 与原 POST `/api/v1/system/mail-jobs/{job_id}/retry`。列表每页 25 项，cursor 仅在本身份内存中保存；上页重新读取、刷新返回首页。详情只展示正式安全投影的邀请、密码重置或测试任务，不推断历史收件人、链接、root/谱系或完整 attempt 历史。

新测试需要当前已配置的合法观察，保留输入邮箱原文；严格 `{job_id}` 202 只确认测试请求接受。人工重试从详情捕获源 JobID/version，明确提示可能重复投递，由服务裁决当前配置、业务材料和并发资格；严格 202 确认一个不同 JobID 的新周期，源与新周期分开显示。任务 phase、实际 attempt 渠道/结果与请求是否确认分别表达；`sent` 不等于外部收件箱送达，满足按钮条件也不保证服务接受重试。确认后至多读取一次对应新任务；后读失败只重试 GET，不重发已确认 POST。

没有公开 command lookup，Session、列表、详情、相同值或版本推进都不是原写 receipt。派发后 404/5xx、截断响应等即使标为 `not_started`，也可能发生在已接受后的读取，须保留原请求；曾经未确认后，后续业务拒绝也不抹除此前不确定性。显式恢复使用原 method/path/key/body/version，只在同完整 identity（userID/sessionID/epoch）、当前管理员且原 CSRF 仍为合法当前值时重放；token 变化会更换 epoch 并销毁旧 intent，不把新 token 移植给旧请求。当前停用或旧任务版本推进不预先阻挡合法历史确认。放弃只终止客户端追踪，不撤销发送。

投递是第九个独立状态域，与配置域分离并共用唯一 Cookie owner。30 秒可见截止、取消或离页不会提前释放实际 fetch/body/read/cancel 尾部；取消列表/详情读取后须显式重读。普通用户不发管理请求，当前身份失效或本身份 403 清理系统私有材料；旧身份的待决确认不能使旧导航继续。切换区块、离页和注销保护草稿及未确认请求；checking/失败共同卸载确认 DOM，同 Session 恢复保留待决 Promise，关闭恢复当前合法焦点。已清理的匿名或拒绝状态仍允许新的合法离页动作。

页面阶段完整 `npm run check` 通过 38 文件、1303 项测试及格式/类型/构建，后续窄修通过受影响 App、类型与构建检查，Go1.27.1 integration-tag race 编译和 vet 通过。作者五新五旧按 `input01`–`input04` 的固定版本组合通过，独立最终 A/B 在 `input04` 完整通过；不称最终输入重跑全部十组或 1303 全套。原失败及独立 `real01` 环境监督中断记录均保留，后者不算完整通过轮。 完整来源、分版本复用和恢复边界见[SMTP投递验收记录](../agent-team/system-smtp-delivery-ui-verification.md)。

以下为精确目标集合；真实验收按固定输入与独占资源分批执行，沿原 45 秒浏览器、2 分钟顶层、每包 6 分钟、race/count1、workers=1/retries=0 及实际退出/双扫清理预算：

```sh
npm run test:unit --prefix web -- src/tests/system-smtp-delivery-client.spec.ts src/tests/system-smtp-delivery-state.spec.ts src/tests/system-smtp-delivery.spec.ts
sh scripts/test-security.sh -run '^TestAccountSystemSMTPDeliveryWeb(ReadAndPagination|TestAndRetry|OutcomeRecovery|AuthorityAndIdentity|NavigationAndLayouts)$'
sh scripts/test-security.sh -run '^TestAccount(SystemSMTPSettingsWeb(OutcomeRecovery|AuthorityAndIdentity|NavigationAndLayouts)|SystemInvitationsWeb(DeliveryRetry|OutcomeRecovery))$'
```

`navigation02` 的 light/dark × 1440/1024/768/390 八图由作者逐张审阅，主线程未作图审；桌面六列换行、窄屏逐字段堆叠，下方内容仍需滚动。真实浏览器验证原生焦点/Tab 与同 Session 确认恢复，合成 `pageshow` 不称真实 BFCache。实际 SMTP 仅使用自有私网 `none` 模式成功/失败/未知及人工重试，未重验全部 TLS/崩溃恢复；取消样本的 due 扫描不证明内部入队或目标 ClaimBusy。外部邮箱、生产 SPA 托管、真实 Vite 代理浏览器、原生缩放、Runtime、ready503 和完整 D27 仍不在本次通过范围内。

## 系统出站规则

[出站规则工作项](../work-items/d27-system-outbound-policy-ui.md)提供 `/system/outbound-policy`，位于“平台配置 → 出站规则”，是第八个系统叶子和第十二个精确登录返回目标。页面只向当前已确认且未被拒绝的管理员开放，通过 GET/PUT `/api/v1/system/outbound-policy` 读取和保存；普通用户直链不发管理请求。

编辑区逐行填写 CIDR、指定端口或全部端口，以及“允许 HTTP”，统一显式保存。支持 0–256 条规则；CIDR 须为已掩码的规范形式，完整位于 `10.0.0.0/8`、`172.16.0.0/12`、`192.168.0.0/16` 或 `fc00::/7`。指定端口为逗号分隔的 1–256 个互异整数，范围 1–65535，不接受范围写法或前导零；全部端口必须主动选择，空输入不表示全部。新行默认不允许 HTTP，重复规则拒绝，保存空列表移除内网放行例外。无变化或非法输入不发 PUT；不提供原始 JSON 编辑、规则优先级或客户端试连。

当前读取值、编辑基线、草稿与历史 receipt 分开保存。严格 receipt 只确认原命令及其版本，不包含当前规则；GET 相同值或更高版本也不能确认原写。版本冲突保留草稿，须明确重读、核对并采用新基线，不自动合并或换版本覆盖。确认后至多刷新一次；刷新失败保留历史确认和已提交草稿，显式重试只发 GET，成功新读取前不能把旧草稿继续当作新编辑基线。

没有 command lookup。结果未确认时保留私有原 method/path/key/body/expected_version，仅由用户点击“用原请求确认”；不自动重试或更换 key。恢复要求同完整 userID/sessionID/epoch 且原捕获 CSRF 仍为当前合法值，token 变化销毁旧 intent。已派发后的 5xx（包括 `503/not_started`）或先前未知后的业务拒绝均不能证明未提交；放弃只结束本地追踪，不承诺回滚。

出站规则是第十个独立状态域，仍共用唯一 Cookie 请求 owner。30 秒可见截止、取消、离页或身份变化不提前释放实际 fetch/body/read/cancel 尾部；取消读取同步转为可显式重读状态，不自动补发。同 Session checking/失败保留草稿与待决确认，恢复后使用当前合法焦点；身份失效、CSRF 变化或当前权限拒绝清除旧恢复资格，旧待决确认不能借新身份继续。桌面表格与窄屏逐字段布局支持长 CIDR、端口换行及键盘操作。

完整前端检查通过 42 文件、1517 项测试及格式/类型/生产构建；四个新真实组、三个必要旧组按固定版本组合通过，独立 A/B 通过，原失败与清理复核记录保留。以下精确目标逐组执行，沿前述锁依赖、生产 dist、Go1.27.1、MinIO、PG17.8+ 与任务自有独占 runtime 准备；保持浏览器 45 秒、顶层 2 分钟、每包 6 分钟、race/count1、workers=1/retries=0，实际退出与自有资源双扫完成后再启动下一组：

```sh
npm run check --prefix web
npm run test:unit --prefix web -- src/tests/outbound-rules.spec.ts src/tests/system-outbound-policy-client.spec.ts src/tests/system-outbound-policy-state.spec.ts src/tests/system-outbound-policy.spec.ts
sh scripts/test-security.sh -run '^TestAccountSystemOutboundPolicyWebRulesAndRead$'
sh scripts/test-security.sh -run '^TestAccountSystemOutboundPolicyWebMutationAndRecovery$'
sh scripts/test-security.sh -run '^TestAccountSystemOutboundPolicyWebAuthorityAndOwnership$'
sh scripts/test-security.sh -run '^TestAccountSystemOutboundPolicyWebNavigationAndLayouts$'
sh scripts/test-security.sh -run '^TestAccountSystemSMTPDeliveryWebNavigationAndLayouts$'
sh scripts/test-security.sh -run '^TestAccountSystemSMTPSettingsWebOutcomeRecovery$'
sh scripts/test-security.sh -run '^TestAccountSystemSMTPDeliveryWebOutcomeRecovery$'
```

浅深主题 × 390/768/1024/1440 的八张页面图由作者逐张查看，900px 视口截图不代表整页覆盖。保存仅管理规则，不证明镜像健康、目标可达或外部邮箱送达，不新增 Runtime/Provider/MCP/Project 连接调用。生产 SPA 托管、真实 Vite 代理浏览器、原生 BFCache/缩放与完整 D27 不在本次通过范围内。

## 系统审计

[系统审计工作项](../work-items/d27-system-audit-ui.md)提供 `/system/audit`，位于“审计 → 系统审计”。系统设置现为九叶四组，该路径是第十三个精确登录返回目标。页面仅向当前合法管理员开放，通过 GET `/api/v1/system/audit` 和 GET `/api/v1/system/audit/{audit_id}` 观察 System 安全事件；不提供 Project 审计、写操作、导出或关联业务正文补读。

十四个过滤字段与每页数量分别编辑，点击“应用筛选”才读取；每页为 1–200 项，默认 50。时间使用完整时区输入，记录统一显示 UTC。53 个合法 action 过滤输入与 37 种正式输出投影分开校验，不能把过滤选项当作已存在的输出种类。分页只使用已取得的 cursor，刷新回到第一页，不猜总数或全域完整性；空结果、失败、取消和失权分别显示。两个成功 JSON 读口的实际上限为 1MiB，完整 EOF、UTF-8 与 typed DTO 校验通过后才发布整页或详情，错误不发布部分记录。

详情在同一叶子内联打开，按固定字段和标签显示安全 metadata，不 dump 原始响应或自动链接；返回时复用本页列表并恢复当前合法行按钮或标题焦点。长 ID、MIME 和计数可换行，桌面七列、窄屏逐字段堆叠。筛选草稿只属于当前页面实例，真正离页或 checking 卸载即清除；同 Session 恢复建立新实例，从默认第一页读取，不复活旧过滤或详情。离页若被后续守卫挡回，已停止的读取须显式刷新。

系统审计是第十一个独立状态域，专用于只读审计，共用唯一 Cookie 请求 owner。GET 不带命令 key、body 或 CSRF，不产生写 intent/receipt；30 秒可见截止、取消或离页不提前释放实际 fetch/body/read/cancel 尾部。取消后须显式重读，新实例也须等待旧实际尾部结束。当前 401/403 沿身份与权限边界清理，旧实例或旧身份的迟到结果不发布。空闲 `pageshow` 的 Session 检查与新实例等待旧读取尾部是分别验证的场景，不把合成事件称作真实 BFCache。

页面阶段完整检查为 46 文件、1763 项测试及格式/类型/构建通过；最后两项局部 CSS 修复另通过原 12 项页面代表、格式与含类型检查的构建。作者真实结果按 `read03/input03`、`authority03/input05`、`navigation04/input08` 及两项旧导航 `input08` 组合接受，独立最终两代表在最终输入上通过；不称全部场景在最终输入或 main 重跑。原读取、权限与布局失败及后置清理恢复记录均保留。

以下是精确复验入口。真实组沿前述锁依赖、生产 dist、Go1.27.1、MinIO、PG17.8+ 与自有独占短 runtime 准备，逐组执行；保持浏览器 45 秒、顶层 2 分钟、每包 6 分钟、race/count1、workers=1/retries=0，实际退出及资源/PID 双扫后再启动下一组：

```sh
npm run check --prefix web
npm run test:unit --prefix web -- src/tests/system-audit-client.spec.ts src/tests/system-audit-metadata.spec.ts src/tests/system-audit-state.spec.ts src/tests/system-audit.spec.ts
sh scripts/test-security.sh -run '^TestAccountSystemAuditWebReadAndFilters$'
sh scripts/test-security.sh -run '^TestAccountSystemAuditWebAuthorityAndOwnership$'
sh scripts/test-security.sh -run '^TestAccountSystemAuditWebNavigationAndLayouts$'
sh scripts/test-security.sh -run '^TestAccountSystemOutboundPolicyWebNavigationAndLayouts$'
sh scripts/test-security.sh -run '^TestAccountSystemSMTPDeliveryWebNavigationAndLayouts$'
```

最终 navigation 的 light/dark × 390/768/1024/1440 八图由作者逐张查看，均为 900px 高的局部视口，不代表整页覆盖；768px 的表格仍较密，长值有较多换行。必要旧导航各保留八图，仅各两张代表作本次图审。实际浏览器使用自有服务器托管固定生产 dist；生产 SPA 托管、真实 Vite 代理浏览器、原生缩放、Runtime 连接或完整 D27 不在本卡通过范围内。

## 运行信息

[运行信息工作项](../work-items/d27-system-runtime-information-ui.md)提供 `/system/runtime-information`，位于“平台配置 → 运行信息”。系统设置现为十叶四组，该路径是第十四个精确登录返回目标。页面仅向当前合法管理员开放，通过 GET `/api/v1/system/runtime-information` 读取服务端已记录的缓存快照；不自动轮询，点击“重新读取运行信息”也不触发健康检查或修复。

页面分开显示 Central、数据库、对象存储聚合观测和就绪状态。Central 未记录构建版本时明确显示未知；数据库当前聚合状态与最近成功记录的 PostgreSQL/pgvector 版本和时间分开表达。MinIO 仅显示聚合状态与历史成功接收时间，不补版本或逐项诊断。四个 UTC 微秒时刻按返回原文显示，不用客户端时钟重算陈旧状态；`ready: false` 仍显示尚未就绪，读取成功不表示 ready 状态改变。成功 JSON 上限为 16KiB，完整读取与严格 DTO 校验通过后才发布快照。

运行信息是第十二个独立状态域，与原十一域共用唯一 Cookie 请求 owner。GET 不携带 body、命令 key 或 CSRF，不产生写 intent/receipt。30 秒可见截止、取消、离页或身份变化不提前释放实际 fetch/body/read/cancel 尾部；等待读取也可取消。取消后清除旧观察，须显式重读；离页先取消，实际卸载才销毁页面实例，后续守卫挡回时可明确重读。checking 卸载旧实例，同身份恢复创建新实例并等待旧实际尾部，不复活旧快照。合法当前 401/403 沿原身份和权限规则处理；显示“会话尚未确认”时，点击“检查当前会话”走正式恢复流程，不把 Runtime 401 当作已经完成 Session 恢复。旧实例、旧身份与旧异步焦点续体不能回写当前页面。

页面阶段完整检查通过 1875 项测试及格式/类型/构建，另有公开 App 八项组合。作者最终四组 `read03`、`navigation01`、旧 Audit 导航及旧 Outbound 导航均在 `input04` 通过；独立两个真实代表在最终输入上通过，并完成实际退出和双扫清理。原 `read01` 的取消后 done/EOF 观察前提失败、`read02` 缺少显式 Session 恢复步骤的失败均保留；后续仅修新 browser 测试，不改产品来适配断言。

以下为精确复验入口。真实组沿前述锁依赖、生产 dist、Go1.27.1、MinIO、PG17.8+ 与自有独占短 runtime 准备，逐组执行；保持浏览器 45 秒、顶层 2 分钟、每包 6 分钟、race/count1、workers=1/retries=0，实际退出及自有资源/PID 双扫完成后再运行下一组：

```sh
npm run check --prefix web
npm run test:unit --prefix web -- src/tests/system-runtime-information-client.spec.ts src/tests/system-runtime-information-state.spec.ts src/tests/system-runtime-information.spec.ts
sh scripts/test-security.sh -run '^TestAccountSystemRuntimeInformationWebReadAndAuthority$'
sh scripts/test-security.sh -run '^TestAccountSystemRuntimeInformationWebNavigationAndLifecycle$'
sh scripts/test-security.sh -run '^TestAccountSystemAuditWebNavigationAndLayouts$'
sh scripts/test-security.sh -run '^TestAccountSystemOutboundPolicyWebNavigationAndLayouts$'
```

Runtime 的 light/dark × 390/768/1024/1440 八图由作者逐张查看，主线程仅查看 light390、dark768；两个旧导航各保留八图，作者各查看 light390、dark1440，合计实际查看十二张。图均为 900px 高局部视口，不代表整页覆盖。原生焦点、Tab、Drawer 与实际取消尾部另由相应断言验证；合成 `pageshow` 不称真实 BFCache，native reader 的 done 也可能由取消产生，不能单独证明完整响应或 owner 已释放。本次真实运行基于私有固定 `a0e73bd` 与冻结 dist，不称 main 动态实测、生产 SPA 托管、健康探测、ready 行为改变，亦不代表完整 D27/D28。

## Debug 与主题

Debug 覆盖按钮、表单、选择、树、内容容器、消息、浮层、异常和导航组件，以及实际 CSS 参数。演示操作只修改本地状态，消息使用文本插值，搜索范围仅为组件目录。

Debug 路由及动态导入使用编译期 `import.meta.env.DEV` 条件。生产构建不包含 Debug 导航、路由、示例代码及数据；`/debug` 在生产预览中进入未找到页面。这里的“开发环境”由 Vite 编译模式决定，不是后端用户权限。

未认证时主题跟随系统，入口在首次绘制前应用系统主题。正式页面在当前 Session 确认后应用服务端用户偏好；外观设置可以即时预览，只有明确保存才写入账号偏好。Debug 可以即时切换浅深主题，选择仅保存在当前会话内存中，不存储账号偏好，也不代表个人设置接口。

已确认的[样式规范](../../frontend-design/styles/README.md)保持不变。旧独立 HTML 展示已由 Vue Debug 替代。

## 使用组件

```vue
<script setup lang="ts">
import { ref } from 'vue'
import { UiButton, UiField, UiInput, UiDialog } from '../../../web-crape/src/components/ui'
import { useActionFeedback } from '../../../web-crape/src/composables/useActionFeedback'

const name = ref('')
const open = ref(false)
const { state, error, run } = useActionFeedback()

async function save() {
  await run(async () => {
    // 在正式页面接入实际保存逻辑；完成后才显示成功。
  })
}
</script>

<template>
  <UiField label="名称" :error="error">
    <template #default="field">
      <UiInput :id="field.id" v-model="name" :invalid="field.invalid" :aria-describedby="field.describedby" />
    </template>
  </UiField>
  <UiButton :state="state" success-label="已保存" @click="save">保存修改</UiButton>
  <UiDialog v-model:open="open" title="确认">
    <p>正文与操作区分别组织。</p>
    <template #footer="{ close }">
      <UiButton @click="close">取消</UiButton>
    </template>
  </UiDialog>
</template>
```

示例导入路径仅表示源文件位置；实际页面按所在目录调整相对路径。组件支持的 props / events 以源代码类型为准，使用说明见[公共组件接口](components.md)。

检查范围和结果见[验证记录](verification.md)。
